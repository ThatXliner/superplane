package organizations

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/core"
	"github.com/superplanehq/superplane/pkg/database"
	"github.com/superplanehq/superplane/pkg/models"
	"github.com/superplanehq/superplane/test/support"
	"github.com/superplanehq/superplane/test/support/impl"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func Test__RefreshIntegration(t *testing.T) {
	r := support.Setup(t)
	defer r.Close()

	t.Run("missing integration returns not found", func(t *testing.T) {
		_, err := RefreshIntegration(
			context.Background(),
			r.Registry,
			nil,
			"",
			"",
			r.Organization.ID.String(),
			uuid.NewString(),
		)
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("non-ready integration returns failed precondition", func(t *testing.T) {
		integration, err := models.CreateIntegration(
			uuid.New(), r.Organization.ID, "planelet",
			support.RandomName("integration"), map[string]any{},
		)
		require.NoError(t, err)

		_, err = RefreshIntegration(
			context.Background(), r.Registry, nil, "", "",
			r.Organization.ID.String(), integration.ID.String(),
		)
		require.Error(t, err)
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	})

	t.Run("non-planelet integration is rejected", func(t *testing.T) {
		r.Registry.Integrations["dummy"] = impl.NewDummyIntegration(impl.DummyIntegrationOptions{})
		integration, err := models.CreateIntegration(
			uuid.New(), r.Organization.ID, "dummy",
			support.RandomName("integration"), map[string]any{},
		)
		require.NoError(t, err)
		require.NoError(t, database.Conn().Model(integration).Update("state", models.IntegrationStateReady).Error)

		_, err = RefreshIntegration(
			context.Background(), r.Registry, nil, "", "",
			r.Organization.ID.String(), integration.ID.String(),
		)
		require.Error(t, err)
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	})

	t.Run("ready planelet integration re-syncs and returns it", func(t *testing.T) {
		synced := false
		r.Registry.Integrations["planelet"] = impl.NewDummyIntegration(impl.DummyIntegrationOptions{
			OnSync: func(ctx core.SyncContext) error {
				synced = true
				return nil
			},
		})
		integration, err := models.CreateIntegration(
			uuid.New(), r.Organization.ID, "planelet",
			support.RandomName("integration"), map[string]any{},
		)
		require.NoError(t, err)
		require.NoError(t, database.Conn().Model(integration).Update("state", models.IntegrationStateReady).Error)

		resp, err := RefreshIntegration(
			context.Background(), r.Registry, nil, "", "",
			r.Organization.ID.String(), integration.ID.String(),
		)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Integration)
		assert.True(t, synced)
	})

	t.Run("sync failure marks error and returns failed precondition", func(t *testing.T) {
		r.Registry.Integrations["planelet"] = impl.NewDummyIntegration(impl.DummyIntegrationOptions{
			OnSync: func(ctx core.SyncContext) error {
				return errors.New("manifest unreachable")
			},
		})
		integration, err := models.CreateIntegration(
			uuid.New(), r.Organization.ID, "planelet",
			support.RandomName("integration"), map[string]any{},
		)
		require.NoError(t, err)
		require.NoError(t, database.Conn().Model(integration).Update("state", models.IntegrationStateReady).Error)

		_, err = RefreshIntegration(
			context.Background(), r.Registry, nil, "", "",
			r.Organization.ID.String(), integration.ID.String(),
		)
		require.Error(t, err)
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))

		reloaded, ferr := models.FindIntegration(r.Organization.ID, integration.ID)
		require.NoError(t, ferr)
		assert.Equal(t, models.IntegrationStateError, reloaded.State)
	})

	t.Run("recovers to ready after a failed refresh then a successful one", func(t *testing.T) {
		r.Registry.Integrations["planelet"] = impl.NewDummyIntegration(impl.DummyIntegrationOptions{
			OnSync: func(ctx core.SyncContext) error { return errors.New("boom") },
		})
		integration, err := models.CreateIntegration(
			uuid.New(), r.Organization.ID, "planelet",
			support.RandomName("integration"), map[string]any{},
		)
		require.NoError(t, err)
		require.NoError(t, database.Conn().Model(integration).Update("state", models.IntegrationStateReady).Error)

		_, err = RefreshIntegration(context.Background(), r.Registry, nil, "", "",
			r.Organization.ID.String(), integration.ID.String())
		require.Error(t, err)
		errored, ferr := models.FindIntegration(r.Organization.ID, integration.ID)
		require.NoError(t, ferr)
		require.Equal(t, models.IntegrationStateError, errored.State)

		r.Registry.Integrations["planelet"] = impl.NewDummyIntegration(impl.DummyIntegrationOptions{
			OnSync: func(ctx core.SyncContext) error { return nil },
		})
		_, err = RefreshIntegration(context.Background(), r.Registry, nil, "", "",
			r.Organization.ID.String(), integration.ID.String())
		require.NoError(t, err)
		recovered, ferr := models.FindIntegration(r.Organization.ID, integration.ID)
		require.NoError(t, ferr)
		assert.Equal(t, models.IntegrationStateReady, recovered.State)
	})
}

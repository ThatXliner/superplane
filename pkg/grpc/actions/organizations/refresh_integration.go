package organizations

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"github.com/superplanehq/superplane/pkg/core"
	"github.com/superplanehq/superplane/pkg/database"
	"github.com/superplanehq/superplane/pkg/logging"
	"github.com/superplanehq/superplane/pkg/models"
	"github.com/superplanehq/superplane/pkg/oidc"
	pb "github.com/superplanehq/superplane/pkg/protos/organizations"
	"github.com/superplanehq/superplane/pkg/registry"
	"github.com/superplanehq/superplane/pkg/workers/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// planeletAppName is the only integration whose action/trigger manifest is
// fetched from a remote server, so it is the only one that can be "refreshed".
const planeletAppName = "planelet"

func RefreshIntegration(
	ctx context.Context,
	reg *registry.Registry,
	oidcProvider oidc.Provider,
	baseURL string,
	webhooksBaseURL string,
	orgID string,
	integrationID string,
) (*pb.RefreshIntegrationResponse, error) {
	org, err := uuid.Parse(orgID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid organization ID")
	}

	id, err := uuid.Parse(integrationID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid integration ID")
	}

	instance, err := models.FindIntegration(org, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.NotFound, "integration not found")
		}
		return nil, status.Error(codes.Internal, "failed to load integration")
	}

	if instance.AppName != planeletAppName {
		return nil, status.Error(codes.FailedPrecondition, "only Planelet integrations can be refreshed")
	}

	if instance.State != models.IntegrationStateReady {
		return nil, status.Error(codes.FailedPrecondition, "integration is not ready")
	}

	integration, err := reg.GetIntegration(instance.AppName)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "integration %s is unavailable", instance.AppName)
	}

	encryptor := reg.Encryptor
	integrationCtx := contexts.NewIntegrationContext(database.Conn(), nil, instance, encryptor, reg, nil)
	syncErr := integration.Sync(core.SyncContext{
		Logger:          logging.ForIntegration(*instance),
		HTTP:            reg.HTTPContext(),
		Integration:     integrationCtx,
		Configuration:   instance.Configuration.Data(),
		BaseURL:         baseURL,
		WebhooksBaseURL: webhooksBaseURL,
		OrganizationID:  instance.OrganizationID.String(),
		OIDC:            oidcProvider,
	})

	if syncErr != nil {
		instance.State = models.IntegrationStateError
		instance.StateDescription = fmt.Sprintf("Refresh failed: %v", syncErr)
		if saveErr := database.Conn().Save(instance).Error; saveErr != nil {
			log.WithError(saveErr).Warn("failed to persist integration error state after refresh")
		}
		return nil, status.Errorf(codes.FailedPrecondition, "failed to refresh integration: %v", syncErr)
	}

	instance.StateDescription = ""
	if err := database.Conn().Save(instance).Error; err != nil {
		return nil, status.Error(codes.Internal, "failed to save integration after refresh")
	}

	nodeRefs, err := models.ListIntegrationNodeReferences(instance.ID)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to load integration references")
	}

	proto, err := serializeIntegration(reg, instance, nodeRefs)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to serialize integration")
	}

	return &pb.RefreshIntegrationResponse{Integration: proto}, nil
}

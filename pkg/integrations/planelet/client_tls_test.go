package planelet

import (
	"crypto/tls"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/registry"
	"github.com/superplanehq/superplane/test/support/contexts"
)

func TestClientLiveCertificateTrust(t *testing.T) {
	if os.Getenv("SUPERPLANE_LIVE_TLS_TEST") != "1" {
		t.Skip("set SUPERPLANE_LIVE_TLS_TEST=1 to check public certificate test hosts")
	}
	previousTimeout := http.DefaultClient.Timeout
	http.DefaultClient.Timeout = 20 * time.Second
	t.Cleanup(func() { http.DefaultClient.Timeout = previousTimeout })

	for _, transport := range []string{"default", "registry"} {
		for _, host := range []string{"dv-g3b-emudhra.we-have-a-problem.com", "dv-g3b-aaa-emudhra.we-have-a-problem.com", "example.com"} {
			t.Run(transport+"/"+host, func(t *testing.T) {
				integration := &contexts.IntegrationContext{Configuration: map[string]any{"serverUrl": "https://" + host}}
				client, err := NewClient(integration)
				require.NoError(t, err)
				if transport == "registry" {
					httpContext, err := registry.NewHTTPContext(registry.HTTPOptions{})
					require.NoError(t, err)
					client, err = NewClientWithHTTP(integration, httpContext)
					require.NoError(t, err)
				}
				status := 0
				do := client.httpDo
				client.httpDo = func(req *http.Request) (*http.Response, error) {
					resp, err := do(req)
					if resp != nil {
						status = resp.StatusCode
					}
					return resp, err
				}
				var certErr *tls.CertificateVerificationError
				for attempt := 1; attempt <= 3; attempt++ {
					_, err = client.FetchManifest()
					if status != 0 || errors.As(err, &certErr) || attempt == 3 {
						break
					}
					t.Logf("attempt=%d inconclusive transport error: %v", attempt, err)
				}
				if host == "example.com" {
					// example.com/manifest is not a Planelet manifest; reaching HTTP proves TLS succeeded.
					require.NotZero(t, status, "HTTPS control did not reach HTTP: %v", err)
					require.False(t, errors.As(err, &certErr))
				} else {
					require.True(t, errors.As(err, &certErr), "expected certificate rejection, got %v", err)
					require.Zero(t, status)
				}
				t.Logf("transport=%s host=%s status=%d error=%v", transport, host, status, err)
			})
		}
	}
}

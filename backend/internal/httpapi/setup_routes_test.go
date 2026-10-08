package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/setup"
)

type setupActions struct {
	completed bool
	entra     bool
	token     string
	config    setup.Configuration
	err       error
	center    setup.CenterStatus
	update    setup.UpdateCenterConfigurationCommand
	verify    setup.VerifyObjectStorageCommand
	backup    setup.AcceptBackupEvidenceCommand
}

func (a *setupActions) AcceptBackupEvidence(
	_ context.Context, command setup.AcceptBackupEvidenceCommand,
) (setup.CenterStatus, error) {
	a.backup = command
	return setup.CenterStatus{ConfigurationVersion: command.ExpectedVersion + 1}, a.err
}

func (a *setupActions) VerifyObjectStorage(
	_ context.Context, command setup.VerifyObjectStorageCommand,
) (setup.CenterStatus, error) {
	a.verify = command
	return setup.CenterStatus{ConfigurationVersion: command.ExpectedVersion + 1}, a.err
}

func (a *setupActions) Status(context.Context) (bool, bool, error) {
	return a.completed, a.entra, a.err
}
func (a *setupActions) CenterStatus(
	context.Context,
	authorization.Principal,
) (setup.CenterStatus, error) {
	return a.center, a.err
}
func (a *setupActions) UpdateCenterConfiguration(
	_ context.Context,
	command setup.UpdateCenterConfigurationCommand,
) (setup.CenterStatus, error) {
	a.update = command
	return setup.CenterStatus{
		ConfigurationVersion: command.ExpectedVersion + 1,
	}, a.err
}

func TestSetupCenterRequiresAuthenticatedOrganizationRead(t *testing.T) {
	actions := &setupActions{center: setup.CenterStatus{
		Sections: []setup.CenterSection{{
			Key: "backups", Label: "Backup and PITR", State: "missing",
		}},
	}}
	handler := NewRouter(Dependencies{
		Setup: actions,
		Principal: func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{
				ID: "admin-id", Scope: scope.Principal{MSPID: "msp-id"},
				Capabilities: authorization.NewCapabilitySet("organization.read"),
			}, nil
		},
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/setup/center", nil),
	)
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"state":"missing"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSetupCenterRetiresArbitraryConfigurationReferences(t *testing.T) {
	actions := &setupActions{}
	handler := NewRouter(Dependencies{
		Setup: actions,
		Principal: func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{
				ID: "admin-id", Scope: scope.Principal{MSPID: "msp-id"},
				Capabilities: authorization.NewCapabilitySet("organization.manage"),
			}, nil
		},
	})
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/setup/center/configuration",
		strings.NewReader(`{
			"expected_version":3,
			"intake":{"graph_mailbox":"configured"},
			"object_storage":{"bucket_ref":"env://RARITY_S3_BUCKET"},
			"backups":{"repository_ref":"env://PGBACKREST_REPO"},
			"reason":"Record deployment references"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusGone ||
		!strings.Contains(response.Body.String(), "setup_reference_editor_retired") ||
		actions.update.ExpectedVersion != 0 {
		t.Fatalf(
			"status=%d update=%+v body=%s",
			response.Code,
			actions.update,
			response.Body.String(),
		)
	}
}

func TestSetupCenterVerifiesObjectStorage(t *testing.T) {
	actions := &setupActions{}
	handler := NewRouter(Dependencies{
		Setup: actions,
		Principal: func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{
				ID: "admin-id", Scope: scope.Principal{MSPID: "msp-id"},
				Capabilities: authorization.NewCapabilitySet("organization.manage"),
			}, nil
		},
	})
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/setup/center/object-storage/verify",
		strings.NewReader(`{"expected_version":3,"reason":"Verify bucket"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || actions.verify.ExpectedVersion != 3 ||
		actions.verify.Reason != "Verify bucket" || actions.verify.Source == "" {
		t.Fatalf("status=%d verify=%+v body=%s",
			response.Code, actions.verify, response.Body.String())
	}
}

func TestSetupCenterAcceptsNarrowSignedBackupEvidence(t *testing.T) {
	actions := &setupActions{}
	handler := NewRouter(Dependencies{Setup: actions})
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/setup/center/backups/evidence",
		strings.NewReader(`{
		  "expected_version":3,
		  "payload":{"msp_id":"msp-id","nonce":"nonce"}
		}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "BackupEvidence signature-value")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || actions.backup.Signature != "signature-value" ||
		actions.backup.ExpectedVersion != 3 {
		t.Fatalf("status=%d backup=%+v body=%s",
			response.Code, actions.backup, response.Body.String())
	}
}

func (a *setupActions) Bootstrap(_ context.Context, token string, config setup.Configuration) error {
	a.token, a.config = token, config
	return a.err
}

func TestSetupStatusExposesOnlyCompletionState(t *testing.T) {
	handler := NewRouter(Dependencies{Setup: &setupActions{}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil))
	if response.Code != http.StatusOK ||
		response.Body.String() != "{\"bootstrap_available\":true,\"completed\":false,\"entra_available\":false}\n" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBootstrapRequiresHTTPSAndMapsTokenOutsideBody(t *testing.T) {
	actions := &setupActions{}
	handler := NewRouter(Dependencies{Setup: actions})
	body := `{
		"organization_id":"00000000-0000-4000-8000-000000000001",
		"organization_name":"Rarity MSP","organization_display_id":"RARITY",
		"entra_tenant_id":"tenant","entra_client_id":"client",
		"entra_client_secret":"synthetic-client-secret",
		"entra_redirect_url":"https://rarity.example/auth/entra/callback",
		"admin_email":"admin@example.com","admin_display_name":"Admin",
		"admin_entra_subject":"subject","recovery_username":"recovery",
		"recovery_password":"correct horse battery staple",
		"recovery_allowed_cidrs":["10.0.0.0/8"],
		"intake":{},"object_storage":{},"backups":{}
	}`
	insecure := httptest.NewRequest(http.MethodPost, "http://rarity.example/api/v1/setup/bootstrap", strings.NewReader(body))
	insecure.Header.Set("Authorization", "Bootstrap secret-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, insecure)
	if response.Code != http.StatusUpgradeRequired {
		t.Fatalf("insecure status=%d", response.Code)
	}

	secure := httptest.NewRequest(http.MethodPost, "https://rarity.example/api/v1/setup/bootstrap", strings.NewReader(body))
	secure.Header.Set("Authorization", "Bootstrap secret-token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, secure)
	if response.Code != http.StatusCreated || actions.token != "secret-token" ||
		actions.config.OrganizationDisplay != "RARITY" {
		t.Fatalf("status=%d token=%q config=%+v body=%s", response.Code, actions.token, actions.config, response.Body.String())
	}
}

func TestBootstrapClosesWithoutLeakingTokenValidity(t *testing.T) {
	handler := NewRouter(Dependencies{Setup: &setupActions{err: setup.ErrBootstrapClosed}})
	request := httptest.NewRequest(
		http.MethodPost, "https://rarity.example/api/v1/setup/bootstrap",
		strings.NewReader(`{"organization_name":"value"}`),
	)
	request.Header.Set("Authorization", "Bootstrap expired-or-used")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusGone ||
		!strings.Contains(response.Body.String(), `"bootstrap_closed"`) ||
		strings.Contains(response.Body.String(), "expired-or-used") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

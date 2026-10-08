package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/identity"
)

type entraSettingsActionsStub struct {
	configured identity.ConfigureEntraCommand
	verified   identity.VerifyEntraCommand
	disabled   identity.DisableEntraCommand
}

func (*entraSettingsActionsStub) Get(context.Context, authorization.Principal) (identity.EntraSettings, error) {
	return identity.EntraSettings{State: "not_connected", Version: 1}, nil
}
func (s *entraSettingsActionsStub) Configure(_ context.Context, command identity.ConfigureEntraCommand) (identity.EntraSettings, error) {
	s.configured = command
	return identity.EntraSettings{
		State: "verification_required", Version: command.ExpectedVersion + 1,
		TenantID: command.TenantID, ClientID: command.ClientID,
		RedirectURL: command.RedirectURL, CredentialConfigured: true,
	}, nil
}
func (s *entraSettingsActionsStub) Verify(_ context.Context, command identity.VerifyEntraCommand) (identity.EntraSettings, error) {
	s.verified = command
	return identity.EntraSettings{State: "connected", Version: command.ExpectedVersion + 1, CredentialConfigured: true}, nil
}
func (s *entraSettingsActionsStub) Disable(_ context.Context, command identity.DisableEntraCommand) (identity.EntraSettings, error) {
	s.disabled = command
	return identity.EntraSettings{State: "not_connected", Version: command.ExpectedVersion + 1}, nil
}

func TestEntraSettingsRoutesKeepClientSecretWriteOnlyAndRequireVersions(t *testing.T) {
	actions := &entraSettingsActionsStub{}
	handler := NewRouter(Dependencies{Principal: recoveryManagerPrincipal, EntraSettings: actions})

	configure := httptest.NewRequest(http.MethodPut, "/api/v1/admin/identity/entra", bytes.NewBufferString(
		`{"expected_version":1,"tenant_id":"tenant","client_id":"client","client_secret":"never-return-secret","redirect_url":"https://rarity.example/auth/callback","reason":"connect SSO"}`,
	))
	configureResponse := httptest.NewRecorder()
	handler.ServeHTTP(configureResponse, configure)
	if configureResponse.Code != http.StatusOK || actions.configured.ExpectedVersion != 1 ||
		actions.configured.ClientSecret != "never-return-secret" ||
		strings.Contains(configureResponse.Body.String(), "never-return-secret") ||
		strings.Contains(configureResponse.Body.String(), "client_secret") {
		t.Fatalf("status=%d command=%+v body=%s", configureResponse.Code, actions.configured, configureResponse.Body.String())
	}

	verify := httptest.NewRequest(http.MethodPost, "/api/v1/admin/identity/entra/verify", bytes.NewBufferString(
		`{"expected_version":2,"reason":"discovery verified"}`,
	))
	verifyResponse := httptest.NewRecorder()
	handler.ServeHTTP(verifyResponse, verify)
	if verifyResponse.Code != http.StatusOK || actions.verified.ExpectedVersion != 2 {
		t.Fatalf("status=%d command=%+v body=%s", verifyResponse.Code, actions.verified, verifyResponse.Body.String())
	}

	disable := httptest.NewRequest(http.MethodPost, "/api/v1/admin/identity/entra/disable", bytes.NewBufferString(
		`{"expected_version":3,"reason":"return to local only"}`,
	))
	disableResponse := httptest.NewRecorder()
	handler.ServeHTTP(disableResponse, disable)
	if disableResponse.Code != http.StatusOK || actions.disabled.ExpectedVersion != 3 {
		t.Fatalf("status=%d command=%+v body=%s", disableResponse.Code, actions.disabled, disableResponse.Body.String())
	}
}

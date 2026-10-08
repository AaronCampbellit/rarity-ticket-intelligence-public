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
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type breakGlassManagementActionsStub struct {
	created  identity.CreateBreakGlassAccountCommand
	disabled identity.DisableBreakGlassAccountCommand
	reset    identity.ResetBreakGlassPasswordCommand
	networks identity.UpdateBreakGlassNetworksCommand
	err      error
}

func (s *breakGlassManagementActionsStub) Create(_ context.Context, command identity.CreateBreakGlassAccountCommand) (identity.ManagedBreakGlassAccount, error) {
	s.created = command
	return identity.ManagedBreakGlassAccount{
		ID: "account-id", TechnicianID: command.Principal.ID,
		Username: command.Username, AllowedCIDRs: command.AllowedCIDRs,
		Enabled: true, Version: 1,
	}, nil
}
func (*breakGlassManagementActionsStub) List(context.Context, authorization.Principal) ([]identity.ManagedBreakGlassAccount, error) {
	return []identity.ManagedBreakGlassAccount{{ID: "account-id", Username: "recovery", Enabled: true, Version: 1}}, nil
}
func (s *breakGlassManagementActionsStub) Disable(_ context.Context, command identity.DisableBreakGlassAccountCommand) (identity.ManagedBreakGlassAccount, error) {
	s.disabled = command
	return identity.ManagedBreakGlassAccount{ID: command.AccountID, Enabled: false, Version: command.ExpectedVersion + 1}, nil
}
func (s *breakGlassManagementActionsStub) ResetPassword(_ context.Context, command identity.ResetBreakGlassPasswordCommand) (identity.ManagedBreakGlassAccount, error) {
	s.reset = command
	return identity.ManagedBreakGlassAccount{ID: command.AccountID, Enabled: true, Version: command.ExpectedVersion + 1}, s.err
}
func (s *breakGlassManagementActionsStub) UpdateNetworks(_ context.Context, command identity.UpdateBreakGlassNetworksCommand) (identity.ManagedBreakGlassAccount, error) {
	s.networks = command
	return identity.ManagedBreakGlassAccount{ID: command.AccountID, AllowedCIDRs: command.AllowedCIDRs, Enabled: true, Version: command.ExpectedVersion + 1}, nil
}

func recoveryManagerPrincipal(*http.Request) (authorization.Principal, error) {
	return authorization.Principal{
		ID: "technician-id", Scope: scope.Principal{MSPID: "msp-id"},
		Capabilities: authorization.NewCapabilitySet("organization.manage"),
	}, nil
}

func TestBreakGlassManagementCreateKeepsPasswordWriteOnly(t *testing.T) {
	actions := &breakGlassManagementActionsStub{}
	handler := NewRouter(Dependencies{Principal: recoveryManagerPrincipal, BreakGlassManagement: actions})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/break-glass-accounts", bytes.NewBufferString(
		`{"username":"recovery","password":"never-return-this-password","allowed_cidrs":["10.0.0.0/24"],"reason":"outage access"}`,
	))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || actions.created.Password != "never-return-this-password" ||
		actions.created.Principal.Scope.MSPID != "msp-id" ||
		strings.Contains(response.Body.String(), "never-return-this-password") ||
		strings.Contains(response.Body.String(), "password") {
		t.Fatalf("status=%d command=%+v response=%s", response.Code, actions.created, response.Body.String())
	}
}

func TestLocalAdministratorRouteCreatesCredentialAndKeepsPasswordWriteOnly(t *testing.T) {
	actions := &breakGlassManagementActionsStub{}
	handler := NewRouter(Dependencies{
		Principal: recoveryManagerPrincipal, BreakGlassManagement: actions,
	})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/admin/local-administrators",
		bytes.NewBufferString(
			`{"email":"local@example.test","display_name":"Local Admin","username":"local-admin","password":"never-return-this-password","allowed_cidrs":["192.168.86.0/24"],"reason":"installation administration"}`,
		),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated ||
		actions.created.Username != "local-admin" ||
		actions.created.Email != "local@example.test" ||
		actions.created.DisplayName != "Local Admin" ||
		strings.Contains(response.Body.String(), "never-return-this-password") {
		t.Fatalf("status=%d command=%+v response=%s", response.Code, actions.created, response.Body.String())
	}
}

func TestBreakGlassManagementDisableUsesTrustedPrincipalAndVersion(t *testing.T) {
	actions := &breakGlassManagementActionsStub{}
	handler := NewRouter(Dependencies{Principal: recoveryManagerPrincipal, BreakGlassManagement: actions})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/break-glass-accounts/account-id/disable", bytes.NewBufferString(
		`{"expected_version":3,"reason":"retired credential"}`,
	))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || actions.disabled.AccountID != "account-id" ||
		actions.disabled.ExpectedVersion != 3 || actions.disabled.Principal.ID != "technician-id" {
		t.Fatalf("status=%d command=%+v response=%s", response.Code, actions.disabled, response.Body.String())
	}
}

func TestLocalAdministratorPasswordResetAndNetworkUpdateUseTrustedVersion(t *testing.T) {
	actions := &breakGlassManagementActionsStub{}
	handler := NewRouter(Dependencies{Principal: recoveryManagerPrincipal, BreakGlassManagement: actions})

	reset := httptest.NewRequest(http.MethodPost, "/api/v1/admin/local-administrators/account-id/password", bytes.NewBufferString(
		`{"expected_version":3,"password":"a new sufficiently long password","reason":"scheduled rotation"}`,
	))
	resetResponse := httptest.NewRecorder()
	handler.ServeHTTP(resetResponse, reset)
	if resetResponse.Code != http.StatusOK || actions.reset.AccountID != "account-id" ||
		actions.reset.ExpectedVersion != 3 || actions.reset.Password != "a new sufficiently long password" ||
		strings.Contains(resetResponse.Body.String(), "a new sufficiently long password") {
		t.Fatalf("reset status=%d command=%+v response=%s", resetResponse.Code, actions.reset, resetResponse.Body.String())
	}

	networks := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/local-administrators/account-id/networks", bytes.NewBufferString(
		`{"expected_version":4,"allowed_cidrs":["192.168.86.0/24"],"reason":"network change"}`,
	))
	networkResponse := httptest.NewRecorder()
	handler.ServeHTTP(networkResponse, networks)
	if networkResponse.Code != http.StatusOK || actions.networks.AccountID != "account-id" ||
		actions.networks.ExpectedVersion != 4 || len(actions.networks.AllowedCIDRs) != 1 {
		t.Fatalf("networks status=%d command=%+v response=%s", networkResponse.Code, actions.networks, networkResponse.Body.String())
	}
}

func TestLocalAdministratorStaleMutationReturnsVersionConflict(t *testing.T) {
	actions := &breakGlassManagementActionsStub{err: identity.ErrBreakGlassVersionConflict}
	handler := NewRouter(Dependencies{Principal: recoveryManagerPrincipal, BreakGlassManagement: actions})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/local-administrators/account-id/password", bytes.NewBufferString(
		`{"expected_version":3,"password":"a new sufficiently long password","reason":"scheduled rotation"}`,
	))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "version_conflict") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

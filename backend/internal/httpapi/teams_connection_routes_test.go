package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type teamsConnectionActionsStub struct {
	created           notifications.CreateTeamsConnectionCommand
	credential        notifications.ReplaceTeamsConnectionCredentialCommand
	createWebhook     string
	credentialWebhook string
	updated           notifications.UpdateTeamsConnectionMetadataCommand
	enabled           notifications.SetTeamsConnectionEnabledCommand
	tested            notifications.TestTeamsConnectionCommand
}

type emptyTeamsConnectionActionsStub struct {
	teamsConnectionActionsStub
}

func (*emptyTeamsConnectionActionsStub) List(
	context.Context,
	notifications.ListTeamsConnectionsCommand,
) ([]notifications.ManagedTeamsConnection, error) {
	return nil, nil
}

func (stub *teamsConnectionActionsStub) Create(_ context.Context, command notifications.CreateTeamsConnectionCommand) (notifications.ManagedTeamsConnection, error) {
	stub.created = command
	stub.createWebhook = string(command.PlaintextWebhookURL)
	return managedTeamsConnection(1), nil
}
func (*teamsConnectionActionsStub) List(context.Context, notifications.ListTeamsConnectionsCommand) ([]notifications.ManagedTeamsConnection, error) {
	return []notifications.ManagedTeamsConnection{managedTeamsConnection(1)}, nil
}
func (stub *teamsConnectionActionsStub) UpdateMetadata(_ context.Context, command notifications.UpdateTeamsConnectionMetadataCommand) (notifications.ManagedTeamsConnection, error) {
	stub.updated = command
	return managedTeamsConnection(command.ExpectedVersion + 1), nil
}
func (stub *teamsConnectionActionsStub) ReplaceCredential(_ context.Context, command notifications.ReplaceTeamsConnectionCredentialCommand) (notifications.ManagedTeamsConnection, error) {
	stub.credential = command
	stub.credentialWebhook = string(command.PlaintextWebhookURL)
	return managedTeamsConnection(command.ExpectedVersion + 1), nil
}
func (stub *teamsConnectionActionsStub) SetEnabled(_ context.Context, command notifications.SetTeamsConnectionEnabledCommand) (notifications.ManagedTeamsConnection, error) {
	stub.enabled = command
	connection := managedTeamsConnection(command.ExpectedVersion + 1)
	connection.Enabled = command.Enabled
	return connection, nil
}
func (stub *teamsConnectionActionsStub) Test(_ context.Context, command notifications.TestTeamsConnectionCommand) (notifications.ManagedTeamsConnection, error) {
	stub.tested = command
	return managedTeamsConnection(2), nil
}

func managedTeamsConnection(version int64) notifications.ManagedTeamsConnection {
	return notifications.ManagedTeamsConnection{
		ID: "connection-id", ClientID: "client-id", Name: "Service Desk",
		CredentialConfigured: true, Health: notifications.TeamsHealthPending, Version: version,
	}
}

func teamsManagerPrincipal(*http.Request) (authorization.Principal, error) {
	return authorization.Principal{
		ID: "actor-id", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("integration.manage"),
	}, nil
}

func TestListTeamsConnectionsReturnsAnEmptyArray(t *testing.T) {
	handler := NewRouter(Dependencies{
		Principal:        teamsManagerPrincipal,
		TeamsConnections: &emptyTeamsConnectionActionsStub{},
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/integrations/teams/connections",
			nil,
		),
	)
	if response.Code != http.StatusOK ||
		strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTeamsConnectionCreateUsesTrustedScopeAndNeverReturnsWebhook(t *testing.T) {
	actions := &teamsConnectionActionsStub{}
	handler := NewRouter(Dependencies{Principal: teamsManagerPrincipal, TeamsConnections: actions})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/teams/connections",
		bytes.NewBufferString(`{"name":"Service Desk","webhook_url":"https://teams.example.test/hook/secret","reason":"configure notifications"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || response.Header().Get("ETag") != `"1"` ||
		actions.created.Target.MSPID != "msp-id" || actions.created.ClientID != "client-id" ||
		actions.createWebhook != "https://teams.example.test/hook/secret" ||
		strings.Contains(response.Body.String(), "hook/secret") || strings.Contains(response.Body.String(), "webhook_url") {
		t.Fatalf("status=%d command=%+v response=%s", response.Code, actions.created, response.Body.String())
	}
}

func TestTeamsConnectionCredentialUsesVersionAndWriteOnlyBody(t *testing.T) {
	actions := &teamsConnectionActionsStub{}
	handler := NewRouter(Dependencies{Principal: teamsManagerPrincipal, TeamsConnections: actions})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/teams/connections/connection-id/credential",
		bytes.NewBufferString(`{"expected_version":2,"webhook_url":"https://teams.example.test/new-secret","reason":"rotate webhook"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"2"`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || actions.credential.ExpectedVersion != 2 ||
		actions.credentialWebhook != "https://teams.example.test/new-secret" ||
		strings.Contains(response.Body.String(), "new-secret") {
		t.Fatalf("status=%d command=%+v response=%s", response.Code, actions.credential, response.Body.String())
	}
}

func TestTeamsConnectionPatchRequiresExactlyOneMutation(t *testing.T) {
	handler := NewRouter(Dependencies{Principal: teamsManagerPrincipal, TeamsConnections: &teamsConnectionActionsStub{}})
	for _, body := range []string{
		`{"expected_version":1,"reason":"ambiguous","name":"NOC","enabled":true}`,
		`{"expected_version":1,"reason":"empty"}`,
	} {
		request := httptest.NewRequest(http.MethodPatch, "/api/v1/integrations/teams/connections/connection-id", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, response.Code, response.Body.String())
		}
	}
}

func TestTeamsConnectionRoutesRejectUnknownFieldsAndMissingCapability(t *testing.T) {
	actions := &teamsConnectionActionsStub{}
	handler := NewRouter(Dependencies{Principal: teamsManagerPrincipal, TeamsConnections: actions})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/teams/connections",
		bytes.NewBufferString(`{"name":"NOC","webhook_url":"https://teams.example.test/hook","reason":"configure","secret_ref":"env://forbidden"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status=%d response=%s", response.Code, response.Body.String())
	}

	denied := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{ID: "actor", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"}}, nil
		},
		TeamsConnections: actions,
	})
	deniedResponse := httptest.NewRecorder()
	denied.ServeHTTP(deniedResponse, httptest.NewRequest(http.MethodGet, "/api/v1/integrations/teams/connections", nil))
	if deniedResponse.Code != http.StatusForbidden {
		t.Fatalf("denied status=%d response=%s", deniedResponse.Code, deniedResponse.Body.String())
	}
}

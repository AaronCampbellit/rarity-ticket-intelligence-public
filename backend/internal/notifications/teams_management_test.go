package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type teamsManagementRepositoryStub struct {
	connection ManagedTeamsConnection
	create     TeamsConnectionMutation
	update     TeamsConnectionMutation
	replace    TeamsConnectionMutation
	enabled    TeamsConnectionMutation
	health     TeamsConnectionHealthMutation
	used       []byte
}

func (r *teamsManagementRepositoryStub) CreateTeamsConnection(_ context.Context, value TeamsConnectionMutation) error {
	r.create = value
	return nil
}
func (r *teamsManagementRepositoryStub) ListTeamsConnections(context.Context, scope.Target) ([]ManagedTeamsConnection, error) {
	return []ManagedTeamsConnection{r.connection}, nil
}
func (r *teamsManagementRepositoryStub) GetTeamsConnection(context.Context, scope.Target, string) (ManagedTeamsConnection, error) {
	return r.connection, nil
}
func (r *teamsManagementRepositoryStub) UpdateTeamsConnectionMetadata(_ context.Context, value TeamsConnectionMutation) error {
	r.update = value
	return nil
}
func (r *teamsManagementRepositoryStub) ReplaceTeamsConnectionCredential(_ context.Context, value TeamsConnectionMutation) error {
	r.replace = value
	return nil
}
func (r *teamsManagementRepositoryStub) SetTeamsConnectionEnabled(_ context.Context, value TeamsConnectionMutation) error {
	r.enabled = value
	return nil
}
func (r *teamsManagementRepositoryStub) RecordTeamsConnectionHealth(_ context.Context, value TeamsConnectionHealthMutation) error {
	r.health = value
	return nil
}
func (r *teamsManagementRepositoryStub) UseTeamsWebhook(_ context.Context, _ scope.Target, _ ManagedTeamsConnection, use func([]byte) error) error {
	r.used = []byte("https://teams.example.test/webhook")
	return use(r.used)
}

type teamsTesterStub struct {
	err      error
	endpoint []byte
}

func (t *teamsTesterStub) TestTeamsWebhook(_ context.Context, _ ManagedTeamsConnection, endpoint []byte) error {
	t.endpoint = append([]byte(nil), endpoint...)
	return t.err
}

func teamsPrincipal(clientID string) authorization.Principal {
	return authorization.Principal{ID: "technician", Scope: scope.Principal{MSPID: "msp", ClientID: clientID}, Capabilities: authorization.NewCapabilitySet("integration.manage")}
}

func TestTeamsManagementCreatesWriteOnlyEncryptedConnectionWithFacts(t *testing.T) {
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	repository := &teamsManagementRepositoryStub{}
	ids := []string{"connection", "correlation", "audit", "event"}
	service := NewTeamsConnectionManagementService(repository, &teamsTesterStub{}, func() time.Time { return at }, func() string { id := ids[0]; ids = ids[1:]; return id })
	connection, err := service.Create(context.Background(), CreateTeamsConnectionCommand{Principal: teamsPrincipal("client"), Name: "Primary Teams", ClientID: "client", PlaintextWebhookURL: []byte("https://teams.example.test/webhook"), Reason: "initial connection"})
	if err != nil {
		t.Fatalf("Create() error=%v", err)
	}
	if connection.ID != "connection" || connection.Enabled || !connection.CredentialConfigured || repository.create.Audit.Action != "teams.connection.created" || repository.create.Event.EventType != "teams.connection.created" {
		t.Fatalf("created=%+v mutation=%+v", connection, repository.create)
	}
	encoded, err := json.Marshal(connection)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || string(encoded) == "https://teams.example.test/webhook" || containsJSONKey(encoded, "webhook") {
		t.Fatalf("browser-safe connection leaked credential: %s", encoded)
	}
}

func TestTeamsManagementRequiresIntegrationManageStrictReasonVersionsAndActiveClientScope(t *testing.T) {
	repository := &teamsManagementRepositoryStub{connection: ManagedTeamsConnection{ID: "connection", MSPID: "msp", ClientID: "client", Name: "Primary", CredentialConfigured: true, Health: TeamsHealthPending, Version: 2}}
	service := NewTeamsConnectionManagementService(repository, &teamsTesterStub{}, time.Now, func() string { return "id" })
	if _, err := service.UpdateMetadata(context.Background(), UpdateTeamsConnectionMetadataCommand{Principal: teamsPrincipal("client"), ID: "connection", Name: "Updated", ExpectedVersion: 2}); !errors.Is(err, ErrInvalidTeamsConnection) {
		t.Fatalf("missing reason error=%v", err)
	}
	if _, err := service.UpdateMetadata(context.Background(), UpdateTeamsConnectionMetadataCommand{Principal: teamsPrincipal("client"), ID: "connection", Name: "Updated", ExpectedVersion: 1, Reason: "rename"}); !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("stale version error=%v", err)
	}
	if _, err := service.List(context.Background(), ListTeamsConnectionsCommand{Principal: teamsPrincipal("client"), Target: scope.Target{MSPID: "msp", ClientID: "other-client"}}); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-client error=%v", err)
	}
	forbidden := teamsPrincipal("client")
	forbidden.Capabilities = nil
	if _, err := service.List(context.Background(), ListTeamsConnectionsCommand{Principal: forbidden}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("missing capability error=%v", err)
	}
}

func TestTeamsManagementReplacesEnablesAndRecordsNonLeakingHealth(t *testing.T) {
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	repository := &teamsManagementRepositoryStub{connection: ManagedTeamsConnection{ID: "connection", MSPID: "msp", ClientID: "client", Name: "Primary", CredentialConfigured: true, Health: TeamsHealthDisabled, Version: 2}}
	tester := &teamsTesterStub{err: errors.New("provider says https://secret.example.test")}
	ids := []string{"correlation", "audit", "event", "correlation2", "audit2", "event2", "correlation3", "audit3", "event3"}
	service := NewTeamsConnectionManagementService(repository, tester, func() time.Time { return at }, func() string { id := ids[0]; ids = ids[1:]; return id })
	if _, err := service.ReplaceCredential(context.Background(), ReplaceTeamsConnectionCredentialCommand{Principal: teamsPrincipal("client"), ID: "connection", ExpectedVersion: 2, PlaintextWebhookURL: []byte("https://teams.example.test/new"), Reason: "rotate"}); err != nil {
		t.Fatalf("ReplaceCredential() error=%v", err)
	}
	if repository.replace.Audit.Action != "teams.connection.credential_replaced" || !allTeamsBytesZero(repository.replace.PlaintextWebhookURL) {
		t.Fatalf("credential contract=%+v", repository.replace)
	}
	if _, err := service.SetEnabled(context.Background(), SetTeamsConnectionEnabledCommand{Principal: teamsPrincipal("client"), ID: "connection", ExpectedVersion: 2, Enabled: true, Reason: "activate"}); err != nil {
		t.Fatalf("SetEnabled() error=%v", err)
	}
	if repository.enabled.Connection.Health != TeamsHealthPending || repository.enabled.Audit.Action != "teams.connection.enabled" {
		t.Fatalf("enable mutation=%+v", repository.enabled)
	}
	repository.connection.Version = 3
	connection, err := service.Test(context.Background(), TestTeamsConnectionCommand{Principal: teamsPrincipal("client"), ID: "connection", Reason: "verify callback"})
	if err != nil || connection.LastErrorCode != teamsConnectionTestFailedCode || repository.health.Health != TeamsHealthFailed || string(tester.endpoint) != "https://teams.example.test/webhook" {
		t.Fatalf("test result=%+v error=%v health=%+v endpoint=%q", connection, err, repository.health, tester.endpoint)
	}
}

func containsJSONKey(value []byte, key string) bool {
	var body map[string]any
	_ = json.Unmarshal(value, &body)
	_, found := body[key]
	return found
}
func allTeamsBytesZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}

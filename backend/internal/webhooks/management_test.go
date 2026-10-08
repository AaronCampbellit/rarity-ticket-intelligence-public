package webhooks

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type managementRepositoryStub struct {
	target  scope.Target
	created ConnectionMutation
}

func (r *managementRepositoryStub) ListManagedConnections(_ context.Context, target scope.Target) ([]ManagedConnection, error) {
	r.target = target
	return []ManagedConnection{{ID: "connection-1", ClientID: target.ClientID}}, nil
}

func (r *managementRepositoryStub) ListManagedDeliveries(_ context.Context, target scope.Target, limit int) ([]ManagedDelivery, error) {
	r.target = target
	if limit != 200 {
		panic("unexpected delivery limit")
	}
	return []ManagedDelivery{{ConnectionID: "connection-1"}}, nil
}

func (r *managementRepositoryStub) GetManagedConnection(_ context.Context, _ scope.Target, _ string) (ManagedConnection, error) {
	return ManagedConnection{}, nil
}
func (r *managementRepositoryStub) CreateManagedConnection(_ context.Context, mutation ConnectionMutation) error {
	r.created = mutation
	return nil
}

func TestCreateProducesEncryptedRepositoryBoundaryAndAuditEvidence(t *testing.T) {
	repository := &managementRepositoryStub{}
	now := time.Date(2026, time.July, 30, 17, 0, 0, 0, time.UTC)
	ids := []string{"connection", "audit", "event", "correlation"}
	service := NewManagementService(repository, func() time.Time { return now }).
		WithIDGenerator(func() string {
			id := ids[0]
			ids = ids[1:]
			return id
		})
	principal := authorization.Principal{
		ID: "actor", Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
		Capabilities: authorization.NewCapabilitySet("integration.manage"),
	}
	secret := []byte("12345678901234567890123456789012")
	connection, err := service.Create(context.Background(), CreateConnectionCommand{
		Principal: principal, Name: "Ticket events", Direction: DirectionOutbound,
		Endpoint:   "https://hooks.example.test/rarity",
		EventTypes: []string{"work_record.created"}, RetryWindow: 24 * time.Hour,
		PlaintextSecret: secret, Reason: "New integration",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if connection.ID != "connection" || repository.created.Audit.Action != "webhook.connection.created" ||
		repository.created.Event.EventType != "webhook.connection.created" {
		t.Fatalf("unexpected connection/mutation: %+v %+v", connection, repository.created)
	}
	if string(repository.created.PlaintextSecret) != string(secret) {
		t.Fatal("repository did not receive the transient secret")
	}
}
func (r *managementRepositoryStub) UpdateManagedConnection(_ context.Context, _ ConnectionMutation) error {
	return nil
}
func (r *managementRepositoryStub) ReplaceManagedCredential(_ context.Context, _ ConnectionMutation) error {
	return nil
}
func (r *managementRepositoryStub) RetryManagedDelivery(_ context.Context, _ DeliveryRetryMutation) error {
	return nil
}

func TestManagementUsesPrincipalClientScopeAndDistinctCapabilities(t *testing.T) {
	repository := &managementRepositoryStub{}
	service := NewManagementService(repository)
	principal := authorization.Principal{
		ID: "actor", Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
		Capabilities: authorization.NewCapabilitySet("integration.read"),
	}
	if _, err := service.ListDeliveries(context.Background(), principal, scope.Target{}); err != nil {
		t.Fatalf("ListDeliveries() error = %v", err)
	}
	if repository.target != (scope.Target{MSPID: "msp", ClientID: "client"}) {
		t.Fatalf("target = %+v", repository.target)
	}
	if _, err := service.ListConnections(context.Background(), principal, scope.Target{}); err == nil {
		t.Fatal("ListConnections() allowed integration.read without integration.manage")
	}
}

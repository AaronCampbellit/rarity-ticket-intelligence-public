package intake

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type forwardingManagementRepositoryStub struct {
	created ForwardingManagementMutation
	current ManagedForwardingConnection
}

func (r *forwardingManagementRepositoryStub) ListManagedForwarding(_ context.Context, _ string) ([]ManagedForwardingConnection, error) {
	return []ManagedForwardingConnection{r.current}, nil
}
func (r *forwardingManagementRepositoryStub) GetManagedForwarding(_ context.Context, _, _ string) (ManagedForwardingConnection, error) {
	return r.current, nil
}
func (r *forwardingManagementRepositoryStub) CreateManagedForwarding(_ context.Context, mutation ForwardingManagementMutation) error {
	r.created = mutation
	return nil
}
func (r *forwardingManagementRepositoryStub) UpdateManagedForwarding(_ context.Context, mutation ForwardingManagementMutation) error {
	r.created = mutation
	return nil
}

func TestForwardingManagementCreatesNormalizedAuditedConfiguration(t *testing.T) {
	repository := &forwardingManagementRepositoryStub{}
	now := time.Date(2026, time.July, 30, 19, 0, 0, 0, time.UTC)
	ids := []string{"connection", "audit", "event", "correlation"}
	service := NewForwardingManagementService(repository, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	connection, err := service.Create(context.Background(), CreateForwardingConnectionCommand{
		Principal: forwardingAdmin(), IntakeAddress: "Intake@Example.COM",
		AllowedSenderDomains: []string{"@Customer.COM", "customer.com"},
		MaxMessageBytes:      25 << 20, RateLimitPerMinute: 60,
		Reason: "Protected mailbox configured",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if connection.IntakeAddress != "intake@example.com" ||
		len(connection.AllowedSenderDomains) != 1 ||
		connection.AllowedSenderDomains[0] != "customer.com" ||
		repository.created.Audit.Action != "forwarding.connection.created" {
		t.Fatalf("unexpected connection/mutation: %+v %+v", connection, repository.created)
	}
}

func TestForwardingManagementRejectsUnsafeOrUnboundedConfiguration(t *testing.T) {
	service := NewForwardingManagementService(
		&forwardingManagementRepositoryStub{}, time.Now, func() string { return "id" },
	)
	for _, command := range []CreateForwardingConnectionCommand{
		{Principal: forwardingAdmin(), IntakeAddress: "not-an-email", AllowedSenderDomains: []string{"example.com"}, MaxMessageBytes: 1, RateLimitPerMinute: 1, Reason: "test"},
		{Principal: forwardingAdmin(), IntakeAddress: "intake@example.com", AllowedSenderDomains: []string{"localhost"}, MaxMessageBytes: 1, RateLimitPerMinute: 1, Reason: "test"},
		{Principal: forwardingAdmin(), IntakeAddress: "intake@example.com", AllowedSenderDomains: []string{"example.com"}, MaxMessageBytes: 51 << 20, RateLimitPerMinute: 1, Reason: "test"},
	} {
		if _, err := service.Create(context.Background(), command); err == nil {
			t.Fatalf("Create() accepted invalid command: %+v", command)
		}
	}
}

func forwardingAdmin() authorization.Principal {
	return authorization.Principal{
		ID: "actor", Scope: scope.Principal{MSPID: "msp"},
		Capabilities: authorization.NewCapabilitySet("integration.manage"),
	}
}

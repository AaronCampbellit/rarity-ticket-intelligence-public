package graphintake

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type graphManagementRepositoryStub struct{ created MailboxMutation }

func (r *graphManagementRepositoryStub) ListManagedMailboxes(context.Context, string) ([]ManagedMailbox, error) {
	return nil, nil
}
func (r *graphManagementRepositoryStub) GetManagedMailbox(context.Context, string, string) (ManagedMailbox, error) {
	return ManagedMailbox{}, nil
}
func (r *graphManagementRepositoryStub) CreateManagedMailbox(_ context.Context, mutation MailboxMutation) error {
	r.created = mutation
	return nil
}
func (r *graphManagementRepositoryStub) UpdateManagedMailbox(context.Context, MailboxMutation) error {
	return nil
}
func (r *graphManagementRepositoryStub) ReplaceManagedMailboxCredential(context.Context, MailboxMutation) error {
	return nil
}

func TestGraphManagementCreatesWriteOnlyCredentialAndRandomClientState(t *testing.T) {
	repository := &graphManagementRepositoryStub{}
	ids := []string{"mailbox", "audit", "event", "correlation"}
	service := NewManagementService(repository, func() time.Time {
		return time.Date(2026, time.July, 30, 20, 0, 0, 0, time.UTC)
	}, func() string { id := ids[0]; ids = ids[1:]; return id }, func(size int) ([]byte, error) {
		return make([]byte, size), nil
	})
	principal := authorization.Principal{
		ID: "actor", Scope: scope.Principal{MSPID: "msp"},
		Capabilities: authorization.NewCapabilitySet("integration.manage"),
	}
	item, err := service.Create(context.Background(), CreateMailboxCommand{
		Principal: principal, Mailbox: "Support@Example.com", TenantID: "tenant",
		ClientID: "application", ClientSecret: []byte("secret"), Reason: "Onboarding",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if item.MailboxAddress != "support@example.com" ||
		repository.created.Audit.Action != "graph.mailbox.created" ||
		len(repository.created.ClientState) < 32 {
		t.Fatalf("unexpected mailbox/mutation: %+v %+v", item, repository.created)
	}
	if string(repository.created.ProtectedCredential) == "secret" {
		t.Fatal("repository received an unstructured raw client secret")
	}
}

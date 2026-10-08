package auditlog

import (
	"context"
	"errors"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type repositoryStub struct {
	target scope.Target
	limit  int
}

func (r *repositoryStub) List(
	_ context.Context,
	target scope.Target,
	limit int,
) ([]Entry, error) {
	r.target, r.limit = target, limit
	return []Entry{{ID: "audit-id"}}, nil
}

func TestListUsesAuthorizedPrincipalScope(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository)
	entries, err := service.List(context.Background(), authorization.Principal{
		ID: "admin", Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
		Capabilities: authorization.NewCapabilitySet("audit.read"),
	}, 50)
	if err != nil || len(entries) != 1 ||
		repository.target.ClientID != "client" || repository.limit != 50 {
		t.Fatalf("entries=%+v target=%+v limit=%d err=%v", entries, repository.target, repository.limit, err)
	}
}

func TestListRequiresCapabilityAndBoundedLimit(t *testing.T) {
	service := NewService(&repositoryStub{})
	principal := authorization.Principal{
		ID: "user", Scope: scope.Principal{MSPID: "msp"},
	}
	if _, err := service.List(context.Background(), principal, 25); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("capability error=%v", err)
	}
	principal.Capabilities = authorization.NewCapabilitySet("audit.read")
	if _, err := service.List(context.Background(), principal, 101); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("limit error=%v", err)
	}
}

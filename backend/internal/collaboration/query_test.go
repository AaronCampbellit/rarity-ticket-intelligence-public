package collaboration

import (
	"context"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mentions"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type listRepositoryStub struct {
	query ListQuery
	rows  []ListedSource
}

func (s *listRepositoryStub) ListInternalContent(_ context.Context, query ListQuery) ([]ListedSource, error) {
	s.query = query
	return s.rows, nil
}

func TestListServicePreservesTrustedPrincipalAndNonNilCollections(t *testing.T) {
	repository := &listRepositoryStub{rows: []ListedSource{{Source: Source{ID: "legacy-1", Tokens: nil}, ReadOnly: true, Legacy: true}}}
	service := NewListService(repository)
	principal := authorization.Principal{ID: "staff-1", Scope: scope.Principal{MSPID: "msp-1", ClientID: "client-1"}}
	rows, err := service.ListInternalContent(context.Background(), ListQuery{Principal: principal, Parent: ParentRef{Type: mentions.ParentWorkRecord, ID: "work-1"}})
	if err != nil || repository.query.Principal.ID != principal.ID || repository.query.Principal.Scope != principal.Scope || len(rows) != 1 || rows[0].Source.Tokens == nil {
		t.Fatalf("err=%v query=%+v rows=%+v", err, repository.query, rows)
	}
}

package search

import (
	"context"
	"errors"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type captureRepository struct {
	target scope.Target
	query  string
}

func (r *captureRepository) Search(_ context.Context, target scope.Target, query string, _ int) ([]Result, error) {
	r.target = target
	r.query = query
	return []Result{{ID: "work-id", ObjectType: "work_record", ClientID: target.ClientID}}, nil
}

func TestSearchAlwaysPassesExplicitAuthorizedClientScope(t *testing.T) {
	repository := &captureRepository{}
	service := NewService(repository)
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id"},
		Capabilities: authorization.NewCapabilitySet("search.read"),
	}
	results, err := service.Search(context.Background(), Query{
		Principal: principal,
		Target:    scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		Text:      "email outage",
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if repository.target.ClientID != "client-id" || len(results) != 1 {
		t.Fatalf("search scope/results = %+v / %+v", repository.target, results)
	}
}

func TestSearchRejectsMissingClientScopeAndCrossClientQueries(t *testing.T) {
	repository := &captureRepository{}
	service := NewService(repository)
	global := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id"},
		Capabilities: authorization.NewCapabilitySet("search.read"),
	}
	if _, err := service.Search(context.Background(), Query{
		Principal: global, Text: "outage", Limit: 20,
	}); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("unscoped search error = %v", err)
	}

	client := global
	client.Scope.ClientID = "client-alpha"
	if _, err := service.Search(context.Background(), Query{
		Principal: client,
		Target:    scope.Target{MSPID: "msp-id", ClientID: "client-bravo"},
		Text:      "outage", Limit: 20,
	}); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-client search error = %v", err)
	}
}

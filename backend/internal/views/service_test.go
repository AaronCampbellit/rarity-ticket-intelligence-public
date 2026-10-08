package views

import (
	"context"
	"errors"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type fakeRepository struct {
	view  View
	saved View
	views []View
}

func (r *fakeRepository) List(_ context.Context, _ string, _ Kind) ([]View, error) {
	return r.views, nil
}

func (r *fakeRepository) Save(_ context.Context, view View) error {
	r.saved = view
	return nil
}

func TestListReturnsOnlyReadableViewsWithTrustedScope(t *testing.T) {
	repository := &fakeRepository{views: []View{
		{
			ID: "private-own", MSPID: "msp", OwnerID: "tech",
			Kind: SavedSearch, Name: "Mine",
			Query:    map[string]any{"status": "new"},
			Audience: Audience{Type: Private},
		},
		{
			ID: "private-other", MSPID: "msp", OwnerID: "other",
			Kind: SavedSearch, Name: "Hidden",
			Query:    map[string]any{"status": "closed"},
			Audience: Audience{Type: Private},
		},
		{
			ID: "shared", MSPID: "msp", OwnerID: "other",
			Kind: SavedSearch, Name: "Shared",
			Query:    map[string]any{"priority": "high"},
			Audience: Audience{Type: MSP},
		},
	}}
	service := NewService(repository, func() string { return "id" })
	found, err := service.List(context.Background(), ListCommand{
		Principal: authorization.Principal{
			ID: "tech", Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("search.read"),
		},
		Kind: SavedSearch,
	})
	if err != nil || len(found) != 2 ||
		found[0].Query["client_id"] != "client" ||
		found[1].Name != "Shared" {
		t.Fatalf("found=%+v err=%v", found, err)
	}
}
func (r *fakeRepository) Find(_ context.Context, _, _ string) (View, error) {
	return r.view, nil
}

func TestSaveSharedViewRequiresShareCapability(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, func() string { return "view-id" })
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id"},
		Capabilities: authorization.NewCapabilitySet("view.save"),
	}
	_, err := service.Save(context.Background(), SaveCommand{
		Principal: principal, OwnerID: "tech-id", Kind: SavedSearch,
		Name: "SLA risk", Query: map[string]any{"sla_state": "warning"},
		Audience: Audience{Type: Queue, ID: "queue-id"},
	})
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("Save() error = %v, want ErrForbidden", err)
	}
}

func TestResolveReplacesStoredScopeWithCurrentAuthorizedScope(t *testing.T) {
	repository := &fakeRepository{view: View{
		ID: "view-id", MSPID: "msp-id", OwnerID: "owner-id", Kind: SavedSearch,
		Query:    map[string]any{"client_id": "client-bravo", "status": "new"},
		Audience: Audience{Type: MSP},
	}}
	service := NewService(repository, func() string { return "unused" })
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-alpha"},
		Capabilities: authorization.NewCapabilitySet("search.read"),
	}
	resolved, err := service.Resolve(context.Background(), ResolveCommand{
		Principal: principal, ViewID: "view-id",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Query["client_id"] != "client-alpha" {
		t.Fatalf("stored scope escaped authorization: %+v", resolved.Query)
	}
}

func TestSharedCalendarLensStripsUnauthorizedClientsAndRejectsUnknownKeys(t *testing.T) {
	repository := &fakeRepository{view: View{ID: "lens", MSPID: "msp", OwnerID: "other", Kind: KindCalendarLens, Name: "Shared", Query: map[string]any{"lens": "week", "client_ids": []any{"client-a", "client-secret"}, "event_roles": []any{"due"}}, Audience: Audience{Type: MSP}}}
	service := NewService(repository, func() string { return "id" })
	principal := authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp", ClientID: "selected-but-not-a-filter"}, Capabilities: authorization.NewCapabilitySet("calendar.read"), DataScopes: authorization.NewCapabilitySet("client:client-a")}
	resolved, err := service.Resolve(context.Background(), ResolveCommand{Principal: principal, ViewID: "lens"})
	if err != nil {
		t.Fatal(err)
	}
	clients, ok := resolved.Query["client_ids"].([]string)
	if !ok || len(clients) != 1 || clients[0] != "client-a" {
		t.Fatalf("query=%#v", resolved.Query)
	}
	if _, exists := resolved.Query["client_id"]; exists {
		t.Fatalf("active client leaked into lens: %#v", resolved.Query)
	}
	_, err = service.Save(context.Background(), SaveCommand{Principal: authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp"}, Capabilities: authorization.NewCapabilitySet("view.save")}, OwnerID: "tech", Kind: KindCalendarLens, Name: "Bad", Query: map[string]any{"sql": "drop table"}, Audience: Audience{Type: Private}})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error=%v", err)
	}
}

func TestCalendarLensRequiresOneOfTheSixCanonicalBaseLenses(t *testing.T) {
	service := NewService(&fakeRepository{}, func() string { return "id" })
	principal := authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp"}, Capabilities: authorization.NewCapabilitySet("view.save")}
	for _, lens := range []string{"day", "week", "month", "timeline", "capacity", "agenda"} {
		if _, err := service.Save(context.Background(), SaveCommand{Principal: principal, OwnerID: "tech", Kind: KindCalendarLens, Name: lens, Query: map[string]any{"lens": lens}, Audience: Audience{Type: Private}}); err != nil {
			t.Fatalf("canonical lens %q rejected: %v", lens, err)
		}
	}
	for _, query := range []map[string]any{{"lens": "quarter"}, {"client_ids": []string{"client-a"}}, {"lens": ""}} {
		if _, err := service.Save(context.Background(), SaveCommand{Principal: principal, OwnerID: "tech", Kind: KindCalendarLens, Name: "invalid", Query: query, Audience: Audience{Type: Private}}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid lens query %#v accepted: %v", query, err)
		}
	}
}

func TestCalendarLensPreservesExplicitEmptyClientsAfterAuthorizationStripping(t *testing.T) {
	repository := &fakeRepository{view: View{ID: "lens", MSPID: "msp", OwnerID: "other", Kind: KindCalendarLens, Name: "Shared", Query: map[string]any{"lens": "week", "client_ids": []any{"client-secret"}}, Audience: Audience{Type: MSP}}}
	service := NewService(repository, func() string { return "id" })
	principal := authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp"}, Capabilities: authorization.NewCapabilitySet("calendar.read"), DataScopes: authorization.NewCapabilitySet("client:client-a")}
	resolved, err := service.Resolve(context.Background(), ResolveCommand{Principal: principal, ViewID: "lens"})
	if err != nil {
		t.Fatal(err)
	}
	clients, ok := resolved.Query["client_ids"].([]string)
	if !ok || clients == nil || len(clients) != 0 {
		t.Fatalf("stripped clients must remain explicit-empty, got %#v", resolved.Query["client_ids"])
	}
}

package calendar

import (
	"context"
	"errors"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"testing"
	"time"
)

type visibleDependencyRepository struct {
	dependencyRepositoryStub
	denied map[string]bool
}

func (r *visibleDependencyRepository) CanReadCalendarSource(_ context.Context, _ authorization.Principal, source SourceRef) (bool, error) {
	return !r.denied[source.ID], nil
}
func (r *visibleDependencyRepository) CanScheduleCalendarTechnician(context.Context, authorization.Principal, string) (bool, error) {
	return true, nil
}
func TestDependencyListRequiresBothVisibleEndpoints(t *testing.T) {
	r := &visibleDependencyRepository{dependencyRepositoryStub: dependencyRepositoryStub{events: map[string]Projection{}, edges: []Dependency{
		{ID: "visible", MSPID: "msp", ClientID: "client", PredecessorID: "a", SuccessorID: "b"},
		{ID: "hidden", MSPID: "msp", ClientID: "client", PredecessorID: "a", SuccessorID: "c"},
		{ID: "other-tenant", MSPID: "other", ClientID: "client", PredecessorID: "a", SuccessorID: "b"},
	}}, denied: map[string]bool{"c-source": true}}
	for _, id := range []string{"a", "b", "c"} {
		r.events[id] = dependencyProjection(id, "msp", "client", 9)
	}
	s := NewDependencyService(r, time.Now, func() string { return "id" })
	edges, err := s.List(context.Background(), dependencyPrincipal("msp", "unrelated-selected-client"), "a")
	if err != nil || len(edges) != 1 || edges[0].ID != "visible" {
		t.Fatalf("visible edges=%+v, err=%v", edges, err)
	}
	if _, err = s.List(context.Background(), dependencyPrincipal("msp", ""), "c"); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("hidden source error=%v", err)
	}
	if _, err = s.List(context.Background(), dependencyPrincipal("other", ""), "a"); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-tenant error=%v", err)
	}
}

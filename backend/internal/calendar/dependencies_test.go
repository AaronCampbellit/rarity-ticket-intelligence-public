package calendar

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type dependencyRepositoryStub struct {
	events       map[string]Projection
	edges        []Dependency
	created      []Dependency
	createdFacts []DependencyChangeFact
	deleted      []string
	deletedFacts []DependencyDeletionFact
	loaded       []string
	listErr      error
	createErr    error
	deleteErr    error
	loadDepError error
}

func (r *dependencyRepositoryStub) LoadDependencyProjection(_ context.Context, id string) (Projection, error) {
	r.loaded = append(r.loaded, id)
	p, ok := r.events[id]
	if !ok {
		return Projection{}, scope.ErrNotFound
	}
	return p, nil
}
func (r *dependencyRepositoryStub) ListDependencies(context.Context, string, string) ([]Dependency, error) {
	return append([]Dependency(nil), r.edges...), r.listErr
}
func (r *dependencyRepositoryStub) InsertDependency(_ context.Context, dependency Dependency, fact DependencyChangeFact) (Dependency, error) {
	if r.createErr != nil {
		return Dependency{}, r.createErr
	}
	r.created = append(r.created, dependency)
	r.createdFacts = append(r.createdFacts, fact)
	r.edges = append(r.edges, dependency)
	return dependency, nil
}
func (r *dependencyRepositoryStub) LoadDependency(_ context.Context, _, id string) (Dependency, error) {
	if r.loadDepError != nil {
		return Dependency{}, r.loadDepError
	}
	for _, dependency := range r.edges {
		if dependency.ID == id {
			return dependency, nil
		}
	}
	return Dependency{}, scope.ErrNotFound
}
func (r *dependencyRepositoryStub) DeleteDependency(_ context.Context, dependency Dependency, fact DependencyDeletionFact) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	r.deleted = append(r.deleted, dependency.ID)
	r.deletedFacts = append(r.deletedFacts, fact)
	return nil
}

func dependencyPrincipal(mspID, clientID string) authorization.Principal {
	return authorization.Principal{
		ID:           "actor",
		Scope:        scope.Principal{MSPID: mspID, ClientID: clientID},
		Capabilities: authorization.NewCapabilitySet("calendar.read", "calendar.schedule", "work_record.read", "work_record.edit", "project.read", "project.edit"),
	}
}

func dependencyProjection(id, mspID, clientID string, startHour int) Projection {
	start := time.Date(2026, 8, 7, startHour, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	return Projection{
		ID: id, EventRole: "scheduled_work",
		Source:         SourceRef{MSPID: mspID, ClientID: clientID, Type: "task", ID: id + "-source"},
		SourceRevision: 1, Title: id, StartsAt: &start, EndsAt: &end, Timezone: "UTC",
		SchedulingMode: FixedBlock, TerminalState: Active, AssigneeID: "tech-" + id,
	}
}

func TestDependencyServiceRejectsCrossClientCyclesSelfDuplicatesAndUnsupportedTypes(t *testing.T) {
	repository := &dependencyRepositoryStub{events: map[string]Projection{
		"a": dependencyProjection("a", "msp", "client-1", 9),
		"b": dependencyProjection("b", "msp", "client-2", 10),
	}}
	service := NewDependencyService(repository, time.Now, func() string { return "dependency-id" })
	command := CreateDependencyCommand{Principal: dependencyPrincipal("msp", ""), PredecessorID: "a", SuccessorID: "b", Type: FinishToStart}
	if _, err := service.Preview(context.Background(), command); !errors.Is(err, ErrCrossClientDependency) {
		t.Fatalf("cross-client error = %v", err)
	}

	repository.events["b"] = dependencyProjection("b", "msp", "client-1", 10)
	repository.edges = []Dependency{{ID: "reverse", MSPID: "msp", ClientID: "client-1", PredecessorID: "b", SuccessorID: "a", Type: FinishToStart}}
	if _, err := service.Preview(context.Background(), command); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("cycle error = %v", err)
	}

	repository.edges = nil
	command.SuccessorID = "a"
	if _, err := service.Preview(context.Background(), command); !errors.Is(err, ErrSelfDependency) {
		t.Fatalf("self error = %v", err)
	}

	command.SuccessorID = "b"
	repository.edges = []Dependency{{ID: "existing", MSPID: "msp", ClientID: "client-1", PredecessorID: "a", SuccessorID: "b", Type: FinishToStart}}
	if _, err := service.Preview(context.Background(), command); !errors.Is(err, ErrDuplicateDependency) {
		t.Fatalf("duplicate error = %v", err)
	}
	command.Type = StartToStart
	if _, err := service.Preview(context.Background(), command); err != nil {
		t.Fatalf("distinct relationship on same pair rejected: %v", err)
	}

	repository.edges = nil
	command.Type = DependencyType("start_to_finish")
	if _, err := service.Preview(context.Background(), command); !errors.Is(err, ErrInvalidDependency) {
		t.Fatalf("type error = %v", err)
	}
	if validDependencyType(DependencyType("start_to_finish")) {
		t.Fatal("start-to-finish dependency accepted")
	}
}

func TestDependencyServiceRequiresEligibleRolesConfiguredLeadLagAndBothAuthorizations(t *testing.T) {
	repository := &dependencyRepositoryStub{events: map[string]Projection{
		"a": dependencyProjection("a", "msp", "client", 9),
		"b": dependencyProjection("b", "msp", "client", 10),
	}}
	service := NewDependencyService(repository, time.Now, func() string { return "dependency-id" }, WithMaximumLeadLagMinutes(60))
	command := CreateDependencyCommand{Principal: dependencyPrincipal("msp", "client"), PredecessorID: "a", SuccessorID: "b", Type: StartToStart, LeadLagMinutes: 61}
	if _, err := service.Preview(context.Background(), command); !errors.Is(err, ErrLeadLagOutOfRange) {
		t.Fatalf("lead/lag error = %v", err)
	}
	command.LeadLagMinutes = -61
	if _, err := service.Preview(context.Background(), command); !errors.Is(err, ErrLeadLagOutOfRange) {
		t.Fatalf("negative lead/lag error = %v", err)
	}

	command.LeadLagMinutes = 0
	repository.events["b"] = func() Projection {
		p := dependencyProjection("b", "msp", "client", 10)
		p.EventRole = "due"
		p.SchedulingMode = Informational
		return p
	}()
	if _, err := service.Preview(context.Background(), command); !errors.Is(err, ErrDependencyIneligible) {
		t.Fatalf("role error = %v", err)
	}

	repository.events["b"] = dependencyProjection("b", "msp", "client", 10)
	command.Principal.Capabilities = authorization.NewCapabilitySet("calendar.schedule", "work_record.read", "work_record.edit")
	if _, err := service.Preview(context.Background(), command); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("missing read authorization error = %v", err)
	}
	command.Principal.Capabilities = authorization.NewCapabilitySet("calendar.read", "work_record.read", "work_record.edit")
	if _, err := service.Preview(context.Background(), command); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("missing edit authorization error = %v", err)
	}
	if !reflect.DeepEqual(repository.loaded[len(repository.loaded)-2:], []string{"a", "b"}) {
		t.Fatalf("authorized contexts loaded = %v", repository.loaded)
	}
}

func TestDependencyPreviewIsPureAndCreateDeleteMutateAfterValidation(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repository := &dependencyRepositoryStub{events: map[string]Projection{
		"a": dependencyProjection("a", "msp", "client", 9),
		"b": dependencyProjection("b", "msp", "client", 9),
	}}
	service := NewDependencyService(repository, func() time.Time { return now }, func() string { return "dependency-id" })
	command := CreateDependencyCommand{Principal: dependencyPrincipal("msp", "client"), PredecessorID: "a", SuccessorID: "b", Type: FinishToStart, LeadLagMinutes: 30}
	preview, err := service.Preview(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if len(repository.created) != 0 || len(preview.Impact.RequiredMoves) != 1 || preview.Impact.RequiredMoves[0].ProjectionID != "b" {
		t.Fatalf("preview=%+v created=%v", preview, repository.created)
	}
	created, err := service.Create(context.Background(), command)
	if err != nil || created.ID != "dependency-id" || len(repository.created) != 1 || !created.CreatedAt.Equal(now) {
		t.Fatalf("created=%+v writes=%v err=%v", created, repository.created, err)
	}
	if len(repository.createdFacts) != 1 || repository.createdFacts[0].EventID != "dependency-id" || repository.createdFacts[0].ActorID != command.Principal.ID || !repository.createdFacts[0].OccurredAt.Equal(now) {
		t.Fatalf("creation facts=%+v", repository.createdFacts)
	}
	if err = service.Delete(context.Background(), DeleteDependencyCommand{Principal: command.Principal, DependencyID: created.ID, ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(repository.deleted, []string{"dependency-id"}) {
		t.Fatalf("deleted=%v", repository.deleted)
	}
	if len(repository.deletedFacts) != 1 || repository.deletedFacts[0].EventID != "dependency-id" || repository.deletedFacts[0].ActorID != command.Principal.ID || !repository.deletedFacts[0].OccurredAt.Equal(now) {
		t.Fatalf("deletion facts=%+v", repository.deletedFacts)
	}
}

func TestBuildCascadeImpactTraversesDeterministicallyWithoutMutatingProjections(t *testing.T) {
	projections := map[string]Projection{
		"a": dependencyProjection("a", "msp", "client", 9),
		"b": dependencyProjection("b", "msp", "client", 9),
		"c": dependencyProjection("c", "msp", "client", 10),
	}
	originalB := *projections["b"].StartsAt
	originalC := *projections["c"].StartsAt
	edges := []Dependency{
		{ID: "z", MSPID: "msp", ClientID: "client", PredecessorID: "b", SuccessorID: "c", Type: FinishToFinish},
		{ID: "a", MSPID: "msp", ClientID: "client", PredecessorID: "a", SuccessorID: "b", Type: FinishToStart},
	}
	proposedStart := time.Date(2026, 8, 7, 11, 0, 0, 0, time.UTC)
	proposedEnd := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	proposed := ScheduleInterval{StartsAt: &proposedStart, EndsAt: &proposedEnd}
	impact, err := BuildCascadeImpact("a", proposed, projections, edges)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{impact.RequiredMoves[0].ProjectionID, impact.RequiredMoves[1].ProjectionID}; !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Fatalf("move order=%v impact=%+v", got, impact)
	}
	if !projections["b"].StartsAt.Equal(originalB) || !projections["c"].StartsAt.Equal(originalC) {
		t.Fatal("cascade preview mutated projections")
	}
	if !reflect.DeepEqual(impact.CapacityRecalculationKeys, []string{"tech-a", "tech-b", "tech-c"}) || !reflect.DeepEqual(impact.AffectedHealth, []string{"a", "b", "c"}) {
		t.Fatalf("derived impact=%+v", impact)
	}
}

func TestBuildCascadeImpactIncludesHealthWhenConstraintAlreadySatisfied(t *testing.T) {
	projections := map[string]Projection{
		"a": dependencyProjection("a", "msp", "client", 9),
		"b": dependencyProjection("b", "msp", "client", 12),
	}
	root, _ := projectionInterval(projections["a"])
	impact, err := BuildCascadeImpact("a", root, projections, []Dependency{{ID: "edge", MSPID: "msp", ClientID: "client", PredecessorID: "a", SuccessorID: "b", Type: FinishToStart}})
	if err != nil {
		t.Fatal(err)
	}
	if len(impact.RequiredMoves) != 0 || !reflect.DeepEqual(impact.AffectedHealth, []string{"b"}) {
		t.Fatalf("impact=%+v", impact)
	}
}

func TestDependencyPreviewLoadsOnlyReachableCascade(t *testing.T) {
	repository := &dependencyRepositoryStub{events: map[string]Projection{
		"a": dependencyProjection("a", "msp", "client", 9),
		"b": dependencyProjection("b", "msp", "client", 10),
		"c": dependencyProjection("c", "msp", "client", 11),
		"x": dependencyProjection("x", "msp", "client", 12),
		"y": dependencyProjection("y", "msp", "client", 13),
	}, edges: []Dependency{
		{ID: "downstream", MSPID: "msp", ClientID: "client", PredecessorID: "b", SuccessorID: "c", Type: FinishToStart},
		{ID: "disconnected", MSPID: "msp", ClientID: "client", PredecessorID: "x", SuccessorID: "y", Type: FinishToStart},
	}}
	service := NewDependencyService(repository, time.Now, func() string { return "new-edge" })
	_, err := service.Preview(context.Background(), CreateDependencyCommand{Principal: dependencyPrincipal("msp", "client"), PredecessorID: "a", SuccessorID: "b", Type: FinishToStart})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(repository.loaded, []string{"a", "b", "c"}) {
		t.Fatalf("loaded projections=%v", repository.loaded)
	}
}

func TestDependencyEligibilityUsesRegistryAndIncludesMilestones(t *testing.T) {
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	milestone := func(id string, date time.Time) Projection {
		return Projection{ID: id, Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "milestone", ID: id + "-source"}, EventRole: "milestone", SourceRevision: 1, Title: id, AllDay: true, StartsOn: &date, SchedulingMode: Informational, TerminalState: Active, Dimensions: FilterDimensions{ProjectIDs: []string{"project"}}}
	}
	repository := &dependencyRepositoryStub{events: map[string]Projection{"a": milestone("a", start), "b": milestone("b", end)}}
	service := NewDependencyService(repository, time.Now, func() string { return "edge" })
	if _, err := service.Preview(context.Background(), CreateDependencyCommand{Principal: dependencyPrincipal("msp", "client"), PredecessorID: "a", SuccessorID: "b", Type: FinishToStart}); err != nil {
		t.Fatalf("canonical milestone dependency rejected: %v", err)
	}

	registry := NewRoleRegistry()
	if err := registry.Register(EventRoleDefinition{SourceType: "task", Role: "scheduled_work", SchedulingMode: FixedBlock, DependencyEligible: false}); err != nil {
		t.Fatal(err)
	}
	repository.events = map[string]Projection{"a": dependencyProjection("a", "msp", "client", 9), "b": dependencyProjection("b", "msp", "client", 10)}
	service = NewDependencyService(repository, time.Now, func() string { return "edge" }, WithDependencyRoleRegistry(registry))
	if _, err := service.Preview(context.Background(), CreateDependencyCommand{Principal: dependencyPrincipal("msp", "client"), PredecessorID: "a", SuccessorID: "b", Type: FinishToStart}); !errors.Is(err, ErrDependencyIneligible) {
		t.Fatalf("registry-ineligible role error=%v", err)
	}
}

func TestDependencyPreviewRejectsMinutePrecisionForAllDayMilestones(t *testing.T) {
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	milestone := func(id string, date time.Time) Projection {
		return Projection{ID: id, Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "milestone", ID: id + "-source"}, EventRole: "milestone", SourceRevision: 1, Title: id, AllDay: true, StartsOn: &date, SchedulingMode: Informational, TerminalState: Active, Dimensions: FilterDimensions{ProjectIDs: []string{"project"}}}
	}
	repository := &dependencyRepositoryStub{events: map[string]Projection{"a": milestone("a", start), "b": milestone("b", start.AddDate(0, 0, 1))}}
	service := NewDependencyService(repository, time.Now, func() string { return "edge" })
	_, err := service.Preview(context.Background(), CreateDependencyCommand{Principal: dependencyPrincipal("msp", "client"), PredecessorID: "a", SuccessorID: "b", Type: FinishToStart, LeadLagMinutes: 30})
	if !errors.Is(err, ErrLeadLagPrecision) {
		t.Fatalf("all-day minute lag error=%v", err)
	}
}

func TestAllDayMilestoneCascadeProducesDateOnlyMove(t *testing.T) {
	first := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	second := first.AddDate(0, 0, 1)
	projections := map[string]Projection{
		"a": {ID: "a", Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "milestone", ID: "a-source"}, EventRole: "milestone", SourceRevision: 1, Title: "a", AllDay: true, StartsOn: &first, SchedulingMode: Informational, TerminalState: Active},
		"b": {ID: "b", Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "milestone", ID: "b-source"}, EventRole: "milestone", SourceRevision: 1, Title: "b", AllDay: true, StartsOn: &second, SchedulingMode: Informational, TerminalState: Active},
	}
	proposedDate := first.AddDate(0, 0, 2)
	impact, err := BuildCascadeImpact("a", ScheduleInterval{StartsOn: &proposedDate}, projections, []Dependency{{ID: "edge", MSPID: "msp", ClientID: "client", PredecessorID: "a", SuccessorID: "b", Type: FinishToStart, LeadLagMinutes: 24 * 60}})
	if err != nil || len(impact.RequiredMoves) != 1 {
		t.Fatalf("impact=%+v err=%v", impact, err)
	}
	move := impact.RequiredMoves[0]
	wantFrom, wantTo := second, first.AddDate(0, 0, 4)
	if move.From.StartsAt != nil || move.From.EndsAt != nil || move.To.StartsAt != nil || move.To.EndsAt != nil || move.From.StartsOn == nil || !move.From.StartsOn.Equal(wantFrom) || move.To.StartsOn == nil || !move.To.StartsOn.Equal(wantTo) {
		t.Fatalf("date-only move=%+v want from=%s to=%s", move, wantFrom.Format(time.DateOnly), wantTo.Format(time.DateOnly))
	}
}

func TestDependencyEligibilityRejectsInformationalScheduledWork(t *testing.T) {
	repository := &dependencyRepositoryStub{events: map[string]Projection{
		"a": dependencyProjection("a", "msp", "client", 9),
		"b": dependencyProjection("b", "msp", "client", 10),
	}}
	for id, projection := range repository.events {
		projection.AllDay = true
		date := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
		projection.StartsOn = &date
		projection.StartsAt = nil
		projection.EndsAt = nil
		projection.Timezone = ""
		projection.SchedulingMode = Informational
		projection.CapacityBearing = false
		repository.events[id] = projection
	}
	service := NewDependencyService(repository, time.Now, func() string { return "edge" })
	_, err := service.Preview(context.Background(), CreateDependencyCommand{Principal: dependencyPrincipal("msp", "client"), PredecessorID: "a", SuccessorID: "b", Type: FinishToStart})
	if !errors.Is(err, ErrDependencyIneligible) {
		t.Fatalf("informational scheduled work error=%v", err)
	}
}

func TestDependencyEligibilityAcceptsEffortAllocationScheduledWork(t *testing.T) {
	repository := &dependencyRepositoryStub{events: map[string]Projection{
		"a": dependencyProjection("a", "msp", "client", 9),
		"b": dependencyProjection("b", "msp", "client", 10),
	}}
	for id, projection := range repository.events {
		projection.SchedulingMode = EffortAllocation
		repository.events[id] = projection
	}
	service := NewDependencyService(repository, time.Now, func() string { return "edge" })
	_, err := service.Preview(context.Background(), CreateDependencyCommand{Principal: dependencyPrincipal("msp", "client"), PredecessorID: "a", SuccessorID: "b", Type: FinishToStart})
	if err != nil {
		t.Fatalf("effort-allocation dependency rejected: %v", err)
	}
}

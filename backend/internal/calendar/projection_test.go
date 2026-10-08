package calendar

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type projectionRepositoryStub struct {
	applied  []ProjectionBatch
	advanced []ProjectionCursor
	revision int64
	err      error
}

func (r *projectionRepositoryStub) ApplyProjectionBatchAtomic(_ context.Context, b ProjectionBatch) (bool, error) {
	r.applied = append(r.applied, b)
	return b.SourceRevision >= r.revision, r.err
}

func (r *projectionRepositoryStub) AdvanceProjectionCursorAtomic(_ context.Context, _ string, cursor ProjectionCursor) error {
	r.advanced = append(r.advanced, cursor)
	return r.err
}

func TestProjectionServiceValidatesRegistryBeforeAtomicApply(t *testing.T) {
	date := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	r := &projectionRepositoryStub{}
	roles := NewRoleRegistry()
	_ = roles.Register(EventRoleDefinition{SourceType: "task", Role: "due", SchedulingMode: Informational, ReadOnly: true})
	s := NewProjectionService(r, roles)
	_, err := s.Apply(context.Background(), ProjectionBatch{Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "task"}, SourceRevision: 2, Projections: []Projection{{ID: "projection", Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "task"}, EventRole: "due", SourceRevision: 2, Title: "Due", AllDay: true, StartsOn: &date, SchedulingMode: Informational, TerminalState: Active}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.applied) != 1 {
		t.Fatalf("atomic applies = %d", len(r.applied))
	}
	r.applied = nil
	bad := ProjectionBatch{Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "task"}, SourceRevision: 3, Projections: []Projection{{ID: "projection", Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "task"}, EventRole: "scheduled_work", SourceRevision: 3, Title: "Work", AllDay: true, StartsOn: &date, SchedulingMode: Informational}}}
	if _, err := s.Apply(context.Background(), bad); err == nil {
		t.Fatal("unregistered role accepted")
	}
	if len(r.applied) != 0 {
		t.Fatal("invalid projection reached repository")
	}
}

func TestProjectionServiceRejectsMixedSourceAndRevision(t *testing.T) {
	r := &projectionRepositoryStub{}
	roles, err := NewProductionRoleRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectionService(r, roles)
	b := ProjectionBatch{Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "task"}, SourceRevision: 2, Projections: []Projection{{ID: "projection", Source: SourceRef{MSPID: "other", ClientID: "client", Type: "task", ID: "task"}, EventRole: "due", SourceRevision: 2, Title: "Due", AllDay: true, StartsOn: datePtr(2026, 8, 9), SchedulingMode: Informational}}}
	if _, err := s.Apply(context.Background(), b); err == nil {
		t.Fatal("mixed source accepted")
	}
	b.Projections[0].Source = b.Source
	b.Projections[0].SourceRevision = 1
	if _, err := s.Apply(context.Background(), b); err == nil {
		t.Fatal("mixed revision accepted")
	}
}

func TestProjectionServicePropagatesRepositoryAppliedFalse(t *testing.T) {
	r := &projectionRepositoryStub{revision: 7}
	roles, _ := NewProductionRoleRegistry(nil)
	service := NewProjectionService(r, roles)
	applied, err := service.Apply(context.Background(), ProjectionBatch{Source: SourceRef{MSPID: "msp", Type: "pto", ID: "pto"}, SourceRevision: 6})
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("stale repository result was reported applied")
	}
}

func datePtr(y int, m time.Month, d int) *time.Time {
	v := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &v
}

type eventResolverStub struct {
	resolved ResolvedProjectionSource
	event    mutation.EventRecord
	revision int64
}

func (s *eventResolverStub) ResolveProjectionSource(_ context.Context, event mutation.EventRecord) (ResolvedProjectionSource, error) {
	s.event = event
	return s.resolved, nil
}
func (s *eventResolverStub) ResolveCalendarProjectionRevision(_ context.Context, _ SourceRef, base int64) (int64, error) {
	if s.revision > 0 {
		return s.revision, nil
	}
	return base, nil
}

func TestProjectionWorkerReloadsTypedSourceAndIgnoresPayloadContent(t *testing.T) {
	date := datePtr(2026, 8, 10)
	ref := SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "task"}
	adapter := &projectingAdapterStub{sourceType: "task", projected: []Projection{{ID: "projection", Source: ref, EventRole: "due", SourceRevision: 8, Title: "Reloaded title", AllDay: true, StartsOn: date, SchedulingMode: Informational, TerminalState: Active}}}
	registry := NewAdapterRegistry()
	_ = registry.Register(adapter)
	repo := &projectionRepositoryStub{}
	roles, _ := NewProductionRoleRegistry(nil)
	resolver := &eventResolverStub{resolved: ResolvedProjectionSource{Source: ref, Relevant: true}}
	worker := NewProjectionWorker(resolver, registry, NewProjectionService(repo, roles), "calendar")
	event := mutation.EventRecord{EventID: "event", EventType: "task.updated", OccurredAt: time.Now(), MSPID: "msp", ClientID: "client", SubjectType: "task", SubjectID: "task", SubjectVersion: 8, Data: map[string]any{"title": "attacker payload", "description": "private"}}
	if err := worker.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(adapter.refs) != 1 || adapter.refs[0] != ref {
		t.Fatalf("adapter refs=%v", adapter.refs)
	}
	if len(repo.applied) != 1 || repo.applied[0].Projections[0].Title != "Reloaded title" {
		t.Fatalf("batch=%+v", repo.applied)
	}
}

func TestProjectionWorkerSkipsUnrelatedEvents(t *testing.T) {
	resolver := &eventResolverStub{resolved: ResolvedProjectionSource{Relevant: false}}
	registry := NewAdapterRegistry()
	repo := &projectionRepositoryStub{}
	roles, _ := NewProductionRoleRegistry(nil)
	worker := NewProjectionWorker(resolver, registry, NewProjectionService(repo, roles), "calendar")
	event := mutation.EventRecord{EventID: "00000000-0000-4000-8000-000000000011", EventType: "email.sent", OccurredAt: time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC), MSPID: "00000000-0000-4000-8000-000000000012"}
	if err := worker.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(repo.applied) != 0 {
		t.Fatal("unrelated event projected")
	}
	if len(repo.advanced) != 1 || repo.advanced[0].EventID != event.EventID {
		t.Fatalf("cursor advances=%+v", repo.advanced)
	}
}

func TestProjectionWorkerAdvancesOrderedIrrelevantEventsIndividually(t *testing.T) {
	resolver := &eventResolverStub{resolved: ResolvedProjectionSource{Relevant: false}}
	repo := &projectionRepositoryStub{}
	roles, _ := NewProductionRoleRegistry(nil)
	worker := NewProjectionWorker(resolver, NewAdapterRegistry(), NewProjectionService(repo, roles), "calendar")
	for i, eventID := range []string{"00000000-0000-4000-8000-000000000021", "00000000-0000-4000-8000-000000000022"} {
		event := mutation.EventRecord{EventID: eventID, OccurredAt: time.Date(2026, 8, 9, 1, i, 0, 0, time.UTC), MSPID: "00000000-0000-4000-8000-000000000012"}
		if err := worker.Handle(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	if len(repo.advanced) != 2 || repo.advanced[0].EventID != "00000000-0000-4000-8000-000000000021" || repo.advanced[1].EventID != "00000000-0000-4000-8000-000000000022" {
		t.Fatalf("cursor advances=%+v", repo.advanced)
	}
}

func TestProjectionWorkerTombstonesSourceMissingAtReplay(t *testing.T) {
	ref := SourceRef{MSPID: "msp", Type: "technician_schedule", ID: "old"}
	adapter := &projectingAdapterStub{sourceType: "technician_schedule", err: scope.ErrNotFound}
	registry := NewAdapterRegistry()
	_ = registry.Register(adapter)
	repo := &projectionRepositoryStub{}
	roles, _ := NewProductionRoleRegistry(nil)
	worker := NewProjectionWorker(&eventResolverStub{resolved: ResolvedProjectionSource{Source: ref, Relevant: true}}, registry, NewProjectionService(repo, roles), "")
	if err := worker.Handle(context.Background(), mutation.EventRecord{SubjectVersion: 4}); err != nil {
		t.Fatal(err)
	}
	if len(repo.applied) != 1 || repo.applied[0].SourceRevision != 4 || len(repo.applied[0].Projections) != 0 || !repo.applied[0].TrustedSourceReload {
		t.Fatalf("batch=%+v", repo.applied)
	}
}

func TestProjectionWorkerUsesCompositeRevisionWhenDeletedSourceCannotReload(t *testing.T) {
	ref := SourceRef{MSPID: "msp", ClientID: "client", Type: "work_record", ID: "work"}
	adapter := &projectingAdapterStub{sourceType: "work_record", err: scope.ErrNotFound}
	registry := NewAdapterRegistry()
	_ = registry.Register(adapter)
	repo := &projectionRepositoryStub{}
	roles, _ := NewProductionRoleRegistry(nil)
	resolver := &eventResolverStub{resolved: ResolvedProjectionSource{Source: ref, Relevant: true}, revision: 11}
	worker := NewProjectionWorker(resolver, registry, NewProjectionService(repo, roles), "")
	if err := worker.Handle(context.Background(), mutation.EventRecord{SubjectVersion: 7}); err != nil {
		t.Fatal(err)
	}
	if len(repo.applied) != 1 || repo.applied[0].SourceRevision != 11 || len(repo.applied[0].Projections) != 0 {
		t.Fatalf("batch=%+v", repo.applied)
	}
}

type currentRoleRepository struct {
	projectionRepositoryStub
	definitions map[string]EventRoleDefinition
}

func (r *currentRoleRepository) LoadCustomRoleDefinitions(context.Context, string) (map[string]EventRoleDefinition, error) {
	return r.definitions, nil
}
func TestProjectionServiceUsesCurrentCustomDefinitions(t *testing.T) {
	role := EventRoleDefinition{SourceType: "custom_date", Role: "follow_up", SourceRoleKey: "follow_up", SchedulingMode: Informational, ReadOnly: true}
	repo := &currentRoleRepository{definitions: map[string]EventRoleDefinition{"field": role}}
	registry, _ := NewProductionRoleRegistry(nil)
	service := NewProjectionService(repo, registry)
	date := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	source := SourceRef{MSPID: "msp", ClientID: "client", Type: "custom_date", ID: "value"}
	batch := ProjectionBatch{Source: source, SourceRevision: 1, Projections: []Projection{{ID: "projection", Source: source, SourceRevision: 1, SourceRoleKey: "follow_up", EventRole: "follow_up", Title: "Follow up", AllDay: true, StartsOn: &date, SchedulingMode: Informational, TerminalState: Active}}}
	if _, err := service.Apply(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	repo.definitions = map[string]EventRoleDefinition{}
	if _, err := service.Apply(context.Background(), batch); err == nil {
		t.Fatal("removed role accepted")
	}
	if len(repo.applied) != 1 {
		t.Fatal("invalid projection reached persistence")
	}
}

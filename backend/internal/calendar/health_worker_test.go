package calendar

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type healthRepositoryStub struct {
	resolved             []HealthProjectionContext
	updates              [][]HealthUpdate
	events               []HealthRecomputeEvent
	resolveErr, applyErr error
}

func (r *healthRepositoryStub) ResolveAffectedHealth(context.Context, HealthRecomputeEvent) ([]HealthProjectionContext, error) {
	return append([]HealthProjectionContext(nil), r.resolved...), r.resolveErr
}
func (r *healthRepositoryStub) ApplyHealthResultsAtomic(_ context.Context, event HealthRecomputeEvent, updates []HealthUpdate) (bool, error) {
	r.events = append(r.events, event)
	r.updates = append(r.updates, append([]HealthUpdate(nil), updates...))
	return true, r.applyErr
}

func TestHealthWorkerConsumesEveryRelevantFactAndSortsUpdates(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	for _, kind := range []HealthFactKind{HealthFactProjection, HealthFactSourceStatus, HealthFactDependency, HealthFactSchedule, HealthFactAvailability, HealthFactCapacity, HealthFactSLA} {
		repository := &healthRepositoryStub{resolved: []HealthProjectionContext{
			{ProjectionID: "z", MSPID: "msp", ClientID: "client", Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "source-z"}, SourceRevision: 2, Context: HealthContext{CapacityShortage: true}},
			{ProjectionID: "a", MSPID: "msp", ClientID: "client", Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "source-a"}, SourceRevision: 3, Context: HealthContext{}},
		}}
		worker := NewHealthWorker(repository, func() time.Time { return now })
		event := HealthRecomputeEvent{SourceEventID: "00000000-0000-4000-8000-000000000001", MSPID: "msp", Kind: kind, SubjectID: "subject", OccurredAt: now}
		if err := worker.Handle(context.Background(), event); err != nil {
			t.Fatalf("kind=%q error=%v", kind, err)
		}
		if got := []string{repository.updates[0][0].ProjectionID, repository.updates[0][1].ProjectionID}; !reflect.DeepEqual(got, []string{"a", "z"}) {
			t.Fatalf("kind=%q order=%v", kind, got)
		}
		if repository.updates[0][1].Result.State != HealthAtRisk || repository.updates[0][1].Result.RuleVersion != CurrentHealthRuleVersion {
			t.Fatalf("kind=%q update=%+v", kind, repository.updates[0][1])
		}
	}
}

func TestHealthWorkerRejectsUnknownFactsBeforePersistence(t *testing.T) {
	repository := &healthRepositoryStub{}
	worker := NewHealthWorker(repository, time.Now)
	err := worker.Handle(context.Background(), HealthRecomputeEvent{SourceEventID: "00000000-0000-4000-8000-000000000001", MSPID: "msp", Kind: "comment", SubjectID: "subject", OccurredAt: time.Now()})
	if !errors.Is(err, ErrInvalidHealthRecompute) || len(repository.updates) != 0 {
		t.Fatalf("error=%v updates=%v", err, repository.updates)
	}
}

func TestHealthWorkerRejectsMismatchedResolvedScope(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repository := &healthRepositoryStub{resolved: []HealthProjectionContext{{
		ProjectionID: "projection", MSPID: "msp", ClientID: "client",
		Source: SourceRef{MSPID: "other-msp", ClientID: "client", Type: "task", ID: "source"}, SourceRevision: 1,
	}}}
	err := NewHealthWorker(repository, func() time.Time { return now }).Handle(context.Background(), HealthRecomputeEvent{SourceEventID: "event", MSPID: "msp", Kind: HealthFactProjection, SubjectID: "projection", OccurredAt: now})
	if !errors.Is(err, ErrInvalidHealthRecompute) || len(repository.updates) != 0 {
		t.Fatalf("error=%v updates=%v", err, repository.updates)
	}
}

func TestHealthWorkerAcceptsDeletedDependencyEndpointContext(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repository := &healthRepositoryStub{}
	event := HealthRecomputeEvent{SourceEventID: "event", MSPID: "msp", Kind: HealthFactDependency, SubjectID: "deleted-edge", ProjectionIDs: []string{"successor"}, OccurredAt: now}
	if err := NewHealthWorker(repository, func() time.Time { return now }).Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(repository.events[0].ProjectionIDs, []string{"successor"}) {
		t.Fatalf("event=%+v", repository.events[0])
	}
}

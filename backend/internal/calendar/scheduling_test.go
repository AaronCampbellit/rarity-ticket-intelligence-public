package calendar

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type schedulingTxStub struct {
	applied  int
	failAt   int
	evidence mutation.Evidence
}

func (t *schedulingTxStub) RevalidateSchedulingChange(context.Context, authorization.Principal, PreparedChange) error {
	return nil
}
func (t *schedulingTxStub) ApplyTypedScheduleMutation(_ context.Context, _ PreparedChange, evidence mutation.Evidence) error {
	t.evidence = evidence
	t.applied++
	if t.applied == t.failAt {
		return errors.New("injected")
	}
	return nil
}

type schedulingAtomicStub struct {
	tx        *schedulingTxStub
	committed bool
}

func (r *schedulingAtomicStub) ApplySchedulingProposalAtomic(ctx context.Context, request ScheduleApplyRequest, apply func(context.Context, ScheduleTx) error) (AppliedProposal, error) {
	if err := apply(ctx, r.tx); err != nil {
		return AppliedProposal{}, err
	}
	r.committed = true
	return AppliedProposal{ProposalID: request.Proposal.ID}, nil
}

type schedulingAdapter struct{ typ string }

func (a schedulingAdapter) SourceType() string { return a.typ }
func (a schedulingAdapter) Prepare(context.Context, authorization.Principal, RequestedChange) (PreparedChange, error) {
	return PreparedChange{}, nil
}
func (a schedulingAdapter) Apply(ctx context.Context, tx ScheduleTx, p PreparedChange, e mutation.Evidence) error {
	return ApplyPreparedWithTx(ctx, tx, authorization.Principal{ID: e.ActorID}, p, e)
}

func TestScheduleUnitOfWorkStopsBeforeCommitWhenCascadeFails(t *testing.T) {
	registry := NewWriteAdapterRegistry()
	_ = registry.Register(schedulingAdapter{typ: "task"})
	repository := &schedulingAtomicStub{tx: &schedulingTxStub{failAt: 2}}
	uow := NewScheduleUnitOfWork(repository, registry, func() time.Time { return time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC) }, func() string { return "correlation" })
	proposal := SchedulingProposal{ID: "proposal", ActorID: "actor", Conflicts: []Conflict{{Severity: ConflictOverrideable, PolicyID: "policy"}}}
	change := ProposedChange{Prepared: PreparedChange{Source: SourceRef{Type: "task"}}, Conflicts: []Conflict{{Severity: ConflictOverrideable, PolicyID: "policy"}}}
	_, err := uow.ApplyAtomic(context.Background(), ScheduleApplyRequest{Principal: authorization.Principal{ID: "actor"}, Proposal: proposal, Changes: []ProposedChange{change, change}, OverrideReason: " approved overlap "})
	if err == nil || repository.committed {
		t.Fatalf("err=%v committed=%v", err, repository.committed)
	}
	if repository.tx.evidence.Reason != "approved overlap" || len(repository.tx.evidence.OverriddenPolicyIDs) != 1 || repository.tx.evidence.OverriddenPolicyIDs[0] != "policy" {
		t.Fatalf("evidence=%+v", repository.tx.evidence)
	}
}

func TestScheduleUnitOfWorkRejectsPerChangeHardConflictsAndDependencyBlockers(t *testing.T) {
	registry := NewWriteAdapterRegistry()
	_ = registry.Register(schedulingAdapter{typ: "task"})
	for name, test := range map[string]struct {
		change ProposedChange
		want   error
	}{
		"hard conflict":      {change: ProposedChange{Prepared: PreparedChange{Source: SourceRef{Type: "task"}}, Conflicts: []Conflict{{Severity: ConflictHard}}}, want: ErrHardSchedulingConflict},
		"dependency blocker": {change: ProposedChange{Prepared: PreparedChange{Source: SourceRef{Type: "task"}}, BlockedSources: []CascadeBlockedSource{{ProjectionID: "blocked", ReasonCode: "fixed_schedule"}}}, want: ErrBlockedSchedulingCascade},
	} {
		t.Run(name, func(t *testing.T) {
			repository := &schedulingAtomicStub{tx: &schedulingTxStub{}}
			uow := NewScheduleUnitOfWork(repository, registry, time.Now, func() string { return "correlation" })
			_, err := uow.ApplyAtomic(context.Background(), ScheduleApplyRequest{Principal: authorization.Principal{ID: "actor"}, Proposal: SchedulingProposal{ID: "proposal", ActorID: "actor"}, Changes: []ProposedChange{test.change}})
			if !errors.Is(err, test.want) || repository.committed {
				t.Fatalf("err=%v want=%v committed=%v", err, test.want, repository.committed)
			}
		})
	}
}

type impactRepositoryStub struct {
	projections    map[string]Projection
	edges          []Dependency
	conflictInputs map[string]ConflictInput
	capacityInputs map[string]CapacityInput
}

func (r impactRepositoryStub) LoadDependencyProjection(_ context.Context, id string) (Projection, error) {
	return r.projections[id], nil
}
func (r impactRepositoryStub) ListDependencies(context.Context, string, string) ([]Dependency, error) {
	return r.edges, nil
}
func (r impactRepositoryStub) LoadConflictInput(_ context.Context, _ authorization.Principal, p ProposedSchedule) (ConflictInput, error) {
	if input, ok := r.conflictInputs[p.ProjectionID]; ok {
		return input, nil
	}
	return ConflictInput{Proposed: p, Policies: []ConflictPolicy{{ID: "policy", Kind: ConflictOrdinaryOverbooking, Severity: ConflictOverrideable, Version: 2}}, Constraints: []ConflictConstraint{{Kind: ConflictOrdinaryOverbooking, Interval: p.Interval}}}, nil
}
func (r impactRepositoryStub) LoadCapacityInputs(_ context.Context, _ authorization.Principal, w QueryWindow, ids []string) (map[string]CapacityInput, error) {
	found := map[string]CapacityInput{}
	for _, id := range ids {
		if input, ok := r.capacityInputs[id]; ok {
			input.Window = w
			found[id] = input
			continue
		}
		found[id] = CapacityInput{Window: w, Available: []AvailabilitySegment{{Interval: TimeInterval{Start: w.Start, End: w.End}, CapacityPercent: 100}}}
	}
	return found, nil
}

type changingBindingImpactRepository struct {
	impactRepositoryStub
	calls int
}

func (r *changingBindingImpactRepository) SchedulingRevisionBindings(context.Context, authorization.Principal, ProposedSchedule) ([]RevisionBinding, error) {
	r.calls++
	return []RevisionBinding{{Kind: RevisionPolicyScope, ID: "msp", Version: int64(r.calls)}}, nil
}

func TestSchedulingImpactServiceUsesDependencyConflictCapacityHealthAndNotificationEngines(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	successorStart := end
	successorEnd := successorStart.Add(time.Hour)
	root := Projection{ID: "root", Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "task-a"}, EventRole: "scheduled_work", SourceRevision: 2, Title: "Root", StartsAt: &start, EndsAt: &end, Timezone: "UTC", SchedulingMode: FixedBlock, AssigneeID: "tech-a", PlannedMinutes: 60, CapacityBearing: true, TerminalState: Active}
	successor := Projection{ID: "successor", Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "task-b"}, EventRole: "scheduled_work", SourceRevision: 3, Title: "Successor", StartsAt: &successorStart, EndsAt: &successorEnd, Timezone: "UTC", SchedulingMode: FixedBlock, AssigneeID: "tech-b", PlannedMinutes: 60, CapacityBearing: true, TerminalState: Active}
	repo := impactRepositoryStub{projections: map[string]Projection{"root": root, "successor": successor}, edges: []Dependency{{ID: "dependency", MSPID: "msp", ClientID: "client", PredecessorID: "root", SuccessorID: "successor", Type: FinishToStart, Version: 4}}}
	newStart, newEnd := start.Add(2*time.Hour), end.Add(2*time.Hour)
	impact, err := NewSchedulingImpactService(repo).PreviewSchedulingImpact(context.Background(), authorization.Principal{Scope: scope.Principal{MSPID: "msp"}}, ProposedChange{Requested: RequestedChange{ProjectionID: "root", StartsAt: &newStart, EndsAt: &newEnd}, Schedule: ProposedSchedule{ProjectionID: "root", ClientID: "client", TechnicianID: "tech-a", Interval: TimeInterval{Start: newStart, End: newEnd}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(impact.Cascade) != 1 || !impact.Cascade[0].Required || len(impact.Conflicts) != 1 || len(impact.Capacity) != 1 || len(impact.Health) != 2 || len(impact.Notifications) != 1 {
		t.Fatalf("impact=%+v", impact)
	}
	if impact.Capacity[0].DeltaMinutes != 60 || impact.Capacity[0].CommittedMinutes != 60 {
		t.Fatalf("proposed capacity was not applied: %+v", impact.Capacity[0])
	}
}

func TestSchedulingImpactServiceSurfacesBlockedDependencySources(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	root := schedulingTestProjection("root", "task-a", "tech-a", 2)
	root.StartsAt, root.EndsAt = &start, &end
	blocked := schedulingTestProjection("blocked", "task-b", "tech-b", 3)
	blockedStart, blockedEnd := end, end.Add(time.Hour)
	blocked.StartsAt, blocked.EndsAt = &blockedStart, &blockedEnd
	blocked.SchedulingMode = Informational
	repo := impactRepositoryStub{projections: map[string]Projection{"root": root, "blocked": blocked}, edges: []Dependency{{ID: "dependency", MSPID: "msp", ClientID: "client", PredecessorID: "root", SuccessorID: "blocked", Type: FinishToStart, Version: 1}}}
	newStart, newEnd := start.Add(2*time.Hour), end.Add(2*time.Hour)
	impact, err := NewSchedulingImpactService(repo).PreviewSchedulingImpact(context.Background(), schedulingPrincipal(), ProposedChange{Requested: RequestedChange{ProjectionID: "root", StartsAt: &newStart, EndsAt: &newEnd}, Schedule: ProposedSchedule{ProjectionID: "root", ClientID: "client", TechnicianID: "tech-a", Interval: TimeInterval{Start: newStart, End: newEnd}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(impact.BlockedSources) != 1 || impact.BlockedSources[0].ProjectionID != "blocked" {
		t.Fatalf("blocked dependency sources=%+v", impact.BlockedSources)
	}
}

func TestSchedulingImpactCascadeCarriesRecurringOccurrenceIdentityAndScope(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	root := schedulingTestProjection("root", "task-a", "tech-a", 2)
	root.StartsAt, root.EndsAt = &start, &end
	successorStart, successorEnd := end, end.Add(time.Hour)
	successor := schedulingTestProjection("successor", "task-b", "tech-b", 3)
	successor.StartsAt, successor.EndsAt = &successorStart, &successorEnd
	successor.Recurrence = &RecurrenceRule{Frequency: Daily, Interval: 1, Count: 4}
	repo := impactRepositoryStub{projections: map[string]Projection{"root": root, "successor": successor}, edges: []Dependency{{ID: "dependency", MSPID: "msp", ClientID: "client", PredecessorID: "root", SuccessorID: "successor", Type: FinishToStart, Version: 1}}}
	newStart, newEnd := start.Add(2*time.Hour), end.Add(2*time.Hour)
	impact, err := NewSchedulingImpactService(repo).PreviewSchedulingImpact(context.Background(), schedulingPrincipal(), ProposedChange{Requested: RequestedChange{ProjectionID: "root", OccurrenceScope: ThisAndFuture, StartsAt: &newStart, EndsAt: &newEnd}, Schedule: ProposedSchedule{ProjectionID: "root", ClientID: "client", TechnicianID: "tech-a", Interval: TimeInterval{Start: newStart, End: newEnd}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(impact.Cascade) != 1 || impact.Cascade[0].OccurrenceScope != ThisAndFuture || impact.Cascade[0].OccurrenceKey != "2026-08-10T10:00:00" {
		t.Fatalf("cascade=%+v", impact.Cascade)
	}
}

func TestSchedulingImpactCombinedOverlayMovesExistingConflictAwayAndDetectsProposedOverlap(t *testing.T) {
	start := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	aCurrentStart, aCurrentEnd := start.Add(-2*time.Hour), start.Add(-time.Hour)
	bCurrentStart, bCurrentEnd := start, start.Add(time.Hour)
	a := schedulingTestProjection("a", "task-a", "tech-a", 1)
	a.StartsAt, a.EndsAt = &aCurrentStart, &aCurrentEnd
	a.SchedulingMode, a.PlannedMinutes = FixedBlock, 60
	b := schedulingTestProjection("b", "task-b", "tech-a", 1)
	b.StartsAt, b.EndsAt = &bCurrentStart, &bCurrentEnd
	b.SchedulingMode, b.PlannedMinutes = FixedBlock, 60
	policy := []ConflictPolicy{{ID: "ordinary", Kind: ConflictOrdinaryOverbooking, Severity: ConflictOverrideable, Version: 1}}
	repo := impactRepositoryStub{
		projections: map[string]Projection{"a": a, "b": b},
		conflictInputs: map[string]ConflictInput{
			"a": {Policies: policy, Constraints: []ConflictConstraint{{Kind: ConflictOrdinaryOverbooking, Interval: TimeInterval{Start: bCurrentStart, End: bCurrentEnd}, Related: SafeSourceRef{Type: "task", ID: "task-b"}}}},
			"b": {Policies: policy},
		},
		capacityInputs: map[string]CapacityInput{
			"tech-a": {
				Available: []AvailabilitySegment{{Interval: TimeInterval{Start: start, End: start.Add(2 * time.Hour)}, CapacityPercent: 100}},
				Events: []CapacityEvent{
					{ID: "a", Assigned: true, Mode: FixedBlock, PlannedMinutes: 60, Interval: TimeInterval{Start: aCurrentStart, End: aCurrentEnd}},
					{ID: "b", Assigned: true, Mode: FixedBlock, PlannedMinutes: 60, Interval: TimeInterval{Start: bCurrentStart, End: bCurrentEnd}},
				},
			},
		},
	}
	change := func(id string, from, to time.Time) ProposedChange {
		return ProposedChange{
			Requested: RequestedChange{ProjectionID: id, StartsAt: &from, EndsAt: &to},
			Schedule:  ProposedSchedule{ProjectionID: id, ClientID: "client", TechnicianID: "tech-a", Interval: TimeInterval{Start: from, End: to}},
		}
	}
	service := NewSchedulingImpactService(repo)
	nonOverlapping, err := service.PreviewCombinedSchedulingImpact(context.Background(), schedulingPrincipal(), []ProposedChange{
		change("a", start, start.Add(time.Hour)),
		change("b", start.Add(time.Hour), start.Add(2*time.Hour)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(nonOverlapping[0].Conflicts) != 0 {
		t.Fatalf("conflict with moving source was not removed: %+v", nonOverlapping[0].Conflicts)
	}
	if len(nonOverlapping[0].Capacity) != 1 || nonOverlapping[0].Capacity[0].CommittedMinutes != 120 || nonOverlapping[0].Capacity[0].OverbookedMinutes != 0 {
		t.Fatalf("non-overlapping aggregate capacity=%+v", nonOverlapping[0].Capacity)
	}

	overlapping, err := service.PreviewCombinedSchedulingImpact(context.Background(), schedulingPrincipal(), []ProposedChange{
		change("a", start, start.Add(time.Hour)),
		change("b", start.Add(30*time.Minute), start.Add(90*time.Minute)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(overlapping[0].Conflicts) != 1 || overlapping[0].Conflicts[0].Related.ID != "task-b" {
		t.Fatalf("proposed overlap conflicts=%+v", overlapping[0].Conflicts)
	}
	if len(overlapping[0].Capacity) != 1 || overlapping[0].Capacity[0].CommittedMinutes != 120 || overlapping[0].Capacity[0].OverbookedMinutes != 30 {
		t.Fatalf("overlapping aggregate capacity=%+v", overlapping[0].Capacity)
	}
}

func TestSchedulingImpactRejectsScopeBindingsChangedDuringPreview(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	projection := schedulingTestProjection("root", "task-a", "tech-a", 1)
	projection.StartsAt, projection.EndsAt = &start, &end
	repo := &changingBindingImpactRepository{impactRepositoryStub: impactRepositoryStub{projections: map[string]Projection{"root": projection}}}
	_, err := NewSchedulingImpactService(repo).PreviewSchedulingImpact(context.Background(), schedulingPrincipal(), ProposedChange{
		Requested: RequestedChange{ProjectionID: "root", StartsAt: &start, EndsAt: &end},
		Schedule:  ProposedSchedule{ProjectionID: "root", ClientID: "client", TechnicianID: "tech-a", Interval: TimeInterval{Start: start, End: end}},
	})
	if !errors.Is(err, ErrStaleProposal) {
		t.Fatalf("err=%v want=%v", err, ErrStaleProposal)
	}
}

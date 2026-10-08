package calendar

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type proposalProjectionStub struct {
	values map[string]Projection
}

func (s *proposalProjectionStub) ResolveSchedulingProjection(_ context.Context, _ authorization.Principal, id, _ string) (Projection, error) {
	p, ok := s.values[id]
	if !ok {
		return Projection{}, scope.ErrNotFound
	}
	return p, nil
}

type proposalWorkforceStub struct{ allowed map[string]bool }

func (s *proposalWorkforceStub) AuthorizeScheduling(_ context.Context, _ authorization.Principal, proposed ProposedSchedule, _ time.Time) error {
	if proposed.TechnicianID != "" && !s.allowed[proposed.TechnicianID] {
		return authorization.ErrForbidden
	}
	return nil
}

type proposalImpactStub struct {
	impact  SchedulingImpact
	impacts map[string]SchedulingImpact
}

type declinedOptionalImpactStub struct {
	optional RequestedChange
}

func (s *declinedOptionalImpactStub) PreviewSchedulingImpact(_ context.Context, _ authorization.Principal, change ProposedChange) (SchedulingImpact, error) {
	if change.Requested.ProjectionID == "event-a" {
		return SchedulingImpact{Cascade: []RequestedChange{s.optional}}, nil
	}
	return SchedulingImpact{}, nil
}

func (s *declinedOptionalImpactStub) PreviewCombinedSchedulingImpact(_ context.Context, _ authorization.Principal, changes []ProposedChange) ([]SchedulingImpact, error) {
	impacts := make([]SchedulingImpact, len(changes))
	if len(changes) == 2 {
		impacts[0].Cascade = []RequestedChange{s.optional}
		return impacts, nil
	}
	impacts[0].Conflicts = []Conflict{{Severity: ConflictOverrideable, ReasonRequired: true, PolicyID: "fresh-policy", PolicyVersion: 7}}
	return impacts, nil
}

func (s *proposalImpactStub) PreviewSchedulingImpact(_ context.Context, _ authorization.Principal, change ProposedChange) (SchedulingImpact, error) {
	if impact, ok := s.impacts[change.Requested.ProjectionID]; ok {
		return impact, nil
	}
	return s.impact, nil
}

type proposalStoreStub struct {
	proposal      SchedulingProposal
	stripPrepared bool
}

func (s *proposalStoreStub) SaveSchedulingProposal(_ context.Context, proposal SchedulingProposal) error {
	s.proposal = proposal
	return nil
}
func (s *proposalStoreStub) LoadSchedulingProposal(context.Context, string, string) (SchedulingProposal, error) {
	p := s.proposal
	if s.stripPrepared {
		for i := range p.Changes {
			p.Changes[i].Prepared = PreparedChange{}
		}
	}
	return p, nil
}

type proposalUOWStub struct {
	calls int
	err   error
}

func (s *proposalUOWStub) ApplyAtomic(context.Context, ScheduleApplyRequest) (AppliedProposal, error) {
	s.calls++
	return AppliedProposal{ProposalID: "proposal"}, s.err
}

type proposalAdapterStub struct{}

func (proposalAdapterStub) SourceType() string { return "task" }
func (proposalAdapterStub) Prepare(_ context.Context, _ authorization.Principal, requested RequestedChange) (PreparedChange, error) {
	return PreparedChange{Request: requested, Source: requested.Source, EventRole: requested.EventRole, ExpectedSourceRevision: requested.SourceRevision}, nil
}
func (proposalAdapterStub) Apply(context.Context, ScheduleTx, PreparedChange, SchedulingEvidence) error {
	return nil
}

func schedulingTestProjection(id, sourceID, technician string, revision int64) Projection {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	return Projection{ID: id, Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: sourceID}, EventRole: "scheduled_work", SourceRevision: revision, Title: "Task", StartsAt: &start, EndsAt: &end, Timezone: "UTC", SchedulingMode: FixedBlock, AssigneeID: technician, PlannedMinutes: 60, CapacityBearing: true, TerminalState: Active}
}

func schedulingPrincipal() authorization.Principal {
	return authorization.Principal{ID: "actor", Scope: scope.Principal{MSPID: "msp"}, Capabilities: authorization.NewCapabilitySet("calendar.schedule")}
}

func proposalServiceFixture() (*ProposalService, *proposalProjectionStub, *proposalStoreStub, *proposalUOWStub) {
	projections := &proposalProjectionStub{values: map[string]Projection{
		"event-a": schedulingTestProjection("event-a", "task-a", "tech-a", 2),
		"event-b": schedulingTestProjection("event-b", "task-b", "tech-b", 3),
	}}
	store := &proposalStoreStub{}
	uow := &proposalUOWStub{}
	adapters := NewWriteAdapterRegistry()
	_ = adapters.Register(proposalAdapterStub{})
	service := NewProposalService(ProposalServiceDependencies{
		Projections: projections,
		Workforce:   &proposalWorkforceStub{allowed: map[string]bool{"tech-a": true, "tech-b": true}},
		Impacts: &proposalImpactStub{impacts: map[string]SchedulingImpact{
			"event-a": {
				Cascade:   []RequestedChange{{ProjectionID: "event-b", StartsAt: timePtr(time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)), EndsAt: timePtr(time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)), Required: true}},
				Conflicts: []Conflict{{Severity: ConflictOverrideable, ReasonRequired: true, PolicyID: "policy", PolicyVersion: 4}},
				Capacity:  []CapacityImpact{{TechnicianID: "tech-a", DeltaMinutes: 60}},
				Health:    []HealthImpact{{ProjectionID: "event-b", State: HealthAtRisk}},
				Bindings:  []RevisionBinding{{Kind: RevisionPolicy, ID: "policy", Version: 4}},
			},
			"event-b": {},
		}},
		Store: store, UnitOfWork: uow, Adapters: adapters,
		Now:   func() time.Time { return time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC) },
		NewID: func() string { return "proposal" }, TTL: 5 * time.Minute,
	})
	return service, projections, store, uow
}

func TestApplyRejectsStaleExpectedProposalVersionBeforeMutation(t *testing.T) {
	service, _, store, uow := proposalServiceFixture()
	store.proposal = SchedulingProposal{ID: "proposal", Version: 2, ActorID: schedulingPrincipal().ID, State: "previewed"}
	_, err := service.Apply(context.Background(), ApplyCommand{Principal: schedulingPrincipal(), ProposalID: "proposal", ExpectedProposalVersion: 1})
	if !errors.Is(err, ErrStaleProposal) || uow.calls != 0 {
		t.Fatalf("error=%v apply_calls=%d", err, uow.calls)
	}
}

func TestPreviewEvaluatesHardConflictsAndDependencyBlockersForCascadeChanges(t *testing.T) {
	service, _, store, uow := proposalServiceFixture()
	service.dependencies.Impacts = &proposalImpactStub{impacts: map[string]SchedulingImpact{
		"event-a": {Cascade: []RequestedChange{{ProjectionID: "event-b", StartsAt: timePtr(time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)), EndsAt: timePtr(time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)), Required: true}}},
		"event-b": {
			Conflicts:      []Conflict{{Severity: ConflictHard, ReasonCode: string(ConflictApprovedPTO)}},
			BlockedSources: []CascadeBlockedSource{{ProjectionID: "event-b", Source: schedulingTestProjection("event-b", "task-b", "tech-b", 3).Source, ReasonCode: "fixed_schedule"}},
		},
	}}
	proposal, err := service.Preview(context.Background(), PreviewCommand{Principal: schedulingPrincipal(), PrimaryChange: RequestedChange{ProjectionID: "event-a", StartsAt: timePtr(time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)), EndsAt: timePtr(time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC))}})
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Changes) != 2 || len(proposal.Changes[1].Conflicts) != 1 || len(proposal.Changes[1].BlockedSources) != 1 || len(proposal.Conflicts) != 1 || len(proposal.BlockedSources) != 1 {
		t.Fatalf("cascade impact not surfaced: %+v", proposal)
	}
	store.proposal = proposal
	if _, err = service.Apply(context.Background(), ApplyCommand{Principal: schedulingPrincipal(), ProposalID: proposal.ID}); !errors.Is(err, ErrHardSchedulingConflict) {
		t.Fatalf("apply error=%v, want hard conflict", err)
	}
	if uow.calls != 0 {
		t.Fatal("blocked cascade reached unit of work")
	}
}

func TestOptionalCascadeConflictsOnlyBlockWhenAccepted(t *testing.T) {
	service, _, store, uow := proposalServiceFixture()
	service.dependencies.Impacts = &proposalImpactStub{impacts: map[string]SchedulingImpact{
		"event-a": {Cascade: []RequestedChange{{ProjectionID: "event-b", StartsAt: timePtr(time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)), EndsAt: timePtr(time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)), Required: false}}},
		"event-b": {Conflicts: []Conflict{{Severity: ConflictHard, ReasonCode: string(ConflictNonWorkingTime)}}},
	}}
	proposal, err := service.Preview(context.Background(), PreviewCommand{Principal: schedulingPrincipal(), PrimaryChange: RequestedChange{ProjectionID: "event-a", StartsAt: timePtr(time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)), EndsAt: timePtr(time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC))}})
	if err != nil {
		t.Fatal(err)
	}
	store.proposal = proposal
	if _, err = service.Apply(context.Background(), ApplyCommand{Principal: schedulingPrincipal(), ProposalID: proposal.ID}); err != nil {
		t.Fatalf("unaccepted optional conflict blocked apply: %v", err)
	}
	if uow.calls != 1 {
		t.Fatalf("apply calls=%d", uow.calls)
	}
	uow.calls = 0
	if _, err = service.Apply(context.Background(), ApplyCommand{Principal: schedulingPrincipal(), ProposalID: proposal.ID, AcceptedOptionalChangeIDs: proposal.OptionalChangeIDs()}); !errors.Is(err, ErrHardSchedulingConflict) {
		t.Fatalf("accepted optional error=%v, want hard conflict", err)
	}
	if uow.calls != 0 {
		t.Fatal("accepted optional hard conflict reached unit of work")
	}
}

func TestApplyAuditsFreshOverrideWhenOptionalMoveIsDeclined(t *testing.T) {
	service, _, store, _ := proposalServiceFixture()
	optional := RequestedChange{ProjectionID: "event-b", StartsAt: timePtr(time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)), EndsAt: timePtr(time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)), Required: false}
	service.dependencies.Impacts = &declinedOptionalImpactStub{optional: optional}
	repository := &schedulingAtomicStub{tx: &schedulingTxStub{}}
	registry := NewWriteAdapterRegistry()
	if err := registry.Register(schedulingAdapter{typ: "task"}); err != nil {
		t.Fatal(err)
	}
	service.dependencies.UnitOfWork = NewScheduleUnitOfWork(repository, registry, service.dependencies.Now, func() string { return "correlation" })
	proposal, err := service.Preview(context.Background(), PreviewCommand{Principal: schedulingPrincipal(), PrimaryChange: RequestedChange{ProjectionID: "event-a", StartsAt: timePtr(time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)), EndsAt: timePtr(time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC))}})
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Changes[0].Conflicts) != 0 {
		t.Fatalf("preview unexpectedly stored root conflicts: %+v", proposal.Changes[0].Conflicts)
	}
	store.proposal = proposal
	if _, err = service.Apply(context.Background(), ApplyCommand{Principal: schedulingPrincipal(), ProposalID: proposal.ID, OverrideReason: "declined optional move"}); err != nil {
		t.Fatal(err)
	}
	if len(store.proposal.Changes[0].Conflicts) != 0 {
		t.Fatalf("stored proposal conflict tuple was mutated: %+v", store.proposal.Changes[0].Conflicts)
	}
	if got := repository.tx.evidence.OverriddenPolicyIDs; len(got) != 1 || got[0] != "fresh-policy" {
		t.Fatalf("audited override policies=%v want=[fresh-policy]", got)
	}
}

func TestPreviewReturnsCascadeCapacityAndOverrideRequirements(t *testing.T) {
	service, _, _, _ := proposalServiceFixture()
	found, err := service.Preview(context.Background(), PreviewCommand{Principal: schedulingPrincipal(), PrimaryChange: RequestedChange{ProjectionID: "event-a", StartsAt: timePtr(time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)), EndsAt: timePtr(time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))}})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Changes) != 2 || len(found.Conflicts) != 1 || len(found.Capacity) != 1 || len(found.Health) != 1 || !found.RequiresReason {
		t.Fatalf("proposal lacks impact: %+v", found)
	}
}

func TestPreviewRequiresSourceAndWorkforceAuthorityForEveryChange(t *testing.T) {
	service, _, _, _ := proposalServiceFixture()
	service.dependencies.Workforce = &proposalWorkforceStub{allowed: map[string]bool{"tech-a": true}}
	_, err := service.Preview(context.Background(), PreviewCommand{Principal: schedulingPrincipal(), PrimaryChange: RequestedChange{ProjectionID: "event-a", StartsAt: timePtr(time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)), EndsAt: timePtr(time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC))}})
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("error = %v, want forbidden", err)
	}
}

func TestRecurringChangeRequiresExplicitScope(t *testing.T) {
	service, projections, _, _ := proposalServiceFixture()
	p := projections.values["event-a"]
	p.Recurrence = &RecurrenceRule{Frequency: Weekly, Interval: 1, Count: 2}
	projections.values["event-a"] = p
	_, err := service.Preview(context.Background(), PreviewCommand{Principal: schedulingPrincipal(), PrimaryChange: RequestedChange{ProjectionID: "event-a", OccurrenceKey: "2026-08-10T09:00:00", StartsAt: timePtr(time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)), EndsAt: timePtr(time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC))}})
	if !errors.Is(err, ErrOccurrenceScopeRequired) {
		t.Fatalf("error = %v, want occurrence scope", err)
	}
}

func TestApplyRejectsStaleProposalWithoutPartialChanges(t *testing.T) {
	service, projections, store, uow := proposalServiceFixture()
	proposal, err := service.Preview(context.Background(), PreviewCommand{Principal: schedulingPrincipal(), PrimaryChange: RequestedChange{ProjectionID: "event-a", StartsAt: timePtr(time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)), EndsAt: timePtr(time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC))}})
	if err != nil {
		t.Fatal(err)
	}
	p := projections.values["event-a"]
	p.SourceRevision++
	projections.values["event-a"] = p
	store.proposal = proposal
	_, err = service.Apply(context.Background(), ApplyCommand{Principal: schedulingPrincipal(), ProposalID: proposal.ID, AcceptedOptionalChangeIDs: proposal.OptionalChangeIDs(), OverrideReason: "Dispatch-approved overlap"})
	if !errors.Is(err, ErrStaleProposal) {
		t.Fatalf("error = %v, want stale proposal", err)
	}
	if uow.calls != 0 {
		t.Fatal("stale proposal applied partial changes")
	}
}

func TestApplyRepreparesProposalLoadedFromPersistence(t *testing.T) {
	service, _, store, uow := proposalServiceFixture()
	proposal, err := service.Preview(context.Background(), PreviewCommand{Principal: schedulingPrincipal(), PrimaryChange: RequestedChange{ProjectionID: "event-a", StartsAt: timePtr(time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)), EndsAt: timePtr(time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC))}})
	if err != nil {
		t.Fatal(err)
	}
	store.proposal = proposal
	store.stripPrepared = true
	_, err = service.Apply(context.Background(), ApplyCommand{Principal: schedulingPrincipal(), ProposalID: proposal.ID, AcceptedOptionalChangeIDs: proposal.OptionalChangeIDs(), OverrideReason: "Dispatch-approved overlap"})
	if err != nil {
		t.Fatal(err)
	}
	if uow.calls != 1 {
		t.Fatalf("apply calls=%d", uow.calls)
	}
}

func TestApplyRejectsActorExpiryHardConflictReasonAndUnknownOptional(t *testing.T) {
	service, _, store, uow := proposalServiceFixture()
	proposal, err := service.Preview(context.Background(), PreviewCommand{Principal: schedulingPrincipal(), PrimaryChange: RequestedChange{ProjectionID: "event-a", StartsAt: timePtr(time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)), EndsAt: timePtr(time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC))}})
	if err != nil {
		t.Fatal(err)
	}
	other := schedulingPrincipal()
	other.ID = "other"
	if _, err = service.Apply(context.Background(), ApplyCommand{Principal: other, ProposalID: proposal.ID, OverrideReason: "reason"}); !errors.Is(err, ErrProposalActorMismatch) {
		t.Fatalf("actor error=%v", err)
	}
	if _, err = service.Apply(context.Background(), ApplyCommand{Principal: schedulingPrincipal(), ProposalID: proposal.ID}); !errors.Is(err, ErrOverrideReasonRequired) {
		t.Fatalf("reason error=%v", err)
	}
	if _, err = service.Apply(context.Background(), ApplyCommand{Principal: schedulingPrincipal(), ProposalID: proposal.ID, AcceptedOptionalChangeIDs: []string{"unknown"}, OverrideReason: "reason"}); !errors.Is(err, ErrInvalidOptionalChange) {
		t.Fatalf("optional error=%v", err)
	}
	store.proposal.Conflicts = []Conflict{{Severity: ConflictHard}}
	store.proposal.Changes[0].Conflicts = []Conflict{{Severity: ConflictHard}}
	store.proposal.RequiresReason = false
	if _, err = service.Apply(context.Background(), ApplyCommand{Principal: schedulingPrincipal(), ProposalID: proposal.ID}); !errors.Is(err, ErrHardSchedulingConflict) {
		t.Fatalf("hard error=%v", err)
	}
	store.proposal = proposal
	service.dependencies.Now = func() time.Time { return proposal.ExpiresAt }
	if _, err = service.Apply(context.Background(), ApplyCommand{Principal: schedulingPrincipal(), ProposalID: proposal.ID, OverrideReason: "reason"}); !errors.Is(err, ErrExpiredProposal) {
		t.Fatalf("expiry error=%v", err)
	}
	if uow.calls != 0 {
		t.Fatalf("invalid proposal reached unit of work %d times", uow.calls)
	}
}

func timePtr(value time.Time) *time.Time { return &value }

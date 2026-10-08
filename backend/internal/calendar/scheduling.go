package calendar

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
)

type ScheduleApplyRequest struct {
	Principal      authorization.Principal
	Proposal       SchedulingProposal
	Changes        []ProposedChange
	OverrideReason string
	// OverridePolicyIDs carries policies found by the authoritative accepted-set
	// recomputation. Changes retain their persisted conflict tuples so the
	// repository can validate the locked proposal without accepting mutations.
	OverridePolicyIDs []string
	EvaluatedAt       time.Time
}

type SchedulingImpactRepository interface {
	LoadDependencyProjection(context.Context, string) (Projection, error)
	ListDependencies(context.Context, string, string) ([]Dependency, error)
	LoadConflictInput(context.Context, authorization.Principal, ProposedSchedule) (ConflictInput, error)
	LoadCapacityInputs(context.Context, authorization.Principal, QueryWindow, []string) (map[string]CapacityInput, error)
}
type SchedulingBindingProvider interface {
	SchedulingRevisionBindings(context.Context, authorization.Principal, ProposedSchedule) ([]RevisionBinding, error)
}

type SchedulingImpactService struct{ repository SchedulingImpactRepository }

func NewSchedulingImpactService(repository SchedulingImpactRepository) *SchedulingImpactService {
	return &SchedulingImpactService{repository}
}
func (s *SchedulingImpactService) PreviewSchedulingImpact(ctx context.Context, principal authorization.Principal, change ProposedChange) (SchedulingImpact, error) {
	if s == nil || s.repository == nil || change.Requested.ProjectionID == "" || !change.Schedule.Interval.valid() {
		return SchedulingImpact{}, ErrInvalidScheduleChange
	}
	var initialScopeBindings []RevisionBinding
	if provider, ok := s.repository.(SchedulingBindingProvider); ok {
		var err error
		initialScopeBindings, err = provider.SchedulingRevisionBindings(ctx, principal, change.Schedule)
		if err != nil {
			return SchedulingImpact{}, err
		}
	}
	root, err := s.repository.LoadDependencyProjection(ctx, change.Requested.ProjectionID)
	if err != nil {
		return SchedulingImpact{}, err
	}
	edges, err := s.repository.ListDependencies(ctx, root.Source.MSPID, root.Source.ClientID)
	if err != nil {
		return SchedulingImpact{}, err
	}
	projections := map[string]Projection{root.ID: root}
	ids := map[string]struct{}{}
	for _, edge := range edges {
		ids[edge.PredecessorID] = struct{}{}
		ids[edge.SuccessorID] = struct{}{}
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		if id != root.ID {
			ordered = append(ordered, id)
		}
	}
	sort.Strings(ordered)
	for _, id := range ordered {
		p, e := s.repository.LoadDependencyProjection(ctx, id)
		if e != nil {
			return SchedulingImpact{}, e
		}
		if p.Source.MSPID != root.Source.MSPID || p.Source.ClientID != root.Source.ClientID {
			return SchedulingImpact{}, ErrCrossClientDependency
		}
		projections[id] = p
	}
	proposed := ScheduleInterval{StartsOn: change.Requested.StartsOn, EndsOn: change.Requested.EndsOn, StartsAt: change.Requested.StartsAt, EndsAt: change.Requested.EndsAt}
	cascade, err := BuildCascadeImpact(root.ID, proposed, projections, edges)
	if err != nil {
		return SchedulingImpact{}, err
	}
	impact := SchedulingImpact{}
	impact.BlockedSources = append(impact.BlockedSources, cascade.BlockedSources...)
	appendMove := func(move CascadeMove, required bool) {
		p := projections[move.ProjectionID]
		requested := RequestedChange{ProjectionID: p.ID, Source: p.Source, EventRole: p.EventRole, SourceRoleKey: p.SourceRoleKey, SourceRevision: p.SourceRevision, AllDay: p.AllDay, StartsOn: move.To.StartsOn, EndsOn: move.To.EndsOn, StartsAt: move.To.StartsAt, EndsAt: move.To.EndsAt, Timezone: p.Timezone, Recurrence: p.Recurrence, Required: required}
		if p.Recurrence != nil {
			requested.OccurrenceScope = change.Requested.OccurrenceScope
			if requested.OccurrenceScope == "" {
				requested.OccurrenceScope = ThisOccurrence
			}
			requested.OccurrenceKey = scheduleIntervalOccurrenceKey(move.From, p)
		}
		impact.Cascade = append(impact.Cascade, requested)
	}
	for _, move := range cascade.RequiredMoves {
		appendMove(move, true)
	}
	for _, move := range cascade.OptionalMoves {
		appendMove(move, false)
	}
	conflictInput, err := s.repository.LoadConflictInput(ctx, principal, change.Schedule)
	if err != nil {
		return SchedulingImpact{}, err
	}
	conflictInput.Proposed = change.Schedule
	impact.Conflicts = EvaluateConflicts(conflictInput)
	technicians := map[string]struct{}{}
	if change.Schedule.TechnicianID != "" {
		technicians[change.Schedule.TechnicianID] = struct{}{}
	}
	techIDs := make([]string, 0, len(technicians))
	for id := range technicians {
		techIDs = append(techIDs, id)
	}
	sort.Strings(techIDs)
	if len(techIDs) > 0 {
		inputs, e := s.repository.LoadCapacityInputs(ctx, principal, QueryWindow{Start: change.Schedule.Interval.Start, End: change.Schedule.Interval.End}, techIDs)
		if e != nil {
			return SchedulingImpact{}, e
		}
		for _, id := range techIDs {
			current := inputs[id]
			before := CalculateCapacity(current)
			updatedEvents := make([]CapacityEvent, 0, len(current.Events)+1)
			for _, event := range current.Events {
				if event.ID == root.ID || strings.HasPrefix(event.ID, root.ID+":") {
					continue
				}
				updatedEvents = append(updatedEvents, event)
			}
			plannedMinutes := root.PlannedMinutes
			if root.SchedulingMode == FixedBlock || plannedMinutes < 1 {
				plannedMinutes = int64(change.Schedule.Interval.End.Sub(change.Schedule.Interval.Start) / time.Minute)
			}
			updatedEvents = append(updatedEvents, CapacityEvent{ID: root.ID, ClientID: root.Source.ClientID, Assigned: true, Mode: root.SchedulingMode, PlannedMinutes: plannedMinutes, Interval: change.Schedule.Interval, Source: SafeSourceRef{Type: root.Source.Type, ID: root.Source.ID}})
			current.Events = updatedEvents
			after := CalculateCapacity(current)
			impact.Capacity = append(impact.Capacity, CapacityImpact{TechnicianID: id, DeltaMinutes: after.CommittedMinutes - before.CommittedMinutes, AvailableMinutes: after.AvailableMinutes, CommittedMinutes: after.CommittedMinutes, OverbookedMinutes: after.OverbookedMinutes})
		}
	}
	for _, id := range cascade.AffectedHealth {
		impact.Health = append(impact.Health, HealthImpact{ProjectionID: id, State: HealthAtRisk, Reasons: []HealthReason{{Code: "schedule_change"}}})
	}
	for _, id := range techIDs {
		impact.Notifications = append(impact.Notifications, NotificationImpact{RecipientID: id, ReasonCode: "schedule_changed", ProjectionID: root.ID})
	}
	for _, edge := range edges {
		impact.Bindings = append(impact.Bindings, RevisionBinding{Kind: RevisionDependency, ID: edge.ID, Version: edge.Version})
	}
	for _, conflict := range impact.Conflicts {
		if strings.HasPrefix(conflict.PolicyID, "default:") {
			continue
		}
		impact.Bindings = append(impact.Bindings, RevisionBinding{Kind: RevisionPolicy, ID: conflict.PolicyID, Version: conflict.PolicyVersion})
	}
	if provider, ok := s.repository.(SchedulingBindingProvider); ok {
		extra, e := provider.SchedulingRevisionBindings(ctx, principal, change.Schedule)
		if e != nil {
			return SchedulingImpact{}, e
		}
		if !reflect.DeepEqual(normalizeBindings(initialScopeBindings), normalizeBindings(extra)) {
			return SchedulingImpact{}, ErrStaleProposal
		}
		impact.Bindings = append(impact.Bindings, extra...)
	}
	impact.Bindings = normalizeBindings(impact.Bindings)
	return impact, nil
}

func (s *SchedulingImpactService) PreviewCombinedSchedulingImpact(ctx context.Context, principal authorization.Principal, changes []ProposedChange) ([]SchedulingImpact, error) {
	if s == nil || s.repository == nil || len(changes) == 0 {
		return nil, ErrInvalidScheduleChange
	}
	initialBindings, err := s.combinedScopeBindings(ctx, principal, changes)
	if err != nil {
		return nil, err
	}
	impacts := make([]SchedulingImpact, len(changes))
	projections := make([]Projection, len(changes))
	movingSources := map[SafeSourceRef]struct{}{}
	for i, change := range changes {
		impact, previewErr := s.PreviewSchedulingImpact(ctx, principal, change)
		if previewErr != nil {
			return nil, previewErr
		}
		impacts[i] = impact
		projection, loadErr := s.repository.LoadDependencyProjection(ctx, change.Requested.ProjectionID)
		if loadErr != nil {
			return nil, loadErr
		}
		projections[i] = projection
		movingSources[SafeSourceRef{Type: projection.Source.Type, ID: projection.Source.ID}] = struct{}{}
	}
	for i, change := range changes {
		filtered := impacts[i].Conflicts[:0]
		for _, conflict := range impacts[i].Conflicts {
			if conflict.ReasonCode == string(ConflictOrdinaryOverbooking) {
				if _, moving := movingSources[conflict.Related]; moving {
					continue
				}
			}
			filtered = append(filtered, conflict)
		}
		impacts[i].Conflicts = filtered
		if change.Schedule.TechnicianID == "" {
			continue
		}
		input, inputErr := s.repository.LoadConflictInput(ctx, principal, change.Schedule)
		if inputErr != nil {
			return nil, inputErr
		}
		constraints := []ConflictConstraint{}
		for j, other := range changes {
			if i == j || other.Schedule.TechnicianID == "" || other.Schedule.TechnicianID != change.Schedule.TechnicianID {
				continue
			}
			if overlap := intersectInterval(change.Schedule.Interval, other.Schedule.Interval); overlap.valid() {
				constraints = append(constraints, ConflictConstraint{Kind: ConflictOrdinaryOverbooking, Interval: overlap, Related: SafeSourceRef{Type: projections[j].Source.Type, ID: projections[j].Source.ID}})
			}
		}
		input.Proposed = change.Schedule
		input.Constraints = constraints
		input.Dependencies = nil
		impacts[i].Conflicts = append(impacts[i].Conflicts, EvaluateConflicts(input)...)
	}
	if err = s.applyCombinedCapacity(ctx, principal, changes, projections, impacts); err != nil {
		return nil, err
	}
	finalBindings, err := s.combinedScopeBindings(ctx, principal, changes)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(initialBindings, finalBindings) {
		return nil, ErrStaleProposal
	}
	return impacts, nil
}

func (s *SchedulingImpactService) combinedScopeBindings(ctx context.Context, principal authorization.Principal, changes []ProposedChange) ([]RevisionBinding, error) {
	provider, ok := s.repository.(SchedulingBindingProvider)
	if !ok {
		return nil, nil
	}
	bindings := []RevisionBinding{}
	for _, change := range changes {
		found, err := provider.SchedulingRevisionBindings(ctx, principal, change.Schedule)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, found...)
	}
	return normalizeBindings(bindings), nil
}

func (s *SchedulingImpactService) applyCombinedCapacity(ctx context.Context, principal authorization.Principal, changes []ProposedChange, projections []Projection, impacts []SchedulingImpact) error {
	type technicianOverlay struct {
		window  QueryWindow
		indexes []int
	}
	byTechnician := map[string]*technicianOverlay{}
	for i, change := range changes {
		impacts[i].Capacity = nil
		technicianID := change.Schedule.TechnicianID
		if technicianID == "" {
			continue
		}
		overlay := byTechnician[technicianID]
		if overlay == nil {
			overlay = &technicianOverlay{window: QueryWindow{Start: change.Schedule.Interval.Start, End: change.Schedule.Interval.End}}
			byTechnician[technicianID] = overlay
		}
		if change.Schedule.Interval.Start.Before(overlay.window.Start) {
			overlay.window.Start = change.Schedule.Interval.Start
		}
		if change.Schedule.Interval.End.After(overlay.window.End) {
			overlay.window.End = change.Schedule.Interval.End
		}
		overlay.indexes = append(overlay.indexes, i)
	}
	for technicianID, overlay := range byTechnician {
		inputs, err := s.repository.LoadCapacityInputs(ctx, principal, overlay.window, []string{technicianID})
		if err != nil {
			return err
		}
		current := inputs[technicianID]
		before := CalculateCapacity(current)
		movingProjectionIDs := map[string]struct{}{}
		for _, index := range overlay.indexes {
			movingProjectionIDs[projections[index].ID] = struct{}{}
		}
		events := make([]CapacityEvent, 0, len(current.Events)+len(overlay.indexes))
		for _, event := range current.Events {
			moving := false
			for projectionID := range movingProjectionIDs {
				if event.ID == projectionID || strings.HasPrefix(event.ID, projectionID+":") {
					moving = true
					break
				}
			}
			if !moving {
				events = append(events, event)
			}
		}
		for _, index := range overlay.indexes {
			projection, change := projections[index], changes[index]
			planned := projection.PlannedMinutes
			if projection.SchedulingMode == FixedBlock || planned < 1 {
				planned = int64(change.Schedule.Interval.End.Sub(change.Schedule.Interval.Start) / time.Minute)
			}
			events = append(events, CapacityEvent{ID: projection.ID, ClientID: projection.Source.ClientID, Assigned: true, Mode: projection.SchedulingMode, PlannedMinutes: planned, Interval: change.Schedule.Interval, Source: SafeSourceRef{Type: projection.Source.Type, ID: projection.Source.ID}})
		}
		current.Events = events
		after := CalculateCapacity(current)
		impact := CapacityImpact{TechnicianID: technicianID, DeltaMinutes: after.CommittedMinutes - before.CommittedMinutes, AvailableMinutes: after.AvailableMinutes, CommittedMinutes: after.CommittedMinutes, OverbookedMinutes: after.OverbookedMinutes}
		impacts[overlay.indexes[0]].Capacity = []CapacityImpact{impact}
	}
	return nil
}

func scheduleIntervalOccurrenceKey(interval ScheduleInterval, projection Projection) string {
	if projection.AllDay && interval.StartsOn != nil {
		return interval.StartsOn.Format("2006-01-02")
	}
	if interval.StartsAt == nil {
		return ""
	}
	location, err := time.LoadLocation(projection.Timezone)
	if err != nil {
		location = time.UTC
	}
	return interval.StartsAt.In(location).Format("2006-01-02T15:04:05")
}

type ScheduleAtomicRepository interface {
	ApplySchedulingProposalAtomic(context.Context, ScheduleApplyRequest, func(context.Context, ScheduleTx) error) (AppliedProposal, error)
}
type ScheduleUnitOfWork struct {
	repository ScheduleAtomicRepository
	adapters   *WriteAdapterRegistry
	now        func() time.Time
	newID      func() string
}

func NewScheduleUnitOfWork(repository ScheduleAtomicRepository, adapters *WriteAdapterRegistry, now func() time.Time, newID func() string) *ScheduleUnitOfWork {
	return &ScheduleUnitOfWork{repository, adapters, now, newID}
}
func (u *ScheduleUnitOfWork) ApplyAtomic(ctx context.Context, request ScheduleApplyRequest) (AppliedProposal, error) {
	if u == nil || u.repository == nil || u.adapters == nil || u.now == nil || u.newID == nil || request.Proposal.ID == "" || request.Principal.ID != request.Proposal.ActorID {
		return AppliedProposal{}, ErrInvalidProposal
	}
	policies := append([]string(nil), request.OverridePolicyIDs...)
	for _, change := range request.Changes {
		if err := validateAcceptedSchedulingImpact(change, request.OverrideReason); err != nil {
			return AppliedProposal{}, err
		}
		for _, c := range change.Conflicts {
			if c.Severity == ConflictOverrideable && c.PolicyID != "" {
				policies = append(policies, c.PolicyID)
			}
		}
	}
	normalizedPolicies := policies[:0]
	for _, policyID := range policies {
		if policyID = strings.TrimSpace(policyID); policyID != "" {
			normalizedPolicies = append(normalizedPolicies, policyID)
		}
	}
	policies = normalizedPolicies
	sort.Strings(policies)
	policies = compactStrings(policies)
	if len(policies) > 0 && strings.TrimSpace(request.OverrideReason) == "" {
		return AppliedProposal{}, ErrOverrideReasonRequired
	}
	request.OverridePolicyIDs = append([]string(nil), policies...)
	if request.EvaluatedAt.IsZero() {
		request.EvaluatedAt = u.now().UTC()
	} else {
		request.EvaluatedAt = request.EvaluatedAt.UTC()
	}
	evidence := mutation.Evidence{ActorID: request.Principal.ID, Reason: strings.TrimSpace(request.OverrideReason), Source: "calendar", CorrelationID: u.newID(), OverriddenPolicyIDs: policies, OccurredAt: request.EvaluatedAt}
	return u.repository.ApplySchedulingProposalAtomic(ctx, request, func(txctx context.Context, tx ScheduleTx) error {
		for _, change := range request.Changes {
			adapter, ok := u.adapters.ForSource(change.Prepared.Source.Type)
			if !ok {
				return ErrWriteAdapterNotFound
			}
			if err := adapter.Apply(txctx, tx, change.Prepared, evidence); err != nil {
				return err
			}
		}
		return nil
	})
}
func compactStrings(v []string) []string {
	out := v[:0]
	for _, s := range v {
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}

var _ SchedulingUnitOfWork = (*ScheduleUnitOfWork)(nil)
var _ = errors.Is

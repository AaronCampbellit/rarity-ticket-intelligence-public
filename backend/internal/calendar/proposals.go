package calendar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrInvalidProposal          = errors.New("invalid scheduling proposal")
	ErrStaleProposal            = errors.New("stale scheduling proposal")
	ErrExpiredProposal          = errors.New("expired scheduling proposal")
	ErrProposalActorMismatch    = errors.New("scheduling proposal actor mismatch")
	ErrOccurrenceScopeRequired  = errors.New("recurring change requires occurrence scope")
	ErrOverrideReasonRequired   = errors.New("scheduling override reason required")
	ErrHardSchedulingConflict   = errors.New("hard scheduling conflict")
	ErrBlockedSchedulingCascade = errors.New("blocked scheduling dependency cascade")
	ErrInvalidOptionalChange    = errors.New("invalid optional scheduling change")
)

type RevisionBindingKind string

const (
	RevisionSource          RevisionBindingKind = "source"
	RevisionSchedule        RevisionBindingKind = "schedule"
	RevisionDependency      RevisionBindingKind = "dependency"
	RevisionPolicy          RevisionBindingKind = "policy"
	RevisionDependencyScope RevisionBindingKind = "dependency_scope"
	RevisionPolicyScope     RevisionBindingKind = "policy_scope"
)

type RevisionBinding struct {
	Kind    RevisionBindingKind `json:"kind"`
	ID      string              `json:"id"`
	Version int64               `json:"version"`
}
type CapacityImpact struct {
	TechnicianID      string `json:"technician_id"`
	DeltaMinutes      int64  `json:"delta_minutes"`
	AvailableMinutes  int64  `json:"available_minutes"`
	CommittedMinutes  int64  `json:"committed_minutes"`
	OverbookedMinutes int64  `json:"overbooked_minutes"`
}
type HealthImpact struct {
	ProjectionID string         `json:"projection_id"`
	State        HealthState    `json:"state"`
	Reasons      []HealthReason `json:"reasons,omitempty"`
}
type NotificationImpact struct {
	RecipientID  string `json:"recipient_id"`
	ReasonCode   string `json:"reason_code"`
	ProjectionID string `json:"projection_id"`
}

type ProposedChange struct {
	ID             string                 `json:"id"`
	Required       bool                   `json:"required"`
	Requested      RequestedChange        `json:"requested"`
	Prepared       PreparedChange         `json:"-"`
	Schedule       ProposedSchedule       `json:"schedule"`
	Conflicts      []Conflict             `json:"conflicts,omitempty"`
	BlockedSources []CascadeBlockedSource `json:"blocked_sources,omitempty"`
}

type SchedulingImpact struct {
	Cascade        []RequestedChange
	Conflicts      []Conflict
	BlockedSources []CascadeBlockedSource
	Capacity       []CapacityImpact
	Health         []HealthImpact
	Notifications  []NotificationImpact
	Bindings       []RevisionBinding
}

type SchedulingProposal struct {
	ID                string                 `json:"id"`
	ActorID           string                 `json:"-"`
	MSPID             string                 `json:"-"`
	ClientID          string                 `json:"client_id,omitempty"`
	AuthorizationHash string                 `json:"-"`
	CreatedAt         time.Time              `json:"created_at"`
	ExpiresAt         time.Time              `json:"expires_at"`
	Changes           []ProposedChange       `json:"changes"`
	Conflicts         []Conflict             `json:"conflicts"`
	BlockedSources    []CascadeBlockedSource `json:"blocked_sources"`
	Capacity          []CapacityImpact       `json:"capacity"`
	Health            []HealthImpact         `json:"health"`
	Notifications     []NotificationImpact   `json:"notifications"`
	Bindings          []RevisionBinding      `json:"-"`
	RequiresReason    bool                   `json:"requires_reason"`
	State             string                 `json:"state"`
	Version           int64                  `json:"version"`
}

func (p SchedulingProposal) OptionalChangeIDs() []string {
	result := []string{}
	for _, change := range p.Changes {
		if !change.Required {
			result = append(result, change.ID)
		}
	}
	sort.Strings(result)
	return result
}

type PreviewCommand struct {
	Principal     authorization.Principal
	PrimaryChange RequestedChange
}
type ApplyCommand struct {
	Principal                 authorization.Principal
	ProposalID                string
	AcceptedOptionalChangeIDs []string
	OverrideReason            string
	ExpectedProposalVersion   int64
}
type AppliedProposal struct {
	ProposalID    string           `json:"proposal_id"`
	CorrelationID string           `json:"correlation_id"`
	AppliedAt     time.Time        `json:"applied_at"`
	Changes       []ProposedChange `json:"changes"`
}

type SchedulingProjectionResolver interface {
	ResolveSchedulingProjection(context.Context, authorization.Principal, string, string) (Projection, error)
}
type SchedulingWorkforceAuthorizer interface {
	AuthorizeScheduling(context.Context, authorization.Principal, ProposedSchedule, time.Time) error
}
type SchedulingImpactPreviewer interface {
	PreviewSchedulingImpact(context.Context, authorization.Principal, ProposedChange) (SchedulingImpact, error)
}
type CombinedSchedulingImpactPreviewer interface {
	PreviewCombinedSchedulingImpact(context.Context, authorization.Principal, []ProposedChange) ([]SchedulingImpact, error)
}
type SchedulingProposalStore interface {
	SaveSchedulingProposal(context.Context, SchedulingProposal) error
	LoadSchedulingProposal(context.Context, string, string) (SchedulingProposal, error)
}
type SchedulingRevisionValidator interface {
	ValidateSchedulingBindings(context.Context, authorization.Principal, []RevisionBinding) error
}
type SchedulingUnitOfWork interface {
	ApplyAtomic(context.Context, ScheduleApplyRequest) (AppliedProposal, error)
}

type ProposalServiceDependencies struct {
	Projections       SchedulingProjectionResolver
	Workforce         SchedulingWorkforceAuthorizer
	Impacts           SchedulingImpactPreviewer
	Store             SchedulingProposalStore
	UnitOfWork        SchedulingUnitOfWork
	RevisionValidator SchedulingRevisionValidator
	Adapters          *WriteAdapterRegistry
	Roles             *RoleRegistry
	Now               func() time.Time
	NewID             func() string
	TTL               time.Duration
}
type ProposalService struct{ dependencies ProposalServiceDependencies }

func NewProposalService(d ProposalServiceDependencies) *ProposalService {
	if d.TTL <= 0 {
		d.TTL = 5 * time.Minute
	}
	if d.Roles == nil {
		d.Roles, _ = NewProductionRoleRegistry(nil)
	}
	return &ProposalService{dependencies: d}
}

func (s *ProposalService) Preview(ctx context.Context, command PreviewCommand) (SchedulingProposal, error) {
	var result SchedulingProposal
	d := s.dependencies
	if s == nil || d.Projections == nil || d.Workforce == nil || d.Impacts == nil || d.Store == nil || d.UnitOfWork == nil || d.Adapters == nil || d.Roles == nil || d.Now == nil || d.NewID == nil || strings.TrimSpace(command.Principal.ID) == "" || strings.TrimSpace(command.PrimaryChange.ProjectionID) == "" {
		return result, ErrInvalidProposal
	}
	evaluatedAt := d.Now().UTC()
	if err := authorization.AuthorizeAt(command.Principal, "calendar.schedule", scope.Target{MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID}, evaluatedAt); err != nil {
		return result, err
	}
	primary, err := s.prepare(ctx, command.Principal, command.PrimaryChange, true, evaluatedAt)
	if err != nil {
		return result, err
	}
	impact, err := d.Impacts.PreviewSchedulingImpact(ctx, command.Principal, primary)
	if err != nil {
		return result, err
	}
	primary.Conflicts = append([]Conflict(nil), impact.Conflicts...)
	primary.BlockedSources = append([]CascadeBlockedSource(nil), impact.BlockedSources...)
	changes := []ProposedChange{primary}
	impacts := []SchedulingImpact{impact}
	for _, requested := range impact.Cascade {
		prepared, prepareErr := s.prepare(ctx, command.Principal, requested, requested.Required, evaluatedAt)
		if prepareErr != nil {
			return result, prepareErr
		}
		cascadeImpact, impactErr := d.Impacts.PreviewSchedulingImpact(ctx, command.Principal, prepared)
		if impactErr != nil {
			return result, impactErr
		}
		prepared.Conflicts = append([]Conflict(nil), cascadeImpact.Conflicts...)
		prepared.BlockedSources = append([]CascadeBlockedSource(nil), cascadeImpact.BlockedSources...)
		changes = append(changes, prepared)
		impacts = append(impacts, cascadeImpact)
	}
	if combined, ok := d.Impacts.(CombinedSchedulingImpactPreviewer); ok {
		combinedImpacts, combinedErr := combined.PreviewCombinedSchedulingImpact(ctx, command.Principal, changes)
		if combinedErr != nil {
			return result, combinedErr
		}
		if len(combinedImpacts) != len(changes) {
			return result, ErrInvalidProposal
		}
		// The combined preview re-reads the dependency graph. If that graph
		// changed after the initial cascade was discovered, the prepared change
		// set is no longer a coherent representation of the proposed cascade.
		if !reflect.DeepEqual(impact.Cascade, combinedImpacts[0].Cascade) {
			return result, ErrStaleProposal
		}
		impacts = combinedImpacts
		for i := range changes {
			changes[i].Conflicts = append([]Conflict(nil), impacts[i].Conflicts...)
			changes[i].BlockedSources = append([]CascadeBlockedSource(nil), impacts[i].BlockedSources...)
		}
	}
	for i := range changes {
		changes[i].ID = d.NewID()
		changes[i].Requested.ID = changes[i].ID
		changes[i].Prepared.ID = changes[i].ID
	}
	bindings := []RevisionBinding{}
	conflicts := []Conflict{}
	blockedSources := []CascadeBlockedSource{}
	capacity := []CapacityImpact{}
	health := []HealthImpact{}
	notifications := []NotificationImpact{}
	for _, evaluated := range impacts {
		bindings = append(bindings, evaluated.Bindings...)
		conflicts = append(conflicts, evaluated.Conflicts...)
		blockedSources = append(blockedSources, evaluated.BlockedSources...)
		capacity = append(capacity, evaluated.Capacity...)
		health = append(health, evaluated.Health...)
		notifications = append(notifications, evaluated.Notifications...)
	}
	for _, change := range changes {
		bindings = append(bindings, RevisionBinding{Kind: RevisionSource, ID: bindingSourceID(change.Prepared.Source), Version: change.Prepared.ExpectedSourceRevision})
	}
	bindings = normalizeBindings(bindings)
	now := evaluatedAt
	result = SchedulingProposal{ID: d.NewID(), ActorID: command.Principal.ID, MSPID: command.Principal.Scope.MSPID, ClientID: commonClient(changes), AuthorizationHash: AuthorizationFingerprint(command.Principal), CreatedAt: now, ExpiresAt: now.Add(d.TTL), Changes: changes, Conflicts: conflicts, BlockedSources: blockedSources, Capacity: capacity, Health: health, Notifications: notifications, Bindings: bindings, State: "previewed", Version: 1}
	for _, conflict := range result.Conflicts {
		if conflict.Severity == ConflictOverrideable || conflict.ReasonRequired {
			result.RequiresReason = true
		}
	}
	if err = d.Store.SaveSchedulingProposal(ctx, result); err != nil {
		return SchedulingProposal{}, err
	}
	return result, nil
}

func (s *ProposalService) prepare(ctx context.Context, principal authorization.Principal, requested RequestedChange, required bool, evaluatedAt time.Time) (ProposedChange, error) {
	p, err := s.dependencies.Projections.ResolveSchedulingProjection(ctx, principal, requested.ProjectionID, requested.OccurrenceKey)
	if err != nil {
		return ProposedChange{}, err
	}
	if err = authorization.AuthorizeAt(principal, "calendar.schedule", p.Source.ScopeTarget(), evaluatedAt); err != nil {
		return ProposedChange{}, err
	}
	definition, ok := s.dependencies.Roles.DefinitionForProjection(p)
	if !ok {
		return ProposedChange{}, ErrInvalidScheduleChange
	}
	if definition.ReadOnly {
		return ProposedChange{}, ErrReadOnlyEventRole
	}
	if p.Recurrence != nil {
		if requested.OccurrenceScope == "" {
			return ProposedChange{}, ErrOccurrenceScopeRequired
		}
		if requested.OccurrenceScope != ThisOccurrence && requested.OccurrenceScope != ThisAndFuture && requested.OccurrenceScope != EntireSeries {
			return ProposedChange{}, ErrInvalidScheduleChange
		}
		if requested.OccurrenceScope != EntireSeries && strings.TrimSpace(requested.OccurrenceKey) == "" {
			return ProposedChange{}, ErrOccurrenceScopeRequired
		}
	} else if requested.OccurrenceScope != "" && requested.OccurrenceScope != EntireSeries {
		return ProposedChange{}, ErrInvalidScheduleChange
	}
	requested.Source, requested.EventRole, requested.SourceRoleKey, requested.SourceRevision = p.Source, p.EventRole, p.SourceRoleKey, p.SourceRevision
	if requested.Recurrence == nil {
		requested.Recurrence = p.Recurrence
	}
	requested.Required = required
	if requested.StartsOn != nil {
		requested.AllDay = true
	}
	if requested.StartsAt != nil && requested.Timezone == "" {
		requested.Timezone = p.Timezone
	}
	if err = requested.interval().Validate(false); err != nil || requested.interval().Empty() {
		return ProposedChange{}, ErrInvalidScheduleChange
	}
	adapter, ok := s.dependencies.Adapters.ForSource(p.Source.Type)
	if !ok {
		return ProposedChange{}, ErrWriteAdapterNotFound
	}
	prepared, err := adapter.Prepare(ctx, principal, requested)
	if err != nil {
		return ProposedChange{}, err
	}
	schedule, err := requestedSchedule(p, requested)
	if err != nil {
		return ProposedChange{}, err
	}
	if err = s.dependencies.Workforce.AuthorizeScheduling(ctx, principal, schedule, evaluatedAt); err != nil {
		return ProposedChange{}, err
	}
	return ProposedChange{Required: required, Requested: requested, Prepared: prepared, Schedule: schedule}, nil
}

func (s *ProposalService) Apply(ctx context.Context, command ApplyCommand) (AppliedProposal, error) {
	var result AppliedProposal
	if s == nil || s.dependencies.Store == nil || s.dependencies.UnitOfWork == nil || s.dependencies.Now == nil || strings.TrimSpace(command.ProposalID) == "" {
		return result, ErrInvalidProposal
	}
	p, err := s.dependencies.Store.LoadSchedulingProposal(ctx, command.Principal.Scope.MSPID, command.ProposalID)
	if err != nil {
		return result, err
	}
	if command.ExpectedProposalVersion > 0 && p.Version != command.ExpectedProposalVersion {
		return result, ErrStaleProposal
	}
	if p.ActorID != command.Principal.ID {
		return result, ErrProposalActorMismatch
	}
	if p.State != "previewed" {
		return result, ErrStaleProposal
	}
	evaluatedAt := s.dependencies.Now().UTC()
	if !evaluatedAt.Before(p.ExpiresAt) {
		return result, ErrExpiredProposal
	}
	if p.AuthorizationHash != AuthorizationFingerprint(command.Principal) {
		return result, ErrStaleProposal
	}
	accepted, err := acceptedChanges(p, command.AcceptedOptionalChangeIDs)
	if err != nil {
		return result, err
	}
	freshOverridePolicyIDs := []string{}
	if combined, ok := s.dependencies.Impacts.(CombinedSchedulingImpactPreviewer); ok {
		freshImpacts, impactErr := combined.PreviewCombinedSchedulingImpact(ctx, command.Principal, accepted)
		if impactErr != nil {
			return result, impactErr
		}
		if len(freshImpacts) != len(accepted) {
			return result, ErrInvalidProposal
		}
		for i := range accepted {
			fresh := accepted[i]
			fresh.Conflicts = freshImpacts[i].Conflicts
			fresh.BlockedSources = freshImpacts[i].BlockedSources
			if err = validateAcceptedSchedulingImpact(fresh, command.OverrideReason); err != nil {
				return result, err
			}
			for _, conflict := range fresh.Conflicts {
				if conflict.Severity == ConflictOverrideable && strings.TrimSpace(conflict.PolicyID) != "" {
					freshOverridePolicyIDs = append(freshOverridePolicyIDs, strings.TrimSpace(conflict.PolicyID))
				}
			}
		}
	}
	for _, change := range accepted {
		if err = validateAcceptedSchedulingImpact(change, command.OverrideReason); err != nil {
			return result, err
		}
	}
	for i := range accepted {
		fresh, resolveErr := s.dependencies.Projections.ResolveSchedulingProjection(ctx, command.Principal, accepted[i].Requested.ProjectionID, accepted[i].Requested.OccurrenceKey)
		if resolveErr != nil || fresh.SourceRevision != accepted[i].Requested.SourceRevision || fresh.Source != accepted[i].Requested.Source || fresh.EventRole != accepted[i].Requested.EventRole || fresh.SourceRoleKey != accepted[i].Requested.SourceRoleKey {
			return result, ErrStaleProposal
		}
		if authErr := authorization.AuthorizeAt(command.Principal, "calendar.schedule", fresh.Source.ScopeTarget(), evaluatedAt); authErr != nil {
			return result, authErr
		}
		if authErr := s.dependencies.Workforce.AuthorizeScheduling(ctx, command.Principal, accepted[i].Schedule, evaluatedAt); authErr != nil {
			return result, authErr
		}
		adapter, ok := s.dependencies.Adapters.ForSource(fresh.Source.Type)
		if !ok {
			return result, ErrWriteAdapterNotFound
		}
		prepared, prepErr := adapter.Prepare(ctx, command.Principal, accepted[i].Requested)
		if prepErr != nil {
			return result, prepErr
		}
		if prepared.ExpectedSourceRevision != fresh.SourceRevision || prepared.Source != fresh.Source || prepared.EventRole != fresh.EventRole {
			return result, ErrStaleProposal
		}
		prepared.ID = accepted[i].ID
		accepted[i].Prepared = prepared
	}
	if s.dependencies.RevisionValidator != nil {
		if err = s.dependencies.RevisionValidator.ValidateSchedulingBindings(ctx, command.Principal, p.Bindings); err != nil {
			if errors.Is(err, ErrStaleProposal) {
				return result, err
			}
			return result, fmt.Errorf("%w: %v", ErrStaleProposal, err)
		}
	}
	return s.dependencies.UnitOfWork.ApplyAtomic(ctx, ScheduleApplyRequest{Principal: command.Principal, Proposal: p, Changes: accepted, OverrideReason: strings.TrimSpace(command.OverrideReason), OverridePolicyIDs: freshOverridePolicyIDs, EvaluatedAt: evaluatedAt})
}

func validateAcceptedSchedulingImpact(change ProposedChange, overrideReason string) error {
	for _, conflict := range change.Conflicts {
		if conflict.Severity == ConflictHard {
			return ErrHardSchedulingConflict
		}
		if (conflict.Severity == ConflictOverrideable || conflict.ReasonRequired) && strings.TrimSpace(overrideReason) == "" {
			return ErrOverrideReasonRequired
		}
	}
	if len(change.BlockedSources) > 0 {
		return ErrBlockedSchedulingCascade
	}
	return nil
}

func acceptedChanges(p SchedulingProposal, ids []string) ([]ProposedChange, error) {
	requested := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, ErrInvalidOptionalChange
		}
		requested[id] = struct{}{}
	}
	result := []ProposedChange{}
	for _, change := range p.Changes {
		if change.Required {
			result = append(result, change)
			continue
		}
		if _, ok := requested[change.ID]; ok {
			result = append(result, change)
			delete(requested, change.ID)
		}
	}
	if len(requested) != 0 {
		return nil, ErrInvalidOptionalChange
	}
	return result, nil
}

func requestedSchedule(p Projection, r RequestedChange) (ProposedSchedule, error) {
	interval := TimeInterval{}
	if r.AllDay {
		if r.StartsOn == nil {
			return ProposedSchedule{}, ErrInvalidScheduleChange
		}
		interval.Start = *r.StartsOn
		interval.End = r.StartsOn.AddDate(0, 0, 1)
		if r.EndsOn != nil {
			interval.End = r.EndsOn.AddDate(0, 0, 1)
		}
	} else {
		if r.StartsAt == nil {
			return ProposedSchedule{}, ErrInvalidScheduleChange
		}
		interval.Start = *r.StartsAt
		interval.End = *r.StartsAt
		if r.EndsAt != nil {
			interval.End = *r.EndsAt
		}
	}
	if !interval.valid() {
		return ProposedSchedule{}, ErrInvalidScheduleChange
	}
	return ProposedSchedule{Interval: interval, Source: SafeSourceRef{Type: p.Source.Type, ID: p.Source.ID}, ProjectionID: p.ID, ClientID: p.Source.ClientID, ServiceID: first(p.Dimensions.TechnologyIDs), AssetID: first(p.Dimensions.AssetIDs), TeamID: first(p.Dimensions.TeamIDs), TechnicianID: p.AssigneeID}, nil
}
func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}
func commonClient(changes []ProposedChange) string {
	if len(changes) == 0 {
		return ""
	}
	v := changes[0].Prepared.Source.ClientID
	for _, c := range changes[1:] {
		if c.Prepared.Source.ClientID != v {
			return ""
		}
	}
	return v
}
func bindingSourceID(s SourceRef) string {
	return s.MSPID + "/" + s.ClientID + "/" + s.Type + "/" + s.ID
}
func normalizeBindings(in []RevisionBinding) []RevisionBinding {
	by := map[string]RevisionBinding{}
	for _, b := range in {
		if b.Version < 0 || strings.TrimSpace(b.ID) == "" || b.Version == 0 && b.Kind != RevisionSchedule && b.Kind != RevisionDependencyScope && b.Kind != RevisionPolicyScope {
			continue
		}
		k := string(b.Kind) + "\x00" + b.ID
		if old, ok := by[k]; !ok || b.Version > old.Version {
			by[k] = b
		}
	}
	out := make([]RevisionBinding, 0, len(by))
	for _, b := range by {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].ID < out[j].ID
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func AuthorizationFingerprint(p authorization.Principal) string {
	type fingerprint struct {
		ID, MSPID, ClientID      string
		Capabilities, DataScopes []string
		BreakGlass               any
	}
	caps := make([]string, 0, len(p.Capabilities))
	for c := range p.Capabilities {
		caps = append(caps, c)
	}
	sort.Strings(caps)
	data := make([]string, 0, len(p.DataScopes))
	for c := range p.DataScopes {
		data = append(data, c)
	}
	sort.Strings(data)
	body, _ := json.Marshal(fingerprint{p.ID, p.Scope.MSPID, p.Scope.ClientID, caps, data, p.BreakGlass})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

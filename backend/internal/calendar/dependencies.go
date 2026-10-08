package calendar

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
)

var (
	ErrInvalidDependency     = errors.New("invalid calendar dependency")
	ErrCrossClientDependency = errors.New("calendar dependencies must remain within one client")
	ErrDependencyCycle       = errors.New("calendar dependency would create a cycle")
	ErrSelfDependency        = errors.New("calendar event cannot depend on itself")
	ErrDuplicateDependency   = errors.New("calendar dependency already exists")
	ErrDependencyIneligible  = errors.New("calendar event role is not dependency eligible")
	ErrLeadLagOutOfRange     = errors.New("calendar dependency lead or lag exceeds policy")
	ErrLeadLagPrecision      = errors.New("all-day calendar dependencies require day-aligned lead or lag")
)

type DependencyType string

const (
	FinishToStart  DependencyType = "finish_to_start"
	StartToStart   DependencyType = "start_to_start"
	FinishToFinish DependencyType = "finish_to_finish"
)

const defaultMaximumLeadLagMinutes = 365 * 24 * 60

type Dependency struct {
	ID             string         `json:"id"`
	MSPID          string         `json:"-"`
	ClientID       string         `json:"client_id"`
	PredecessorID  string         `json:"predecessor_id"`
	SuccessorID    string         `json:"successor_id"`
	Type           DependencyType `json:"type"`
	LeadLagMinutes int            `json:"lead_lag_minutes"`
	Version        int64          `json:"version"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	CreatedBy      string         `json:"created_by"`
	UpdatedBy      string         `json:"updated_by"`
}

type CreateDependencyCommand struct {
	Principal                  authorization.Principal
	PredecessorID, SuccessorID string
	Type                       DependencyType
	LeadLagMinutes             int
}

type DeleteDependencyCommand struct {
	Principal       authorization.Principal
	DependencyID    string
	ExpectedVersion int64
}

type DependencyChangeFact struct {
	EventID    string
	ActorID    string
	OccurredAt time.Time
}

type DependencyDeletionFact = DependencyChangeFact

type ScheduleInterval struct {
	StartsOn *time.Time `json:"starts_on,omitempty"`
	EndsOn   *time.Time `json:"ends_on,omitempty"`
	StartsAt *time.Time `json:"starts_at,omitempty"`
	EndsAt   *time.Time `json:"ends_at,omitempty"`
}

type CascadeMove struct {
	ProjectionID string           `json:"projection_id"`
	DependencyID string           `json:"dependency_id"`
	From         ScheduleInterval `json:"from"`
	To           ScheduleInterval `json:"to"`
	ReasonCode   string           `json:"reason_code"`
}

type CascadeBlockedSource struct {
	ProjectionID string    `json:"projection_id"`
	Source       SourceRef `json:"source"`
	ReasonCode   string    `json:"reason_code"`
}

type CascadeImpact struct {
	RequiredMoves             []CascadeMove          `json:"required_moves"`
	OptionalMoves             []CascadeMove          `json:"optional_moves"`
	BlockedSources            []CascadeBlockedSource `json:"blocked_sources"`
	AffectedHealth            []string               `json:"affected_health"`
	CapacityRecalculationKeys []string               `json:"capacity_recalculation_keys"`
}

type DependencyPreview struct {
	Dependency Dependency    `json:"dependency"`
	Impact     CascadeImpact `json:"impact"`
}

type DependencyRepository interface {
	LoadDependencyProjection(context.Context, string) (Projection, error)
	ListDependencies(context.Context, string, string) ([]Dependency, error)
	InsertDependency(context.Context, Dependency, DependencyChangeFact) (Dependency, error)
	LoadDependency(context.Context, string, string) (Dependency, error)
	DeleteDependency(context.Context, Dependency, DependencyDeletionFact) error
}

type DependencyOption func(*DependencyService)

func WithMaximumLeadLagMinutes(minutes int) DependencyOption {
	return func(service *DependencyService) { service.maximumLeadLagMinutes = minutes }
}

func WithDependencyRoleRegistry(registry *RoleRegistry) DependencyOption {
	return func(service *DependencyService) { service.roles = registry }
}

type DependencyService struct {
	repository            DependencyRepository
	now                   func() time.Time
	newID                 func() string
	maximumLeadLagMinutes int
	roles                 *RoleRegistry
}

func NewDependencyService(repository DependencyRepository, now func() time.Time, newID func() string, options ...DependencyOption) *DependencyService {
	roles, _ := NewProductionRoleRegistry(nil)
	service := &DependencyService{repository: repository, now: now, newID: newID, maximumLeadLagMinutes: defaultMaximumLeadLagMinutes, roles: roles}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

func (s *DependencyService) Preview(ctx context.Context, command CreateDependencyCommand) (DependencyPreview, error) {
	var preview DependencyPreview
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || s.roles == nil || s.maximumLeadLagMinutes < 0 || strings.TrimSpace(command.PredecessorID) == "" || strings.TrimSpace(command.SuccessorID) == "" || !validDependencyType(command.Type) {
		return preview, ErrInvalidDependency
	}
	if command.PredecessorID == command.SuccessorID {
		return preview, ErrSelfDependency
	}
	if command.LeadLagMinutes > s.maximumLeadLagMinutes || command.LeadLagMinutes < -s.maximumLeadLagMinutes {
		return preview, ErrLeadLagOutOfRange
	}
	predecessor, err := s.repository.LoadDependencyProjection(ctx, command.PredecessorID)
	if err != nil {
		return preview, err
	}
	successor, err := s.repository.LoadDependencyProjection(ctx, command.SuccessorID)
	if err != nil {
		return preview, err
	}
	if predecessor.Source.MSPID == "" || predecessor.Source.MSPID != successor.Source.MSPID || predecessor.Source.ClientID == "" || predecessor.Source.ClientID != successor.Source.ClientID {
		return preview, ErrCrossClientDependency
	}
	if predecessor.AllDay != successor.AllDay || predecessor.AllDay && command.LeadLagMinutes%(24*60) != 0 {
		return preview, ErrLeadLagPrecision
	}
	if !dependencyEligible(s.roles, predecessor) || !dependencyEligible(s.roles, successor) {
		return preview, ErrDependencyIneligible
	}
	evaluatedAt := s.now()
	for _, projection := range []Projection{predecessor, successor} {
		if err = authorizeDependencyProjection(command.Principal, projection, evaluatedAt); err != nil {
			return preview, err
		}
	}
	edges, err := s.repository.ListDependencies(ctx, predecessor.Source.MSPID, predecessor.Source.ClientID)
	if err != nil {
		return preview, err
	}
	for _, edge := range edges {
		if edge.PredecessorID == command.PredecessorID && edge.SuccessorID == command.SuccessorID && edge.Type == command.Type {
			return preview, ErrDuplicateDependency
		}
	}
	prospective := Dependency{
		ID: s.newID(), MSPID: predecessor.Source.MSPID, ClientID: predecessor.Source.ClientID,
		PredecessorID: command.PredecessorID, SuccessorID: command.SuccessorID,
		Type: command.Type, LeadLagMinutes: command.LeadLagMinutes, Version: 1,
		CreatedAt: evaluatedAt, UpdatedAt: evaluatedAt, CreatedBy: command.Principal.ID, UpdatedBy: command.Principal.ID,
	}
	if strings.TrimSpace(prospective.ID) == "" || strings.TrimSpace(command.Principal.ID) == "" {
		return preview, ErrInvalidDependency
	}
	if dependencyCreatesCycle(append(edges, prospective), prospective.PredecessorID, prospective.SuccessorID) {
		return preview, ErrDependencyCycle
	}
	if predecessor.ID != command.PredecessorID || successor.ID != command.SuccessorID {
		return preview, ErrInvalidDependency
	}
	prospectiveEdges := append(edges, prospective)
	projections := map[string]Projection{predecessor.ID: predecessor, successor.ID: successor}
	for _, id := range reachableDependencyIDs(prospectiveEdges, predecessor.ID) {
		if _, ok := projections[id]; ok {
			continue
		}
		projection, loadErr := s.repository.LoadDependencyProjection(ctx, id)
		if loadErr != nil {
			return preview, loadErr
		}
		if projection.ID != id || projection.Source.MSPID != predecessor.Source.MSPID || projection.Source.ClientID != predecessor.Source.ClientID {
			return preview, ErrCrossClientDependency
		}
		if loadErr = authorizeDependencyProjection(command.Principal, projection, evaluatedAt); loadErr != nil {
			return preview, loadErr
		}
		projections[id] = projection
	}
	interval, ok := projectionInterval(predecessor)
	if !ok {
		return preview, fmt.Errorf("%w: predecessor has no scheduling interval", ErrInvalidDependency)
	}
	impact, err := BuildCascadeImpactWithRegistry(s.roles, predecessor.ID, interval, projections, prospectiveEdges)
	if err != nil {
		return preview, err
	}
	preview.Dependency, preview.Impact = prospective, impact
	return preview, nil
}

func (s *DependencyService) Create(ctx context.Context, command CreateDependencyCommand) (Dependency, error) {
	preview, err := s.Preview(ctx, command)
	if err != nil {
		return Dependency{}, err
	}
	fact := DependencyChangeFact{EventID: s.newID(), ActorID: preview.Dependency.CreatedBy, OccurredAt: preview.Dependency.CreatedAt}
	if strings.TrimSpace(fact.EventID) == "" || strings.TrimSpace(fact.ActorID) == "" || fact.OccurredAt.IsZero() {
		return Dependency{}, ErrInvalidDependency
	}
	return s.repository.InsertDependency(ctx, preview.Dependency, fact)
}

func (s *DependencyService) Delete(ctx context.Context, command DeleteDependencyCommand) error {
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || strings.TrimSpace(command.DependencyID) == "" || command.ExpectedVersion < 1 {
		return ErrInvalidDependency
	}
	dependency, err := s.repository.LoadDependency(ctx, command.Principal.Scope.MSPID, command.DependencyID)
	if err != nil {
		return err
	}
	if dependency.Version != command.ExpectedVersion {
		return fmt.Errorf("%w: stale dependency version", ErrInvalidDependency)
	}
	evaluatedAt := s.now()
	for _, id := range []string{dependency.PredecessorID, dependency.SuccessorID} {
		projection, loadErr := s.repository.LoadDependencyProjection(ctx, id)
		if loadErr != nil {
			return loadErr
		}
		if loadErr = authorizeDependencyProjection(command.Principal, projection, evaluatedAt); loadErr != nil {
			return loadErr
		}
	}
	fact := DependencyDeletionFact{EventID: s.newID(), ActorID: command.Principal.ID, OccurredAt: evaluatedAt}
	if strings.TrimSpace(fact.EventID) == "" || strings.TrimSpace(fact.ActorID) == "" || fact.OccurredAt.IsZero() {
		return ErrInvalidDependency
	}
	return s.repository.DeleteDependency(ctx, dependency, fact)
}

func validDependencyType(value DependencyType) bool {
	return value == FinishToStart || value == StartToStart || value == FinishToFinish
}

func dependencyEligible(registry *RoleRegistry, projection Projection) bool {
	definition, ok := registry.DefinitionForProjection(projection)
	if !ok || !definition.DependencyEligible || projection.TerminalState == Completed || projection.TerminalState == Cancelled {
		return false
	}
	if definition.SchedulingMode == Informational {
		return projection.SchedulingMode == Informational
	}
	return projection.SchedulingMode == FixedBlock || projection.SchedulingMode == EffortAllocation
}

func authorizeDependencyProjection(principal authorization.Principal, projection Projection, now time.Time) error {
	target := projection.Source.ScopeTarget()
	for _, capability := range []string{"calendar.read", "calendar.schedule"} {
		if err := authorization.AuthorizeAt(principal, capability, target, now); err != nil {
			return err
		}
	}
	readCapability, editCapability := "work_record.read", "work_record.edit"
	if projection.Source.Type == "milestone" || projection.Source.Type == "project" || projection.Source.Type == "phase" || (projection.Source.Type == "task" && len(projection.Dimensions.ProjectIDs) > 0) {
		readCapability, editCapability = "project.read", "project.edit"
	}
	if err := authorization.AuthorizeAt(principal, readCapability, target, now); err != nil {
		return err
	}
	return authorization.AuthorizeAt(principal, editCapability, target, now)
}

func dependencyCreatesCycle(edges []Dependency, predecessorID, successorID string) bool {
	adjacency := make(map[string][]string)
	for _, edge := range edges {
		adjacency[edge.PredecessorID] = append(adjacency[edge.PredecessorID], edge.SuccessorID)
	}
	for id := range adjacency {
		sort.Strings(adjacency[id])
	}
	visited := map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if id == predecessorID {
			return true
		}
		if visited[id] {
			return false
		}
		visited[id] = true
		for _, next := range adjacency[id] {
			if visit(next) {
				return true
			}
		}
		return false
	}
	return visit(successorID)
}

func reachableDependencyIDs(edges []Dependency, rootID string) []string {
	adjacency := make(map[string][]string)
	for _, edge := range edges {
		adjacency[edge.PredecessorID] = append(adjacency[edge.PredecessorID], edge.SuccessorID)
	}
	for id := range adjacency {
		sort.Strings(adjacency[id])
	}
	seen := map[string]bool{rootID: true}
	queue := []string{rootID}
	result := []string{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, successorID := range adjacency[id] {
			if seen[successorID] {
				continue
			}
			seen[successorID] = true
			result = append(result, successorID)
			queue = append(queue, successorID)
			sort.Strings(queue)
		}
	}
	sort.Strings(result)
	return result
}

// BuildCascadeImpact computes a downstream preview over copies of intervals.
// It never modifies a projection or an authoritative source.
func BuildCascadeImpact(rootID string, proposed ScheduleInterval, projections map[string]Projection, dependencies []Dependency) (CascadeImpact, error) {
	registry, err := NewProductionRoleRegistry(nil)
	if err != nil {
		return CascadeImpact{}, err
	}
	return BuildCascadeImpactWithRegistry(registry, rootID, proposed, projections, dependencies)
}

func BuildCascadeImpactWithRegistry(registry *RoleRegistry, rootID string, proposed ScheduleInterval, projections map[string]Projection, dependencies []Dependency) (CascadeImpact, error) {
	impact := CascadeImpact{}
	if registry == nil || strings.TrimSpace(rootID) == "" || !validScheduleInterval(proposed) {
		return impact, ErrInvalidDependency
	}
	if _, ok := projections[rootID]; !ok {
		return impact, ErrInvalidDependency
	}
	edges := append([]Dependency(nil), dependencies...)
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].PredecessorID == edges[j].PredecessorID {
			if edges[i].SuccessorID == edges[j].SuccessorID {
				return edges[i].ID < edges[j].ID
			}
			return edges[i].SuccessorID < edges[j].SuccessorID
		}
		return edges[i].PredecessorID < edges[j].PredecessorID
	})
	intervals := make(map[string]ScheduleInterval, len(projections))
	for id, projection := range projections {
		if interval, ok := projectionInterval(projection); ok {
			intervals[id] = interval
		}
	}
	intervals[rootID] = proposed
	moveByProjection := map[string]CascadeMove{}
	blockedByProjection := map[string]CascadeBlockedSource{}
	affected := map[string]struct{}{}
	capacity := map[string]struct{}{}
	if original, ok := projectionInterval(projections[rootID]); ok && !scheduleIntervalsEqual(original, proposed) {
		affected[rootID] = struct{}{}
		if assigneeID := projections[rootID].AssigneeID; assigneeID != "" {
			capacity[assigneeID] = struct{}{}
		}
	}
	for _, id := range reachableDependencyIDs(edges, rootID) {
		affected[id] = struct{}{}
	}
	queue := []string{rootID}
	queued := map[string]bool{rootID: true}
	for len(queue) > 0 {
		predecessorID := queue[0]
		queue = queue[1:]
		queued[predecessorID] = false
		predecessor, ok := intervals[predecessorID]
		if !ok {
			continue
		}
		for _, edge := range edges {
			if edge.PredecessorID != predecessorID {
				continue
			}
			successorProjection, exists := projections[edge.SuccessorID]
			successor, hasInterval := intervals[edge.SuccessorID]
			if !exists || !hasInterval || !dependencyEligible(registry, successorProjection) {
				if exists {
					blockedByProjection[edge.SuccessorID] = CascadeBlockedSource{ProjectionID: edge.SuccessorID, Source: successorProjection.Source, ReasonCode: "dependency_source_not_movable"}
					affected[edge.SuccessorID] = struct{}{}
				}
				continue
			}
			shifted, moved, shiftErr := constrainedSuccessor(predecessor, successor, edge.Type, edge.LeadLagMinutes)
			if shiftErr != nil {
				return impact, shiftErr
			}
			if !moved {
				continue
			}
			original := successor
			if prior, moved := moveByProjection[edge.SuccessorID]; moved {
				original = prior.From
			}
			successor = shifted
			intervals[edge.SuccessorID] = successor
			moveByProjection[edge.SuccessorID] = CascadeMove{ProjectionID: edge.SuccessorID, DependencyID: edge.ID, From: original, To: successor, ReasonCode: "dependency_constraint"}
			affected[edge.SuccessorID] = struct{}{}
			if successorProjection.AssigneeID != "" {
				capacity[successorProjection.AssigneeID] = struct{}{}
			}
			if !queued[edge.SuccessorID] {
				queue = append(queue, edge.SuccessorID)
				sort.Strings(queue)
				queued[edge.SuccessorID] = true
			}
		}
	}
	moveIDs := make([]string, 0, len(moveByProjection))
	for id := range moveByProjection {
		moveIDs = append(moveIDs, id)
	}
	sort.Strings(moveIDs)
	for _, id := range moveIDs {
		move := moveByProjection[id]
		if projections[id].SchedulingMode == EffortAllocation {
			impact.OptionalMoves = append(impact.OptionalMoves, move)
		} else {
			impact.RequiredMoves = append(impact.RequiredMoves, move)
		}
	}
	blockedIDs := make([]string, 0, len(blockedByProjection))
	for id := range blockedByProjection {
		blockedIDs = append(blockedIDs, id)
	}
	sort.Strings(blockedIDs)
	for _, id := range blockedIDs {
		impact.BlockedSources = append(impact.BlockedSources, blockedByProjection[id])
	}
	for id := range affected {
		impact.AffectedHealth = append(impact.AffectedHealth, id)
	}
	sort.Strings(impact.AffectedHealth)
	for key := range capacity {
		impact.CapacityRecalculationKeys = append(impact.CapacityRecalculationKeys, key)
	}
	sort.Strings(impact.CapacityRecalculationKeys)
	return impact, nil
}

func constrainedSuccessor(predecessor, successor ScheduleInterval, relationship DependencyType, leadLagMinutes int) (ScheduleInterval, bool, error) {
	predecessorDate, successorDate := predecessor.StartsOn != nil, successor.StartsOn != nil
	if predecessorDate != successorDate {
		return successor, false, ErrLeadLagPrecision
	}
	if predecessorDate {
		if leadLagMinutes%(24*60) != 0 {
			return successor, false, ErrLeadLagPrecision
		}
		required, actual := dependencyDateAnchors(predecessor, successor, relationship)
		if required == nil || actual == nil {
			return successor, false, ErrInvalidDependency
		}
		requiredDate := required.AddDate(0, 0, leadLagMinutes/(24*60))
		if !requiredDate.After(*actual) {
			return successor, false, nil
		}
		days := int(requiredDate.Sub(*actual) / (24 * time.Hour))
		return shiftDateInterval(successor, days), true, nil
	}
	delta := dependencyTimedShift(predecessor, successor, relationship, leadLagMinutes)
	if delta <= 0 {
		return successor, false, nil
	}
	return shiftTimedInterval(successor, delta), true, nil
}

func dependencyTimedShift(predecessor, successor ScheduleInterval, relationship DependencyType, leadLagMinutes int) time.Duration {
	lag := time.Duration(leadLagMinutes) * time.Minute
	var required, actual *time.Time
	switch relationship {
	case FinishToStart:
		required, actual = timedEnd(predecessor), successor.StartsAt
	case StartToStart:
		required, actual = predecessor.StartsAt, successor.StartsAt
	case FinishToFinish:
		required, actual = timedEnd(predecessor), timedEnd(successor)
	default:
		return 0
	}
	if required == nil || actual == nil {
		return 0
	}
	requiredAt := required.Add(lag)
	if requiredAt.After(*actual) {
		return requiredAt.Sub(*actual)
	}
	return 0
}

func dependencyDateAnchors(predecessor, successor ScheduleInterval, relationship DependencyType) (*time.Time, *time.Time) {
	switch relationship {
	case FinishToStart:
		return dateEndExclusive(predecessor), successor.StartsOn
	case StartToStart:
		return predecessor.StartsOn, successor.StartsOn
	case FinishToFinish:
		return dateEndExclusive(predecessor), dateEndExclusive(successor)
	default:
		return nil, nil
	}
}

func timedEnd(interval ScheduleInterval) *time.Time {
	if interval.EndsAt != nil {
		return interval.EndsAt
	}
	return interval.StartsAt
}

func dateEndExclusive(interval ScheduleInterval) *time.Time {
	end := interval.StartsOn
	if interval.EndsOn != nil {
		end = interval.EndsOn
	}
	if end == nil {
		return nil
	}
	value := end.AddDate(0, 0, 1)
	return &value
}

func shiftTimedInterval(interval ScheduleInterval, delta time.Duration) ScheduleInterval {
	start := interval.StartsAt.Add(delta)
	interval.StartsAt = &start
	if interval.EndsAt != nil {
		end := interval.EndsAt.Add(delta)
		interval.EndsAt = &end
	}
	return interval
}

func shiftDateInterval(interval ScheduleInterval, days int) ScheduleInterval {
	start := interval.StartsOn.AddDate(0, 0, days)
	interval.StartsOn = &start
	if interval.EndsOn != nil {
		end := interval.EndsOn.AddDate(0, 0, days)
		interval.EndsOn = &end
	}
	return interval
}

func validScheduleInterval(interval ScheduleInterval) bool {
	if interval.StartsOn != nil {
		return interval.StartsAt == nil && interval.EndsAt == nil && (interval.EndsOn == nil || !interval.EndsOn.Before(*interval.StartsOn))
	}
	return interval.EndsOn == nil && interval.StartsAt != nil && (interval.EndsAt == nil || !interval.EndsAt.Before(*interval.StartsAt))
}

func scheduleIntervalsEqual(left, right ScheduleInterval) bool {
	return equalOptionalTime(left.StartsOn, right.StartsOn) && equalOptionalTime(left.EndsOn, right.EndsOn) && equalOptionalTime(left.StartsAt, right.StartsAt) && equalOptionalTime(left.EndsAt, right.EndsAt)
}

func equalOptionalTime(left, right *time.Time) bool {
	return left == nil && right == nil || left != nil && right != nil && left.Equal(*right)
}

func projectionInterval(projection Projection) (ScheduleInterval, bool) {
	if projection.AllDay {
		if projection.StartsOn == nil {
			return ScheduleInterval{}, false
		}
		return ScheduleInterval{StartsOn: projection.StartsOn, EndsOn: projection.EndsOn}, true
	}
	if projection.StartsAt == nil {
		return ScheduleInterval{}, false
	}
	if projection.EndsAt != nil && projection.EndsAt.Before(*projection.StartsAt) {
		return ScheduleInterval{}, false
	}
	return ScheduleInterval{StartsAt: projection.StartsAt, EndsAt: projection.EndsAt}, true
}

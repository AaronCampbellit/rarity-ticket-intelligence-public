package psa

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

const (
	maximumCalendarRecommendationDependencies = 50
)

type CalendarAIContextRepository struct {
	db       database
	calendar *CalendarRepository
	now      func() time.Time
}

var _ aiassist.CalendarRecommendationContextLoader = (*CalendarAIContextRepository)(nil)

func NewCalendarAIContextRepository(db database, now func() time.Time) *CalendarAIContextRepository {
	return &CalendarAIContextRepository{db: db, calendar: NewCalendarRepository(db), now: now}
}

func (r *CalendarAIContextRepository) LoadCalendarRecommendationContext(ctx context.Context, principal authorization.Principal, projectionID string) (aiassist.CalendarRecommendationContext, error) {
	var result aiassist.CalendarRecommendationContext
	if r == nil || r.db == nil || r.calendar == nil || r.now == nil || !internalid.ValidCanonical(principal.ID) ||
		!internalid.ValidCanonical(principal.Scope.MSPID) || !internalid.ValidCanonical(projectionID) {
		return result, aiassist.ErrInvalidCalendarRecommendation
	}
	projection, err := r.calendar.loadCalendarQueryProjection(ctx, principal.Scope.MSPID, projectionID)
	if err != nil {
		return result, err
	}
	target := projection.Projection.Source.ScopeTarget()
	if projection.Projection.TerminalState != calendar.Active {
		return result, scope.ErrNotFound
	}
	if err = authorization.Authorize(principal, "calendar.schedule", target); err != nil {
		return result, err
	}
	readAllowed, err := r.calendar.CanReadCalendarSource(ctx, principal, projection.Projection.Source)
	if err != nil {
		return result, err
	}
	if !readAllowed {
		return result, authorization.ErrForbidden
	}
	window := calendarRecommendationWindow(projection.Projection, r.now().UTC())
	currentAssignee := projection.Projection.AssigneeID
	if !internalid.ValidCanonical(currentAssignee) {
		return result, scope.ErrNotFound
	}
	allowed, err := r.calendar.CanScheduleCalendarTechnician(ctx, principal, currentAssignee)
	if err != nil {
		return result, err
	}
	if !allowed {
		return result, authorization.ErrForbidden
	}
	technicians := []string{currentAssignee}
	availability, err := r.calendar.LoadCalendarQueryAvailability(ctx, principal.Scope.MSPID, technicians, window)
	if err != nil {
		return result, err
	}
	workload, err := r.workload(ctx, principal.Scope.MSPID, technicians, window)
	if err != nil {
		return result, err
	}
	dependencies, err := r.dependencies(ctx, projection.Projection)
	if err != nil {
		return result, err
	}
	byTechnician := make(map[string][]calendar.TimeInterval, len(technicians))
	allIntervals := make([]calendar.TimeInterval, 0)
	for _, technicianID := range technicians {
		resolved := availability[technicianID]
		intervals := make([]calendar.TimeInterval, 0, len(resolved.Segments))
		for _, segment := range resolved.Segments {
			if segment.CapacityPercent <= 0 || !segment.Interval.End.After(segment.Interval.Start) {
				continue
			}
			intervals = append(intervals, segment.Interval)
			allIntervals = append(allIntervals, segment.Interval)
		}
		byTechnician[technicianID] = intervals
	}
	priority := ""
	if len(projection.Projection.Dimensions.Priorities) > 0 {
		priority = projection.Projection.Dimensions.Priorities[0]
	}
	return aiassist.CalendarRecommendationContext{
		MSPID: principal.Scope.MSPID, ProjectionID: projectionID,
		AuthorizedTechnicianIDs: technicians, RequiredSkillIDs: []string{},
		AvailableIntervals: allIntervals, AvailableIntervalsByTechnicianID: byTechnician,
		WorkloadMinutes: workload, Priority: priority,
		RequiredTechnologyIDs: append([]string(nil), projection.Projection.Dimensions.TechnologyIDs...),
		DependencyConstraints: dependencies, Health: projection.Health.State, Conflicts: []calendar.Conflict{},
	}, nil
}

func calendarRecommendationWindow(projection calendar.Projection, now time.Time) calendar.QueryWindow {
	start := now
	if projection.StartsAt != nil && projection.StartsAt.After(now) {
		start = projection.StartsAt.Add(-24 * time.Hour)
	}
	if start.Before(now) {
		start = now
	}
	return calendar.QueryWindow{Start: start.UTC(), End: start.Add(14 * 24 * time.Hour).UTC()}
}

func (r *CalendarAIContextRepository) workload(ctx context.Context, mspID string, technicians []string, window calendar.QueryWindow) (map[string]int64, error) {
	result := make(map[string]int64, len(technicians))
	for _, id := range technicians {
		result[id] = 0
	}
	rows, err := r.db.Query(ctx, `SELECT assignee_id::text,COALESCE(sum(planned_minutes),0)::bigint FROM calendar_event_projections WHERE msp_id=$1::uuid AND assignee_id::text=ANY($2::text[]) AND capacity_bearing AND terminal_state='active' AND starts_at<$4 AND COALESCE(ends_at,starts_at)>$3 GROUP BY assignee_id ORDER BY assignee_id`, mspID, technicians, window.Start, window.End)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var minutes int64
		if err = rows.Scan(&id, &minutes); err != nil {
			return nil, err
		}
		if _, authorized := result[id]; authorized {
			result[id] = minutes
		}
	}
	return result, rows.Err()
}

func (r *CalendarAIContextRepository) dependencies(ctx context.Context, projection calendar.Projection) ([]string, error) {
	if projection.Source.ClientID == "" {
		return []string{}, nil
	}
	rows, err := r.db.Query(ctx, `SELECT relationship_type,lead_lag_minutes,CASE WHEN predecessor_projection_id=$3::uuid THEN 'successor' ELSE 'predecessor' END FROM calendar_dependencies WHERE msp_id=$1::uuid AND client_id=$2::uuid AND (predecessor_projection_id=$3::uuid OR successor_projection_id=$3::uuid) ORDER BY relationship_type,id LIMIT $4`, projection.Source.MSPID, projection.Source.ClientID, projection.ID, maximumCalendarRecommendationDependencies)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var relationship, direction string
		var lag int64
		if err = rows.Scan(&relationship, &lag, &direction); err != nil {
			return nil, err
		}
		result = append(result, fmt.Sprintf("%s:%s:%d", direction, strings.TrimSpace(relationship), lag))
	}
	sort.Strings(result)
	return result, rows.Err()
}

package psa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar/adapters"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/commitments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/customfields"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

type CalendarRepository struct{ db database }

func NewCalendarRepository(db database) *CalendarRepository { return &CalendarRepository{db: db} }

const authorizedCalendarClientsSQL = `SELECT client.id::text
FROM client_organizations client
WHERE client.msp_id=$1::uuid AND client.lifecycle_state='active'
AND EXISTS (
  SELECT 1 FROM role_assignments assignment
  JOIN role_capabilities capability ON capability.role_id=assignment.role_id AND capability.msp_id=assignment.msp_id
  WHERE assignment.msp_id=client.msp_id AND assignment.technician_id=$2::uuid
    AND (assignment.client_id IS NULL OR assignment.client_id=client.id)
    AND (assignment.expires_at IS NULL OR assignment.expires_at>now())
    AND capability.capability=$3
)
ORDER BY client.id`

func (r *CalendarRepository) AuthorizedCalendarClientIDs(ctx context.Context, principal authorization.Principal) ([]string, error) {
	if r == nil || r.db == nil || !internalid.ValidCanonical(principal.ID) || !internalid.ValidCanonical(principal.Scope.MSPID) {
		return nil, calendar.ErrInvalidCalendarQuery
	}
	rows, err := r.db.Query(ctx, authorizedCalendarClientsSQL, principal.Scope.MSPID, principal.ID, "calendar.read")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		found = append(found, id)
	}
	return found, rows.Err()
}

func (r *CalendarRepository) LoadCalendarQueryAvailability(ctx context.Context, mspID string, technicianIDs []string, window calendar.QueryWindow) (map[string]calendar.ResolvedAvailability, error) {
	if r == nil || r.db == nil || !internalid.ValidCanonical(mspID) || len(technicianIDs) == 0 || !window.End.After(window.Start) {
		return nil, calendar.ErrInvalidAvailabilityWindow
	}
	service := calendar.NewAvailabilityService(r)
	result := make(map[string]calendar.ResolvedAvailability, len(technicianIDs))
	for _, technicianID := range technicianIDs {
		if !internalid.ValidCanonical(technicianID) {
			return nil, calendar.ErrInvalidAvailabilityWindow
		}
		resolved, err := service.Resolve(ctx, mspID, technicianID, window)
		if err != nil {
			return nil, err
		}
		result[technicianID] = resolved
	}
	return result, nil
}

const calendarProjectionSourceReadSQL = `EXISTS(SELECT 1 FROM role_assignments source_assignment
JOIN role_capabilities source_capability ON source_capability.role_id=source_assignment.role_id AND source_capability.msp_id=source_assignment.msp_id
WHERE source_assignment.msp_id=projection.msp_id AND source_assignment.technician_id=$24::uuid
  AND (source_assignment.client_id IS NULL OR source_assignment.client_id IS NOT DISTINCT FROM projection.client_id)
  AND (source_assignment.expires_at IS NULL OR source_assignment.expires_at>now())
  AND source_capability.capability=CASE projection.source_type
    WHEN 'work_record' THEN 'work_record.read'
    WHEN 'task' THEN (SELECT CASE task.parent_type WHEN 'work_record' THEN 'work_record.read' WHEN 'project' THEN 'project.read' WHEN 'phase' THEN 'project.read' WHEN 'opportunity' THEN 'opportunity.read' ELSE '__denied__' END FROM tasks task WHERE task.id=projection.source_id AND task.msp_id=projection.msp_id AND task.client_id=projection.client_id)
    WHEN 'project' THEN 'project.read' WHEN 'phase' THEN 'project.read' WHEN 'milestone' THEN 'project.read' WHEN 'resource_plan' THEN 'project.read'
    WHEN 'technician_schedule' THEN 'calendar.workforce.manage' WHEN 'pto' THEN 'calendar.workforce.manage'
    WHEN 'maintenance_window' THEN 'calendar.commitment.manage' WHEN 'commercial_commitment' THEN 'calendar.commitment.manage'
    WHEN 'custom_date' THEN (SELECT CASE WHEN value.object_type='work_record' THEN 'work_record.read' WHEN value.object_type='task' AND task.parent_type='work_record' THEN 'work_record.read' WHEN value.object_type='task' AND task.parent_type IN ('project','phase') THEN 'project.read' WHEN value.object_type='task' AND task.parent_type='opportunity' THEN 'opportunity.read' WHEN value.object_type='project' THEN 'project.read' WHEN value.object_type='asset' THEN 'search.read' WHEN value.object_type='knowledge_article' THEN 'knowledge.read' WHEN value.object_type='time_entry' THEN 'time_entry.read_scoped' ELSE '__denied__' END
      FROM object_custom_date_values value
      LEFT JOIN tasks task ON value.object_type='task' AND task.id=value.object_id AND task.msp_id=value.msp_id AND task.client_id=value.client_id
      LEFT JOIN work_records work ON value.object_type='work_record' AND work.id=value.object_id AND work.msp_id=value.msp_id AND work.client_id=value.client_id
      LEFT JOIN projects project ON value.object_type='project' AND project.id=value.object_id AND project.msp_id=value.msp_id AND project.client_id=value.client_id
      LEFT JOIN assets asset ON value.object_type='asset' AND asset.id=value.object_id AND asset.msp_id=value.msp_id AND asset.client_id=value.client_id
      LEFT JOIN knowledge_articles article ON value.object_type='knowledge_article' AND article.id=value.object_id AND article.msp_id=value.msp_id AND article.client_id IS NOT DISTINCT FROM value.client_id
      LEFT JOIN time_entries entry ON value.object_type='time_entry' AND entry.id=value.object_id AND entry.msp_id=value.msp_id AND entry.client_id=value.client_id
      WHERE value.id=projection.source_id AND value.msp_id=projection.msp_id AND value.client_id IS NOT DISTINCT FROM projection.client_id
        AND CASE value.object_type WHEN 'work_record' THEN work.id IS NOT NULL WHEN 'task' THEN task.id IS NOT NULL WHEN 'project' THEN project.id IS NOT NULL WHEN 'asset' THEN asset.id IS NOT NULL WHEN 'knowledge_article' THEN article.id IS NOT NULL WHEN 'time_entry' THEN entry.id IS NOT NULL ELSE false END)
    ELSE '__denied__' END)`

const calendarProjectionWorkforceSQL = `projection.assignee_id IS NOT NULL AND EXISTS(SELECT 1 FROM technicians target
WHERE target.id=projection.assignee_id AND target.msp_id=projection.msp_id AND target.lifecycle_state='active'
AND (target.id=$24::uuid
  OR EXISTS(SELECT 1 FROM team_memberships target_member JOIN team_memberships actor_member ON actor_member.team_id=target_member.team_id AND actor_member.msp_id=target_member.msp_id AND actor_member.technician_id=$24::uuid AND actor_member.lifecycle_state='active' WHERE target_member.technician_id=target.id AND target_member.msp_id=target.msp_id AND target_member.lifecycle_state='active')
  OR EXISTS(SELECT 1 FROM team_memberships target_member JOIN teams team ON team.id=target_member.team_id AND team.msp_id=target_member.msp_id AND team.workforce_manager_id=$24::uuid WHERE target_member.technician_id=target.id AND target_member.msp_id=target.msp_id AND target_member.lifecycle_state='active')
  OR EXISTS(SELECT 1 FROM role_assignments workforce_assignment JOIN role_capabilities workforce_capability ON workforce_capability.role_id=workforce_assignment.role_id AND workforce_capability.msp_id=workforce_assignment.msp_id AND workforce_capability.capability='calendar.workforce.manage' WHERE workforce_assignment.technician_id=$24::uuid AND workforce_assignment.msp_id=projection.msp_id AND workforce_assignment.client_id IS NULL AND (workforce_assignment.expires_at IS NULL OR workforce_assignment.expires_at>now()))))`

func (r *CalendarRepository) ListCalendarProjections(ctx context.Context, principal authorization.Principal, clientIDs []string, window calendar.QueryWindow, filter calendar.Filter, visibility calendar.QueryVisibility, limit int) ([]calendar.QueryProjection, error) {
	mspID := principal.Scope.MSPID
	if r == nil || r.db == nil || !internalid.ValidCanonical(principal.ID) || !internalid.ValidCanonical(mspID) || (visibility != calendar.QueryVisibilityFull && visibility != calendar.QueryVisibilityBusy) || window.Start.IsZero() || !window.End.After(window.Start) || limit < 1 || limit > 10_001 {
		return nil, calendar.ErrInvalidCalendarQuery
	}
	query := `SELECT projection.id::text,COALESCE(projection.client_id::text,''),projection.source_type,projection.source_id::text,projection.event_role,projection.source_role_key,projection.source_revision,projection.title,projection.all_day,projection.starts_on,projection.ends_on,projection.starts_at,projection.ends_at,COALESCE(projection.timezone,''),projection.scheduling_mode,projection.capacity_bearing,COALESCE(projection.owner_id::text,''),COALESCE(projection.assignee_id::text,''),projection.planned_minutes,projection.filter_dimensions,projection.recurrence_rule,projection.terminal_state,projection.health_state,projection.health_reasons,projection.health_rule_version,projection.health_evaluated_at,COALESCE((projection.health_inputs->>'hard_constraint')::boolean,false) OR EXISTS(SELECT 1 FROM jsonb_array_elements(projection.health_reasons) reason WHERE reason->>'code' IN('hard_constraint','ordinary_overbooking','approved_pto','non_working_time','protected_maintenance'))
FROM calendar_event_projections projection
WHERE projection.msp_id=$1::uuid
  AND (projection.client_id IS NULL OR projection.client_id = ANY($2::uuid[]))
  AND projection.terminal_state <> 'cancelled'
  AND ((NOT projection.all_day AND projection.starts_at < $4 AND (projection.recurrence_rule IS NOT NULL OR COALESCE(projection.ends_at,projection.starts_at) > $3))
    OR (projection.all_day AND projection.starts_on < $4::date AND (projection.recurrence_rule IS NOT NULL OR COALESCE(projection.ends_on,projection.starts_on) >= $3::date)))
	AND (COALESCE(cardinality($6::text[]),0)=0 OR projection.assignee_id::text=ANY($6::text[]) OR projection.filter_dimensions->'technician_ids' ?| $6::text[])
	AND (COALESCE(cardinality($7::text[]),0)=0 OR projection.owner_id::text=ANY($7::text[]) OR projection.filter_dimensions->'owner_ids' ?| $7::text[])
	AND (COALESCE(cardinality($8::text[]),0)=0 OR projection.filter_dimensions->'team_ids' ?| $8::text[])
	AND ($9::text[] IS NULL OR projection.client_id::text=ANY($9::text[]))
	AND (COALESCE(cardinality($10::text[]),0)=0 OR projection.filter_dimensions->'technology_ids' ?| $10::text[])
	AND (COALESCE(cardinality($11::text[]),0)=0 OR projection.filter_dimensions->'project_ids' ?| $11::text[])
	AND (COALESCE(cardinality($12::text[]),0)=0 OR projection.filter_dimensions->'phase_ids' ?| $12::text[] OR (projection.source_type='phase' AND projection.source_id::text=ANY($12::text[])))
	AND (COALESCE(cardinality($13::text[]),0)=0 OR projection.filter_dimensions->'sla_ids' ?| $13::text[])
	AND (COALESCE(cardinality($14::text[]),0)=0 OR projection.filter_dimensions->'ticket_types' ?| $14::text[])
	AND (COALESCE(cardinality($15::text[]),0)=0 OR projection.filter_dimensions->'tag_ids' ?| $15::text[])
	AND (COALESCE(cardinality($16::text[]),0)=0 OR projection.filter_dimensions->'priorities' ?| $16::text[])
	AND (COALESCE(cardinality($17::text[]),0)=0 OR projection.event_role=ANY($17::text[]))
	AND (COALESCE(cardinality($18::text[]),0)=0 OR projection.source_type=ANY($18::text[]))
	AND (COALESCE(cardinality($19::text[]),0)=0 OR projection.scheduling_mode=ANY($19::text[]))
	AND (COALESCE(cardinality($20::text[]),0)=0 OR projection.terminal_state=ANY($20::text[]))
	AND (COALESCE(cardinality($21::text[]),0)=0 OR projection.health_state=ANY($21::text[]))
	AND (NOT $22::boolean OR COALESCE((projection.health_inputs->>'hard_constraint')::boolean,false) OR EXISTS(SELECT 1 FROM jsonb_array_elements(projection.health_reasons) reason WHERE reason->>'code' IN('hard_constraint','ordinary_overbooking','approved_pto','non_working_time','protected_maintenance')))
	AND (($23='full' AND ` + calendarProjectionSourceReadSQL + `)
	  OR ($23='busy' AND ` + calendarProjectionWorkforceSQL + ` AND NOT ` + calendarProjectionSourceReadSQL + `))
ORDER BY COALESCE(projection.starts_at,projection.starts_on::timestamp AT TIME ZONE 'UTC'),projection.id
LIMIT $5`
	rows, err := r.db.Query(ctx, query, mspID, clientIDs, window.Start, window.End, limit,
		filter.TechnicianIDs, filter.OwnerIDs, filter.TeamIDs, filter.ClientIDs,
		filter.TechnologyIDs, filter.ProjectIDs, filter.PhaseIDs, filter.SLAIDs,
		filter.TicketTypes, filter.TagIDs, filter.Priorities, filter.EventRoles,
		filter.SourceTypes, calendarSchedulingModeStrings(filter.SchedulingModes), calendarTerminalStateStrings(filter.TerminalStates),
		calendarHealthStateStrings(filter.HealthStates), filter.ConflictsOnly, string(visibility), principal.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]calendar.QueryProjection, 0)
	for rows.Next() {
		row, scanErr := scanCalendarQueryProjection(rows, mspID)
		if scanErr != nil {
			return nil, scanErr
		}
		if row.Projection.Recurrence != nil {
			row.Exceptions, scanErr = r.loadRecurrenceExceptions(ctx, mspID, row.Projection.ID)
			if scanErr != nil {
				return nil, scanErr
			}
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func calendarSchedulingModeStrings(values []calendar.SchedulingMode) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}

func calendarTerminalStateStrings(values []calendar.TerminalState) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}

func calendarHealthStateStrings(values []calendar.HealthState) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}

type calendarQueryScanner interface{ Scan(...any) error }

func scanCalendarQueryProjection(scanner calendarQueryScanner, mspID string) (calendar.QueryProjection, error) {
	var row calendar.QueryProjection
	var dimensions, recurrence, healthReasons []byte
	var healthEvaluatedAt *time.Time
	err := scanner.Scan(&row.Projection.ID, &row.Projection.Source.ClientID, &row.Projection.Source.Type, &row.Projection.Source.ID, &row.Projection.EventRole, &row.Projection.SourceRoleKey, &row.Projection.SourceRevision, &row.Projection.Title, &row.Projection.AllDay, &row.Projection.StartsOn, &row.Projection.EndsOn, &row.Projection.StartsAt, &row.Projection.EndsAt, &row.Projection.Timezone, &row.Projection.SchedulingMode, &row.Projection.CapacityBearing, &row.Projection.OwnerID, &row.Projection.AssigneeID, &row.Projection.PlannedMinutes, &dimensions, &recurrence, &row.Projection.TerminalState, &row.Health.State, &healthReasons, &row.Health.RuleVersion, &healthEvaluatedAt, &row.HasConflict)
	if err != nil {
		return row, err
	}
	row.Projection.Source.MSPID = mspID
	if healthEvaluatedAt != nil {
		row.Health.EvaluatedAt = *healthEvaluatedAt
	}
	if len(dimensions) > 0 {
		if err = json.Unmarshal(dimensions, &row.Projection.Dimensions); err != nil {
			return row, err
		}
	}
	if len(recurrence) > 0 {
		row.Projection.Recurrence = &calendar.RecurrenceRule{}
		if err = json.Unmarshal(recurrence, row.Projection.Recurrence); err != nil {
			return row, err
		}
	}
	if len(healthReasons) > 0 {
		if err = json.Unmarshal(healthReasons, &row.Health.Reasons); err != nil {
			return row, err
		}
	}
	return row, nil
}

func (r *CalendarRepository) loadRecurrenceExceptions(ctx context.Context, mspID, projectionID string) ([]calendar.RecurrenceException, error) {
	rows, err := r.db.Query(ctx, `SELECT original_local_key,state,COALESCE(occurrence_scope,'this_occurrence'),starts_on,ends_on,starts_at,ends_at FROM calendar_recurrence_exceptions WHERE msp_id=$1::uuid AND projection_id=$2::uuid ORDER BY original_local_key,id`, mspID, projectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []calendar.RecurrenceException
	for rows.Next() {
		var value calendar.RecurrenceException
		if err = rows.Scan(&value.OriginalLocalKey, &value.State, &value.OccurrenceScope, &value.StartsOn, &value.EndsOn, &value.StartsAt, &value.EndsAt); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (r *CalendarRepository) CanReadCalendarSource(ctx context.Context, principal authorization.Principal, source calendar.SourceRef) (bool, error) {
	if r == nil || r.db == nil || !internalid.ValidCanonical(principal.ID) || !internalid.ValidCanonical(source.MSPID) || !internalid.ValidCanonical(source.ID) || source.MSPID != principal.Scope.MSPID {
		return false, nil
	}
	if source.Type == "task" {
		var allowed bool
		err := r.db.QueryRow(ctx, `SELECT EXISTS(
SELECT 1 FROM tasks task
JOIN role_assignments assignment ON assignment.msp_id=task.msp_id
  AND assignment.technician_id=$2::uuid
  AND (assignment.client_id IS NULL OR assignment.client_id=task.client_id)
  AND (assignment.expires_at IS NULL OR assignment.expires_at>now())
JOIN role_capabilities capability ON capability.role_id=assignment.role_id AND capability.msp_id=assignment.msp_id
WHERE task.msp_id=$1::uuid AND task.id=$4::uuid AND task.client_id=$3::uuid
  AND capability.capability=CASE task.parent_type
    WHEN 'work_record' THEN 'work_record.read'
    WHEN 'project' THEN 'project.read'
    WHEN 'phase' THEN 'project.read'
    WHEN 'opportunity' THEN 'opportunity.read'
    ELSE '__denied__'
  END)`, source.MSPID, principal.ID, source.ClientID, source.ID).Scan(&allowed)
		return allowed, err
	}
	if source.Type == "custom_date" {
		var allowed bool
		err := r.db.QueryRow(ctx, `SELECT EXISTS(
SELECT 1 FROM object_custom_date_values value
LEFT JOIN tasks task ON value.object_type='task' AND task.id=value.object_id AND task.msp_id=value.msp_id AND task.client_id=value.client_id
LEFT JOIN work_records work ON value.object_type='work_record' AND work.id=value.object_id AND work.msp_id=value.msp_id AND work.client_id=value.client_id
LEFT JOIN projects project ON value.object_type='project' AND project.id=value.object_id AND project.msp_id=value.msp_id AND project.client_id=value.client_id
LEFT JOIN assets asset ON value.object_type='asset' AND asset.id=value.object_id AND asset.msp_id=value.msp_id AND asset.client_id=value.client_id
LEFT JOIN knowledge_articles article ON value.object_type='knowledge_article' AND article.id=value.object_id AND article.msp_id=value.msp_id AND article.client_id IS NOT DISTINCT FROM value.client_id
LEFT JOIN time_entries entry ON value.object_type='time_entry' AND entry.id=value.object_id AND entry.msp_id=value.msp_id AND entry.client_id=value.client_id
JOIN role_assignments assignment ON assignment.msp_id=value.msp_id
  AND assignment.technician_id=$2::uuid
  AND (assignment.client_id IS NULL OR assignment.client_id IS NOT DISTINCT FROM value.client_id)
  AND (assignment.expires_at IS NULL OR assignment.expires_at>now())
JOIN role_capabilities capability ON capability.role_id=assignment.role_id AND capability.msp_id=assignment.msp_id
WHERE value.msp_id=$1::uuid AND value.id=$4::uuid
  AND value.client_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid
  AND CASE value.object_type
    WHEN 'work_record' THEN work.id IS NOT NULL
    WHEN 'task' THEN task.id IS NOT NULL
    WHEN 'project' THEN project.id IS NOT NULL
    WHEN 'asset' THEN asset.id IS NOT NULL
    WHEN 'knowledge_article' THEN article.id IS NOT NULL
    WHEN 'time_entry' THEN entry.id IS NOT NULL
    ELSE false
  END
  AND capability.capability=CASE
    WHEN value.object_type='work_record' THEN 'work_record.read'
    WHEN value.object_type='task' AND task.parent_type='work_record' THEN 'work_record.read'
    WHEN value.object_type='task' AND task.parent_type IN ('project','phase') THEN 'project.read'
    WHEN value.object_type='task' AND task.parent_type='opportunity' THEN 'opportunity.read'
    WHEN value.object_type='project' THEN 'project.read'
    WHEN value.object_type='asset' THEN 'search.read'
    WHEN value.object_type='knowledge_article' THEN 'knowledge.read'
    WHEN value.object_type='time_entry' THEN 'time_entry.read_scoped'
    ELSE '__denied__'
  END)`, source.MSPID, principal.ID, source.ClientID, source.ID).Scan(&allowed)
		return allowed, err
	}
	capabilities := calendarSourceReadCapabilities(source.Type)
	if len(capabilities) == 0 {
		return false, nil
	}
	var allowed bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM role_assignments assignment JOIN role_capabilities capability ON capability.role_id=assignment.role_id AND capability.msp_id=assignment.msp_id WHERE assignment.msp_id=$1::uuid AND assignment.technician_id=$2::uuid AND (assignment.client_id IS NULL OR assignment.client_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid) AND (assignment.expires_at IS NULL OR assignment.expires_at>now()) AND capability.capability=ANY($4))`, source.MSPID, principal.ID, source.ClientID, capabilities).Scan(&allowed)
	return allowed, err
}

func calendarSourceReadCapabilities(sourceType string) []string {
	switch sourceType {
	case "work_record":
		return []string{"work_record.read"}
	case "project", "phase", "milestone", "resource_plan":
		return []string{"project.read"}
	case "technician_schedule", "pto":
		return []string{"calendar.workforce.manage"}
	case "maintenance_window", "commercial_commitment":
		return []string{"calendar.commitment.manage"}
	default:
		return nil
	}
}

func (r *CalendarRepository) CanScheduleCalendarTechnician(ctx context.Context, principal authorization.Principal, technicianID string) (bool, error) {
	if r == nil || r.db == nil || !internalid.ValidCanonical(principal.ID) || !internalid.ValidCanonical(principal.Scope.MSPID) || !internalid.ValidCanonical(technicianID) {
		return false, nil
	}
	var allowed bool
	err := r.db.QueryRow(ctx, schedulingWorkforceAuthoritySQL, principal.ID, principal.Scope.MSPID, technicianID, time.Now().UTC()).Scan(&allowed)
	return allowed, err
}

func (r *CalendarRepository) ListCalendarChangesAfter(ctx context.Context, mspID string, clientIDs []string, cursor uint64, limit int) (calendar.LiveRepositoryPage, error) {
	var page calendar.LiveRepositoryPage
	if r == nil || r.db == nil || !internalid.ValidCanonical(mspID) || limit < 1 || limit > 1000 {
		return page, calendar.ErrInvalidCalendarQuery
	}
	if err := r.db.QueryRow(ctx, `SELECT COALESCE(MIN(cursor),0),COALESCE(MAX(cursor),0) FROM calendar_live_changes WHERE msp_id=$1::uuid AND (client_id IS NULL OR client_id=ANY($2::uuid[]))`, mspID, clientIDs).Scan(&page.OldestCursor, &page.LatestCursor); err != nil {
		return page, err
	}
	if cursor > 0 && page.OldestCursor > 0 && cursor+1 < page.OldestCursor {
		page.Expired = true
		return page, nil
	}
	rows, err := r.db.Query(ctx, `SELECT cursor,COALESCE(client_id::text,''),COALESCE(projection_id::text,''),source_type,source_id::text,event_role,change_type FROM calendar_live_changes WHERE msp_id=$1::uuid AND cursor>$2 AND (client_id IS NULL OR client_id=ANY($3::uuid[])) ORDER BY cursor LIMIT $4`, mspID, cursor, clientIDs, limit)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var change calendar.LiveRepositoryChange
		var clientID, projectionID, changeType string
		if err = rows.Scan(&change.Cursor, &clientID, &projectionID, &change.Source.Type, &change.Source.ID, &change.EventRole, &changeType); err != nil {
			return page, err
		}
		change.Source.MSPID = mspID
		change.Source.ClientID = clientID
		if changeType == "removed" || projectionID == "" {
			change.Type = calendar.LiveRemove
		} else {
			change.Type = calendar.LiveUpsert
			projection, loadErr := r.loadCalendarQueryProjection(ctx, mspID, projectionID)
			if errors.Is(loadErr, scope.ErrNotFound) {
				change.Type = calendar.LiveRemove
			} else if loadErr != nil {
				return page, loadErr
			} else {
				change.Projection = &projection
			}
		}
		page.Changes = append(page.Changes, change)
	}
	return page, rows.Err()
}

func (r *CalendarRepository) loadCalendarQueryProjection(ctx context.Context, mspID, projectionID string) (calendar.QueryProjection, error) {
	row := r.db.QueryRow(ctx, `SELECT projection.id::text,COALESCE(projection.client_id::text,''),projection.source_type,projection.source_id::text,projection.event_role,projection.source_role_key,projection.source_revision,projection.title,projection.all_day,projection.starts_on,projection.ends_on,projection.starts_at,projection.ends_at,COALESCE(projection.timezone,''),projection.scheduling_mode,projection.capacity_bearing,COALESCE(projection.owner_id::text,''),COALESCE(projection.assignee_id::text,''),projection.planned_minutes,projection.filter_dimensions,projection.recurrence_rule,projection.terminal_state,projection.health_state,projection.health_reasons,projection.health_rule_version,projection.health_evaluated_at,COALESCE((projection.health_inputs->>'hard_constraint')::boolean,false) OR EXISTS(SELECT 1 FROM jsonb_array_elements(projection.health_reasons) reason WHERE reason->>'code' IN('hard_constraint','ordinary_overbooking','approved_pto','non_working_time','protected_maintenance')) FROM calendar_event_projections projection WHERE projection.id=$1::uuid AND projection.msp_id=$2::uuid`, projectionID, mspID)
	result, err := scanCalendarQueryProjection(row, mspID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, scope.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	if result.Projection.Recurrence != nil {
		result.Exceptions, err = r.loadRecurrenceExceptions(ctx, mspID, projectionID)
	}
	return result, err
}

func (r *CalendarRepository) ResolveSchedulingProjection(ctx context.Context, principal authorization.Principal, projectionID, occurrenceKey string) (calendar.Projection, error) {
	projection, err := r.LoadDependencyProjection(ctx, projectionID)
	if err != nil {
		return calendar.Projection{}, err
	}
	if err = authorization.Authorize(principal, "calendar.schedule", projection.Source.ScopeTarget()); err != nil {
		return calendar.Projection{}, err
	}
	if occurrenceKey != "" && projection.Recurrence == nil {
		return calendar.Projection{}, calendar.ErrInvalidScheduleChange
	}
	return projection, nil
}

type storedSchedulingBindings struct {
	Bindings []calendar.RevisionBinding `json:"bindings"`
}

func (r *CalendarRepository) SaveSchedulingProposal(ctx context.Context, proposal calendar.SchedulingProposal) (err error) {
	if r == nil || r.db == nil || !internalid.ValidCanonical(proposal.ID) || !internalid.ValidCanonical(proposal.MSPID) || !internalid.ValidCanonical(proposal.ActorID) || (proposal.ClientID != "" && !internalid.ValidCanonical(proposal.ClientID)) || proposal.AuthorizationHash == "" || !proposal.ExpiresAt.After(proposal.CreatedAt) || len(proposal.Changes) == 0 {
		return calendar.ErrInvalidProposal
	}
	authHash, decodeErr := hex.DecodeString(proposal.AuthorizationHash)
	if decodeErr != nil || len(authHash) == 0 {
		return calendar.ErrInvalidProposal
	}
	bindings, marshalErr := json.Marshal(storedSchedulingBindings{Bindings: proposal.Bindings})
	if marshalErr != nil {
		return marshalErr
	}
	policyVersion := int64(1)
	for _, b := range proposal.Bindings {
		if b.Kind == calendar.RevisionPolicy && b.Version > policyVersion {
			policyVersion = b.Version
		}
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	_, err = tx.Exec(ctx, `INSERT INTO calendar_scheduling_proposals(id,msp_id,client_id,actor_id,state,authorization_context_hash,source_revision_bindings,conflict_policy_version,requires_override,required_reason,expires_at,version,created_at,updated_at)
VALUES($1::uuid,$2::uuid,NULLIF($3,'')::uuid,$4::uuid,'previewed',$5,$6::jsonb,$7,$8,$8,$9,1,$10,$10)`, proposal.ID, proposal.MSPID, proposal.ClientID, proposal.ActorID, authHash, bindings, policyVersion, proposal.RequiresReason, proposal.ExpiresAt, proposal.CreatedAt)
	if err != nil {
		return err
	}
	for ordinal, change := range proposal.Changes {
		if !internalid.ValidCanonical(change.ID) || !internalid.ValidCanonical(change.Requested.Source.ID) || change.Requested.SourceRevision < 1 {
			return calendar.ErrInvalidProposal
		}
		proposedValue, e := json.Marshal(struct {
			Requested calendar.RequestedChange  `json:"requested"`
			Required  bool                      `json:"required"`
			Schedule  calendar.ProposedSchedule `json:"schedule"`
		}{change.Requested, change.Required, change.Schedule})
		if e != nil {
			return e
		}
		changeConflicts := change.Conflicts
		if changeConflicts == nil {
			changeConflicts = []calendar.Conflict{}
		}
		conflicts, e := json.Marshal(changeConflicts)
		if e != nil {
			return e
		}
		blockedSources := change.BlockedSources
		if blockedSources == nil {
			blockedSources = []calendar.CascadeBlockedSource{}
		}
		dependencyImpact, e := json.Marshal(blockedSources)
		if e != nil {
			return e
		}
		clientKey := "global"
		if change.Requested.Source.ClientID != "" {
			clientKey = "client:" + change.Requested.Source.ClientID
		}
		changeRequiresOverride := false
		for _, conflict := range change.Conflicts {
			changeRequiresOverride = changeRequiresOverride || conflict.Severity == calendar.ConflictOverrideable || conflict.ReasonRequired
		}
		_, err = tx.Exec(ctx, `INSERT INTO calendar_proposal_changes(id,proposal_id,msp_id,client_id,client_scope_key,ordinal,source_type,source_id,event_role,source_revision,change_type,current_value,proposed_value,conflicts,dependency_impact,requires_override)
VALUES($1::uuid,$2::uuid,$3::uuid,NULLIF($4,'')::uuid,$5,$6,$7,$8::uuid,$9,$10,'reschedule','{}'::jsonb,$11::jsonb,$12::jsonb,$13::jsonb,$14)`, change.ID, proposal.ID, proposal.MSPID, change.Requested.Source.ClientID, clientKey, ordinal+1, change.Requested.Source.Type, change.Requested.Source.ID, change.Requested.EventRole, change.Requested.SourceRevision, proposedValue, conflicts, dependencyImpact, changeRequiresOverride)
		if err != nil {
			return err
		}
	}
	err = tx.Commit(ctx)
	return err
}

func (r *CalendarRepository) LoadSchedulingProposal(ctx context.Context, mspID, proposalID string) (calendar.SchedulingProposal, error) {
	var p calendar.SchedulingProposal
	var authHash, bindings []byte
	err := r.db.QueryRow(ctx, `SELECT id::text,msp_id::text,COALESCE(client_id::text,''),actor_id::text,state,encode(authorization_context_hash,'hex'),source_revision_bindings,requires_override,created_at,expires_at,version FROM calendar_scheduling_proposals WHERE id=$1::uuid AND msp_id=$2::uuid`, proposalID, mspID).Scan(&p.ID, &p.MSPID, &p.ClientID, &p.ActorID, &p.State, &p.AuthorizationHash, &bindings, &p.RequiresReason, &p.CreatedAt, &p.ExpiresAt, &p.Version)
	_ = authHash
	if errors.Is(err, pgx.ErrNoRows) {
		return p, scope.ErrNotFound
	}
	if err != nil {
		return p, err
	}
	var stored storedSchedulingBindings
	if err = json.Unmarshal(bindings, &stored); err != nil {
		return p, err
	}
	p.Bindings = stored.Bindings
	rows, err := r.db.Query(ctx, `SELECT id::text,source_type,source_id::text,event_role,source_revision,proposed_value,conflicts,dependency_impact FROM calendar_proposal_changes WHERE proposal_id=$1::uuid AND msp_id=$2::uuid ORDER BY ordinal`, proposalID, mspID)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var change calendar.ProposedChange
		var sourceType, sourceID, eventRole string
		var revision int64
		var proposed, conflicts, dependencyImpact []byte
		if err = rows.Scan(&change.ID, &sourceType, &sourceID, &eventRole, &revision, &proposed, &conflicts, &dependencyImpact); err != nil {
			return p, err
		}
		var value struct {
			Requested calendar.RequestedChange  `json:"requested"`
			Required  bool                      `json:"required"`
			Schedule  calendar.ProposedSchedule `json:"schedule"`
		}
		if err = json.Unmarshal(proposed, &value); err != nil {
			return p, err
		}
		change.Requested = value.Requested
		change.Requested.Source.Type = sourceType
		change.Requested.Source.ID = sourceID
		change.Requested.EventRole = eventRole
		change.Requested.SourceRevision = revision
		change.Required = value.Required
		change.Schedule = value.Schedule
		if err = json.Unmarshal(conflicts, &change.Conflicts); err != nil {
			return p, err
		}
		if err = json.Unmarshal(dependencyImpact, &change.BlockedSources); err != nil {
			return p, err
		}
		p.Changes = append(p.Changes, change)
		p.Conflicts = append(p.Conflicts, change.Conflicts...)
		p.BlockedSources = append(p.BlockedSources, change.BlockedSources...)
	}
	return p, rows.Err()
}

func (r *CalendarRepository) AuthorizeScheduling(ctx context.Context, principal authorization.Principal, proposed calendar.ProposedSchedule, evaluatedAt time.Time) error {
	if r == nil || r.db == nil || !internalid.ValidCanonical(principal.ID) || !internalid.ValidCanonical(principal.Scope.MSPID) || !proposed.Interval.End.After(proposed.Interval.Start) {
		return calendar.ErrInvalidScheduleChange
	}
	if evaluatedAt.IsZero() {
		return calendar.ErrInvalidScheduleChange
	}
	if err := authorization.AuthorizeAt(principal, "calendar.schedule", scope.Target{MSPID: principal.Scope.MSPID, ClientID: proposed.ClientID}, evaluatedAt); err != nil {
		return err
	}
	if proposed.TechnicianID == "" {
		return nil
	}
	if !internalid.ValidCanonical(proposed.TechnicianID) {
		return calendar.ErrInvalidScheduleChange
	}
	var ok bool
	if err := r.db.QueryRow(ctx, schedulingWorkforceAuthoritySQL, principal.ID, principal.Scope.MSPID, proposed.TechnicianID, evaluatedAt.UTC()).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return authorization.ErrForbidden
	}
	return nil
}

const schedulingWorkforceAuthoritySQL = `SELECT EXISTS(
SELECT 1 FROM technicians target
WHERE target.id=$3::uuid AND target.msp_id=$2::uuid AND target.lifecycle_state='active'
AND (
  target.id=$1::uuid
  OR EXISTS(
    SELECT 1 FROM team_memberships target_member
    JOIN team_memberships actor_member ON actor_member.team_id=target_member.team_id AND actor_member.msp_id=target_member.msp_id AND actor_member.technician_id=$1::uuid AND actor_member.lifecycle_state='active'
    WHERE target_member.technician_id=target.id AND target_member.msp_id=target.msp_id AND target_member.lifecycle_state='active'
  )
  OR EXISTS(
    SELECT 1 FROM team_memberships target_member
    JOIN teams team ON team.id=target_member.team_id AND team.msp_id=target_member.msp_id AND team.workforce_manager_id=$1::uuid
    WHERE target_member.technician_id=target.id AND target_member.msp_id=target.msp_id AND target_member.lifecycle_state='active'
  )
  OR EXISTS(
    SELECT 1 FROM role_assignments assignment
    JOIN role_capabilities capability ON capability.role_id=assignment.role_id AND capability.msp_id=assignment.msp_id AND capability.capability='calendar.workforce.manage'
    WHERE assignment.technician_id=$1::uuid AND assignment.msp_id=$2::uuid AND assignment.client_id IS NULL AND (assignment.expires_at IS NULL OR assignment.expires_at>$4)
  )
))`

func (r *CalendarRepository) ValidateSchedulingBindings(ctx context.Context, principal authorization.Principal, bindings []calendar.RevisionBinding) error {
	for _, binding := range bindings {
		current, err := r.schedulingBindingVersion(ctx, r.db, binding)
		if err != nil || current != binding.Version {
			return calendar.ErrStaleProposal
		}
	}
	return nil
}

func (r *CalendarRepository) SchedulingRevisionBindings(ctx context.Context, principal authorization.Principal, proposed calendar.ProposedSchedule) ([]calendar.RevisionBinding, error) {
	bindings := []calendar.RevisionBinding{}
	if proposed.ClientID != "" {
		binding := calendar.RevisionBinding{Kind: calendar.RevisionDependencyScope, ID: principal.Scope.MSPID + "|" + proposed.ClientID}
		version, err := r.schedulingBindingVersion(ctx, r.db, binding)
		if err != nil {
			return nil, err
		}
		binding.Version = version
		bindings = append(bindings, binding)
	}
	policyScopes := [][2]string{{string(calendar.ConflictScopeMSP), ""}}
	if proposed.TeamID != "" {
		policyScopes = append(policyScopes, [2]string{string(calendar.ConflictScopeTeam), proposed.TeamID})
	}
	if proposed.TechnicianID != "" {
		policyScopes = append(policyScopes, [2]string{string(calendar.ConflictScopeTechnician), proposed.TechnicianID})
	}
	for _, policyScope := range policyScopes {
		binding := calendar.RevisionBinding{Kind: calendar.RevisionPolicyScope, ID: strings.Join([]string{principal.Scope.MSPID, policyScope[0], policyScope[1], proposed.Interval.Start.UTC().Format(time.RFC3339Nano)}, "|")}
		version, err := r.schedulingBindingVersion(ctx, r.db, binding)
		if err != nil {
			return nil, err
		}
		binding.Version = version
		bindings = append(bindings, binding)
	}
	if proposed.TechnicianID != "" {
		binding := calendar.RevisionBinding{Kind: calendar.RevisionSchedule, ID: proposed.TechnicianID}
		version, err := r.schedulingBindingVersion(ctx, r.db, binding)
		if err != nil {
			return nil, err
		}
		binding.Version = version
		bindings = append(bindings, binding)
	}
	return bindings, nil
}

func (r *CalendarRepository) schedulingBindingVersion(ctx context.Context, q queryRower, binding calendar.RevisionBinding) (int64, error) {
	switch binding.Kind {
	case calendar.RevisionSource:
		parts := strings.Split(binding.ID, "/")
		if len(parts) != 4 {
			return 0, calendar.ErrStaleProposal
		}
		return r.currentCalendarSourceRevision(ctx, q, calendar.SourceRef{MSPID: parts[0], ClientID: parts[1], Type: parts[2], ID: parts[3]}, false)
	case calendar.RevisionDependency:
		var version int64
		err := q.QueryRow(ctx, `SELECT version FROM calendar_dependencies WHERE id=$1::uuid`, binding.ID).Scan(&version)
		return version, err
	case calendar.RevisionPolicy:
		var version int64
		err := q.QueryRow(ctx, `SELECT version FROM calendar_conflict_policies WHERE id=$1::uuid ORDER BY version DESC LIMIT 1`, binding.ID).Scan(&version)
		return version, err
	case calendar.RevisionSchedule:
		var version int64
		err := q.QueryRow(ctx, `SELECT COALESCE(MAX(version),0) FROM technician_schedule_versions WHERE technician_id=$1::uuid AND lifecycle_state='active'`, binding.ID).Scan(&version)
		return version, err
	case calendar.RevisionDependencyScope:
		parts := strings.Split(binding.ID, "|")
		if len(parts) != 2 || !internalid.ValidCanonical(parts[0]) || !internalid.ValidCanonical(parts[1]) {
			return 0, calendar.ErrStaleProposal
		}
		var version int64
		err := q.QueryRow(ctx, `SELECT ('x'||substr(md5(COALESCE(string_agg(id::text||':'||version::text,',' ORDER BY id),'')),1,15))::bit(60)::bigint FROM calendar_dependencies WHERE msp_id=$1::uuid AND client_id=$2::uuid`, parts[0], parts[1]).Scan(&version)
		return version, err
	case calendar.RevisionPolicyScope:
		parts := strings.Split(binding.ID, "|")
		if len(parts) != 4 || !internalid.ValidCanonical(parts[0]) {
			return 0, calendar.ErrStaleProposal
		}
		at, err := time.Parse(time.RFC3339Nano, parts[3])
		if err != nil {
			return 0, calendar.ErrStaleProposal
		}
		teamID, technicianID := "", ""
		switch calendar.ConflictPolicyScopeType(parts[1]) {
		case calendar.ConflictScopeMSP:
			if parts[2] != "" {
				return 0, calendar.ErrStaleProposal
			}
		case calendar.ConflictScopeTeam:
			teamID = parts[2]
		case calendar.ConflictScopeTechnician:
			technicianID = parts[2]
		default:
			return 0, calendar.ErrStaleProposal
		}
		var version int64
		err = q.QueryRow(ctx, `SELECT ('x'||substr(md5(COALESCE(string_agg(id::text||':'||version::text||':'||lifecycle_state,',' ORDER BY id),'')),1,15))::bit(60)::bigint FROM calendar_conflict_policies WHERE msp_id=$1::uuid AND scope_type=$2 AND team_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid AND technician_id IS NOT DISTINCT FROM NULLIF($4,'')::uuid AND lifecycle_state='active' AND effective_from<=$5 AND (effective_through IS NULL OR effective_through>$5)`, parts[0], parts[1], teamID, technicianID, at).Scan(&version)
		return version, err
	default:
		return 0, calendar.ErrStaleProposal
	}
}

func (r *CalendarRepository) ApplySchedulingProposalAtomic(ctx context.Context, request calendar.ScheduleApplyRequest, apply func(context.Context, calendar.ScheduleTx) error) (result calendar.AppliedProposal, err error) {
	if r == nil || r.db == nil || apply == nil || !internalid.ValidCanonical(request.Proposal.ID) || !internalid.ValidCanonical(request.Principal.ID) {
		return result, calendar.ErrInvalidProposal
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	var actor, state, storedHash string
	var expires time.Time
	var storedBindingsJSON []byte
	err = tx.QueryRow(ctx, `SELECT actor_id::text,state,encode(authorization_context_hash,'hex'),expires_at,source_revision_bindings FROM calendar_scheduling_proposals WHERE id=$1::uuid AND msp_id=$2::uuid FOR UPDATE`, request.Proposal.ID, request.Principal.Scope.MSPID).Scan(&actor, &state, &storedHash, &expires, &storedBindingsJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, scope.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	if actor != request.Principal.ID {
		return result, calendar.ErrProposalActorMismatch
	}
	if request.EvaluatedAt.IsZero() || state != "previewed" || !request.EvaluatedAt.UTC().Before(expires) || storedHash != calendar.AuthorizationFingerprint(request.Principal) {
		return result, calendar.ErrStaleProposal
	}
	var storedBindings storedSchedulingBindings
	if err = json.Unmarshal(storedBindingsJSON, &storedBindings); err != nil {
		return result, calendar.ErrStaleProposal
	}
	request.Changes, err = r.loadAndValidateLockedProposalChanges(ctx, tx, request, storedBindings.Bindings)
	if err != nil {
		return result, err
	}
	request.Proposal.Bindings = storedBindings.Bindings
	if err = r.revalidateSchedulingAuthority(ctx, tx, request.Principal, request.Changes, request.EvaluatedAt); err != nil {
		return result, err
	}
	if err = r.revalidateSchedulingBindingsTx(ctx, tx, request.Proposal.Bindings); err != nil {
		return result, err
	}
	uowtx := &calendarScheduleTx{repository: r, tx: tx, principal: request.Principal, schedules: map[string]calendar.ProposedSchedule{}}
	for _, change := range request.Changes {
		uowtx.schedules[change.Prepared.ID] = change.Schedule
	}
	if err = apply(ctx, uowtx); err != nil {
		return result, err
	}
	if uowtx.correlationID == "" {
		return result, calendar.ErrInvalidProposal
	}
	if err = emitAcceptedScheduleCalendarFacts(ctx, tx, request, uowtx.correlationID, uowtx.appliedAt); err != nil {
		return result, err
	}
	selectedRequiresOverride := len(request.OverridePolicyIDs) > 0
	for _, change := range request.Changes {
		for _, conflict := range change.Conflicts {
			selectedRequiresOverride = selectedRequiresOverride || conflict.Severity == calendar.ConflictOverrideable || conflict.ReasonRequired
		}
	}
	tag, e := tx.Exec(ctx, `UPDATE calendar_scheduling_proposals SET state='applied',requires_override=$7,required_reason=$7,approval_reason=$3,approved_at=$4,approved_by=$5::uuid,applied_at=$4,applied_audit_correlation_id=$6::uuid,version=version+1,updated_at=$4 WHERE id=$1::uuid AND msp_id=$2::uuid AND state='previewed'`, request.Proposal.ID, request.Principal.Scope.MSPID, strings.TrimSpace(request.OverrideReason), uowtx.appliedAt, request.Principal.ID, uowtx.correlationID, selectedRequiresOverride)
	if e != nil {
		return result, e
	}
	if tag.RowsAffected() != 1 {
		return result, calendar.ErrStaleProposal
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	return calendar.AppliedProposal{ProposalID: request.Proposal.ID, CorrelationID: uowtx.correlationID, AppliedAt: uowtx.appliedAt, Changes: append([]calendar.ProposedChange(nil), request.Changes...)}, nil
}

func (r *CalendarRepository) loadAndValidateLockedProposalChanges(ctx context.Context, tx transaction, request calendar.ScheduleApplyRequest, bindings []calendar.RevisionBinding) ([]calendar.ProposedChange, error) {
	rows, err := tx.Query(ctx, `SELECT id::text,source_type,source_id::text,event_role,source_revision,proposed_value,conflicts,dependency_impact FROM calendar_proposal_changes WHERE proposal_id=$1::uuid AND msp_id=$2::uuid ORDER BY ordinal FOR UPDATE`, request.Proposal.ID, request.Principal.Scope.MSPID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	claimed := make(map[string]calendar.ProposedChange, len(request.Changes))
	for _, change := range request.Changes {
		if change.ID == "" {
			return nil, calendar.ErrStaleProposal
		}
		if _, duplicate := claimed[change.ID]; duplicate {
			return nil, calendar.ErrStaleProposal
		}
		claimed[change.ID] = change
	}
	accepted := make([]calendar.ProposedChange, 0, len(request.Changes))
	seen := map[string]struct{}{}
	for rows.Next() {
		var stored calendar.ProposedChange
		var sourceType, sourceID, eventRole string
		var sourceRevision int64
		var proposedJSON, conflictsJSON, blockedJSON []byte
		if err = rows.Scan(&stored.ID, &sourceType, &sourceID, &eventRole, &sourceRevision, &proposedJSON, &conflictsJSON, &blockedJSON); err != nil {
			return nil, err
		}
		var value struct {
			Requested calendar.RequestedChange  `json:"requested"`
			Required  bool                      `json:"required"`
			Schedule  calendar.ProposedSchedule `json:"schedule"`
		}
		if json.Unmarshal(proposedJSON, &value) != nil || json.Unmarshal(conflictsJSON, &stored.Conflicts) != nil || json.Unmarshal(blockedJSON, &stored.BlockedSources) != nil {
			return nil, calendar.ErrStaleProposal
		}
		stored.Required, stored.Requested, stored.Schedule = value.Required, value.Requested, value.Schedule
		stored.Requested.Source.Type, stored.Requested.Source.ID = sourceType, sourceID
		stored.Requested.EventRole, stored.Requested.SourceRevision = eventRole, sourceRevision
		candidate, selected := claimed[stored.ID]
		if !selected {
			if stored.Required {
				return nil, calendar.ErrStaleProposal
			}
			continue
		}
		conflictsEqual := len(candidate.Conflicts) == 0 && len(stored.Conflicts) == 0 || reflect.DeepEqual(candidate.Conflicts, stored.Conflicts)
		blockedEqual := len(candidate.BlockedSources) == 0 && len(stored.BlockedSources) == 0 || reflect.DeepEqual(candidate.BlockedSources, stored.BlockedSources)
		if candidate.Required != stored.Required || !reflect.DeepEqual(candidate.Requested, stored.Requested) || !reflect.DeepEqual(candidate.Schedule, stored.Schedule) || !conflictsEqual || !blockedEqual || candidate.Prepared.ID != stored.ID || candidate.Prepared.Source != stored.Requested.Source || candidate.Prepared.EventRole != stored.Requested.EventRole || candidate.Prepared.SourceRoleKey != stored.Requested.SourceRoleKey || candidate.Prepared.ExpectedSourceRevision != stored.Requested.SourceRevision || !reflect.DeepEqual(candidate.Prepared.Request, stored.Requested) {
			return nil, calendar.ErrStaleProposal
		}
		stored.Prepared = candidate.Prepared
		accepted = append(accepted, stored)
		seen[stored.ID] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(seen) != len(claimed) || !reflect.DeepEqual(request.Proposal.Bindings, bindings) {
		return nil, calendar.ErrStaleProposal
	}
	return accepted, nil
}

type calendarScheduleTx struct {
	repository    *CalendarRepository
	tx            transaction
	principal     authorization.Principal
	schedules     map[string]calendar.ProposedSchedule
	correlationID string
	appliedAt     time.Time
}

func (t *calendarScheduleTx) RevalidateSchedulingChange(ctx context.Context, _ authorization.Principal, prepared calendar.PreparedChange) error {
	if t == nil || t.tx == nil {
		return calendar.ErrInvalidScheduleChange
	}
	current, err := t.repository.currentCalendarSourceRevision(ctx, t.tx, prepared.Source, true)
	if err != nil || current != prepared.ExpectedSourceRevision {
		return calendar.ErrStaleProposal
	}
	if err = authorization.Authorize(t.principal, "calendar.schedule", prepared.Source.ScopeTarget()); err != nil {
		return err
	}
	return nil
}
func (t *calendarScheduleTx) ApplyTypedScheduleMutation(ctx context.Context, prepared calendar.PreparedChange, evidence mutation.Evidence) error {
	if evidence.ActorID != t.principal.ID || !internalid.ValidCanonical(evidence.CorrelationID) {
		return calendar.ErrInvalidScheduleChange
	}
	t.correlationID = evidence.CorrelationID
	t.appliedAt = evidence.OccurredAt
	var version int64
	var subjectType string
	switch accepted := prepared.Mutation.(type) {
	case workrecords.ScheduleMutation:
		if accepted.RecordID != prepared.Source.ID || accepted.MSPID != prepared.Source.MSPID || accepted.ClientID != prepared.Source.ClientID || accepted.ProjectionID != prepared.Request.ProjectionID || !reflect.DeepEqual(accepted.Interval, prepared.RequestInterval()) {
			return calendar.ErrStaleProposal
		}
		if err := workrecords.ValidateScheduleMutation(t.principal, accepted); err != nil {
			return err
		}
		if accepted.Interval.AllDay {
			return calendar.ErrInvalidScheduleChange
		}
		version = accepted.ExpectedVersion + 1
		subjectType = "work_record"
		if err := t.applyWorkRecord(ctx, prepared, accepted); err != nil {
			return err
		}
	case tasks.ScheduleMutation:
		if accepted.TaskID != prepared.Source.ID || accepted.MSPID != prepared.Source.MSPID || accepted.ClientID != prepared.Source.ClientID || accepted.ProjectionID != prepared.Request.ProjectionID || !reflect.DeepEqual(accepted.Interval, prepared.RequestInterval()) {
			return calendar.ErrStaleProposal
		}
		if err := tasks.ValidateScheduleMutation(t.principal, accepted); err != nil {
			return err
		}
		if accepted.Interval.AllDay {
			return calendar.ErrInvalidScheduleChange
		}
		version = accepted.ExpectedVersion + 1
		subjectType = "task"
		if err := t.applyTask(ctx, prepared, accepted); err != nil {
			return err
		}
	case projects.ScheduleMutation:
		if accepted.SourceID != prepared.Source.ID || accepted.SourceType != prepared.Source.Type || accepted.MSPID != prepared.Source.MSPID || accepted.ClientID != prepared.Source.ClientID || accepted.EventRole != prepared.EventRole || accepted.ProjectionID != prepared.Request.ProjectionID || !reflect.DeepEqual(accepted.Interval, prepared.RequestInterval()) {
			return calendar.ErrStaleProposal
		}
		if err := projects.ValidateScheduleMutation(t.principal, accepted); err != nil {
			return err
		}
		version = accepted.ExpectedVersion + 1
		subjectType = map[string]string{"project": "project", "phase": "phase", "milestone": "project_milestone", "resource_plan": "resource_plan"}[accepted.SourceType]
		if subjectType == "" {
			return calendar.ErrInvalidScheduleChange
		}
		if err := t.applyProjectSchedule(ctx, prepared, accepted); err != nil {
			return err
		}
	default:
		return calendar.ErrInvalidScheduleChange
	}
	auditID, eventID := schedulingFactID(prepared.ID, "audit"), schedulingFactID(prepared.ID, "event")
	overrideContext := map[string]any{"override_reason": evidence.Reason, "overridden_policy_ids": evidence.OverriddenPolicyIDs}
	audit := mutation.AuditRecord{ID: auditID, OccurredAt: evidence.OccurredAt, MSPID: prepared.Source.MSPID, ClientID: prepared.Source.ClientID, ActorType: "technician", ActorID: evidence.ActorID, Action: subjectType + ".scheduled", SubjectType: subjectType, SubjectID: prepared.Source.ID, SubjectVersion: version, Source: "calendar", Reason: evidence.Reason, CorrelationID: evidence.CorrelationID, SafeDiff: overrideContext, AuthorizationContext: overrideContext}
	event := mutation.EventRecord{EventID: eventID, EventType: subjectType + ".scheduled", SchemaVersion: 1, OccurredAt: evidence.OccurredAt, MSPID: prepared.Source.MSPID, ClientID: prepared.Source.ClientID, ActorType: "technician", ActorID: evidence.ActorID, SubjectType: subjectType, SubjectID: prepared.Source.ID, SubjectVersion: version, Source: "calendar", CorrelationID: evidence.CorrelationID, Data: map[string]any{"event_role": prepared.EventRole, "occurrence_scope": prepared.Request.OccurrenceScope, "overridden_policy_ids": evidence.OverriddenPolicyIDs}}
	if err := writeAuditOnly(ctx, t.tx, audit); err != nil {
		return err
	}
	return writeEventOnly(ctx, t.tx, event)
}

func (t *calendarScheduleTx) applyProjectSchedule(ctx context.Context, p calendar.PreparedChange, m projects.ScheduleMutation) error {
	if m.SourceType == "milestone" {
		return t.applyMilestone(ctx, p, m)
	}
	if !m.Interval.AllDay || m.Interval.StartsOn == nil {
		return calendar.ErrInvalidScheduleChange
	}
	if m.SourceType == "resource_plan" {
		if m.EventRole != "allocation" || m.Interval.EndsOn == nil {
			return calendar.ErrInvalidScheduleChange
		}
		tag, err := t.tx.Exec(ctx, `UPDATE resource_plans SET starts_on=$5::date,ends_on=$6::date,version=version+1 WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid AND version=$4`, m.SourceID, m.MSPID, m.ClientID, m.ExpectedVersion, m.Interval.StartsOn, m.Interval.EndsOn)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return calendar.ErrStaleProposal
		}
		return nil
	}
	column := "planned_start"
	if m.EventRole == "planned_end" {
		column = "planned_end"
	} else if m.EventRole != "planned_start" {
		return calendar.ErrInvalidScheduleChange
	}
	var query string
	args := []any{m.SourceID, m.MSPID, m.ClientID, m.ExpectedVersion, m.Interval.StartsOn}
	switch m.SourceType {
	case "project":
		query = fmt.Sprintf(`UPDATE projects SET %s=$5::date,version=version+1,updated_at=$6,updated_by=$7::uuid WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid AND version=$4`, column)
		args = append(args, t.appliedAt, t.principal.ID)
	case "phase":
		query = fmt.Sprintf(`UPDATE phases SET %s=$5::date,version=version+1 WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid AND version=$4`, column)
	default:
		return calendar.ErrInvalidScheduleChange
	}
	tag, err := t.tx.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return calendar.ErrStaleProposal
	}
	return nil
}

func (t *calendarScheduleTx) applyWorkRecord(ctx context.Context, p calendar.PreparedChange, m workrecords.ScheduleMutation) error {
	if p.Request.OccurrenceScope == calendar.ThisOccurrence || p.Request.OccurrenceScope == calendar.ThisAndFuture {
		return t.applyOccurrence(ctx, p, m.ExpectedVersion)
	}
	recurrence, e := marshalOptionalJSON(m.Recurrence)
	if e != nil {
		return e
	}
	tag, e := t.tx.Exec(ctx, `UPDATE work_records SET scheduled_starts_at=$5,scheduled_ends_at=$6,schedule_timezone=$7,scheduling_mode=$8,planned_effort_minutes=$9,schedule_recurrence=$10::jsonb,version=version+1,updated_at=$11,updated_by=$12::uuid WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid AND version=$4`, m.RecordID, m.MSPID, m.ClientID, m.ExpectedVersion, m.Interval.StartsAt, m.Interval.EndsAt, m.Interval.Timezone, m.Mode, m.PlannedMinutes, recurrence, t.appliedAt, t.principal.ID)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return calendar.ErrStaleProposal
	}
	return nil
}
func (t *calendarScheduleTx) applyTask(ctx context.Context, p calendar.PreparedChange, m tasks.ScheduleMutation) error {
	if p.Request.OccurrenceScope == calendar.ThisOccurrence || p.Request.OccurrenceScope == calendar.ThisAndFuture {
		return t.applyOccurrence(ctx, p, m.ExpectedVersion)
	}
	recurrence, e := marshalOptionalJSON(m.Recurrence)
	if e != nil {
		return e
	}
	tag, e := t.tx.Exec(ctx, `UPDATE tasks SET scheduled_starts_at=$5,scheduled_ends_at=$6,schedule_timezone=$7,scheduling_mode=$8,schedule_recurrence=$9::jsonb,version=version+1,updated_at=$10,updated_by=$11::uuid WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid AND version=$4`, m.TaskID, m.MSPID, m.ClientID, m.ExpectedVersion, m.Interval.StartsAt, m.Interval.EndsAt, m.Interval.Timezone, m.Mode, recurrence, t.appliedAt, t.principal.ID)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return calendar.ErrStaleProposal
	}
	return nil
}
func (t *calendarScheduleTx) applyMilestone(ctx context.Context, p calendar.PreparedChange, m projects.ScheduleMutation) error {
	if p.Request.OccurrenceScope == calendar.ThisOccurrence || p.Request.OccurrenceScope == calendar.ThisAndFuture {
		return t.applyOccurrence(ctx, p, m.ExpectedVersion)
	}
	recurrence, e := marshalOptionalJSON(m.Recurrence)
	if e != nil {
		return e
	}
	tag, e := t.tx.Exec(ctx, `UPDATE project_milestones SET all_day=$5,starts_on=$6,ends_on=$7,starts_at=$8,ends_at=$9,timezone=NULLIF($10,''),recurrence_rule=$11::jsonb,version=version+1,updated_at=$12,updated_by=$13::uuid WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid AND version=$4`, m.SourceID, m.MSPID, m.ClientID, m.ExpectedVersion, m.Interval.AllDay, m.Interval.StartsOn, m.Interval.EndsOn, m.Interval.StartsAt, m.Interval.EndsAt, m.Interval.Timezone, recurrence, t.appliedAt, t.principal.ID)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return calendar.ErrStaleProposal
	}
	return nil
}
func (t *calendarScheduleTx) applyOccurrence(ctx context.Context, p calendar.PreparedChange, baseVersion int64) error {
	r := p.Request
	clientKey := "global"
	if p.Source.ClientID != "" {
		clientKey = "client:" + p.Source.ClientID
	}
	tag, e := t.tx.Exec(ctx, `INSERT INTO calendar_recurrence_exceptions(id,msp_id,client_id,client_scope_key,projection_id,original_local_key,state,occurrence_scope,starts_on,ends_on,starts_at,ends_at,timezone,all_day,source_revision,version,created_at,created_by,updated_at,updated_by) VALUES($1::uuid,$2::uuid,NULLIF($3,'')::uuid,$4,$5::uuid,$6,'rescheduled',$7,$8,$9,$10,$11,NULLIF($12,''),$13,$14,1,$15,$16::uuid,$15,$16::uuid) ON CONFLICT(msp_id,projection_id,original_local_key) DO UPDATE SET state='rescheduled',occurrence_scope=EXCLUDED.occurrence_scope,starts_on=EXCLUDED.starts_on,ends_on=EXCLUDED.ends_on,starts_at=EXCLUDED.starts_at,ends_at=EXCLUDED.ends_at,timezone=EXCLUDED.timezone,all_day=EXCLUDED.all_day,source_revision=EXCLUDED.source_revision,version=calendar_recurrence_exceptions.version+1,updated_at=EXCLUDED.updated_at,updated_by=EXCLUDED.updated_by`, p.ID, p.Source.MSPID, p.Source.ClientID, clientKey, r.ProjectionID, r.OccurrenceKey, r.OccurrenceScope, r.StartsOn, r.EndsOn, r.StartsAt, r.EndsAt, r.Timezone, r.AllDay, p.ExpectedSourceRevision+1, t.appliedAt, t.principal.ID)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return calendar.ErrStaleProposal
	}
	var table string
	switch p.Source.Type {
	case "work_record":
		table = "work_records"
	case "task":
		table = "tasks"
	case "milestone":
		table = "project_milestones"
	default:
		return calendar.ErrInvalidScheduleChange
	}
	query := fmt.Sprintf(`UPDATE %s SET version=version+1,updated_at=$5,updated_by=$6::uuid WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid AND version=$4`, table)
	tag, e = t.tx.Exec(ctx, query, p.Source.ID, p.Source.MSPID, p.Source.ClientID, baseVersion, t.appliedAt, t.principal.ID)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return calendar.ErrStaleProposal
	}
	return nil
}

func (r *CalendarRepository) revalidateSchedulingAuthority(ctx context.Context, tx transaction, p authorization.Principal, changes []calendar.ProposedChange, evaluatedAt time.Time) error {
	capabilities := map[string]string{"work_record": "work_record.edit", "task": "task.edit", "project": "project.edit", "phase": "project.edit", "milestone": "project.edit", "resource_plan": "project.edit"}
	for _, change := range changes {
		capability := capabilities[change.Prepared.Source.Type]
		if capability == "" {
			return calendar.ErrReadOnlyEventRole
		}
		if err := authorization.AuthorizeAt(p, capability, change.Prepared.Source.ScopeTarget(), evaluatedAt); err != nil {
			return err
		}
		var ok bool
		err := tx.QueryRow(ctx, `SELECT COUNT(DISTINCT capability.capability)=2 FROM technicians actor JOIN role_assignments assignment ON assignment.technician_id=actor.id AND assignment.msp_id=actor.msp_id JOIN role_capabilities capability ON capability.role_id=assignment.role_id AND capability.msp_id=assignment.msp_id WHERE actor.id=$1::uuid AND actor.msp_id=$2::uuid AND actor.lifecycle_state='active' AND capability.capability=ANY($3) AND (assignment.client_id IS NULL OR assignment.client_id IS NOT DISTINCT FROM NULLIF($4,'')::uuid) AND (assignment.expires_at IS NULL OR assignment.expires_at>$5)`, p.ID, p.Scope.MSPID, []string{"calendar.schedule", capability}, change.Prepared.Source.ClientID, evaluatedAt).Scan(&ok)
		if err != nil {
			return err
		}
		if !ok {
			return authorization.ErrForbidden
		}
		if technician := change.Schedule.TechnicianID; technician != "" {
			err = tx.QueryRow(ctx, schedulingWorkforceAuthoritySQL, p.ID, p.Scope.MSPID, technician, evaluatedAt).Scan(&ok)
			if err != nil {
				return err
			}
			if !ok {
				return authorization.ErrForbidden
			}
		}
	}
	return nil
}
func (r *CalendarRepository) revalidateSchedulingBindingsTx(ctx context.Context, tx transaction, bindings []calendar.RevisionBinding) error {
	for _, b := range bindings {
		switch b.Kind {
		case calendar.RevisionSource:
			parts := strings.Split(b.ID, "/")
			if len(parts) != 4 {
				return calendar.ErrStaleProposal
			}
			v, e := r.currentCalendarSourceRevision(ctx, tx, calendar.SourceRef{MSPID: parts[0], ClientID: parts[1], Type: parts[2], ID: parts[3]}, true)
			if e != nil || v != b.Version {
				return calendar.ErrStaleProposal
			}
		case calendar.RevisionDependency:
			var v int64
			if !internalid.ValidCanonical(b.ID) || tx.QueryRow(ctx, `SELECT version FROM calendar_dependencies WHERE id=$1::uuid FOR UPDATE`, b.ID).Scan(&v) != nil || v != b.Version {
				return calendar.ErrStaleProposal
			}
		case calendar.RevisionPolicy:
			var v int64
			if !internalid.ValidCanonical(b.ID) || tx.QueryRow(ctx, `SELECT version FROM calendar_conflict_policies WHERE id=$1::uuid ORDER BY version DESC LIMIT 1 FOR UPDATE`, b.ID).Scan(&v) != nil || v != b.Version {
				return calendar.ErrStaleProposal
			}
		case calendar.RevisionSchedule:
			var v int64
			if !internalid.ValidCanonical(b.ID) || tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0) FROM technician_schedule_versions WHERE technician_id=$1::uuid AND lifecycle_state='active'`, b.ID).Scan(&v) != nil || v != b.Version {
				return calendar.ErrStaleProposal
			}
		case calendar.RevisionDependencyScope:
			parts := strings.Split(b.ID, "|")
			if len(parts) != 2 {
				return calendar.ErrStaleProposal
			}
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('calendar-dependency:' || $1 || ':' || $2,0))`, parts[0], parts[1]); err != nil {
				return err
			}
			v, err := r.schedulingBindingVersion(ctx, tx, b)
			if err != nil || v != b.Version {
				return calendar.ErrStaleProposal
			}
		case calendar.RevisionPolicyScope:
			parts := strings.Split(b.ID, "|")
			if len(parts) != 4 {
				return calendar.ErrStaleProposal
			}
			teamID, technicianID := "", ""
			if parts[1] == string(calendar.ConflictScopeTeam) {
				teamID = parts[2]
			} else if parts[1] == string(calendar.ConflictScopeTechnician) {
				technicianID = parts[2]
			}
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('calendar-conflict-policy:' || $1 || ':' || $2 || ':' || $3 || ':' || $4,0))`, parts[0], parts[1], teamID, technicianID); err != nil {
				return err
			}
			v, err := r.schedulingBindingVersion(ctx, tx, b)
			if err != nil || v != b.Version {
				return calendar.ErrStaleProposal
			}
		}
	}
	return nil
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) row
}

func (r *CalendarRepository) currentCalendarSourceRevision(ctx context.Context, q queryRower, ref calendar.SourceRef, lock bool) (int64, error) {
	suffix := ""
	var v int64
	var query string
	switch ref.Type {
	case "work_record":
		if lock {
			suffix = " FOR UPDATE OF work"
		}
		query = `SELECT work.version+COALESCE(classification.version,0)+COALESCE(sla.version,0) FROM work_records work LEFT JOIN classification_object_versions classification ON classification.msp_id=work.msp_id AND classification.client_id=work.client_id AND classification.object_type='work_record' AND classification.object_id=work.id LEFT JOIN LATERAL(SELECT MAX(version) version FROM work_record_slas WHERE msp_id=work.msp_id AND client_id=work.client_id AND work_record_id=work.id)sla ON true WHERE work.id=$1::uuid AND work.msp_id=$2::uuid AND work.client_id=$3::uuid`
	case "task":
		if lock {
			suffix = " FOR UPDATE OF task"
		}
		query = `SELECT task.version+COALESCE(classification.version,0) FROM tasks task LEFT JOIN classification_object_versions classification ON classification.msp_id=task.msp_id AND classification.client_id=task.client_id AND classification.object_type='task' AND classification.object_id=task.id WHERE task.id=$1::uuid AND task.msp_id=$2::uuid AND task.client_id=$3::uuid`
	case "milestone":
		if lock {
			suffix = " FOR UPDATE OF milestone"
		}
		query = `SELECT milestone.version FROM project_milestones milestone WHERE milestone.id=$1::uuid AND milestone.msp_id=$2::uuid AND milestone.client_id=$3::uuid`
	case "resource_plan":
		if lock {
			suffix = " FOR UPDATE OF plan"
		}
		query = `SELECT plan.version FROM resource_plans plan WHERE plan.id=$1::uuid AND plan.msp_id=$2::uuid AND plan.client_id=$3::uuid`
	case "project":
		if lock {
			suffix = " FOR UPDATE OF project"
		}
		query = `SELECT project.version+COALESCE(classification.version,0) FROM projects project LEFT JOIN classification_object_versions classification ON classification.msp_id=project.msp_id AND classification.client_id=project.client_id AND classification.object_type='project' AND classification.object_id=project.id WHERE project.id=$1::uuid AND project.msp_id=$2::uuid AND project.client_id=$3::uuid`
	case "phase":
		if lock {
			suffix = " FOR UPDATE OF phase"
		}
		query = `SELECT phase.version FROM phases phase WHERE phase.id=$1::uuid AND phase.msp_id=$2::uuid AND phase.client_id=$3::uuid`
	default:
		return 0, calendar.ErrReadOnlyEventRole
	}
	err := q.QueryRow(ctx, query+suffix, ref.ID, ref.MSPID, ref.ClientID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, scope.ErrNotFound
	}
	return v, err
}
func schedulingFactID(seed, suffix string) string {
	sum := sha256.Sum256([]byte(seed + "\x00" + suffix))
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

type canonicalCalendarSourceRef struct {
	Type           string `json:"type"`
	ID             string `json:"id"`
	ClientID       string `json:"client_id"`
	EventRole      string `json:"event_role"`
	SourceRevision int64  `json:"source_revision"`
}

type canonicalCalendarEventData struct {
	RecipientID string                       `json:"recipient_id"`
	ChangeClass string                       `json:"change_class"`
	Urgency     string                       `json:"urgency"`
	SourceRefs  []canonicalCalendarSourceRef `json:"source_refs"`
	ActionPath  string                       `json:"action_path"`
}

func emitAcceptedScheduleCalendarFacts(ctx context.Context, tx transaction, request calendar.ScheduleApplyRequest, correlationID string, occurredAt time.Time) error {
	type recipientGroup struct {
		urgency string
		refs    map[string]canonicalCalendarSourceRef
	}
	groups := map[string]*recipientGroup{}
	for _, change := range request.Changes {
		recipientID := strings.TrimSpace(change.Schedule.TechnicianID)
		if recipientID == "" {
			continue
		}
		group := groups[recipientID]
		if group == nil {
			group = &recipientGroup{urgency: "routine", refs: map[string]canonicalCalendarSourceRef{}}
			groups[recipientID] = group
		}
		for _, conflict := range change.Conflicts {
			switch conflict.Severity {
			case calendar.ConflictHard, calendar.ConflictOverrideable:
				group.urgency = "urgent"
			case calendar.ConflictWarning:
				if group.urgency == "routine" {
					group.urgency = "important"
				}
			}
		}
		ref := canonicalCalendarSourceRef{Type: change.Requested.Source.Type, ID: change.Requested.Source.ID, ClientID: change.Requested.Source.ClientID, EventRole: change.Requested.EventRole, SourceRevision: change.Requested.SourceRevision + 1}
		key := strings.Join([]string{ref.Type, ref.ID, ref.ClientID, ref.EventRole, fmt.Sprint(ref.SourceRevision)}, "\x00")
		group.refs[key] = ref
	}
	recipients := make([]string, 0, len(groups))
	for recipientID := range groups {
		recipients = append(recipients, recipientID)
	}
	sort.Strings(recipients)
	for _, recipientID := range recipients {
		group := groups[recipientID]
		refs := make([]canonicalCalendarSourceRef, 0, len(group.refs))
		for _, ref := range group.refs {
			refs = append(refs, ref)
		}
		sortCanonicalCalendarSourceRefs(refs)
		clientID := canonicalCalendarCommonClient(refs)
		payload := canonicalCalendarEventData{RecipientID: recipientID, ChangeClass: "schedule", Urgency: group.urgency, SourceRefs: refs, ActionPath: "/calendar"}
		eventID := schedulingFactID(request.Proposal.ID+"\x00"+recipientID, "calendar.schedule_changed")
		if err := writeCanonicalCalendarEvent(ctx, tx, eventID, occurredAt, request.Principal.Scope.MSPID, clientID, "technician", request.Principal.ID, "calendar_scheduling_proposal", request.Proposal.ID, 1, correlationID, payload); err != nil {
			return err
		}
	}
	return nil
}

func sortCanonicalCalendarSourceRefs(refs []canonicalCalendarSourceRef) {
	sort.Slice(refs, func(i, j int) bool {
		left, right := refs[i], refs[j]
		return strings.Join([]string{left.Type, left.ID, left.ClientID, left.EventRole, fmt.Sprint(left.SourceRevision)}, "\x00") < strings.Join([]string{right.Type, right.ID, right.ClientID, right.EventRole, fmt.Sprint(right.SourceRevision)}, "\x00")
	})
}

func canonicalCalendarCommonClient(refs []canonicalCalendarSourceRef) string {
	if len(refs) == 0 {
		return ""
	}
	clientID := refs[0].ClientID
	for _, ref := range refs[1:] {
		if ref.ClientID != clientID {
			return ""
		}
	}
	return clientID
}

func writeCanonicalCalendarEvent(ctx context.Context, tx transaction, eventID string, occurredAt time.Time, mspID, clientID, actorType, actorID, subjectType, subjectID string, subjectVersion int64, correlationID string, payload canonicalCalendarEventData) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source,data) VALUES($1::uuid,'calendar.schedule_changed',1,$2,$3::uuid,NULLIF($4,'')::uuid,$5,$6::uuid,$7,$8::uuid,$9,$10::uuid,'calendar',$11::jsonb) ON CONFLICT(event_id) DO NOTHING`, eventID, occurredAt, mspID, clientID, actorType, actorID, subjectType, subjectID, subjectVersion, correlationID, data)
	return err
}

type existingCalendarRole struct {
	role, sourceRole, id, clientID, assigneeID, ownerID string
	revision                                            int64
	terminalState                                       calendar.TerminalState
	healthInputs                                        calendar.HealthInputs
	allDay                                              bool
	startsOn, endsOn, startsAt, endsAt                  *time.Time
	timezone                                            string
	recurrence                                          []byte
}

func emitProjectionCalendarFacts(ctx context.Context, tx transaction, batch calendar.ProjectionBatch, prior []existingCalendarRole) error {
	if strings.TrimSpace(batch.Cursor.ConsumerKey) == "" || !internalid.ValidCanonical(batch.Cursor.EventID) || batch.Cursor.OccurredAt.IsZero() {
		return nil
	}
	priorByRole := make(map[string]existingCalendarRole, len(prior))
	for _, value := range prior {
		priorByRole[value.role+"\x00"+value.sourceRole] = value
	}
	currentByRole := make(map[string]calendar.Projection, len(batch.Projections))
	for _, projection := range batch.Projections {
		currentByRole[projection.EventRole+"\x00"+projection.SourceRoleKey] = projection
	}
	type factGroup struct {
		urgency string
		refs    map[string]canonicalCalendarSourceRef
	}
	groups := map[string]*factGroup{}
	add := func(recipientID, changeClass, urgency, eventRole string) {
		if strings.TrimSpace(recipientID) == "" {
			return
		}
		key := recipientID + "\x00" + changeClass
		group := groups[key]
		if group == nil {
			group = &factGroup{urgency: urgency, refs: map[string]canonicalCalendarSourceRef{}}
			groups[key] = group
		} else if canonicalCalendarUrgencyStrength(urgency) > canonicalCalendarUrgencyStrength(group.urgency) {
			group.urgency = urgency
		}
		ref := canonicalCalendarSourceRef{Type: batch.Source.Type, ID: batch.Source.ID, ClientID: batch.Source.ClientID, EventRole: eventRole, SourceRevision: batch.SourceRevision}
		group.refs[eventRole] = ref
	}
	for key, old := range priorByRole {
		if old.assigneeID == "" || old.terminalState != calendar.Active {
			continue
		}
		projection, retained := currentByRole[key]
		state := projection.TerminalState
		if state == "" {
			state = calendar.Active
		}
		if !retained || state != calendar.Active || projection.AssigneeID != old.assigneeID {
			add(old.assigneeID, "cancellation", "important", old.role)
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts := strings.SplitN(key, "\x00", 2)
		recipientID, changeClass := parts[0], parts[1]
		group := groups[key]
		refs := make([]canonicalCalendarSourceRef, 0, len(group.refs))
		for _, ref := range group.refs {
			refs = append(refs, ref)
		}
		sortCanonicalCalendarSourceRefs(refs)
		seed := strings.Join([]string{batch.Source.MSPID, batch.Source.Type, batch.Source.ID, fmt.Sprint(batch.SourceRevision), recipientID, changeClass}, "\x00")
		payload := canonicalCalendarEventData{RecipientID: recipientID, ChangeClass: changeClass, Urgency: group.urgency, SourceRefs: refs, ActionPath: "/calendar"}
		if err := writeCanonicalCalendarEvent(ctx, tx, schedulingFactID(seed, "calendar.schedule_changed"), batch.Cursor.OccurredAt, batch.Source.MSPID, batch.Source.ClientID, "system", "00000000-0000-0000-0000-000000000000", batch.Source.Type, batch.Source.ID, batch.SourceRevision, batch.Cursor.EventID, payload); err != nil {
			return err
		}
	}
	return nil
}

func canonicalCalendarUrgencyStrength(urgency string) int {
	switch urgency {
	case "urgent":
		return 3
	case "important":
		return 2
	default:
		return 1
	}
}

func (r *CalendarRepository) ApplyProjectionBatchAtomic(ctx context.Context, batch calendar.ProjectionBatch) (applied bool, err error) {
	if r == nil || r.db == nil || !validProjectionBatchIDs(batch) {
		return false, calendar.ErrInvalidProjectionBatch
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2 || ':' || $3, 0))`, batch.Source.MSPID, batch.Source.Type, batch.Source.ID); err != nil {
		return false, err
	}
	rows, err := tx.Query(ctx, `SELECT event_role,source_role_key,id::text,source_revision,COALESCE(client_id::text,''),COALESCE(assignee_id::text,''),terminal_state,health_inputs,all_day,starts_on,ends_on,starts_at,ends_at,COALESCE(timezone,''),recurrence_rule,COALESCE(owner_id::text,'') FROM calendar_event_projections WHERE msp_id=$1::uuid AND source_type=$2 AND source_id=$3::uuid FOR UPDATE`, batch.Source.MSPID, batch.Source.Type, batch.Source.ID)
	if err != nil {
		return false, err
	}
	existing := []existingCalendarRole{}
	current := int64(0)
	for rows.Next() {
		var value existingCalendarRole
		var healthJSON []byte
		if err = rows.Scan(&value.role, &value.sourceRole, &value.id, &value.revision, &value.clientID, &value.assigneeID, &value.terminalState, &healthJSON, &value.allDay, &value.startsOn, &value.endsOn, &value.startsAt, &value.endsAt, &value.timezone, &value.recurrence, &value.ownerID); err != nil {
			rows.Close()
			return false, err
		}
		if len(healthJSON) > 0 {
			err = json.Unmarshal(healthJSON, &value.healthInputs)
		}
		if err != nil {
			rows.Close()
			return false, calendar.ErrInvalidProjectionBatch
		}
		existing = append(existing, value)
		if value.revision > current {
			current = value.revision
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()
	var historical int64
	var storedClientID string
	if len(existing) > 0 {
		storedClientID = existing[0].clientID
		for _, value := range existing[1:] {
			if value.clientID != storedClientID {
				return false, calendar.ErrInvalidProjectionBatch
			}
		}
	}
	if err = tx.QueryRow(ctx, `SELECT source_revision,COALESCE(client_id::text,'') FROM calendar_live_changes WHERE msp_id=$1::uuid AND source_type=$2 AND source_id=$3::uuid ORDER BY source_revision DESC,cursor DESC LIMIT 1`, batch.Source.MSPID, batch.Source.Type, batch.Source.ID).Scan(&historical, &storedClientID); errors.Is(err, pgx.ErrNoRows) {
		err = nil
	} else if err != nil {
		return false, err
	}
	if historical > current {
		current = historical
	}
	transfer := storedClientID != "" || len(existing) > 0 || historical > 0
	transfer = transfer && storedClientID != batch.Source.ClientID
	if transfer && !batch.TrustedSourceReload {
		return false, scope.ErrNotFound
	}
	if batch.SourceRevision <= current {
		if err = advanceProjectionCursor(ctx, tx, batch); err != nil {
			return false, err
		}
		if err = tx.Commit(ctx); err != nil {
			return false, err
		}
		return false, nil
	}
	priorForNotifications := append([]existingCalendarRole(nil), existing...)
	if transfer {
		for _, old := range existing {
			tag, deleteErr := tx.Exec(ctx, `DELETE FROM calendar_event_projections WHERE id=$1::uuid AND msp_id=$2::uuid AND source_type=$3 AND source_id=$4::uuid`, old.id, batch.Source.MSPID, batch.Source.Type, batch.Source.ID)
			if deleteErr != nil {
				return false, deleteErr
			}
			if tag.RowsAffected() > 0 {
				if _, err = tx.Exec(ctx, `INSERT INTO calendar_live_changes(msp_id,client_id,client_scope_key,projection_id,source_type,source_id,event_role,change_type,source_revision) VALUES($1::uuid,NULLIF($2,'')::uuid,CASE WHEN NULLIF($2,'')::uuid IS NULL THEN 'global' ELSE 'client:'||$2 END,NULL,$3,$4::uuid,$5,'removed',$6)`, batch.Source.MSPID, old.clientID, batch.Source.Type, batch.Source.ID, old.role, batch.SourceRevision); err != nil {
					return false, err
				}
			}
		}
		existing = nil
	}
	keep := map[string]struct{}{}
	for _, projection := range batch.Projections {
		if projection.Source != batch.Source || projection.SourceRevision != batch.SourceRevision || projection.Validate() != nil {
			return false, calendar.ErrInvalidProjectionBatch
		}
		dimensions, _ := json.Marshal(projection.Dimensions)
		health, _ := json.Marshal(projection.HealthInputs)
		recurrence, marshalErr := json.Marshal(projection.Recurrence)
		if marshalErr != nil {
			return false, marshalErr
		}
		if projection.Recurrence == nil {
			recurrence = nil
		}
		state := projection.TerminalState
		if state == "" {
			state = calendar.Active
		}
		tag, writeErr := tx.Exec(ctx, `INSERT INTO calendar_event_projections(id,msp_id,client_id,source_type,source_id,event_role,source_role_key,source_revision,title,starts_on,ends_on,starts_at,ends_at,timezone,all_day,scheduling_mode,capacity_bearing,owner_id,assignee_id,planned_minutes,filter_dimensions,recurrence_rule,health_inputs,terminal_state,projection_revision,created_at,updated_at)
VALUES($1::uuid,$2::uuid,NULLIF($3,'')::uuid,$4,$5::uuid,$6,$7,$8,$9,$10,$11,$12,$13,NULLIF($14,''),$15,$16,$17,NULLIF($18,'')::uuid,NULLIF($19,'')::uuid,$20,$21::jsonb,$22::jsonb,$23::jsonb,$24,1,now(),now())
			ON CONFLICT(msp_id,source_type,source_id,event_role,source_role_key) DO UPDATE SET id=EXCLUDED.id,source_revision=EXCLUDED.source_revision,title=EXCLUDED.title,starts_on=EXCLUDED.starts_on,ends_on=EXCLUDED.ends_on,starts_at=EXCLUDED.starts_at,ends_at=EXCLUDED.ends_at,timezone=EXCLUDED.timezone,all_day=EXCLUDED.all_day,scheduling_mode=EXCLUDED.scheduling_mode,capacity_bearing=EXCLUDED.capacity_bearing,owner_id=EXCLUDED.owner_id,assignee_id=EXCLUDED.assignee_id,planned_minutes=EXCLUDED.planned_minutes,filter_dimensions=EXCLUDED.filter_dimensions,recurrence_rule=EXCLUDED.recurrence_rule,health_inputs=EXCLUDED.health_inputs,terminal_state=EXCLUDED.terminal_state,projection_revision=calendar_event_projections.projection_revision+1,updated_at=now() WHERE calendar_event_projections.client_id IS NOT DISTINCT FROM EXCLUDED.client_id`, projection.ID, batch.Source.MSPID, batch.Source.ClientID, batch.Source.Type, batch.Source.ID, projection.EventRole, projection.SourceRoleKey, batch.SourceRevision, projection.Title, projection.StartsOn, projection.EndsOn, projection.StartsAt, projection.EndsAt, projection.Timezone, projection.AllDay, projection.SchedulingMode, projection.CapacityBearing, projection.OwnerID, projection.AssigneeID, projection.PlannedMinutes, dimensions, recurrence, health, state)
		if writeErr != nil {
			return false, writeErr
		}
		if tag.RowsAffected() != 1 {
			return false, scope.ErrNotFound
		}
		keep[projection.EventRole+"\x00"+projection.SourceRoleKey] = struct{}{}
		if _, err = tx.Exec(ctx, `INSERT INTO calendar_live_changes(msp_id,client_id,client_scope_key,projection_id,source_type,source_id,event_role,change_type,source_revision) VALUES($1::uuid,NULLIF($2,'')::uuid,CASE WHEN NULLIF($2,'')::uuid IS NULL THEN 'global' ELSE 'client:'||$2 END,$3::uuid,$4,$5::uuid,$6,'upserted',$7)`, batch.Source.MSPID, batch.Source.ClientID, projection.ID, batch.Source.Type, batch.Source.ID, projection.EventRole, batch.SourceRevision); err != nil {
			return false, err
		}
	}
	for _, old := range existing {
		if _, ok := keep[old.role+"\x00"+old.sourceRole]; ok {
			continue
		}
		tag, deleteErr := tx.Exec(ctx, `DELETE FROM calendar_event_projections WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid AND source_type=$4 AND source_id=$5::uuid`, old.id, batch.Source.MSPID, batch.Source.ClientID, batch.Source.Type, batch.Source.ID)
		if deleteErr != nil {
			return false, deleteErr
		}
		if tag.RowsAffected() > 0 {
			if _, err = tx.Exec(ctx, `INSERT INTO calendar_live_changes(msp_id,client_id,client_scope_key,projection_id,source_type,source_id,event_role,change_type,source_revision) VALUES($1::uuid,NULLIF($2,'')::uuid,CASE WHEN NULLIF($2,'')::uuid IS NULL THEN 'global' ELSE 'client:'||$2 END,NULL,$3,$4::uuid,$5,'removed',$6)`, batch.Source.MSPID, batch.Source.ClientID, batch.Source.Type, batch.Source.ID, old.role, batch.SourceRevision); err != nil {
				return false, err
			}
		}
	}
	if len(batch.Projections) == 0 && len(existing) == 0 {
		if _, err = tx.Exec(ctx, `INSERT INTO calendar_live_changes(msp_id,client_id,client_scope_key,projection_id,source_type,source_id,event_role,change_type,source_revision) VALUES($1::uuid,NULLIF($2,'')::uuid,CASE WHEN NULLIF($2,'')::uuid IS NULL THEN 'global' ELSE 'client:'||$2 END,NULL,$3,$4::uuid,'source','removed',$5)`, batch.Source.MSPID, batch.Source.ClientID, batch.Source.Type, batch.Source.ID, batch.SourceRevision); err != nil {
			return false, err
		}
	}
	if err = emitProjectionCalendarFacts(ctx, tx, batch, priorForNotifications); err != nil {
		return false, err
	}
	if err = advanceProjectionCursor(ctx, tx, batch); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (r *CalendarRepository) CurrentConflictPolicyVersion(ctx context.Context, mspID string, policyScope calendar.ConflictPolicyScope) (int64, error) {
	if r == nil || r.db == nil || !internalid.ValidCanonical(mspID) || !validConflictPolicyScopeIDs(policyScope) {
		return 0, calendar.ErrInvalidConflictPolicy
	}
	var version int64
	err := r.db.QueryRow(ctx, `SELECT COALESCE(MAX(version),0) FROM calendar_conflict_policies WHERE msp_id=$1::uuid AND scope_type=$2 AND team_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid AND technician_id IS NOT DISTINCT FROM NULLIF($4,'')::uuid`, mspID, policyScope.Type, policyScope.TeamID, policyScope.TechnicianID).Scan(&version)
	return version, err
}

func (r *CalendarRepository) ReplaceConflictPoliciesAtomic(ctx context.Context, accepted calendar.ConflictPolicyMutation) (err error) {
	if r == nil || r.db == nil || !validConflictPolicyMutation(accepted) {
		return calendar.ErrInvalidConflictPolicy
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('calendar-conflict-policy:' || $1 || ':' || $2 || ':' || $3 || ':' || $4,0))`, accepted.MSPID, accepted.Scope.Type, accepted.Scope.TeamID, accepted.Scope.TechnicianID); err != nil {
		return err
	}
	var current int64
	err = tx.QueryRow(ctx, `SELECT version FROM calendar_conflict_policies WHERE msp_id=$1::uuid AND scope_type=$2 AND team_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid AND technician_id IS NOT DISTINCT FROM NULLIF($4,'')::uuid ORDER BY version DESC LIMIT 1 FOR UPDATE`, accepted.MSPID, accepted.Scope.Type, accepted.Scope.TeamID, accepted.Scope.TechnicianID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		current, err = 0, nil
	}
	if err != nil {
		return err
	}
	if current != accepted.Version-1 {
		return calendar.ErrConflictPolicyVersionConflict
	}
	rules := map[calendar.ConflictKind]calendar.ConflictSeverity{
		calendar.ConflictApprovedPTO:          calendar.ConflictHard,
		calendar.ConflictNonWorkingTime:       calendar.ConflictHard,
		calendar.ConflictProtectedMaintenance: calendar.ConflictHard,
		calendar.ConflictOrdinaryOverbooking:  calendar.ConflictOverrideable,
	}
	for _, rule := range accepted.Rules {
		rules[rule.Kind] = rule.Severity
	}
	if current > 0 {
		if _, err = tx.Exec(ctx, `UPDATE calendar_conflict_policies SET lifecycle_state='superseded',effective_through=$5,updated_at=$5,updated_by=$6::uuid WHERE msp_id=$1::uuid AND scope_type=$2 AND team_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid AND technician_id IS NOT DISTINCT FROM NULLIF($4,'')::uuid AND lifecycle_state='active'`, accepted.MSPID, accepted.Scope.Type, accepted.Scope.TeamID, accepted.Scope.TechnicianID, accepted.EffectiveFrom, accepted.Audit.ActorID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO calendar_conflict_policies(id,msp_id,scope_type,team_id,technician_id,approved_pto_rule,non_working_time_rule,protected_maintenance_rule,ordinary_overbooking_rule,effective_from,version,lifecycle_state,created_at,created_by,updated_at,updated_by) VALUES($1::uuid,$2::uuid,$3,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,$6,$7,$8,$9,$10,$11,'active',$12,$13::uuid,$12,$13::uuid)`, accepted.ID, accepted.MSPID, accepted.Scope.Type, accepted.Scope.TeamID, accepted.Scope.TechnicianID, rules[calendar.ConflictApprovedPTO], rules[calendar.ConflictNonWorkingTime], rules[calendar.ConflictProtectedMaintenance], rules[calendar.ConflictOrdinaryOverbooking], accepted.EffectiveFrom, accepted.Version, accepted.Audit.OccurredAt, accepted.Audit.ActorID); err != nil {
		return err
	}
	if err = writeAuditOnly(ctx, tx, accepted.Audit); err != nil {
		return err
	}
	if err = writeEventOnly(ctx, tx, accepted.Event); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (r *CalendarRepository) FindCustomDateField(ctx context.Context, mspID, objectType, fieldID string) (calendar.CustomDateField, error) {
	var field calendar.CustomDateField
	if r == nil || r.db == nil || !internalid.ValidCanonical(mspID) {
		return field, calendar.ErrInvalidCustomDateConfiguration
	}
	var valueKind, timezoneSource, fixedTimezone string
	err := r.db.QueryRow(ctx, `SELECT id::text,msp_id::text,object_type,internal_key,label,category,value_kind,scheduling_mode,capacity_bearing,COALESCE(timezone_source,''),COALESCE(fixed_timezone,''),COALESCE(planned_effort_source,''),version FROM calendar_custom_date_fields WHERE msp_id=$1::uuid AND object_type=$2 AND internal_key=$3`, mspID, objectType, fieldID).Scan(&field.ID, &field.MSPID, &field.ObjectType, &field.FieldID, &field.Label, &field.Category, &valueKind, &field.SchedulingMode, &field.CapacityBearing, &timezoneSource, &fixedTimezone, &field.PlannedEffortSource, &field.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return calendar.CustomDateField{}, nil
	}
	if err != nil {
		return field, err
	}
	if valueKind == "timestamp" {
		field.FieldType = calendar.FieldDateTime
	} else {
		field.FieldType = calendar.FieldDate
	}
	field.TimezoneSource = timezoneSource
	if timezoneSource == "fixed" {
		field.TimezoneSource = fixedTimezone
	}
	return field, nil
}

func (r *CalendarRepository) UpsertCustomDateFieldAtomic(ctx context.Context, accepted calendar.CustomDateFieldMutation) (err error) {
	field := accepted.Field
	if r == nil || r.db == nil || !validCustomDateFieldMutation(accepted) {
		return calendar.ErrInvalidCustomDateConfiguration
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	var current int64
	err = tx.QueryRow(ctx, `SELECT version FROM calendar_custom_date_fields WHERE msp_id=$1::uuid AND object_type=$2 AND internal_key=$3 FOR UPDATE`, field.MSPID, field.ObjectType, field.FieldID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		current, err = 0, nil
	}
	if err != nil {
		return err
	}
	if current != accepted.ExpectedVersion {
		return calendar.ErrCustomDateFieldVersionConflict
	}
	valueKind := "date"
	timezoneSource, fixedTimezone := "", ""
	if field.FieldType == calendar.FieldDateTime {
		valueKind = "timestamp"
		timezoneSource = field.TimezoneSource
		if _, loadErr := time.LoadLocation(field.TimezoneSource); loadErr == nil && field.TimezoneSource != "Local" {
			timezoneSource, fixedTimezone = "fixed", field.TimezoneSource
		}
	}
	calendarMode := "read_only"
	if field.SchedulingMode != calendar.Informational {
		calendarMode = "schedulable"
	}
	tag, err := tx.Exec(ctx, `INSERT INTO calendar_custom_date_fields(id,msp_id,object_type,internal_key,label,value_kind,event_role,category,color_category,calendar_mode,scheduling_mode,capacity_bearing,timezone_source,fixed_timezone,planned_effort_source,lifecycle_state,version,created_at,created_by,updated_at,updated_by) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$4,$7,'neutral',$8,$9,$10,NULLIF($11,''),NULLIF($12,''),NULLIF($13,''),'active',$14,$15,$16::uuid,$15,$16::uuid) ON CONFLICT(msp_id,object_type,internal_key) DO UPDATE SET label=EXCLUDED.label,value_kind=EXCLUDED.value_kind,event_role=EXCLUDED.event_role,category=EXCLUDED.category,calendar_mode=EXCLUDED.calendar_mode,scheduling_mode=EXCLUDED.scheduling_mode,capacity_bearing=EXCLUDED.capacity_bearing,timezone_source=EXCLUDED.timezone_source,fixed_timezone=EXCLUDED.fixed_timezone,planned_effort_source=EXCLUDED.planned_effort_source,lifecycle_state='active',version=EXCLUDED.version,updated_at=EXCLUDED.updated_at,updated_by=EXCLUDED.updated_by WHERE calendar_custom_date_fields.version=$17`, field.ID, field.MSPID, field.ObjectType, field.FieldID, field.Label, valueKind, field.Category, calendarMode, field.SchedulingMode, field.CapacityBearing, timezoneSource, fixedTimezone, field.PlannedEffortSource, field.Version, accepted.Audit.OccurredAt, accepted.Audit.ActorID, accepted.ExpectedVersion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return calendar.ErrCustomDateFieldVersionConflict
	}
	if err = writeAuditOnly(ctx, tx, accepted.Audit); err != nil {
		return err
	}
	if err = writeEventOnly(ctx, tx, accepted.Event); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func validConflictPolicyScopeIDs(value calendar.ConflictPolicyScope) bool {
	switch value.Type {
	case calendar.ConflictScopeMSP:
		return value.TeamID == "" && value.TechnicianID == ""
	case calendar.ConflictScopeTeam:
		return internalid.ValidCanonical(value.TeamID) && value.TechnicianID == ""
	case calendar.ConflictScopeTechnician:
		return value.TeamID == "" && internalid.ValidCanonical(value.TechnicianID)
	}
	return false
}
func validConflictPolicyMutation(value calendar.ConflictPolicyMutation) bool {
	if !internalid.ValidCanonical(value.ID) || !internalid.ValidCanonical(value.MSPID) || !validConflictPolicyScopeIDs(value.Scope) || value.Version < 1 || value.EffectiveFrom.IsZero() || len(value.Rules) == 0 || !validConfigurationFacts(value.ID, value.MSPID, value.Version, value.Audit, value.Event) {
		return false
	}
	seen := map[calendar.ConflictKind]bool{}
	for _, rule := range value.Rules {
		known := rule.Kind == calendar.ConflictApprovedPTO || rule.Kind == calendar.ConflictNonWorkingTime || rule.Kind == calendar.ConflictProtectedMaintenance || rule.Kind == calendar.ConflictOrdinaryOverbooking
		severity := rule.Severity == calendar.ConflictInfo || rule.Severity == calendar.ConflictWarning || rule.Severity == calendar.ConflictOverrideable || rule.Severity == calendar.ConflictHard
		if !known || !severity || seen[rule.Kind] {
			return false
		}
		seen[rule.Kind] = true
	}
	return true
}
func validCustomDateFieldMutation(value calendar.CustomDateFieldMutation) bool {
	field := value.Field
	return field.Validate() == nil && field.Version == value.ExpectedVersion+1 && value.ExpectedVersion >= 0 && validConfigurationFacts(field.ID, field.MSPID, field.Version, value.Audit, value.Event)
}
func validConfigurationFacts(subjectID, mspID string, version int64, audit mutation.AuditRecord, event mutation.EventRecord) bool {
	return internalid.ValidCanonical(audit.ID) && internalid.ValidCanonical(event.EventID) && internalid.ValidCanonical(audit.ActorID) && audit.MSPID == mspID && event.MSPID == mspID && audit.SubjectID == subjectID && event.SubjectID == subjectID && audit.SubjectVersion == version && event.SubjectVersion == version && audit.CorrelationID == event.CorrelationID && internalid.ValidCanonical(audit.CorrelationID) && !audit.OccurredAt.IsZero() && !event.OccurredAt.IsZero()
}

func (r *CalendarRepository) LoadAvailabilityInput(ctx context.Context, mspID, technicianID string, window calendar.QueryWindow) (calendar.AvailabilityInput, error) {
	var input calendar.AvailabilityInput
	if r == nil || r.db == nil || !internalid.ValidCanonical(mspID) || !internalid.ValidCanonical(technicianID) || !window.End.After(window.Start) {
		return input, calendar.ErrInvalidAvailabilityWindow
	}
	rows, err := r.db.Query(ctx, `SELECT schedule.id::text,schedule.msp_id::text,schedule.technician_id::text,schedule.timezone,schedule.effective_from,schedule.effective_through,schedule.version,schedule.created_at,schedule.created_by::text,schedule_window.id::text,schedule_window.weekday,(extract(hour FROM schedule_window.starts_local)*60+extract(minute FROM schedule_window.starts_local))::integer,(extract(hour FROM schedule_window.ends_local)*60+extract(minute FROM schedule_window.ends_local))::integer,schedule_window.capacity_percent
FROM technician_schedule_versions schedule JOIN technician_schedule_windows schedule_window ON schedule_window.schedule_version_id=schedule.id AND schedule_window.msp_id=schedule.msp_id AND schedule_window.technician_id=schedule.technician_id
WHERE schedule.msp_id=$1::uuid AND schedule.technician_id=$2::uuid AND schedule.lifecycle_state IN('active','superseded')
AND schedule.effective_from <= (($4 AT TIME ZONE schedule.timezone)::date + 1)
AND (schedule.effective_through IS NULL OR schedule.effective_through >= (($3 AT TIME ZONE schedule.timezone)::date - 1))
ORDER BY schedule.version,schedule_window.weekday,schedule_window.starts_local,schedule_window.id`, mspID, technicianID, window.Start, window.End)
	if err != nil {
		return input, err
	}
	byID := map[string]int{}
	for rows.Next() {
		var schedule workforce.Schedule
		var weekly workforce.WeeklyWindow
		var weekday int
		if err = rows.Scan(&schedule.ID, &schedule.MSPID, &schedule.TechnicianID, &schedule.Timezone, &schedule.EffectiveFrom, &schedule.EffectiveThrough, &schedule.Version, &schedule.CreatedAt, &schedule.CreatedBy, &weekly.ID, &weekday, &weekly.StartsMinute, &weekly.EndsMinute, &weekly.CapacityPercent); err != nil {
			rows.Close()
			return input, err
		}
		weekly.Weekday = time.Weekday(weekday)
		index, exists := byID[schedule.ID]
		if !exists {
			index = len(input.Schedules)
			byID[schedule.ID] = index
			input.Schedules = append(input.Schedules, schedule)
		}
		input.Schedules[index].Windows = append(input.Schedules[index].Windows, weekly)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return input, err
	}
	rows.Close()
	if len(byID) > 0 {
		scheduleIDs := make([]string, 0, len(byID))
		for id := range byID {
			scheduleIDs = append(scheduleIDs, id)
		}
		sort.Strings(scheduleIDs)
		rows, err = r.db.Query(ctx, `SELECT id::text,schedule_version_id::text,exception_on,availability_state,all_day,COALESCE((extract(hour FROM starts_local)*60+extract(minute FROM starts_local))::integer,0),COALESCE((extract(hour FROM ends_local)*60+extract(minute FROM ends_local))::integer,0),capacity_percent,reason,version FROM technician_schedule_exceptions WHERE msp_id=$1::uuid AND technician_id=$2::uuid AND schedule_version_id::text=ANY($3) AND exception_on BETWEEN $4::date AND $5::date ORDER BY exception_on,starts_local,id`, mspID, technicianID, scheduleIDs, window.Start.AddDate(0, 0, -1), window.End.AddDate(0, 0, 1))
		if err != nil {
			return input, err
		}
		for rows.Next() {
			var exception workforce.ScheduleException
			var scheduleID string
			if err = rows.Scan(&exception.ID, &scheduleID, &exception.ExceptionOn, &exception.State, &exception.AllDay, &exception.StartsMinute, &exception.EndsMinute, &exception.CapacityPercent, &exception.Reason, &exception.Version); err != nil {
				rows.Close()
				return input, err
			}
			index, ok := byID[scheduleID]
			if ok {
				input.Schedules[index].Exceptions = append(input.Schedules[index].Exceptions, exception)
			}
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return input, err
		}
		rows.Close()
	}
	rows, err = r.db.Query(ctx, `SELECT id::text,msp_id::text,technician_id::text,pto_type,COALESCE(manager_id::text,''),COALESCE(decided_by::text,''),decision_reason,created_by::text,updated_by::text,COALESCE(timezone,''),starts_on,ends_on,starts_at,ends_at,all_day,state,version,created_at,updated_at FROM pto_requests WHERE msp_id=$1::uuid AND technician_id=$2::uuid AND state IN('requested','approved') AND ((NOT all_day AND starts_at<$4 AND ends_at>$3) OR (all_day AND starts_on<=($4::date + 1) AND COALESCE(ends_on,starts_on)>=($3::date - 1))) ORDER BY created_at,id`, mspID, technicianID, window.Start, window.End)
	if err != nil {
		return input, err
	}
	for rows.Next() {
		var p workforce.PTORequest
		if err = rows.Scan(&p.ID, &p.MSPID, &p.TechnicianID, &p.PTOType, &p.ManagerID, &p.DecidedBy, &p.DecisionReason, &p.CreatedBy, &p.UpdatedBy, &p.Timezone, &p.StartsOn, &p.EndsOn, &p.StartsAt, &p.EndsAt, &p.AllDay, &p.State, &p.Version, &p.CreatedAt, &p.UpdatedAt); err != nil {
			rows.Close()
			return input, err
		}
		input.PTO = append(input.PTO, p)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return input, err
	}
	rows.Close()
	return input, nil
}

func (r *CalendarRepository) loadProtectedMaintenanceForProposed(ctx context.Context, mspID string, proposed calendar.ProposedSchedule, window calendar.QueryWindow, schedules []workforce.Schedule) ([]calendar.AvailabilityAdjustment, error) {
	if proposed.ClientID == "" {
		return nil, nil
	}
	rows, err := r.db.Query(ctx, `SELECT DISTINCT maintenance.id::text,maintenance.title,maintenance.starts_on,maintenance.ends_on,maintenance.starts_at,maintenance.ends_at,COALESCE(maintenance.timezone,''),maintenance.all_day,maintenance.recurrence_rule,maintenance.conflict_policy,maintenance.version
FROM maintenance_windows maintenance
JOIN maintenance_window_scopes maintenance_scope ON maintenance_scope.maintenance_window_id=maintenance.id AND maintenance_scope.msp_id=maintenance.msp_id
WHERE maintenance.msp_id=$1::uuid AND maintenance.protected AND maintenance.status IN('planned','active')
AND ((NOT maintenance.all_day AND maintenance.starts_at<$6 AND (maintenance.recurrence_rule IS NOT NULL OR maintenance.ends_at>$5)) OR (maintenance.all_day AND maintenance.starts_on<=$6::date AND (maintenance.recurrence_rule IS NOT NULL OR COALESCE(maintenance.ends_on,maintenance.starts_on)>=$5::date)))
AND maintenance_scope.client_id=$2::uuid
AND (maintenance_scope.scope_type='client'
 OR maintenance_scope.scope_type='service' AND maintenance_scope.service_id=NULLIF($3,'')::uuid
 OR maintenance_scope.scope_type='asset' AND maintenance_scope.asset_id=NULLIF($4,'')::uuid)
ORDER BY maintenance.id::text`, mspID, proposed.ClientID, proposed.ServiceID, proposed.AssetID, window.Start, window.End)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return expandProtectedMaintenanceRows(rows, mspID, window, schedules)
}

type protectedMaintenanceRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func expandProtectedMaintenanceRows(rows protectedMaintenanceRows, mspID string, window calendar.QueryWindow, schedules []workforce.Schedule) ([]calendar.AvailabilityAdjustment, error) {
	timezone := "UTC"
	if len(schedules) > 0 {
		timezone = scheduleTimezoneAt(schedules, window.Start)
		if timezone == "" {
			timezone = schedules[0].Timezone
		}
	}
	location, err := time.LoadLocation(timezone)
	if err != nil || timezone == "Local" {
		return nil, calendar.ErrInvalidAvailabilityWindow
	}
	result := []calendar.AvailabilityAdjustment{}
	for rows.Next() {
		var id, title, zone string
		var severity calendar.ConflictSeverity
		var startsOn, endsOn, startsAt, endsAt *time.Time
		var allDay bool
		var recurrence []byte
		var revision int64
		if err = rows.Scan(&id, &title, &startsOn, &endsOn, &startsAt, &endsAt, &zone, &allDay, &recurrence, &severity, &revision); err != nil {
			return nil, err
		}
		projection := calendar.Projection{ID: id, Source: calendar.SourceRef{MSPID: mspID, Type: "maintenance_window", ID: id}, EventRole: "maintenance", SourceRevision: revision, Title: title, AllDay: allDay, StartsOn: startsOn, EndsOn: endsOn, StartsAt: startsAt, EndsAt: endsAt, Timezone: zone, SchedulingMode: calendar.FixedBlock, TerminalState: calendar.Active}
		if len(recurrence) > 0 {
			var rule calendar.RecurrenceRule
			if err = json.Unmarshal(recurrence, &rule); err != nil {
				return nil, err
			}
			projection.Recurrence = &rule
		}
		occurrences, expandErr := calendar.ExpandOccurrences(projection, window, nil)
		if expandErr != nil {
			return nil, expandErr
		}
		for _, occurrence := range occurrences {
			var interval calendar.TimeInterval
			if occurrence.AllDay && occurrence.StartsOn != nil {
				start := time.Date(occurrence.StartsOn.Year(), occurrence.StartsOn.Month(), occurrence.StartsOn.Day(), 0, 0, 0, 0, location)
				endDate := occurrence.StartsOn
				if occurrence.EndsOn != nil {
					endDate = occurrence.EndsOn
				}
				end := time.Date(endDate.Year(), endDate.Month(), endDate.Day(), 0, 0, 0, 0, location).AddDate(0, 0, 1)
				interval = calendar.TimeInterval{Start: start.UTC(), End: end.UTC()}
			} else {
				interval = calendar.TimeInterval{Start: occurrence.StartsAt, End: occurrence.EndsAt}
			}
			result = append(result, calendar.AvailabilityAdjustment{Kind: calendar.ProtectedMaintenanceAdjustment, State: calendar.AvailabilityUnavailable, Interval: interval, Source: calendar.SafeSourceRef{Type: "maintenance_window", ID: id}, Policy: &calendar.ConflictPolicyEvidence{ID: "maintenance_window:" + id, Version: revision, Severity: severity}})
		}
	}
	return result, rows.Err()
}

func (r *CalendarRepository) LoadCapacityInputs(ctx context.Context, principal authorization.Principal, window calendar.QueryWindow, technicianIDs []string) (map[string]calendar.CapacityInput, error) {
	if r == nil || r.db == nil || !internalid.ValidCanonical(principal.Scope.MSPID) || !window.End.After(window.Start) || len(technicianIDs) == 0 {
		return nil, calendar.ErrInvalidCapacityInput
	}
	for _, id := range technicianIDs {
		if !internalid.ValidCanonical(id) {
			return nil, calendar.ErrInvalidCapacityInput
		}
	}
	result := make(map[string]calendar.CapacityInput, len(technicianIDs))
	availability := calendar.NewAvailabilityService(r)
	for _, technicianID := range technicianIDs {
		resolved, err := availability.Resolve(ctx, principal.Scope.MSPID, technicianID, window)
		if err != nil {
			return nil, err
		}
		result[technicianID] = calendar.CapacityInput{Window: window, Available: resolved.Segments, TentativeUnavailableMinutes: resolved.TentativeMinutes}
	}
	rows, err := r.db.Query(ctx, `SELECT id::text,COALESCE(client_id::text,''),source_type,source_id::text,event_role,source_revision,title,starts_at,ends_at,timezone,scheduling_mode,planned_minutes,assignee_id::text,recurrence_rule FROM calendar_event_projections WHERE msp_id=$1::uuid AND assignee_id::text=ANY($2) AND capacity_bearing AND terminal_state='active' AND NOT all_day AND starts_at<$4 AND (recurrence_rule IS NOT NULL OR COALESCE(ends_at,starts_at)>$3) AND (client_id IS NULL OR NULLIF($5,'')::uuid IS NULL OR client_id=$5::uuid) ORDER BY assignee_id,starts_at,id`, principal.Scope.MSPID, technicianIDs, window.Start, window.End, principal.Scope.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p calendar.Projection
		var recurrence []byte
		if err = rows.Scan(&p.ID, &p.Source.ClientID, &p.Source.Type, &p.Source.ID, &p.EventRole, &p.SourceRevision, &p.Title, &p.StartsAt, &p.EndsAt, &p.Timezone, &p.SchedulingMode, &p.PlannedMinutes, &p.AssigneeID, &recurrence); err != nil {
			return nil, err
		}
		p.Source.MSPID = principal.Scope.MSPID
		p.CapacityBearing = true
		p.TerminalState = calendar.Active
		if len(recurrence) > 0 {
			var rule calendar.RecurrenceRule
			if err = json.Unmarshal(recurrence, &rule); err != nil {
				return nil, err
			}
			p.Recurrence = &rule
		}
		exceptions := []calendar.RecurrenceException(nil)
		if p.Recurrence != nil {
			exceptions, err = r.loadCapacityRecurrenceExceptions(ctx, p.Source.MSPID, p.ID)
			if err != nil {
				return nil, err
			}
		}
		occurrences, expandErr := calendar.ExpandOccurrences(p, window, exceptions)
		if expandErr != nil {
			return nil, expandErr
		}
		input := result[p.AssigneeID]
		for _, occurrence := range occurrences {
			event := calendar.CapacityEvent{ID: occurrence.ID, ClientID: p.Source.ClientID, Assigned: true, Mode: p.SchedulingMode, PlannedMinutes: p.PlannedMinutes, Interval: calendar.TimeInterval{Start: occurrence.StartsAt, End: occurrence.EndsAt}, Source: calendar.SafeSourceRef{Type: p.Source.Type, ID: p.Source.ID}}
			input.Events = append(input.Events, event)
			dependencies, dependencyErr := r.loadTimedDependencyConstraints(ctx, principal.Scope.MSPID, p.ID, event.Interval)
			if dependencyErr != nil {
				return nil, dependencyErr
			}
			input.Dependencies = append(input.Dependencies, dependencies...)
		}
		result[p.AssigneeID] = input
	}
	return result, rows.Err()
}

func (r *CalendarRepository) loadCapacityRecurrenceExceptions(ctx context.Context, mspID, projectionID string) ([]calendar.RecurrenceException, error) {
	rows, err := r.db.Query(ctx, `SELECT original_local_key,state,occurrence_scope,starts_on,ends_on,starts_at,ends_at FROM calendar_recurrence_exceptions WHERE msp_id=$1::uuid AND projection_id=$2::uuid ORDER BY original_local_key,id`, mspID, projectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := []calendar.RecurrenceException{}
	for rows.Next() {
		var value calendar.RecurrenceException
		if err = rows.Scan(&value.OriginalLocalKey, &value.State, &value.OccurrenceScope, &value.StartsOn, &value.EndsOn, &value.StartsAt, &value.EndsAt); err != nil {
			return nil, err
		}
		found = append(found, value)
	}
	return found, rows.Err()
}

type loadedAvailabilityRepository struct{ input calendar.AvailabilityInput }

func (r loadedAvailabilityRepository) LoadAvailabilityInput(context.Context, string, string, calendar.QueryWindow) (calendar.AvailabilityInput, error) {
	return r.input, nil
}

func (r *CalendarRepository) loadAuthoritativeProposedSchedule(ctx context.Context, mspID string, claimed calendar.ProposedSchedule) (calendar.ProposedSchedule, error) {
	resolved := claimed
	var clientID, sourceType, sourceID, technicianID string
	var dimensionsJSON []byte
	err := r.db.QueryRow(ctx, `SELECT COALESCE(client_id::text,''),source_type,source_id::text,COALESCE(assignee_id::text,''),filter_dimensions FROM calendar_event_projections WHERE id=$1::uuid AND msp_id=$2::uuid`, claimed.ProjectionID, mspID).Scan(&clientID, &sourceType, &sourceID, &technicianID, &dimensionsJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return claimed, calendar.ErrInvalidConflictInput
	}
	if err != nil {
		return claimed, err
	}
	var dimensions calendar.FilterDimensions
	if err = json.Unmarshal(dimensionsJSON, &dimensions); err != nil {
		return claimed, calendar.ErrInvalidConflictInput
	}
	teamIDs := uniqueCanonicalIDs(dimensions.TeamIDs)
	if len(teamIDs) != len(dimensions.TeamIDs) || len(teamIDs) > 1 || (technicianID != "" && !internalid.ValidCanonical(technicianID)) || (clientID != "" && !internalid.ValidCanonical(clientID)) || !internalid.ValidCanonical(sourceID) {
		return claimed, calendar.ErrInvalidConflictInput
	}
	teamID := ""
	if len(teamIDs) == 1 {
		teamID = teamIDs[0]
	}
	serviceID, assetID := "", ""
	if sourceType == "work_record" {
		err = r.db.QueryRow(ctx, `SELECT COALESCE(service_id::text,''),COALESCE(asset_id::text,'') FROM work_records WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid AND deleted_at IS NULL`, sourceID, mspID, clientID).Scan(&serviceID, &assetID)
		if errors.Is(err, pgx.ErrNoRows) {
			return claimed, calendar.ErrInvalidConflictInput
		}
		if err != nil {
			return claimed, err
		}
	}
	if claimed.ClientID != "" && claimed.ClientID != clientID || claimed.TechnicianID != "" && claimed.TechnicianID != technicianID || claimed.TeamID != "" && claimed.TeamID != teamID || claimed.ServiceID != "" && claimed.ServiceID != serviceID || claimed.AssetID != "" && claimed.AssetID != assetID || claimed.Source.Type != "" && claimed.Source.Type != sourceType || claimed.Source.ID != "" && claimed.Source.ID != sourceID {
		return claimed, calendar.ErrInvalidConflictInput
	}
	resolved.ClientID, resolved.TechnicianID, resolved.TeamID = clientID, technicianID, teamID
	resolved.ServiceID, resolved.AssetID = serviceID, assetID
	resolved.Source = calendar.SafeSourceRef{Type: sourceType, ID: sourceID}
	return resolved, nil
}

func uniqueCanonicalIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !internalid.ValidCanonical(value) {
			return nil
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (r *CalendarRepository) LoadConflictInput(ctx context.Context, principal authorization.Principal, proposed calendar.ProposedSchedule) (calendar.ConflictInput, error) {
	input := calendar.ConflictInput{Proposed: proposed}
	if r == nil || r.db == nil || !internalid.ValidCanonical(principal.Scope.MSPID) || !proposed.Interval.End.After(proposed.Interval.Start) || !internalid.ValidCanonical(proposed.ProjectionID) || (proposed.TechnicianID != "" && !internalid.ValidCanonical(proposed.TechnicianID)) || (proposed.ClientID != "" && !internalid.ValidCanonical(proposed.ClientID)) || (proposed.ServiceID != "" && !internalid.ValidCanonical(proposed.ServiceID)) || (proposed.AssetID != "" && !internalid.ValidCanonical(proposed.AssetID)) || (proposed.TeamID != "" && !internalid.ValidCanonical(proposed.TeamID)) || (proposed.Source.ID != "" && !internalid.ValidCanonical(proposed.Source.ID)) {
		return input, calendar.ErrInvalidConflictInput
	}
	var err error
	proposed, err = r.loadAuthoritativeProposedSchedule(ctx, principal.Scope.MSPID, proposed)
	if err != nil {
		return input, err
	}
	input.Proposed = proposed
	if err := scope.Authorize(principal.Scope, scope.Target{MSPID: principal.Scope.MSPID, ClientID: proposed.ClientID}); err != nil {
		return input, err
	}
	rows, err := r.db.Query(ctx, `SELECT id::text,scope_type,COALESCE(team_id::text,''),COALESCE(technician_id::text,''),approved_pto_rule,non_working_time_rule,protected_maintenance_rule,ordinary_overbooking_rule,version FROM calendar_conflict_policies WHERE msp_id=$1::uuid AND lifecycle_state='active' AND effective_from<=$4 AND (effective_through IS NULL OR effective_through>$4) AND (scope_type='msp' OR (scope_type='team' AND team_id=NULLIF($2,'')::uuid) OR (scope_type='technician' AND technician_id=NULLIF($3,'')::uuid)) ORDER BY CASE scope_type WHEN 'technician' THEN 1 WHEN 'team' THEN 2 ELSE 3 END,version DESC,id`, principal.Scope.MSPID, proposed.TeamID, proposed.TechnicianID, proposed.Interval.Start)
	if err != nil {
		return input, err
	}
	seenPolicy := map[calendar.ConflictKind]bool{}
	for rows.Next() {
		var id string
		var scopeType calendar.ConflictPolicyScopeType
		var teamID, technicianID string
		var approved, nonWorking, maintenance, overbooking calendar.ConflictSeverity
		var version int64
		if err = rows.Scan(&id, &scopeType, &teamID, &technicianID, &approved, &nonWorking, &maintenance, &overbooking, &version); err != nil {
			rows.Close()
			return input, err
		}
		for kind, severity := range map[calendar.ConflictKind]calendar.ConflictSeverity{calendar.ConflictApprovedPTO: approved, calendar.ConflictNonWorkingTime: nonWorking, calendar.ConflictProtectedMaintenance: maintenance, calendar.ConflictOrdinaryOverbooking: overbooking} {
			if !seenPolicy[kind] {
				input.Policies = append(input.Policies, calendar.ConflictPolicy{ID: id, MSPID: principal.Scope.MSPID, ScopeType: scopeType, TeamID: teamID, TechnicianID: technicianID, Kind: kind, Severity: severity, Version: version})
				seenPolicy[kind] = true
			}
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return input, err
	}
	rows.Close()
	if proposed.TechnicianID == "" {
		return input, nil
	}
	availabilityInput, err := r.LoadAvailabilityInput(ctx, principal.Scope.MSPID, proposed.TechnicianID, calendar.QueryWindow{Start: proposed.Interval.Start, End: proposed.Interval.End})
	if err != nil {
		return input, err
	}
	for _, request := range availabilityInput.PTO {
		if request.State != workforce.Approved {
			continue
		}
		interval, ok := repositoryPTOInterval(request, proposed.Interval, scheduleTimezoneAt(availabilityInput.Schedules, proposed.Interval.Start))
		if ok {
			input.Constraints = append(input.Constraints, calendar.ConflictConstraint{Kind: calendar.ConflictApprovedPTO, Interval: interval, Related: calendar.SafeSourceRef{Type: "pto", ID: request.ID}})
		}
	}
	baseInput := availabilityInput
	baseInput.PTO = nil
	baseAdjustments := make([]calendar.AvailabilityAdjustment, 0, len(baseInput.Adjustments))
	for _, adjustment := range baseInput.Adjustments {
		if adjustment.Kind == calendar.ProtectedMaintenanceAdjustment {
			continue
		}
		baseAdjustments = append(baseAdjustments, adjustment)
	}
	baseInput.Adjustments = baseAdjustments
	maintenance, err := r.loadProtectedMaintenanceForProposed(ctx, principal.Scope.MSPID, proposed, calendar.QueryWindow{Start: proposed.Interval.Start, End: proposed.Interval.End}, availabilityInput.Schedules)
	if err != nil {
		return input, err
	}
	for _, adjustment := range maintenance {
		input.Constraints = append(input.Constraints, calendar.ConflictConstraint{Kind: calendar.ConflictProtectedMaintenance, Interval: adjustment.Interval, Related: adjustment.Source, Policy: adjustment.Policy})
	}
	base, err := calendar.NewAvailabilityService(loadedAvailabilityRepository{input: baseInput}).Resolve(ctx, principal.Scope.MSPID, proposed.TechnicianID, calendar.QueryWindow{Start: proposed.Interval.Start, End: proposed.Interval.End})
	if err != nil {
		return input, err
	}
	for _, gap := range calendar.AvailabilityGaps(base.Segments, proposed.Interval) {
		input.Constraints = append(input.Constraints, calendar.ConflictConstraint{Kind: calendar.ConflictNonWorkingTime, Interval: gap, Related: calendar.SafeSourceRef{Type: "technician_schedule", ID: proposed.TechnicianID}})
	}
	capacityInputs, err := r.LoadCapacityInputs(ctx, principal, calendar.QueryWindow{Start: proposed.Interval.Start, End: proposed.Interval.End}, []string{proposed.TechnicianID})
	if err != nil {
		return input, err
	}
	capacityInput := capacityInputs[proposed.TechnicianID]
	allocationWindow := calendar.QueryWindow{Start: proposed.Interval.Start, End: proposed.Interval.End}
	for _, event := range capacityInput.Events {
		if event.Interval.Start.Before(allocationWindow.Start) {
			allocationWindow.Start = event.Interval.Start
		}
		if event.Interval.End.After(allocationWindow.End) {
			allocationWindow.End = event.Interval.End
		}
	}
	if allocationWindow.Start.Before(proposed.Interval.Start) || allocationWindow.End.After(proposed.Interval.End) {
		capacityInputs, err = r.LoadCapacityInputs(ctx, principal, allocationWindow, []string{proposed.TechnicianID})
		if err != nil {
			return input, err
		}
		capacityInput = capacityInputs[proposed.TechnicianID]
	}
	capacityInput.Events = excludeProposedCapacityEvent(capacityInput.Events, proposed)
	for _, segment := range calendar.CalculateCapacity(capacityInput).Segments {
		if proposed.Source.Type == segment.Related.Type && proposed.Source.ID == segment.Related.ID {
			continue
		}
		if interval := calendarIntervalIntersection(segment.Interval, proposed.Interval); interval.End.After(interval.Start) {
			input.Constraints = append(input.Constraints, calendar.ConflictConstraint{Kind: calendar.ConflictOrdinaryOverbooking, Interval: interval, Related: segment.Related})
		}
	}
	dependencies, err := r.loadProposedDependencyConstraints(ctx, principal.Scope.MSPID, proposed)
	if err != nil {
		return input, err
	}
	input.Dependencies = append(input.Dependencies, dependencies...)
	return input, nil
}

func repositoryPTOInterval(request workforce.PTORequest, proposed calendar.TimeInterval, fallbackTimezone string) (calendar.TimeInterval, bool) {
	if !request.AllDay && request.StartsAt != nil && request.EndsAt != nil {
		return calendar.TimeInterval{Start: request.StartsAt.UTC(), End: request.EndsAt.UTC()}, true
	}
	if !request.AllDay || request.StartsOn == nil {
		return calendar.TimeInterval{}, false
	}
	zone := request.Timezone
	if zone == "" {
		zone = fallbackTimezone
	}
	location, err := time.LoadLocation(zone)
	if err != nil || zone == "Local" {
		return calendar.TimeInterval{}, false
	}
	start := time.Date(request.StartsOn.Year(), request.StartsOn.Month(), request.StartsOn.Day(), 0, 0, 0, 0, location)
	endDate := request.StartsOn
	if request.EndsOn != nil {
		endDate = request.EndsOn
	}
	end := time.Date(endDate.Year(), endDate.Month(), endDate.Day(), 0, 0, 0, 0, location).AddDate(0, 0, 1)
	return calendar.TimeInterval{Start: start.UTC(), End: end.UTC()}, end.After(proposed.Start) && start.Before(proposed.End)
}

func scheduleTimezoneAt(schedules []workforce.Schedule, instant time.Time) string {
	ordered := append([]workforce.Schedule(nil), schedules...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Version > ordered[j].Version })
	for _, schedule := range ordered {
		location, err := time.LoadLocation(schedule.Timezone)
		if err != nil || schedule.Timezone == "Local" {
			continue
		}
		day := instant.In(location).Format("2006-01-02")
		if day < schedule.EffectiveFrom.Format("2006-01-02") || schedule.EffectiveThrough != nil && day > schedule.EffectiveThrough.Format("2006-01-02") {
			continue
		}
		return schedule.Timezone
	}
	return ""
}

func calendarIntervalIntersection(left, right calendar.TimeInterval) calendar.TimeInterval {
	start, end := left.Start, left.End
	if right.Start.After(start) {
		start = right.Start
	}
	if right.End.Before(end) {
		end = right.End
	}
	return calendar.TimeInterval{Start: start, End: end}
}

func excludeProposedCapacityEvent(events []calendar.CapacityEvent, proposed calendar.ProposedSchedule) []calendar.CapacityEvent {
	result := make([]calendar.CapacityEvent, 0, len(events))
	for _, event := range events {
		if proposed.Source.Type != "" && proposed.Source.ID != "" && event.Source.Type == proposed.Source.Type && event.Source.ID == proposed.Source.ID {
			continue
		}
		result = append(result, event)
	}
	return result
}

func (r *CalendarRepository) loadTimedDependencyConstraints(ctx context.Context, mspID, successorProjectionID string, successor calendar.TimeInterval) ([]calendar.DependencyConstraint, error) {
	rows, err := r.db.Query(ctx, `SELECT dependency.id::text,dependency.relationship_type,dependency.lead_lag_minutes,predecessor.starts_at,COALESCE(predecessor.ends_at,predecessor.starts_at),predecessor.terminal_state
FROM calendar_dependencies dependency
JOIN calendar_event_projections predecessor ON predecessor.id=dependency.predecessor_projection_id AND predecessor.msp_id=dependency.msp_id AND predecessor.client_id=dependency.client_id
WHERE dependency.msp_id=$1::uuid AND dependency.successor_projection_id=$2::uuid AND NOT predecessor.all_day
ORDER BY dependency.id`, mspID, successorProjectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTimedDependencyConstraints(rows, successor)
}

func (r *CalendarRepository) loadProposedDependencyConstraints(ctx context.Context, mspID string, proposed calendar.ProposedSchedule) ([]calendar.DependencyConstraint, error) {
	if proposed.ProjectionID == "" && (proposed.Source.Type == "" || proposed.Source.ID == "") {
		return nil, nil
	}
	rows, err := r.db.Query(ctx, `SELECT dependency.id::text,dependency.relationship_type,dependency.lead_lag_minutes,predecessor.starts_at,COALESCE(predecessor.ends_at,predecessor.starts_at),predecessor.terminal_state
FROM calendar_dependencies dependency
JOIN calendar_event_projections successor ON successor.id=dependency.successor_projection_id AND successor.msp_id=dependency.msp_id AND successor.client_id=dependency.client_id
JOIN calendar_event_projections predecessor ON predecessor.id=dependency.predecessor_projection_id AND predecessor.msp_id=dependency.msp_id AND predecessor.client_id=dependency.client_id
WHERE dependency.msp_id=$1::uuid AND NOT predecessor.all_day
AND ((NULLIF($2,'')::uuid IS NOT NULL AND successor.id=NULLIF($2,'')::uuid)
 OR (NULLIF($2,'')::uuid IS NULL AND successor.source_type=$3 AND successor.source_id=NULLIF($4,'')::uuid))
ORDER BY dependency.id`, mspID, proposed.ProjectionID, proposed.Source.Type, proposed.Source.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTimedDependencyConstraints(rows, proposed.Interval)
}

type dependencyConstraintRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func scanTimedDependencyConstraints(rows dependencyConstraintRows, successor calendar.TimeInterval) ([]calendar.DependencyConstraint, error) {
	result := []calendar.DependencyConstraint{}
	for rows.Next() {
		var id string
		var relationship calendar.DependencyType
		var lag int
		var predecessorStart, predecessorEnd time.Time
		var terminal calendar.TerminalState
		if err := rows.Scan(&id, &relationship, &lag, &predecessorStart, &predecessorEnd, &terminal); err != nil {
			return nil, err
		}
		if terminal == calendar.Completed || terminal == calendar.Cancelled {
			continue
		}
		required, unmet := successor, true
		lagDuration := time.Duration(lag) * time.Minute
		switch relationship {
		case calendar.FinishToStart:
			bound := predecessorEnd.Add(lagDuration)
			if successor.Start.Before(bound) {
				unmet = true
				required.End = bound
			}
		case calendar.StartToStart:
			bound := predecessorStart.Add(lagDuration)
			if successor.Start.Before(bound) {
				unmet = true
				required.End = bound
			}
		case calendar.FinishToFinish:
			bound := predecessorEnd.Add(lagDuration)
			if successor.End.Before(bound) {
				unmet = true
				shortfall := bound.Sub(successor.End)
				required.Start = successor.End.Add(-shortfall)
				if required.Start.Before(successor.Start) {
					required.Start = successor.Start
				}
				required.End = successor.End
			}
		default:
			continue
		}
		required = calendarIntervalIntersection(required, successor)
		if unmet && required.End.After(required.Start) {
			result = append(result, calendar.DependencyConstraint{ID: id, Type: relationship, Unmet: true, Interval: required, Related: calendar.SafeSourceRef{Type: "calendar_dependency", ID: id}})
		}
	}
	return result, rows.Err()
}

func advanceProjectionCursor(ctx context.Context, tx transaction, batch calendar.ProjectionBatch) error {
	c := batch.Cursor
	if c.ConsumerKey == "" {
		return nil
	}
	if strings.TrimSpace(c.ConsumerKey) == "" || c.OccurredAt.IsZero() || !internalid.ValidCanonical(c.EventID) {
		return calendar.ErrInvalidProjectionBatch
	}
	_, err := tx.Exec(ctx, `INSERT INTO calendar_projection_cursors(consumer_key,msp_id,last_outbox_occurred_at,last_event_id,updated_at) VALUES($1,$2::uuid,$3,$4::uuid,now()) ON CONFLICT(msp_id,consumer_key) DO UPDATE SET last_outbox_occurred_at=EXCLUDED.last_outbox_occurred_at,last_event_id=EXCLUDED.last_event_id,updated_at=now() WHERE (calendar_projection_cursors.last_outbox_occurred_at,calendar_projection_cursors.last_event_id)<(EXCLUDED.last_outbox_occurred_at,EXCLUDED.last_event_id)`, c.ConsumerKey, batch.Source.MSPID, c.OccurredAt, c.EventID)
	return err
}

func (r *CalendarRepository) AdvanceProjectionCursorAtomic(ctx context.Context, mspID string, cursor calendar.ProjectionCursor) error {
	if r == nil || r.db == nil || !internalid.ValidCanonical(mspID) || strings.TrimSpace(cursor.ConsumerKey) == "" || cursor.OccurredAt.IsZero() || !internalid.ValidCanonical(cursor.EventID) {
		return calendar.ErrInvalidProjectionBatch
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO calendar_projection_cursors(consumer_key,msp_id,last_outbox_occurred_at,last_event_id,updated_at) VALUES($1,$2::uuid,$3,$4::uuid,now()) ON CONFLICT(msp_id,consumer_key) DO UPDATE SET last_outbox_occurred_at=EXCLUDED.last_outbox_occurred_at,last_event_id=EXCLUDED.last_event_id,updated_at=now() WHERE (calendar_projection_cursors.last_outbox_occurred_at,calendar_projection_cursors.last_event_id)<(EXCLUDED.last_outbox_occurred_at,EXCLUDED.last_event_id)`, cursor.ConsumerKey, mspID, cursor.OccurredAt, cursor.EventID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}
func validProjectionBatchIDs(batch calendar.ProjectionBatch) bool {
	if batch.Source.Validate() != nil || batch.SourceRevision < 1 || !internalid.ValidCanonical(batch.Source.MSPID) || !internalid.ValidCanonical(batch.Source.ID) || (batch.Source.ClientID != "" && !internalid.ValidCanonical(batch.Source.ClientID)) {
		return false
	}
	for _, p := range batch.Projections {
		if !internalid.ValidCanonical(p.ID) {
			return false
		}
	}
	return true
}

var _ calendar.ProjectionRepository = (*CalendarRepository)(nil)
var _ calendar.ProjectionCursorRepository = (*CalendarRepository)(nil)

func (r *CalendarRepository) LoadDependencyProjection(ctx context.Context, projectionID string) (calendar.Projection, error) {
	var projection calendar.Projection
	var dimensions []byte
	if r == nil || r.db == nil || !internalid.ValidCanonical(projectionID) {
		return projection, calendar.ErrInvalidDependency
	}
	err := r.db.QueryRow(ctx, `SELECT id::text,msp_id::text,COALESCE(client_id::text,''),source_type,source_id::text,event_role,source_role_key,source_revision,title,all_day,starts_on,ends_on,starts_at,ends_at,COALESCE(timezone,''),scheduling_mode,capacity_bearing,COALESCE(owner_id::text,''),COALESCE(assignee_id::text,''),planned_minutes,terminal_state,filter_dimensions FROM calendar_event_projections WHERE id=$1::uuid`, projectionID).Scan(
		&projection.ID, &projection.Source.MSPID, &projection.Source.ClientID, &projection.Source.Type, &projection.Source.ID,
		&projection.EventRole, &projection.SourceRoleKey, &projection.SourceRevision, &projection.Title, &projection.AllDay,
		&projection.StartsOn, &projection.EndsOn, &projection.StartsAt, &projection.EndsAt, &projection.Timezone,
		&projection.SchedulingMode, &projection.CapacityBearing, &projection.OwnerID, &projection.AssigneeID,
		&projection.PlannedMinutes, &projection.TerminalState, &dimensions,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return calendar.Projection{}, scope.ErrNotFound
	}
	if err != nil {
		return projection, err
	}
	if err = json.Unmarshal(dimensions, &projection.Dimensions); err != nil {
		return calendar.Projection{}, err
	}
	return projection, nil
}

func (r *CalendarRepository) ListDependencies(ctx context.Context, mspID, clientID string) ([]calendar.Dependency, error) {
	if r == nil || r.db == nil || !internalid.ValidCanonical(mspID) || !internalid.ValidCanonical(clientID) {
		return nil, calendar.ErrInvalidDependency
	}
	rows, err := r.db.Query(ctx, `SELECT id::text,msp_id::text,client_id::text,predecessor_projection_id::text,successor_projection_id::text,relationship_type,lead_lag_minutes,version,created_at,created_by::text,updated_at,updated_by::text FROM calendar_dependencies WHERE msp_id=$1::uuid AND client_id=$2::uuid ORDER BY predecessor_projection_id,successor_projection_id,relationship_type,id`, mspID, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	dependencies := []calendar.Dependency{}
	for rows.Next() {
		var dependency calendar.Dependency
		if err = rows.Scan(&dependency.ID, &dependency.MSPID, &dependency.ClientID, &dependency.PredecessorID, &dependency.SuccessorID, &dependency.Type, &dependency.LeadLagMinutes, &dependency.Version, &dependency.CreatedAt, &dependency.CreatedBy, &dependency.UpdatedAt, &dependency.UpdatedBy); err != nil {
			return nil, err
		}
		dependencies = append(dependencies, dependency)
	}
	return dependencies, rows.Err()
}

func (r *CalendarRepository) InsertDependency(ctx context.Context, dependency calendar.Dependency, fact calendar.DependencyChangeFact) (created calendar.Dependency, err error) {
	if r == nil || r.db == nil || !validDependencyIDs(dependency) || !internalid.ValidCanonical(fact.EventID) || !internalid.ValidCanonical(fact.ActorID) || fact.OccurredAt.IsZero() {
		return created, calendar.ErrInvalidDependency
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return created, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('calendar-dependency:' || $1 || ':' || $2,0))`, dependency.MSPID, dependency.ClientID); err != nil {
		return created, err
	}
	var predecessorAllDay, successorAllDay bool
	if err = tx.QueryRow(ctx, `SELECT predecessor.all_day,successor.all_day FROM calendar_event_projections predecessor JOIN calendar_event_projections successor ON successor.id=$4::uuid AND successor.msp_id=$1::uuid AND successor.client_id=$2::uuid WHERE predecessor.id=$3::uuid AND predecessor.msp_id=$1::uuid AND predecessor.client_id=$2::uuid`, dependency.MSPID, dependency.ClientID, dependency.PredecessorID, dependency.SuccessorID).Scan(&predecessorAllDay, &successorAllDay); errors.Is(err, pgx.ErrNoRows) {
		return created, calendar.ErrInvalidDependency
	} else if err != nil {
		return created, err
	}
	if predecessorAllDay != successorAllDay || predecessorAllDay && dependency.LeadLagMinutes%(24*60) != 0 {
		return created, calendar.ErrLeadLagPrecision
	}
	var cycle bool
	if err = tx.QueryRow(ctx, `WITH RECURSIVE reachable(id) AS (
 SELECT successor_projection_id FROM calendar_dependencies
  WHERE msp_id=$1::uuid AND client_id=$2::uuid AND predecessor_projection_id=$3::uuid
 UNION
 SELECT dependency.successor_projection_id FROM calendar_dependencies dependency
 JOIN reachable ON dependency.predecessor_projection_id=reachable.id
  WHERE dependency.msp_id=$1::uuid AND dependency.client_id=$2::uuid
) SELECT EXISTS(SELECT 1 FROM reachable WHERE id=$4::uuid)`, dependency.MSPID, dependency.ClientID, dependency.SuccessorID, dependency.PredecessorID).Scan(&cycle); err != nil {
		return created, err
	}
	if cycle {
		return created, calendar.ErrDependencyCycle
	}
	tag, err := tx.Exec(ctx, `INSERT INTO calendar_dependencies(id,msp_id,client_id,predecessor_projection_id,successor_projection_id,relationship_type,lead_lag_minutes,version,created_at,created_by,updated_at,updated_by) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,$7,$8,$9,$10::uuid,$11,$12::uuid) ON CONFLICT DO NOTHING`, dependency.ID, dependency.MSPID, dependency.ClientID, dependency.PredecessorID, dependency.SuccessorID, dependency.Type, dependency.LeadLagMinutes, dependency.Version, dependency.CreatedAt, dependency.CreatedBy, dependency.UpdatedAt, dependency.UpdatedBy)
	if err != nil {
		return created, err
	}
	if tag.RowsAffected() != 1 {
		return created, calendar.ErrDuplicateDependency
	}
	if _, err = tx.Exec(ctx, `INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source,data) VALUES($1::uuid,'calendar.dependency.created',1,$2,$3::uuid,$4::uuid,'technician',$5::uuid,'calendar_dependency',$6::uuid,$7,$1::uuid,'calendar',jsonb_build_object('fact_kind','dependency','predecessor_projection_id',$8::text,'successor_projection_id',$9::text,'projection_ids',jsonb_build_array($8::text,$9::text)))`, fact.EventID, fact.OccurredAt, dependency.MSPID, dependency.ClientID, fact.ActorID, dependency.ID, dependency.Version, dependency.PredecessorID, dependency.SuccessorID); err != nil {
		return created, err
	}
	if err = tx.Commit(ctx); err != nil {
		return created, err
	}
	return dependency, nil
}

func (r *CalendarRepository) LoadDependency(ctx context.Context, mspID, dependencyID string) (calendar.Dependency, error) {
	var dependency calendar.Dependency
	if r == nil || r.db == nil || !internalid.ValidCanonical(mspID) || !internalid.ValidCanonical(dependencyID) {
		return dependency, calendar.ErrInvalidDependency
	}
	err := r.db.QueryRow(ctx, `SELECT id::text,msp_id::text,client_id::text,predecessor_projection_id::text,successor_projection_id::text,relationship_type,lead_lag_minutes,version,created_at,created_by::text,updated_at,updated_by::text FROM calendar_dependencies WHERE id=$1::uuid AND msp_id=$2::uuid`, dependencyID, mspID).Scan(&dependency.ID, &dependency.MSPID, &dependency.ClientID, &dependency.PredecessorID, &dependency.SuccessorID, &dependency.Type, &dependency.LeadLagMinutes, &dependency.Version, &dependency.CreatedAt, &dependency.CreatedBy, &dependency.UpdatedAt, &dependency.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return calendar.Dependency{}, scope.ErrNotFound
	}
	return dependency, err
}

func (r *CalendarRepository) DeleteDependency(ctx context.Context, dependency calendar.Dependency, fact calendar.DependencyDeletionFact) (err error) {
	if r == nil || r.db == nil || !validDependencyIDs(dependency) || !internalid.ValidCanonical(fact.EventID) || !internalid.ValidCanonical(fact.ActorID) || fact.OccurredAt.IsZero() {
		return calendar.ErrInvalidDependency
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('calendar-dependency:' || $1 || ':' || $2,0))`, dependency.MSPID, dependency.ClientID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM calendar_dependencies WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid AND version=$4`, dependency.ID, dependency.MSPID, dependency.ClientID, dependency.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return calendar.ErrInvalidDependency
	}
	if _, err = tx.Exec(ctx, `INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source,data) VALUES($1::uuid,'calendar.dependency.deleted',1,$2,$3::uuid,$4::uuid,'technician',$5::uuid,'calendar_dependency',$6::uuid,$7,$1::uuid,'calendar',jsonb_build_object('fact_kind','dependency','predecessor_projection_id',$8::text,'successor_projection_id',$9::text,'projection_ids',jsonb_build_array($8::text,$9::text)))`, fact.EventID, fact.OccurredAt, dependency.MSPID, dependency.ClientID, fact.ActorID, dependency.ID, dependency.Version+1, dependency.PredecessorID, dependency.SuccessorID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func validDependencyIDs(dependency calendar.Dependency) bool {
	if !internalid.ValidCanonical(dependency.ID) || !internalid.ValidCanonical(dependency.MSPID) || !internalid.ValidCanonical(dependency.ClientID) || !internalid.ValidCanonical(dependency.PredecessorID) || !internalid.ValidCanonical(dependency.SuccessorID) || !internalid.ValidCanonical(dependency.CreatedBy) || !internalid.ValidCanonical(dependency.UpdatedBy) || dependency.PredecessorID == dependency.SuccessorID || dependency.Version < 1 || dependency.CreatedAt.IsZero() || dependency.UpdatedAt.IsZero() {
		return false
	}
	return dependency.Type == calendar.FinishToStart || dependency.Type == calendar.StartToStart || dependency.Type == calendar.FinishToFinish
}

func (r *CalendarRepository) ResolveAffectedHealth(ctx context.Context, event calendar.HealthRecomputeEvent) ([]calendar.HealthProjectionContext, error) {
	if r == nil || r.db == nil || !validHealthEvent(event) {
		return nil, calendar.ErrInvalidHealthRecompute
	}
	rows, err := r.db.Query(ctx, `WITH RECURSIVE seed(id) AS (
 SELECT projection.id FROM calendar_event_projections projection
 WHERE projection.msp_id=$1::uuid AND (
   ($2 IN ('projection','source_status','sla') AND (projection.id=$3::uuid OR projection.source_id=$3::uuid))
   OR ($2 IN ('schedule','availability','capacity') AND (projection.id=$3::uuid OR projection.assignee_id=$3::uuid))
   OR ($2='dependency' AND (projection.id=$3::uuid OR projection.id=ANY($4::uuid[]) OR EXISTS(
     SELECT 1 FROM calendar_dependencies dependency WHERE dependency.msp_id=$1::uuid
       AND dependency.id=$3::uuid
       AND projection.id IN (dependency.predecessor_projection_id,dependency.successor_projection_id)
   )))
 )
), affected(id) AS (
 SELECT id FROM seed
 UNION
 SELECT dependency.successor_projection_id FROM calendar_dependencies dependency
 JOIN affected ON affected.id=dependency.predecessor_projection_id
 WHERE dependency.msp_id=$1::uuid
)
SELECT projection.id::text,projection.msp_id::text,COALESCE(projection.client_id::text,''),
       projection.source_type,projection.source_id::text,projection.source_revision,
       projection.terminal_state,
       CASE WHEN projection.all_day THEN COALESCE(projection.ends_on,projection.starts_on) END AS authoritative_deadline_on,
       CASE WHEN NOT projection.all_day THEN COALESCE(projection.ends_at,projection.starts_at) END AS authoritative_deadline_at,
       projection.health_inputs,
       CASE projection.source_type
         WHEN 'work_record' THEN EXISTS(SELECT 1 FROM work_records source WHERE source.id=projection.source_id AND source.msp_id=projection.msp_id AND source.client_id=projection.client_id AND source.status='blocked')
         WHEN 'task' THEN EXISTS(SELECT 1 FROM tasks source WHERE source.id=projection.source_id AND source.msp_id=projection.msp_id AND source.client_id=projection.client_id AND source.status='blocked')
         WHEN 'project' THEN EXISTS(SELECT 1 FROM projects source WHERE source.id=projection.source_id AND source.msp_id=projection.msp_id AND source.client_id=projection.client_id AND source.lifecycle_state='blocked')
         WHEN 'phase' THEN EXISTS(SELECT 1 FROM phases source WHERE source.id=projection.source_id AND source.msp_id=projection.msp_id AND source.client_id=projection.client_id AND source.state='blocked')
         WHEN 'milestone' THEN EXISTS(SELECT 1 FROM project_milestones source WHERE source.id=projection.source_id AND source.msp_id=projection.msp_id AND source.client_id=projection.client_id AND source.status='blocked')
         ELSE false
       END AS source_blocked,
       EXISTS(
         SELECT 1 FROM calendar_dependencies dependency
         JOIN calendar_event_projections predecessor
           ON predecessor.id=dependency.predecessor_projection_id
          AND predecessor.msp_id=dependency.msp_id
          AND predecessor.client_id=dependency.client_id
         WHERE dependency.msp_id=projection.msp_id
           AND dependency.client_id=projection.client_id
           AND dependency.successor_projection_id=projection.id
           AND (predecessor.terminal_state<>'completed'
             OR (predecessor.all_day AND projection.all_day AND MOD(dependency.lead_lag_minutes,1440)=0 AND (
               (dependency.relationship_type='finish_to_start' AND COALESCE(predecessor.ends_on,predecessor.starts_on)+1+(dependency.lead_lag_minutes / 1440)>projection.starts_on)
               OR (dependency.relationship_type='start_to_start' AND predecessor.starts_on+(dependency.lead_lag_minutes / 1440)>projection.starts_on)
               OR (dependency.relationship_type='finish_to_finish' AND COALESCE(predecessor.ends_on,predecessor.starts_on)+(dependency.lead_lag_minutes / 1440)>COALESCE(projection.ends_on,projection.starts_on))
             ))
             OR (NOT predecessor.all_day AND NOT projection.all_day AND (
               (dependency.relationship_type='finish_to_start' AND COALESCE(predecessor.ends_at,predecessor.starts_at)+make_interval(mins=>dependency.lead_lag_minutes)>projection.starts_at)
               OR (dependency.relationship_type='start_to_start' AND predecessor.starts_at+make_interval(mins=>dependency.lead_lag_minutes)>projection.starts_at)
               OR (dependency.relationship_type='finish_to_finish' AND COALESCE(predecessor.ends_at,predecessor.starts_at)+make_interval(mins=>dependency.lead_lag_minutes)>COALESCE(projection.ends_at,projection.starts_at))
             )))
       ) AS dependency_blocked
FROM calendar_event_projections projection
JOIN affected ON affected.id=projection.id
WHERE projection.msp_id=$1::uuid
ORDER BY projection.id`, event.MSPID, event.Kind, event.SubjectID, event.ProjectionIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	contexts := []calendar.HealthProjectionContext{}
	for rows.Next() {
		var value calendar.HealthProjectionContext
		var terminal calendar.TerminalState
		var deadlineOn, deadlineAt *time.Time
		var healthJSON []byte
		var sourceBlocked, dependencyBlocked bool
		if err = rows.Scan(&value.ProjectionID, &value.MSPID, &value.ClientID, &value.Source.Type, &value.Source.ID, &value.SourceRevision, &terminal, &deadlineOn, &deadlineAt, &healthJSON, &sourceBlocked, &dependencyBlocked); err != nil {
			return nil, err
		}
		value.Source.MSPID, value.Source.ClientID = value.MSPID, value.ClientID
		var inputs calendar.HealthInputs
		if len(healthJSON) > 0 {
			if err = json.Unmarshal(healthJSON, &inputs); err != nil {
				return nil, err
			}
		}
		value.Context = calendar.HealthContext{TerminalState: terminal, SourceBlocked: inputs.Blocked || sourceBlocked, UnmetDependency: inputs.DependencyBlocked || dependencyBlocked, HardConstraint: inputs.HardConstraint, DueAt: inputs.DueAt, EndsAt: deadlineAt, EndsOn: deadlineOn, CapacityShortage: inputs.CapacityShortage, DependencyDelay: inputs.DependencyDelay, ScheduleVariance: inputs.ScheduleVariance, SLAAtRisk: inputs.SLAAtRisk, SLAAtRiskAt: inputs.RiskAt}
		contexts = append(contexts, value)
	}
	return contexts, rows.Err()
}

func (r *CalendarRepository) ApplyHealthResultsAtomic(ctx context.Context, event calendar.HealthRecomputeEvent, updates []calendar.HealthUpdate) (applied bool, err error) {
	if r == nil || r.db == nil || !validHealthEvent(event) {
		return false, calendar.ErrInvalidHealthRecompute
	}
	for _, update := range updates {
		if !validHealthUpdate(event.MSPID, update) {
			return false, calendar.ErrInvalidHealthRecompute
		}
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	claim, err := tx.Exec(ctx, `INSERT INTO calendar_health_event_claims(source_event_id,rule_version,msp_id,fact_kind,processed_at) VALUES($1::uuid,$2,$3::uuid,$4,$5) ON CONFLICT DO NOTHING`, event.SourceEventID, calendar.CurrentHealthRuleVersion, event.MSPID, event.Kind, event.OccurredAt)
	if err != nil {
		return false, err
	}
	if claim.RowsAffected() == 0 {
		if err = tx.Commit(ctx); err != nil {
			return false, err
		}
		return false, nil
	}
	sort.Slice(updates, func(i, j int) bool { return updates[i].ProjectionID < updates[j].ProjectionID })
	for _, update := range updates {
		var sourceType, sourceID, clientID, eventRole, assigneeID string
		var sourceRevision int64
		var terminalState calendar.TerminalState
		var priorHealth calendar.HealthState
		lockErr := tx.QueryRow(ctx, `SELECT source_type,source_id::text,COALESCE(client_id::text,''),event_role,source_revision,terminal_state,COALESCE(assignee_id::text,''),health_state FROM calendar_event_projections WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid AND source_type=$4 AND source_id=$5::uuid AND source_revision=$6 FOR UPDATE`, update.ProjectionID, update.MSPID, update.ClientID, update.Source.Type, update.Source.ID, update.SourceRevision).Scan(&sourceType, &sourceID, &clientID, &eventRole, &sourceRevision, &terminalState, &assigneeID, &priorHealth)
		if errors.Is(lockErr, pgx.ErrNoRows) {
			continue
		}
		if lockErr != nil {
			return false, lockErr
		}
		reasons, marshalErr := json.Marshal(update.Result.Reasons)
		if marshalErr != nil {
			return false, marshalErr
		}
		tag, writeErr := tx.Exec(ctx, `UPDATE calendar_event_projections SET health_state=$2,health_reasons=$3::jsonb,health_rule_version=$4,health_evaluated_at=$5,updated_at=updated_at WHERE id=$1::uuid AND msp_id=$6::uuid AND client_id IS NOT DISTINCT FROM NULLIF($7,'')::uuid AND source_revision=$8 AND (health_state,health_reasons,health_rule_version) IS DISTINCT FROM ($2,$3::jsonb,$4)`, update.ProjectionID, update.Result.State, reasons, update.Result.RuleVersion, update.Result.EvaluatedAt, update.MSPID, update.ClientID, update.SourceRevision)
		if writeErr != nil {
			return false, writeErr
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		if _, err = tx.Exec(ctx, `INSERT INTO calendar_live_changes(msp_id,client_id,client_scope_key,projection_id,source_type,source_id,event_role,change_type,source_revision) SELECT projection.msp_id,projection.client_id,projection.client_scope_key,projection.id,projection.source_type,projection.source_id,projection.event_role,'health_changed',projection.source_revision FROM calendar_event_projections projection WHERE projection.id=$1::uuid AND projection.msp_id=$2::uuid AND projection.client_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid`, update.ProjectionID, update.MSPID, update.ClientID); err != nil {
			return false, err
		}
		if terminalState != calendar.Active || strings.TrimSpace(assigneeID) == "" || actionableCalendarHealth(priorHealth) || !actionableCalendarHealth(update.Result.State) {
			continue
		}
		urgency := "important"
		if update.Result.State == calendar.HealthBlocked {
			urgency = "urgent"
		}
		seed := strings.Join([]string{event.SourceEventID, update.ProjectionID, fmt.Sprint(sourceRevision), assigneeID, "conflict"}, "\x00")
		payload := canonicalCalendarEventData{
			RecipientID: assigneeID,
			ChangeClass: "conflict",
			Urgency:     urgency,
			SourceRefs: []canonicalCalendarSourceRef{{
				Type: sourceType, ID: sourceID, ClientID: clientID, EventRole: eventRole, SourceRevision: sourceRevision,
			}},
			ActionPath: "/calendar",
		}
		if err = writeCanonicalCalendarEvent(ctx, tx, schedulingFactID(seed, "calendar.schedule_changed"), event.OccurredAt, event.MSPID, clientID, "system", "00000000-0000-0000-0000-000000000000", sourceType, sourceID, sourceRevision, event.SourceEventID, payload); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func actionableCalendarHealth(state calendar.HealthState) bool {
	switch state {
	case calendar.HealthBlocked, calendar.HealthOverdue, calendar.HealthAtRisk:
		return true
	default:
		return false
	}
}

func validHealthEvent(event calendar.HealthRecomputeEvent) bool {
	if !internalid.ValidCanonical(event.SourceEventID) || !internalid.ValidCanonical(event.MSPID) || !internalid.ValidCanonical(event.SubjectID) || event.OccurredAt.IsZero() {
		return false
	}
	seen := map[string]struct{}{}
	for _, projectionID := range event.ProjectionIDs {
		if !internalid.ValidCanonical(projectionID) {
			return false
		}
		if _, exists := seen[projectionID]; exists {
			return false
		}
		seen[projectionID] = struct{}{}
	}
	switch event.Kind {
	case calendar.HealthFactProjection, calendar.HealthFactSourceStatus, calendar.HealthFactDependency, calendar.HealthFactSchedule, calendar.HealthFactAvailability, calendar.HealthFactCapacity, calendar.HealthFactSLA:
		return true
	default:
		return false
	}
}

func validHealthUpdate(mspID string, update calendar.HealthUpdate) bool {
	if !internalid.ValidCanonical(update.ProjectionID) || update.MSPID != mspID || !internalid.ValidCanonical(update.MSPID) || (update.ClientID != "" && !internalid.ValidCanonical(update.ClientID)) || update.Source.MSPID != update.MSPID || update.Source.ClientID != update.ClientID || update.SourceRevision < 1 || update.Source.Validate() != nil || !internalid.ValidCanonical(update.Source.ID) || update.Result.RuleVersion != calendar.CurrentHealthRuleVersion || update.Result.EvaluatedAt.IsZero() {
		return false
	}
	switch update.Result.State {
	case calendar.HealthBlocked, calendar.HealthOverdue, calendar.HealthAtRisk, calendar.HealthOnTrack, calendar.HealthTerminal:
		return true
	default:
		return false
	}
}

var _ calendar.DependencyRepository = (*CalendarRepository)(nil)
var _ calendar.HealthRepository = (*CalendarRepository)(nil)

func (r *CalendarRepository) LoadWorkRecord(ctx context.Context, ref calendar.SourceRef) (adapters.WorkRecordSource, error) {
	var s adapters.WorkRecordSource
	var recurrence []byte
	err := r.db.QueryRow(ctx, `SELECT work.id::text,work.msp_id::text,work.client_id::text,work.title,work.record_type,work.status,work.priority,COALESCE(work.primary_owner_id::text,''),COALESCE(work.queue_id::text,''),COALESCE(queue.team_id::text,''),COALESCE(work.service_id::text,''),COALESCE(work.asset_id::text,''),work.version,work.planned_effort_minutes,work.scheduling_mode,work.scheduled_starts_at,work.scheduled_ends_at,COALESCE(work.schedule_timezone,''),work.due_on,work.follow_up_on,work.schedule_recurrence,COALESCE(sla.policy_id::text,''),sla.response_due_at,sla.resolution_due_at FROM work_records work LEFT JOIN queues queue ON queue.id=work.queue_id AND queue.msp_id=work.msp_id LEFT JOIN LATERAL(SELECT policy_id,response_due_at,resolution_due_at FROM work_record_slas WHERE work_record_id=work.id AND msp_id=work.msp_id AND client_id=work.client_id ORDER BY version DESC LIMIT 1)sla ON true WHERE work.id=$1::uuid AND work.msp_id=$2::uuid AND work.client_id=$3::uuid AND work.deleted_at IS NULL`, ref.ID, ref.MSPID, ref.ClientID).Scan(&s.ID, &s.MSPID, &s.ClientID, &s.Title, &s.RecordType, &s.Status, &s.Priority, &s.OwnerID, &s.QueueID, &s.TeamID, &s.ServiceID, &s.AssetID, &s.Version, &s.PlannedMinutes, &s.SchedulingMode, &s.ScheduledStartsAt, &s.ScheduledEndsAt, &s.Timezone, &s.DueOn, &s.FollowUpOn, &recurrence, &s.SLAID, &s.SLAResponseDueAt, &s.SLAResolutionDueAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, scope.ErrNotFound
	}
	if err == nil && len(recurrence) > 0 {
		err = json.Unmarshal(recurrence, &s.Recurrence)
	}
	return s, err
}

func (r *CalendarRepository) LoadTask(ctx context.Context, ref calendar.SourceRef) (adapters.TaskSource, error) {
	var s adapters.TaskSource
	var recurrence []byte
	err := r.db.QueryRow(ctx, `SELECT task.id::text,task.msp_id::text,task.client_id::text,task.title,task.status,COALESCE(task.owner_id::text,''),task.version,COALESCE(task.estimate_minutes,0),task.scheduling_mode,task.scheduled_starts_at,task.scheduled_ends_at,COALESCE(task.schedule_timezone,''),task.due_on,task.schedule_recurrence,COALESCE(CASE WHEN task.parent_type='project' THEN task.parent_id::text WHEN task.parent_type='phase' THEN phase.project_id::text ELSE '' END,'') FROM tasks task LEFT JOIN phases phase ON task.parent_type='phase' AND phase.id=task.parent_id AND phase.msp_id=task.msp_id AND phase.client_id=task.client_id WHERE task.id=$1::uuid AND task.msp_id=$2::uuid AND task.client_id=$3::uuid`, ref.ID, ref.MSPID, ref.ClientID).Scan(&s.ID, &s.MSPID, &s.ClientID, &s.Title, &s.Status, &s.OwnerID, &s.Version, &s.PlannedMinutes, &s.SchedulingMode, &s.ScheduledStartsAt, &s.ScheduledEndsAt, &s.Timezone, &s.DueOn, &recurrence, &s.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, scope.ErrNotFound
	}
	if err == nil && len(recurrence) > 0 {
		err = json.Unmarshal(recurrence, &s.Recurrence)
	}
	return s, err
}

func (r *CalendarRepository) LoadProject(ctx context.Context, ref calendar.SourceRef) (projects.Project, error) {
	return NewProjectRepository(r.db).FindProject(ctx, scope.Target{MSPID: ref.MSPID, ClientID: ref.ClientID}, projects.ProjectID(ref.ID))
}
func (r *CalendarRepository) LoadPhase(ctx context.Context, ref calendar.SourceRef) (projects.Phase, error) {
	return NewProjectRepository(r.db).FindPhase(ctx, scope.Target{MSPID: ref.MSPID, ClientID: ref.ClientID}, projects.PhaseID(ref.ID))
}
func (r *CalendarRepository) LoadMilestone(ctx context.Context, ref calendar.SourceRef) (projects.Milestone, error) {
	return NewProjectRepository(r.db).FindMilestone(ctx, scope.Target{MSPID: ref.MSPID, ClientID: ref.ClientID}, ref.ID)
}
func (r *CalendarRepository) LoadResourcePlan(ctx context.Context, ref calendar.SourceRef) (projects.ResourcePlan, error) {
	var plan projects.ResourcePlan
	err := r.db.QueryRow(ctx, `SELECT id::text,project_id::text,COALESCE(phase_id::text,''),msp_id::text,client_id::text,COALESCE(role_id::text,''),COALESCE(team_id::text,''),starts_on,ends_on,planned_minutes,version FROM resource_plans WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid`, ref.ID, ref.MSPID, ref.ClientID).Scan(&plan.ID, &plan.ProjectID, &plan.PhaseID, &plan.MSPID, &plan.ClientID, &plan.RoleID, &plan.TeamID, &plan.StartsOn, &plan.EndsOn, &plan.PlannedMinutes, &plan.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return plan, scope.ErrNotFound
	}
	return plan, err
}
func (r *CalendarRepository) LoadSchedule(ctx context.Context, ref calendar.SourceRef) (workforce.Schedule, error) {
	var schedule workforce.Schedule
	err := r.db.QueryRow(ctx, `SELECT id::text,msp_id::text,technician_id::text,timezone,effective_from,effective_through,version,created_at,created_by::text FROM technician_schedule_versions WHERE id=$1::uuid AND msp_id=$2::uuid AND lifecycle_state='active'`, ref.ID, ref.MSPID).Scan(&schedule.ID, &schedule.MSPID, &schedule.TechnicianID, &schedule.Timezone, &schedule.EffectiveFrom, &schedule.EffectiveThrough, &schedule.Version, &schedule.CreatedAt, &schedule.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return workforce.Schedule{}, scope.ErrNotFound
	}
	if err != nil {
		return workforce.Schedule{}, err
	}
	rows, err := r.db.Query(ctx, `SELECT id::text,weekday,(extract(hour FROM starts_local)*60+extract(minute FROM starts_local))::integer,(extract(hour FROM ends_local)*60+extract(minute FROM ends_local))::integer,capacity_percent FROM technician_schedule_windows WHERE schedule_version_id=$1::uuid AND msp_id=$2::uuid AND technician_id=$3::uuid ORDER BY weekday,starts_local,id`, schedule.ID, schedule.MSPID, schedule.TechnicianID)
	if err != nil {
		return workforce.Schedule{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var window workforce.WeeklyWindow
		var weekday int
		if err = rows.Scan(&window.ID, &weekday, &window.StartsMinute, &window.EndsMinute, &window.CapacityPercent); err != nil {
			return workforce.Schedule{}, err
		}
		window.Weekday = time.Weekday(weekday)
		schedule.Windows = append(schedule.Windows, window)
	}
	return schedule, rows.Err()
}
func (r *CalendarRepository) LoadPTO(ctx context.Context, ref calendar.SourceRef) (workforce.PTORequest, error) {
	return NewWorkforceRepository(r.db).FindPTO(ctx, ref.MSPID, ref.ID)
}
func (r *CalendarRepository) LoadMaintenance(ctx context.Context, ref calendar.SourceRef) (commitments.MaintenanceWindow, error) {
	return NewCommitmentRepository(r.db).FindMaintenance(ctx, ref.MSPID, ref.ID)
}
func (r *CalendarRepository) LoadCommercial(ctx context.Context, ref calendar.SourceRef) (commitments.CommercialCommitment, error) {
	return NewCommitmentRepository(r.db).FindCommercial(ctx, scope.Target{MSPID: ref.MSPID, ClientID: ref.ClientID}, ref.ID)
}
func (r *CalendarRepository) LoadCustomDate(ctx context.Context, ref calendar.SourceRef) (customfields.DateDefinition, customfields.DateValue, error) {
	var d customfields.DateDefinition
	var v customfields.DateValue
	err := r.db.QueryRow(ctx, `SELECT field.id::text,field.msp_id::text,field.internal_key,field.label,field.lifecycle_state,field.object_type,field.value_kind,field.version,value.id::text,value.field_id::text,value.msp_id::text,COALESCE(value.client_id::text,''),value.object_id::text,value.object_type,value.source_revision,value.version,value.date_value,value.timestamp_value,COALESCE(value.timezone,''),value.created_at,value.created_by::text,value.updated_at,value.updated_by::text FROM object_custom_date_values value JOIN calendar_custom_date_fields field ON field.id=value.field_id AND field.msp_id=value.msp_id AND field.object_type=value.object_type WHERE value.id=$1::uuid AND value.msp_id=$2::uuid AND value.client_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid`, ref.ID, ref.MSPID, ref.ClientID).Scan(&d.ID, &d.MSPID, &d.InternalKey, &d.Label, &d.LifecycleState, &d.ObjectType, &d.ValueKind, &d.Version, &v.ID, &v.FieldID, &v.MSPID, &v.ClientID, &v.ObjectID, &v.ObjectType, &v.SourceRevision, &v.Version, &v.DateValue, &v.TimestampValue, &v.Timezone, &v.CreatedAt, &v.CreatedBy, &v.UpdatedAt, &v.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, v, scope.ErrNotFound
	}
	return d, v, err
}
func (r *CalendarRepository) LoadCustomCapacity(ctx context.Context, value customfields.DateValue) (string, int64, error) {
	var assignee string
	var minutes int64
	switch value.ObjectType {
	case customfields.ObjectWorkRecord:
		err := r.db.QueryRow(ctx, `SELECT COALESCE(primary_owner_id::text,''),planned_effort_minutes FROM work_records WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid`, value.ObjectID, value.MSPID, value.ClientID).Scan(&assignee, &minutes)
		return assignee, minutes, err
	case customfields.ObjectTask:
		err := r.db.QueryRow(ctx, `SELECT COALESCE(owner_id::text,''),COALESCE(estimate_minutes,0) FROM tasks WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid`, value.ObjectID, value.MSPID, value.ClientID).Scan(&assignee, &minutes)
		return assignee, minutes, err
	default:
		return "", 0, nil
	}
}
func (r *CalendarRepository) ResolveCalendarTags(ctx context.Context, ref calendar.SourceRef) ([]string, error) {
	if ref.Type != "work_record" && ref.Type != "task" && ref.Type != "project" {
		return []string{}, nil
	}
	rows, err := r.db.Query(ctx, `SELECT tag_id::text FROM object_tag_assignments WHERE msp_id=$1::uuid AND client_id=$2::uuid AND object_type=$3 AND object_id=$4::uuid ORDER BY tag_id`, ref.MSPID, ref.ClientID, ref.Type, ref.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		values = append(values, id)
	}
	return values, rows.Err()
}
func (r *CalendarRepository) LoadCustomRoleDefinitions(ctx context.Context, mspID string) (map[string]calendar.EventRoleDefinition, error) {
	rows, err := r.db.Query(ctx, `SELECT id::text,internal_key,event_role,scheduling_mode,capacity_bearing,calendar_mode='read_only' FROM calendar_custom_date_fields WHERE msp_id=$1::uuid AND lifecycle_state='active' ORDER BY id`, mspID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]calendar.EventRoleDefinition{}
	for rows.Next() {
		var id, key string
		var d calendar.EventRoleDefinition
		d.SourceType = "custom_date"
		if err = rows.Scan(&id, &key, &d.Role, &d.SchedulingMode, &d.CapacityBearing, &d.ReadOnly); err != nil {
			return nil, err
		}
		d.SourceRoleKey = key
		result[id] = d
	}
	return result, rows.Err()
}

var _ adapters.WorkSource = (*CalendarRepository)(nil)
var _ adapters.ProjectSource = (*CalendarRepository)(nil)
var _ adapters.WorkforceSource = (*CalendarRepository)(nil)
var _ adapters.CommitmentSource = (*CalendarRepository)(nil)
var _ adapters.CustomDateSource = (*CalendarRepository)(nil)
var _ adapters.CustomCapacitySource = (*CalendarRepository)(nil)
var _ adapters.TagResolver = (*CalendarRepository)(nil)
var _ adapters.ProjectionRevisionResolver = (*CalendarRepository)(nil)

func (r *CalendarRepository) ResolveCalendarProjectionRevision(ctx context.Context, ref calendar.SourceRef, base int64) (int64, error) {
	if base < 1 {
		return 0, calendar.ErrInvalidProjectionBatch
	}
	extras := int64(0)
	switch ref.Type {
	case "work_record":
		err := r.db.QueryRow(ctx, `SELECT COALESCE(classification.version,0)+COALESCE(sla.version,0)
FROM work_records work
LEFT JOIN classification_object_versions classification ON classification.msp_id=work.msp_id AND classification.client_id=work.client_id AND classification.object_type='work_record' AND classification.object_id=work.id
LEFT JOIN LATERAL (SELECT MAX(version) version FROM work_record_slas WHERE msp_id=work.msp_id AND client_id=work.client_id AND work_record_id=work.id) sla ON true
WHERE work.id=$1::uuid AND work.msp_id=$2::uuid AND work.client_id=$3::uuid`, ref.ID, ref.MSPID, ref.ClientID).Scan(&extras)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, scope.ErrNotFound
		}
		if err != nil {
			return 0, err
		}
	case "task", "project":
		if err := r.db.QueryRow(ctx, `SELECT COALESCE(version,0) FROM classification_object_versions WHERE msp_id=$1::uuid AND client_id=$2::uuid AND object_type=$3 AND object_id=$4::uuid`, ref.MSPID, ref.ClientID, ref.Type, ref.ID).Scan(&extras); errors.Is(err, pgx.ErrNoRows) {
			extras = 0
		} else if err != nil {
			return 0, err
		}
	case "custom_date":
		err := r.db.QueryRow(ctx, `SELECT value.version+field.version+COALESCE(classification.version,0)
FROM object_custom_date_values value
JOIN calendar_custom_date_fields field ON field.id=value.field_id AND field.msp_id=value.msp_id
LEFT JOIN classification_object_versions classification ON classification.msp_id=value.msp_id AND classification.client_id IS NOT DISTINCT FROM value.client_id AND classification.object_type=value.object_type AND classification.object_id=value.object_id
WHERE value.id=$1::uuid AND value.msp_id=$2::uuid AND value.client_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid`, ref.ID, ref.MSPID, ref.ClientID).Scan(&extras)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, scope.ErrNotFound
		}
		if err != nil {
			return 0, err
		}
	}
	if extras < 0 || base > math.MaxInt64-extras {
		return 0, calendar.ErrInvalidProjectionBatch
	}
	return base + extras, nil
}

func (r *CalendarRepository) ResolveProjectionSource(ctx context.Context, event mutation.EventRecord) (calendar.ResolvedProjectionSource, error) {
	typeMap := map[string]string{"work_record": "work_record", "task": "task", "project": "project", "phase": "phase", "project_milestone": "milestone", "resource_plan": "resource_plan", "technician_schedule": "technician_schedule", "pto_request": "pto", "maintenance_window": "maintenance_window", "commercial_commitment": "commercial_commitment"}
	if sourceType, ok := typeMap[event.SubjectType]; ok {
		return calendar.ResolvedProjectionSource{Source: calendar.SourceRef{MSPID: event.MSPID, ClientID: event.ClientID, Type: sourceType, ID: event.SubjectID}, Relevant: true}, nil
	}
	if event.SubjectType == "work_record_sla" {
		var ref calendar.SourceRef
		err := r.db.QueryRow(ctx, `SELECT msp_id::text,client_id::text,work_record_id::text FROM work_record_slas WHERE id=$1::uuid AND msp_id=$2::uuid`, event.SubjectID, event.MSPID).Scan(&ref.MSPID, &ref.ClientID, &ref.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return calendar.ResolvedProjectionSource{}, nil
		}
		if err != nil {
			return calendar.ResolvedProjectionSource{}, err
		}
		ref.Type = "work_record"
		return calendar.ResolvedProjectionSource{Source: ref, Relevant: true}, nil
	}
	if event.EventType != "custom_date.set" {
		return calendar.ResolvedProjectionSource{}, nil
	}
	var ref calendar.SourceRef
	err := r.db.QueryRow(ctx, `SELECT value.msp_id::text,COALESCE(value.client_id::text,''),value.id::text FROM event_outbox event JOIN object_custom_date_values value ON value.msp_id=event.msp_id AND value.object_type=event.subject_type AND value.object_id=event.subject_id AND value.field_id=NULLIF(event.data->>'field_id','')::uuid WHERE event.event_id=$1::uuid AND event.msp_id=$2::uuid`, event.EventID, event.MSPID).Scan(&ref.MSPID, &ref.ClientID, &ref.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return calendar.ResolvedProjectionSource{}, nil
	}
	if err != nil {
		return calendar.ResolvedProjectionSource{}, err
	}
	ref.Type = "custom_date"
	return calendar.ResolvedProjectionSource{Source: ref, Relevant: true}, nil
}

func (r *CalendarRepository) ListCalendarProjectionEvents(ctx context.Context, mspID, consumerKey string, limit int) ([]mutation.EventRecord, error) {
	if r == nil || r.db == nil || !internalid.ValidCanonical(mspID) || strings.TrimSpace(consumerKey) == "" || limit < 1 || limit > 500 {
		return nil, calendar.ErrInvalidProjectionBatch
	}
	rows, err := r.db.Query(ctx, `SELECT event.event_id::text,event.event_type,event.schema_version,event.occurred_at,event.msp_id::text,COALESCE(event.client_id::text,''),event.actor_type,event.actor_id::text,event.subject_type,event.subject_id::text,event.subject_version,event.correlation_id::text,COALESCE(event.causation_id::text,''),event.source,event.data
FROM event_outbox event
LEFT JOIN calendar_projection_cursors cursor ON cursor.msp_id=event.msp_id AND cursor.consumer_key=$2
WHERE event.msp_id=$1::uuid
  AND (event.subject_type=ANY($3::text[]) OR event.event_type='custom_date.set')
  AND (event.occurred_at,event.event_id)>(COALESCE(cursor.last_outbox_occurred_at,'1970-01-01'::timestamptz),COALESCE(cursor.last_event_id,'00000000-0000-0000-0000-000000000000'::uuid))
ORDER BY event.occurred_at,event.event_id
LIMIT $4`, mspID, consumerKey, []string{"work_record", "task", "project", "phase", "project_milestone", "resource_plan", "technician_schedule", "pto_request", "maintenance_window", "commercial_commitment", "work_record_sla"}, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []mutation.EventRecord{}
	for rows.Next() {
		var event mutation.EventRecord
		var data []byte
		if err = rows.Scan(&event.EventID, &event.EventType, &event.SchemaVersion, &event.OccurredAt, &event.MSPID, &event.ClientID, &event.ActorType, &event.ActorID, &event.SubjectType, &event.SubjectID, &event.SubjectVersion, &event.CorrelationID, &event.CausationID, &event.Source, &data); err != nil {
			return nil, err
		}
		if len(data) > 0 && json.Unmarshal(data, &event.Data) != nil {
			return nil, calendar.ErrInvalidProjectionBatch
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func (r *CalendarRepository) ScanProjectionSources(ctx context.Context, request calendar.ReconcileRequest) ([]calendar.ReconcileSource, error) {
	if r == nil || r.db == nil || !internalid.ValidCanonical(request.MSPID) || request.Limit < 1 {
		return nil, calendar.ErrInvalidProjectionBatch
	}
	types := request.SourceTypes
	if len(types) == 0 {
		types = []string{"work_record", "task", "project", "phase", "milestone", "resource_plan", "technician_schedule", "pto", "maintenance_window", "commercial_commitment", "custom_date"}
	}
	result := []calendar.ReconcileSource{}
	for _, typ := range types {
		sourceSQL, ok := calendarReconcileSourceSQL[typ]
		if !ok {
			return nil, calendar.ErrInvalidProjectionBatch
		}
		remaining := request.Limit - len(result)
		if remaining <= 0 {
			break
		}
		query := `WITH sources AS (` + sourceSQL + `),
projection_roles AS (
 SELECT msp_id,client_id,source_id,MAX(source_revision) revision,
        array_agg(event_role||':'||source_role_key ORDER BY event_role,source_role_key) roles
 FROM calendar_event_projections WHERE msp_id=$1::uuid AND source_type=$2
 GROUP BY msp_id,client_id,source_id
), applied_state AS (
 SELECT msp_id,client_id,source_id,MAX(source_revision) revision
 FROM (
  SELECT msp_id,client_id,source_id,source_revision FROM calendar_event_projections
   WHERE msp_id=$1::uuid AND source_type=$2
  UNION ALL
  SELECT msp_id,client_id,source_id,source_revision FROM calendar_live_changes
   WHERE msp_id=$1::uuid AND source_type=$2
 ) revisions GROUP BY msp_id,client_id,source_id
), projected AS (
 SELECT COALESCE(role.msp_id,state.msp_id) msp_id,
        COALESCE(role.client_id,state.client_id) client_id,
        COALESCE(role.source_id,state.source_id) source_id,
        GREATEST(COALESCE(role.revision,0),COALESCE(state.revision,0)) revision,
        COALESCE(role.roles,ARRAY[]::text[]) roles
 FROM projection_roles role FULL OUTER JOIN applied_state state
  ON state.msp_id=role.msp_id
 AND state.client_id IS NOT DISTINCT FROM role.client_id
 AND state.source_id=role.source_id
)
SELECT COALESCE(source.msp_id,projection.msp_id)::text,
       COALESCE(COALESCE(source.client_id,projection.client_id)::text,''),
       COALESCE(source.id,projection.source_id)::text,
       COALESCE(source.revision,0),COALESCE(projection.revision,0),
       COALESCE(projection.roles,ARRAY[]::text[]),source.id IS NOT NULL
FROM sources source FULL OUTER JOIN projected projection
 ON projection.msp_id=source.msp_id
AND projection.client_id IS NOT DISTINCT FROM source.client_id
AND projection.source_id=source.id
WHERE source.id IS NOT NULL OR cardinality(projection.roles)>0
ORDER BY COALESCE(source.id,projection.source_id) LIMIT $3`
		rows, err := r.db.Query(ctx, query, request.MSPID, typ, remaining)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var value calendar.ReconcileSource
			value.Source.Type = typ
			if err = rows.Scan(&value.Source.MSPID, &value.Source.ClientID, &value.Source.ID, &value.SourceRevision, &value.AppliedRevision, &value.ProjectionRoles, &value.SourceExists); err != nil {
				rows.Close()
				return nil, err
			}
			result = append(result, value)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return result, nil
}

var calendarReconcileSourceSQL = map[string]string{
	"work_record": `SELECT work.id,work.msp_id,work.client_id,work.version+COALESCE(classification.version,0)+COALESCE(sla.version,0) revision
FROM work_records work
LEFT JOIN classification_object_versions classification ON classification.msp_id=work.msp_id AND classification.client_id=work.client_id AND classification.object_type='work_record' AND classification.object_id=work.id
LEFT JOIN LATERAL (SELECT MAX(version) version FROM work_record_slas WHERE msp_id=work.msp_id AND client_id=work.client_id AND work_record_id=work.id) sla ON true
WHERE work.msp_id=$1::uuid AND work.deleted_at IS NULL`,
	"task": `SELECT task.id,task.msp_id,task.client_id,task.version+COALESCE(classification.version,0) revision FROM tasks task
LEFT JOIN classification_object_versions classification ON classification.msp_id=task.msp_id AND classification.client_id=task.client_id AND classification.object_type='task' AND classification.object_id=task.id
WHERE task.msp_id=$1::uuid`,
	"project": `SELECT project.id,project.msp_id,project.client_id,project.version+COALESCE(classification.version,0) revision FROM projects project
LEFT JOIN classification_object_versions classification ON classification.msp_id=project.msp_id AND classification.client_id=project.client_id AND classification.object_type='project' AND classification.object_id=project.id
WHERE project.msp_id=$1::uuid`,
	"phase":                 `SELECT id,msp_id,client_id,version revision FROM phases WHERE msp_id=$1::uuid`,
	"milestone":             `SELECT id,msp_id,client_id,version revision FROM project_milestones WHERE msp_id=$1::uuid`,
	"resource_plan":         `SELECT id,msp_id,client_id,version revision FROM resource_plans WHERE msp_id=$1::uuid`,
	"technician_schedule":   `SELECT id,msp_id,NULL::uuid client_id,version revision FROM technician_schedule_versions WHERE msp_id=$1::uuid AND lifecycle_state='active'`,
	"pto":                   `SELECT id,msp_id,NULL::uuid client_id,version revision FROM pto_requests WHERE msp_id=$1::uuid`,
	"maintenance_window":    `SELECT id,msp_id,NULL::uuid client_id,version revision FROM maintenance_windows WHERE msp_id=$1::uuid`,
	"commercial_commitment": `SELECT id,msp_id,client_id,version revision FROM commercial_commitments WHERE msp_id=$1::uuid`,
	"custom_date": `SELECT value.id,value.msp_id,value.client_id,value.source_revision+value.version+field.version+COALESCE(classification.version,0) revision
FROM object_custom_date_values value
JOIN calendar_custom_date_fields field ON field.id=value.field_id AND field.msp_id=value.msp_id
LEFT JOIN classification_object_versions classification ON classification.msp_id=value.msp_id AND classification.client_id IS NOT DISTINCT FROM value.client_id AND classification.object_type=value.object_type AND classification.object_id=value.object_id
WHERE value.msp_id=$1::uuid`,
}

var _ calendar.ProjectionSourceResolver = (*CalendarRepository)(nil)
var _ calendar.ReconciliationCatalog = (*CalendarRepository)(nil)

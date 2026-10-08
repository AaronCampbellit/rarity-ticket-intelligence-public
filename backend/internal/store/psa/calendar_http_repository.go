package psa

import (
	"context"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/commitments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/customfields"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
)

// These query methods are the typed-source HTTP read boundary. They return
// authoritative objects and re-check source/workforce authority rather than
// treating calendar projections as mutable source records.
func (r *CalendarRepository) ListConflictPolicies(ctx context.Context, principal authorization.Principal, policyScope calendar.ConflictPolicyScope) ([]calendar.ConflictPolicy, error) {
	principal.Scope.ClientID = ""
	if r == nil || r.db == nil || !internalid.ValidCanonical(principal.ID) || !internalid.ValidCanonical(principal.Scope.MSPID) || authorization.Authorize(principal, "calendar.policy.manage", scope.Target{MSPID: principal.Scope.MSPID}) != nil {
		return nil, authorization.ErrForbidden
	}
	rows, err := r.db.Query(ctx, `SELECT policy.id::text,policy.scope_type,COALESCE(policy.team_id::text,''),COALESCE(policy.technician_id::text,''),rule.kind,rule.severity,policy.version FROM calendar_conflict_policies policy CROSS JOIN LATERAL (VALUES ('approved_pto',policy.approved_pto_rule),('non_working_time',policy.non_working_time_rule),('protected_maintenance',policy.protected_maintenance_rule),('ordinary_overbooking',policy.ordinary_overbooking_rule)) AS rule(kind,severity) WHERE policy.msp_id=$1::uuid AND policy.lifecycle_state='active' AND policy.scope_type=$2 AND policy.team_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid AND policy.technician_id IS NOT DISTINCT FROM NULLIF($4,'')::uuid ORDER BY rule.kind,policy.id`, principal.Scope.MSPID, policyScope.Type, policyScope.TeamID, policyScope.TechnicianID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []calendar.ConflictPolicy{}
	for rows.Next() {
		var value calendar.ConflictPolicy
		value.MSPID = principal.Scope.MSPID
		if err = rows.Scan(&value.ID, &value.ScopeType, &value.TeamID, &value.TechnicianID, &value.Kind, &value.Severity, &value.Version); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (r *CalendarRepository) ListCustomDateFields(ctx context.Context, principal authorization.Principal) ([]calendar.CustomDateField, error) {
	principal.Scope.ClientID = ""
	if r == nil || r.db == nil || !internalid.ValidCanonical(principal.ID) || !internalid.ValidCanonical(principal.Scope.MSPID) || authorization.Authorize(principal, "calendar.policy.manage", scope.Target{MSPID: principal.Scope.MSPID}) != nil {
		return nil, authorization.ErrForbidden
	}
	return r.listCustomDateDefinitions(ctx, principal.Scope.MSPID, "")
}

func (r *CalendarRepository) listCustomDateDefinitions(ctx context.Context, mspID, objectType string) ([]calendar.CustomDateField, error) {
	rows, err := r.db.Query(ctx, `SELECT id::text,object_type,internal_key,label,category,CASE value_kind WHEN 'timestamp' THEN 'datetime' ELSE value_kind END,scheduling_mode,capacity_bearing,CASE WHEN timezone_source='fixed' THEN COALESCE(fixed_timezone,'') ELSE COALESCE(timezone_source,'') END,COALESCE(planned_effort_source,''),version FROM calendar_custom_date_fields WHERE msp_id=$1::uuid AND lifecycle_state='active' AND ($2='' OR object_type=$2) ORDER BY object_type,label,id`, mspID, objectType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []calendar.CustomDateField{}
	for rows.Next() {
		var value calendar.CustomDateField
		value.MSPID = mspID
		if err = rows.Scan(&value.ID, &value.ObjectType, &value.FieldID, &value.Label, &value.Category, &value.FieldType, &value.SchedulingMode, &value.CapacityBearing, &value.TimezoneSource, &value.PlannedEffortSource, &value.Version); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (r *CalendarRepository) ListCustomDateValues(ctx context.Context, principal authorization.Principal, objectType customfields.ObjectType, objectID string) ([]customfields.DateValue, error) {
	if r == nil || r.db == nil || !internalid.ValidCanonical(principal.ID) || !internalid.ValidCanonical(principal.Scope.MSPID) || !internalid.ValidCanonical(objectID) {
		return nil, customfields.ErrInvalidCustomDate
	}
	rows, err := r.db.Query(ctx, `SELECT id::text,field_id::text,msp_id::text,COALESCE(client_id::text,''),object_type,object_id::text,source_revision,COALESCE(timezone,''),version,date_value,timestamp_value,created_at,created_by::text,updated_at,updated_by::text FROM object_custom_date_values WHERE msp_id=$1::uuid AND object_type=$2 AND object_id=$3::uuid AND (NULLIF($4,'')::uuid IS NULL OR client_id=$4::uuid) ORDER BY field_id,id`, principal.Scope.MSPID, objectType, objectID, principal.Scope.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []customfields.DateValue{}
	for rows.Next() {
		var value customfields.DateValue
		if err = rows.Scan(&value.ID, &value.FieldID, &value.MSPID, &value.ClientID, &value.ObjectType, &value.ObjectID, &value.SourceRevision, &value.Timezone, &value.Version, &value.DateValue, &value.TimestampValue, &value.CreatedAt, &value.CreatedBy, &value.UpdatedAt, &value.UpdatedBy); err != nil {
			return nil, err
		}
		allowed, authErr := r.CanReadCalendarSource(ctx, principal, calendar.SourceRef{MSPID: value.MSPID, ClientID: value.ClientID, Type: "custom_date", ID: value.ID})
		if authErr != nil {
			return nil, authErr
		}
		if allowed {
			result = append(result, value)
		}
	}
	return result, rows.Err()
}

func (r *CalendarRepository) ListSchedules(ctx context.Context, principal authorization.Principal, technicianIDs []string, _ calendar.QueryWindow) ([]workforce.Schedule, error) {
	ids, err := r.authorizedWorkforceTechnicianIDs(ctx, principal, technicianIDs)
	if err != nil {
		return nil, err
	}
	result := []workforce.Schedule{}
	for _, technicianID := range ids {
		var scheduleID string
		err = r.db.QueryRow(ctx, `SELECT id::text FROM technician_schedule_versions WHERE msp_id=$1::uuid AND technician_id=$2::uuid AND lifecycle_state='active' ORDER BY version DESC LIMIT 1`, principal.Scope.MSPID, technicianID).Scan(&scheduleID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		value, loadErr := r.LoadSchedule(ctx, calendar.SourceRef{MSPID: principal.Scope.MSPID, Type: "technician_schedule", ID: scheduleID})
		if loadErr != nil {
			return nil, loadErr
		}
		result = append(result, value)
	}
	return result, nil
}
func (r *CalendarRepository) ListPTO(ctx context.Context, principal authorization.Principal, technicianIDs []string, window calendar.QueryWindow) ([]workforce.PTORequest, error) {
	ids, err := r.authorizedWorkforceTechnicianIDs(ctx, principal, technicianIDs)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []workforce.PTORequest{}, nil
	}
	rows, err := r.db.Query(ctx, `SELECT id::text,technician_id::text FROM pto_requests WHERE msp_id=$1::uuid AND technician_id::text=ANY($2::text[]) AND ($3::timestamptz IS NULL OR (all_day AND starts_on<$4::date AND COALESCE(ends_on,starts_on)>=$3::date) OR (NOT all_day AND starts_at<$4 AND ends_at>$3)) ORDER BY COALESCE(starts_at,starts_on::timestamp AT TIME ZONE 'UTC'),id`, principal.Scope.MSPID, ids, nullableWindowStart(window), nullableWindowEnd(window))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []workforce.PTORequest{}
	for rows.Next() {
		var id, technicianID string
		if err = rows.Scan(&id, &technicianID); err != nil {
			return nil, err
		}
		value, loadErr := r.LoadPTO(ctx, calendar.SourceRef{MSPID: principal.Scope.MSPID, Type: "pto", ID: id})
		if loadErr != nil {
			return nil, loadErr
		}
		result = append(result, value)
	}
	return result, rows.Err()
}
func (r *CalendarRepository) authorizedWorkforceTechnicianIDs(ctx context.Context, principal authorization.Principal, requested []string) ([]string, error) {
	if r == nil || r.db == nil || !internalid.ValidCanonical(principal.ID) || !internalid.ValidCanonical(principal.Scope.MSPID) {
		return nil, authorization.ErrForbidden
	}
	ids := append([]string(nil), requested...)
	if len(ids) == 0 {
		rows, err := r.db.Query(ctx, `SELECT id::text FROM technicians WHERE msp_id=$1::uuid AND lifecycle_state='active' ORDER BY id LIMIT 2000`, principal.Scope.MSPID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
	}
	result := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		if !internalid.ValidCanonical(id) || seen[id] {
			continue
		}
		allowed, err := r.CanScheduleCalendarTechnician(ctx, principal, id)
		if err != nil {
			return nil, err
		}
		if allowed {
			seen[id] = true
			result = append(result, id)
		}
	}
	sort.Strings(result)
	return result, nil
}

func (r *CalendarRepository) ListMilestones(ctx context.Context, principal authorization.Principal, projectID projects.ProjectID) ([]projects.Milestone, error) {
	if r == nil || r.db == nil || !internalid.ValidCanonical(principal.ID) || !internalid.ValidCanonical(principal.Scope.MSPID) || !internalid.ValidCanonical(string(projectID)) {
		return nil, projects.ErrInvalidMilestone
	}
	var clientID string
	if err := r.db.QueryRow(ctx, `SELECT client_id::text FROM projects WHERE id=$1::uuid AND msp_id=$2::uuid AND (NULLIF($3,'')::uuid IS NULL OR client_id=$3::uuid)`, projectID, principal.Scope.MSPID, principal.Scope.ClientID).Scan(&clientID); errors.Is(err, pgx.ErrNoRows) {
		return nil, scope.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	target := scope.Target{MSPID: principal.Scope.MSPID, ClientID: clientID}
	if err := authorization.Authorize(principal, "project.read", target); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT id::text FROM project_milestones WHERE msp_id=$1::uuid AND client_id=$2::uuid AND project_id=$3::uuid ORDER BY due_on,id`, target.MSPID, target.ClientID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	repository := NewProjectRepository(r.db)
	result := []projects.Milestone{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		value, loadErr := repository.FindMilestone(ctx, target, id)
		if loadErr != nil {
			return nil, loadErr
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (r *CalendarRepository) ListMaintenanceWindows(ctx context.Context, principal authorization.Principal, window calendar.QueryWindow) ([]commitments.MaintenanceWindow, error) {
	principal.Scope.ClientID = ""
	if r == nil || r.db == nil || !internalid.ValidCanonical(principal.ID) || !internalid.ValidCanonical(principal.Scope.MSPID) {
		return nil, authorization.ErrForbidden
	}
	if err := authorization.Authorize(principal, "calendar.commitment.manage", scope.Target{MSPID: principal.Scope.MSPID}); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT id::text FROM maintenance_windows WHERE msp_id=$1::uuid AND ($2::timestamptz IS NULL OR (all_day AND starts_on<$3::date AND COALESCE(ends_on,starts_on)>=$2::date) OR (NOT all_day AND starts_at<$3 AND ends_at>$2)) ORDER BY COALESCE(starts_at,starts_on::timestamp AT TIME ZONE 'UTC'),id`, principal.Scope.MSPID, nullableWindowStart(window), nullableWindowEnd(window))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []commitments.MaintenanceWindow{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		value, loadErr := r.LoadMaintenance(ctx, calendar.SourceRef{MSPID: principal.Scope.MSPID, Type: "maintenance_window", ID: id})
		if loadErr != nil {
			return nil, loadErr
		}
		result = append(result, value)
	}
	return result, rows.Err()
}
func (r *CalendarRepository) ListCommercialCommitments(ctx context.Context, principal authorization.Principal, requested []string) ([]commitments.CommercialCommitment, error) {
	if r == nil || r.db == nil || !internalid.ValidCanonical(principal.ID) || !internalid.ValidCanonical(principal.Scope.MSPID) {
		return nil, authorization.ErrForbidden
	}
	clients, err := r.AuthorizedCalendarClientIDs(ctx, principal)
	if err != nil {
		return nil, err
	}
	if requested != nil {
		clients = intersectCalendarClients(clients, requested)
	}
	allowed := []string{}
	for _, clientID := range clients {
		if authorization.Authorize(principal, "calendar.commitment.manage", scope.Target{MSPID: principal.Scope.MSPID, ClientID: clientID}) == nil {
			allowed = append(allowed, clientID)
		}
	}
	if len(allowed) == 0 {
		return []commitments.CommercialCommitment{}, nil
	}
	rows, err := r.db.Query(ctx, `SELECT id::text,client_id::text FROM commercial_commitments WHERE msp_id=$1::uuid AND client_id::text=ANY($2::text[]) ORDER BY expiration_on,id`, principal.Scope.MSPID, allowed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []commitments.CommercialCommitment{}
	for rows.Next() {
		var id, clientID string
		if err = rows.Scan(&id, &clientID); err != nil {
			return nil, err
		}
		value, loadErr := r.LoadCommercial(ctx, calendar.SourceRef{MSPID: principal.Scope.MSPID, ClientID: clientID, Type: "commercial_commitment", ID: id})
		if loadErr != nil {
			return nil, loadErr
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func nullableWindowStart(window calendar.QueryWindow) any {
	if window.Start.IsZero() {
		return nil
	}
	return window.Start.UTC()
}
func nullableWindowEnd(window calendar.QueryWindow) any {
	if window.End.IsZero() {
		return nil
	}
	return window.End.UTC()
}
func intersectCalendarClients(allowed, requested []string) []string {
	set := map[string]bool{}
	for _, id := range allowed {
		set[id] = true
	}
	result := []string{}
	seen := map[string]bool{}
	for _, id := range requested {
		if set[id] && !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	sort.Strings(result)
	return result
}

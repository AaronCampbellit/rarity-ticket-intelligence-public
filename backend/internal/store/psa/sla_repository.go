package psa

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
)

type SLARepository struct {
	db database
}

var _ sla.CalendarManagementRepository = (*SLARepository)(nil)
var _ sla.PolicyManagementRepository = (*SLARepository)(nil)
var _ sla.EvaluatorRepository = (*SLARepository)(nil)

func NewSLARepository(db database) *SLARepository {
	return &SLARepository{db: db}
}

func (r *SLARepository) FindCalendar(
	ctx context.Context,
	target scope.Target,
	calendarID string,
) (sla.PublishedCalendar, error) {
	var (
		result           sla.PublishedCalendar
		weekly, holidays []byte
	)
	err := r.db.QueryRow(ctx, `
SELECT c.id::text, c.msp_id::text, COALESCE(c.client_id::text, ''),
       c.key, c.name, c.version, v.timezone, v.weekly_schedule, v.holidays,
       v.published_at, COALESCE(v.published_by::text, '')
FROM business_calendars c
JOIN business_calendar_versions v
  ON v.calendar_id = c.id AND v.version = c.version
WHERE c.id = $1 AND c.msp_id = $2
  AND (
    ($3 = '' AND c.client_id IS NULL)
    OR ($3 <> '' AND c.client_id = $3::uuid)
  )
`, calendarID, target.MSPID, target.ClientID).Scan(
		&result.ID, &result.MSPID, &result.ClientID,
		&result.Key, &result.Name, &result.Version,
		&result.Definition.Timezone, &weekly, &holidays,
		&result.PublishedAt, &result.PublishedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return sla.PublishedCalendar{}, scope.ErrNotFound
	}
	if err != nil {
		return sla.PublishedCalendar{}, err
	}
	if err := json.Unmarshal(weekly, &result.Definition.Weekly); err != nil {
		return sla.PublishedCalendar{}, err
	}
	if err := json.Unmarshal(holidays, &result.Definition.Holidays); err != nil {
		return sla.PublishedCalendar{}, err
	}
	return result, nil
}

func (r *SLARepository) ListCalendars(
	ctx context.Context,
	target scope.Target,
) ([]sla.PublishedCalendar, error) {
	rows, err := r.db.Query(ctx, `
SELECT c.id::text, c.msp_id::text, COALESCE(c.client_id::text, ''),
       c.key, c.name, c.version, v.timezone, v.weekly_schedule, v.holidays,
       v.published_at, COALESCE(v.published_by::text, '')
FROM business_calendars c
JOIN business_calendar_versions v
  ON v.calendar_id = c.id AND v.version = c.version
WHERE c.msp_id = $1
  AND (
    ($2 = '' AND c.client_id IS NULL)
    OR ($2 <> '' AND (c.client_id IS NULL OR c.client_id = $2::uuid))
  )
ORDER BY c.name, c.id
`, target.MSPID, target.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	calendars := make([]sla.PublishedCalendar, 0)
	for rows.Next() {
		var calendar sla.PublishedCalendar
		var weekly, holidays []byte
		if err := rows.Scan(
			&calendar.ID, &calendar.MSPID, &calendar.ClientID,
			&calendar.Key, &calendar.Name, &calendar.Version,
			&calendar.Definition.Timezone, &weekly, &holidays,
			&calendar.PublishedAt, &calendar.PublishedBy,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(weekly, &calendar.Definition.Weekly); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(holidays, &calendar.Definition.Holidays); err != nil {
			return nil, err
		}
		calendars = append(calendars, calendar)
	}
	return calendars, rows.Err()
}

func (r *SLARepository) PublishCalendarAtomic(
	ctx context.Context,
	accepted sla.CalendarPublishMutation,
) error {
	weekly, err := json.Marshal(accepted.Calendar.Definition.Weekly)
	if err != nil {
		return err
	}
	holidays, err := json.Marshal(accepted.Calendar.Definition.Holidays)
	if err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	calendar := accepted.Calendar
	if accepted.Created {
		if _, err := tx.Exec(ctx, `
INSERT INTO business_calendars (
  id, msp_id, client_id, key, name, timezone,
  weekly_schedule, holidays, version
) VALUES (
  $1, $2, NULLIF($3, '')::uuid, $4, $5, $6, $7, $8, $9
)
`, calendar.ID, calendar.MSPID, calendar.ClientID, calendar.Key, calendar.Name,
			calendar.Definition.Timezone, weekly, holidays, calendar.Version); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
	} else {
		tag, err := tx.Exec(ctx, `
UPDATE business_calendars
SET key = $4, name = $5, timezone = $6,
    weekly_schedule = $7, holidays = $8, version = version + 1
WHERE id = $1 AND msp_id = $2
  AND (
    ($3 = '' AND client_id IS NULL)
    OR ($3 <> '' AND client_id = $3::uuid)
  )
  AND version = $9
`, calendar.ID, calendar.MSPID, calendar.ClientID, calendar.Key, calendar.Name,
			calendar.Definition.Timezone, weekly, holidays, accepted.ExpectedVersion)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if tag.RowsAffected() != 1 {
			_ = tx.Rollback(ctx)
			return object.ErrVersionConflict
		}
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO business_calendar_versions (
  calendar_id, msp_id, version, timezone, weekly_schedule, holidays,
  published_at, published_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
`, calendar.ID, calendar.MSPID, calendar.Version,
		calendar.Definition.Timezone, weekly, holidays,
		calendar.PublishedAt, calendar.PublishedBy); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *SLARepository) ResolveCalendar(
	ctx context.Context,
	target scope.Target,
	calendarID string,
) (sla.PublishedCalendar, error) {
	calendar, err := r.FindCalendar(ctx, target, calendarID)
	if err == nil || target.ClientID == "" || !errors.Is(err, scope.ErrNotFound) {
		return calendar, err
	}
	return r.FindCalendar(ctx, scope.Target{MSPID: target.MSPID}, calendarID)
}

func (r *SLARepository) ListPolicies(
	ctx context.Context,
	target scope.Target,
) ([]sla.PublishedPolicy, error) {
	rows, err := r.db.Query(ctx, `
SELECT p.id::text, p.msp_id::text, COALESCE(p.client_id::text, ''),
       p.key, p.name, p.version,
       pv.conditions, pv.response_target_seconds, pv.resolution_target_seconds,
       pv.warning_percent, pv.pause_states, pv.enabled, pv.priority,
       pv.stable_order, pv.fallback, pv.published_at,
       COALESCE(pv.published_by::text, ''),
       c.id::text, c.msp_id::text, COALESCE(c.client_id::text, ''),
       c.key, c.name, cv.version, cv.timezone, cv.weekly_schedule, cv.holidays,
       cv.published_at, COALESCE(cv.published_by::text, '')
FROM sla_policies p
JOIN sla_policy_versions pv
  ON pv.policy_id = p.id AND pv.version = p.version
JOIN business_calendars c
  ON c.id = pv.calendar_id AND c.msp_id = p.msp_id
JOIN business_calendar_versions cv
  ON cv.calendar_id = pv.calendar_id AND cv.version = pv.calendar_version
WHERE p.msp_id = $1
  AND (
    ($2 = '' AND p.client_id IS NULL)
    OR ($2 <> '' AND (p.client_id IS NULL OR p.client_id = $2::uuid))
  )
ORDER BY p.priority DESC, p.stable_order, p.id
`, target.MSPID, target.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]sla.PublishedPolicy, 0)
	for rows.Next() {
		var (
			policy                   sla.PublishedPolicy
			conditions, pauseStates  []byte
			weeklySchedule, holidays []byte
		)
		if err := rows.Scan(
			&policy.ID, &policy.MSPID, &policy.ClientID,
			&policy.Key, &policy.Name, &policy.Version,
			&conditions, &policy.ResponseTargetSeconds,
			&policy.ResolutionTargetSeconds, &policy.WarningPercent,
			&pauseStates, &policy.Enabled, &policy.Priority,
			&policy.StableOrder, &policy.Fallback,
			&policy.PublishedAt, &policy.PublishedBy,
			&policy.Calendar.ID, &policy.Calendar.MSPID, &policy.Calendar.ClientID,
			&policy.Calendar.Key, &policy.Calendar.Name, &policy.Calendar.Version,
			&policy.Calendar.Definition.Timezone, &weeklySchedule, &holidays,
			&policy.Calendar.PublishedAt, &policy.Calendar.PublishedBy,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(conditions, &policy.Conditions); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(pauseStates, &policy.PauseStates); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(
			weeklySchedule, &policy.Calendar.Definition.Weekly,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(holidays, &policy.Calendar.Definition.Holidays); err != nil {
			return nil, err
		}
		result = append(result, policy)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *SLARepository) PublishPolicyAtomic(
	ctx context.Context,
	accepted sla.PolicyPublishMutation,
) error {
	conditions, err := json.Marshal(accepted.Policy.Conditions)
	if err != nil {
		return err
	}
	pauseStates, err := json.Marshal(accepted.Policy.PauseStates)
	if err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	policy := accepted.Policy
	if accepted.Created {
		if _, err := tx.Exec(ctx, `
INSERT INTO sla_policies (
  id, msp_id, client_id, key, name, calendar_id, conditions,
  response_target_seconds, resolution_target_seconds, warning_percent,
  pause_states, enabled, priority, stable_order, fallback, version
) VALUES (
  $1, $2, NULLIF($3, '')::uuid, $4, $5, $6, $7,
  $8, $9, $10, $11, $12, $13, $14, $15, $16
)
`, policy.ID, policy.MSPID, policy.ClientID, policy.Key, policy.Name,
			policy.Calendar.ID, conditions, policy.ResponseTargetSeconds,
			policy.ResolutionTargetSeconds, policy.WarningPercent, pauseStates,
			policy.Enabled, policy.Priority, policy.StableOrder, policy.Fallback,
			policy.Version); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
	} else {
		tag, err := tx.Exec(ctx, `
UPDATE sla_policies
SET key = $4, name = $5, calendar_id = $6, conditions = $7,
    response_target_seconds = $8, resolution_target_seconds = $9,
    warning_percent = $10, pause_states = $11, enabled = $12,
    priority = $13, stable_order = $14, fallback = $15,
    version = version + 1
WHERE id = $1 AND msp_id = $2
  AND (
    ($3 = '' AND client_id IS NULL)
    OR ($3 <> '' AND client_id = $3::uuid)
  )
  AND version = $16
`, policy.ID, policy.MSPID, policy.ClientID, policy.Key, policy.Name,
			policy.Calendar.ID, conditions, policy.ResponseTargetSeconds,
			policy.ResolutionTargetSeconds, policy.WarningPercent, pauseStates,
			policy.Enabled, policy.Priority, policy.StableOrder, policy.Fallback,
			accepted.ExpectedVersion)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if tag.RowsAffected() != 1 {
			_ = tx.Rollback(ctx)
			return object.ErrVersionConflict
		}
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO sla_policy_versions (
  policy_id, msp_id, version, calendar_id, calendar_version, conditions,
  response_target_seconds, resolution_target_seconds, warning_percent,
  pause_states, enabled, priority, stable_order, fallback,
  published_at, published_by
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8,
  $9, $10, $11, $12, $13, $14, $15, $16
)
`, policy.ID, policy.MSPID, policy.Version,
		policy.Calendar.ID, policy.Calendar.Version, conditions,
		policy.ResponseTargetSeconds, policy.ResolutionTargetSeconds,
		policy.WarningPercent, pauseStates, policy.Enabled, policy.Priority,
		policy.StableOrder, policy.Fallback, policy.PublishedAt,
		policy.PublishedBy); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *SLARepository) ListDue(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]sla.Timer, error) {
	rows, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, client_id::text, work_record_id::text,
       response_warning_at, response_due_at,
       resolution_warning_at, resolution_due_at,
       responded_at, resolved_at, paused_at,
       response_state, resolution_state, version
FROM work_record_slas
WHERE paused_at IS NULL
  AND (
    (
      response_state IN ('running', 'warning')
      AND response_warning_at <= $1
    )
    OR (
      resolution_state IN ('running', 'warning')
      AND resolution_warning_at <= $1
    )
  )
ORDER BY LEAST(response_warning_at, resolution_warning_at), id
LIMIT $2
`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]sla.Timer, 0)
	for rows.Next() {
		var timer sla.Timer
		if err := rows.Scan(
			&timer.ID, &timer.MSPID, &timer.ClientID, &timer.WorkRecordID,
			&timer.ResponseWarningAt, &timer.ResponseDueAt,
			&timer.ResolutionWarningAt, &timer.ResolutionDueAt,
			&timer.RespondedAt, &timer.ResolvedAt, &timer.PausedAt,
			&timer.ResponseState, &timer.ResolutionState, &timer.Version,
		); err != nil {
			return nil, err
		}
		result = append(result, timer)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *SLARepository) UpdateEvaluation(
	ctx context.Context,
	accepted sla.EvaluationMutation,
) error {
	if len(accepted.Audits) == 0 || len(accepted.Audits) != len(accepted.Events) {
		return sla.ErrInvalidPolicy
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	timer := accepted.Timer
	tag, err := tx.Exec(ctx, `
UPDATE work_record_slas
SET response_state = $5, resolution_state = $6,
    state = $6, version = version + 1
WHERE id = $1 AND msp_id = $2 AND client_id = $3 AND version = $4
  AND paused_at IS NULL
`, timer.ID, timer.MSPID, timer.ClientID, timer.Version-1,
		timer.ResponseState, timer.ResolutionState)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return object.ErrVersionConflict
	}
	for index := range accepted.Audits {
		if err := writeMutationFacts(
			ctx, tx, accepted.Audits[index], accepted.Events[index],
		); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

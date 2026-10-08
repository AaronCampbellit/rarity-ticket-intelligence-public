package psa

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/timeentries"
)

type TimeWorkforceRepository struct {
	db database
}

var _ timeentries.LaborRoleRepository = (*TimeWorkforceRepository)(nil)
var _ timeentries.TimerRepository = (*TimeWorkforceRepository)(nil)
var _ timeentries.CaptureRepository = (*TimeWorkforceRepository)(nil)
var _ timeentries.TimesheetRepository = (*TimeWorkforceRepository)(nil)

func NewTimeWorkforceRepository(db database) *TimeWorkforceRepository {
	return &TimeWorkforceRepository{db: db}
}

func (r *TimeWorkforceRepository) LoadMSPTimezone(
	ctx context.Context,
	mspID string,
) (string, error) {
	var timezone string
	err := r.db.QueryRow(ctx, `
SELECT COALESCE((
  SELECT version.timezone
  FROM business_calendars calendar
  JOIN business_calendar_versions version
    ON version.calendar_id = calendar.id
   AND version.msp_id = calendar.msp_id
   AND version.version = calendar.version
  WHERE calendar.msp_id = $1 AND calendar.client_id IS NULL
  ORDER BY calendar.key, calendar.id
  LIMIT 1
), 'UTC')
`, mspID).Scan(&timezone)
	if err != nil {
		return "", err
	}
	return timezone, nil
}

func (r *TimeWorkforceRepository) ListTimesheetRows(
	ctx context.Context,
	target scope.Target,
	technicianID string,
	from time.Time,
	through time.Time,
) ([]timeentries.TimesheetRow, error) {
	foundRows, err := r.db.Query(
		ctx,
		timesheetRowSelect+`
WHERE entry.msp_id = $1 AND entry.client_id = $2
  AND entry.technician_id = $3
  AND entry.started_at >= $4 AND entry.started_at < $5
ORDER BY entry.started_at, entry.id
`,
		target.MSPID,
		target.ClientID,
		technicianID,
		from,
		through,
	)
	if err != nil {
		return nil, err
	}
	defer foundRows.Close()
	found := make([]timeentries.TimesheetRow, 0)
	for foundRows.Next() {
		var candidate timeentries.TimesheetRow
		if err := scanTimesheetRow(foundRows, &candidate); err != nil {
			return nil, err
		}
		found = append(found, candidate)
	}
	return found, foundRows.Err()
}

func (r *TimeWorkforceRepository) LoadTimesheetRow(
	ctx context.Context,
	target scope.Target,
	id string,
) (timeentries.TimesheetRow, error) {
	var found timeentries.TimesheetRow
	err := scanTimesheetRow(
		r.db.QueryRow(
			ctx,
			timesheetRowSelect+`
WHERE entry.id = $1 AND entry.msp_id = $2 AND entry.client_id = $3
`,
			id,
			target.MSPID,
			target.ClientID,
		),
		&found,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return timeentries.TimesheetRow{}, scope.ErrNotFound
	}
	if err != nil {
		return timeentries.TimesheetRow{}, err
	}
	return found, nil
}

func (r *TimeWorkforceRepository) AmendTimeEntryAtomic(
	ctx context.Context,
	accepted timeentries.AmendmentMutation,
) (timeentries.TimesheetRow, error) {
	err := r.withTransaction(ctx, func(tx transaction) error {
		entry := accepted.Result.Entry
		tag, err := tx.Exec(ctx, `
UPDATE time_entries
SET started_at = $4, ended_at = $5, duration_seconds = $6,
    billable = $7, note = $8, version = $9
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND version = $10 AND approval_state = 'pending' AND reversed_at IS NULL
`, entry.ID, entry.MSPID, entry.ClientID, entry.StartedAt,
			entry.EndedAt, entry.DurationSeconds, entry.Billable,
			entry.Note, entry.Version, accepted.ExpectedVersion,
		)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		beforeValues, err := json.Marshal(accepted.Amendment.BeforeValues)
		if err != nil {
			return err
		}
		afterValues, err := json.Marshal(accepted.Amendment.AfterValues)
		if err != nil {
			return err
		}
		evidence := accepted.Amendment
		if _, err := tx.Exec(ctx, `
INSERT INTO time_entry_amendments (
  id, time_entry_id, msp_id, client_id, prior_version,
  resulting_version, before_values, after_values, reason,
  amended_at, amended_by
) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9, $10, $11)
`, evidence.ID, evidence.TimeEntryID, evidence.MSPID, evidence.ClientID,
			evidence.PriorVersion, evidence.ResultingVersion,
			beforeValues, afterValues, evidence.Reason,
			evidence.AmendedAt, evidence.AmendedBy,
		); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
	if err != nil {
		return timeentries.TimesheetRow{}, err
	}
	return accepted.Result, nil
}

func (r *TimeWorkforceRepository) ReverseTimeEntryAtomic(
	ctx context.Context,
	accepted timeentries.ReversalMutation,
) (timeentries.ReversalResult, error) {
	err := r.withTransaction(ctx, func(tx transaction) error {
		replacement := accepted.Replacement.Entry
		if _, err := tx.Exec(ctx, `
INSERT INTO time_entries (
  id, msp_id, client_id, work_record_id, task_id, technician_id,
  started_at, ended_at, duration_seconds, billable, note,
  labor_role_version_id, internal_cost_minor, bill_rate_minor, rate_currency,
  version, created_at, created_by, approval_state
) VALUES (
  $1, $2, $3, $4, NULLIF($5, '')::uuid, $6,
  $7, $8, $9, $10, $11,
  NULLIF($12, '')::uuid, $13, $14, NULLIF($15, ''),
  $16, $17, $18, 'pending'
)
`, replacement.ID, replacement.MSPID, replacement.ClientID,
			replacement.WorkRecordID, replacement.TaskID,
			replacement.TechnicianID, replacement.StartedAt,
			replacement.EndedAt, replacement.DurationSeconds,
			replacement.Billable, replacement.Note,
			replacement.LaborRoleVersionID, nullableRate(replacement.LaborRoleVersionID, replacement.InternalCostMinor),
			nullableRate(replacement.LaborRoleVersionID, replacement.BillRateMinor),
			replacement.RateCurrency, replacement.Version,
			replacement.CreatedAt, replacement.CreatedBy,
		); err != nil {
			return err
		}
		if err := insertInitialTagAssignments(ctx, tx, tagging.TargetRef{
			MSPID: replacement.MSPID, ClientID: replacement.ClientID, ObjectType: tagging.ObjectTimeEntry, ObjectID: replacement.ID,
		}, replacement.Version, accepted.InitialTags); err != nil {
			return err
		}
		original := accepted.Original
		tag, err := tx.Exec(ctx, `
UPDATE time_entries
SET reversed_at = $4, reversed_by = $5, reversal_reason = $6,
    replacement_time_entry_id = $7, version = $8
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND version = $9 AND approval_state = 'approved' AND reversed_at IS NULL
`, original.Entry.ID, original.Entry.MSPID, original.Entry.ClientID,
			original.ReversedAt, original.ReversedBy,
			original.ReversalReason, original.ReplacementTimeEntryID,
			original.Entry.Version, accepted.ExpectedVersion,
		)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		if err := writeMutationFacts(
			ctx,
			tx,
			accepted.OriginalAudit,
			accepted.OriginalEvent,
		); err != nil {
			return err
		}
		return writeMutationFacts(
			ctx,
			tx,
			accepted.ReplacementAudit,
			accepted.ReplacementEvent,
		)
	})
	if err != nil {
		return timeentries.ReversalResult{}, err
	}
	return timeentries.ReversalResult{
		Original: accepted.Original, Replacement: accepted.Replacement,
	}, nil
}

const timesheetRowSelect = `
SELECT
  entry.id::text, entry.msp_id::text, entry.client_id::text,
  entry.work_record_id::text, COALESCE(entry.task_id::text, ''),
  entry.technician_id::text, entry.started_at, entry.ended_at,
  entry.duration_seconds, entry.billable, entry.note,
  COALESCE(entry.labor_role_version_id::text, ''),
  COALESCE(entry.internal_cost_minor, 0),
  COALESCE(entry.bill_rate_minor, 0),
  COALESCE(entry.rate_currency, ''), entry.version,
  entry.created_at, entry.created_by::text,
  client.name, COALESCE(task.title, work.title, 'Time entry'),
  COALESCE(role.name, ''), entry.approval_state,
  entry.approved_at, COALESCE(entry.approved_by::text, ''),
  entry.reversed_at, COALESCE(entry.reversed_by::text, ''),
  COALESCE(entry.reversal_reason, ''),
  COALESCE(entry.replacement_time_entry_id::text, ''),
  COALESCE(amendment.id::text, ''), COALESCE(amendment.prior_version, 0),
  COALESCE(amendment.resulting_version, 0),
  COALESCE(amendment.before_values, '{}'::jsonb),
  COALESCE(amendment.after_values, '{}'::jsonb),
  COALESCE(amendment.reason, ''), amendment.amended_at,
  COALESCE(amendment.amended_by::text, '')
FROM time_entries entry
JOIN client_organizations client
  ON client.id = entry.client_id AND client.msp_id = entry.msp_id
LEFT JOIN work_records work
  ON work.id = entry.work_record_id
 AND work.msp_id = entry.msp_id AND work.client_id = entry.client_id
LEFT JOIN tasks task
  ON task.id = entry.task_id
 AND task.msp_id = entry.msp_id AND task.client_id = entry.client_id
LEFT JOIN labor_role_versions role
  ON role.id = entry.labor_role_version_id AND role.msp_id = entry.msp_id
LEFT JOIN LATERAL (
  SELECT candidate.*
  FROM time_entry_amendments candidate
  WHERE candidate.time_entry_id = entry.id
    AND candidate.msp_id = entry.msp_id
    AND candidate.client_id = entry.client_id
  ORDER BY candidate.resulting_version DESC
  LIMIT 1
) amendment ON true
`

func scanTimesheetRow(
	found row,
	result *timeentries.TimesheetRow,
) error {
	entry := &result.Entry
	var (
		amendmentID               string
		amendmentPriorVersion     int64
		amendmentResultingVersion int64
		amendmentBefore           []byte
		amendmentAfter            []byte
		amendmentReason           string
		amendmentAt               *time.Time
		amendmentBy               string
	)
	if err := found.Scan(
		&entry.ID,
		&entry.MSPID,
		&entry.ClientID,
		&entry.WorkRecordID,
		&entry.TaskID,
		&entry.TechnicianID,
		&entry.StartedAt,
		&entry.EndedAt,
		&entry.DurationSeconds,
		&entry.Billable,
		&entry.Note,
		&entry.LaborRoleVersionID,
		&entry.InternalCostMinor,
		&entry.BillRateMinor,
		&entry.RateCurrency,
		&entry.Version,
		&entry.CreatedAt,
		&entry.CreatedBy,
		&result.ClientName,
		&result.WorkItemTitle,
		&result.LaborRoleName,
		&result.ApprovalState,
		&result.ApprovedAt,
		&result.ApprovedBy,
		&result.ReversedAt,
		&result.ReversedBy,
		&result.ReversalReason,
		&result.ReplacementTimeEntryID,
		&amendmentID,
		&amendmentPriorVersion,
		&amendmentResultingVersion,
		&amendmentBefore,
		&amendmentAfter,
		&amendmentReason,
		&amendmentAt,
		&amendmentBy,
	); err != nil {
		return err
	}
	if amendmentID == "" {
		return nil
	}
	var beforeValues, afterValues map[string]any
	if err := json.Unmarshal(amendmentBefore, &beforeValues); err != nil {
		return err
	}
	if err := json.Unmarshal(amendmentAfter, &afterValues); err != nil {
		return err
	}
	result.LastAmendment = &timeentries.AmendmentEvidence{
		ID: amendmentID, TimeEntryID: entry.ID,
		MSPID: entry.MSPID, ClientID: entry.ClientID,
		PriorVersion:     amendmentPriorVersion,
		ResultingVersion: amendmentResultingVersion,
		BeforeValues:     beforeValues, AfterValues: afterValues,
		Reason: amendmentReason, AmendedBy: amendmentBy,
	}
	if amendmentAt != nil {
		result.LastAmendment.AmendedAt = *amendmentAt
	}
	return nil
}

func nullableRate(roleVersionID string, value int64) any {
	if roleVersionID == "" {
		return nil
	}
	return value
}

func (r *TimeWorkforceRepository) CreateLaborRoleAtomic(
	ctx context.Context,
	accepted timeentries.LaborRoleMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		role := accepted.Role
		version := accepted.Version
		if _, err := tx.Exec(ctx, `
INSERT INTO labor_roles (
  id, msp_id, key, version, created_at, created_by
) VALUES ($1, $2, $3, $4, $5, $6)
`, role.ID, role.MSPID, role.Key, role.Version,
			version.CreatedAt, version.CreatedBy,
		); err != nil {
			return err
		}
		if err := insertLaborRoleVersion(ctx, tx, version); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *TimeWorkforceRepository) ListLaborRoles(
	ctx context.Context,
	mspID string,
	at time.Time,
) ([]timeentries.LaborRole, error) {
	rows, err := r.db.Query(ctx, `
SELECT role.id::text, role.msp_id::text, role.key, role.version,
  version.id::text, version.name, version.internal_cost_minor,
  version.bill_rate_minor, version.currency, version.effective_from,
  version.effective_until, version.enabled, version.created_at,
  version.created_by::text
FROM labor_roles role
JOIN LATERAL (
  SELECT candidate.*
  FROM labor_role_versions candidate
  WHERE candidate.labor_role_id = role.id
    AND candidate.msp_id = role.msp_id
    AND candidate.effective_from <= $2
    AND (candidate.effective_until IS NULL OR candidate.effective_until > $2)
  ORDER BY candidate.effective_from DESC, candidate.id DESC
  LIMIT 1
) version ON true
WHERE role.msp_id = $1 AND version.enabled
ORDER BY version.name, role.id
`, mspID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := make([]timeentries.LaborRole, 0)
	for rows.Next() {
		var role timeentries.LaborRole
		version := &role.CurrentVersion
		if err := rows.Scan(
			&role.ID, &role.MSPID, &role.Key, &role.Version,
			&version.ID, &version.Name, &version.InternalCostMinor,
			&version.BillRateMinor, &version.Currency,
			&version.EffectiveFrom, &version.EffectiveUntil,
			&version.Enabled, &version.CreatedAt, &version.CreatedBy,
		); err != nil {
			return nil, err
		}
		version.LaborRoleID, version.MSPID = role.ID, role.MSPID
		found = append(found, role)
	}
	return found, rows.Err()
}

func (r *TimeWorkforceRepository) ListLaborRolesForManagement(
	ctx context.Context,
	mspID string,
) ([]timeentries.LaborRole, error) {
	rows, err := r.db.Query(ctx, `
SELECT role.id::text, role.msp_id::text, role.key, role.version,
  version.id::text, version.name, version.internal_cost_minor,
  version.bill_rate_minor, version.currency, version.effective_from,
  version.effective_until, version.enabled, version.created_at,
  version.created_by::text
FROM labor_roles role
JOIN LATERAL (
  SELECT candidate.*
  FROM labor_role_versions candidate
  WHERE candidate.labor_role_id = role.id
    AND candidate.msp_id = role.msp_id
  ORDER BY candidate.effective_from DESC, candidate.id DESC
  LIMIT 1
) version ON true
WHERE role.msp_id = $1
ORDER BY version.name, role.id
`, mspID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := make([]timeentries.LaborRole, 0)
	for rows.Next() {
		var role timeentries.LaborRole
		version := &role.CurrentVersion
		if err := rows.Scan(
			&role.ID, &role.MSPID, &role.Key, &role.Version,
			&version.ID, &version.Name, &version.InternalCostMinor,
			&version.BillRateMinor, &version.Currency,
			&version.EffectiveFrom, &version.EffectiveUntil,
			&version.Enabled, &version.CreatedAt, &version.CreatedBy,
		); err != nil {
			return nil, err
		}
		version.LaborRoleID, version.MSPID = role.ID, role.MSPID
		found = append(found, role)
	}
	return found, rows.Err()
}

func (r *TimeWorkforceRepository) VersionLaborRoleAtomic(
	ctx context.Context,
	accepted timeentries.LaborRoleVersionMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		role := accepted.Role
		tag, err := tx.Exec(ctx, `
UPDATE labor_roles
SET version = $3
WHERE id = $1 AND msp_id = $2 AND version = $3 - 1
`, role.ID, role.MSPID, role.Version)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		if err := insertLaborRoleVersion(ctx, tx, accepted.Version); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *TimeWorkforceRepository) ResolveLaborRate(
	ctx context.Context,
	mspID string,
	laborRoleID string,
	technicianID string,
	at time.Time,
) (timeentries.RateSnapshot, error) {
	var found timeentries.RateSnapshot
	err := r.db.QueryRow(ctx, `
SELECT
  role_version.id::text,
  COALESCE(technician_rate.hourly_rate_minor, role_version.internal_cost_minor),
  role_version.bill_rate_minor,
  role_version.currency
FROM labor_roles role
JOIN LATERAL (
  SELECT candidate.*
  FROM labor_role_versions candidate
  WHERE candidate.labor_role_id = role.id
    AND candidate.msp_id = role.msp_id
    AND candidate.effective_from <= $4
    AND (candidate.effective_until IS NULL OR candidate.effective_until > $4)
    AND candidate.enabled
  ORDER BY candidate.effective_from DESC, candidate.id DESC
  LIMIT 1
) role_version ON true
LEFT JOIN LATERAL (
  SELECT candidate.hourly_rate_minor
  FROM technician_labor_cost_rates candidate
  WHERE candidate.msp_id = role.msp_id
    AND candidate.technician_id = $3
    AND candidate.currency = role_version.currency
    AND candidate.effective_at <= $4
  ORDER BY candidate.effective_at DESC, candidate.id DESC
  LIMIT 1
) technician_rate ON true
WHERE role.msp_id = $1 AND role.id = $2
`, mspID, laborRoleID, technicianID, at).Scan(
		&found.LaborRoleVersionID,
		&found.InternalCostMinor,
		&found.BillRateMinor,
		&found.Currency,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return timeentries.RateSnapshot{}, scope.ErrNotFound
	}
	if err != nil {
		return timeentries.RateSnapshot{}, err
	}
	return found, nil
}

func insertLaborRoleVersion(
	ctx context.Context,
	tx transaction,
	version timeentries.LaborRoleVersion,
) error {
	_, err := tx.Exec(ctx, `
INSERT INTO labor_role_versions (
  id, labor_role_id, msp_id, name, internal_cost_minor, bill_rate_minor,
  currency, effective_from, effective_until, enabled, created_at, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
`, version.ID, version.LaborRoleID, version.MSPID, version.Name,
		version.InternalCostMinor, version.BillRateMinor, version.Currency,
		version.EffectiveFrom, version.EffectiveUntil, version.Enabled,
		version.CreatedAt, version.CreatedBy,
	)
	return err
}

func (r *TimeWorkforceRepository) StartTimerAtomic(
	ctx context.Context,
	accepted timeentries.TimerMutation,
) (timeentries.TimerSession, error) {
	var found timeentries.TimerSession
	err := r.withTransaction(ctx, func(tx transaction) error {
		session := accepted.Session
		var inserted bool
		err := scanTimer(
			tx.QueryRow(ctx, `
INSERT INTO ticket_timer_sessions (
  id, msp_id, client_id, work_record_id, technician_id, state,
  started_at, idempotency_key, version, created_at, updated_at,
  created_by, updated_by
)
SELECT $1, $2, $3, $4, $5, 'running', $6, $7, $8, $9, $10, $5, $5
WHERE EXISTS (
  SELECT 1
  FROM work_records
  WHERE id = $4 AND msp_id = $2 AND client_id = $3
    AND deleted_at IS NULL
) AND EXISTS (
  SELECT 1
  FROM technicians
  WHERE id = $5 AND msp_id = $2 AND lifecycle_state = 'active'
)
ON CONFLICT (msp_id, technician_id, idempotency_key)
DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key
RETURNING
  id::text, msp_id::text, client_id::text, work_record_id::text,
  technician_id::text, state, started_at, stopped_at, duration_seconds,
  COALESCE(consumed_time_entry_id::text, ''), idempotency_key, version,
  created_at, updated_at, (xmax = 0)
`, session.ID, session.MSPID, session.ClientID, session.WorkRecordID,
				session.TechnicianID, session.StartedAt, session.IdempotencyKey,
				session.Version, session.CreatedAt, session.UpdatedAt,
			),
			&found,
			&inserted,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return scope.ErrNotFound
		}
		if err != nil {
			return err
		}
		if !inserted {
			return nil
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
	if err != nil {
		return timeentries.TimerSession{}, err
	}
	return found, nil
}

func (r *TimeWorkforceRepository) GetTimer(
	ctx context.Context,
	target scope.Target,
	id string,
) (timeentries.TimerSession, error) {
	var found timeentries.TimerSession
	err := scanTimer(r.db.QueryRow(ctx, `
SELECT
  id::text, msp_id::text, client_id::text, work_record_id::text,
  technician_id::text, state, started_at, stopped_at, duration_seconds,
  COALESCE(consumed_time_entry_id::text, ''), idempotency_key, version,
  created_at, updated_at
FROM ticket_timer_sessions
WHERE id = $1 AND msp_id = $2 AND client_id = $3
`, id, target.MSPID, target.ClientID), &found, nil)
	if errors.Is(err, pgx.ErrNoRows) {
		return timeentries.TimerSession{}, scope.ErrNotFound
	}
	if err != nil {
		return timeentries.TimerSession{}, err
	}
	return found, nil
}

func (r *TimeWorkforceRepository) ListTicketTimers(
	ctx context.Context,
	target scope.Target,
	workRecordID string,
	technicianID string,
) ([]timeentries.TimerSession, error) {
	foundRows, err := r.db.Query(ctx, `
SELECT
  id::text, msp_id::text, client_id::text, work_record_id::text,
  technician_id::text, state, started_at, stopped_at, duration_seconds,
  COALESCE(consumed_time_entry_id::text, ''), idempotency_key, version,
  created_at, updated_at
FROM ticket_timer_sessions
WHERE msp_id = $1 AND client_id = $2 AND work_record_id = $3
  AND technician_id = $4 AND state IN ('running', 'stopped')
ORDER BY created_at DESC, id DESC
`, target.MSPID, target.ClientID, workRecordID, technicianID)
	if err != nil {
		return nil, err
	}
	defer foundRows.Close()
	found := make([]timeentries.TimerSession, 0)
	for foundRows.Next() {
		var session timeentries.TimerSession
		if err := scanTimer(foundRows, &session, nil); err != nil {
			return nil, err
		}
		found = append(found, session)
	}
	if err := foundRows.Err(); err != nil {
		return nil, err
	}
	return found, nil
}

func (r *TimeWorkforceRepository) StopTimerAtomic(
	ctx context.Context,
	accepted timeentries.TimerMutation,
) (timeentries.TimerSession, error) {
	session := accepted.Session
	return r.transitionTimer(ctx, accepted, `
UPDATE ticket_timer_sessions
SET state = $5, stopped_at = $6, duration_seconds = $7,
    updated_at = $8, updated_by = $4, version = $9
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND technician_id = $4 AND state = 'running' AND version = $9 - 1
RETURNING
  id::text, msp_id::text, client_id::text, work_record_id::text,
  technician_id::text, state, started_at, stopped_at, duration_seconds,
  COALESCE(consumed_time_entry_id::text, ''), idempotency_key, version,
  created_at, updated_at
`, session.ID, session.MSPID, session.ClientID, session.TechnicianID,
		session.State, session.StoppedAt, session.DurationSeconds,
		session.UpdatedAt, session.Version,
	)
}

func (r *TimeWorkforceRepository) DiscardTimerAtomic(
	ctx context.Context,
	accepted timeentries.TimerMutation,
) (timeentries.TimerSession, error) {
	session := accepted.Session
	return r.transitionTimer(ctx, accepted, `
UPDATE ticket_timer_sessions
SET state = 'discarded', updated_at = $5, updated_by = $4, version = $6
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND technician_id = $4 AND state = 'stopped' AND version = $6 - 1
RETURNING
  id::text, msp_id::text, client_id::text, work_record_id::text,
  technician_id::text, state, started_at, stopped_at, duration_seconds,
  COALESCE(consumed_time_entry_id::text, ''), idempotency_key, version,
  created_at, updated_at
`, session.ID, session.MSPID, session.ClientID, session.TechnicianID,
		session.UpdatedAt, session.Version,
	)
}

func (r *TimeWorkforceRepository) ConsumeTimerAtomic(
	ctx context.Context,
	accepted timeentries.TimerMutation,
) (timeentries.TimerSession, error) {
	session := accepted.Session
	return r.transitionTimer(ctx, accepted, `
UPDATE ticket_timer_sessions
SET state = 'consumed', consumed_time_entry_id = $5,
    updated_at = $6, updated_by = $4, version = $7
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND technician_id = $4 AND state = 'stopped' AND version = $7 - 1
RETURNING
  id::text, msp_id::text, client_id::text, work_record_id::text,
  technician_id::text, state, started_at, stopped_at, duration_seconds,
  COALESCE(consumed_time_entry_id::text, ''), idempotency_key, version,
  created_at, updated_at
`, session.ID, session.MSPID, session.ClientID, session.TechnicianID,
		session.ConsumedTimeEntryID, session.UpdatedAt, session.Version,
	)
}

func (r *TimeWorkforceRepository) CreateFromCaptureAtomic(
	ctx context.Context,
	accepted timeentries.CaptureMutation,
) (timeentries.Entry, error) {
	var entry timeentries.Entry
	err := r.withTransaction(ctx, func(tx transaction) error {
		var err error
		entry, err = createFromCaptureInTransaction(ctx, tx, accepted)
		return err
	})
	if err != nil {
		return timeentries.Entry{}, err
	}
	return entry, nil
}

func createFromCaptureInTransaction(
	ctx context.Context,
	tx transaction,
	accepted timeentries.CaptureMutation,
) (timeentries.Entry, error) {
	var startedAt, stoppedAt time.Time
	var durationSeconds int64
	err := tx.QueryRow(ctx, `
SELECT started_at, stopped_at, duration_seconds
FROM ticket_timer_sessions
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND work_record_id = $4 AND technician_id = $5
  AND state = 'stopped' AND version = $6
FOR UPDATE
`, accepted.CaptureID, accepted.Target.MSPID, accepted.Target.ClientID,
		accepted.WorkRecordID, accepted.TechnicianID,
		accepted.ExpectedCaptureVersion,
	).Scan(&startedAt, &stoppedAt, &durationSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return timeentries.Entry{}, object.ErrVersionConflict
	}
	if err != nil {
		return timeentries.Entry{}, err
	}

	var snapshot timeentries.RateSnapshot
	err = tx.QueryRow(ctx, `
SELECT
  role_version.id::text,
  COALESCE(technician_rate.hourly_rate_minor, role_version.internal_cost_minor),
  role_version.bill_rate_minor,
  role_version.currency
FROM labor_roles role
JOIN LATERAL (
  SELECT candidate.*
  FROM labor_role_versions candidate
  WHERE candidate.labor_role_id = role.id
    AND candidate.msp_id = role.msp_id
    AND candidate.effective_from <= $4
    AND (candidate.effective_until IS NULL OR candidate.effective_until > $4)
    AND candidate.enabled
  ORDER BY candidate.effective_from DESC, candidate.id DESC
  LIMIT 1
) role_version ON true
LEFT JOIN LATERAL (
  SELECT candidate.hourly_rate_minor
  FROM technician_labor_cost_rates candidate
  WHERE candidate.msp_id = role.msp_id
    AND candidate.technician_id = $3
    AND candidate.currency = role_version.currency
    AND candidate.effective_at <= $4
  ORDER BY candidate.effective_at DESC, candidate.id DESC
  LIMIT 1
) technician_rate ON true
WHERE role.msp_id = $1 AND role.id = $2
`, accepted.Target.MSPID, accepted.LaborRoleID,
		accepted.TechnicianID, startedAt,
	).Scan(
		&snapshot.LaborRoleVersionID,
		&snapshot.InternalCostMinor,
		&snapshot.BillRateMinor,
		&snapshot.Currency,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return timeentries.Entry{}, scope.ErrNotFound
	}
	if err != nil {
		return timeentries.Entry{}, err
	}

	entry := timeentries.Entry{
		ID:    accepted.EntryID,
		MSPID: accepted.Target.MSPID, ClientID: accepted.Target.ClientID,
		WorkRecordID: accepted.WorkRecordID,
		TechnicianID: accepted.TechnicianID,
		StartedAt:    startedAt, EndedAt: stoppedAt,
		DurationSeconds: durationSeconds,
		Billable:        accepted.Billable, Note: accepted.Note,
		LaborRoleVersionID: snapshot.LaborRoleVersionID,
		InternalCostMinor:  snapshot.InternalCostMinor,
		BillRateMinor:      snapshot.BillRateMinor,
		RateCurrency:       snapshot.Currency,
		Version:            1, CreatedAt: accepted.CreatedAt,
		CreatedBy: accepted.TechnicianID,
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO time_entries (
  id, msp_id, client_id, work_record_id, technician_id,
  started_at, ended_at, duration_seconds, billable, note,
  labor_role_version_id, internal_cost_minor, bill_rate_minor, rate_currency,
  version, created_at, created_by
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
  $11, $12, $13, $14, $15, $16, $17
)
`, entry.ID, entry.MSPID, entry.ClientID, entry.WorkRecordID,
		entry.TechnicianID, entry.StartedAt, entry.EndedAt,
		entry.DurationSeconds, entry.Billable, entry.Note,
		entry.LaborRoleVersionID, entry.InternalCostMinor,
		entry.BillRateMinor, entry.RateCurrency, entry.Version,
		entry.CreatedAt, entry.CreatedBy,
	); err != nil {
		return timeentries.Entry{}, err
	}
	if err := insertInitialTagAssignments(ctx, tx, tagging.TargetRef{
		MSPID: entry.MSPID, ClientID: entry.ClientID, ObjectType: tagging.ObjectTimeEntry, ObjectID: entry.ID,
	}, entry.Version, accepted.InitialTags); err != nil {
		return timeentries.Entry{}, err
	}
	tag, err := tx.Exec(ctx, `
UPDATE ticket_timer_sessions
SET state = 'consumed', consumed_time_entry_id = $7,
    version = $6 + 1, updated_at = $8, updated_by = $5
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND work_record_id = $4 AND technician_id = $5
  AND state = 'stopped' AND version = $6
`, accepted.CaptureID, accepted.Target.MSPID, accepted.Target.ClientID,
		accepted.WorkRecordID, accepted.TechnicianID,
		accepted.ExpectedCaptureVersion, entry.ID, accepted.CreatedAt,
	)
	if err != nil {
		return timeentries.Entry{}, err
	}
	if tag.RowsAffected() != 1 {
		return timeentries.Entry{}, object.ErrVersionConflict
	}
	if err := writeMutationFacts(
		ctx,
		tx,
		accepted.EntryAudit,
		accepted.EntryEvent,
	); err != nil {
		return timeentries.Entry{}, err
	}
	if err := writeMutationFacts(
		ctx,
		tx,
		accepted.TimerAudit,
		accepted.TimerEvent,
	); err != nil {
		return timeentries.Entry{}, err
	}
	return entry, nil
}

func (r *TimeWorkforceRepository) transitionTimer(
	ctx context.Context,
	accepted timeentries.TimerMutation,
	query string,
	args ...any,
) (timeentries.TimerSession, error) {
	var found timeentries.TimerSession
	err := r.withTransaction(ctx, func(tx transaction) error {
		if err := scanTimer(tx.QueryRow(ctx, query, args...), &found, nil); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return object.ErrVersionConflict
			}
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
	if err != nil {
		return timeentries.TimerSession{}, err
	}
	return found, nil
}

func scanTimer(
	found row,
	session *timeentries.TimerSession,
	inserted *bool,
) error {
	destinations := []any{
		&session.ID,
		&session.MSPID,
		&session.ClientID,
		&session.WorkRecordID,
		&session.TechnicianID,
		&session.State,
		&session.StartedAt,
		&session.StoppedAt,
		&session.DurationSeconds,
		&session.ConsumedTimeEntryID,
		&session.IdempotencyKey,
		&session.Version,
		&session.CreatedAt,
		&session.UpdatedAt,
	}
	if inserted != nil {
		destinations = append(destinations, inserted)
	}
	return found.Scan(destinations...)
}

func (r *TimeWorkforceRepository) withTransaction(
	ctx context.Context,
	fn func(transaction) error,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

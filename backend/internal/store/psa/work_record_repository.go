package psa

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

// Keep every query consumed by scanWorkRecord on the same projection.
const workRecordColumns = `id::text, msp_id::text, client_id::text, display_id, record_type,
       title, description, status, priority, COALESCE(queue_id::text, ''),
       COALESCE(primary_owner_id::text, ''),
       COALESCE(service_id::text, ''), COALESCE(contract_id::text, ''),
       lifecycle_state, version,
       created_at, created_by::text, updated_at, updated_by::text,
       deleted_at, COALESCE(deleted_by::text, ''),
       COALESCE(merged_into_id::text, ''), scheduled_starts_at,
       scheduled_ends_at, COALESCE(schedule_timezone,''), scheduling_mode,
       planned_effort_minutes, due_on, follow_up_on, schedule_recurrence`

type WorkRecordRepository struct {
	db database
}

var _ workrecords.Repository = (*WorkRecordRepository)(nil)
var _ workrecords.AssignmentRepository = (*WorkRecordRepository)(nil)
var _ workrecords.QueueRepository = (*WorkRecordRepository)(nil)
var _ workrecords.MergeRepository = (*WorkRecordRepository)(nil)
var _ workrecords.ParticipantRepository = (*WorkRecordRepository)(nil)
var _ workrecords.TransitionRepository = (*WorkRecordRepository)(nil)
var _ workrecords.PriorityRepository = (*WorkRecordRepository)(nil)
var _ workrecords.SLAOverrideRepository = (*WorkRecordRepository)(nil)

func NewWorkRecordRepository(db database) *WorkRecordRepository {
	return &WorkRecordRepository{db: db}
}

func (r *WorkRecordRepository) EnsureDisplayIDAvailable(
	ctx context.Context,
	target scope.Target,
	displayID string,
) error {
	var exists bool
	err := r.db.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM work_records
  WHERE msp_id = $1 AND client_id = $2 AND display_id = $3
)
`, target.MSPID, target.ClientID, displayID).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return workrecords.ErrDisplayIDConflict
	}
	return nil
}

func (r *WorkRecordRepository) ValidateReferences(
	ctx context.Context,
	target scope.Target,
	references workrecords.ContextReferences,
	at time.Time,
) error {
	var valid bool
	err := r.db.QueryRow(ctx, `
SELECT
  (
    EXISTS (
      SELECT 1 FROM client_organizations
      WHERE msp_id = $1 AND id = $2::uuid
        AND lifecycle_state = 'active'
    )
    AND
    (
    $3 = ''
    OR EXISTS (
      SELECT 1 FROM services
      WHERE id = $3::uuid AND msp_id = $1 AND client_id = $2
        AND lifecycle_state = 'active'
    )
  )
  AND
  (
    $4 = ''
    OR EXISTS (
      SELECT 1 FROM contracts
      WHERE id = $4::uuid AND msp_id = $1 AND client_id = $2
        AND lifecycle_state = 'active'
        AND starts_on <= $5::date
        AND (ends_on IS NULL OR ends_on >= $5::date)
    )
  )
  )
`, target.MSPID, target.ClientID, references.ServiceID, references.ContractID, at).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return scope.ErrNotFound
	}
	return nil
}

func (r *WorkRecordRepository) Find(
	ctx context.Context,
	target scope.Target,
	id string,
) (workrecords.Record, error) {
	record, err := scanWorkRecord(r.db.QueryRow(ctx, `
SELECT `+workRecordColumns+`
FROM work_records
WHERE id = $1 AND msp_id = $2 AND client_id = $3 AND deleted_at IS NULL
`, id, target.MSPID, target.ClientID))
	if errors.Is(err, pgx.ErrNoRows) {
		return workrecords.Record{}, scope.ErrNotFound
	}
	return record, err
}

func (r *WorkRecordRepository) List(
	ctx context.Context,
	filter workrecords.ListFilter,
) ([]workrecords.Record, error) {
	var before any
	if !filter.BeforeUpdatedAt.IsZero() {
		before = filter.BeforeUpdatedAt
	}
	rows, err := r.db.Query(ctx, `
SELECT `+workRecordColumns+`
FROM work_records
WHERE msp_id = $1 AND client_id = $2 AND deleted_at IS NULL
  AND ($3 = '' OR status = $3)
  AND ($4 = '' OR queue_id = NULLIF($4, '')::uuid)
  AND ($5 = '' OR primary_owner_id = NULLIF($5, '')::uuid)
  AND ($6::timestamptz IS NULL OR
       (updated_at, id) < ($6, NULLIF($7, '')::uuid))
  AND ($9 = '' OR strpos(lower(display_id || ' ' || title || ' ' || description), lower($9)) > 0)
  AND ($10 = '' OR priority = $10)
  AND ($11 = '' OR $11 = 'all'
       OR ($11 = 'assigned' AND primary_owner_id IS NOT NULL)
       OR ($11 = 'unassigned' AND primary_owner_id IS NULL))
ORDER BY updated_at DESC, id DESC
LIMIT $8
`, filter.Target.MSPID, filter.Target.ClientID, filter.Status,
		filter.QueueID, filter.PrimaryOwnerID, before, filter.BeforeID,
		filter.Limit, filter.Text, filter.Priority, filter.Ownership)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]workrecords.Record, 0)
	for rows.Next() {
		record, err := scanWorkRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func scanWorkRecord(source interface{ Scan(...any) error }) (workrecords.Record, error) {
	var record workrecords.Record
	var recurrence []byte
	err := source.Scan(
		&record.ID, &record.MSPID, &record.ClientID, &record.DisplayID,
		&record.Type, &record.Title, &record.Description, &record.Status,
		&record.Priority, &record.QueueID, &record.PrimaryOwnerID,
		&record.ServiceID, &record.ContractID,
		&record.LifecycleState, &record.Version, &record.CreatedAt,
		&record.CreatedBy, &record.UpdatedAt, &record.UpdatedBy,
		&record.DeletedAt, &record.DeletedBy, &record.MergedIntoID,
		&record.ScheduledStartsAt, &record.ScheduledEndsAt, &record.ScheduleTimezone,
		&record.SchedulingMode, &record.PlannedEffortMinutes, &record.DueOn,
		&record.FollowUpOn, &recurrence,
	)
	if err == nil && len(recurrence) > 0 {
		err = json.Unmarshal(recurrence, &record.ScheduleRecurrence)
	}
	if err == nil {
		record.ObjectType = "work_record"
	}
	return record, err
}

func (r *WorkRecordRepository) FindForQueue(
	ctx context.Context,
	target scope.Target,
	id string,
) (workrecords.Record, error) {
	return r.Find(ctx, target, id)
}

func (r *WorkRecordRepository) ResolveWorkRecordForQueue(
	ctx context.Context,
	target scope.Target,
	reference string,
	limit int,
) ([]workrecords.Record, error) {
	if limit < 1 || limit > 2 {
		limit = 2
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(reference), " "))
	rows, err := r.db.Query(ctx, `
SELECT `+workRecordColumns+`
FROM work_records
WHERE msp_id = $1 AND client_id = $2::uuid AND deleted_at IS NULL
  AND lifecycle_state = 'active'
  AND (
    lower(regexp_replace(btrim(display_id), '\s+', ' ', 'g')) = $3
    OR lower(regexp_replace(btrim(title), '\s+', ' ', 'g')) = $3
  )
ORDER BY id
LIMIT $4
`, target.MSPID, target.ClientID, normalized, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]workrecords.Record, 0, limit)
	for rows.Next() {
		record, err := scanWorkRecord(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

func (r *WorkRecordRepository) ResolveWorkRecordForAssignment(
	ctx context.Context,
	target scope.Target,
	reference string,
	limit int,
) ([]workrecords.Record, error) {
	if limit < 1 || limit > 2 {
		limit = 2
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(reference), " "))
	if target.MSPID == "" || target.ClientID == "" || normalized == "" {
		return nil, workrecords.ErrInvalid
	}
	rows, err := r.db.Query(ctx, `
SELECT `+workRecordColumns+`
FROM work_records
WHERE msp_id = $1 AND client_id = $2::uuid AND deleted_at IS NULL
  AND lifecycle_state = 'active'
  AND (
    lower(regexp_replace(btrim(display_id), '\s+', ' ', 'g')) = $3
    OR lower(regexp_replace(btrim(title), '\s+', ' ', 'g')) = $3
  )
ORDER BY CASE WHEN lower(regexp_replace(btrim(display_id), '\s+', ' ', 'g')) = $3 THEN 0 ELSE 1 END, id
LIMIT $4
`, target.MSPID, target.ClientID, normalized, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]workrecords.Record, 0, limit)
	for rows.Next() {
		record, err := scanWorkRecord(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

func (r *WorkRecordRepository) ResolveQueueForRoute(
	ctx context.Context,
	target scope.Target,
	reference string,
	limit int,
) ([]workrecords.QueueRef, error) {
	if limit < 1 || limit > 2 {
		limit = 2
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(reference), " "))
	rows, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, COALESCE(client_id::text, ''),
       key, name, version
FROM queues
WHERE msp_id = $1
  AND (client_id IS NULL OR client_id = $2::uuid)
  AND (
    lower(regexp_replace(btrim(key), '\s+', ' ', 'g')) = $3
    OR lower(regexp_replace(btrim(name), '\s+', ' ', 'g')) = $3
  )
ORDER BY id
LIMIT $4
`, target.MSPID, target.ClientID, normalized, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]workrecords.QueueRef, 0, limit)
	for rows.Next() {
		var queue workrecords.QueueRef
		if err := rows.Scan(
			&queue.ID, &queue.MSPID, &queue.ClientID,
			&queue.Key, &queue.Name, &queue.Version,
		); err != nil {
			return nil, err
		}
		result = append(result, queue)
	}
	return result, rows.Err()
}

func (r *WorkRecordRepository) FindQueueForRoute(
	ctx context.Context,
	target scope.Target,
	id string,
) (workrecords.QueueRef, error) {
	var queue workrecords.QueueRef
	err := r.db.QueryRow(ctx, `
SELECT id::text, msp_id::text, COALESCE(client_id::text, ''),
       key, name, version
FROM queues
WHERE id = $1::uuid AND msp_id = $2
  AND (client_id IS NULL OR client_id = $3::uuid)
`, id, target.MSPID, target.ClientID).Scan(
		&queue.ID, &queue.MSPID, &queue.ClientID,
		&queue.Key, &queue.Name, &queue.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return workrecords.QueueRef{}, scope.ErrNotFound
	}
	return queue, err
}

func (r *WorkRecordRepository) FindForMerge(
	ctx context.Context,
	target scope.Target,
	id string,
) (workrecords.Record, error) {
	return r.Find(ctx, target, id)
}

func (r *WorkRecordRepository) FindForParticipation(
	ctx context.Context,
	target scope.Target,
	id string,
) (workrecords.Record, error) {
	return r.Find(ctx, target, id)
}

func (r *WorkRecordRepository) FindParticipant(
	ctx context.Context,
	target scope.Target,
	workRecordID string,
	participantID string,
) (workrecords.Participant, error) {
	var participant workrecords.Participant
	err := r.db.QueryRow(ctx, `
SELECT id::text, msp_id::text, client_id::text, work_record_id::text,
       technician_id::text, role, version, added_at, added_by::text,
       removed_at, COALESCE(removed_by::text, '')
FROM work_record_participants
WHERE id = $1 AND work_record_id = $2
  AND msp_id = $3 AND client_id = $4
`, participantID, workRecordID, target.MSPID, target.ClientID).Scan(
		&participant.ID, &participant.MSPID, &participant.ClientID,
		&participant.WorkRecordID, &participant.TechnicianID,
		&participant.Role, &participant.Version, &participant.AddedAt,
		&participant.AddedBy, &participant.RemovedAt, &participant.RemovedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return workrecords.Participant{}, scope.ErrNotFound
	}
	return participant, err
}

func (r *WorkRecordRepository) AddParticipantAtomic(
	ctx context.Context,
	accepted workrecords.ParticipantMutation,
) error {
	record, participant := accepted.Record, accepted.Participant
	return r.updateWithFacts(ctx, func(tx transaction) error {
		if err := updateParticipationRecord(ctx, tx, record); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
INSERT INTO work_record_participants (
  id, work_record_id, msp_id, client_id, technician_id, role,
  version, added_at, added_by
)
SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9
WHERE EXISTS (
  SELECT 1 FROM technicians
  WHERE id = $5 AND msp_id = $3 AND lifecycle_state = 'active'
)
`, participant.ID, participant.WorkRecordID, participant.MSPID,
			participant.ClientID, participant.TechnicianID, participant.Role,
			participant.Version, participant.AddedAt, participant.AddedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return scope.ErrNotFound
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *WorkRecordRepository) RemoveParticipantAtomic(
	ctx context.Context,
	accepted workrecords.ParticipantMutation,
) error {
	record, participant := accepted.Record, accepted.Participant
	return r.updateWithFacts(ctx, func(tx transaction) error {
		if err := updateParticipationRecord(ctx, tx, record); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
UPDATE work_record_participants
SET version = version + 1, removed_at = $6, removed_by = $7
WHERE id = $1 AND work_record_id = $2
  AND msp_id = $3 AND client_id = $4
  AND version = $5 AND removed_at IS NULL
`, participant.ID, record.ID, record.MSPID, record.ClientID,
			participant.Version-1, participant.RemovedAt, participant.RemovedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func updateParticipationRecord(
	ctx context.Context,
	tx transaction,
	record workrecords.Record,
) error {
	tag, err := tx.Exec(ctx, `
UPDATE work_records
SET version = version + 1, updated_at = $6, updated_by = $7
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND version = $4 AND deleted_at IS NULL AND lifecycle_state = $5
`, record.ID, record.MSPID, record.ClientID, record.Version-1,
		record.LifecycleState, record.UpdatedAt, record.UpdatedBy)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	return nil
}

func (r *WorkRecordRepository) AssignAtomic(
	ctx context.Context,
	accepted workrecords.AssignmentMutation,
) error {
	record := accepted.Record
	return r.updateWithFacts(ctx, func(tx transaction) error {
		if err := lockActiveClientAtVersion(ctx, tx, record.MSPID, record.ClientID, accepted.ExpectedClientVersion); err != nil {
			return err
		}
		if err := lockActiveWorkRecordAtVersion(ctx, tx, record); err != nil {
			return err
		}
		if err := lockActiveAssignmentCandidate(ctx, tx, record, accepted.ExpectedOwnerVersion); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
UPDATE work_records
SET primary_owner_id = $5, version = version + 1,
    updated_at = $6, updated_by = $7
WHERE id = $1 AND msp_id = $2 AND client_id = $3 AND version = $4
  AND deleted_at IS NULL
`, record.ID, record.MSPID, record.ClientID, record.Version-1,
			record.PrimaryOwnerID, record.UpdatedAt, record.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func lockActiveWorkRecordAtVersion(
	ctx context.Context,
	tx transaction,
	record workrecords.Record,
) error {
	tag, err := tx.Exec(ctx, `
SELECT 1
FROM work_records
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND version = $4 AND deleted_at IS NULL AND lifecycle_state = 'active'
FOR UPDATE
`, record.ID, record.MSPID, record.ClientID, record.Version-1)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	return nil
}

func lockActiveAssignmentCandidate(
	ctx context.Context,
	tx transaction,
	record workrecords.Record,
	expectedVersion int64,
) error {
	tag, err := tx.Exec(ctx, `
SELECT 1
FROM technicians t
JOIN role_assignments ra
  ON ra.technician_id = t.id AND ra.msp_id = t.msp_id
WHERE t.id = $1 AND t.msp_id = $2 AND t.lifecycle_state = 'active'
  AND (ra.client_id IS NULL OR ra.client_id = $3::uuid)
  AND ($4 = 0 OR t.version = $4)
  AND (ra.expires_at IS NULL OR ra.expires_at > $5)
ORDER BY ra.id
FOR SHARE OF t, ra
LIMIT 1
`, record.PrimaryOwnerID, record.MSPID, record.ClientID, expectedVersion, record.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return scope.ErrNotFound
	}
	return nil
}

func (r *WorkRecordRepository) ChangePriorityAtomic(
	ctx context.Context,
	accepted workrecords.PriorityMutation,
) error {
	record := accepted.Record
	return r.updateWithFacts(ctx, func(tx transaction) error {
		if err := lockActiveClient(
			ctx, tx, record.MSPID, record.ClientID,
		); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
UPDATE work_records
SET priority = $5, version = version + 1, updated_at = $6, updated_by = $7
WHERE id = $1 AND msp_id = $2 AND client_id = $3 AND version = $4
  AND deleted_at IS NULL
`, record.ID, record.MSPID, record.ClientID, record.Version-1,
			record.Priority, record.UpdatedAt, record.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		if err := writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.SLAAudit, accepted.SLAEvent)
	})
}

func (r *WorkRecordRepository) OverrideSLAAtomic(
	ctx context.Context,
	accepted workrecords.SLAOverrideMutation,
) error {
	record, slaRecord := accepted.Record, accepted.SLA
	return r.updateWithFacts(ctx, func(tx transaction) error {
		tag, err := tx.Exec(ctx, `
UPDATE work_records
SET version = version + 1, updated_at = $5, updated_by = $6
WHERE id = $1 AND msp_id = $2 AND client_id = $3 AND version = $4
  AND deleted_at IS NULL
`, record.ID, record.MSPID, record.ClientID, record.Version-1,
			record.UpdatedAt, record.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		tag, err = tx.Exec(ctx, `
UPDATE work_record_slas
SET response_warning_at = $6, response_due_at = $7,
    resolution_warning_at = $8, resolution_due_at = $9,
    response_state = $10, resolution_state = $11,
    state = $11, version = version + 1
WHERE id = $1 AND work_record_id = $2 AND msp_id = $3 AND client_id = $4
  AND version = $5
`, slaRecord.ID, record.ID, record.MSPID, record.ClientID,
			slaRecord.Version-1, slaRecord.ResponseWarningAt, slaRecord.ResponseDueAt,
			slaRecord.ResolutionWarningAt, slaRecord.ResolutionDueAt,
			slaRecord.ResponseState, slaRecord.ResolutionState)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		evidence := accepted.Evidence
		if _, err := tx.Exec(ctx, `
INSERT INTO sla_overrides (
  id, sla_id, work_record_id, msp_id, client_id, work_record_version,
  sla_version_before, sla_version_after,
  response_warning_at_before, response_warning_at_after,
  response_due_at_before, response_due_at_after,
  resolution_warning_at_before, resolution_warning_at_after,
  resolution_due_at_before, resolution_due_at_after,
  reason, overridden_at, overridden_by
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8,
  $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
)
`, evidence.ID, evidence.SLAID, evidence.WorkRecordID,
			evidence.MSPID, evidence.ClientID, evidence.WorkRecordVersion,
			evidence.SLAVersionBefore, evidence.SLAVersionAfter,
			evidence.ResponseWarningAtBefore, evidence.ResponseWarningAtAfter,
			evidence.ResponseDueAtBefore, evidence.ResponseDueAtAfter,
			evidence.ResolutionWarningAtBefore, evidence.ResolutionWarningAtAfter,
			evidence.ResolutionDueAtBefore, evidence.ResolutionDueAtAfter,
			evidence.Reason, evidence.OverriddenAt, evidence.OverriddenBy); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *WorkRecordRepository) FindSelectedWorkflow(
	ctx context.Context,
	target scope.Target,
	workRecordID string,
) (workrecords.SelectedWorkflow, error) {
	var selected workrecords.SelectedWorkflow
	var definition []byte
	err := r.db.QueryRow(ctx, `
SELECT wrw.workflow_id::text, wrw.workflow_version, wv.definition
FROM work_record_workflows wrw
JOIN workflow_versions wv
  ON wv.workflow_id = wrw.workflow_id AND wv.version = wrw.workflow_version
WHERE wrw.work_record_id = $1 AND wrw.msp_id = $2 AND wrw.client_id = $3
`, workRecordID, target.MSPID, target.ClientID).Scan(
		&selected.WorkflowID, &selected.Version, &definition,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return workrecords.SelectedWorkflow{}, scope.ErrNotFound
	}
	if err != nil {
		return workrecords.SelectedWorkflow{}, err
	}
	if err := json.Unmarshal(definition, &selected.Definition); err != nil {
		return workrecords.SelectedWorkflow{}, err
	}
	return selected, nil
}

func (r *WorkRecordRepository) FindAppliedSLA(
	ctx context.Context,
	target scope.Target,
	workRecordID string,
) (workrecords.AppliedSLA, error) {
	var (
		applied                     workrecords.AppliedSLA
		pauseStates, selectionTrace []byte
		weeklySchedule, holidays    []byte
	)
	err := r.db.QueryRow(ctx, `
SELECT wrs.id::text, wrs.policy_id::text, wrs.policy_version,
       wrs.calendar_id::text, wrs.calendar_version,
       wrs.response_warning_at, wrs.response_due_at,
       wrs.resolution_warning_at, wrs.resolution_due_at,
       wrs.responded_at, wrs.resolved_at, wrs.paused_at, wrs.paused_seconds,
       wrs.pause_states, wrs.response_state, wrs.resolution_state,
       wrs.version, wrs.selection_trace,
       cv.timezone, cv.weekly_schedule, cv.holidays
FROM work_record_slas wrs
JOIN business_calendar_versions cv
  ON cv.calendar_id = wrs.calendar_id AND cv.version = wrs.calendar_version
WHERE wrs.work_record_id = $1 AND wrs.msp_id = $2 AND wrs.client_id = $3
`, workRecordID, target.MSPID, target.ClientID).Scan(
		&applied.ID, &applied.PolicyID, &applied.PolicyVersion,
		&applied.CalendarID, &applied.CalendarVersion,
		&applied.ResponseWarningAt, &applied.ResponseDueAt,
		&applied.ResolutionWarningAt, &applied.ResolutionDueAt,
		&applied.RespondedAt, &applied.ResolvedAt, &applied.PausedAt,
		&applied.PausedSeconds, &pauseStates,
		&applied.ResponseState, &applied.ResolutionState,
		&applied.Version, &selectionTrace,
		&applied.CalendarDefinition.Timezone, &weeklySchedule, &holidays,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return workrecords.AppliedSLA{}, scope.ErrNotFound
	}
	if err != nil {
		return workrecords.AppliedSLA{}, err
	}
	if err := json.Unmarshal(pauseStates, &applied.PauseStates); err != nil {
		return workrecords.AppliedSLA{}, err
	}
	if err := json.Unmarshal(selectionTrace, &applied.SelectionTrace); err != nil {
		return workrecords.AppliedSLA{}, err
	}
	if err := json.Unmarshal(weeklySchedule, &applied.CalendarDefinition.Weekly); err != nil {
		return workrecords.AppliedSLA{}, err
	}
	if err := json.Unmarshal(holidays, &applied.CalendarDefinition.Holidays); err != nil {
		return workrecords.AppliedSLA{}, err
	}
	return applied, nil
}

func (r *WorkRecordRepository) TransitionAtomic(
	ctx context.Context,
	accepted workrecords.TransitionMutation,
) error {
	record := accepted.Record
	return r.updateWithFacts(ctx, func(tx transaction) error {
		if err := lockActiveClient(
			ctx, tx, record.MSPID, record.ClientID,
		); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
UPDATE work_records
SET status = $5, version = version + 1, updated_at = $6, updated_by = $7
WHERE id = $1 AND msp_id = $2 AND client_id = $3 AND version = $4
  AND deleted_at IS NULL
  AND EXISTS (
    SELECT 1 FROM work_record_workflows
    WHERE work_record_id = $1 AND msp_id = $2 AND client_id = $3
      AND workflow_id = $8 AND workflow_version = $9
  )
`, record.ID, record.MSPID, record.ClientID, record.Version-1,
			record.Status, record.UpdatedAt, record.UpdatedBy,
			accepted.WorkflowID, accepted.WorkflowVersion)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		if accepted.SLAChanged {
			slaRecord := accepted.SLA
			tag, err := tx.Exec(ctx, `
UPDATE work_record_slas
SET response_warning_at = $6, response_due_at = $7,
    resolution_warning_at = $8, resolution_due_at = $9,
    responded_at = $10, resolved_at = $11, paused_at = $12,
    paused_seconds = $13, response_state = $14, resolution_state = $15,
    state = $15, version = version + 1
WHERE id = $1 AND work_record_id = $2 AND msp_id = $3 AND client_id = $4
  AND version = $5
`, slaRecord.ID, record.ID, record.MSPID, record.ClientID,
				slaRecord.Version-1, slaRecord.ResponseWarningAt, slaRecord.ResponseDueAt,
				slaRecord.ResolutionWarningAt, slaRecord.ResolutionDueAt,
				slaRecord.RespondedAt, slaRecord.ResolvedAt, slaRecord.PausedAt,
				slaRecord.PausedSeconds, slaRecord.ResponseState,
				slaRecord.ResolutionState)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return object.ErrVersionConflict
			}
		}
		if err := writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
			return err
		}
		if accepted.SLAChanged {
			return writeMutationFacts(ctx, tx, accepted.SLAAudit, accepted.SLAEvent)
		}
		return nil
	})
}

func (r *WorkRecordRepository) TransferQueueAtomic(
	ctx context.Context,
	accepted workrecords.QueueMutation,
) error {
	record := accepted.Record
	queue := accepted.Queue
	if queue.ID != record.QueueID || queue.MSPID != record.MSPID ||
		(queue.ClientID != "" && queue.ClientID != record.ClientID) ||
		strings.TrimSpace(queue.Key) == "" || strings.TrimSpace(queue.Name) == "" ||
		queue.Version < 1 {
		return workrecords.ErrInvalid
	}
	return r.updateWithFacts(ctx, func(tx transaction) error {
		if err := lockActiveClient(
			ctx, tx, record.MSPID, record.ClientID,
		); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
UPDATE work_records
SET queue_id = $5, version = version + 1, updated_at = $6, updated_by = $7
WHERE id = $1 AND msp_id = $2 AND client_id = $3 AND version = $4
  AND deleted_at IS NULL
  AND EXISTS (
    SELECT 1 FROM queues
    WHERE queues.id = $5 AND queues.msp_id = $2
      AND (queues.client_id IS NULL OR queues.client_id = $3)
      AND queues.version = $8 AND queues.key = $9 AND queues.name = $10
  )
`, record.ID, record.MSPID, record.ClientID, record.Version-1,
			record.QueueID, record.UpdatedAt, record.UpdatedBy,
			queue.Version, queue.Key, queue.Name)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *WorkRecordRepository) MergeAtomic(
	ctx context.Context,
	accepted workrecords.MergeMutation,
) error {
	if !accepted.ReparentChildren {
		return workrecords.ErrInvalid
	}
	winner, duplicate := accepted.Winner, accepted.Duplicate
	return r.updateWithFacts(ctx, func(tx transaction) error {
		if _, err := tx.Exec(ctx, `SELECT lock_mention_authorization_revision($1::uuid)`, winner.MSPID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
UPDATE work_records
SET version = version + 1, updated_at = $5, updated_by = $6
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND version = $4 AND deleted_at IS NULL
`, winner.ID, winner.MSPID, winner.ClientID, winner.Version-1,
			winner.UpdatedAt, winner.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		tag, err = tx.Exec(ctx, `
UPDATE work_records
SET lifecycle_state = $5, deleted_at = $6, deleted_by = $7,
    merged_into_id = $8, version = version + 1,
    updated_at = $9, updated_by = $10
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND version = $4 AND deleted_at IS NULL
`, duplicate.ID, duplicate.MSPID, duplicate.ClientID,
			duplicate.Version-1, duplicate.LifecycleState,
			duplicate.DeletedAt, duplicate.DeletedBy, duplicate.MergedIntoID,
			duplicate.UpdatedAt, duplicate.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		if _, err := tx.Exec(ctx, `
UPDATE tasks
SET work_record_id = $1,
    parent_id = CASE WHEN parent_type = 'work_record' THEN $1 ELSE parent_id END,
    version = version + 1, updated_at = $5, updated_by = $6
WHERE work_record_id = $2 AND msp_id = $3 AND client_id = $4
`, winner.ID, duplicate.ID, winner.MSPID, winner.ClientID,
			winner.UpdatedAt, winner.UpdatedBy); err != nil {
			return err
		}
		for _, table := range []string{"comments", "attachments", "time_entries"} {
			query := "UPDATE " + table + `
 SET work_record_id = $1
 WHERE work_record_id = $2 AND msp_id = $3 AND client_id = $4`
			if _, err := tx.Exec(
				ctx, query, winner.ID, duplicate.ID, winner.MSPID, winner.ClientID,
			); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `
UPDATE work_record_participants AS duplicate_participant
SET removed_at = $5, removed_by = $6, version = version + 1
WHERE duplicate_participant.work_record_id = $2
  AND duplicate_participant.msp_id = $3
  AND duplicate_participant.client_id = $4
  AND duplicate_participant.removed_at IS NULL
  AND EXISTS (
    SELECT 1
    FROM work_record_participants AS winner_participant
    WHERE winner_participant.work_record_id = $1
      AND winner_participant.msp_id = $3
      AND winner_participant.client_id = $4
      AND winner_participant.technician_id = duplicate_participant.technician_id
      AND winner_participant.role = duplicate_participant.role
      AND winner_participant.removed_at IS NULL
  )
`, winner.ID, duplicate.ID, winner.MSPID, winner.ClientID,
			winner.UpdatedAt, winner.UpdatedBy); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE work_record_participants
SET work_record_id = $1
WHERE work_record_id = $2 AND msp_id = $3 AND client_id = $4
  AND removed_at IS NULL
`, winner.ID, duplicate.ID, winner.MSPID, winner.ClientID); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *WorkRecordRepository) updateWithFacts(
	ctx context.Context,
	update func(transaction) error,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if err := update(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *WorkRecordRepository) CreateAtomic(
	ctx context.Context,
	accepted workrecords.CreateMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	record := accepted.Record
	if err := lockActiveClientAtVersion(
		ctx, tx, record.MSPID, record.ClientID, accepted.ExpectedClientVersion,
	); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := lockWorkRecordCreateDependencies(ctx, tx, accepted); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := lockWorkRecordCreateConfiguration(ctx, tx, accepted); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO work_records (
  id, msp_id, client_id, display_id, record_type, title, description,
  status, priority, queue_id, primary_owner_id, service_id, contract_id,
  lifecycle_state, version, created_at, created_by, updated_at, updated_by
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9,
  NULLIF($10, '')::uuid, NULLIF($11, '')::uuid,
  NULLIF($12, '')::uuid, NULLIF($13, '')::uuid,
  $14, $15, $16, $17, $18, $19
)
`, record.ID, record.MSPID, record.ClientID, record.DisplayID, record.Type,
		record.Title, record.Description, record.Status, record.Priority,
		record.QueueID, record.PrimaryOwnerID, record.ServiceID, record.ContractID,
		record.LifecycleState, record.Version, record.CreatedAt, record.CreatedBy, record.UpdatedAt,
		record.UpdatedBy); err != nil {
		_ = tx.Rollback(ctx)
		return workRecordWriteError(err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO work_record_routing (
  work_record_id, msp_id, client_id, rule_set_id, rule_set_version,
  rule_id, queue_id, decided_at, explanation
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
`, record.ID, record.MSPID, record.ClientID,
		accepted.Routing.RuleSetID, accepted.Routing.RuleSetVersion,
		accepted.Routing.Decision.RuleID, accepted.Routing.Decision.QueueID,
		accepted.Routing.DecidedAt, accepted.Routing.Decision.Explanation); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	trace, err := json.Marshal(accepted.Workflow.Trace)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO work_record_workflows (
  work_record_id, msp_id, client_id, workflow_id, workflow_version,
  selected_at, matched_trace
) VALUES ($1, $2, $3, $4, $5, $6, $7)
`, record.ID, record.MSPID, record.ClientID, accepted.Workflow.WorkflowID,
		accepted.Workflow.Version, accepted.Workflow.EvaluatedAt, trace); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	slaTrace, err := json.Marshal(accepted.SLA.SelectionTrace)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	pauseStates, err := json.Marshal(accepted.SLA.PauseStates)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO work_record_slas (
  id, work_record_id, msp_id, client_id, policy_id, policy_version,
  calendar_id, calendar_version,
  response_warning_at, response_due_at,
  resolution_warning_at, resolution_due_at,
  responded_at, resolved_at, paused_at, paused_seconds, pause_states,
  response_state, resolution_state, state, version, selection_trace
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8,
  $9, $10, $11, $12, $13, $14, $15, $16, $17,
  $18, $19, $19, $20, $21
)
	`, accepted.SLA.ID, record.ID, record.MSPID, record.ClientID,
		accepted.SLA.PolicyID, accepted.SLA.PolicyVersion,
		accepted.SLA.CalendarID, accepted.SLA.CalendarVersion,
		accepted.SLA.ResponseWarningAt, accepted.SLA.ResponseDueAt,
		accepted.SLA.ResolutionWarningAt, accepted.SLA.ResolutionDueAt,
		accepted.SLA.RespondedAt, accepted.SLA.ResolvedAt,
		accepted.SLA.PausedAt, accepted.SLA.PausedSeconds, pauseStates,
		accepted.SLA.ResponseState, accepted.SLA.ResolutionState,
		accepted.SLA.Version, slaTrace); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := insertInitialTagAssignments(ctx, tx, tagging.TargetRef{MSPID: record.MSPID, ClientID: record.ClientID, ObjectType: tagging.ObjectWorkRecord, ObjectID: record.ID}, record.Version, accepted.InitialTags); err != nil {
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

func lockWorkRecordCreateDependencies(
	ctx context.Context,
	tx transaction,
	accepted workrecords.CreateMutation,
) error {
	record := accepted.Record
	if record.ServiceID != "" {
		tag, err := tx.Exec(ctx, `
SELECT 1
FROM services
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND lifecycle_state = 'active'
  AND ($4 = 0 OR version = $4)
FOR SHARE
`, record.ServiceID, record.MSPID, record.ClientID,
			accepted.ExpectedServiceVersion)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return scope.ErrNotFound
		}
	}
	if record.ContractID != "" {
		tag, err := tx.Exec(ctx, `
SELECT 1
FROM contracts
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND lifecycle_state = 'active'
  AND starts_on <= $4::date
  AND (ends_on IS NULL OR ends_on >= $4::date)
  AND ($5 = 0 OR version = $5)
FOR SHARE
`, record.ContractID, record.MSPID, record.ClientID, record.CreatedAt,
			accepted.ExpectedContractVersion)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return scope.ErrNotFound
		}
	}
	queueID := accepted.Routing.Decision.QueueID
	tag, err := tx.Exec(ctx, `
SELECT 1
FROM queues
WHERE id = $1 AND msp_id = $2
  AND (client_id IS NULL OR client_id = $3::uuid)
FOR SHARE
`, queueID, record.MSPID, record.ClientID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return scope.ErrNotFound
	}
	return nil
}

func lockWorkRecordCreateConfiguration(
	ctx context.Context,
	tx transaction,
	accepted workrecords.CreateMutation,
) error {
	record := accepted.Record
	tag, err := tx.Exec(ctx, `
SELECT 1
FROM routing_rule_sets
WHERE id = $1 AND msp_id = $2 AND current_version = $3
FOR SHARE
`, accepted.Routing.RuleSetID, record.MSPID, accepted.Routing.RuleSetVersion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	tag, err = tx.Exec(ctx, `
SELECT 1
FROM routing_rule_set_versions AS set_version
JOIN routing_rule_versions AS rule
  ON rule.rule_set_id = set_version.rule_set_id
  AND rule.version = set_version.version
WHERE set_version.rule_set_id = $1 AND set_version.msp_id = $2
  AND set_version.version = $3 AND rule.rule_id = $4
  AND rule.queue_id = $5
  AND (rule.client_id IS NULL OR rule.client_id = $6::uuid)
FOR SHARE OF set_version, rule
`, accepted.Routing.RuleSetID, record.MSPID, accepted.Routing.RuleSetVersion,
		accepted.Routing.Decision.RuleID, accepted.Routing.Decision.QueueID,
		record.ClientID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	tag, err = tx.Exec(ctx, `
SELECT 1
FROM workflows
WHERE id = $1 AND msp_id = $2 AND current_version = $3 AND enabled
  AND (client_id IS NULL OR client_id = $4::uuid)
FOR SHARE
`, accepted.Workflow.WorkflowID, record.MSPID, accepted.Workflow.Version,
		record.ClientID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	tag, err = tx.Exec(ctx, `
SELECT 1
FROM workflow_versions
WHERE workflow_id = $1 AND msp_id = $2 AND version = $3
FOR SHARE
`, accepted.Workflow.WorkflowID, record.MSPID, accepted.Workflow.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	tag, err = tx.Exec(ctx, `
SELECT 1
FROM sla_policies
WHERE id = $1 AND msp_id = $2 AND version = $3 AND enabled
  AND calendar_id = $4
  AND (client_id IS NULL OR client_id = $5::uuid)
FOR SHARE
`, accepted.SLA.PolicyID, record.MSPID, accepted.SLA.PolicyVersion,
		accepted.SLA.CalendarID, record.ClientID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	tag, err = tx.Exec(ctx, `
SELECT 1
FROM sla_policy_versions
WHERE policy_id = $1 AND msp_id = $2 AND version = $3 AND enabled
  AND calendar_id = $4 AND calendar_version = $5
FOR SHARE
`, accepted.SLA.PolicyID, record.MSPID, accepted.SLA.PolicyVersion,
		accepted.SLA.CalendarID, accepted.SLA.CalendarVersion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	tag, err = tx.Exec(ctx, `
SELECT 1
FROM business_calendars
WHERE id = $1 AND msp_id = $2 AND version = $3
  AND (client_id IS NULL OR client_id = $4::uuid)
FOR SHARE
`, accepted.SLA.CalendarID, record.MSPID, accepted.SLA.CalendarVersion,
		record.ClientID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	tag, err = tx.Exec(ctx, `
SELECT 1
FROM business_calendar_versions
WHERE calendar_id = $1 AND msp_id = $2 AND version = $3
FOR SHARE
`, accepted.SLA.CalendarID, record.MSPID, accepted.SLA.CalendarVersion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	return nil
}

func workRecordWriteError(err error) error {
	var postgres *pgconn.PgError
	if errors.As(err, &postgres) && postgres.Code == "23505" &&
		postgres.ConstraintName == "work_records_msp_id_client_id_display_id_key" {
		return workrecords.ErrDisplayIDConflict
	}
	return err
}

package psa

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
)

type TaskRepository struct {
	db database
}

var _ tasks.Repository = (*TaskRepository)(nil)

func NewTaskRepository(db database) *TaskRepository {
	return &TaskRepository{db: db}
}

func (r *TaskRepository) Find(
	ctx context.Context,
	target scope.Target,
	id string,
) (tasks.Task, error) {
	return scanTask(r.db.QueryRow(ctx, taskSelect+`
WHERE id = $1 AND msp_id = $2 AND client_id = $3
`, id, target.MSPID, target.ClientID))
}

func (r *TaskRepository) ListForParent(
	ctx context.Context,
	target scope.Target,
	parent tasks.Ref,
	parentTaskID string,
) ([]tasks.Task, error) {
	found, err := r.db.Query(ctx, taskSelect+`
WHERE msp_id = $1 AND client_id = $2
  AND parent_type = $3 AND parent_id = $4
  AND parent_task_id IS NOT DISTINCT FROM NULLIF($5, '')::uuid
ORDER BY position, id
`, target.MSPID, target.ClientID, parent.Type, parent.ID, parentTaskID)
	if err != nil {
		return nil, err
	}
	defer found.Close()
	return scanTasks(found)
}

func (r *TaskRepository) CreateAtomic(
	ctx context.Context,
	accepted tasks.CreateMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	task := accepted.Task
	if err := lockActiveClient(
		ctx, tx, task.MSPID, task.ClientID,
	); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO tasks (
  id, msp_id, client_id, parent_type, parent_id, work_record_id,
  parent_task_id, title, status, position, owner_id, estimate_minutes,
  version, created_at, created_by, updated_at, updated_by
)
SELECT $1, $2, $3, $4, $5::uuid,
       CASE WHEN $4 = 'work_record' THEN $5::uuid ELSE NULL END,
       NULLIF($6, '')::uuid, $7, $8, $9,
       NULLIF($10, '')::uuid, $11,
       $12, $13, $14, $13, $14
WHERE (
  ($4 = 'work_record' AND EXISTS (
    SELECT 1 FROM work_records
    WHERE id = $5::uuid AND msp_id = $2 AND client_id = $3
      AND lifecycle_state = 'active' AND deleted_at IS NULL
  )) OR
  ($4 = 'opportunity' AND EXISTS (
    SELECT 1 FROM opportunities
    WHERE id = $5::uuid AND msp_id = $2 AND client_id = $3
      AND lifecycle_state = 'active'
  )) OR
  ($4 = 'project' AND EXISTS (
    SELECT 1 FROM projects
    WHERE id = $5::uuid AND msp_id = $2 AND client_id = $3
      AND lifecycle_state <> 'cancelled'
  )) OR
  ($4 = 'phase' AND EXISTS (
    SELECT 1 FROM phases
    WHERE id = $5::uuid AND msp_id = $2 AND client_id = $3
  ))
) AND (
  NULLIF($6, '')::uuid IS NULL OR EXISTS (
    SELECT 1 FROM tasks
    WHERE id = NULLIF($6, '')::uuid AND msp_id = $2 AND client_id = $3
      AND parent_type = $4 AND parent_id = $5::uuid
  )
) AND (
  NULLIF($10, '')::uuid IS NULL OR EXISTS (
    SELECT 1 FROM technicians
    WHERE id = NULLIF($10, '')::uuid AND msp_id = $2
      AND lifecycle_state = 'active'
  )
)
`, task.ID, task.MSPID, task.ClientID, task.Parent.Type, task.Parent.ID,
		task.ParentTaskID, task.Title, task.Status, task.Position,
		task.OwnerID, task.EstimateMinutes, task.Version,
		accepted.Audit.OccurredAt, task.CreatedBy)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return scope.ErrNotFound
	}
	if err := insertInitialTagAssignments(ctx, tx, tagging.TargetRef{MSPID: task.MSPID, ClientID: task.ClientID, ObjectType: tagging.ObjectTask, ObjectID: task.ID}, task.Version, accepted.InitialTags); err != nil {
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

func (r *TaskRepository) LoadSelected(
	ctx context.Context,
	parent tasks.Ref,
	selected []tasks.ID,
) ([]tasks.Task, error) {
	found, err := r.db.Query(ctx, taskSelect+`
WHERE msp_id = $1 AND client_id = $2
  AND parent_type = $3 AND parent_id = $4
  AND id = ANY($5::uuid[])
ORDER BY position, id
`, parent.MSPID, parent.ClientID, parent.Type, parent.ID, selected)
	if err != nil {
		return nil, err
	}
	defer found.Close()
	loaded, err := scanTasks(found)
	if err != nil {
		return nil, err
	}
	if len(loaded) != len(selected) {
		return nil, scope.ErrNotFound
	}
	return loaded, nil
}

func (r *TaskRepository) MoveAtomic(
	ctx context.Context,
	accepted tasks.MoveMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	for index, task := range accepted.Tasks {
		history := accepted.Histories[index]
		tag, err := tx.Exec(ctx, `
UPDATE tasks
SET parent_type = $5, parent_id = $6,
    work_record_id = CASE WHEN $5 = 'work_record' THEN $6 ELSE NULL END,
    version = $4, updated_at = $7, updated_by = $8
WHERE id = $1 AND msp_id = $2 AND client_id = $3 AND version = $4 - 1
`, task.ID, task.MSPID, task.ClientID, task.Version,
			task.Parent.Type, task.Parent.ID, history.MovedAt, history.MovedBy)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if tag.RowsAffected() != 1 {
			_ = tx.Rollback(ctx)
			return object.ErrVersionConflict
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO task_movement_history (
  id, task_id, msp_id, client_id, from_parent_type, from_parent_id,
  to_parent_type, to_parent_id, previous_version, accepted_version,
  moved_at, moved_by, correlation_id
) VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
`, task.ID, task.MSPID, task.ClientID,
			history.From.Type, history.From.ID, history.To.Type, history.To.ID,
			history.PreviousVersion, history.AcceptedVersion,
			history.MovedAt, history.MovedBy, accepted.Audit.CorrelationID); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
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

const taskSelect = `
SELECT id::text, msp_id::text, client_id::text, parent_type, parent_id::text,
       COALESCE(work_record_id::text, ''), COALESCE(parent_task_id::text, ''),
       title, status, position, version, created_by::text,
       COALESCE(owner_id::text, ''), COALESCE(estimate_minutes, 0),
       scheduled_starts_at, scheduled_ends_at, COALESCE(schedule_timezone,''),
       scheduling_mode, due_on, schedule_recurrence
FROM tasks
`

func scanTask(row row) (tasks.Task, error) {
	var task tasks.Task
	var recurrence []byte
	err := row.Scan(
		&task.ID, &task.MSPID, &task.ClientID, &task.Parent.Type, &task.Parent.ID,
		&task.WorkRecordID, &task.ParentTaskID, &task.Title, &task.Status,
		&task.Position, &task.Version, &task.CreatedBy, &task.OwnerID,
		&task.EstimateMinutes,
		&task.ScheduledStartsAt, &task.ScheduledEndsAt, &task.ScheduleTimezone,
		&task.SchedulingMode, &task.DueOn, &recurrence,
	)
	if err == nil && len(recurrence) > 0 {
		err = json.Unmarshal(recurrence, &task.ScheduleRecurrence)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return tasks.Task{}, scope.ErrNotFound
	}
	if err != nil {
		return tasks.Task{}, err
	}
	task.Parent.MSPID = task.MSPID
	task.Parent.ClientID = task.ClientID
	return task, nil
}

func scanTasks(rows rows) ([]tasks.Task, error) {
	result := make([]tasks.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

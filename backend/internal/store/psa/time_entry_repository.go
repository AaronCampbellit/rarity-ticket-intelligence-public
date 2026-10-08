package psa

import (
	"context"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/timeentries"
)

type TimeEntryRepository struct {
	db database
}

var _ timeentries.Repository = (*TimeEntryRepository)(nil)

func NewTimeEntryRepository(db database) *TimeEntryRepository {
	return &TimeEntryRepository{db: db}
}

func (r *TimeEntryRepository) CreateAtomic(
	ctx context.Context,
	accepted timeentries.CreateMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	entry := accepted.Entry
	tag, err := tx.Exec(ctx, `
INSERT INTO time_entries (
  id, msp_id, client_id, work_record_id, task_id, technician_id,
  started_at, ended_at, duration_seconds, billable, note,
  version, created_at, created_by
)
SELECT $1, $2, $3, NULLIF($4, '')::uuid, NULLIF($5, '')::uuid, $6,
       $7, $8, $9, $10, $11, $12, $13, $14
WHERE (
  (NULLIF($4, '')::uuid IS NOT NULL AND EXISTS (
    SELECT 1 FROM work_records
    WHERE id = NULLIF($4, '')::uuid AND msp_id = $2 AND client_id = $3
      AND deleted_at IS NULL
  )) OR
  (NULLIF($4, '')::uuid IS NULL AND EXISTS (
    SELECT 1 FROM tasks
    WHERE id = NULLIF($5, '')::uuid AND msp_id = $2 AND client_id = $3
      AND parent_type IN ('project', 'phase')
  ))
) AND EXISTS (
  SELECT 1 FROM technicians
  WHERE id = $6 AND msp_id = $2 AND lifecycle_state = 'active'
) AND (
  NULLIF($5, '')::uuid IS NULL OR EXISTS (
    SELECT 1 FROM tasks
    WHERE id = NULLIF($5, '')::uuid AND msp_id = $2 AND client_id = $3
      AND (
        (NULLIF($4, '')::uuid IS NOT NULL
          AND work_record_id = NULLIF($4, '')::uuid) OR
        (NULLIF($4, '')::uuid IS NULL
          AND parent_type IN ('project', 'phase'))
      )
  )
)
`, entry.ID, entry.MSPID, entry.ClientID, entry.WorkRecordID,
		entry.TaskID, entry.TechnicianID, entry.StartedAt, entry.EndedAt,
		entry.DurationSeconds, entry.Billable, entry.Note, entry.Version,
		entry.CreatedAt, entry.CreatedBy)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return scope.ErrNotFound
	}
	if err := insertInitialTagAssignments(ctx, tx, tagging.TargetRef{MSPID: entry.MSPID, ClientID: entry.ClientID, ObjectType: tagging.ObjectTimeEntry, ObjectID: entry.ID}, entry.Version, accepted.InitialTags); err != nil {
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

package psa

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/billingexport"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type BillingExportRepository struct {
	db database
}

var _ billingexport.Repository = (*BillingExportRepository)(nil)

func NewBillingExportRepository(db database) *BillingExportRepository {
	return &BillingExportRepository{db: db}
}

func (r *BillingExportRepository) ListApprovalEntries(
	ctx context.Context,
	target scope.Target,
	state billingexport.ApprovalState,
	limit int,
) ([]billingexport.ApprovalEntry, error) {
	rows, err := r.db.Query(ctx, `
SELECT entry.id::text, entry.work_record_id::text, entry.technician_id::text,
       entry.duration_seconds::bigint, entry.billable,
       entry.approval_state = 'approved', entry.version,
       entry.msp_id::text, entry.client_id::text, entry.approval_state,
       entry.approved_at, COALESCE(entry.approved_by::text, ''),
       entry.started_at, entry.ended_at, entry.note
FROM time_entries entry
WHERE entry.msp_id = $1 AND entry.client_id = $2::uuid
  AND entry.work_record_id IS NOT NULL
  AND entry.approval_state = $3
ORDER BY entry.started_at DESC, entry.id
LIMIT $4
`, target.MSPID, target.ClientID, state, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]billingexport.ApprovalEntry, 0)
	for rows.Next() {
		var entry billingexport.ApprovalEntry
		var rawState string
		if err := rows.Scan(
			&entry.ID, &entry.WorkRecordID, &entry.TechnicianID,
			&entry.DurationSeconds, &entry.Billable, &entry.Approved,
			&entry.Version, &entry.MSPID, &entry.ClientID, &rawState,
			&entry.ApprovedAt, &entry.ApprovedBy, &entry.StartedAt,
			&entry.EndedAt, &entry.Note,
		); err != nil {
			return nil, err
		}
		entry.ApprovalState = billingexport.ApprovalState(rawState)
		result = append(result, entry)
	}
	return result, rows.Err()
}

func (r *BillingExportRepository) LoadApprovalEntry(
	ctx context.Context,
	target scope.Target,
	entryID string,
) (billingexport.ApprovalEntry, error) {
	var entry billingexport.ApprovalEntry
	var state string
	err := r.db.QueryRow(ctx, `
SELECT entry.id::text, entry.work_record_id::text, entry.technician_id::text,
       entry.duration_seconds::bigint, entry.billable,
       entry.approval_state = 'approved', entry.version,
       entry.msp_id::text, entry.client_id::text, entry.approval_state,
       entry.approved_at, COALESCE(entry.approved_by::text, '')
FROM time_entries entry
WHERE entry.id = $1 AND entry.msp_id = $2
  AND entry.client_id = $3::uuid
  AND entry.work_record_id IS NOT NULL
`, entryID, target.MSPID, target.ClientID).Scan(
		&entry.ID, &entry.WorkRecordID, &entry.TechnicianID,
		&entry.DurationSeconds, &entry.Billable, &entry.Approved, &entry.Version,
		&entry.MSPID, &entry.ClientID, &state,
		&entry.ApprovedAt, &entry.ApprovedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return billingexport.ApprovalEntry{}, scope.ErrNotFound
	}
	if err != nil {
		return billingexport.ApprovalEntry{}, err
	}
	entry.ApprovalState = billingexport.ApprovalState(state)
	return entry, nil
}

func (r *BillingExportRepository) DecideApprovalAtomic(
	ctx context.Context,
	accepted billingexport.ApprovalMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	entry := accepted.Entry
	tag, err := tx.Exec(ctx, `
UPDATE time_entries
SET approval_state = $5, approved_at = $6, approved_by = NULLIF($7, '')::uuid,
    version = version + 1
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND version = $4
`, entry.ID, entry.MSPID, entry.ClientID, accepted.ExpectedVersion,
		entry.ApprovalState, entry.ApprovedAt, entry.ApprovedBy)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return object.ErrVersionConflict
	}
	decision := accepted.Decision
	if _, err := tx.Exec(ctx, `
INSERT INTO time_entry_approval_decisions (
  id, time_entry_id, msp_id, client_id, time_entry_version,
  decision, reason, decided_at, decided_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
`, decision.ID, decision.EntryID, decision.MSPID, decision.ClientID,
		decision.Version, decision.Decision, decision.Reason,
		decision.DecidedAt, decision.DecidedBy); err != nil {
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

func (r *BillingExportRepository) ListEntries(
	ctx context.Context,
	target scope.Target,
	from time.Time,
	through time.Time,
	limit int,
) ([]billingexport.Entry, error) {
	rows, err := r.db.Query(ctx, `
SELECT entry.id::text, entry.work_record_id::text, entry.technician_id::text,
       entry.duration_seconds::bigint, entry.billable,
       entry.approval_state = 'approved' AS approved, entry.version
FROM time_entries entry
WHERE entry.msp_id = $1
  AND entry.client_id = $2::uuid
  AND entry.work_record_id IS NOT NULL
  AND entry.started_at >= $3
  AND entry.started_at < $4
ORDER BY entry.started_at, entry.id
LIMIT $5
`, target.MSPID, target.ClientID, from, through, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]billingexport.Entry, 0)
	for rows.Next() {
		var entry billingexport.Entry
		if err := rows.Scan(
			&entry.ID, &entry.WorkRecordID, &entry.TechnicianID,
			&entry.DurationSeconds, &entry.Billable, &entry.Approved,
			&entry.Version,
		); err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, rows.Err()
}

func (r *BillingExportRepository) RecordExportAtomic(
	ctx context.Context,
	accepted billingexport.ExportMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	export := accepted.Export
	if _, err := tx.Exec(ctx, `
INSERT INTO billing_exports (
  id, msp_id, client_id, from_at, through_at, entry_count,
  csv_sha256, created_at, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
`, export.ID, export.MSPID, export.ClientID, export.From, export.Through,
		export.EntryCount, export.SHA256[:], export.CreatedAt,
		export.CreatedBy); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	for _, entry := range accepted.Entries {
		if _, err := tx.Exec(ctx, `
INSERT INTO billing_export_entries (
  export_id, msp_id, client_id, time_entry_id, time_entry_version,
  work_record_id, technician_id, duration_seconds, billable, approved
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
`, export.ID, export.MSPID, export.ClientID, entry.ID, entry.Version,
			entry.WorkRecordID, entry.TechnicianID, entry.DurationSeconds,
			entry.Billable, entry.Approved); err != nil {
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

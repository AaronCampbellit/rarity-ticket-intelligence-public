package psa

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/attachments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type AttachmentRepository struct {
	db database
}

var _ attachments.Repository = (*AttachmentRepository)(nil)

func NewAttachmentRepository(db database) *AttachmentRepository {
	return &AttachmentRepository{db: db}
}

func (r *AttachmentRepository) CreateAtomic(
	ctx context.Context,
	accepted attachments.UploadMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	attachment := accepted.Attachment
	tag, err := tx.Exec(ctx, `
INSERT INTO attachments (
  id, msp_id, client_id, work_record_id, opportunity_id, comment_id, storage_key,
  filename, content_type, size_bytes, sha256, uploaded_by, created_at, version
)
SELECT $1, $2, $3, NULLIF($4, '')::uuid, NULLIF($5, '')::uuid,
       NULL, $6, $7, $8, $9, $10, $11, $12, $13
WHERE (
  (NULLIF($4, '')::uuid IS NOT NULL AND NULLIF($5, '')::uuid IS NULL AND EXISTS (
  SELECT 1 FROM work_records
  WHERE id = $4 AND msp_id = $2 AND client_id = $3
    AND lifecycle_state = 'active' AND deleted_at IS NULL
  ))
  OR
  (NULLIF($5, '')::uuid IS NOT NULL AND NULLIF($4, '')::uuid IS NULL AND EXISTS (
    SELECT 1 FROM opportunities
    WHERE id = $5 AND msp_id = $2 AND client_id = $3
      AND lifecycle_state = 'active'
  ))
) AND EXISTS (
  SELECT 1 FROM technicians
  WHERE id = $11 AND msp_id = $2 AND lifecycle_state = 'active'
)
`, attachment.ID, attachment.MSPID, attachment.ClientID,
		attachment.WorkRecordID, attachment.OpportunityID, attachment.StorageKey,
		attachment.Filename, attachment.ContentType, attachment.SizeBytes,
		attachment.SHA256[:], attachment.UploadedBy, attachment.CreatedAt,
		attachment.Version)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return scope.ErrNotFound
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

func (r *AttachmentRepository) ListOpportunity(
	ctx context.Context,
	target scope.Target,
	opportunityID string,
	limit int,
) ([]attachments.Attachment, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM opportunities
  WHERE id = $1 AND msp_id = $2 AND client_id = $3
    AND lifecycle_state = 'active'
)
`, opportunityID, target.MSPID, target.ClientID).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !exists) {
		return nil, scope.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, client_id::text, opportunity_id::text,
       filename, content_type, size_bytes, sha256, version,
       uploaded_by::text, created_at
FROM attachments
WHERE opportunity_id = $1 AND msp_id = $2 AND client_id = $3
ORDER BY created_at DESC, id DESC
LIMIT $4
`, opportunityID, target.MSPID, target.ClientID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := make([]attachments.Attachment, 0)
	for rows.Next() {
		var attachment attachments.Attachment
		var digest []byte
		if err := rows.Scan(
			&attachment.ID, &attachment.MSPID, &attachment.ClientID,
			&attachment.OpportunityID, &attachment.Filename,
			&attachment.ContentType, &attachment.SizeBytes, &digest,
			&attachment.Version, &attachment.UploadedBy, &attachment.CreatedAt,
		); err != nil {
			return nil, err
		}
		if len(digest) != len(attachment.SHA256) {
			return nil, attachments.ErrRejected
		}
		copy(attachment.SHA256[:], digest)
		found = append(found, attachment)
	}
	return found, rows.Err()
}

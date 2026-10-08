package psa

import (
	"context"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/comments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/timeentries"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

type CommentRepository struct {
	db database
}

var _ comments.Repository = (*CommentRepository)(nil)
var _ comments.CaptureRepository = (*CommentRepository)(nil)

func NewCommentRepository(db database) *CommentRepository {
	return &CommentRepository{db: db}
}

func (r *CommentRepository) FindAppliedSLA(
	ctx context.Context,
	target scope.Target,
	workRecordID string,
) (workrecords.AppliedSLA, error) {
	return NewWorkRecordRepository(r.db).FindAppliedSLA(ctx, target, workRecordID)
}

func (r *CommentRepository) CreateAtomic(
	ctx context.Context,
	accepted comments.CreateMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if err := createCommentInTransaction(ctx, tx, accepted); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *CommentRepository) CreateWithCaptureAtomic(
	ctx context.Context,
	comment comments.CreateMutation,
	capture timeentries.CaptureMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if err := createCommentInTransaction(ctx, tx, comment); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := createFromCaptureInTransaction(ctx, tx, capture); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func createCommentInTransaction(
	ctx context.Context,
	tx transaction,
	accepted comments.CreateMutation,
) error {
	comment := accepted.Comment
	if err := lockActiveClient(
		ctx, tx, comment.MSPID, comment.ClientID,
	); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO comments (
  id, msp_id, client_id, work_record_id, author_id,
  visibility, body, created_at, version
)
SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9
WHERE EXISTS (
  SELECT 1 FROM work_records
  WHERE id = $4 AND msp_id = $2 AND client_id = $3 AND deleted_at IS NULL
) AND EXISTS (
  SELECT 1 FROM technicians
  WHERE id = $5 AND msp_id = $2 AND lifecycle_state = 'active'
)
`, comment.ID, comment.MSPID, comment.ClientID, comment.WorkRecordID,
		comment.AuthorID, comment.Visibility, comment.Body, comment.CreatedAt,
		comment.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return scope.ErrNotFound
	}
	if accepted.SLAChanged {
		slaRecord := accepted.SLA
		tag, err := tx.Exec(ctx, `
UPDATE work_record_slas
SET response_warning_at = $6, response_due_at = $7,
    responded_at = $8, response_state = $9, version = version + 1
WHERE id = $1 AND work_record_id = $2 AND msp_id = $3 AND client_id = $4
  AND version = $5 AND responded_at IS NULL
`, slaRecord.ID, comment.WorkRecordID, comment.MSPID, comment.ClientID,
			slaRecord.Version-1, slaRecord.ResponseWarningAt,
			slaRecord.ResponseDueAt, slaRecord.RespondedAt,
			slaRecord.ResponseState)
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
		if err := writeMutationFacts(ctx, tx, accepted.SLAAudit, accepted.SLAEvent); err != nil {
			return err
		}
	}
	return nil
}

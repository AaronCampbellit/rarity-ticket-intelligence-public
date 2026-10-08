package psa

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
)

type WorkflowRepository struct {
	db database
}

var _ workflow.ManagementRepository = (*WorkflowRepository)(nil)

func NewWorkflowRepository(db database) *WorkflowRepository {
	return &WorkflowRepository{db: db}
}

func (r *WorkflowRepository) ListPublished(
	ctx context.Context,
	target scope.Target,
) ([]workflow.Published, error) {
	rows, err := r.db.Query(ctx, `
SELECT w.id::text, w.msp_id::text, COALESCE(w.client_id::text, ''),
       w.key, w.name, w.current_version, w.priority, w.stable_order,
       w.fallback, w.enabled, w.effective_from, w.effective_to,
       w.conditions, v.definition
FROM workflows w
JOIN workflow_versions v
  ON v.workflow_id = w.id AND v.version = w.current_version
WHERE w.msp_id = $1
  AND (
    ($2 = '' AND w.client_id IS NULL)
    OR ($2 <> '' AND (w.client_id IS NULL OR w.client_id = $2::uuid))
  )
ORDER BY w.priority DESC, w.stable_order, w.id
`, target.MSPID, target.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]workflow.Published, 0)
	for rows.Next() {
		var (
			published                  workflow.Published
			effectiveFrom, effectiveTo *time.Time
			conditions, definition     []byte
		)
		if err := rows.Scan(
			&published.ID, &published.MSPID, &published.ClientID,
			&published.Key, &published.Name, &published.Version,
			&published.Priority, &published.StableOrder,
			&published.Fallback, &published.Enabled,
			&effectiveFrom, &effectiveTo, &conditions, &definition,
		); err != nil {
			return nil, err
		}
		if effectiveFrom != nil {
			published.EffectiveFrom = *effectiveFrom
		}
		published.EffectiveTo = effectiveTo
		if err := json.Unmarshal(conditions, &published.Conditions); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(definition, &published.Definition); err != nil {
			return nil, err
		}
		result = append(result, published)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *WorkflowRepository) PublishAtomic(
	ctx context.Context,
	accepted workflow.PublishMutation,
) error {
	conditions, err := json.Marshal(accepted.Workflow.Conditions)
	if err != nil {
		return err
	}
	definition, err := json.Marshal(accepted.Workflow.Definition)
	if err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	published := accepted.Workflow
	if accepted.Created {
		_, err = tx.Exec(ctx, `
INSERT INTO workflows (
  id, msp_id, client_id, key, name, enabled, priority, stable_order,
  fallback, effective_from, effective_to, conditions, current_version,
  created_at, created_by, updated_at, updated_by
) VALUES (
  $1, $2, NULLIF($3, '')::uuid, $4, $5, $6, $7, $8,
  $9, $10, $11,
  $12, $13, $14, $15, $14, $15
)
`, published.ID, published.MSPID, published.ClientID,
			published.Key, published.Name, published.Enabled,
			published.Priority, published.StableOrder, published.Fallback,
			nullableTime(published.EffectiveFrom), published.EffectiveTo, conditions,
			published.Version, accepted.PublishedAt, accepted.PublishedBy)
	} else {
		var tag pgconn.CommandTag
		tag, err = tx.Exec(ctx, `
UPDATE workflows
SET key = $5, name = $6, enabled = $7, priority = $8,
    stable_order = $9, fallback = $10, effective_from = $11,
    effective_to = $12, conditions = $13, current_version = $14,
    updated_at = $15, updated_by = $16
WHERE id = $1 AND msp_id = $2
  AND client_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
  AND current_version = $4
`, published.ID, published.MSPID, published.ClientID,
			accepted.ExpectedVersion, published.Key, published.Name,
			published.Enabled, published.Priority, published.StableOrder,
			published.Fallback, nullableTime(published.EffectiveFrom),
			published.EffectiveTo, conditions, published.Version,
			accepted.PublishedAt, accepted.PublishedBy)
		if err == nil && tag.RowsAffected() != 1 {
			err = object.ErrVersionConflict
		}
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO workflow_versions (
  workflow_id, msp_id, version, definition, published_at, published_by
) VALUES ($1, $2, $3, $4, $5, $6)
`, published.ID, published.MSPID, published.Version,
		definition, accepted.PublishedAt, accepted.PublishedBy); err != nil {
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

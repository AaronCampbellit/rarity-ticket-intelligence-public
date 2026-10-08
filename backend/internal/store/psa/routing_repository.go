package psa

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/routing"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type RoutingRepository struct {
	db database
}

var _ routing.ManagementRepository = (*RoutingRepository)(nil)

func NewRoutingRepository(db database) *RoutingRepository {
	return &RoutingRepository{db: db}
}

func (r *RoutingRepository) LoadCurrent(
	ctx context.Context,
	mspID string,
) (routing.RuleSet, error) {
	var result routing.RuleSet
	err := r.db.QueryRow(ctx, `
SELECT rs.id::text, rs.msp_id::text, rs.current_version,
       v.published_at, v.published_by::text
FROM routing_rule_sets rs
JOIN routing_rule_set_versions v
  ON v.rule_set_id = rs.id AND v.version = rs.current_version
WHERE rs.msp_id = $1
`, mspID).Scan(
		&result.ID, &result.MSPID, &result.Version,
		&result.PublishedAt, &result.PublishedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return routing.RuleSet{}, scope.ErrNotFound
	}
	if err != nil {
		return routing.RuleSet{}, err
	}
	rows, err := r.db.Query(ctx, `
SELECT rule_id::text, position, COALESCE(client_id::text, ''),
       COALESCE(record_type, ''), COALESCE(priority, ''), queue_id::text
FROM routing_rule_versions
WHERE rule_set_id = $1 AND version = $2
ORDER BY position
`, result.ID, result.Version)
	if err != nil {
		return routing.RuleSet{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var rule routing.Rule
		if err := rows.Scan(
			&rule.ID, &rule.Position, &rule.ClientID,
			&rule.RecordType, &rule.Priority, &rule.QueueID,
		); err != nil {
			return routing.RuleSet{}, err
		}
		result.Rules = append(result.Rules, rule)
	}
	if err := rows.Err(); err != nil {
		return routing.RuleSet{}, err
	}
	return result, nil
}

func (r *RoutingRepository) ValidateDestinations(
	ctx context.Context,
	target scope.Target,
	rules []routing.Rule,
) error {
	payload, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	var valid int
	err = r.db.QueryRow(ctx, `
WITH requested AS (
  SELECT COALESCE(client_id, '') AS client_id, queue_id
  FROM jsonb_to_recordset($2::jsonb)
    AS item(client_id text, queue_id text)
)
SELECT count(*)::integer
FROM requested
JOIN queues q
  ON q.id = requested.queue_id::uuid
 AND q.msp_id = $1
 AND (
   (requested.client_id = '' AND q.client_id IS NULL)
   OR (
     requested.client_id <> ''
     AND (q.client_id IS NULL OR q.client_id = requested.client_id::uuid)
   )
 )
`, target.MSPID, payload).Scan(&valid)
	if err != nil {
		return err
	}
	if valid != len(rules) {
		return scope.ErrNotFound
	}
	return nil
}

func (r *RoutingRepository) PublishAtomic(
	ctx context.Context,
	accepted routing.PublishMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	ruleSet := accepted.RuleSet
	if accepted.Created {
		if _, err := tx.Exec(ctx, `
INSERT INTO routing_rule_sets (
  id, msp_id, current_version, created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, $3, $4, $5, $4, $5)
`, ruleSet.ID, ruleSet.MSPID, ruleSet.Version,
			ruleSet.PublishedAt, ruleSet.PublishedBy); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
	} else {
		tag, err := tx.Exec(ctx, `
UPDATE routing_rule_sets
SET current_version = $3, updated_at = $4, updated_by = $5
WHERE id = $1 AND msp_id = $2 AND current_version = $6
`, ruleSet.ID, ruleSet.MSPID, ruleSet.Version,
			ruleSet.PublishedAt, ruleSet.PublishedBy, accepted.ExpectedVersion)
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
INSERT INTO routing_rule_set_versions (
  rule_set_id, msp_id, version, published_at, published_by
) VALUES ($1, $2, $3, $4, $5)
`, ruleSet.ID, ruleSet.MSPID, ruleSet.Version,
		ruleSet.PublishedAt, ruleSet.PublishedBy); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	for _, rule := range ruleSet.Rules {
		if _, err := tx.Exec(ctx, `
INSERT INTO routing_rule_versions (
  rule_set_id, msp_id, version, rule_id, position,
  client_id, record_type, priority, queue_id
) VALUES (
  $1, $2, $3, $4, $5, NULLIF($6, '')::uuid,
  NULLIF($7, ''), NULLIF($8, ''), $9
)
`, ruleSet.ID, ruleSet.MSPID, ruleSet.Version, rule.ID, rule.Position,
			rule.ClientID, rule.RecordType, rule.Priority, rule.QueueID); err != nil {
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

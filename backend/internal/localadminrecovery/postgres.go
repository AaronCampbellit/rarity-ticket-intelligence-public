package localadminrecovery

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) ResetPasswordAtomic(ctx context.Context, accepted Mutation) (Result, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
SELECT organization.id::text, organization.display_id, account.id::text,
       account.technician_id::text, account.username, account.version
FROM msp_organizations organization
JOIN break_glass_accounts account ON account.msp_id = organization.id
WHERE lower(organization.display_id) = lower($1)
  AND account.username = $2
  AND organization.lifecycle_state = 'active'
  AND account.enabled
ORDER BY organization.id
LIMIT 2
FOR UPDATE OF account
`, accepted.MSPDisplayID, accepted.Username)
	if err != nil {
		return Result{}, err
	}
	type target struct {
		mspID, mspDisplayID, accountID, technicianID, username string
		version                                                int64
	}
	targets := make([]target, 0, 2)
	for rows.Next() {
		var value target
		if err := rows.Scan(&value.mspID, &value.mspDisplayID, &value.accountID,
			&value.technicianID, &value.username, &value.version); err != nil {
			rows.Close()
			return Result{}, err
		}
		targets = append(targets, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Result{}, err
	}
	rows.Close()
	if len(targets) != 1 {
		return Result{}, ErrTargetUnavailable
	}
	selected := targets[0]
	newVersion := selected.version + 1
	tag, err := tx.Exec(ctx, `
UPDATE break_glass_accounts
SET password_hash = $2, updated_at = $3, version = $4
WHERE id = $1 AND version = $5 AND enabled
`, selected.accountID, accepted.PasswordHash, accepted.OccurredAt, newVersion, selected.version)
	if err != nil {
		return Result{}, err
	}
	if tag.RowsAffected() != 1 {
		return Result{}, ErrTargetUnavailable
	}
	sessionTag, err := tx.Exec(ctx, `
UPDATE sessions
SET revoked_at = $3, revoked_by = $4
WHERE msp_id = $1 AND technician_id = $2 AND revoked_at IS NULL
`, selected.mspID, selected.technicianID, accepted.OccurredAt, accepted.ActorID)
	if err != nil {
		return Result{}, err
	}
	safeFacts, err := json.Marshal(map[string]any{
		"recovery":         true,
		"sessions_revoked": sessionTag.RowsAffected(),
	})
	if err != nil {
		return Result{}, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_ledger (
  id, occurred_at, msp_id, actor_type, actor_id, action, subject_type,
  subject_id, subject_version, source, reason, correlation_id, safe_diff
) VALUES ($1,$2,$3,$4,$5,$6,'local_administrator',$7,$8,$9,$10,$11,$12)
`, accepted.AuditID, accepted.OccurredAt, selected.mspID, accepted.ActorType,
		accepted.ActorID, accepted.Action, selected.accountID, newVersion,
		accepted.Source, accepted.Reason, accepted.CorrelationID, safeFacts); err != nil {
		return Result{}, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO event_outbox (
  event_id, event_type, schema_version, occurred_at, msp_id, actor_type,
  actor_id, subject_type, subject_id, subject_version, correlation_id,
  source, data
) VALUES ($1,$2,1,$3,$4,$5,$6,'local_administrator',$7,$8,$9,$10,$11)
`, accepted.EventID, accepted.Action, accepted.OccurredAt, selected.mspID,
		accepted.ActorType, accepted.ActorID, selected.accountID, newVersion,
		accepted.CorrelationID, accepted.Source, safeFacts); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{
		MSPDisplayID:    selected.mspDisplayID,
		Username:        selected.username,
		Version:         newVersion,
		SessionsRevoked: sessionTag.RowsAffected(),
	}, nil
}

var _ Repository = (*PostgresRepository)(nil)

package authn

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/auditlog"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type AuditRepository struct {
	pool *pgxpool.Pool
}

var _ auditlog.Repository = (*AuditRepository)(nil)

func NewAuditRepository(pool *pgxpool.Pool) *AuditRepository {
	return &AuditRepository{pool: pool}
}

func (r *AuditRepository) List(
	ctx context.Context,
	target scope.Target,
	limit int,
) ([]auditlog.Entry, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id::text, occurred_at, COALESCE(client_id::text, ''),
       actor_type, actor_id::text, action, subject_type, subject_id::text,
       subject_version, source, COALESCE(reason, ''), correlation_id::text
FROM audit_ledger
WHERE msp_id = $1
  AND (NULLIF($2, '')::uuid IS NULL OR client_id = NULLIF($2, '')::uuid)
ORDER BY occurred_at DESC, id DESC
LIMIT $3
`, target.MSPID, target.ClientID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]auditlog.Entry, 0, limit)
	for rows.Next() {
		var entry auditlog.Entry
		if err := rows.Scan(
			&entry.ID, &entry.OccurredAt, &entry.ClientID,
			&entry.ActorType, &entry.ActorID, &entry.Action,
			&entry.SubjectType, &entry.SubjectID, &entry.SubjectVersion,
			&entry.Source, &entry.Reason, &entry.CorrelationID,
		); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

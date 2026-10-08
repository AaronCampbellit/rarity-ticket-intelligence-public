package authn

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sessions"
)

type SessionStore struct {
	pool *pgxpool.Pool
}

var _ sessions.Store = (*SessionStore)(nil)

func NewSessionStore(pool *pgxpool.Pool) *SessionStore {
	return &SessionStore{pool: pool}
}

func (s *SessionStore) Create(ctx context.Context, record sessions.Record) error {
	_, err := s.pool.Exec(ctx, `
INSERT INTO sessions (
  id, msp_id, technician_id, token_hash, created_at, last_seen_at,
  idle_expires_at, idle_timeout_seconds, expires_at, user_agent_hash, ip_prefix,
  rotated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
`, record.ID, record.MSPID, record.TechnicianID, record.TokenHash[:],
		record.CreatedAt, record.LastSeenAt, record.IdleExpiresAt,
		int64(record.IdleTimeout/time.Second), record.ExpiresAt,
		nullableHash(record.UserAgentHash), nullableAuthText(record.IPPrefix),
		record.RotatedAt)
	return err
}

func nullableHash(hash [32]byte) any {
	if hash == ([32]byte{}) {
		return nil
	}
	return hash[:]
}

func (s *SessionStore) FindByTokenHash(
	ctx context.Context,
	hash [32]byte,
) (sessions.Record, error) {
	var record sessions.Record
	var digest []byte
	var idleTimeoutSeconds int64
	err := s.pool.QueryRow(ctx, `
SELECT id::text, msp_id::text, technician_id::text, token_hash,
       created_at, rotated_at, last_seen_at, idle_expires_at, idle_timeout_seconds,
       expires_at, revoked_at
FROM sessions
WHERE token_hash = $1
`, hash[:]).Scan(
		&record.ID, &record.MSPID, &record.TechnicianID, &digest,
		&record.CreatedAt, &record.RotatedAt, &record.LastSeenAt, &record.IdleExpiresAt,
		&idleTimeoutSeconds, &record.ExpiresAt, &record.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && len(digest) != len(record.TokenHash) {
		return sessions.Record{}, sessions.ErrInvalidSession
	}
	if err != nil {
		return sessions.Record{}, err
	}
	copy(record.TokenHash[:], digest)
	record.IdleTimeout = time.Duration(idleTimeoutSeconds) * time.Second
	return record, nil
}

func (s *SessionStore) Rotate(
	ctx context.Context,
	oldHash [32]byte,
	newHash [32]byte,
	rotatedAt time.Time,
	idleExpiresAt time.Time,
) error {
	tag, err := s.pool.Exec(ctx, `
UPDATE sessions
SET token_hash = $2, rotated_at = $3, last_seen_at = $3,
    idle_expires_at = $4
WHERE token_hash = $1 AND revoked_at IS NULL
  AND expires_at > $3 AND idle_expires_at > $3
`, oldHash[:], newHash[:], rotatedAt, idleExpiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return sessions.ErrInvalidSession
	}
	return nil
}

func (s *SessionStore) Touch(
	ctx context.Context,
	hash [32]byte,
	lastSeenAt time.Time,
	idleExpiresAt time.Time,
) error {
	tag, err := s.pool.Exec(ctx, `
UPDATE sessions
SET last_seen_at = $2, idle_expires_at = $3
WHERE token_hash = $1 AND revoked_at IS NULL
  AND expires_at > $2 AND idle_expires_at > $2
`, hash[:], lastSeenAt, idleExpiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return sessions.ErrInvalidSession
	}
	return nil
}

func (s *SessionStore) RevokeByTokenHash(
	ctx context.Context,
	hash [32]byte,
	revokedAt time.Time,
	revokedBy string,
) error {
	tag, err := s.pool.Exec(ctx, `
UPDATE sessions
SET revoked_at = $2, revoked_by = $3
WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > $2
`, hash[:], revokedAt, revokedBy)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return sessions.ErrInvalidSession
	}
	return nil
}

func (s *SessionStore) ListActive(
	ctx context.Context,
	mspID string,
	technicianID string,
	now time.Time,
) ([]sessions.Record, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id::text, msp_id::text, technician_id::text, created_at, rotated_at,
       last_seen_at, idle_expires_at, idle_timeout_seconds, expires_at,
       token_hash, user_agent_hash, COALESCE(ip_prefix::text, '')
FROM sessions
WHERE msp_id = $1 AND technician_id = $2
  AND revoked_at IS NULL AND expires_at > $3 AND idle_expires_at > $3
ORDER BY last_seen_at DESC, id DESC
`, mspID, technicianID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []sessions.Record
	for rows.Next() {
		var record sessions.Record
		var idleSeconds int64
		var tokenHash []byte
		var userAgentHash []byte
		if err := rows.Scan(
			&record.ID, &record.MSPID, &record.TechnicianID,
			&record.CreatedAt, &record.RotatedAt, &record.LastSeenAt, &record.IdleExpiresAt,
			&idleSeconds, &record.ExpiresAt, &tokenHash,
			&userAgentHash, &record.IPPrefix,
		); err != nil {
			return nil, err
		}
		record.IdleTimeout = time.Duration(idleSeconds) * time.Second
		if len(tokenHash) == len(record.TokenHash) {
			copy(record.TokenHash[:], tokenHash)
		}
		if len(userAgentHash) == len(record.UserAgentHash) {
			copy(record.UserAgentHash[:], userAgentHash)
		}
		found = append(found, record)
	}
	return found, rows.Err()
}

func (s *SessionStore) RevokeByID(
	ctx context.Context,
	mspID string,
	technicianID string,
	sessionID string,
	revokedAt time.Time,
	revokedBy string,
) error {
	tag, err := s.pool.Exec(ctx, `
UPDATE sessions
SET revoked_at = $5, revoked_by = $6
WHERE id = $1 AND msp_id = $2 AND technician_id = $3
  AND revoked_at IS NULL AND expires_at > $4
`, sessionID, mspID, technicianID, revokedAt, revokedAt, revokedBy)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return sessions.ErrInvalidSession
	}
	return nil
}

type PrincipalStore struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

var _ sessions.PrincipalLoader = (*PrincipalStore)(nil)

func NewPrincipalStore(pool *pgxpool.Pool, now func() time.Time) *PrincipalStore {
	return &PrincipalStore{pool: pool, now: now}
}

func (s *PrincipalStore) LoadPrincipal(
	ctx context.Context,
	authenticated sessions.Authenticated,
	clientID string,
) (authorization.Principal, error) {
	if clientID != "" {
		var exists bool
		if err := s.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM client_organizations
  WHERE id = $1 AND msp_id = $2 AND lifecycle_state = 'active'
)
`, clientID, authenticated.MSPID).Scan(&exists); err != nil || !exists {
			return authorization.Principal{}, sessions.ErrInvalidSession
		}
	}
	rows, err := s.pool.Query(ctx, `
SELECT DISTINCT rc.capability
FROM technicians t
JOIN role_assignments ra
  ON ra.technician_id = t.id AND ra.msp_id = t.msp_id
JOIN role_capabilities rc
  ON rc.role_id = ra.role_id AND rc.msp_id = ra.msp_id
WHERE t.id = $1 AND t.msp_id = $2 AND t.lifecycle_state = 'active'
  AND (ra.expires_at IS NULL OR ra.expires_at > $3)
  AND (
    ($4 = '' AND ra.client_id IS NULL)
    OR ($4 <> '' AND (ra.client_id IS NULL OR ra.client_id = $4::uuid))
  )
`, authenticated.TechnicianID, authenticated.MSPID, s.now().UTC(), clientID)
	if err != nil {
		return authorization.Principal{}, err
	}
	defer rows.Close()
	var capabilities []string
	for rows.Next() {
		var capability string
		if err := rows.Scan(&capability); err != nil {
			return authorization.Principal{}, err
		}
		capabilities = append(capabilities, capability)
	}
	if err := rows.Err(); err != nil {
		return authorization.Principal{}, err
	}
	return authorization.Principal{
		ID: authenticated.TechnicianID,
		Scope: scope.Principal{
			MSPID: authenticated.MSPID, ClientID: clientID,
		},
		Capabilities: authorization.NewCapabilitySet(capabilities...),
	}, nil
}

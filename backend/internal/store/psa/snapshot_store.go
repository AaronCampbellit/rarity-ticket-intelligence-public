package psa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
)

var ErrInvalidSnapshot = errors.New("invalid proposal snapshot")

type SnapshotStore struct {
	db    database
	now   func() time.Time
	newID func() string
}

var _ sales.SnapshotStore = (*SnapshotStore)(nil)

func NewSnapshotStore(db database, now func() time.Time, newID func() string) *SnapshotStore {
	return &SnapshotStore{db: db, now: now, newID: newID}
}

func (s *SnapshotStore) PutImmutable(
	ctx context.Context,
	input sales.SnapshotInput,
) (sales.PDFSnapshot, error) {
	if input.ProposalVersionID == "" || len(input.Data) == 0 {
		return sales.PDFSnapshot{}, ErrInvalidSnapshot
	}
	sum := sha256.Sum256(input.Data)
	hash := hex.EncodeToString(sum[:])
	var snapshot sales.PDFSnapshot
	err := s.db.QueryRow(ctx, `
WITH inserted AS (
  INSERT INTO proposal_pdf_snapshots (
    id, proposal_version_id, sha256, content, stored_at
  ) VALUES ($1, $2, $3, $4, $5)
  ON CONFLICT DO NOTHING
  RETURNING id::text, proposal_version_id::text, sha256, stored_at
)
SELECT id, proposal_version_id, sha256, stored_at FROM inserted
UNION ALL
SELECT id::text, proposal_version_id::text, sha256, stored_at
FROM proposal_pdf_snapshots
WHERE proposal_version_id = $2 AND sha256 = $3
LIMIT 1
`, s.newID(), input.ProposalVersionID, hash, input.Data, s.now().UTC()).Scan(
		&snapshot.ID, &snapshot.ProposalVersionID, &snapshot.SHA256, &snapshot.StoredAt,
	)
	if err != nil {
		return sales.PDFSnapshot{}, err
	}
	return snapshot, nil
}

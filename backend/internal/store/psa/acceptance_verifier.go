package psa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type AcceptanceVerifier struct {
	db  database
	now func() time.Time
}

var _ sales.ElectronicAcceptanceVerifier = (*AcceptanceVerifier)(nil)

func NewAcceptanceVerifier(db database, now func() time.Time) *AcceptanceVerifier {
	return &AcceptanceVerifier{db: db, now: now}
}

func (v *AcceptanceVerifier) VerifyAndConsume(
	ctx context.Context,
	target scope.Target,
	proposalVersionID string,
	grant string,
) (sales.VerifiedElectronicAcceptance, error) {
	if target.MSPID == "" ||
		strings.TrimSpace(proposalVersionID) == "" ||
		strings.TrimSpace(grant) == "" {
		return sales.VerifiedElectronicAcceptance{}, sales.ErrInvalidAcceptanceGrant
	}
	sum := sha256.Sum256([]byte(grant))
	tokenHash := hex.EncodeToString(sum[:])
	acceptedAt := v.now().UTC()
	var verified sales.VerifiedElectronicAcceptance
	var evidence []byte
	err := v.db.QueryRow(ctx, `
UPDATE proposal_acceptance_grants
SET consumed_at = $5
WHERE token_sha256 = $1 AND proposal_version_id = $2 AND msp_id = $3
  AND (
    client_id = NULLIF($4, '')::uuid
    OR (NULLIF($4, '')::uuid IS NULL AND client_id IS NULL)
  )
  AND consumed_at IS NULL AND expires_at > $5
RETURNING id::text, signer_name, signer_email, evidence, consumed_at
`, tokenHash, proposalVersionID, target.MSPID, target.ClientID, acceptedAt).Scan(
		&verified.GrantID, &verified.SignerName, &verified.SignerEmail,
		&evidence, &verified.AcceptedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return sales.VerifiedElectronicAcceptance{}, sales.ErrInvalidAcceptanceGrant
	}
	if err != nil {
		return sales.VerifiedElectronicAcceptance{}, err
	}
	if err := json.Unmarshal(evidence, &verified.Evidence); err != nil {
		return sales.VerifiedElectronicAcceptance{}, err
	}
	return verified, nil
}

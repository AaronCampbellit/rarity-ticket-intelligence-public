package psa

import (
	"context"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func lockActiveClient(
	ctx context.Context,
	tx transaction,
	mspID string,
	clientID string,
) error {
	mspID = strings.TrimSpace(mspID)
	clientID = strings.TrimSpace(clientID)
	if tx == nil || mspID == "" || clientID == "" {
		return scope.ErrNotFound
	}
	tag, err := tx.Exec(ctx, `
SELECT 1
FROM client_organizations
WHERE msp_id = $1 AND id = $2::uuid
  AND lifecycle_state = 'active'
FOR SHARE
`, mspID, clientID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return scope.ErrNotFound
	}
	return nil
}

func lockActiveClientAtVersion(
	ctx context.Context,
	tx transaction,
	mspID string,
	clientID string,
	expectedVersion int64,
) error {
	mspID = strings.TrimSpace(mspID)
	clientID = strings.TrimSpace(clientID)
	if tx == nil || mspID == "" || clientID == "" || expectedVersion < 0 {
		return scope.ErrNotFound
	}
	tag, err := tx.Exec(ctx, `
SELECT 1
FROM client_organizations
WHERE msp_id = $1 AND id = $2::uuid AND lifecycle_state = 'active'
  AND ($3 = 0 OR version = $3)
FOR SHARE
`, mspID, clientID, expectedVersion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return scope.ErrNotFound
	}
	return nil
}

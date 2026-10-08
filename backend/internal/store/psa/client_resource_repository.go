package psa

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type LocationRepository struct {
	db database
}

var _ clientresources.Repository = (*LocationRepository)(nil)

func NewLocationRepository(db database) *LocationRepository {
	return &LocationRepository{db: db}
}

func (r *LocationRepository) CreateAtomic(
	ctx context.Context,
	accepted clientresources.CreateMutation,
) error {
	location, ok := accepted.Payload.(clientresources.Location)
	if !ok || accepted.Kind != "location" || location.ID != accepted.Object.ID {
		return clientresources.ErrInvalid
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if err := lockActiveClient(ctx, tx, location.MSPID, location.ClientID); err != nil {
		_ = tx.Rollback(ctx)
		return resourceWriteError(err)
	}
	if err := createLocation(ctx, tx, location, accepted); err != nil {
		_ = tx.Rollback(ctx)
		return resourceWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return resourceWriteError(err)
	}
	return nil
}

func lockActiveLocation(
	ctx context.Context,
	tx transaction,
	mspID string,
	clientID string,
	locationID string,
) error {
	return lockLocationForShare(ctx, tx, mspID, clientID, locationID, true)
}

func lockLocationForShare(
	ctx context.Context,
	tx transaction,
	mspID string,
	clientID string,
	locationID string,
	requireActive bool,
) error {
	if locationID == "" {
		return nil
	}
	query := `
SELECT 1
FROM locations
WHERE id = $1::uuid AND msp_id = $2 AND client_id = $3`
	if requireActive {
		query += `
  AND lifecycle_state = 'active'`
	}
	query += `
FOR SHARE`
	tag, err := tx.Exec(ctx, query, locationID, mspID, clientID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return scope.ErrNotFound
	}
	return nil
}

func resourceWriteError(err error) error {
	var postgres *pgconn.PgError
	if errors.As(err, &postgres) && postgres.Code == "23505" {
		switch postgres.ConstraintName {
		case "locations_msp_id_client_id_display_id_key",
			"contacts_msp_id_client_id_display_id_key",
			"assets_msp_id_client_id_display_id_key",
			"services_msp_id_client_id_display_id_key",
			"contracts_msp_id_client_id_display_id_key",
			"assets_external_identity_unique":
			return clientresources.ErrResourceIdentityConflict
		}
	}
	return err
}

func createLocation(
	ctx context.Context,
	tx transaction,
	location clientresources.Location,
	accepted clientresources.CreateMutation,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO locations (
  id, msp_id, client_id, display_id, name, lifecycle_state, version,
  created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
`, location.ID, location.MSPID, location.ClientID, location.DisplayID,
		location.Name, location.LifecycleState, location.Version,
		location.CreatedAt, location.CreatedBy, location.UpdatedAt,
		location.UpdatedBy); err != nil {
		return err
	}
	return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
}

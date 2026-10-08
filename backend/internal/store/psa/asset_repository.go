package psa

import (
	"context"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

type AssetRepository struct {
	db database
}

var _ clientresources.Repository = (*AssetRepository)(nil)

func NewAssetRepository(db database) *AssetRepository {
	return &AssetRepository{db: db}
}

func (r *AssetRepository) CreateAtomic(
	ctx context.Context,
	accepted clientresources.CreateMutation,
) error {
	asset, ok := accepted.Payload.(clientresources.Asset)
	if !ok || accepted.Kind != "asset" || asset.ID != accepted.Object.ID {
		return clientresources.ErrInvalid
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if err := lockActiveClient(ctx, tx, asset.MSPID, asset.ClientID); err != nil {
		_ = tx.Rollback(ctx)
		return resourceWriteError(err)
	}
	if err := lockActiveLocation(ctx, tx, asset.MSPID, asset.ClientID, asset.LocationID); err != nil {
		_ = tx.Rollback(ctx)
		return resourceWriteError(err)
	}
	if err := createAsset(ctx, tx, asset, accepted); err != nil {
		_ = tx.Rollback(ctx)
		return resourceWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return resourceWriteError(err)
	}
	return nil
}

func createAsset(
	ctx context.Context,
	tx transaction,
	asset clientresources.Asset,
	accepted clientresources.CreateMutation,
) error {
	_, err := tx.Exec(ctx, `
INSERT INTO assets (
  id, msp_id, client_id, location_id, display_id, name, asset_type,
  source_system, external_id, authority, lifecycle_state, version,
  created_at, created_by, updated_at, updated_by
)
VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5, $6, $7, $8, $9, $10,
        $11, $12, $13, $14, $15, $16)
`, asset.ID, asset.MSPID, asset.ClientID, asset.LocationID,
		asset.DisplayID, asset.Name, asset.AssetType,
		asset.Provenance.SourceSystem, asset.Provenance.ExternalID,
		asset.Provenance.Authority, asset.LifecycleState, asset.Version,
		asset.CreatedAt, asset.CreatedBy, asset.UpdatedAt, asset.UpdatedBy)
	if err != nil {
		return err
	}
	if err := insertInitialTagAssignments(ctx, tx, tagging.TargetRef{MSPID: asset.MSPID, ClientID: asset.ClientID, ObjectType: tagging.ObjectAsset, ObjectID: asset.ID}, asset.Version, accepted.InitialTags); err != nil {
		return err
	}
	return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
}

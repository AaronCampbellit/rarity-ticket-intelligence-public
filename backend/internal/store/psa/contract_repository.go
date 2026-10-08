package psa

import (
	"context"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
)

type ContractRepository struct {
	db database
}

var _ clientresources.Repository = (*ContractRepository)(nil)

func NewContractRepository(db database) *ContractRepository {
	return &ContractRepository{db: db}
}

func (r *ContractRepository) CreateAtomic(
	ctx context.Context,
	accepted clientresources.CreateMutation,
) error {
	contract, ok := accepted.Payload.(clientresources.Contract)
	if !ok || accepted.Kind != "contract" || contract.ID != accepted.Object.ID {
		return clientresources.ErrInvalid
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if err := lockActiveClient(ctx, tx, contract.MSPID, contract.ClientID); err != nil {
		_ = tx.Rollback(ctx)
		return resourceWriteError(err)
	}
	if err := createContract(ctx, tx, contract, accepted); err != nil {
		_ = tx.Rollback(ctx)
		return resourceWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return resourceWriteError(err)
	}
	return nil
}

func createContract(
	ctx context.Context,
	tx transaction,
	contract clientresources.Contract,
	accepted clientresources.CreateMutation,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO contracts (
  id, msp_id, client_id, display_id, name, starts_on, ends_on,
  lifecycle_state, version, created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
`, contract.ID, contract.MSPID, contract.ClientID, contract.DisplayID,
		contract.Name, contract.StartsOn, contract.EndsOn,
		contract.LifecycleState, contract.Version, contract.CreatedAt,
		contract.CreatedBy, contract.UpdatedAt, contract.UpdatedBy); err != nil {
		return err
	}
	return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
}

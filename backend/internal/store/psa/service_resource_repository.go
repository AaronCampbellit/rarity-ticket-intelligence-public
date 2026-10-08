package psa

import (
	"context"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
)

type ServiceResourceRepository struct {
	db database
}

var _ clientresources.Repository = (*ServiceResourceRepository)(nil)

func NewServiceResourceRepository(db database) *ServiceResourceRepository {
	return &ServiceResourceRepository{db: db}
}

func (r *ServiceResourceRepository) CreateAtomic(
	ctx context.Context,
	accepted clientresources.CreateMutation,
) error {
	service, ok := accepted.Payload.(clientresources.ServiceRecord)
	if !ok || accepted.Kind != "service" || service.ID != accepted.Object.ID {
		return clientresources.ErrInvalid
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if err := lockActiveClient(ctx, tx, service.MSPID, service.ClientID); err != nil {
		_ = tx.Rollback(ctx)
		return resourceWriteError(err)
	}
	if err := createServiceResource(ctx, tx, service, accepted); err != nil {
		_ = tx.Rollback(ctx)
		return resourceWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return resourceWriteError(err)
	}
	return nil
}

func createServiceResource(
	ctx context.Context,
	tx transaction,
	service clientresources.ServiceRecord,
	accepted clientresources.CreateMutation,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO services (
  id, msp_id, client_id, display_id, name, criticality,
  lifecycle_state, version, created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
`, service.ID, service.MSPID, service.ClientID, service.DisplayID,
		service.Name, service.Criticality, service.LifecycleState,
		service.Version, service.CreatedAt, service.CreatedBy,
		service.UpdatedAt, service.UpdatedBy); err != nil {
		return err
	}
	return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
}

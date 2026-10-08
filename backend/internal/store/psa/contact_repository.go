package psa

import (
	"context"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
)

type ContactRepository struct {
	db database
}

var _ clientresources.Repository = (*ContactRepository)(nil)

func NewContactRepository(db database) *ContactRepository {
	return &ContactRepository{db: db}
}

func (r *ContactRepository) CreateAtomic(
	ctx context.Context,
	accepted clientresources.CreateMutation,
) error {
	contact, ok := accepted.Payload.(clientresources.Contact)
	if !ok || accepted.Kind != "contact" || contact.ID != accepted.Object.ID {
		return clientresources.ErrInvalid
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if err := lockActiveClient(ctx, tx, contact.MSPID, contact.ClientID); err != nil {
		_ = tx.Rollback(ctx)
		return resourceWriteError(err)
	}
	if err := lockActiveLocation(ctx, tx, contact.MSPID, contact.ClientID, contact.LocationID); err != nil {
		_ = tx.Rollback(ctx)
		return resourceWriteError(err)
	}
	if err := createContact(ctx, tx, contact, accepted); err != nil {
		_ = tx.Rollback(ctx)
		return resourceWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return resourceWriteError(err)
	}
	return nil
}

func createContact(
	ctx context.Context,
	tx transaction,
	contact clientresources.Contact,
	accepted clientresources.CreateMutation,
) error {
	_, err := tx.Exec(ctx, `
INSERT INTO contacts (
  id, msp_id, client_id, location_id, display_id, display_name, email, phone,
  lifecycle_state, version, created_at, created_by, updated_at, updated_by
)
VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5, $6, NULLIF($7, ''), NULLIF($8, ''),
        $9, $10, $11, $12, $13, $14)
`, contact.ID, contact.MSPID, contact.ClientID, contact.LocationID,
		contact.DisplayID, contact.DisplayName, contact.Email, contact.Phone,
		contact.LifecycleState, contact.Version, contact.CreatedAt,
		contact.CreatedBy, contact.UpdatedAt, contact.UpdatedBy)
	if err != nil {
		return err
	}
	return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
}

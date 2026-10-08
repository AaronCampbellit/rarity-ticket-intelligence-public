package psa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/commitments"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type CommitmentRepository struct{ db database }

var _ commitments.MaintenanceRepository = (*CommitmentRepository)(nil)
var _ commitments.CommercialRepository = (*CommitmentRepository)(nil)

func NewCommitmentRepository(db database) *CommitmentRepository { return &CommitmentRepository{db: db} }
func (r *CommitmentRepository) FindMaintenance(ctx context.Context, mspID, id string) (commitments.MaintenanceWindow, error) {
	if !internalid.ValidCanonical(mspID) || !internalid.ValidCanonical(id) {
		return commitments.MaintenanceWindow{}, commitments.ErrInvalidMaintenance
	}
	var w commitments.MaintenanceWindow
	var recurrence []byte
	err := r.db.QueryRow(ctx, `SELECT id::text,msp_id::text,title,description,starts_on,ends_on,starts_at,ends_at,COALESCE(timezone,''),all_day,recurrence_rule,protected,conflict_policy,status,COALESCE(owner_id::text,''),version,created_at,created_by::text,updated_at,updated_by::text FROM maintenance_windows WHERE id=$1::uuid AND msp_id=$2::uuid`, id, mspID).Scan(&w.ID, &w.MSPID, &w.Title, &w.Description, &w.StartsOn, &w.EndsOn, &w.StartsAt, &w.EndsAt, &w.Timezone, &w.AllDay, &recurrence, &w.Protected, &w.ConflictPolicy, &w.Status, &w.OwnerID, &w.Version, &w.CreatedAt, &w.CreatedBy, &w.UpdatedAt, &w.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return commitments.MaintenanceWindow{}, scope.ErrNotFound
	}
	if err == nil && len(recurrence) > 0 {
		err = json.Unmarshal(recurrence, &w.Recurrence)
	}
	return w, err
}

func (r *CommitmentRepository) ResolveMaintenanceScopes(ctx context.Context, mspID string, refs []commitments.ScopeRef) ([]commitments.ResolvedScope, error) {
	if !internalid.ValidCanonical(mspID) {
		return nil, commitments.ErrInvalidMaintenance
	}
	found := make([]commitments.ResolvedScope, 0, len(refs))
	for _, ref := range refs {
		if !internalid.ValidCanonical(ref.ID) {
			return nil, commitments.ErrInvalidMaintenance
		}
		var clientID string
		query := ""
		switch ref.Type {
		case commitments.ScopeClient:
			query = `SELECT id::text FROM client_organizations WHERE id=$1::uuid AND msp_id=$2::uuid`
		case commitments.ScopeService:
			query = `SELECT client_id::text FROM services WHERE id=$1::uuid AND msp_id=$2::uuid`
		case commitments.ScopeAsset:
			query = `SELECT client_id::text FROM assets WHERE id=$1::uuid AND msp_id=$2::uuid`
		default:
			return nil, commitments.ErrInvalidMaintenance
		}
		err := r.db.QueryRow(ctx, query, ref.ID, mspID).Scan(&clientID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, scope.ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		found = append(found, commitments.ResolvedScope{ClientID: clientID, Type: ref.Type, ResourceID: ref.ID})
	}
	return found, nil
}

func (r *CommitmentRepository) CreateMaintenanceAtomic(ctx context.Context, accepted commitments.MaintenanceMutation) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		w := accepted.Window
		if !validMaintenanceMutationUUIDs(accepted) {
			return commitments.ErrInvalidMaintenance
		}
		if err := claimCalendarDomainRequest(ctx, tx, accepted.RequestID, w.MSPID, "", "maintenance.create", accepted.IdempotencyKey, accepted.RequestFingerprint, w, w.CreatedAt); err != nil {
			return err
		}
		recurrence, err := marshalOptionalJSON(w.Recurrence)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO maintenance_windows(id,msp_id,title,description,starts_on,ends_on,starts_at,ends_at,timezone,all_day,recurrence_rule,protected,conflict_policy,status,owner_id,version,created_at,created_by,updated_at,updated_by)
SELECT $1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10,$11::jsonb,$12,$13,$14,NULLIF($15,'')::uuid,1,$16,$17::uuid,$18,$19::uuid
WHERE EXISTS(SELECT 1 FROM msp_organizations WHERE id=$2::uuid) AND EXISTS(SELECT 1 FROM technicians actor WHERE actor.id=$17::uuid AND actor.msp_id=$2::uuid AND actor.lifecycle_state='active') AND (NULLIF($15,'')::uuid IS NULL OR EXISTS(SELECT 1 FROM technicians owner WHERE owner.id=$15::uuid AND owner.msp_id=$2::uuid AND owner.lifecycle_state='active'))`, w.ID, w.MSPID, w.Title, w.Description, w.StartsOn, w.EndsOn, nullableTime(w.StartsAt), nullableTime(w.EndsAt), w.Timezone, w.AllDay, recurrence, w.Protected, w.ConflictPolicy, w.Status, w.OwnerID, w.CreatedAt, w.CreatedBy, w.UpdatedAt, w.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return scope.ErrNotFound
		}
		for _, s := range w.Scopes {
			query, err := maintenanceScopeInsert(s.Type)
			if err != nil {
				return err
			}
			tag, err = tx.Exec(ctx, query, s.ID, w.ID, w.MSPID, s.ResourceID, s.Type, w.CreatedAt, w.CreatedBy, s.ClientID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return scope.ErrNotFound
			}
		}
		if err = writeAuditOnly(ctx, tx, accepted.Audit); err != nil {
			return err
		}
		return writeEventOnly(ctx, tx, accepted.Event)
	})
}
func maintenanceScopeInsert(t commitments.ScopeType) (string, error) {
	switch t {
	case commitments.ScopeClient:
		return `INSERT INTO maintenance_window_scopes(id,maintenance_window_id,msp_id,client_id,scope_type,service_id,asset_id,created_at,created_by) SELECT $1::uuid,$2::uuid,$3::uuid,client.id,$5,NULL,NULL,$6,$7::uuid FROM client_organizations client WHERE client.id=$4::uuid AND client.msp_id=$3::uuid AND client.id=$8::uuid`, nil
	case commitments.ScopeService:
		return `INSERT INTO maintenance_window_scopes(id,maintenance_window_id,msp_id,client_id,scope_type,service_id,asset_id,created_at,created_by) SELECT $1::uuid,$2::uuid,$3::uuid,resource.client_id,$5,resource.id,NULL,$6,$7::uuid FROM services resource WHERE resource.id=$4::uuid AND resource.msp_id=$3::uuid AND resource.client_id=$8::uuid`, nil
	case commitments.ScopeAsset:
		return `INSERT INTO maintenance_window_scopes(id,maintenance_window_id,msp_id,client_id,scope_type,service_id,asset_id,created_at,created_by) SELECT $1::uuid,$2::uuid,$3::uuid,resource.client_id,$5,NULL,resource.id,$6,$7::uuid FROM assets resource WHERE resource.id=$4::uuid AND resource.msp_id=$3::uuid AND resource.client_id=$8::uuid`, nil
	}
	return "", commitments.ErrInvalidMaintenance
}

func (r *CommitmentRepository) UpdateMaintenanceAtomic(ctx context.Context, accepted commitments.MaintenanceMutation, expected int64) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		w := accepted.Window
		if !validMaintenanceMutationUUIDs(accepted) {
			return commitments.ErrInvalidMaintenance
		}
		if err := claimCalendarDomainRequest(ctx, tx, accepted.RequestID, w.MSPID, "", "maintenance.update", accepted.IdempotencyKey, accepted.RequestFingerprint, w, w.UpdatedAt); err != nil {
			return err
		}
		recurrence, err := marshalOptionalJSON(w.Recurrence)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE maintenance_windows SET title=$4,description=$5,starts_on=$6,ends_on=$7,starts_at=$8,ends_at=$9,timezone=NULLIF($10,''),all_day=$11,recurrence_rule=$12::jsonb,protected=$13,conflict_policy=$14,status=$15,owner_id=NULLIF($16,'')::uuid,version=version+1,updated_at=$17,updated_by=$18::uuid WHERE id=$1::uuid AND msp_id=$2::uuid AND version=$3 AND (NULLIF($16,'')::uuid IS NULL OR EXISTS(SELECT 1 FROM technicians owner WHERE owner.id=$16::uuid AND owner.msp_id=$2::uuid AND owner.lifecycle_state='active'))`, w.ID, w.MSPID, expected, w.Title, w.Description, w.StartsOn, w.EndsOn, nullableTime(w.StartsAt), nullableTime(w.EndsAt), w.Timezone, w.AllDay, recurrence, w.Protected, w.ConflictPolicy, w.Status, w.OwnerID, w.UpdatedAt, w.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return classifyMSPVersion(ctx, tx, "maintenance_windows", w.ID, w.MSPID, expected, commitments.ErrCommitmentVersionConflict)
		}
		if _, err = tx.Exec(ctx, `DELETE FROM maintenance_window_scopes WHERE maintenance_window_id=$1::uuid AND msp_id=$2::uuid`, w.ID, w.MSPID); err != nil {
			return err
		}
		for _, s := range w.Scopes {
			query, e := maintenanceScopeInsert(s.Type)
			if e != nil {
				return e
			}
			tag, e = tx.Exec(ctx, query, s.ID, w.ID, w.MSPID, s.ResourceID, s.Type, w.UpdatedAt, w.UpdatedBy, s.ClientID)
			if e != nil {
				return e
			}
			if tag.RowsAffected() != 1 {
				return scope.ErrNotFound
			}
		}
		if err = writeAuditOnly(ctx, tx, accepted.Audit); err != nil {
			return err
		}
		return writeEventOnly(ctx, tx, accepted.Event)
	})
}

func (r *CommitmentRepository) TransitionMaintenanceAtomic(ctx context.Context, accepted commitments.MaintenanceMutation, expected int64) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		w := accepted.Window
		if !validMaintenanceMutationUUIDs(accepted) {
			return commitments.ErrInvalidMaintenance
		}
		if err := claimCalendarDomainRequest(ctx, tx, accepted.RequestID, w.MSPID, "", "maintenance.transition", accepted.IdempotencyKey, accepted.RequestFingerprint, w, w.UpdatedAt); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE maintenance_windows SET status=$4,version=version+1,updated_at=$5,updated_by=$6::uuid WHERE id=$1::uuid AND msp_id=$2::uuid AND version=$3 AND EXISTS(SELECT 1 FROM technicians actor WHERE actor.id=$6::uuid AND actor.msp_id=$2::uuid AND actor.lifecycle_state='active')`, w.ID, w.MSPID, expected, w.Status, w.UpdatedAt, w.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return classifyMSPVersion(ctx, tx, "maintenance_windows", w.ID, w.MSPID, expected, commitments.ErrCommitmentVersionConflict)
		}
		if err = writeAuditOnly(ctx, tx, accepted.Audit); err != nil {
			return err
		}
		return writeEventOnly(ctx, tx, accepted.Event)
	})
}

func (r *CommitmentRepository) CommercialRelationshipsBelongToClient(ctx context.Context, target scope.Target, links commitments.CommercialLinks) (bool, error) {
	if !validTargetUUIDs(target) || !validCommercialLinkUUIDs(links) {
		return false, commitments.ErrInvalidCommercial
	}
	for table, id := range map[string]string{"services": links.ServiceID, "assets": links.AssetID, "contracts": links.ContractID} {
		if id == "" {
			continue
		}
		var found bool
		err := r.db.QueryRow(ctx, fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid)`, table), id, target.MSPID, target.ClientID).Scan(&found)
		if err != nil || !found {
			return found, err
		}
	}
	return true, nil
}

func (r *CommitmentRepository) FindCommercial(ctx context.Context, target scope.Target, id string) (commitments.CommercialCommitment, error) {
	if !validTargetUUIDs(target) || !internalid.ValidCanonical(id) {
		return commitments.CommercialCommitment{}, commitments.ErrInvalidCommercial
	}
	var found commitments.CommercialCommitment
	var recurrence []byte
	err := r.db.QueryRow(ctx, `SELECT id::text,msp_id::text,client_id::text,commitment_type,title,description,vendor_name,external_reference,effective_on,COALESCE(notice_on,'0001-01-01'::date),COALESCE(renewal_on,'0001-01-01'::date),expiration_on,ROUND(quantity*10000)::bigint,cost IS NOT NULL,COALESCE(ROUND(cost*100)::bigint,0),COALESCE(currency,''),COALESCE(service_id::text,''),COALESCE(asset_id::text,''),COALESCE(contract_id::text,''),status,owner_id::text,recurrence_rule,version,created_at,created_by::text,updated_at,updated_by::text FROM commercial_commitments WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid`, id, target.MSPID, target.ClientID).Scan(
		&found.ID, &found.MSPID, &found.ClientID, &found.Type, &found.Title, &found.Description,
		&found.Vendor, &found.ExternalReference, &found.EffectiveOn, &found.NoticeOn, &found.RenewalOn,
		&found.ExpirationOn, &found.QuantityUnits, &found.HasCost, &found.CostMinor, &found.Currency,
		&found.ServiceID, &found.AssetID, &found.ContractID, &found.Status, &found.OwnerID, &recurrence,
		&found.Version, &found.CreatedAt, &found.CreatedBy, &found.UpdatedAt, &found.UpdatedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return commitments.CommercialCommitment{}, scope.ErrNotFound
	}
	if err == nil && len(recurrence) > 0 {
		err = json.Unmarshal(recurrence, &found.Recurrence)
	}
	return found, err
}

func (r *CommitmentRepository) CreateCommercialAtomic(ctx context.Context, accepted commitments.CommercialMutation) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		c := accepted.Commitment
		if !validCommercialMutationUUIDs(accepted) {
			return commitments.ErrInvalidCommercial
		}
		if err := claimCalendarDomainRequest(ctx, tx, accepted.RequestID, c.MSPID, c.ClientID, "commercial.create", accepted.IdempotencyKey, accepted.RequestFingerprint, c, c.CreatedAt); err != nil {
			return err
		}
		recurrence, err := marshalOptionalJSON(c.Recurrence)
		if err != nil {
			return err
		}
		var costMinor any
		if c.HasCost {
			costMinor = c.CostMinor
		}
		tag, err := tx.Exec(ctx, `INSERT INTO commercial_commitments(id,msp_id,client_id,commitment_type,title,description,vendor_name,external_reference,effective_on,notice_on,renewal_on,expiration_on,recurrence_rule,quantity,cost,currency,service_id,asset_id,contract_id,status,owner_id,version,created_at,created_by,updated_at,updated_by)
SELECT $1::uuid,$2::uuid,client.id,$4,$5,$6,$7,$8,$9,NULLIF($10,'0001-01-01'::date),NULLIF($11,'0001-01-01'::date),$12,$13::jsonb,$14::numeric/10000,CASE WHEN $15::bigint IS NULL THEN NULL ELSE $15::numeric/100 END,NULLIF($16,''),NULLIF($17,'')::uuid,NULLIF($18,'')::uuid,NULLIF($19,'')::uuid,$20,$21::uuid,1,$22,$23::uuid,$24,$25::uuid FROM client_organizations client
WHERE client.id=$3::uuid AND client.msp_id=$2::uuid AND EXISTS(SELECT 1 FROM technicians owner WHERE owner.id=$21::uuid AND owner.msp_id=$2::uuid AND owner.lifecycle_state='active') AND EXISTS(SELECT 1 FROM technicians actor WHERE actor.id=$23::uuid AND actor.msp_id=$2::uuid AND actor.lifecycle_state='active')
AND (NULLIF($17,'')::uuid IS NULL OR EXISTS(SELECT 1 FROM services WHERE id=$17::uuid AND msp_id=$2::uuid AND client_id=$3::uuid)) AND (NULLIF($18,'')::uuid IS NULL OR EXISTS(SELECT 1 FROM assets WHERE id=$18::uuid AND msp_id=$2::uuid AND client_id=$3::uuid)) AND (NULLIF($19,'')::uuid IS NULL OR EXISTS(SELECT 1 FROM contracts WHERE id=$19::uuid AND msp_id=$2::uuid AND client_id=$3::uuid))`, c.ID, c.MSPID, c.ClientID, c.Type, c.Title, c.Description, c.Vendor, c.ExternalReference, c.EffectiveOn, c.NoticeOn, c.RenewalOn, c.ExpirationOn, recurrence, c.QuantityUnits, costMinor, c.Currency, c.ServiceID, c.AssetID, c.ContractID, c.Status, c.OwnerID, c.CreatedAt, c.CreatedBy, c.UpdatedAt, c.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return scope.ErrNotFound
		}
		if err = writeAuditOnly(ctx, tx, accepted.Audit); err != nil {
			return err
		}
		return writeEventOnly(ctx, tx, accepted.Event)
	})
}
func (r *CommitmentRepository) UpdateCommercialAtomic(ctx context.Context, accepted commitments.CommercialMutation, expected int64) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		c := accepted.Commitment
		if !validCommercialMutationUUIDs(accepted) {
			return commitments.ErrInvalidCommercial
		}
		if err := claimCalendarDomainRequest(ctx, tx, accepted.RequestID, c.MSPID, c.ClientID, "commercial.update", accepted.IdempotencyKey, accepted.RequestFingerprint, c, c.UpdatedAt); err != nil {
			return err
		}
		recurrence, err := marshalOptionalJSON(c.Recurrence)
		if err != nil {
			return err
		}
		var costMinor any
		if c.HasCost {
			costMinor = c.CostMinor
		}
		tag, err := tx.Exec(ctx, `UPDATE commercial_commitments SET title=$6,description=$7,vendor_name=$8,external_reference=$9,effective_on=$10,notice_on=NULLIF($11,'0001-01-01'::date),renewal_on=NULLIF($12,'0001-01-01'::date),expiration_on=$13,recurrence_rule=$14::jsonb,quantity=$15::numeric/10000,cost=CASE WHEN $16::bigint IS NULL THEN NULL ELSE $16::numeric/100 END,currency=NULLIF($17,''),service_id=NULLIF($18,'')::uuid,asset_id=NULLIF($19,'')::uuid,contract_id=NULLIF($20,'')::uuid,status=$21,owner_id=$22::uuid,version=version+1,updated_at=$23,updated_by=$24::uuid WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid AND version=$4 AND commitment_type=$5 AND EXISTS(SELECT 1 FROM technicians owner WHERE owner.id=$22::uuid AND owner.msp_id=$2::uuid AND owner.lifecycle_state='active') AND (NULLIF($18,'')::uuid IS NULL OR EXISTS(SELECT 1 FROM services WHERE id=$18::uuid AND msp_id=$2::uuid AND client_id=$3::uuid)) AND (NULLIF($19,'')::uuid IS NULL OR EXISTS(SELECT 1 FROM assets WHERE id=$19::uuid AND msp_id=$2::uuid AND client_id=$3::uuid)) AND (NULLIF($20,'')::uuid IS NULL OR EXISTS(SELECT 1 FROM contracts WHERE id=$20::uuid AND msp_id=$2::uuid AND client_id=$3::uuid))`, c.ID, c.MSPID, c.ClientID, expected, c.Type, c.Title, c.Description, c.Vendor, c.ExternalReference, c.EffectiveOn, c.NoticeOn, c.RenewalOn, c.ExpirationOn, recurrence, c.QuantityUnits, costMinor, c.Currency, c.ServiceID, c.AssetID, c.ContractID, c.Status, c.OwnerID, c.UpdatedAt, c.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return classifyScopedVersion(ctx, tx, "commercial_commitments", c.ID, c.MSPID, c.ClientID, expected, commitments.ErrCommitmentVersionConflict)
		}
		if err = writeAuditOnly(ctx, tx, accepted.Audit); err != nil {
			return err
		}
		return writeEventOnly(ctx, tx, accepted.Event)
	})
}
func (r *CommitmentRepository) withTransaction(ctx context.Context, fn func(transaction) error) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func validMaintenanceMutationUUIDs(accepted commitments.MaintenanceMutation) bool {
	w := accepted.Window
	if !internalid.ValidCanonical(w.ID) || !internalid.ValidCanonical(w.MSPID) || (w.OwnerID != "" && !internalid.ValidCanonical(w.OwnerID)) || !internalid.ValidCanonical(w.CreatedBy) || !internalid.ValidCanonical(w.UpdatedBy) || !validCalendarFactUUIDs(accepted.RequestID, accepted.Audit, accepted.Event) {
		return false
	}
	seen := make(map[string]struct{}, len(w.Scopes))
	for _, item := range w.Scopes {
		if !internalid.ValidCanonical(item.ID) || !internalid.ValidCanonical(item.ClientID) || !internalid.ValidCanonical(item.ResourceID) {
			return false
		}
		key := string(item.Type) + ":" + item.ResourceID
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}
func validCommercialLinkUUIDs(links commitments.CommercialLinks) bool {
	return (links.ServiceID == "" || internalid.ValidCanonical(links.ServiceID)) && (links.AssetID == "" || internalid.ValidCanonical(links.AssetID)) && (links.ContractID == "" || internalid.ValidCanonical(links.ContractID))
}
func validCommercialMutationUUIDs(accepted commitments.CommercialMutation) bool {
	c := accepted.Commitment
	return internalid.ValidCanonical(c.ID) && internalid.ValidCanonical(c.MSPID) && internalid.ValidCanonical(c.ClientID) && internalid.ValidCanonical(c.OwnerID) && internalid.ValidCanonical(c.CreatedBy) && internalid.ValidCanonical(c.UpdatedBy) && validCalendarFactUUIDs(accepted.RequestID, accepted.Audit, accepted.Event) && validCommercialLinkUUIDs(c.CommercialLinks)
}

package psa

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func (r *LocationRepository) UpdateLocation(ctx context.Context, accepted clientresources.UpdateMutation) (clientresources.ResourceDetail, error) {
	return updateClientResource(ctx, r.db, clientresources.LocationKind, accepted)
}
func (r *ContactRepository) UpdateContact(ctx context.Context, accepted clientresources.UpdateMutation) (clientresources.ResourceDetail, error) {
	return updateClientResource(ctx, r.db, clientresources.ContactKind, accepted)
}
func (r *AssetRepository) UpdateAsset(ctx context.Context, accepted clientresources.UpdateMutation) (clientresources.ResourceDetail, error) {
	return updateClientResource(ctx, r.db, clientresources.AssetKind, accepted)
}
func (r *ServiceResourceRepository) UpdateService(ctx context.Context, accepted clientresources.UpdateMutation) (clientresources.ResourceDetail, error) {
	return updateClientResource(ctx, r.db, clientresources.ServiceKind, accepted)
}
func (r *ContractRepository) UpdateContract(ctx context.Context, accepted clientresources.UpdateMutation) (clientresources.ResourceDetail, error) {
	return updateClientResource(ctx, r.db, clientresources.ContractKind, accepted)
}

func (r *LocationRepository) Preflight(ctx context.Context, accepted clientresources.LifecyclePreflightMutation) (clientresources.LifecyclePreflight, error) {
	return preflightClientResource(ctx, r.db, clientresources.LocationKind, accepted)
}
func (r *ContactRepository) Preflight(ctx context.Context, accepted clientresources.LifecyclePreflightMutation) (clientresources.LifecyclePreflight, error) {
	return preflightClientResource(ctx, r.db, clientresources.ContactKind, accepted)
}
func (r *AssetRepository) Preflight(ctx context.Context, accepted clientresources.LifecyclePreflightMutation) (clientresources.LifecyclePreflight, error) {
	return preflightClientResource(ctx, r.db, clientresources.AssetKind, accepted)
}
func (r *ServiceResourceRepository) Preflight(ctx context.Context, accepted clientresources.LifecyclePreflightMutation) (clientresources.LifecyclePreflight, error) {
	return preflightClientResource(ctx, r.db, clientresources.ServiceKind, accepted)
}
func (r *ContractRepository) Preflight(ctx context.Context, accepted clientresources.LifecyclePreflightMutation) (clientresources.LifecyclePreflight, error) {
	return preflightClientResource(ctx, r.db, clientresources.ContractKind, accepted)
}

func (r *LocationRepository) DeactivateLocation(ctx context.Context, accepted clientresources.LifecycleMutation) (clientresources.ResourceDetail, error) {
	if !validLifecycleWrapper(clientresources.LocationKind, "active", "inactive", "location.deactivated", accepted) {
		return clientresources.ResourceDetail{}, clientresources.ErrInvalid
	}
	return transitionClientResource(ctx, r.db, clientresources.LocationKind, accepted)
}
func (r *LocationRepository) ReactivateLocation(ctx context.Context, accepted clientresources.LifecycleMutation) (clientresources.ResourceDetail, error) {
	if !validLifecycleWrapper(clientresources.LocationKind, "inactive", "active", "location.reactivated", accepted) {
		return clientresources.ResourceDetail{}, clientresources.ErrInvalid
	}
	return transitionClientResource(ctx, r.db, clientresources.LocationKind, accepted)
}
func (r *ContactRepository) DeactivateContact(ctx context.Context, accepted clientresources.LifecycleMutation) (clientresources.ResourceDetail, error) {
	if !validLifecycleWrapper(clientresources.ContactKind, "active", "inactive", "contact.deactivated", accepted) {
		return clientresources.ResourceDetail{}, clientresources.ErrInvalid
	}
	return transitionClientResource(ctx, r.db, clientresources.ContactKind, accepted)
}
func (r *ContactRepository) ReactivateContact(ctx context.Context, accepted clientresources.LifecycleMutation) (clientresources.ResourceDetail, error) {
	if !validLifecycleWrapper(clientresources.ContactKind, "inactive", "active", "contact.reactivated", accepted) {
		return clientresources.ResourceDetail{}, clientresources.ErrInvalid
	}
	return transitionClientResource(ctx, r.db, clientresources.ContactKind, accepted)
}
func (r *AssetRepository) DeactivateAsset(ctx context.Context, accepted clientresources.LifecycleMutation) (clientresources.ResourceDetail, error) {
	if !validLifecycleWrapper(clientresources.AssetKind, "active", "inactive", "asset.deactivated", accepted) {
		return clientresources.ResourceDetail{}, clientresources.ErrInvalid
	}
	return transitionClientResource(ctx, r.db, clientresources.AssetKind, accepted)
}
func (r *AssetRepository) ReactivateAsset(ctx context.Context, accepted clientresources.LifecycleMutation) (clientresources.ResourceDetail, error) {
	if !validLifecycleWrapper(clientresources.AssetKind, "inactive", "active", "asset.reactivated", accepted) {
		return clientresources.ResourceDetail{}, clientresources.ErrInvalid
	}
	return transitionClientResource(ctx, r.db, clientresources.AssetKind, accepted)
}
func (r *ServiceResourceRepository) DeactivateService(ctx context.Context, accepted clientresources.LifecycleMutation) (clientresources.ResourceDetail, error) {
	if !validLifecycleWrapper(clientresources.ServiceKind, "active", "inactive", "service.deactivated", accepted) {
		return clientresources.ResourceDetail{}, clientresources.ErrInvalid
	}
	return transitionClientResource(ctx, r.db, clientresources.ServiceKind, accepted)
}
func (r *ServiceResourceRepository) ReactivateService(ctx context.Context, accepted clientresources.LifecycleMutation) (clientresources.ResourceDetail, error) {
	if !validLifecycleWrapper(clientresources.ServiceKind, "inactive", "active", "service.reactivated", accepted) {
		return clientresources.ResourceDetail{}, clientresources.ErrInvalid
	}
	return transitionClientResource(ctx, r.db, clientresources.ServiceKind, accepted)
}
func (r *ContractRepository) DeactivateContract(ctx context.Context, accepted clientresources.LifecycleMutation) (clientresources.ResourceDetail, error) {
	if !validLifecycleWrapper(clientresources.ContractKind, "active", "inactive", "contract.deactivated", accepted) {
		return clientresources.ResourceDetail{}, clientresources.ErrInvalid
	}
	return transitionClientResource(ctx, r.db, clientresources.ContractKind, accepted)
}
func (r *ContractRepository) ReactivateContract(ctx context.Context, accepted clientresources.LifecycleMutation) (clientresources.ResourceDetail, error) {
	if !validLifecycleWrapper(clientresources.ContractKind, "inactive", "active", "contract.reactivated", accepted) {
		return clientresources.ResourceDetail{}, clientresources.ErrInvalid
	}
	return transitionClientResource(ctx, r.db, clientresources.ContractKind, accepted)
}

func validLifecycleWrapper(
	kind clientresources.Kind,
	fromState, toState, action string,
	accepted clientresources.LifecycleMutation,
) bool {
	return accepted.FromState == fromState && accepted.ToState == toState &&
		accepted.Audit.SubjectType == string(kind) &&
		accepted.Event.SubjectType == string(kind) &&
		accepted.Audit.Action == action && accepted.Event.EventType == action
}

func updateClientResource(
	ctx context.Context,
	db database,
	kind clientresources.Kind,
	accepted clientresources.UpdateMutation,
) (clientresources.ResourceDetail, error) {
	if !validUpdateMutation(kind, accepted) {
		return clientresources.ResourceDetail{}, clientresources.ErrInvalid
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return clientresources.ResourceDetail{}, err
	}
	fail := func(err error) (clientresources.ResourceDetail, error) {
		_ = tx.Rollback(ctx)
		return clientresources.ResourceDetail{}, resourceWriteError(err)
	}
	if err := lockActiveClient(ctx, tx, accepted.Target.MSPID, accepted.Target.ClientID); err != nil {
		return fail(err)
	}
	current, err := loadClientResourceForUpdate(
		ctx, tx, kind, accepted,
	)
	if err != nil {
		return fail(err)
	}
	if current.Version != accepted.ExpectedVersion {
		return fail(object.ErrVersionConflict)
	}
	if current.LifecycleState != "active" {
		return fail(clientresources.ErrLifecycleConflict)
	}
	if integrationOwnedAsset(kind, current) {
		return fail(clientresources.ErrResourceAuthorityConflict)
	}
	next, before, after, err := applyResourcePatch(kind, current, accepted.Patch)
	if err != nil {
		return fail(err)
	}
	diff := resourceSafeDiff(kind, before, after)
	if len(diff) == 0 {
		return fail(clientresources.ErrInvalid)
	}
	if err := updateLockedClientResource(ctx, tx, kind, next, accepted); err != nil {
		return fail(err)
	}
	accepted.Audit.SafeDiff = diff
	accepted.Event.Data = map[string]any{"changed_fields": sortedDiffFields(diff)}
	if err := writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		return fail(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fail(err)
	}
	return next, nil
}

func transitionClientResource(
	ctx context.Context,
	db database,
	kind clientresources.Kind,
	accepted clientresources.LifecycleMutation,
) (clientresources.ResourceDetail, error) {
	if !validLifecycleMutation(kind, accepted) {
		return clientresources.ResourceDetail{}, clientresources.ErrInvalid
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return clientresources.ResourceDetail{}, err
	}
	fail := func(err error) (clientresources.ResourceDetail, error) {
		_ = tx.Rollback(ctx)
		return clientresources.ResourceDetail{}, resourceWriteError(err)
	}
	if err := lockActiveClient(ctx, tx, accepted.Target.MSPID, accepted.Target.ClientID); err != nil {
		return fail(err)
	}
	current, err := loadClientResourceForLifecycle(
		ctx, tx, kind, accepted,
	)
	if err != nil {
		return fail(err)
	}
	if current.Version != accepted.ExpectedVersion {
		return fail(object.ErrVersionConflict)
	}
	if current.LifecycleState != accepted.FromState {
		return fail(clientresources.ErrLifecycleConflict)
	}
	if integrationOwnedAsset(kind, current) {
		return fail(clientresources.ErrResourceAuthorityConflict)
	}
	if kind == clientresources.LocationKind && accepted.ToState == "inactive" {
		if err := lockActiveLocationDependencies(ctx, tx, accepted.Target, accepted.ResourceID); err != nil {
			return fail(err)
		}
	}
	if err := transitionLockedClientResource(ctx, tx, kind, accepted); err != nil {
		return fail(err)
	}
	next := current
	next.LifecycleState = accepted.ToState
	next.Version++
	accepted.Audit.SafeDiff = mutation.BuildSafeDiff(
		map[string]any{"lifecycle_state": current.LifecycleState},
		map[string]any{"lifecycle_state": next.LifecycleState},
	)
	accepted.Event.Data = map[string]any{"changed_fields": []string{"lifecycle_state"}}
	if err := writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		return fail(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fail(err)
	}
	return next, nil
}

func preflightClientResource(
	ctx context.Context,
	db database,
	kind clientresources.Kind,
	accepted clientresources.LifecyclePreflightMutation,
) (clientresources.LifecyclePreflight, error) {
	if accepted.Kind != kind ||
		(accepted.Operation != clientresources.PreflightUpdate &&
			accepted.Operation != clientresources.PreflightDeactivate &&
			accepted.Operation != clientresources.PreflightReactivate) {
		return clientresources.LifecyclePreflight{}, clientresources.ErrInvalid
	}
	current, err := loadClientResourceReadOnly(ctx, db, accepted.Kind, accepted.Target, accepted.ResourceID)
	if err != nil {
		return clientresources.LifecyclePreflight{}, err
	}
	if current.Version != accepted.ExpectedVersion {
		return clientresources.LifecyclePreflight{}, object.ErrVersionConflict
	}
	wantState := "active"
	if accepted.Operation == clientresources.PreflightReactivate {
		wantState = "inactive"
	}
	if current.LifecycleState != wantState {
		return clientresources.LifecyclePreflight{}, clientresources.ErrLifecycleConflict
	}
	if integrationOwnedAsset(accepted.Kind, current) {
		return clientresources.LifecyclePreflight{}, clientresources.ErrResourceAuthorityConflict
	}
	result := clientresources.LifecyclePreflight{
		Resource: current, DependencyStatus: "not_applicable", RelationshipStatus: "not_applicable",
	}
	if accepted.Operation == clientresources.PreflightUpdate {
		next, before, after, err := applyResourcePatch(accepted.Kind, current, accepted.Patch)
		if err != nil {
			return clientresources.LifecyclePreflight{}, err
		}
		if len(resourceSafeDiff(accepted.Kind, before, after)) == 0 {
			return clientresources.LifecyclePreflight{}, clientresources.ErrInvalid
		}
		result.Resource = next
	}
	if accepted.Kind == clientresources.LocationKind &&
		accepted.Operation == clientresources.PreflightDeactivate {
		inUse, err := hasActiveLocationDependencies(ctx, db, accepted.Target, accepted.ResourceID)
		if err != nil {
			return clientresources.LifecyclePreflight{}, err
		}
		if inUse {
			return clientresources.LifecyclePreflight{}, clientresources.ErrResourceInUse
		}
		result.DependencyStatus = "clear"
	}
	if locationBoundResource(accepted.Kind) &&
		(accepted.Operation == clientresources.PreflightUpdate ||
			accepted.Operation == clientresources.PreflightReactivate) {
		locationID := result.Resource.LocationID
		if locationID == "" {
			result.RelationshipStatus = "unassigned"
		} else {
			active, err := activeLocationExists(ctx, db, accepted.Target, locationID)
			if err != nil {
				return clientresources.LifecyclePreflight{}, err
			}
			if !active {
				return clientresources.LifecyclePreflight{}, scope.ErrNotFound
			}
			result.RelationshipStatus = "active"
		}
	}
	return result, nil
}

func loadClientResourceReadOnly(
	ctx context.Context,
	db database,
	kind clientresources.Kind,
	target scope.Target,
	resourceID string,
) (clientresources.ResourceDetail, error) {
	base, ok := clientResourceSelectSQL(kind)
	if !ok {
		return clientresources.ResourceDetail{}, clientresources.ErrInvalid
	}
	resource, err := scanClientResourceDetail(db.QueryRow(ctx, base+`
WHERE id = $1::uuid AND msp_id = $2 AND client_id = $3::uuid
  AND EXISTS (
    SELECT 1 FROM client_organizations
    WHERE id = $3::uuid AND msp_id = $2 AND lifecycle_state = 'active'
  )
`, resourceID, target.MSPID, target.ClientID))
	if errors.Is(err, pgx.ErrNoRows) {
		return clientresources.ResourceDetail{}, scope.ErrNotFound
	}
	return resource, err
}

func hasActiveLocationDependencies(
	ctx context.Context,
	db database,
	target scope.Target,
	locationID string,
) (bool, error) {
	var inUse bool
	err := db.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM contacts
  WHERE msp_id = $1 AND client_id = $2::uuid AND location_id = $3::uuid
    AND lifecycle_state = 'active'
) OR EXISTS (
  SELECT 1 FROM assets
  WHERE msp_id = $1 AND client_id = $2::uuid AND location_id = $3::uuid
    AND lifecycle_state = 'active'
)`, target.MSPID, target.ClientID, locationID).Scan(&inUse)
	return inUse, err
}

func activeLocationExists(
	ctx context.Context,
	db database,
	target scope.Target,
	locationID string,
) (bool, error) {
	var active bool
	err := db.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM locations
  WHERE id = $1::uuid AND msp_id = $2 AND client_id = $3::uuid
    AND lifecycle_state = 'active'
)`, locationID, target.MSPID, target.ClientID).Scan(&active)
	return active, err
}

func validUpdateMutation(kind clientresources.Kind, value clientresources.UpdateMutation) bool {
	return clientResourceTable(kind) != "" && value.Target.MSPID != "" &&
		value.Target.ClientID != "" && value.ResourceID != "" &&
		value.ExpectedVersion > 0 && value.ActorID != "" && !value.UpdatedAt.IsZero() &&
		value.Audit.SubjectType == string(kind) && value.Audit.SubjectID == value.ResourceID &&
		value.Audit.SubjectVersion == value.ExpectedVersion+1 &&
		value.Event.SubjectType == string(kind) && value.Event.SubjectID == value.ResourceID &&
		value.Event.SubjectVersion == value.ExpectedVersion+1
}

func validLifecycleMutation(kind clientresources.Kind, value clientresources.LifecycleMutation) bool {
	if !validUpdateMutation(kind, clientresources.UpdateMutation{
		Target: value.Target, ResourceID: value.ResourceID, ExpectedVersion: value.ExpectedVersion,
		ActorID: value.ActorID, UpdatedAt: value.UpdatedAt, Audit: value.Audit, Event: value.Event,
	}) {
		return false
	}
	return (value.FromState == "active" && value.ToState == "inactive") ||
		(value.FromState == "inactive" && value.ToState == "active")
}

func loadLockedClientResource(
	ctx context.Context,
	tx transaction,
	kind clientresources.Kind,
	target scope.Target,
	resourceID string,
) (clientresources.ResourceDetail, error) {
	base, ok := clientResourceSelectSQL(kind)
	if !ok {
		return clientresources.ResourceDetail{}, clientresources.ErrInvalid
	}
	resource, err := scanClientResourceDetail(tx.QueryRow(ctx, base+`
WHERE id = $1::uuid AND msp_id = $2 AND client_id = $3::uuid
FOR UPDATE
`, resourceID, target.MSPID, target.ClientID))
	if errors.Is(err, pgx.ErrNoRows) {
		return clientresources.ResourceDetail{}, scope.ErrNotFound
	}
	return resource, err
}

func loadClientResourceSnapshot(
	ctx context.Context,
	tx transaction,
	kind clientresources.Kind,
	target scope.Target,
	resourceID string,
) (clientresources.ResourceDetail, error) {
	base, ok := clientResourceSelectSQL(kind)
	if !ok {
		return clientresources.ResourceDetail{}, clientresources.ErrInvalid
	}
	resource, err := scanClientResourceDetail(tx.QueryRow(ctx, base+`
WHERE id = $1::uuid AND msp_id = $2 AND client_id = $3::uuid
`, resourceID, target.MSPID, target.ClientID))
	if errors.Is(err, pgx.ErrNoRows) {
		return clientresources.ResourceDetail{}, scope.ErrNotFound
	}
	return resource, err
}

func loadClientResourceForUpdate(
	ctx context.Context,
	tx transaction,
	kind clientresources.Kind,
	accepted clientresources.UpdateMutation,
) (clientresources.ResourceDetail, error) {
	if !locationBoundResource(kind) {
		return loadLockedClientResource(
			ctx, tx, kind, accepted.Target, accepted.ResourceID,
		)
	}
	// The unlocked snapshot selects the Location lock set only. The exact
	// resource is locked and revalidated before any decision or mutation.
	snapshot, err := loadClientResourceSnapshot(
		ctx, tx, kind, accepted.Target, accepted.ResourceID,
	)
	if err != nil {
		return clientresources.ResourceDetail{}, err
	}
	if err := lockResourceUpdateLocations(
		ctx, tx, accepted.Target, snapshot.LocationID, accepted.Patch.LocationID,
	); err != nil {
		return clientresources.ResourceDetail{}, err
	}
	current, err := loadLockedClientResource(
		ctx, tx, kind, accepted.Target, accepted.ResourceID,
	)
	if err != nil {
		return clientresources.ResourceDetail{}, err
	}
	if !sameResourceLockSnapshot(snapshot, current) {
		return clientresources.ResourceDetail{}, object.ErrVersionConflict
	}
	return current, nil
}

func loadClientResourceForLifecycle(
	ctx context.Context,
	tx transaction,
	kind clientresources.Kind,
	accepted clientresources.LifecycleMutation,
) (clientresources.ResourceDetail, error) {
	if accepted.ToState != "active" || !locationBoundResource(kind) {
		return loadLockedClientResource(
			ctx, tx, kind, accepted.Target, accepted.ResourceID,
		)
	}
	// Keep the global order Client -> Location -> Contact/Asset.
	snapshot, err := loadClientResourceSnapshot(
		ctx, tx, kind, accepted.Target, accepted.ResourceID,
	)
	if err != nil {
		return clientresources.ResourceDetail{}, err
	}
	if err := lockActiveLocation(
		ctx, tx, accepted.Target.MSPID, accepted.Target.ClientID,
		snapshot.LocationID,
	); err != nil {
		return clientresources.ResourceDetail{}, err
	}
	current, err := loadLockedClientResource(
		ctx, tx, kind, accepted.Target, accepted.ResourceID,
	)
	if err != nil {
		return clientresources.ResourceDetail{}, err
	}
	if !sameResourceLockSnapshot(snapshot, current) {
		return clientresources.ResourceDetail{}, object.ErrVersionConflict
	}
	return current, nil
}

type resourceLocationLock struct {
	id            string
	requireActive bool
}

func lockResourceUpdateLocations(
	ctx context.Context,
	tx transaction,
	target scope.Target,
	currentLocationID string,
	requestedLocationID *string,
) error {
	locks := resourceUpdateLocationLocks(currentLocationID, requestedLocationID)
	for _, lock := range locks {
		if err := lockLocationForShare(
			ctx, tx, target.MSPID, target.ClientID, lock.id,
			lock.requireActive,
		); err != nil {
			return err
		}
	}
	return nil
}

func resourceUpdateLocationLocks(
	currentLocationID string,
	requestedLocationID *string,
) []resourceLocationLock {
	targetLocationID := currentLocationID
	if requestedLocationID != nil {
		targetLocationID = *requestedLocationID
	}
	requirements := map[string]bool{}
	if currentLocationID != "" {
		requirements[currentLocationID] = targetLocationID == currentLocationID
	}
	if targetLocationID != "" {
		requirements[targetLocationID] = true
	}
	ids := make([]string, 0, len(requirements))
	for id := range requirements {
		ids = append(ids, id)
	}
	// A switch can touch two Locations; stable ordering prevents switch/switch
	// transactions from introducing another lock cycle.
	sort.Strings(ids)
	locks := make([]resourceLocationLock, 0, len(ids))
	for _, id := range ids {
		locks = append(locks, resourceLocationLock{
			id: id, requireActive: requirements[id],
		})
	}
	return locks
}

func locationBoundResource(kind clientresources.Kind) bool {
	return kind == clientresources.ContactKind ||
		kind == clientresources.AssetKind
}

func sameResourceLockSnapshot(
	snapshot clientresources.ResourceDetail,
	current clientresources.ResourceDetail,
) bool {
	return current.ID == snapshot.ID && current.Kind == snapshot.Kind &&
		current.Version == snapshot.Version &&
		current.LifecycleState == snapshot.LifecycleState &&
		current.LocationID == snapshot.LocationID
}

func updateLockedClientResource(
	ctx context.Context,
	tx transaction,
	kind clientresources.Kind,
	next clientresources.ResourceDetail,
	accepted clientresources.UpdateMutation,
) error {
	var (
		query string
		args  []any
	)
	prefix := []any{accepted.ResourceID, accepted.Target.MSPID, accepted.Target.ClientID, accepted.ExpectedVersion, "active"}
	switch kind {
	case clientresources.LocationKind:
		query = `UPDATE locations
SET name = $6, version = version + 1, updated_at = $7, updated_by = $8
WHERE id = $1::uuid AND msp_id = $2 AND client_id = $3::uuid
  AND version = $4 AND lifecycle_state = $5`
		args = append(prefix, next.Name, accepted.UpdatedAt, accepted.ActorID)
	case clientresources.ContactKind:
		query = `UPDATE contacts
SET display_name = $6, email = NULLIF($7, ''), phone = NULLIF($8, ''),
    location_id = NULLIF($9, '')::uuid, version = version + 1,
    updated_at = $10, updated_by = $11
WHERE id = $1::uuid AND msp_id = $2 AND client_id = $3::uuid
  AND version = $4 AND lifecycle_state = $5`
		args = append(prefix, next.Name, next.Email, next.Phone, next.LocationID, accepted.UpdatedAt, accepted.ActorID)
	case clientresources.AssetKind:
		query = `UPDATE assets
SET name = $6, asset_type = $7, location_id = NULLIF($8, '')::uuid,
    version = version + 1, updated_at = $9, updated_by = $10
WHERE id = $1::uuid AND msp_id = $2 AND client_id = $3::uuid
  AND version = $4 AND lifecycle_state = $5`
		args = append(prefix, next.Name, next.AssetType, next.LocationID, accepted.UpdatedAt, accepted.ActorID)
	case clientresources.ServiceKind:
		query = `UPDATE services
SET name = $6, criticality = $7, version = version + 1,
    updated_at = $8, updated_by = $9
WHERE id = $1::uuid AND msp_id = $2 AND client_id = $3::uuid
  AND version = $4 AND lifecycle_state = $5`
		args = append(prefix, next.Name, next.Criticality, accepted.UpdatedAt, accepted.ActorID)
	case clientresources.ContractKind:
		query = `UPDATE contracts
SET name = $6, starts_on = $7, ends_on = $8, version = version + 1,
    updated_at = $9, updated_by = $10
WHERE id = $1::uuid AND msp_id = $2 AND client_id = $3::uuid
  AND version = $4 AND lifecycle_state = $5`
		args = append(prefix, next.Name, next.StartsOn, next.EndsOn, accepted.UpdatedAt, accepted.ActorID)
	default:
		return clientresources.ErrInvalid
	}
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	return nil
}

func transitionLockedClientResource(
	ctx context.Context,
	tx transaction,
	kind clientresources.Kind,
	accepted clientresources.LifecycleMutation,
) error {
	table := clientResourceTable(kind)
	if table == "" {
		return clientresources.ErrInvalid
	}
	tag, err := tx.Exec(ctx, `UPDATE `+table+`
SET lifecycle_state = $6, version = version + 1,
    updated_at = $7, updated_by = $8
WHERE id = $1::uuid AND msp_id = $2 AND client_id = $3::uuid
  AND version = $4 AND lifecycle_state = $5`,
		accepted.ResourceID, accepted.Target.MSPID, accepted.Target.ClientID,
		accepted.ExpectedVersion, accepted.FromState, accepted.ToState,
		accepted.UpdatedAt, accepted.ActorID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	return nil
}

func clientResourceTable(kind clientresources.Kind) string {
	switch kind {
	case clientresources.LocationKind:
		return "locations"
	case clientresources.ContactKind:
		return "contacts"
	case clientresources.AssetKind:
		return "assets"
	case clientresources.ServiceKind:
		return "services"
	case clientresources.ContractKind:
		return "contracts"
	default:
		return ""
	}
}

func applyResourcePatch(
	kind clientresources.Kind,
	current clientresources.ResourceDetail,
	patch clientresources.UpdatePatch,
) (clientresources.ResourceDetail, map[string]any, map[string]any, error) {
	next := current
	switch kind {
	case clientresources.LocationKind:
		if patch.Name != nil {
			next.Name = *patch.Name
		}
	case clientresources.ContactKind:
		if patch.DisplayName != nil {
			next.Name = *patch.DisplayName
		}
		if patch.Email != nil {
			next.Email = *patch.Email
		}
		if patch.Phone != nil {
			next.Phone = *patch.Phone
		}
		if patch.LocationID != nil {
			next.LocationID = *patch.LocationID
		}
	case clientresources.AssetKind:
		if patch.Name != nil {
			next.Name = *patch.Name
		}
		if patch.AssetType != nil {
			next.AssetType = *patch.AssetType
		}
		if patch.LocationID != nil {
			next.LocationID = *patch.LocationID
		}
	case clientresources.ServiceKind:
		if patch.Name != nil {
			next.Name = *patch.Name
		}
		if patch.Criticality != nil {
			next.Criticality = *patch.Criticality
		}
	case clientresources.ContractKind:
		if patch.Name != nil {
			next.Name = *patch.Name
		}
		if patch.StartsOn != nil {
			value := patch.StartsOn.UTC()
			next.StartsOn = &value
		}
		if patch.EndsOn != nil {
			value := patch.EndsOn.UTC()
			next.EndsOn = &value
		}
		if patch.ClearEndsOn {
			next.EndsOn = nil
		}
		if next.StartsOn == nil || next.StartsOn.IsZero() ||
			(next.EndsOn != nil && next.EndsOn.Before(*next.StartsOn)) {
			return clientresources.ResourceDetail{}, nil, nil, clientresources.ErrInvalid
		}
	default:
		return clientresources.ResourceDetail{}, nil, nil, clientresources.ErrInvalid
	}
	before := resourceMutableValues(kind, current)
	after := resourceMutableValues(kind, next)
	next.Version++
	refreshResourceDetail(&next)
	return next, before, after, nil
}

func resourceMutableValues(kind clientresources.Kind, value clientresources.ResourceDetail) map[string]any {
	switch kind {
	case clientresources.LocationKind:
		return map[string]any{"name": value.Name}
	case clientresources.ContactKind:
		return map[string]any{"display_name": value.Name, "email": nullableResourceString(value.Email), "phone": nullableResourceString(value.Phone), "location_id": nullableResourceString(value.LocationID)}
	case clientresources.AssetKind:
		return map[string]any{"name": value.Name, "asset_type": value.AssetType, "location_id": nullableResourceString(value.LocationID)}
	case clientresources.ServiceKind:
		return map[string]any{"name": value.Name, "criticality": value.Criticality}
	case clientresources.ContractKind:
		return map[string]any{"name": value.Name, "starts_on": resourceDate(value.StartsOn), "ends_on": resourceDate(value.EndsOn)}
	default:
		return nil
	}
}

func resourceSafeDiff(kind clientresources.Kind, before, after map[string]any) mutation.SafeDiff {
	if kind == clientresources.ContactKind {
		return mutation.BuildSafeDiff(before, after, "email", "phone")
	}
	return mutation.BuildSafeDiff(before, after)
}

func sortedDiffFields(diff mutation.SafeDiff) []string {
	fields := make([]string, 0, len(diff))
	for field := range diff {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

func nullableResourceString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func resourceDate(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.DateOnly)
}

func refreshResourceDetail(value *clientresources.ResourceDetail) {
	switch clientresources.Kind(value.Kind) {
	case clientresources.ContactKind:
		value.Detail = value.Email
	case clientresources.AssetKind:
		value.Detail = value.AssetType
	case clientresources.ServiceKind:
		value.Detail = value.Criticality
	case clientresources.ContractKind:
		value.Detail = resourceDate(value.StartsOn).(string)
		if value.EndsOn != nil {
			value.Detail += " through " + resourceDate(value.EndsOn).(string)
		}
	}
}

func integrationOwnedAsset(kind clientresources.Kind, value clientresources.ResourceDetail) bool {
	return kind == clientresources.AssetKind &&
		(value.Authority == clientresources.Discovered || value.SourceSystem != "" || value.ExternalID != "")
}

func lockActiveLocationDependencies(
	ctx context.Context,
	tx transaction,
	target scope.Target,
	locationID string,
) error {
	for _, table := range []string{"contacts", "assets"} {
		var dependencyID string
		err := tx.QueryRow(ctx, `SELECT id::text
FROM `+table+`
WHERE msp_id = $1 AND client_id = $2::uuid AND location_id = $3::uuid
  AND lifecycle_state = 'active'
ORDER BY id
LIMIT 1
FOR SHARE`, target.MSPID, target.ClientID, locationID).Scan(&dependencyID)
		if err == nil {
			return clientresources.ErrResourceInUse
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	return nil
}

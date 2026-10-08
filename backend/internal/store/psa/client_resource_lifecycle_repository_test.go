package psa

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestResourceUpdateLocksActiveClientAndExactResourceThenWritesFacts(t *testing.T) {
	name := "Branch Office"
	tx := &fakeSalesTx{queryRows: []row{resourceDetailRow(resourceDetail(
		clientresources.LocationKind, "active", 7,
	))}}
	result, err := NewLocationRepository(&fakeSalesDB{tx: tx}).UpdateLocation(
		context.Background(), validResourceUpdate(clientresources.LocationKind, 7, clientresources.UpdatePatch{Name: &name}),
	)
	if err != nil {
		t.Fatalf("UpdateLocation() error=%v", err)
	}
	if result.Name != "Branch Office" || result.Version != 8 || result.LifecycleState != "active" {
		t.Fatalf("UpdateLocation() result=%+v", result)
	}
	assertQueryOrder(t, tx.calls,
		"FROM client_organizations", "FROM locations", "UPDATE locations", "INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	load := tx.calls[1]
	update := tx.calls[2]
	if !containsAll(load, "id = $1", "msp_id = $2", "client_id = $3", "FOR UPDATE") {
		t.Fatalf("resource load is not exact and locked: %s", load)
	}
	if !containsAll(update,
		"WHERE id = $1", "AND msp_id = $2", "AND client_id = $3", "AND version = $4", "AND lifecycle_state = $5", "version = version + 1",
	) {
		t.Fatalf("resource update lacks exact version/state predicate: %s", update)
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("atomic update did not commit exactly once: %+v", tx)
	}
}

func TestResourceUpdateUsesOnlyApprovedColumnsForEveryKind(t *testing.T) {
	name, displayName, assetType, criticality := "New Name", "New Contact", "server", "critical"
	startsOn := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	endsOn := startsOn.AddDate(1, 0, 0)
	tests := []struct {
		kind      clientresources.Kind
		patch     clientresources.UpdatePatch
		approved  []string
		forbidden []string
		call      func(database, clientresources.UpdateMutation) (clientresources.ResourceDetail, error)
	}{
		{clientresources.LocationKind, clientresources.UpdatePatch{Name: &name}, []string{"name = $6"}, []string{"display_id =", "client_id =", "source_system ="}, func(db database, value clientresources.UpdateMutation) (clientresources.ResourceDetail, error) {
			return NewLocationRepository(db).UpdateLocation(context.Background(), value)
		}},
		{clientresources.ContactKind, clientresources.UpdatePatch{DisplayName: &displayName}, []string{"display_name = $6", "email = NULLIF($7", "phone = NULLIF($8", "location_id = NULLIF($9"}, []string{"display_id =", "client_id =", "source_system ="}, func(db database, value clientresources.UpdateMutation) (clientresources.ResourceDetail, error) {
			return NewContactRepository(db).UpdateContact(context.Background(), value)
		}},
		{clientresources.AssetKind, clientresources.UpdatePatch{Name: &name, AssetType: &assetType}, []string{"name = $6", "asset_type = $7", "location_id = NULLIF($8"}, []string{"display_id =", "source_system =", "external_id =", "authority ="}, func(db database, value clientresources.UpdateMutation) (clientresources.ResourceDetail, error) {
			return NewAssetRepository(db).UpdateAsset(context.Background(), value)
		}},
		{clientresources.ServiceKind, clientresources.UpdatePatch{Name: &name, Criticality: &criticality}, []string{"name = $6", "criticality = $7"}, []string{"display_id =", "client_id =", "source_system ="}, func(db database, value clientresources.UpdateMutation) (clientresources.ResourceDetail, error) {
			return NewServiceResourceRepository(db).UpdateService(context.Background(), value)
		}},
		{clientresources.ContractKind, clientresources.UpdatePatch{Name: &name, StartsOn: &startsOn, EndsOn: &endsOn}, []string{"name = $6", "starts_on = $7", "ends_on = $8"}, []string{"display_id =", "client_id =", "source_system ="}, func(db database, value clientresources.UpdateMutation) (clientresources.ResourceDetail, error) {
			return NewContractRepository(db).UpdateContract(context.Background(), value)
		}},
	}
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			current := resourceDetail(test.kind, "active", 3)
			if test.kind == clientresources.AssetKind {
				current.Authority = clientresources.TechnicianConfirmed
			}
			if test.kind == clientresources.ContractKind {
				current.StartsOn, current.EndsOn = &startsOn, &endsOn
			}
			tx := &fakeSalesTx{queryRows: []row{resourceDetailRow(current)}}
			if test.kind == clientresources.ContactKind || test.kind == clientresources.AssetKind {
				tx.queryRows = append(tx.queryRows, resourceDetailRow(current))
			}
			if _, err := test.call(&fakeSalesDB{tx: tx}, validResourceUpdate(test.kind, 3, test.patch)); err != nil {
				t.Fatalf("update error=%v", err)
			}
			update := findCall(t, tx.calls, "UPDATE "+clientResourceTable(test.kind))
			for _, fragment := range test.approved {
				if !strings.Contains(update, fragment) {
					t.Errorf("approved field %q missing from %s", fragment, update)
				}
			}
			setClause := strings.Split(update, "WHERE")[0]
			for _, fragment := range test.forbidden {
				if strings.Contains(setClause, fragment) {
					t.Errorf("immutable field %q appeared in %s", fragment, update)
				}
			}
		})
	}
}

func TestResourceUpdateReturnsTypedVersionAndLifecycleConflictsBeforeFacts(t *testing.T) {
	name := "New Name"
	for _, test := range []struct {
		name     string
		current  clientresources.ResourceDetail
		expected int64
		want     error
	}{
		{"stale version", resourceDetail(clientresources.LocationKind, "active", 8), 7, object.ErrVersionConflict},
		{"inactive update", resourceDetail(clientresources.LocationKind, "inactive", 7), 7, clientresources.ErrLifecycleConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeSalesTx{queryRows: []row{resourceDetailRow(test.current)}}
			_, err := NewLocationRepository(&fakeSalesDB{tx: tx}).UpdateLocation(
				context.Background(), validResourceUpdate(clientresources.LocationKind, test.expected, clientresources.UpdatePatch{Name: &name}),
			)
			if !errors.Is(err, test.want) || tx.committed || !tx.rolledBack || len(tx.calls) != 2 {
				t.Fatalf("UpdateLocation() error=%v tx=%+v, want %v before facts", err, tx, test.want)
			}
		})
	}
}

func TestResourceUpdateRequiresExactlyOneVersionStateQualifiedRow(t *testing.T) {
	name := "New Name"
	current := resourceDetail(clientresources.LocationKind, "active", 7)
	tx := &fakeSalesTx{
		queryRows:  []row{resourceDetailRow(current)},
		zeroRowsAt: 2,
	}
	_, err := NewLocationRepository(&fakeSalesDB{tx: tx}).UpdateLocation(
		context.Background(), validResourceUpdate(clientresources.LocationKind, 7, clientresources.UpdatePatch{Name: &name}),
	)
	if !errors.Is(err, object.ErrVersionConflict) || tx.committed || !tx.rolledBack || len(tx.calls) != 3 {
		t.Fatalf("zero-row update was accepted: error=%v tx=%+v", err, tx)
	}
}

func TestResourceUpdateRollsBackPartialFactFailure(t *testing.T) {
	name := "New Name"
	current := resourceDetail(clientresources.LocationKind, "active", 7)
	tx := &fakeSalesTx{
		queryRows: []row{resourceDetailRow(current)},
		failAt:    3,
	}
	_, err := NewLocationRepository(&fakeSalesDB{tx: tx}).UpdateLocation(
		context.Background(), validResourceUpdate(clientresources.LocationKind, 7, clientresources.UpdatePatch{Name: &name}),
	)
	if err == nil || tx.committed || !tx.rolledBack || len(tx.calls) != 4 {
		t.Fatalf("partial audit failure was not atomic: error=%v tx=%+v", err, tx)
	}
}

func TestResourceUpdateRejectsSemanticNoOpBeforeFacts(t *testing.T) {
	name := "Headquarters"
	current := resourceDetail(clientresources.LocationKind, "active", 4)
	current.Name = name
	tx := &fakeSalesTx{queryRows: []row{resourceDetailRow(current)}}
	_, err := NewLocationRepository(&fakeSalesDB{tx: tx}).UpdateLocation(
		context.Background(), validResourceUpdate(clientresources.LocationKind, 4, clientresources.UpdatePatch{Name: &name}),
	)
	if !errors.Is(err, clientresources.ErrInvalid) || tx.committed || !tx.rolledBack || len(tx.calls) != 2 {
		t.Fatalf("semantic no-op wrote facts: error=%v tx=%+v", err, tx)
	}
}

func TestResourceUpdateValidatesActiveLocationAndPreservesExplicitClear(t *testing.T) {
	locationID := "new-location"
	current := resourceDetail(clientresources.ContactKind, "active", 2)
	tx := &fakeSalesTx{queryRows: []row{
		resourceDetailRow(current),
		resourceDetailRow(current),
	}}
	result, err := NewContactRepository(&fakeSalesDB{tx: tx}).UpdateContact(
		context.Background(), validResourceUpdate(clientresources.ContactKind, 2, clientresources.UpdatePatch{LocationID: &locationID}),
	)
	if err != nil || result.LocationID != locationID {
		t.Fatalf("UpdateContact() result=%+v error=%v", result, err)
	}
	assertQueryOrder(t, tx.calls,
		"FROM client_organizations", "FROM contacts", "FROM locations", "FROM contacts", "UPDATE contacts",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)

	empty := ""
	current.LocationID = locationID
	clearTx := &fakeSalesTx{queryRows: []row{
		resourceDetailRow(current),
		resourceDetailRow(current),
	}}
	cleared, err := NewContactRepository(&fakeSalesDB{tx: clearTx}).UpdateContact(
		context.Background(), validResourceUpdate(clientresources.ContactKind, 2, clientresources.UpdatePatch{LocationID: &empty}),
	)
	locationLocks := callIndexesContaining(clearTx.calls, "FROM locations")
	if err != nil || cleared.LocationID != "" || len(locationLocks) != 1 ||
		strings.Contains(clearTx.calls[locationLocks[0]], "lifecycle_state = 'active'") {
		t.Fatalf("explicit Location clear was not distinct from omission: result=%+v error=%v calls=%v", cleared, err, clearTx.calls)
	}
}

func TestContactAndAssetUpdateAndReactivateLockLocationBeforeExactResource(t *testing.T) {
	displayName, assetName := "Renamed Contact", "RENAMED-ASSET"
	tests := []struct {
		name  string
		kind  clientresources.Kind
		state string
		call  func(database) error
	}{
		{
			name: "contact update", kind: clientresources.ContactKind, state: "active",
			call: func(db database) error {
				_, err := NewContactRepository(db).UpdateContact(
					context.Background(),
					validResourceUpdate(clientresources.ContactKind, 3, clientresources.UpdatePatch{DisplayName: &displayName}),
				)
				return err
			},
		},
		{
			name: "asset update", kind: clientresources.AssetKind, state: "active",
			call: func(db database) error {
				_, err := NewAssetRepository(db).UpdateAsset(
					context.Background(),
					validResourceUpdate(clientresources.AssetKind, 3, clientresources.UpdatePatch{Name: &assetName}),
				)
				return err
			},
		},
		{
			name: "contact reactivate", kind: clientresources.ContactKind, state: "inactive",
			call: func(db database) error {
				_, err := NewContactRepository(db).ReactivateContact(
					context.Background(),
					validResourceLifecycle(clientresources.ContactKind, 3, "inactive", "active"),
				)
				return err
			},
		},
		{
			name: "asset reactivate", kind: clientresources.AssetKind, state: "inactive",
			call: func(db database) error {
				_, err := NewAssetRepository(db).ReactivateAsset(
					context.Background(),
					validResourceLifecycle(clientresources.AssetKind, 3, "inactive", "active"),
				)
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current := resourceDetail(test.kind, test.state, 3)
			current.LocationID = "location"
			tx := &fakeSalesTx{queryRows: []row{
				resourceDetailRow(current),
				resourceDetailRow(current),
			}}
			if err := test.call(&fakeSalesDB{tx: tx}); err != nil {
				t.Fatalf("%s error=%v", test.name, err)
			}
			resourceCalls := callIndexesContaining(tx.calls, "FROM "+clientResourceTable(test.kind))
			locationCalls := callIndexesContaining(tx.calls, "FROM locations")
			if len(resourceCalls) != 2 || len(locationCalls) != 1 ||
				resourceCalls[0] >= locationCalls[0] || locationCalls[0] >= resourceCalls[1] ||
				strings.Contains(tx.calls[resourceCalls[0]], "FOR UPDATE") ||
				!strings.Contains(tx.calls[locationCalls[0]], "FOR SHARE") ||
				!strings.Contains(tx.calls[locationCalls[0]], "lifecycle_state = 'active'") ||
				!strings.Contains(tx.calls[resourceCalls[1]], "FOR UPDATE") {
				t.Fatalf("cyclic resource/Location lock order: calls=%#v", tx.calls)
			}
		})
	}
}

func TestContactAndAssetLocationSwitchAndClearLockOldAndNewBeforeResource(t *testing.T) {
	newLocation, clearLocation := "a-location", ""
	tests := []struct {
		name     string
		kind     clientresources.Kind
		target   *string
		wantIDs  []string
		wantLive map[string]bool
		call     func(database, clientresources.UpdateMutation) error
	}{
		{
			name: "contact switch", kind: clientresources.ContactKind,
			target: &newLocation, wantIDs: []string{"a-location", "z-location"},
			wantLive: map[string]bool{"a-location": true, "z-location": false},
			call: func(db database, value clientresources.UpdateMutation) error {
				_, err := NewContactRepository(db).UpdateContact(context.Background(), value)
				return err
			},
		},
		{
			name: "contact clear", kind: clientresources.ContactKind,
			target: &clearLocation, wantIDs: []string{"z-location"},
			wantLive: map[string]bool{"z-location": false},
			call: func(db database, value clientresources.UpdateMutation) error {
				_, err := NewContactRepository(db).UpdateContact(context.Background(), value)
				return err
			},
		},
		{
			name: "asset switch", kind: clientresources.AssetKind,
			target: &newLocation, wantIDs: []string{"a-location", "z-location"},
			wantLive: map[string]bool{"a-location": true, "z-location": false},
			call: func(db database, value clientresources.UpdateMutation) error {
				_, err := NewAssetRepository(db).UpdateAsset(context.Background(), value)
				return err
			},
		},
		{
			name: "asset clear", kind: clientresources.AssetKind,
			target: &clearLocation, wantIDs: []string{"z-location"},
			wantLive: map[string]bool{"z-location": false},
			call: func(db database, value clientresources.UpdateMutation) error {
				_, err := NewAssetRepository(db).UpdateAsset(context.Background(), value)
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current := resourceDetail(test.kind, "active", 3)
			current.LocationID = "z-location"
			tx := &fakeSalesTx{queryRows: []row{
				resourceDetailRow(current),
				resourceDetailRow(current),
			}}
			mutation := validResourceUpdate(
				test.kind, 3, clientresources.UpdatePatch{LocationID: test.target},
			)
			if err := test.call(&fakeSalesDB{tx: tx}, mutation); err != nil {
				t.Fatalf("%s error=%v", test.name, err)
			}
			resourceCalls := callIndexesContaining(tx.calls, "FROM "+clientResourceTable(test.kind))
			locationCalls := callIndexesContaining(tx.calls, "FROM locations")
			locationQueryIndexes := queryIndexesContaining(tx.queries, "FROM locations")
			if len(resourceCalls) != 2 || len(locationCalls) != len(test.wantIDs) ||
				len(locationQueryIndexes) != len(test.wantIDs) {
				t.Fatalf("required Location locks missing: calls=%#v", tx.calls)
			}
			for index, queryIndex := range locationQueryIndexes {
				gotID, _ := tx.args[queryIndex][0].(string)
				if gotID != test.wantIDs[index] ||
					strings.Contains(tx.queries[queryIndex], "lifecycle_state = 'active'") != test.wantLive[gotID] {
					t.Fatalf("Location locks not deterministic/qualified: queries=%#v args=%#v", tx.queries, tx.args)
				}
			}
			if locationCalls[len(locationCalls)-1] >= resourceCalls[1] {
				t.Fatalf("Location lock followed exact resource lock: calls=%#v", tx.calls)
			}
		})
	}
}

func TestContactAndAssetMutationsRevalidateLocationAfterResourceLock(t *testing.T) {
	displayName := "Renamed Contact"
	tests := []struct {
		name  string
		kind  clientresources.Kind
		state string
		call  func(database) error
	}{
		{
			name: "contact update", kind: clientresources.ContactKind, state: "active",
			call: func(db database) error {
				_, err := NewContactRepository(db).UpdateContact(
					context.Background(),
					validResourceUpdate(clientresources.ContactKind, 3, clientresources.UpdatePatch{DisplayName: &displayName}),
				)
				return err
			},
		},
		{
			name: "asset update", kind: clientresources.AssetKind, state: "active",
			call: func(db database) error {
				_, err := NewAssetRepository(db).UpdateAsset(
					context.Background(),
					validResourceUpdate(clientresources.AssetKind, 3, clientresources.UpdatePatch{Name: &displayName}),
				)
				return err
			},
		},
		{
			name: "contact reactivate", kind: clientresources.ContactKind, state: "inactive",
			call: func(db database) error {
				_, err := NewContactRepository(db).ReactivateContact(
					context.Background(),
					validResourceLifecycle(clientresources.ContactKind, 3, "inactive", "active"),
				)
				return err
			},
		},
		{
			name: "asset reactivate", kind: clientresources.AssetKind, state: "inactive",
			call: func(db database) error {
				_, err := NewAssetRepository(db).ReactivateAsset(
					context.Background(),
					validResourceLifecycle(clientresources.AssetKind, 3, "inactive", "active"),
				)
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := resourceDetail(test.kind, test.state, 3)
			snapshot.LocationID = "old-location"
			locked := snapshot
			locked.LocationID = "changed-location"
			tx := &fakeSalesTx{queryRows: []row{
				resourceDetailRow(snapshot),
				resourceDetailRow(locked),
			}}
			err := test.call(&fakeSalesDB{tx: tx})
			if !errors.Is(err, object.ErrVersionConflict) || tx.committed || !tx.rolledBack ||
				strings.Contains(strings.Join(tx.calls, "\n"), "UPDATE "+clientResourceTable(test.kind)) {
				t.Fatalf("changed relationship was not revalidated: error=%v tx=%+v", err, tx)
			}
		})
	}
}

func TestResourceLifecycleRejectsDiscoveredOrDattoAssetAuthority(t *testing.T) {
	for _, current := range []clientresources.ResourceDetail{
		func() clientresources.ResourceDetail {
			value := resourceDetail(clientresources.AssetKind, "active", 5)
			value.Authority = clientresources.Discovered
			return value
		}(),
		func() clientresources.ResourceDetail {
			value := resourceDetail(clientresources.AssetKind, "active", 5)
			value.Authority = clientresources.TechnicianConfirmed
			value.SourceSystem = "datto"
			value.ExternalID = "device-1"
			return value
		}(),
	} {
		tx := &fakeSalesTx{queryRows: []row{resourceDetailRow(current)}}
		_, err := NewAssetRepository(&fakeSalesDB{tx: tx}).DeactivateAsset(
			context.Background(), validResourceLifecycle(clientresources.AssetKind, 5, "active", "inactive"),
		)
		if !errors.Is(err, clientresources.ErrResourceAuthorityConflict) || tx.committed || !tx.rolledBack || len(tx.calls) != 2 {
			t.Fatalf("integration-owned Asset reached lifecycle SQL: error=%v tx=%+v", err, tx)
		}
	}
}

func TestResourceUpdateRejectsDiscoveredAssetAuthority(t *testing.T) {
	name := "Renamed Asset"
	current := resourceDetail(clientresources.AssetKind, "active", 5)
	current.Authority = clientresources.Discovered
	tx := &fakeSalesTx{queryRows: []row{
		resourceDetailRow(current),
		resourceDetailRow(current),
	}}
	_, err := NewAssetRepository(&fakeSalesDB{tx: tx}).UpdateAsset(
		context.Background(), validResourceUpdate(clientresources.AssetKind, 5, clientresources.UpdatePatch{Name: &name}),
	)
	if !errors.Is(err, clientresources.ErrResourceAuthorityConflict) || tx.committed || !tx.rolledBack || len(tx.calls) != 3 {
		t.Fatalf("discovered Asset reached update SQL: error=%v tx=%+v", err, tx)
	}
}

func TestResourceLifecycleBlocksLocationWithActiveDependencies(t *testing.T) {
	current := resourceDetail(clientresources.LocationKind, "active", 4)
	tx := &fakeSalesTx{queryRows: []row{
		resourceDetailRow(current),
		fakeRow{scan: func(destinations ...any) { *destinations[0].(*string) = "contact-id" }},
	}}
	_, err := NewLocationRepository(&fakeSalesDB{tx: tx}).DeactivateLocation(
		context.Background(), validResourceLifecycle(clientresources.LocationKind, 4, "active", "inactive"),
	)
	if !errors.Is(err, clientresources.ErrResourceInUse) || tx.committed || !tx.rolledBack || len(tx.calls) != 3 ||
		!containsAll(tx.calls[1], "FROM locations", "FOR UPDATE") ||
		!containsAll(tx.calls[2], "FROM contacts", "lifecycle_state = 'active'", "FOR SHARE") {
		t.Fatalf("active Location dependency did not block mutation: error=%v tx=%+v", err, tx)
	}
}

func TestLifecyclePreflightIsReadOnlyAndChecksLocationDependencies(t *testing.T) {
	current := resourceDetail(clientresources.LocationKind, "active", 4)
	db := &fakeSalesDB{queryQueue: []row{
		resourceDetailRow(current),
		fakeRow{scan: func(destinations ...any) { *destinations[0].(*bool) = true }},
	}}
	_, err := NewLocationRepository(db).Preflight(context.Background(), clientresources.LifecyclePreflightMutation{
		Target: scope.Target{MSPID: "msp", ClientID: "client"}, Kind: clientresources.LocationKind, ResourceID: current.ID,
		ExpectedVersion: 4, Operation: clientresources.PreflightDeactivate,
	})
	if !errors.Is(err, clientresources.ErrResourceInUse) || db.beginCalls != 0 ||
		len(db.queries) != 2 || !containsAll(db.queries[0], "FROM locations", "client_organizations") ||
		!containsAll(db.queries[1], "FROM contacts", "FROM assets", "lifecycle_state = 'active'") {
		t.Fatalf("Preflight() error=%v db=%+v", err, db)
	}
}

func TestLifecyclePreflightChecksFinalContactLocationAndAssetAuthority(t *testing.T) {
	current := resourceDetail(clientresources.ContactKind, "active", 3)
	current.LocationID = "location-id"
	displayName := "Renamed"
	db := &fakeSalesDB{queryQueue: []row{
		resourceDetailRow(current),
		fakeRow{scan: func(destinations ...any) { *destinations[0].(*bool) = false }},
	}}
	_, err := NewContactRepository(db).Preflight(context.Background(), clientresources.LifecyclePreflightMutation{
		Target: scope.Target{MSPID: "msp", ClientID: "client"}, Kind: clientresources.ContactKind, ResourceID: current.ID,
		ExpectedVersion: 3, Patch: clientresources.UpdatePatch{DisplayName: &displayName},
		Operation: clientresources.PreflightUpdate,
	})
	if !errors.Is(err, scope.ErrNotFound) || db.beginCalls != 0 || len(db.queries) != 2 ||
		!containsAll(db.queries[1], "FROM locations", "lifecycle_state = 'active'") {
		t.Fatalf("Contact Preflight() error=%v db=%+v", err, db)
	}

	asset := resourceDetail(clientresources.AssetKind, "active", 2)
	asset.Authority = clientresources.Discovered
	assetDB := &fakeSalesDB{queryQueue: []row{resourceDetailRow(asset)}}
	_, err = NewAssetRepository(assetDB).Preflight(context.Background(), clientresources.LifecyclePreflightMutation{
		Target: scope.Target{MSPID: "msp", ClientID: "client"}, Kind: clientresources.AssetKind, ResourceID: asset.ID,
		ExpectedVersion: 2, Operation: clientresources.PreflightDeactivate,
	})
	if !errors.Is(err, clientresources.ErrResourceAuthorityConflict) ||
		assetDB.beginCalls != 0 || len(assetDB.queries) != 1 {
		t.Fatalf("Asset Preflight() error=%v db=%+v", err, assetDB)
	}
}

func TestResourceLifecycleChecksAssetDependencyAfterContacts(t *testing.T) {
	current := resourceDetail(clientresources.LocationKind, "active", 4)
	tx := &fakeSalesTx{queryRows: []row{
		resourceDetailRow(current),
		fakeRow{err: pgx.ErrNoRows},
		fakeRow{scan: func(destinations ...any) { *destinations[0].(*string) = "asset-id" }},
	}}
	_, err := NewLocationRepository(&fakeSalesDB{tx: tx}).DeactivateLocation(
		context.Background(), validResourceLifecycle(clientresources.LocationKind, 4, "active", "inactive"),
	)
	if !errors.Is(err, clientresources.ErrResourceInUse) || tx.committed || !tx.rolledBack || len(tx.calls) != 4 ||
		!containsAll(tx.calls[1], "FROM locations", "FOR UPDATE") ||
		!containsAll(tx.calls[2], "FROM contacts", "FOR SHARE") ||
		!containsAll(tx.calls[3], "FROM assets", "FOR SHARE") {
		t.Fatalf("active Asset dependency did not block mutation: error=%v tx=%+v", err, tx)
	}
}

func TestResourceUpdateValidatesFinalContractWindow(t *testing.T) {
	current := resourceDetail(clientresources.ContractKind, "active", 3)
	start := time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC)
	current.StartsOn = &start
	end := start.Add(-24 * time.Hour)
	tx := &fakeSalesTx{queryRows: []row{resourceDetailRow(current)}}
	_, err := NewContractRepository(&fakeSalesDB{tx: tx}).UpdateContract(
		context.Background(), validResourceUpdate(clientresources.ContractKind, 3, clientresources.UpdatePatch{EndsOn: &end}),
	)
	if !errors.Is(err, clientresources.ErrInvalid) || tx.committed || !tx.rolledBack || len(tx.calls) != 2 {
		t.Fatalf("invalid final Contract window reached update SQL: error=%v tx=%+v", err, tx)
	}
}

func TestResourceReactivateRequiresActiveRelationship(t *testing.T) {
	current := resourceDetail(clientresources.ContactKind, "inactive", 6)
	current.LocationID = "inactive-location"
	tx := &fakeSalesTx{queryRows: []row{resourceDetailRow(current)}, zeroRowsAt: 2}
	_, err := NewContactRepository(&fakeSalesDB{tx: tx}).ReactivateContact(
		context.Background(), validResourceLifecycle(clientresources.ContactKind, 6, "inactive", "active"),
	)
	if !errors.Is(err, scope.ErrNotFound) || tx.committed || !tx.rolledBack || len(tx.calls) != 3 ||
		!strings.Contains(tx.calls[2], "FROM locations") {
		t.Fatalf("inactive relationship did not block reactivation: error=%v tx=%+v", err, tx)
	}
}

func TestResourceLifecycleUsesVersionStatePredicateAndIncrementsVersion(t *testing.T) {
	current := resourceDetail(clientresources.ServiceKind, "inactive", 9)
	tx := &fakeSalesTx{queryRows: []row{resourceDetailRow(current)}}
	result, err := NewServiceResourceRepository(&fakeSalesDB{tx: tx}).ReactivateService(
		context.Background(), validResourceLifecycle(clientresources.ServiceKind, 9, "inactive", "active"),
	)
	if err != nil || result.Version != 10 || result.LifecycleState != "active" {
		t.Fatalf("ReactivateService() result=%+v error=%v", result, err)
	}
	update := tx.calls[2]
	if !containsAll(update, "UPDATE services", "lifecycle_state = $6", "version = version + 1", "WHERE id = $1", "AND msp_id = $2", "AND client_id = $3", "AND version = $4", "AND lifecycle_state = $5") {
		t.Fatalf("lifecycle update lacks version/state predicate: %s", update)
	}
}

func TestResourceUpdateWritesRedactedAuditAndMinimizedOutbox(t *testing.T) {
	email, phone := "new@example.com", "555-0199"
	current := resourceDetail(clientresources.ContactKind, "active", 3)
	current.Email, current.Phone = "old@example.com", "555-0100"
	tx := &fakeSalesTx{queryRows: []row{
		resourceDetailRow(current),
		resourceDetailRow(current),
	}}
	_, err := NewContactRepository(&fakeSalesDB{tx: tx}).UpdateContact(
		context.Background(), validResourceUpdate(clientresources.ContactKind, 3, clientresources.UpdatePatch{Email: &email, Phone: &phone}),
	)
	if err != nil {
		t.Fatalf("UpdateContact() error=%v", err)
	}
	if len(tx.args) < 4 {
		t.Fatalf("missing atomic fact arguments: %+v", tx.args)
	}
	facts := ""
	for _, call := range tx.args[2:] {
		for _, arg := range call {
			switch value := arg.(type) {
			case string:
				facts += value
			case []byte:
				facts += string(value)
			}
		}
	}
	for _, secret := range []string{"old@example.com", "new@example.com", "555-0100", "555-0199"} {
		if strings.Contains(facts, secret) {
			t.Fatalf("audit/outbox facts leaked %q: %s", secret, facts)
		}
	}
	if !strings.Contains(facts, `"email":{"changed":true,"redacted":true}`) ||
		!strings.Contains(facts, `"changed_fields":["email","phone"]`) {
		t.Fatalf("safe/minimized evidence missing: %s", facts)
	}
}

func TestResourceLifecycleHidesWrongScopeOrKindAsNotFound(t *testing.T) {
	tx := &fakeSalesTx{queryRows: []row{fakeRow{err: pgx.ErrNoRows}}}
	_, err := NewContractRepository(&fakeSalesDB{tx: tx}).DeactivateContract(
		context.Background(), validResourceLifecycle(clientresources.ContractKind, 1, "active", "inactive"),
	)
	if !errors.Is(err, scope.ErrNotFound) || tx.committed || !tx.rolledBack || len(tx.calls) != 2 {
		t.Fatalf("missing scoped resource was distinguishable: error=%v tx=%+v", err, tx)
	}
}

func TestResourceLifecycleWrappersRejectInverseDirectionAndMismatchedFactsBeforeBegin(t *testing.T) {
	type lifecycleCall func(database, clientresources.LifecycleMutation) error
	tests := []struct {
		name, action, from, to string
		kind                   clientresources.Kind
		call                   lifecycleCall
	}{
		{"deactivate location", "location.deactivated", "active", "inactive", clientresources.LocationKind, func(db database, value clientresources.LifecycleMutation) error {
			_, err := NewLocationRepository(db).DeactivateLocation(context.Background(), value)
			return err
		}},
		{"reactivate location", "location.reactivated", "inactive", "active", clientresources.LocationKind, func(db database, value clientresources.LifecycleMutation) error {
			_, err := NewLocationRepository(db).ReactivateLocation(context.Background(), value)
			return err
		}},
		{"deactivate contact", "contact.deactivated", "active", "inactive", clientresources.ContactKind, func(db database, value clientresources.LifecycleMutation) error {
			_, err := NewContactRepository(db).DeactivateContact(context.Background(), value)
			return err
		}},
		{"reactivate contact", "contact.reactivated", "inactive", "active", clientresources.ContactKind, func(db database, value clientresources.LifecycleMutation) error {
			_, err := NewContactRepository(db).ReactivateContact(context.Background(), value)
			return err
		}},
		{"deactivate asset", "asset.deactivated", "active", "inactive", clientresources.AssetKind, func(db database, value clientresources.LifecycleMutation) error {
			_, err := NewAssetRepository(db).DeactivateAsset(context.Background(), value)
			return err
		}},
		{"reactivate asset", "asset.reactivated", "inactive", "active", clientresources.AssetKind, func(db database, value clientresources.LifecycleMutation) error {
			_, err := NewAssetRepository(db).ReactivateAsset(context.Background(), value)
			return err
		}},
		{"deactivate service", "service.deactivated", "active", "inactive", clientresources.ServiceKind, func(db database, value clientresources.LifecycleMutation) error {
			_, err := NewServiceResourceRepository(db).DeactivateService(context.Background(), value)
			return err
		}},
		{"reactivate service", "service.reactivated", "inactive", "active", clientresources.ServiceKind, func(db database, value clientresources.LifecycleMutation) error {
			_, err := NewServiceResourceRepository(db).ReactivateService(context.Background(), value)
			return err
		}},
		{"deactivate contract", "contract.deactivated", "active", "inactive", clientresources.ContractKind, func(db database, value clientresources.LifecycleMutation) error {
			_, err := NewContractRepository(db).DeactivateContract(context.Background(), value)
			return err
		}},
		{"reactivate contract", "contract.reactivated", "inactive", "active", clientresources.ContractKind, func(db database, value clientresources.LifecycleMutation) error {
			_, err := NewContractRepository(db).ReactivateContract(context.Background(), value)
			return err
		}},
	}
	for _, test := range tests {
		for _, invalid := range []struct {
			name   string
			mutate func(*clientresources.LifecycleMutation)
		}{
			{"inverse direction", func(value *clientresources.LifecycleMutation) { value.FromState, value.ToState = test.to, test.from }},
			{"audit action", func(value *clientresources.LifecycleMutation) { value.Audit.Action = "wrong.action" }},
			{"event action", func(value *clientresources.LifecycleMutation) { value.Event.EventType = "wrong.action" }},
		} {
			t.Run(test.name+"/"+invalid.name, func(t *testing.T) {
				value := validResourceLifecycle(test.kind, 3, test.from, test.to)
				if value.Audit.Action != test.action || value.Event.EventType != test.action {
					t.Fatalf("test fixture action=%q/%q, want %q", value.Audit.Action, value.Event.EventType, test.action)
				}
				invalid.mutate(&value)
				db := &fakeSalesDB{tx: &fakeSalesTx{}}
				if err := test.call(db, value); !errors.Is(err, clientresources.ErrInvalid) || db.beginCalls != 0 {
					t.Fatalf("wrong lifecycle wrapper accepted metadata: error=%v begins=%d mutation=%+v", err, db.beginCalls, value)
				}
			})
		}
	}
}

func validResourceUpdate(kind clientresources.Kind, version int64, patch clientresources.UpdatePatch) clientresources.UpdateMutation {
	at := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	return clientresources.UpdateMutation{
		Target: scope.Target{MSPID: "msp", ClientID: "client"}, ResourceID: "resource", ExpectedVersion: version,
		Patch: patch, ActorID: "actor", UpdatedAt: at,
		Audit: mutation.AuditRecord{ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client", ActorType: "technician", ActorID: "actor", Action: string(kind) + ".updated", SubjectType: string(kind), SubjectID: "resource", SubjectVersion: version + 1, Source: "api", Reason: "correction", CorrelationID: "correlation"},
		Event: mutation.EventRecord{EventID: "event", EventType: string(kind) + ".updated", SchemaVersion: 1, OccurredAt: at, MSPID: "msp", ClientID: "client", ActorType: "technician", ActorID: "actor", SubjectType: string(kind), SubjectID: "resource", SubjectVersion: version + 1, Source: "api", CorrelationID: "correlation"},
	}
}

func validResourceLifecycle(kind clientresources.Kind, version int64, from, to string) clientresources.LifecycleMutation {
	at := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	action := string(kind) + ".deactivated"
	if to == "active" {
		action = string(kind) + ".reactivated"
	}
	return clientresources.LifecycleMutation{
		Target: scope.Target{MSPID: "msp", ClientID: "client"}, ResourceID: "resource", ExpectedVersion: version,
		FromState: from, ToState: to, ActorID: "actor", UpdatedAt: at,
		Audit: mutation.AuditRecord{ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client", ActorType: "technician", ActorID: "actor", Action: action, SubjectType: string(kind), SubjectID: "resource", SubjectVersion: version + 1, Source: "api", Reason: "correction", CorrelationID: "correlation"},
		Event: mutation.EventRecord{EventID: "event", EventType: action, SchemaVersion: 1, OccurredAt: at, MSPID: "msp", ClientID: "client", ActorType: "technician", ActorID: "actor", SubjectType: string(kind), SubjectID: "resource", SubjectVersion: version + 1, Source: "api", CorrelationID: "correlation"},
	}
}

func resourceDetail(kind clientresources.Kind, state string, version int64) clientresources.ResourceDetail {
	startsOn := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	value := clientresources.ResourceDetail{
		Summary: clientresources.Summary{ID: "resource", Kind: string(kind), DisplayID: "RESOURCE-1", Name: "Headquarters", Version: version, LifecycleState: state},
	}
	switch kind {
	case clientresources.ContactKind:
		value.Name = "Alex Client"
	case clientresources.AssetKind:
		value.Name, value.AssetType, value.Authority = "MAIL01", "server", clientresources.TechnicianConfirmed
	case clientresources.ServiceKind:
		value.Name, value.Criticality = "Email", "high"
	case clientresources.ContractKind:
		value.Name, value.StartsOn = "Managed Services", &startsOn
	}
	return value
}

func resourceDetailRow(value clientresources.ResourceDetail) row {
	return fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*string) = value.ID
		*destinations[1].(*string) = value.Kind
		*destinations[2].(*string) = value.DisplayID
		*destinations[3].(*string) = value.Name
		*destinations[4].(*string) = value.Detail
		*destinations[5].(*string) = value.LocationID
		*destinations[6].(*int64) = value.Version
		*destinations[7].(*string) = value.LifecycleState
		*destinations[8].(*string) = value.Email
		*destinations[9].(*string) = value.Phone
		*destinations[10].(*string) = value.AssetType
		*destinations[11].(*string) = value.SourceSystem
		*destinations[12].(*string) = value.ExternalID
		*destinations[13].(*clientresources.Authority) = value.Authority
		*destinations[14].(*string) = value.Criticality
		*destinations[15].(**time.Time) = value.StartsOn
		*destinations[16].(**time.Time) = value.EndsOn
	}}
}

func callIndexesContaining(calls []string, fragment string) []int {
	var indexes []int
	for index, call := range calls {
		if strings.Contains(call, fragment) {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func queryIndexesContaining(queries []string, fragment string) []int {
	return callIndexesContaining(queries, fragment)
}

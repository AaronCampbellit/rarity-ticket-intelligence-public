package psa

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestClientResourceCatalogListsAllKindsInsideClientScope(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	_, err := NewClientResourceCatalogRepository(db).ListCatalog(
		context.Background(),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		500,
		clientresources.CatalogAll,
		[]clientresources.Kind{clientresources.AssetKind},
	)
	if err != nil {
		t.Fatalf("ListCatalog() error=%v", err)
	}
	for _, required := range []string{
		"FROM locations", "FROM contacts", "FROM assets", "FROM services",
		"FROM contracts", "msp_id = $1", "client_id = $2::uuid",
		"lifecycle_state = $4", "$4 = 'all'", "LIMIT $3",
		"''::text, COALESCE(location_id::text, '')",
		"authority::text", "lifecycle_state, authority",
	} {
		if !strings.Contains(db.query, required) {
			t.Fatalf("catalog query missing %q: %s", required, db.query)
		}
	}
	if len(db.args) != 9 || db.args[3] != clientresources.CatalogAll ||
		db.args[5] != false || db.args[6] != true {
		t.Fatalf("catalog lifecycle filter is not safely bound: args=%v", db.args)
	}
}

func TestClientResourceCatalogReturnsMinimizedLifecycleFields(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*destinations[0].(*string) = "asset-id"
			*destinations[1].(*string) = "asset"
			*destinations[2].(*string) = "AST-1"
			*destinations[3].(*string) = "Firewall"
			*destinations[4].(*string) = "firewall"
			*destinations[5].(*string) = ""
			*destinations[6].(*string) = "inactive"
			*destinations[7].(*clientresources.Authority) = clientresources.Discovered
			*destinations[8].(*int64) = 4
		},
	}}}
	items, err := NewClientResourceCatalogRepository(db).ListCatalog(
		context.Background(),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		500,
		clientresources.CatalogAll,
		[]clientresources.Kind{clientresources.AssetKind},
	)
	if err != nil || len(items) != 1 || items[0].LifecycleState != "inactive" ||
		items[0].Authority != clientresources.Discovered || items[0].Version != 4 {
		t.Fatalf("ListCatalog() items=%+v error=%v", items, err)
	}
}

func TestClientResourceCatalogQueriesOnlyRequestedKindInsideClientScope(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		resourceDetailScan("asset-id", "asset", "AST-1", "Firewall", "active"),
	}}}
	items, err := NewClientResourceCatalogRepository(db).QueryResources(
		context.Background(),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		clientresources.Query{Kind: clientresources.AssetKind, Literal: "fire", Limit: 50},
	)
	if err != nil || len(items) != 1 || items[0].AssetType != "firewall" ||
		items[0].LifecycleState != "active" ||
		items[0].Authority != clientresources.Discovered {
		t.Fatalf("QueryResources() items=%+v error=%v", items, err)
	}
	for _, required := range []string{
		"FROM assets", "msp_id = $1", "client_id = $2::uuid",
		"FROM client_organizations", "id = $2::uuid",
		"lifecycle_state = 'active'", "LIMIT $5",
	} {
		if !strings.Contains(db.query, required) {
			t.Fatalf("resource query missing %q: %s", required, db.query)
		}
	}
	if strings.Contains(db.query, "FROM locations") || len(db.args) != 5 ||
		db.args[0] != "msp-id" || db.args[1] != "client-id" ||
		db.args[2] != "fire" || db.args[3] != false || db.args[4] != 50 {
		t.Fatalf("resource query is not fixed and safely bound: query=%s args=%v", db.query, db.args)
	}
}

func TestClientResourceCatalogResolveIsExactDisplayIDFirstAndSQLBounded(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		resourceDetailScan("asset-id", "asset", "AST-501", "Firewall", "active"),
	}}}
	items, err := NewClientResourceCatalogRepository(db).ResolveResource(
		context.Background(),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		clientresources.AssetKind, "AST-501", false,
	)
	if err != nil || len(items) != 1 || items[0].ID != "asset-id" {
		t.Fatalf("ResolveResource() items=%+v error=%v", items, err)
	}
	for _, required := range []string{
		"display_id = $3",
		"regexp_replace(lower(name), '\\s+', ' ', 'g')",
		"NOT EXISTS",
		"LIMIT 2",
		"lifecycle_state = 'active'",
	} {
		if !strings.Contains(db.query, required) {
			t.Fatalf("exact resolver query missing %q: %s", required, db.query)
		}
	}
	if strings.Contains(db.query, "strpos(") || len(db.args) != 4 ||
		db.args[2] != "AST-501" || db.args[3] != false {
		t.Fatalf("resolver is broad or incorrectly bound: query=%s args=%v", db.query, db.args)
	}
}

func TestClientResourceCatalogGetsExactResourceWithLifecycleOption(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: resourceDetailScan(
		"location-id", "location", "LOC-1", "Old Office", "inactive",
	)}}
	item, err := NewClientResourceCatalogRepository(db).GetResource(
		context.Background(),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		clientresources.LocationKind, "location-id", true,
	)
	if err != nil || item.LifecycleState != "inactive" {
		t.Fatalf("GetResource() item=%+v error=%v", item, err)
	}
	for _, required := range []string{
		"FROM locations", "id = $3::uuid", "msp_id = $1",
		"client_id = $2::uuid", "$4 OR lifecycle_state = 'active'",
	} {
		if !strings.Contains(db.query, required) {
			t.Fatalf("resource get missing %q: %s", required, db.query)
		}
	}
	if len(db.args) != 4 || db.args[0] != "msp-id" ||
		db.args[1] != "client-id" || db.args[2] != "location-id" ||
		db.args[3] != true {
		t.Fatalf("resource get is not exact and safely bound: args=%v", db.args)
	}
}

func TestClientResourceCatalogNormalizesWhitespaceInBoundQuery(t *testing.T) {
	sql, ok := clientResourceQuerySQL(clientresources.LocationKind)
	if !ok {
		t.Fatal("clientResourceQuerySQL() did not return location SQL")
	}
	if !strings.Contains(sql, "regexp_replace(lower($3), '\\s+', ' ', 'g')") ||
		strings.Contains(sql, "regexp_replace(lower($3), '\\\\s+', ' ', 'g')") {
		t.Fatalf("resource query does not normalize PostgreSQL whitespace expression: %s", sql)
	}
}

func TestClientResourceCatalogContactQueryUsesContactNameColumn(t *testing.T) {
	sql, ok := clientResourceQuerySQL(clientresources.ContactKind)
	if !ok {
		t.Fatal("clientResourceQuerySQL() did not return contact SQL")
	}
	for _, required := range []string{
		"lower(display_id || ' ' || display_name)",
		"ORDER BY display_name, display_id, id",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("contact resource query missing %q: %s", required, sql)
		}
	}
}

func resourceDetailScan(
	id string,
	kind string,
	displayID string,
	name string,
	lifecycle string,
) func(...any) {
	return func(destinations ...any) {
		startsOn := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
		*destinations[0].(*string) = id
		*destinations[1].(*string) = kind
		*destinations[2].(*string) = displayID
		*destinations[3].(*string) = name
		*destinations[4].(*string) = "detail"
		*destinations[5].(*string) = "location-id"
		*destinations[6].(*int64) = 2
		*destinations[7].(*string) = lifecycle
		*destinations[8].(*string) = "person@example.test"
		*destinations[9].(*string) = "555-0100"
		*destinations[10].(*string) = "firewall"
		*destinations[11].(*string) = "datto"
		*destinations[12].(*string) = "external-id"
		*destinations[13].(*clientresources.Authority) = clientresources.Discovered
		*destinations[14].(*string) = "high"
		*destinations[15].(**time.Time) = &startsOn
		*destinations[16].(**time.Time) = nil
	}
}

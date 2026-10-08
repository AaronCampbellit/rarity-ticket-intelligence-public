package psa

import (
	"context"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestSearchScopesEveryProjectionToMSPAndClient(t *testing.T) {
	rows := &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*destinations[0].(*string) = "work-id"
			*destinations[1].(*string) = "work_record"
			*destinations[2].(*string) = "client"
			*destinations[3].(*string) = "Email outage"
			*destinations[4].(*string) = "Multiple users affected"
		},
	}}
	db := &fakeSalesDB{queryRows: rows}
	results, err := NewSearchRepository(db).Search(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"email outage",
		25,
	)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].ClientID != "client" {
		t.Fatalf("unexpected results: %+v", results)
	}
	if !strings.Contains(db.query, "msp_id = $1 AND client_id = $2") ||
		!strings.Contains(db.query, "LIMIT $4") {
		t.Fatalf("search query lacks bounded scope: %s", db.query)
	}
	if len(db.args) != 4 || db.args[0] != "msp" ||
		db.args[1] != "client" || db.args[2] != "email outage" ||
		db.args[3] != 25 {
		t.Fatalf("unexpected query args: %+v", db.args)
	}
}

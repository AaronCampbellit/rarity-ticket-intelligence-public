package psa

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestAssignmentCandidateDirectoryResolvesOnlyActiveScopedExactCandidates(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			values := []any{"technician-id", "msp-id", "Taylor  Jones", "taylor@example.com", int64(7)}
			for index, value := range values {
				setScanDestination(destinations[index], value)
			}
		},
	}}}
	candidates, err := NewTechnicianDirectoryRepository(db).ResolveAssignmentCandidates(
		context.Background(), scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		"  TAYLOR\tJONES ", 10,
	)
	if err != nil || len(candidates) != 1 || candidates[0].ID != "technician-id" ||
		candidates[0].Version != 7 {
		t.Fatalf("ResolveAssignmentCandidates() candidates=%+v error=%v", candidates, err)
	}
	for _, fragment := range []string{
		"t.lifecycle_state = 'active'",
		"lower(regexp_replace(btrim(t.display_name), '\\s+', ' ', 'g')) = $3",
		"lower(regexp_replace(btrim(t.email), '\\s+', ' ', 'g')) = $3",
		"ra.technician_id = t.id AND ra.msp_id = t.msp_id",
		"ra.client_id IS NULL OR ra.client_id = $2",
		"ra.expires_at IS NULL OR ra.expires_at > $4",
		"ORDER BY t.id", "LIMIT $5",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("candidate query missing %q: %s", fragment, db.query)
		}
	}
	if len(db.args) != 5 || db.args[0] != "msp-id" || db.args[1] != "client-id" ||
		db.args[2] != "taylor jones" || db.args[4] != 2 {
		t.Fatalf("candidate query args=%+v", db.args)
	}
	if at, ok := db.args[3].(time.Time); !ok || at.IsZero() {
		t.Fatalf("candidate expiry reference=%#v", db.args[3])
	}
}

func TestAssignmentCandidateDirectoryFindsCurrentOwnerIdentityWithoutActiveScope(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		values := []any{"technician-id", "msp-id", "Former Owner", "former@example.test", int64(4)}
		for index, value := range values {
			setScanDestination(destinations[index], value)
		}
	}}}
	candidate, err := NewTechnicianDirectoryRepository(db).FindAssignmentIdentity(
		context.Background(), scope.Target{MSPID: "msp-id", ClientID: "client-id"}, "technician-id",
	)
	if err != nil || candidate.DisplayName != "Former Owner" || candidate.Version != 4 {
		t.Fatalf("FindAssignmentIdentity() candidate=%+v error=%v", candidate, err)
	}
	if !strings.Contains(db.query, "id = $1::uuid AND msp_id = $2") || strings.Contains(db.query, "lifecycle_state") || len(db.args) != 2 {
		t.Fatalf("current-owner identity query=%s args=%+v", db.query, db.args)
	}
}

package psa

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
)

func TestSnapshotStorePersistsContentAddressedImmutableBytes(t *testing.T) {
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*string) = "snapshot-id"
		*destinations[1].(*string) = "proposal-version"
		*destinations[2].(*string) = "hash"
		*destinations[3].(*time.Time) = at
	}}}
	store := NewSnapshotStore(db, func() time.Time { return at }, func() string { return "snapshot-id" })

	snapshot, err := store.PutImmutable(context.Background(), sales.SnapshotInput{
		ProposalVersionID: "proposal-version",
		Data:              []byte("rendered proposal"),
	})

	if err != nil {
		t.Fatalf("PutImmutable() error = %v", err)
	}
	if snapshot.ID != "snapshot-id" || snapshot.ProposalVersionID != "proposal-version" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if !strings.Contains(db.query, "INSERT INTO proposal_pdf_snapshots") ||
		!strings.Contains(db.query, "ON CONFLICT DO NOTHING") {
		t.Fatalf("snapshot write is not immutable: %s", db.query)
	}
	if len(db.args) != 5 || db.args[2] != "1223e4b4b1e000d5c0917b38dc58d8bc7bab2defcab6f06185c75ee8abb647a4" {
		t.Fatalf("snapshot hash args = %#v", db.args)
	}
}

func TestSnapshotStoreRejectsEmptyInput(t *testing.T) {
	store := NewSnapshotStore(&fakeSalesDB{}, time.Now, func() string { return "snapshot" })
	if _, err := store.PutImmutable(context.Background(), sales.SnapshotInput{}); err == nil {
		t.Fatal("PutImmutable() unexpectedly accepted empty input")
	}
}

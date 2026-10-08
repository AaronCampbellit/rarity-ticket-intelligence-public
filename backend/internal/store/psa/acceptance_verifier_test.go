package psa

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestAcceptanceVerifierConsumesExactBoundGrantAtomically(t *testing.T) {
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*string) = "grant-id"
		*destinations[1].(*string) = "Alex Client"
		*destinations[2].(*string) = "alex@example.com"
		*destinations[3].(*[]byte) = []byte(`{"challenge":"verified"}`)
		*destinations[4].(*time.Time) = at
	}}}
	verifier := NewAcceptanceVerifier(db, func() time.Time { return at })

	verified, err := verifier.VerifyAndConsume(
		context.Background(),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		"version-id", "opaque-grant",
	)
	if err != nil {
		t.Fatalf("VerifyAndConsume() error = %v", err)
	}
	if verified.GrantID != "grant-id" || verified.Evidence["challenge"] != "verified" {
		t.Fatalf("unexpected verified grant: %+v", verified)
	}
	if !strings.Contains(db.query, "UPDATE proposal_acceptance_grants") ||
		!strings.Contains(db.query, "consumed_at IS NULL") ||
		!strings.Contains(db.query, "expires_at > $5") {
		t.Fatalf("grant consumption is not atomic and bounded: %s", db.query)
	}
	if len(db.args) != 5 || db.args[1] != "version-id" ||
		db.args[2] != "msp-id" || db.args[3] != "client-id" {
		t.Fatalf("grant binding args = %#v", db.args)
	}
}

func TestAcceptanceVerifierHidesExpiredReplayedAndMismatchedGrants(t *testing.T) {
	verifier := NewAcceptanceVerifier(
		&fakeSalesDB{queryRow: fakeRow{err: pgx.ErrNoRows}},
		time.Now,
	)
	_, err := verifier.VerifyAndConsume(
		context.Background(),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		"version-id", "opaque-grant",
	)
	if !errors.Is(err, sales.ErrInvalidAcceptanceGrant) {
		t.Fatalf("VerifyAndConsume() error = %v", err)
	}
}

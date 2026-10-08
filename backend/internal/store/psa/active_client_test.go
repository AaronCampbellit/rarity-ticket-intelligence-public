package psa

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestLockActiveClientHoldsLifecycleStableInsideMutationTransaction(t *testing.T) {
	target := scope.Target{
		MSPID:    "11111111-1111-4111-8111-111111111111",
		ClientID: "22222222-2222-4222-8222-222222222222",
	}
	tx := &fakeSalesTx{}

	if err := lockActiveClient(
		context.Background(), tx, target.MSPID, target.ClientID,
	); err != nil {
		t.Fatalf("lockActiveClient() error=%v", err)
	}
	if len(tx.queries) != 1 ||
		!strings.Contains(tx.queries[0], "FROM client_organizations") ||
		!strings.Contains(tx.queries[0], "lifecycle_state = 'active'") ||
		!strings.Contains(tx.queries[0], "FOR SHARE") {
		t.Fatalf("active Client lock query=%q", tx.queries)
	}
	if len(tx.args[0]) != 2 ||
		tx.args[0][0] != target.MSPID ||
		tx.args[0][1] != target.ClientID {
		t.Fatalf("active Client lock args=%v", tx.args)
	}

	inactive := &fakeSalesTx{zeroRowsAt: 1}
	err := lockActiveClient(
		context.Background(), inactive, target.MSPID, target.ClientID,
	)
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("inactive Client error=%v, want ErrNotFound", err)
	}
}

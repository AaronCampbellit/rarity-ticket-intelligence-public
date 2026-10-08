package psa

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/intake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
)

func TestForwardingManagementCreateWritesConfigurationAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 30, 20, 0, 0, 0, time.UTC)
	mutation := intake.ForwardingManagementMutation{
		MSPID: "msp-id",
		Connection: intake.ManagedForwardingConnection{
			ID: "connection-id", IntakeAddress: "intake@example.com",
			AllowedSenderDomains: []string{"customer.example"},
			MaxMessageBytes:      25 << 20, RateLimitPerMinute: 60,
			Enabled: true, HealthState: "pending", Version: 1, UpdatedAt: at,
		},
		Audit: validAudit(at, "forwarding.connection.created", "forwarding_connection", "connection-id"),
		Event: validEvent(at, "forwarding.connection.created", "forwarding_connection", "connection-id"),
	}
	if err := NewForwardingRepository(&fakeSalesDB{tx: tx}).CreateManagedForwarding(context.Background(), mutation); err != nil {
		t.Fatalf("CreateManagedForwarding() error = %v", err)
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("transaction committed=%v rolledBack=%v", tx.committed, tx.rolledBack)
	}
	assertQueryOrder(t, tx.queries, "INSERT INTO forwarding_intake_connections", "INSERT INTO audit_ledger", "INSERT INTO event_outbox")
}

func TestForwardingManagementUpdateUsesMSPAndOptimisticVersion(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 1}
	mutation := intake.ForwardingManagementMutation{
		MSPID: "msp-id", ExpectedVersion: 2,
		Connection: intake.ManagedForwardingConnection{ID: "connection-id"},
		Audit:      validAudit(time.Now(), "forwarding.connection.updated", "forwarding_connection", "connection-id"),
		Event:      validEvent(time.Now(), "forwarding.connection.updated", "forwarding_connection", "connection-id"),
	}
	err := NewForwardingRepository(&fakeSalesDB{tx: tx}).UpdateManagedForwarding(context.Background(), mutation)
	if err != object.ErrVersionConflict || !tx.rolledBack || tx.committed {
		t.Fatalf("UpdateManagedForwarding() error=%v committed=%v rolledBack=%v", err, tx.committed, tx.rolledBack)
	}
	if !strings.Contains(tx.queries[0], "id = $1 AND msp_id = $2 AND version = $3") {
		t.Fatalf("update missing scope/version fence: %s", tx.queries[0])
	}
}

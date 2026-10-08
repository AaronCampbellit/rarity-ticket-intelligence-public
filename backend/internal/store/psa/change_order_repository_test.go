package psa

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestCreateChangeOrderAtomicWritesDraftAndFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewChangeOrderRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	err := repository.CreateChangeOrderAtomic(context.Background(), projects.CreateChangeOrderMutation{
		Order: projects.ChangeOrder{
			ID: "order", ProjectID: "project", MSPID: "msp", ClientID: "client",
			DisplayID: "CO-1", State: projects.ChangeOrderDraft, Version: 1,
			CreatedAt: at, CreatedBy: "actor", UpdatedAt: at, UpdatedBy: "actor",
		},
		Audit: validAudit(at, "change_order.created", "change_order", "order"),
		Event: validEvent(at, "change_order.created", "change_order", "order"),
	})
	if err != nil {
		t.Fatalf("CreateChangeOrderAtomic() error = %v", err)
	}
	assertQueryOrder(t, tx.queries,
		"INSERT INTO change_orders", "INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestIssueChangeOrderAtomicUsesVersionedOrderAndFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewChangeOrderRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	err := repository.IssueChangeOrderAtomic(context.Background(), projects.IssueChangeOrderMutation{
		Order: projects.ChangeOrder{
			ID: "order", ProjectID: "project", MSPID: "msp", ClientID: "client",
			State: projects.ChangeOrderIssued, CurrentVersion: 2, Version: 3,
			UpdatedAt: at, UpdatedBy: "actor",
		},
		Version: projects.ChangeOrderVersion{
			ID: "version", ChangeOrderID: "order", MSPID: "msp", ClientID: "client",
			Version: 2, Description: "Added scope", Currency: "USD",
			IssuedAt: at, IssuedBy: "actor",
		},
		Audit: validAudit(at, "change_order.version.issued", "change_order_version", "version"),
		Event: validEvent(at, "change_order.version.issued", "change_order_version", "version"),
	})
	if err != nil {
		t.Fatalf("IssueChangeOrderAtomic() error = %v", err)
	}
	assertQueryOrder(t, tx.queries,
		"UPDATE change_orders", "INSERT INTO change_order_versions",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestDecideChangeOrderAtomicRejectsStaleOrder(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 1}
	repository := NewChangeOrderRepository(&fakeSalesDB{tx: tx})
	err := repository.DecideChangeOrderAtomic(context.Background(), projects.DecideChangeOrderMutation{
		Order: projects.ChangeOrder{
			ID: "order", MSPID: "msp", ClientID: "client",
			State: projects.ChangeOrderApproved, Version: 3,
		},
	})
	if !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("DecideChangeOrderAtomic() error = %v", err)
	}
	if len(tx.queries) != 1 || !tx.rolledBack {
		t.Fatalf("stale decision wrote dependent facts: %+v", tx)
	}
}

func TestApplyChangeOrderAtomicUpdatesCurrentBaselineOnce(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewChangeOrderRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	err := repository.ApplyChangeOrderAtomic(context.Background(), projects.ApplyChangeOrderMutation{
		Order: projects.ChangeOrder{
			ID: "order", MSPID: "msp", ClientID: "client",
			State: projects.ChangeOrderApplied, Version: 4,
			UpdatedAt: at, UpdatedBy: "actor",
		},
		Project: projects.Project{
			ID: "project", MSPID: "msp", ClientID: "client", Version: 5,
			CurrentBaseline: projects.BudgetBaseline{
				Currency: "USD", RevenueMinor: 1200, CostMinor: 500, PlannedMinutes: 60,
			},
		},
		Application: projects.ChangeOrderApplication{
			ID: "application", ChangeOrderID: "order",
			ChangeOrderVersionID: "version", ProjectID: "project",
			MSPID: "msp", ClientID: "client", AppliedAt: at, AppliedBy: "actor",
		},
		Audit: validAudit(at, "change_order.applied", "change_order_version", "version"),
		Event: validEvent(at, "change_order.applied", "change_order_version", "version"),
	})
	if err != nil {
		t.Fatalf("ApplyChangeOrderAtomic() error = %v", err)
	}
	assertQueryOrder(t, tx.queries,
		"UPDATE change_orders", "UPDATE projects", "UPDATE project_budgets",
		"INSERT INTO change_order_applications", "INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
}

func TestFindChangeOrderIsClientScoped(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: errors.New("capture")}}
	_, _ = NewChangeOrderRepository(db).FindChangeOrder(
		context.Background(), scope.Target{MSPID: "msp", ClientID: "client"}, "order",
	)
	if !strings.Contains(db.query, "client_id = $3") {
		t.Fatalf("change order lookup is not client scoped: %s", db.query)
	}
}

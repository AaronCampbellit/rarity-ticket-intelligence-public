package psa

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/links"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestCreateRelationshipValidatesBothEndpointsAndWritesFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewLinkRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 16, 0, 0, 0, time.UTC)
	err := repository.CreateAtomic(context.Background(), links.CreateMutation{
		Link: links.Link{
			ID: "relationship", MSPID: "msp", ClientID: "client",
			Source:   links.Ref{Type: "work_record", ID: "work", MSPID: "msp", ClientID: "client"},
			Target:   links.Ref{Type: "asset", ID: "asset", MSPID: "msp", ClientID: "client"},
			LinkType: "affected_asset", Version: 1, CreatedAt: at, CreatedBy: "actor",
		},
		Audit: validAudit(at, "relationship.created", "relationship", "relationship"),
		Event: validEvent(at, "relationship.created", "relationship", "relationship"),
	})
	if err != nil {
		t.Fatalf("CreateAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO object_links", "INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	if !strings.Contains(tx.queries[0], "FROM work_records") ||
		!strings.Contains(tx.queries[0], "FROM assets") {
		t.Fatalf("endpoint existence is not validated: %s", tx.queries[0])
	}
}

func TestCreateRelationshipRejectsInaccessibleEndpointBeforeFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 1}
	err := NewLinkRepository(&fakeSalesDB{tx: tx}).CreateAtomic(
		context.Background(),
		links.CreateMutation{Link: links.Link{
			ID: "relationship", MSPID: "msp", ClientID: "client",
			Source:   links.Ref{Type: "work_record", ID: "missing", MSPID: "msp", ClientID: "client"},
			Target:   links.Ref{Type: "asset", ID: "asset", MSPID: "msp", ClientID: "client"},
			LinkType: "affected_asset",
		}},
	)
	if !errors.Is(err, scope.ErrNotFound) || len(tx.queries) != 1 || !tx.rolledBack {
		t.Fatalf("inaccessible endpoint wrote facts: err=%v tx=%+v", err, tx)
	}
}

func TestProblemRelationshipConstrainsRecordTypes(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 30, 16, 30, 0, 0, time.UTC)
	err := NewLinkRepository(&fakeSalesDB{tx: tx}).CreateAtomic(
		context.Background(),
		links.CreateMutation{
			Link: links.Link{
				ID: "relationship", MSPID: "msp", ClientID: "client",
				Source:   links.Ref{Type: "work_record", ID: "incident"},
				Target:   links.Ref{Type: "work_record", ID: "problem"},
				LinkType: "caused_by_problem", CreatedAt: at, CreatedBy: "actor",
			},
			Audit: validAudit(at, "relationship.created", "relationship", "relationship"),
			Event: validEvent(at, "relationship.created", "relationship", "relationship"),
		},
	)
	if err != nil {
		t.Fatalf("CreateAtomic() error = %v", err)
	}
	if !strings.Contains(tx.queries[0], "record_type = 'incident'") ||
		!strings.Contains(tx.queries[0], "record_type = 'problem'") {
		t.Fatalf("relationship subtype constraints are missing: %s", tx.queries[0])
	}
}

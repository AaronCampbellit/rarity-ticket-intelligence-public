package psa

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
)

func TestPublishWorkflowCreatesRecordVersionAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 30, 20, 0, 0, 0, time.UTC)
	err := NewWorkflowRepository(&fakeSalesDB{tx: tx}).PublishAtomic(
		context.Background(),
		workflow.PublishMutation{
			Workflow: workflow.Published{
				ID: "workflow", MSPID: "msp", Key: "default", Name: "Default",
				Version: 1, Enabled: true, Fallback: true,
				Definition: workflow.Definition{States: []workflow.State{{Key: "new"}}},
			},
			Created: true, PublishedAt: at, PublishedBy: "actor",
			Audit: validAudit(at, "workflow.published", "workflow", "workflow"),
			Event: validEvent(at, "workflow.published", "workflow", "workflow"),
		},
	)
	if err != nil {
		t.Fatalf("PublishAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO workflows", "INSERT INTO workflow_versions",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestPublishWorkflowRejectsStaleCurrentVersionBeforeNewVersion(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 1}
	err := NewWorkflowRepository(&fakeSalesDB{tx: tx}).PublishAtomic(
		context.Background(),
		workflow.PublishMutation{
			Workflow:        workflow.Published{ID: "workflow", MSPID: "msp", Version: 3},
			ExpectedVersion: 2,
		},
	)
	if err == nil || len(tx.queries) != 1 || !tx.rolledBack {
		t.Fatalf("stale workflow published: err=%v tx=%+v", err, tx)
	}
}

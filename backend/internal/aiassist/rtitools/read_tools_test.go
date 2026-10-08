package rtitools

import (
	"context"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/integrationhealth"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
)

type knowledgeActionsStub struct {
	query string
}

func (s *knowledgeActionsStub) Search(_ context.Context, query string, _ aiassist.ProductAudience, _ int) ([]aiassist.Citation, error) {
	s.query = query
	return []aiassist.Citation{{SourceKey: "docs/03-user-guide/work.md", Section: "Queues"}}, nil
}

func TestProductHelpReturnsVersionedCitations(t *testing.T) {
	actions := &knowledgeActionsStub{}
	tool := NewProductHelpTool(actions)
	result, err := tool.Execute(
		context.Background(),
		testPrincipal("ai.assist"),
		[]byte(`{"query":"how do queues work?","audience":"all_users"}`),
		"correlation-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if actions.query != "how do queues work?" || result.Data["citations"] == nil {
		t.Fatalf("query=%q result=%+v", actions.query, result)
	}
}

func testEnvelope(id, clientID string, version int64) object.Envelope {
	return object.Envelope{
		ID: id, ObjectType: "incident", MSPID: "msp-1", ClientID: clientID,
		DisplayID: "INC-1", LifecycleState: "active", Version: version,
		CreatedBy: "technician-1", UpdatedBy: "technician-1",
	}
}

type healthActionsStub struct {
	snapshot integrationhealth.Snapshot
}

func (s *healthActionsStub) Snapshot(context.Context, integrationhealth.SnapshotCommand) (integrationhealth.Snapshot, error) {
	return s.snapshot, nil
}

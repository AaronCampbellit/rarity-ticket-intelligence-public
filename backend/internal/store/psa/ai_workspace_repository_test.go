package psa

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestAIWorkspaceConversationLookupIsTenantAndClientScoped(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: pgx.ErrNoRows}}
	_, err := NewAIWorkspaceRepository(db).GetConversation(
		context.Background(),
		scope.Target{MSPID: "msp-1", ClientID: "client-1"},
		"conversation-1",
	)
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("GetConversation() error=%v", err)
	}
	for _, fragment := range []string{
		"conversation.id = $1",
		"conversation.msp_id = $2",
		"conversation.client_id = NULLIF($3, '')::uuid",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("query missing %q: %s", fragment, db.query)
		}
	}
}

func TestAIWorkspaceProposalWritesExactPreviewAndScopedConfirmation(t *testing.T) {
	tx := &fakeSalesTx{queryRow: fakeRow{err: pgx.ErrNoRows}}
	repository := NewAIWorkspaceRepository(&fakeSalesDB{tx: tx})
	now := time.Date(2026, time.August, 4, 15, 0, 0, 0, time.UTC)
	proposal := aiassist.ActionProposal{
		ID: "proposal-1", MSPID: "msp-1", ClientID: "client-1",
		ConversationID: "conversation-1", PrincipalID: "technician-1",
		ToolName: "ticket.update", ToolVersion: 1,
		NormalizedInput: json.RawMessage(`{"id":"ticket-1"}`),
		Preview: aiassist.Preview{
			Summary: "Resolve ticket", TargetType: "work_record",
			TargetID: "ticket-1", TargetVersion: 3,
		},
		RequiredCapability: "work_record.update",
		ExpiresAt:          now.Add(10 * time.Minute), State: aiassist.ProposalPending,
		CorrelationID: "correlation-1", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.CreateProposal(context.Background(), proposal); err != nil {
		t.Fatal(err)
	}
	if len(tx.queries) != 1 {
		t.Fatalf("queries=%d", len(tx.queries))
	}
	for _, fragment := range []string{
		"INSERT INTO ai_action_proposals",
		"normalized_input", "preview", "required_capability", "expires_at",
	} {
		if !strings.Contains(tx.queries[0], fragment) {
			t.Fatalf("create query missing %q: %s", fragment, tx.queries[0])
		}
	}

	tx.queries = nil
	_, err := repository.ConfirmProposal(
		context.Background(),
		scope.Target{MSPID: "msp-1", ClientID: "client-1"},
		"proposal-1", "technician-1", 1, now,
	)
	if !errors.Is(err, aiassist.ErrProposalConflict) {
		t.Fatalf("ConfirmProposal() error=%v", err)
	}
	for _, fragment := range []string{
		"state = 'pending'", "expires_at > $6", "principal_id = $4", "version = $5",
	} {
		if !strings.Contains(tx.query, fragment) {
			t.Fatalf("confirm query missing %q: %s", fragment, tx.query)
		}
	}
}

func TestAIWorkspaceProposalPersistsTargetClientSeparatelyFromAccessScope(t *testing.T) {
	now := time.Date(2026, time.August, 4, 15, 0, 0, 0, time.UTC)
	tx := &fakeSalesTx{}
	db := &fakeSalesDB{
		tx: tx,
		queryRow: fakeRow{scan: func(destinations ...any) {
			*(destinations[0].(*string)) = "proposal-1"
			*(destinations[1].(*string)) = "msp-1"
			*(destinations[2].(*string)) = ""
			*(destinations[3].(*string)) = "client-1"
			*(destinations[4].(*string)) = "conversation-1"
			*(destinations[5].(*string)) = ""
			*(destinations[6].(*string)) = "technician-1"
			*(destinations[7].(*string)) = "ticket.update"
			*(destinations[8].(*int)) = 1
			*(destinations[9].(*[]byte)) = []byte(`{"id":"ticket-1"}`)
			*(destinations[10].(*[]byte)) = []byte(
				`{"summary":"Resolve ticket","target_type":"work_record","target_id":"ticket-1","target_version":3}`,
			)
			*(destinations[11].(*string)) = "work_record"
			*(destinations[12].(*string)) = "ticket-1"
			*(destinations[13].(*int64)) = 3
			*(destinations[14].(*string)) = "work_record.update"
			*(destinations[15].(*time.Time)) = now.Add(10 * time.Minute)
			*(destinations[16].(*aiassist.ProposalState)) = aiassist.ProposalPending
			*(destinations[21].(*string)) = ""
			*(destinations[22].(*string)) = "correlation-1"
			*(destinations[23].(*int64)) = 1
			*(destinations[24].(*time.Time)) = now
			*(destinations[25].(*time.Time)) = now
		}},
	}
	repository := NewAIWorkspaceRepository(db)
	proposal := aiassist.ActionProposal{
		ID: "proposal-1", MSPID: "msp-1", TargetClientID: "client-1",
		ConversationID: "conversation-1", PrincipalID: "technician-1",
		ToolName: "ticket.update", ToolVersion: 1,
		NormalizedInput: json.RawMessage(`{"id":"ticket-1"}`),
		Preview: aiassist.Preview{
			Summary: "Resolve ticket", TargetType: "work_record",
			TargetID: "ticket-1", TargetVersion: 3,
		},
		RequiredCapability: "work_record.update",
		ExpiresAt:          now.Add(10 * time.Minute),
		State:              aiassist.ProposalPending,
		CorrelationID:      "correlation-1",
		Version:            1,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := repository.CreateProposal(context.Background(), proposal); err != nil {
		t.Fatal(err)
	}
	if len(tx.args) != 1 || tx.args[0][2] != "" || tx.args[0][3] != "client-1" {
		t.Fatalf("create scope args=%+v", tx.args)
	}
	stored, err := repository.GetProposal(
		context.Background(), scope.Target{MSPID: "msp-1"}, proposal.ID, proposal.PrincipalID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ClientID != "" || stored.TargetClientID != "client-1" {
		t.Fatalf(
			"stored access client=%q target client=%q",
			stored.ClientID, stored.TargetClientID,
		)
	}
	if len(db.args) != 4 || db.args[2] != "" {
		t.Fatalf("get access args=%+v", db.args)
	}
}

func TestAIWorkspaceProductSearchIsBoundedAndAudienceFiltered(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	_, err := NewAIWorkspaceRepository(db).SearchProductKnowledge(
		context.Background(),
		"ticket routing",
		aiassist.ProductAudienceAllUsers,
		20,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"websearch_to_tsquery",
		"left(document.body, 480)",
		"document.audience = 'all_users'",
		"LIMIT $3",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("query missing %q: %s", fragment, db.query)
		}
	}
	if strings.Contains(db.query, "ts_headline") {
		t.Fatalf("query must return plain product text without headline markup: %s", db.query)
	}
}

package psa

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestDecideAIRecommendationWritesDecisionAuditAndOutboxAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewAIRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)

	err := repository.Decide(context.Background(), aiassist.DecisionMutation{
		Recommendation: aiassist.Recommendation{
			ID: "recommendation", MSPID: "msp", ClientID: "client",
			State: aiassist.RecommendationAccepted,
		},
		DecisionID: "decision", Decision: aiassist.DecisionAccepted,
		Reason: "Technician reviewed the draft", DecidedAt: at,
		DecidedBy: "actor", Applied: false, Sent: false,
		Audit: mutation.AuditRecord{
			ID: "decision", OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor",
			Action:      "ai.recommendation.accepted",
			SubjectType: "ai_recommendation", SubjectID: "recommendation",
			SubjectVersion: 2, Source: "api", Reason: "Technician reviewed the draft",
			CorrelationID: "correlation",
		},
		Event: mutation.EventRecord{
			EventID: "event", EventType: "ai.recommendation.accepted",
			SchemaVersion: 1, OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor",
			SubjectType: "ai_recommendation", SubjectID: "recommendation",
			SubjectVersion: 2, Source: "api", CorrelationID: "correlation",
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	assertQueryOrder(t, tx.queries,
		"UPDATE ai_recommendations", "INSERT INTO ai_recommendation_decisions",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestFindAIRecommendationIsClientScoped(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: context.Canceled}}
	_, _ = NewAIRepository(db).Get(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"recommendation",
	)
	if !strings.Contains(db.query, "msp_id = $2 AND client_id = $3") {
		t.Fatalf("AI recommendation lookup is not Client scoped: %s", db.query)
	}
}

func TestFindAIRecommendationForReviewJoinsItsJobWithoutReadingProviderData(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: context.Canceled}}
	_, _ = NewAIRepository(db).GetForReview(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"recommendation",
	)
	for _, required := range []string{"JOIN ai_generation_jobs", "msp_id = $2 AND recommendation.client_id = $3"} {
		if !strings.Contains(db.query, required) {
			t.Fatalf("review lookup missing %q: %s", required, db.query)
		}
	}
	for _, forbidden := range []string{"provider", "model", "prompt_version"} {
		if strings.Contains(db.query, forbidden) {
			t.Fatalf("review lookup read %q: %s", forbidden, db.query)
		}
	}
}

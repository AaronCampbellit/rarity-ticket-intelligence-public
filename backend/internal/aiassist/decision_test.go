package aiassist

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type decisionStore struct {
	recommendation Recommendation
	mutation       DecisionMutation
}

func (s *decisionStore) Get(context.Context, scope.Target, string) (Recommendation, error) {
	return s.recommendation, nil
}
func (s *decisionStore) Decide(_ context.Context, mutation DecisionMutation) error {
	s.mutation = mutation
	return nil
}

func TestHumanDecisionRecordsAcceptanceWithoutApplyingOrSending(t *testing.T) {
	now := time.Date(2026, time.July, 30, 2, 30, 0, 0, time.UTC)
	store := &decisionStore{recommendation: Recommendation{
		ID: "recommendation-id", MSPID: "msp-id",
		ClientID: "client-alpha", WorkRecordID: "work-id",
		State: RecommendationPendingHuman,
	}}
	service := NewDecisionService(store, func() time.Time { return now }, sequenceIDs("decision-id", "event-id", "correlation-id"))
	principal := authorization.Principal{
		ID:           "technician-id",
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-alpha"},
		Capabilities: authorization.NewCapabilitySet("ai.assist"),
	}
	result, err := service.Decide(context.Background(), DecisionCommand{
		Principal: principal, ID: "recommendation-id",
		Decision: DecisionAccepted, Reason: "useful after review",
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if result.State != RecommendationAccepted ||
		store.mutation.Audit.Action != "ai.recommendation.accepted" ||
		store.mutation.Applied || store.mutation.Sent {
		t.Fatalf("human decision gained autonomous side effects: result=%+v mutation=%+v", result, store.mutation)
	}
}

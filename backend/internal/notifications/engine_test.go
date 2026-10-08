package notifications

import (
	"testing"
	"time"
)

func TestEvaluateAppliesQuietPeriodButCriticalBypasses(t *testing.T) {
	now := time.Date(2026, time.July, 30, 3, 0, 0, 0, time.UTC)
	policy := Policy{
		ID: "sla-policy", EventType: "sla.warning",
		Channels:    []Channel{InApp, Email, Teams},
		QuietPeriod: 10 * time.Minute, CriticalBypass: true,
	}
	recent := []RecentDelivery{{
		PolicyID: "sla-policy", WorkRecordID: "work-id", DeliveredAt: now.Add(-time.Minute),
	}}
	decision := Evaluate(policy, Event{
		Type: "sla.warning", WorkRecordID: "work-id", OccurredAt: now,
	}, recent)
	if !decision.Suppressed || len(decision.Deliveries) != 0 {
		t.Fatalf("duplicate decision = %+v", decision)
	}

	decision = Evaluate(policy, Event{
		Type: "sla.warning", WorkRecordID: "work-id", OccurredAt: now, Critical: true,
	}, recent)
	if decision.Suppressed || len(decision.Deliveries) != 3 {
		t.Fatalf("critical decision = %+v", decision)
	}
}

func TestTeamsCardContainsOnlyApprovedCompactFields(t *testing.T) {
	card, err := BuildTeamsCard(WorkSummary{
		DisplayID: "INC-100", Priority: "critical", Status: "in_progress",
		Subject: "Email unavailable", SLAState: "warning",
		AuthenticatedURL: "https://rarity.example/work/INC-100",
		InternalNote:     "credential is secret", EmailBody: "full private message",
	})
	if err != nil {
		t.Fatalf("BuildTeamsCard() error = %v", err)
	}
	encoded := string(card)
	for _, forbidden := range []string{"credential is secret", "full private message"} {
		if contains(encoded, forbidden) {
			t.Fatalf("Teams card leaked %q: %s", forbidden, encoded)
		}
	}
	for _, required := range []string{"INC-100", "critical", "warning"} {
		if !contains(encoded, required) {
			t.Fatalf("Teams card missing %q: %s", required, encoded)
		}
	}
}

func contains(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

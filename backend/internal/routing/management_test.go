package routing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type managementRepository struct {
	current   RuleSet
	loadErr   error
	accepted  PublishMutation
	validated []Rule
	calls     int
}

func (r *managementRepository) LoadCurrent(context.Context, string) (RuleSet, error) {
	return r.current, r.loadErr
}

func (r *managementRepository) ValidateDestinations(
	_ context.Context,
	_ scope.Target,
	rules []Rule,
) error {
	r.validated = rules
	return nil
}

func (r *managementRepository) PublishAtomic(_ context.Context, accepted PublishMutation) error {
	r.calls++
	r.accepted = accepted
	return nil
}

func TestPublishCreatesVersionedDeterministicRuleSet(t *testing.T) {
	repository := &managementRepository{loadErr: scope.ErrNotFound}
	at := time.Date(2026, time.July, 29, 20, 0, 0, 0, time.UTC)
	ids := []string{"set-id", "specific-id", "fallback-id", "audit-id", "event-id", "correlation-id"}
	service := NewManagementService(repository, func() time.Time { return at }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	ruleSet, err := service.Publish(context.Background(), PublishCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp"},
			Capabilities: authorization.NewCapabilitySet("routing.manage"),
		},
		Rules: []Rule{
			{Position: 1, Priority: "critical", QueueID: "noc"},
			{Position: 2, QueueID: "triage"},
		},
		ActorID: "actor", Source: "api",
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if ruleSet.ID != "set-id" || ruleSet.Version != 1 ||
		ruleSet.Rules[0].ID != "specific-id" ||
		ruleSet.Rules[1].ID != "fallback-id" {
		t.Fatalf("unexpected rule set: %+v", ruleSet)
	}
	if repository.calls != 1 || len(repository.validated) != 2 ||
		repository.accepted.Audit.Action != "routing.rules.published" ||
		repository.accepted.Event.EventType != "routing.rules.published" {
		t.Fatalf("publish evidence incomplete: %+v", repository)
	}
}

func TestCurrentReturnsTheAuthorizedMSPGlobalRuleSet(t *testing.T) {
	repository := &managementRepository{current: RuleSet{
		ID: "set", MSPID: "msp", Version: 3,
		Rules: []Rule{{ID: "fallback", Position: 1, QueueID: "triage"}},
	}}
	service := NewManagementService(repository, time.Now, func() string { return "unused" })
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp"},
		Capabilities: authorization.NewCapabilitySet("routing.manage"),
	}

	found, err := service.Current(context.Background(), principal)
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if found.ID != "set" || found.Version != 3 || found.MSPID != "msp" {
		t.Fatalf("Current() = %+v", found)
	}
}

func TestPublishRejectsStaleOrAmbiguousReplacement(t *testing.T) {
	repository := &managementRepository{current: RuleSet{
		ID: "set", MSPID: "msp", Version: 3,
		Rules: []Rule{{ID: "old", Position: 1, QueueID: "triage"}},
	}}
	service := NewManagementService(repository, time.Now, func() string { return "new-id" })
	command := PublishCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp"},
			Capabilities: authorization.NewCapabilitySet("routing.manage"),
		},
		ExpectedVersion: 2,
		Rules: []Rule{
			{ID: "one", Position: 1, QueueID: "queue-one"},
			{ID: "two", Position: 1, QueueID: "queue-two"},
		},
		ActorID: "actor", Source: "api",
	}
	if _, err := service.Publish(context.Background(), command); !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("Publish() stale error = %v", err)
	}
	command.ExpectedVersion = 3
	if _, err := service.Publish(context.Background(), command); !errors.Is(err, ErrInvalidRules) {
		t.Fatalf("Publish() ambiguous error = %v", err)
	}
	if repository.calls != 0 {
		t.Fatal("invalid replacement reached repository")
	}
}

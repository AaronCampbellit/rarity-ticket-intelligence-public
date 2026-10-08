package sla

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type policyRepository struct {
	current  []PublishedPolicy
	calendar PublishedCalendar
	accepted PolicyPublishMutation
	calls    int
}

func (r *policyRepository) ListPolicies(
	context.Context,
	scope.Target,
) ([]PublishedPolicy, error) {
	return r.current, nil
}

func (r *policyRepository) ResolveCalendar(
	context.Context,
	scope.Target,
	string,
) (PublishedCalendar, error) {
	return r.calendar, nil
}

func (r *policyRepository) PublishPolicyAtomic(
	_ context.Context,
	accepted PolicyPublishMutation,
) error {
	r.calls++
	r.accepted = accepted
	return nil
}

func TestPublishPolicyCreatesValidatedFallbackVersion(t *testing.T) {
	repository := &policyRepository{calendar: PublishedCalendar{
		ID: "calendar", MSPID: "msp", Version: 2,
		Definition: businessCalendarDefinition(),
	}}
	at := time.Date(2026, time.July, 29, 23, 0, 0, 0, time.UTC)
	ids := []string{"policy", "audit", "event", "correlation"}
	service := NewPolicyManagementService(repository, func() time.Time { return at }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	published, err := service.PublishPolicy(context.Background(), PublishPolicyCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp"},
			Capabilities: authorization.NewCapabilitySet("sla.manage"),
		},
		Key: "default", Name: "Default SLA", CalendarID: "calendar",
		ResponseTargetSeconds: 3600, ResolutionTargetSeconds: 14400,
		WarningPercent: 80, Enabled: true, StableOrder: 1, Fallback: true,
		ActorID: "actor", Source: "api",
	})
	if err != nil {
		t.Fatalf("PublishPolicy() error = %v", err)
	}
	if published.ID != "policy" || published.Version != 1 ||
		published.Calendar.Version != 2 || repository.calls != 1 ||
		repository.accepted.Audit.Action != "sla_policy.published" ||
		repository.accepted.Event.EventType != "sla_policy.published" {
		t.Fatalf("unexpected policy publication: published=%+v repository=%+v", published, repository)
	}
}

func TestPublishPolicyRejectsStaleReplacementAndCrossClientCondition(t *testing.T) {
	current := publishedPolicies()
	current[0].MSPID, current[0].ClientID = "msp", "client"
	repository := &policyRepository{
		current: current,
		calendar: PublishedCalendar{
			ID: "calendar", MSPID: "msp", ClientID: "client", Version: 2,
			Definition: businessCalendarDefinition(),
		},
	}
	service := NewPolicyManagementService(repository, time.Now, func() string { return "unused" })
	command := PublishPolicyCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("sla.manage"),
		},
		PolicyID: "contract-policy", ExpectedVersion: 3,
		Key: "contract", Name: "Contract SLA", CalendarID: "calendar",
		Conditions:            PolicyConditions{ClientID: "other-client"},
		ResponseTargetSeconds: 900, ResolutionTargetSeconds: 7200,
		WarningPercent: 80, Enabled: true, Priority: 100, StableOrder: 1,
		ActorID: "actor", Source: "api",
	}
	if _, err := service.PublishPolicy(context.Background(), command); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-client PublishPolicy() error = %v", err)
	}
	command.Conditions.ClientID = "client"
	if _, err := service.PublishPolicy(context.Background(), command); !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("stale PublishPolicy() error = %v", err)
	}
	if repository.calls != 0 {
		t.Fatal("rejected policy reached repository")
	}
}

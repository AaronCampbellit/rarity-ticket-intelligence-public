package notifications

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type policyRepository struct {
	current   []PublishedPolicy
	accepted  PolicyPublishMutation
	validated []Destination
	calls     int
}

func (r *policyRepository) ListPolicies(context.Context, scope.Target) ([]PublishedPolicy, error) {
	return r.current, nil
}

func (r *policyRepository) ValidateDestinations(
	_ context.Context,
	_ scope.Target,
	destinations []Destination,
) error {
	r.validated = destinations
	return nil
}

func (r *policyRepository) PublishPolicyAtomic(
	_ context.Context,
	accepted PolicyPublishMutation,
) error {
	r.calls++
	r.accepted = accepted
	return nil
}

func TestPublishPolicyCreatesImmutableVersionWithAtomicEvidence(t *testing.T) {
	repository := &policyRepository{}
	at := time.Date(2026, time.July, 29, 14, 0, 0, 0, time.UTC)
	ids := []string{"policy-id", "audit-id", "event-id", "correlation-id"}
	service := NewPolicyManagementService(repository, func() time.Time { return at }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	policy, err := service.PublishPolicy(context.Background(), PublishPolicyCommand{
		Principal: authorization.Principal{
			ID: "actor", Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("notification.manage"),
		},
		Key: "sla-warning", Name: "SLA warning", EventType: "sla.warning",
		Conditions: PolicyConditions{Priority: "critical", QueueID: "noc"},
		Destinations: []Destination{
			{Channel: InApp, RecipientRef: "queue:noc", ContentClassification: "internal"},
			{Channel: Teams, RecipientRef: "teams-connection", ContentClassification: "restricted"},
		},
		QuietPeriodSeconds: 600, CriticalBypass: true, Enabled: true,
		Priority: 100, StableOrder: 10, ActorID: "actor", Source: "api",
	})
	if err != nil {
		t.Fatalf("PublishPolicy() error = %v", err)
	}
	if policy.ID != "policy-id" || policy.Version != 1 ||
		policy.ClientID != "client" || len(repository.validated) != 2 {
		t.Fatalf("unexpected published policy: %+v repository=%+v", policy, repository)
	}
	if repository.calls != 1 ||
		repository.accepted.Audit.Action != "notification_policy.published" ||
		repository.accepted.Event.EventType != "notification_policy.published" {
		t.Fatalf("publish evidence incomplete: %+v", repository.accepted)
	}
}

func TestPublishPolicyRejectsStaleReplacementAndDuplicateDestination(t *testing.T) {
	repository := &policyRepository{current: []PublishedPolicy{{
		ID: "policy", MSPID: "msp", ClientID: "client", Key: "existing",
		Name: "Existing", EventType: "sla.warning", Version: 3, Enabled: true,
		Priority: 10, StableOrder: 10,
		Destinations: []Destination{{
			Channel: InApp, RecipientRef: "queue:noc", ContentClassification: "internal",
		}},
	}}}
	service := NewPolicyManagementService(repository, time.Now, func() string { return "id" })
	command := PublishPolicyCommand{
		Principal: authorization.Principal{
			ID: "actor", Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("notification.manage"),
		},
		PolicyID: "policy", ExpectedVersion: 2, Key: "existing", Name: "Existing",
		EventType: "sla.warning", Enabled: true, Priority: 10, StableOrder: 10,
		Destinations: []Destination{{
			Channel: InApp, RecipientRef: "queue:noc", ContentClassification: "internal",
		}},
		ActorID: "actor", Source: "api",
	}
	if _, err := service.PublishPolicy(context.Background(), command); !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("PublishPolicy() stale error = %v", err)
	}
	command.ExpectedVersion = 3
	command.Destinations = append(command.Destinations, command.Destinations[0])
	if _, err := service.PublishPolicy(context.Background(), command); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("PublishPolicy() duplicate error = %v", err)
	}
	if repository.calls != 0 {
		t.Fatal("invalid policy reached repository")
	}
}

func TestPublishMentionPolicyRejectsWidgetAndWebhookDestinations(t *testing.T) {
	service := NewPolicyManagementService(&policyRepository{}, time.Now, func() string { return "id" })
	for _, channel := range []Channel{InApp, Webhook} {
		_, err := service.PublishPolicy(context.Background(), PublishPolicyCommand{
			Principal: authorization.Principal{ID: "actor", Scope: scope.Principal{MSPID: "msp", ClientID: "client"}, Capabilities: authorization.NewCapabilitySet("notification.manage")},
			Key:       "mentions-" + string(channel), Name: "Mentions", EventType: MentionOccurred,
			Destinations: []Destination{{Channel: channel, RecipientRef: "recipient", ContentClassification: "internal"}},
			Enabled:      true, ActorID: "actor", Source: "api",
		})
		if !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("channel=%s error=%v", channel, err)
		}
	}
}

func TestCalendarPolicyDestinationsArePrivateAndDynamic(t *testing.T) {
	cases := []struct {
		name    string
		channel Channel
		ref     string
		wantErr bool
	}{
		{"in app", InApp, CalendarAssigneeRecipientRef, false},
		{"email", Email, CalendarAssigneeRecipientRef, false},
		{"teams", Teams, CalendarAssigneeRecipientRef, true},
		{"webhook", Webhook, CalendarAssigneeRecipientRef, true},
		{"static", Email, "technician:fixed", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := NewPolicyManagementService(&policyRepository{}, time.Now, func() string { return "id" })
			_, err := service.PublishPolicy(context.Background(), calendarPolicyCommand(Destination{
				Channel: tc.channel, RecipientRef: tc.ref, ContentClassification: "internal",
			}))
			if errors.Is(err, ErrInvalidPolicy) != tc.wantErr {
				t.Fatalf("PublishPolicy() error = %v, want invalid=%t", err, tc.wantErr)
			}
		})
	}
}

func TestCalendarPolicyNormalizesCalendarConditions(t *testing.T) {
	service := NewPolicyManagementService(&policyRepository{}, time.Now, func() string { return "id" })
	policy, err := service.PublishPolicy(context.Background(), PublishPolicyCommand{
		Principal: authorization.Principal{ID: "actor", Scope: scope.Principal{MSPID: "msp", ClientID: "client"}, Capabilities: authorization.NewCapabilitySet("notification.manage")},
		Key:       "calendar-schedule", Name: "Calendar schedule", EventType: CalendarNotificationEventType,
		Conditions: PolicyConditions{
			CalendarChangeClasses: []CalendarChangeClass{CalendarPTO, CalendarSchedule, CalendarPTO},
			CalendarUrgencies:     []CalendarUrgency{CalendarUrgent, CalendarRoutine, CalendarUrgent},
		},
		Destinations: []Destination{{Channel: InApp, RecipientRef: CalendarAssigneeRecipientRef, ContentClassification: "internal"}},
		Enabled:      true, ActorID: "actor", Source: "api",
	})
	if err != nil {
		t.Fatalf("PublishPolicy() error = %v", err)
	}
	if got, want := policy.Conditions.CalendarChangeClasses, []CalendarChangeClass{CalendarPTO, CalendarSchedule}; !slices.Equal(got, want) {
		t.Fatalf("CalendarChangeClasses = %v, want %v", got, want)
	}
	if got, want := policy.Conditions.CalendarUrgencies, []CalendarUrgency{CalendarRoutine, CalendarUrgent}; !slices.Equal(got, want) {
		t.Fatalf("CalendarUrgencies = %v, want %v", got, want)
	}
}

func TestCalendarPolicyRejectsInvalidCalendarConditionValues(t *testing.T) {
	service := NewPolicyManagementService(&policyRepository{}, time.Now, func() string { return "id" })
	for _, conditions := range []PolicyConditions{
		{CalendarChangeClasses: []CalendarChangeClass{"unplanned"}},
		{CalendarUrgencies: []CalendarUrgency{"immediate"}},
	} {
		command := calendarPolicyCommand(Destination{Channel: InApp, RecipientRef: CalendarAssigneeRecipientRef, ContentClassification: "internal"})
		command.Conditions = conditions
		if _, err := service.PublishPolicy(context.Background(), command); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("PublishPolicy() error = %v, want ErrInvalidPolicy for %#v", err, conditions)
		}
	}
}

func calendarPolicyCommand(destination Destination) PublishPolicyCommand {
	return PublishPolicyCommand{
		Principal: authorization.Principal{ID: "actor", Scope: scope.Principal{MSPID: "msp", ClientID: "client"}, Capabilities: authorization.NewCapabilitySet("notification.manage")},
		Key:       "calendar-policy", Name: "Calendar policy", EventType: CalendarNotificationEventType,
		Destinations: []Destination{destination}, Enabled: true, ActorID: "actor", Source: "api",
	}
}

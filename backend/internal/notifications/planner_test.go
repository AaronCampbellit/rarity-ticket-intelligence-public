package notifications

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/observability"
)

type planningRepository struct {
	events              []PlanningEvent
	policies            map[string][]PublishedPolicy
	recent              map[string][]RecentDelivery
	accepted            []PlannedDecision
	planCalls           int
	preferences         map[string]RecipientPreference
	calendarPreferences map[string]CalendarPreference
	losePlan            bool
}

func (r *planningRepository) PreferenceForMention(_ context.Context, event PlanningEvent) (RecipientPreference, error) {
	if preference, ok := r.preferences[event.RecipientTechnicianID]; ok {
		return preference, nil
	}
	return DefaultMentionPreference(event.RecipientTechnicianID), nil
}

func (r *planningRepository) CalendarPreference(_ context.Context, _ string, technicianID string) (CalendarPreference, error) {
	if preference, ok := r.calendarPreferences[technicianID]; ok {
		return preference, nil
	}
	return CalendarPreference{TechnicianID: technicianID}, nil
}

func (r *planningRepository) ClaimEvents(context.Context, int, time.Time) ([]PlanningEvent, error) {
	return r.events, nil
}

func TestPlannerPlansMentionForExactRecipientWithoutInAppOrDuplicateChannels(t *testing.T) {
	now := time.Date(2026, time.August, 6, 15, 0, 0, 0, time.UTC)
	repository := &planningRepository{
		events: []PlanningEvent{
			{ID: "event", Type: MentionOccurred, MSPID: "msp", ClientID: "client", SubjectType: "task", SubjectID: "task", MentionOccurrenceID: "occurrence-direct", RecipientTechnicianID: "tech-2", OccurredAt: now},
			{ID: "event", Type: MentionOccurred, MSPID: "msp", ClientID: "client", SubjectType: "task", SubjectID: "task", MentionOccurrenceID: "occurrence-team", RecipientTechnicianID: "tech-2", OccurredAt: now},
		},
		policies: map[string][]PublishedPolicy{"event": {{
			ID: "mentions", Version: 1, EventType: MentionOccurred, Enabled: true,
			Destinations: []Destination{
				{Channel: InApp, RecipientRef: "recipient", ContentClassification: "internal"},
				{Channel: Email, RecipientRef: "recipient", ContentClassification: "internal"},
				{Channel: Email, RecipientRef: "duplicate", ContentClassification: "internal"},
				{Channel: Teams, RecipientRef: "connection", ContentClassification: "internal"},
			},
		}}},
	}
	result, err := NewPlanner(repository, func() time.Time { return now }, sequentialTestIDs()).RunOnce(context.Background(), 10)
	if err != nil || result.Events != 1 || result.Pending != 2 || repository.planCalls != 1 {
		t.Fatalf("result=%+v err=%v decisions=%+v", result, err, repository.accepted)
	}
	for _, delivery := range repository.accepted[0].Deliveries {
		if delivery.Channel == InApp || delivery.RecipientTechnicianID != "tech-2" || delivery.MentionOccurrenceID == "" {
			t.Fatalf("unsafe mention delivery=%+v", delivery)
		}
	}
}

func TestPlannerAppliesRecipientPreferenceAndOvernightQuietHours(t *testing.T) {
	now := time.Date(2026, time.August, 7, 5, 30, 0, 0, time.UTC) // 01:30 America/New_York.
	event := PlanningEvent{ID: "event", Type: MentionOccurred, MSPID: "msp", ClientID: "client", SubjectType: "project", SubjectID: "project", MentionOccurrenceID: "occurrence", RecipientTechnicianID: "tech", OccurredAt: now}
	repository := &planningRepository{
		events: []PlanningEvent{event},
		policies: map[string][]PublishedPolicy{"event": {{ID: "mentions", Version: 1, EventType: MentionOccurred, Enabled: true, Destinations: []Destination{
			{Channel: Email, RecipientRef: "recipient", ContentClassification: "internal"},
			{Channel: Teams, RecipientRef: "connection", ContentClassification: "internal"},
		}}}},
		preferences: map[string]RecipientPreference{"tech": {TechnicianID: "tech", EventType: MentionOccurred, EmailEnabled: false, TeamsEnabled: true, TimeZone: "America/New_York", QuietStart: "22:00", QuietEnd: "06:00", Version: 2}},
	}
	result, err := NewPlanner(repository, func() time.Time { return now }, sequentialTestIDs()).RunOnce(context.Background(), 10)
	if err != nil || result.Pending != 0 || result.Suppressed != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	delivery := repository.accepted[0].Deliveries[0]
	if delivery.Channel != Teams || delivery.SuppressionReason != "quiet_hours" {
		t.Fatalf("delivery=%+v", delivery)
	}
}

func TestPlannerEmitsPendingAndQuietHoursMentionOutcomesAfterPersistence(t *testing.T) {
	now := time.Date(2026, time.August, 7, 5, 30, 0, 0, time.UTC)
	telemetry := observability.NewMentionTelemetry(nil)
	repository := &planningRepository{
		events: []PlanningEvent{
			{ID: "pending-event", Type: MentionOccurred, MSPID: "msp", ClientID: "client", SubjectType: "task", SubjectID: "task", MentionOccurrenceID: "pending-occurrence", RecipientTechnicianID: "pending-tech", OccurredAt: now},
			{ID: "quiet-event", Type: MentionOccurred, MSPID: "msp", ClientID: "client", SubjectType: "project", SubjectID: "project", MentionOccurrenceID: "quiet-occurrence", RecipientTechnicianID: "quiet-tech", OccurredAt: now},
		},
		policies: map[string][]PublishedPolicy{
			"pending-event": {{ID: "mentions", Version: 1, EventType: MentionOccurred, Enabled: true, Destinations: []Destination{{Channel: Email, RecipientRef: "recipient", ContentClassification: "internal"}}}},
			"quiet-event":   {{ID: "mentions", Version: 1, EventType: MentionOccurred, Enabled: true, Destinations: []Destination{{Channel: Teams, RecipientRef: "connection", ContentClassification: "internal"}}}},
		},
		preferences: map[string]RecipientPreference{
			"pending-tech": DefaultMentionPreference("pending-tech"),
			"quiet-tech":   {TechnicianID: "quiet-tech", EventType: MentionOccurred, EmailEnabled: true, TeamsEnabled: true, TimeZone: "America/New_York", QuietStart: "22:00", QuietEnd: "06:00", Version: 1},
		},
	}
	_, err := NewPlanner(repository, func() time.Time { return now }, sequentialTestIDs()).WithTelemetry(telemetry).RunOnce(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := telemetry.Value("notification", "task", "pending"); got != 1 {
		t.Fatalf("pending outcome=%d want 1", got)
	}
	if got := telemetry.Value("notification", "project", "quiet_hours"); got != 1 {
		t.Fatalf("quiet-hours outcome=%d want 1", got)
	}
}

func sequentialTestIDs() func() string {
	index := 0
	return func() string { index++; return fmt.Sprintf("delivery-%d", index) }
}

func (r *planningRepository) ListPoliciesForEvent(
	_ context.Context,
	event PlanningEvent,
) ([]PublishedPolicy, error) {
	return r.policies[event.ID], nil
}

func (r *planningRepository) RecentDeliveries(
	_ context.Context,
	event PlanningEvent,
) ([]RecentDelivery, error) {
	return r.recent[event.ID], nil
}

func (r *planningRepository) PlanAtomic(
	_ context.Context,
	decision PlannedDecision,
) (bool, error) {
	r.planCalls++
	r.accepted = append(r.accepted, decision)
	return !r.losePlan, nil
}

func TestPlannerDoesNotCountOrEmitWhenTheAtomicEventClaimIsLost(t *testing.T) {
	now := time.Date(2026, time.August, 7, 5, 30, 0, 0, time.UTC)
	telemetry := observability.NewMentionTelemetry(nil)
	repository := &planningRepository{
		losePlan: true,
		events:   []PlanningEvent{{ID: "event", Type: MentionOccurred, MSPID: "msp", ClientID: "client", SubjectType: "task", SubjectID: "task", MentionOccurrenceID: "occurrence", RecipientTechnicianID: "tech", OccurredAt: now}},
		policies: map[string][]PublishedPolicy{"event": {{ID: "mentions", Version: 1, EventType: MentionOccurred, Enabled: true, Destinations: []Destination{{Channel: Email, RecipientRef: "recipient", ContentClassification: "internal"}}}}},
	}
	result, err := NewPlanner(repository, func() time.Time { return now }, sequentialTestIDs()).WithTelemetry(telemetry).RunOnce(context.Background(), 10)
	if err != nil || result != (PlanningResult{}) || repository.planCalls != 1 {
		t.Fatalf("lost ownership result=%+v calls=%d error=%v", result, repository.planCalls, err)
	}
	if got := telemetry.Value("notification", "task", "pending"); got != 0 {
		t.Fatalf("lost ownership emitted notification telemetry=%d", got)
	}
}

func TestPlannerCreatesDestinationDeliveriesAndPersistsSuppression(t *testing.T) {
	now := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	repository := &planningRepository{
		events: []PlanningEvent{
			{ID: "event-new", Type: "sla.warning", MSPID: "msp", ClientID: "client",
				WorkRecordID: "work-new", OccurredAt: now},
			{ID: "event-repeat", Type: "sla.warning", MSPID: "msp", ClientID: "client",
				WorkRecordID: "work-repeat", OccurredAt: now},
		},
		policies: map[string][]PublishedPolicy{
			"event-new": {{
				ID: "policy", Version: 2, EventType: "sla.warning", Enabled: true,
				Destinations: []Destination{
					{Channel: InApp, RecipientRef: "queue:noc", ContentClassification: "internal"},
					{Channel: Teams, RecipientRef: "teams", ContentClassification: "restricted"},
				},
			}},
			"event-repeat": {{
				ID: "policy", Version: 2, EventType: "sla.warning", Enabled: true,
				QuietPeriodSeconds: 600,
				Destinations: []Destination{{
					Channel: Teams, RecipientRef: "teams", ContentClassification: "restricted",
				}},
			}},
		},
		recent: map[string][]RecentDelivery{
			"event-repeat": {{
				PolicyID: "policy", WorkRecordID: "work-repeat", DeliveredAt: now.Add(-time.Minute),
			}},
		},
	}
	ids := []string{"delivery-1", "delivery-2", "suppressed-1"}
	planner := NewPlanner(repository, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	result, err := planner.RunOnce(context.Background(), 100)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Events != 2 || result.Pending != 2 || result.Suppressed != 1 ||
		repository.planCalls != 2 {
		t.Fatalf("unexpected planning result: %+v repository=%+v", result, repository)
	}
	if got := repository.accepted[0].Deliveries; len(got) != 2 ||
		got[0].PolicyVersion != 2 || got[0].State != Pending ||
		got[1].RecipientRef != "teams" {
		t.Fatalf("pending deliveries = %+v", got)
	}
	if got := repository.accepted[1].Deliveries; len(got) != 1 ||
		got[0].State != Suppressed || got[0].SuppressionReason != "quiet_period" {
		t.Fatalf("suppressed delivery = %+v", got)
	}
}

func TestPlannerCriticalEventBypassesQuietPeriod(t *testing.T) {
	now := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	event := PlanningEvent{
		ID: "critical", Type: "sla.breached", MSPID: "msp", ClientID: "client",
		WorkRecordID: "work", OccurredAt: now, Critical: true,
	}
	repository := &planningRepository{
		events: []PlanningEvent{event},
		policies: map[string][]PublishedPolicy{"critical": {{
			ID: "policy", Version: 1, EventType: "sla.breached", Enabled: true,
			QuietPeriodSeconds: 3600, CriticalBypass: true,
			Destinations: []Destination{{
				Channel: Teams, RecipientRef: "teams", ContentClassification: "restricted",
			}},
		}}},
		recent: map[string][]RecentDelivery{"critical": {{
			PolicyID: "policy", WorkRecordID: "work", DeliveredAt: now.Add(-time.Minute),
		}}},
	}
	planner := NewPlanner(repository, func() time.Time { return now }, func() string { return "delivery" })
	result, err := planner.RunOnce(context.Background(), 10)
	if err != nil || result.Pending != 1 || result.Suppressed != 0 ||
		repository.accepted[0].Deliveries[0].State != Pending {
		t.Fatalf("critical planning failed: result=%+v err=%v accepted=%+v", result, err, repository.accepted)
	}
}

func TestPlannerCalendarNoMatchingPolicyConsumesEventWithZeroDeliveries(t *testing.T) {
	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	event := canonicalCalendarPlanningEvent(now)
	repository := &planningRepository{
		events: []PlanningEvent{event},
		policies: map[string][]PublishedPolicy{event.ID: {{
			ID: "organization-conflicts", Version: 1, EventType: CalendarNotificationEventType, Enabled: true,
			Conditions:   PolicyConditions{CalendarChangeClasses: []CalendarChangeClass{CalendarConflict}},
			Destinations: []Destination{{Channel: InApp, RecipientRef: CalendarAssigneeRecipientRef, ContentClassification: "internal"}},
		}}},
	}

	result, err := NewPlanner(repository, func() time.Time { return now }, sequentialTestIDs()).RunOnce(context.Background(), 10)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Events != 1 || result.Pending != 0 || result.Suppressed != 0 || repository.planCalls != 1 {
		t.Fatalf("result=%+v plan calls=%d", result, repository.planCalls)
	}
	if got := repository.accepted[0].Deliveries; len(got) != 0 {
		t.Fatalf("deliveries=%+v, want none", got)
	}
}

func TestPlannerCalendarMatchesConditionsAndCannotAddUnconfiguredChannel(t *testing.T) {
	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	event := canonicalCalendarPlanningEvent(now)
	repository := &planningRepository{
		events: []PlanningEvent{event},
		policies: map[string][]PublishedPolicy{event.ID: {
			{ID: "wrong-class", Version: 1, EventType: CalendarNotificationEventType, Enabled: true, Conditions: PolicyConditions{CalendarChangeClasses: []CalendarChangeClass{CalendarConflict}}, Destinations: []Destination{{Channel: InApp, RecipientRef: CalendarAssigneeRecipientRef, ContentClassification: "internal"}}},
			{ID: "wrong-urgency", Version: 1, EventType: CalendarNotificationEventType, Enabled: true, Conditions: PolicyConditions{CalendarChangeClasses: []CalendarChangeClass{CalendarSchedule}, CalendarUrgencies: []CalendarUrgency{CalendarUrgent}}, Destinations: []Destination{{Channel: InApp, RecipientRef: CalendarAssigneeRecipientRef, ContentClassification: "internal"}}},
			{ID: "static-recipient", Version: 1, EventType: CalendarNotificationEventType, Enabled: true, Destinations: []Destination{{Channel: InApp, RecipientRef: "technician:someone-else", ContentClassification: "internal"}}},
			{ID: "schedule", Version: 3, EventType: CalendarNotificationEventType, Enabled: true, Conditions: PolicyConditions{CalendarChangeClasses: []CalendarChangeClass{CalendarSchedule}, CalendarUrgencies: []CalendarUrgency{CalendarRoutine}}, Destinations: []Destination{{Channel: Email, RecipientRef: CalendarAssigneeRecipientRef, ContentClassification: "internal"}}},
		}},
		calendarPreferences: map[string]CalendarPreference{"tech": {TechnicianID: "tech", Rules: []CalendarPreferenceRule{
			{EventClass: CalendarScheduleChanged, ChangeClass: CalendarSchedule, Urgency: CalendarRoutine, Channel: InApp, Enabled: true},
			{EventClass: CalendarScheduleChanged, ChangeClass: CalendarSchedule, Urgency: CalendarRoutine, Channel: Email, Enabled: true},
		}}},
	}

	result, err := NewPlanner(repository, func() time.Time { return now }, sequentialTestIDs()).RunOnce(context.Background(), 10)
	if err != nil || result.Events != 1 || result.Pending != 1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	deliveries := repository.accepted[0].Deliveries
	if len(deliveries) != 1 || deliveries[0].Channel != Email || deliveries[0].PolicyID != "schedule" {
		t.Fatalf("deliveries=%+v, want only configured matching email", deliveries)
	}
}

func TestPlannerCalendarLegacyPreferenceClassDoesNotDisableCanonicalDefault(t *testing.T) {
	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	event := canonicalCalendarPlanningEvent(now)
	repository := &planningRepository{
		events:              []PlanningEvent{event},
		policies:            map[string][]PublishedPolicy{event.ID: {{ID: "schedule", Version: 1, EventType: CalendarNotificationEventType, Enabled: true, Destinations: []Destination{{Channel: Email, RecipientRef: CalendarAssigneeRecipientRef, ContentClassification: "internal"}}}}},
		calendarPreferences: map[string]CalendarPreference{"tech": {TechnicianID: "tech", Rules: []CalendarPreferenceRule{{EventClass: CalendarScheduleChanged, ChangeClass: CalendarAssigned, Urgency: CalendarRoutine, Channel: Email, Enabled: true}}}},
	}

	result, err := NewPlanner(repository, func() time.Time { return now }, sequentialTestIDs()).RunOnce(context.Background(), 10)
	if err != nil || result.Events != 1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if got := repository.accepted[0].Deliveries; len(got) != 1 || got[0].Channel != Email {
		t.Fatalf("legacy preference changed canonical default: %+v", got)
	}
}

func TestPlannerCalendarDeduplicatesDuplicateClaimsPerPolicyVersionAndDigestsSources(t *testing.T) {
	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	event := canonicalCalendarPlanningEvent(now)
	event.CalendarSources = append(event.CalendarSources, CalendarSourceReference{Type: "work_record", ID: "work-2", ClientID: "client", EventRole: "scheduled_work", SourceRevision: 8})
	policyV2 := PublishedPolicy{ID: "schedule", Version: 2, EventType: CalendarNotificationEventType, Enabled: true, Destinations: []Destination{{Channel: InApp, RecipientRef: CalendarAssigneeRecipientRef, ContentClassification: "internal"}}}
	policyV3 := policyV2
	policyV3.Version = 3
	repository := &planningRepository{
		events:   []PlanningEvent{event, event},
		policies: map[string][]PublishedPolicy{event.ID: {policyV2, policyV2, policyV3}},
	}

	result, err := NewPlanner(repository, func() time.Time { return now }, sequentialTestIDs()).RunOnce(context.Background(), 10)
	if err != nil || result.Events != 1 || result.Pending != 2 || repository.planCalls != 1 {
		t.Fatalf("result=%+v error=%v plan calls=%d", result, err, repository.planCalls)
	}
	deliveries := repository.accepted[0].Deliveries
	if len(deliveries) != 2 || deliveries[0].PolicyVersion != 2 || deliveries[1].PolicyVersion != 3 {
		t.Fatalf("deliveries=%+v, want one per policy version", deliveries)
	}
	for _, delivery := range deliveries {
		if delivery.CalendarPayload == nil || len(delivery.CalendarPayload.Sources) != 2 {
			t.Fatalf("delivery does not contain one two-source digest: %+v", delivery)
		}
	}
}

func TestPlannerCalendarMergesDistinctEventIDsIntoOneDigestAndKeepsDifferentUrgencySeparate(t *testing.T) {
	now := time.Date(2026, time.August, 15, 11, 0, 0, 0, time.UTC)
	first := canonicalCalendarPlanningEvent(now)
	first.ID = "calendar-a"
	first.CalendarSources = []CalendarSourceReference{{Type: "task", ID: "task-a", ClientID: "client", EventRole: "scheduled_work", SourceRevision: 1}}
	second := canonicalCalendarPlanningEvent(now)
	second.ID = "calendar-b"
	second.CalendarSources = []CalendarSourceReference{{Type: "task", ID: "task-b", ClientID: "client", EventRole: "scheduled_work", SourceRevision: 2}}
	differentUrgency := canonicalCalendarPlanningEvent(now)
	differentUrgency.ID = "calendar-c"
	differentUrgency.CalendarUrgency = CalendarImportant
	differentUrgency.CalendarSources = []CalendarSourceReference{{Type: "task", ID: "task-c", ClientID: "client", EventRole: "scheduled_work", SourceRevision: 3}}
	differentCorrelation := canonicalCalendarPlanningEvent(now)
	differentCorrelation.ID, differentCorrelation.CorrelationID = "calendar-d", "other-correlation"
	differentCorrelation.CalendarSources = []CalendarSourceReference{{Type: "task", ID: "task-d", ClientID: "client", EventRole: "scheduled_work", SourceRevision: 4}}
	differentRecipient := canonicalCalendarPlanningEvent(now)
	differentRecipient.ID, differentRecipient.CalendarRecipientID = "calendar-e", "other-tech"
	differentRecipient.CalendarSources = []CalendarSourceReference{{Type: "task", ID: "task-e", ClientID: "client", EventRole: "scheduled_work", SourceRevision: 5}}
	differentClass := canonicalCalendarPlanningEvent(now)
	differentClass.ID, differentClass.CalendarChangeClass = "calendar-f", CalendarPTO
	differentClass.CalendarSources = []CalendarSourceReference{{Type: "pto", ID: "pto-f", ClientID: "client", EventRole: "unavailability", SourceRevision: 6}}
	differentOrganization := canonicalCalendarPlanningEvent(now)
	differentOrganization.ID, differentOrganization.MSPID = "calendar-g", "other-msp"
	differentOrganization.CalendarSources = []CalendarSourceReference{{Type: "task", ID: "task-g", ClientID: "client", EventRole: "scheduled_work", SourceRevision: 7}}
	policy := PublishedPolicy{ID: "schedule", Version: 4, EventType: CalendarNotificationEventType, Enabled: true, Destinations: []Destination{{Channel: InApp, RecipientRef: CalendarAssigneeRecipientRef, ContentClassification: "internal"}}}
	repository := &planningRepository{
		events: []PlanningEvent{first, second, differentUrgency, differentCorrelation, differentRecipient, differentClass, differentOrganization},
		policies: map[string][]PublishedPolicy{
			first.ID:                 {policy},
			differentUrgency.ID:      {policy},
			differentCorrelation.ID:  {policy},
			differentRecipient.ID:    {policy},
			differentClass.ID:        {policy},
			differentOrganization.ID: {policy},
		},
	}

	result, err := NewPlanner(repository, func() time.Time { return now }, sequentialTestIDs()).RunOnce(context.Background(), 10)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Events != 7 || result.Pending != 6 || repository.planCalls != 6 {
		t.Fatalf("result=%+v plan calls=%d decisions=%+v", result, repository.planCalls, repository.accepted)
	}
	merged := repository.accepted[0]
	if len(merged.Events) != 2 || merged.Events[0].ID != "calendar-a" || merged.Events[1].ID != "calendar-b" {
		t.Fatalf("merged claimed events=%+v", merged.Events)
	}
	if len(merged.Deliveries) != 1 || merged.Deliveries[0].CalendarPayload == nil || len(merged.Deliveries[0].CalendarPayload.Sources) != 2 {
		t.Fatalf("merged digest=%+v", merged.Deliveries)
	}
	for index, wantID := range []string{"calendar-c", "calendar-d", "calendar-e", "calendar-f", "calendar-g"} {
		separate := repository.accepted[index+1]
		if len(separate.Events) != 1 || separate.Events[0].ID != wantID || len(separate.Deliveries) != 1 {
			t.Fatalf("separate digest %s=%+v", wantID, separate)
		}
	}
}

func TestPlannerCalendarMalformedCanonicalDataRemainsRetryable(t *testing.T) {
	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	event := canonicalCalendarPlanningEvent(now)
	event.CalendarChangeClass = CalendarAssigned
	repository := &planningRepository{events: []PlanningEvent{event}}

	result, err := NewPlanner(repository, func() time.Time { return now }, sequentialTestIDs()).RunOnce(context.Background(), 10)
	if err == nil {
		t.Fatal("RunOnce() error = nil, want malformed canonical calendar error")
	}
	if result != (PlanningResult{}) || repository.planCalls != 0 {
		t.Fatalf("malformed event was consumed: result=%+v plan calls=%d", result, repository.planCalls)
	}
}

func canonicalCalendarPlanningEvent(now time.Time) PlanningEvent {
	return PlanningEvent{
		ID: "calendar-event", Type: CalendarNotificationEventType, MSPID: "msp", ClientID: "client", OccurredAt: now,
		CorrelationID: "correlation", CalendarRecipientID: "tech", CalendarChangeClass: CalendarSchedule, CalendarUrgency: CalendarRoutine,
		CalendarSources: []CalendarSourceReference{{Type: "task", ID: "task-1", ClientID: "client", EventRole: "scheduled_work", SourceRevision: 7}}, CalendarActionPath: "/calendar",
	}
}

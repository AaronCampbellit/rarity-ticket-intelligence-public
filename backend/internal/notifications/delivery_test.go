package notifications

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/observability"
)

type deliveryRepository struct {
	jobs                   []DeliveryJob
	delivered              []string
	retries                []DeliveryFailure
	failed                 []DeliveryFailure
	suppressed             []DeliveryFailure
	authorization          DeliveryAuthorization
	authorizeCalls         int
	calendarAuthorization  CalendarRenderedDelivery
	calendarAuthorizeCalls int
	inbox                  map[string]RecipientNotification
	completed              []string
}

func (r *deliveryRepository) ReauthorizeCalendarDelivery(_ context.Context, _ DeliveryJob) (CalendarRenderedDelivery, error) {
	r.calendarAuthorizeCalls++
	return r.calendarAuthorization, nil
}

func (r *deliveryRepository) CompleteCalendarInApp(_ context.Context, job DeliveryJob, rendered CalendarRenderedDelivery, at time.Time) error {
	if r.inbox == nil {
		r.inbox = map[string]RecipientNotification{}
	}
	if _, exists := r.inbox[job.DeduplicationKey]; !exists {
		r.inbox[job.DeduplicationKey] = RecipientNotification{
			RecipientID: job.CalendarRecipientID, DeliveryID: job.ID,
			DeduplicationKey: job.DeduplicationKey, Title: rendered.Title,
			Body: rendered.Body, ActionPath: rendered.ActionPath,
			ContentClassification: job.ContentClassification, CreatedAt: at, Version: 1,
		}
	}
	r.completed = append(r.completed, job.ID)
	return nil
}

func calendarDeliveryJob(now time.Time, channel Channel) DeliveryJob {
	return DeliveryJob{
		ID: "calendar-delivery", EventID: "calendar-event", MSPID: "msp",
		CalendarRecipientID: "tech", Channel: channel, PlannedAt: now.Add(-time.Minute),
		ContentClassification: "internal", DeduplicationKey: "calendar-dedupe",
		CalendarPayload: &CalendarDeliveryPayload{
			CorrelationID: "correlation", ChangeClass: CalendarSchedule, Urgency: CalendarRoutine,
			Sources:    []CalendarSourceReference{{Type: "task", ID: "task", ClientID: "client", EventRole: "scheduled_work", SourceRevision: 7}},
			ActionPath: "/calendar",
		},
	}
}

func TestCalendarDeliveryCompletesOneRecipientInboxItemAndConvergesOnRetry(t *testing.T) {
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	repository := &deliveryRepository{
		jobs:                  []DeliveryJob{calendarDeliveryJob(now, InApp)},
		calendarAuthorization: CalendarRenderedDelivery{Authorized: true, Title: "Calendar schedule changed", Body: "Contoso: Install router", ActionPath: "/calendar"},
	}
	worker := NewDeliveryWorker(repository, nil, func() time.Time { return now })
	for attempt := 0; attempt < 2; attempt++ {
		result, err := worker.RunOnce(context.Background(), 1)
		if err != nil || result.Delivered != 1 {
			t.Fatalf("attempt %d result=%+v err=%v", attempt, result, err)
		}
	}
	if len(repository.inbox) != 1 || len(repository.completed) != 2 || repository.calendarAuthorizeCalls != 2 {
		t.Fatalf("inbox=%+v completed=%v authorize_calls=%d", repository.inbox, repository.completed, repository.calendarAuthorizeCalls)
	}
}

func TestCalendarDeliverySuppressesCurrentIneligibleRecipientOrDisabledPreference(t *testing.T) {
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	for _, reason := range []string{"recipient_inactive", "recipient_no_longer_relevant", "preference_disabled"} {
		t.Run(reason, func(t *testing.T) {
			repository := &deliveryRepository{jobs: []DeliveryJob{calendarDeliveryJob(now, InApp)}, calendarAuthorization: CalendarRenderedDelivery{SuppressionReason: reason}}
			result, err := NewDeliveryWorker(repository, nil, func() time.Time { return now }).RunOnce(context.Background(), 1)
			if err != nil || result.Suppressed != 1 || len(repository.suppressed) != 1 || repository.suppressed[0].ErrorCode != reason || len(repository.inbox) != 0 {
				t.Fatalf("result=%+v repository=%+v err=%v", result, repository, err)
			}
		})
	}
}

func TestCalendarDeliveryRetriesProjectionLagWithBackoff(t *testing.T) {
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	job := calendarDeliveryJob(now, InApp)
	job.Attempts = 2
	repository := &deliveryRepository{
		jobs: []DeliveryJob{job},
		calendarAuthorization: CalendarRenderedDelivery{
			Outcome: CalendarAuthorizationRetryable, RetryReason: "projection_lag",
		},
	}
	result, err := NewDeliveryWorker(repository, nil, func() time.Time { return now }).RunOnce(context.Background(), 1)
	if err != nil || result.Retrying != 1 || result.Suppressed != 0 || len(repository.retries) != 1 || len(repository.suppressed) != 0 {
		t.Fatalf("result=%+v retries=%+v suppressed=%+v error=%v", result, repository.retries, repository.suppressed, err)
	}
	retry := repository.retries[0]
	if retry.ErrorCode != "projection_lag" || !retry.NextAttemptAt.After(now) {
		t.Fatalf("retry=%+v now=%s", retry, now)
	}
}

func TestCalendarDeliveryEmailUsesCurrentAuthorizedRenderingAndRecipient(t *testing.T) {
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	repository := &deliveryRepository{
		jobs:                  []DeliveryJob{calendarDeliveryJob(now, Email)},
		calendarAuthorization: CalendarRenderedDelivery{Authorized: true, RecipientEmail: "current@example.test", Title: "Calendar schedule changed", Body: "calendar item", ActionPath: "https://rarity.example/calendar"},
	}
	transport := &emailTransport{}
	result, err := NewDeliveryWorkerWithEmail(repository, nil, NewEmailService(transport), func() time.Time { return now }).RunOnce(context.Background(), 1)
	if err != nil || result.Delivered != 1 || transport.request.Recipient != "current@example.test" || transport.request.Subject != "Calendar schedule changed" || transport.request.AuthenticatedURL != "https://rarity.example/calendar" || transport.request.IdempotencyKey != "calendar-dedupe" {
		t.Fatalf("result=%+v request=%+v err=%v", result, transport.request, err)
	}
}

func TestCalendarDeliveryEmailUnavailableIsTerminalAndTransportFailuresExpire(t *testing.T) {
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	unavailable := &deliveryRepository{jobs: []DeliveryJob{calendarDeliveryJob(now, Email)}, calendarAuthorization: CalendarRenderedDelivery{SuppressionReason: "channel_unavailable"}}
	result, err := NewDeliveryWorkerWithEmail(unavailable, nil, NewEmailService(&emailTransport{}), func() time.Time { return now }).RunOnce(context.Background(), 1)
	if err != nil || result.Suppressed != 1 || len(unavailable.retries) != 0 {
		t.Fatalf("unavailable result=%+v repository=%+v err=%v", result, unavailable, err)
	}

	for _, test := range []struct {
		age              time.Duration
		retrying, failed int
	}{{time.Hour, 1, 0}, {24 * time.Hour, 0, 1}} {
		job := calendarDeliveryJob(now, Email)
		job.PlannedAt = now.Add(-test.age)
		repository := &deliveryRepository{jobs: []DeliveryJob{job}, calendarAuthorization: CalendarRenderedDelivery{Authorized: true, RecipientEmail: "tech@example.test", Title: "Calendar changed", Body: "Safe", ActionPath: "https://rarity.example/calendar"}}
		result, err = NewDeliveryWorkerWithEmail(repository, nil, NewEmailService(&emailTransport{err: errors.New("offline")}), func() time.Time { return now }).RunOnce(context.Background(), 1)
		if err != nil || result.Retrying != test.retrying || result.Failed != test.failed {
			t.Fatalf("age=%s result=%+v repository=%+v err=%v", test.age, result, repository, err)
		}
	}
}

func TestCalendarDeliveryInvalidAuthorizedEmailURLIsTerminal(t *testing.T) {
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	repository := &deliveryRepository{
		jobs: []DeliveryJob{calendarDeliveryJob(now, Email)},
		calendarAuthorization: CalendarRenderedDelivery{
			Authorized: true, RecipientEmail: "tech@example.test", Title: "Calendar changed",
			Body: "Safe", ActionPath: "https://rarity.example/calendar#restricted",
		},
	}
	transport := &emailTransport{}
	result, err := NewDeliveryWorkerWithEmail(repository, nil, NewEmailService(transport), func() time.Time { return now }).RunOnce(context.Background(), 1)
	if err != nil || result.Suppressed != 1 || len(repository.suppressed) != 1 || repository.suppressed[0].ErrorCode != "channel_unavailable" || len(repository.retries) != 0 || transport.request.EventID != "" {
		t.Fatalf("result=%+v repository=%+v request=%+v err=%v", result, repository, transport.request, err)
	}
}

func TestMentionDeliveryEmitsOutcomeCounter(t *testing.T) {
	now := time.Date(2026, time.August, 8, 17, 0, 0, 0, time.UTC)
	telemetry := observability.NewMentionTelemetry(nil)
	repository := &deliveryRepository{
		jobs:          []DeliveryJob{{ID: "email", EventID: "event", SubjectType: "project", SubjectID: "project", MentionOccurrenceID: "occurrence", RecipientTechnicianID: "tech", Channel: Email, PlannedAt: now}},
		authorization: DeliveryAuthorization{SuppressionReason: "access_revoked"},
	}
	worker := NewDeliveryWorkerWithEmail(repository, nil, nil, func() time.Time { return now }).WithTelemetry(telemetry)
	if _, err := worker.RunOnce(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if telemetry.Value("notification", "project", "access_revoked") != 1 {
		t.Fatal("notification outcome counter missing")
	}
}

func TestMentionDeliveryEmitsRetryAndTerminalOutcomeCounters(t *testing.T) {
	now := time.Date(2026, time.August, 8, 17, 0, 0, 0, time.UTC)
	telemetry := observability.NewMentionTelemetry(nil)
	repository := &deliveryRepository{
		jobs: []DeliveryJob{
			{ID: "retry", EventID: "retry-event", SubjectType: "task", SubjectID: "task", MentionOccurrenceID: "retry-occurrence", RecipientTechnicianID: "tech", Channel: Email, PlannedAt: now.Add(-time.Hour)},
			{ID: "failed", EventID: "failed-event", SubjectType: "project", SubjectID: "project", MentionOccurrenceID: "failed-occurrence", RecipientTechnicianID: "tech", Channel: Email, PlannedAt: now.Add(-25 * time.Hour)},
		},
		authorization: DeliveryAuthorization{Authorized: true, RecipientEmail: "tech@example.test", AuthorLabel: "Ada", Summary: WorkSummary{DisplayID: "OBJ-1", Subject: "Safe", AuthenticatedURL: "https://rarity.example/mentions"}},
	}
	worker := NewDeliveryWorkerWithEmail(repository, nil, NewEmailService(&emailTransport{err: errors.New("offline")}), func() time.Time { return now }).WithTelemetry(telemetry)
	if _, err := worker.RunOnce(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	if telemetry.Value("notification", "task", "retrying") != 1 {
		t.Fatal("notification retry counter missing")
	}
	if telemetry.Value("notification", "project", "failed") != 1 {
		t.Fatal("notification terminal counter missing")
	}
}

func (r *deliveryRepository) ReauthorizeMentionDelivery(_ context.Context, _ DeliveryJob) (DeliveryAuthorization, error) {
	r.authorizeCalls++
	return r.authorization, nil
}

func (r *deliveryRepository) MarkSuppressed(_ context.Context, failure DeliveryFailure) error {
	r.suppressed = append(r.suppressed, failure)
	return nil
}

func (r *deliveryRepository) ClaimPending(context.Context, int, time.Time) ([]DeliveryJob, error) {
	return r.jobs, nil
}

func TestMentionDeliveryReauthorizesImmediatelyAndUsesCurrentEmail(t *testing.T) {
	now := time.Date(2026, time.August, 7, 17, 0, 0, 0, time.UTC)
	repository := &deliveryRepository{
		jobs:          []DeliveryJob{{ID: "email", EventID: "event", SubjectID: "task", MentionOccurrenceID: "occurrence", RecipientTechnicianID: "tech", Channel: Email, PlannedAt: now.Add(-time.Minute)}},
		authorization: DeliveryAuthorization{Authorized: true, RecipientEmail: "current@example.test", AuthorLabel: "Ada", Summary: WorkSummary{DisplayID: "TASK-2", Subject: "Investigate VPN", AuthenticatedURL: "https://rarity.example/mentions?occurrence=occurrence"}},
	}
	transport := &emailTransport{}
	worker := NewDeliveryWorkerWithEmail(repository, nil, NewEmailService(transport), func() time.Time { return now })
	result, err := worker.RunOnce(context.Background(), 10)
	if err != nil || result.Delivered != 1 || repository.authorizeCalls != 1 || transport.request.Recipient != "current@example.test" {
		t.Fatalf("result=%+v request=%+v repo=%+v err=%v", result, transport.request, repository, err)
	}
}

func TestMentionDeliverySuppressesRevokedAccessWithoutTransport(t *testing.T) {
	now := time.Date(2026, time.August, 7, 17, 0, 0, 0, time.UTC)
	repository := &deliveryRepository{
		jobs:          []DeliveryJob{{ID: "teams", EventID: "event", SubjectID: "project", MentionOccurrenceID: "occurrence", RecipientTechnicianID: "tech", Channel: Teams, PlannedAt: now.Add(-time.Minute)}},
		authorization: DeliveryAuthorization{SuppressionReason: "access_revoked"},
	}
	worker := NewDeliveryWorkerWithEmail(repository, nil, nil, func() time.Time { return now })
	result, err := worker.RunOnce(context.Background(), 10)
	if err != nil || result.Suppressed != 1 || len(repository.suppressed) != 1 || repository.suppressed[0].ErrorCode != "access_revoked" || len(repository.retries) != 0 {
		t.Fatalf("result=%+v repo=%+v err=%v", result, repository, err)
	}
}

func TestMentionDeliverySuppressesUnavailableCurrentEmailWithoutTransportRetry(t *testing.T) {
	now := time.Date(2026, time.August, 7, 17, 0, 0, 0, time.UTC)
	repository := &deliveryRepository{
		jobs:          []DeliveryJob{{ID: "email", EventID: "event", MentionOccurrenceID: "occurrence", RecipientTechnicianID: "tech", Channel: Email, PlannedAt: now.Add(-time.Minute)}},
		authorization: DeliveryAuthorization{SuppressionReason: "channel_unavailable"},
	}
	transport := &emailTransport{}
	worker := NewDeliveryWorkerWithEmail(repository, nil, NewEmailService(transport), func() time.Time { return now })
	result, err := worker.RunOnce(context.Background(), 10)
	if err != nil || result.Suppressed != 1 || len(repository.suppressed) != 1 || len(repository.retries) != 0 || transport.request.Recipient != "" {
		t.Fatalf("result=%+v repo=%+v request=%+v err=%v", result, repository, transport.request, err)
	}
}

func (r *deliveryRepository) MarkDelivered(
	_ context.Context,
	id string,
	_ int,
	_ time.Time,
) error {
	r.delivered = append(r.delivered, id)
	return nil
}

func (r *deliveryRepository) MarkRetry(
	_ context.Context,
	failure DeliveryFailure,
) error {
	r.retries = append(r.retries, failure)
	return nil
}

func (r *deliveryRepository) MarkFailed(
	_ context.Context,
	failure DeliveryFailure,
) error {
	r.failed = append(r.failed, failure)
	return nil
}

func TestDeliveryWorkerCompletesInAppAndTeamsJobs(t *testing.T) {
	now := time.Date(2026, time.July, 29, 17, 0, 0, 0, time.UTC)
	repository := &deliveryRepository{jobs: []DeliveryJob{
		{ID: "in-app", Channel: InApp, Attempts: 0, PlannedAt: now.Add(-time.Minute)},
		{
			ID: "teams", EventID: "event", WorkRecordID: "work",
			Channel: Teams, Attempts: 0, PlannedAt: now.Add(-time.Minute),
			Connection: TeamsConnection{
				ID: "connection", MSPID: "msp", WebhookSecretRef: "secret-ref", Version: 1,
			},
			Summary: WorkSummary{
				DisplayID: "INC-1", Priority: "critical", Status: "open",
				Subject:          "Service unavailable",
				AuthenticatedURL: "https://rarity.example/work/INC-1",
			},
		},
	}}
	history := &teamsHistory{}
	teams := NewTeamsService(
		&teamsSecrets{value: "https://teams.example.test/hook"},
		&teamsSender{}, history, func() time.Time { return now },
	)
	worker := NewDeliveryWorker(repository, teams, func() time.Time { return now })
	result, err := worker.RunOnce(context.Background(), 100)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Delivered != 2 || len(repository.delivered) != 2 ||
		repository.delivered[0] != "in-app" || repository.delivered[1] != "teams" ||
		history.attempt.EventID != "event" || history.attempt.State != TeamsDelivered {
		t.Fatalf("delivery incomplete: result=%+v repository=%+v history=%+v", result, repository, history)
	}
}

func TestDeliveryWorkerRetriesTeamsThenFailsAtTwentyFourHours(t *testing.T) {
	now := time.Date(2026, time.July, 29, 17, 0, 0, 0, time.UTC)
	repository := &deliveryRepository{jobs: []DeliveryJob{{
		ID: "retry", EventID: "event", WorkRecordID: "work",
		Channel: Teams, Attempts: 2, PlannedAt: now.Add(-23 * time.Hour),
		Connection: TeamsConnection{
			ID: "connection", MSPID: "msp", WebhookSecretRef: "secret-ref", Version: 1,
		},
		Summary: WorkSummary{
			DisplayID: "INC-1", Priority: "critical", Status: "open",
			Subject:          "Service unavailable",
			AuthenticatedURL: "https://rarity.example/work/INC-1",
		},
	}}}
	teams := NewTeamsService(
		&teamsSecrets{value: "https://teams.example.test/hook"},
		&teamsSender{err: errors.New("offline")}, &teamsHistory{},
		func() time.Time { return now },
	)
	worker := NewDeliveryWorker(repository, teams, func() time.Time { return now })
	result, err := worker.RunOnce(context.Background(), 10)
	if err != nil || result.Retrying != 1 || len(repository.retries) != 1 ||
		repository.retries[0].ErrorCode != "delivery_failed" ||
		!repository.retries[0].NextAttemptAt.After(now) {
		t.Fatalf("retry not persisted: result=%+v err=%v repository=%+v", result, err, repository)
	}

	repository.jobs[0].ID = "expired"
	repository.jobs[0].PlannedAt = now.Add(-24 * time.Hour)
	repository.retries = nil
	result, err = worker.RunOnce(context.Background(), 10)
	if err != nil || result.Failed != 1 || len(repository.failed) != 1 ||
		repository.failed[0].ErrorCode != "retry_window_expired" {
		t.Fatalf("terminal failure not persisted: result=%+v err=%v repository=%+v", result, err, repository)
	}
}

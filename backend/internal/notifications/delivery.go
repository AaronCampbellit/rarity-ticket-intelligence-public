package notifications

import (
	"context"
	"errors"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/observability"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/outbox"
)

type DeliveryJob struct {
	ID                    string
	EventID               string
	MSPID                 string
	WorkRecordID          string
	SubjectType           string
	SubjectID             string
	MentionOccurrenceID   string
	RecipientTechnicianID string
	CalendarRecipientID   string
	Channel               Channel
	ContentClassification string
	DeduplicationKey      string
	CalendarPayload       *CalendarDeliveryPayload
	Attempts              int
	PlannedAt             time.Time
	Connection            TeamsConnection
	Summary               WorkSummary
}

type CalendarAuthorizationOutcome string

const (
	CalendarAuthorizationAuthorized CalendarAuthorizationOutcome = "authorized"
	CalendarAuthorizationSuppressed CalendarAuthorizationOutcome = "suppressed"
	CalendarAuthorizationRetryable  CalendarAuthorizationOutcome = "retryable"
)

type CalendarRenderedDelivery struct {
	Outcome           CalendarAuthorizationOutcome
	Authorized        bool
	SuppressionReason string
	RetryReason       string
	RecipientEmail    string
	Title             string
	Body              string
	ActionPath        string
}

type DeliveryAuthorization struct {
	Authorized        bool
	SuppressionReason string
	RecipientEmail    string
	AuthorLabel       string
	Connection        TeamsConnection
	Summary           WorkSummary
}

type DeliveryFailure struct {
	ID               string
	ExpectedAttempts int
	ErrorCode        string
	NextAttemptAt    time.Time
	FailedAt         time.Time
}

type DeliveryRunResult struct {
	Claimed    int
	Delivered  int
	Retrying   int
	Failed     int
	Suppressed int
}

type DeliveryRepository interface {
	ClaimPending(context.Context, int, time.Time) ([]DeliveryJob, error)
	MarkDelivered(context.Context, string, int, time.Time) error
	MarkRetry(context.Context, DeliveryFailure) error
	MarkFailed(context.Context, DeliveryFailure) error
	ReauthorizeMentionDelivery(context.Context, DeliveryJob) (DeliveryAuthorization, error)
	ReauthorizeCalendarDelivery(context.Context, DeliveryJob) (CalendarRenderedDelivery, error)
	CompleteCalendarInApp(context.Context, DeliveryJob, CalendarRenderedDelivery, time.Time) error
	MarkSuppressed(context.Context, DeliveryFailure) error
}

type DeliveryWorker struct {
	repository DeliveryRepository
	teams      *TeamsService
	email      *EmailService
	now        func() time.Time
	telemetry  *observability.MentionTelemetry
}

func (w *DeliveryWorker) WithTelemetry(telemetry *observability.MentionTelemetry) *DeliveryWorker {
	if w != nil {
		w.telemetry = telemetry
	}
	return w
}

func NewDeliveryWorker(
	repository DeliveryRepository,
	teams *TeamsService,
	now func() time.Time,
) *DeliveryWorker {
	return &DeliveryWorker{repository: repository, teams: teams, now: now}
}

func NewDeliveryWorkerWithEmail(repository DeliveryRepository, teams *TeamsService, email *EmailService, now func() time.Time) *DeliveryWorker {
	return &DeliveryWorker{repository: repository, teams: teams, email: email, now: now}
}

func (w *DeliveryWorker) RunOnce(
	ctx context.Context,
	limit int,
) (DeliveryRunResult, error) {
	now := w.now().UTC()
	jobs, err := w.repository.ClaimPending(ctx, limit, now)
	if err != nil {
		return DeliveryRunResult{}, err
	}
	result := DeliveryRunResult{Claimed: len(jobs)}
	for _, job := range jobs {
		if job.CalendarPayload != nil {
			rendered, err := w.repository.ReauthorizeCalendarDelivery(ctx, job)
			if err != nil {
				return result, err
			}
			if rendered.Outcome == CalendarAuthorizationRetryable {
				reason := rendered.RetryReason
				if reason == "" {
					reason = "authorization_pending"
				}
				if !job.PlannedAt.Add(24 * time.Hour).After(now) {
					if err := w.repository.MarkFailed(ctx, DeliveryFailure{ID: job.ID, ExpectedAttempts: job.Attempts, ErrorCode: "retry_window_expired", FailedAt: now}); err != nil {
						return result, err
					}
					result.Failed++
				} else {
					if err := w.repository.MarkRetry(ctx, DeliveryFailure{ID: job.ID, ExpectedAttempts: job.Attempts, ErrorCode: reason, NextAttemptAt: now.Add(outbox.RetryDelay(job.Attempts))}); err != nil {
						return result, err
					}
					result.Retrying++
				}
				continue
			}
			if !rendered.Authorized {
				reason := rendered.SuppressionReason
				if reason == "" {
					reason = "recipient_no_longer_relevant"
				}
				if err := w.repository.MarkSuppressed(ctx, DeliveryFailure{ID: job.ID, ExpectedAttempts: job.Attempts, ErrorCode: reason, FailedAt: now}); err != nil {
					return result, err
				}
				result.Suppressed++
				continue
			}
			switch job.Channel {
			case InApp:
				if err := w.repository.CompleteCalendarInApp(ctx, job, rendered, now); err != nil {
					return result, err
				}
				result.Delivered++
			case Email:
				if w.email == nil {
					if err := w.repository.MarkSuppressed(ctx, DeliveryFailure{ID: job.ID, ExpectedAttempts: job.Attempts, ErrorCode: "channel_unavailable", FailedAt: now}); err != nil {
						return result, err
					}
					result.Suppressed++
					continue
				}
				calendarEmail := CalendarEmail{EventID: job.EventID, DeduplicationKey: job.DeduplicationKey, Recipient: rendered.RecipientEmail, Title: rendered.Title, Body: rendered.Body, AuthenticatedURL: rendered.ActionPath}
				if !validCalendarEmail(calendarEmail) {
					if err := w.repository.MarkSuppressed(ctx, DeliveryFailure{ID: job.ID, ExpectedAttempts: job.Attempts, ErrorCode: "channel_unavailable", FailedAt: now}); err != nil {
						return result, err
					}
					result.Suppressed++
					continue
				}
				err = w.email.DeliverCalendar(ctx, calendarEmail)
				if err == nil {
					if err := w.repository.MarkDelivered(ctx, job.ID, job.Attempts, now); err != nil {
						return result, err
					}
					result.Delivered++
					continue
				}
				if !job.PlannedAt.Add(24 * time.Hour).After(now) {
					if err := w.repository.MarkFailed(ctx, DeliveryFailure{ID: job.ID, ExpectedAttempts: job.Attempts, ErrorCode: "retry_window_expired", FailedAt: now}); err != nil {
						return result, err
					}
					result.Failed++
				} else {
					if err := w.repository.MarkRetry(ctx, DeliveryFailure{ID: job.ID, ExpectedAttempts: job.Attempts, ErrorCode: "delivery_failed", NextAttemptAt: now.Add(outbox.RetryDelay(job.Attempts))}); err != nil {
						return result, err
					}
					result.Retrying++
				}
			default:
				if err := w.repository.MarkFailed(ctx, DeliveryFailure{ID: job.ID, ExpectedAttempts: job.Attempts, ErrorCode: "channel_unavailable", FailedAt: now}); err != nil {
					return result, err
				}
				result.Failed++
			}
			continue
		}
		if job.MentionOccurrenceID != "" || job.RecipientTechnicianID != "" {
			authorized, err := w.repository.ReauthorizeMentionDelivery(ctx, job)
			if err != nil {
				return result, err
			}
			if !authorized.Authorized {
				reason := authorized.SuppressionReason
				if reason == "" {
					reason = "access_revoked"
				}
				if err := w.repository.MarkSuppressed(ctx, DeliveryFailure{ID: job.ID, ExpectedAttempts: job.Attempts, ErrorCode: reason, FailedAt: now}); err != nil {
					return result, err
				}
				result.Suppressed++
				w.countMentionDelivery(job, reason)
				continue
			}
			job.Connection, job.Summary = authorized.Connection, authorized.Summary
			if job.WorkRecordID == "" {
				job.WorkRecordID = job.SubjectID
			}
			if job.Channel == Email {
				if w.email == nil {
					if err := w.repository.MarkSuppressed(ctx, DeliveryFailure{ID: job.ID, ExpectedAttempts: job.Attempts, ErrorCode: "channel_unavailable", FailedAt: now}); err != nil {
						return result, err
					}
					result.Suppressed++
					w.countMentionDelivery(job, "channel_unavailable")
					continue
				}
				err = w.email.Deliver(ctx, MentionEmail{EventID: job.EventID, Recipient: authorized.RecipientEmail, AuthorLabel: authorized.AuthorLabel, ObjectDisplayID: authorized.Summary.DisplayID, ObjectSubject: authorized.Summary.Subject, AuthenticatedURL: authorized.Summary.AuthenticatedURL})
				if err == nil {
					if err := w.repository.MarkDelivered(ctx, job.ID, job.Attempts, now); err != nil {
						return result, err
					}
					result.Delivered++
					w.countMentionDelivery(job, "delivered")
					continue
				}
				if !job.PlannedAt.Add(24 * time.Hour).After(now) {
					if err := w.repository.MarkFailed(ctx, DeliveryFailure{ID: job.ID, ExpectedAttempts: job.Attempts, ErrorCode: "retry_window_expired", FailedAt: now}); err != nil {
						return result, err
					}
					result.Failed++
					w.countMentionDelivery(job, "failed")
				} else {
					if err := w.repository.MarkRetry(ctx, DeliveryFailure{ID: job.ID, ExpectedAttempts: job.Attempts, ErrorCode: "delivery_failed", NextAttemptAt: now.Add(outbox.RetryDelay(job.Attempts))}); err != nil {
						return result, err
					}
					result.Retrying++
					w.countMentionDelivery(job, "retrying")
				}
				continue
			}
		}
		switch job.Channel {
		case InApp:
			if err := w.repository.MarkDelivered(
				ctx, job.ID, job.Attempts, now,
			); err != nil {
				return result, err
			}
			result.Delivered++
			w.countMentionDelivery(job, "delivered")
		case Teams:
			if w.teams == nil {
				if err := w.repository.MarkFailed(ctx, DeliveryFailure{
					ID: job.ID, ExpectedAttempts: job.Attempts,
					ErrorCode: "channel_unavailable", FailedAt: now,
				}); err != nil {
					return result, err
				}
				result.Failed++
				w.countMentionDelivery(job, "failed")
				continue
			}
			err := w.teams.Deliver(ctx, TeamsRequest{
				Connection: job.Connection, EventID: job.EventID,
				WorkRecordID: job.WorkRecordID, Summary: job.Summary,
				FirstAttemptAt: job.PlannedAt, Attempt: job.Attempts + 1,
			})
			if err == nil {
				if err := w.repository.MarkDelivered(
					ctx, job.ID, job.Attempts, now,
				); err != nil {
					return result, err
				}
				result.Delivered++
				w.countMentionDelivery(job, "delivered")
				continue
			}
			if errors.Is(err, ErrTeamsRetryExpired) {
				if err := w.repository.MarkFailed(ctx, DeliveryFailure{
					ID: job.ID, ExpectedAttempts: job.Attempts,
					ErrorCode: "retry_window_expired", FailedAt: now,
				}); err != nil {
					return result, err
				}
				result.Failed++
				w.countMentionDelivery(job, "failed")
				continue
			}
			if err := w.repository.MarkRetry(ctx, DeliveryFailure{
				ID: job.ID, ExpectedAttempts: job.Attempts,
				ErrorCode:     "delivery_failed",
				NextAttemptAt: now.Add(outbox.RetryDelay(job.Attempts)),
			}); err != nil {
				return result, err
			}
			result.Retrying++
			w.countMentionDelivery(job, "retrying")
		default:
			if err := w.repository.MarkFailed(ctx, DeliveryFailure{
				ID: job.ID, ExpectedAttempts: job.Attempts,
				ErrorCode: "channel_unavailable", FailedAt: now,
			}); err != nil {
				return result, err
			}
			result.Failed++
			w.countMentionDelivery(job, "failed")
		}
	}
	return result, nil
}

func (w *DeliveryWorker) countMentionDelivery(job DeliveryJob, outcome string) {
	if job.MentionOccurrenceID == "" && job.RecipientTechnicianID == "" {
		return
	}
	w.telemetry.Count(observability.MentionMetric{
		Name: "notification", ParentType: job.SubjectType,
		Channel: string(job.Channel), Outcome: outcome,
		ObjectID: job.SubjectID, OccurrenceID: job.MentionOccurrenceID,
	})
}

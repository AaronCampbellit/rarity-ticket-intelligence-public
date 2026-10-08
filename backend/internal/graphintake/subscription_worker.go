package graphintake

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
)

const (
	subscriptionLease       = 5 * time.Minute
	subscriptionLifetime    = 6*24*time.Hour + 23*time.Hour
	subscriptionRenewalLead = 24 * time.Hour
)

var ErrInvalidSubscription = errors.New("invalid Graph subscription")

type SubscriptionJob struct {
	ID                   string
	ConnectionID         string
	Mailbox              string
	CredentialSecretRef  string
	ClientStateSecretRef string
	ExternalID           string
	Resource             string
	ExpiresAt            time.Time
	Recovery             RecoveryAction
}

type SubscriptionRequest struct {
	ExternalID               string
	Resource                 string
	ChangeType               string
	NotificationURL          string
	LifecycleNotificationURL string
	ClientState              string
	ExpiresAt                time.Time
}

type ProviderSubscription struct {
	ID        string
	Resource  string
	ExpiresAt time.Time
}

type SubscriptionCompletion struct {
	ID           string
	ConnectionID string
	ExternalID   string
	Resource     string
	ExpiresAt    time.Time
	CompletedAt  time.Time
	Recovery     RecoveryAction
	Created      bool
}

type SubscriptionFailure struct {
	ID           string
	ConnectionID string
	FailedAt     time.Time
	ErrorCode    string
}

type SubscriptionQueue interface {
	ClaimSubscriptions(
		context.Context,
		int,
		time.Time,
		time.Duration,
	) ([]SubscriptionJob, error)
	CompleteSubscription(context.Context, SubscriptionCompletion) error
	FailSubscription(context.Context, SubscriptionFailure) error
}

type SubscriptionProvider interface {
	Create(
		context.Context,
		SubscriptionRequest,
	) (ProviderSubscription, error)
	Renew(
		context.Context,
		SubscriptionRequest,
	) (ProviderSubscription, error)
}

type SubscriptionProviderFactory interface {
	Provider(
		context.Context,
		SubscriptionJob,
	) (SubscriptionProvider, error)
}

type SubscriptionWorker struct {
	queue           SubscriptionQueue
	providers       SubscriptionProviderFactory
	clientStates    ClientStateResolver
	notificationURL string
	now             func() time.Time
}

type SubscriptionWorkerResult struct {
	Claimed int
	Created int
	Renewed int
	Failed  int
}

func NewSubscriptionWorker(
	queue SubscriptionQueue,
	providers SubscriptionProviderFactory,
	clientStates ClientStateResolver,
	notificationURL string,
	now func() time.Time,
) *SubscriptionWorker {
	return &SubscriptionWorker{
		queue: queue, providers: providers, clientStates: clientStates,
		notificationURL: strings.TrimSpace(notificationURL), now: now,
	}
}

func (w *SubscriptionWorker) RunOnce(
	ctx context.Context,
	limit int,
) (SubscriptionWorkerResult, error) {
	if w.queue == nil || w.providers == nil || w.clientStates == nil ||
		w.now == nil || limit <= 0 ||
		!validNotificationURL(w.notificationURL) {
		return SubscriptionWorkerResult{}, ErrInvalidSubscription
	}
	now := w.now().UTC()
	jobs, err := w.queue.ClaimSubscriptions(
		ctx, limit, now, subscriptionLease,
	)
	if err != nil {
		return SubscriptionWorkerResult{}, err
	}
	result := SubscriptionWorkerResult{Claimed: len(jobs)}
	for _, job := range jobs {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		created := strings.TrimSpace(job.ExternalID) == "" ||
			job.Recovery == RecoveryRecreate
		clientState, err := w.clientStates.ResolveClientState(
			ctx, job.ClientStateSecretRef,
		)
		var provider SubscriptionProvider
		if err == nil {
			provider, err = w.providers.Provider(ctx, job)
		}
		request := SubscriptionRequest{
			ExternalID: job.ExternalID, Resource: job.Resource,
			ChangeType:               "created,updated",
			NotificationURL:          w.notificationURL,
			LifecycleNotificationURL: w.notificationURL,
			ClientState:              clientState,
			ExpiresAt:                now.Add(subscriptionLifetime),
		}
		var subscription ProviderSubscription
		if err == nil {
			if created {
				subscription, err = provider.Create(ctx, request)
			} else {
				subscription, err = provider.Renew(ctx, request)
			}
		}
		if err == nil {
			err = validateProviderSubscription(subscription, request, now)
		}
		if err != nil {
			if failErr := w.queue.FailSubscription(
				ctx,
				SubscriptionFailure{
					ID: job.ID, ConnectionID: job.ConnectionID,
					FailedAt: now, ErrorCode: subscriptionFailureCode(err),
				},
			); failErr != nil {
				return result, failErr
			}
			result.Failed++
			continue
		}
		recovery := RecoveryNone
		if created && job.Recovery == RecoveryRecreate {
			recovery = RecoveryRunDelta
		}
		if err := w.queue.CompleteSubscription(
			ctx,
			SubscriptionCompletion{
				ID: job.ID, ConnectionID: job.ConnectionID,
				ExternalID: subscription.ID, Resource: subscription.Resource,
				ExpiresAt: subscription.ExpiresAt, CompletedAt: now,
				Recovery: recovery, Created: created,
			},
		); err != nil {
			return result, err
		}
		if created {
			result.Created++
		} else {
			result.Renewed++
		}
	}
	return result, nil
}

func validNotificationURL(raw string) bool {
	value, err := url.Parse(raw)
	return err == nil && value.IsAbs() && value.Scheme == "https" &&
		value.Host != "" && value.User == nil && value.Fragment == ""
}

func validateProviderSubscription(
	subscription ProviderSubscription,
	request SubscriptionRequest,
	now time.Time,
) error {
	if strings.TrimSpace(subscription.ID) == "" ||
		strings.TrimSpace(subscription.Resource) !=
			strings.TrimSpace(request.Resource) ||
		!subscription.ExpiresAt.After(now) ||
		subscription.ExpiresAt.After(request.ExpiresAt.Add(time.Minute)) {
		return ErrInvalidSubscription
	}
	return nil
}

func subscriptionFailureCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidGraphCredentials):
		return "credentials_invalid"
	case errors.Is(err, ErrInvalidSubscription):
		return "subscription_invalid"
	default:
		return "subscription_provider_failed"
	}
}

package graphintake

import (
	"context"
	"testing"
	"time"
)

type subscriptionQueueStub struct {
	jobs      []SubscriptionJob
	completed []SubscriptionCompletion
	failed    []SubscriptionFailure
}

func (q *subscriptionQueueStub) ClaimSubscriptions(
	context.Context,
	int,
	time.Time,
	time.Duration,
) ([]SubscriptionJob, error) {
	return q.jobs, nil
}

func (q *subscriptionQueueStub) CompleteSubscription(
	_ context.Context,
	completion SubscriptionCompletion,
) error {
	q.completed = append(q.completed, completion)
	return nil
}

func (q *subscriptionQueueStub) FailSubscription(
	_ context.Context,
	failure SubscriptionFailure,
) error {
	q.failed = append(q.failed, failure)
	return nil
}

type subscriptionProviderStub struct {
	created []SubscriptionRequest
	renewed []SubscriptionRequest
}

func (p *subscriptionProviderStub) Create(
	_ context.Context,
	request SubscriptionRequest,
) (ProviderSubscription, error) {
	p.created = append(p.created, request)
	return ProviderSubscription{
		ID: "new-subscription-id", Resource: request.Resource,
		ExpiresAt: request.ExpiresAt,
	}, nil
}

func (p *subscriptionProviderStub) Renew(
	_ context.Context,
	request SubscriptionRequest,
) (ProviderSubscription, error) {
	p.renewed = append(p.renewed, request)
	return ProviderSubscription{
		ID: request.ExternalID, Resource: request.Resource,
		ExpiresAt: request.ExpiresAt,
	}, nil
}

type subscriptionProviderFactoryStub struct {
	provider SubscriptionProvider
}

func (f subscriptionProviderFactoryStub) Provider(
	context.Context,
	SubscriptionJob,
) (SubscriptionProvider, error) {
	return f.provider, nil
}

type graphClientStateStub struct {
	value string
}

func (s graphClientStateStub) ResolveClientState(
	context.Context,
	string,
) (string, error) {
	return s.value, nil
}

func TestSubscriptionWorkerRenewsBeforeExpiryWithLifecycleEndpoint(t *testing.T) {
	now := time.Date(2026, time.July, 29, 22, 0, 0, 0, time.UTC)
	queue := &subscriptionQueueStub{jobs: []SubscriptionJob{{
		ID: "subscription-record-id", ConnectionID: "connection-id",
		Mailbox:              "support@example.com",
		CredentialSecretRef:  "env://RARITY_GRAPH_CREDENTIAL_PRIMARY",
		ClientStateSecretRef: "env://RARITY_GRAPH_CLIENT_STATE_PRIMARY",
		ExternalID:           "graph-subscription-id",
		Resource:             "users/support@example.com/mailFolders('Inbox')/messages",
		ExpiresAt:            now.Add(30 * time.Minute),
	}}}
	provider := &subscriptionProviderStub{}
	worker := NewSubscriptionWorker(
		queue,
		subscriptionProviderFactoryStub{provider: provider},
		graphClientStateStub{value: "client-state"},
		"https://rarity.example/api/v1/graph/notifications",
		func() time.Time { return now },
	)

	result, err := worker.RunOnce(context.Background(), 25)

	if err != nil || result.Renewed != 1 ||
		len(provider.renewed) != 1 || len(provider.created) != 0 ||
		provider.renewed[0].ClientState != "client-state" ||
		provider.renewed[0].NotificationURL !=
			"https://rarity.example/api/v1/graph/notifications" ||
		provider.renewed[0].LifecycleNotificationURL !=
			provider.renewed[0].NotificationURL ||
		provider.renewed[0].ExpiresAt != now.Add(6*24*time.Hour+23*time.Hour) ||
		len(queue.completed) != 1 || len(queue.failed) != 0 {
		t.Fatalf(
			"RunOnce() result=%+v error=%v renewed=%+v completed=%+v failed=%+v",
			result, err, provider.renewed, queue.completed, queue.failed,
		)
	}
}

func TestSubscriptionWorkerRecreatesRemovedSubscription(t *testing.T) {
	now := time.Now().UTC()
	queue := &subscriptionQueueStub{jobs: []SubscriptionJob{{
		ID: "subscription-record-id", ConnectionID: "connection-id",
		Mailbox:              "support@example.com",
		CredentialSecretRef:  "env://RARITY_GRAPH_CREDENTIAL_PRIMARY",
		ClientStateSecretRef: "env://RARITY_GRAPH_CLIENT_STATE_PRIMARY",
		ExternalID:           "removed-subscription-id",
		Resource:             "users/support@example.com/mailFolders('Inbox')/messages",
		Recovery:             RecoveryRecreate,
	}}}
	provider := &subscriptionProviderStub{}
	worker := NewSubscriptionWorker(
		queue, subscriptionProviderFactoryStub{provider: provider},
		graphClientStateStub{value: "client-state"},
		"https://rarity.example/api/v1/graph/notifications",
		func() time.Time { return now },
	)

	result, err := worker.RunOnce(context.Background(), 25)

	if err != nil || result.Created != 1 ||
		len(provider.created) != 1 ||
		len(provider.renewed) != 0 ||
		queue.completed[0].ExternalID != "new-subscription-id" ||
		queue.completed[0].Recovery != RecoveryRunDelta {
		t.Fatalf(
			"RunOnce() result=%+v error=%v created=%+v completed=%+v",
			result, err, provider.created, queue.completed,
		)
	}
}

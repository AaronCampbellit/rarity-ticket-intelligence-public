package graphintake

import (
	"context"
	"errors"
	"testing"
	"time"
)

type graphSubscriptionRepository struct {
	subscription SubscriptionTarget
	accepted     NotificationMutation
}

func (r *graphSubscriptionRepository) LoadSubscription(
	context.Context,
	string,
) (SubscriptionTarget, error) {
	return r.subscription, nil
}

func (r *graphSubscriptionRepository) AcceptNotifications(
	_ context.Context,
	mutation NotificationMutation,
) error {
	r.accepted = mutation
	return nil
}

type graphClientStateResolver struct {
	value string
}

func (r graphClientStateResolver) ResolveClientState(
	context.Context,
	string,
) (string, error) {
	return r.value, nil
}

func TestNotificationServiceValidatesEveryHintAndPersistsOneAtomicMutation(t *testing.T) {
	now := time.Date(2026, time.July, 29, 21, 0, 0, 0, time.UTC)
	repository := &graphSubscriptionRepository{subscription: SubscriptionTarget{
		ConnectionID: "connection-id", SubscriptionID: "subscription-id",
		Mailbox:              "support@example.com",
		ClientStateSecretRef: "env://RARITY_GRAPH_CLIENT_STATE_SUPPORT",
	}}
	service := NewNotificationService(
		repository, graphClientStateResolver{value: "expected-state"},
		func() time.Time { return now },
		func() string { return "11111111-1111-4111-8111-111111111111" },
	)
	body := []byte(`{"value":[
	  {
	    "subscriptionId":"subscription-id",
	    "clientState":"expected-state",
	    "resource":"users/support@example.com/mailFolders/inbox/messages/message-id",
	    "resourceData":{"id":"message-id"}
	  },
	  {
	    "subscriptionId":"subscription-id",
	    "clientState":"expected-state",
	    "resource":"users/support@example.com/mailFolders/inbox/messages/ignored",
	    "lifecycleEvent":"missed"
	  }
	]}`)

	result, err := service.Accept(context.Background(), body)
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if result.Accepted != 2 ||
		len(repository.accepted.Hints) != 1 ||
		repository.accepted.Hints[0].MessageID != "message-id" ||
		repository.accepted.Hints[0].ReceivedAt != now ||
		len(repository.accepted.Lifecycle) != 1 ||
		repository.accepted.Lifecycle[0].Recovery != RecoveryRunDelta {
		t.Fatalf("unexpected result=%+v mutation=%+v", result, repository.accepted)
	}
}

func TestNotificationServiceRejectsEntireBatchWhenAnyClientStateIsInvalid(t *testing.T) {
	repository := &graphSubscriptionRepository{subscription: SubscriptionTarget{
		ConnectionID: "connection-id", SubscriptionID: "subscription-id",
		Mailbox: "support@example.com", ClientStateSecretRef: "secret-ref",
	}}
	service := NewNotificationService(
		repository, graphClientStateResolver{value: "expected-state"},
		time.Now, func() string { return "id" },
	)
	_, err := service.Accept(context.Background(), []byte(`{"value":[{
	  "subscriptionId":"subscription-id",
	  "clientState":"wrong-state",
	  "resource":"users/support@example.com/mailFolders/inbox/messages/message-id",
	  "resourceData":{"id":"message-id"}
	}]}`))
	if !errors.Is(err, ErrInvalidNotification) ||
		len(repository.accepted.Hints) != 0 {
		t.Fatalf("Accept() error=%v mutation=%+v", err, repository.accepted)
	}
}

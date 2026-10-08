package webhooks

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recordingSender struct {
	request DeliveryRequest
	status  int
	err     error
}

func (s *recordingSender) Send(_ context.Context, request DeliveryRequest) (int, error) {
	s.request = request
	return s.status, s.err
}

type recordingHistory struct {
	attempts []DeliveryAttempt
}

func (h *recordingHistory) Record(_ context.Context, attempt DeliveryAttempt) error {
	h.attempts = append(h.attempts, attempt)
	return nil
}

func TestPublisherSignsAllowedHTTPSDeliveryAndRecordsSuccess(t *testing.T) {
	now := time.Date(2026, time.July, 29, 17, 0, 0, 123456789, time.UTC)
	sender := &recordingSender{status: 202}
	history := &recordingHistory{}
	publisher := NewPublisher(sender, history, func() time.Time { return now })
	event := OutboundEvent{
		ID: "event-id", Type: "work_record.created", Body: []byte(`{"id":"work-id"}`),
	}
	connection := WebhookConnection{
		ID: "connection-id", MSPID: "msp-id", ClientID: "client-id",
		Endpoint: "https://hooks.example.com/rarity", Secret: []byte("secret"),
	}

	if err := publisher.Publish(context.Background(), connection, event, 1); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if sender.request.Headers["X-Rarity-Event-ID"] != event.ID ||
		sender.request.Headers["X-Rarity-Event-Type"] != event.Type ||
		sender.request.Headers["X-Rarity-Signature"] == "" {
		t.Fatalf("delivery headers incomplete: %+v", sender.request.Headers)
	}
	headerTime, err := time.Parse(
		time.RFC3339Nano, sender.request.Headers["X-Rarity-Timestamp"],
	)
	if err != nil || sender.request.Headers["X-Rarity-Signature"] !=
		Sign(connection.Secret, headerTime, event.ID, event.Body) {
		t.Fatalf("signature does not match transmitted timestamp: headers=%+v error=%v", sender.request.Headers, err)
	}
	if len(history.attempts) != 1 || history.attempts[0].State != DeliverySucceeded ||
		history.attempts[0].HTTPStatus != 202 {
		t.Fatalf("success history invalid: %+v", history.attempts)
	}
}

func TestPublisherRecordsStableFailureAndReturnsRetryableError(t *testing.T) {
	now := time.Date(2026, time.July, 29, 17, 0, 0, 0, time.UTC)
	sender := &recordingSender{status: 503}
	history := &recordingHistory{}
	publisher := NewPublisher(sender, history, func() time.Time { return now })

	err := publisher.Publish(context.Background(), WebhookConnection{
		ID: "connection-id", MSPID: "msp-id", Endpoint: "https://hooks.example.com/rarity",
		Secret: []byte("secret"),
	}, OutboundEvent{ID: "event-id", Type: "project.created", Body: []byte(`{}`)}, 3)
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("Publish() error = %v", err)
	}
	if len(history.attempts) != 1 || history.attempts[0].State != DeliveryFailed ||
		history.attempts[0].ErrorCode != "remote_5xx" {
		t.Fatalf("failure history invalid: %+v", history.attempts)
	}
}

func TestPublisherRecordsUnsafeDestinationWithoutSending(t *testing.T) {
	now := time.Date(2026, time.July, 29, 17, 0, 0, 0, time.UTC)
	sender := &recordingSender{}
	history := &recordingHistory{}
	publisher := NewPublisher(sender, history, func() time.Time { return now })
	err := publisher.Publish(context.Background(), WebhookConnection{
		ID: "connection-id", MSPID: "msp-id", Endpoint: "https://127.0.0.1/hook",
		Secret: []byte("secret"),
	}, OutboundEvent{
		ID: "event-id", Type: "project.created", Body: []byte(`{}`),
	}, 1)
	if !errors.Is(err, ErrUnsafeDestination) || len(history.attempts) != 1 ||
		history.attempts[0].ErrorCode != "unsafe_destination" ||
		sender.request.URL != "" {
		t.Fatalf("unsafe error=%v history=%+v request=%+v", err, history.attempts, sender.request)
	}
}

func TestPublisherTreatsDNSRebindingRejectionAsPermanent(t *testing.T) {
	now := time.Date(2026, time.July, 29, 17, 0, 0, 0, time.UTC)
	sender := &recordingSender{err: ErrUnsafeDestination}
	history := &recordingHistory{}
	publisher := NewPublisher(sender, history, func() time.Time { return now })
	err := publisher.Publish(context.Background(), WebhookConnection{
		ID: "connection-id", MSPID: "msp-id",
		Endpoint: "https://hooks.example.com/rarity", Secret: []byte("secret"),
	}, OutboundEvent{
		ID: "event-id", Type: "project.created", Body: []byte(`{}`),
	}, 1)
	if !errors.Is(err, ErrUnsafeDestination) || len(history.attempts) != 1 ||
		history.attempts[0].ErrorCode != "unsafe_destination" {
		t.Fatalf("DNS rejection error=%v history=%+v", err, history.attempts)
	}
}

func TestValidateDestinationRejectsUnsafeTargets(t *testing.T) {
	for _, endpoint := range []string{
		"http://hooks.example.com/path",
		"https://localhost/path",
		"https://127.0.0.1/path",
		"https://10.0.0.4/path",
		"https://169.254.169.254/latest/meta-data",
		"https://user:password@hooks.example.com/path",
	} {
		if err := ValidateDestination(endpoint); !errors.Is(err, ErrUnsafeDestination) {
			t.Fatalf("ValidateDestination(%q) error = %v", endpoint, err)
		}
	}
	if err := ValidateDestination("https://hooks.example.com/rarity"); err != nil {
		t.Fatalf("public HTTPS destination rejected: %v", err)
	}
}

func TestResolvingPublisherLoadsSecretAtDeliveryWithoutPersistedPlaintext(t *testing.T) {
	now := time.Date(2026, time.July, 29, 17, 0, 0, 0, time.UTC)
	sender := &recordingSender{status: 202}
	history := &recordingHistory{}
	secrets := &inboundSecrets{value: []byte("resolved-secret")}
	publisher := NewResolvingPublisher(
		secrets, NewPublisher(sender, history, func() time.Time { return now }),
	)
	job := OutboundJob{
		Connection: WebhookConnection{
			ID: "connection", MSPID: "msp",
			Endpoint:  "https://hooks.example.com/rarity",
			SecretRef: "env://RARITY_WEBHOOK_SECRET_CUSTOMER",
		},
		Event:   OutboundEvent{ID: "event", Type: "work_record.created", Body: []byte(`{}`)},
		Attempt: 1,
	}
	if err := publisher.PublishOutbound(context.Background(), job); err != nil {
		t.Fatalf("PublishOutbound() error=%v", err)
	}
	if secrets.ref != job.Connection.SecretRef ||
		sender.request.Headers["X-Rarity-Signature"] == "" {
		t.Fatalf("secret was not resolved safely: ref=%q request=%+v", secrets.ref, sender.request)
	}
}

func TestResolvingPublisherRecordsUnavailableSecretAsSafeFailure(t *testing.T) {
	now := time.Date(2026, time.July, 29, 17, 0, 0, 0, time.UTC)
	history := &recordingHistory{}
	publisher := NewResolvingPublisher(
		&inboundSecrets{err: ErrInboundSecretUnavailable},
		NewPublisher(&recordingSender{}, history, func() time.Time { return now }),
	)
	err := publisher.PublishOutbound(context.Background(), OutboundJob{
		Connection: WebhookConnection{
			ID: "connection", MSPID: "msp",
			Endpoint:  "https://hooks.example.com/rarity",
			SecretRef: "env://RARITY_WEBHOOK_SECRET_MISSING",
		},
		Event:   OutboundEvent{ID: "event", Type: "work_record.created", Body: []byte(`{}`)},
		Attempt: 2,
	})
	if !errors.Is(err, ErrDeliveryFailed) || len(history.attempts) != 1 ||
		history.attempts[0].ErrorCode != "connection_unavailable" ||
		history.attempts[0].State != DeliveryFailed {
		t.Fatalf("PublishOutbound() error=%v history=%+v", err, history.attempts)
	}
}

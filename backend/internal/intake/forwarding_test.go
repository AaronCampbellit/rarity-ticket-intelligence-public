package intake

import (
	"context"
	"testing"
	"time"
)

type fixedLimiter struct {
	allowed bool
	key     string
}

func (l *fixedLimiter) Allow(_ context.Context, key string, _ time.Time) (bool, error) {
	l.key = key
	return l.allowed, nil
}

type recordingStore struct {
	event InboundEvent
}

func (s *recordingStore) Record(_ context.Context, event InboundEvent) error {
	s.event = event
	return nil
}

func TestForwardingGatewayAcceptsAuthenticatedMessageIntoNormalizedIntake(t *testing.T) {
	now := time.Date(2026, time.July, 29, 20, 0, 0, 0, time.UTC)
	limiter := &fixedLimiter{allowed: true}
	store := &recordingStore{}
	gateway := NewForwardingGateway(ForwardingPolicy{
		ConnectionID: "connection-id", MSPID: "msp-id",
		IntakeAddress:        "intake+abc@rarity.example",
		AllowedSenderDomains: []string{"customer.example"}, MaxMessageBytes: 25 << 20,
	}, limiter, store, func() time.Time { return now }, func() string { return "event-id" })

	result, err := gateway.Accept(context.Background(), ForwardedMessage{
		EnvelopeFrom: "alerts@customer.example", From: "alerts@customer.example",
		To: "intake+abc@rarity.example", MessageID: "<message@example.com>",
		RawMIMERef: "objects/mime/message-id", SizeBytes: 2048,
		Authentication: SenderAuthentication{SPF: true, DKIM: true, DMARC: true},
	})
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if result.State != StateReceived || store.event.Source != SourceForwardedEmail ||
		store.event.ExternalID != "<message@example.com>" ||
		store.event.AuthenticationResult != AuthenticationPassed ||
		store.event.RawPayloadRef == "" {
		t.Fatalf("forwarded message was not normalized: result=%+v event=%+v", result, store.event)
	}
}

func TestForwardingGatewayQuarantinesUntrustedOversizedAndRateLimitedMessages(t *testing.T) {
	now := time.Date(2026, time.July, 29, 20, 0, 0, 0, time.UTC)
	base := ForwardedMessage{
		EnvelopeFrom: "alerts@customer.example", From: "alerts@customer.example",
		To: "intake+abc@rarity.example", MessageID: "<message@example.com>",
		RawMIMERef: "objects/mime/message-id", SizeBytes: 1024,
		Authentication: SenderAuthentication{SPF: true, DKIM: true, DMARC: true},
	}
	cases := []struct {
		name    string
		message ForwardedMessage
		allowed bool
		reason  QuarantineReason
	}{
		{name: "sender", message: withFrom(base, "attacker@evil.example"), allowed: true, reason: QuarantineSender},
		{name: "authentication", message: withDMARC(base, false), allowed: true, reason: QuarantineAuthentication},
		{name: "size", message: withSize(base, 2049), allowed: true, reason: QuarantineSize},
		{name: "rate", message: base, allowed: false, reason: QuarantineRateLimit},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			store := &recordingStore{}
			gateway := NewForwardingGateway(ForwardingPolicy{
				ConnectionID: "connection-id", MSPID: "msp-id",
				IntakeAddress:        base.To,
				AllowedSenderDomains: []string{"customer.example"}, MaxMessageBytes: 2048,
			}, &fixedLimiter{allowed: test.allowed}, store, func() time.Time { return now }, func() string { return "event-id" })
			result, err := gateway.Accept(context.Background(), test.message)
			if err != nil {
				t.Fatalf("Accept() error = %v", err)
			}
			if result.State != StateQuarantined || result.QuarantineReason != test.reason ||
				store.event.ProcessingState != StateQuarantined {
				t.Fatalf("message not quarantined correctly: result=%+v event=%+v", result, store.event)
			}
		})
	}
}

func withFrom(message ForwardedMessage, value string) ForwardedMessage {
	message.EnvelopeFrom = value
	message.From = value
	return message
}
func withDMARC(message ForwardedMessage, value bool) ForwardedMessage {
	message.Authentication.DMARC = value
	return message
}
func withSize(message ForwardedMessage, value int64) ForwardedMessage {
	message.SizeBytes = value
	return message
}

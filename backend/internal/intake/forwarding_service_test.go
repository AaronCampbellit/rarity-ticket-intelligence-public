package intake

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type forwardingRuntimeRepository struct {
	connection  ForwardingConnection
	loadErr     error
	existing    InboundEvent
	findErr     error
	allowed     bool
	allowCalls  int
	accepted    ForwardingMutation
	createErr   error
	loadCalls   int
	createCalls int
}

func (r *forwardingRuntimeRepository) LoadForwardingConnection(
	context.Context,
	scope.Target,
	string,
) (ForwardingConnection, error) {
	r.loadCalls++
	return r.connection, r.loadErr
}

func (r *forwardingRuntimeRepository) FindForwardingEvent(
	context.Context,
	scope.Target,
	string,
	string,
) (InboundEvent, error) {
	return r.existing, r.findErr
}

func (r *forwardingRuntimeRepository) AllowForwarding(
	context.Context,
	string,
	string,
	int,
	time.Time,
) (bool, error) {
	r.allowCalls++
	return r.allowed, nil
}

func (r *forwardingRuntimeRepository) CreateForwardingAtomic(
	_ context.Context,
	accepted ForwardingMutation,
) error {
	r.createCalls++
	r.accepted = accepted
	return r.createErr
}

type forwardingObjectStore struct {
	key     string
	body    []byte
	deleted string
}

func (s *forwardingObjectStore) Put(
	_ context.Context,
	key string,
	body []byte,
) error {
	s.key = key
	s.body = append([]byte(nil), body...)
	return nil
}

func (s *forwardingObjectStore) Delete(
	_ context.Context,
	key string,
) error {
	s.deleted = key
	return nil
}

func TestForwardingServiceStoresExactMIMEAndAtomicAcceptedEvent(t *testing.T) {
	now := time.Date(2026, time.July, 30, 0, 0, 0, 0, time.UTC)
	repository := &forwardingRuntimeRepository{
		connection: ForwardingConnection{
			ID: "connection", MSPID: "msp",
			IntakeAddress:        "intake+abc@rarity.example",
			AllowedSenderDomains: []string{"customer.example"},
			MaxMessageBytes:      25 << 20, RateLimitPerMinute: 60, Enabled: true,
		},
		findErr: scope.ErrNotFound, allowed: true,
	}
	raw := &forwardingObjectStore{}
	ids := []string{"inbound", "audit", "outbox", "correlation"}
	service := NewForwardingService(
		repository, raw, func() time.Time { return now },
		func() string {
			value := ids[0]
			ids = ids[1:]
			return value
		},
	)
	mime := []byte("From: alerts@customer.example\r\nMessage-ID: <message@example.com>\r\n\r\nAlert")
	result, err := service.Accept(context.Background(), ForwardingCommand{
		Principal: authorization.Principal{
			ID: "relay-key", Scope: scope.Principal{MSPID: "msp"},
			Capabilities: authorization.NewCapabilitySet("intake.forwarding.write"),
		},
		ConnectionID: "connection",
		Message: ForwardedMessage{
			EnvelopeFrom: "alerts@customer.example",
			From:         "alerts@customer.example", To: "intake+abc@rarity.example",
			MessageID: "<message@example.com>", SizeBytes: int64(len(mime)),
			Authentication: SenderAuthentication{SPF: true, DMARC: true},
		},
		RawMIME: mime, ActorID: "relay-key",
		ActorType: "service_key", SourceName: "api",
	})
	event := repository.accepted.Inbound
	if err != nil || result.State != StateReceived ||
		event.ID != "inbound" || event.ForwardingConnectionID != "connection" ||
		event.AuthenticationResult != AuthenticationPassed ||
		event.RawPayloadRef != raw.key || !bytes.Equal(raw.body, mime) ||
		repository.accepted.Audit.Action != "intake.forwarding.received" ||
		repository.accepted.Event.EventType != "intake.forwarding.received" {
		t.Fatalf("forwarding result=%+v error=%v mutation=%+v raw=%+v", result, err, repository.accepted, raw)
	}
}

func TestForwardingServicePersistsUntrustedMessageInQuarantine(t *testing.T) {
	repository := &forwardingRuntimeRepository{
		connection: ForwardingConnection{
			ID: "connection", MSPID: "msp",
			IntakeAddress:        "intake+abc@rarity.example",
			AllowedSenderDomains: []string{"customer.example"},
			MaxMessageBytes:      1024, RateLimitPerMinute: 60, Enabled: true,
		},
		findErr: scope.ErrNotFound, allowed: true,
	}
	service := NewForwardingService(
		repository, &forwardingObjectStore{}, time.Now,
		func() string { return "id" },
	)
	result, err := service.Accept(context.Background(), ForwardingCommand{
		Principal: authorization.Principal{
			ID: "relay-key", Scope: scope.Principal{MSPID: "msp"},
			Capabilities: authorization.NewCapabilitySet("intake.forwarding.write"),
		},
		ConnectionID: "connection",
		Message: ForwardedMessage{
			EnvelopeFrom: "attacker@evil.example", From: "attacker@evil.example",
			To: "intake+abc@rarity.example", MessageID: "<attack@example.com>",
			SizeBytes: 8,
		},
		RawMIME: []byte("untrusted"), ActorID: "relay-key",
		ActorType: "service_key", SourceName: "api",
	})
	if err != nil || result.State != StateQuarantined ||
		result.QuarantineReason != QuarantineSender ||
		repository.accepted.Inbound.ProcessingState != StateQuarantined ||
		repository.accepted.Audit.Action != "intake.forwarding.quarantined" ||
		repository.allowCalls != 1 {
		t.Fatalf("quarantine result=%+v error=%v mutation=%+v", result, err, repository.accepted)
	}
}

func TestForwardingServiceDeletesMIMEWhenAtomicPersistenceFails(t *testing.T) {
	repository := &forwardingRuntimeRepository{
		connection: ForwardingConnection{
			ID: "connection", MSPID: "msp",
			IntakeAddress:        "intake@rarity.example",
			AllowedSenderDomains: []string{"customer.example"},
			MaxMessageBytes:      1024, RateLimitPerMinute: 60, Enabled: true,
		},
		findErr: scope.ErrNotFound, allowed: true,
		createErr: errors.New("database unavailable"),
	}
	raw := &forwardingObjectStore{}
	service := NewForwardingService(repository, raw, time.Now, func() string { return "id" })
	_, err := service.Accept(context.Background(), ForwardingCommand{
		Principal: authorization.Principal{
			ID: "relay-key", Scope: scope.Principal{MSPID: "msp"},
			Capabilities: authorization.NewCapabilitySet("intake.forwarding.write"),
		},
		ConnectionID: "connection",
		Message: ForwardedMessage{
			EnvelopeFrom: "alerts@customer.example",
			From:         "alerts@customer.example", To: "intake@rarity.example",
			MessageID:      "<message@example.com>",
			Authentication: SenderAuthentication{SPF: true, DMARC: true},
		},
		RawMIME: []byte("mime"), ActorID: "relay-key",
		ActorType: "service_key", SourceName: "api",
	})
	if err == nil || raw.key == "" || raw.deleted != raw.key {
		t.Fatalf("persistence error=%v raw=%+v", err, raw)
	}
}

func TestForwardingServiceAuthorizesBeforeConnectionOrStorage(t *testing.T) {
	repository := &forwardingRuntimeRepository{}
	raw := &forwardingObjectStore{}
	service := NewForwardingService(repository, raw, time.Now, func() string { return "id" })
	_, err := service.Accept(context.Background(), ForwardingCommand{
		Principal: authorization.Principal{
			ID: "relay-key", Scope: scope.Principal{MSPID: "msp"},
		},
		ConnectionID: "connection", RawMIME: []byte("mime"),
		ActorID: "relay-key", ActorType: "service_key", SourceName: "api",
	})
	if !errors.Is(err, authorization.ErrForbidden) ||
		repository.loadCalls != 0 || raw.key != "" {
		t.Fatalf("unauthorized error=%v repository=%+v raw=%+v", err, repository, raw)
	}
}

func TestForwardingServiceReturnsExistingWithoutStoringDuplicateMIME(t *testing.T) {
	existing := InboundEvent{
		ID: "existing", MSPID: "msp", ForwardingConnectionID: "connection",
		Source: SourceForwardedEmail, ExternalID: "<message@example.com>",
		ProcessingState: StateReceived,
	}
	repository := &forwardingRuntimeRepository{
		connection: ForwardingConnection{
			ID: "connection", MSPID: "msp", IntakeAddress: "intake@rarity.example",
			AllowedSenderDomains: []string{"customer.example"},
			MaxMessageBytes:      1024, RateLimitPerMinute: 60, Enabled: true,
		},
		existing: existing,
	}
	raw := &forwardingObjectStore{}
	service := NewForwardingService(repository, raw, time.Now, func() string { return "id" })
	result, err := service.Accept(context.Background(), ForwardingCommand{
		Principal: authorization.Principal{
			ID: "relay-key", Scope: scope.Principal{MSPID: "msp"},
			Capabilities: authorization.NewCapabilitySet("intake.forwarding.write"),
		},
		ConnectionID: "connection",
		Message:      ForwardedMessage{MessageID: "<message@example.com>"},
		RawMIME:      []byte("duplicate"), ActorID: "relay-key",
		ActorType: "service_key", SourceName: "api",
	})
	if err != nil || result.EventID != "existing" ||
		raw.key != "" || repository.allowCalls != 0 ||
		repository.createCalls != 0 {
		t.Fatalf("duplicate result=%+v error=%v repository=%+v raw=%+v", result, err, repository, raw)
	}
}

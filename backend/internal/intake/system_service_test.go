package intake

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type rawPayloadStore struct {
	key     string
	body    []byte
	deleted string
}

func (s *rawPayloadStore) Put(
	_ context.Context,
	key string,
	body []byte,
) error {
	s.key = key
	s.body = append([]byte(nil), body...)
	return nil
}

func (s *rawPayloadStore) Delete(_ context.Context, key string) error {
	s.deleted = key
	return nil
}

type systemRepository struct {
	existing    InboundEvent
	findErr     error
	accepted    SystemMutation
	createErr   error
	createCalls int
}

type racingSystemRepository struct {
	findCalls int
	existing  InboundEvent
}

func (r *racingSystemRepository) FindSystemEvent(
	context.Context,
	scope.Target,
	Source,
	string,
) (InboundEvent, error) {
	r.findCalls++
	if r.findCalls == 1 {
		return InboundEvent{}, scope.ErrNotFound
	}
	return r.existing, nil
}

func (*racingSystemRepository) CreateSystemEventAtomic(
	context.Context,
	SystemMutation,
) error {
	return ErrDuplicateSystemEvent
}

func (r *systemRepository) FindSystemEvent(
	context.Context,
	scope.Target,
	Source,
	string,
) (InboundEvent, error) {
	return r.existing, r.findErr
}

func (r *systemRepository) CreateSystemEventAtomic(
	_ context.Context,
	accepted SystemMutation,
) error {
	r.createCalls++
	r.accepted = accepted
	return r.createErr
}

func TestSystemServiceStoresRawPayloadAndAtomicScopedEvent(t *testing.T) {
	now := time.Date(2026, time.July, 29, 22, 0, 0, 0, time.UTC)
	store := &rawPayloadStore{}
	repository := &systemRepository{findErr: scope.ErrNotFound}
	ids := []string{"event-id", "audit-id", "outbox-id", "correlation-id"}
	service := NewSystemService(repository, store, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	payload := []byte(`{"title":"VPN unavailable","priority":"critical"}`)
	result, err := service.Accept(context.Background(), SystemCommand{
		Principal: authorization.Principal{
			ID: "service-key", Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("intake.write"),
		},
		ExternalID: "remote-100", Payload: payload,
		ActorID: "service-key", ActorType: "service_key", SourceName: "api",
	})
	if err != nil {
		t.Fatalf("Accept() error=%v", err)
	}
	if result.ID != "event-id" || result.ClientID != "client" ||
		result.Source != SourceDirectAPI || result.RawPayloadRef == "" ||
		repository.createCalls != 1 ||
		repository.accepted.Audit.Action != "intake.direct.received" ||
		repository.accepted.Event.EventType != "intake.direct.received" ||
		store.key != result.RawPayloadRef || !bytes.Equal(store.body, payload) {
		t.Fatalf("direct intake incomplete: result=%+v mutation=%+v store=%+v", result, repository.accepted, store)
	}
}

func TestSystemServiceReturnsExistingEventWithoutStoringDuplicatePayload(t *testing.T) {
	existing := InboundEvent{
		ID: "existing", MSPID: "msp", ClientID: "client",
		Source: SourceDirectAPI, ExternalID: "remote-100",
		RawPayloadRef: "intake/existing", ProcessingState: StateReceived,
	}
	store := &rawPayloadStore{}
	repository := &systemRepository{existing: existing}
	service := NewSystemService(repository, store, time.Now, func() string { return "unused" })
	result, err := service.Accept(context.Background(), SystemCommand{
		Principal: authorization.Principal{
			ID: "service-key", Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("intake.write"),
		},
		ExternalID: "remote-100", Payload: []byte(`{"duplicate":true}`),
		ActorID: "service-key", ActorType: "service_key", SourceName: "api",
	})
	if err != nil || result.ID != "existing" || store.key != "" || repository.createCalls != 0 {
		t.Fatalf("idempotent intake failed: result=%+v error=%v store=%+v repository=%+v", result, err, store, repository)
	}
}

func TestSystemServiceDeletesRawPayloadWhenAtomicPersistenceFails(t *testing.T) {
	store := &rawPayloadStore{}
	repository := &systemRepository{
		findErr: scope.ErrNotFound, createErr: errors.New("database unavailable"),
	}
	service := NewSystemService(repository, store, time.Now, func() string { return "id" })
	_, err := service.Accept(context.Background(), SystemCommand{
		Principal: authorization.Principal{
			ID: "service-key", Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("intake.write"),
		},
		ExternalID: "remote", Payload: []byte(`{"event":true}`),
		ActorID: "service-key", ActorType: "service_key", SourceName: "api",
	})
	if err == nil || store.key == "" || store.deleted != store.key {
		t.Fatalf("failed intake leaked payload: error=%v store=%+v", err, store)
	}
}

func TestSystemServiceAuthorizesBeforeLookupOrStorage(t *testing.T) {
	store := &rawPayloadStore{}
	repository := &systemRepository{}
	service := NewSystemService(repository, store, time.Now, func() string { return "id" })
	_, err := service.Accept(context.Background(), SystemCommand{
		Principal: authorization.Principal{
			ID: "service-key", Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
		},
		ExternalID: "remote", Payload: []byte(`{"event":true}`),
		ActorID: "service-key", ActorType: "service_key", SourceName: "api",
	})
	if !errors.Is(err, authorization.ErrForbidden) || store.key != "" {
		t.Fatalf("unauthorized intake error=%v store=%+v", err, store)
	}
}

func TestSystemServiceRejectsOversizedExternalIDBeforeLookupOrStorage(t *testing.T) {
	store := &rawPayloadStore{}
	repository := &systemRepository{}
	service := NewSystemService(repository, store, time.Now, func() string { return "id" })
	_, err := service.Accept(context.Background(), SystemCommand{
		Principal: authorization.Principal{
			ID: "service-key", Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("intake.write"),
		},
		ExternalID: strings.Repeat("x", 513), Payload: []byte(`{"event":true}`),
		ActorID: "service-key", ActorType: "service_key", SourceName: "api",
	})
	if !errors.Is(err, ErrInvalidSystemIntake) || store.key != "" ||
		repository.createCalls != 0 {
		t.Fatalf("oversized external ID error=%v store=%+v repository=%+v", err, store, repository)
	}
}

func TestSystemServiceRaceReturnsWinningIdempotentEventAndDeletesLosingPayload(t *testing.T) {
	existing := InboundEvent{
		ID: "winner", MSPID: "msp", ClientID: "client",
		Source: SourceDirectAPI, ExternalID: "remote",
		RawPayloadRef: "intake/winner", ProcessingState: StateReceived,
	}
	repository := &racingSystemRepository{existing: existing}
	store := &rawPayloadStore{}
	service := NewSystemService(repository, store, time.Now, func() string { return "loser" })
	result, err := service.Accept(context.Background(), SystemCommand{
		Principal: authorization.Principal{
			ID: "service-key", Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("intake.write"),
		},
		ExternalID: "remote", Payload: []byte(`{"event":true}`),
		ActorID: "service-key", ActorType: "service_key", SourceName: "api",
	})
	if err != nil || result.ID != "winner" || store.deleted != store.key ||
		repository.findCalls != 2 {
		t.Fatalf("race recovery failed: result=%+v error=%v store=%+v repository=%+v", result, err, store, repository)
	}
}

package webhooks

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/intake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type inboundSecrets struct {
	ref   string
	value []byte
	err   error
}

func (s *inboundSecrets) Resolve(_ context.Context, ref string) ([]byte, error) {
	s.ref = ref
	return append([]byte(nil), s.value...), s.err
}

type inboundRepository struct {
	connection InboundConnection
	loadErr    error
	accepted   InboundMutation
	createErr  error
}

func (r *inboundRepository) LoadInboundConnection(
	context.Context,
	string,
) (InboundConnection, error) {
	return r.connection, r.loadErr
}

func (r *inboundRepository) CreateInboundAtomic(
	_ context.Context,
	accepted InboundMutation,
) error {
	r.accepted = accepted
	return r.createErr
}

type inboundRawStore struct {
	key     string
	body    []byte
	deleted string
}

func (s *inboundRawStore) Put(_ context.Context, key string, body []byte) error {
	s.key = key
	s.body = append([]byte(nil), body...)
	return nil
}

func (s *inboundRawStore) Delete(_ context.Context, key string) error {
	s.deleted = key
	return nil
}

func TestInboundServiceAuthenticatesExactBodyAndPersistsReplayWithEvent(t *testing.T) {
	now := time.Date(2026, time.July, 29, 23, 0, 0, 0, time.UTC)
	secret := []byte("webhook-secret")
	body := []byte(`{"event":"monitor.alert","severity":"critical"}`)
	repository := &inboundRepository{connection: InboundConnection{
		ID: "connection", MSPID: "msp", ClientID: "client",
		SecretRef: "env://RARITY_WEBHOOK_SECRET_MONITOR",
		Direction: DirectionInbound, Enabled: true,
	}}
	secrets := &inboundSecrets{value: secret}
	raw := &inboundRawStore{}
	ids := []string{"inbound", "audit", "outbox", "correlation"}
	service := NewInboundService(
		repository, secrets, raw, func() time.Time { return now },
		func() string {
			value := ids[0]
			ids = ids[1:]
			return value
		},
	)
	event, err := service.Accept(context.Background(), InboundCommand{
		ConnectionID: "connection", EventID: "remote-event",
		Timestamp: now.Format(time.RFC3339),
		Signature: Sign(secret, now, "remote-event", body),
		Body:      body, SourceName: "api",
	})
	if err != nil {
		t.Fatalf("Accept() error=%v", err)
	}
	if event.ID != "inbound" || event.Source != intake.SourceInboundWebhook ||
		event.MSPID != "msp" || event.ClientID != "client" ||
		event.ConnectionID != "connection" ||
		!bytes.Equal(raw.body, body) || raw.key != event.RawPayloadRef ||
		repository.accepted.ConnectionID != "connection" ||
		repository.accepted.ReplayExpiresAt != now.Add(5*time.Minute) ||
		repository.accepted.System.Audit.Action != "intake.webhook.received" ||
		repository.accepted.System.Event.EventType != "intake.webhook.received" {
		t.Fatalf("inbound webhook incomplete: event=%+v mutation=%+v raw=%+v", event, repository.accepted, raw)
	}
}

func TestInboundServiceRejectsDisabledOrUnscopedConnectionBeforeSecret(t *testing.T) {
	for _, connection := range []InboundConnection{
		{ID: "connection", MSPID: "msp", ClientID: "client", SecretRef: "secret", Direction: DirectionInbound},
		{ID: "connection", MSPID: "msp", SecretRef: "secret", Direction: DirectionInbound, Enabled: true},
		{ID: "connection", MSPID: "msp", ClientID: "client", SecretRef: "secret", Direction: DirectionOutbound, Enabled: true},
	} {
		repository := &inboundRepository{connection: connection}
		secrets := &inboundSecrets{value: []byte("secret")}
		service := NewInboundService(repository, secrets, &inboundRawStore{}, time.Now, func() string { return "id" })
		_, err := service.Accept(context.Background(), InboundCommand{
			ConnectionID: "connection", EventID: "event",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Signature: "v1=bad", Body: []byte(`{}`), SourceName: "api",
		})
		if !errors.Is(err, scope.ErrNotFound) || secrets.ref != "" {
			t.Fatalf("connection=%+v error=%v secret_ref=%q", connection, err, secrets.ref)
		}
	}
}

func TestInboundServiceDeletesRawPayloadWhenReplayOrPersistenceFails(t *testing.T) {
	now := time.Date(2026, time.July, 29, 23, 0, 0, 0, time.UTC)
	secret := []byte("webhook-secret")
	for _, persistenceErr := range []error{ErrReplay, errors.New("database unavailable")} {
		repository := &inboundRepository{
			connection: InboundConnection{
				ID: "connection", MSPID: "msp", ClientID: "client",
				SecretRef: "secret", Direction: DirectionInbound, Enabled: true,
			},
			createErr: persistenceErr,
		}
		raw := &inboundRawStore{}
		service := NewInboundService(
			repository, &inboundSecrets{value: secret}, raw,
			func() time.Time { return now }, func() string { return "id" },
		)
		_, err := service.Accept(context.Background(), InboundCommand{
			ConnectionID: "connection", EventID: "event",
			Timestamp: now.Format(time.RFC3339),
			Signature: Sign(secret, now, "event", []byte(`{}`)),
			Body:      []byte(`{}`), SourceName: "api",
		})
		if !errors.Is(err, persistenceErr) || raw.key == "" || raw.deleted != raw.key {
			t.Fatalf("persistence error=%v returned=%v raw=%+v", persistenceErr, err, raw)
		}
	}
}

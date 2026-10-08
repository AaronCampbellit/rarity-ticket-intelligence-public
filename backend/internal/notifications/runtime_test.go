package notifications

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
)

type teamsHTTPSenderStub struct {
	called bool
	status int
	err    error
}

type teamsTransportStub struct {
	endpoint string
	payload  []byte
	err      error
}

func (transport *teamsTransportStub) Send(_ context.Context, endpoint string, payload []byte) error {
	transport.endpoint = endpoint
	transport.payload = append([]byte(nil), payload...)
	return transport.err
}

func (sender *teamsHTTPSenderStub) Send(context.Context, webhooks.DeliveryRequest) (int, error) {
	sender.called = true
	return sender.status, sender.err
}

func TestEnvironmentSecretResolverAllowsOnlyDedicatedTeamsReferences(t *testing.T) {
	resolver := NewEnvironmentSecretResolver(func(name string) (string, bool) {
		if name == "RARITY_TEAMS_WEBHOOK_NOC" {
			return "https://teams.example.test/hook", true
		}
		return "", false
	})
	value, err := resolver.Resolve(
		context.Background(), TeamsConnection{WebhookSecretRef: "env://RARITY_TEAMS_WEBHOOK_NOC"},
	)
	if err != nil || value != "https://teams.example.test/hook" {
		t.Fatalf("Resolve() value=%q error=%v", value, err)
	}
	for _, ref := range []string{
		"env://DATABASE_URL",
		"env://RARITY_TEAMS_WEBHOOK_MISSING",
		"https://teams.example.test/plaintext",
	} {
		if _, err := resolver.Resolve(context.Background(), TeamsConnection{WebhookSecretRef: ref}); !errors.Is(err, ErrTeamsSecretUnavailable) {
			t.Fatalf("Resolve(%q) error=%v", ref, err)
		}
	}
}

func TestHTTPTeamsTransportRejectsOversizedPayloadBeforeSending(t *testing.T) {
	transport := NewHTTPTeamsTransport()
	err := transport.Send(
		context.Background(),
		"https://teams.example.test/hook",
		[]byte(strings.Repeat("x", maxTeamsPayloadBytes+1)),
	)
	if !errors.Is(err, ErrTeamsDeliveryFailed) {
		t.Fatalf("Send() error=%v", err)
	}
}

func TestHTTPTeamsTransportRejectsUnsafeDestinationBeforeSending(t *testing.T) {
	sender := &teamsHTTPSenderStub{}
	transport := newHTTPTeamsTransport(sender)
	err := transport.Send(context.Background(), "https://127.0.0.1/hook", []byte(`{"ok":true}`))
	if !errors.Is(err, ErrTeamsDeliveryFailed) || sender.called {
		t.Fatalf("Send() error=%v called=%t", err, sender.called)
	}
}

func TestHTTPTeamsTransportRefusesRedirects(t *testing.T) {
	sender := &teamsHTTPSenderStub{status: 302}
	transport := newHTTPTeamsTransport(sender)
	err := transport.Send(
		context.Background(),
		"https://teams.example.test/hook",
		[]byte(`{"ok":true}`),
	)
	if !errors.Is(err, ErrTeamsDeliveryFailed) || !sender.called {
		t.Fatalf("Send() error=%v called=%t", err, sender.called)
	}
}

func TestHTTPTeamsConnectionTesterSendsOnlySyntheticSafeCard(t *testing.T) {
	transport := &teamsTransportStub{}
	tester := NewHTTPTeamsConnectionTester(transport)
	err := tester.TestTeamsWebhook(
		context.Background(),
		ManagedTeamsConnection{ID: "connection-id"},
		[]byte(" https://teams.example.test/hook/secret "),
	)
	if err != nil || transport.endpoint != "https://teams.example.test/hook/secret" ||
		!strings.Contains(string(transport.payload), "Rarity Teams connection test") ||
		strings.Contains(string(transport.payload), "connection-id") {
		t.Fatalf("TestTeamsWebhook() error=%v endpoint=%q payload=%s", err, transport.endpoint, transport.payload)
	}
}

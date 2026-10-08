package notifications

import (
	"context"
	"errors"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
)

var ErrTeamsSecretUnavailable = errors.New("Teams secret unavailable")

const maxTeamsPayloadBytes = 28 * 1024

type EnvironmentSecretResolver struct {
	lookup func(string) (string, bool)
}

func NewEnvironmentSecretResolver(
	lookup func(string) (string, bool),
) *EnvironmentSecretResolver {
	return &EnvironmentSecretResolver{lookup: lookup}
}

func (r *EnvironmentSecretResolver) Resolve(
	_ context.Context,
	connection TeamsConnection,
) (string, error) {
	const prefix = "env://"
	reference := connection.WebhookSecretRef
	if r == nil || r.lookup == nil || !strings.HasPrefix(reference, prefix) {
		return "", ErrTeamsSecretUnavailable
	}
	name := strings.TrimPrefix(reference, prefix)
	if !strings.HasPrefix(name, "RARITY_TEAMS_WEBHOOK_") {
		return "", ErrTeamsSecretUnavailable
	}
	value, ok := r.lookup(name)
	if !ok || strings.TrimSpace(value) == "" {
		return "", ErrTeamsSecretUnavailable
	}
	return value, nil
}

type HTTPTeamsTransport struct {
	sender teamsHTTPSender
}

type teamsHTTPSender interface {
	Send(context.Context, webhooks.DeliveryRequest) (int, error)
}

func NewHTTPTeamsTransport() *HTTPTeamsTransport {
	return &HTTPTeamsTransport{sender: webhooks.NewHTTPSender(nil)}
}

func newHTTPTeamsTransport(sender teamsHTTPSender) *HTTPTeamsTransport {
	return &HTTPTeamsTransport{sender: sender}
}

type HTTPTeamsConnectionTester struct {
	transport TeamsTransport
}

func NewHTTPTeamsConnectionTester(transport TeamsTransport) *HTTPTeamsConnectionTester {
	return &HTTPTeamsConnectionTester{transport: transport}
}

func (tester *HTTPTeamsConnectionTester) TestTeamsWebhook(
	ctx context.Context,
	_ ManagedTeamsConnection,
	webhook []byte,
) error {
	if tester == nil || tester.transport == nil {
		return ErrTeamsConnectionTestFailed
	}
	payload, err := BuildTeamsCard(WorkSummary{
		DisplayID: "TEST", Priority: "normal", Status: "test",
		Subject: "Rarity Teams connection test", SLAState: "not_applicable",
		AuthenticatedURL: "/",
	})
	if err != nil || tester.transport.Send(ctx, strings.TrimSpace(string(webhook)), payload) != nil {
		return ErrTeamsConnectionTestFailed
	}
	return nil
}

func (t *HTTPTeamsTransport) Send(
	ctx context.Context,
	endpoint string,
	payload []byte,
) error {
	if t == nil || t.sender == nil || len(payload) == 0 ||
		len(payload) > maxTeamsPayloadBytes ||
		webhooks.ValidateDestination(endpoint) != nil {
		return ErrTeamsDeliveryFailed
	}
	status, err := t.sender.Send(ctx, webhooks.DeliveryRequest{
		URL:     endpoint,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    payload,
	})
	if err != nil || status < 200 || status >= 300 {
		return ErrTeamsDeliveryFailed
	}
	return nil
}

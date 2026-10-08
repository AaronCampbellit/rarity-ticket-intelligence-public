package automation

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
)

type externalConnectionRepositoryStub struct {
	connection ExternalConnection
	loadedRef  string
}

func (r *externalConnectionRepositoryStub) Load(
	_ context.Context,
	ref, _, _ string,
) (ExternalConnection, error) {
	r.loadedRef = ref
	return r.connection, nil
}

type externalSecretResolverStub struct {
	value []byte
}

func (r externalSecretResolverStub) Resolve(
	context.Context,
	string,
) ([]byte, error) {
	return append([]byte(nil), r.value...), nil
}

type externalSenderStub struct {
	delivery webhooks.DeliveryRequest
}

func (s *externalSenderStub) Send(
	_ context.Context,
	delivery webhooks.DeliveryRequest,
) (int, error) {
	s.delivery = delivery
	return 204, nil
}

func TestRuntimeConnectionsAuthorizesScopeAndSignsSafeExternalCall(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	repository := &externalConnectionRepositoryStub{connection: ExternalConnection{
		ID: "connection-id", MSPID: "msp-id", ClientID: "client-id",
		Endpoint:         "https://automation.example/hook",
		SigningSecretRef: "env://RARITY_AUTOMATION_HTTP_SECRET_PRIMARY",
	}}
	sender := &externalSenderStub{}
	connections := NewRuntimeConnections(
		repository,
		externalSecretResolverStub{value: []byte("signing-secret")},
		sender,
		func() time.Time { return now },
	)
	principal := authorization.Principal{
		ID:    "automation-id",
		Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
	}

	if err := connections.Authorize(
		context.Background(), "connection-id", "msp-id", "client-id",
		ActionCallHTTP,
	); err != nil {
		t.Fatalf("Authorize() error=%v", err)
	}
	result, err := connections.Call(
		context.Background(), principal, "connection-id",
		map[string]string{"request_template_id": "template-id"},
		map[string]string{
			"_automation_run_id": "run-id", "priority": "critical",
			"password": "must-not-leave",
		},
	)
	var body map[string]any
	if decodeErr := json.Unmarshal(sender.delivery.Body, &body); decodeErr != nil {
		t.Fatalf("external body decode error=%v", decodeErr)
	}
	input, _ := body["input"].(map[string]any)
	if err != nil || len(result.ChangedObjectIDs) != 0 ||
		repository.loadedRef != "connection-id" ||
		sender.delivery.URL != "https://automation.example/hook" ||
		sender.delivery.Headers["X-Rarity-Signature"] == "" ||
		sender.delivery.Headers["X-Rarity-Automation-Run-ID"] != "run-id" ||
		input["priority"] != "critical" || input["password"] != nil {
		t.Fatalf(
			"Call() result=%+v error=%v delivery=%+v body=%+v",
			result, err, sender.delivery, body,
		)
	}
}

func TestEnvironmentExternalSecretResolverRestrictsNamespace(t *testing.T) {
	resolver := NewEnvironmentExternalSecretResolver(
		func(name string) (string, bool) {
			if name == "RARITY_AUTOMATION_HTTP_SECRET_PRIMARY" {
				return "secret", true
			}
			return "", false
		},
	)
	if _, err := resolver.Resolve(
		context.Background(),
		"env://RARITY_AUTOMATION_HTTP_SECRET_PRIMARY",
	); err != nil {
		t.Fatalf("Resolve() error=%v", err)
	}
	if _, err := resolver.Resolve(
		context.Background(),
		"env://RARITY_GRAPH_CREDENTIAL_PRIMARY",
	); err == nil {
		t.Fatal("resolver accepted a secret outside its namespace")
	}
}

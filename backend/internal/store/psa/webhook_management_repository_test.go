package psa

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
)

func TestWebhookManagementRepositorySealsCredentialAndWritesFactsAtomically(t *testing.T) {
	tx, provider := &fakeSalesTx{}, &recordingSecretProvider{}
	at := time.Date(2026, time.July, 30, 18, 0, 0, 0, time.UTC)
	secret := []byte("12345678901234567890123456789012")
	mutation := webhooks.ConnectionMutation{
		Connection: webhooks.ManagedConnection{
			ID: "connection-id", ClientID: "client-id", Name: "Ticket events",
			Direction:            webhooks.DirectionOutbound,
			Endpoint:             "https://hooks.example.test/rarity",
			CredentialConfigured: true, Enabled: true,
			EventTypes:         []string{"work_record.created"},
			RetryWindowSeconds: 86400, Version: 1, UpdatedAt: at,
		},
		PlaintextSecret: secret,
		Audit:           validAudit(at, "webhook.connection.created", "webhook_connection", "connection-id"),
		Event:           validEvent(at, "webhook.connection.created", "webhook_connection", "connection-id"),
	}
	repository := NewWebhookManagementRepository(
		&fakeSalesDB{tx: tx}, provider, nil, func() string { return "id" },
	)
	if err := repository.CreateManagedConnection(context.Background(), mutation); err != nil {
		t.Fatalf("CreateManagedConnection() error = %v", err)
	}
	if !credentialZeroed(secret) || provider.purpose != "webhook.connection.connection-id" {
		t.Fatalf("secret retained or wrong purpose: secret=%q provider=%+v", secret, provider)
	}
	assertQueryOrder(t, tx.queries, "INSERT INTO webhook_connections", "INSERT INTO audit_ledger", "INSERT INTO event_outbox")
	for _, args := range tx.args {
		if strings.Contains(fmt.Sprint(args), "12345678901234567890123456789012") {
			t.Fatal("plaintext webhook secret reached SQL")
		}
	}
}

func TestWebhookResolverSupportsEncryptedAndLegacyReferences(t *testing.T) {
	provider := &recordingSecretProvider{open: []byte("encrypted-secret")}
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(**string)) = nil
		version := 1
		*(destinations[1].(**int)) = &version
		*(destinations[2].(*[]byte)) = []byte("nonce")
		*(destinations[3].(*[]byte)) = []byte("ciphertext")
	}}}
	legacy := &inboundSecretResolverStub{value: []byte("legacy-secret")}
	repository := NewWebhookManagementRepository(db, provider, legacy, nil)
	value, err := repository.Resolve(context.Background(), "db://webhook/connection-id/1")
	if err != nil || string(value) != "encrypted-secret" ||
		provider.purpose != "webhook.connection.connection-id" {
		t.Fatalf("encrypted resolve value=%q provider=%+v err=%v", value, provider, err)
	}
	value, err = repository.Resolve(context.Background(), "env://RARITY_WEBHOOK_SECRET_LEGACY")
	if err != nil || string(value) != "legacy-secret" {
		t.Fatalf("legacy resolve value=%q err=%v", value, err)
	}
	reference := "env://RARITY_WEBHOOK_SECRET_LEGACY"
	legacyDB := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(**string)) = &reference
		*(destinations[1].(**int)) = nil
		*(destinations[2].(*[]byte)) = nil
		*(destinations[3].(*[]byte)) = nil
	}}}
	repository = NewWebhookManagementRepository(legacyDB, provider, legacy, nil)
	value, err = repository.Resolve(context.Background(), "db://webhook/connection-id/1")
	if err != nil || string(value) != "legacy-secret" {
		t.Fatalf("legacy database resolve value=%q err=%v", value, err)
	}
}

type inboundSecretResolverStub struct {
	value []byte
}

func (r *inboundSecretResolverStub) Resolve(_ context.Context, _ string) ([]byte, error) {
	return append([]byte(nil), r.value...), nil
}

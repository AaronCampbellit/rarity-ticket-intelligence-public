package psa

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/datto"
)

func TestDattoManagementSealsCredentialAndWritesFactsAtomically(t *testing.T) {
	tx, provider := &fakeSalesTx{}, &recordingSecretProvider{}
	at := time.Date(2026, time.July, 30, 20, 0, 0, 0, time.UTC)
	credential := []byte(`{"api_url":"https://example.centrastage.net","api_key":"key","api_secret":"top-secret"}`)
	accepted := datto.ConnectionMutation{
		MSPID: "msp-id",
		Connection: datto.ManagedConnection{
			ID: "connection-id", Name: "Primary RMM",
			APIURL:               "https://example.centrastage.net",
			CredentialConfigured: true, SyncIntervalSeconds: 900,
			Enabled: true, HealthState: "pending", Version: 1, UpdatedAt: at,
		},
		ProtectedCredential: credential,
		Audit:               validAudit(at, "datto.connection.created", "datto_connection", "connection-id"),
		Event:               validEvent(at, "datto.connection.created", "datto_connection", "connection-id"),
	}
	repository := NewDattoRepository(
		&fakeSalesDB{tx: tx}, provider, func() string { return "id" },
	)
	if err := repository.CreateManagedConnection(context.Background(), accepted); err != nil {
		t.Fatalf("CreateManagedConnection() error = %v", err)
	}
	if !credentialZeroed(credential) ||
		provider.purpose != "datto.connection.credential.connection-id" {
		t.Fatalf("credential retained or wrong purpose: provider=%+v", provider)
	}
	assertQueryOrder(
		t, tx.queries, "INSERT INTO datto_connections",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	for _, args := range tx.args {
		if strings.Contains(fmt.Sprint(args), "top-secret") {
			t.Fatal("plaintext Datto secret reached SQL")
		}
	}
}

func TestDattoRuntimeResolverRequiresEnabledExactGeneration(t *testing.T) {
	version := 1
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(**string)) = nil
		*(destinations[1].(**int)) = &version
		*(destinations[2].(*[]byte)) = []byte("nonce")
		*(destinations[3].(*[]byte)) = []byte("ciphertext")
	}}}
	provider := &recordingSecretProvider{
		open: []byte(`{"api_url":"https://example.centrastage.net","api_key":"key","api_secret":"secret"}`),
	}
	repository := NewDattoRepository(db, provider, nil)
	value, err := repository.Resolve(
		context.Background(), "db://datto/connection-id/3/credential",
	)
	if err != nil || len(value) == 0 ||
		provider.purpose != "datto.connection.credential.connection-id" {
		t.Fatalf("Resolve() value=%q provider=%+v err=%v", value, provider, err)
	}
	for _, fragment := range []string{
		"id = $1", "configuration_generation = $2", "enabled",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("resolver query missing %q: %s", fragment, db.query)
		}
	}
}

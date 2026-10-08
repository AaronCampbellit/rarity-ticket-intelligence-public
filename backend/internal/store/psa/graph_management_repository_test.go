package psa

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/graphintake"
)

func TestGraphManagementSealsBothSecretsAndWritesFactsAtomically(t *testing.T) {
	tx, provider := &fakeSalesTx{}, &recordingSecretProvider{}
	at := time.Date(2026, time.July, 30, 20, 0, 0, 0, time.UTC)
	credential := []byte(`{"tenant_id":"tenant","client_id":"application","client_secret":"top-secret"}`)
	clientState := []byte("random-client-state")
	mutation := graphintake.MailboxMutation{
		MSPID: "msp-id",
		Mailbox: graphintake.ManagedMailbox{
			ID: "mailbox-id", MailboxAddress: "support@example.com",
			TenantID: "tenant", ClientID: "application",
			CredentialConfigured: true, ClientStateConfigured: true,
			Enabled: true, HealthState: "pending", Version: 1, UpdatedAt: at,
		},
		ProtectedCredential: credential, ClientState: clientState,
		Audit: validAudit(at, "graph.mailbox.created", "graph_mailbox", "mailbox-id"),
		Event: validEvent(at, "graph.mailbox.created", "graph_mailbox", "mailbox-id"),
	}
	repository := NewGraphRepository(&fakeSalesDB{tx: tx}, provider, func() string { return "id" })
	if err := repository.CreateManagedMailbox(context.Background(), mutation); err != nil {
		t.Fatalf("CreateManagedMailbox() error = %v", err)
	}
	if !credentialZeroed(credential) || !credentialZeroed(clientState) ||
		provider.purpose != "graph.mailbox.client-state.mailbox-id" {
		t.Fatalf("protected buffers retained or wrong purpose: provider=%+v", provider)
	}
	assertQueryOrder(t, tx.queries, "INSERT INTO graph_mailbox_connections", "INSERT INTO audit_ledger", "INSERT INTO event_outbox")
	for _, args := range tx.args {
		if strings.Contains(fmt.Sprint(args), "top-secret") ||
			strings.Contains(fmt.Sprint(args), "random-client-state") {
			t.Fatal("plaintext Graph secret reached SQL")
		}
	}
}

func TestGraphRuntimeResolverRequiresEnabledExactVersion(t *testing.T) {
	version := 1
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(**string)) = nil
		*(destinations[1].(**int)) = &version
		*(destinations[2].(*[]byte)) = []byte("nonce")
		*(destinations[3].(*[]byte)) = []byte("ciphertext")
	}}}
	provider := &recordingSecretProvider{open: []byte(`{"tenant_id":"t","client_id":"c","client_secret":"s"}`)}
	repository := NewGraphRepository(db, provider, nil)
	value, err := repository.Resolve(context.Background(), "db://graph/mailbox-id/3/credential")
	if err != nil || len(value) == 0 ||
		provider.purpose != "graph.mailbox.credential.mailbox-id" {
		t.Fatalf("Resolve() value=%q provider=%+v err=%v", value, provider, err)
	}
	for _, fragment := range []string{"id = $1", "configuration_generation = $2", "enabled"} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("resolver query missing %q: %s", fragment, db.query)
		}
	}
}

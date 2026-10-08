package psa

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type legacyTeamsResolverStub struct {
	value     string
	err       error
	reference string
}

func (r *legacyTeamsResolverStub) Resolve(_ context.Context, connection notifications.TeamsConnection) (string, error) {
	r.reference = connection.WebhookSecretRef
	return r.value, r.err
}

func validManagedTeamsConnection() notifications.ManagedTeamsConnection {
	return notifications.ManagedTeamsConnection{ID: "connection-id", MSPID: "msp-id", ClientID: "client-id", Name: "Primary Teams", CredentialConfigured: true, Health: notifications.TeamsHealthPending, Version: 1}
}

func validTeamsMutation() notifications.TeamsConnectionMutation {
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	connection := validManagedTeamsConnection()
	return notifications.TeamsConnectionMutation{Connection: connection, PlaintextWebhookURL: []byte("https://teams.example.test/webhook"), Audit: validAudit(at, "teams.connection.created", "teams_connection", connection.ID), Event: validEvent(at, "teams.connection.created", "teams_connection", connection.ID)}
}

func TestTeamsConnectionRepositorySealsWriteOnlyWebhookAndWritesFactsAtomically(t *testing.T) {
	tx, provider := &fakeSalesTx{}, &recordingSecretProvider{}
	mutation := validTeamsMutation()
	if err := NewTeamsConnectionRepository(&fakeSalesDB{tx: tx}, provider, nil).CreateTeamsConnection(context.Background(), mutation); err != nil {
		t.Fatalf("CreateTeamsConnection() error=%v", err)
	}
	if !credentialZeroed(mutation.PlaintextWebhookURL) {
		t.Fatalf("repository retained plaintext webhook bytes: %q", mutation.PlaintextWebhookURL)
	}
	if provider.purpose != "teams.connection.connection-id" || !tx.committed || tx.rolledBack {
		t.Fatalf("provider=%+v committed=%v rolledBack=%v", provider, tx.committed, tx.rolledBack)
	}
	assertQueryOrder(t, tx.queries, "pg_advisory_xact_lock", "INSERT INTO teams_connections", "INSERT INTO audit_ledger", "INSERT INTO event_outbox")
	for _, args := range tx.args {
		if fmt.Sprint(args) == "[https://teams.example.test/webhook]" {
			t.Fatal("plaintext webhook reached SQL")
		}
	}
}

func TestTeamsConnectionRepositoryUpdatesAreMSPVersionScopedAndRollBackBeforeFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 2}
	mutation := validTeamsMutation()
	mutation.ExpectedVersion = 1
	err := NewTeamsConnectionRepository(&fakeSalesDB{tx: tx}, &recordingSecretProvider{}, nil).UpdateTeamsConnectionMetadata(context.Background(), mutation)
	if !errors.Is(err, object.ErrVersionConflict) || !tx.rolledBack || tx.committed {
		t.Fatalf("UpdateTeamsConnectionMetadata() error=%v committed=%v rolledBack=%v", err, tx.committed, tx.rolledBack)
	}
	if len(tx.queries) != 2 || !strings.Contains(tx.queries[1], "id = $1 AND msp_id = $2 AND version = $3") {
		t.Fatalf("optimistic MSP update missing: %s", tx.queries)
	}
}

func TestTeamsConnectionRepositoryUsesExactSnapshotAndSupportsEncryptedAndLegacyCredentials(t *testing.T) {
	t.Run("encrypted", func(t *testing.T) {
		db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
			*(destinations[0].(**string)) = nil
			*(destinations[1].(*int)) = 7
			*(destinations[2].(*[]byte)) = []byte("nonce")
			*(destinations[3].(*[]byte)) = []byte("ciphertext")
		}}}
		provider := &recordingSecretProvider{open: []byte("https://teams.example.test/encrypted")}
		var observed []byte
		err := NewTeamsConnectionRepository(db, provider, nil).UseTeamsWebhook(context.Background(), scope.Target{MSPID: "msp-id", ClientID: "client-id"}, validManagedTeamsConnection(), func(value []byte) error { observed = append([]byte(nil), value...); return nil })
		if err != nil || string(observed) != "https://teams.example.test/encrypted" || provider.purpose != "teams.connection.connection-id" {
			t.Fatalf("UseTeamsWebhook() value=%q provider=%+v err=%v", observed, provider, err)
		}
		for _, fragment := range []string{"connection.version = $3", "connection.client_id IS NOT DISTINCT FROM NULLIF($4, '')::uuid", "connection.msp_id = $2"} {
			if !strings.Contains(db.query, fragment) {
				t.Fatalf("snapshot fence missing %q in %s", fragment, db.query)
			}
		}
	})
	t.Run("legacy env reference", func(t *testing.T) {
		ref := "env://RARITY_TEAMS_WEBHOOK_PRIMARY"
		db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
			*(destinations[0].(**string)) = &ref
			*(destinations[1].(*int)) = 0
			*(destinations[2].(*[]byte)) = nil
			*(destinations[3].(*[]byte)) = nil
		}}}
		legacy := &legacyTeamsResolverStub{value: "https://teams.example.test/legacy"}
		var observed []byte
		err := NewTeamsConnectionRepository(db, &recordingSecretProvider{}, legacy).UseTeamsWebhook(context.Background(), scope.Target{MSPID: "msp-id", ClientID: "client-id"}, validManagedTeamsConnection(), func(value []byte) error { observed = append([]byte(nil), value...); return nil })
		if err != nil || legacy.reference != ref || string(observed) != legacy.value {
			t.Fatalf("legacy observed=%q reference=%q err=%v", observed, legacy.reference, err)
		}
	})
}

func TestTeamsConnectionRepositoryHidesCrossScopeLookupsAsNotFound(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: pgx.ErrNoRows}}
	_, err := NewTeamsConnectionRepository(db, &recordingSecretProvider{}, nil).GetTeamsConnection(context.Background(), scope.Target{MSPID: "msp-id", ClientID: "client-id"}, "connection-id")
	if !errors.Is(err, scope.ErrNotFound) || !strings.Contains(db.query, "msp_id = $2 AND ($3 = '' OR client_id = $3::uuid)") {
		t.Fatalf("GetTeamsConnection() error=%v query=%s", err, db.query)
	}
}

package psa

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/graphintake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
)

func graphTestProvider(t *testing.T) secrets.Provider {
	t.Helper()
	provider, err := secrets.NewLocalProvider(
		[]byte("0123456789abcdef0123456789abcdef"),
		func(size int) ([]byte, error) {
			return []byte("0123456789ab")[:size], nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestGraphRepositoryPlansCursorBeforeClaimingDueMailbox(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewGraphRepository(
		&fakeSalesDB{
			tx: tx,
			queryRows: &fakeRows{scans: []func(...any){
				func(destinations ...any) {
					*(destinations[0].(*string)) = "22222222-2222-4222-8222-222222222222"
				},
			}},
		}, graphTestProvider(t),
		func() string { return "11111111-1111-4111-8111-111111111111" },
	)

	planned, err := repository.Plan(
		context.Background(), 25,
		time.Date(2026, time.July, 29, 20, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if planned != 1 || !tx.committed {
		t.Fatalf("Plan() planned=%d committed=%v", planned, tx.committed)
	}
	assertQueryOrder(t, tx.queries, "INSERT INTO graph_delta_cursors")
}

func TestNewGraphRepositoryFromPoolBuildsProductionAdapter(t *testing.T) {
	repository := NewGraphRepositoryFromPool(
		nil, graphTestProvider(t),
		func() string { return "11111111-1111-4111-8111-111111111111" },
	)
	if repository == nil || repository.db == nil {
		t.Fatal("production repository did not retain a pool adapter")
	}
}

func TestGraphRepositoryClaimsDueMailboxWithTimestampCutoff(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	jobs, err := NewGraphRepository(
		db, graphTestProvider(t), func() string { return "id" },
	).Claim(context.Background(), 25, time.Now(), 10*time.Minute)
	if err != nil || len(jobs) != 0 ||
		!strings.Contains(db.query, "$1::timestamptz - interval '5 minutes'") {
		t.Fatalf("Claim() jobs=%+v error=%v query=%s", jobs, err, db.query)
	}
}

func TestBoundGraphRepositoryCommitsMessagesCursorAuditAndOutboxAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewGraphRepository(
		&fakeSalesDB{tx: tx}, graphTestProvider(t),
		func() string { return "11111111-1111-4111-8111-111111111111" },
	)
	boundRepository, _ := repository.Bind(
		"22222222-2222-4222-8222-222222222222",
	)
	at := time.Date(2026, time.July, 29, 20, 0, 0, 0, time.UTC)

	err := boundRepository.Commit(context.Background(), graphintake.IntakeBatch{
		Mailbox: "support@example.com", Folder: "inbox",
		NextCursor:  "https://graph.microsoft.com/v1.0/delta?token=opaque",
		CommittedAt: at,
		Messages: []graphintake.NormalizedMessage{{
			ExternalID: "message-id", ConversationID: "conversation-id",
			InternetMessageID: "<message@example.com>",
			Subject:           "Printer unavailable", Sender: "sender@example.net",
			ReceivedAt: at, RawMIMERef: "graph/connection/message-id.eml",
			Thread: graphintake.ThreadResolution{
				MatchedBy: graphintake.MatchNone, CreateNew: true,
			},
		}},
	})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("transaction state committed=%v rolledBack=%v", tx.committed, tx.rolledBack)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO inbound_email_messages",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
		"UPDATE graph_delta_cursors",
		"UPDATE graph_mailbox_connections",
		"UPDATE graph_subscriptions",
	)
	var cursorUpdate string
	for _, query := range tx.queries {
		if strings.Contains(query, "token=opaque") {
			t.Fatal("delta cursor was embedded in SQL")
		}
		if strings.Contains(query, "UPDATE graph_delta_cursors") {
			cursorUpdate = query
		}
	}
	if !strings.Contains(cursorUpdate, "version = cursor.version + 1") {
		t.Fatalf("cursor update does not qualify version: %s", cursorUpdate)
	}
}

func TestBoundGraphRepositoryRollsBackCursorWhenMessageEvidenceFails(t *testing.T) {
	tx := &fakeSalesTx{failAt: 2}
	repository := NewGraphRepository(
		&fakeSalesDB{tx: tx}, graphTestProvider(t),
		func() string { return "11111111-1111-4111-8111-111111111111" },
	)
	boundRepository, _ := repository.Bind(
		"22222222-2222-4222-8222-222222222222",
	)
	err := boundRepository.Commit(context.Background(), graphintake.IntakeBatch{
		Mailbox: "support@example.com", Folder: "inbox",
		NextCursor:  "https://graph.microsoft.com/v1.0/delta?token=opaque",
		CommittedAt: time.Now(),
		Messages: []graphintake.NormalizedMessage{{
			ExternalID: "message-id", Sender: "sender@example.net",
			ReceivedAt: time.Now(), RawMIMERef: "graph/connection/message-id.eml",
		}},
	})
	if err == nil || tx.committed || !tx.rolledBack {
		t.Fatalf("failure did not roll back: error=%v tx=%+v", err, tx)
	}
	for _, query := range tx.queries {
		if strings.Contains(query, "UPDATE graph_delta_cursors") {
			t.Fatal("cursor advanced after evidence failure")
		}
	}
}

func TestGraphRepositoryLoadsEnabledSubscriptionWithoutExposingSecretValue(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(*string)) = "connection-id"
		*(destinations[1].(*string)) = "subscription-id"
		*(destinations[2].(*string)) = "support@example.com"
		*(destinations[3].(*string)) = "env://RARITY_GRAPH_CLIENT_STATE_SUPPORT"
	}}}
	target, err := NewGraphRepository(
		db, graphTestProvider(t), func() string { return "id" },
	).LoadSubscription(context.Background(), "subscription-id")
	if err != nil {
		t.Fatalf("LoadSubscription() error = %v", err)
	}
	if target.ConnectionID != "connection-id" ||
		target.ClientStateSecretRef != "env://RARITY_GRAPH_CLIENT_STATE_SUPPORT" ||
		!strings.Contains(db.query, "connection.enabled") {
		t.Fatalf("target=%+v query=%s", target, db.query)
	}
}

func TestGraphRepositoryAcceptsHintAndLifecycleRecoveryAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewGraphRepository(
		&fakeSalesDB{tx: tx}, graphTestProvider(t),
		func() string { return "id" },
	)
	at := time.Date(2026, time.July, 29, 21, 0, 0, 0, time.UTC)
	err := repository.AcceptNotifications(
		context.Background(),
		graphintake.NotificationMutation{
			Hints: []graphintake.NotificationHint{{
				ID: "hint-id", ConnectionID: "connection-id",
				SubscriptionID: "subscription-id", MessageID: "message-id",
				ReceivedAt: at,
			}},
			Lifecycle: []graphintake.LifecycleMutation{{
				ConnectionID: "connection-id", SubscriptionID: "subscription-id",
				Event:    graphintake.LifecycleMissedNotifications,
				Recovery: graphintake.RecoveryRunDelta, ReceivedAt: at,
			}},
		},
	)
	if err != nil {
		t.Fatalf("AcceptNotifications() error = %v", err)
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("transaction state committed=%v rolledBack=%v", tx.committed, tx.rolledBack)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO graph_notification_hints",
		"UPDATE graph_subscriptions",
	)
	var subscriptionUpdate string
	for _, query := range tx.queries {
		if strings.Contains(query, "UPDATE graph_subscriptions subscription") {
			subscriptionUpdate = query
		}
	}
	if !strings.Contains(
		subscriptionUpdate,
		"version = subscription.version + 1",
	) {
		t.Fatalf(
			"subscription update does not qualify version: %s",
			subscriptionUpdate,
		)
	}
}

func TestGraphRepositoryClaimsOnlyPendingNotificationHintsWithLease(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*(destinations[0].(*string)) = "hint-id"
			*(destinations[1].(*string)) = "connection-id"
			*(destinations[2].(*string)) = "msp-id"
			*(destinations[3].(*string)) = "support@example.com"
			*(destinations[4].(*string)) = "env://RARITY_GRAPH_CREDENTIAL_SUPPORT"
			*(destinations[5].(*string)) = "message-id"
		},
	}}}
	jobs, err := NewGraphRepository(
		db, graphTestProvider(t), func() string { return "id" },
	).ClaimNotifications(
		context.Background(), 100, time.Now(), 10*time.Minute,
	)
	if err != nil {
		t.Fatalf("ClaimNotifications() error = %v", err)
	}
	if len(jobs) != 1 || jobs[0].HintID != "hint-id" ||
		jobs[0].MessageID != "message-id" ||
		!strings.Contains(db.query, "FOR UPDATE OF hint SKIP LOCKED") {
		t.Fatalf("jobs=%+v query=%s", jobs, db.query)
	}
}

func TestGraphRepositoryMarksNotificationCompletionByLeasedState(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(*GraphRepository) error
		want string
	}{
		{
			name: "processed",
			run: func(repository *GraphRepository) error {
				return repository.MarkNotificationProcessed(
					context.Background(), "hint-id", time.Now(),
				)
			},
			want: "state = 'processed'",
		},
		{
			name: "failed",
			run: func(repository *GraphRepository) error {
				return repository.MarkNotificationFailed(
					context.Background(), "hint-id", time.Now(), "provider_timeout",
				)
			},
			want: "last_error_code",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeSalesTx{}
			repository := NewGraphRepository(
				&fakeSalesDB{tx: tx}, graphTestProvider(t),
				func() string { return "id" },
			)
			if err := test.run(repository); err != nil {
				t.Fatalf("completion error = %v", err)
			}
			if !tx.committed || len(tx.queries) != 1 ||
				!strings.Contains(tx.queries[0], test.want) {
				t.Fatalf("transaction=%+v queries=%v", tx, tx.queries)
			}
		})
	}
}

func TestGraphRepositoryClaimsMissingAndExpiringSubscriptions(t *testing.T) {
	expiresAt := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*(destinations[0].(*string)) = ""
			*(destinations[1].(*string)) = "connection-id"
			*(destinations[2].(*string)) = "support@example.com"
			*(destinations[3].(*string)) = "env://RARITY_GRAPH_CREDENTIAL_SUPPORT"
			*(destinations[4].(*string)) = "env://RARITY_GRAPH_CLIENT_STATE_SUPPORT"
			*(destinations[5].(*string)) = ""
			*(destinations[6].(*string)) = ""
			*(destinations[7].(*time.Time)) = expiresAt
			*(destinations[8].(*graphintake.RecoveryAction)) = graphintake.RecoveryNone
		},
	}}}
	jobs, err := NewGraphRepository(
		db, graphTestProvider(t),
		func() string { return "new-subscription-record-id" },
	).ClaimSubscriptions(
		context.Background(), 25, time.Now(), 5*time.Minute,
	)
	if err != nil || len(jobs) != 1 ||
		jobs[0].ID != "new-subscription-record-id" ||
		jobs[0].Mailbox != "support@example.com" ||
		jobs[0].ClientStateSecretRef !=
			"env://RARITY_GRAPH_CLIENT_STATE_SUPPORT" ||
		!strings.Contains(
			db.query,
			"subscription.expires_at <= $1::timestamptz + interval '24 hours'",
		) ||
		!strings.Contains(db.query, "SKIP LOCKED") {
		t.Fatalf("ClaimSubscriptions() jobs=%+v error=%v query=%s", jobs, err, db.query)
	}
}

func TestGraphRepositoryCompletesSubscriptionAuditAndRecoveryAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewGraphRepository(
		&fakeSalesDB{tx: tx}, graphTestProvider(t),
		func() string { return "11111111-1111-4111-8111-111111111111" },
	)
	err := repository.CompleteSubscription(
		context.Background(),
		graphintake.SubscriptionCompletion{
			ID: "subscription-record-id", ConnectionID: "connection-id",
			ExternalID:  "graph-subscription-id",
			Resource:    "users/support@example.com/mailFolders('Inbox')/messages",
			ExpiresAt:   time.Now().Add(6 * 24 * time.Hour),
			CompletedAt: time.Now(), Recovery: graphintake.RecoveryRunDelta,
			Created: true,
		},
	)
	if err != nil {
		t.Fatalf("CompleteSubscription() error=%v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO graph_subscriptions",
		"UPDATE graph_mailbox_connections",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
	if !strings.Contains(tx.queries[0], "COALESCE(") ||
		!strings.Contains(tx.queries[0], "connection.client_state_secret_ref") ||
		!strings.Contains(tx.queries[0], "'db://graph/'") {
		t.Fatalf(
			"subscription does not retain a resolvable client-state reference: %s",
			tx.queries[0],
		)
	}
	if !strings.Contains(
		tx.queries[0], "connection.last_subscription_attempt_at = $6",
	) || !strings.Contains(
		tx.queries[1], "last_subscription_attempt_at = $2",
	) {
		t.Fatalf("completion is not fenced by the claimed attempt: %v", tx.queries[:2])
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("transaction committed=%v rolledBack=%v", tx.committed, tx.rolledBack)
	}
}

func TestGraphRepositoryFailsOnlyTheClaimedSubscriptionAttempt(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewGraphRepository(
		&fakeSalesDB{tx: tx}, graphTestProvider(t),
		func() string { return "id" },
	)
	failedAt := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)

	err := repository.FailSubscription(
		context.Background(),
		graphintake.SubscriptionFailure{
			ID: "subscription-record-id", ConnectionID: "connection-id",
			FailedAt: failedAt, ErrorCode: "subscription_provider_failed",
		},
	)

	if err != nil || len(tx.queries) != 1 ||
		!strings.Contains(tx.queries[0], "last_subscription_attempt_at = $2") ||
		!tx.committed || tx.rolledBack {
		t.Fatalf(
			"FailSubscription() error=%v query=%v committed=%v rolledBack=%v",
			err, tx.queries, tx.committed, tx.rolledBack,
		)
	}
}

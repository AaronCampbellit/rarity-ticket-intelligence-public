package graphintake

import (
	"errors"
	"testing"
	"time"
)

func TestValidateNotificationRequiresExactClientStateAndAllowedMailbox(t *testing.T) {
	notification := Notification{
		SubscriptionID: "subscription-id",
		ClientState:    "expected-state",
		Resource:       "users/support@example.com/mailFolders/inbox/messages/message-id",
	}
	if err := ValidateNotification(notification, "expected-state", "support@example.com"); err != nil {
		t.Fatalf("ValidateNotification() error = %v", err)
	}
	notification.ClientState = "wrong"
	if err := ValidateNotification(notification, "expected-state", "support@example.com"); !errors.Is(err, ErrInvalidNotification) {
		t.Fatalf("client-state mismatch error = %v", err)
	}
	notification.ClientState = "expected-state"
	notification.Resource = "users/other@example.com/mailFolders/inbox/messages/message-id"
	if err := ValidateNotification(notification, "expected-state", "support@example.com"); !errors.Is(err, ErrInvalidNotification) {
		t.Fatalf("mailbox mismatch error = %v", err)
	}
}

func TestReliabilityScheduleRenewsEarlyAndReconcilesWithinFiveMinutes(t *testing.T) {
	now := time.Date(2026, time.July, 29, 18, 0, 0, 0, time.UTC)
	if !SubscriptionNeedsRenewal(now, now.Add(30*time.Minute), time.Hour) {
		t.Fatal("subscription inside renewal lead time was not selected")
	}
	if SubscriptionNeedsRenewal(now, now.Add(2*time.Hour), time.Hour) {
		t.Fatal("healthy subscription selected too early")
	}
	if !DeltaReconciliationDue(now.Add(-5*time.Minute), now) {
		t.Fatal("five-minute delta reconciliation is not due")
	}
	if DeltaReconciliationDue(now.Add(-4*time.Minute), now) {
		t.Fatal("delta reconciliation selected before five minutes")
	}
}

func TestLifecycleSignalsDriveExplicitRecoveryActions(t *testing.T) {
	cases := map[LifecycleEvent]RecoveryAction{
		LifecycleReauthorizationRequired: RecoveryReauthorize,
		LifecycleSubscriptionRemoved:     RecoveryRecreate,
		LifecycleMissedNotifications:     RecoveryRunDelta,
	}
	for event, expected := range cases {
		if actual := RecoveryFor(event); actual != expected {
			t.Fatalf("RecoveryFor(%q) = %q, want %q", event, actual, expected)
		}
	}
}

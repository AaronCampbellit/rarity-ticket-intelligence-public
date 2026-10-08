// Package graphintake defines the recoverable Microsoft Graph mailbox intake
// boundary. Webhook notifications are hints; delta reconciliation remains the
// durable recovery path.
package graphintake

import (
	"crypto/subtle"
	"errors"
	"strings"
	"time"
)

var ErrInvalidNotification = errors.New("invalid Graph notification")

type Notification struct {
	SubscriptionID string
	ClientState    string
	Resource       string
}

func ValidateNotification(
	notification Notification,
	expectedClientState string,
	allowedMailbox string,
) error {
	if notification.SubscriptionID == "" || expectedClientState == "" ||
		allowedMailbox == "" ||
		subtle.ConstantTimeCompare(
			[]byte(notification.ClientState),
			[]byte(expectedClientState),
		) != 1 {
		return ErrInvalidNotification
	}
	resource := strings.ToLower(strings.TrimSpace(notification.Resource))
	mailboxPrefix := "users/" + strings.ToLower(strings.TrimSpace(allowedMailbox)) + "/"
	if !strings.HasPrefix(resource, mailboxPrefix) ||
		!strings.Contains(resource, "/mailfolders/") ||
		!strings.Contains(resource, "/messages/") {
		return ErrInvalidNotification
	}
	return nil
}

func SubscriptionNeedsRenewal(now, expiresAt time.Time, lead time.Duration) bool {
	return lead > 0 && !expiresAt.After(now.Add(lead))
}

func DeltaReconciliationDue(lastCompletedAt, now time.Time) bool {
	return !lastCompletedAt.After(now.Add(-5 * time.Minute))
}

type LifecycleEvent string

const (
	LifecycleReauthorizationRequired LifecycleEvent = "reauthorizationRequired"
	LifecycleSubscriptionRemoved     LifecycleEvent = "subscriptionRemoved"
	LifecycleMissedNotifications     LifecycleEvent = "missed"
)

type RecoveryAction string

const (
	RecoveryNone        RecoveryAction = "none"
	RecoveryReauthorize RecoveryAction = "reauthorize"
	RecoveryRecreate    RecoveryAction = "recreate_subscription"
	RecoveryRunDelta    RecoveryAction = "run_delta"
)

func RecoveryFor(event LifecycleEvent) RecoveryAction {
	switch event {
	case LifecycleReauthorizationRequired:
		return RecoveryReauthorize
	case LifecycleSubscriptionRemoved:
		return RecoveryRecreate
	case LifecycleMissedNotifications:
		return RecoveryRunDelta
	default:
		return RecoveryNone
	}
}

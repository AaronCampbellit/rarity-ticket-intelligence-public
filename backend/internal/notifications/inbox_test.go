package notifications

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type recipientInboxRepository struct {
	notifications              []RecipientNotification
	mspID, recipientID, cursor string
	limit                      int
	markedID                   string
	markedVersion              int64
}

func (r *recipientInboxRepository) ListRecipientNotifications(_ context.Context, mspID, recipientID, cursor string, limit int) (InboxPage, error) {
	r.mspID, r.recipientID, r.cursor, r.limit = mspID, recipientID, cursor, limit
	return InboxPage{Notifications: append([]RecipientNotification(nil), r.notifications...), NextCursor: "next"}, nil
}

func (r *recipientInboxRepository) CountUnreadRecipientNotifications(_ context.Context, mspID, recipientID string) (int, error) {
	r.mspID, r.recipientID = mspID, recipientID
	return 2, nil
}

func (r *recipientInboxRepository) MarkRecipientNotificationRead(_ context.Context, mspID, recipientID, notificationID string, expectedVersion int64, _ time.Time) (RecipientNotification, error) {
	r.mspID, r.recipientID, r.markedID, r.markedVersion = mspID, recipientID, notificationID, expectedVersion
	value := r.notifications[0]
	return value, nil
}

func TestRecipientInboxScopesEveryActionToAuthenticatedPrincipal(t *testing.T) {
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	const notificationID = "00000000-0000-4000-8000-000000000201"
	repository := &recipientInboxRepository{notifications: []RecipientNotification{{ID: notificationID, RecipientID: "tech", Version: 3}}}
	service := NewInboxService(repository, func() time.Time { return now })
	principal := authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp"}}
	page, err := service.List(context.Background(), principal, "cursor", 150)
	if err != nil || len(page.Notifications) != 1 || repository.mspID != "msp" || repository.recipientID != "tech" || repository.limit != 100 {
		t.Fatalf("page=%+v repository=%+v err=%v", page, repository, err)
	}
	count, err := service.UnreadCount(context.Background(), principal)
	if err != nil || count != 2 || repository.recipientID != "tech" {
		t.Fatalf("count=%d repository=%+v err=%v", count, repository, err)
	}
	marked, err := service.MarkRead(context.Background(), principal, notificationID, 3)
	if err != nil || marked.ID != notificationID || repository.markedID != notificationID || repository.markedVersion != 3 {
		t.Fatalf("marked=%+v repository=%+v err=%v", marked, repository, err)
	}
}

func TestRecipientInboxRejectsIncompletePrincipalAndInvalidReadVersion(t *testing.T) {
	service := NewInboxService(&recipientInboxRepository{}, time.Now)
	if _, err := service.List(context.Background(), authorization.Principal{}, "", 50); err == nil {
		t.Fatal("incomplete principal accepted")
	}
	principal := authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp"}}
	if _, err := service.MarkRead(context.Background(), principal, "notification", 0); err == nil {
		t.Fatal("invalid read version accepted")
	}
}

func TestRecipientInboxRejectsMalformedNotificationIDBeforeRepositoryLookup(t *testing.T) {
	repository := &recipientInboxRepository{notifications: []RecipientNotification{{ID: "unchanged"}}}
	service := NewInboxService(repository, time.Now)
	principal := authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp"}}
	_, err := service.MarkRead(context.Background(), principal, "not-a-canonical-uuid", 1)
	if !errors.Is(err, ErrInvalidInboxRequest) || repository.markedID != "" {
		t.Fatalf("error=%v marked ID=%q", err, repository.markedID)
	}
}

package notifications

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
)

var ErrInvalidInboxRequest = errors.New("invalid notification inbox request")

type RecipientNotification struct {
	ID, RecipientID, DeliveryID, DeduplicationKey  string
	Title, Body, ActionPath, ContentClassification string
	CreatedAt                                      time.Time
	ReadAt                                         *time.Time
	Version                                        int64
}

type InboxPage struct {
	Notifications []RecipientNotification
	NextCursor    string
}

type InboxRepository interface {
	ListRecipientNotifications(context.Context, string, string, string, int) (InboxPage, error)
	CountUnreadRecipientNotifications(context.Context, string, string) (int, error)
	MarkRecipientNotificationRead(context.Context, string, string, string, int64, time.Time) (RecipientNotification, error)
}

type InboxService struct {
	repository InboxRepository
	now        func() time.Time
}

func NewInboxService(repository InboxRepository, now func() time.Time) *InboxService {
	return &InboxService{repository: repository, now: now}
}

func (s *InboxService) List(ctx context.Context, principal authorization.Principal, cursor string, limit int) (InboxPage, error) {
	if !validInboxPrincipal(s, principal) {
		return InboxPage{}, ErrInvalidInboxRequest
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	return s.repository.ListRecipientNotifications(ctx, principal.Scope.MSPID, principal.ID, cursor, limit)
}

func (s *InboxService) UnreadCount(ctx context.Context, principal authorization.Principal) (int, error) {
	if !validInboxPrincipal(s, principal) {
		return 0, ErrInvalidInboxRequest
	}
	return s.repository.CountUnreadRecipientNotifications(ctx, principal.Scope.MSPID, principal.ID)
}

func (s *InboxService) MarkRead(ctx context.Context, principal authorization.Principal, notificationID string, expectedVersion int64) (RecipientNotification, error) {
	if !validInboxPrincipal(s, principal) || !internalid.ValidCanonical(notificationID) || expectedVersion < 1 || s.now == nil {
		return RecipientNotification{}, ErrInvalidInboxRequest
	}
	return s.repository.MarkRecipientNotificationRead(ctx, principal.Scope.MSPID, principal.ID, notificationID, expectedVersion, s.now().UTC())
}

func validInboxPrincipal(s *InboxService, principal authorization.Principal) bool {
	return s != nil && s.repository != nil && strings.TrimSpace(principal.ID) != "" && strings.TrimSpace(principal.Scope.MSPID) != ""
}

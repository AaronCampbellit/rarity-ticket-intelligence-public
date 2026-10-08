package graphintake

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

type SubscriptionTarget struct {
	ConnectionID         string
	SubscriptionID       string
	Mailbox              string
	ClientStateSecretRef string
}

type NotificationHint struct {
	ID             string
	ConnectionID   string
	SubscriptionID string
	MessageID      string
	ReceivedAt     time.Time
}

type LifecycleMutation struct {
	ConnectionID   string
	SubscriptionID string
	Event          LifecycleEvent
	Recovery       RecoveryAction
	ReceivedAt     time.Time
}

type NotificationMutation struct {
	Hints     []NotificationHint
	Lifecycle []LifecycleMutation
}

type NotificationRepository interface {
	LoadSubscription(context.Context, string) (SubscriptionTarget, error)
	AcceptNotifications(context.Context, NotificationMutation) error
}

type ClientStateResolver interface {
	ResolveClientState(context.Context, string) (string, error)
}

type NotificationService struct {
	repository  NotificationRepository
	clientState ClientStateResolver
	now         func() time.Time
	newID       func() string
}

type NotificationAcceptance struct {
	Accepted int `json:"accepted"`
}

func NewNotificationService(
	repository NotificationRepository,
	clientState ClientStateResolver,
	now func() time.Time,
	newID func() string,
) *NotificationService {
	return &NotificationService{
		repository: repository, clientState: clientState,
		now: now, newID: newID,
	}
}

func (s *NotificationService) Accept(
	ctx context.Context,
	body []byte,
) (NotificationAcceptance, error) {
	if s.repository == nil || s.clientState == nil ||
		s.now == nil || s.newID == nil || len(body) == 0 {
		return NotificationAcceptance{}, ErrInvalidNotification
	}
	var envelope struct {
		Value []struct {
			SubscriptionID string `json:"subscriptionId"`
			ClientState    string `json:"clientState"`
			Resource       string `json:"resource"`
			LifecycleEvent string `json:"lifecycleEvent"`
			ResourceData   struct {
				ID string `json:"id"`
			} `json:"resourceData"`
		} `json:"value"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&envelope); err != nil ||
		len(envelope.Value) == 0 || len(envelope.Value) > 1000 {
		return NotificationAcceptance{}, ErrInvalidNotification
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return NotificationAcceptance{}, ErrInvalidNotification
	}
	at := s.now().UTC()
	mutation := NotificationMutation{
		Hints:     make([]NotificationHint, 0, len(envelope.Value)),
		Lifecycle: make([]LifecycleMutation, 0),
	}
	for _, value := range envelope.Value {
		target, err := s.repository.LoadSubscription(
			ctx, strings.TrimSpace(value.SubscriptionID),
		)
		if err != nil {
			return NotificationAcceptance{}, ErrInvalidNotification
		}
		expectedState, err := s.clientState.ResolveClientState(
			ctx, target.ClientStateSecretRef,
		)
		if err != nil || ValidateNotification(Notification{
			SubscriptionID: value.SubscriptionID,
			ClientState:    value.ClientState,
			Resource:       value.Resource,
		}, expectedState, target.Mailbox) != nil ||
			target.SubscriptionID != value.SubscriptionID {
			return NotificationAcceptance{}, ErrInvalidNotification
		}
		if strings.TrimSpace(value.LifecycleEvent) != "" {
			event := LifecycleEvent(value.LifecycleEvent)
			recovery := RecoveryFor(event)
			if recovery == RecoveryNone {
				return NotificationAcceptance{}, ErrInvalidNotification
			}
			mutation.Lifecycle = append(
				mutation.Lifecycle,
				LifecycleMutation{
					ConnectionID:   target.ConnectionID,
					SubscriptionID: target.SubscriptionID,
					Event:          event, Recovery: recovery, ReceivedAt: at,
				},
			)
			continue
		}
		messageID := strings.TrimSpace(value.ResourceData.ID)
		resource := strings.Trim(strings.TrimSpace(value.Resource), "/")
		if messageID == "" ||
			!strings.HasSuffix(resource, "/messages/"+messageID) {
			return NotificationAcceptance{}, ErrInvalidNotification
		}
		hintID := s.newID()
		if strings.TrimSpace(hintID) == "" {
			return NotificationAcceptance{}, ErrInvalidNotification
		}
		mutation.Hints = append(mutation.Hints, NotificationHint{
			ID: hintID, ConnectionID: target.ConnectionID,
			SubscriptionID: target.SubscriptionID,
			MessageID:      messageID, ReceivedAt: at,
		})
	}
	if err := s.repository.AcceptNotifications(ctx, mutation); err != nil {
		return NotificationAcceptance{}, err
	}
	return NotificationAcceptance{Accepted: len(envelope.Value)}, nil
}

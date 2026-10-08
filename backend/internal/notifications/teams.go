package notifications

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
)

var (
	ErrTeamsDeliveryFailed = errors.New("Teams delivery failed")
	ErrTeamsRetryExpired   = errors.New("Teams retry window expired")
)

type TeamsConnection struct {
	ID               string
	MSPID            string
	ClientID         string
	Name             string
	WebhookSecretRef string
	Version          int64
	Disabled         bool
}

type TeamsDeliveryState string

const (
	TeamsDelivered TeamsDeliveryState = "delivered"
	TeamsRetrying  TeamsDeliveryState = "retrying"
	TeamsFailed    TeamsDeliveryState = "failed"
)

type TeamsAttempt struct {
	ConnectionID      string
	ConnectionVersion int64
	EventID           string
	WorkRecordID      string
	Attempt           int
	FirstAttemptAt    time.Time
	AttemptedAt       time.Time
	State             TeamsDeliveryState
	ErrorCode         string
}

type TeamsRequest struct {
	Connection     TeamsConnection
	EventID        string
	WorkRecordID   string
	Summary        WorkSummary
	FirstAttemptAt time.Time
	Attempt        int
}

type TeamsSecretResolver interface {
	Resolve(context.Context, TeamsConnection) (string, error)
}

type TeamsTransport interface {
	Send(context.Context, string, []byte) error
}

type TeamsDeliveryHistory interface {
	Record(context.Context, TeamsAttempt) error
}

type TeamsService struct {
	secrets TeamsSecretResolver
	sender  TeamsTransport
	history TeamsDeliveryHistory
	now     func() time.Time
}

func NewTeamsService(
	secrets TeamsSecretResolver,
	sender TeamsTransport,
	history TeamsDeliveryHistory,
	now func() time.Time,
) *TeamsService {
	return &TeamsService{secrets: secrets, sender: sender, history: history, now: now}
}

func (s *TeamsService) Deliver(
	ctx context.Context,
	request TeamsRequest,
) error {
	connection := request.Connection
	if s.secrets == nil || s.sender == nil || s.history == nil || s.now == nil ||
		strings.TrimSpace(connection.ID) == "" || strings.TrimSpace(connection.MSPID) == "" ||
		connection.Version < 1 ||
		strings.TrimSpace(request.EventID) == "" ||
		strings.TrimSpace(request.WorkRecordID) == "" ||
		request.FirstAttemptAt.IsZero() || request.Attempt < 1 {
		return ErrTeamsDeliveryFailed
	}
	now := s.now().UTC()
	if !request.FirstAttemptAt.Add(24 * time.Hour).After(now) {
		if err := s.history.Record(
			ctx, request.attempt(now, TeamsFailed, "retry_window_expired"),
		); err != nil {
			return err
		}
		return ErrTeamsRetryExpired
	}
	if connection.Disabled {
		if err := s.history.Record(
			ctx, request.attempt(now, TeamsRetrying, "connection_disabled"),
		); err != nil {
			return err
		}
		return ErrTeamsDeliveryFailed
	}
	payload, err := BuildTeamsCard(request.Summary)
	if err != nil {
		return err
	}
	endpoint, err := s.secrets.Resolve(ctx, connection)
	if err != nil || webhooks.ValidateDestination(endpoint) != nil {
		if recordErr := s.history.Record(
			ctx, request.attempt(now, TeamsRetrying, "connection_unavailable"),
		); recordErr != nil {
			return recordErr
		}
		return ErrTeamsDeliveryFailed
	}
	if err := s.sender.Send(ctx, endpoint, payload); err != nil {
		if recordErr := s.history.Record(
			ctx, request.attempt(now, TeamsRetrying, "delivery_failed"),
		); recordErr != nil {
			return recordErr
		}
		return ErrTeamsDeliveryFailed
	}
	return s.history.Record(ctx, request.attempt(now, TeamsDelivered, ""))
}

func (request TeamsRequest) attempt(
	at time.Time,
	state TeamsDeliveryState,
	errorCode string,
) TeamsAttempt {
	return TeamsAttempt{
		ConnectionID: request.Connection.ID, ConnectionVersion: request.Connection.Version,
		EventID:      request.EventID,
		WorkRecordID: request.WorkRecordID, Attempt: request.Attempt,
		FirstAttemptAt: request.FirstAttemptAt, AttemptedAt: at,
		State: state, ErrorCode: errorCode,
	}
}

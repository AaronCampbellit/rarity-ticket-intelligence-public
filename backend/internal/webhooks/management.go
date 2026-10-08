package webhooks

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrManagementUnavailable = errors.New("webhook management unavailable")
	ErrInvalidManagement     = errors.New("invalid webhook management request")
)

type ManagedConnection struct {
	ID                   string    `json:"id"`
	ClientID             string    `json:"client_id,omitempty"`
	Name                 string    `json:"name"`
	Direction            Direction `json:"direction"`
	Endpoint             string    `json:"endpoint_url,omitempty"`
	CredentialConfigured bool      `json:"credential_configured"`
	Enabled              bool      `json:"enabled"`
	EventTypes           []string  `json:"event_types"`
	RetryWindowSeconds   int64     `json:"retry_window_seconds"`
	Version              int64     `json:"version"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type ManagedDelivery struct {
	ConnectionID string        `json:"connection_id"`
	Connection   string        `json:"connection_name"`
	EventID      string        `json:"event_id"`
	EventType    string        `json:"event_type"`
	State        string        `json:"state"`
	AttemptCount int           `json:"attempt_count"`
	FirstAttempt time.Time     `json:"first_attempt_at"`
	NextAttempt  *time.Time    `json:"next_attempt_at,omitempty"`
	DeliveredAt  *time.Time    `json:"delivered_at,omitempty"`
	FailedAt     *time.Time    `json:"failed_at,omitempty"`
	ErrorCode    string        `json:"error_code,omitempty"`
	LastAttempt  *time.Time    `json:"last_attempt_at,omitempty"`
	LastStatus   int           `json:"last_http_status,omitempty"`
	LastState    DeliveryState `json:"last_attempt_state,omitempty"`
}

type ManagementRepository interface {
	ListManagedConnections(context.Context, scope.Target) ([]ManagedConnection, error)
	ListManagedDeliveries(context.Context, scope.Target, int) ([]ManagedDelivery, error)
	GetManagedConnection(context.Context, scope.Target, string) (ManagedConnection, error)
	CreateManagedConnection(context.Context, ConnectionMutation) error
	UpdateManagedConnection(context.Context, ConnectionMutation) error
	ReplaceManagedCredential(context.Context, ConnectionMutation) error
	RetryManagedDelivery(context.Context, DeliveryRetryMutation) error
}

type ManagementService struct {
	repository ManagementRepository
	now        func() time.Time
	newID      func() string
}

func NewManagementService(
	repository ManagementRepository,
	now ...func() time.Time,
) *ManagementService {
	clock := time.Now
	if len(now) > 0 && now[0] != nil {
		clock = now[0]
	}
	return &ManagementService{repository: repository, now: clock}
}

func (s *ManagementService) WithIDGenerator(newID func() string) *ManagementService {
	s.newID = newID
	return s
}

type CreateConnectionCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	Name            string
	Direction       Direction
	Endpoint        string
	EventTypes      []string
	RetryWindow     time.Duration
	PlaintextSecret []byte
	Reason          string
}

type UpdateConnectionCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	ID              string
	ExpectedVersion int64
	Name            string
	Endpoint        string
	EventTypes      []string
	RetryWindow     time.Duration
	Enabled         *bool
	Reason          string
}

type ReplaceCredentialCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	ID              string
	ExpectedVersion int64
	PlaintextSecret []byte
	Reason          string
}

type RetryDeliveryCommand struct {
	Principal    authorization.Principal
	Target       scope.Target
	ConnectionID string
	EventID      string
	Reason       string
}

type ConnectionMutation struct {
	Connection      ManagedConnection
	ExpectedVersion int64
	PlaintextSecret []byte `json:"-"`
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type DeliveryRetryMutation struct {
	ConnectionID string
	EventID      string
	RetriedAt    time.Time
	Audit        mutation.AuditRecord
	Event        mutation.EventRecord
}

func (s *ManagementService) ListConnections(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
) ([]ManagedConnection, error) {
	if s == nil || s.repository == nil {
		return nil, ErrManagementUnavailable
	}
	target = webhookTarget(principal, target)
	if err := authorization.Authorize(principal, "integration.manage", target); err != nil {
		return nil, err
	}
	return s.repository.ListManagedConnections(ctx, target)
}

func (s *ManagementService) ListDeliveries(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
) ([]ManagedDelivery, error) {
	if s == nil || s.repository == nil {
		return nil, ErrManagementUnavailable
	}
	target = webhookTarget(principal, target)
	if err := authorization.Authorize(principal, "integration.read", target); err != nil {
		return nil, err
	}
	return s.repository.ListManagedDeliveries(ctx, target, 200)
}

func (s *ManagementService) Create(
	ctx context.Context,
	command CreateConnectionCommand,
) (ManagedConnection, error) {
	target := webhookTarget(command.Principal, command.Target)
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil ||
		!validConnection(command.Name, command.Direction, command.Endpoint, command.EventTypes, command.RetryWindow) ||
		len(command.PlaintextSecret) < 32 || strings.TrimSpace(command.Reason) == "" {
		return ManagedConnection{}, ErrInvalidManagement
	}
	if err := authorization.Authorize(command.Principal, "integration.manage", target); err != nil {
		return ManagedConnection{}, err
	}
	now := s.now().UTC()
	connection := ManagedConnection{
		ID: s.newID(), ClientID: target.ClientID, Name: strings.TrimSpace(command.Name),
		Direction: command.Direction, Endpoint: strings.TrimSpace(command.Endpoint),
		CredentialConfigured: true, Enabled: true,
		EventTypes:         normalizedStrings(command.EventTypes),
		RetryWindowSeconds: int64(command.RetryWindow / time.Second),
		Version:            1, UpdatedAt: now,
	}
	accepted := s.mutation(command.Principal, target, connection, 0, command.PlaintextSecret, "webhook.connection.created", command.Reason, now)
	if err := s.repository.CreateManagedConnection(ctx, accepted); err != nil {
		return ManagedConnection{}, err
	}
	return connection, nil
}

func (s *ManagementService) Update(
	ctx context.Context,
	command UpdateConnectionCommand,
) (ManagedConnection, error) {
	target := webhookTarget(command.Principal, command.Target)
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil ||
		strings.TrimSpace(command.ID) == "" || command.ExpectedVersion < 1 ||
		strings.TrimSpace(command.Reason) == "" {
		return ManagedConnection{}, ErrInvalidManagement
	}
	if err := authorization.Authorize(command.Principal, "integration.manage", target); err != nil {
		return ManagedConnection{}, err
	}
	current, err := s.repository.GetManagedConnection(ctx, target, command.ID)
	if err != nil {
		return ManagedConnection{}, err
	}
	if current.Version != command.ExpectedVersion {
		return ManagedConnection{}, object.ErrVersionConflict
	}
	if command.Enabled != nil {
		current.Enabled = *command.Enabled
	} else {
		if !validConnection(command.Name, current.Direction, command.Endpoint, command.EventTypes, command.RetryWindow) {
			return ManagedConnection{}, ErrInvalidManagement
		}
		current.Name = strings.TrimSpace(command.Name)
		current.Endpoint = strings.TrimSpace(command.Endpoint)
		current.EventTypes = normalizedStrings(command.EventTypes)
		current.RetryWindowSeconds = int64(command.RetryWindow / time.Second)
	}
	now := s.now().UTC()
	current.Version++
	current.UpdatedAt = now
	accepted := s.mutation(command.Principal, target, current, command.ExpectedVersion, nil, "webhook.connection.updated", command.Reason, now)
	if err := s.repository.UpdateManagedConnection(ctx, accepted); err != nil {
		return ManagedConnection{}, err
	}
	return current, nil
}

func (s *ManagementService) ReplaceCredential(
	ctx context.Context,
	command ReplaceCredentialCommand,
) (ManagedConnection, error) {
	target := webhookTarget(command.Principal, command.Target)
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil ||
		command.ExpectedVersion < 1 || len(command.PlaintextSecret) < 32 ||
		strings.TrimSpace(command.Reason) == "" {
		return ManagedConnection{}, ErrInvalidManagement
	}
	if err := authorization.Authorize(command.Principal, "integration.manage", target); err != nil {
		return ManagedConnection{}, err
	}
	current, err := s.repository.GetManagedConnection(ctx, target, command.ID)
	if err != nil {
		return ManagedConnection{}, err
	}
	if current.Version != command.ExpectedVersion {
		return ManagedConnection{}, object.ErrVersionConflict
	}
	now := s.now().UTC()
	current.CredentialConfigured = true
	current.Version++
	current.UpdatedAt = now
	accepted := s.mutation(command.Principal, target, current, command.ExpectedVersion, command.PlaintextSecret, "webhook.connection.credential_replaced", command.Reason, now)
	if err := s.repository.ReplaceManagedCredential(ctx, accepted); err != nil {
		return ManagedConnection{}, err
	}
	return current, nil
}

func (s *ManagementService) RetryDelivery(
	ctx context.Context,
	command RetryDeliveryCommand,
) error {
	target := webhookTarget(command.Principal, command.Target)
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil ||
		strings.TrimSpace(command.ConnectionID) == "" ||
		strings.TrimSpace(command.EventID) == "" ||
		strings.TrimSpace(command.Reason) == "" {
		return ErrInvalidManagement
	}
	if err := authorization.Authorize(command.Principal, "integration.manage", target); err != nil {
		return err
	}
	now := s.now().UTC()
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	return s.repository.RetryManagedDelivery(ctx, DeliveryRetryMutation{
		ConnectionID: command.ConnectionID, EventID: command.EventID, RetriedAt: now,
		Audit: mutation.AuditRecord{ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID, ActorType: "technician", ActorID: command.Principal.ID, Action: "webhook.delivery.retried", SubjectType: "webhook_delivery", SubjectID: command.EventID, SubjectVersion: 1, Source: "api", Reason: strings.TrimSpace(command.Reason), CorrelationID: correlationID},
		Event: mutation.EventRecord{EventID: eventID, EventType: "webhook.delivery.retried", SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID, ActorType: "technician", ActorID: command.Principal.ID, SubjectType: "webhook_delivery", SubjectID: command.EventID, SubjectVersion: 1, Source: "api", CorrelationID: correlationID},
	})
}

func (s *ManagementService) mutation(
	principal authorization.Principal,
	target scope.Target,
	connection ManagedConnection,
	expected int64,
	secret []byte,
	action string,
	reason string,
	now time.Time,
) ConnectionMutation {
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	return ConnectionMutation{
		Connection: connection, ExpectedVersion: expected, PlaintextSecret: secret,
		Audit: mutation.AuditRecord{ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID, ActorType: "technician", ActorID: principal.ID, Action: action, SubjectType: "webhook_connection", SubjectID: connection.ID, SubjectVersion: connection.Version, Source: "api", Reason: strings.TrimSpace(reason), CorrelationID: correlationID},
		Event: mutation.EventRecord{EventID: eventID, EventType: action, SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID, ActorType: "technician", ActorID: principal.ID, SubjectType: "webhook_connection", SubjectID: connection.ID, SubjectVersion: connection.Version, Source: "api", CorrelationID: correlationID},
	}
}

func validConnection(name string, direction Direction, endpoint string, eventTypes []string, retry time.Duration) bool {
	if strings.TrimSpace(name) == "" || retry <= 0 || retry > 24*time.Hour {
		return false
	}
	if direction != DirectionInbound && direction != DirectionOutbound && direction != DirectionBidirectional {
		return false
	}
	if direction != DirectionInbound && ValidateDestination(strings.TrimSpace(endpoint)) != nil {
		return false
	}
	return direction == DirectionInbound || len(normalizedStrings(eventTypes)) > 0
}

func normalizedStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			if _, ok := seen[value]; !ok {
				seen[value] = struct{}{}
				result = append(result, value)
			}
		}
	}
	return result
}

func webhookTarget(principal authorization.Principal, target scope.Target) scope.Target {
	if target.MSPID == "" {
		return scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID}
	}
	return target
}

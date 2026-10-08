package intake

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidForwardingManagement = errors.New("invalid forwarding management request")

type ManagedForwardingConnection struct {
	ID                   string     `json:"id"`
	IntakeAddress        string     `json:"intake_address"`
	AllowedSenderDomains []string   `json:"allowed_sender_domains"`
	MaxMessageBytes      int64      `json:"max_message_bytes"`
	RateLimitPerMinute   int        `json:"rate_limit_per_minute"`
	Enabled              bool       `json:"enabled"`
	HealthState          string     `json:"health_state"`
	LastReceivedAt       *time.Time `json:"last_received_at,omitempty"`
	LastErrorCode        string     `json:"last_error_code,omitempty"`
	Version              int64      `json:"version"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type ForwardingManagementMutation struct {
	Connection      ManagedForwardingConnection
	MSPID           string
	ExpectedVersion int64
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type ForwardingManagementRepository interface {
	ListManagedForwarding(context.Context, string) ([]ManagedForwardingConnection, error)
	GetManagedForwarding(context.Context, string, string) (ManagedForwardingConnection, error)
	CreateManagedForwarding(context.Context, ForwardingManagementMutation) error
	UpdateManagedForwarding(context.Context, ForwardingManagementMutation) error
}

type ForwardingManagementService struct {
	repository ForwardingManagementRepository
	now        func() time.Time
	newID      func() string
}

func NewForwardingManagementService(
	repository ForwardingManagementRepository,
	now func() time.Time,
	newID func() string,
) *ForwardingManagementService {
	return &ForwardingManagementService{repository: repository, now: now, newID: newID}
}

type CreateForwardingConnectionCommand struct {
	Principal            authorization.Principal
	IntakeAddress        string
	AllowedSenderDomains []string
	MaxMessageBytes      int64
	RateLimitPerMinute   int
	Reason               string
}

type UpdateForwardingConnectionCommand struct {
	Principal            authorization.Principal
	ID                   string
	ExpectedVersion      int64
	IntakeAddress        string
	AllowedSenderDomains []string
	MaxMessageBytes      int64
	RateLimitPerMinute   int
	Enabled              *bool
	Reason               string
}

func (s *ForwardingManagementService) List(
	ctx context.Context,
	principal authorization.Principal,
) ([]ManagedForwardingConnection, error) {
	target := scope.Target{MSPID: principal.Scope.MSPID}
	if s == nil || s.repository == nil || target.MSPID == "" {
		return nil, ErrInvalidForwardingManagement
	}
	if err := authorization.Authorize(principal, "integration.manage", target); err != nil {
		return nil, err
	}
	return s.repository.ListManagedForwarding(ctx, target.MSPID)
}

func (s *ForwardingManagementService) Create(
	ctx context.Context,
	command CreateForwardingConnectionCommand,
) (ManagedForwardingConnection, error) {
	target := scope.Target{MSPID: command.Principal.Scope.MSPID}
	domains := forwardingDomains(command.AllowedSenderDomains)
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil ||
		target.MSPID == "" || !validForwardingAddress(command.IntakeAddress) ||
		len(domains) == 0 || command.MaxMessageBytes < 1 ||
		command.MaxMessageBytes > maxForwardingRawMIMEBytes ||
		command.RateLimitPerMinute < 1 || command.RateLimitPerMinute > 10000 ||
		strings.TrimSpace(command.Reason) == "" {
		return ManagedForwardingConnection{}, ErrInvalidForwardingManagement
	}
	if err := authorization.Authorize(command.Principal, "integration.manage", target); err != nil {
		return ManagedForwardingConnection{}, err
	}
	now := s.now().UTC()
	connection := ManagedForwardingConnection{
		ID: s.newID(), IntakeAddress: strings.ToLower(strings.TrimSpace(command.IntakeAddress)),
		AllowedSenderDomains: domains, MaxMessageBytes: command.MaxMessageBytes,
		RateLimitPerMinute: command.RateLimitPerMinute, Enabled: true,
		HealthState: "pending", Version: 1, UpdatedAt: now,
	}
	accepted := s.forwardingMutation(command.Principal, connection, 0, "forwarding.connection.created", command.Reason, now)
	if err := s.repository.CreateManagedForwarding(ctx, accepted); err != nil {
		return ManagedForwardingConnection{}, err
	}
	return connection, nil
}

func (s *ForwardingManagementService) Update(
	ctx context.Context,
	command UpdateForwardingConnectionCommand,
) (ManagedForwardingConnection, error) {
	target := scope.Target{MSPID: command.Principal.Scope.MSPID}
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil ||
		target.MSPID == "" || strings.TrimSpace(command.ID) == "" ||
		command.ExpectedVersion < 1 || strings.TrimSpace(command.Reason) == "" {
		return ManagedForwardingConnection{}, ErrInvalidForwardingManagement
	}
	if err := authorization.Authorize(command.Principal, "integration.manage", target); err != nil {
		return ManagedForwardingConnection{}, err
	}
	current, err := s.repository.GetManagedForwarding(ctx, target.MSPID, command.ID)
	if err != nil {
		return ManagedForwardingConnection{}, err
	}
	if current.Version != command.ExpectedVersion {
		return ManagedForwardingConnection{}, object.ErrVersionConflict
	}
	if command.Enabled != nil {
		current.Enabled = *command.Enabled
		if current.Enabled {
			current.HealthState = "pending"
		} else {
			current.HealthState = "disabled"
		}
	} else {
		domains := forwardingDomains(command.AllowedSenderDomains)
		if !validForwardingAddress(command.IntakeAddress) || len(domains) == 0 ||
			command.MaxMessageBytes < 1 || command.MaxMessageBytes > maxForwardingRawMIMEBytes ||
			command.RateLimitPerMinute < 1 || command.RateLimitPerMinute > 10000 {
			return ManagedForwardingConnection{}, ErrInvalidForwardingManagement
		}
		current.IntakeAddress = strings.ToLower(strings.TrimSpace(command.IntakeAddress))
		current.AllowedSenderDomains = domains
		current.MaxMessageBytes = command.MaxMessageBytes
		current.RateLimitPerMinute = command.RateLimitPerMinute
	}
	now := s.now().UTC()
	current.Version++
	current.UpdatedAt = now
	accepted := s.forwardingMutation(command.Principal, current, command.ExpectedVersion, "forwarding.connection.updated", command.Reason, now)
	if err := s.repository.UpdateManagedForwarding(ctx, accepted); err != nil {
		return ManagedForwardingConnection{}, err
	}
	return current, nil
}

func (s *ForwardingManagementService) forwardingMutation(
	principal authorization.Principal,
	connection ManagedForwardingConnection,
	expected int64,
	action string,
	reason string,
	now time.Time,
) ForwardingManagementMutation {
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	return ForwardingManagementMutation{
		Connection: connection, MSPID: principal.Scope.MSPID, ExpectedVersion: expected,
		Audit: mutation.AuditRecord{ID: auditID, OccurredAt: now, MSPID: principal.Scope.MSPID, ActorType: "technician", ActorID: principal.ID, Action: action, SubjectType: "forwarding_connection", SubjectID: connection.ID, SubjectVersion: connection.Version, Source: "api", Reason: strings.TrimSpace(reason), CorrelationID: correlationID},
		Event: mutation.EventRecord{EventID: eventID, EventType: action, SchemaVersion: 1, OccurredAt: now, MSPID: principal.Scope.MSPID, ActorType: "technician", ActorID: principal.ID, SubjectType: "forwarding_connection", SubjectID: connection.ID, SubjectVersion: connection.Version, Source: "api", CorrelationID: correlationID},
	}
}

func validForwardingAddress(value string) bool {
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	return err == nil && strings.EqualFold(address.Address, strings.TrimSpace(value))
}

func forwardingDomains(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "@")))
		if !validForwardingDomain(value) {
			continue
		}
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

func validForwardingDomain(value string) bool {
	if len(value) > 253 || !strings.Contains(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 ||
			strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, character := range label {
			if !((character >= 'a' && character <= 'z') ||
				(character >= '0' && character <= '9') || character == '-') {
				return false
			}
		}
	}
	return true
}

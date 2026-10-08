package intake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

const (
	maxForwardingExternalIDBytes = 512
	maxForwardingHeaderBytes     = 2048
	maxForwardingRawMIMEBytes    = 50 << 20
)

var ErrDuplicateForwardingEvent = errors.New("duplicate forwarding event")

type ForwardingConnection struct {
	ID                   string
	MSPID                string
	IntakeAddress        string
	AllowedSenderDomains []string
	MaxMessageBytes      int64
	RateLimitPerMinute   int
	Enabled              bool
	Version              int64
}

type ForwardingCommand struct {
	Principal    authorization.Principal
	ConnectionID string
	Message      ForwardedMessage
	RawMIME      []byte
	ActorID      string
	ActorType    string
	SourceName   string
}

type ForwardingMutation struct {
	Inbound           InboundEvent
	ConnectionVersion int64
	Audit             mutation.AuditRecord
	Event             mutation.EventRecord
}

type ForwardingRepository interface {
	LoadForwardingConnection(context.Context, scope.Target, string) (ForwardingConnection, error)
	FindForwardingEvent(context.Context, scope.Target, string, string) (InboundEvent, error)
	AllowForwarding(context.Context, string, string, int, time.Time) (bool, error)
	CreateForwardingAtomic(context.Context, ForwardingMutation) error
}

type ForwardingRawStore interface {
	Put(context.Context, string, []byte) error
	Delete(context.Context, string) error
}

type ForwardingService struct {
	repository ForwardingRepository
	raw        ForwardingRawStore
	now        func() time.Time
	newID      func() string
}

func NewForwardingService(
	repository ForwardingRepository,
	raw ForwardingRawStore,
	now func() time.Time,
	newID func() string,
) *ForwardingService {
	return &ForwardingService{
		repository: repository, raw: raw, now: now, newID: newID,
	}
}

func (s *ForwardingService) Accept(
	ctx context.Context,
	command ForwardingCommand,
) (ForwardingResult, error) {
	target := scope.Target{MSPID: command.Principal.Scope.MSPID}
	if err := authorization.Authorize(
		command.Principal, "intake.forwarding.write", target,
	); err != nil {
		return ForwardingResult{}, err
	}
	connectionID := strings.TrimSpace(command.ConnectionID)
	if s.repository == nil || s.raw == nil || s.now == nil || s.newID == nil ||
		target.MSPID == "" || connectionID == "" || len(command.RawMIME) == 0 ||
		len(command.RawMIME) > maxForwardingRawMIMEBytes ||
		strings.TrimSpace(command.ActorID) == "" ||
		strings.TrimSpace(command.ActorType) == "" ||
		strings.TrimSpace(command.SourceName) == "" {
		return ForwardingResult{}, ErrInvalidForwardingConfiguration
	}
	connection, err := s.repository.LoadForwardingConnection(
		ctx, target, connectionID,
	)
	if err != nil {
		return ForwardingResult{}, err
	}
	if connection.ID != connectionID || connection.MSPID != target.MSPID ||
		!connection.Enabled || strings.TrimSpace(connection.IntakeAddress) == "" ||
		len(connection.AllowedSenderDomains) == 0 ||
		connection.MaxMessageBytes <= 0 || connection.RateLimitPerMinute <= 0 {
		return ForwardingResult{}, scope.ErrNotFound
	}
	message := command.Message
	message.SizeBytes = int64(len(command.RawMIME))
	externalID := forwardingExternalID(message.MessageID, command.RawMIME)
	existing, err := s.repository.FindForwardingEvent(
		ctx, target, connection.ID, externalID,
	)
	if err == nil {
		if existing.MSPID != target.MSPID ||
			existing.ForwardingConnectionID != connection.ID ||
			existing.Source != SourceForwardedEmail ||
			existing.ExternalID != externalID {
			return ForwardingResult{}, scope.ErrNotFound
		}
		return ForwardingResult{
			EventID: existing.ID, State: existing.ProcessingState,
			QuarantineReason: existing.QuarantineReason,
		}, nil
	}
	if !errors.Is(err, scope.ErrNotFound) {
		return ForwardingResult{}, err
	}
	now := s.now().UTC()
	inboundID := s.newID()
	rawRef := forwardingPayloadRef(connection, externalID, inboundID)
	if err := s.raw.Put(ctx, rawRef, command.RawMIME); err != nil {
		return ForwardingResult{}, err
	}
	message.RawMIMERef = rawRef
	allowed, err := s.repository.AllowForwarding(
		ctx, connection.ID, forwardingSenderKey(message),
		connection.RateLimitPerMinute, now,
	)
	if err != nil {
		_ = s.raw.Delete(ctx, rawRef)
		return ForwardingResult{}, err
	}
	policy := ForwardingPolicy{
		ConnectionID: connection.ID, MSPID: connection.MSPID,
		IntakeAddress:        connection.IntakeAddress,
		AllowedSenderDomains: connection.AllowedSenderDomains,
		MaxMessageBytes:      connection.MaxMessageBytes,
	}
	reason := forwardingQuarantineReason(policy, message)
	if !allowed {
		reason = QuarantineRateLimit
	}
	state, authentication := StateReceived, AuthenticationPassed
	if reason != QuarantineNone {
		state = StateQuarantined
		if reason == QuarantineAuthentication || reason == QuarantineSender {
			authentication = AuthenticationFailed
		}
	}
	inbound := InboundEvent{
		ID: inboundID, MSPID: connection.MSPID,
		ForwardingConnectionID: connection.ID,
		Source:                 SourceForwardedEmail, ExternalID: externalID,
		ReceivedAt: now, AuthenticationResult: authentication,
		RawPayloadRef: rawRef, ProcessingState: state,
		QuarantineReason: reason,
		Sender:           forwardingMetadata(message.From),
		Recipient:        forwardingMetadata(message.To),
	}
	action := "intake.forwarding.received"
	if state == StateQuarantined {
		action = "intake.forwarding.quarantined"
	}
	auditID, outboxID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := ForwardingMutation{
		Inbound: inbound, ConnectionVersion: connection.Version,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: connection.MSPID,
			ActorType: command.ActorType, ActorID: command.ActorID,
			Action: action, SubjectType: "inbound_event",
			SubjectID: inbound.ID, SubjectVersion: 1,
			Source: command.SourceName, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: outboxID, EventType: action, SchemaVersion: 1,
			OccurredAt: now, MSPID: connection.MSPID,
			ActorType: command.ActorType, ActorID: command.ActorID,
			SubjectType: "inbound_event", SubjectID: inbound.ID,
			SubjectVersion: 1, Source: command.SourceName,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.CreateForwardingAtomic(ctx, accepted); err != nil {
		_ = s.raw.Delete(ctx, rawRef)
		if errors.Is(err, ErrDuplicateForwardingEvent) {
			winner, findErr := s.repository.FindForwardingEvent(
				ctx, target, connection.ID, externalID,
			)
			if findErr != nil {
				return ForwardingResult{}, findErr
			}
			return ForwardingResult{
				EventID: winner.ID, State: winner.ProcessingState,
				QuarantineReason: winner.QuarantineReason,
			}, nil
		}
		return ForwardingResult{}, err
	}
	return ForwardingResult{
		EventID: inbound.ID, State: state, QuarantineReason: reason,
	}, nil
}

func forwardingExternalID(messageID string, raw []byte) string {
	trimmed := strings.TrimSpace(messageID)
	if trimmed != "" && len(trimmed) <= maxForwardingExternalIDBytes {
		return trimmed
	}
	hash := sha256.Sum256(append([]byte(trimmed+"\n"), raw...))
	return "invalid:" + hex.EncodeToString(hash[:])
}

func forwardingPayloadRef(
	connection ForwardingConnection,
	externalID string,
	inboundID string,
) string {
	hash := sha256.Sum256([]byte(externalID))
	return "intake/" + connection.MSPID + "/forwarded_email/" +
		connection.ID + "/" + hex.EncodeToString(hash[:]) + "/" +
		inboundID + ".eml"
}

func forwardingSenderKey(message ForwardedMessage) string {
	value := strings.ToLower(strings.TrimSpace(message.EnvelopeFrom))
	if value == "" {
		return "_invalid"
	}
	if len(value) > maxForwardingHeaderBytes {
		hash := sha256.Sum256([]byte(value))
		return "invalid:" + hex.EncodeToString(hash[:])
	}
	return value
}

func forwardingMetadata(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if len(normalized) <= maxForwardingHeaderBytes {
		return normalized
	}
	hash := sha256.Sum256([]byte(normalized))
	return "invalid:" + hex.EncodeToString(hash[:])
}

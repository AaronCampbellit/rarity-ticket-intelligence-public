package webhooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/intake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

const (
	maxInboundEventIDBytes = 512
	maxInboundPayloadBytes = 1 << 20
	inboundMaxSkew         = 5 * time.Minute
)

type Direction string

const (
	DirectionInbound       Direction = "inbound"
	DirectionOutbound      Direction = "outbound"
	DirectionBidirectional Direction = "bidirectional"
)

type InboundConnection struct {
	ID        string
	MSPID     string
	ClientID  string
	SecretRef string
	Direction Direction
	Enabled   bool
	Version   int64
}

type InboundCommand struct {
	ConnectionID string
	Timestamp    string
	EventID      string
	Signature    string
	Body         []byte
	SourceName   string
}

type InboundMutation struct {
	ConnectionID      string
	ConnectionVersion int64
	ReplayExpiresAt   time.Time
	System            intake.SystemMutation
}

type InboundRepository interface {
	LoadInboundConnection(context.Context, string) (InboundConnection, error)
	CreateInboundAtomic(context.Context, InboundMutation) error
}

type InboundSecretResolver interface {
	Resolve(context.Context, string) ([]byte, error)
}

type InboundRawStore interface {
	Put(context.Context, string, []byte) error
	Delete(context.Context, string) error
}

type InboundService struct {
	repository InboundRepository
	secrets    InboundSecretResolver
	raw        InboundRawStore
	now        func() time.Time
	newID      func() string
}

func NewInboundService(
	repository InboundRepository,
	secrets InboundSecretResolver,
	raw InboundRawStore,
	now func() time.Time,
	newID func() string,
) *InboundService {
	return &InboundService{
		repository: repository, secrets: secrets, raw: raw, now: now, newID: newID,
	}
}

func (s *InboundService) Accept(
	ctx context.Context,
	command InboundCommand,
) (intake.InboundEvent, error) {
	connectionID := strings.TrimSpace(command.ConnectionID)
	eventID := strings.TrimSpace(command.EventID)
	if s.repository == nil || s.secrets == nil || s.raw == nil ||
		s.now == nil || s.newID == nil || connectionID == "" ||
		eventID == "" || len(eventID) > maxInboundEventIDBytes ||
		len(command.Body) == 0 || len(command.Body) > maxInboundPayloadBytes ||
		strings.TrimSpace(command.SourceName) == "" {
		return intake.InboundEvent{}, intake.ErrInvalidSystemIntake
	}
	connection, err := s.repository.LoadInboundConnection(ctx, connectionID)
	if err != nil {
		return intake.InboundEvent{}, err
	}
	if connection.ID != connectionID || strings.TrimSpace(connection.MSPID) == "" ||
		strings.TrimSpace(connection.ClientID) == "" ||
		strings.TrimSpace(connection.SecretRef) == "" || !connection.Enabled ||
		(connection.Direction != DirectionInbound &&
			connection.Direction != DirectionBidirectional) {
		return intake.InboundEvent{}, scope.ErrNotFound
	}
	secret, err := s.secrets.Resolve(ctx, connection.SecretRef)
	if err != nil {
		return intake.InboundEvent{}, ErrInvalidSignature
	}
	now := s.now().UTC()
	if _, err := Authenticate(VerifyCommand{
		Secret: secret, Timestamp: command.Timestamp, EventID: eventID,
		Signature: command.Signature, Body: command.Body, Now: now,
		MaxSkew: inboundMaxSkew,
	}); err != nil {
		return intake.InboundEvent{}, err
	}
	inboundID := s.newID()
	rawRef := inboundPayloadRef(connection, eventID, inboundID)
	inbound, err := intake.NormalizeSystem(intake.SystemInput{
		ID: inboundID, MSPID: connection.MSPID, ClientID: connection.ClientID,
		ConnectionID: connection.ID, Source: intake.SourceInboundWebhook,
		ExternalID:    eventID,
		RawPayloadRef: rawRef, Authenticated: true, ReceivedAt: now,
	})
	if err != nil {
		return intake.InboundEvent{}, err
	}
	if err := s.raw.Put(ctx, rawRef, command.Body); err != nil {
		return intake.InboundEvent{}, err
	}
	auditID, outboxID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := InboundMutation{
		ConnectionID: connection.ID, ConnectionVersion: connection.Version,
		ReplayExpiresAt: now.Add(inboundMaxSkew),
		System: intake.SystemMutation{
			Inbound: inbound,
			Audit: mutation.AuditRecord{
				ID: auditID, OccurredAt: now, MSPID: connection.MSPID,
				ClientID: connection.ClientID, ActorType: "webhook_connection",
				ActorID: connection.ID, Action: "intake.webhook.received",
				SubjectType: "inbound_event", SubjectID: inbound.ID,
				SubjectVersion: 1, Source: command.SourceName,
				CorrelationID: correlationID,
			},
			Event: mutation.EventRecord{
				EventID: outboxID, EventType: "intake.webhook.received",
				SchemaVersion: 1, OccurredAt: now, MSPID: connection.MSPID,
				ClientID: connection.ClientID, ActorType: "webhook_connection",
				ActorID: connection.ID, SubjectType: "inbound_event",
				SubjectID: inbound.ID, SubjectVersion: 1,
				Source: command.SourceName, CorrelationID: correlationID,
			},
		},
	}
	if err := s.repository.CreateInboundAtomic(ctx, accepted); err != nil {
		_ = s.raw.Delete(ctx, rawRef)
		return intake.InboundEvent{}, err
	}
	return inbound, nil
}

func inboundPayloadRef(
	connection InboundConnection,
	eventID string,
	inboundID string,
) string {
	hash := sha256.Sum256([]byte(eventID))
	return "intake/" + connection.MSPID + "/" + connection.ClientID +
		"/inbound_webhook/" + connection.ID + "/" +
		hex.EncodeToString(hash[:]) + "/" + inboundID + ".json"
}

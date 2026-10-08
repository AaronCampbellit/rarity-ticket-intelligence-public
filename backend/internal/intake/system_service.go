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
	maxSystemExternalIDBytes = 512
	maxSystemPayloadBytes    = 1 << 20
)

var ErrDuplicateSystemEvent = errors.New("duplicate system intake event")

type SystemCommand struct {
	Principal  authorization.Principal
	Target     scope.Target
	ExternalID string
	Payload    []byte
	ActorID    string
	ActorType  string
	SourceName string
}

type SystemMutation struct {
	Inbound InboundEvent
	Audit   mutation.AuditRecord
	Event   mutation.EventRecord
}

type SystemRepository interface {
	FindSystemEvent(context.Context, scope.Target, Source, string) (InboundEvent, error)
	CreateSystemEventAtomic(context.Context, SystemMutation) error
}

type RawPayloadStore interface {
	Put(context.Context, string, []byte) error
	Delete(context.Context, string) error
}

type SystemService struct {
	repository SystemRepository
	store      RawPayloadStore
	now        func() time.Time
	newID      func() string
}

func NewSystemService(
	repository SystemRepository,
	store RawPayloadStore,
	now func() time.Time,
	newID func() string,
) *SystemService {
	return &SystemService{
		repository: repository, store: store, now: now, newID: newID,
	}
}

func (s *SystemService) Accept(
	ctx context.Context,
	command SystemCommand,
) (InboundEvent, error) {
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID:    command.Principal.Scope.MSPID,
			ClientID: command.Principal.Scope.ClientID,
		}
	}
	if err := authorization.Authorize(
		command.Principal, "intake.write", target,
	); err != nil {
		return InboundEvent{}, err
	}
	externalID := strings.TrimSpace(command.ExternalID)
	if s.repository == nil || s.store == nil || s.now == nil || s.newID == nil ||
		target.MSPID == "" || target.ClientID == "" || externalID == "" ||
		len(externalID) > maxSystemExternalIDBytes ||
		len(command.Payload) == 0 || len(command.Payload) > maxSystemPayloadBytes ||
		command.ActorID == "" || strings.TrimSpace(command.ActorType) == "" ||
		strings.TrimSpace(command.SourceName) == "" {
		return InboundEvent{}, ErrInvalidSystemIntake
	}
	existing, err := s.repository.FindSystemEvent(
		ctx, target, SourceDirectAPI, externalID,
	)
	if err == nil {
		if existing.MSPID != target.MSPID || existing.ClientID != target.ClientID ||
			existing.Source != SourceDirectAPI || existing.ExternalID != externalID {
			return InboundEvent{}, scope.ErrNotFound
		}
		return existing, nil
	}
	if !errors.Is(err, scope.ErrNotFound) {
		return InboundEvent{}, err
	}
	now := s.now().UTC()
	eventID := s.newID()
	rawRef := systemPayloadRef(target, externalID, eventID)
	inbound, err := NormalizeSystem(SystemInput{
		ID: eventID, MSPID: target.MSPID, ClientID: target.ClientID,
		Source: SourceDirectAPI, ExternalID: externalID,
		RawPayloadRef: rawRef, Authenticated: true, ReceivedAt: now,
	})
	if err != nil {
		return InboundEvent{}, err
	}
	if err := s.store.Put(ctx, rawRef, command.Payload); err != nil {
		return InboundEvent{}, err
	}
	auditID, outboxID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := SystemMutation{
		Inbound: inbound,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: command.ActorType,
			ActorID: command.ActorID, Action: "intake.direct.received",
			SubjectType: "inbound_event", SubjectID: inbound.ID,
			SubjectVersion: 1, Source: command.SourceName,
			CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: outboxID, EventType: "intake.direct.received",
			SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: command.ActorType,
			ActorID: command.ActorID, SubjectType: "inbound_event",
			SubjectID: inbound.ID, SubjectVersion: 1,
			Source: command.SourceName, CorrelationID: correlationID,
		},
	}
	if err := s.repository.CreateSystemEventAtomic(ctx, accepted); err != nil {
		_ = s.store.Delete(ctx, rawRef)
		if errors.Is(err, ErrDuplicateSystemEvent) {
			winner, findErr := s.repository.FindSystemEvent(
				ctx, target, SourceDirectAPI, externalID,
			)
			if findErr != nil {
				return InboundEvent{}, findErr
			}
			if winner.MSPID != target.MSPID || winner.ClientID != target.ClientID ||
				winner.Source != SourceDirectAPI || winner.ExternalID != externalID {
				return InboundEvent{}, scope.ErrNotFound
			}
			return winner, nil
		}
		return InboundEvent{}, err
	}
	return inbound, nil
}

func systemPayloadRef(target scope.Target, externalID, eventID string) string {
	hash := sha256.Sum256([]byte(externalID))
	return "intake/" + target.MSPID + "/" + target.ClientID +
		"/direct_api/" + hex.EncodeToString(hash[:]) + "/" + eventID + ".json"
}

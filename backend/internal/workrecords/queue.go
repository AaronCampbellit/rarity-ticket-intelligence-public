package workrecords

import (
	"context"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type QueueRef struct {
	ID       string
	MSPID    string
	ClientID string
	Key      string
	Name     string
	Version  int64
}

type QueueCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	WorkRecordID    string
	ExpectedVersion int64
	Queue           QueueRef
	Actor           Actor
	Reason          string
	CorrelationID   string
}

type QueuePreflightCommand struct {
	Principal           authorization.Principal
	Target              scope.Target
	WorkRecordReference string
	QueueReference      string
	ExpectedVersion     int64
}

type QueuePreflight struct {
	Record       Record
	CurrentQueue QueueRef
	Queue        QueueRef
}

type QueueMutation struct {
	Record          Record
	Queue           QueueRef
	PreviousQueueID string
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type QueueRepository interface {
	FindForQueue(context.Context, scope.Target, string) (Record, error)
	ResolveWorkRecordForQueue(context.Context, scope.Target, string, int) ([]Record, error)
	ResolveQueueForRoute(context.Context, scope.Target, string, int) ([]QueueRef, error)
	FindQueueForRoute(context.Context, scope.Target, string) (QueueRef, error)
	TransferQueueAtomic(context.Context, QueueMutation) error
}

type QueueService struct {
	repository QueueRepository
	now        func() time.Time
	newID      func() string
}

func NewQueueService(repository QueueRepository, now func() time.Time, newID func() string) *QueueService {
	return &QueueService{repository: repository, now: now, newID: newID}
}

func (s *QueueService) Preflight(
	ctx context.Context,
	command QueuePreflightCommand,
) (QueuePreflight, error) {
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID:    command.Principal.Scope.MSPID,
			ClientID: command.Principal.Scope.ClientID,
		}
	}
	workReference := strings.TrimSpace(command.WorkRecordReference)
	queueReference := strings.TrimSpace(command.QueueReference)
	if target.MSPID == "" || target.ClientID == "" || workReference == "" ||
		queueReference == "" || command.ExpectedVersion < 1 || s.repository == nil {
		return QueuePreflight{}, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, "work_record.route", target); err != nil {
		return QueuePreflight{}, err
	}
	records, err := s.repository.ResolveWorkRecordForQueue(ctx, target, workReference, 2)
	if err != nil {
		return QueuePreflight{}, err
	}
	if len(records) != 1 || records[0].ID == "" ||
		records[0].MSPID != target.MSPID || records[0].ClientID != target.ClientID {
		return QueuePreflight{}, scope.ErrNotFound
	}
	if err := object.RequireVersion(records[0].Version, command.ExpectedVersion); err != nil {
		return QueuePreflight{}, err
	}
	queues, err := s.repository.ResolveQueueForRoute(ctx, target, queueReference, 2)
	if err != nil {
		return QueuePreflight{}, err
	}
	if len(queues) != 1 || queues[0].ID == "" || queues[0].MSPID != target.MSPID ||
		(queues[0].ClientID != "" && queues[0].ClientID != target.ClientID) {
		return QueuePreflight{}, scope.ErrNotFound
	}
	if records[0].QueueID == queues[0].ID {
		return QueuePreflight{}, ErrInvalid
	}
	var currentQueue QueueRef
	if records[0].QueueID != "" {
		currentQueue, err = s.repository.FindQueueForRoute(ctx, target, records[0].QueueID)
		if err != nil {
			return QueuePreflight{}, err
		}
		if !validCanonicalQueue(currentQueue, target) {
			return QueuePreflight{}, scope.ErrNotFound
		}
	}
	return QueuePreflight{Record: records[0], CurrentQueue: currentQueue, Queue: queues[0]}, nil
}

func (s *QueueService) Transfer(ctx context.Context, command QueueCommand) (Record, error) {
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		}
	}
	if target.ClientID == "" ||
		command.WorkRecordID == "" ||
		command.ExpectedVersion < 1 ||
		command.Queue.ID == "" ||
		command.Queue.MSPID != target.MSPID ||
		(command.Queue.ClientID != "" && command.Queue.ClientID != target.ClientID) ||
		command.Actor.Type == "" ||
		command.Actor.ID == "" ||
		command.Actor.Source == "" ||
		strings.TrimSpace(command.Reason) == "" {
		return Record{}, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, "work_record.route", target); err != nil {
		return Record{}, err
	}
	current, err := s.repository.FindForQueue(ctx, target, command.WorkRecordID)
	if err != nil {
		return Record{}, err
	}
	if current.MSPID != target.MSPID || current.ClientID != target.ClientID {
		return Record{}, scope.ErrNotFound
	}
	if err := object.RequireVersion(current.Version, command.ExpectedVersion); err != nil {
		return Record{}, err
	}
	canonicalQueue, err := s.repository.FindQueueForRoute(ctx, target, command.Queue.ID)
	if err != nil {
		return Record{}, err
	}
	if !validCanonicalQueue(canonicalQueue, target) {
		return Record{}, scope.ErrNotFound
	}
	if preparedQueueFacts(command.Queue) && !sameQueueFacts(command.Queue, canonicalQueue) {
		return Record{}, object.ErrVersionConflict
	}
	now := s.now().UTC()
	previousQueueID := current.QueueID
	current.QueueID = command.Queue.ID
	current.Version++
	current.UpdatedAt = now
	current.UpdatedBy = command.Actor.ID
	auditID := s.newID()
	eventID := s.newID()
	correlationID := strings.TrimSpace(command.CorrelationID)
	if correlationID == "" {
		correlationID = s.newID()
	}
	accepted := QueueMutation{
		Record: current, Queue: canonicalQueue, PreviousQueueID: previousQueueID,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			Action: "work_record.queue.changed", SubjectType: "work_record",
			SubjectID: current.ID, SubjectVersion: current.Version,
			Source: command.Actor.Source, Reason: strings.TrimSpace(command.Reason),
			CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "work_record.queue.changed", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			SubjectType: "work_record", SubjectID: current.ID,
			SubjectVersion: current.Version, Source: command.Actor.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.TransferQueueAtomic(ctx, accepted); err != nil {
		return Record{}, err
	}
	return current, nil
}

func validCanonicalQueue(queue QueueRef, target scope.Target) bool {
	return strings.TrimSpace(queue.ID) != "" &&
		queue.MSPID == target.MSPID &&
		(queue.ClientID == "" || queue.ClientID == target.ClientID) &&
		strings.TrimSpace(queue.Key) != "" &&
		strings.TrimSpace(queue.Name) != "" &&
		queue.Version > 0
}

func preparedQueueFacts(queue QueueRef) bool {
	return strings.TrimSpace(queue.Key) != "" ||
		strings.TrimSpace(queue.Name) != "" ||
		queue.Version > 0
}

func sameQueueFacts(prepared, current QueueRef) bool {
	return prepared.ID == current.ID &&
		prepared.MSPID == current.MSPID &&
		prepared.ClientID == current.ClientID &&
		prepared.Key == current.Key &&
		prepared.Name == current.Name &&
		prepared.Version == current.Version
}

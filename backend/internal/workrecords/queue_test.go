package workrecords

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type queueRepository struct {
	current  Record
	records  []Record
	queues   []QueueRef
	byID     map[string]QueueRef
	accepted QueueMutation
}

func (r *queueRepository) FindForQueue(_ context.Context, _ scope.Target, _ string) (Record, error) {
	return r.current, nil
}
func (r *queueRepository) TransferQueueAtomic(_ context.Context, mutation QueueMutation) error {
	r.accepted = mutation
	return nil
}
func (r *queueRepository) ResolveWorkRecordForQueue(_ context.Context, _ scope.Target, _ string, _ int) ([]Record, error) {
	return r.records, nil
}
func (r *queueRepository) ResolveQueueForRoute(_ context.Context, _ scope.Target, _ string, _ int) ([]QueueRef, error) {
	return r.queues, nil
}
func (r *queueRepository) FindQueueForRoute(_ context.Context, _ scope.Target, id string) (QueueRef, error) {
	queue, ok := r.byID[id]
	if !ok {
		return QueueRef{}, scope.ErrNotFound
	}
	return queue, nil
}

func TestTransferQueueAdvancesVersionAndRecordsExplainableDecision(t *testing.T) {
	repository := &queueRepository{current: Record{Envelope: object.Envelope{
		ID: "work-id", ObjectType: "work_record", MSPID: "msp-id", ClientID: "client-id",
		LifecycleState: "active", Version: 2, CreatedBy: "creator", UpdatedBy: "creator",
	}, QueueID: "triage"}, byID: map[string]QueueRef{
		"service-desk": {ID: "service-desk", MSPID: "msp-id", Key: "service-desk", Name: "Service Desk", Version: 2},
	}}
	ids := []string{"audit-id", "event-id", "correlation-id"}
	service := NewQueueService(repository, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("work_record.route"),
	}
	updated, err := service.Transfer(context.Background(), QueueCommand{
		Principal: principal, WorkRecordID: "work-id", ExpectedVersion: 2,
		Queue:  QueueRef{ID: "service-desk", MSPID: "msp-id"},
		Actor:  Actor{Type: "technician", ID: "actor-id", Source: "routing"},
		Reason: "matched routing rule client-default at position 2",
	})
	if err != nil {
		t.Fatalf("Transfer() error = %v", err)
	}
	if updated.QueueID != "service-desk" || updated.Version != 3 {
		t.Fatalf("unexpected transfer: %+v", updated)
	}
	if repository.accepted.Audit.Reason == "" ||
		repository.accepted.Event.EventType != "work_record.queue.changed" {
		t.Fatal("queue decision was not explainably audited")
	}
}

func TestTicketRoutePreflightResolvesExactTicketQueueAndVersion(t *testing.T) {
	record := Record{Envelope: object.Envelope{
		ID: "work-id", ObjectType: "work_record", MSPID: "msp-id", ClientID: "client-id",
		DisplayID: "INC-200", LifecycleState: "active", Version: 4,
	}, Title: "VPN unavailable", QueueID: "triage"}
	queue := QueueRef{ID: "service-desk", MSPID: "msp-id", Key: "service-desk", Name: "Service Desk", Version: 2}
	repository := &queueRepository{
		current: record, records: []Record{record}, queues: []QueueRef{queue},
		byID: map[string]QueueRef{
			"triage": {ID: "triage", MSPID: "msp-id", Key: "triage", Name: "Triage", Version: 7},
		},
	}
	service := NewQueueService(repository, time.Now, func() string { return "id" })
	principal := authorization.Principal{
		ID: "actor-id", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("work_record.route"),
	}

	preflight, err := service.Preflight(context.Background(), QueuePreflightCommand{
		Principal: principal, Target: scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		WorkRecordReference: "INC-200", QueueReference: "Service Desk", ExpectedVersion: 4,
	})
	if err != nil {
		t.Fatalf("Preflight() error=%v", err)
	}
	if preflight.Record.ID != "work-id" || preflight.Record.DisplayID != "INC-200" ||
		preflight.Queue.ID != "service-desk" || preflight.Queue.Name != "Service Desk" ||
		preflight.CurrentQueue.ID != "triage" || preflight.CurrentQueue.Name != "Triage" ||
		preflight.CurrentQueue.Version != 7 {
		t.Fatalf("preflight=%+v", preflight)
	}
}

func TestTicketRouteTransferUsesProposalCorrelationAtomically(t *testing.T) {
	repository := &queueRepository{current: Record{Envelope: object.Envelope{
		ID: "work-id", MSPID: "msp-id", ClientID: "client-id", Version: 4,
	}}, byID: map[string]QueueRef{
		"service-desk": {ID: "service-desk", MSPID: "msp-id", Key: "service-desk", Name: "Service Desk", Version: 2},
	}}
	service := NewQueueService(repository, time.Now, func() string { return "generated-id" })
	principal := authorization.Principal{
		ID: "actor-id", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("work_record.route"),
	}
	_, err := service.Transfer(context.Background(), QueueCommand{
		Principal: principal, WorkRecordID: "work-id", ExpectedVersion: 4,
		Queue:  QueueRef{ID: "service-desk", MSPID: "msp-id"},
		Actor:  Actor{Type: "technician", ID: "actor-id", Source: "ai_workspace"},
		Reason: "Escalate to the service desk", CorrelationID: "proposal-correlation",
	})
	if err != nil {
		t.Fatalf("Transfer() error=%v", err)
	}
	if repository.accepted.Audit.CorrelationID != "proposal-correlation" ||
		repository.accepted.Event.CorrelationID != "proposal-correlation" ||
		repository.accepted.Audit.Source != "ai_workspace" ||
		repository.accepted.Queue.Key != "service-desk" ||
		repository.accepted.Queue.Name != "Service Desk" ||
		repository.accepted.Queue.Version != 2 {
		t.Fatalf("mutation=%+v", repository.accepted)
	}
}

func TestTicketRouteTransferRejectsPreparedQueueFactDriftBeforeMutation(t *testing.T) {
	current := Record{Envelope: object.Envelope{
		ID: "work-id", MSPID: "msp-id", ClientID: "client-id", Version: 4,
	}}
	canonical := QueueRef{
		ID: "service-desk", MSPID: "msp-id", Key: "service-desk",
		Name: "Service Desk", Version: 3,
	}
	principal := authorization.Principal{
		ID: "actor-id", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("work_record.route"),
	}
	for _, test := range []struct {
		name     string
		prepared QueueRef
	}{
		{name: "version", prepared: QueueRef{ID: "service-desk", MSPID: "msp-id", Key: "service-desk", Name: "Service Desk", Version: 2}},
		{name: "key", prepared: QueueRef{ID: "service-desk", MSPID: "msp-id", Key: "old-key", Name: "Service Desk", Version: 3}},
		{name: "name", prepared: QueueRef{ID: "service-desk", MSPID: "msp-id", Key: "service-desk", Name: "Old Desk", Version: 3}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &queueRepository{
				current: current,
				byID:    map[string]QueueRef{"service-desk": canonical},
			}
			service := NewQueueService(repository, time.Now, func() string { return "id" })
			_, err := service.Transfer(context.Background(), QueueCommand{
				Principal: principal, WorkRecordID: "work-id", ExpectedVersion: 4,
				Queue:  test.prepared,
				Actor:  Actor{Type: "technician", ID: "actor-id", Source: "ai_workspace"},
				Reason: "Escalate",
			})
			if !errors.Is(err, object.ErrVersionConflict) {
				t.Fatalf("Transfer() error=%v, want version conflict", err)
			}
			if repository.accepted.Record.ID != "" {
				t.Fatalf("queue drift reached mutation: %+v", repository.accepted)
			}
		})
	}
}

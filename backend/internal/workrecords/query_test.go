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

type queryRepositoryStub struct {
	found      Record
	listed     []Record
	findTarget scope.Target
	findID     string
	filter     ListFilter
}

func (r *queryRepositoryStub) Find(
	_ context.Context,
	target scope.Target,
	id string,
) (Record, error) {
	r.findTarget, r.findID = target, id
	return r.found, nil
}

func (r *queryRepositoryStub) List(
	_ context.Context,
	filter ListFilter,
) ([]Record, error) {
	r.filter = filter
	return r.listed, nil
}

func TestQueryServiceReadsOnlyWithinAuthorizedClient(t *testing.T) {
	repository := &queryRepositoryStub{found: Record{
		Envelope: recordEnvelope("work-id", "client-id"),
		Title:    "Email unavailable",
	}}
	service := NewQueryService(repository)
	principal := authorization.Principal{
		Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet(
			"work_record.read",
		),
	}

	record, err := service.Get(context.Background(), GetCommand{
		Principal: principal,
		ID:        "work-id",
	})
	if err != nil || record.ID != "work-id" {
		t.Fatalf("Get() record=%+v error=%v", record, err)
	}
	if repository.findTarget != (scope.Target{MSPID: "msp-id", ClientID: "client-id"}) ||
		repository.findID != "work-id" {
		t.Fatalf("repository lookup escaped trusted scope: %+v %q", repository.findTarget, repository.findID)
	}

	_, err = service.Get(context.Background(), GetCommand{
		Principal: principal,
		Target:    scope.Target{MSPID: "msp-id", ClientID: "other-client"},
		ID:        "work-id",
	})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-client Get() error=%v, want ErrNotFound", err)
	}
}

func TestQueryServiceBuildsBoundedStableWorklist(t *testing.T) {
	before := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	repository := &queryRepositoryStub{listed: []Record{{
		Envelope: recordEnvelope("work-id", "client-id"),
	}}}
	service := NewQueryService(repository)
	principal := authorization.Principal{
		Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet(
			"work_record.read",
		),
	}

	records, err := service.List(context.Background(), ListCommand{
		Principal:       principal,
		Status:          "in_progress",
		QueueID:         "queue-id",
		PrimaryOwnerID:  "technician-id",
		BeforeUpdatedAt: before,
		BeforeID:        "cursor-id",
		Limit:           25,
	})
	if err != nil || len(records) != 1 {
		t.Fatalf("List() records=%+v error=%v", records, err)
	}
	if repository.filter.Target != (scope.Target{MSPID: "msp-id", ClientID: "client-id"}) ||
		repository.filter.Status != "in_progress" ||
		repository.filter.QueueID != "queue-id" ||
		repository.filter.PrimaryOwnerID != "technician-id" ||
		repository.filter.BeforeUpdatedAt != before ||
		repository.filter.BeforeID != "cursor-id" ||
		repository.filter.Limit != 25 {
		t.Fatalf("unexpected repository filter: %+v", repository.filter)
	}

	_, err = service.List(context.Background(), ListCommand{
		Principal: principal,
		Limit:     101,
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("unbounded List() error=%v, want ErrInvalid", err)
	}
}

func recordEnvelope(id, clientID string) object.Envelope {
	return object.Envelope{
		ID: id, ObjectType: "work_record", MSPID: "msp-id",
		ClientID: clientID, LifecycleState: "active", Version: 1,
	}
}

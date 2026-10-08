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

type assignmentRepository struct {
	current  Record
	accepted AssignmentMutation
	calls    int
}

func (r *assignmentRepository) Find(_ context.Context, _ scope.Target, _ string) (Record, error) {
	return r.current, nil
}
func (r *assignmentRepository) AssignAtomic(_ context.Context, mutation AssignmentMutation) error {
	r.calls++
	r.accepted = mutation
	return nil
}

func TestAssignAuditsTrimmedReasonWithOwnerChange(t *testing.T) {
	repository := &assignmentRepository{current: Record{
		Envelope: object.Envelope{
			ID: "work-id", ObjectType: "work_record", MSPID: "msp-id",
			ClientID: "client-id", Version: 4, LifecycleState: "active",
			CreatedBy: "creator", UpdatedBy: "creator",
		},
	}}
	now := time.Date(2026, time.July, 29, 17, 0, 0, 0, time.UTC)
	ids := []string{"audit-id", "event-id", "correlation-id"}
	service := NewAssignmentService(repository, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("work_record.assign"),
	}

	updated, err := service.Assign(context.Background(), AssignCommand{
		Principal: principal, WorkRecordID: "work-id", ExpectedVersion: 4,
		OwnerID: "owner-id", ExpectedClientVersion: 3, ExpectedOwnerVersion: 6,
		Reason: "  Reassigned after reviewing capacity  ",
		Actor:  Actor{Type: "technician", ID: "actor-id", Source: "web"},
	})
	if err != nil {
		t.Fatalf("Assign() error = %v", err)
	}
	if updated.PrimaryOwnerID != "owner-id" || updated.Version != 5 {
		t.Fatalf("unexpected assigned record: %+v", updated)
	}
	if repository.accepted.Event.EventType != "work_record.owner.changed" ||
		repository.accepted.Audit.Action != "work_record.owner.changed" {
		t.Fatal("assignment did not emit matching audit/event records")
	}
	if repository.accepted.Audit.Reason != "Reassigned after reviewing capacity" {
		t.Fatalf("assignment audit reason = %q", repository.accepted.Audit.Reason)
	}
	if repository.accepted.ExpectedClientVersion != 3 || repository.accepted.ExpectedOwnerVersion != 6 {
		t.Fatalf("assignment mutation lost commit fences: %+v", repository.accepted)
	}
}

func TestAssignRejectsBlankReasonAndSameOwnerBeforeMutation(t *testing.T) {
	for name, command := range map[string]AssignCommand{
		"blank reason": {OwnerID: "owner-id", Reason: " \t "},
		"same owner":   {OwnerID: "current-owner", Reason: "Ownership already reviewed"},
	} {
		t.Run(name, func(t *testing.T) {
			repository := &assignmentRepository{current: Record{Envelope: object.Envelope{
				ID: "work-id", ObjectType: "work_record", MSPID: "msp-id", ClientID: "client-id",
				Version: 4, LifecycleState: "active",
			}, PrimaryOwnerID: "current-owner"}}
			service := NewAssignmentService(repository, time.Now, func() string { return "unused" })
			principal := authorization.Principal{
				Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
				Capabilities: authorization.NewCapabilitySet("work_record.assign"),
			}
			command.Principal, command.WorkRecordID, command.ExpectedVersion = principal, "work-id", 4
			command.Actor = Actor{Type: "technician", ID: "actor-id", Source: "web"}

			_, err := service.Assign(context.Background(), command)
			if !errors.Is(err, ErrInvalid) || repository.calls != 0 {
				t.Fatalf("Assign() error=%v mutation calls=%d", err, repository.calls)
			}
		})
	}
}

func TestAssignRejectsStaleVersionBeforeMutation(t *testing.T) {
	repository := &assignmentRepository{current: Record{
		Envelope: object.Envelope{
			ID: "work-id", ObjectType: "work_record", MSPID: "msp-id",
			ClientID: "client-id", Version: 4, LifecycleState: "active",
			CreatedBy: "creator", UpdatedBy: "creator",
		},
	}}
	service := NewAssignmentService(repository, time.Now, func() string { return "unused" })
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("work_record.assign"),
	}
	_, err := service.Assign(context.Background(), AssignCommand{
		Principal: principal, WorkRecordID: "work-id", ExpectedVersion: 3,
		OwnerID: "owner-id", Reason: "Reassigned after triage",
		Actor: Actor{Type: "technician", ID: "actor-id", Source: "web"},
	})
	if !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("Assign() error = %v, want version conflict", err)
	}
	if repository.calls != 0 {
		t.Fatal("stale assignment reached mutation repository")
	}
}

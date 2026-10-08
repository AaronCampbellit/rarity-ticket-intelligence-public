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

type mergeRepository struct {
	records  map[string]Record
	accepted MergeMutation
	calls    int
}

func (r *mergeRepository) FindForMerge(_ context.Context, _ scope.Target, id string) (Record, error) {
	record, ok := r.records[id]
	if !ok {
		return Record{}, scope.ErrNotFound
	}
	return record, nil
}
func (r *mergeRepository) MergeAtomic(_ context.Context, mutation MergeMutation) error {
	r.calls++
	r.accepted = mutation
	return nil
}

func TestMergeTombstonesDuplicateAndPreservesCanonicalRecord(t *testing.T) {
	repository := &mergeRepository{records: map[string]Record{
		"winner": mergeRecord("winner", "client-id", 5),
		"loser":  mergeRecord("loser", "client-id", 3),
	}}
	now := time.Date(2026, time.July, 29, 19, 0, 0, 0, time.UTC)
	ids := []string{"audit-id", "event-id", "correlation-id"}
	service := NewMergeService(repository, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("work_record.merge"),
	}

	result, err := service.Merge(context.Background(), MergeCommand{
		Principal: principal, WinnerID: "winner", WinnerVersion: 5,
		DuplicateID: "loser", DuplicateVersion: 3,
		Actor:  Actor{Type: "technician", ID: "actor-id", Source: "web"},
		Reason: "Duplicate inbound email",
	})
	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}
	if result.Winner.ID != "winner" ||
		result.Duplicate.MergedIntoID != "winner" ||
		result.Duplicate.LifecycleState != "deleted" {
		t.Fatalf("unexpected merge result: %+v", result)
	}
	if repository.accepted.Audit.Action != "work_record.merged" ||
		repository.accepted.Event.EventType != "work_record.merged" {
		t.Fatal("merge did not produce matching audit/event facts")
	}
	if repository.accepted.Event.Data["winner_id"] != "winner" {
		t.Fatalf("merge event omitted reparent destination: %+v", repository.accepted.Event.Data)
	}
	if repository.accepted.ReparentChildren == false {
		t.Fatal("merge transaction did not require child-history preservation")
	}
}

func TestMergeRejectsCrossClientAndStaleRecords(t *testing.T) {
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-a"},
		Capabilities: authorization.NewCapabilitySet("work_record.merge"),
	}
	tests := []struct {
		name       string
		repository *mergeRepository
		command    MergeCommand
		want       error
	}{
		{
			name: "cross client",
			repository: &mergeRepository{records: map[string]Record{
				"winner": mergeRecord("winner", "client-a", 1),
				"loser":  mergeRecord("loser", "client-b", 1),
			}},
			command: MergeCommand{Principal: principal, WinnerID: "winner", WinnerVersion: 1, DuplicateID: "loser", DuplicateVersion: 1, Actor: Actor{Type: "technician", ID: "actor", Source: "web"}, Reason: "duplicate"},
			want:    scope.ErrNotFound,
		},
		{
			name: "stale",
			repository: &mergeRepository{records: map[string]Record{
				"winner": mergeRecord("winner", "client-a", 2),
				"loser":  mergeRecord("loser", "client-a", 1),
			}},
			command: MergeCommand{Principal: principal, WinnerID: "winner", WinnerVersion: 1, DuplicateID: "loser", DuplicateVersion: 1, Actor: Actor{Type: "technician", ID: "actor", Source: "web"}, Reason: "duplicate"},
			want:    object.ErrVersionConflict,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewMergeService(tt.repository, time.Now, func() string { return "unused" })
			_, err := service.Merge(context.Background(), tt.command)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Merge() error = %v, want %v", err, tt.want)
			}
			if tt.repository.calls != 0 {
				t.Fatal("invalid merge reached repository")
			}
		})
	}
}

func mergeRecord(id, clientID string, version int64) Record {
	return Record{Envelope: object.Envelope{
		ID: id, ObjectType: "work_record", MSPID: "msp-id", ClientID: clientID,
		LifecycleState: "active", Version: version, CreatedBy: "creator", UpdatedBy: "creator",
	}}
}

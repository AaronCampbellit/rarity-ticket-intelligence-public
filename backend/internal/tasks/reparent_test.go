package tasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type moveRepository struct {
	tasks    map[ID]Task
	accepted MoveMutation
	calls    int
}

func (r *moveRepository) Find(context.Context, scope.Target, string) (Task, error) {
	return Task{}, ErrNotFound
}
func (r *moveRepository) ListForParent(context.Context, scope.Target, Ref, string) ([]Task, error) {
	return nil, nil
}
func (r *moveRepository) CreateAtomic(context.Context, CreateMutation) error {
	return nil
}

func (r *moveRepository) LoadSelected(_ context.Context, _ Ref, selected []ID) ([]Task, error) {
	result := make([]Task, 0, len(selected))
	for _, id := range selected {
		task, ok := r.tasks[id]
		if !ok {
			return nil, ErrNotFound
		}
		result = append(result, task)
	}
	return result, nil
}

func (r *moveRepository) MoveAtomic(_ context.Context, mutation MoveMutation) error {
	r.calls++
	r.accepted = mutation
	for _, task := range mutation.Tasks {
		r.tasks[task.ID] = task
	}
	return nil
}

func TestMoveIncompletePreservesIdentityAndLeavesCompletedTasks(t *testing.T) {
	opportunity := Ref{Type: ParentOpportunity, ID: "opportunity-id", MSPID: "msp-id", ClientID: "client-id"}
	project := Ref{Type: ParentProject, ID: "project-id", MSPID: "msp-id", ClientID: "client-id"}
	incompleteID, completedID := ID("incomplete"), ID("completed")
	repository := &moveRepository{tasks: map[ID]Task{
		incompleteID: {
			ID: string(incompleteID), MSPID: "msp-id", ClientID: "client-id",
			Parent: opportunity, Title: "Schedule kickoff", Status: "open",
			OwnerID: "owner-id", EstimateMinutes: 90, Version: 4,
		},
		completedID: {
			ID: string(completedID), MSPID: "msp-id", ClientID: "client-id",
			Parent: opportunity, Title: "Qualify", Status: "completed", Version: 2,
		},
	}}
	ids := []string{"audit-id", "event-id", "correlation-id"}
	service := NewService(repository, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}, taskCreationPreparer())
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("task.move"),
	}

	result, err := service.MoveIncomplete(context.Background(), MoveCommand{
		From: opportunity, To: project,
		Selected:         []ID{incompleteID, completedID},
		ExpectedVersions: map[ID]int64{incompleteID: 4, completedID: 2},
		Principal:        principal, ActorID: "actor-id", Source: "conversion",
	})
	if err != nil {
		t.Fatalf("MoveIncomplete() error = %v", err)
	}
	if len(result) != 1 || result[0].Parent != project ||
		result[0].ID != string(incompleteID) ||
		result[0].OwnerID != "owner-id" || result[0].EstimateMinutes != 90 {
		t.Fatalf("unexpected moved tasks: %+v", result)
	}
	if repository.tasks[completedID].Parent != opportunity {
		t.Fatal("completed task moved from Opportunity history")
	}
	if !repository.accepted.ReparentDescendants ||
		repository.accepted.Event.EventType != "task.parent.changed" {
		t.Fatal("movement did not preserve descendants/history contract")
	}
}

func TestMoveIncompleteRejectsCrossScopeStaleAndUnauthorizedMoves(t *testing.T) {
	base := Ref{Type: ParentOpportunity, ID: "opportunity-id", MSPID: "msp-id", ClientID: "client-a"}
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-a"},
		Capabilities: authorization.NewCapabilitySet("task.move"),
	}
	tests := []struct {
		name      string
		to        Ref
		principal authorization.Principal
		expected  int64
		want      error
	}{
		{name: "cross client", to: Ref{Type: ParentProject, ID: "project-id", MSPID: "msp-id", ClientID: "client-b"}, principal: principal, expected: 1, want: scope.ErrNotFound},
		{name: "stale", to: Ref{Type: ParentProject, ID: "project-id", MSPID: "msp-id", ClientID: "client-a"}, principal: principal, expected: 0, want: ErrStaleTask},
		{name: "unauthorized", to: Ref{Type: ParentProject, ID: "project-id", MSPID: "msp-id", ClientID: "client-a"}, principal: authorization.Principal{Scope: principal.Scope}, expected: 1, want: authorization.ErrForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &moveRepository{tasks: map[ID]Task{
				"task-id": {ID: "task-id", MSPID: "msp-id", ClientID: "client-a", Parent: base, Status: "open", Version: 1},
			}}
			service := NewService(repository, time.Now, func() string { return "unused" }, taskCreationPreparer())
			_, err := service.MoveIncomplete(context.Background(), MoveCommand{
				From: base, To: tt.to, Selected: []ID{"task-id"},
				ExpectedVersions: map[ID]int64{"task-id": tt.expected},
				Principal:        tt.principal, ActorID: "actor-id", Source: "conversion",
			})
			if !errors.Is(err, tt.want) {
				t.Fatalf("MoveIncomplete() error = %v, want %v", err, tt.want)
			}
			if repository.calls != 0 {
				t.Fatal("invalid movement reached repository")
			}
		})
	}
}

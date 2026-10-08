package workrecords

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
)

type transitionGuardRepository struct{ meaningful bool }

func (r transitionGuardRepository) Get(context.Context, tagging.TargetRef) (tagging.TaggedObject, error) {
	if !r.meaningful {
		return tagging.TaggedObject{}, nil
	}
	return tagging.TaggedObject{Effective: []tagging.Assignment{{Tag: tagging.Tag{ID: "meaningful", InternalKey: "network", State: tagging.StateActive}}}}, nil
}
func (transitionGuardRepository) ResolveTags(context.Context, string, []string) ([]tagging.Tag, error) {
	return nil, nil
}
func (transitionGuardRepository) Accepted(context.Context, tagging.TargetRef, string) (tagging.TaggedObject, bool, error) {
	return tagging.TaggedObject{}, false, nil
}
func (transitionGuardRepository) ReplaceDirect(context.Context, tagging.AssociationMutation) (tagging.TaggedObject, error) {
	return tagging.TaggedObject{}, nil
}
func (transitionGuardRepository) History(context.Context, tagging.TargetRef) ([]tagging.HistoryEntry, error) {
	return nil, nil
}
func transitionGuard(meaningful bool) *tagging.TerminalGuard {
	return tagging.NewTerminalGuard(tagging.NewAssociationService(transitionGuardRepository{meaningful: meaningful}))
}

type transitionRepository struct {
	record   Record
	selected SelectedWorkflow
	sla      AppliedSLA
	mutation TransitionMutation
	calls    int
}

func (r *transitionRepository) Find(context.Context, scope.Target, string) (Record, error) {
	return r.record, nil
}

func (r *transitionRepository) FindSelectedWorkflow(
	context.Context,
	scope.Target,
	string,
) (SelectedWorkflow, error) {
	return r.selected, nil
}

func (r *transitionRepository) FindAppliedSLA(
	context.Context,
	scope.Target,
	string,
) (AppliedSLA, error) {
	return r.sla, nil
}

func (r *transitionRepository) TransitionAtomic(_ context.Context, mutation TransitionMutation) error {
	r.calls++
	r.mutation = mutation
	return nil
}

func TestTransitionUsesExactSelectedDefinitionAndWritesFacts(t *testing.T) {
	at := time.Date(2026, time.July, 29, 18, 0, 0, 0, time.UTC)
	repository := &transitionRepository{
		record: Record{
			Envelope: object.Envelope{
				ID: "work", ObjectType: "work_record", MSPID: "msp", ClientID: "client",
				Version: 2,
			},
			Status: "new", PrimaryOwnerID: "owner",
		},
		selected: SelectedWorkflow{
			WorkflowID: "workflow", Version: 4,
			Definition: workflow.Definition{
				States:      []workflow.State{{Key: "new"}, {Key: "in_progress", RequiresOwner: true}},
				Transitions: []workflow.Transition{{From: "new", To: "in_progress"}},
			},
		},
		sla: activeTestSLA(at),
	}
	ids := []string{"audit", "event", "correlation"}
	service := NewTransitionService(repository, func() time.Time { return at }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}, transitionGuard(true))
	record, err := service.Transition(context.Background(), TransitionCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("work_record.transition"),
		},
		WorkRecordID: "work", ExpectedVersion: 2, ToStatus: "in_progress",
		Actor: Actor{Type: "technician", ID: "actor", Source: "api"},
	})
	if err != nil {
		t.Fatalf("Transition() error = %v", err)
	}
	if record.Status != "in_progress" || record.Version != 3 || repository.calls != 1 {
		t.Fatalf("unexpected transition result: record=%+v calls=%d", record, repository.calls)
	}
	if repository.mutation.PreviousStatus != "new" ||
		repository.mutation.WorkflowID != "workflow" ||
		repository.mutation.WorkflowVersion != 4 ||
		repository.mutation.Audit.Action != "work_record.transitioned" ||
		repository.mutation.Event.EventType != "work_record.transitioned" {
		t.Fatalf("transition evidence incomplete: %+v", repository.mutation)
	}
}

func activeTestSLA(at time.Time) AppliedSLA {
	return AppliedSLA{
		ID: "sla", PolicyID: "policy", PolicyVersion: 1,
		CalendarID: "calendar", CalendarVersion: 1,
		CalendarDefinition: sla.CalendarDefinition{
			Timezone: "UTC",
			Weekly: map[string][]sla.Window{
				"monday":    {{StartMinute: 0, EndMinute: 1440}},
				"tuesday":   {{StartMinute: 0, EndMinute: 1440}},
				"wednesday": {{StartMinute: 0, EndMinute: 1440}},
				"thursday":  {{StartMinute: 0, EndMinute: 1440}},
				"friday":    {{StartMinute: 0, EndMinute: 1440}},
				"saturday":  {{StartMinute: 0, EndMinute: 1440}},
				"sunday":    {{StartMinute: 0, EndMinute: 1440}},
			},
		},
		ResponseWarningAt: at.Add(time.Hour), ResponseDueAt: at.Add(2 * time.Hour),
		ResolutionWarningAt: at.Add(3 * time.Hour), ResolutionDueAt: at.Add(4 * time.Hour),
		ResponseState: sla.Running, ResolutionState: sla.Running, Version: 1,
	}
}

func TestTransitionRejectsDisallowedAndStaleChanges(t *testing.T) {
	base := &transitionRepository{
		record: Record{
			Envelope: object.Envelope{ID: "work", MSPID: "msp", ClientID: "client", Version: 2},
			Status:   "new",
		},
		selected: SelectedWorkflow{
			WorkflowID: "workflow", Version: 1,
			Definition: workflow.Definition{
				States: []workflow.State{{Key: "new"}, {Key: "resolved"}},
			},
		},
		sla: activeTestSLA(time.Now()),
	}
	service := NewTransitionService(base, time.Now, func() string { return "unused" }, transitionGuard(true))
	command := TransitionCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("work_record.transition"),
		},
		WorkRecordID: "work", ExpectedVersion: 2, ToStatus: "resolved",
		Actor: Actor{Type: "technician", ID: "actor", Source: "api"},
	}
	if _, err := service.Transition(context.Background(), command); !errors.Is(err, workflow.ErrTransitionNotAllowed) {
		t.Fatalf("Transition() error = %v, want transition not allowed", err)
	}
	command.ExpectedVersion = 1
	if _, err := service.Transition(context.Background(), command); !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("Transition() error = %v, want version conflict", err)
	}
	if base.calls != 0 {
		t.Fatal("rejected transition reached repository")
	}
}

func TestTransitionPausesAndResumesSLAUsingBusinessTime(t *testing.T) {
	pauseAt := time.Date(2026, time.July, 31, 16, 0, 0, 0, time.UTC)
	repository := &transitionRepository{
		record: Record{
			Envelope: object.Envelope{
				ID: "work", ObjectType: "work_record", MSPID: "msp",
				ClientID: "client", Version: 2,
			},
			Status: "active",
		},
		selected: SelectedWorkflow{
			WorkflowID: "workflow", Version: 1,
			Definition: workflow.Definition{
				States: []workflow.State{{Key: "active"}, {Key: "waiting"}},
				Transitions: []workflow.Transition{
					{From: "active", To: "waiting"}, {From: "waiting", To: "active"},
				},
			},
		},
		sla: activeTestSLA(pauseAt),
	}
	repository.sla.PauseStates = []string{"waiting"}
	repository.sla.CalendarDefinition = sla.CalendarDefinition{
		Timezone: "UTC",
		Weekly: map[string][]sla.Window{
			"monday": {{StartMinute: 9 * 60, EndMinute: 17 * 60}},
			"friday": {{StartMinute: 9 * 60, EndMinute: 17 * 60}},
		},
	}
	service := NewTransitionService(repository, func() time.Time { return pauseAt }, sequenceIDs(), transitionGuard(true))
	_, err := service.Transition(context.Background(), validTransitionCommand("waiting", 2))
	if err != nil {
		t.Fatalf("pause Transition() error = %v", err)
	}
	if !repository.mutation.SLAChanged ||
		repository.mutation.SLA.PausedAt == nil ||
		repository.mutation.SLA.ResolutionState != sla.Paused ||
		repository.mutation.SLAAudit.Action != "sla.paused" {
		t.Fatalf("pause mutation = %+v", repository.mutation)
	}

	resumeAt := time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC)
	repository.record = repository.mutation.Record
	repository.sla = repository.mutation.SLA
	repository.calls = 0
	service = NewTransitionService(repository, func() time.Time { return resumeAt }, sequenceIDs(), transitionGuard(true))
	_, err = service.Transition(context.Background(), validTransitionCommand("active", 3))
	if err != nil {
		t.Fatalf("resume Transition() error = %v", err)
	}
	if repository.mutation.SLA.PausedAt != nil ||
		repository.mutation.SLA.PausedSeconds != 66*60*60 ||
		repository.mutation.SLA.ResponseDueAt != resumeAt.Add(time.Hour) ||
		repository.mutation.SLAAudit.Action != "sla.resumed" {
		t.Fatalf("resume mutation = %+v", repository.mutation)
	}
}

func TestTransitionResolvesSLAButPreservesLateBreach(t *testing.T) {
	now := time.Date(2026, time.July, 30, 18, 0, 0, 0, time.UTC)
	repository := &transitionRepository{
		record: Record{
			Envelope: object.Envelope{
				ID: "work", ObjectType: "work_record", MSPID: "msp",
				ClientID: "client", Version: 2,
			},
			Status: "active",
		},
		selected: SelectedWorkflow{
			WorkflowID: "workflow", Version: 1,
			Definition: workflow.Definition{
				States: []workflow.State{
					{Key: "active"},
					{Key: "resolved", SLABehavior: workflow.SLAResolved},
				},
				Transitions: []workflow.Transition{{From: "active", To: "resolved"}},
			},
		},
		sla: activeTestSLA(now),
	}
	repository.sla.ResolutionDueAt = now.Add(-time.Minute)
	service := NewTransitionService(repository, func() time.Time { return now }, sequenceIDs(), transitionGuard(true))
	_, err := service.Transition(context.Background(), validTransitionCommand("resolved", 2))
	if err != nil {
		t.Fatalf("Transition() error = %v", err)
	}
	if repository.mutation.SLA.ResolvedAt == nil ||
		repository.mutation.SLA.ResolutionState != sla.Breached ||
		repository.mutation.SLAAudit.Action != "sla.resolved" {
		t.Fatalf("late resolution mutation = %+v", repository.mutation)
	}
}

func validTransitionCommand(status string, version int64) TransitionCommand {
	return TransitionCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("work_record.transition"),
		},
		WorkRecordID: "work", ExpectedVersion: version, ToStatus: status,
		Actor: Actor{Type: "technician", ID: "actor", Source: "api"},
	}
}

func TestTransitionBlocksUnclassifiedResolveAndCancelBeforeMutation(t *testing.T) {
	for _, terminal := range []struct {
		status   string
		behavior workflow.SLABehavior
	}{
		{status: "resolved", behavior: workflow.SLAResolved},
		{status: "cancelled", behavior: workflow.SLACancelled},
	} {
		t.Run(terminal.status, func(t *testing.T) {
			repository := &transitionRepository{
				record:   Record{Envelope: object.Envelope{ID: "work", ObjectType: "work_record", MSPID: "msp", ClientID: "client", Version: 1}, Status: "active"},
				selected: SelectedWorkflow{WorkflowID: "workflow", Version: 1, Definition: workflow.Definition{States: []workflow.State{{Key: "active"}, {Key: terminal.status, SLABehavior: terminal.behavior}}, Transitions: []workflow.Transition{{From: "active", To: terminal.status}}}},
				sla:      activeTestSLA(time.Now()),
			}
			service := NewTransitionService(repository, time.Now, sequenceIDs(), transitionGuard(false))
			_, err := service.Transition(context.Background(), validTransitionCommand(terminal.status, 1))
			if !errors.Is(err, tagging.ErrMeaningfulTagRequired) || repository.calls != 0 {
				t.Fatalf("Transition() error=%v calls=%d", err, repository.calls)
			}
			service = NewTransitionService(repository, time.Now, sequenceIDs(), transitionGuard(true))
			if _, err := service.Transition(context.Background(), validTransitionCommand(terminal.status, 1)); err != nil || repository.calls != 1 {
				t.Fatalf("classified Transition() error=%v calls=%d", err, repository.calls)
			}
		})
	}
}

func sequenceIDs() func() string {
	index := 0
	return func() string {
		index++
		return "id-" + string(rune('0'+index))
	}
}

package automation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/collaboration"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/comments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

type tagActionsStub struct {
	current  tagging.TaggedObject
	gets     []tagging.GetCommand
	replaced tagging.ReplaceCommand
	calls    int
}

func (s *tagActionsStub) Get(_ context.Context, command tagging.GetCommand) (tagging.TaggedObject, error) {
	s.gets = append(s.gets, command)
	return s.current, nil
}
func (s *tagActionsStub) ReplaceDirect(_ context.Context, command tagging.ReplaceCommand) (tagging.TaggedObject, error) {
	s.calls++
	s.replaced = command
	s.current.ObjectVersion++
	return s.current, nil
}

func TestRuntimeActionExecutorAddsTagsWithStableMutationFingerprint(t *testing.T) {
	tags := &tagActionsStub{current: tagging.TaggedObject{ObjectVersion: 4, Direct: []tagging.Assignment{{Tag: tagging.Tag{ID: "11111111-1111-4111-8111-111111111111", InternalKey: "service", State: tagging.StateActive}, Source: tagging.SourceHuman}}}}
	executor := NewRuntimeActionExecutor(&priorityActionsStub{}, &assignmentActionsStub{}, &transitionActionsStub{}, &commentActionsStub{}, &externalActionsStub{}, tags)
	principal := authorization.Principal{ID: "automation-id", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"}, Capabilities: authorization.NewCapabilitySet("classification.apply")}
	snapshot := map[string]string{"_subject_type": "work_record", "_subject_id": "work-id", "_subject_version": "3", "_automation_run_id": "run-id", "_automation_step_id": "step-id"}
	_, err := executor.Execute(context.Background(), principal, Action{Kind: ActionAddTags, Parameters: map[string]string{"tag_ids": `["22222222-2222-4222-8222-222222222222"]`}}, snapshot)
	if err != nil || tags.calls != 1 || tags.replaced.ExpectedObjectVersion != 4 || tags.replaced.Source != tagging.SourceAutomation || tags.replaced.IdempotencyKey != `run-id:step-id:add_tags:["22222222-2222-4222-8222-222222222222"]` || tags.replaced.CausationID != "run-id" || len(tags.replaced.TagIDs) != 2 {
		t.Fatalf("command=%+v calls=%d error=%v", tags.replaced, tags.calls, err)
	}
}

func TestRuntimeActionExecutorRejectsInheritedTagRemoval(t *testing.T) {
	tags := &tagActionsStub{current: tagging.TaggedObject{ObjectVersion: 2, Direct: []tagging.Assignment{{Tag: tagging.Tag{ID: "11111111-1111-4111-8111-111111111111"}}}, Inherited: []tagging.Assignment{{Tag: tagging.Tag{ID: "22222222-2222-4222-8222-222222222222"}, Inherited: true}}}}
	executor := NewRuntimeActionExecutor(&priorityActionsStub{}, &assignmentActionsStub{}, &transitionActionsStub{}, &commentActionsStub{}, &externalActionsStub{}, tags)
	principal := authorization.Principal{ID: "automation-id", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"}}
	_, err := executor.Execute(context.Background(), principal, Action{Kind: ActionRemoveTags, Parameters: map[string]string{"tag_ids": `["22222222-2222-4222-8222-222222222222"]`}}, map[string]string{"_subject_type": "work_record", "_subject_id": "work-id", "_subject_version": "1", "_automation_run_id": "run", "_automation_step_id": "step"})
	if !errors.Is(err, ErrActionFailed) || tags.calls != 0 {
		t.Fatalf("calls=%d error=%v", tags.calls, err)
	}
}

func TestRuntimeActionExecutorTargetsEverySupportedTaggedSubject(t *testing.T) {
	for _, objectType := range []tagging.ObjectType{
		tagging.ObjectWorkRecord, tagging.ObjectTask, tagging.ObjectProject,
		tagging.ObjectAsset, tagging.ObjectKnowledgeArticle, tagging.ObjectTimeEntry,
	} {
		t.Run(objectType.String(), func(t *testing.T) {
			tags := &tagActionsStub{current: tagging.TaggedObject{ObjectVersion: 7}}
			executor := NewRuntimeActionExecutor(
				&priorityActionsStub{}, &assignmentActionsStub{},
				&transitionActionsStub{}, &commentActionsStub{},
				&externalActionsStub{}, tags,
			)
			principal := authorization.Principal{
				ID:           "automation-id",
				Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
				Capabilities: authorization.NewCapabilitySet("classification.apply"),
			}
			_, err := executor.Execute(context.Background(), principal, Action{
				Kind: ActionAddTags,
				Parameters: map[string]string{
					"tag_ids": `["22222222-2222-4222-8222-222222222222"]`,
				},
			}, map[string]string{
				"_subject_type": objectType.String(), "_subject_id": "subject-id",
				"_subject_version": "7", "_automation_run_id": "run-id",
				"_automation_step_id": "step-id",
			})
			if err != nil || len(tags.gets) != 1 ||
				tags.gets[0].Target.ObjectType != objectType ||
				tags.gets[0].Target.MSPID != "msp-id" ||
				tags.gets[0].Target.ClientID != "client-id" ||
				tags.replaced.Target != tags.gets[0].Target {
				t.Fatalf("Execute() gets=%+v replaced=%+v error=%v", tags.gets, tags.replaced, err)
			}
		})
	}
}

func TestRuntimeActionExecutorRejectsInvalidTaggedSubjectAndNonWorkRecordAction(t *testing.T) {
	tags := &tagActionsStub{current: tagging.TaggedObject{ObjectVersion: 1}}
	executor := NewRuntimeActionExecutor(
		&priorityActionsStub{}, &assignmentActionsStub{},
		&transitionActionsStub{}, &commentActionsStub{}, &externalActionsStub{}, tags,
	)
	principal := authorization.Principal{
		ID:    "automation-id",
		Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
	}
	base := map[string]string{
		"_subject_type": "foreign", "_subject_id": "subject-id",
		"_subject_version": "1", "_automation_run_id": "run-id",
		"_automation_step_id": "step-id",
	}
	_, invalidErr := executor.Execute(context.Background(), principal, Action{
		Kind: ActionAddTags, Parameters: map[string]string{
			"tag_ids": `["22222222-2222-4222-8222-222222222222"]`,
		},
	}, base)
	base["_subject_type"] = tagging.ObjectTask.String()
	_, wrongActionErr := executor.Execute(context.Background(), principal, Action{
		Kind: ActionUpdateField, Parameters: map[string]string{
			"field": "priority", "value": "high", "reason": "policy",
		},
	}, base)
	principal.Scope.ClientID = ""
	base["_subject_type"] = tagging.ObjectAsset.String()
	_, crossScopeErr := executor.Execute(context.Background(), principal, Action{
		Kind: ActionAddTags, Parameters: map[string]string{
			"tag_ids": `["22222222-2222-4222-8222-222222222222"]`,
		},
	}, base)
	if !errors.Is(invalidErr, ErrActionFailed) ||
		!errors.Is(wrongActionErr, ErrActionFailed) ||
		!errors.Is(crossScopeErr, ErrActionFailed) || len(tags.gets) != 0 {
		t.Fatalf("invalid=%v wrong_action=%v cross_scope=%v gets=%+v", invalidErr, wrongActionErr, crossScopeErr, tags.gets)
	}
}

type priorityActionsStub struct {
	command workrecords.PriorityCommand
}

func (s *priorityActionsStub) Change(
	_ context.Context,
	command workrecords.PriorityCommand,
) (workrecords.Record, error) {
	s.command = command
	return workrecords.Record{Envelope: object.Envelope{Version: 2}}, nil
}

type assignmentActionsStub struct {
	command workrecords.AssignCommand
}

func (s *assignmentActionsStub) Assign(
	_ context.Context,
	command workrecords.AssignCommand,
) (workrecords.Record, error) {
	s.command = command
	return workrecords.Record{Envelope: object.Envelope{Version: 2}}, nil
}

type legacyAssignmentRepository struct {
	current  workrecords.Record
	accepted workrecords.AssignmentMutation
}

func (r *legacyAssignmentRepository) Find(
	context.Context,
	scope.Target,
	string,
) (workrecords.Record, error) {
	return r.current, nil
}

func (r *legacyAssignmentRepository) AssignAtomic(
	_ context.Context,
	mutation workrecords.AssignmentMutation,
) error {
	r.accepted = mutation
	return nil
}

type transitionActionsStub struct {
	command workrecords.TransitionCommand
}

func (s *transitionActionsStub) Transition(
	_ context.Context,
	command workrecords.TransitionCommand,
) (workrecords.Record, error) {
	s.command = command
	return workrecords.Record{Envelope: object.Envelope{Version: 2}}, nil
}

type commentActionsStub struct {
	command CommentCommand
	err     error
	id      string
}

func (s *commentActionsStub) Create(
	_ context.Context,
	command CommentCommand,
) (string, error) {
	s.command = command
	if s.id == "" {
		s.id = "comment-id"
	}
	return s.id, s.err
}

type publicCommentActionsStub struct {
	command comments.CreateCommand
}

func (s *publicCommentActionsStub) Create(_ context.Context, command comments.CreateCommand) (comments.Comment, error) {
	s.command = command
	return comments.Comment{ID: "public-comment-id"}, nil
}

type internalCommentActionsStub struct {
	command collaboration.CreateCommand
	order   *[]string
}

func (s *internalCommentActionsStub) CreateComment(_ context.Context, command collaboration.CreateCommand) (collaboration.Source, error) {
	s.command = command
	if s.order != nil {
		*s.order = append(*s.order, "internal")
	}
	return collaboration.Source{ID: "internal-source-id"}, nil
}

type commentActorResolverStub struct {
	input authorization.Principal
	actor authorization.Principal
	err   error
	calls int
	order *[]string
}

func (s *commentActorResolverStub) ResolveAutomationCommentActor(_ context.Context, principal authorization.Principal) (authorization.Principal, error) {
	s.input = principal
	s.calls++
	if s.order != nil {
		*s.order = append(*s.order, "resolver")
	}
	return s.actor, s.err
}

type externalActionsStub struct {
	ref      string
	snapshot map[string]string
}

func (s *externalActionsStub) Call(
	_ context.Context,
	_ authorization.Principal,
	ref string,
	_ map[string]string,
	snapshot map[string]string,
) (ActionResult, error) {
	s.ref, s.snapshot = ref, snapshot
	return ActionResult{ChangedObjectIDs: []string{"external-call-id"}}, nil
}

func TestRuntimeActionExecutorUsesGovernedApplicationServices(t *testing.T) {
	priority := &priorityActionsStub{}
	assignments := &assignmentActionsStub{}
	transitions := &transitionActionsStub{}
	commentService := &commentActionsStub{}
	external := &externalActionsStub{}
	executor := NewRuntimeActionExecutor(
		priority, assignments, transitions, commentService, external,
	)
	principal := authorization.Principal{
		ID:    "automation-id",
		Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet(
			"work_record.edit", "work_record.assign",
			"work_record.transition", "comment.internal.create",
		),
	}
	snapshot := map[string]string{
		"_subject_type": "work_record", "_subject_id": "work-id",
		"_subject_version": "1", "_automation_run_id": "run-id",
		"_automation_step_id": "step-id",
	}

	result, err := executor.Execute(
		context.Background(), principal,
		Action{
			Kind: ActionUpdateField,
			Parameters: map[string]string{
				"field": "priority", "value": "critical",
				"reason": "Escalated by policy",
			},
		},
		snapshot,
	)

	if err != nil || priority.command.WorkRecordID != "work-id" ||
		priority.command.ExpectedVersion != 1 ||
		priority.command.Actor.Type != "automation" ||
		priority.command.CausationID != "run-id" ||
		result.OutputSnapshot["_subject_version"] != "2" {
		t.Fatalf(
			"Execute() result=%+v error=%v command=%+v",
			result, err, priority.command,
		)
	}

	_, err = executor.Execute(
		context.Background(), principal,
		Action{
			Kind: ActionAddComment,
			Parameters: map[string]string{
				"visibility": "internal", "body": "Investigating automatically.",
			},
		},
		snapshot,
	)
	if err != nil || commentService.command.Principal.ID != "automation-id" ||
		commentService.command.CausationID != "run-id" ||
		commentService.command.IdempotencyKey != automationCommentIdempotencyKey("run-id", "step-id") {
		t.Fatalf("comment command=%+v error=%v", commentService.command, err)
	}
}

func TestRuntimeCommentActionsRoutesInternalWithoutMentionInputsAndKeepsClientLegacy(t *testing.T) {
	public := &publicCommentActionsStub{}
	order := []string{}
	internal := &internalCommentActionsStub{order: &order}
	resolver := &commentActorResolverStub{actor: authorization.Principal{ID: "creator-id", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"}}, order: &order}
	actions := NewRuntimeCommentActions(public, internal, resolver)
	principal := authorization.Principal{ID: "automation-id", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"}, Capabilities: authorization.NewCapabilitySet("comment.internal.create")}

	id, err := actions.Create(context.Background(), CommentCommand{
		Principal: principal, WorkRecordID: "work-id", Visibility: comments.Internal,
		Body: "Plain @text", CausationID: "run-id", IdempotencyKey: "run-id:step-id:add_comment",
	})
	if err != nil || id != "internal-source-id" || resolver.input.ID != "automation-id" || internal.command.Principal.ID != "creator-id" ||
		len(order) != 2 || order[0] != "resolver" || order[1] != "internal" ||
		len(internal.command.Principal.Capabilities) != 0 ||
		internal.command.Parent.ID != "work-id" || internal.command.Source != "automation" ||
		internal.command.IdempotencyKey != "run-id:step-id:add_comment" ||
		internal.command.Tokens == nil || len(internal.command.Tokens) != 0 ||
		internal.command.ConfirmedTeamSnapshots == nil || len(internal.command.ConfirmedTeamSnapshots) != 0 {
		t.Fatalf("id=%q err=%v command=%+v", id, err, internal.command)
	}

	id, err = actions.Create(context.Background(), CommentCommand{
		Principal: principal, WorkRecordID: "work-id", Visibility: comments.ClientVisible,
		Body: "Customer update", CausationID: "run-id", IdempotencyKey: "run-id:step-2:add_comment",
	})
	if err != nil || id != "public-comment-id" || public.command.Visibility != comments.ClientVisible ||
		public.command.ActorType != "automation" || public.command.Source != "automation" ||
		public.command.CausationID != "run-id" {
		t.Fatalf("id=%q err=%v command=%+v", id, err, public.command)
	}
}

func TestRuntimeCommentActionsRequireExactDefinitionCapabilityBeforeResolvingCreator(t *testing.T) {
	for _, test := range []struct {
		name         string
		capabilities authorization.CapabilitySet
	}{
		{name: "empty capability set", capabilities: authorization.NewCapabilitySet()},
		{name: "unrelated capability", capabilities: authorization.NewCapabilitySet("comment.public.create")},
	} {
		t.Run(test.name, func(t *testing.T) {
			public := &publicCommentActionsStub{}
			internal := &internalCommentActionsStub{}
			resolver := &commentActorResolverStub{actor: authorization.Principal{ID: "creator-id", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"}}}
			actions := NewRuntimeCommentActions(public, internal, resolver)
			_, err := actions.Create(context.Background(), CommentCommand{
				Principal: authorization.Principal{
					ID: "definition-id", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
					Capabilities: test.capabilities,
				},
				WorkRecordID: "work-id", Visibility: comments.Internal, Body: "plain",
				CausationID: "run-id", IdempotencyKey: "request-id",
			})
			if !errors.Is(err, ErrActionFailed) || resolver.calls != 0 ||
				internal.command.Body != "" || public.command.Body != "" {
				t.Fatalf("err=%v resolver_calls=%d internal=%+v public=%+v", err, resolver.calls, internal.command, public.command)
			}
		})
	}
}

func TestRuntimeCommentActionsFailClosedWhenPersistedCreatorCannotResolve(t *testing.T) {
	public := &publicCommentActionsStub{}
	internal := &internalCommentActionsStub{}
	resolver := &commentActorResolverStub{err: scope.ErrNotFound}
	actions := NewRuntimeCommentActions(public, internal, resolver)
	_, err := actions.Create(context.Background(), CommentCommand{
		Principal: authorization.Principal{
			ID: "definition-id", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
			Capabilities: authorization.NewCapabilitySet("comment.internal.create"),
		},
		WorkRecordID: "work-id", Visibility: comments.Internal, Body: "plain",
		CausationID: "run-id", IdempotencyKey: "request-id",
	})
	if !errors.Is(err, scope.ErrNotFound) || resolver.calls != 1 || internal.command.Body != "" || public.command.Body != "" {
		t.Fatalf("err=%v internal=%+v public=%+v", err, internal.command, public.command)
	}
}

func TestRuntimeActionExecutorPassesAssignmentReasonToOrdinaryService(t *testing.T) {
	assignments := &assignmentActionsStub{}
	executor := NewRuntimeActionExecutor(
		&priorityActionsStub{}, assignments, &transitionActionsStub{},
		&commentActionsStub{}, &externalActionsStub{},
	)
	_, err := executor.Execute(
		context.Background(),
		authorization.Principal{
			ID:           "automation-id",
			Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
			Capabilities: authorization.NewCapabilitySet("work_record.assign"),
		},
		Action{Kind: ActionAssign, Parameters: map[string]string{
			"owner_id": "technician-id", "reason": "  Matched escalation policy  ",
		}},
		map[string]string{
			"_subject_type": "work_record", "_subject_id": "work-id",
			"_subject_version": "1", "_automation_run_id": "run-id",
		},
	)
	if err != nil || assignments.command.OwnerID != "technician-id" ||
		assignments.command.Reason != "Matched escalation policy" {
		t.Fatalf("assignment command=%+v error=%v", assignments.command, err)
	}
}

func TestPublishedLegacyAssignmentExecutesWithDeterministicAuditReason(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	repository := &legacyAssignmentRepository{current: workrecords.Record{
		Envelope: object.Envelope{
			ID: "work-id", ObjectType: "work_record", MSPID: "msp-id",
			ClientID: "client-alpha", Version: 4, LifecycleState: "active",
			CreatedBy: "creator-id", UpdatedBy: "creator-id",
		},
		PrimaryOwnerID: "previous-owner-id",
	}}
	assignmentService := workrecords.NewAssignmentService(
		repository,
		func() time.Time { return now },
		sequenceAutomationIDs("audit-id", "event-id", "correlation-id"),
	)
	executor := NewRuntimeActionExecutor(
		&priorityActionsStub{}, assignmentService, &transitionActionsStub{},
		&commentActionsStub{}, &externalActionsStub{},
	)
	store := &runStore{}
	engine := NewEngine(
		store, executor, &connectionGate{},
		func() time.Time { return now },
		sequenceAutomationIDs("run-id"),
	)
	definition := publishedDefinition(Action{
		Kind: ActionAssign,
		Parameters: map[string]string{
			"team_id": "team-id",
		},
	})
	definition.Capabilities = []string{"work_record.assign"}
	if err := Validate(definition); err != nil {
		t.Fatalf("legacy published definition no longer validates: %v", err)
	}

	result, err := engine.Run(context.Background(), RunCommand{
		Definition: definition,
		Event: TriggerEvent{
			ID: "event-id", Type: "work_record.created",
			MSPID: "msp-id", ClientID: "client-alpha",
			InputSnapshot: map[string]string{
				"_subject_type":    "work_record",
				"_subject_id":      "work-id",
				"_subject_version": "4",
			},
		},
		IdempotencyKey: "legacy-assignment-event",
		Attempt:        1,
		MaxAttempts:    3,
	})

	if err != nil {
		t.Fatalf("Run() legacy assignment error = %v", err)
	}
	if len(result.ChangedObjectIDs) != 1 ||
		result.ChangedObjectIDs[0] != "work-id" ||
		repository.accepted.Record.PrimaryOwnerID != "team-id" {
		t.Fatalf(
			"legacy assignment result=%+v mutation=%+v",
			result, repository.accepted,
		)
	}
	const wantReason = "Automation assignment by definition automation-id (run run-id)"
	if repository.accepted.Audit.Reason != wantReason ||
		repository.accepted.Audit.Action != "work_record.owner.changed" ||
		repository.accepted.Audit.ActorType != "automation" ||
		repository.accepted.Audit.Source != "automation" {
		t.Fatalf("legacy assignment audit=%+v", repository.accepted.Audit)
	}
}

func TestRuntimeActionExecutorKeepsExternalCallsBehindApprovedCaller(t *testing.T) {
	external := &externalActionsStub{}
	executor := NewRuntimeActionExecutor(
		&priorityActionsStub{}, &assignmentActionsStub{},
		&transitionActionsStub{}, &commentActionsStub{}, external,
	)
	principal := authorization.Principal{ID: "automation-id"}
	result, err := executor.Execute(
		context.Background(), principal,
		Action{
			Kind: ActionCallHTTP, ConnectionRef: "connection-id",
			Parameters: map[string]string{"request_template_id": "template-id"},
		},
		map[string]string{"safe": "value"},
	)
	if err != nil || external.ref != "connection-id" ||
		external.snapshot["safe"] != "value" ||
		len(result.ChangedObjectIDs) != 1 {
		t.Fatalf(
			"Execute() result=%+v error=%v external=%+v",
			result, err, external,
		)
	}
}

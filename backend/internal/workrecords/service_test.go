package workrecords

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/routing"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
)

type captureRepository struct {
	mutation         CreateMutation
	calls            int
	referencesErr    error
	displayIDErr     error
	checkedDisplayID string
}

func (r *captureRepository) EnsureDisplayIDAvailable(
	_ context.Context,
	_ scope.Target,
	displayID string,
) error {
	r.checkedDisplayID = displayID
	return r.displayIDErr
}

type workRecordClassificationRepository struct{}

func (workRecordClassificationRepository) ResolveTags(_ context.Context, _ string, ids []string) ([]tagging.Tag, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return []tagging.Tag{{ID: "meaningful", InternalKey: "network", State: tagging.StateActive}}, nil
}
func (workRecordClassificationRepository) FindUnclassified(_ context.Context, msp string) (tagging.Tag, error) {
	return tagging.Tag{ID: "unclassified", MSPID: msp, InternalKey: "unclassified", State: tagging.StateActive}, nil
}
func TestInitialTagsClassificationPolicyForWorkRecord(t *testing.T) {
	s := &Service{creation: tagging.NewCreationPreparer(workRecordClassificationRepository{})}
	target := scope.Target{MSPID: "msp", ClientID: "client"}
	if _, err := s.initialTags(context.Background(), target, CreateCommand{Actor: Actor{Source: "api"}, ClassificationPolicy: tagging.CreationRequireMeaningful}); !errors.Is(err, tagging.ErrMeaningfulTagRequired) {
		t.Fatalf("interactive error=%v", err)
	}
	_, err := s.initialTags(context.Background(), target, CreateCommand{Actor: Actor{Source: "integration"}, ClassificationPolicy: tagging.CreationAllowFallback})
	if !errors.Is(err, tagging.ErrInvalidAssociation) {
		t.Fatalf("untrusted fallback error=%v", err)
	}
}

func (r *captureRepository) ValidateReferences(
	context.Context,
	scope.Target,
	ContextReferences,
	time.Time,
) error {
	return r.referencesErr
}

type workflowSource struct {
	published []workflow.Published
	err       error
	target    scope.Target
}

type routingSource struct {
	ruleSet routing.RuleSet
	err     error
	mspID   string
}

type policySource struct {
	policies []sla.PublishedPolicy
	err      error
	target   scope.Target
}

func (s *policySource) ListPolicies(
	_ context.Context,
	target scope.Target,
) ([]sla.PublishedPolicy, error) {
	s.target = target
	return s.policies, s.err
}

func publishedSLAPolicies() []sla.PublishedPolicy {
	return []sla.PublishedPolicy{
		{
			ID: "contract-sla", Version: 3, Enabled: true,
			Priority: 100, StableOrder: 1,
			Conditions: sla.PolicyConditions{ContractID: "contract-id"},
			Calendar: sla.PublishedCalendar{
				ID: "calendar", Version: 2,
				Definition: sla.CalendarDefinition{
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
			},
			ResponseTargetSeconds: 3600, ResolutionTargetSeconds: 7200,
			WarningPercent: 80,
		},
		{
			ID: "fallback-sla", Version: 1, Enabled: true, Fallback: true,
			StableOrder: 2,
			Calendar: sla.PublishedCalendar{
				ID: "calendar", Version: 2,
				Definition: sla.CalendarDefinition{
					Timezone: "UTC",
					Weekly: map[string][]sla.Window{
						"monday": {{StartMinute: 0, EndMinute: 1440}},
					},
				},
			},
			ResponseTargetSeconds: 7200, ResolutionTargetSeconds: 14400,
			WarningPercent: 80,
		},
	}
}

func (s *routingSource) LoadCurrent(_ context.Context, mspID string) (routing.RuleSet, error) {
	s.mspID = mspID
	return s.ruleSet, s.err
}

func publishedRouting() routing.RuleSet {
	return routing.RuleSet{
		ID: "routing-set", MSPID: "msp-id", Version: 5,
		Rules: []routing.Rule{
			{ID: "critical-route", Position: 1, Priority: "high", QueueID: "noc-queue"},
			{ID: "fallback-route", Position: 2, QueueID: "triage-queue"},
		},
	}
}

func (s *workflowSource) ListPublished(_ context.Context, target scope.Target) ([]workflow.Published, error) {
	s.target = target
	return s.published, s.err
}

func publishedWorkflows() []workflow.Published {
	return []workflow.Published{
		{
			ID: "priority-workflow", Version: 3, Priority: 100, StableOrder: 1, Enabled: true,
			Conditions: workflow.Conditions{RecordType: "incident", Priority: "high"},
			Definition: workflow.Definition{
				States:      []workflow.State{{Key: "new"}, {Key: "resolved"}},
				Transitions: []workflow.Transition{{From: "new", To: "resolved"}},
			},
		},
		{
			ID: "fallback-workflow", Version: 7, Fallback: true, Enabled: true,
			Definition: workflow.Definition{States: []workflow.State{{Key: "new"}}},
		},
	}
}

func (r *captureRepository) CreateAtomic(_ context.Context, mutation CreateMutation) error {
	r.calls++
	r.mutation = mutation
	return nil
}

func TestCreateDerivesScopeAndProducesAuditedEvent(t *testing.T) {
	repository := &captureRepository{}
	routes := &routingSource{ruleSet: publishedRouting()}
	workflows := &workflowSource{published: publishedWorkflows()}
	policies := &policySource{policies: publishedSLAPolicies()}
	now := time.Date(2026, time.July, 29, 16, 0, 0, 0, time.UTC)
	ids := []string{"work-id", "sla-id", "audit-id", "event-id", "correlation-id"}
	service := NewService(repository, routes, workflows, policies, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}, tagging.NewCreationPreparer(workRecordClassificationRepository{}))
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("work_record.create"),
	}

	record, err := service.Create(context.Background(), CreateCommand{
		Principal: principal,
		Actor: Actor{
			Type: "technician", ID: "actor-id", Source: "web",
		},
		DisplayID: "INC-100", Type: Incident, Title: "Email unavailable",
		Status: "new", Priority: "high", ContractID: "contract-id",
		TagIDs: []string{"meaningful"}, ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if repository.calls != 1 || record.ID != "work-id" || record.ClientID != "client-id" {
		t.Fatalf("unexpected accepted record: %+v calls=%d", record, repository.calls)
	}
	if repository.mutation.Audit.Action != "work_record.created" ||
		repository.mutation.Event.EventType != "work_record.created" ||
		repository.mutation.Event.Data["priority"] != "high" ||
		repository.mutation.Event.Data["status"] != "new" {
		t.Fatal("accepted mutation did not produce matching audit and event facts")
	}
	if repository.mutation.Audit.CorrelationID != repository.mutation.Event.CorrelationID {
		t.Fatal("audit and event correlation IDs differ")
	}
	if workflows.target != (scope.Target{MSPID: "msp-id", ClientID: "client-id"}) {
		t.Fatalf("workflow lookup target = %+v", workflows.target)
	}
	if routes.mspID != "msp-id" || record.QueueID != "noc-queue" ||
		repository.mutation.Routing.RuleSetID != "routing-set" ||
		repository.mutation.Routing.RuleSetVersion != 5 ||
		repository.mutation.Routing.Decision.RuleID != "critical-route" {
		t.Fatalf("routing decision not applied and persisted: record=%+v routing=%+v", record, repository.mutation.Routing)
	}
	if repository.mutation.Workflow.WorkflowID != "priority-workflow" ||
		repository.mutation.Workflow.Version != 3 ||
		repository.mutation.Workflow.EvaluatedAt != now ||
		len(repository.mutation.Workflow.Trace) != 2 {
		t.Fatalf("workflow selection not persisted: %+v", repository.mutation.Workflow)
	}
	if policies.target != (scope.Target{MSPID: "msp-id", ClientID: "client-id"}) ||
		repository.mutation.SLA.ID != "sla-id" ||
		repository.mutation.SLA.PolicyID != "contract-sla" ||
		repository.mutation.SLA.PolicyVersion != 3 ||
		repository.mutation.SLA.CalendarVersion != 2 ||
		len(repository.mutation.SLA.SelectionTrace) != 2 ||
		repository.mutation.SLA.ResponseDueAt != now.Add(time.Hour) ||
		repository.mutation.SLA.ResolutionDueAt != now.Add(2*time.Hour) {
		t.Fatalf("SLA selection not applied: %+v", repository.mutation.SLA)
	}
}

func TestCreateRejectsInteractiveMissingTagsBeforePersistence(t *testing.T) {
	repository := &captureRepository{}
	service := NewService(repository, &routingSource{ruleSet: publishedRouting()}, &workflowSource{published: publishedWorkflows()}, &policySource{policies: publishedSLAPolicies()}, time.Now, func() string { return "id" }, tagging.NewCreationPreparer(workRecordClassificationRepository{}))
	_, err := service.Create(context.Background(), CreateCommand{
		Principal: authorization.Principal{Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"}, Capabilities: authorization.NewCapabilitySet("work_record.create")},
		Actor:     workrecordsActor(), DisplayID: "INC-1", Type: Incident, Title: "Email unavailable", Status: "new", Priority: "high", ContractID: "contract-id", ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if !errors.Is(err, tagging.ErrMeaningfulTagRequired) || repository.calls != 0 {
		t.Fatalf("Create() error=%v calls=%d", err, repository.calls)
	}
}

func workrecordsActor() Actor { return Actor{Type: "technician", ID: "actor-id", Source: "api"} }

func TestCreateUsesCallerSuppliedRecordIdentityForIdempotentIntake(t *testing.T) {
	repository := &captureRepository{}
	ids := []string{"sla-id", "audit-id", "event-id", "correlation-id"}
	service := NewService(
		repository,
		&routingSource{ruleSet: publishedRouting()},
		&workflowSource{published: publishedWorkflows()},
		&policySource{policies: publishedSLAPolicies()},
		time.Now,
		func() string {
			value := ids[0]
			ids = ids[1:]
			return value
		},
		tagging.NewCreationPreparer(workRecordClassificationRepository{}),
	)
	principal := authorization.Principal{
		Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet(
			"work_record.create",
		),
	}

	record, err := service.Create(
		context.Background(),
		CreateCommand{
			RecordID: "intake-stable-id", Principal: principal,
			Actor: Actor{
				Type: "integration", ID: "actor-id", Source: "datto",
			},
			DisplayID: "DATTO-ALERT1", Type: Incident,
			Title: "Gateway unavailable", Status: "new", Priority: "critical",
			TagIDs: []string{"meaningful"}, ClassificationPolicy: tagging.CreationRequireMeaningful,
		},
	)

	if err != nil || record.ID != "intake-stable-id" ||
		repository.mutation.Record.ID != "intake-stable-id" {
		t.Fatalf("Create() record=%+v error=%v", record, err)
	}
}

func TestCreateRejectsCrossClientTargetWithoutExistenceDisclosure(t *testing.T) {
	repository := &captureRepository{}
	service := NewService(repository, &routingSource{ruleSet: publishedRouting()}, &workflowSource{published: publishedWorkflows()}, &policySource{policies: publishedSLAPolicies()}, time.Now, func() string { return "unused" }, tagging.NewCreationPreparer(workRecordClassificationRepository{}))
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-alpha"},
		Capabilities: authorization.NewCapabilitySet("work_record.create"),
	}

	_, err := service.Create(context.Background(), CreateCommand{
		Principal: principal,
		Target:    scope.Target{MSPID: "msp-id", ClientID: "client-bravo"},
		Actor:     Actor{Type: "technician", ID: "actor-id", Source: "api"},
		DisplayID: "INC-200", Type: Incident, Title: "Other client",
		Status: "new", Priority: "normal",
	})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("Create() error = %v, want ErrNotFound", err)
	}
	if repository.calls != 0 {
		t.Fatal("cross-client mutation reached repository")
	}
}

func TestCreateRejectsUnsupportedRecordType(t *testing.T) {
	repository := &captureRepository{}
	service := NewService(repository, &routingSource{ruleSet: publishedRouting()}, &workflowSource{published: publishedWorkflows()}, &policySource{policies: publishedSLAPolicies()}, time.Now, func() string { return "unused" }, tagging.NewCreationPreparer(workRecordClassificationRepository{}))
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("work_record.create"),
	}
	_, err := service.Create(context.Background(), CreateCommand{
		Principal: principal,
		Actor:     Actor{Type: "technician", ID: "actor-id", Source: "web"},
		DisplayID: "WR-1", Type: Type("project"), Title: "Not work management",
		Status: "new", Priority: "normal",
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Create() error = %v, want ErrInvalid", err)
	}
}

func TestCreateRejectsStatusOutsideSelectedWorkflow(t *testing.T) {
	repository := &captureRepository{}
	service := NewService(
		repository,
		&routingSource{ruleSet: publishedRouting()},
		&workflowSource{published: publishedWorkflows()},
		&policySource{policies: publishedSLAPolicies()},
		time.Now,
		func() string { return "unused" },
		tagging.NewCreationPreparer(workRecordClassificationRepository{}),
	)
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("work_record.create"),
	}
	_, err := service.Create(context.Background(), CreateCommand{
		Principal: principal,
		Actor:     Actor{Type: "technician", ID: "actor-id", Source: "web"},
		DisplayID: "INC-201", Type: Incident, Title: "Invalid initial state",
		Status: "triage", Priority: "high", TagIDs: []string{"meaningful"}, ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Create() error = %v, want ErrInvalid", err)
	}
	if repository.calls != 0 {
		t.Fatal("invalid workflow state reached repository")
	}
}

func TestCreateRejectsInaccessibleSLAContextBeforeSelection(t *testing.T) {
	repository := &captureRepository{referencesErr: scope.ErrNotFound}
	routes := &routingSource{ruleSet: publishedRouting()}
	workflows := &workflowSource{published: publishedWorkflows()}
	policies := &policySource{policies: publishedSLAPolicies()}
	service := NewService(repository, routes, workflows, policies, time.Now, func() string {
		return "unused"
	}, tagging.NewCreationPreparer(workRecordClassificationRepository{}))
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("work_record.create"),
	}
	_, err := service.Create(context.Background(), CreateCommand{
		Principal: principal,
		Actor:     Actor{Type: "technician", ID: "actor-id", Source: "web"},
		DisplayID: "INC-202", Type: Incident, Title: "Invalid contract",
		Status: "new", Priority: "high", ContractID: "other-client-contract", TagIDs: []string{"meaningful"}, ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("Create() error = %v, want ErrNotFound", err)
	}
	if routes.mspID != "" || workflows.target.MSPID != "" || policies.target.MSPID != "" ||
		repository.calls != 0 {
		t.Fatal("invalid SLA context reached selection or persistence")
	}
}

func TestCreatePreflightAndCreateUseTheSameSelection(t *testing.T) {
	tests := []struct {
		name          string
		command       CreateCommand
		wantQueue     string
		wantWorkflow  string
		wantState     string
		wantSLAPolicy string
		wantCalendar  string
		wantService   string
		wantContract  string
	}{
		{
			name: "contract policy",
			command: CreateCommand{
				DisplayID: "INC-301", Type: Incident, Title: "Email unavailable",
				Status: "new", Priority: "high", ContractID: "contract-id",
			},
			wantQueue: "noc-queue", wantWorkflow: "priority-workflow", wantState: "new",
			wantSLAPolicy: "contract-sla", wantCalendar: "calendar", wantContract: "contract-id",
		},
		{
			name: "fallback policy",
			command: CreateCommand{
				DisplayID: "INC-302", Type: Incident, Title: "Printer unavailable",
				Status: "new", Priority: "normal", ServiceID: "service-id",
			},
			wantQueue: "triage-queue", wantWorkflow: "fallback-workflow", wantState: "new",
			wantSLAPolicy: "fallback-sla", wantCalendar: "calendar", wantService: "service-id",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &captureRepository{}
			ids := []string{"work-id", "sla-id", "audit-id", "event-id", "correlation-id"}
			service := NewService(
				repository,
				&routingSource{ruleSet: publishedRouting()},
				&workflowSource{published: publishedWorkflows()},
				&policySource{policies: publishedSLAPolicies()},
				func() time.Time { return time.Date(2026, time.July, 29, 16, 0, 0, 0, time.UTC) },
				func() string {
					id := ids[0]
					ids = ids[1:]
					return id
				},
			)
			command := test.command
			command.Principal = authorization.Principal{
				Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
				Capabilities: authorization.NewCapabilitySet("work_record.create"),
			}
			command.Actor = Actor{Type: "technician", ID: "actor-id", Source: "web"}
			command.ExpectedClientVersion = 11
			command.ExpectedServiceVersion = 13
			command.ExpectedContractVersion = 17

			preflight, err := service.PreflightCreate(context.Background(), command)
			if err != nil {
				t.Fatal(err)
			}
			if repository.calls != 0 {
				t.Fatal("preflight wrote a work record")
			}
			if preflight.Routing.Decision.QueueID != test.wantQueue ||
				preflight.Workflow.WorkflowID != test.wantWorkflow ||
				preflight.SLA.PolicyID != test.wantSLAPolicy ||
				preflight.SLA.CalendarID != test.wantCalendar ||
				preflight.ServiceID != test.wantService || preflight.ContractID != test.wantContract {
				t.Fatalf("preflight = %+v", preflight)
			}

			command.ExpectedSelection = &preflight.Fence
			record, err := service.Create(context.Background(), command)
			if err != nil || record.QueueID != preflight.Routing.Decision.QueueID ||
				record.Status != test.wantState || record.ServiceID != preflight.ServiceID ||
				record.ContractID != preflight.ContractID || repository.calls != 1 ||
				repository.mutation.Workflow.WorkflowID != preflight.Workflow.WorkflowID ||
				repository.mutation.SLA.PolicyID != preflight.SLA.PolicyID ||
				repository.mutation.SLA.CalendarID != preflight.SLA.CalendarID ||
				repository.mutation.ExpectedClientVersion != 11 ||
				repository.mutation.ExpectedServiceVersion != 13 ||
				repository.mutation.ExpectedContractVersion != 17 {
				t.Fatalf("create=%+v preflight=%+v mutation=%+v err=%v", record, preflight, repository.mutation, err)
			}
		})
	}
}

func TestCreateSelectionFenceRejectsDriftBeforeAtomicMutation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*routingSource, *workflowSource, *policySource)
		want   error
	}{
		{
			name: "rule set version", mutate: func(routes *routingSource, _ *workflowSource, _ *policySource) {
				routes.ruleSet.Version++
			}, want: object.ErrVersionConflict,
		},
		{
			name: "queue", mutate: func(routes *routingSource, _ *workflowSource, _ *policySource) {
				routes.ruleSet.Rules[0].QueueID = "other-queue"
			}, want: object.ErrVersionConflict,
		},
		{
			name: "workflow version", mutate: func(_ *routingSource, workflows *workflowSource, _ *policySource) {
				workflows.published[0].Version++
			}, want: object.ErrVersionConflict,
		},
		{
			name: "initial state", mutate: func(_ *routingSource, workflows *workflowSource, _ *policySource) {
				workflows.published[0].Definition.States = []workflow.State{{Key: "triage"}}
			}, want: ErrInvalid,
		},
		{
			name: "SLA policy", mutate: func(_ *routingSource, _ *workflowSource, policies *policySource) {
				policies.policies[0].Version++
			}, want: object.ErrVersionConflict,
		},
		{
			name: "SLA calendar", mutate: func(_ *routingSource, _ *workflowSource, policies *policySource) {
				policies.policies[0].Calendar.Version++
			}, want: object.ErrVersionConflict,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &captureRepository{}
			routes := &routingSource{ruleSet: publishedRouting()}
			workflows := &workflowSource{published: publishedWorkflows()}
			policies := &policySource{policies: publishedSLAPolicies()}
			service := NewService(repository, routes, workflows, policies, time.Now, func() string { return "unused" })
			command := authorizedCreateCommand()
			preflight, err := service.PreflightCreate(context.Background(), command)
			if err != nil {
				t.Fatal(err)
			}
			command.ExpectedSelection = &preflight.Fence
			test.mutate(routes, workflows, policies)

			if _, err := service.Create(context.Background(), command); !errors.Is(err, test.want) {
				t.Fatalf("Create() error = %v, want %v", err, test.want)
			}
			if repository.calls != 0 {
				t.Fatal("selection drift reached CreateAtomic")
			}
		})
	}
}

func TestCreatePreflightRejectsInvalidContextAndSetupWithoutWrites(t *testing.T) {
	tests := []struct {
		name          string
		repositoryErr error
		ruleSet       routing.RuleSet
		workflows     []workflow.Published
		policies      []sla.PublishedPolicy
		want          error
	}{
		{
			name: "invalid service or contract", repositoryErr: scope.ErrNotFound,
			ruleSet: publishedRouting(), workflows: publishedWorkflows(), policies: publishedSLAPolicies(), want: scope.ErrNotFound,
		},
		{
			name: "inactive client", repositoryErr: scope.ErrNotFound,
			ruleSet: publishedRouting(), workflows: publishedWorkflows(), policies: publishedSLAPolicies(), want: scope.ErrNotFound,
		},
		{
			name: "missing routing setup", ruleSet: routing.RuleSet{ID: "routing-set", Version: 1},
			workflows: publishedWorkflows(), policies: publishedSLAPolicies(), want: routing.ErrInvalidRules,
		},
		{
			name: "missing workflow setup", ruleSet: publishedRouting(),
			policies: publishedSLAPolicies(), want: workflow.ErrInvalidConfiguration,
		},
		{
			name: "missing SLA setup", ruleSet: publishedRouting(), workflows: publishedWorkflows(), want: sla.ErrInvalidPolicy,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &captureRepository{referencesErr: test.repositoryErr}
			service := NewService(
				repository,
				&routingSource{ruleSet: test.ruleSet},
				&workflowSource{published: test.workflows},
				&policySource{policies: test.policies},
				time.Now,
				func() string { return "unused" },
			)

			command := authorizedCreateCommand()
			if _, err := service.PreflightCreate(context.Background(), command); !errors.Is(err, test.want) {
				t.Fatalf("PreflightCreate() error = %v, want %v", err, test.want)
			}
			if repository.calls != 0 {
				t.Fatal("rejected preflight reached CreateAtomic")
			}
			if _, err := service.Create(context.Background(), command); !errors.Is(err, test.want) {
				t.Fatalf("Create() error = %v, want %v", err, test.want)
			}
			if repository.calls != 0 {
				t.Fatal("rejected create reached CreateAtomic")
			}
		})
	}
}

func TestPreflightCreateRejectsDuplicateDisplayIDBeforeCreateAtomic(t *testing.T) {
	repository := &captureRepository{displayIDErr: ErrDisplayIDConflict}
	service := NewService(
		repository,
		&routingSource{ruleSet: publishedRouting()},
		&workflowSource{published: publishedWorkflows()},
		&policySource{policies: publishedSLAPolicies()},
		time.Now,
		func() string { return "unused" },
	)
	command := authorizedCreateCommand()
	if _, err := service.PreflightCreate(context.Background(), command); !errors.Is(err, ErrDisplayIDConflict) {
		t.Fatalf("PreflightCreate() error=%v, want ErrDisplayIDConflict", err)
	}
	if repository.checkedDisplayID != command.DisplayID || repository.calls != 0 {
		t.Fatalf("checked display ID=%q CreateAtomic calls=%d", repository.checkedDisplayID, repository.calls)
	}
}

func authorizedCreateCommand() CreateCommand {
	return CreateCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
			Capabilities: authorization.NewCapabilitySet("work_record.create"),
		},
		Actor:     Actor{Type: "technician", ID: "actor-id", Source: "web"},
		DisplayID: "INC-303", Type: Incident, Title: "Email unavailable",
		Status: "new", Priority: "high", ServiceID: "service-id", ContractID: "contract-id",
	}
}

package rtitools

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type operationalSalesWriteStub struct {
	opportunity sales.Opportunity
	matches     []sales.Opportunity
	stages      map[string][]sales.PipelineStage
	pipeline    sales.Pipeline
	transition  sales.TransitionCommand
	activity    sales.CreateOpportunityActivityCommand
	writes      int
}

func (s *operationalSalesWriteStub) ListPipelines(
	context.Context,
	authorization.Principal,
) ([]sales.Pipeline, error) {
	return []sales.Pipeline{s.pipeline}, nil
}

func (s *operationalSalesWriteStub) ResolveOpportunityReference(
	context.Context,
	authorization.Principal,
	scope.Target,
	string,
	int,
) ([]sales.Opportunity, error) {
	return s.matches, nil
}

func (s *operationalSalesWriteStub) GetOpportunityInTarget(
	context.Context,
	authorization.Principal,
	scope.Target,
	sales.OpportunityID,
) (sales.Opportunity, error) {
	return s.opportunity, nil
}

func (s *operationalSalesWriteStub) ResolveStageReference(
	_ context.Context,
	_ authorization.Principal,
	_ scope.Target,
	_ sales.OpportunityID,
	reference string,
	_ int,
) ([]sales.PipelineStage, error) {
	return s.stages[reference], nil
}

func (s *operationalSalesWriteStub) TransitionOpportunity(
	_ context.Context,
	command sales.TransitionCommand,
) (sales.Opportunity, error) {
	s.writes++
	s.transition = command
	updated := s.opportunity
	updated.StageID = command.StageID
	updated.Version++
	return updated, nil
}

func (s *operationalSalesWriteStub) CreateOpportunityActivity(
	_ context.Context,
	command sales.CreateOpportunityActivityCommand,
) (sales.OpportunityActivity, error) {
	s.writes++
	s.activity = command
	return sales.OpportunityActivity{
		ID: "activity-1", OpportunityID: command.OpportunityID,
		Kind: command.Kind, Summary: command.Summary, Details: command.Details,
	}, nil
}

func opportunityWriteFixture() *operationalSalesWriteStub {
	opportunity := sales.Opportunity{
		ID: "opportunity-1", MSPID: "msp-1", ClientID: "client-1",
		PipelineID: "pipeline-1", StageID: "discovery", DisplayID: "OPP-2042",
		Name: "Northwind onboarding", Version: 3,
		Fields: map[sales.FieldKey]string{"expected_close_on": "2026-09-30"},
	}
	current := sales.PipelineStage{
		ID: "discovery", PipelineID: "pipeline-1", Key: "discovery",
		Name: "Discovery", AllowedNext: []sales.PipelineStageID{"qualified"}, Version: 6,
	}
	destination := sales.PipelineStage{
		ID: "qualified", PipelineID: "pipeline-1", Key: "qualified",
		Name: "Qualified", RequiredFields: []sales.FieldKey{"expected_close_on"}, Version: 7,
	}
	return &operationalSalesWriteStub{
		opportunity: opportunity,
		matches:     []sales.Opportunity{opportunity},
		stages: map[string][]sales.PipelineStage{
			"discovery": {current},
			"Qualified": {destination},
		},
		pipeline: sales.Pipeline{
			ID: "pipeline-1", MSPID: "msp-1", Key: "default", Name: "Default",
			Stages: []sales.PipelineStage{current, destination}, Version: 5,
		},
	}
}

func opportunityWriteDirectory() *resourceDirectoryStub {
	directory := activeResourceDirectory()
	directory.resolved.Version = 4
	return directory
}

func opportunityWritePrincipal(capability string) authorization.Principal {
	return authorization.Principal{
		ID: "technician-1", Scope: scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet(capability, "opportunity.read"),
	}
}

func opportunityWriteRegistry(
	t *testing.T,
	tool aiassist.Tool,
	principalLoader aiassist.PrincipalLoader,
) (*aiassist.Registry, *composedProposalStore) {
	t.Helper()
	store := &composedProposalStore{}
	ids := []string{"proposal-1", "correlation-1"}
	registry, err := aiassist.NewRegistry(
		[]aiassist.Tool{tool}, store, principalLoader,
		func() time.Time { return time.Date(2026, time.August, 6, 14, 0, 0, 0, time.UTC) },
		func() string {
			value := ids[0]
			ids = ids[1:]
			return value
		},
		&composedTargetAuthorizer{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return registry, store
}

func TestOpportunityTransitionToolPreparesCanonicalFactsPreviewsAndExecutesOrdinaryService(t *testing.T) {
	directory := opportunityWriteDirectory()
	actions := opportunityWriteFixture()
	tool := NewOpportunityTransitionTool(directory, actions)
	principal := opportunityWritePrincipal("opportunity.transition")
	registry, _ := opportunityWriteRegistry(t, tool, func(context.Context, authorization.Principal) (authorization.Principal, error) {
		return principal, nil
	})

	proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{
		Name: "opportunity.transition", ConversationID: "conversation-1",
		Input: json.RawMessage(`{
			"client":"Northwind Legal",
			"opportunity":"OPP-2042",
			"stage":"Qualified",
			"expected_version":3,
			"reason":"  Discovery completed  "
		}`),
	})
	if err != nil {
		t.Fatalf("Propose() error=%v", err)
	}
	if proposal.Preview.TargetID != "opportunity-1" || proposal.Preview.TargetVersion != 3 ||
		proposal.Preview.Changes["reason"].After != "Discovery completed" ||
		!reflect.DeepEqual(proposal.Preview.Changes["stage"].Before, map[string]any{
			"id": "discovery", "key": "discovery", "name": "Discovery", "version": int64(6),
		}) || !reflect.DeepEqual(proposal.Preview.Changes["stage"].After, map[string]any{
		"id": "qualified", "key": "qualified", "name": "Qualified", "version": int64(7),
	}) {
		t.Fatalf("preview=%+v", proposal.Preview)
	}
	result, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version)
	if err != nil {
		t.Fatalf("Confirm() error=%v", err)
	}
	if result.Data["version"] != int64(4) || actions.writes != 1 ||
		actions.transition.Target != (scope.Target{MSPID: "msp-1", ClientID: "client-1"}) ||
		actions.transition.ID != "opportunity-1" || actions.transition.StageID != "qualified" ||
		actions.transition.ExpectedVersion != 3 || actions.transition.Reason != "Discovery completed" ||
		actions.transition.ExpectedClientVersion != 4 ||
		actions.transition.ExpectedPipelineVersion != 5 ||
		actions.transition.ExpectedCurrentStageVersion != 6 ||
		actions.transition.ExpectedDestinationStageVersion != 7 ||
		actions.transition.ActorID != principal.ID || actions.transition.Source != "ai_workspace" {
		t.Fatalf("result=%+v command=%+v writes=%d", result, actions.transition, actions.writes)
	}
}

func TestOpportunityTransitionToolRejectsPublicStageInternalUUID(t *testing.T) {
	const stageID = sales.PipelineStageID("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	actions := opportunityWriteFixture()
	current := actions.stages["discovery"][0]
	current.AllowedNext = []sales.PipelineStageID{stageID}
	actions.stages["discovery"] = []sales.PipelineStage{current}
	destination := actions.stages["Qualified"][0]
	destination.ID = stageID
	actions.stages[string(stageID)] = []sales.PipelineStage{destination}
	_, err := NewOpportunityTransitionTool(
		opportunityWriteDirectory(), actions,
	).(aiassist.ToolInputPreparer).Prepare(
		context.Background(), opportunityWritePrincipal("opportunity.transition"),
		json.RawMessage(`{"client":"Northwind Legal","opportunity":"OPP-2042","stage":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","expected_version":3,"reason":"Discovery completed"}`),
	)
	if !errors.Is(err, aiassist.ErrInvalidTool) || actions.writes != 0 {
		t.Fatalf("Prepare(internal stage UUID) error=%v writes=%d", err, actions.writes)
	}
}

func TestOpportunityActivityToolPreparesCanonicalParentAndPreviewsExactContents(t *testing.T) {
	directory := opportunityWriteDirectory()
	actions := opportunityWriteFixture()
	tool := NewOpportunityActivityCreateTool(directory, actions)
	principal := opportunityWritePrincipal("opportunity.activity.create")
	registry, _ := opportunityWriteRegistry(t, tool, func(context.Context, authorization.Principal) (authorization.Principal, error) {
		return principal, nil
	})

	proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{
		Name: "opportunity.activity.create", ConversationID: "conversation-1",
		Input: json.RawMessage(`{
			"client":"Northwind Legal",
			"opportunity":"OPP-2042",
			"kind":"call",
			"summary":"Reviewed onboarding scope",
			"details":"Customer confirmed the supplied milestones"
		}`),
	})
	if err != nil {
		t.Fatalf("Propose() error=%v", err)
	}
	if proposal.Preview.TargetID != "opportunity-1" || proposal.Preview.TargetVersion != 3 ||
		proposal.Preview.Changes["kind"].After != "call" ||
		proposal.Preview.Changes["summary"].After != "Reviewed onboarding scope" ||
		proposal.Preview.Changes["details"].After != "Customer confirmed the supplied milestones" {
		t.Fatalf("preview=%+v", proposal.Preview)
	}
	if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); err != nil {
		t.Fatalf("Confirm() error=%v", err)
	}
	if actions.writes != 1 || actions.activity.Target != (scope.Target{MSPID: "msp-1", ClientID: "client-1"}) ||
		actions.activity.OpportunityID != "opportunity-1" || actions.activity.Kind != "call" ||
		actions.activity.ExpectedClientVersion != 4 ||
		actions.activity.ExpectedOpportunityVersion != 3 ||
		actions.activity.ExpectedPipelineVersion != 5 ||
		actions.activity.ExpectedStageVersion != 6 ||
		actions.activity.Summary != "Reviewed onboarding scope" ||
		actions.activity.Details != "Customer confirmed the supplied milestones" ||
		!actions.activity.OccurredAt.IsZero() || actions.activity.ActorID != principal.ID ||
		actions.activity.Source != "ai_workspace" {
		t.Fatalf("command=%+v writes=%d", actions.activity, actions.writes)
	}
}

func TestOpportunityTransitionToolRejectsNoInventionAndProhibitedActions(t *testing.T) {
	transition := NewOpportunityTransitionTool(opportunityWriteDirectory(), opportunityWriteFixture())
	activity := NewOpportunityActivityCreateTool(opportunityWriteDirectory(), opportunityWriteFixture())
	for name, test := range map[string]struct {
		tool aiassist.Tool
		raw  string
	}{
		"missing stage":        {transition, `{"client":"Northwind Legal","opportunity":"OPP-2042","expected_version":3,"reason":"Reviewed"}`},
		"missing reason":       {transition, `{"client":"Northwind Legal","opportunity":"OPP-2042","stage":"Qualified","expected_version":3}`},
		"trusted stage id":     {transition, `{"client":"Northwind Legal","opportunity":"OPP-2042","stage":"Qualified","stage_id":"qualified","expected_version":3,"reason":"Reviewed"}`},
		"amount":               {transition, `{"client":"Northwind Legal","opportunity":"OPP-2042","stage":"Qualified","expected_version":3,"reason":"Reviewed","amount_minor":10}`},
		"probability":          {transition, `{"client":"Northwind Legal","opportunity":"OPP-2042","stage":"Qualified","expected_version":3,"reason":"Reviewed","probability":80}`},
		"custom fields":        {transition, `{"client":"Northwind Legal","opportunity":"OPP-2042","stage":"Qualified","expected_version":3,"reason":"Reviewed","custom_fields":{"approval":"yes"}}`},
		"conversion language":  {transition, `{"client":"Northwind Legal","opportunity":"OPP-2042","stage":"Qualified","expected_version":3,"reason":"Convert this opportunity to a project"}`},
		"high impact language": {transition, `{"client":"Northwind Legal","opportunity":"OPP-2042","stage":"Qualified","expected_version":3,"reason":"Override amount and accept contract"}`},
		"missing kind":         {activity, `{"client":"Northwind Legal","opportunity":"OPP-2042","summary":"Reviewed","details":"Exact details"}`},
		"missing summary":      {activity, `{"client":"Northwind Legal","opportunity":"OPP-2042","kind":"call","details":"Exact details"}`},
		"missing details":      {activity, `{"client":"Northwind Legal","opportunity":"OPP-2042","kind":"call","summary":"Reviewed"}`},
		"activity conversion":  {activity, `{"client":"Northwind Legal","opportunity":"OPP-2042","kind":"note","summary":"Convert opportunity","details":"Create the project"}`},
	} {
		t.Run(name, func(t *testing.T) {
			principal := opportunityWritePrincipal("opportunity.transition")
			if test.tool == activity {
				principal = opportunityWritePrincipal("opportunity.activity.create")
			}
			_, err := test.tool.(aiassist.ToolInputPreparer).Prepare(
				context.Background(), principal, json.RawMessage(test.raw),
			)
			if !errors.Is(err, aiassist.ErrInvalidTool) {
				t.Fatalf("Prepare() error=%v, want ErrInvalidTool", err)
			}
		})
	}
}

func TestOpportunityTransitionToolRejectsAmbiguityAndConfirmationDriftWithoutWrite(t *testing.T) {
	for _, test := range []struct {
		name  string
		apply func(*resourceDirectoryStub, *operationalSalesWriteStub, *authorization.Principal)
	}{
		{name: "inactive client", apply: func(directory *resourceDirectoryStub, _ *operationalSalesWriteStub, _ *authorization.Principal) {
			directory.err = scope.ErrNotFound
		}},
		{name: "client version", apply: func(directory *resourceDirectoryStub, _ *operationalSalesWriteStub, _ *authorization.Principal) {
			directory.resolved.Version++
		}},
		{name: "opportunity version", apply: func(_ *resourceDirectoryStub, actions *operationalSalesWriteStub, _ *authorization.Principal) {
			actions.opportunity.Version++
		}},
		{name: "pipeline version", apply: func(_ *resourceDirectoryStub, actions *operationalSalesWriteStub, _ *authorization.Principal) {
			actions.pipeline.Version++
		}},
		{name: "stage eligibility", apply: func(_ *resourceDirectoryStub, actions *operationalSalesWriteStub, _ *authorization.Principal) {
			stage := actions.stages["discovery"][0]
			stage.AllowedNext = nil
			actions.stages["discovery"] = []sales.PipelineStage{stage}
		}},
		{name: "current stage version", apply: func(_ *resourceDirectoryStub, actions *operationalSalesWriteStub, _ *authorization.Principal) {
			stage := actions.stages["discovery"][0]
			stage.Version++
			actions.stages["discovery"] = []sales.PipelineStage{stage}
		}},
		{name: "destination stage version", apply: func(_ *resourceDirectoryStub, actions *operationalSalesWriteStub, _ *authorization.Principal) {
			stage := actions.stages["Qualified"][0]
			stage.Version++
			actions.stages["Qualified"] = []sales.PipelineStage{stage}
		}},
		{name: "authorization", apply: func(_ *resourceDirectoryStub, _ *operationalSalesWriteStub, principal *authorization.Principal) {
			principal.Capabilities = authorization.NewCapabilitySet("opportunity.read")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := opportunityWriteDirectory()
			actions := opportunityWriteFixture()
			tool := NewOpportunityTransitionTool(directory, actions)
			principal := opportunityWritePrincipal("opportunity.transition")
			loaded := principal
			registry, _ := opportunityWriteRegistry(t, tool, func(context.Context, authorization.Principal) (authorization.Principal, error) {
				return loaded, nil
			})
			proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{
				Name: "opportunity.transition", ConversationID: "conversation-1",
				Input: json.RawMessage(`{"client":"Northwind Legal","opportunity":"OPP-2042","stage":"Qualified","expected_version":3,"reason":"Discovery completed"}`),
			})
			if err != nil {
				t.Fatal(err)
			}
			test.apply(directory, actions, &loaded)
			if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); err == nil || actions.writes != 0 {
				t.Fatalf("Confirm() error=%v writes=%d", err, actions.writes)
			}
		})
	}

	ambiguous := opportunityWriteFixture()
	ambiguous.matches = append(ambiguous.matches, ambiguous.opportunity)
	tool := NewOpportunityTransitionTool(opportunityWriteDirectory(), ambiguous)
	_, err := tool.(aiassist.ToolInputPreparer).Prepare(
		context.Background(), opportunityWritePrincipal("opportunity.transition"),
		json.RawMessage(`{"client":"Northwind Legal","opportunity":"OPP-2042","stage":"Qualified","expected_version":3,"reason":"Discovery completed"}`),
	)
	if !errors.Is(err, sales.ErrAmbiguousReference) || ambiguous.writes != 0 {
		t.Fatalf("Prepare() error=%v writes=%d", err, ambiguous.writes)
	}
}

func TestOpportunityActivityToolRejectsStaleParentAtConfirmationWithoutWrite(t *testing.T) {
	directory := opportunityWriteDirectory()
	actions := opportunityWriteFixture()
	tool := NewOpportunityActivityCreateTool(directory, actions)
	principal := opportunityWritePrincipal("opportunity.activity.create")
	registry, _ := opportunityWriteRegistry(t, tool, func(context.Context, authorization.Principal) (authorization.Principal, error) {
		return principal, nil
	})
	proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{
		Name: "opportunity.activity.create", ConversationID: "conversation-1",
		Input: json.RawMessage(`{"client":"Northwind Legal","opportunity":"OPP-2042","kind":"call","summary":"Reviewed onboarding scope","details":"Customer confirmed the supplied milestones"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	actions.opportunity.Version++
	if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); !errors.Is(err, object.ErrVersionConflict) || actions.writes != 0 {
		t.Fatalf("Confirm() error=%v writes=%d", err, actions.writes)
	}
}

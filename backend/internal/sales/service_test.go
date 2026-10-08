package sales

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type fakeRepository struct {
	opportunity   Opportunity
	foundTarget   scope.Target
	listTarget    scope.Target
	reference     string
	limit         int
	resolved      []Opportunity
	resolvedStage []PipelineStage
	stages        map[string]PipelineStage
	accepted      TransitionMutation
	fields        ReplaceOpportunityFieldsMutation
	participants  ReplaceOpportunityParticipantsMutation
	created       CreateOpportunityMutation
	pipeline      CreatePipelineMutation
	activity      CreateOpportunityActivityMutation
	activities    []OpportunityActivity
	forecast      []ForecastBucket
	pipelines     []Pipeline
	prospects     []Prospect
	prospect      CreateProspectMutation
	listed        []Opportunity
	calls         int
}

func (r *fakeRepository) FindOpportunity(_ context.Context, target scope.Target, _ OpportunityID) (Opportunity, error) {
	r.foundTarget = target
	return r.opportunity, nil
}
func (r *fakeRepository) ListOpportunities(_ context.Context, target scope.Target, _ OpportunityListFilter) ([]Opportunity, error) {
	r.listTarget = target
	return r.listed, nil
}
func (r *fakeRepository) FindOpportunitiesByReference(_ context.Context, target scope.Target, reference string, limit int) ([]Opportunity, error) {
	r.foundTarget, r.reference, r.limit = target, reference, limit
	return r.resolved, nil
}
func (r *fakeRepository) FindStagesByReference(_ context.Context, target scope.Target, _ OpportunityID, reference string, limit int) ([]PipelineStage, error) {
	r.foundTarget, r.reference, r.limit = target, reference, limit
	return r.resolvedStage, nil
}
func (r *fakeRepository) FindStage(_ context.Context, _ string, id PipelineStageID) (PipelineStage, error) {
	stage, ok := r.stages[string(id)]
	if !ok {
		return PipelineStage{}, ErrStageNotFound
	}
	return stage, nil
}
func (r *fakeRepository) TransitionAtomic(_ context.Context, mutation TransitionMutation) error {
	r.calls++
	r.accepted = mutation
	return nil
}
func (r *fakeRepository) ReplaceOpportunityFieldsAtomic(_ context.Context, mutation ReplaceOpportunityFieldsMutation) error {
	r.fields = mutation
	return nil
}
func (r *fakeRepository) ReplaceOpportunityParticipantsAtomic(_ context.Context, mutation ReplaceOpportunityParticipantsMutation) error {
	r.participants = mutation
	return nil
}
func (r *fakeRepository) CreateProspectAtomic(_ context.Context, mutation CreateProspectMutation) error {
	r.prospect = mutation
	return nil
}
func (r *fakeRepository) ListProspects(_ context.Context, _ string, _ int) ([]Prospect, error) {
	return r.prospects, nil
}
func (r *fakeRepository) FindProspectsByReference(_ context.Context, _ string, reference string, limit int) ([]Prospect, error) {
	result := make([]Prospect, 0, limit)
	for _, prospect := range r.prospects {
		if ProspectReferenceMatches(reference, prospect.Name, prospect.DisplayID) {
			result = append(result, prospect)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}
func (r *fakeRepository) CreatePipelineAtomic(_ context.Context, mutation CreatePipelineMutation) error {
	r.pipeline = mutation
	return nil
}
func (r *fakeRepository) ListPipelines(_ context.Context, _ string) ([]Pipeline, error) {
	return r.pipelines, nil
}
func (r *fakeRepository) CreateOpportunityAtomic(_ context.Context, mutation CreateOpportunityMutation) error {
	r.created = mutation
	return nil
}
func (r *fakeRepository) CreateOpportunityActivityAtomic(_ context.Context, mutation CreateOpportunityActivityMutation) error {
	r.activity = mutation
	return nil
}
func (r *fakeRepository) ListOpportunityActivities(_ context.Context, _ scope.Target, _ OpportunityID, _ int) ([]OpportunityActivity, error) {
	return r.activities, nil
}
func (r *fakeRepository) Forecast(_ context.Context, _ scope.Target, _ string) ([]ForecastBucket, error) {
	return r.forecast, nil
}

func TestForecastRequiresReadCapabilityAndReturnsRepositoryBuckets(t *testing.T) {
	repository := &fakeRepository{forecast: []ForecastBucket{{
		PipelineID: "pipeline-id", StageID: "stage-id",
		OpportunityCount: 2, Amount: Money{Minor: 10000, Currency: "USD"},
	}}}
	service := NewService(repository, time.Now, func() string { return "id" })
	principal := salesPrincipal("opportunity.read")
	buckets, err := service.Forecast(context.Background(), principal, "pipeline-id")
	if err != nil || len(buckets) != 1 || buckets[0].OpportunityCount != 2 {
		t.Fatalf("Forecast() buckets=%+v error=%v", buckets, err)
	}
}

func TestProspectCreateAcceptsTrustedPreparedIdentityCorrelationAndOmittedContacts(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, func() time.Time {
		return time.Date(2026, time.August, 6, 14, 0, 0, 0, time.UTC)
	}, func() string { return "generated-id" })
	principal := salesPrincipal("prospect.create")
	principal.Scope.ClientID = ""
	principal.ID = "actor-id"

	prospect, err := service.CreateProspect(context.Background(), CreateProspectCommand{
		Principal: principal, ProspectID: "prepared-prospect", DisplayID: "PRO-200",
		Name: "Exact Prospect", ActorID: principal.ID, Source: "ai_workspace",
		CorrelationID: "proposal-correlation",
	})
	if err != nil {
		t.Fatalf("CreateProspect() error=%v", err)
	}
	if prospect.ID != "prepared-prospect" || prospect.Email != "" || prospect.Phone != "" ||
		repository.prospect.Audit.CorrelationID != "proposal-correlation" ||
		repository.prospect.Event.CorrelationID != "proposal-correlation" ||
		repository.prospect.Audit.Source != "ai_workspace" {
		t.Fatalf("prospect=%+v mutation=%+v", prospect, repository.prospect)
	}
}

func TestProspectCreatePreflightUsesBoundedExactMSPIdentity(t *testing.T) {
	repository := &fakeRepository{prospects: []Prospect{
		{ID: "one", MSPID: "msp-id", DisplayID: "PRO-200", Name: "Alpha"},
		{ID: "two", MSPID: "msp-id", DisplayID: "PRO-201", Name: "Alpha"},
	}}
	service := NewService(repository, time.Now, func() string { return "id" })
	principal := salesPrincipal("prospect.create")
	principal.Scope.ClientID = ""

	matches, err := service.ResolveProspectReference(
		context.Background(), principal, scope.Target{MSPID: "msp-id"}, " Alpha ",
	)
	if err != nil || len(matches) != 2 {
		t.Fatalf("matches=%+v error=%v", matches, err)
	}
}

func TestListPipelinesRequiresMSPAdministrationScope(t *testing.T) {
	repository := &fakeRepository{pipelines: []Pipeline{{ID: "pipeline-id"}}}
	service := NewService(repository, time.Now, func() string { return "id" })

	principal := salesPrincipal("opportunity.read")
	principal.Scope.ClientID = ""
	found, err := service.ListPipelines(context.Background(), principal)
	if err != nil || len(found) != 1 {
		t.Fatalf("ListPipelines() found=%+v error=%v", found, err)
	}
	principal.Scope.ClientID = "client-id"
	if _, err := service.ListPipelines(context.Background(), principal); !errors.Is(err, ErrInvalidSalesRecord) {
		t.Fatalf("client-scoped ListPipelines() error=%v", err)
	}
}

func TestListProspectsRequiresMSPSalesReadScope(t *testing.T) {
	repository := &fakeRepository{prospects: []Prospect{{ID: "prospect-id"}}}
	service := NewService(repository, time.Now, func() string { return "id" })
	principal := salesPrincipal("opportunity.read")
	principal.Scope.ClientID = ""
	found, err := service.ListProspects(context.Background(), principal, 1000)
	if err != nil || len(found) != 1 {
		t.Fatalf("ListProspects() found=%+v error=%v", found, err)
	}
	principal.Scope.ClientID = "client-id"
	if _, err := service.ListProspects(context.Background(), principal, 100); !errors.Is(err, ErrInvalidSalesRecord) {
		t.Fatalf("client-scoped ListProspects() error=%v", err)
	}
}

func TestListOpportunitiesRequiresPairedStableCursor(t *testing.T) {
	repository := &fakeRepository{listed: []Opportunity{{ID: "opportunity-id"}}}
	service := NewService(repository, time.Now, func() string { return "id" })
	principal := salesPrincipal("opportunity.read")
	found, err := service.ListOpportunities(context.Background(), principal, OpportunityListFilter{Limit: 250})
	if err != nil || len(found) != 1 {
		t.Fatalf("ListOpportunities() found=%+v error=%v", found, err)
	}
	_, err = service.ListOpportunities(context.Background(), principal, OpportunityListFilter{
		BeforeID: "opportunity-id",
	})
	if !errors.Is(err, ErrInvalidSalesRecord) {
		t.Fatalf("unpaired cursor error=%v", err)
	}
}

func TestOpportunityReadsInTargetUseExplicitAuthorizedClientWithoutMutatingPrincipal(t *testing.T) {
	repository := &fakeRepository{
		opportunity: Opportunity{ID: "opportunity-id"},
		listed:      []Opportunity{{ID: "opportunity-id"}},
	}
	service := NewService(repository, time.Now, func() string { return "id" })
	principal := salesPrincipal("opportunity.read")
	principal.Scope.ClientID = ""
	target := scope.Target{MSPID: "msp-id", ClientID: "client-id"}

	if _, err := service.GetOpportunityInTarget(
		context.Background(), principal, target, "opportunity-id",
	); err != nil {
		t.Fatalf("GetOpportunityInTarget() error=%v", err)
	}
	if repository.foundTarget != target || principal.Scope.ClientID != "" {
		t.Fatalf("target=%+v principal=%+v", repository.foundTarget, principal.Scope)
	}
	if _, err := service.ListOpportunitiesInTarget(
		context.Background(), principal, target, OpportunityListFilter{Limit: 25},
	); err != nil {
		t.Fatalf("ListOpportunitiesInTarget() error=%v", err)
	}
	if repository.listTarget != target || principal.Scope.ClientID != "" {
		t.Fatalf("target=%+v principal=%+v", repository.listTarget, principal.Scope)
	}
}

func TestOpportunityReadsInTargetDoNotLetClientPrincipalEscape(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, time.Now, func() string { return "id" })
	principal := salesPrincipal("opportunity.read")

	for name, call := range map[string]func() error{
		"get": func() error {
			_, err := service.GetOpportunityInTarget(
				context.Background(), principal,
				scope.Target{MSPID: "msp-id", ClientID: "other-client"},
				"opportunity-id",
			)
			return err
		},
		"list": func() error {
			_, err := service.ListOpportunitiesInTarget(
				context.Background(), principal,
				scope.Target{MSPID: "msp-id", ClientID: "other-client"},
				OpportunityListFilter{Limit: 25},
			)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, scope.ErrNotFound) {
				t.Fatalf("error=%v, want scope.ErrNotFound", err)
			}
		})
	}
	if repository.foundTarget != (scope.Target{}) ||
		repository.listTarget != (scope.Target{}) {
		t.Fatalf("cross-client read reached repository: %+v", repository)
	}
}

func TestOpportunityResolversAreExactBoundedAndTargetAuthorized(t *testing.T) {
	repository := &fakeRepository{
		resolved:      []Opportunity{{ID: "opportunity-id"}},
		resolvedStage: []PipelineStage{{ID: "qualified", Name: "Qualified"}},
	}
	service := NewService(repository, time.Now, func() string { return "id" })
	principal := salesPrincipal("opportunity.read")
	principal.Scope.ClientID = ""
	target := scope.Target{MSPID: "msp-id", ClientID: "client-id"}

	found, err := service.ResolveOpportunityReference(
		context.Background(), principal, target, "  OPP-2042  ", 2,
	)
	if err != nil || len(found) != 1 {
		t.Fatalf("ResolveOpportunityReference() found=%+v error=%v", found, err)
	}
	if repository.foundTarget != target || repository.reference != "OPP-2042" ||
		repository.limit != 2 || principal.Scope.ClientID != "" {
		t.Fatalf("target=%+v reference=%q limit=%d principal=%+v",
			repository.foundTarget, repository.reference, repository.limit, principal.Scope)
	}

	stages, err := service.ResolveStageReference(
		context.Background(), principal, target, "opportunity-id", " Qualified ", 2,
	)
	if err != nil || len(stages) != 1 || repository.reference != "Qualified" {
		t.Fatalf("ResolveStageReference() stages=%+v reference=%q error=%v",
			stages, repository.reference, err)
	}

	clientPrincipal := salesPrincipal("opportunity.read")
	if _, err := service.ResolveOpportunityReference(
		context.Background(), clientPrincipal,
		scope.Target{MSPID: "msp-id", ClientID: "other-client"}, "OPP-2042", 2,
	); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-client resolver error=%v, want scope.ErrNotFound", err)
	}
	for _, limit := range []int{0, 3} {
		if _, err := service.ResolveStageReference(
			context.Background(), principal, target, "opportunity-id", "Qualified", limit,
		); !errors.Is(err, ErrInvalidSalesRecord) {
			t.Fatalf("limit %d error=%v, want ErrInvalidSalesRecord", limit, err)
		}
	}
}

func TestCreateOpportunityActivityValidatesParentScopeAndWritesFacts(t *testing.T) {
	repository := &fakeRepository{opportunity: Opportunity{
		ID: "opportunity-id", MSPID: "msp-id", ClientID: "client-id",
		PipelineID: "pipeline-id", StageID: "stage-id", Version: 8,
	}, stages: map[string]PipelineStage{
		"stage-id": {ID: "stage-id", PipelineID: "pipeline-id", Key: "qualified", Name: "Qualified", Version: 6},
	}, pipelines: []Pipeline{{
		ID: "pipeline-id", MSPID: "msp-id", Key: "default", Name: "Default", Version: 5,
	}}}
	at := time.Date(2026, time.July, 30, 18, 0, 0, 0, time.UTC)
	service := NewService(repository, func() time.Time { return at }, sequenceSalesIDs(
		"activity-id", "audit-id", "event-id", "correlation-id",
	))
	activity, err := service.CreateOpportunityActivity(context.Background(), CreateOpportunityActivityCommand{
		Principal:     salesMSPPrincipal("opportunity.activity.create"),
		Target:        scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		OpportunityID: "opportunity-id", Kind: "call",
		Summary: "Discovery call", Details: "Confirmed rollout scope",
		ExpectedClientVersion: 4, ExpectedOpportunityVersion: 8,
		ExpectedPipelineVersion: 5, ExpectedStageVersion: 6,
		OccurredAt: at.Add(-time.Hour), ActorID: "actor-id", Source: "api",
	})
	if err != nil || activity.ID != "activity-id" {
		t.Fatalf("CreateOpportunityActivity() activity=%+v error=%v", activity, err)
	}
	if repository.activity.Audit.Action != "opportunity.activity.created" ||
		repository.activity.Event.EventType != "opportunity.activity.created" ||
		repository.activity.Activity.OpportunityID != "opportunity-id" ||
		repository.activity.ExpectedClientVersion != 4 ||
		repository.activity.ExpectedOpportunityVersion != 8 ||
		repository.activity.Pipeline.Version != 5 || repository.activity.Stage.Version != 6 ||
		repository.foundTarget != (scope.Target{MSPID: "msp-id", ClientID: "client-id"}) {
		t.Fatalf("unexpected activity mutation: %+v", repository.activity)
	}
}

func TestReplaceOpportunityCustomFieldsIsScopedVersionedAndAudited(t *testing.T) {
	repository := &fakeRepository{opportunity: Opportunity{
		ID: "opportunity-id", MSPID: "msp-id", ClientID: "client-id",
		Version: 3, Fields: map[FieldKey]string{"name": "Network refresh"},
	}}
	at := time.Date(2026, time.July, 31, 20, 0, 0, 0, time.UTC)
	service := NewService(repository, func() time.Time { return at }, sequenceSalesIDs(
		"audit-id", "event-id", "correlation-id",
	))
	updated, err := service.ReplaceOpportunityCustomFields(
		context.Background(),
		ReplaceOpportunityCustomFieldsCommand{
			Principal: salesPrincipal("opportunity.update"),
			ID:        "opportunity-id", ExpectedVersion: 3,
			Fields: map[string]string{
				"procurement_reference": "PO pending",
				"risk_summary":          "Weekend cutover",
			},
			ActorID: "actor-id", Source: "api",
		},
	)
	if err != nil {
		t.Fatalf("ReplaceOpportunityCustomFields() error = %v", err)
	}
	if updated.Version != 4 ||
		updated.CustomFields["risk_summary"] != "Weekend cutover" ||
		repository.fields.Audit.Action != "opportunity.custom_fields.replaced" ||
		repository.fields.Event.EventType != "opportunity.custom_fields.replaced" {
		t.Fatalf("unexpected field mutation: %+v / %+v", updated, repository.fields)
	}
}

func TestReplaceOpportunityCustomFieldsRejectsReservedOrOversizedFields(t *testing.T) {
	repository := &fakeRepository{opportunity: Opportunity{
		ID: "opportunity-id", MSPID: "msp-id", ClientID: "client-id", Version: 1,
	}}
	service := testSalesService(repository)
	for name, fields := range map[string]map[string]string{
		"reserved":    {"name": "shadowed"},
		"invalid key": {"Not Valid": "value"},
		"too many": func() map[string]string {
			found := make(map[string]string)
			for index := 0; index < 51; index++ {
				found[fmt.Sprintf("field_%d", index)] = "value"
			}
			return found
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := service.ReplaceOpportunityCustomFields(
				context.Background(),
				ReplaceOpportunityCustomFieldsCommand{
					Principal: salesPrincipal("opportunity.update"),
					ID:        "opportunity-id", ExpectedVersion: 1, Fields: fields,
					ActorID: "actor-id", Source: "api",
				},
			)
			if !errors.Is(err, ErrInvalidSalesRecord) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestReplaceOpportunityParticipantsIsScopedVersionedAndAudited(t *testing.T) {
	repository := &fakeRepository{opportunity: Opportunity{
		ID: "opportunity-id", MSPID: "msp-id", ClientID: "client-id", Version: 4,
	}}
	at := time.Date(2026, time.July, 31, 22, 0, 0, 0, time.UTC)
	service := NewService(repository, func() time.Time { return at }, sequenceSalesIDs(
		"audit-id", "event-id", "correlation-id",
	))
	updated, err := service.ReplaceOpportunityParticipants(
		context.Background(),
		ReplaceOpportunityParticipantsCommand{
			Principal: salesPrincipal("opportunity.update"),
			ID:        "opportunity-id", ExpectedVersion: 4, TeamID: "team-id",
			ContactIDs: []string{"contact-a", "contact-b", "contact-a"},
			ActorID:    "actor-id", Source: "api",
		},
	)
	if err != nil {
		t.Fatalf("ReplaceOpportunityParticipants() error = %v", err)
	}
	if updated.Version != 5 || updated.TeamID != "team-id" ||
		len(updated.ContactIDs) != 2 ||
		repository.participants.Event.EventType != "opportunity.participants.replaced" ||
		repository.participants.Audit.Action != "opportunity.participants.replaced" {
		t.Fatalf("unexpected participants mutation: %+v / %+v",
			updated, repository.participants)
	}
}

func TestCreateOpportunityValidatesInitialStageAndWritesFacts(t *testing.T) {
	repository := &fakeRepository{stages: map[string]PipelineStage{
		"stage-id": {
			ID: "stage-id", PipelineID: "pipeline-id",
			RequiredFields: []FieldKey{"expected_close_on"},
		},
	}}
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	service := NewService(repository, func() time.Time { return at }, sequenceSalesIDs(
		"opportunity-id", "audit-id", "event-id", "correlation-id",
	))
	principal := salesPrincipal("opportunity.create")

	opportunity, err := service.CreateOpportunity(context.Background(), CreateOpportunityCommand{
		Principal: principal, ClientID: "client-id",
		PipelineID: "pipeline-id", StageID: "stage-id",
		DisplayID: "OPP-100", Name: "Network refresh",
		Description: "Replace core network", Amount: Money{Minor: 1250000, Currency: "USD"},
		OwnerID: "actor-id", ExpectedCloseOn: "2026-09-30",
		ActorID: "actor-id", Source: "api",
	})
	if err != nil || opportunity.ID != "opportunity-id" ||
		opportunity.ClientID != "client-id" {
		t.Fatalf("CreateOpportunity() opportunity=%+v error=%v", opportunity, err)
	}
	if repository.created.Audit.Action != "opportunity.created" ||
		repository.created.Event.EventType != "opportunity.created" ||
		repository.created.Opportunity.Fields["description"] != "Replace core network" {
		t.Fatalf("unexpected accepted mutation: %+v", repository.created)
	}

	_, err = service.CreateOpportunity(context.Background(), CreateOpportunityCommand{
		Principal: principal, ClientID: "client-id", ProspectID: "prospect-id",
		PipelineID: "pipeline-id", StageID: "stage-id",
		DisplayID: "OPP-101", Name: "Invalid", Amount: Money{Currency: "USD"},
		ExpectedCloseOn: "2026-09-30", ActorID: "actor-id", Source: "api",
	})
	if !errors.Is(err, ErrInvalidSalesRecord) {
		t.Fatalf("ambiguous opportunity source error=%v", err)
	}
}

func TestCreatePipelinePreservesConfigurableStageIdentityAndOrder(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, time.Now, sequenceSalesIDs(
		"11111111-1111-4111-8111-111111111111",
		"77777777-7777-4777-8777-777777777777",
		"99999999-9999-4999-8999-999999999999",
		"88888888-8888-4888-8888-888888888888",
	))
	principal := salesPrincipal("pipeline.create")
	principal.Scope.ClientID = ""

	pipeline, err := service.CreatePipeline(context.Background(), CreatePipelineCommand{
		Principal: principal, Key: "enterprise", Name: "Enterprise",
		Stages: []PipelineStage{
			{ID: "44444444-4444-4444-8444-444444444444", Key: "discovery", Name: "Discovery", Position: 1, Probability: 10, Category: PipelineCategory},
			{ID: "55555555-5555-4555-8555-555555555555", Key: "proposal", Name: "Proposal", Position: 2, Probability: 50, Category: Weighted},
			{ID: "66666666-6666-4666-8666-666666666666", Key: "closed-won", Name: "Closed won", Position: 3, Probability: 100, Category: ClosedWon},
		},
		ActorID: "actor-id", Source: "api",
	})

	if err != nil {
		t.Fatalf("CreatePipeline() error = %v", err)
	}
	if pipeline.Stages[1].Key != "proposal" || pipeline.Stages[1].Name != "Proposal" ||
		pipeline.Stages[1].Position != 2 ||
		repository.pipeline.Pipeline.Stages[1].PipelineID != pipeline.ID {
		t.Fatalf("stage contract was not preserved: %+v", pipeline.Stages)
	}
}

func TestCreatePipelineRejectsMissingOrDuplicateStagePosition(t *testing.T) {
	for name, stages := range map[string][]PipelineStage{
		"missing identity": {
			{ID: "stage-1", Position: 1, Probability: 10, Category: PipelineCategory},
		},
		"duplicate position": {
			{ID: "stage-1", Key: "one", Name: "One", Position: 1, Probability: 10, Category: PipelineCategory},
			{ID: "stage-2", Key: "two", Name: "Two", Position: 1, Probability: 20, Category: Weighted},
		},
		"missing closed won stage": {
			{ID: "stage-1", Key: "one", Name: "One", Position: 1, Probability: 10, Category: PipelineCategory},
			{ID: "stage-2", Key: "two", Name: "Two", Position: 2, Probability: 60, Category: Weighted},
		},
	} {
		t.Run(name, func(t *testing.T) {
			repository := &fakeRepository{}
			service := NewService(repository, time.Now, func() string { return "pipeline-id" })
			principal := salesPrincipal("pipeline.create")
			principal.Scope.ClientID = ""
			_, err := service.CreatePipeline(context.Background(), CreatePipelineCommand{
				Principal: principal, Key: "default", Name: "Default",
				Stages: stages, ActorID: "actor-id", Source: "api",
			})
			if !errors.Is(err, ErrInvalidSalesRecord) {
				t.Fatalf("CreatePipeline() error = %v", err)
			}
			if repository.pipeline.Pipeline.ID != "" {
				t.Fatal("invalid pipeline reached repository")
			}
		})
	}
}

func TestTransitionRequiresConfiguredFieldsAndAllowedEdge(t *testing.T) {
	repository := &fakeRepository{
		opportunity: Opportunity{
			ID: "opportunity-id", MSPID: "msp-id", ClientID: "client-id",
			PipelineID: "pipeline-id", StageID: "discovery", Version: 3,
			Fields: map[FieldKey]string{"expected_close_on": ""},
		},
		stages: map[string]PipelineStage{
			"discovery": {ID: "discovery", PipelineID: "pipeline-id", Key: "discovery", Name: "Discovery", AllowedNext: []PipelineStageID{"proposal"}, Version: 4},
			"proposal": {
				ID: "proposal", PipelineID: "pipeline-id",
				Key: "proposal", Name: "Proposal", RequiredFields: []FieldKey{"expected_close_on"}, RequiresProposal: true, Version: 5,
			},
		},
		pipelines: []Pipeline{{ID: "pipeline-id", MSPID: "msp-id", Key: "default", Name: "Default", Version: 3}},
	}
	service := testSalesService(repository)
	_, err := service.TransitionOpportunity(context.Background(), TransitionCommand{
		Principal: salesPrincipal("opportunity.transition"),
		ID:        "opportunity-id", ExpectedVersion: 3, StageID: "proposal",
		ActorID: "actor-id", Source: "web", Reason: "Advance after review",
	})
	if !errors.Is(err, ErrStageRequirements) {
		t.Fatalf("TransitionOpportunity() error = %v, want ErrStageRequirements", err)
	}

	repository.opportunity.Fields["expected_close_on"] = "2026-08-31"
	_, err = service.TransitionOpportunity(context.Background(), TransitionCommand{
		Principal: salesPrincipal("opportunity.transition"),
		ID:        "opportunity-id", ExpectedVersion: 3, StageID: "proposal",
		ActorID: "actor-id", Source: "web", Reason: "Advance after review",
	})
	if !errors.Is(err, ErrProposalRequired) {
		t.Fatalf("proposal-gated transition error = %v, want ErrProposalRequired", err)
	}
}

func TestTransitionPersistsAuthorizedVersionedStageFact(t *testing.T) {
	repository := &fakeRepository{
		opportunity: Opportunity{
			ID: "opportunity-id", MSPID: "msp-id", ClientID: "client-id",
			PipelineID: "pipeline-id", StageID: "qualified", Version: 2,
			ProposalIssued: true, ApprovalGranted: true,
		},
		stages: map[string]PipelineStage{
			"qualified": {ID: "qualified", PipelineID: "pipeline-id", Key: "qualified", Name: "Qualified", AllowedNext: []PipelineStageID{"committed"}, Version: 4},
			"committed": {ID: "committed", PipelineID: "pipeline-id", Key: "committed", Name: "Committed", RequiresProposal: true, RequiresApproval: true, Version: 5},
		},
		pipelines: []Pipeline{{ID: "pipeline-id", MSPID: "msp-id", Key: "default", Name: "Default", Version: 3}},
	}
	service := testSalesService(repository)
	updated, err := service.TransitionOpportunity(context.Background(), TransitionCommand{
		Principal: salesPrincipal("opportunity.transition"),
		ID:        "opportunity-id", ExpectedVersion: 2, StageID: "committed",
		ExpectedClientVersion: 7, ExpectedPipelineVersion: 3,
		ExpectedCurrentStageVersion: 4, ExpectedDestinationStageVersion: 5,
		ActorID: "actor-id", Source: "web", Reason: "  Discovery completed  ",
	})
	if err != nil {
		t.Fatalf("TransitionOpportunity() error = %v", err)
	}
	if updated.StageID != "committed" || updated.Version != 3 {
		t.Fatalf("unexpected opportunity: %+v", updated)
	}
	if repository.accepted.Event.EventType != "opportunity.stage.changed" ||
		repository.accepted.Audit.Action != "opportunity.stage.changed" ||
		repository.accepted.Audit.Reason != "Discovery completed" ||
		repository.accepted.ExpectedClientVersion != 7 ||
		repository.accepted.Pipeline.Version != 3 ||
		repository.accepted.CurrentStage.Version != 4 ||
		repository.accepted.DestinationStage.Version != 5 {
		t.Fatal("stage transition did not produce matching audit/event facts")
	}
}

func TestOpportunityTransitionReasonRejectsBlankBeforeWrite(t *testing.T) {
	repository := &fakeRepository{}
	service := testSalesService(repository)
	_, err := service.TransitionOpportunity(context.Background(), TransitionCommand{
		Principal: salesPrincipal("opportunity.transition"),
		ID:        "opportunity-id", ExpectedVersion: 2, StageID: "committed",
		ActorID: "actor-id", Source: "web", Reason: " \t ",
	})
	if !errors.Is(err, ErrInvalidSalesRecord) || repository.calls != 0 {
		t.Fatalf("TransitionOpportunity() error=%v writes=%d", err, repository.calls)
	}
}

func TestTransitionDeniesCrossClientWithoutExistenceDisclosure(t *testing.T) {
	repository := &fakeRepository{opportunity: Opportunity{
		ID: "opportunity-id", MSPID: "msp-id", ClientID: "client-bravo",
		PipelineID: "pipeline-id", StageID: "new", Version: 1,
	}}
	service := testSalesService(repository)
	principal := salesPrincipal("opportunity.transition")
	principal.Scope.ClientID = "client-alpha"
	_, err := service.TransitionOpportunity(context.Background(), TransitionCommand{
		Principal: principal,
		Target:    scope.Target{MSPID: "msp-id", ClientID: "client-bravo"},
		ID:        "opportunity-id", ExpectedVersion: 1, StageID: "qualified",
		ActorID: "actor-id", Source: "api", Reason: "Advance after review",
	})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-client transition error = %v, want ErrNotFound", err)
	}
	if repository.calls != 0 {
		t.Fatal("cross-client transition reached repository")
	}
}

func TestForecastUsesConfiguredProbabilityAndCommittedCategory(t *testing.T) {
	got, err := CalculateForecast([]ForecastInput{
		{Amount: Money{Minor: 10000, Currency: "USD"}, Probability: 40, Category: Weighted},
		{Amount: Money{Minor: 5000, Currency: "USD"}, Probability: 100, Category: Committed},
	})
	if err != nil {
		t.Fatalf("CalculateForecast() error = %v", err)
	}
	if got.WeightedRevenue.Minor != 9000 || got.CommittedRevenue.Minor != 5000 {
		t.Fatalf("unexpected forecast: %+v", got)
	}
}

func TestForecastRejectsMixedCurrencies(t *testing.T) {
	_, err := CalculateForecast([]ForecastInput{
		{Amount: Money{Minor: 100, Currency: "USD"}, Probability: 50, Category: Weighted},
		{Amount: Money{Minor: 100, Currency: "CAD"}, Probability: 50, Category: Weighted},
	})
	if !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("mixed currency error = %v", err)
	}
}

func salesPrincipal(capabilities ...string) authorization.Principal {
	return authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet(capabilities...),
	}
}

func salesMSPPrincipal(capabilities ...string) authorization.Principal {
	principal := salesPrincipal(capabilities...)
	principal.Scope.ClientID = ""
	return principal
}

func testSalesService(repository Repository) *Service {
	ids := []string{"audit-id", "event-id", "correlation-id"}
	return NewService(repository, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
}

func sequenceSalesIDs(ids ...string) func() string {
	return func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}
}

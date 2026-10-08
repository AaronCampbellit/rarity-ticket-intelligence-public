package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/attachments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/automation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/billingexport"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientidentity"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/comments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/graphintake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/intake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/integrationhealth"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/links"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/routing"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/search"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/servicekeys"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/timeentries"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/views"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

type salesActions struct {
	got          sales.CreateProspectCommand
	opportunity  sales.CreateOpportunityCommand
	activity     sales.CreateOpportunityActivityCommand
	transition   sales.TransitionCommand
	customFields sales.ReplaceOpportunityCustomFieldsCommand
	participants sales.ReplaceOpportunityParticipantsCommand
	forecast     string
	pipelines    []sales.Pipeline
}

func (a *salesActions) ReplaceOpportunityParticipants(
	_ context.Context,
	command sales.ReplaceOpportunityParticipantsCommand,
) (sales.Opportunity, error) {
	a.participants = command
	return sales.Opportunity{
		ID: command.ID, Version: command.ExpectedVersion + 1,
		TeamID: command.TeamID, ContactIDs: command.ContactIDs,
	}, nil
}

func (a *salesActions) ReplaceOpportunityCustomFields(
	_ context.Context,
	command sales.ReplaceOpportunityCustomFieldsCommand,
) (sales.Opportunity, error) {
	a.customFields = command
	return sales.Opportunity{
		ID: command.ID, Version: command.ExpectedVersion + 1,
		CustomFields: command.Fields,
	}, nil
}

func TestReplaceOpportunityCustomFieldsRouteUsesExactETag(t *testing.T) {
	actions := &salesActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Sales: actions})
	request := httptest.NewRequest(
		http.MethodPut, "/api/v1/opportunities/opportunity-id/custom-fields",
		strings.NewReader(`{"expected_version":3,"fields":{"risk_summary":"Weekend cutover"}}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"3"`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		actions.customFields.ExpectedVersion != 3 ||
		actions.customFields.Fields["risk_summary"] != "Weekend cutover" ||
		response.Header().Get("ETag") != `"4"` {
		t.Fatalf("unexpected response: status=%d command=%+v headers=%v body=%s",
			response.Code, actions.customFields, response.Header(), response.Body.String())
	}
}

func TestReplaceOpportunityParticipantsRouteUsesExactETag(t *testing.T) {
	actions := &salesActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Sales: actions})
	request := httptest.NewRequest(
		http.MethodPut, "/api/v1/opportunities/opportunity-id/participants",
		strings.NewReader(`{"expected_version":4,"team_id":"team-id","contact_ids":["contact-id"]}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"4"`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		actions.participants.ExpectedVersion != 4 ||
		actions.participants.TeamID != "team-id" ||
		len(actions.participants.ContactIDs) != 1 ||
		response.Header().Get("ETag") != `"5"` {
		t.Fatalf("unexpected response: status=%d command=%+v headers=%v body=%s",
			response.Code, actions.participants, response.Header(), response.Body.String())
	}
}

func (a *salesActions) ListPipelines(
	_ context.Context,
	_ authorization.Principal,
) ([]sales.Pipeline, error) {
	return a.pipelines, nil
}

func (a *salesActions) Forecast(
	_ context.Context,
	_ authorization.Principal,
	pipelineID string,
) ([]sales.ForecastBucket, error) {
	a.forecast = pipelineID
	return []sales.ForecastBucket{{PipelineID: pipelineID, OpportunityCount: 2}}, nil
}

func (*salesActions) ListProspects(
	context.Context,
	authorization.Principal,
	int,
) ([]sales.Prospect, error) {
	return nil, nil
}

type proposalActions struct {
	create     sales.CreateProposalCommand
	decision   sales.DecideInternalApprovalCommand
	grant      sales.IssueAcceptanceGrantCommand
	approval   sales.InternalApproval
	approvalID string
}

func TestOpportunityForecastRouteMapsOptionalPipeline(t *testing.T) {
	actions := &salesActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Sales: actions})
	request := httptest.NewRequest(
		http.MethodGet, "/api/v1/opportunity-forecast?pipeline_id=pipeline-id", nil,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || actions.forecast != "pipeline-id" ||
		!strings.Contains(response.Body.String(), `"opportunity_count":2`) {
		t.Fatalf("unexpected response: status=%d pipeline=%q body=%s",
			response.Code, actions.forecast, response.Body.String())
	}
}

func TestListPipelinesRouteUsesStableJSONFields(t *testing.T) {
	actions := &salesActions{pipelines: []sales.Pipeline{{
		ID: "pipeline-id", Key: "default", Name: "Default",
		Stages: []sales.PipelineStage{{ID: "stage-id", Name: "Qualified"}},
	}}}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Sales: actions})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", nil))

	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"pipeline_id":""`) ||
		!strings.Contains(response.Body.String(), `"stages"`) {
		t.Fatalf("unexpected response: status=%d body=%s", response.Code, response.Body.String())
	}
}

func (a *proposalActions) IssueAcceptanceGrant(
	_ context.Context,
	command sales.IssueAcceptanceGrantCommand,
) (sales.IssuedAcceptanceGrant, error) {
	a.grant = command
	return sales.IssuedAcceptanceGrant{
		Grant: sales.AcceptanceGrant{ID: "grant-id"},
		Token: "rag_secret",
	}, nil
}

func (a *proposalActions) GetInternalApproval(
	_ context.Context,
	_ authorization.Principal,
	_ scope.Target,
	id string,
) (sales.InternalApproval, error) {
	a.approvalID = id
	return a.approval, nil
}

func TestGetInternalApprovalRouteReturnsVersionedEvidence(t *testing.T) {
	actions := &proposalActions{approval: sales.InternalApproval{
		ID: "approval-id", ProposalVersionID: "version-id",
		State: "pending", Version: 3,
	}}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Proposals: actions})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/api/v1/proposal-versions/version-id/internal-approval",
		nil,
	))
	if response.Code != http.StatusOK || actions.approvalID != "version-id" ||
		response.Header().Get("ETag") != `"3"` ||
		!strings.Contains(response.Body.String(), `"state":"pending"`) {
		t.Fatalf("unexpected response: status=%d id=%q headers=%v body=%s",
			response.Code, actions.approvalID, response.Header(), response.Body.String())
	}
}

func (a *proposalActions) CreateProposal(
	_ context.Context,
	command sales.CreateProposalCommand,
) (sales.Proposal, error) {
	a.create = command
	return sales.Proposal{ID: "proposal-id", Version: 1}, nil
}
func (*proposalActions) GetProposal(
	_ context.Context,
	_ authorization.Principal,
	_ scope.Target,
	id string,
) (sales.Proposal, error) {
	return sales.Proposal{ID: id, Version: 3}, nil
}
func (*proposalActions) ListProposals(
	_ context.Context,
	_ authorization.Principal,
	_ scope.Target,
	filter sales.ProposalListFilter,
) ([]sales.Proposal, error) {
	return []sales.Proposal{{
		ID: "proposal-id", State: filter.State, OpportunityID: filter.OpportunityID,
	}}, nil
}
func (*proposalActions) IssueVersion(context.Context, sales.IssueVersionCommand) (sales.ProposalVersion, error) {
	return sales.ProposalVersion{}, nil
}

func TestCreateProposalRouteMapsOpportunityAndTrustedActor(t *testing.T) {
	actions := &proposalActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Proposals: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/proposals",
		bytes.NewBufferString(`{"opportunity_id":"opportunity-id","display_id":"PROP-100"}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated ||
		actions.create.OpportunityID != "opportunity-id" ||
		actions.create.ActorID != "actor-id" || actions.create.Source != "api" {
		t.Fatalf("unexpected response/command: status=%d command=%+v body=%s",
			response.Code, actions.create, response.Body.String())
	}
}

func TestProposalReadRoutesExposeScopedStableWorklist(t *testing.T) {
	handler := NewRouter(Dependencies{Principal: testPrincipal, Proposals: &proposalActions{}})
	list := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/proposals?state=issued&opportunity_id=opportunity-id&limit=25",
		nil,
	)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK ||
		!strings.Contains(listResponse.Body.String(), `"id":"proposal-id"`) {
		t.Fatalf("list response=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	get := httptest.NewRequest(http.MethodGet, "/api/v1/proposals/proposal-id", nil)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK || getResponse.Header().Get("ETag") != `"3"` {
		t.Fatalf("get response=%d etag=%q body=%s",
			getResponse.Code, getResponse.Header().Get("ETag"), getResponse.Body.String())
	}
}

func TestAcceptanceGrantRouteReturnsTokenOnceAndUsesTrustedActor(t *testing.T) {
	actions := &proposalActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Proposals: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/proposal-versions/version-id/acceptance-grants",
		bytes.NewBufferString(`{
			"signer_name":"Alex Client",
			"signer_email":"alex@example.com",
			"evidence":{"delivery":"email"},
			"expires_at":"2026-08-01T20:00:00Z"
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated ||
		actions.grant.ProposalVersionID != "version-id" ||
		actions.grant.ActorID != "actor-id" ||
		response.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(response.Body.String(), `"token":"rag_secret"`) {
		t.Fatalf("unexpected response/command: status=%d command=%+v body=%s",
			response.Code, actions.grant, response.Body.String())
	}
}
func (a *proposalActions) DecideInternalApproval(
	_ context.Context,
	command sales.DecideInternalApprovalCommand,
) (sales.InternalApproval, error) {
	a.decision = command
	return sales.InternalApproval{ID: "approval-id", Version: command.ExpectedVersion + 1}, nil
}
func (*proposalActions) AcceptElectronically(context.Context, sales.ElectronicAcceptanceCommand) (sales.Acceptance, error) {
	return sales.Acceptance{}, nil
}
func (*proposalActions) RecordOfflineAcceptance(context.Context, sales.OfflineAcceptanceCommand) (sales.Acceptance, error) {
	return sales.Acceptance{}, nil
}

func TestInternalApprovalRouteMapsIfMatchAndTrustedActor(t *testing.T) {
	actions := &proposalActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Proposals: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/proposal-versions/version-id/internal-approval",
		bytes.NewBufferString(`{"decision":"approved","reason":"Margin accepted"}`),
	)
	request.Header.Set("If-Match", `"1"`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("ETag") != `"2"` ||
		actions.decision.ProposalVersionID != "version-id" ||
		actions.decision.ExpectedVersion != 1 ||
		actions.decision.ActorID != "actor-id" ||
		actions.decision.Source != "api" {
		t.Fatalf("unexpected response/command: status=%d command=%+v body=%s",
			response.Code, actions.decision, response.Body.String())
	}
}

func (a *salesActions) CreateProspect(_ context.Context, command sales.CreateProspectCommand) (sales.Prospect, error) {
	a.got = command
	return sales.Prospect{ID: "prospect-id"}, nil
}

func (*salesActions) CreatePipeline(context.Context, sales.CreatePipelineCommand) (sales.Pipeline, error) {
	return sales.Pipeline{}, nil
}
func (a *salesActions) CreateOpportunity(
	_ context.Context,
	command sales.CreateOpportunityCommand,
) (sales.Opportunity, error) {
	a.opportunity = command
	return sales.Opportunity{ID: "opportunity-id", Version: 1}, nil
}
func (*salesActions) GetOpportunity(
	_ context.Context,
	_ authorization.Principal,
	id sales.OpportunityID,
) (sales.Opportunity, error) {
	return sales.Opportunity{ID: id, Version: 2}, nil
}
func (*salesActions) ListOpportunities(
	_ context.Context,
	_ authorization.Principal,
	filter sales.OpportunityListFilter,
) ([]sales.Opportunity, error) {
	return []sales.Opportunity{{
		ID: "opportunity-id", PipelineID: filter.PipelineID,
		StageID: filter.StageID, Version: 2,
	}}, nil
}
func (a *salesActions) CreateOpportunityActivity(
	_ context.Context,
	command sales.CreateOpportunityActivityCommand,
) (sales.OpportunityActivity, error) {
	a.activity = command
	return sales.OpportunityActivity{
		ID: "activity-id", OpportunityID: command.OpportunityID,
		Kind: command.Kind, Summary: command.Summary,
	}, nil
}
func (*salesActions) ListOpportunityActivities(
	context.Context,
	authorization.Principal,
	sales.OpportunityID,
	int,
) ([]sales.OpportunityActivity, error) {
	return []sales.OpportunityActivity{{ID: "activity-id"}}, nil
}

func (a *salesActions) TransitionOpportunity(_ context.Context, command sales.TransitionCommand) (sales.Opportunity, error) {
	a.transition = command
	return sales.Opportunity{}, nil
}

func TestCreateOpportunityActivityRouteUsesTrustedActorAndParent(t *testing.T) {
	actions := &salesActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Sales: actions})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/opportunities/opportunity-id/activities",
		bytes.NewBufferString(`{
			"kind":"call",
			"summary":"Discovery call",
			"details":"Confirmed rollout scope",
			"occurred_at":"2026-07-30T17:00:00Z"
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated ||
		actions.activity.OpportunityID != "opportunity-id" ||
		actions.activity.Target != (scope.Target{MSPID: "msp-id", ClientID: "client-id"}) ||
		actions.activity.ActorID != "actor-id" ||
		actions.activity.Source != "api" {
		t.Fatalf("unexpected response/command: status=%d command=%+v body=%s",
			response.Code, actions.activity, response.Body.String())
	}
}

func TestCreateOpportunityRouteMapsTrustedScopeAndCommercialFields(t *testing.T) {
	actions := &salesActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Sales: actions})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/opportunities",
		bytes.NewBufferString(`{
			"client_id":"client-id",
			"pipeline_id":"pipeline-id",
			"stage_id":"stage-id",
			"display_id":"OPP-100",
			"name":"Network refresh",
			"description":"Replace core network",
			"amount_minor":1250000,
			"currency":"USD",
			"owner_id":"owner-id",
			"expected_close_on":"2026-09-30"
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated ||
		actions.opportunity.Principal.ID != "actor-id" ||
		actions.opportunity.ClientID != "client-id" ||
		actions.opportunity.Amount.Minor != 1250000 ||
		actions.opportunity.ActorID != "actor-id" ||
		actions.opportunity.Source != "api" {
		t.Fatalf(
			"status=%d command=%+v body=%s",
			response.Code, actions.opportunity, response.Body.String(),
		)
	}
}

func TestOpportunityReadRoutesExposeScopedStableWorklist(t *testing.T) {
	handler := NewRouter(Dependencies{Principal: testPrincipal, Sales: &salesActions{}})
	list := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/opportunities?pipeline_id=pipeline-id&stage_id=stage-id&limit=25",
		nil,
	)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK ||
		!strings.Contains(listResponse.Body.String(), `"id":"opportunity-id"`) {
		t.Fatalf("list response=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	get := httptest.NewRequest(http.MethodGet, "/api/v1/opportunities/opportunity-id", nil)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK || getResponse.Header().Get("ETag") != `"2"` {
		t.Fatalf("get response=%d etag=%q body=%s",
			getResponse.Code, getResponse.Header().Get("ETag"), getResponse.Body.String())
	}
}

type conversionActions struct {
	result projects.ConversionResult
	err    error
	got    projects.ConversionCommand
	calls  int
}

type aiDecisionActions struct {
	got   aiassist.DecisionCommand
	calls int
}

type deadLetterActions struct {
	got automation.DeadLetterCommand
}

func (a *deadLetterActions) List(
	context.Context,
	authorization.Principal,
) ([]automation.DeadLetter, error) {
	return []automation.DeadLetter{{ID: "dead-letter-id", State: "open"}}, nil
}

type locationActions struct {
	got clientresources.CreateLocationCommand
	err error
}

type contractActions struct {
	got clientresources.CreateContractCommand
}

type contactActions struct {
	got clientresources.CreateContactCommand
}

type serviceRecordActions struct {
	got clientresources.CreateServiceCommand
}

type assetActions struct {
	got clientresources.CreateAssetCommand
}

type workRecordActions struct {
	got workrecords.CreateCommand
}

type workRecordQueryActions struct {
	get    workrecords.GetCommand
	list   workrecords.ListCommand
	record workrecords.Record
}

func (a *workRecordQueryActions) Get(
	_ context.Context,
	command workrecords.GetCommand,
) (workrecords.Record, error) {
	a.get = command
	return a.record, nil
}

func (a *workRecordQueryActions) List(
	_ context.Context,
	command workrecords.ListCommand,
) ([]workrecords.Record, error) {
	a.list = command
	return []workrecords.Record{a.record}, nil
}

type viewActions struct {
	saved    views.SaveCommand
	resolved views.ResolveCommand
}

func TestWorkRecordReadRoutesUseAuthenticatedClientAndStableCursor(t *testing.T) {
	updatedAt := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	actions := &workRecordQueryActions{record: workrecords.Record{
		Envelope: object.Envelope{
			ID: "work-id", MSPID: "msp-id", ClientID: "client-id",
			Version: 4, UpdatedAt: updatedAt,
		},
		Type: workrecords.Incident, Title: "Email unavailable",
	}}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, WorkRecordQueries: actions,
	})

	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(
		getResponse,
		httptest.NewRequest(http.MethodGet, "/api/v1/work-records/work-id", nil),
	)
	if getResponse.Code != http.StatusOK ||
		getResponse.Header().Get("ETag") != `"4"` ||
		actions.get.ID != "work-id" ||
		actions.get.Principal.Scope.ClientID != "client-id" {
		t.Fatalf(
			"unexpected get response/command: status=%d etag=%q command=%+v body=%s",
			getResponse.Code, getResponse.Header().Get("ETag"),
			actions.get, getResponse.Body.String(),
		)
	}

	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(
		listResponse,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/work-records?status=in_progress&queue_id=queue-id"+
				"&owner_id=technician-id&limit=25&before_updated_at="+
				url.QueryEscape(updatedAt.Format(time.RFC3339Nano))+
				"&before_id=cursor-id",
			nil,
		),
	)
	if listResponse.Code != http.StatusOK ||
		actions.list.Principal.Scope.ClientID != "client-id" ||
		actions.list.Status != "in_progress" ||
		actions.list.QueueID != "queue-id" ||
		actions.list.PrimaryOwnerID != "technician-id" ||
		actions.list.Limit != 25 ||
		actions.list.BeforeUpdatedAt != updatedAt ||
		actions.list.BeforeID != "cursor-id" {
		t.Fatalf(
			"unexpected list response/command: status=%d command=%+v body=%s",
			listResponse.Code, actions.list, listResponse.Body.String(),
		)
	}
}

type routingActions struct {
	got     routing.PublishCommand
	current routing.RuleSet
}

type calendarActions struct {
	got sla.PublishCalendarCommand
}

func (a *calendarActions) ListCalendars(
	_ context.Context,
	_ authorization.Principal,
	_ scope.Target,
) ([]sla.PublishedCalendar, error) {
	return []sla.PublishedCalendar{{ID: "calendar"}}, nil
}

type policyActions struct {
	got sla.PublishPolicyCommand
}

type notificationPolicyActions struct {
	got notifications.PublishPolicyCommand
}

func (a *notificationPolicyActions) ListPolicies(
	_ context.Context,
	_ authorization.Principal,
	_ scope.Target,
) ([]notifications.PublishedPolicy, error) {
	return []notifications.PublishedPolicy{{ID: "notification-policy"}}, nil
}

type knowledgeActions struct {
	listed    knowledge.ListCommand
	created   knowledge.CreateDraftCommand
	revised   knowledge.ReviseDraftCommand
	published knowledge.PublishCommand
	found     knowledge.FindCommand
}

type billingExportActions struct {
	listed   billingexport.ListApprovalsCommand
	approval billingexport.ApprovalCommand
	export   billingexport.ExportCommand
	err      error
}

type integrationHealthActions struct {
	got integrationhealth.SnapshotCommand
}

type clientResourceCatalogActions struct {
	got   clientresources.ListCatalogCommand
	get   clientresources.GetCatalogCommand
	calls int
}

func (a *clientResourceCatalogActions) List(
	_ context.Context,
	command clientresources.ListCatalogCommand,
) ([]clientresources.Summary, error) {
	a.calls++
	a.got = command
	return []clientresources.Summary{{
		ID: "asset-id", Kind: "asset", DisplayID: "AST-1", Name: "Router",
	}}, nil
}

func (a *clientResourceCatalogActions) GetSummary(_ context.Context, command clientresources.GetCatalogCommand) (clientresources.Summary, error) {
	a.get = command
	return clientresources.Summary{ID: command.ID, Kind: "asset", DisplayID: "AST-1", Name: "Router"}, nil
}

type directIntakeActions struct {
	got   intake.SystemCommand
	calls int
}

func (a *directIntakeActions) Accept(
	_ context.Context,
	command intake.SystemCommand,
) (intake.InboundEvent, error) {
	a.calls++
	a.got = command
	return intake.InboundEvent{
		ID: "inbound", MSPID: command.Target.MSPID,
		ClientID: command.Target.ClientID, Source: intake.SourceDirectAPI,
		ExternalID: command.ExternalID, RawPayloadRef: "intake/raw",
		ProcessingState: intake.StateReceived,
	}, nil
}

type inboundWebhookActions struct {
	got   webhooks.InboundCommand
	err   error
	calls int
}

type graphNotificationActions struct {
	body  []byte
	err   error
	calls int
}

func (a *graphNotificationActions) Accept(
	_ context.Context,
	body []byte,
) (graphintake.NotificationAcceptance, error) {
	a.calls++
	a.body = append([]byte(nil), body...)
	return graphintake.NotificationAcceptance{Accepted: 1}, a.err
}

func (a *inboundWebhookActions) Accept(
	_ context.Context,
	command webhooks.InboundCommand,
) (intake.InboundEvent, error) {
	a.calls++
	a.got = command
	return intake.InboundEvent{
		ID: "inbound", MSPID: "msp-id", ClientID: "client-id",
		Source: intake.SourceInboundWebhook, ExternalID: command.EventID,
		RawPayloadRef: "intake/raw", ProcessingState: intake.StateReceived,
	}, a.err
}

type forwardingIntakeActions struct {
	got   intake.ForwardingCommand
	calls int
}

func (a *forwardingIntakeActions) Accept(
	_ context.Context,
	command intake.ForwardingCommand,
) (intake.ForwardingResult, error) {
	a.calls++
	a.got = command
	return intake.ForwardingResult{
		EventID: "inbound", State: intake.StateReceived,
	}, nil
}

func (a *integrationHealthActions) Snapshot(
	_ context.Context,
	command integrationhealth.SnapshotCommand,
) (integrationhealth.Snapshot, error) {
	a.got = command
	return integrationhealth.Snapshot{
		State:       integrationhealth.Degraded,
		GeneratedAt: time.Date(2026, time.July, 29, 21, 0, 0, 0, time.UTC),
		Connections: []integrationhealth.ConnectionResult{{
			ID: "teams", Kind: "teams", LastErrorCode: "delivery_failed",
			Result: integrationhealth.Result{
				State: integrationhealth.Degraded, Reason: "pending_failures",
				PendingFailures: 2,
			},
		}},
	}, nil
}

func (a *billingExportActions) ListApprovals(
	_ context.Context,
	command billingexport.ListApprovalsCommand,
) ([]billingexport.ApprovalEntry, error) {
	a.listed = command
	return []billingexport.ApprovalEntry{{Entry: billingexport.Entry{ID: "entry"}}}, nil
}

func (a *billingExportActions) DecideApproval(
	_ context.Context,
	command billingexport.ApprovalCommand,
) (billingexport.ApprovalEntry, error) {
	a.approval = command
	return billingexport.ApprovalEntry{
		Entry:         billingexport.Entry{ID: command.EntryID, Version: command.ExpectedVersion + 1},
		ApprovalState: command.Decision,
	}, nil
}

func (a *billingExportActions) Export(
	_ context.Context,
	command billingexport.ExportCommand,
) (billingexport.ExportResult, error) {
	a.export = command
	if a.err != nil {
		return billingexport.ExportResult{}, a.err
	}
	return billingexport.ExportResult{
		ID: "export-id", EntryCount: 1,
		CSV: []byte("time_entry_id,duration_seconds\nentry,3600\n"),
	}, nil
}

func (a *knowledgeActions) List(
	_ context.Context,
	command knowledge.ListCommand,
) ([]knowledge.Article, error) {
	a.listed = command
	return []knowledge.Article{{ID: "article", DisplayID: "KB-100"}}, nil
}

func (a *knowledgeActions) CreateDraft(
	_ context.Context,
	command knowledge.CreateDraftCommand,
) (knowledge.ArticleDetail, error) {
	a.created = command
	return knowledge.ArticleDetail{
		Article: knowledge.Article{ID: "article", CurrentVersion: 1},
		Version: knowledge.Version{ArticleID: "article", Version: 1},
	}, nil
}

func (a *knowledgeActions) ReviseDraft(
	_ context.Context,
	command knowledge.ReviseDraftCommand,
) (knowledge.ArticleDetail, error) {
	a.revised = command
	return knowledge.ArticleDetail{
		Article: knowledge.Article{
			ID: command.ArticleID, CurrentVersion: command.ExpectedVersion + 1,
		},
		Version: knowledge.Version{
			ArticleID: command.ArticleID, Version: command.ExpectedVersion + 1,
		},
	}, nil
}

func (a *knowledgeActions) Publish(
	_ context.Context,
	command knowledge.PublishCommand,
) (knowledge.Version, error) {
	a.published = command
	return knowledge.Version{
		ArticleID: command.ArticleID, Version: command.ExpectedVersion,
		State: knowledge.Published,
	}, nil
}

func (a *knowledgeActions) Find(
	_ context.Context,
	command knowledge.FindCommand,
) (knowledge.ArticleDetail, error) {
	a.found = command
	return knowledge.ArticleDetail{
		Article: knowledge.Article{ID: command.ArticleID, CurrentVersion: 2},
		Version: knowledge.Version{ArticleID: command.ArticleID, Version: 2},
	}, nil
}

func (a *notificationPolicyActions) PublishPolicy(
	_ context.Context,
	command notifications.PublishPolicyCommand,
) (notifications.PublishedPolicy, error) {
	a.got = command
	return notifications.PublishedPolicy{
		ID: "notification-policy", MSPID: command.Target.MSPID,
		ClientID: command.Target.ClientID, Key: command.Key, Name: command.Name,
		Version: command.ExpectedVersion + 1,
	}, nil
}

func (a *policyActions) PublishPolicy(
	_ context.Context,
	command sla.PublishPolicyCommand,
) (sla.PublishedPolicy, error) {
	a.got = command
	return sla.PublishedPolicy{
		ID: "policy", MSPID: command.Target.MSPID, ClientID: command.Target.ClientID,
		Key: command.Key, Name: command.Name, Version: command.ExpectedVersion + 1,
	}, nil
}

func (a *policyActions) ListPolicies(
	_ context.Context,
	_ authorization.Principal,
	_ scope.Target,
) ([]sla.PublishedPolicy, error) {
	return []sla.PublishedPolicy{{ID: "policy"}}, nil
}

func (a *calendarActions) PublishCalendar(
	_ context.Context,
	command sla.PublishCalendarCommand,
) (sla.PublishedCalendar, error) {
	a.got = command
	return sla.PublishedCalendar{
		ID: "calendar", MSPID: command.Target.MSPID, ClientID: command.Target.ClientID,
		Key: command.Key, Name: command.Name, Version: command.ExpectedVersion + 1,
		Definition: command.Definition,
	}, nil
}

func (a *routingActions) Publish(
	_ context.Context,
	command routing.PublishCommand,
) (routing.RuleSet, error) {
	a.got = command
	return routing.RuleSet{
		ID: "routing-set", MSPID: command.Principal.Scope.MSPID,
		Version: command.ExpectedVersion + 1, Rules: command.Rules,
	}, nil
}

func (a *routingActions) Current(
	_ context.Context,
	_ authorization.Principal,
) (routing.RuleSet, error) {
	if a.current.ID == "" {
		return routing.RuleSet{}, scope.ErrNotFound
	}
	return a.current, nil
}

func (a *viewActions) Save(_ context.Context, command views.SaveCommand) (views.View, error) {
	a.saved = command
	return views.View{
		ID: "view-id", MSPID: command.Principal.Scope.MSPID, OwnerID: command.OwnerID,
		Kind: command.Kind, Name: command.Name, Query: command.Query,
		Audience: command.Audience, Version: 1,
	}, nil
}

func (a *viewActions) List(_ context.Context, command views.ListCommand) ([]views.View, error) {
	return []views.View{{
		ID: "view-id", Kind: command.Kind, Name: "My work",
		Query: map[string]any{"status": "new"},
	}}, nil
}

func (a *viewActions) Resolve(_ context.Context, command views.ResolveCommand) (views.Resolved, error) {
	a.resolved = command
	return views.Resolved{
		ViewID: command.ViewID, Kind: views.SavedSearch,
		Query: map[string]any{"client_id": command.Target.ClientID, "status": "open"},
	}, nil
}

type workAssignmentActions struct {
	got workrecords.AssignCommand
}

type workTransitionActions struct {
	got workrecords.TransitionCommand
}

type workPriorityActions struct {
	got workrecords.PriorityCommand
}

type slaOverrideActions struct {
	got workrecords.SLAOverrideCommand
}

func (a *slaOverrideActions) Override(
	_ context.Context,
	command workrecords.SLAOverrideCommand,
) (workrecords.Record, error) {
	a.got = command
	return workrecords.Record{Envelope: object.Envelope{
		ID: command.WorkRecordID, Version: command.ExpectedVersion + 1,
	}}, nil
}

func (a *workPriorityActions) Change(
	_ context.Context,
	command workrecords.PriorityCommand,
) (workrecords.Record, error) {
	a.got = command
	return workrecords.Record{
		Envelope: object.Envelope{
			ID: command.WorkRecordID, Version: command.ExpectedVersion + 1,
		},
		Priority: command.Priority,
	}, nil
}

func (a *workTransitionActions) Transition(
	_ context.Context,
	command workrecords.TransitionCommand,
) (workrecords.Record, error) {
	a.got = command
	return workrecords.Record{
		Envelope: object.Envelope{ID: command.WorkRecordID, Version: command.ExpectedVersion + 1},
		Status:   command.ToStatus,
	}, nil
}

func (a *workAssignmentActions) Assign(
	_ context.Context,
	command workrecords.AssignCommand,
) (workrecords.Record, error) {
	a.got = command
	return workrecords.Record{
		Envelope:       object.Envelope{ID: command.WorkRecordID, Version: 3},
		PrimaryOwnerID: command.OwnerID,
	}, nil
}

type workQueueActions struct {
	got workrecords.QueueCommand
}

type workMergeActions struct {
	got workrecords.MergeCommand
}

type workParticipantActions struct {
	add    workrecords.AddParticipantCommand
	remove workrecords.RemoveParticipantCommand
}

type commentActions struct {
	got comments.CreateCommand
}

type attachmentActions struct {
	got    attachments.UploadCommand
	body   []byte
	listed []attachments.Attachment
}

func (a *attachmentActions) Upload(
	_ context.Context,
	command attachments.UploadCommand,
	body io.Reader,
) (attachments.Attachment, error) {
	a.got = command
	a.body, _ = io.ReadAll(body)
	return attachments.Attachment{
		ID: "attachment-id", MSPID: command.Principal.Scope.MSPID,
		ClientID:     command.Principal.Scope.ClientID,
		WorkRecordID: command.WorkRecordID, Filename: command.Filename,
		OpportunityID: command.OpportunityID,
		ContentType:   command.ContentType, SizeBytes: command.SizeBytes,
		Version: 1, UploadedBy: command.ActorID,
	}, nil
}

func (a *attachmentActions) ListOpportunity(
	_ context.Context,
	_ authorization.Principal,
	opportunityID string,
	_ int,
) ([]attachments.Attachment, error) {
	if a.listed != nil {
		return a.listed, nil
	}
	return []attachments.Attachment{{
		ID: "attachment-id", OpportunityID: opportunityID,
		Filename: "scope.txt", ContentType: "text/plain", SizeBytes: 5, Version: 1,
	}}, nil
}

func TestOpportunityAttachmentsStreamAndListScopedMetadata(t *testing.T) {
	actions := &attachmentActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Attachments: actions})
	body := []byte("scope")
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/opportunities/opportunity-id/attachments",
		bytes.NewReader(body),
	)
	request.Header.Set("Content-Type", "text/plain")
	request.Header.Set("X-Rarity-Filename", "scope.txt")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated ||
		actions.got.OpportunityID != "opportunity-id" ||
		!bytes.Equal(actions.body, body) ||
		!strings.Contains(response.Body.String(), `"opportunity_id":"opportunity-id"`) {
		t.Fatalf("upload status=%d command=%+v body=%s",
			response.Code, actions.got, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet, "/api/v1/opportunities/opportunity-id/attachments", nil,
	))
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"filename":"scope.txt"`) {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestUploadAttachmentStreamsBoundedRawBody(t *testing.T) {
	actions := &attachmentActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Attachments: actions})
	body := []byte("synthetic attachment")
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/work-records/work-id/attachments",
		bytes.NewReader(body),
	)
	request.Header.Set("Content-Type", "text/plain")
	request.Header.Set("X-Rarity-Filename", "notes.txt")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	if actions.got.WorkRecordID != "work-id" ||
		actions.got.SizeBytes != int64(len(body)) ||
		actions.got.Filename != "notes.txt" ||
		actions.got.ActorID != "actor-id" ||
		!bytes.Equal(actions.body, body) {
		t.Fatalf("unexpected upload command/body: %+v %q", actions.got, actions.body)
	}
}

type timeEntryActions struct {
	got timeentries.CreateCommand
}

type relationshipActions struct {
	got links.CreateCommand
}

type taskActions struct {
	got tasks.CreateCommand
}

func (*taskActions) Get(_ context.Context, principal authorization.Principal, id string) (tasks.Task, error) {
	return tasks.Task{ID: id, MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID, Parent: tasks.Ref{Type: tasks.ParentWorkRecord, ID: "work-id"}}, nil
}

func (*taskActions) ListOpportunityTasks(
	context.Context,
	authorization.Principal,
	string,
) ([]tasks.Task, error) {
	return nil, nil
}

type searchActions struct {
	got search.Query
}

type serviceKeyActions struct {
	issue  servicekeys.IssueCommand
	rotate servicekeys.RotateCommand
	revoke servicekeys.RevokeCommand
}

func (a *serviceKeyActions) List(
	_ context.Context,
	principal authorization.Principal,
	target scope.Target,
) ([]servicekeys.Record, error) {
	return []servicekeys.Record{{
		ID: "key-id", MSPID: principal.Scope.MSPID, ClientID: target.ClientID,
		Name: "Monitoring", Prefix: "prefix123",
		Capabilities: []string{"work_record.create"},
		DataScopes:   []string{"work_records"}, CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}}, nil
}

type workflowActions struct {
	got workflow.PublishCommand
}

func (a *workflowActions) List(
	_ context.Context,
	_ authorization.Principal,
	_ scope.Target,
) ([]workflow.Published, error) {
	return []workflow.Published{{ID: "workflow-id"}}, nil
}

func (a *workflowActions) Publish(
	_ context.Context,
	command workflow.PublishCommand,
) (workflow.Published, error) {
	a.got = command
	return workflow.Published{
		ID: "workflow-id", MSPID: command.Principal.Scope.MSPID,
		ClientID: command.Principal.Scope.ClientID,
		Key:      command.Key, Name: command.Name, Version: 1,
		Fallback: command.Fallback, Definition: command.Definition,
	}, nil
}

func TestPublishWorkflowMapsImmutableDefinitionAndTrustedActor(t *testing.T) {
	actions := &workflowActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Workflows: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/workflows",
		bytes.NewBufferString(`{
			"key":"default",
			"name":"Default",
			"enabled":true,
			"priority":0,
			"stable_order":0,
			"fallback":true,
			"definition":{
				"states":[{"key":"new"},{"key":"in_progress","requires_owner":true}],
				"transitions":[{"from":"new","to":"in_progress"}]
			}
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated ||
		actions.got.ActorID != "actor-id" ||
		actions.got.Source != "api" ||
		!actions.got.Fallback ||
		len(actions.got.Definition.States) != 2 {
		t.Fatalf("unexpected publish response/command: status=%d command=%+v body=%s", response.Code, actions.got, response.Body.String())
	}
}

func (a *serviceKeyActions) Issue(
	_ context.Context,
	command servicekeys.IssueCommand,
) (servicekeys.Issued, error) {
	a.issue = command
	return servicekeys.Issued{
		Record: servicekeys.Record{
			ID: "key-id", MSPID: command.Principal.Scope.MSPID,
			ClientID: command.Principal.Scope.ClientID,
			Name:     command.Name, Prefix: "prefix123",
			Capabilities: command.Capabilities, DataScopes: command.DataScopes,
			ExpiresAt: time.Now().Add(command.TTL),
		},
		Token: "rsk_prefix123_secret",
	}, nil
}

func (a *serviceKeyActions) Rotate(
	_ context.Context,
	command servicekeys.RotateCommand,
) (servicekeys.Issued, error) {
	a.rotate = command
	return servicekeys.Issued{
		Record: servicekeys.Record{ID: "replacement-id", Prefix: "newprefix"},
		Token:  "rsk_newprefix_secret",
	}, nil
}

func (a *serviceKeyActions) Revoke(
	_ context.Context,
	command servicekeys.RevokeCommand,
) error {
	a.revoke = command
	return nil
}

func TestServiceKeyLifecycleUsesTrustedActorAndKeyIDs(t *testing.T) {
	actions := &serviceKeyActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, ServiceKeys: actions,
	})
	issue := httptest.NewRequest(
		http.MethodPost, "/api/v1/admin/service-keys",
		bytes.NewBufferString(`{
			"name":"Monitoring",
			"capabilities":["work_record.create"],
			"data_scopes":["work_records"],
			"ttl_seconds":3600
		}`),
	)
	issueResponse := httptest.NewRecorder()
	handler.ServeHTTP(issueResponse, issue)
	if issueResponse.Code != http.StatusCreated {
		t.Fatalf("issue status = %d; body=%s", issueResponse.Code, issueResponse.Body.String())
	}
	if actions.issue.ActorID != "actor-id" ||
		actions.issue.Principal.Scope.ClientID != "client-id" ||
		actions.issue.TTL != time.Hour {
		t.Fatalf("unexpected issue command: %+v", actions.issue)
	}

	rotate := httptest.NewRequest(
		http.MethodPost, "/api/v1/admin/service-keys/key-id/rotate",
		bytes.NewBufferString(`{"ttl_seconds":7200,"reason":"scheduled rotation"}`),
	)
	rotateResponse := httptest.NewRecorder()
	handler.ServeHTTP(rotateResponse, rotate)
	if rotateResponse.Code != http.StatusCreated ||
		actions.rotate.KeyID != "key-id" ||
		actions.rotate.Reason != "scheduled rotation" {
		t.Fatalf("unexpected rotate response/command: status=%d command=%+v", rotateResponse.Code, actions.rotate)
	}

	revoke := httptest.NewRequest(
		http.MethodPost, "/api/v1/admin/service-keys/key-id/revoke",
		bytes.NewBufferString(`{"reason":"integration retired"}`),
	)
	revokeResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokeResponse, revoke)
	if revokeResponse.Code != http.StatusNoContent ||
		actions.revoke.KeyID != "key-id" ||
		actions.revoke.Reason != "integration retired" {
		t.Fatalf("unexpected revoke response/command: status=%d command=%+v", revokeResponse.Code, actions.revoke)
	}
}

func TestServiceKeyListReturnsMetadataWithoutSecretMaterial(t *testing.T) {
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, ServiceKeys: &serviceKeyActions{},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-keys", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d; body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"prefix":"prefix123"`) ||
		strings.Contains(body, "token") || strings.Contains(body, "hash") {
		t.Fatalf("list exposed incorrect service-key representation: %s", body)
	}
}

func (a *searchActions) Search(
	_ context.Context,
	query search.Query,
) ([]search.Result, error) {
	a.got = query
	return []search.Result{{
		ID: "work-id", ObjectType: "work_record",
		ClientID: query.Target.ClientID, Title: "Email outage",
	}}, nil
}

func TestSearchMapsAuthenticatedClientScopeAndBoundedQuery(t *testing.T) {
	actions := &searchActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Search: actions})
	request := httptest.NewRequest(
		http.MethodGet, "/api/v1/search?q=email+outage&limit=25", nil,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if actions.got.Target.MSPID != "msp-id" ||
		actions.got.Target.ClientID != "client-id" ||
		actions.got.Text != "email outage" ||
		actions.got.Limit != 25 {
		t.Fatalf("unexpected search query: %+v", actions.got)
	}
}

func (a *taskActions) Create(
	_ context.Context,
	command tasks.CreateCommand,
) (tasks.Task, error) {
	a.got = command
	return tasks.Task{
		ID: "task-id", MSPID: command.Principal.Scope.MSPID,
		ClientID:     command.Principal.Scope.ClientID,
		WorkRecordID: command.WorkRecordID, ParentTaskID: command.ParentTaskID,
		Title: command.Title, Status: "open", Position: 1, Version: 1,
		CreatedBy: command.ActorID,
	}, nil
}

func TestCreateTaskMapsOrderedSubtaskAndTrustedRequestSource(t *testing.T) {
	actions := &taskActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Tasks: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/work-records/work-id/tasks",
		bytes.NewBufferString(`{"parent_task_id":"parent-id","title":"Collect logs","tag_ids":["tag-id"]}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if actions.got.WorkRecordID != "work-id" ||
		actions.got.ParentTaskID != "parent-id" ||
		actions.got.Title != "Collect logs" ||
		actions.got.ActorID != "actor-id" ||
		actions.got.Source != "api" || len(actions.got.TagIDs) != 1 ||
		actions.got.TagIDs[0] != "tag-id" || actions.got.ClassificationPolicy != tagging.CreationRequireMeaningful {
		t.Fatalf("unexpected task command: %+v", actions.got)
	}
}

func TestCreateOpportunityTaskMapsPolymorphicParent(t *testing.T) {
	actions := &taskActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Tasks: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/opportunities/opportunity-id/tasks",
		bytes.NewBufferString(`{"title":"Prepare proposal"}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated ||
		actions.got.Parent.Type != tasks.ParentOpportunity ||
		actions.got.Parent.ID != "opportunity-id" ||
		actions.got.ActorID != "actor-id" {
		t.Fatalf("unexpected response/command: status=%d command=%+v body=%s",
			response.Code, actions.got, response.Body.String())
	}
}

func TestCreateProjectTaskMapsPolymorphicParentAndSubtask(t *testing.T) {
	actions := &taskActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Tasks: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/projects/project-id/tasks",
		bytes.NewBufferString(`{"parent_task_id":"parent-id","title":"Confirm handoff"}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated ||
		actions.got.Parent.Type != tasks.ParentProject ||
		actions.got.Parent.ID != "project-id" ||
		actions.got.ParentTaskID != "parent-id" ||
		actions.got.ActorID != "actor-id" {
		t.Fatalf("unexpected response/command: status=%d command=%+v body=%s",
			response.Code, actions.got, response.Body.String())
	}
}

func TestCreatePhaseTaskMapsPolymorphicParentAndSubtask(t *testing.T) {
	actions := &taskActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Tasks: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/phases/phase-id/tasks",
		bytes.NewBufferString(`{
			"parent_task_id":"parent-id",
			"title":"Validate discovery",
			"owner_id":"technician-id",
			"estimate_minutes":90
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated ||
		actions.got.Parent.Type != tasks.ParentPhase ||
		actions.got.Parent.ID != "phase-id" ||
		actions.got.ParentTaskID != "parent-id" ||
		actions.got.OwnerID != "technician-id" ||
		actions.got.EstimateMinutes != 90 ||
		actions.got.ActorID != "actor-id" {
		t.Fatalf("unexpected response/command: status=%d command=%+v body=%s",
			response.Code, actions.got, response.Body.String())
	}
}

func (a *relationshipActions) Create(
	_ context.Context,
	command links.CreateCommand,
) (links.Link, error) {
	a.got = command
	return links.Link{
		ID: "relationship-id", MSPID: command.Source.MSPID,
		ClientID: command.Source.ClientID, Source: command.Source,
		Target: command.Target, LinkType: command.LinkType, Version: 1,
		CreatedBy: command.ActorID,
	}, nil
}

func TestCreateRelationshipMapsScopedEndpointsAndTrustedActor(t *testing.T) {
	actions := &relationshipActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Relationships: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/relationships",
		bytes.NewBufferString(`{
			"source_type":"work_record",
			"source_id":"work-id",
			"target_type":"asset",
			"target_id":"asset-id",
			"relationship_type":"affected_asset"
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if actions.got.Source.MSPID != "msp-id" ||
		actions.got.Source.ClientID != "client-id" ||
		actions.got.Target.MSPID != "msp-id" ||
		actions.got.Target.ClientID != "client-id" ||
		actions.got.LinkType != "affected_asset" ||
		actions.got.ActorID != "actor-id" ||
		actions.got.RequestSource != "api" {
		t.Fatalf("unexpected relationship command: %+v", actions.got)
	}
}

func (a *timeEntryActions) Create(
	_ context.Context,
	command timeentries.CreateCommand,
) (timeentries.Entry, error) {
	a.got = command
	return timeentries.Entry{
		ID: "entry-id", MSPID: command.Principal.Scope.MSPID,
		ClientID:     command.Principal.Scope.ClientID,
		WorkRecordID: command.WorkRecordID, TaskID: command.TaskID,
		TechnicianID: command.TechnicianID, StartedAt: command.StartedAt,
		EndedAt: command.EndedAt, Billable: command.Billable, Version: 1,
	}, nil
}

func TestCreateTimeEntryMapsExactIntervalAndTrustedActor(t *testing.T) {
	actions := &timeEntryActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, TimeEntries: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/work-records/work-id/time-entries",
		bytes.NewBufferString(`{
			"task_id":"task-id",
			"technician_id":"technician-id",
			"started_at":"2026-07-30T14:00:00Z",
			"ended_at":"2026-07-30T14:30:00Z",
			"billable":true,
			"note":"Investigated logs",
			"tag_ids":["tag-id"]
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if actions.got.WorkRecordID != "work-id" || actions.got.TaskID != "task-id" ||
		actions.got.TechnicianID != "technician-id" || !actions.got.Billable ||
		actions.got.ActorID != "actor-id" || actions.got.Source != "api" ||
		len(actions.got.TagIDs) != 1 || actions.got.TagIDs[0] != "tag-id" ||
		actions.got.ClassificationPolicy != tagging.CreationRequireMeaningful {
		t.Fatalf("unexpected time-entry command: %+v", actions.got)
	}
}

func TestCreateProjectTaskTimeEntryUsesPathTaskAndNoWorkRecord(t *testing.T) {
	actions := &timeEntryActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, TimeEntries: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/tasks/project-task-id/time-entries",
		bytes.NewBufferString(`{
			"technician_id":"technician-id",
			"started_at":"2026-07-30T14:00:00Z",
			"ended_at":"2026-07-30T15:00:00Z",
			"billable":true,
			"note":"Project delivery"
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated ||
		actions.got.WorkRecordID != "" ||
		actions.got.TaskID != "project-task-id" ||
		actions.got.TechnicianID != "technician-id" ||
		actions.got.ActorID != "actor-id" {
		t.Fatalf("status=%d command=%+v body=%s",
			response.Code, actions.got, response.Body.String())
	}
}

func (a *commentActions) Create(
	_ context.Context,
	command comments.CreateCommand,
) (comments.Comment, error) {
	a.got = command
	return comments.Comment{
		ID: "comment-id", MSPID: command.Principal.Scope.MSPID,
		ClientID:     command.Principal.Scope.ClientID,
		WorkRecordID: command.WorkRecordID, AuthorID: command.ActorID,
		Visibility: command.Visibility, Body: command.Body, Version: 1,
	}, nil
}

func TestInternalCommentDelegatesToCollaboration(t *testing.T) {
	actions := &collaborationActionsStub{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Collaboration: actions, NewID: func() string { return "generated-key" }})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/work-records/work-id/comments",
		bytes.NewBufferString(`{
			"visibility":"internal",
			"body":"Investigating logs"
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if actions.create.Parent.ID != "work-id" || actions.create.Parent.Type != "work_record" ||
		actions.create.Principal.ID != "actor-id" || actions.create.Source != "api" ||
		actions.create.IdempotencyKey != "generated-key" || actions.create.Tokens == nil ||
		actions.create.ConfirmedTeamSnapshots == nil {
		t.Fatalf("unexpected collaboration command: %+v", actions.create)
	}
}

func TestClientCommentDelegatesToCommentsAndRejectsStructuredTokens(t *testing.T) {
	actions := &commentActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Comments: actions})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/work-records/work-id/comments", bytes.NewBufferString(`{"visibility":"client","body":"Customer update"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || actions.got.Visibility != comments.ClientVisible || actions.got.Body != "Customer update" {
		t.Fatalf("status=%d command=%+v body=%s", response.Code, actions.got, response.Body.String())
	}

	rejected := httptest.NewRecorder()
	rejectedRequest := httptest.NewRequest(http.MethodPost, "/api/v1/work-records/work-id/comments", bytes.NewBufferString(`{"visibility":"client","body":"Customer @Mira","tokens":[{"id":"token-1","target_type":"staff","target_id":"staff-2","label":"@Mira","start":9,"end":14}]}`))
	handler.ServeHTTP(rejected, rejectedRequest)
	if rejected.Code != http.StatusConflict || !strings.Contains(rejected.Body.String(), `"code":"source_not_internal"`) {
		t.Fatalf("status=%d body=%s", rejected.Code, rejected.Body.String())
	}
}

func (a *workParticipantActions) Add(
	_ context.Context,
	command workrecords.AddParticipantCommand,
) (workrecords.ParticipationResult, error) {
	a.add = command
	return workrecords.ParticipationResult{
		Record: workrecords.Record{
			Envelope: object.Envelope{ID: command.WorkRecordID, Version: command.ExpectedVersion + 1},
		},
		Participant: workrecords.Participant{
			ID: "participant-id", WorkRecordID: command.WorkRecordID,
			TechnicianID: command.TechnicianID, Role: command.Role, Version: 1,
		},
	}, nil
}

func (a *workParticipantActions) Remove(
	_ context.Context,
	command workrecords.RemoveParticipantCommand,
) (workrecords.ParticipationResult, error) {
	a.remove = command
	return workrecords.ParticipationResult{
		Record: workrecords.Record{
			Envelope: object.Envelope{ID: command.WorkRecordID, Version: command.ExpectedVersion + 1},
		},
		Participant: workrecords.Participant{
			ID: command.ParticipantID, WorkRecordID: command.WorkRecordID,
			Version: command.ParticipantVersion + 1,
		},
	}, nil
}

func TestWorkParticipationMapsVersionedAddAndReasonedRemove(t *testing.T) {
	actions := &workParticipantActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, WorkParticipants: actions,
	})
	add := httptest.NewRequest(
		http.MethodPost, "/api/v1/work-records/work-id/participants",
		bytes.NewBufferString(`{
			"expected_version":4,
			"technician_id":"technician-id",
			"role":"reviewer"
		}`),
	)
	addResponse := httptest.NewRecorder()
	handler.ServeHTTP(addResponse, add)
	if addResponse.Code != http.StatusCreated ||
		actions.add.WorkRecordID != "work-id" ||
		actions.add.ExpectedVersion != 4 ||
		actions.add.Actor.ID != "actor-id" {
		t.Fatalf("unexpected add response/command: status=%d command=%+v", addResponse.Code, actions.add)
	}

	remove := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/work-records/work-id/participants/participant-id/remove",
		bytes.NewBufferString(`{
			"expected_version":5,
			"participant_version":1,
			"reason":"review complete"
		}`),
	)
	removeResponse := httptest.NewRecorder()
	handler.ServeHTTP(removeResponse, remove)
	if removeResponse.Code != http.StatusOK ||
		actions.remove.ParticipantID != "participant-id" ||
		actions.remove.Reason != "review complete" {
		t.Fatalf("unexpected remove response/command: status=%d command=%+v", removeResponse.Code, actions.remove)
	}
}

func (a *workMergeActions) Merge(
	_ context.Context,
	command workrecords.MergeCommand,
) (workrecords.MergeResult, error) {
	a.got = command
	return workrecords.MergeResult{
		Winner: workrecords.Record{
			Envelope: object.Envelope{ID: command.WinnerID, Version: command.WinnerVersion + 1},
		},
		Duplicate: workrecords.Record{
			Envelope:     object.Envelope{ID: command.DuplicateID, Version: command.DuplicateVersion + 1},
			MergedIntoID: command.WinnerID,
		},
	}, nil
}

func TestMergeWorkRecordMapsReasonedVersionedCommand(t *testing.T) {
	actions := &workMergeActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, WorkMerges: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/work-records/winner-id/merge",
		bytes.NewBufferString(`{
			"winner_version":5,
			"duplicate_id":"duplicate-id",
			"duplicate_version":3,
			"reason":"Duplicate inbound email"
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if actions.got.WinnerID != "winner-id" || actions.got.WinnerVersion != 5 ||
		actions.got.DuplicateID != "duplicate-id" || actions.got.DuplicateVersion != 3 ||
		actions.got.Reason != "Duplicate inbound email" ||
		actions.got.Actor.ID != "actor-id" {
		t.Fatalf("unexpected merge command: %+v", actions.got)
	}
}

func (a *workQueueActions) Transfer(
	_ context.Context,
	command workrecords.QueueCommand,
) (workrecords.Record, error) {
	a.got = command
	return workrecords.Record{
		Envelope: object.Envelope{ID: command.WorkRecordID, Version: 4},
		QueueID:  command.Queue.ID,
	}, nil
}

func TestAssignWorkRecordMapsTrustedOptimisticCommand(t *testing.T) {
	actions := &workAssignmentActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, WorkAssignments: actions,
	})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/work-records/work-id/assign",
		bytes.NewBufferString(`{"expected_version":2,"owner_id":"owner-id","reason":"Reassigned after triage"}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if actions.got.WorkRecordID != "work-id" ||
		actions.got.ExpectedVersion != 2 || actions.got.OwnerID != "owner-id" ||
		actions.got.Reason != "Reassigned after triage" ||
		actions.got.Target.ClientID != "client-id" ||
		actions.got.Actor.ID != "actor-id" || actions.got.Actor.Source != "api" {
		t.Fatalf("unexpected assignment command: %+v", actions.got)
	}
}

func TestTransitionWorkRecordMapsTrustedGovernedCommand(t *testing.T) {
	actions := &workTransitionActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, WorkTransitions: actions,
	})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/work-records/work-id/transition",
		bytes.NewBufferString(`{
			"expected_version":3,
			"to_status":"in_progress",
			"reason":"triage completed"
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if actions.got.WorkRecordID != "work-id" ||
		actions.got.ExpectedVersion != 3 || actions.got.ToStatus != "in_progress" ||
		actions.got.Reason != "triage completed" ||
		actions.got.Target.ClientID != "client-id" ||
		actions.got.Actor.ID != "actor-id" || actions.got.Actor.Source != "api" {
		t.Fatalf("unexpected transition command: %+v", actions.got)
	}
	if response.Header().Get("ETag") != `"4"` {
		t.Fatalf("ETag = %q, want %q", response.Header().Get("ETag"), `"4"`)
	}
}

func TestChangeWorkRecordPriorityMapsReasonedRetentionCommand(t *testing.T) {
	actions := &workPriorityActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, WorkPriorities: actions,
	})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/work-records/work-id/priority",
		bytes.NewBufferString(`{
			"expected_version":3,
			"priority":"critical",
			"reason":"Major incident declared"
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	if actions.got.WorkRecordID != "work-id" ||
		actions.got.ExpectedVersion != 3 || actions.got.Priority != "critical" ||
		actions.got.Reason != "Major incident declared" ||
		actions.got.Actor.ID != "actor-id" {
		t.Fatalf("unexpected priority command: %+v", actions.got)
	}
}

func TestOverrideSLARequiresTrustedDualVersionsAndReason(t *testing.T) {
	actions := &slaOverrideActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, SLAOverrides: actions,
	})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/work-records/work-id/sla/override",
		bytes.NewBufferString(`{
			"expected_version":4,
			"sla_expected_version":2,
			"response_due_at":"2026-07-31T20:00:00Z",
			"reason":"Contract exclusion"
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	if actions.got.WorkRecordID != "work-id" ||
		actions.got.ExpectedVersion != 4 || actions.got.SLAExpectedVersion != 2 ||
		actions.got.ResponseDueAt == nil ||
		actions.got.Reason != "Contract exclusion" ||
		actions.got.Actor.ID != "actor-id" {
		t.Fatalf("unexpected SLA override command: %+v", actions.got)
	}
}

func TestSavedViewRoutesDeriveOwnerAndClientScope(t *testing.T) {
	actions := &viewActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Views: actions})
	save := httptest.NewRequest(
		http.MethodPost, "/api/v1/views",
		bytes.NewBufferString(`{
			"kind":"saved_search",
			"name":"Open work",
			"query":{"status":"open","client_id":"attacker-client"},
			"audience":{"type":"private"}
		}`),
	)
	saveResponse := httptest.NewRecorder()
	handler.ServeHTTP(saveResponse, save)
	if saveResponse.Code != http.StatusCreated {
		t.Fatalf("save status = %d; body=%s", saveResponse.Code, saveResponse.Body.String())
	}
	if actions.saved.OwnerID != "actor-id" || actions.saved.Principal.ID != "actor-id" {
		t.Fatalf("save did not derive trusted owner: %+v", actions.saved)
	}

	resolve := httptest.NewRequest(http.MethodGet, "/api/v1/views/view-id/resolve", nil)
	resolveResponse := httptest.NewRecorder()
	handler.ServeHTTP(resolveResponse, resolve)
	if resolveResponse.Code != http.StatusOK {
		t.Fatalf("resolve status = %d; body=%s", resolveResponse.Code, resolveResponse.Body.String())
	}
	if actions.resolved.ViewID != "view-id" ||
		actions.resolved.Target.ClientID != "client-id" {
		t.Fatalf("resolve was not scoped to active Client: %+v", actions.resolved)
	}
}

func TestPublishRoutingRulesMapsTrustedVersionedCommand(t *testing.T) {
	actions := &routingActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Routing: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/routing-rules/versions",
		bytes.NewBufferString(`{
			"expected_version":2,
			"rules":[
				{"position":1,"priority":"critical","queue_id":"noc"},
				{"position":2,"queue_id":"triage"}
			]
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	if actions.got.ExpectedVersion != 2 || len(actions.got.Rules) != 2 ||
		actions.got.ActorID != "actor-id" || actions.got.Source != "api" {
		t.Fatalf("unexpected routing publish command: %+v", actions.got)
	}
	if response.Header().Get("ETag") != `"3"` {
		t.Fatalf("ETag = %q", response.Header().Get("ETag"))
	}
}

func TestGetRoutingRulesReturnsTheCurrentMSPGlobalVersion(t *testing.T) {
	actions := &routingActions{current: routing.RuleSet{
		ID: "routing-set", MSPID: "msp-id", Version: 2,
		Rules: []routing.Rule{{ID: "fallback", Position: 1, QueueID: "triage"}},
	}}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Routing: actions})
	request := httptest.NewRequest(
		http.MethodGet, "/api/v1/routing-rules/versions", nil,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("ETag") != `"2"` ||
		!strings.Contains(response.Body.String(), `"id":"routing-set"`) {
		t.Fatalf("unexpected routing response headers/body: headers=%v body=%s", response.Header(), response.Body.String())
	}
}

func TestPublishBusinessCalendarMapsTrustedVersionedCommand(t *testing.T) {
	actions := &calendarActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, SLACalendars: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/business-calendars/calendar-id/versions",
		bytes.NewBufferString(`{
			"expected_version":2,
			"key":"support-hours",
			"name":"Support Hours",
			"definition":{
				"timezone":"America/Chicago",
				"weekly":{"monday":[{"start_minute":540,"end_minute":1020}]},
				"holidays":["2026-12-25"]
			}
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	if actions.got.CalendarID != "calendar-id" || actions.got.ExpectedVersion != 2 ||
		actions.got.Target.ClientID != "client-id" ||
		actions.got.ActorID != "actor-id" || actions.got.Source != "api" {
		t.Fatalf("unexpected calendar publish command: %+v", actions.got)
	}
	if response.Header().Get("ETag") != `"3"` {
		t.Fatalf("ETag = %q", response.Header().Get("ETag"))
	}
}

func TestPublishSLAPolicyMapsTrustedVersionedCommand(t *testing.T) {
	actions := &policyActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, SLAPolicies: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/sla-policies/policy-id/versions",
		bytes.NewBufferString(`{
			"expected_version":4,
			"key":"critical",
			"name":"Critical SLA",
			"calendar_id":"calendar-id",
			"conditions":{"priority":"critical","queue_id":"noc"},
			"response_target_seconds":900,
			"resolution_target_seconds":7200,
			"warning_percent":80,
			"pause_states":["waiting_client"],
			"enabled":true,
			"priority":100,
			"stable_order":1,
			"fallback":false
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	if actions.got.PolicyID != "policy-id" || actions.got.ExpectedVersion != 4 ||
		actions.got.Conditions.QueueID != "noc" ||
		actions.got.Target.ClientID != "client-id" ||
		actions.got.ActorID != "actor-id" {
		t.Fatalf("unexpected SLA policy command: %+v", actions.got)
	}
	if response.Header().Get("ETag") != `"5"` {
		t.Fatalf("ETag = %q", response.Header().Get("ETag"))
	}
}

func TestPublishNotificationPolicyMapsTrustedDestinationsAndVersion(t *testing.T) {
	actions := &notificationPolicyActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, NotificationPolicies: actions,
	})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/notification-policies/policy-id/versions",
		bytes.NewBufferString(`{
			"expected_version":3,
			"key":"sla-warning",
			"name":"SLA Warning",
			"event_type":"sla.warning",
			"conditions":{"priority":"critical","queue_id":"noc"},
			"destinations":[
				{"channel":"in_app","recipient_ref":"queue:noc","content_classification":"internal"},
				{"channel":"teams","recipient_ref":"teams-connection","content_classification":"restricted"}
			],
			"quiet_period_seconds":600,
			"critical_bypass":true,
			"enabled":true,
			"priority":100,
			"stable_order":10
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	if actions.got.PolicyID != "policy-id" || actions.got.ExpectedVersion != 3 ||
		actions.got.Target.ClientID != "client-id" ||
		actions.got.Conditions.QueueID != "noc" ||
		len(actions.got.Destinations) != 2 ||
		actions.got.ActorID != "actor-id" || actions.got.Source != "api" {
		t.Fatalf("unexpected notification policy command: %+v", actions.got)
	}
	if response.Header().Get("ETag") != `"4"` {
		t.Fatalf("ETag = %q", response.Header().Get("ETag"))
	}
}

func TestKnowledgeRoutesMapTrustedScopedDraftLifecycle(t *testing.T) {
	actions := &knowledgeActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Knowledge: actions})
	list := httptest.NewRequest(
		http.MethodGet, "/api/v1/knowledge/articles?q=token&limit=25", nil,
	)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK ||
		actions.listed.Query != "token" || actions.listed.Limit != 25 ||
		actions.listed.Target.ClientID != "client-id" {
		t.Fatalf("list response=%d command=%+v body=%s", listResponse.Code, actions.listed, listResponse.Body.String())
	}
	create := httptest.NewRequest(
		http.MethodPost, "/api/v1/knowledge/articles",
		bytes.NewBufferString(`{
			"display_id":"KB-100",
			"title":"Reset token",
			"body":"# Resolution\nReset token."
		}`),
	)
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusCreated ||
		actions.created.ActorID != "actor-id" ||
		actions.created.Target.ClientID != "client-id" {
		t.Fatalf("create response=%d command=%+v body=%s", createResponse.Code, actions.created, createResponse.Body.String())
	}

	revise := httptest.NewRequest(
		http.MethodPost, "/api/v1/knowledge/articles/article/versions",
		bytes.NewBufferString(`{
			"expected_version":1,
			"title":"Rotate token",
			"body":"Updated body"
		}`),
	)
	reviseResponse := httptest.NewRecorder()
	handler.ServeHTTP(reviseResponse, revise)
	if reviseResponse.Code != http.StatusCreated ||
		actions.revised.ArticleID != "article" ||
		actions.revised.ExpectedVersion != 1 ||
		reviseResponse.Header().Get("ETag") != `"2"` {
		t.Fatalf("revise response=%d command=%+v body=%s", reviseResponse.Code, actions.revised, reviseResponse.Body.String())
	}

	publish := httptest.NewRequest(
		http.MethodPost, "/api/v1/knowledge/articles/article/publish",
		bytes.NewBufferString(`{"expected_version":2,"reason":"Reviewed for internal use"}`),
	)
	publishResponse := httptest.NewRecorder()
	handler.ServeHTTP(publishResponse, publish)
	if publishResponse.Code != http.StatusOK ||
		actions.published.ArticleID != "article" ||
		actions.published.ExpectedVersion != 2 ||
		actions.published.Reason != "Reviewed for internal use" {
		t.Fatalf("publish response=%d command=%+v body=%s", publishResponse.Code, actions.published, publishResponse.Body.String())
	}

	find := httptest.NewRequest(
		http.MethodGet, "/api/v1/knowledge/articles/article", nil,
	)
	findResponse := httptest.NewRecorder()
	handler.ServeHTTP(findResponse, find)
	if findResponse.Code != http.StatusOK ||
		actions.found.ArticleID != "article" ||
		actions.found.Target.ClientID != "client-id" {
		t.Fatalf("find response=%d command=%+v body=%s", findResponse.Code, actions.found, findResponse.Body.String())
	}
	if !strings.Contains(findResponse.Body.String(), `"id":"article"`) ||
		strings.Contains(findResponse.Body.String(), `"ID"`) {
		t.Fatalf("knowledge response is not API-shaped: %s", findResponse.Body.String())
	}
}

func TestBillingRoutesMapTrustedApprovalAndReturnCSV(t *testing.T) {
	actions := &billingExportActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, BillingExports: actions,
	})
	list := httptest.NewRequest(
		http.MethodGet, "/api/v1/time-entries/approvals?state=pending&limit=25", nil,
	)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK ||
		actions.listed.State != billingexport.PendingApproval ||
		actions.listed.Limit != 25 ||
		actions.listed.Target.ClientID != "client-id" {
		t.Fatalf("list response=%d command=%+v body=%s", listResponse.Code, actions.listed, listResponse.Body.String())
	}
	approval := httptest.NewRequest(
		http.MethodPost, "/api/v1/time-entries/entry/approval",
		bytes.NewBufferString(`{
			"expected_version":2,
			"decision":"approved",
			"reason":"Reviewed against contract"
		}`),
	)
	approvalResponse := httptest.NewRecorder()
	handler.ServeHTTP(approvalResponse, approval)
	if approvalResponse.Code != http.StatusOK ||
		actions.approval.EntryID != "entry" ||
		actions.approval.ExpectedVersion != 2 ||
		actions.approval.ActorID != "actor-id" ||
		actions.approval.Target.ClientID != "client-id" ||
		approvalResponse.Header().Get("ETag") != `"3"` ||
		!strings.Contains(approvalResponse.Body.String(), `"id":"entry"`) ||
		strings.Contains(approvalResponse.Body.String(), `"ID"`) {
		t.Fatalf("approval response=%d command=%+v body=%s", approvalResponse.Code, actions.approval, approvalResponse.Body.String())
	}

	export := httptest.NewRequest(
		http.MethodPost, "/api/v1/billing-exports",
		bytes.NewBufferString(`{
			"from":"2026-07-01T00:00:00Z",
			"through":"2026-08-01T00:00:00Z"
		}`),
	)
	exportResponse := httptest.NewRecorder()
	handler.ServeHTTP(exportResponse, export)
	if exportResponse.Code != http.StatusOK ||
		actions.export.Target.ClientID != "client-id" ||
		actions.export.ActorID != "actor-id" ||
		exportResponse.Header().Get("X-Rarity-Export-ID") != "export-id" ||
		exportResponse.Header().Get("Content-Type") != "text/csv; charset=utf-8" ||
		!strings.Contains(exportResponse.Body.String(), "entry,3600") {
		t.Fatalf("export response=%d command=%+v headers=%v body=%s", exportResponse.Code, actions.export, exportResponse.Header(), exportResponse.Body.String())
	}
}

func TestBillingExportClassificationFailurePointsToBlockingEntry(t *testing.T) {
	actions := &billingExportActions{err: &billingexport.ClassificationRequiredError{
		EntryID: "entry-needing-tags", Err: tagging.ErrMeaningfulTagRequired,
	}}
	handler := NewRouter(Dependencies{Principal: testPrincipal, BillingExports: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/billing-exports",
		bytes.NewBufferString(`{"from":"2026-07-01T00:00:00Z","through":"2026-08-01T00:00:00Z"}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(response.Body.String(), `"code":"classification_required"`) ||
		!strings.Contains(response.Body.String(), `"recovery_url":"/api/v1/objects/time_entry/entry-needing-tags/tags"`) {
		t.Fatalf("response=%d body=%s", response.Code, response.Body.String())
	}
}

func TestIntegrationHealthRouteUsesTrustedPrincipalAndAPIShape(t *testing.T) {
	actions := &integrationHealthActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, IntegrationHealth: actions,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/health", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		actions.got.Principal.ID != "actor-id" ||
		!strings.Contains(response.Body.String(), `"state":"degraded"`) ||
		!strings.Contains(response.Body.String(), `"last_error_code":"delivery_failed"`) ||
		strings.Contains(response.Body.String(), `"State"`) {
		t.Fatalf("health response=%d command=%+v body=%s", response.Code, actions.got, response.Body.String())
	}
}

func TestDirectIntakeRoutePreservesRawPayloadAndTrustedScope(t *testing.T) {
	actions := &directIntakeActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, DirectIntake: actions,
	})
	payload := `{"title":"VPN unavailable","nested":{"source":"monitor"}}`
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/intake/direct",
		bytes.NewBufferString(payload),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "remote-100")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted ||
		actions.got.ExternalID != "remote-100" ||
		string(actions.got.Payload) != payload ||
		actions.got.Target.ClientID != "client-id" ||
		actions.got.ActorID != "actor-id" ||
		!strings.Contains(response.Body.String(), `"id":"inbound"`) ||
		strings.Contains(response.Body.String(), `"raw_payload_ref"`) ||
		strings.Contains(response.Body.String(), `"msp_id"`) ||
		strings.Contains(response.Body.String(), `"client_id"`) ||
		strings.Contains(response.Body.String(), `"ID"`) {
		t.Fatalf("direct intake response=%d command=%+v body=%s", response.Code, actions.got, response.Body.String())
	}
}

func TestDirectIntakeRouteRejectsMalformedJSONBeforeService(t *testing.T) {
	actions := &directIntakeActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, DirectIntake: actions,
	})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/intake/direct",
		bytes.NewBufferString(`{"title":`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "remote-100")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || actions.calls != 0 {
		t.Fatalf("malformed JSON response=%d calls=%d body=%s", response.Code, actions.calls, response.Body.String())
	}
}

func TestInboundWebhookRoutePreservesExactBodyWithoutSessionPrincipal(t *testing.T) {
	actions := &inboundWebhookActions{}
	handler := NewRouter(Dependencies{InboundWebhooks: actions})
	payload := `{"event":"monitor.alert","nested":{"state":"open"}}`
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/webhooks/inbound/connection-id",
		bytes.NewBufferString(payload),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Rarity-Event-ID", "remote-event")
	request.Header.Set("X-Rarity-Timestamp", "2026-07-29T23:00:00Z")
	request.Header.Set("X-Rarity-Signature", "v1=signature")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted ||
		actions.got.ConnectionID != "connection-id" ||
		actions.got.EventID != "remote-event" ||
		string(actions.got.Body) != payload ||
		!strings.Contains(response.Body.String(), `"source":"inbound_webhook"`) ||
		strings.Contains(response.Body.String(), `"raw_payload_ref"`) ||
		strings.Contains(response.Body.String(), `"msp_id"`) ||
		strings.Contains(response.Body.String(), `"client_id"`) {
		t.Fatalf("webhook response=%d command=%+v body=%s", response.Code, actions.got, response.Body.String())
	}
}

func TestGraphNotificationRouteCompletesValidationHandshakeWithoutServiceCall(t *testing.T) {
	actions := &graphNotificationActions{}
	handler := NewRouter(Dependencies{GraphNotifications: actions})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/graph/notifications?validationToken=opaque%20challenge",
		nil,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		response.Header().Get("Content-Type") != "text/plain; charset=utf-8" ||
		response.Body.String() != "opaque challenge" ||
		actions.calls != 0 {
		t.Fatalf("validation response=%d calls=%d body=%q", response.Code, actions.calls, response.Body.String())
	}
}

func TestGraphNotificationRouteQueuesExactJSONWithoutSessionPrincipal(t *testing.T) {
	actions := &graphNotificationActions{}
	handler := NewRouter(Dependencies{GraphNotifications: actions})
	payload := `{"value":[{"subscriptionId":"subscription-id"}]}`
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/graph/notifications",
		bytes.NewBufferString(payload),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted ||
		string(actions.body) != payload ||
		!strings.Contains(response.Body.String(), `"accepted":1`) {
		t.Fatalf("notification response=%d body=%s accepted=%q", response.Code, response.Body.String(), actions.body)
	}
}

func TestInboundWebhookRouteHidesConnectionAndSignatureFailures(t *testing.T) {
	for _, domainErr := range []error{
		scope.ErrNotFound, webhooks.ErrInvalidSignature, webhooks.ErrStaleTimestamp,
	} {
		actions := &inboundWebhookActions{err: domainErr}
		handler := NewRouter(Dependencies{InboundWebhooks: actions})
		request := httptest.NewRequest(
			http.MethodPost, "/api/v1/webhooks/inbound/connection-id",
			bytes.NewBufferString(`{}`),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Rarity-Event-ID", "remote-event")
		request.Header.Set("X-Rarity-Timestamp", "2026-07-29T23:00:00Z")
		request.Header.Set("X-Rarity-Signature", "v1=signature")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized ||
			!strings.Contains(response.Body.String(), `"code":"webhook_authentication_failed"`) {
			t.Fatalf("domain error=%v response=%d body=%s", domainErr, response.Code, response.Body.String())
		}
	}
}

func TestForwardingIntakeRoutePreservesExactMIMEAndTrustedRelay(t *testing.T) {
	actions := &forwardingIntakeActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, ForwardingIntake: actions,
	})
	mime := "From: alerts@customer.example\r\nMessage-ID: <message@example.com>\r\n\r\nAlert"
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/intake/forwarding/connection-id",
		bytes.NewBufferString(mime),
	)
	request.Header.Set("Content-Type", "message/rfc822")
	request.Header.Set("X-Rarity-Envelope-From", "alerts@customer.example")
	request.Header.Set("X-Rarity-Header-From", "alerts@customer.example")
	request.Header.Set("X-Rarity-Recipient", "intake+abc@rarity.example")
	request.Header.Set("X-Rarity-Message-ID", "<message@example.com>")
	request.Header.Set("X-Rarity-SPF", "pass")
	request.Header.Set("X-Rarity-DMARC", "pass")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted ||
		actions.got.ConnectionID != "connection-id" ||
		actions.got.Principal.ID != "actor-id" ||
		actions.got.ActorID != "actor-id" ||
		string(actions.got.RawMIME) != mime ||
		!actions.got.Message.Authentication.SPF ||
		!actions.got.Message.Authentication.DMARC ||
		!strings.Contains(response.Body.String(), `"state":"received"`) {
		t.Fatalf("forwarding response=%d command=%+v body=%s", response.Code, actions.got, response.Body.String())
	}
}

func TestRouteWorkRecordRequiresReasonAndScopesQueue(t *testing.T) {
	actions := &workQueueActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, WorkQueues: actions,
	})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/work-records/work-id/route",
		bytes.NewBufferString(`{
			"expected_version":3,
			"queue_id":"queue-id",
			"reason":"matched client routing rule"
		}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if actions.got.Queue.ID != "queue-id" ||
		actions.got.Queue.MSPID != "msp-id" ||
		actions.got.ExpectedVersion != 3 ||
		actions.got.Reason != "matched client routing rule" {
		t.Fatalf("unexpected queue command: %+v", actions.got)
	}
}

func (a *workRecordActions) Create(
	_ context.Context,
	command workrecords.CreateCommand,
) (workrecords.Record, error) {
	a.got = command
	return workrecords.Record{
		Envelope: object.Envelope{
			ID: "work-id", MSPID: command.Principal.Scope.MSPID,
			ClientID: command.Principal.Scope.ClientID, Version: 1,
		},
		Type: command.Type, Title: command.Title, Status: command.Status,
		Priority: command.Priority,
	}, nil
}

func TestCreateWorkRecordUsesTrustedActorAndActiveClient(t *testing.T) {
	actions := &workRecordActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, WorkRecords: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/work-records",
		bytes.NewBufferString(`{
			"display_id":"INC-100",
			"type":"incident",
			"title":"Email unavailable",
			"description":"Multiple users affected",
			"status":"new",
			"priority":"high",
			"tag_ids":["tag-id"]
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if actions.got.Target.ClientID != "client-id" ||
		actions.got.Actor.ID != "actor-id" ||
		actions.got.Actor.Type != "technician" ||
		actions.got.Actor.Source != "api" ||
		actions.got.Type != workrecords.Incident || len(actions.got.TagIDs) != 1 ||
		actions.got.TagIDs[0] != "tag-id" || actions.got.ClassificationPolicy != tagging.CreationRequireMeaningful {
		t.Fatalf("unexpected work-record command: %+v", actions.got)
	}
}

func (a *assetActions) CreateAsset(
	_ context.Context,
	command clientresources.CreateAssetCommand,
) (clientresources.Asset, error) {
	a.got = command
	return clientresources.Asset{
		Envelope: object.Envelope{
			ID: "asset-id", MSPID: command.Principal.Scope.MSPID,
			ClientID: command.Principal.Scope.ClientID, Version: 1,
		},
		LocationID: command.Location.ID, Name: command.Name,
		AssetType: command.AssetType,
		Provenance: clientresources.Provenance{
			SourceSystem: command.SourceSystem, ExternalID: command.ExternalID,
			Authority: command.Authority,
		},
	}, nil
}

func TestCreateAssetPreservesScopedProvenance(t *testing.T) {
	actions := &assetActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Assets: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/assets",
		bytes.NewBufferString(`{
			"display_id":"ASSET-1",
			"name":"MAIL01",
			"asset_type":"server",
			"location_id":"00000000-0000-4000-8000-000000000010",
			"source_system":"datto",
			"external_id":"device-100",
			"authority":"discovered"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if actions.got.Location.ClientID != "client-id" ||
		actions.got.SourceSystem != "datto" ||
		actions.got.ExternalID != "device-100" ||
		actions.got.Authority != clientresources.Discovered {
		t.Fatalf("unexpected asset command: %+v", actions.got)
	}
}

func TestCreateAssetRejectsMalformedLocationBeforePersistence(t *testing.T) {
	actions := &assetActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Assets: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/assets",
		bytes.NewBufferString(`{
			"display_id":"ASSET-1",
			"name":"MAIL01",
			"asset_type":"server",
			"location_id":"not-a-uuid",
			"authority":"discovered"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf(
			"status = %d, want %d; body=%s",
			response.Code, http.StatusUnprocessableEntity, response.Body.String(),
		)
	}
	if actions.got.DisplayID != "" {
		t.Fatalf("malformed location reached asset service: %+v", actions.got)
	}
}

func (a *serviceRecordActions) CreateService(
	_ context.Context,
	command clientresources.CreateServiceCommand,
) (clientresources.ServiceRecord, error) {
	a.got = command
	return clientresources.ServiceRecord{
		Envelope: object.Envelope{
			ID: "service-id", MSPID: command.Principal.Scope.MSPID,
			ClientID: command.Principal.Scope.ClientID, Version: 1,
		},
		Name: command.Name, Criticality: command.Criticality,
	}, nil
}

func TestCreateServicePreservesCriticalityInActiveClient(t *testing.T) {
	actions := &serviceRecordActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Services: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/services",
		bytes.NewBufferString(`{
			"display_id":"SERVICE-1",
			"name":"Email",
			"criticality":"high"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if actions.got.Target.ClientID != "client-id" ||
		actions.got.Name != "Email" || actions.got.Criticality != "high" {
		t.Fatalf("unexpected service command: %+v", actions.got)
	}
}

func (a *contactActions) CreateContact(
	_ context.Context,
	command clientresources.CreateContactCommand,
) (clientresources.Contact, error) {
	a.got = command
	return clientresources.Contact{
		Envelope: object.Envelope{
			ID: "contact-id", MSPID: command.Principal.Scope.MSPID,
			ClientID: command.Principal.Scope.ClientID, Version: 1,
		},
		LocationID: command.Location.ID, DisplayName: command.DisplayName,
		Email: command.Email,
	}, nil
}

func TestCreateContactScopesOptionalLocationToActiveClient(t *testing.T) {
	actions := &contactActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Contacts: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/contacts",
		bytes.NewBufferString(`{
			"display_id":"CONTACT-1",
			"display_name":"Alex Client",
			"email":"alex@example.com",
			"phone":"555-0100",
			"location_id":"00000000-0000-4000-8000-000000000010"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if actions.got.Location.ID != "00000000-0000-4000-8000-000000000010" ||
		actions.got.Location.MSPID != "msp-id" ||
		actions.got.Location.ClientID != "client-id" ||
		actions.got.DisplayName != "Alex Client" || actions.got.Phone != "555-0100" {
		t.Fatalf("unexpected contact command: %+v", actions.got)
	}
}

func TestCreateContactRejectsMalformedLocationBeforePersistence(t *testing.T) {
	actions := &contactActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Contacts: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/contacts",
		bytes.NewBufferString(`{
			"display_id":"CONTACT-1",
			"display_name":"Alex Client",
			"location_id":"not-a-uuid"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf(
			"status = %d, want %d; body=%s",
			response.Code, http.StatusUnprocessableEntity, response.Body.String(),
		)
	}
	if actions.got.DisplayID != "" {
		t.Fatalf("malformed location reached contact service: %+v", actions.got)
	}
}

func (a *contractActions) CreateContract(
	_ context.Context,
	command clientresources.CreateContractCommand,
) (clientresources.Contract, error) {
	a.got = command
	return clientresources.Contract{
		Envelope: object.Envelope{
			ID: "contract-id", MSPID: command.Principal.Scope.MSPID,
			ClientID: command.Principal.Scope.ClientID, Version: 1,
		},
		Name: command.Name, StartsOn: command.StartsOn, EndsOn: command.EndsOn,
	}, nil
}

func TestCreateContractMapsEffectiveWindowInActiveClient(t *testing.T) {
	actions := &contractActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Contracts: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/contracts",
		bytes.NewBufferString(`{
			"display_id":"CONTRACT-1",
			"name":"Managed Services",
			"starts_on":"2026-08-01T00:00:00Z",
			"ends_on":"2027-07-31T00:00:00Z"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if actions.got.DisplayID != "CONTRACT-1" ||
		actions.got.Target.ClientID != "client-id" ||
		actions.got.StartsOn.Format("2006-01-02") != "2026-08-01" ||
		actions.got.EndsOn == nil ||
		actions.got.EndsOn.Format("2006-01-02") != "2027-07-31" {
		t.Fatalf("unexpected contract command: %+v", actions.got)
	}
}

func (a *locationActions) CreateLocation(
	_ context.Context,
	command clientresources.CreateLocationCommand,
) (clientresources.Location, error) {
	a.got = command
	if a.err != nil {
		return clientresources.Location{}, a.err
	}
	return clientresources.Location{
		Envelope: object.Envelope{
			ID: "location-id", MSPID: command.Principal.Scope.MSPID,
			ClientID: command.Principal.Scope.ClientID, Version: 1,
		},
		Name: command.Name,
	}, nil
}

func TestCreateLocationUsesActiveClientPrincipal(t *testing.T) {
	actions := &locationActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Locations: actions})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/locations",
		bytes.NewBufferString(`{"display_id":"LOC-1","name":"Headquarters"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if actions.got.DisplayID != "LOC-1" || actions.got.Name != "Headquarters" ||
		actions.got.Target.ClientID != "client-id" ||
		actions.got.ActorID != "actor-id" || actions.got.Source != "api" {
		t.Fatalf("unexpected location command: %+v", actions.got)
	}
}

func TestCreateLocationReturnsSafeResourceIdentityConflict(t *testing.T) {
	actions := &locationActions{err: clientresources.ErrResourceIdentityConflict}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Locations: actions})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost, "/api/v1/locations",
		bytes.NewBufferString(`{"display_id":"LOC-1","name":"Headquarters"}`),
	))
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "resource_identity_conflict" ||
		body.Error.Message != "resource identity conflicts with an existing record" {
		t.Fatalf("error response=%+v", body.Error)
	}
}

func TestClientResourceCreateRoutesRejectCallerSuppliedPreparedIdentities(t *testing.T) {
	locations := &locationActions{}
	contacts := &contactActions{}
	assets := &assetActions{}
	services := &serviceRecordActions{}
	contracts := &contractActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, Locations: locations, Contacts: contacts,
		Assets: assets, Services: services, Contracts: contracts,
	})
	for _, test := range []struct {
		path string
		body string
	}{
		{"/api/v1/locations", `{"display_id":"LOC-1","name":"Headquarters","prepared":{"resource_id":"019fb3c2-0000-7000-8000-000000000301"}}`},
		{"/api/v1/contacts", `{"display_id":"CONTACT-1","display_name":"Alex","correlation_id":"019fb3c2-0000-7000-8000-000000000302"}`},
		{"/api/v1/assets", `{"display_id":"ASSET-1","name":"MAIL01","asset_type":"server","authority":"discovered","actor_id":"model"}`},
		{"/api/v1/services", `{"display_id":"SERVICE-1","name":"Email","target":{"client_id":"other"}}`},
		{"/api/v1/contracts", `{"display_id":"CONTRACT-1","name":"Managed Services","starts_on":"2026-08-05T00:00:00Z","source":"model"}`},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body)))
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("path=%s status=%d body=%s", test.path, response.Code, response.Body.String())
		}
	}
	if locations.got.Prepared != (clientresources.PreparedIdentity{}) ||
		contacts.got.Prepared != (clientresources.PreparedIdentity{}) ||
		assets.got.Prepared != (clientresources.PreparedIdentity{}) ||
		services.got.Prepared != (clientresources.PreparedIdentity{}) ||
		contracts.got.Prepared != (clientresources.PreparedIdentity{}) {
		t.Fatalf("public route set trusted prepared identity: locations=%+v contacts=%+v assets=%+v services=%+v contracts=%+v", locations.got, contacts.got, assets.got, services.got, contracts.got)
	}
}

func TestListClientResourcesUsesActiveClientScope(t *testing.T) {
	actions := &clientResourceCatalogActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, ClientResourceCatalog: actions,
	})
	request := httptest.NewRequest(
		http.MethodGet, "/api/v1/client-resources?limit=250", nil,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		actions.got.Target.ClientID != "client-id" ||
		actions.got.Limit != 250 ||
		!strings.Contains(response.Body.String(), `"kind":"asset"`) {
		t.Fatalf(
			"response=%d command=%+v body=%s",
			response.Code, actions.got, response.Body.String(),
		)
	}
}

func TestGetClientResourceUsesExactIDAndActiveClientScope(t *testing.T) {
	actions := &clientResourceCatalogActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, ClientResourceCatalog: actions})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/client-resources/off-page-asset", nil))
	if response.Code != http.StatusOK || actions.get.ID != "off-page-asset" || actions.get.Target.ClientID != "client-id" {
		t.Fatalf("status=%d command=%+v body=%s", response.Code, actions.get, response.Body.String())
	}
}

func TestGetTaskUsesStandaloneExactIDContract(t *testing.T) {
	actions := &taskActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Tasks: actions})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/tasks/standalone-task", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"standalone-task"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestListClientResourcesRejectsUnknownLifecycleFilterBeforeQuery(t *testing.T) {
	actions := &clientResourceCatalogActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, ClientResourceCatalog: actions,
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet, "/api/v1/client-resources?lifecycle=deleted", nil,
	))
	if response.Code != http.StatusUnprocessableEntity || actions.calls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, actions.calls, response.Body.String())
	}
}

type clientResourceQueryActions struct {
	got            clientresources.GetCommand
	lifecycleGot   clientresources.LifecycleGetCommand
	item           clientresources.ResourceDetail
	lifecycleItem  clientresources.ResourceDetail
	err            error
	calls          int
	lifecycleCalls int
}

func (a *clientResourceQueryActions) GetForLifecycle(
	_ context.Context,
	command clientresources.LifecycleGetCommand,
) (clientresources.ResourceDetail, error) {
	a.lifecycleCalls++
	a.lifecycleGot = command
	return a.lifecycleItem, a.err
}

func (a *clientResourceQueryActions) Get(
	_ context.Context,
	command clientresources.GetCommand,
) (clientresources.ResourceDetail, error) {
	a.calls++
	a.got = command
	return a.item, a.err
}

func clientResourceLifecycleDependencies(kind clientresources.Kind, actions *clientResourceLifecycleActions) Dependencies {
	dependencies := Dependencies{Principal: testPrincipal}
	switch kind {
	case clientresources.LocationKind:
		dependencies.LocationLifecycle = actions
	case clientresources.ContactKind:
		dependencies.ContactLifecycle = actions
	case clientresources.AssetKind:
		dependencies.AssetLifecycle = actions
	case clientresources.ServiceKind:
		dependencies.ServiceLifecycle = actions
	case clientresources.ContractKind:
		dependencies.ContractLifecycle = actions
	}
	return dependencies
}

type clientResourceLifecycleActions struct {
	update     clientresources.UpdateCommand
	lifecycle  clientresources.LifecycleCommand
	operation  string
	result     clientresources.ResourceDetail
	err        error
	writeCalls int
}

func (a *clientResourceLifecycleActions) Update(
	_ context.Context,
	command clientresources.UpdateCommand,
) (clientresources.ResourceDetail, error) {
	a.update, a.operation = command, "update"
	a.writeCalls++
	return a.result, a.err
}

func (a *clientResourceLifecycleActions) Deactivate(
	_ context.Context,
	command clientresources.LifecycleCommand,
) (clientresources.ResourceDetail, error) {
	a.lifecycle, a.operation = command, "deactivate"
	a.writeCalls++
	return a.result, a.err
}

func (a *clientResourceLifecycleActions) Reactivate(
	_ context.Context,
	command clientresources.LifecycleCommand,
) (clientresources.ResourceDetail, error) {
	a.lifecycle, a.operation = command, "reactivate"
	a.writeCalls++
	return a.result, a.err
}

func TestClientResourceLifecycleGetReturnsExactScopedResourceAndETag(t *testing.T) {
	const assetID = "00000000-0000-4000-8000-000000000101"
	queries := &clientResourceQueryActions{item: clientresources.ResourceDetail{
		Summary: clientresources.Summary{ID: assetID, Kind: "asset", DisplayID: "AST-1", Name: "Firewall", Version: 7, LifecycleState: "active"}, AssetType: "firewall",
	}}
	handler := NewRouter(Dependencies{Principal: testPrincipal, ClientResourceQueries: queries})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+assetID, nil))
	if response.Code != http.StatusOK || response.Header().Get("ETag") != `"7"` ||
		queries.got.Target != (scope.Target{MSPID: "msp-id", ClientID: "client-id"}) ||
		queries.got.Kind != clientresources.AssetKind || queries.got.ID != assetID ||
		!strings.Contains(response.Body.String(), `"asset_type":"firewall"`) {
		t.Fatalf("status=%d headers=%v command=%+v body=%s", response.Code, response.Header(), queries.got, response.Body.String())
	}
}

func TestClientResourceLifecycleGetCanReloadInactiveResourceWithLifecycleAuthority(t *testing.T) {
	const assetID = "00000000-0000-4000-8000-000000000111"
	queries := &clientResourceQueryActions{
		item: clientresources.ResourceDetail{Summary: clientresources.Summary{
			ID: assetID, Kind: "asset", DisplayID: "AST-1", Name: "Firewall", Version: 6,
		}},
		lifecycleItem: clientresources.ResourceDetail{
			Summary: clientresources.Summary{ID: assetID, Kind: "asset", DisplayID: "AST-1", Name: "Firewall", Version: 7, LifecycleState: "inactive"}, AssetType: "firewall",
		},
	}
	principal := func(*http.Request) (authorization.Principal, error) {
		return authorization.Principal{
			ID: "actor-id", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
			Capabilities: authorization.NewCapabilitySet("search.read", "asset.lifecycle"),
		}, nil
	}
	handler := NewRouter(Dependencies{Principal: principal, ClientResourceQueries: queries})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+assetID, nil))
	if response.Code != http.StatusOK || response.Header().Get("ETag") != `"7"` ||
		queries.calls != 0 || queries.lifecycleCalls != 1 ||
		queries.lifecycleGot.Kind != clientresources.AssetKind ||
		!strings.Contains(response.Body.String(), `"lifecycle_state":"inactive"`) {
		t.Fatalf("status=%d headers=%v get=%d lifecycle=%d command=%+v body=%s", response.Code, response.Header(), queries.calls, queries.lifecycleCalls, queries.lifecycleGot, response.Body.String())
	}
}

func TestClientResourceUpdateRequiresQuotedMatchingIfMatch(t *testing.T) {
	const contactID = "00000000-0000-4000-8000-000000000102"
	for _, test := range []struct {
		name, ifMatch, body string
	}{
		{name: "missing", body: `{"expected_version":3,"reason":"Correct contact","display_name":"Ada"}`},
		{name: "unquoted", ifMatch: `3`, body: `{"expected_version":3,"reason":"Correct contact","display_name":"Ada"}`},
		{name: "mismatch", ifMatch: `"4"`, body: `{"expected_version":3,"reason":"Correct contact","display_name":"Ada"}`},
		{name: "explicit zero", ifMatch: `"3"`, body: `{"expected_version":0,"reason":"Correct contact","display_name":"Ada"}`},
		{name: "explicit negative", ifMatch: `"3"`, body: `{"expected_version":-1,"reason":"Correct contact","display_name":"Ada"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			actions := &clientResourceLifecycleActions{}
			handler := NewRouter(Dependencies{Principal: testPrincipal, ContactLifecycle: actions})
			request := httptest.NewRequest(http.MethodPatch, "/api/v1/contacts/"+contactID, strings.NewReader(test.body))
			if test.ifMatch != "" {
				request.Header.Set("If-Match", test.ifMatch)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnprocessableEntity || actions.writeCalls != 0 {
				t.Fatalf("If-Match=%q status=%d writes=%d body=%s", test.ifMatch, response.Code, actions.writeCalls, response.Body.String())
			}
		})
	}
}

func TestClientResourceUpdateMapsOnlyApprovedPatchAndExplicitClears(t *testing.T) {
	const contactID = "00000000-0000-4000-8000-000000000102"
	actions := &clientResourceLifecycleActions{result: clientresources.ResourceDetail{
		Summary: clientresources.Summary{ID: contactID, Kind: "contact", DisplayID: "CON-1", Name: "Ada", Version: 4, LifecycleState: "active"},
	}}
	handler := NewRouter(Dependencies{Principal: testPrincipal, ContactLifecycle: actions})
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/contacts/"+contactID, strings.NewReader(
		`{"expected_version":3,"reason":"Correct contact record","display_name":"Ada Lovelace","email":"","location_id":""}`,
	))
	request.Header.Set("If-Match", `"3"`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("ETag") != `"4"` || actions.writeCalls != 1 {
		t.Fatalf("status=%d headers=%v writes=%d body=%s", response.Code, response.Header(), actions.writeCalls, response.Body.String())
	}
	command := actions.update
	if command.Kind != clientresources.ContactKind || command.ResourceID != contactID || command.ExpectedVersion != 3 ||
		command.Reason != "Correct contact record" || command.ActorID != "actor-id" || command.Source != "api" || command.CorrelationID == "" ||
		command.Patch.DisplayName == nil || *command.Patch.DisplayName != "Ada Lovelace" ||
		command.Patch.Email == nil || *command.Patch.Email != "" || command.Patch.LocationID == nil || *command.Patch.LocationID != "" ||
		command.Patch.Phone != nil || command.Patch.Name != nil {
		t.Fatalf("unexpected update command: %+v patch=%+v", command, command.Patch)
	}
}

func TestClientResourceDeactivateAndReactivateUseReasonedVersionedService(t *testing.T) {
	const locationID = "00000000-0000-4000-8000-000000000103"
	for _, test := range []struct {
		path, operation string
	}{
		{path: "/api/v1/locations/" + locationID + "/deactivate", operation: "deactivate"},
		{path: "/api/v1/locations/" + locationID + "/reactivate", operation: "reactivate"},
	} {
		t.Run(test.operation, func(t *testing.T) {
			actions := &clientResourceLifecycleActions{result: clientresources.ResourceDetail{
				Summary: clientresources.Summary{ID: locationID, Kind: "location", DisplayID: "LOC-1", Name: "Office", Version: 6},
			}}
			handler := NewRouter(Dependencies{Principal: testPrincipal, LocationLifecycle: actions})
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(`{"expected_version":5,"reason":"Office status changed"}`))
			request.Header.Set("If-Match", `"5"`)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || actions.operation != test.operation || actions.lifecycle.Kind != clientresources.LocationKind ||
				actions.lifecycle.ResourceID != locationID || actions.lifecycle.ExpectedVersion != 5 ||
				actions.lifecycle.Reason != "Office status changed" || actions.lifecycle.CorrelationID == "" {
				t.Fatalf("status=%d operation=%q command=%+v body=%s", response.Code, actions.operation, actions.lifecycle, response.Body.String())
			}
		})
	}
}

func TestClientResourceLifecycleErrorsAreSafeAndTyped(t *testing.T) {
	const locationID = "00000000-0000-4000-8000-000000000103"
	for _, test := range []struct {
		err  error
		code string
		want int
	}{
		{err: scope.ErrNotFound, code: "not_found", want: http.StatusNotFound},
		{err: object.ErrVersionConflict, code: "version_conflict", want: http.StatusConflict},
		{err: clientresources.ErrLifecycleConflict, code: "lifecycle_conflict", want: http.StatusConflict},
		{err: clientresources.ErrResourceInUse, code: "resource_in_use", want: http.StatusConflict},
		{err: clientresources.ErrResourceAuthorityConflict, code: "resource_authority_conflict", want: http.StatusConflict},
		{err: clientresources.ErrInvalid, code: "validation_failed", want: http.StatusUnprocessableEntity},
	} {
		actions := &clientResourceLifecycleActions{err: test.err}
		handler := NewRouter(Dependencies{Principal: testPrincipal, LocationLifecycle: actions})
		request := httptest.NewRequest(http.MethodPost, "/api/v1/locations/"+locationID+"/deactivate", strings.NewReader(`{"expected_version":5,"reason":"Retired"}`))
		request.Header.Set("If-Match", `"5"`)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var body ErrorResponse
		_ = json.Unmarshal(response.Body.Bytes(), &body)
		if response.Code != test.want || body.Error.Code != test.code || strings.Contains(response.Body.String(), locationID) {
			t.Fatalf("error=%v status=%d response=%+v", test.err, response.Code, body)
		}
	}
}

func TestClientResourceLifecycleRoutesRejectMalformedPathUUIDAsSafeNotFoundForEveryKind(t *testing.T) {
	for _, test := range []struct {
		plural string
		kind   clientresources.Kind
	}{
		{plural: "locations", kind: clientresources.LocationKind},
		{plural: "contacts", kind: clientresources.ContactKind},
		{plural: "assets", kind: clientresources.AssetKind},
		{plural: "services", kind: clientresources.ServiceKind},
		{plural: "contracts", kind: clientresources.ContractKind},
	} {
		t.Run(test.plural, func(t *testing.T) {
			queries := &clientResourceQueryActions{}
			getHandler := NewRouter(Dependencies{Principal: testPrincipal, ClientResourceQueries: queries})
			getResponse := httptest.NewRecorder()
			getHandler.ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, "/api/v1/"+test.plural+"/not-a-uuid", nil))
			if getResponse.Code != http.StatusNotFound || queries.calls != 0 || !strings.Contains(getResponse.Body.String(), `"code":"not_found"`) {
				t.Fatalf("GET kind=%s status=%d calls=%d body=%s", test.kind, getResponse.Code, queries.calls, getResponse.Body.String())
			}

			actions := &clientResourceLifecycleActions{}
			mutationHandler := NewRouter(clientResourceLifecycleDependencies(test.kind, actions))
			for _, request := range []*http.Request{
				httptest.NewRequest(http.MethodPatch, "/api/v1/"+test.plural+"/not-a-uuid", strings.NewReader(`{"reason":"Correct","name":"Updated"}`)),
				httptest.NewRequest(http.MethodPost, "/api/v1/"+test.plural+"/not-a-uuid/deactivate", strings.NewReader(`{"reason":"Retired"}`)),
				httptest.NewRequest(http.MethodPost, "/api/v1/"+test.plural+"/not-a-uuid/reactivate", strings.NewReader(`{"reason":"Restored"}`)),
			} {
				request.Header.Set("If-Match", `"3"`)
				response := httptest.NewRecorder()
				mutationHandler.ServeHTTP(response, request)
				if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"not_found"`) {
					t.Fatalf("%s kind=%s status=%d body=%s", request.Method, test.kind, response.Code, response.Body.String())
				}
			}
			if actions.writeCalls != 0 {
				t.Fatalf("malformed %s UUID reached writer %d times", test.kind, actions.writeCalls)
			}
		})
	}
}

func TestClientResourceUpdateRejectsMalformedLocationUUIDBeforeService(t *testing.T) {
	resourceID := "00000000-0000-4000-8000-000000000101"
	for _, test := range []struct {
		plural string
		kind   clientresources.Kind
	}{
		{plural: "contacts", kind: clientresources.ContactKind},
		{plural: "assets", kind: clientresources.AssetKind},
	} {
		actions := &clientResourceLifecycleActions{}
		handler := NewRouter(clientResourceLifecycleDependencies(test.kind, actions))
		request := httptest.NewRequest(http.MethodPatch, "/api/v1/"+test.plural+"/"+resourceID, strings.NewReader(
			`{"reason":"Move resource","location_id":"not-a-uuid"}`,
		))
		request.Header.Set("If-Match", `"3"`)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnprocessableEntity || actions.writeCalls != 0 ||
			!strings.Contains(response.Body.String(), `"code":"validation_failed"`) {
			t.Fatalf("kind=%s status=%d calls=%d body=%s", test.kind, response.Code, actions.writeCalls, response.Body.String())
		}
	}
}

func (a *deadLetterActions) Act(
	_ context.Context,
	command automation.DeadLetterCommand,
) (automation.DeadLetter, error) {
	a.got = command
	return automation.DeadLetter{
		ID: command.ID, MSPID: command.Principal.Scope.MSPID,
		ClientID: command.Principal.Scope.ClientID, State: "retrying",
	}, nil
}

func TestActOnAutomationDeadLetterMapsScopedReasonedCommand(t *testing.T) {
	actions := &deadLetterActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, AutomationDeadLetters: actions,
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/automation/dead-letters/dead-letter-id/actions",
		bytes.NewBufferString(`{"action":"retry","reason":"Connection repaired"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if actions.got.ID != "dead-letter-id" ||
		actions.got.Action != automation.DeadLetterRetry ||
		actions.got.Reason != "Connection repaired" ||
		actions.got.Principal.ID != "actor-id" {
		t.Fatalf("unexpected dead-letter command: %+v", actions.got)
	}
}

type automationManagementActions struct {
	createDefinition  automation.CreateDefinitionCommand
	reviseDefinition  automation.ReviseDefinitionCommand
	publishDefinition automation.PublishDefinitionCommand
	createConnection  automation.CreateExternalConnectionCommand
}

func (a *automationManagementActions) ListDefinitions(
	context.Context,
	authorization.Principal,
) ([]automation.ManagedDefinition, error) {
	return []automation.ManagedDefinition{{
		Definition: automation.Definition{
			ID: "automation-id", Version: 1, State: automation.Draft,
		},
		Name: "Triage",
	}}, nil
}

func (a *automationManagementActions) CreateDefinition(
	_ context.Context,
	command automation.CreateDefinitionCommand,
) (automation.Definition, error) {
	a.createDefinition = command
	return automation.Definition{
		ID: "automation-id", MSPID: command.Principal.Scope.MSPID,
		Version: 1, State: automation.Draft,
		ClientScopes: []string{command.Principal.Scope.ClientID},
		Capabilities: command.Capabilities, Trigger: command.Trigger,
		Steps: command.Steps,
	}, nil
}

func (a *automationManagementActions) ReviseDefinition(
	_ context.Context,
	command automation.ReviseDefinitionCommand,
) (automation.Definition, error) {
	a.reviseDefinition = command
	return automation.Definition{
		ID: command.ID, MSPID: command.Principal.Scope.MSPID,
		Version: command.Version + 1, State: automation.Draft,
		ClientScopes: []string{command.Principal.Scope.ClientID},
		Capabilities: command.Capabilities, Trigger: command.Trigger,
		Steps: command.Steps,
	}, nil
}

func (a *automationManagementActions) PublishDefinition(
	_ context.Context,
	command automation.PublishDefinitionCommand,
) (automation.Definition, error) {
	a.publishDefinition = command
	return automation.Definition{
		ID: command.ID, MSPID: command.Principal.Scope.MSPID,
		Version: command.Version, State: automation.Published,
		ClientScopes: []string{command.Principal.Scope.ClientID},
		Capabilities: []string{"work_record.comment"},
		Trigger:      automation.Trigger{EventType: "work_record.created"},
		Steps: []automation.Step{{
			ID: "comment", Kind: automation.StepAction,
			Action: &automation.Action{
				Kind:       automation.ActionAddComment,
				Parameters: map[string]string{"body": "Acknowledged"},
			},
		}},
	}, nil
}

func (a *automationManagementActions) CreateExternalConnection(
	_ context.Context,
	command automation.CreateExternalConnectionCommand,
) (automation.ExternalConnection, error) {
	a.createConnection = command
	return automation.ExternalConnection{
		ID: "connection-id", MSPID: command.Principal.Scope.MSPID,
		ClientID: command.Principal.Scope.ClientID, Name: command.Name,
		Endpoint: command.Endpoint, SigningSecretRef: command.SigningSecretRef,
	}, nil
}

func TestAutomationManagementRoutesMapVersionedScopedCommands(t *testing.T) {
	actions := &automationManagementActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, AutomationManagement: actions,
	})
	definitionBody := `{
		"name":"Acknowledge new records",
		"trigger":{"event_type":"work_record.created"},
		"capabilities":["work_record.comment"],
		"steps":[{
			"id":"comment",
			"kind":"action",
			"action":{"kind":"add_comment","parameters":{"body":"Acknowledged"}}
		}]
	}`
	create := httptest.NewRequest(
		http.MethodPost, "/api/v1/automation/definitions",
		bytes.NewBufferString(definitionBody),
	)
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusCreated ||
		actions.createDefinition.Name != "Acknowledge new records" ||
		actions.createDefinition.Principal.ID != "actor-id" ||
		createResponse.Header().Get("ETag") != `"1"` {
		t.Fatalf(
			"create status=%d etag=%q command=%+v body=%s",
			createResponse.Code, createResponse.Header().Get("ETag"),
			actions.createDefinition, createResponse.Body.String(),
		)
	}

	revisionBody := `{
		"expected_version":3,
		"trigger":{"event_type":"work_record.created"},
		"capabilities":["work_record.comment"],
		"steps":[{
			"id":"comment",
			"kind":"action",
			"action":{"kind":"add_comment","parameters":{"body":"Updated"}}
		}]
	}`
	revise := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/automation/definitions/automation-id/versions/2/revisions",
		bytes.NewBufferString(revisionBody),
	)
	reviseResponse := httptest.NewRecorder()
	handler.ServeHTTP(reviseResponse, revise)
	if reviseResponse.Code != http.StatusCreated ||
		actions.reviseDefinition.ID != "automation-id" ||
		actions.reviseDefinition.Version != 2 ||
		actions.reviseDefinition.ExpectedRecordVersion != 3 ||
		reviseResponse.Header().Get("ETag") != `"4"` {
		t.Fatalf(
			"revise status=%d etag=%q command=%+v body=%s",
			reviseResponse.Code, reviseResponse.Header().Get("ETag"),
			actions.reviseDefinition, reviseResponse.Body.String(),
		)
	}

	publish := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/automation/definitions/automation-id/versions/3/publish",
		bytes.NewBufferString(`{"expected_version":4}`),
	)
	publishResponse := httptest.NewRecorder()
	handler.ServeHTTP(publishResponse, publish)
	if publishResponse.Code != http.StatusOK ||
		actions.publishDefinition.Version != 3 ||
		actions.publishDefinition.ExpectedRecordVersion != 4 ||
		publishResponse.Header().Get("ETag") != `"5"` {
		t.Fatalf(
			"publish status=%d etag=%q command=%+v body=%s",
			publishResponse.Code, publishResponse.Header().Get("ETag"),
			actions.publishDefinition, publishResponse.Body.String(),
		)
	}

	connection := httptest.NewRequest(
		http.MethodPost, "/api/v1/automation/connections",
		bytes.NewBufferString(`{
			"name":"Customer callback",
			"endpoint":"https://automation.example.test/callback",
			"signing_secret_ref":"env://RARITY_AUTOMATION_HTTP_SECRET_CUSTOMER"
		}`),
	)
	connectionResponse := httptest.NewRecorder()
	handler.ServeHTTP(connectionResponse, connection)
	if connectionResponse.Code != http.StatusCreated ||
		actions.createConnection.Name != "Customer callback" ||
		actions.createConnection.Principal.Scope.ClientID != "client-id" ||
		connectionResponse.Header().Get("ETag") != `"1"` {
		t.Fatalf(
			"connection status=%d etag=%q command=%+v body=%s",
			connectionResponse.Code, connectionResponse.Header().Get("ETag"),
			actions.createConnection, connectionResponse.Body.String(),
		)
	}
}

func TestAutomationListRoutesExposeDiscoverableTypedState(t *testing.T) {
	management := &automationManagementActions{}
	deadLetters := &deadLetterActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, AutomationManagement: management,
		AutomationDeadLetters: deadLetters,
	})
	for _, test := range []struct {
		path string
		want string
	}{
		{"/api/v1/automation/definitions", `"name":"Triage"`},
		{"/api/v1/automation/dead-letters", `"state":"open"`},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(
			response, httptest.NewRequest(http.MethodGet, test.path, nil),
		)
		if response.Code != http.StatusOK ||
			!strings.Contains(response.Body.String(), test.want) {
			t.Fatalf(
				"path=%s status=%d body=%s",
				test.path, response.Code, response.Body.String(),
			)
		}
	}
}

func (a *aiDecisionActions) Decide(
	_ context.Context,
	command aiassist.DecisionCommand,
) (aiassist.Recommendation, error) {
	a.calls++
	a.got = command
	return aiassist.Recommendation{
		ID:       command.ID,
		MSPID:    command.Principal.Scope.MSPID,
		ClientID: command.Principal.Scope.ClientID,
		State:    aiassist.RecommendationAccepted,
	}, nil
}

func TestDecideAIRecommendationPreservesHumanOnlyBoundary(t *testing.T) {
	actions := &aiDecisionActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, AIDecisions: actions})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/ai/recommendations/recommendation-id/decide",
		bytes.NewBufferString(`{"decision":"accepted","reason":"Technician reviewed the draft"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if actions.got.ID != "recommendation-id" ||
		actions.got.Decision != aiassist.DecisionAccepted ||
		actions.got.Reason != "Technician reviewed the draft" ||
		actions.got.Principal.ID != "actor-id" {
		t.Fatalf("unexpected AI decision command: %+v", actions.got)
	}
	var result struct {
		ID      string `json:"id"`
		State   string `json:"state"`
		Applied bool   `json:"applied"`
		Sent    bool   `json:"sent"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode AI decision response: %v", err)
	}
	if result.ID != "recommendation-id" || result.State != "accepted" ||
		result.Applied || result.Sent {
		t.Fatalf("unexpected AI decision response: %+v", result)
	}
}

func TestDecideAIRecommendationRequiresActiveClientAIGrant(t *testing.T) {
	for _, test := range []struct {
		name      string
		principal authorization.Principal
		want      int
	}{
		{name: "MSP only", principal: authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp-id"}, Capabilities: authorization.NewCapabilitySet("ai.assist")}, want: http.StatusForbidden},
		{name: "wrong capability", principal: authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"}, Capabilities: authorization.NewCapabilitySet("prospect.create")}, want: http.StatusForbidden},
		{name: "active client", principal: authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"}, Capabilities: authorization.NewCapabilitySet("ai.assist")}, want: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			actions := &aiDecisionActions{}
			handler := NewRouter(Dependencies{Principal: func(*http.Request) (authorization.Principal, error) { return test.principal, nil }, AIDecisions: actions})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/ai/recommendations/recommendation-id/decide", bytes.NewBufferString(`{"decision":"accepted","reason":"Technician reviewed the draft"}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want || (test.want != http.StatusOK && actions.calls != 0) {
				t.Fatalf("status=%d calls=%d command=%+v", response.Code, actions.calls, actions.got)
			}
			if test.want == http.StatusOK && (actions.calls != 1 || actions.got.Principal.Scope.ClientID != "client-id" || !strings.Contains(response.Body.String(), `"applied":false`) || !strings.Contains(response.Body.String(), `"sent":false`)) {
				t.Fatalf("valid decision lost client/human boundary: calls=%d command=%+v body=%s", actions.calls, actions.got, response.Body.String())
			}
		})
	}
}

func TestTransitionOpportunityRejectsConflictingOrMalformedIfMatch(t *testing.T) {
	for _, test := range []struct{ name, ifMatch string }{
		{name: "conflict", ifMatch: `"6"`},
		{name: "malformed", ifMatch: `W/"7"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			actions := &salesActions{}
			handler := NewRouter(Dependencies{Principal: testPrincipal, Sales: actions})
			request := httptest.NewRequest(http.MethodPatch, "/api/v1/opportunities/opportunity-id", bytes.NewBufferString(`{"expected_version":7,"stage_id":"triage","reason":"Discovery completed"}`))
			request.Header.Set("If-Match", test.ifMatch)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || actions.transition.ExpectedVersion != 0 {
				t.Fatalf("If-Match=%q status=%d transition=%+v", test.ifMatch, response.Code, actions.transition)
			}
		})
	}
	actions := &salesActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Sales: actions})
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/opportunities/opportunity-id", bytes.NewBufferString(`{"expected_version":7,"stage_id":"triage","reason":"Discovery completed"}`))
	request.Header.Set("If-Match", `"7"`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || actions.transition.ExpectedVersion != 7 ||
		actions.transition.Reason != "Discovery completed" {
		t.Fatalf("matching If-Match status=%d transition=%+v", response.Code, actions.transition)
	}
}

func (a *conversionActions) Preview(context.Context, projects.ConversionPreviewCommand) (projects.ConversionPreview, error) {
	return projects.ConversionPreview{}, a.err
}

func (a *conversionActions) Convert(_ context.Context, command projects.ConversionCommand) (projects.ConversionResult, error) {
	a.calls++
	a.got = command
	return a.result, a.err
}

func TestConvertRequiresExpectedVersionAndIdempotencyKey(t *testing.T) {
	actions := &conversionActions{}
	handler := NewRouter(Dependencies{
		Principal:   testPrincipal,
		Conversions: actions,
	})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/opportunities/opportunity-id/convert",
		bytes.NewBufferString(`{}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
	}
	var body ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Error.Code != "validation_failed" || actions.calls != 0 {
		t.Fatalf("unexpected validation response: %+v calls=%d", body, actions.calls)
	}
}

func TestConvertMapsStableDTOAndReturnsCreatedResult(t *testing.T) {
	actions := &conversionActions{result: projects.ConversionResult{
		ProjectID: "project-id", ClientID: "client-id", ConversionID: "conversion-id",
	}}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Conversions: actions})
	body := `{
		"expected_version": 7,
		"idempotency_key": "request-1",
		"preview_hash": "preview-hash",
		"accepted_proposal_version_id": "proposal-version-id",
		"existing_client_id": "client-id",
		"project_display_id": "PRJ-1",
		"project_name": "Migration",
		"phases": [{"name":"Delivery","proposal_line_ids":["line-1"]}],
		"selected_task_ids": ["task-id"],
		"task_versions": {"task-id": 3}
	}`
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/opportunities/opportunity-id/convert",
		bytes.NewBufferString(body),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if actions.got.OpportunityID != "opportunity-id" ||
		actions.got.ExpectedOpportunityVersion != 7 ||
		actions.got.IdempotencyKey != "request-1" ||
		actions.got.TaskVersions["task-id"] != 3 {
		t.Fatalf("unexpected conversion command: %+v", actions.got)
	}
	var result ConversionResultResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode conversion result: %v", err)
	}
	if result.ProjectID != "project-id" || result.ConversionID != "conversion-id" {
		t.Fatalf("unexpected stable conversion response: %+v", result)
	}
}

func TestConvertReturnsSafeSharedClientIdentityConflict(t *testing.T) {
	actions := &conversionActions{err: clientidentity.ErrConflict}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Conversions: actions})
	body := `{
		"expected_version": 7,
		"idempotency_key": "request-1",
		"preview_hash": "preview-hash",
		"accepted_proposal_version_id": "proposal-version-id",
		"project_display_id": "PRJ-1",
		"project_name": "Migration",
		"phases": [{"name":"Delivery","proposal_line_ids":["line-1"]}]
	}`
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/opportunities/opportunity-id/convert",
		bytes.NewBufferString(body),
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf(
			"status = %d, want %d; body=%s",
			response.Code,
			http.StatusConflict,
			response.Body.String(),
		)
	}
	var result ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode conflict response: %v", err)
	}
	if result.Error.Code != "client_identity_conflict" ||
		result.Error.Message != "client name or display ID already exists" {
		t.Fatalf("unexpected safe conflict response: %+v", result)
	}
	if strings.Contains(strings.ToLower(response.Body.String()), "café") {
		t.Fatalf("conflict response revealed Client identity: %s", response.Body.String())
	}
	if actions.calls != 1 {
		t.Fatalf("conversion calls = %d, want 1", actions.calls)
	}
}

func TestCrossScopeErrorsRemainEnumerationSafe(t *testing.T) {
	actions := &conversionActions{err: scope.ErrNotFound}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Conversions: actions})
	body := `{
		"expected_version": 7,
		"idempotency_key": "request-1",
		"preview_hash": "preview-hash",
		"accepted_proposal_version_id": "proposal-version-id",
		"existing_client_id": "client-id",
		"project_display_id": "PRJ-1",
		"project_name": "Migration",
		"phases": [{"name":"Delivery","proposal_line_ids":["line-1"]}]
	}`
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/opportunities/opportunity-id/convert",
		bytes.NewBufferString(body),
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNotFound, response.Body.String())
	}
}

func TestRouterPreservesPlatformFallbackRoutes(t *testing.T) {
	fallback := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})
	handler := NewRouter(Dependencies{Fallback: fallback})
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("fallback status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestMutationSourceCannotBeSpoofedByRequestHeader(t *testing.T) {
	actions := &salesActions{}
	handler := NewRouter(Dependencies{Principal: testPrincipal, Sales: actions})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/prospects",
		bytes.NewBufferString(`{"display_id":"LEAD-1","name":"Example","email":"client@example.com"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Rarity-Source", "system")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if actions.got.Source != "api" {
		t.Fatalf("source = %q, want api", actions.got.Source)
	}
}

func testPrincipal(*http.Request) (authorization.Principal, error) {
	return authorization.Principal{
		ID:           "actor-id",
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("opportunity.convert", "prospect.create", "ai.assist"),
	}, nil
}

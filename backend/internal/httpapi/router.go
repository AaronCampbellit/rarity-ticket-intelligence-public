package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/attachments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/auditlog"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/automation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/billingexport"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/collaboration"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/comments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/commitments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/customfields"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/datto"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/graphintake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/identity"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/intake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/integrationhealth"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/links"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mentions"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/routing"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/search"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/servicekeys"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/setup"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/timeentries"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/views"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

type PrincipalResolver func(*http.Request) (authorization.Principal, error)

type SalesActions interface {
	ListPipelines(context.Context, authorization.Principal) ([]sales.Pipeline, error)
	CreateProspect(context.Context, sales.CreateProspectCommand) (sales.Prospect, error)
	ListProspects(context.Context, authorization.Principal, int) ([]sales.Prospect, error)
	CreatePipeline(context.Context, sales.CreatePipelineCommand) (sales.Pipeline, error)
	CreateOpportunity(context.Context, sales.CreateOpportunityCommand) (sales.Opportunity, error)
	GetOpportunity(context.Context, authorization.Principal, sales.OpportunityID) (sales.Opportunity, error)
	ListOpportunities(context.Context, authorization.Principal, sales.OpportunityListFilter) ([]sales.Opportunity, error)
	CreateOpportunityActivity(context.Context, sales.CreateOpportunityActivityCommand) (sales.OpportunityActivity, error)
	ListOpportunityActivities(context.Context, authorization.Principal, sales.OpportunityID, int) ([]sales.OpportunityActivity, error)
	Forecast(context.Context, authorization.Principal, string) ([]sales.ForecastBucket, error)
	TransitionOpportunity(context.Context, sales.TransitionCommand) (sales.Opportunity, error)
	ReplaceOpportunityCustomFields(context.Context, sales.ReplaceOpportunityCustomFieldsCommand) (sales.Opportunity, error)
	ReplaceOpportunityParticipants(context.Context, sales.ReplaceOpportunityParticipantsCommand) (sales.Opportunity, error)
}

type ProposalActions interface {
	CreateProposal(context.Context, sales.CreateProposalCommand) (sales.Proposal, error)
	GetProposal(context.Context, authorization.Principal, scope.Target, string) (sales.Proposal, error)
	ListProposals(context.Context, authorization.Principal, scope.Target, sales.ProposalListFilter) ([]sales.Proposal, error)
	IssueVersion(context.Context, sales.IssueVersionCommand) (sales.ProposalVersion, error)
	GetInternalApproval(context.Context, authorization.Principal, scope.Target, string) (sales.InternalApproval, error)
	DecideInternalApproval(context.Context, sales.DecideInternalApprovalCommand) (sales.InternalApproval, error)
	IssueAcceptanceGrant(context.Context, sales.IssueAcceptanceGrantCommand) (sales.IssuedAcceptanceGrant, error)
	AcceptElectronically(context.Context, sales.ElectronicAcceptanceCommand) (sales.Acceptance, error)
	RecordOfflineAcceptance(context.Context, sales.OfflineAcceptanceCommand) (sales.Acceptance, error)
}

type ProposalVersionQueryActions interface {
	GetProposalVersion(context.Context, authorization.Principal, scope.Target, string) (sales.ProposalVersion, error)
}

type ProjectActions interface {
	Create(context.Context, projects.CreateCommand) (projects.Project, error)
	UpdatePhase(context.Context, projects.UpdatePhaseCommand) (projects.Phase, error)
}

type ResourcePlanActions interface {
	Plan(context.Context, projects.PlanResourceCommand) (projects.ResourcePlan, error)
}

type CostActualActions interface {
	Create(context.Context, projects.CreateCostActualCommand) (projects.CostActual, error)
}

type AvailabilityActions interface {
	Create(
		context.Context,
		projects.CreateAvailabilityCommand,
	) (projects.TechnicianAvailability, error)
}

type FinancialInputActions interface {
	CreateLaborCostRate(
		context.Context,
		projects.CreateLaborCostRateCommand,
	) (projects.TechnicianLaborCostRate, error)
	RecognizeBillableWork(
		context.Context,
		projects.RecognizeBillableWorkCommand,
	) (projects.BillableWorkRecognition, error)
}

type ProjectQueryActions interface {
	List(context.Context, authorization.Principal, scope.Target, int) ([]projects.ProjectSummary, error)
	ResolveReference(
		context.Context,
		authorization.Principal,
		scope.Target,
		string,
	) ([]projects.ProjectSummary, error)
	Get(context.Context, authorization.Principal, scope.Target, projects.ProjectID) (projects.ProjectWorkspace, error)
}

type ConversionActions interface {
	Preview(context.Context, projects.ConversionPreviewCommand) (projects.ConversionPreview, error)
	Convert(context.Context, projects.ConversionCommand) (projects.ConversionResult, error)
}

type ChangeOrderActions interface {
	Create(context.Context, projects.CreateChangeOrderCommand) (projects.ChangeOrder, error)
	IssueVersion(context.Context, projects.IssueChangeOrderCommand) (projects.ChangeOrderVersion, error)
	Approve(context.Context, projects.ApprovalCommand) (projects.ChangeOrderVersion, error)
	OverrideApproval(context.Context, projects.OverrideCommand) (projects.ChangeOrderVersion, error)
	Apply(context.Context, projects.ApplyChangeOrderCommand) (projects.Project, error)
}

type AIDecisionActions interface {
	Decide(context.Context, aiassist.DecisionCommand) (aiassist.Recommendation, error)
}

type AIRecommendationActions interface {
	Get(context.Context, authorization.Principal, string) (aiassist.RecommendationView, error)
}

type CalendarAIRecommendationActions interface {
	Recommend(context.Context, aiassist.RecommendCalendarCommand) (aiassist.CalendarRecommendation, error)
}

// AIManagementActions is deliberately the small HTTP-facing command seam.
// Provider credentials only cross it as write-only command bytes.
type AIManagementActions interface {
	CreateConnection(context.Context, aiassist.CreateConnectionCommand) (aiassist.ProviderConnection, error)
	ListConnections(context.Context, aiassist.ListConnectionsCommand) ([]aiassist.ProviderConnection, error)
	UpdateConnection(context.Context, aiassist.UpdateConnectionCommand) (aiassist.ProviderConnection, error)
	SetConnectionEnabled(context.Context, aiassist.SetConnectionEnabledCommand) (aiassist.ProviderConnection, error)
	ReplaceCredential(context.Context, aiassist.ReplaceCredentialCommand) (aiassist.ProviderConnection, error)
	TestConnection(context.Context, aiassist.TestConnectionCommand) (aiassist.ProviderConnection, error)
	DiscoverModels(context.Context, aiassist.DiscoverModelsCommand) ([]aiassist.ModelProfile, error)
	ListModels(context.Context, aiassist.ListModelsCommand) ([]aiassist.ModelProfile, error)
	UpdateModels(context.Context, aiassist.UpdateModelsCommand) ([]aiassist.ModelProfile, error)
	GetPolicy(context.Context, aiassist.GetPolicyCommand) (aiassist.Policy, error)
	UpdatePolicy(context.Context, aiassist.UpdatePolicyCommand) (aiassist.Policy, error)
}

type AIJobActions interface {
	Submit(context.Context, aiassist.SubmitCommand) (aiassist.GenerationJob, error)
	Get(context.Context, authorization.Principal, string) (aiassist.GenerationJob, error)
	Cancel(context.Context, aiassist.CancelCommand) (aiassist.GenerationJob, error)
	Retry(context.Context, aiassist.RetryCommand) (aiassist.GenerationJob, error)
}

type AIWorkspaceConversationActions interface {
	Create(context.Context, authorization.Principal, string) (aiassist.Conversation, error)
	Get(context.Context, authorization.Principal, string) (aiassist.Conversation, error)
	List(context.Context, authorization.Principal, bool, int) ([]aiassist.Conversation, error)
	Archive(context.Context, authorization.Principal, string, int64) error
	Append(context.Context, authorization.Principal, string, aiassist.MessageRole, string, map[string]any) (aiassist.Message, error)
	Messages(context.Context, authorization.Principal, string, int) ([]aiassist.Message, error)
}

type AIWorkspaceToolActions interface {
	RunRead(context.Context, authorization.Principal, aiassist.ToolRequest) (aiassist.ToolResult, error)
	Propose(context.Context, authorization.Principal, aiassist.ToolRequest) (aiassist.ActionProposal, error)
	Confirm(context.Context, authorization.Principal, string, int64) (aiassist.ToolResult, error)
	Reject(context.Context, authorization.Principal, string, int64) error
}

type TeamsConnectionActions interface {
	Create(context.Context, notifications.CreateTeamsConnectionCommand) (notifications.ManagedTeamsConnection, error)
	List(context.Context, notifications.ListTeamsConnectionsCommand) ([]notifications.ManagedTeamsConnection, error)
	UpdateMetadata(context.Context, notifications.UpdateTeamsConnectionMetadataCommand) (notifications.ManagedTeamsConnection, error)
	ReplaceCredential(context.Context, notifications.ReplaceTeamsConnectionCredentialCommand) (notifications.ManagedTeamsConnection, error)
	SetEnabled(context.Context, notifications.SetTeamsConnectionEnabledCommand) (notifications.ManagedTeamsConnection, error)
	Test(context.Context, notifications.TestTeamsConnectionCommand) (notifications.ManagedTeamsConnection, error)
}

type AutomationDeadLetterActions interface {
	List(context.Context, authorization.Principal) ([]automation.DeadLetter, error)
	Act(context.Context, automation.DeadLetterCommand) (automation.DeadLetter, error)
}

type AutomationManagementActions interface {
	ListDefinitions(
		context.Context,
		authorization.Principal,
	) ([]automation.ManagedDefinition, error)
	CreateDefinition(
		context.Context,
		automation.CreateDefinitionCommand,
	) (automation.Definition, error)
	ReviseDefinition(
		context.Context,
		automation.ReviseDefinitionCommand,
	) (automation.Definition, error)
	PublishDefinition(
		context.Context,
		automation.PublishDefinitionCommand,
	) (automation.Definition, error)
	CreateExternalConnection(
		context.Context,
		automation.CreateExternalConnectionCommand,
	) (automation.ExternalConnection, error)
}

type LocationActions interface {
	CreateLocation(
		context.Context,
		clientresources.CreateLocationCommand,
	) (clientresources.Location, error)
}

type ContractActions interface {
	CreateContract(
		context.Context,
		clientresources.CreateContractCommand,
	) (clientresources.Contract, error)
}

type ContactActions interface {
	CreateContact(
		context.Context,
		clientresources.CreateContactCommand,
	) (clientresources.Contact, error)
}

type ServiceRecordActions interface {
	CreateService(
		context.Context,
		clientresources.CreateServiceCommand,
	) (clientresources.ServiceRecord, error)
}

type AssetActions interface {
	CreateAsset(
		context.Context,
		clientresources.CreateAssetCommand,
	) (clientresources.Asset, error)
}

type ClientResourceCatalogActions interface {
	List(
		context.Context,
		clientresources.ListCatalogCommand,
	) ([]clientresources.Summary, error)
	GetSummary(context.Context, clientresources.GetCatalogCommand) (clientresources.Summary, error)
}

type ClientResourceQueryActions interface {
	Get(context.Context, clientresources.GetCommand) (clientresources.ResourceDetail, error)
	GetForLifecycle(context.Context, clientresources.LifecycleGetCommand) (clientresources.ResourceDetail, error)
}

type ClientResourceLifecycleActions interface {
	Update(context.Context, clientresources.UpdateCommand) (clientresources.ResourceDetail, error)
	Deactivate(context.Context, clientresources.LifecycleCommand) (clientresources.ResourceDetail, error)
	Reactivate(context.Context, clientresources.LifecycleCommand) (clientresources.ResourceDetail, error)
}

type WorkRecordActions interface {
	Create(context.Context, workrecords.CreateCommand) (workrecords.Record, error)
}

type WorkRecordQueryActions interface {
	Get(context.Context, workrecords.GetCommand) (workrecords.Record, error)
	List(context.Context, workrecords.ListCommand) ([]workrecords.Record, error)
}

type OrganizationActions interface {
	CreateClient(context.Context, organizations.CreateClientCommand) (organizations.Client, error)
}

type DirectoryActions interface {
	CreateDepartment(context.Context, organizations.CreateDepartmentCommand) (organizations.Department, error)
	CreateTeam(context.Context, organizations.CreateTeamCommand) (organizations.Team, error)
	CreateQueue(context.Context, organizations.CreateQueueCommand) (organizations.Queue, error)
	ReplaceTeamMembers(context.Context, organizations.ReplaceTeamMembersCommand) (organizations.Team, error)
	List(context.Context, organizations.ListDirectoryCommand) (organizations.Directory, error)
}

type WorkAssignmentActions interface {
	Assign(context.Context, workrecords.AssignCommand) (workrecords.Record, error)
}

type WorkTransitionActions interface {
	Transition(context.Context, workrecords.TransitionCommand) (workrecords.Record, error)
}

type WorkPriorityActions interface {
	Change(context.Context, workrecords.PriorityCommand) (workrecords.Record, error)
}

type SLAOverrideActions interface {
	Override(context.Context, workrecords.SLAOverrideCommand) (workrecords.Record, error)
}

type WorkQueueActions interface {
	Transfer(context.Context, workrecords.QueueCommand) (workrecords.Record, error)
}

type WorkMergeActions interface {
	Merge(context.Context, workrecords.MergeCommand) (workrecords.MergeResult, error)
}

type WorkParticipantActions interface {
	Add(context.Context, workrecords.AddParticipantCommand) (workrecords.ParticipationResult, error)
	Remove(context.Context, workrecords.RemoveParticipantCommand) (workrecords.ParticipationResult, error)
}

type CommentActions interface {
	Create(context.Context, comments.CreateCommand) (comments.Comment, error)
}

type TimeEntryActions interface {
	Create(context.Context, timeentries.CreateCommand) (timeentries.Entry, error)
}

type TimeCaptureEntryActions interface {
	Create(context.Context, timeentries.CaptureCommand) (timeentries.Entry, error)
}

type LaborRoleActions interface {
	List(
		context.Context,
		authorization.Principal,
	) ([]timeentries.LaborRole, error)
	ListForManagement(
		context.Context,
		authorization.Principal,
	) ([]timeentries.LaborRole, error)
	Create(
		context.Context,
		timeentries.CreateLaborRoleCommand,
	) (timeentries.LaborRole, error)
	Version(
		context.Context,
		timeentries.VersionLaborRoleCommand,
	) (timeentries.LaborRole, error)
}

type TimesheetActions interface {
	ListWeek(
		context.Context,
		timeentries.ListWeekCommand,
	) (timeentries.Timesheet, error)
	Get(context.Context, timeentries.GetCommand) (timeentries.TimesheetRow, error)
	Amend(
		context.Context,
		timeentries.AmendCommand,
	) (timeentries.TimesheetRow, error)
	ReverseAndReplace(
		context.Context,
		timeentries.ReverseCommand,
	) (timeentries.ReversalResult, error)
}

type TimerActions interface {
	Start(
		context.Context,
		timeentries.StartTimerCommand,
	) (timeentries.TimerSession, error)
	List(
		context.Context,
		authorization.Principal,
		string,
	) ([]timeentries.TimerSession, error)
	Stop(
		context.Context,
		timeentries.StopTimerCommand,
	) (timeentries.TimerSession, error)
	Discard(
		context.Context,
		timeentries.DiscardTimerCommand,
	) (timeentries.TimerSession, error)
}

type AttachmentActions interface {
	Upload(context.Context, attachments.UploadCommand, io.Reader) (attachments.Attachment, error)
	ListOpportunity(context.Context, authorization.Principal, string, int) ([]attachments.Attachment, error)
}

type RelationshipActions interface {
	Create(context.Context, links.CreateCommand) (links.Link, error)
}

type TaskActions interface {
	Create(context.Context, tasks.CreateCommand) (tasks.Task, error)
	Get(context.Context, authorization.Principal, string) (tasks.Task, error)
	ListOpportunityTasks(context.Context, authorization.Principal, string) ([]tasks.Task, error)
}

type SearchActions interface {
	Search(context.Context, search.Query) ([]search.Result, error)
}

type ServiceKeyActions interface {
	List(context.Context, authorization.Principal, scope.Target) ([]servicekeys.Record, error)
	Issue(context.Context, servicekeys.IssueCommand) (servicekeys.Issued, error)
	Rotate(context.Context, servicekeys.RotateCommand) (servicekeys.Issued, error)
	Revoke(context.Context, servicekeys.RevokeCommand) error
}

type WebhookManagementActions interface {
	ListConnections(context.Context, authorization.Principal, scope.Target) ([]webhooks.ManagedConnection, error)
	ListDeliveries(context.Context, authorization.Principal, scope.Target) ([]webhooks.ManagedDelivery, error)
	Create(context.Context, webhooks.CreateConnectionCommand) (webhooks.ManagedConnection, error)
	Update(context.Context, webhooks.UpdateConnectionCommand) (webhooks.ManagedConnection, error)
	ReplaceCredential(context.Context, webhooks.ReplaceCredentialCommand) (webhooks.ManagedConnection, error)
	RetryDelivery(context.Context, webhooks.RetryDeliveryCommand) error
}

type WorkflowActions interface {
	List(
		context.Context,
		authorization.Principal,
		scope.Target,
	) ([]workflow.Published, error)
	Publish(context.Context, workflow.PublishCommand) (workflow.Published, error)
}

type ViewActions interface {
	Save(context.Context, views.SaveCommand) (views.View, error)
	List(context.Context, views.ListCommand) ([]views.View, error)
	Resolve(context.Context, views.ResolveCommand) (views.Resolved, error)
}

type RoutingActions interface {
	Current(context.Context, authorization.Principal) (routing.RuleSet, error)
	Publish(context.Context, routing.PublishCommand) (routing.RuleSet, error)
}

type SLACalendarActions interface {
	ListCalendars(
		context.Context,
		authorization.Principal,
		scope.Target,
	) ([]sla.PublishedCalendar, error)
	PublishCalendar(context.Context, sla.PublishCalendarCommand) (sla.PublishedCalendar, error)
}

type SLAPolicyActions interface {
	ListPolicies(
		context.Context,
		authorization.Principal,
		scope.Target,
	) ([]sla.PublishedPolicy, error)
	PublishPolicy(context.Context, sla.PublishPolicyCommand) (sla.PublishedPolicy, error)
}

type NotificationPolicyActions interface {
	ListPolicies(
		context.Context,
		authorization.Principal,
		scope.Target,
	) ([]notifications.PublishedPolicy, error)
	PublishPolicy(context.Context, notifications.PublishPolicyCommand) (notifications.PublishedPolicy, error)
}

type NotificationPreferenceActions interface {
	Get(context.Context, authorization.Principal) (notifications.RecipientPreference, error)
	Update(context.Context, notifications.UpdatePreferenceCommand) (notifications.RecipientPreference, error)
}

type KnowledgeActions interface {
	List(context.Context, knowledge.ListCommand) ([]knowledge.Article, error)
	CreateDraft(context.Context, knowledge.CreateDraftCommand) (knowledge.ArticleDetail, error)
	ReviseDraft(context.Context, knowledge.ReviseDraftCommand) (knowledge.ArticleDetail, error)
	Publish(context.Context, knowledge.PublishCommand) (knowledge.Version, error)
	Find(context.Context, knowledge.FindCommand) (knowledge.ArticleDetail, error)
}

type BillingExportActions interface {
	ListApprovals(context.Context, billingexport.ListApprovalsCommand) ([]billingexport.ApprovalEntry, error)
	DecideApproval(context.Context, billingexport.ApprovalCommand) (billingexport.ApprovalEntry, error)
	Export(context.Context, billingexport.ExportCommand) (billingexport.ExportResult, error)
}

type IntegrationHealthActions interface {
	Snapshot(context.Context, integrationhealth.SnapshotCommand) (integrationhealth.Snapshot, error)
}

type DirectIntakeActions interface {
	Accept(context.Context, intake.SystemCommand) (intake.InboundEvent, error)
}

type InboundWebhookActions interface {
	Accept(context.Context, webhooks.InboundCommand) (intake.InboundEvent, error)
}

type ForwardingIntakeActions interface {
	Accept(context.Context, intake.ForwardingCommand) (intake.ForwardingResult, error)
}

type ForwardingManagementActions interface {
	List(context.Context, authorization.Principal) ([]intake.ManagedForwardingConnection, error)
	Create(context.Context, intake.CreateForwardingConnectionCommand) (intake.ManagedForwardingConnection, error)
	Update(context.Context, intake.UpdateForwardingConnectionCommand) (intake.ManagedForwardingConnection, error)
}

type GraphNotificationActions interface {
	Accept(context.Context, []byte) (graphintake.NotificationAcceptance, error)
}

type GraphManagementActions interface {
	List(context.Context, authorization.Principal) ([]graphintake.ManagedMailbox, error)
	Create(context.Context, graphintake.CreateMailboxCommand) (graphintake.ManagedMailbox, error)
	Update(context.Context, graphintake.UpdateMailboxCommand) (graphintake.ManagedMailbox, error)
	ReplaceCredential(context.Context, graphintake.ReplaceMailboxCredentialCommand) (graphintake.ManagedMailbox, error)
}

type DattoActions interface {
	MapSite(
		context.Context,
		datto.MapSiteCommand,
	) (datto.SiteMapping, error)
	QueueManualSync(
		context.Context,
		datto.ManualSyncCommand,
	) (datto.ManualSyncRequest, error)
	Progress(context.Context, datto.ProgressCommand) (datto.SyncProgress, error)
	ListCandidates(
		context.Context,
		datto.ListCandidatesCommand,
	) ([]datto.ReconciliationCandidate, error)
	Decide(
		context.Context,
		datto.DecisionCommand,
	) (datto.CandidateDecision, error)
}

type DattoManagementActions interface {
	List(context.Context, authorization.Principal) ([]datto.ManagedConnection, error)
	Create(context.Context, datto.CreateConnectionCommand) (datto.ManagedConnection, error)
	Update(context.Context, datto.UpdateConnectionCommand) (datto.ManagedConnection, error)
	ReplaceCredential(context.Context, datto.ReplaceCredentialCommand) (datto.ManagedConnection, error)
}

type TagCatalogActions interface {
	List(context.Context, tagging.ListCatalogCommand) (tagging.Catalog, error)
	Health(context.Context, tagging.HealthCommand) (tagging.Health, error)
	MigrationHistory(context.Context, tagging.MigrationHistoryCommand) ([]tagging.MigrationRun, error)
	CreateGroup(context.Context, tagging.CreateGroupCommand) (tagging.Group, error)
	UpdateGroup(context.Context, tagging.UpdateGroupCommand) (tagging.Group, error)
	CreateTag(context.Context, tagging.CreateTagCommand) (tagging.Tag, error)
	UpdateTag(context.Context, tagging.UpdateTagCommand) (tagging.Tag, error)
	PreviewImpact(context.Context, tagging.ImpactCommand) (tagging.Impact, error)
	Merge(context.Context, tagging.MergeCommand) (tagging.Tag, error)
	Archive(context.Context, tagging.ArchiveCommand) (tagging.Tag, error)
}

type TagAssociationActions interface {
	Get(context.Context, tagging.GetCommand) (tagging.TaggedObject, error)
	History(context.Context, tagging.HistoryCommand) ([]tagging.HistoryEntry, error)
	ReplaceDirect(context.Context, tagging.ReplaceCommand) (tagging.TaggedObject, error)
	Bulk(context.Context, tagging.BulkCommand) ([]tagging.BulkResult, error)
	RequireMeaningful(context.Context, tagging.GuardCommand) error
}

type TagClassificationActions interface {
	Policy(context.Context, authorization.Principal) (tagging.ClassificationPolicy, error)
	UpdatePolicy(context.Context, tagging.UpdateClassificationPolicyCommand) (tagging.ClassificationPolicy, error)
}

type TagClassificationSuggestionActions interface {
	Request(context.Context, authorization.Principal, tagging.TargetRef) (tagging.ClassificationSuggestionRecord, error)
	GetByID(context.Context, authorization.Principal, string) (tagging.ClassificationSuggestionRecord, error)
	DecideByID(context.Context, authorization.Principal, string, string, string) (tagging.ClassificationSuggestionRecord, error)
}

type TagReportActions interface {
	Query(context.Context, authorization.Principal, tagging.ReportKind, tagging.ReportFilter) (tagging.Report, error)
	Evidence(context.Context, authorization.Principal, string, tagging.ReportFilter) (tagging.EvidencePage, error)
	Technicians(context.Context, authorization.Principal) ([]tagging.TechnicianOption, error)
}

type CalendarQueryActions interface {
	List(context.Context, calendar.QueryRequest) (calendar.QueryPage, error)
	FilterOptions(context.Context, calendar.QueryRequest) (calendar.FilterOptionCounts, error)
	Capacity(context.Context, calendar.QueryRequest) (map[string]calendar.CapacitySummary, error)
}
type CalendarLiveActions interface {
	ListAfter(context.Context, calendar.LiveRequest) (calendar.LivePage, error)
}
type CalendarProposalActions interface {
	Preview(context.Context, calendar.PreviewCommand) (calendar.SchedulingProposal, error)
	Apply(context.Context, calendar.ApplyCommand) (calendar.AppliedProposal, error)
}
type CalendarDependencyActions interface {
	Preview(context.Context, calendar.CreateDependencyCommand) (calendar.DependencyPreview, error)
	Create(context.Context, calendar.CreateDependencyCommand) (calendar.Dependency, error)
	Delete(context.Context, calendar.DeleteDependencyCommand) error
}
type CalendarConfigurationActions interface {
	ReplaceConflictPolicies(context.Context, calendar.ReplaceConflictPoliciesCommand) ([]calendar.ConflictPolicy, error)
	UpsertCustomDateField(context.Context, calendar.UpsertCustomDateCommand) (calendar.CustomDateField, error)
}
type CalendarConfigurationQueryActions interface {
	ListConflictPolicies(context.Context, authorization.Principal, calendar.ConflictPolicyScope) ([]calendar.ConflictPolicy, error)
	ListCustomDateFields(context.Context, authorization.Principal) ([]calendar.CustomDateField, error)
}
type CalendarPreferenceActions interface {
	Get(context.Context, authorization.Principal) (notifications.CalendarPreference, error)
	Replace(context.Context, notifications.ReplaceCalendarPreferenceCommand) (notifications.CalendarPreference, error)
}
type InboxActions interface {
	List(context.Context, authorization.Principal, string, int) (notifications.InboxPage, error)
	UnreadCount(context.Context, authorization.Principal) (int, error)
	MarkRead(context.Context, authorization.Principal, string, int64) (notifications.RecipientNotification, error)
}
type CalendarCustomDateValueActions interface {
	Set(context.Context, customfields.SetDateCommand) (customfields.DateValue, error)
}
type CalendarCustomDateValueQueryActions interface {
	ListCustomDateValues(context.Context, authorization.Principal, customfields.ObjectType, string) ([]customfields.DateValue, error)
}
type WorkforceScheduleActions interface {
	Publish(context.Context, workforce.PublishScheduleCommand) (workforce.Schedule, error)
}
type WorkforceScheduleExceptionActions interface {
	AddException(context.Context, workforce.AddScheduleExceptionCommand) (workforce.Schedule, error)
}
type WorkforcePTOActions interface {
	Request(context.Context, workforce.RequestPTOCommand) (workforce.PTORequest, error)
	Decide(context.Context, workforce.DecidePTOCommand) (workforce.PTORequest, error)
	Cancel(context.Context, workforce.CancelPTOCommand) (workforce.PTORequest, error)
}
type WorkforceQueryActions interface {
	ListSchedules(context.Context, authorization.Principal, []string, calendar.QueryWindow) ([]workforce.Schedule, error)
	ListPTO(context.Context, authorization.Principal, []string, calendar.QueryWindow) ([]workforce.PTORequest, error)
}
type ProjectMilestoneActions interface {
	Create(context.Context, projects.CreateMilestoneCommand) (projects.Milestone, error)
	Update(context.Context, projects.UpdateMilestoneCommand) (projects.Milestone, error)
	Transition(context.Context, projects.TransitionMilestoneCommand) (projects.Milestone, error)
}
type ProjectMilestoneQueryActions interface {
	ListMilestones(context.Context, authorization.Principal, projects.ProjectID) ([]projects.Milestone, error)
}
type MaintenanceWindowActions interface {
	Create(context.Context, commitments.CreateMaintenanceCommand) (commitments.MaintenanceWindow, error)
	Update(context.Context, commitments.UpdateMaintenanceCommand) (commitments.MaintenanceWindow, error)
	Transition(context.Context, commitments.TransitionMaintenanceCommand) (commitments.MaintenanceWindow, error)
}
type CommercialCommitmentActions interface {
	Create(context.Context, commitments.CreateCommercialCommand) (commitments.CommercialCommitment, error)
	Update(context.Context, commitments.UpdateCommercialCommand) (commitments.CommercialCommitment, error)
	Transition(context.Context, commitments.TransitionCommercialCommand) (commitments.CommercialCommitment, error)
}
type CommitmentQueryActions interface {
	ListMaintenanceWindows(context.Context, authorization.Principal, calendar.QueryWindow) ([]commitments.MaintenanceWindow, error)
	ListCommercialCommitments(context.Context, authorization.Principal, []string) ([]commitments.CommercialCommitment, error)
}

type Dependencies struct {
	Principal                    PrincipalResolver
	Sales                        SalesActions
	Proposals                    ProposalActions
	ProposalVersionQueries       ProposalVersionQueryActions
	Projects                     ProjectActions
	ProjectQueries               ProjectQueryActions
	ResourcePlans                ResourcePlanActions
	CostActuals                  CostActualActions
	Availability                 AvailabilityActions
	FinancialInputs              FinancialInputActions
	Conversions                  ConversionActions
	ChangeOrders                 ChangeOrderActions
	AIDecisions                  AIDecisionActions
	AIRecommendations            AIRecommendationActions
	AIManagement                 AIManagementActions
	AIJobs                       AIJobActions
	AIWorkspaceConversations     AIWorkspaceConversationActions
	AIWorkspaceTools             AIWorkspaceToolActions
	CalendarAIRecommendations    CalendarAIRecommendationActions
	TeamsConnections             TeamsConnectionActions
	BreakGlassManagement         BreakGlassManagementActions
	EntraSettings                EntraSettingsActions
	RoleManagement               RoleManagementActions
	Audit                        AuditActions
	AutomationDeadLetters        AutomationDeadLetterActions
	AutomationManagement         AutomationManagementActions
	Locations                    LocationActions
	Contracts                    ContractActions
	Contacts                     ContactActions
	Services                     ServiceRecordActions
	Assets                       AssetActions
	ClientResourceCatalog        ClientResourceCatalogActions
	ClientResourceQueries        ClientResourceQueryActions
	LocationLifecycle            ClientResourceLifecycleActions
	ContactLifecycle             ClientResourceLifecycleActions
	AssetLifecycle               ClientResourceLifecycleActions
	ServiceLifecycle             ClientResourceLifecycleActions
	ContractLifecycle            ClientResourceLifecycleActions
	WorkRecords                  WorkRecordActions
	WorkRecordQueries            WorkRecordQueryActions
	Organizations                OrganizationActions
	Directory                    DirectoryActions
	WorkAssignments              WorkAssignmentActions
	WorkTransitions              WorkTransitionActions
	WorkPriorities               WorkPriorityActions
	SLAOverrides                 SLAOverrideActions
	WorkQueues                   WorkQueueActions
	WorkMerges                   WorkMergeActions
	WorkParticipants             WorkParticipantActions
	Comments                     CommentActions
	Collaboration                CollaborationActions
	InternalContent              InternalContentActions
	Mentions                     MentionActions
	NewID                        func() string
	TimeEntries                  TimeEntryActions
	TimeCaptureEntries           TimeCaptureEntryActions
	LaborRoles                   LaborRoleActions
	Timesheets                   TimesheetActions
	Timers                       TimerActions
	Attachments                  AttachmentActions
	Relationships                RelationshipActions
	Tasks                        TaskActions
	Search                       SearchActions
	ServiceKeys                  ServiceKeyActions
	WebhookManagement            WebhookManagementActions
	Workflows                    WorkflowActions
	Views                        ViewActions
	Routing                      RoutingActions
	SLACalendars                 SLACalendarActions
	SLAPolicies                  SLAPolicyActions
	NotificationPolicies         NotificationPolicyActions
	NotificationPreferences      NotificationPreferenceActions
	NotificationInbox            InboxActions
	Knowledge                    KnowledgeActions
	BillingExports               BillingExportActions
	IntegrationHealth            IntegrationHealthActions
	DirectIntake                 DirectIntakeActions
	InboundWebhooks              InboundWebhookActions
	ForwardingIntake             ForwardingIntakeActions
	ForwardingManagement         ForwardingManagementActions
	GraphNotifications           GraphNotificationActions
	GraphManagement              GraphManagementActions
	Datto                        DattoActions
	DattoManagement              DattoManagementActions
	TagCatalog                   TagCatalogActions
	TagAssociations              TagAssociationActions
	TagClassification            TagClassificationActions
	TagClassificationSuggestions TagClassificationSuggestionActions
	TagReports                   TagReportActions
	TagCreation                  *tagging.CreationPreparer
	CalendarQueries              CalendarQueryActions
	CalendarLive                 CalendarLiveActions
	CalendarProposals            CalendarProposalActions
	CalendarDependencies         CalendarDependencyActions
	CalendarConfiguration        CalendarConfigurationActions
	CalendarConfigurationQueries CalendarConfigurationQueryActions
	CalendarPreferences          CalendarPreferenceActions
	CalendarCustomDateValues     CalendarCustomDateValueActions
	CalendarCustomDateQueries    CalendarCustomDateValueQueryActions
	WorkforceSchedules           WorkforceScheduleActions
	WorkforceScheduleExceptions  WorkforceScheduleExceptionActions
	WorkforcePTO                 WorkforcePTOActions
	WorkforceQueries             WorkforceQueryActions
	ProjectMilestones            ProjectMilestoneActions
	ProjectMilestoneQueries      ProjectMilestoneQueryActions
	MaintenanceWindows           MaintenanceWindowActions
	CommercialCommitments        CommercialCommitmentActions
	CommitmentQueries            CommitmentQueryActions
	CalendarProjectionWorker     *calendar.ProjectionWorker
	CalendarProjectionEvents     calendar.ProjectionEventSource
	CalendarReminders            *calendar.ReminderService
	Setup                        SetupActions
	Fallback                     http.Handler
}

var ErrCalendarFeatureUnavailable = errors.New("calendar feature unavailable")

type Router struct {
	dependencies Dependencies
	mux          *http.ServeMux
}

func NewRouter(dependencies Dependencies) http.Handler {
	router := &Router{dependencies: dependencies, mux: http.NewServeMux()}
	router.registerPrincipalRoutes()
	router.registerSetupRoutes()
	router.registerSalesRoutes()
	router.registerProjectRoutes()
	router.registerAIRoutes()
	router.registerAIWorkspaceRoutes()
	router.registerTeamsConnectionRoutes()
	router.registerBreakGlassManagementRoutes()
	router.registerEntraSettingsRoutes()
	router.registerRoleManagementRoutes()
	router.registerAuditRoutes()
	router.registerAutomationRoutes()
	router.registerClientResourceRoutes()
	router.registerOrganizationRoutes()
	router.registerWorkRecordRoutes()
	router.registerCollaborationRoutes()
	router.registerMentionRoutes()
	router.registerTimeWorkforceRoutes()
	router.registerSearchRoutes()
	router.registerServiceKeyRoutes()
	router.registerWebhookManagementRoutes()
	router.registerWorkflowRoutes()
	router.registerViewRoutes()
	router.registerRoutingRoutes()
	router.registerSLARoutes()
	router.registerNotificationRoutes()
	router.registerNotificationInboxRoutes()
	router.registerKnowledgeRoutes()
	router.registerBillingExportRoutes()
	router.registerIntegrationHealthRoutes()
	router.registerIntakeRoutes()
	router.registerDattoRoutes()
	router.registerDattoManagementRoutes()
	router.registerForwardingManagementRoutes()
	router.registerGraphManagementRoutes()
	router.registerTaggingRoutes()
	router.registerCalendarRoutes()
	router.registerWorkforceScheduleRoutes()
	router.registerCommitmentRoutes()
	if dependencies.Fallback != nil {
		router.mux.Handle("/", dependencies.Fallback)
	}
	return router
}

func (r *Router) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	r.mux.ServeHTTP(writer, request)
}

func (r *Router) principal(request *http.Request) (authorization.Principal, bool) {
	if r.dependencies.Principal == nil {
		return authorization.Principal{}, false
	}
	principal, err := r.dependencies.Principal(request)
	if err != nil {
		return authorization.Principal{}, false
	}
	return principal, true
}

func targetFor(principal authorization.Principal) scope.Target {
	return scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID}
}

func decodeRequest(writer http.ResponseWriter, request *http.Request, target any) bool {
	body := http.MaxBytesReader(writer, request.Body, 1<<20)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "request body is invalid")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "request body must contain one JSON value")
		return false
	}
	return true
}

const aiRequestLimitBytes int64 = 1 << 20

// decodeAIRequest accepts exactly one non-empty JSON object under the AI body
// cap. Unlike the older generic decoder, it does not silently accept a missing
// or non-JSON Content-Type, null, arrays, or scalar JSON values.
func decodeAIRequest(writer http.ResponseWriter, request *http.Request, target any) bool {
	contentType := strings.TrimSpace(request.Header.Get("Content-Type"))
	mediaType, _, err := mime.ParseMediaType(contentType)
	if contentType == "" || err != nil || !strings.EqualFold(mediaType, "application/json") {
		writeError(request.Context(), writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "request content type must be application/json")
		return false
	}
	body := http.MaxBytesReader(writer, request.Body, aiRequestLimitBytes)
	raw, err := io.ReadAll(body)
	if err != nil {
		var limitError *http.MaxBytesError
		if errors.As(err, &limitError) {
			writeError(request.Context(), writer, http.StatusRequestEntityTooLarge, "validation_failed", "request body is too large")
		} else {
			writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "request body is invalid")
		}
		return false
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "request body must contain one JSON object")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "request body is invalid")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "request body must contain one JSON object")
		return false
	}
	return true
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeETag(writer http.ResponseWriter, version int64) {
	writer.Header().Set("ETag", fmt.Sprintf(`"%d"`, version))
}

func writeDomainError(writer http.ResponseWriter, request *http.Request, err error) {
	var exportClassification *billingexport.ClassificationRequiredError
	if errors.As(err, &exportClassification) && errors.Is(err, tagging.ErrMeaningfulTagRequired) {
		writeJSON(writer, http.StatusUnprocessableEntity, ErrorResponse{Error: ErrorDetail{Code: "classification_required", Message: "a meaningful classification tag is required", RecoveryURL: "/api/v1/objects/time_entry/" + exportClassification.EntryID + "/tags"}})
		return
	}
	switch {
	case errors.Is(err, scope.ErrNotFound),
		errors.Is(err, organizations.ErrClientReferenceNotFound):
		writeError(request.Context(), writer, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, ErrCalendarFeatureUnavailable):
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "calendar feature is unavailable")
	case errors.Is(err, calendar.ErrWindowTooLarge):
		writeError(request.Context(), writer, http.StatusBadRequest, "calendar_window_too_large", "calendar window is too large")
	case errors.Is(err, calendar.ErrInvalidCalendarQuery), errors.Is(err, calendar.ErrInvalidAgendaCursor), errors.Is(err, calendar.ErrResultTooLarge):
		writeError(request.Context(), writer, http.StatusBadRequest, "invalid_calendar_window", "calendar query is invalid")
	case errors.Is(err, aiassist.ErrInvalidCalendarRecommendation):
		writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "calendar recommendation request is invalid")
	case errors.Is(err, calendar.ErrStaleProposal), errors.Is(err, calendar.ErrExpiredProposal), errors.Is(err, calendar.ErrProposalActorMismatch):
		writeError(request.Context(), writer, http.StatusConflict, "stale_calendar_proposal", "calendar proposal is stale; preview again")
	case errors.Is(err, calendar.ErrHardSchedulingConflict), errors.Is(err, calendar.ErrBlockedSchedulingCascade):
		writeError(request.Context(), writer, http.StatusConflict, "calendar_hard_conflict", "calendar proposal has a hard conflict")
	case errors.Is(err, calendar.ErrOverrideReasonRequired):
		writeError(request.Context(), writer, http.StatusBadRequest, "calendar_reason_required", "a scheduling reason is required")
	case errors.Is(err, calendar.ErrCrossClientDependency):
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "cross_client_dependency", "calendar dependencies cannot cross clients")
	case errors.Is(err, calendar.ErrDependencyCycle):
		writeError(request.Context(), writer, http.StatusConflict, "dependency_cycle", "calendar dependency would create a cycle")
	case errors.Is(err, calendar.ErrReadOnlyEventRole):
		writeError(request.Context(), writer, http.StatusConflict, "calendar_role_read_only", "calendar event role is read only")
	case errors.Is(err, calendar.ErrConflictPolicyVersionConflict), errors.Is(err, calendar.ErrCustomDateFieldVersionConflict), errors.Is(err, workforce.ErrWorkforceVersionConflict), errors.Is(err, commitments.ErrCommitmentVersionConflict):
		writeError(request.Context(), writer, http.StatusConflict, "version_conflict", "resource changed; refresh and retry")
	case errors.Is(err, calendar.ErrInvalidProposal), errors.Is(err, calendar.ErrInvalidScheduleChange), errors.Is(err, calendar.ErrOccurrenceScopeRequired), errors.Is(err, calendar.ErrInvalidOptionalChange), errors.Is(err, calendar.ErrInvalidRecurrence):
		writeError(request.Context(), writer, http.StatusBadRequest, "invalid_calendar_proposal", "calendar proposal is invalid")
	case errors.Is(err, calendar.ErrInvalidDependency), errors.Is(err, calendar.ErrSelfDependency), errors.Is(err, calendar.ErrDependencyIneligible), errors.Is(err, calendar.ErrLeadLagOutOfRange), errors.Is(err, calendar.ErrLeadLagPrecision):
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "invalid_calendar_dependency", "calendar dependency is invalid")
	case errors.Is(err, calendar.ErrDuplicateDependency):
		writeError(request.Context(), writer, http.StatusConflict, "duplicate_calendar_dependency", "calendar dependency already exists")
	case errors.Is(err, calendar.ErrInvalidConflictPolicy), errors.Is(err, calendar.ErrInvalidCustomDateConfiguration):
		writeError(request.Context(), writer, http.StatusBadRequest, "invalid_calendar_policy", "calendar policy is invalid")
	case errors.Is(err, notifications.ErrInvalidCalendarPreference):
		writeError(request.Context(), writer, http.StatusBadRequest, "invalid_calendar_preferences", "calendar notification preferences are invalid")
	case errors.Is(err, notifications.ErrInvalidInboxRequest):
		writeError(request.Context(), writer, http.StatusBadRequest, "invalid_notification_inbox", "notification inbox request is invalid")
	case errors.Is(err, workforce.ErrInvalidSchedule), errors.Is(err, workforce.ErrScheduleOverlap), errors.Is(err, workforce.ErrScheduleEffectiveOverlap):
		writeError(request.Context(), writer, http.StatusBadRequest, "invalid_workforce_schedule", "workforce schedule is invalid")
	case errors.Is(err, workforce.ErrInvalidPTO), errors.Is(err, workforce.ErrInvalidPTOTransition):
		writeError(request.Context(), writer, http.StatusBadRequest, "invalid_pto_request", "PTO request is invalid")
	case errors.Is(err, projects.ErrInvalidMilestone), errors.Is(err, projects.ErrInvalidMilestoneTransition):
		writeError(request.Context(), writer, http.StatusBadRequest, "invalid_project_milestone", "project milestone is invalid")
	case errors.Is(err, commitments.ErrInvalidMaintenance), errors.Is(err, commitments.ErrInvalidMaintenanceTransition):
		writeError(request.Context(), writer, http.StatusBadRequest, "invalid_maintenance_window", "maintenance window is invalid")
	case errors.Is(err, commitments.ErrInvalidCommercial), errors.Is(err, commitments.ErrInvalidCommercialTransition):
		writeError(request.Context(), writer, http.StatusBadRequest, "invalid_commercial_commitment", "commercial commitment is invalid")
	case errors.Is(err, customfields.ErrInvalidCustomDate):
		writeError(request.Context(), writer, http.StatusBadRequest, "invalid_custom_date_value", "custom date value is invalid")
	case errors.Is(err, customfields.ErrCustomDateVersionConflict), errors.Is(err, customfields.ErrCustomDateBindingConflict):
		writeError(request.Context(), writer, http.StatusConflict, "version_conflict", "custom date value changed; refresh and retry")
	case errors.Is(err, mentions.ErrDirectTargetIneligible):
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "mention_target_ineligible", "mention target is not eligible")
	case errors.Is(err, mentions.ErrTeamHasNoEligibleRecipients):
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "mention_team_empty", "mention team has no eligible recipients")
	case errors.Is(err, mentions.ErrTeamConfirmationRequired), errors.Is(err, mentions.ErrTeamConfirmationMismatch):
		writeError(request.Context(), writer, http.StatusConflict, "mention_team_confirmation_stale", "team mention recipients changed; confirm and retry")
	case errors.Is(err, mentions.ErrInvalidTokens), errors.Is(err, mentions.ErrInvalidResolution), errors.Is(err, mentions.ErrInvalidMutation):
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "mention_token_invalid", "mention tokens are invalid")
	case errors.Is(err, collaboration.ErrPublicMentionsForbidden), errors.Is(err, collaboration.ErrSourceRedacted):
		writeError(request.Context(), writer, http.StatusConflict, "source_not_internal", "mentions require active internal content")
	case errors.Is(err, servicekeys.ErrInvalidKey):
		writeError(request.Context(), writer, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, authorization.ErrForbidden):
		writeError(request.Context(), writer, http.StatusForbidden, "forbidden", "action is not permitted")
	case errors.Is(err, authorization.ErrInvalidRole):
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "role request is invalid")
	case errors.Is(err, auditlog.ErrInvalidQuery):
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "audit query is invalid")
	case errors.Is(err, authorization.ErrLastRoleManager):
		writeError(request.Context(), writer, http.StatusConflict, "last_role_manager", err.Error())
	case errors.Is(err, identity.ErrLastBreakGlassAccount):
		writeError(request.Context(), writer, http.StatusConflict, "last_recovery_account", err.Error())
	case errors.Is(err, identity.ErrInvalidBreakGlassAccount):
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "recovery account request is invalid")
	case errors.Is(err, identity.ErrBreakGlassVersionConflict):
		writeError(request.Context(), writer, http.StatusConflict, "version_conflict", "local administrator changed; refresh and retry")
	case errors.Is(err, identity.ErrEntraSettingsConflict):
		writeError(request.Context(), writer, http.StatusConflict, "version_conflict", "identity settings changed; refresh and retry")
	case errors.Is(err, setup.ErrCenterConfigurationConflict):
		writeError(request.Context(), writer, http.StatusConflict, "version_conflict", "setup configuration changed; refresh and retry")
	case errors.Is(err, setup.ErrInvalidCenterConfiguration):
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "setup configuration references are invalid")
	case errors.Is(err, identity.ErrEntraVerificationFailed):
		writeError(request.Context(), writer, http.StatusServiceUnavailable, "entra_verification_failed", "Microsoft Entra discovery could not be verified")
	case errors.Is(err, identity.ErrInvalidEntraSettings):
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "Microsoft Entra settings are invalid")
	case errors.Is(err, organizations.ErrForbidden):
		writeError(request.Context(), writer, http.StatusForbidden, "forbidden", "action is not permitted")
	case errors.Is(err, organizations.ErrClientIdentityConflict):
		writeError(request.Context(), writer, http.StatusConflict, "client_identity_conflict", "client name or display ID already exists")
	case errors.Is(err, organizations.ErrClientReferenceAmbiguous),
		errors.Is(err, clientresources.ErrAmbiguousResource):
		writeError(request.Context(), writer, http.StatusConflict, "ambiguous_reference", "More than one exact object matches. Use its display ID and try again.")
	case errors.Is(err, clientresources.ErrResourceIdentityConflict):
		writeError(request.Context(), writer, http.StatusConflict, "resource_identity_conflict", "resource identity conflicts with an existing record")
	case errors.Is(err, knowledge.ErrIdentityConflict):
		writeError(request.Context(), writer, http.StatusConflict, "knowledge_identity_conflict", "knowledge title or display ID already exists")
	case errors.Is(err, sales.ErrProspectIdentityConflict):
		writeError(request.Context(), writer, http.StatusConflict, "prospect_identity_conflict", "prospect name or display ID already exists")
	case errors.Is(err, knowledge.ErrDraftStateConflict):
		writeError(request.Context(), writer, http.StatusConflict, "knowledge_state_conflict", "knowledge draft state changed; refresh and retry")
	case errors.Is(err, clientresources.ErrLifecycleConflict):
		writeError(request.Context(), writer, http.StatusConflict, "lifecycle_conflict", "resource lifecycle changed; refresh and retry")
	case errors.Is(err, clientresources.ErrResourceInUse):
		writeError(request.Context(), writer, http.StatusConflict, "resource_in_use", "resource is in use")
	case errors.Is(err, clientresources.ErrResourceAuthorityConflict):
		writeError(request.Context(), writer, http.StatusConflict, "resource_authority_conflict", "resource is managed by its source integration")
	case errors.Is(err, object.ErrVersionConflict),
		errors.Is(err, projects.ErrStaleConversionPreview):
		writeError(request.Context(), writer, http.StatusConflict, "version_conflict", "resource changed; refresh and retry")
	case errors.Is(err, tagging.ErrMeaningfulTagRequired):
		writeClassificationRequired(request.Context(), writer, request)
	case errors.Is(err, tagging.ErrInactiveTarget):
		writeError(request.Context(), writer, http.StatusConflict, "tag_archived", "classification tag is no longer active")
	case errors.Is(err, tagging.ErrDuplicateTerm):
		writeError(request.Context(), writer, http.StatusConflict, "tag_ambiguous", "classification tag label or synonym is ambiguous")
	case errors.Is(err, tagging.ErrSystemManaged):
		writeError(request.Context(), writer, http.StatusConflict, "system_managed", "system-managed classification cannot be changed")
	case errors.Is(err, tagging.ErrInvalidReportFilter):
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "classification report filters are invalid")
	case errors.Is(err, tagging.ErrReportCapacityExceeded):
		writeError(request.Context(), writer, http.StatusTooManyRequests, "report_capacity_exceeded", "classification report snapshot capacity is temporarily exhausted")
	case errors.Is(err, timeentries.ErrTimerState),
		errors.Is(err, timeentries.ErrCaptureConsumed):
		writeError(request.Context(), writer, http.StatusConflict, "timer_state_conflict", "timer state changed; refresh and retry")
	case errors.Is(err, timeentries.ErrApprovedEntryImmutable):
		writeError(request.Context(), writer, http.StatusConflict, "approved_entry_immutable", "approved time must be reversed and replaced")
	case errors.Is(err, timeentries.ErrEntryNotPending):
		writeError(request.Context(), writer, http.StatusConflict, "time_entry_state_conflict", "time entry state changed; refresh and retry")
	case errors.Is(err, projects.ErrOpportunityConverted):
		writeError(request.Context(), writer, http.StatusConflict, "already_converted", "opportunity is already converted")
	case errors.Is(err, projects.ErrAvailabilityOverlap):
		writeError(request.Context(), writer, http.StatusConflict, "availability_overlap", "availability window overlaps an existing window")
	case errors.Is(err, aiassist.ErrJobNotRetryable):
		writeError(request.Context(), writer, http.StatusConflict, "job_not_retryable", "job cannot be retried")
	case errors.Is(err, aiassist.ErrProviderConnectionTestFailed):
		writeError(request.Context(), writer, http.StatusServiceUnavailable, "provider_unavailable", "provider is unavailable")
	case errors.Is(err, aiassist.ErrProviderInUse):
		writeError(request.Context(), writer, http.StatusConflict, "provider_in_use", "provider is referenced by an enabled AI policy or active generation job")
	case errors.Is(err, aiassist.ErrAIDenied):
		writeError(request.Context(), writer, http.StatusForbidden, "forbidden", "action is not permitted")
	case errors.Is(err, aiassist.ErrProposalForbidden):
		writeError(request.Context(), writer, http.StatusForbidden, "forbidden", "action is not permitted")
	case errors.Is(err, aiassist.ErrProposalConflict),
		errors.Is(err, aiassist.ErrProposalStale):
		writeError(request.Context(), writer, http.StatusConflict, "version_conflict", "AI action changed; review a fresh preview")
	case errors.Is(err, aiassist.ErrProposalExpired):
		writeError(request.Context(), writer, http.StatusGone, "proposal_expired", "AI action preview expired")
	case errors.Is(err, aiassist.ErrInvalidConversation),
		errors.Is(err, aiassist.ErrInvalidTool),
		errors.Is(err, aiassist.ErrUnknownTool),
		errors.Is(err, aiassist.ErrInvalidKnowledgeQuery),
		errors.Is(err, aiassist.ErrUnsafeKnowledgeSource):
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "AI workspace request is invalid")
	case errors.Is(err, aiassist.ErrInvalidProviderManagement),
		errors.Is(err, aiassist.ErrInvalidProviderConfiguration),
		errors.Is(err, aiassist.ErrLocalAcknowledgementRequired):
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "AI provider request is invalid")
	case errors.Is(err, notifications.ErrTeamsConnectionTestFailed):
		writeError(request.Context(), writer, http.StatusServiceUnavailable, "teams_unavailable", "Teams connection is unavailable")
	case errors.Is(err, notifications.ErrInvalidTeamsConnection):
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "Teams connection request is invalid")
	case errors.Is(err, aiassist.ErrInvalidDecision):
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "AI decision is invalid")
	default:
		if isValidationError(err) {
			writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", err.Error())
			return
		}
		writeError(request.Context(), writer, http.StatusInternalServerError, "internal_error", "request could not be completed")
	}
}

func writeClassificationRequired(ctx context.Context, writer http.ResponseWriter, request *http.Request) {
	objectType, id := "", request.PathValue("id")
	switch {
	case strings.HasPrefix(request.URL.Path, "/api/v1/work-records/"):
		objectType = "work_record"
	case strings.HasPrefix(request.URL.Path, "/api/v1/knowledge/articles/"):
		objectType = "knowledge_article"
	case strings.HasPrefix(request.URL.Path, "/api/v1/time-entries/"):
		objectType = "time_entry"
	}
	detail := ErrorDetail{Code: "classification_required", Message: "a meaningful classification tag is required"}
	if objectType != "" && id != "" {
		detail.RecoveryURL = "/api/v1/objects/" + objectType + "/" + id + "/tags"
	}
	writeJSON(writer, http.StatusUnprocessableEntity, ErrorResponse{Error: detail})
}

func isValidationError(err error) bool {
	return errors.Is(err, sales.ErrInvalidSalesRecord) ||
		errors.Is(err, organizations.ErrInvalid) ||
		errors.Is(err, sales.ErrInvalidProposal) ||
		errors.Is(err, sales.ErrInternalApprovalRequired) ||
		errors.Is(err, sales.ErrSignerEvidenceRequired) ||
		errors.Is(err, sales.ErrInvalidAcceptanceGrant) ||
		errors.Is(err, projects.ErrInvalidProject) ||
		errors.Is(err, projects.ErrInvalidConversion) ||
		errors.Is(err, projects.ErrInvalidConversionMapping) ||
		errors.Is(err, projects.ErrAcceptedProposalRequired) ||
		errors.Is(err, projects.ErrClientMatchRequired) ||
		errors.Is(err, projects.ErrInvalidChangeOrder) ||
		errors.Is(err, projects.ErrOverrideReasonRequired) ||
		errors.Is(err, projects.ErrChangeOrderNotApproved) ||
		errors.Is(err, projects.ErrInvalidAvailability) ||
		errors.Is(err, projects.ErrInvalidFinancialInput) ||
		errors.Is(err, automation.ErrInvalidDeadLetterAction) ||
		errors.Is(err, automation.ErrInvalidManagement) ||
		errors.Is(err, clientresources.ErrInvalid) ||
		errors.Is(err, clientresources.ErrInvalidCatalog) ||
		errors.Is(err, workrecords.ErrInvalid) ||
		errors.Is(err, links.ErrInvalid) ||
		errors.Is(err, tasks.ErrInvalid) ||
		errors.Is(err, search.ErrInvalidQuery) ||
		errors.Is(err, servicekeys.ErrInvalidCommand) ||
		errors.Is(err, attachments.ErrRejected) ||
		errors.Is(err, workflow.ErrInvalidConfiguration) ||
		errors.Is(err, workflow.ErrNoWorkflow) ||
		errors.Is(err, workflow.ErrTransitionNotAllowed) ||
		errors.Is(err, workflow.ErrTransitionRequirements) ||
		errors.Is(err, views.ErrInvalid) ||
		errors.Is(err, routing.ErrInvalidRules) ||
		errors.Is(err, routing.ErrNoRoute) ||
		errors.Is(err, sla.ErrInvalidCalendar) ||
		errors.Is(err, sla.ErrInvalidPolicy) ||
		errors.Is(err, sla.ErrNoPolicy) ||
		errors.Is(err, notifications.ErrInvalidPolicy) ||
		errors.Is(err, notifications.ErrInvalidPreference) ||
		errors.Is(err, knowledge.ErrInvalid) ||
		errors.Is(err, knowledge.ErrClientVisibilityDeferred) ||
		errors.Is(err, billingexport.ErrInvalid) ||
		errors.Is(err, billingexport.ErrTooLarge) ||
		errors.Is(err, billingexport.ErrApprovalRequired) ||
		errors.Is(err, datto.ErrInvalidManagementAction) ||
		errors.Is(err, datto.ErrInvalidConnectionManagement) ||
		errors.Is(err, intake.ErrInvalidSystemIntake) ||
		errors.Is(err, intake.ErrInvalidForwardingConfiguration) ||
		errors.Is(err, intake.ErrInvalidForwardingManagement) ||
		errors.Is(err, graphintake.ErrInvalidGraphManagement) ||
		errors.Is(err, comments.ErrInvalid) ||
		errors.Is(err, timeentries.ErrInvalid) ||
		errors.Is(err, timeentries.ErrInvalidTimer) ||
		errors.Is(err, timeentries.ErrInvalidLaborRole) ||
		errors.Is(err, timeentries.ErrInvalidTimesheet) ||
		errors.Is(err, tagging.ErrInvalidCatalog) ||
		errors.Is(err, tagging.ErrInvalidAssociation) ||
		errors.Is(err, tagging.ErrReasonRequired) ||
		errors.Is(err, collaboration.ErrInvalid) ||
		errors.Is(err, mentions.ErrInvalidCandidateQuery) ||
		errors.Is(err, mentions.ErrInvalidWidgetQuery) ||
		errors.Is(err, mentions.ErrInvalidStateChange) ||
		errors.Is(err, mentions.ErrInvalidDeepLink)
}

func writeError(_ context.Context, writer http.ResponseWriter, status int, code, message string) {
	if writer == nil {
		return
	}
	writeJSON(writer, status, ErrorResponse{Error: ErrorDetail{Code: code, Message: message}})
}

func source(_ *http.Request) string {
	return "api"
}

func parseExpectedVersionETag(request *http.Request) (int64, bool, error) {
	values := request.Header.Values("If-Match")
	if len(values) == 0 {
		return 0, false, nil
	}
	if len(values) != 1 {
		return 0, true, errors.New("multiple If-Match values")
	}
	value := strings.TrimSpace(values[0])
	if len(value) < 3 || value[0] != '"' || value[len(value)-1] != '"' ||
		strings.ContainsAny(value[1:len(value)-1], `",`) {
		return 0, true, errors.New("invalid If-Match value")
	}
	version, err := strconv.ParseInt(value[1:len(value)-1], 10, 64)
	if err != nil || version < 1 {
		return 0, true, errors.New("invalid If-Match version")
	}
	return version, true, nil
}

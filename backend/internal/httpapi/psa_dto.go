package httpapi

import (
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/automation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/comments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

type ErrorDetail struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	RecoveryURL string `json:"recovery_url,omitempty"`
}

type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type CreateProspectRequest struct {
	DisplayID string `json:"display_id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
}

type CreatePipelineRequest struct {
	Key    string                `json:"key"`
	Name   string                `json:"name"`
	Stages []sales.PipelineStage `json:"stages"`
}

type CreateOpportunityRequest struct {
	ClientID        string                `json:"client_id,omitempty"`
	ProspectID      string                `json:"prospect_id,omitempty"`
	PipelineID      string                `json:"pipeline_id"`
	StageID         sales.PipelineStageID `json:"stage_id"`
	DisplayID       string                `json:"display_id"`
	Name            string                `json:"name"`
	Description     string                `json:"description,omitempty"`
	AmountMinor     int64                 `json:"amount_minor"`
	Currency        string                `json:"currency"`
	OwnerID         string                `json:"owner_id,omitempty"`
	ExpectedCloseOn string                `json:"expected_close_on,omitempty"`
}

type TransitionOpportunityRequest struct {
	ExpectedVersion int64                 `json:"expected_version"`
	StageID         sales.PipelineStageID `json:"stage_id"`
	Reason          string                `json:"reason"`
}

type ReplaceOpportunityCustomFieldsRequest struct {
	ExpectedVersion int64             `json:"expected_version"`
	Fields          map[string]string `json:"fields"`
}

type ReplaceOpportunityParticipantsRequest struct {
	ExpectedVersion int64    `json:"expected_version"`
	TeamID          string   `json:"team_id,omitempty"`
	ContactIDs      []string `json:"contact_ids"`
}

type IssueProposalVersionRequest struct {
	ExpectedVersion int64                `json:"expected_version"`
	Currency        string               `json:"currency"`
	Lines           []sales.ProposalLine `json:"lines"`
	ApprovalRule    sales.ApprovalRule   `json:"approval_rule"`
	ExpiresAt       *time.Time           `json:"expires_at,omitempty"`
}

type AcceptProposalVersionRequest struct {
	Method          string    `json:"method"`
	AcceptanceGrant string    `json:"acceptance_grant,omitempty"`
	SignerName      string    `json:"signer_name,omitempty"`
	SignerEmail     string    `json:"signer_email,omitempty"`
	RecordedBy      string    `json:"recorded_by,omitempty"`
	AcceptedAt      time.Time `json:"accepted_at,omitempty"`
}

type ConvertOpportunityRequest struct {
	ExpectedVersion           int64              `json:"expected_version"`
	IdempotencyKey            string             `json:"idempotency_key"`
	PreviewHash               string             `json:"preview_hash"`
	AcceptedProposalVersionID string             `json:"accepted_proposal_version_id"`
	ExistingClientID          string             `json:"existing_client_id,omitempty"`
	CreateClientFromProspect  bool               `json:"create_client_from_prospect,omitempty"`
	ProjectDisplayID          string             `json:"project_display_id"`
	ProjectName               string             `json:"project_name"`
	ProjectOwnerID            string             `json:"project_owner_id,omitempty"`
	PlannedStart              time.Time          `json:"planned_start,omitempty"`
	PlannedEnd                time.Time          `json:"planned_end,omitempty"`
	Phases                    []PhaseMappingDTO  `json:"phases"`
	SelectedTaskIDs           []tasks.ID         `json:"selected_task_ids,omitempty"`
	TaskVersions              map[tasks.ID]int64 `json:"task_versions,omitempty"`
	TagIDs                    []string           `json:"tag_ids"`
}

type PhaseMappingDTO struct {
	Name               string    `json:"name"`
	OwnerID            string    `json:"owner_id,omitempty"`
	ParticipatingTeams []string  `json:"participating_teams,omitempty"`
	ProposalLineIDs    []string  `json:"proposal_line_ids"`
	PlannedStart       time.Time `json:"planned_start,omitempty"`
	PlannedEnd         time.Time `json:"planned_end,omitempty"`
}

type CreateProjectRequest struct {
	DisplayID                 string          `json:"display_id"`
	Name                      string          `json:"name"`
	OriginalProposalVersionID string          `json:"original_proposal_version_id"`
	PlannedStart              time.Time       `json:"planned_start,omitempty"`
	PlannedEnd                time.Time       `json:"planned_end,omitempty"`
	Phases                    []PhaseInputDTO `json:"phases"`
	TagIDs                    []string        `json:"tag_ids"`
}

type PhaseInputDTO struct {
	Name               string         `json:"name"`
	OwnerID            string         `json:"owner_id,omitempty"`
	ParticipatingTeams []string       `json:"participating_teams,omitempty"`
	PlannedStart       time.Time      `json:"planned_start,omitempty"`
	PlannedEnd         time.Time      `json:"planned_end,omitempty"`
	PlannedMinutes     int64          `json:"planned_minutes"`
	Budget             projects.Money `json:"budget"`
	Deliverables       []string       `json:"deliverables,omitempty"`
	CompletionCriteria []string       `json:"completion_criteria,omitempty"`
}

type UpdatePhaseRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
	PhaseInputDTO
}

type CreateChangeOrderRequest struct {
	DisplayID string `json:"display_id"`
}

type IssueChangeOrderVersionRequest struct {
	ExpectedVersion   int64  `json:"expected_version"`
	Description       string `json:"description"`
	Currency          string `json:"currency"`
	RevenueDeltaMinor int64  `json:"revenue_delta_minor"`
	CostDeltaMinor    int64  `json:"cost_delta_minor"`
	LaborDeltaMinutes int64  `json:"labor_delta_minutes"`
}

type ChangeOrderDecisionRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason,omitempty"`
}

type ConversionResultResponse struct {
	ProjectID     projects.ProjectID `json:"project_id"`
	ClientID      string             `json:"client_id"`
	ConversionID  string             `json:"conversion_id"`
	AlreadyExists bool               `json:"already_exists"`
}

type AIDecisionRequest struct {
	Decision aiassist.HumanDecision `json:"decision"`
	Reason   string                 `json:"reason"`
}

type AIDecisionResponse struct {
	ID      string                       `json:"id"`
	State   aiassist.RecommendationState `json:"state"`
	Applied bool                         `json:"applied"`
	Sent    bool                         `json:"sent"`
}

type AIRecommendationResponse struct {
	ID             string                       `json:"id"`
	JobID          string                       `json:"job_id"`
	ClientID       string                       `json:"client_id"`
	WorkRecordID   string                       `json:"work_record_id"`
	Feature        aiassist.Feature             `json:"feature"`
	Text           string                       `json:"text"`
	CandidateIDs   []string                     `json:"candidate_ids"`
	Confidence     *float64                     `json:"confidence,omitempty"`
	RelevantInputs []string                     `json:"relevant_inputs"`
	State          aiassist.RecommendationState `json:"state"`
	GeneratedAt    time.Time                    `json:"generated_at"`
	Version        int64                        `json:"version"`
}

// AI request DTOs intentionally do not embed domain objects: that prevents
// browser input from setting MSP identity, health, credentials, job context,
// candidate authorization, or other server-owned state.
type AIProviderCreateRequest struct {
	Name                     string               `json:"name"`
	Adapter                  aiassist.AdapterType `json:"adapter"`
	NetworkMode              aiassist.NetworkMode `json:"network_mode"`
	BaseURL                  string               `json:"base_url"`
	Credential               string               `json:"credential,omitempty"`
	TimeoutSeconds           int64                `json:"timeout_seconds,omitempty"`
	RequestLimitBytes        int64                `json:"request_limit_bytes,omitempty"`
	ResponseLimitBytes       int64                `json:"response_limit_bytes,omitempty"`
	LocalNetworkAcknowledged bool                 `json:"local_network_acknowledged"`
	Reason                   string               `json:"reason"`
}

type AIProviderPatchRequest struct {
	Name                     *string               `json:"name,omitempty"`
	Adapter                  *aiassist.AdapterType `json:"adapter,omitempty"`
	NetworkMode              *aiassist.NetworkMode `json:"network_mode,omitempty"`
	BaseURL                  *string               `json:"base_url,omitempty"`
	TimeoutSeconds           *int64                `json:"timeout_seconds,omitempty"`
	RequestLimitBytes        *int64                `json:"request_limit_bytes,omitempty"`
	ResponseLimitBytes       *int64                `json:"response_limit_bytes,omitempty"`
	LocalNetworkAcknowledged *bool                 `json:"local_network_acknowledged,omitempty"`
	Enabled                  *bool                 `json:"enabled,omitempty"`
	ExpectedVersion          int64                 `json:"expected_version,omitempty"`
	Reason                   string                `json:"reason"`
}

type AICredentialReplaceRequest struct {
	Credential      string `json:"credential"`
	ExpectedVersion int64  `json:"expected_version,omitempty"`
	Reason          string `json:"reason"`
}

type AIProviderTestRequest struct{}

type AIModelDiscoveryRequest struct {
	ExpectedVersion int64  `json:"expected_version,omitempty"`
	Reason          string `json:"reason"`
}

type AIModelPatchInput struct {
	ID                        string             `json:"id"`
	ExpectedVersion           int64              `json:"expected_version"`
	DisplayName               string             `json:"display_name"`
	SupportedFeatures         []aiassist.Feature `json:"supported_features"`
	ContextLimit              int64              `json:"context_limit"`
	OutputLimit               int64              `json:"output_limit"`
	ZeroCost                  bool               `json:"zero_cost"`
	InputCostPerMillionMinor  *int64             `json:"input_cost_per_million_minor,omitempty"`
	OutputCostPerMillionMinor *int64             `json:"output_cost_per_million_minor,omitempty"`
	Enabled                   bool               `json:"enabled"`
}

type AIModelsPatchRequest struct {
	Updates []AIModelPatchInput `json:"updates"`
	Reason  string              `json:"reason"`
}

type AIPolicyRequest struct {
	Enabled                              bool               `json:"enabled"`
	ProviderDisclosureAccepted           bool               `json:"provider_disclosure_accepted"`
	PromptVersion                        string             `json:"prompt_version"`
	AllowedFeatures                      []aiassist.Feature `json:"allowed_features"`
	SummaryModelProfileID                string             `json:"summary_model_profile_id,omitempty"`
	ReplyDraftModelProfileID             string             `json:"reply_draft_model_profile_id,omitempty"`
	SimilarSuggestionsModelProfileID     string             `json:"similar_suggestions_model_profile_id,omitempty"`
	CalendarRecommendationModelProfileID string             `json:"calendar_recommendation_model_profile_id,omitempty"`
	CostLimitEnabled                     bool               `json:"cost_limit_enabled"`
	AllowUnmeteredUnknown                bool               `json:"allow_unmetered_unknown"`
	MonthlyCostLimitMinor                int64              `json:"monthly_cost_limit_minor"`
	ExpectedVersion                      int64              `json:"expected_version,omitempty"`
	Reason                               string             `json:"reason"`
}

type AIJobSubmitRequest struct {
	Feature aiassist.Feature `json:"feature"`
}

type AIJobMutationRequest struct {
	ExpectedVersion int64  `json:"expected_version,omitempty"`
	Reason          string `json:"reason"`
}

type AIProviderResponse struct {
	ID                         string               `json:"id"`
	Name                       string               `json:"name"`
	Adapter                    aiassist.AdapterType `json:"adapter"`
	NetworkMode                aiassist.NetworkMode `json:"network_mode"`
	BaseURL                    string               `json:"base_url"`
	CredentialConfigured       bool                 `json:"credential_configured"`
	Enabled                    bool                 `json:"enabled"`
	TimeoutSeconds             int64                `json:"timeout_seconds"`
	RequestLimitBytes          int64                `json:"request_limit_bytes"`
	ResponseLimitBytes         int64                `json:"response_limit_bytes"`
	LocalNetworkAcknowledgedAt *time.Time           `json:"local_network_acknowledged_at,omitempty"`
	Health                     aiassist.HealthState `json:"health"`
	LastTestedAt               *time.Time           `json:"last_tested_at,omitempty"`
	LastSucceededAt            *time.Time           `json:"last_succeeded_at,omitempty"`
	LastErrorCode              string               `json:"last_error_code,omitempty"`
	Version                    int64                `json:"version"`
}

type AIModelResponse struct {
	ID                        string             `json:"id"`
	ConnectionID              string             `json:"connection_id"`
	ProviderModelID           string             `json:"provider_model_id"`
	DisplayName               string             `json:"display_name"`
	SupportedFeatures         []aiassist.Feature `json:"supported_features"`
	ContextLimit              int64              `json:"context_limit"`
	OutputLimit               int64              `json:"output_limit"`
	ZeroCost                  bool               `json:"zero_cost"`
	InputCostPerMillionMinor  *int64             `json:"input_cost_per_million_minor,omitempty"`
	OutputCostPerMillionMinor *int64             `json:"output_cost_per_million_minor,omitempty"`
	Enabled                   bool               `json:"enabled"`
	Version                   int64              `json:"version"`
}

type AIPolicyResponse struct {
	Enabled                              bool               `json:"enabled"`
	ProviderDisclosureAccepted           bool               `json:"provider_disclosure_accepted"`
	PromptVersion                        string             `json:"prompt_version"`
	AllowedFeatures                      []aiassist.Feature `json:"allowed_features"`
	SummaryModelProfileID                string             `json:"summary_model_profile_id,omitempty"`
	ReplyDraftModelProfileID             string             `json:"reply_draft_model_profile_id,omitempty"`
	SimilarSuggestionsModelProfileID     string             `json:"similar_suggestions_model_profile_id,omitempty"`
	ClassificationModelProfileID         string             `json:"classification_model_profile_id,omitempty"`
	CalendarRecommendationModelProfileID string             `json:"calendar_recommendation_model_profile_id,omitempty"`
	CostLimitEnabled                     bool               `json:"cost_limit_enabled"`
	AllowUnmeteredUnknown                bool               `json:"allow_unmetered_unknown"`
	MonthlyCostLimitMinor                int64              `json:"monthly_cost_limit_minor"`
	CurrentMonthlyCostMinor              *int64             `json:"current_monthly_cost_minor,omitempty"`
	Version                              int64              `json:"version"`
}

type AIJobResponse struct {
	ID                      string            `json:"id"`
	WorkRecordID            string            `json:"work_record_id"`
	Feature                 aiassist.Feature  `json:"feature"`
	ModelProfileID          string            `json:"model_profile_id,omitempty"`
	State                   aiassist.JobState `json:"state"`
	Attempt                 int               `json:"attempt"`
	MaxAttempts             int               `json:"max_attempts"`
	CancellationRequestedAt *time.Time        `json:"cancellation_requested_at,omitempty"`
	SafeErrorCode           string            `json:"safe_error_code,omitempty"`
	RecommendationID        string            `json:"recommendation_id,omitempty"`
	CreatedAt               time.Time         `json:"created_at"`
	UpdatedAt               time.Time         `json:"updated_at"`
	CompletedAt             *time.Time        `json:"completed_at,omitempty"`
	Version                 int64             `json:"version"`
}

type AutomationDeadLetterActionRequest struct {
	Action automation.DeadLetterAction `json:"action"`
	Reason string                      `json:"reason"`
}

type AutomationDefinitionRequest struct {
	Name         string             `json:"name"`
	Trigger      automation.Trigger `json:"trigger"`
	Capabilities []string           `json:"capabilities"`
	Steps        []automation.Step  `json:"steps"`
}

type AutomationRevisionRequest struct {
	ExpectedVersion int64              `json:"expected_version"`
	Trigger         automation.Trigger `json:"trigger"`
	Capabilities    []string           `json:"capabilities"`
	Steps           []automation.Step  `json:"steps"`
}

type AutomationPublishRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}

type AutomationConnectionRequest struct {
	Name             string `json:"name"`
	Endpoint         string `json:"endpoint"`
	SigningSecretRef string `json:"signing_secret_ref"`
}

type CreateLocationRequest struct {
	DisplayID string `json:"display_id"`
	Name      string `json:"name"`
}

type CreateContractRequest struct {
	DisplayID string     `json:"display_id"`
	Name      string     `json:"name"`
	StartsOn  time.Time  `json:"starts_on"`
	EndsOn    *time.Time `json:"ends_on,omitempty"`
}

type CreateContactRequest struct {
	DisplayID   string `json:"display_id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email,omitempty"`
	Phone       string `json:"phone,omitempty"`
	LocationID  string `json:"location_id,omitempty"`
}

type CreateServiceRequest struct {
	DisplayID   string `json:"display_id"`
	Name        string `json:"name"`
	Criticality string `json:"criticality,omitempty"`
}

type CreateAssetRequest struct {
	DisplayID    string                    `json:"display_id"`
	Name         string                    `json:"name"`
	AssetType    string                    `json:"asset_type"`
	LocationID   string                    `json:"location_id,omitempty"`
	SourceSystem string                    `json:"source_system,omitempty"`
	ExternalID   string                    `json:"external_id,omitempty"`
	Authority    clientresources.Authority `json:"authority"`
	TagIDs       []string                  `json:"tag_ids"`
}

type ClientResourceUpdateRequest struct {
	ExpectedVersion *int64     `json:"expected_version,omitempty"`
	Reason          string     `json:"reason"`
	Name            *string    `json:"name,omitempty"`
	DisplayName     *string    `json:"display_name,omitempty"`
	Email           *string    `json:"email,omitempty"`
	Phone           *string    `json:"phone,omitempty"`
	LocationID      *string    `json:"location_id,omitempty"`
	AssetType       *string    `json:"asset_type,omitempty"`
	Criticality     *string    `json:"criticality,omitempty"`
	StartsOn        *time.Time `json:"starts_on,omitempty"`
	EndsOn          *time.Time `json:"ends_on,omitempty"`
	ClearEndsOn     bool       `json:"clear_ends_on,omitempty"`
}

type ClientResourceLifecycleRequest struct {
	ExpectedVersion *int64 `json:"expected_version,omitempty"`
	Reason          string `json:"reason"`
}

type CreateWorkRecordRequest struct {
	DisplayID   string           `json:"display_id"`
	Type        workrecords.Type `json:"type"`
	Title       string           `json:"title"`
	Description string           `json:"description,omitempty"`
	Status      string           `json:"status"`
	Priority    string           `json:"priority"`
	ServiceID   string           `json:"service_id,omitempty"`
	ContractID  string           `json:"contract_id,omitempty"`
	TagIDs      []string         `json:"tag_ids"`
}

type AssignWorkRecordRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	OwnerID         string `json:"owner_id"`
	Reason          string `json:"reason"`
}

type TransitionWorkRecordRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	ToStatus        string `json:"to_status"`
	Reason          string `json:"reason,omitempty"`
}

type ChangeWorkRecordPriorityRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Priority        string `json:"priority"`
	Reason          string `json:"reason"`
}

type OverrideSLARequest struct {
	ExpectedVersion    int64      `json:"expected_version"`
	SLAExpectedVersion int64      `json:"sla_expected_version"`
	ResponseDueAt      *time.Time `json:"response_due_at,omitempty"`
	ResolutionDueAt    *time.Time `json:"resolution_due_at,omitempty"`
	Reason             string     `json:"reason"`
}

type RouteWorkRecordRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	QueueID         string `json:"queue_id"`
	Reason          string `json:"reason"`
}

type MergeWorkRecordRequest struct {
	WinnerVersion    int64  `json:"winner_version"`
	DuplicateID      string `json:"duplicate_id"`
	DuplicateVersion int64  `json:"duplicate_version"`
	Reason           string `json:"reason"`
}

type AddWorkParticipantRequest struct {
	ExpectedVersion int64                       `json:"expected_version"`
	TechnicianID    string                      `json:"technician_id"`
	Role            workrecords.ParticipantRole `json:"role"`
}

type RemoveWorkParticipantRequest struct {
	ExpectedVersion    int64  `json:"expected_version"`
	ParticipantVersion int64  `json:"participant_version"`
	Reason             string `json:"reason"`
}

type CreateCommentRequest struct {
	Visibility             comments.Visibility         `json:"visibility"`
	Body                   string                      `json:"body"`
	TimeCapture            *TimeCaptureRequest         `json:"time_capture,omitempty"`
	Tokens                 []mentionTokenDTO           `json:"tokens,omitempty"`
	ConfirmedTeamSnapshots map[string]teamConfirmation `json:"confirmed_team_snapshots,omitempty"`
	IdempotencyKey         string                      `json:"idempotency_key,omitempty"`
}

type CreateTimeEntryRequest struct {
	TaskID       string              `json:"task_id,omitempty"`
	TechnicianID string              `json:"technician_id"`
	StartedAt    time.Time           `json:"started_at"`
	EndedAt      time.Time           `json:"ended_at"`
	Billable     bool                `json:"billable"`
	Note         string              `json:"note,omitempty"`
	TimeCapture  *TimeCaptureRequest `json:"time_capture,omitempty"`
	TagIDs       []string            `json:"tag_ids"`
}

type TimeCaptureRequest struct {
	ID              string   `json:"id"`
	ExpectedVersion int64    `json:"expected_version"`
	LaborRoleID     string   `json:"labor_role_id"`
	Billable        bool     `json:"billable"`
	TagIDs          []string `json:"tag_ids"`
}

type AttachmentResponse struct {
	ID            string    `json:"id"`
	WorkRecordID  string    `json:"work_record_id,omitempty"`
	OpportunityID string    `json:"opportunity_id,omitempty"`
	Filename      string    `json:"filename"`
	ContentType   string    `json:"content_type"`
	SizeBytes     int64     `json:"size_bytes"`
	SHA256        string    `json:"sha256"`
	Version       int64     `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
}

type CreateRelationshipRequest struct {
	SourceType       string `json:"source_type"`
	SourceID         string `json:"source_id"`
	TargetType       string `json:"target_type"`
	TargetID         string `json:"target_id"`
	RelationshipType string `json:"relationship_type"`
}

type CreateTaskRequest struct {
	ParentTaskID    string   `json:"parent_task_id,omitempty"`
	Title           string   `json:"title"`
	OwnerID         string   `json:"owner_id,omitempty"`
	EstimateMinutes int      `json:"estimate_minutes,omitempty"`
	TagIDs          []string `json:"tag_ids"`
}

type CreateResourcePlanRequest struct {
	PhaseID        string    `json:"phase_id"`
	RoleID         string    `json:"role_id,omitempty"`
	TeamID         string    `json:"team_id,omitempty"`
	StartsOn       time.Time `json:"starts_on"`
	EndsOn         time.Time `json:"ends_on"`
	PlannedMinutes int64     `json:"planned_minutes"`
}

type CreateCostActualRequest struct {
	PhaseID     string         `json:"phase_id,omitempty"`
	CostType    string         `json:"cost_type"`
	Description string         `json:"description"`
	Amount      projects.Money `json:"amount"`
	Committed   bool           `json:"committed"`
	IncurredAt  time.Time      `json:"incurred_at"`
}

type CreateOpportunityActivityRequest struct {
	Kind       string    `json:"kind"`
	Summary    string    `json:"summary"`
	Details    string    `json:"details,omitempty"`
	OccurredAt time.Time `json:"occurred_at,omitempty"`
}

type DecideInternalApprovalRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Decision        string `json:"decision"`
	Reason          string `json:"reason"`
}

type IssueAcceptanceGrantRequest struct {
	SignerName  string            `json:"signer_name"`
	SignerEmail string            `json:"signer_email"`
	Evidence    map[string]string `json:"evidence"`
	ExpiresAt   time.Time         `json:"expires_at"`
}

type CreateProposalRequest struct {
	OpportunityID string `json:"opportunity_id"`
	DisplayID     string `json:"display_id"`
}

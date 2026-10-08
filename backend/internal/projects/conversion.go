package projects

import (
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
)

type PhaseMapping struct {
	Name               string
	OwnerID            string
	ParticipatingTeams []string
	ProposalLineIDs    []string
	PlannedStart       time.Time
	PlannedEnd         time.Time
}

type ConversionPreviewCommand struct {
	Principal                  authorization.Principal
	Target                     scope.Target
	OpportunityID              sales.OpportunityID
	AcceptedProposalVersionID  string
	ExpectedOpportunityVersion int64
	ExistingClientID           string
	CreateClientFromProspect   bool
	ProjectDisplayID           string
	ProjectName                string
	ProjectOwnerID             string
	PlannedStart               time.Time
	PlannedEnd                 time.Time
	Phases                     []PhaseMapping
	SelectedTaskIDs            []tasks.ID
	TaskVersions               map[tasks.ID]int64
	ActorID                    string
	Source                     string
}

type ConversionCommand struct {
	ConversionPreviewCommand
	PreviewHash          string
	IdempotencyKey       string
	TagIDs               []string
	ClassificationPolicy tagging.CreationPolicy
}

type ConversionSource struct {
	Opportunity        sales.Opportunity
	ProposalRecord     sales.Proposal
	ProposalVersion    sales.ProposalVersion
	Acceptance         sales.Acceptance
	Prospect           *sales.Prospect
	CandidateClientIDs []string
	ClosedWonStageID   sales.PipelineStageID
	SelectedTasks      []tasks.Task
}

type ClientPreview struct {
	Action     string `json:"action"`
	ClientID   string `json:"client_id,omitempty"`
	ProspectID string `json:"prospect_id,omitempty"`
	Name       string `json:"name,omitempty"`
}

type PhasePreview struct {
	Position        int      `json:"position"`
	Name            string   `json:"name"`
	ProposalLineIDs []string `json:"proposal_line_ids"`
	PlannedMinutes  int64    `json:"planned_minutes"`
	Budget          Money    `json:"budget"`
}

type TaskPreview struct {
	ID      tasks.ID `json:"id"`
	Version int64    `json:"version"`
}

type BudgetBaseline struct {
	Currency       string `json:"currency"`
	RevenueMinor   int64  `json:"revenue_minor"`
	CostMinor      int64  `json:"cost_minor"`
	PlannedMinutes int64  `json:"planned_minutes"`
}

type ConversionPreview struct {
	OpportunityID     sales.OpportunityID `json:"opportunity_id"`
	ProposalVersionID string              `json:"proposal_version_id"`
	Client            ClientPreview       `json:"client"`
	ProjectDisplayID  string              `json:"project_display_id"`
	ProjectName       string              `json:"project_name"`
	PlannedStart      time.Time           `json:"planned_start"`
	PlannedEnd        time.Time           `json:"planned_end"`
	Phases            []PhasePreview      `json:"phases"`
	Tasks             []TaskPreview       `json:"tasks"`
	OriginalBaseline  BudgetBaseline      `json:"original_baseline"`
	OriginalBudget    Money               `json:"original_budget"`
	PlannedMinutes    int64               `json:"planned_minutes"`
	Hash              string              `json:"hash,omitempty"`
}

type ClientSeed struct {
	ID         string
	MSPID      string
	ProspectID string
	DisplayID  string
	Name       string
	Email      string
	Phone      string
	CreatedAt  time.Time
	CreatedBy  string
}

type TaskMove struct {
	Task            tasks.Task
	PreviousVersion int64
	History         tasks.MovementHistory
}

type ConversionRecord struct {
	ID                string
	OpportunityID     sales.OpportunityID
	ProposalVersionID string
	ProjectID         ProjectID
	MSPID             string
	ClientID          string
	RequestKey        string
	PreviewHash       string
	Snapshot          ConversionPreview
	ConvertedAt       time.Time
	ConvertedBy       string
}

type ConversionMutation struct {
	Client         *ClientSeed
	Project        Project
	OriginalBudget BudgetBaseline
	CurrentBudget  BudgetBaseline
	Opportunity    sales.Opportunity
	TaskMoves      []TaskMove
	Conversion     ConversionRecord
	Audit          mutation.AuditRecord
	Event          mutation.EventRecord
	InitialTags    tagging.InitialAssignmentSet
}

type ConversionResult struct {
	ProjectID     ProjectID
	ClientID      string
	ConversionID  string
	AlreadyExists bool
}

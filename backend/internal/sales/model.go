package sales

import "time"

type OpportunityID string
type PipelineStageID string
type FieldKey string

type Money struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
}

type ForecastCategory string

const (
	PipelineCategory ForecastCategory = "pipeline"
	Weighted         ForecastCategory = "weighted"
	Committed        ForecastCategory = "committed"
	ClosedWon        ForecastCategory = "closed_won"
	ClosedLost       ForecastCategory = "closed_lost"
)

type PipelineStage struct {
	ID               PipelineStageID   `json:"id"`
	PipelineID       string            `json:"pipeline_id"`
	Key              string            `json:"key"`
	Name             string            `json:"name"`
	Position         int               `json:"position"`
	Probability      uint8             `json:"probability"`
	Category         ForecastCategory  `json:"forecast_category"`
	RequiredFields   []FieldKey        `json:"required_fields"`
	AllowedNext      []PipelineStageID `json:"allowed_next_stage_ids"`
	RequiresProposal bool              `json:"requires_proposal"`
	RequiresApproval bool              `json:"requires_approval"`
	Version          int64             `json:"version"`
}

type Opportunity struct {
	ID              OpportunityID       `json:"id"`
	MSPID           string              `json:"msp_id"`
	ClientID        string              `json:"client_id,omitempty"`
	ProspectID      string              `json:"prospect_id,omitempty"`
	PipelineID      string              `json:"pipeline_id"`
	StageID         PipelineStageID     `json:"stage_id"`
	DisplayID       string              `json:"display_id"`
	Name            string              `json:"name"`
	Amount          Money               `json:"amount"`
	Fields          map[FieldKey]string `json:"fields"`
	CustomFields    map[string]string   `json:"custom_fields"`
	TeamID          string              `json:"team_id,omitempty"`
	ContactIDs      []string            `json:"contact_ids"`
	ProposalIssued  bool                `json:"proposal_issued"`
	ApprovalGranted bool                `json:"approval_granted"`
	Version         int64               `json:"version"`
	UpdatedAt       time.Time           `json:"updated_at"`
	UpdatedBy       string              `json:"updated_by"`
}

type OpportunityActivity struct {
	ID            string        `json:"id"`
	MSPID         string        `json:"msp_id"`
	ClientID      string        `json:"client_id,omitempty"`
	OpportunityID OpportunityID `json:"opportunity_id"`
	Kind          string        `json:"kind"`
	Summary       string        `json:"summary"`
	Details       string        `json:"details,omitempty"`
	OccurredAt    time.Time     `json:"occurred_at"`
	CreatedAt     time.Time     `json:"created_at"`
	CreatedBy     string        `json:"created_by"`
}

type ForecastBucket struct {
	PipelineID       string           `json:"pipeline_id"`
	StageID          PipelineStageID  `json:"stage_id"`
	StageName        string           `json:"stage_name"`
	Category         ForecastCategory `json:"forecast_category"`
	Probability      uint8            `json:"probability"`
	OpportunityCount int64            `json:"opportunity_count"`
	Amount           Money            `json:"amount"`
	WeightedAmount   Money            `json:"weighted_amount"`
}

type OpportunityListFilter struct {
	PipelineID      string
	StageID         PipelineStageID
	BeforeUpdatedAt time.Time
	BeforeID        OpportunityID
	Limit           int
}

type Prospect struct {
	ID        string    `json:"id"`
	MSPID     string    `json:"msp_id"`
	DisplayID string    `json:"display_id"`
	Name      string    `json:"name"`
	Email     string    `json:"email,omitempty"`
	Phone     string    `json:"phone,omitempty"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

type Pipeline struct {
	ID        string          `json:"id"`
	MSPID     string          `json:"msp_id"`
	Key       string          `json:"key"`
	Name      string          `json:"name"`
	Stages    []PipelineStage `json:"stages"`
	Version   int64           `json:"version"`
	CreatedAt time.Time       `json:"created_at"`
	CreatedBy string          `json:"created_by"`
}

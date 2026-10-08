package sales

import "time"

type ProposalLineType string

const (
	FixedFee         ProposalLineType = "fixed_fee"
	TimeAndMaterials ProposalLineType = "time_and_materials"
	ProductLicense   ProposalLineType = "product_license"
	RecurringService ProposalLineType = "recurring_service"
)

type ProposalState string

const (
	ProposalDraft    ProposalState = "draft"
	ProposalIssued   ProposalState = "issued"
	ProposalAccepted ProposalState = "accepted"
)

type Proposal struct {
	ID               string        `json:"id"`
	MSPID            string        `json:"msp_id"`
	ClientID         string        `json:"client_id,omitempty"`
	ProspectID       string        `json:"prospect_id,omitempty"`
	OpportunityID    string        `json:"opportunity_id"`
	DisplayID        string        `json:"display_id"`
	CurrentVersion   int64         `json:"current_version"`
	CurrentVersionID string        `json:"current_version_id,omitempty"`
	State            ProposalState `json:"state"`
	Version          int64         `json:"version"`
	UpdatedAt        time.Time     `json:"updated_at"`
}

type ProposalListFilter struct {
	State           ProposalState
	OpportunityID   string
	BeforeUpdatedAt time.Time
	BeforeID        string
	Limit           int
}

type ProposalLine struct {
	ID             string           `json:"id"`
	Type           ProposalLineType `json:"type"`
	Description    string           `json:"description"`
	Quantity       int64            `json:"quantity"`
	UnitPrice      Money            `json:"unit_price"`
	UnitCost       Money            `json:"unit_cost"`
	Discount       Money            `json:"discount"`
	TaxTreatment   string           `json:"tax_treatment"`
	Tax            Money            `json:"tax"`
	Recurrence     string           `json:"recurrence,omitempty"`
	PlannedMinutes int64            `json:"planned_minutes"`
}

type ProposalVersion struct {
	ID                       string         `json:"id"`
	ProposalID               string         `json:"proposal_id"`
	MSPID                    string         `json:"msp_id"`
	ClientID                 string         `json:"client_id,omitempty"`
	Version                  int64          `json:"version"`
	State                    ProposalState  `json:"state"`
	Currency                 string         `json:"currency"`
	Lines                    []ProposalLine `json:"lines"`
	Subtotal                 Money          `json:"subtotal"`
	TaxTotal                 Money          `json:"tax_total"`
	Total                    Money          `json:"total"`
	Cost                     Money          `json:"cost"`
	Margin                   Money          `json:"margin"`
	RequiresInternalApproval bool           `json:"requires_internal_approval"`
	IssuedAt                 time.Time      `json:"issued_at"`
	IssuedBy                 string         `json:"issued_by"`
	ExpiresAt                *time.Time     `json:"expires_at,omitempty"`
	PDFSnapshotID            string         `json:"pdf_snapshot_id"`
}

type ApprovalRule struct {
	MaximumWithoutApprovalMinor int64 `json:"maximum_without_approval_minor"`
	MinimumMarginBasisPoints    int64 `json:"minimum_margin_basis_points"`
}

type InternalApproval struct {
	ID                string    `json:"id"`
	MSPID             string    `json:"msp_id"`
	ClientID          string    `json:"client_id,omitempty"`
	ProposalVersionID string    `json:"proposal_version_id"`
	State             string    `json:"state"`
	ApproverID        string    `json:"approver_id,omitempty"`
	Reason            string    `json:"reason,omitempty"`
	DecisionAt        time.Time `json:"decision_at,omitempty"`
	Version           int64     `json:"version"`
}

type AcceptanceGrant struct {
	ID                string            `json:"id"`
	MSPID             string            `json:"msp_id"`
	ClientID          string            `json:"client_id,omitempty"`
	ProposalVersionID string            `json:"proposal_version_id"`
	SignerName        string            `json:"signer_name"`
	SignerEmail       string            `json:"signer_email"`
	Evidence          map[string]string `json:"evidence"`
	ExpiresAt         time.Time         `json:"expires_at"`
	IssuedAt          time.Time         `json:"issued_at"`
	IssuedBy          string            `json:"issued_by"`
	TokenSHA256       string            `json:"-"`
}

type IssuedAcceptanceGrant struct {
	Grant AcceptanceGrant `json:"grant"`
	Token string          `json:"token"`
}

type PDFSnapshot struct {
	ID                string
	ProposalVersionID string
	SHA256            string
	StoredAt          time.Time
}

type SnapshotInput struct {
	ProposalVersionID string
	Data              []byte
}

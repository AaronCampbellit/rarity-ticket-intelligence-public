package projects

import (
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
)

type ChangeOrderState string

const (
	ChangeOrderDraft     ChangeOrderState = "draft"
	ChangeOrderIssued    ChangeOrderState = "issued"
	ChangeOrderApproved  ChangeOrderState = "approved"
	ChangeOrderRejected  ChangeOrderState = "rejected"
	ChangeOrderApplied   ChangeOrderState = "applied"
	ChangeOrderCancelled ChangeOrderState = "cancelled"
)

type ChangeOrder struct {
	ID             string           `json:"id"`
	ProjectID      ProjectID        `json:"project_id"`
	MSPID          string           `json:"msp_id"`
	ClientID       string           `json:"client_id"`
	DisplayID      string           `json:"display_id"`
	State          ChangeOrderState `json:"state"`
	CurrentVersion int64            `json:"current_version_number"`
	Version        int64            `json:"version"`
	CreatedAt      time.Time        `json:"created_at"`
	CreatedBy      string           `json:"created_by"`
	UpdatedAt      time.Time        `json:"updated_at"`
	UpdatedBy      string           `json:"updated_by"`
}

type ChangeOrderVersion struct {
	ID                string    `json:"id"`
	ChangeOrderID     string    `json:"change_order_id"`
	MSPID             string    `json:"msp_id"`
	ClientID          string    `json:"client_id"`
	Version           int64     `json:"version"`
	Description       string    `json:"description"`
	Currency          string    `json:"currency"`
	RevenueDeltaMinor int64     `json:"revenue_delta_minor"`
	CostDeltaMinor    int64     `json:"cost_delta_minor"`
	LaborDeltaMinutes int64     `json:"labor_delta_minutes"`
	IssuedAt          time.Time `json:"issued_at"`
	IssuedBy          string    `json:"issued_by"`
}

type ChangeOrderDecision struct {
	ID                   string           `json:"id"`
	ChangeOrderID        string           `json:"change_order_id"`
	ChangeOrderVersionID string           `json:"change_order_version_id"`
	MSPID                string           `json:"msp_id"`
	ClientID             string           `json:"client_id"`
	PreviousState        ChangeOrderState `json:"previous_state"`
	Decision             ChangeOrderState `json:"decision"`
	Override             bool             `json:"override"`
	Reason               string           `json:"reason,omitempty"`
	DecidedAt            time.Time        `json:"decided_at"`
	DecidedBy            string           `json:"decided_by"`
}

type ChangeOrderApplication struct {
	ID                   string
	ChangeOrderID        string
	ChangeOrderVersionID string
	ProjectID            ProjectID
	MSPID                string
	ClientID             string
	AppliedAt            time.Time
	AppliedBy            string
}

type CreateChangeOrderMutation struct {
	Order ChangeOrder
	Audit mutation.AuditRecord
	Event mutation.EventRecord
}

type IssueChangeOrderMutation struct {
	Order   ChangeOrder
	Version ChangeOrderVersion
	Audit   mutation.AuditRecord
	Event   mutation.EventRecord
}

type DecideChangeOrderMutation struct {
	Order    ChangeOrder
	Decision ChangeOrderDecision
	Audit    mutation.AuditRecord
	Event    mutation.EventRecord
}

type ApplyChangeOrderMutation struct {
	Order       ChangeOrder
	Project     Project
	Application ChangeOrderApplication
	Audit       mutation.AuditRecord
	Event       mutation.EventRecord
}

package httpapi

import (
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
)

type PublishWorkflowRequest struct {
	ExpectedVersion int64               `json:"expected_version,omitempty"`
	Key             string              `json:"key"`
	Name            string              `json:"name"`
	Enabled         bool                `json:"enabled"`
	Priority        int                 `json:"priority"`
	StableOrder     int                 `json:"stable_order"`
	Fallback        bool                `json:"fallback"`
	EffectiveFrom   time.Time           `json:"effective_from,omitempty"`
	EffectiveTo     *time.Time          `json:"effective_to,omitempty"`
	Conditions      workflow.Conditions `json:"conditions,omitempty"`
	Definition      workflow.Definition `json:"definition"`
}

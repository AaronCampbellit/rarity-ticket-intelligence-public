package sla

import (
	"errors"
	"sort"
	"time"
)

var (
	ErrInvalidPolicy = errors.New("invalid SLA policy")
	ErrNoPolicy      = errors.New("no eligible SLA policy")
)

type PolicyConditions struct {
	ClientID   string `json:"client_id,omitempty"`
	RecordType string `json:"record_type,omitempty"`
	Priority   string `json:"priority,omitempty"`
	QueueID    string `json:"queue_id,omitempty"`
	ServiceID  string `json:"service_id,omitempty"`
	ContractID string `json:"contract_id,omitempty"`
}

type PublishedPolicy struct {
	ID                      string            `json:"id"`
	MSPID                   string            `json:"msp_id"`
	ClientID                string            `json:"client_id,omitempty"`
	Key                     string            `json:"key"`
	Name                    string            `json:"name"`
	Version                 int64             `json:"version"`
	Calendar                PublishedCalendar `json:"calendar"`
	Conditions              PolicyConditions  `json:"conditions,omitempty"`
	ResponseTargetSeconds   int               `json:"response_target_seconds"`
	ResolutionTargetSeconds int               `json:"resolution_target_seconds"`
	WarningPercent          int               `json:"warning_percent"`
	PauseStates             []string          `json:"pause_states,omitempty"`
	Enabled                 bool              `json:"enabled"`
	Priority                int               `json:"priority"`
	StableOrder             int               `json:"stable_order"`
	Fallback                bool              `json:"fallback"`
	PublishedAt             time.Time         `json:"published_at"`
	PublishedBy             string            `json:"published_by"`
}

type PolicyInput struct {
	ClientID   string
	RecordType string
	Priority   string
	QueueID    string
	ServiceID  string
	ContractID string
}

type PolicyTraceEntry struct {
	PolicyID string `json:"policy_id"`
	Version  int64  `json:"version"`
	Priority int    `json:"priority"`
	Matched  bool   `json:"matched"`
	Reason   string `json:"reason"`
}

type PolicySelection struct {
	Policy PublishedPolicy    `json:"policy"`
	Trace  []PolicyTraceEntry `json:"trace"`
}

type PolicyEngine struct {
	policies []PublishedPolicy
}

func NewPolicyEngine(policies []PublishedPolicy) (*PolicyEngine, error) {
	if len(policies) == 0 {
		return nil, ErrInvalidPolicy
	}
	ordered := append([]PublishedPolicy(nil), policies...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Priority == ordered[j].Priority {
			return ordered[i].StableOrder < ordered[j].StableOrder
		}
		return ordered[i].Priority > ordered[j].Priority
	})
	ids := map[string]struct{}{}
	orders := map[[2]int]struct{}{}
	fallbacks := 0
	for _, policy := range ordered {
		if err := validatePolicy(policy); err != nil {
			return nil, err
		}
		if _, duplicate := ids[policy.ID]; duplicate {
			return nil, ErrInvalidPolicy
		}
		ids[policy.ID] = struct{}{}
		if policy.Enabled {
			order := [2]int{policy.Priority, policy.StableOrder}
			if _, duplicate := orders[order]; duplicate {
				return nil, ErrInvalidPolicy
			}
			orders[order] = struct{}{}
		}
		if policy.Fallback {
			fallbacks++
			if !policy.Enabled || !emptyPolicyConditions(policy.Conditions) ||
				policy.ClientID != "" {
				return nil, ErrInvalidPolicy
			}
		}
	}
	if fallbacks != 1 {
		return nil, ErrInvalidPolicy
	}
	return &PolicyEngine{policies: ordered}, nil
}

func (e *PolicyEngine) Select(input PolicyInput) (PolicySelection, error) {
	trace := make([]PolicyTraceEntry, 0, len(e.policies))
	var selected *PublishedPolicy
	for index := range e.policies {
		policy := &e.policies[index]
		matched, reason := policyMatches(*policy, input)
		trace = append(trace, PolicyTraceEntry{
			PolicyID: policy.ID, Version: policy.Version,
			Priority: policy.Priority, Matched: matched, Reason: reason,
		})
		if matched && selected == nil {
			selected = policy
		}
	}
	if selected == nil {
		return PolicySelection{Trace: trace}, ErrNoPolicy
	}
	return PolicySelection{Policy: *selected, Trace: trace}, nil
}

func validatePolicy(policy PublishedPolicy) error {
	if policy.ID == "" || policy.Version < 1 ||
		policy.ResponseTargetSeconds < 1 ||
		policy.ResolutionTargetSeconds < 1 ||
		policy.WarningPercent < 1 || policy.WarningPercent > 99 ||
		policy.StableOrder < 0 {
		return ErrInvalidPolicy
	}
	if _, err := policy.Calendar.Definition.Calendar(); err != nil {
		return ErrInvalidPolicy
	}
	seenPauseStates := map[string]struct{}{}
	for _, state := range policy.PauseStates {
		if state == "" {
			return ErrInvalidPolicy
		}
		if _, duplicate := seenPauseStates[state]; duplicate {
			return ErrInvalidPolicy
		}
		seenPauseStates[state] = struct{}{}
	}
	return nil
}

func policyMatches(policy PublishedPolicy, input PolicyInput) (bool, string) {
	if !policy.Enabled {
		return false, "disabled"
	}
	if policy.ClientID != "" && policy.ClientID != input.ClientID {
		return false, "policy scope did not match Client"
	}
	conditions := policy.Conditions
	switch {
	case conditions.ClientID != "" && conditions.ClientID != input.ClientID:
		return false, "client condition did not match"
	case conditions.RecordType != "" && conditions.RecordType != input.RecordType:
		return false, "record type condition did not match"
	case conditions.Priority != "" && conditions.Priority != input.Priority:
		return false, "priority condition did not match"
	case conditions.QueueID != "" && conditions.QueueID != input.QueueID:
		return false, "queue condition did not match"
	case conditions.ServiceID != "" && conditions.ServiceID != input.ServiceID:
		return false, "service condition did not match"
	case conditions.ContractID != "" && conditions.ContractID != input.ContractID:
		return false, "contract condition did not match"
	}
	if policy.Fallback {
		return true, "fallback"
	}
	return true, "all configured conditions matched"
}

func emptyPolicyConditions(conditions PolicyConditions) bool {
	return conditions == (PolicyConditions{})
}

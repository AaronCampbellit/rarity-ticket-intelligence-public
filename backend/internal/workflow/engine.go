// Package workflow selects immutable published workflow versions and validates
// governed state transitions.
package workflow

import (
	"errors"
	"sort"
	"time"
)

var (
	ErrInvalidConfiguration   = errors.New("invalid workflow configuration")
	ErrNoWorkflow             = errors.New("no eligible workflow")
	ErrTransitionNotAllowed   = errors.New("transition not allowed")
	ErrTransitionRequirements = errors.New("transition requirements not met")
)

type Conditions struct {
	ClientID   string `json:"client_id,omitempty"`
	RecordType string `json:"record_type,omitempty"`
	Priority   string `json:"priority,omitempty"`
	QueueID    string `json:"queue_id,omitempty"`
	Source     string `json:"source,omitempty"`
}

type Published struct {
	ID            string     `json:"id"`
	MSPID         string     `json:"msp_id"`
	ClientID      string     `json:"client_id,omitempty"`
	Key           string     `json:"key"`
	Name          string     `json:"name"`
	Version       int64      `json:"version"`
	Priority      int        `json:"priority"`
	StableOrder   int        `json:"stable_order"`
	Fallback      bool       `json:"fallback"`
	Enabled       bool       `json:"enabled"`
	EffectiveFrom time.Time  `json:"effective_from,omitempty"`
	EffectiveTo   *time.Time `json:"effective_to,omitempty"`
	Conditions    Conditions `json:"conditions,omitempty"`
	Definition    Definition `json:"definition"`
}

func (d Definition) Validate() error {
	if len(d.States) == 0 {
		return ErrInvalidConfiguration
	}
	states := make(map[string]struct{}, len(d.States))
	for _, state := range d.States {
		if state.Key == "" || !validSLABehavior(state.SLABehavior) {
			return ErrInvalidConfiguration
		}
		if _, duplicate := states[state.Key]; duplicate {
			return ErrInvalidConfiguration
		}
		states[state.Key] = struct{}{}
	}
	transitions := make(map[Transition]struct{}, len(d.Transitions))
	for _, transition := range d.Transitions {
		if transition.From == transition.To {
			return ErrInvalidConfiguration
		}
		if _, ok := states[transition.From]; !ok {
			return ErrInvalidConfiguration
		}
		if _, ok := states[transition.To]; !ok {
			return ErrInvalidConfiguration
		}
		if _, duplicate := transitions[transition]; duplicate {
			return ErrInvalidConfiguration
		}
		transitions[transition] = struct{}{}
	}
	return nil
}

type Input struct {
	ClientID    string
	RecordType  string
	Priority    string
	QueueID     string
	Source      string
	EvaluatedAt time.Time
}

type TraceEntry struct {
	WorkflowID string `json:"workflow_id"`
	Version    int64  `json:"version"`
	Priority   int    `json:"priority"`
	Matched    bool   `json:"matched"`
	Reason     string `json:"reason"`
}

type Selection struct {
	WorkflowID  string       `json:"workflow_id"`
	Version     int64        `json:"version"`
	EvaluatedAt time.Time    `json:"evaluated_at"`
	Trace       []TraceEntry `json:"trace"`
}

type Engine struct {
	workflows []Published
}

func NewEngine(workflows []Published) (*Engine, error) {
	if len(workflows) == 0 {
		return nil, ErrInvalidConfiguration
	}
	ordered := append([]Published(nil), workflows...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Priority == ordered[j].Priority {
			return ordered[i].StableOrder < ordered[j].StableOrder
		}
		return ordered[i].Priority > ordered[j].Priority
	})
	fallbacks := 0
	seenPriorityOrder := map[[2]int]struct{}{}
	for _, workflow := range ordered {
		if workflow.ID == "" || workflow.Version < 1 {
			return nil, ErrInvalidConfiguration
		}
		if workflow.Fallback {
			fallbacks++
			if !workflow.Enabled ||
				!emptyConditions(workflow.Conditions) ||
				!workflow.EffectiveFrom.IsZero() ||
				workflow.EffectiveTo != nil {
				return nil, ErrInvalidConfiguration
			}
		}
		if workflow.Enabled {
			key := [2]int{workflow.Priority, workflow.StableOrder}
			if _, exists := seenPriorityOrder[key]; exists {
				return nil, ErrInvalidConfiguration
			}
			seenPriorityOrder[key] = struct{}{}
		}
	}
	if fallbacks != 1 {
		return nil, ErrInvalidConfiguration
	}
	return &Engine{workflows: ordered}, nil
}

func (e *Engine) Select(input Input) (Selection, error) {
	evaluatedAt := input.EvaluatedAt.UTC()
	trace := make([]TraceEntry, 0, len(e.workflows))
	var selected *Published
	for index := range e.workflows {
		workflow := &e.workflows[index]
		matched, reason := workflowMatches(*workflow, input, evaluatedAt)
		trace = append(trace, TraceEntry{
			WorkflowID: workflow.ID, Version: workflow.Version,
			Priority: workflow.Priority, Matched: matched, Reason: reason,
		})
		if matched && selected == nil {
			selected = workflow
		}
	}
	if selected == nil {
		return Selection{EvaluatedAt: evaluatedAt, Trace: trace}, ErrNoWorkflow
	}
	return Selection{
		WorkflowID: selected.ID, Version: selected.Version,
		EvaluatedAt: evaluatedAt, Trace: trace,
	}, nil
}

type State struct {
	Key           string      `json:"key"`
	RequiresOwner bool        `json:"requires_owner,omitempty"`
	SLABehavior   SLABehavior `json:"sla_behavior,omitempty"`
}

type SLABehavior string

const (
	SLAActive    SLABehavior = "active"
	SLAResolved  SLABehavior = "resolved"
	SLACancelled SLABehavior = "cancelled"
)

func validSLABehavior(behavior SLABehavior) bool {
	switch behavior {
	case "", SLAActive, SLAResolved, SLACancelled:
		return true
	default:
		return false
	}
}

type Transition struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Definition struct {
	States      []State      `json:"states"`
	Transitions []Transition `json:"transitions"`
}

func (d Definition) HasState(key string) bool {
	for _, state := range d.States {
		if state.Key == key {
			return true
		}
	}
	return false
}

func (d Definition) State(key string) (State, bool) {
	for _, state := range d.States {
		if state.Key == key {
			return state, true
		}
	}
	return State{}, false
}

type TransitionInput struct {
	From    string
	To      string
	OwnerID string
}

func (d Definition) ValidateTransition(input TransitionInput) error {
	allowed := false
	for _, transition := range d.Transitions {
		if transition.From == input.From && transition.To == input.To {
			allowed = true
			break
		}
	}
	if !allowed {
		return ErrTransitionNotAllowed
	}
	for _, state := range d.States {
		if state.Key == input.To {
			if state.RequiresOwner && input.OwnerID == "" {
				return ErrTransitionRequirements
			}
			return nil
		}
	}
	return ErrInvalidConfiguration
}

func workflowMatches(workflow Published, input Input, evaluatedAt time.Time) (bool, string) {
	if !workflow.Enabled {
		return false, "disabled"
	}
	if !workflow.EffectiveFrom.IsZero() && evaluatedAt.Before(workflow.EffectiveFrom) {
		return false, "not yet effective"
	}
	if workflow.EffectiveTo != nil && !evaluatedAt.Before(*workflow.EffectiveTo) {
		return false, "expired"
	}
	conditions := workflow.Conditions
	if conditions.ClientID != "" && conditions.ClientID != input.ClientID {
		return false, "client condition did not match"
	}
	if conditions.RecordType != "" && conditions.RecordType != input.RecordType {
		return false, "record type condition did not match"
	}
	if conditions.Priority != "" && conditions.Priority != input.Priority {
		return false, "priority condition did not match"
	}
	if conditions.QueueID != "" && conditions.QueueID != input.QueueID {
		return false, "queue condition did not match"
	}
	if conditions.Source != "" && conditions.Source != input.Source {
		return false, "source condition did not match"
	}
	if workflow.Fallback {
		return true, "fallback"
	}
	return true, "all configured conditions matched"
}

func emptyConditions(conditions Conditions) bool {
	return conditions == (Conditions{})
}

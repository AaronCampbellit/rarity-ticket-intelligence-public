// Package routing provides deterministic, explainable queue selection.
package routing

import (
	"errors"
	"fmt"
	"sort"
)

var (
	ErrInvalidRules = errors.New("invalid routing rules")
	ErrNoRoute      = errors.New("no route")
)

type Rule struct {
	ID         string `json:"id,omitempty"`
	Position   int    `json:"position"`
	ClientID   string `json:"client_id,omitempty"`
	RecordType string `json:"record_type,omitempty"`
	Priority   string `json:"priority,omitempty"`
	QueueID    string `json:"queue_id"`
}

type Input struct {
	ClientID   string
	RecordType string
	Priority   string
}

type Decision struct {
	QueueID     string `json:"queue_id"`
	RuleID      string `json:"rule_id"`
	Explanation string `json:"explanation"`
}

type Engine struct {
	rules []Rule
}

func NewEngine(rules []Rule) (*Engine, error) {
	if len(rules) == 0 {
		return nil, ErrInvalidRules
	}
	ordered := append([]Rule(nil), rules...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Position < ordered[j].Position })
	positions := map[int]struct{}{}
	ids := map[string]struct{}{}
	for index, rule := range ordered {
		if rule.ID == "" || rule.QueueID == "" || rule.Position < 1 {
			return nil, ErrInvalidRules
		}
		if rule.RecordType != "" && !validRecordType(rule.RecordType) {
			return nil, ErrInvalidRules
		}
		if _, exists := ids[rule.ID]; exists {
			return nil, ErrInvalidRules
		}
		ids[rule.ID] = struct{}{}
		if _, exists := positions[rule.Position]; exists {
			return nil, ErrInvalidRules
		}
		positions[rule.Position] = struct{}{}
		if unconditional(rule) && index != len(ordered)-1 {
			return nil, ErrInvalidRules
		}
	}
	if !unconditional(ordered[len(ordered)-1]) {
		return nil, ErrInvalidRules
	}
	return &Engine{rules: ordered}, nil
}

func validRecordType(recordType string) bool {
	switch recordType {
	case "incident", "request", "change", "problem":
		return true
	default:
		return false
	}
}

func (e *Engine) Route(input Input) (Decision, error) {
	for _, rule := range e.rules {
		if rule.ClientID != "" && rule.ClientID != input.ClientID {
			continue
		}
		if rule.RecordType != "" && rule.RecordType != input.RecordType {
			continue
		}
		if rule.Priority != "" && rule.Priority != input.Priority {
			continue
		}
		return Decision{
			QueueID: rule.QueueID, RuleID: rule.ID,
			Explanation: fmt.Sprintf("matched routing rule %s at position %d", rule.ID, rule.Position),
		}, nil
	}
	return Decision{}, ErrNoRoute
}

func unconditional(rule Rule) bool {
	return rule.ClientID == "" && rule.RecordType == "" && rule.Priority == ""
}

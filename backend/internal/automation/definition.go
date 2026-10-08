// Package automation defines immutable, typed workflow automation contracts.
// Execution is limited to supported application-service actions and approved
// external HTTP connections.
package automation

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
)

var ErrInvalidDefinition = errors.New("invalid automation definition")

const MaxDefinitionDepth = 8

type DefinitionState string

const (
	Draft     DefinitionState = "draft"
	Published DefinitionState = "published"
)

type Definition struct {
	ID           string          `json:"id"`
	MSPID        string          `json:"msp_id"`
	Version      int64           `json:"version"`
	State        DefinitionState `json:"state"`
	ClientScopes []string        `json:"client_scopes"`
	Capabilities []string        `json:"capabilities"`
	Trigger      Trigger         `json:"trigger"`
	Steps        []Step          `json:"steps"`
}

type Trigger struct {
	EventType string `json:"event_type"`
}

type StepKind string

const (
	StepAction    StepKind = "action"
	StepCondition StepKind = "condition"
	StepBranch    StepKind = "branch"
	StepWait      StepKind = "wait"
)

type Step struct {
	ID          string     `json:"id"`
	Kind        StepKind   `json:"kind"`
	Action      *Action    `json:"action,omitempty"`
	Condition   *Condition `json:"condition,omitempty"`
	Children    []Step     `json:"children,omitempty"`
	WaitSeconds int64      `json:"wait_seconds,omitempty"`
}

type ActionKind string

const (
	ActionUpdateField ActionKind = "update_field"
	ActionAddComment  ActionKind = "add_comment"
	ActionAssign      ActionKind = "assign"
	ActionTransition  ActionKind = "transition"
	ActionCallHTTP    ActionKind = "call_http"
	ActionAddTags     ActionKind = "add_tags"
	ActionRemoveTags  ActionKind = "remove_tags"
)

type Action struct {
	Kind          ActionKind        `json:"kind"`
	ConnectionRef string            `json:"connection_ref,omitempty"`
	Parameters    map[string]string `json:"parameters"`
}

type ConditionOperator string

const (
	OperatorEquals        ConditionOperator = "equals"
	OperatorNotEquals     ConditionOperator = "not_equals"
	OperatorContains      ConditionOperator = "contains"
	OperatorHasAnyTag     ConditionOperator = "has_any_tag"
	OperatorHasAllTags    ConditionOperator = "has_all_tags"
	OperatorHasNoTags     ConditionOperator = "has_no_tags"
	OperatorHasTagInGroup ConditionOperator = "has_tag_in_group"
)

type Condition struct {
	Field    string            `json:"field"`
	Operator ConditionOperator `json:"operator"`
	Value    string            `json:"value"`
}

func Validate(definition Definition) error {
	if strings.TrimSpace(definition.ID) == "" ||
		strings.TrimSpace(definition.MSPID) == "" ||
		definition.Version < 1 ||
		(definition.State != Draft && definition.State != Published) ||
		len(normalizedStrings(definition.ClientScopes)) == 0 ||
		len(normalizedStrings(definition.Capabilities)) == 0 ||
		strings.TrimSpace(definition.Trigger.EventType) == "" ||
		len(definition.Steps) == 0 {
		return ErrInvalidDefinition
	}
	for _, step := range definition.Steps {
		if err := validateStep(step, 1); err != nil {
			return err
		}
	}
	return nil
}

func validateStep(step Step, depth int) error {
	if depth > MaxDefinitionDepth || strings.TrimSpace(step.ID) == "" {
		return ErrInvalidDefinition
	}
	switch step.Kind {
	case StepAction:
		if step.Action == nil || step.Condition != nil || len(step.Children) > 0 ||
			step.WaitSeconds != 0 || !validAction(*step.Action) {
			return ErrInvalidDefinition
		}
	case StepCondition:
		if step.Condition == nil || step.Action != nil || len(step.Children) == 0 ||
			step.WaitSeconds != 0 || !validCondition(*step.Condition) {
			return ErrInvalidDefinition
		}
	case StepBranch:
		if step.Action != nil || step.Condition != nil || len(step.Children) == 0 ||
			step.WaitSeconds != 0 {
			return ErrInvalidDefinition
		}
	case StepWait:
		if step.Action != nil || step.Condition != nil || len(step.Children) > 0 ||
			step.WaitSeconds < 1 || step.WaitSeconds > 30*24*60*60 {
			return ErrInvalidDefinition
		}
	default:
		return ErrInvalidDefinition
	}
	for _, child := range step.Children {
		if err := validateStep(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func validAction(action Action) bool {
	switch action.Kind {
	case ActionUpdateField, ActionAddComment, ActionAssign, ActionTransition:
		if strings.TrimSpace(action.ConnectionRef) != "" {
			return false
		}
	case ActionCallHTTP:
		if strings.TrimSpace(action.ConnectionRef) == "" {
			return false
		}
	case ActionAddTags, ActionRemoveTags:
		if strings.TrimSpace(action.ConnectionRef) != "" || len(action.Parameters) != 1 || !validTagIDs(action.Parameters["tag_ids"]) {
			return false
		}
	default:
		return false
	}
	for key := range action.Parameters {
		normalized := strings.ToLower(strings.TrimSpace(key))
		for _, forbidden := range []string{"secret", "password", "token", "credential", "api_key"} {
			if strings.Contains(normalized, forbidden) {
				return false
			}
		}
	}
	return len(action.Parameters) > 0
}

func validCondition(condition Condition) bool {
	if strings.TrimSpace(condition.Field) == "" {
		return false
	}
	switch condition.Operator {
	case OperatorEquals, OperatorNotEquals, OperatorContains:
		return true
	case OperatorHasAnyTag, OperatorHasAllTags, OperatorHasNoTags:
		return condition.Field == "effective_tag_ids" && validTagIDs(condition.Value)
	case OperatorHasTagInGroup:
		return condition.Field == "group_ids" && uuid.Validate(strings.TrimSpace(condition.Value)) == nil
	default:
		return false
	}
}

func validTagIDs(raw string) bool {
	var ids []string
	if json.Unmarshal([]byte(raw), &ids) != nil || len(ids) == 0 || len(ids) > 50 {
		return false
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if uuid.Validate(id) != nil {
			return false
		}
		if _, exists := seen[id]; exists {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

// ReferencedCatalogIDs returns the immutable catalog identities a validated
// definition depends on. Labels are deliberately excluded because they are
// mutable display data.
func ReferencedCatalogIDs(definition Definition) (tagIDs, groupIDs []string) {
	tags := map[string]struct{}{}
	groups := map[string]struct{}{}
	var visit func([]Step)
	visit = func(steps []Step) {
		for _, step := range steps {
			if step.Action != nil && (step.Action.Kind == ActionAddTags || step.Action.Kind == ActionRemoveTags) {
				var ids []string
				_ = json.Unmarshal([]byte(step.Action.Parameters["tag_ids"]), &ids)
				for _, id := range ids {
					tags[strings.TrimSpace(id)] = struct{}{}
				}
			}
			if step.Condition != nil {
				switch step.Condition.Operator {
				case OperatorHasAnyTag, OperatorHasAllTags, OperatorHasNoTags:
					var ids []string
					_ = json.Unmarshal([]byte(step.Condition.Value), &ids)
					for _, id := range ids {
						tags[strings.TrimSpace(id)] = struct{}{}
					}
				case OperatorHasTagInGroup:
					groups[strings.TrimSpace(step.Condition.Value)] = struct{}{}
				}
			}
			visit(step.Children)
		}
	}
	visit(definition.Steps)
	for id := range tags {
		if id != "" {
			tagIDs = append(tagIDs, id)
		}
	}
	for id := range groups {
		if id != "" {
			groupIDs = append(groupIDs, id)
		}
	}
	return tagIDs, groupIDs
}

func Publish(definition Definition) (Definition, error) {
	if definition.State != Draft || Validate(definition) != nil {
		return Definition{}, ErrInvalidDefinition
	}
	result := cloneDefinition(definition)
	result.State = Published
	return result, nil
}

func Revise(definition Definition) (Definition, error) {
	if definition.State != Published || Validate(definition) != nil {
		return Definition{}, ErrInvalidDefinition
	}
	result := cloneDefinition(definition)
	result.State = Draft
	result.Version++
	return result, nil
}

func cloneDefinition(definition Definition) Definition {
	result := definition
	result.ClientScopes = append([]string(nil), definition.ClientScopes...)
	result.Capabilities = append([]string(nil), definition.Capabilities...)
	result.Steps = cloneSteps(definition.Steps)
	return result
}

func cloneSteps(steps []Step) []Step {
	result := make([]Step, len(steps))
	for index, step := range steps {
		result[index] = step
		if step.Action != nil {
			action := *step.Action
			action.Parameters = make(map[string]string, len(step.Action.Parameters))
			for key, value := range step.Action.Parameters {
				action.Parameters[key] = value
			}
			result[index].Action = &action
		}
		if step.Condition != nil {
			condition := *step.Condition
			result[index].Condition = &condition
		}
		result[index].Children = cloneSteps(step.Children)
	}
	return result
}

func normalizedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

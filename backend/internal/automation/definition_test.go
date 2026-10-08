package automation

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestValidateAcceptsBoundedImmutableTagActionsAndConditions(t *testing.T) {
	tagIDs, _ := json.Marshal([]string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"})
	definition := Definition{ID: "definition", MSPID: "msp", Version: 1, State: Draft, ClientScopes: []string{"client"}, Capabilities: []string{"classification.apply"}, Trigger: Trigger{EventType: "tag.added"}, Steps: []Step{{ID: "condition", Kind: StepCondition, Condition: &Condition{Field: "effective_tag_ids", Operator: OperatorHasAllTags, Value: string(tagIDs)}, Children: []Step{{ID: "remove", Kind: StepAction, Action: &Action{Kind: ActionRemoveTags, Parameters: map[string]string{"tag_ids": string(tagIDs)}}}}}}}
	if err := Validate(definition); err != nil {
		t.Fatalf("Validate() error=%v", err)
	}
	definition.Trigger.EventType = "tag.removed"
	definition.Steps[0].Condition = &Condition{Field: "group_ids", Operator: OperatorHasTagInGroup, Value: "33333333-3333-4333-8333-333333333333"}
	definition.Steps[0].Children[0].Action.Kind = ActionAddTags
	if err := Validate(definition); err != nil {
		t.Fatalf("Validate(tag.removed) error=%v", err)
	}
}

func TestValidateRejectsInvalidDuplicateAndOversizedTagIDs(t *testing.T) {
	valid := func(raw string) Definition {
		return Definition{ID: "definition", MSPID: "msp", Version: 1, State: Draft, ClientScopes: []string{"client"}, Capabilities: []string{"classification.apply"}, Trigger: Trigger{EventType: "tag.added"}, Steps: []Step{{ID: "tags", Kind: StepAction, Action: &Action{Kind: ActionAddTags, Parameters: map[string]string{"tag_ids": raw}}}}}
	}
	for _, raw := range []string{`["not-a-uuid"]`, `["11111111-1111-4111-8111-111111111111","11111111-1111-4111-8111-111111111111"]`, `[]`} {
		if err := Validate(valid(raw)); !errors.Is(err, ErrInvalidDefinition) {
			t.Fatalf("raw=%s error=%v", raw, err)
		}
	}
	ids := make([]string, 51)
	for i := range ids {
		ids[i] = "11111111-1111-4111-8111-111111111111"
	}
	raw, _ := json.Marshal(ids)
	if err := Validate(valid(string(raw))); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("oversized error=%v", err)
	}
}

func TestValidateAcceptsTypedNestedAutomationWithConnectionReference(t *testing.T) {
	definition := Definition{
		ID: "automation-id", MSPID: "msp-id", Version: 1, State: Draft,
		ClientScopes: []string{"client-alpha"},
		Capabilities: []string{"work_record.edit", "comment.add"},
		Trigger:      Trigger{EventType: "work_record.created"},
		Steps: []Step{{
			ID: "condition", Kind: StepCondition,
			Condition: &Condition{Field: "priority", Operator: OperatorEquals, Value: "critical"},
			Children: []Step{
				{ID: "assign", Kind: StepAction, Action: &Action{
					Kind: ActionAssign, Parameters: map[string]string{"team_id": "team-id"},
				}},
				{ID: "notify", Kind: StepAction, Action: &Action{
					Kind: ActionCallHTTP, ConnectionRef: "connection-id",
					Parameters: map[string]string{"request_template_id": "template-id"},
				}},
			},
		}},
	}
	if err := Validate(definition); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsUnknownActionsInlineCredentialsAndExcessiveDepth(t *testing.T) {
	base := Definition{
		ID: "automation-id", MSPID: "msp-id", Version: 1, State: Draft,
		ClientScopes: []string{"client-alpha"}, Capabilities: []string{"work_record.edit"},
		Trigger: Trigger{EventType: "work_record.created"},
	}
	cases := []Step{
		{ID: "code", Kind: StepAction, Action: &Action{Kind: "run_shell"}},
		{ID: "http", Kind: StepAction, Action: &Action{
			Kind: ActionCallHTTP, Parameters: map[string]string{"api_key": "secret"},
		}},
		tooDeepStep(MaxDefinitionDepth + 1),
	}
	for _, step := range cases {
		definition := base
		definition.Steps = []Step{step}
		if err := Validate(definition); !errors.Is(err, ErrInvalidDefinition) {
			t.Fatalf("invalid step accepted: %+v error=%v", step, err)
		}
	}
}

func TestPublishAndRevisePreserveImmutablePublishedVersion(t *testing.T) {
	draft := Definition{
		ID: "automation-id", MSPID: "msp-id", Version: 1, State: Draft,
		ClientScopes: []string{"client-alpha"}, Capabilities: []string{"work_record.edit"},
		Trigger: Trigger{EventType: "work_record.created"},
		Steps: []Step{{ID: "assign", Kind: StepAction, Action: &Action{
			Kind: ActionAssign, Parameters: map[string]string{"team_id": "team-id"},
		}}},
	}
	published, err := Publish(draft)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	revision, err := Revise(published)
	if err != nil {
		t.Fatalf("Revise() error = %v", err)
	}
	revision.Steps[0].Action.Parameters["team_id"] = "other-team"
	if published.State != Published || published.Version != 1 ||
		published.Steps[0].Action.Parameters["team_id"] != "team-id" ||
		revision.State != Draft || revision.Version != 2 {
		t.Fatalf("published version mutated: published=%+v revision=%+v", published, revision)
	}
}

func TestDefinitionJSONUsesStableLowercaseRuntimeContract(t *testing.T) {
	definition := Definition{
		ID: "automation-id", MSPID: "msp-id", Version: 1, State: Published,
		ClientScopes: []string{"client-alpha"},
		Capabilities: []string{"work_record.edit"},
		Trigger:      Trigger{EventType: "work_record.created"},
		Steps: []Step{{ID: "assign", Kind: StepAction, Action: &Action{
			Kind:       ActionAssign,
			Parameters: map[string]string{"team_id": "team-id"},
		}}},
	}
	payload, err := json.Marshal(definition)
	if err != nil {
		t.Fatalf("Marshal() error=%v", err)
	}
	var decoded Definition
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("Unmarshal() error=%v", err)
	}
	if strings.Contains(string(payload), `"Steps"`) ||
		!strings.Contains(string(payload), `"steps"`) ||
		Validate(decoded) != nil {
		t.Fatalf("unstable definition JSON=%s decoded=%+v", payload, decoded)
	}
}

func tooDeepStep(depth int) Step {
	step := Step{ID: "leaf", Kind: StepWait, WaitSeconds: 1}
	for index := 0; index < depth; index++ {
		step = Step{
			ID: "branch", Kind: StepBranch, Children: []Step{step},
		}
	}
	return step
}

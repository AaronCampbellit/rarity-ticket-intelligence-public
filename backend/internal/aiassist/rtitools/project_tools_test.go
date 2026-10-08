package rtitools

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
)

type projectWorkspaceActionsStub struct {
	command projects.AIWorkspaceCreateCommand
}

func (s *projectWorkspaceActionsStub) CreateProject(
	_ context.Context,
	command projects.AIWorkspaceCreateCommand,
) (projects.AIWorkspaceCreateResult, error) {
	s.command = command
	return projects.AIWorkspaceCreateResult{
		Project: projects.Project{
			ID:        projects.ProjectID(command.ProjectID),
			DisplayID: command.DisplayID, Name: command.Name,
			Version: 1,
		},
	}, nil
}

func TestProjectCreateToolGeneratesOnlySystemMetadataAndPreviewsUserValues(t *testing.T) {
	actions := &projectWorkspaceActionsStub{}
	ids := []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
		"44444444-4444-4444-8444-444444444444",
	}
	next := 0
	tool := NewProjectCreateTool(actions, func() string {
		id := ids[next]
		next++
		return id
	})
	principal := testPrincipal("project.create", "task.create")
	preparer, ok := tool.(interface {
		Prepare(
			context.Context,
			authorization.Principal,
			json.RawMessage,
		) (json.RawMessage, error)
	})
	if !ok {
		t.Fatal("project tool must prepare system metadata before validation")
	}

	prepared, err := preparer.Prepare(
		context.Background(),
		principal,
		json.RawMessage(`{
			"client_id":"client-1",
			"name":"Onboarding",
			"tasks":["A","B","C"],
			"tag_ids":["tag-1"]
		}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tool.Validate(prepared); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(prepared), "owner") ||
		strings.Contains(string(prepared), "budget") ||
		strings.Contains(string(prepared), "planned_start") {
		t.Fatalf("prepared input invented business fields: %s", prepared)
	}
	preview, err := tool.Preview(context.Background(), principal, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if preview.TargetType != "project" ||
		preview.TargetID != ids[0] ||
		preview.Changes["name"].After != "Onboarding" ||
		!reflect.DeepEqual(preview.Changes["tag_ids"].After, []string{"tag-1"}) {
		t.Fatalf("preview=%+v", preview)
	}
	titles, ok := preview.Changes["tasks"].After.([]string)
	if !ok || len(titles) != 3 || titles[0] != "A" || titles[2] != "C" {
		t.Fatalf("tasks=%#v", preview.Changes["tasks"].After)
	}

	if _, err := tool.Execute(
		context.Background(), principal, prepared, "correlation-1",
	); err != nil {
		t.Fatal(err)
	}
	if actions.command.ProjectID != ids[0] ||
		actions.command.DisplayID != "PRJ-11111111" ||
		actions.command.Name != "Onboarding" ||
		actions.command.CorrelationID != "correlation-1" ||
		len(actions.command.Tasks) != 3 ||
		actions.command.Tasks[0].ID != ids[1] ||
		actions.command.Tasks[2].Title != "C" {
		t.Fatalf("command=%+v", actions.command)
	}
}

func TestProjectCreateToolRejectsUserSuppliedSystemOrMissingBusinessData(t *testing.T) {
	tool := NewProjectCreateTool(
		&projectWorkspaceActionsStub{},
		func() string { return "11111111-1111-4111-8111-111111111111" },
	)
	principal := testPrincipal("project.create", "task.create")
	preparer := tool.(interface {
		Prepare(
			context.Context,
			authorization.Principal,
			json.RawMessage,
		) (json.RawMessage, error)
	})
	for _, raw := range []string{
		`{"client_id":"client-1","name":"","tasks":["A"]}`,
		`{"client_id":"client-1","name":"Onboarding","tasks":[]}`,
		`{"client_id":"client-1","name":"Onboarding","tasks":["A"]}`,
		`{"client_id":"client-1","name":"Onboarding","tasks":["A"],"tag_ids":["tag-1"],"project_id":"spoofed"}`,
	} {
		if _, err := preparer.Prepare(
			context.Background(), principal, json.RawMessage(raw),
		); err == nil {
			t.Fatalf("Prepare(%s) succeeded", raw)
		}
	}
}

var _ aiassist.Tool = NewProjectCreateTool(
	&projectWorkspaceActionsStub{},
	func() string { return "id" },
)

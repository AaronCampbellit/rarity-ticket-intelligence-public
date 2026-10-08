package rtitools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
)

type projectWorkspaceGetterStub struct {
	workspace projects.ProjectWorkspace
}

func (s *projectWorkspaceGetterStub) Get(
	_ context.Context,
	_ authorization.Principal,
	_ scope.Target,
	_ projects.ProjectID,
) (projects.ProjectWorkspace, error) {
	return s.workspace, nil
}

type taskCreatorStub struct {
	command tasks.CreateCommand
}

func (s *taskCreatorStub) Create(
	_ context.Context,
	command tasks.CreateCommand,
) (tasks.Task, error) {
	s.command = command
	return tasks.Task{
		ID: "task-1", Title: command.Title, Status: "open", Version: 1,
	}, nil
}

func TestProjectTaskCreateToolPreviewsResolvedProjectAndExecutesTrustedTask(t *testing.T) {
	projectQueries := &projectWorkspaceGetterStub{workspace: projects.ProjectWorkspace{
		ID: "project-1", DisplayID: "PRJ-NW-2042",
		Name: "Northwind Modernization", Version: 7,
	}}
	taskActions := &taskCreatorStub{}
	tool := NewProjectTaskCreateTool(projectQueries, taskActions)
	principal := testPrincipal("task.create", "project.read")
	input := json.RawMessage(`{
		"client_id":"client-1",
		"project_id":"project-1",
		"project_ref":"Northwind Modernization",
		"title":"Verify backup recovery",
		"tag_ids":["tag-1"]
	}`)

	if err := tool.Validate(input); err != nil {
		t.Fatal(err)
	}
	preview, err := tool.Preview(context.Background(), principal, input)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Summary != "Add task Verify backup recovery to Northwind Modernization" ||
		preview.TargetType != "project" ||
		preview.TargetID != "project-1" ||
		preview.TargetVersion != 7 ||
		preview.Changes["title"].After != "Verify backup recovery" ||
		preview.Changes["status"].After != "open" {
		t.Fatalf("preview=%+v", preview)
	}
	if _, exists := preview.Changes["owner"]; exists {
		t.Fatalf("preview invented owner: %+v", preview.Changes)
	}
	if _, exists := preview.Changes["estimate_minutes"]; exists {
		t.Fatalf("preview invented estimate: %+v", preview.Changes)
	}

	result, err := tool.Execute(
		context.Background(), principal, input, "correlation-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if taskActions.command.Principal.ID != principal.ID ||
		taskActions.command.Parent.Type != tasks.ParentProject ||
		taskActions.command.Parent.ID != "project-1" ||
		taskActions.command.Title != "Verify backup recovery" ||
		taskActions.command.ActorID != principal.ID ||
		taskActions.command.Source != "ai_workspace" ||
		taskActions.command.CorrelationID != "correlation-1" ||
		len(taskActions.command.TagIDs) != 1 || taskActions.command.TagIDs[0] != "tag-1" ||
		taskActions.command.ClassificationPolicy != tagging.CreationRequireMeaningful ||
		taskActions.command.OwnerID != "" ||
		taskActions.command.EstimateMinutes != 0 {
		t.Fatalf("command=%+v", taskActions.command)
	}
	if result.Data["task_id"] != "task-1" ||
		result.Data["project_id"] != "project-1" {
		t.Fatalf("result=%+v", result)
	}
}

func TestProjectTaskCreateToolRejectsInvalidOrMismatchedInput(t *testing.T) {
	projectQueries := &projectWorkspaceGetterStub{workspace: projects.ProjectWorkspace{
		ID: "project-1", DisplayID: "PRJ-NW-2042",
		Name: "Northwind Modernization", Version: 7,
	}}
	tool := NewProjectTaskCreateTool(projectQueries, &taskCreatorStub{})
	principal := testPrincipal("task.create", "project.read")
	for _, raw := range []string{
		`{"client_id":"client-1","project_id":"","project_ref":"Northwind Modernization","title":"A"}`,
		`{"client_id":"client-1","project_id":"project-1","project_ref":"","title":"A"}`,
		`{"client_id":"client-1","project_id":"project-1","project_ref":"Northwind Modernization","title":""}`,
		`{"client_id":"client-1","project_id":"project-1","project_ref":"Northwind Modernization","title":"A","tag_ids":["tag-1"],"owner_id":"invented"}`,
		`{"client_id":"client-1","project_id":"project-1","project_ref":"Northwind Modernization","title":"A"}`,
	} {
		if err := tool.Validate(json.RawMessage(raw)); err == nil {
			t.Fatalf("Validate(%s) succeeded", raw)
		}
	}
	mismatch := json.RawMessage(`{
		"client_id":"client-1",
		"project_id":"project-1",
		"project_ref":"Different Project",
		"title":"A"
	}`)
	if _, err := tool.Preview(
		context.Background(), principal, mismatch,
	); err == nil {
		t.Fatal("mismatched Project reference produced a preview")
	}
}

var _ aiassist.Tool = NewProjectTaskCreateTool(
	&projectWorkspaceGetterStub{},
	&taskCreatorStub{},
)

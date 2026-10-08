package rtitools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
)

type ProjectWorkspaceGetter interface {
	Get(
		context.Context,
		authorization.Principal,
		scope.Target,
		projects.ProjectID,
	) (projects.ProjectWorkspace, error)
}

type TaskCreator interface {
	Create(context.Context, tasks.CreateCommand) (tasks.Task, error)
}

type projectTaskCreateInput struct {
	ClientID   string   `json:"client_id"`
	ProjectID  string   `json:"project_id"`
	ProjectRef string   `json:"project_ref"`
	Title      string   `json:"title"`
	TagIDs     []string `json:"tag_ids"`
}

func NewProjectTaskCreateTool(
	projectQueries ProjectWorkspaceGetter,
	taskActions TaskCreator,
) aiassist.Tool {
	return &functionalTool{
		name: "task.create", capability: "task.create", kind: aiassist.ToolWrite,
		validate: func(raw json.RawMessage) error {
			input, err := decode[projectTaskCreateInput](raw)
			if err != nil ||
				strings.TrimSpace(input.ClientID) == "" ||
				strings.TrimSpace(input.ProjectID) == "" ||
				strings.TrimSpace(input.ProjectRef) == "" ||
				strings.TrimSpace(input.Title) == "" ||
				len(input.TagIDs) == 0 ||
				projectQueries == nil || taskActions == nil {
				return aiassist.ErrInvalidTool
			}
			for _, tagID := range input.TagIDs {
				if strings.TrimSpace(tagID) == "" {
					return aiassist.ErrInvalidTool
				}
			}
			return nil
		},
		resolve: resolveScoped,
		preview: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
		) (aiassist.Preview, error) {
			input, err := decode[projectTaskCreateInput](raw)
			if err != nil {
				return aiassist.Preview{}, err
			}
			project, err := projectQueries.Get(
				ctx, principal, target(principal, input.ClientID),
				projects.ProjectID(input.ProjectID),
			)
			if err != nil {
				return aiassist.Preview{}, err
			}
			if string(project.ID) != strings.TrimSpace(input.ProjectID) ||
				!projects.ProjectReferenceMatches(
					input.ProjectRef, project.Name, project.DisplayID,
				) {
				return aiassist.Preview{}, scope.ErrNotFound
			}
			title := strings.TrimSpace(input.Title)
			return aiassist.Preview{
				Summary: fmt.Sprintf(
					"Add task %s to %s", title, project.Name,
				),
				TargetType: "project", TargetID: string(project.ID),
				TargetVersion: project.Version,
				Changes: map[string]aiassist.Change{
					"project": {Before: nil, After: project.Name},
					"title":   {Before: nil, After: title},
					"status":  {Before: nil, After: "open"},
					"tag_ids": {Before: nil, After: append([]string(nil), input.TagIDs...)},
				},
			}, nil
		},
		execute: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
			correlationID string,
		) (aiassist.ToolResult, error) {
			input, err := decode[projectTaskCreateInput](raw)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			created, err := taskActions.Create(
				ctx,
				tasks.CreateCommand{
					Principal: principal,
					Target:    target(principal, input.ClientID),
					Parent: tasks.Ref{
						Type: tasks.ParentProject, ID: strings.TrimSpace(input.ProjectID),
					},
					Title:                strings.TrimSpace(input.Title),
					ActorID:              principal.ID,
					Source:               "ai_workspace",
					CorrelationID:        correlationID,
					TagIDs:               append([]string(nil), input.TagIDs...),
					ClassificationPolicy: tagging.CreationRequireMeaningful,
				},
			)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{
				Summary: "Created task " + created.Title,
				Data: map[string]any{
					"task_id": created.ID, "project_id": strings.TrimSpace(input.ProjectID),
					"title": created.Title, "status": created.Status,
				},
			}, nil
		},
	}
}

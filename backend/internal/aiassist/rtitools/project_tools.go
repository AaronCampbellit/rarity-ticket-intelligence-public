package rtitools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type projectCreateInput struct {
	ClientID  string   `json:"client_id"`
	Name      string   `json:"name"`
	Tasks     []string `json:"tasks"`
	ProjectID string   `json:"project_id,omitempty"`
	DisplayID string   `json:"display_id,omitempty"`
	TaskIDs   []string `json:"task_ids,omitempty"`
	TagIDs    []string `json:"tag_ids"`
}

func NewProjectCreateTool(
	actions ProjectWorkspaceCreator,
	newID func() string,
) aiassist.Tool {
	return &functionalTool{
		name: "project.create", capability: "project.create", kind: aiassist.ToolWrite,
		prepare: func(
			_ context.Context,
			_ authorization.Principal,
			raw json.RawMessage,
		) (json.RawMessage, error) {
			input, err := decode[projectCreateInput](raw)
			if err != nil ||
				strings.TrimSpace(input.ClientID) == "" ||
				strings.TrimSpace(input.Name) == "" ||
				len(input.Tasks) == 0 ||
				len(input.TagIDs) == 0 ||
				strings.TrimSpace(input.ProjectID) != "" ||
				strings.TrimSpace(input.DisplayID) != "" ||
				len(input.TaskIDs) != 0 ||
				actions == nil || newID == nil {
				return nil, aiassist.ErrInvalidTool
			}
			for _, title := range input.Tasks {
				if strings.TrimSpace(title) == "" {
					return nil, aiassist.ErrInvalidTool
				}
			}
			for _, tagID := range input.TagIDs {
				if strings.TrimSpace(tagID) == "" {
					return nil, aiassist.ErrInvalidTool
				}
			}
			input.ProjectID = strings.TrimSpace(newID())
			input.DisplayID = projectDisplayID(input.ProjectID)
			input.TaskIDs = make([]string, len(input.Tasks))
			for index := range input.Tasks {
				input.Tasks[index] = strings.TrimSpace(input.Tasks[index])
				input.TaskIDs[index] = strings.TrimSpace(newID())
			}
			if input.ProjectID == "" || input.DisplayID == "" {
				return nil, aiassist.ErrInvalidTool
			}
			for _, taskID := range input.TaskIDs {
				if taskID == "" {
					return nil, aiassist.ErrInvalidTool
				}
			}
			prepared, err := json.Marshal(input)
			if err != nil {
				return nil, aiassist.ErrInvalidTool
			}
			return prepared, nil
		},
		validate: func(raw json.RawMessage) error {
			input, err := decode[projectCreateInput](raw)
			if err != nil ||
				strings.TrimSpace(input.ClientID) == "" ||
				strings.TrimSpace(input.Name) == "" ||
				strings.TrimSpace(input.ProjectID) == "" ||
				strings.TrimSpace(input.DisplayID) == "" ||
				len(input.Tasks) == 0 ||
				len(input.TagIDs) == 0 ||
				len(input.Tasks) != len(input.TaskIDs) {
				return aiassist.ErrInvalidTool
			}
			for index, title := range input.Tasks {
				if strings.TrimSpace(title) == "" ||
					strings.TrimSpace(input.TaskIDs[index]) == "" {
					return aiassist.ErrInvalidTool
				}
			}
			return nil
		},
		resolve: func(
			principal authorization.Principal,
			raw json.RawMessage,
		) (targetResult scope.Target, err error) {
			targetResult, err = resolveScoped(principal, raw)
			if err != nil {
				return scope.Target{}, err
			}
			if err := authorization.Authorize(
				principal, "task.create", targetResult,
			); err != nil {
				return scope.Target{}, err
			}
			return targetResult, nil
		},
		preview: func(
			_ context.Context,
			_ authorization.Principal,
			raw json.RawMessage,
		) (aiassist.Preview, error) {
			input, err := decode[projectCreateInput](raw)
			if err != nil {
				return aiassist.Preview{}, err
			}
			return aiassist.Preview{
				Summary: fmt.Sprintf(
					"Create project %s with %d tasks",
					input.Name, len(input.Tasks),
				),
				TargetType: "project", TargetID: input.ProjectID,
				Changes: map[string]aiassist.Change{
					"name":            {Before: nil, After: input.Name},
					"tasks":           {Before: nil, After: append([]string(nil), input.Tasks...)},
					"lifecycle_state": {Before: nil, After: "planned"},
					"task_status":     {Before: nil, After: "open"},
					"tag_ids":         {Before: nil, After: append([]string(nil), input.TagIDs...)},
				},
			}, nil
		},
		execute: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
			correlationID string,
		) (aiassist.ToolResult, error) {
			input, err := decode[projectCreateInput](raw)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			taskInputs := make([]projects.AIWorkspaceTaskInput, len(input.Tasks))
			for index, title := range input.Tasks {
				taskInputs[index] = projects.AIWorkspaceTaskInput{
					ID: input.TaskIDs[index], Title: title,
				}
			}
			result, err := actions.CreateProject(
				ctx,
				projects.AIWorkspaceCreateCommand{
					Principal:     principal,
					Target:        target(principal, input.ClientID),
					ProjectID:     input.ProjectID,
					DisplayID:     input.DisplayID,
					Name:          input.Name,
					Tasks:         taskInputs,
					ActorID:       principal.ID,
					Source:        "ai_workspace",
					CorrelationID: correlationID,
					TagIDs:        input.TagIDs,
				},
			)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{
				Summary: "Created " + result.Project.DisplayID,
				Data: map[string]any{
					"project_id": result.Project.ID,
					"display_id": result.Project.DisplayID,
					"name":       result.Project.Name,
					"task_count": len(taskInputs),
				},
			}, nil
		},
	}
}

func projectDisplayID(id string) string {
	var token strings.Builder
	for _, value := range id {
		if unicode.IsLetter(value) || unicode.IsDigit(value) {
			token.WriteRune(unicode.ToUpper(value))
		}
		if token.Len() == 8 {
			break
		}
	}
	if token.Len() != 8 {
		return ""
	}
	return "PRJ-" + token.String()
}

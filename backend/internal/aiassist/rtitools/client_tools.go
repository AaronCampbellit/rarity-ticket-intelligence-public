package rtitools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type ClientIdentityConflictChecker interface {
	HasClientIdentityConflict(
		context.Context,
		authorization.Principal,
		string,
		string,
	) (bool, error)
}

type ClientCreator interface {
	CreateClient(
		context.Context,
		organizations.CreateClientCommand,
	) (organizations.Client, error)
}

type clientCreateRequest struct {
	DisplayID string `json:"display_id"`
	Name      string `json:"name"`
}

type clientCreateInput struct {
	ClientID  string `json:"client_id"`
	DisplayID string `json:"display_id"`
	Name      string `json:"name"`
}

func NewClientCreateTool(
	conflicts ClientIdentityConflictChecker,
	creator ClientCreator,
	newID func() string,
) aiassist.Tool {
	validInput := func(raw json.RawMessage) (clientCreateInput, error) {
		input, err := decode[clientCreateInput](raw)
		if err != nil ||
			strings.TrimSpace(input.ClientID) == "" ||
			strings.TrimSpace(input.DisplayID) == "" ||
			strings.TrimSpace(input.Name) == "" ||
			conflicts == nil || creator == nil || newID == nil {
			return clientCreateInput{}, aiassist.ErrInvalidTool
		}
		input.ClientID = strings.TrimSpace(input.ClientID)
		input.DisplayID = strings.TrimSpace(input.DisplayID)
		input.Name = strings.TrimSpace(input.Name)
		return input, nil
	}

	return &functionalTool{
		name: "client.create", capability: "client.create", kind: aiassist.ToolWrite,
		prepare: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
		) (json.RawMessage, error) {
			request, err := decode[clientCreateRequest](raw)
			if err != nil ||
				strings.TrimSpace(request.DisplayID) == "" ||
				strings.TrimSpace(request.Name) == "" ||
				strings.TrimSpace(principal.Scope.MSPID) == "" ||
				strings.TrimSpace(principal.Scope.ClientID) != "" ||
				conflicts == nil || creator == nil || newID == nil {
				return nil, aiassist.ErrInvalidTool
			}
			request.DisplayID = strings.TrimSpace(request.DisplayID)
			request.Name = strings.TrimSpace(request.Name)
			if err := rejectClientIdentityConflict(
				ctx, conflicts, principal, request.Name, request.DisplayID,
			); err != nil {
				return nil, err
			}
			input := clientCreateInput{
				ClientID:  strings.TrimSpace(newID()),
				DisplayID: request.DisplayID,
				Name:      request.Name,
			}
			if input.ClientID == "" {
				return nil, aiassist.ErrInvalidTool
			}
			prepared, err := json.Marshal(input)
			if err != nil {
				return nil, aiassist.ErrInvalidTool
			}
			return prepared, nil
		},
		validate: func(raw json.RawMessage) error {
			_, err := validInput(raw)
			return err
		},
		resolve: func(
			principal authorization.Principal,
			_ json.RawMessage,
		) (scope.Target, error) {
			if strings.TrimSpace(principal.Scope.MSPID) == "" ||
				strings.TrimSpace(principal.Scope.ClientID) != "" {
				return scope.Target{}, aiassist.ErrInvalidTool
			}
			return scope.Target{MSPID: principal.Scope.MSPID}, nil
		},
		preview: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
		) (aiassist.Preview, error) {
			input, err := validInput(raw)
			if err != nil ||
				strings.TrimSpace(principal.Scope.MSPID) == "" ||
				strings.TrimSpace(principal.Scope.ClientID) != "" {
				return aiassist.Preview{}, aiassist.ErrInvalidTool
			}
			if err := rejectClientIdentityConflict(
				ctx, conflicts, principal, input.Name, input.DisplayID,
			); err != nil {
				return aiassist.Preview{}, err
			}
			return aiassist.Preview{
				Summary:    "Create client " + input.Name,
				TargetType: "client", TargetID: input.ClientID,
				TargetVersion: 0,
				Changes: map[string]aiassist.Change{
					"name":            {Before: nil, After: input.Name},
					"display_id":      {Before: nil, After: input.DisplayID},
					"lifecycle_state": {Before: nil, After: "active"},
				},
			}, nil
		},
		execute: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
			correlationID string,
		) (aiassist.ToolResult, error) {
			input, err := validInput(raw)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			created, err := creator.CreateClient(
				ctx,
				organizations.CreateClientCommand{
					Principal: principal,
					Actor: organizations.Actor{
						Type: "technician", ID: principal.ID, Source: "ai_workspace",
					},
					ClientID: input.ClientID, CorrelationID: correlationID,
					DisplayID: input.DisplayID, Name: input.Name,
				},
			)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{
				Summary: "Created client " + created.DisplayID,
				Data: map[string]any{
					"client_id": created.ID, "display_id": created.DisplayID,
					"name": created.Name, "lifecycle_state": created.LifecycleState,
				},
			}, nil
		},
	}
}

func rejectClientIdentityConflict(
	ctx context.Context,
	conflicts ClientIdentityConflictChecker,
	principal authorization.Principal,
	name string,
	displayID string,
) error {
	conflict, err := conflicts.HasClientIdentityConflict(
		ctx, principal, name, displayID,
	)
	if err != nil {
		return err
	}
	if conflict {
		return aiassist.ErrInvalidTool
	}
	return nil
}

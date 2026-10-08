package rtitools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/integrationhealth"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type ProductKnowledgeActions interface {
	Search(context.Context, string, aiassist.ProductAudience, int) ([]aiassist.Citation, error)
}

type IntegrationHealthActions interface {
	Snapshot(context.Context, integrationhealth.SnapshotCommand) (integrationhealth.Snapshot, error)
}

type productHelpInput struct {
	Query    string                   `json:"query"`
	Audience aiassist.ProductAudience `json:"audience"`
}

func NewProductHelpTool(actions ProductKnowledgeActions) aiassist.Tool {
	return &functionalTool{
		name: "product.help", capability: "ai.assist", kind: aiassist.ToolRead,
		validate: func(raw json.RawMessage) error {
			input, err := decode[productHelpInput](raw)
			if err != nil || strings.TrimSpace(input.Query) == "" ||
				(input.Audience != aiassist.ProductAudienceAllUsers &&
					input.Audience != aiassist.ProductAudienceAdministrators) {
				return aiassist.ErrInvalidTool
			}
			return nil
		},
		resolve: func(principal authorization.Principal, _ json.RawMessage) (scope.Target, error) {
			return scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID}, nil
		},
		preview: noPreview,
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, _ string) (aiassist.ToolResult, error) {
			input, _ := decode[productHelpInput](raw)
			audience := aiassist.ProductAudienceAllUsers
			if input.Audience == aiassist.ProductAudienceAdministrators &&
				principal.Capabilities.Has("setup.manage") {
				audience = aiassist.ProductAudienceAdministrators
			}
			citations, err := actions.Search(ctx, strings.TrimSpace(input.Query), audience, 8)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{
				Summary: "Found relevant RTI product guidance",
				Data:    map[string]any{"citations": citations},
			}, nil
		},
	}
}

type navigationInput struct {
	Intent string `json:"intent"`
}

type NavigationEntry struct {
	Label      string `json:"label"`
	Route      string `json:"route"`
	Capability string `json:"-"`
}

var navigationCatalog = []NavigationEntry{
	{Label: "Dashboard", Route: "/", Capability: "work_record.read"},
	{Label: "Work", Route: "/work", Capability: "work_record.read"},
	{Label: "Timesheet", Route: "/timesheet", Capability: "timesheet.read_own"},
	{Label: "Directory", Route: "/directory", Capability: "directory.read"},
	{Label: "Projects", Route: "/projects", Capability: "project.read"},
	{Label: "Sales", Route: "/sales", Capability: "sales.read"},
	{Label: "Setup Center", Route: "/setup", Capability: "setup.manage"},
	{Label: "Integration Health", Route: "/integration-health", Capability: "integration.read"},
}

func NewNavigationTool() aiassist.Tool {
	return &functionalTool{
		name: "navigation.find", capability: "ai.assist", kind: aiassist.ToolRead,
		validate: validateAs[navigationInput],
		resolve: func(principal authorization.Principal, _ json.RawMessage) (scope.Target, error) {
			return scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID}, nil
		},
		preview: noPreview,
		execute: func(_ context.Context, principal authorization.Principal, raw json.RawMessage, _ string) (aiassist.ToolResult, error) {
			input, _ := decode[navigationInput](raw)
			intent := strings.ToLower(strings.TrimSpace(input.Intent))
			entries := make([]NavigationEntry, 0, len(navigationCatalog))
			for _, entry := range navigationCatalog {
				if !principal.Capabilities.Has(entry.Capability) {
					continue
				}
				if intent == "" || strings.Contains(strings.ToLower(entry.Label+" "+entry.Route), intent) {
					entries = append(entries, entry)
				}
			}
			return aiassist.ToolResult{
				Summary: "Found available RTI destinations",
				Data:    map[string]any{"destinations": entries},
			}, nil
		},
	}
}

type healthInput struct{}

func NewHealthTool(actions IntegrationHealthActions) aiassist.Tool {
	return &functionalTool{
		name: "system.health", capability: "integration.read", kind: aiassist.ToolRead,
		validate: validateAs[healthInput],
		resolve: func(principal authorization.Principal, _ json.RawMessage) (scope.Target, error) {
			return scope.Target{MSPID: principal.Scope.MSPID}, nil
		},
		preview: noPreview,
		execute: func(ctx context.Context, principal authorization.Principal, _ json.RawMessage, _ string) (aiassist.ToolResult, error) {
			snapshot, err := actions.Snapshot(ctx, integrationhealth.SnapshotCommand{Principal: principal})
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			connections := make([]map[string]any, 0, len(snapshot.Connections))
			for _, connection := range snapshot.Connections {
				connections = append(connections, map[string]any{
					"kind": connection.Kind, "state": connection.State,
					"reason_code": connection.Reason,
				})
			}
			return aiassist.ToolResult{
				Summary: "RTI integration health is " + string(snapshot.State),
				Data:    map[string]any{"state": snapshot.State, "connections": connections},
			}, nil
		},
	}
}

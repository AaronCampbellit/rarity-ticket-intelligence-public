package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/commitments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type milestoneActionsStub struct {
	create projects.CreateMilestoneCommand
	update projects.UpdateMilestoneCommand
}

func (s *milestoneActionsStub) Create(_ context.Context, command projects.CreateMilestoneCommand) (projects.Milestone, error) {
	s.create = command
	return projects.Milestone{ID: "milestone", Version: 1}, nil
}
func (s *milestoneActionsStub) Update(_ context.Context, command projects.UpdateMilestoneCommand) (projects.Milestone, error) {
	s.update = command
	return projects.Milestone{ID: command.Milestone.ID, Version: command.ExpectedVersion + 1}, nil
}
func (s *milestoneActionsStub) Transition(context.Context, projects.TransitionMilestoneCommand) (projects.Milestone, error) {
	return projects.Milestone{}, nil
}

type maintenanceActionsStub struct {
	create commitments.CreateMaintenanceCommand
}

func (s *maintenanceActionsStub) Create(_ context.Context, command commitments.CreateMaintenanceCommand) (commitments.MaintenanceWindow, error) {
	s.create = command
	return commitments.MaintenanceWindow{ID: "window", Version: 1}, nil
}
func (s *maintenanceActionsStub) Update(context.Context, commitments.UpdateMaintenanceCommand) (commitments.MaintenanceWindow, error) {
	return commitments.MaintenanceWindow{}, nil
}
func (s *maintenanceActionsStub) Transition(context.Context, commitments.TransitionMaintenanceCommand) (commitments.MaintenanceWindow, error) {
	return commitments.MaintenanceWindow{}, nil
}

func TestMilestoneCreateUsesProjectPathAndTrustedClientScope(t *testing.T) {
	actions := &milestoneActionsStub{}
	principal := authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp", ClientID: "client"}, Capabilities: authorization.NewCapabilitySet("project.edit")}
	router := NewRouter(Dependencies{Principal: func(*http.Request) (authorization.Principal, error) { return principal, nil }, ProjectMilestones: actions})
	response := performCalendarJSON(router, http.MethodPost, "/api/v1/projects/project-1/milestones", `{"name":"Launch","due_on":"2026-09-01","all_day":true,"starts_on":"2026-09-01","idempotency_key":"create"}`)
	if response.Code != http.StatusCreated || actions.create.ProjectID != "project-1" || actions.create.Principal.Scope.ClientID != "client" || actions.create.ActorID != "tech" || actions.create.Source != "http" {
		t.Fatalf("status=%d command=%+v body=%s", response.Code, actions.create, response.Body.String())
	}
}

func TestMaintenanceCreateRejectsMixedDateAndTimestampIntervals(t *testing.T) {
	actions := &maintenanceActionsStub{}
	principal := authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp"}, Capabilities: authorization.NewCapabilitySet("calendar.commitment.manage")}
	router := NewRouter(Dependencies{Principal: func(*http.Request) (authorization.Principal, error) { return principal, nil }, MaintenanceWindows: actions})
	response := performCalendarJSON(router, http.MethodPost, "/api/v1/maintenance-windows", `{"title":"Upgrade","all_day":true,"starts_on":"2026-09-01","starts_at":"2026-09-01T02:00:00Z","ends_at":"2026-09-01T03:00:00Z","timezone":"UTC","protected":true,"conflict_policy":"hard_block","idempotency_key":"create","scopes":[{"type":"client","id":"client"}]}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

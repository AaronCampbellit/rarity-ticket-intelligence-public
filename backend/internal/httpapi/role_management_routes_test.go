package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type roleManagementActionsStub struct {
	principal authorization.Principal
	roleID    string
	version   int64
	caps      []string
	reason    string
	create    authorization.CreateRoleCommand
	assign    authorization.AssignRoleCommand
}

func (s *roleManagementActionsStub) Create(
	_ context.Context,
	principal authorization.Principal,
	command authorization.CreateRoleCommand,
) (authorization.Role, error) {
	s.principal, s.create = principal, command
	return authorization.Role{
		ID: "new-role", Key: command.Key, Name: command.Name,
		Capabilities: command.Capabilities, Version: 1,
	}, nil
}

func (s *roleManagementActionsStub) Assign(
	_ context.Context,
	principal authorization.Principal,
	command authorization.AssignRoleCommand,
) (authorization.RoleAssignment, error) {
	s.principal, s.assign = principal, command
	return authorization.RoleAssignment{
		ID: "assignment", MSPID: principal.Scope.MSPID,
		ClientID: command.ClientID, TechnicianID: command.PrincipalID,
		RoleID: "review-role", RoleKey: command.RoleKey,
		GrantedBy: principal.ID,
	}, nil
}

func (s *roleManagementActionsStub) List(
	_ context.Context,
	principal authorization.Principal,
) ([]authorization.Role, error) {
	s.principal = principal
	return []authorization.Role{{
		ID: "role-id", Key: "technician", Name: "Technician",
		Capabilities: []string{"work_record.read"}, Version: 2,
	}}, nil
}

func (s *roleManagementActionsStub) ReplaceCapabilities(
	_ context.Context,
	principal authorization.Principal,
	roleID string,
	version int64,
	caps []string,
	reason string,
) (authorization.Role, error) {
	s.principal, s.roleID, s.version = principal, roleID, version
	s.caps, s.reason = caps, reason
	return authorization.Role{
		ID: roleID, Key: "technician", Name: "Technician",
		Capabilities: caps, Version: version + 1,
	}, nil
}

func TestRoleManagementRoutesUseTrustedPrincipalAndVersionedRequest(t *testing.T) {
	actions := &roleManagementActionsStub{}
	principal := authorization.Principal{
		ID: "admin-id", Scope: scope.Principal{MSPID: "msp-id"},
		Capabilities: authorization.NewCapabilitySet("role.manage"),
	}
	handler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return principal, nil
		},
		RoleManagement: actions,
	})
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/admin/roles", nil))
	if list.Code != http.StatusOK || actions.principal.ID != "admin-id" {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}

	update := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPut, "/api/v1/admin/roles/role-id/capabilities",
		bytes.NewBufferString(`{
			"expected_version":2,
			"capabilities":["work_record.read","work_record.update"],
			"reason":"Align access"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(update, request)
	if update.Code != http.StatusOK || actions.roleID != "role-id" ||
		actions.version != 2 || actions.reason != "Align access" ||
		update.Header().Get("ETag") != `"3"` {
		t.Fatalf(
			"status=%d body=%s actions=%+v etag=%q",
			update.Code, update.Body.String(), actions, update.Header().Get("ETag"),
		)
	}
}

func TestRoleManagementRoutesCreateAndAssignWithTrustedActor(t *testing.T) {
	actions := &roleManagementActionsStub{}
	principal := authorization.Principal{
		ID: "admin-id", Scope: scope.Principal{MSPID: "msp-id"},
		Capabilities: authorization.NewCapabilitySet("role.manage"),
	}
	handler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return principal, nil
		},
		RoleManagement: actions,
	})

	created := httptest.NewRecorder()
	handler.ServeHTTP(created, httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/roles",
		bytes.NewBufferString(`{
			"key":"service_lead",
			"name":"Service lead",
			"capabilities":["timesheet.review"],
			"reason":"Delegate review"
		}`),
	))
	if created.Code != http.StatusCreated ||
		actions.create.ActorID != "admin-id" ||
		actions.create.Source != "api" ||
		actions.create.Key != "service_lead" {
		t.Fatalf(
			"create status=%d body=%s command=%+v",
			created.Code,
			created.Body.String(),
			actions.create,
		)
	}

	assigned := httptest.NewRecorder()
	handler.ServeHTTP(assigned, httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/role-assignments",
		bytes.NewBufferString(`{
			"principal_id":"reviewer",
			"role_key":"time_reviewer",
			"client_id":"client-1",
			"reason":"Review client time"
		}`),
	))
	if assigned.Code != http.StatusCreated ||
		actions.assign.ActorID != "admin-id" ||
		actions.assign.ClientID != "client-1" ||
		actions.assign.RoleKey != "time_reviewer" {
		t.Fatalf(
			"assign status=%d body=%s command=%+v",
			assigned.Code,
			assigned.Body.String(),
			actions.assign,
		)
	}
}

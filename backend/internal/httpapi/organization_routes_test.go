package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type organizationActionsStub struct {
	client organizations.CreateClientCommand
	calls  int
	err    error
}

func (a *organizationActionsStub) CreateClient(
	_ context.Context,
	command organizations.CreateClientCommand,
) (organizations.Client, error) {
	a.calls++
	a.client = command
	return organizations.Client{}, a.err
}

type directoryActionsStub struct {
	department  organizations.CreateDepartmentCommand
	team        organizations.CreateTeamCommand
	queue       organizations.CreateQueueCommand
	membership  organizations.ReplaceTeamMembersCommand
	members     organizations.Team
	membersErr  error
	memberCalls int
	list        organizations.ListDirectoryCommand
	directory   organizations.Directory
}

func (a *directoryActionsStub) CreateDepartment(
	_ context.Context,
	command organizations.CreateDepartmentCommand,
) (organizations.Department, error) {
	a.department = command
	return organizations.Department{ID: "department-id", Version: 1}, nil
}

func (a *directoryActionsStub) CreateTeam(
	_ context.Context,
	command organizations.CreateTeamCommand,
) (organizations.Team, error) {
	a.team = command
	return organizations.Team{ID: "team-id", Version: 1}, nil
}

func (a *directoryActionsStub) CreateQueue(
	_ context.Context,
	command organizations.CreateQueueCommand,
) (organizations.Queue, error) {
	a.queue = command
	return organizations.Queue{ID: "queue-id", Version: 1}, nil
}

func (a *directoryActionsStub) ReplaceTeamMembers(
	_ context.Context,
	command organizations.ReplaceTeamMembersCommand,
) (organizations.Team, error) {
	a.memberCalls++
	a.membership = command
	return a.members, a.membersErr
}

func (a *directoryActionsStub) List(
	_ context.Context,
	command organizations.ListDirectoryCommand,
) (organizations.Directory, error) {
	a.list = command
	return a.directory, nil
}

func TestOrganizationRoutesMapTrustedActorAndHierarchy(t *testing.T) {
	organizationActions := &organizationActionsStub{}
	directoryActions := &directoryActionsStub{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, Organizations: organizationActions,
		Directory: directoryActions,
	})

	tests := []struct {
		path string
		body string
		want func(*testing.T)
	}{
		{
			path: "/api/v1/admin/clients",
			body: `{"display_id":"CLIENT-1","name":"Northwind"}`,
			want: func(t *testing.T) {
				if organizationActions.client.Actor.ID != "actor-id" ||
					organizationActions.client.Actor.Source != "api" ||
					organizationActions.client.DisplayID != "CLIENT-1" {
					t.Fatalf("unexpected client command: %+v", organizationActions.client)
				}
			},
		},
		{
			path: "/api/v1/admin/departments",
			body: `{"key":"service-desk","name":"Service Desk","reason":"initial structure"}`,
			want: func(t *testing.T) {
				if directoryActions.department.Principal.ID != "actor-id" ||
					directoryActions.department.Key != "service-desk" ||
					directoryActions.department.Source != "api" {
					t.Fatalf("unexpected department command: %+v", directoryActions.department)
				}
			},
		},
		{
			path: "/api/v1/admin/teams",
			body: `{"department_id":"department-id","key":"tier-one","name":"Tier One","reason":"initial structure"}`,
			want: func(t *testing.T) {
				if directoryActions.team.DepartmentID != "department-id" {
					t.Fatalf("unexpected team command: %+v", directoryActions.team)
				}
			},
		},
		{
			path: "/api/v1/admin/queues",
			body: `{"client_id":"client-id","department_id":"department-id","team_id":"team-id","key":"triage","name":"Triage","reason":"initial structure"}`,
			want: func(t *testing.T) {
				if directoryActions.queue.ClientID != "client-id" ||
					directoryActions.queue.TeamID != "team-id" {
					t.Fatalf("unexpected queue command: %+v", directoryActions.queue)
				}
			},
		},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		handler.ServeHTTP(
			response,
			httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body)),
		)
		if response.Code != http.StatusCreated {
			t.Fatalf("%s status=%d body=%s", test.path, response.Code, response.Body.String())
		}
		test.want(t)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/directory", nil),
	)
	if response.Code != http.StatusOK ||
		directoryActions.list.Principal.Scope.ClientID != "client-id" {
		t.Fatalf("directory status=%d command=%+v body=%s", response.Code, directoryActions.list, response.Body.String())
	}
}

func TestCreateClientRouteRejectsPreparedIdentityFields(t *testing.T) {
	actions := &organizationActionsStub{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, Organizations: actions,
	})

	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodPost,
			"/api/v1/admin/clients",
			bytes.NewBufferString(`{"display_id":"CLIENT-2","name":"Contoso","client_id":"019fb3c2-0000-7000-8000-000000000301","correlation_id":"019fb3c2-0000-7000-8000-000000000302"}`),
		),
	)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown identity fields status=%d body=%s", response.Code, response.Body.String())
	}
	if actions.calls != 0 {
		t.Fatalf("rejected request reached organization service %d times", actions.calls)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodPost,
			"/api/v1/admin/clients",
			bytes.NewBufferString(`{"display_id":"CLIENT-2","name":"Contoso"}`),
		),
	)
	if response.Code != http.StatusCreated {
		t.Fatalf("valid create status=%d body=%s", response.Code, response.Body.String())
	}
	if actions.calls != 1 {
		t.Fatalf("valid request reached organization service %d times, want 1", actions.calls)
	}
	if actions.client.ClientID != "" || actions.client.CorrelationID != "" {
		t.Fatalf("public request set trusted identities: %+v", actions.client)
	}
}

func TestCreateClientRouteReturnsSafeIdentityConflict(t *testing.T) {
	actions := &organizationActionsStub{err: organizations.ErrClientIdentityConflict}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, Organizations: actions,
	})

	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodPost,
			"/api/v1/admin/clients",
			bytes.NewBufferString(`{"display_id":"CLIENT-2","name":"Contoso"}`),
		),
	)
	if response.Code != http.StatusConflict {
		t.Fatalf("identity conflict status=%d body=%s", response.Code, response.Body.String())
	}
	var body ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "client_identity_conflict" ||
		body.Error.Message != "client name or display ID already exists" {
		t.Fatalf("identity conflict response=%+v", body)
	}
}

func TestReplaceTeamMembersRouteUsesStrictVersionedSnapshot(t *testing.T) {
	actions := &directoryActionsStub{members: organizations.Team{
		ID: testTeamUUID, MSPID: "msp-id", Version: 5,
		MemberIDs: []string{testTargetUUID, testMemberUUID},
	}}
	handler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{
				ID: "actor-id", Scope: scope.Principal{MSPID: "msp-id"},
				Capabilities: authorization.NewCapabilitySet("organization.manage"),
			}, nil
		},
		Directory: actions,
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPut, "/api/v1/admin/teams/"+testTeamUUID+"/members",
		bytes.NewBufferString(`{"technician_ids":["`+testMemberUUID+`","`+testTargetUUID+`"],"expected_version":4,"reason":"Staffing change"}`),
	))
	if response.Code != http.StatusOK || response.Header().Get("ETag") != `"5"` {
		t.Fatalf("status=%d etag=%q body=%s", response.Code, response.Header().Get("ETag"), response.Body.String())
	}
	if actions.memberCalls != 1 || actions.membership.TeamID != testTeamUUID ||
		actions.membership.ExpectedVersion != 4 || actions.membership.Reason != "Staffing change" ||
		actions.membership.Source != "api" || actions.membership.Principal.Scope.ClientID != "" {
		t.Fatalf("membership command=%+v calls=%d", actions.membership, actions.memberCalls)
	}
	if !strings.Contains(response.Body.String(), `"member_ids":["`+testTargetUUID+`","`+testMemberUUID+`"]`) {
		t.Fatalf("response body=%s", response.Body.String())
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPut, "/api/v1/admin/teams/"+testTeamUUID+"/members",
		bytes.NewBufferString(`{"technician_ids":[],"expected_version":5,"reason":"Clear team"}`),
	))
	if response.Code != http.StatusOK || actions.memberCalls != 2 ||
		actions.membership.TechnicianIDs == nil || len(actions.membership.TechnicianIDs) != 0 {
		t.Fatalf("explicit empty status=%d command=%+v calls=%d body=%s", response.Code, actions.membership, actions.memberCalls, response.Body.String())
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPut, "/api/v1/admin/teams/"+testTeamUUID+"/members",
		bytes.NewBufferString(`{"technician_ids":[],"expected_version":5,"reason":"Clear team","role_ids":["role-1"]}`),
	))
	if response.Code != http.StatusUnprocessableEntity || actions.memberCalls != 2 {
		t.Fatalf("unknown field status=%d calls=%d body=%s", response.Code, actions.memberCalls, response.Body.String())
	}
}

func TestReplaceTeamMembersRouteRejectsMissingNullAndNonArraySnapshotsWithoutMutation(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "omitted", body: `{"expected_version":5,"reason":"Clear team"}`},
		{name: "null", body: `{"technician_ids":null,"expected_version":5,"reason":"Clear team"}`},
		{name: "non-array", body: `{"technician_ids":"tech-1","expected_version":5,"reason":"Clear team"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			actions := &directoryActionsStub{}
			handler := NewRouter(Dependencies{
				Principal: func(*http.Request) (authorization.Principal, error) {
					return authorization.Principal{
						ID: "actor-id", Scope: scope.Principal{MSPID: "msp-id"},
						Capabilities: authorization.NewCapabilitySet("organization.manage"),
					}, nil
				},
				Directory: actions,
			})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(
				http.MethodPut, "/api/v1/admin/teams/team-id/members",
				bytes.NewBufferString(test.body),
			))
			if response.Code != http.StatusUnprocessableEntity ||
				!strings.Contains(response.Body.String(), `"code":"validation_failed"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if actions.memberCalls != 0 {
				t.Fatalf("membership mutation calls=%d command=%+v", actions.memberCalls, actions.membership)
			}
		})
	}
}

func TestReplaceTeamMembersRejectsMalformedTeamAndMemberUUIDsBeforeMutation(t *testing.T) {
	for _, test := range []struct {
		name, teamID, body string
	}{
		{name: "team", teamID: "not-a-uuid", body: `{"technician_ids":[],"expected_version":1,"reason":"test"}`},
		{name: "member", teamID: testTeamUUID, body: `{"technician_ids":["not-a-uuid"],"expected_version":1,"reason":"test"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			actions := &directoryActionsStub{}
			handler := NewRouter(Dependencies{
				Principal: func(*http.Request) (authorization.Principal, error) {
					return authorization.Principal{ID: "actor", Scope: scope.Principal{MSPID: "msp"}, Capabilities: authorization.NewCapabilitySet("organization.manage")}, nil
				},
				Directory: actions,
			})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/admin/teams/"+test.teamID+"/members", bytes.NewBufferString(test.body)))
			if response.Code != http.StatusUnprocessableEntity || actions.memberCalls != 0 || !strings.Contains(response.Body.String(), `"code":"validation_failed"`) {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, actions.memberCalls, response.Body.String())
			}
		})
	}
}

func TestReplaceTeamMembersRouteMapsSafeConflictAndScopeErrors(t *testing.T) {
	for _, test := range []struct {
		name      string
		err       error
		status    int
		code      string
		principal authorization.Principal
	}{
		{
			name: "stale version", err: object.ErrVersionConflict,
			status: http.StatusConflict, code: "version_conflict",
			principal: authorization.Principal{ID: "actor-id", Scope: scope.Principal{MSPID: "msp-id"}},
		},
		{
			name: "client scoped", err: organizations.ErrForbidden,
			status: http.StatusForbidden, code: "forbidden",
			principal: authorization.Principal{ID: "actor-id", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"}},
		},
		{
			name: "inactive member", err: scope.ErrNotFound,
			status: http.StatusNotFound, code: "not_found",
			principal: authorization.Principal{ID: "actor-id", Scope: scope.Principal{MSPID: "msp-id"}},
		},
		{
			name: "unknown team", err: scope.ErrNotFound,
			status: http.StatusNotFound, code: "not_found",
			principal: authorization.Principal{ID: "actor-id", Scope: scope.Principal{MSPID: "msp-id"}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			actions := &directoryActionsStub{membersErr: test.err}
			test.principal.Capabilities = authorization.NewCapabilitySet("organization.manage")
			handler := NewRouter(Dependencies{
				Principal: func(*http.Request) (authorization.Principal, error) { return test.principal, nil },
				Directory: actions,
			})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(
				http.MethodPut, "/api/v1/admin/teams/"+testTeamUUID+"/members",
				bytes.NewBufferString(`{"technician_ids":["`+testMemberUUID+`"],"expected_version":1,"reason":"Staffing change"}`),
			))
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if (test.status == http.StatusNotFound || test.status == http.StatusForbidden) &&
				(strings.Contains(response.Body.String(), testTeamUUID) || strings.Contains(response.Body.String(), testMemberUUID)) {
				t.Fatalf("scope error disclosed identifiers: %s", response.Body.String())
			}
		})
	}
}

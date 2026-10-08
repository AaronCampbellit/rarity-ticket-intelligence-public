package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/timeentries"
)

type timerActionsStub struct {
	start   timeentries.StartTimerCommand
	stop    timeentries.StopTimerCommand
	discard timeentries.DiscardTimerCommand
	listed  struct {
		principal    authorization.Principal
		workRecordID string
	}
}

type captureActionsStub struct {
	command timeentries.CaptureCommand
}

type laborRoleActionsStub struct {
	principal authorization.Principal
	create    timeentries.CreateLaborRoleCommand
	version   timeentries.VersionLaborRoleCommand
}

type timesheetActionsStub struct {
	listCommand    timeentries.ListWeekCommand
	getCommand     timeentries.GetCommand
	amendCommand   timeentries.AmendCommand
	reverseCommand timeentries.ReverseCommand
}

func (s *timesheetActionsStub) Get(_ context.Context, command timeentries.GetCommand) (timeentries.TimesheetRow, error) {
	s.getCommand = command
	return timeentries.TimesheetRow{Entry: timeentries.Entry{ID: command.EntryID, MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID}}, nil
}

func (s *laborRoleActionsStub) Create(
	_ context.Context,
	command timeentries.CreateLaborRoleCommand,
) (timeentries.LaborRole, error) {
	s.create = command
	return timeentries.LaborRole{
		ID: "role", Key: command.Key, Version: 1,
		CurrentVersion: timeentries.LaborRoleVersion{
			ID: "role-version", Name: command.Name,
			InternalCostMinor: command.InternalCostMinor,
			BillRateMinor:     command.BillRateMinor,
			Currency:          command.Currency, Enabled: true,
		},
	}, nil
}

func (s *laborRoleActionsStub) Version(
	_ context.Context,
	command timeentries.VersionLaborRoleCommand,
) (timeentries.LaborRole, error) {
	s.version = command
	return timeentries.LaborRole{
		ID: command.LaborRoleID, Version: command.ExpectedVersion + 1,
		CurrentVersion: timeentries.LaborRoleVersion{
			ID: "new-version", Name: command.Name,
			Enabled: command.Enabled,
		},
	}, nil
}

func (s *timesheetActionsStub) ListWeek(
	_ context.Context,
	command timeentries.ListWeekCommand,
) (timeentries.Timesheet, error) {
	s.listCommand = command
	return timeentries.Timesheet{
		TechnicianID: command.TechnicianID,
		Rows: []timeentries.TimesheetRow{{
			Entry: timeentries.Entry{
				ID: "entry", DurationSeconds: 3600, Version: 2,
			},
			WorkItemTitle: "VPN unavailable", ApprovalState: "pending",
		}},
		TotalSeconds: 3600,
	}, nil
}

func (s *timesheetActionsStub) Amend(
	_ context.Context,
	command timeentries.AmendCommand,
) (timeentries.TimesheetRow, error) {
	s.amendCommand = command
	return timeentries.TimesheetRow{
		Entry:         timeentries.Entry{ID: command.EntryID, Version: 3},
		ApprovalState: "pending",
	}, nil
}

func (s *timesheetActionsStub) ReverseAndReplace(
	_ context.Context,
	command timeentries.ReverseCommand,
) (timeentries.ReversalResult, error) {
	s.reverseCommand = command
	return timeentries.ReversalResult{
		Original: timeentries.TimesheetRow{
			Entry:         timeentries.Entry{ID: command.EntryID, Version: 3},
			ApprovalState: "approved",
		},
		Replacement: timeentries.TimesheetRow{
			Entry:         timeentries.Entry{ID: "replacement", Version: 1},
			ApprovalState: "pending",
		},
	}, nil
}

func (s *laborRoleActionsStub) List(
	_ context.Context,
	principal authorization.Principal,
) ([]timeentries.LaborRole, error) {
	s.principal = principal
	return []timeentries.LaborRole{{
		ID: "role-1", MSPID: "msp", Key: "service-desk", Version: 1,
		CurrentVersion: timeentries.LaborRoleVersion{
			ID: "role-version-1", LaborRoleID: "role-1",
			Name: "Service Desk", Currency: "USD", Enabled: true,
		},
	}}, nil
}

func (s *laborRoleActionsStub) ListForManagement(
	ctx context.Context,
	principal authorization.Principal,
) ([]timeentries.LaborRole, error) {
	return s.List(ctx, principal)
}

func (s *captureActionsStub) Create(
	_ context.Context,
	command timeentries.CaptureCommand,
) (timeentries.Entry, error) {
	s.command = command
	return timeentries.Entry{
		ID: "entry", MSPID: "msp", ClientID: "client",
		WorkRecordID: "ticket-1", TechnicianID: "technician",
		DurationSeconds: 900, LaborRoleVersionID: "role-version",
		Version: 1,
	}, nil
}

func (s *timerActionsStub) Start(
	_ context.Context,
	command timeentries.StartTimerCommand,
) (timeentries.TimerSession, error) {
	s.start = command
	return timerResponse("timer-1", timeentries.TimerRunning, 1), nil
}

func (s *timerActionsStub) List(
	_ context.Context,
	principal authorization.Principal,
	workRecordID string,
) ([]timeentries.TimerSession, error) {
	s.listed.principal = principal
	s.listed.workRecordID = workRecordID
	return []timeentries.TimerSession{
		timerResponse("timer-1", timeentries.TimerRunning, 1),
	}, nil
}

func (s *timerActionsStub) Stop(
	_ context.Context,
	command timeentries.StopTimerCommand,
) (timeentries.TimerSession, error) {
	s.stop = command
	return timerResponse("timer-1", timeentries.TimerStopped, 2), nil
}

func (s *timerActionsStub) Discard(
	_ context.Context,
	command timeentries.DiscardTimerCommand,
) (timeentries.TimerSession, error) {
	s.discard = command
	return timerResponse("timer-1", timeentries.TimerDiscarded, 3), nil
}

func TestTimeWorkforceRoutesUseTrustedPrincipalAndVersionedCommands(t *testing.T) {
	actions := &timerActionsStub{}
	principal := authorization.Principal{
		ID: "technician", Scope: scope.Principal{
			MSPID: "msp", ClientID: "client",
		},
		Capabilities: authorization.NewCapabilitySet("time_entry.create"),
	}
	handler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return principal, nil
		},
		Timers: actions,
	})

	start := httptest.NewRecorder()
	handler.ServeHTTP(start, httptest.NewRequest(
		http.MethodPost,
		"/api/v1/work-records/ticket-1/timers",
		bytes.NewBufferString(`{"idempotency_key":"start-key"}`),
	))
	if start.Code != http.StatusCreated ||
		actions.start.Principal.ID != "technician" ||
		actions.start.WorkRecordID != "ticket-1" ||
		actions.start.ActorID != "technician" ||
		actions.start.IdempotencyKey != "start-key" ||
		start.Header().Get("ETag") != `"1"` {
		t.Fatalf(
			"start status=%d body=%s command=%+v etag=%q",
			start.Code,
			start.Body.String(),
			actions.start,
			start.Header().Get("ETag"),
		)
	}

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(
		http.MethodGet,
		"/api/v1/work-records/ticket-1/timers",
		nil,
	))
	if list.Code != http.StatusOK ||
		actions.listed.principal.ID != "technician" ||
		actions.listed.workRecordID != "ticket-1" {
		t.Fatalf(
			"list status=%d body=%s listed=%+v",
			list.Code,
			list.Body.String(),
			actions.listed,
		)
	}

	stop := httptest.NewRecorder()
	handler.ServeHTTP(stop, httptest.NewRequest(
		http.MethodPost,
		"/api/v1/timers/timer-1:stop",
		bytes.NewBufferString(`{"expected_version":1}`),
	))
	if stop.Code != http.StatusOK ||
		actions.stop.ID != "timer-1" ||
		actions.stop.ExpectedVersion != 1 ||
		actions.stop.ActorID != "technician" ||
		stop.Header().Get("ETag") != `"2"` {
		t.Fatalf(
			"stop status=%d body=%s command=%+v etag=%q",
			stop.Code,
			stop.Body.String(),
			actions.stop,
			stop.Header().Get("ETag"),
		)
	}
}

func TestTimeWorkforceStartRejectsBrowserDuration(t *testing.T) {
	handler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{
				ID: "technician",
				Scope: scope.Principal{
					MSPID: "msp", ClientID: "client",
				},
			}, nil
		},
		Timers: &timerActionsStub{},
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/api/v1/work-records/ticket-1/timers",
		bytes.NewBufferString(
			`{"idempotency_key":"start-key","duration_seconds":900}`,
		),
	))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTimeEntryRouteConsumesCaptureWithoutBrowserDurationOrRates(t *testing.T) {
	actions := &captureActionsStub{}
	handler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{
				ID: "technician",
				Scope: scope.Principal{
					MSPID: "msp", ClientID: "client",
				},
				Capabilities: authorization.NewCapabilitySet("time_entry.create"),
			}, nil
		},
		TimeCaptureEntries: actions,
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/api/v1/work-records/ticket-1/time-entries",
		bytes.NewBufferString(`{
			"time_capture":{
				"id":"capture-1",
				"expected_version":2,
				"labor_role_id":"role-1",
				"billable":true
			},
			"tag_ids":["tag-1"],
			"note":"Investigated"
		}`),
	))
	if response.Code != http.StatusCreated ||
		actions.command.CaptureID != "capture-1" ||
		actions.command.ExpectedCaptureVersion != 2 ||
		actions.command.LaborRoleID != "role-1" ||
		!actions.command.Billable ||
		actions.command.WorkRecordID != "ticket-1" ||
		actions.command.ActorID != "technician" ||
		len(actions.command.TagIDs) != 1 || actions.command.TagIDs[0] != "tag-1" {
		t.Fatalf(
			"status=%d body=%s command=%+v",
			response.Code,
			response.Body.String(),
			actions.command,
		)
	}
}

func TestLaborRoleRouteListsEffectiveRolesForTrustedPrincipal(t *testing.T) {
	actions := &laborRoleActionsStub{}
	handler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{
				ID: "technician",
				Scope: scope.Principal{
					MSPID: "msp", ClientID: "client",
				},
			}, nil
		},
		LaborRoles: actions,
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/api/v1/labor-roles",
		nil,
	))
	if response.Code != http.StatusOK ||
		actions.principal.ID != "technician" ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"name":"Service Desk"`)) {
		t.Fatalf(
			"status=%d body=%s principal=%+v",
			response.Code,
			response.Body.String(),
			actions.principal,
		)
	}
}

func TestLaborRoleManagementRoutesUseTrustedActorAndVersion(t *testing.T) {
	actions := &laborRoleActionsStub{}
	principal := authorization.Principal{
		ID: "admin", Scope: scope.Principal{MSPID: "msp"},
		Capabilities: authorization.NewCapabilitySet("organization.manage"),
	}
	handler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return principal, nil
		},
		LaborRoles: actions,
	})

	created := httptest.NewRecorder()
	handler.ServeHTTP(created, httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/labor-roles",
		bytes.NewBufferString(`{
			"key":"service_desk",
			"name":"Service Desk",
			"internal_cost_minor":3500,
			"bill_rate_minor":12500,
			"currency":"USD",
			"effective_from":"2026-08-04T00:00:00Z"
		}`),
	))
	if created.Code != http.StatusCreated ||
		actions.create.ActorID != "admin" ||
		actions.create.Source != "api" ||
		actions.create.Key != "service_desk" {
		t.Fatalf(
			"create status=%d body=%s command=%+v",
			created.Code,
			created.Body.String(),
			actions.create,
		)
	}

	versioned := httptest.NewRecorder()
	handler.ServeHTTP(versioned, httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/labor-roles/role/versions",
		bytes.NewBufferString(`{
			"expected_version":1,
			"name":"Service Desk",
			"internal_cost_minor":4000,
			"bill_rate_minor":13000,
			"currency":"USD",
			"effective_from":"2026-09-01T00:00:00Z",
			"enabled":false,
			"reason":"Retire role"
		}`),
	))
	if versioned.Code != http.StatusCreated ||
		actions.version.LaborRoleID != "role" ||
		actions.version.ExpectedVersion != 1 ||
		actions.version.ActorID != "admin" ||
		versioned.Header().Get("ETag") != `"2"` {
		t.Fatalf(
			"version status=%d body=%s command=%+v",
			versioned.Code,
			versioned.Body.String(),
			actions.version,
		)
	}
}

func TestTimesheetRoutesUseTrustedScopeAndVersionedAmendment(t *testing.T) {
	actions := &timesheetActionsStub{}
	principal := authorization.Principal{
		ID: "technician",
		Scope: scope.Principal{
			MSPID: "msp", ClientID: "client",
		},
		Capabilities: authorization.NewCapabilitySet(
			"timesheet.read_own",
			"time_entry.update_own",
		),
	}
	handler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return principal, nil
		},
		Timesheets: actions,
	})
	exact := httptest.NewRecorder()
	handler.ServeHTTP(exact, httptest.NewRequest(http.MethodGet, "/api/v1/time-entries/off-week-entry", nil))
	if exact.Code != http.StatusOK || actions.getCommand.EntryID != "off-week-entry" || actions.getCommand.Target.ClientID != "client" {
		t.Fatalf("exact status=%d command=%+v body=%s", exact.Code, actions.getCommand, exact.Body.String())
	}

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(
		http.MethodGet,
		"/api/v1/timesheets/week?anchor=2026-08-04T18:00:00Z",
		nil,
	))
	if list.Code != http.StatusOK ||
		actions.listCommand.Principal.ID != "technician" ||
		actions.listCommand.Target.ClientID != "client" ||
		!actions.listCommand.Anchor.Equal(
			time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC),
		) {
		t.Fatalf(
			"list status=%d body=%s command=%+v",
			list.Code,
			list.Body.String(),
			actions.listCommand,
		)
	}

	amend := httptest.NewRecorder()
	handler.ServeHTTP(amend, httptest.NewRequest(
		http.MethodPost,
		"/api/v1/time-entries/entry:amend",
		bytes.NewBufferString(`{
			"expected_version":2,
			"started_at":"2026-08-04T18:00:00Z",
			"ended_at":"2026-08-04T19:30:00Z",
			"billable":true,
			"note":"Corrected",
			"reason":"Correct duration"
		}`),
	))
	if amend.Code != http.StatusOK ||
		actions.amendCommand.EntryID != "entry" ||
		actions.amendCommand.ExpectedVersion != 2 ||
		actions.amendCommand.ActorID != "technician" ||
		actions.amendCommand.Target.ClientID != "client" ||
		amend.Header().Get("ETag") != `"3"` {
		t.Fatalf(
			"amend status=%d body=%s command=%+v",
			amend.Code,
			amend.Body.String(),
			actions.amendCommand,
		)
	}
}

func timerResponse(
	id string,
	state timeentries.TimerState,
	version int64,
) timeentries.TimerSession {
	startedAt := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	session := timeentries.TimerSession{
		ID: id, MSPID: "msp", ClientID: "client",
		WorkRecordID: "ticket-1", TechnicianID: "technician",
		State: state, StartedAt: startedAt,
		IdempotencyKey: "start-key", Version: version,
		CreatedAt: startedAt, UpdatedAt: startedAt,
	}
	if state != timeentries.TimerRunning {
		stoppedAt := startedAt.Add(15 * time.Minute)
		session.StoppedAt = &stoppedAt
		session.DurationSeconds = 900
		session.UpdatedAt = stoppedAt
	}
	return session
}

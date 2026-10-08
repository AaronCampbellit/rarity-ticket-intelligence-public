package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/customfields"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type calendarActionsStub struct {
	request calendar.QueryRequest
	live    calendar.LivePage
	preview calendar.PreviewCommand
	apply   calendar.ApplyCommand
	err     error
}

type calendarPreferenceActionsStub struct {
	command notifications.ReplaceCalendarPreferenceCommand
	err     error
}

func (s *calendarPreferenceActionsStub) Get(context.Context, authorization.Principal) (notifications.CalendarPreference, error) {
	return notifications.CalendarPreference{TechnicianID: "technician-id", Rules: []notifications.CalendarPreferenceRule{}, Version: 2}, nil
}

func (s *calendarPreferenceActionsStub) Replace(_ context.Context, command notifications.ReplaceCalendarPreferenceCommand) (notifications.CalendarPreference, error) {
	s.command = command
	return notifications.CalendarPreference{TechnicianID: command.Principal.ID, Rules: command.Rules, Version: command.ExpectedVersion + 1}, s.err
}

func TestCalendarPreferenceRouteMapsInvalidRules(t *testing.T) {
	actions := &calendarPreferenceActionsStub{err: notifications.ErrInvalidCalendarPreference}
	router := NewRouter(Dependencies{Principal: func(*http.Request) (authorization.Principal, error) {
		return calendarRoutePrincipal("calendar.read"), nil
	}, CalendarPreferences: actions})
	response := performCalendarJSON(router, http.MethodPut, "/api/v1/calendar/notification-preferences", `{"expected_version":2,"rules":[{"event_class":"surprise","change_class":"assigned","urgency":"normal","channel":"in_app","enabled":true}]}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

type customDateValueActionsStub struct {
	values []customfields.DateValue
	set    customfields.DateValue
}

func (s *customDateValueActionsStub) ListCustomDateValues(context.Context, authorization.Principal, customfields.ObjectType, string) ([]customfields.DateValue, error) {
	return s.values, nil
}

func (s *customDateValueActionsStub) Set(context.Context, customfields.SetDateCommand) (customfields.DateValue, error) {
	return s.set, nil
}

func (s *calendarActionsStub) List(_ context.Context, request calendar.QueryRequest) (calendar.QueryPage, error) {
	s.request = request
	return calendar.QueryPage{Events: []calendar.EventView{}}, s.err
}
func (s *calendarActionsStub) FilterOptions(_ context.Context, request calendar.QueryRequest) (calendar.FilterOptionCounts, error) {
	s.request = request
	return calendar.FilterOptionCounts{}, s.err
}
func (s *calendarActionsStub) Capacity(_ context.Context, request calendar.QueryRequest) (map[string]calendar.CapacitySummary, error) {
	s.request = request
	return map[string]calendar.CapacitySummary{}, s.err
}
func (s *calendarActionsStub) ListAfter(context.Context, calendar.LiveRequest) (calendar.LivePage, error) {
	return s.live, s.err
}
func (s *calendarActionsStub) Preview(_ context.Context, command calendar.PreviewCommand) (calendar.SchedulingProposal, error) {
	s.preview = command
	return calendar.SchedulingProposal{ID: "proposal-1", State: "previewed"}, s.err
}
func (s *calendarActionsStub) Apply(_ context.Context, command calendar.ApplyCommand) (calendar.AppliedProposal, error) {
	s.apply = command
	return calendar.AppliedProposal{ProposalID: command.ProposalID}, s.err
}

func calendarRoutePrincipal(capabilities ...string) authorization.Principal {
	return authorization.Principal{
		ID:           "technician-id",
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "active-client-id"},
		Capabilities: authorization.NewCapabilitySet(capabilities...),
	}
}

func calendarRouter(actions *calendarActionsStub, capabilities ...string) http.Handler {
	return NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return calendarRoutePrincipal(capabilities...), nil
		},
		CalendarQueries:   actions,
		CalendarLive:      actions,
		CalendarProposals: actions,
	})
}

func TestCalendarRoutesUsePrincipalAuthorizedClients(t *testing.T) {
	actions := &calendarActionsStub{}
	router := calendarRouter(actions, "calendar.read")
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/calendar/events?start=2026-08-01T00:00:00Z&end=2026-08-08T00:00:00Z", nil)
	request.Header.Set("X-Client-ID", "attacker-client")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if actions.request.Filter.ClientIDs != nil || actions.request.Principal.Scope.ClientID != "" {
		t.Fatalf("active client became hidden filter: %+v", actions.request)
	}
}

func TestCalendarPreferenceRoutesUseTrustedRecipientAndTypedRules(t *testing.T) {
	actions := &calendarPreferenceActionsStub{}
	principal := calendarRoutePrincipal("calendar.read")
	router := NewRouter(Dependencies{Principal: func(*http.Request) (authorization.Principal, error) { return principal, nil }, CalendarPreferences: actions})
	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/calendar/notification-preferences", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"technician_id":"technician-id"`) {
		t.Fatalf("GET status=%d body=%s", get.Code, get.Body.String())
	}
	put := performCalendarJSON(router, http.MethodPut, "/api/v1/calendar/notification-preferences", `{"expected_version":2,"rules":[{"event_class":"schedule_changed","change_class":"assigned","urgency":"normal","channel":"in_app","enabled":true}]}`)
	if put.Code != http.StatusOK || actions.command.Principal.ID != "technician-id" || actions.command.ExpectedVersion != 2 || len(actions.command.Rules) != 1 {
		t.Fatalf("PUT status=%d command=%+v body=%s", put.Code, actions.command, put.Body.String())
	}
}

func TestCalendarPreferenceRouteRejectsUnknownFields(t *testing.T) {
	actions := &calendarPreferenceActionsStub{}
	router := NewRouter(Dependencies{Principal: func(*http.Request) (authorization.Principal, error) {
		return calendarRoutePrincipal("calendar.read"), nil
	}, CalendarPreferences: actions})
	response := performCalendarJSON(router, http.MethodPut, "/api/v1/calendar/notification-preferences", `{"expected_version":2,"rules":[],"technician_id":"attacker"}`)
	if response.Code != http.StatusBadRequest || actions.command.Principal.ID != "" {
		t.Fatalf("status=%d command=%+v", response.Code, actions.command)
	}
}

func TestCalendarRoutesParseExplicitFiltersWithoutServerTimezone(t *testing.T) {
	actions := &calendarActionsStub{}
	router := calendarRouter(actions, "calendar.read")
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/calendar/events?start=2026-11-01T05:30:00-04:00&end=2026-11-01T07:30:00-05:00&client_ids=one,two&technician_ids=tech&tags=tag&conflicts_only=true&limit=40", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if actions.request.Window.Start.Location() != time.UTC || len(actions.request.Filter.ClientIDs) != 2 || actions.request.Limit != 40 || !actions.request.Filter.ConflictsOnly {
		t.Fatalf("request = %+v", actions.request)
	}
}

func TestCalendarRouteRejectsInvalidOrTooLargeWindowWithStableCodes(t *testing.T) {
	for _, test := range []struct{ path, code string }{
		{"/api/v1/calendar/events?start=2026-08-01&end=2026-08-02T00:00:00Z", "invalid_calendar_window"},
		{"/api/v1/calendar/events?start=2026-01-01T00:00:00Z&end=2027-01-03T00:00:00Z", "calendar_window_too_large"},
	} {
		response := httptest.NewRecorder()
		calendarRouter(&calendarActionsStub{}, "calendar.read").ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
			t.Fatalf("%s status=%d body=%s", test.path, response.Code, response.Body.String())
		}
	}
}

func TestApplyProposalRequiresExpectedProposalVersionAndReason(t *testing.T) {
	router := calendarRouter(&calendarActionsStub{}, "calendar.schedule")
	for _, body := range []string{
		`{"accepted_optional_change_ids":[],"reason":"approved"}`,
		`{"accepted_optional_change_ids":[],"expected_proposal_version":1}`,
	} {
		response := performCalendarJSON(router, http.MethodPost, "/api/v1/calendar/proposals/proposal-1/apply", body)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_calendar_proposal"`) {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
}

func TestApplyProposalUsesTrustedActorAndMapsStaleProposal(t *testing.T) {
	actions := &calendarActionsStub{err: calendar.ErrStaleProposal}
	router := calendarRouter(actions, "calendar.schedule")
	response := performCalendarJSON(router, http.MethodPost, "/api/v1/calendar/proposals/proposal-1/apply", `{"accepted_optional_change_ids":["change-1"],"expected_proposal_version":2,"reason":"approved cascade"}`)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"stale_calendar_proposal"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if actions.apply.Principal.ID != "technician-id" || actions.apply.ProposalID != "proposal-1" || actions.apply.OverrideReason != "approved cascade" || actions.apply.ExpectedProposalVersion != 2 {
		t.Fatalf("command=%+v", actions.apply)
	}
}

func TestPreviewRecurringProposalMapsMissingOccurrenceScopeToStableBadRequest(t *testing.T) {
	actions := &calendarActionsStub{err: calendar.ErrOccurrenceScopeRequired}
	response := performCalendarJSON(calendarRouter(actions, "calendar.schedule"), http.MethodPost, "/api/v1/calendar/proposals", `{"change":{"projection_id":"projection-1","all_day":true,"starts_on":"2026-08-12","ends_on":"2026-08-13","recurrence":{"frequency":"daily","interval":1}}}`)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_calendar_proposal"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestApplyProposalMapsUnknownOptionalChangeToStableBadRequest(t *testing.T) {
	actions := &calendarActionsStub{err: calendar.ErrInvalidOptionalChange}
	response := performCalendarJSON(calendarRouter(actions, "calendar.schedule"), http.MethodPost, "/api/v1/calendar/proposals/proposal-1/apply", `{"accepted_optional_change_ids":["unknown-change"],"expected_proposal_version":1,"reason":"approved"}`)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_calendar_proposal"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCalendarLiveRouteReturnsRefetchEventForExpiredCursor(t *testing.T) {
	actions := &calendarActionsStub{live: calendar.LivePage{Cursor: "fresh", RefetchRequired: true}}
	router := calendarRouter(actions, "calendar.read")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/calendar/live?cursor=expired", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream" || !strings.Contains(response.Body.String(), "event: refetch\n") || !strings.Contains(response.Body.String(), `"cursor":"fresh"`) {
		t.Fatalf("status=%d type=%q body=%q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
}

func TestCalendarDomainErrorsHaveStableHTTPMappings(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{
		{calendar.ErrHardSchedulingConflict, http.StatusConflict, "calendar_hard_conflict"},
		{calendar.ErrOverrideReasonRequired, http.StatusBadRequest, "calendar_reason_required"},
		{calendar.ErrCrossClientDependency, http.StatusUnprocessableEntity, "cross_client_dependency"},
		{calendar.ErrDependencyCycle, http.StatusConflict, "dependency_cycle"},
		{calendar.ErrReadOnlyEventRole, http.StatusConflict, "calendar_role_read_only"},
	} {
		actions := &calendarActionsStub{err: test.err}
		response := httptest.NewRecorder()
		calendarRouter(actions, "calendar.read").ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/calendar/events?start=2026-08-01T00:00:00Z&end=2026-08-02T00:00:00Z", nil))
		if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
			t.Fatalf("%v status=%d body=%s", test.err, response.Code, response.Body.String())
		}
	}
}

func TestCustomDateValueRoutesUseAllowlistedStableDTOs(t *testing.T) {
	date := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	timestamp := time.Date(2026, 8, 12, 14, 30, 0, 0, time.FixedZone("EDT", -4*60*60))
	created := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	value := customfields.DateValue{ID: "value-1", FieldID: "field-1", MSPID: "secret-msp", ClientID: "client-1", ObjectID: "object-1", ObjectType: customfields.ObjectWorkRecord, SourceRevision: 4, Version: 2, DateValue: &date, Timezone: "America/New_York", CreatedAt: created, UpdatedAt: created, CreatedBy: "secret-created", UpdatedBy: "secret-updated"}
	timestampValue := value
	timestampValue.ID = "value-2"
	timestampValue.DateValue = nil
	timestampValue.TimestampValue = &timestamp
	actions := &customDateValueActionsStub{values: []customfields.DateValue{value, timestampValue}, set: value}
	principal := calendarRoutePrincipal("calendar.read", "custom_date.set")
	router := NewRouter(Dependencies{Principal: func(*http.Request) (authorization.Principal, error) { return principal, nil }, CalendarCustomDateQueries: actions, CalendarCustomDateValues: actions})
	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/objects/work_record/object-1/custom-date-values", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"date_value":"2026-08-12"`) || !strings.Contains(get.Body.String(), `"timestamp_value":"2026-08-12T14:30:00-04:00"`) || !strings.Contains(get.Body.String(), `"field_id":"field-1"`) {
		t.Fatalf("GET status=%d body=%s", get.Code, get.Body.String())
	}
	put := performCalendarJSON(router, http.MethodPut, "/api/v1/objects/work_record/object-1/custom-date-values", `{"field_id":"field-1","date_value":"2026-08-12","timezone":"America/New_York","expected_version":1,"idempotency_key":"set-date"}`)
	if put.Code != http.StatusOK || !strings.Contains(put.Body.String(), `"date_value":"2026-08-12"`) {
		t.Fatalf("PUT status=%d body=%s", put.Code, put.Body.String())
	}
	for _, body := range []string{get.Body.String(), put.Body.String()} {
		for _, forbidden := range []string{"secret-", "MSPID", "CreatedBy", "UpdatedBy", "T00:00:00"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("leaked %q in %s", forbidden, body)
			}
		}
	}
}

func performCalendarJSON(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeCalendarResponse(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatal(err)
	}
}

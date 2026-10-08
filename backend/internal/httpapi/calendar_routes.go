package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/customfields"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

const calendarRequestLimitBytes int64 = 1 << 20

type calendarChangeRequest struct {
	ProjectionID    string                   `json:"projection_id"`
	OccurrenceKey   string                   `json:"occurrence_key"`
	OccurrenceScope calendar.OccurrenceScope `json:"occurrence_scope"`
	AllDay          bool                     `json:"all_day"`
	StartsOn        string                   `json:"starts_on"`
	EndsOn          string                   `json:"ends_on"`
	StartsAt        string                   `json:"starts_at"`
	EndsAt          string                   `json:"ends_at"`
	Timezone        string                   `json:"timezone"`
	Recurrence      *calendar.RecurrenceRule `json:"recurrence"`
}
type calendarProposalRequest struct {
	Change calendarChangeRequest `json:"change"`
}
type calendarApplyRequest struct {
	AcceptedOptionalChangeIDs []string `json:"accepted_optional_change_ids"`
	ExpectedProposalVersion   int64    `json:"expected_proposal_version"`
	Reason                    string   `json:"reason"`
}
type calendarPreferenceRequest struct {
	Rules           []notifications.CalendarPreferenceRule `json:"rules"`
	ExpectedVersion int64                                  `json:"expected_version"`
}
type calendarDependencyRequest struct {
	PredecessorID  string                  `json:"predecessor_id"`
	SuccessorID    string                  `json:"successor_id"`
	Type           calendar.DependencyType `json:"type"`
	LeadLagMinutes int                     `json:"lead_lag_minutes"`
}
type calendarConflictPoliciesRequest struct {
	Scope           calendar.ConflictPolicyScope  `json:"scope"`
	Rules           []calendar.ConflictPolicyRule `json:"rules"`
	ExpectedVersion int64                         `json:"expected_version"`
	EffectiveFrom   string                        `json:"effective_from"`
}
type calendarCustomDateFieldRequest struct {
	ID                  string                  `json:"id"`
	ObjectType          string                  `json:"object_type"`
	FieldID             string                  `json:"field_id"`
	Label               string                  `json:"label"`
	Category            string                  `json:"category"`
	FieldType           calendar.FieldType      `json:"field_type"`
	SchedulingMode      calendar.SchedulingMode `json:"scheduling_mode"`
	CapacityBearing     bool                    `json:"capacity_bearing"`
	TimezoneSource      string                  `json:"timezone_source"`
	PlannedEffortSource string                  `json:"planned_effort_source"`
	ExpectedVersion     int64                   `json:"expected_version"`
}
type calendarCustomDateValueRequest struct {
	FieldID         string `json:"field_id"`
	DateValue       string `json:"date_value"`
	TimestampValue  string `json:"timestamp_value"`
	Timezone        string `json:"timezone"`
	ExpectedVersion int64  `json:"expected_version"`
	IdempotencyKey  string `json:"idempotency_key"`
}

func (r *Router) registerCalendarRoutes() {
	r.mux.HandleFunc("GET /api/v1/objects/{type}/{id}/custom-date-fields", r.getObjectCustomDateFields)
	r.mux.HandleFunc("GET /api/v1/calendar/dependencies", r.listCalendarDependencies)
	r.mux.HandleFunc("GET /api/v1/calendar/events", r.calendarEvents)
	r.mux.HandleFunc("GET /api/v1/calendar/capacity", r.calendarCapacity)
	r.mux.HandleFunc("GET /api/v1/calendar/filter-options", r.calendarFilterOptions)
	r.mux.HandleFunc("GET /api/v1/calendar/live", r.calendarLive)
	r.mux.HandleFunc("POST /api/v1/calendar/proposals", r.previewCalendarProposal)
	r.mux.HandleFunc("POST /api/v1/calendar/proposals/{id}/apply", r.applyCalendarProposal)
	r.mux.HandleFunc("POST /api/v1/calendar/dependencies/preview", r.previewCalendarDependency)
	r.mux.HandleFunc("POST /api/v1/calendar/dependencies", r.createCalendarDependency)
	r.mux.HandleFunc("DELETE /api/v1/calendar/dependencies/{id}", r.deleteCalendarDependency)
	r.mux.HandleFunc("GET /api/v1/calendar/conflict-policies", r.getCalendarConflictPolicies)
	r.mux.HandleFunc("PUT /api/v1/calendar/conflict-policies", r.putCalendarConflictPolicies)
	r.mux.HandleFunc("GET /api/v1/calendar/custom-date-fields", r.getCalendarCustomDateFields)
	r.mux.HandleFunc("PUT /api/v1/calendar/custom-date-fields", r.putCalendarCustomDateField)
	r.mux.HandleFunc("GET /api/v1/calendar/notification-preferences", r.getCalendarNotificationPreferences)
	r.mux.HandleFunc("PUT /api/v1/calendar/notification-preferences", r.putCalendarNotificationPreferences)
	r.mux.HandleFunc("GET /api/v1/calendar/preferences", r.getCalendarNotificationPreferences)
	r.mux.HandleFunc("PUT /api/v1/calendar/preferences", r.putCalendarNotificationPreferences)
	r.mux.HandleFunc("GET /api/v1/objects/{type}/{id}/custom-date-values", r.getObjectCustomDateValues)
	r.mux.HandleFunc("PUT /api/v1/objects/{type}/{id}/custom-date-values", r.putObjectCustomDateValue)
}

func (r *Router) calendarEvents(w http.ResponseWriter, q *http.Request) {
	r.calendarQuery(w, q, "events")
}
func (r *Router) calendarCapacity(w http.ResponseWriter, q *http.Request) {
	r.calendarQuery(w, q, "capacity")
}
func (r *Router) calendarFilterOptions(w http.ResponseWriter, q *http.Request) {
	r.calendarQuery(w, q, "filter-options")
}

func (r *Router) calendarQuery(w http.ResponseWriter, request *http.Request, kind string) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.CalendarQueries == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "calendar query service is unavailable")
		return
	}
	query, code, err := parseCalendarQuery(request, principal)
	if err != nil {
		writeError(request.Context(), w, http.StatusBadRequest, code, "calendar window or filters are invalid")
		return
	}
	var result any
	switch kind {
	case "events":
		result, err = r.dependencies.CalendarQueries.List(request.Context(), query)
	case "capacity":
		result, err = r.dependencies.CalendarQueries.Capacity(request.Context(), query)
	default:
		result, err = r.dependencies.CalendarQueries.FilterOptions(request.Context(), query)
	}
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func parseCalendarQuery(request *http.Request, principal authorization.Principal) (calendar.QueryRequest, string, error) {
	values := request.URL.Query()
	start, err := time.Parse(time.RFC3339, values.Get("start"))
	if err != nil {
		return calendar.QueryRequest{}, "invalid_calendar_window", err
	}
	end, err := time.Parse(time.RFC3339, values.Get("end"))
	if err != nil || !end.After(start) {
		return calendar.QueryRequest{}, "invalid_calendar_window", errors.New("invalid window")
	}
	if end.Sub(start) > 366*24*time.Hour {
		return calendar.QueryRequest{}, "calendar_window_too_large", calendar.ErrWindowTooLarge
	}
	limit := 0
	if raw := values.Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 1000 {
			return calendar.QueryRequest{}, "invalid_calendar_window", errors.New("invalid limit")
		}
	}
	conflicts := false
	if raw := values.Get("conflicts_only"); raw != "" {
		conflicts, err = strconv.ParseBool(raw)
		if err != nil {
			return calendar.QueryRequest{}, "invalid_calendar_window", err
		}
	}
	principal.Scope.ClientID = ""
	filter := calendar.Filter{
		TechnicianIDs: calendarQueryValues(values["technician_ids"]), OwnerIDs: calendarQueryValues(values["owner_ids"]), TeamIDs: calendarQueryValues(values["team_ids"]),
		TechnologyIDs: calendarQueryValues(values["technology_ids"]), ProjectIDs: calendarQueryValues(values["project_ids"]), PhaseIDs: calendarQueryValues(values["phase_ids"]),
		SLAIDs: calendarQueryValues(values["sla_ids"]), TicketTypes: calendarQueryValues(values["ticket_types"]), TagIDs: calendarQueryValues(append(values["tag_ids"], values["tags"]...)),
		Priorities: calendarQueryValues(values["priorities"]), EventRoles: calendarQueryValues(values["event_roles"]), SourceTypes: calendarQueryValues(values["source_types"]), ConflictsOnly: conflicts,
	}
	if _, present := values["client_ids"]; present {
		filter.ClientIDs = calendarQueryValues(values["client_ids"])
	}
	for _, v := range calendarQueryValues(values["scheduling_modes"]) {
		filter.SchedulingModes = append(filter.SchedulingModes, calendar.SchedulingMode(v))
	}
	for _, v := range calendarQueryValues(values["terminal_states"]) {
		filter.TerminalStates = append(filter.TerminalStates, calendar.TerminalState(v))
	}
	for _, v := range calendarQueryValues(values["health_states"]) {
		filter.HealthStates = append(filter.HealthStates, calendar.HealthState(v))
	}
	return calendar.QueryRequest{Principal: principal, Window: calendar.QueryWindow{Start: start.UTC(), End: end.UTC()}, Filter: filter, Cursor: values.Get("cursor"), Limit: limit}, "", nil
}

func calendarQueryValues(values []string) []string {
	if values == nil {
		return nil
	}
	result, seen := []string{}, map[string]bool{}
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item != "" && !seen[item] {
				seen[item] = true
				result = append(result, item)
			}
		}
	}
	return result
}

func (r *Router) calendarLive(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.CalendarLive == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "calendar live service is unavailable")
		return
	}
	limit := 0
	var err error
	if raw := request.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
	}
	if err != nil || limit < 0 || limit > 1000 {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_calendar_cursor", "calendar live cursor is invalid")
		return
	}
	principal.Scope.ClientID = ""
	page, err := r.dependencies.CalendarLive.ListAfter(request.Context(), calendar.LiveRequest{Principal: principal, Cursor: request.URL.Query().Get("cursor"), Limit: limit})
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	if page.RefetchRequired {
		writeSSE(w, "refetch", map[string]any{"cursor": page.Cursor})
		return
	}
	for _, change := range page.Changes {
		writeSSE(w, string(change.Type), change)
	}
	writeSSE(w, "cursor", map[string]string{"cursor": page.Cursor})
}
func writeSSE(w http.ResponseWriter, event string, value any) {
	body, _ := json.Marshal(value)
	_, _ = w.Write([]byte("event: " + event + "\ndata: " + string(body) + "\n\n"))
}

func (r *Router) previewCalendarProposal(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.CalendarProposals == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "calendar scheduling is unavailable")
		return
	}
	var body calendarProposalRequest
	if !decodeCalendarRequest(w, request, &body, "invalid_calendar_proposal") {
		return
	}
	change, err := body.Change.domain()
	if err != nil {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_calendar_proposal", "calendar proposal is invalid")
		return
	}
	result, err := r.dependencies.CalendarProposals.Preview(request.Context(), calendar.PreviewCommand{Principal: principal, PrimaryChange: change})
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
func (r calendarChangeRequest) domain() (calendar.RequestedChange, error) {
	result := calendar.RequestedChange{ProjectionID: strings.TrimSpace(r.ProjectionID), OccurrenceKey: strings.TrimSpace(r.OccurrenceKey), OccurrenceScope: r.OccurrenceScope, AllDay: r.AllDay, Timezone: strings.TrimSpace(r.Timezone), Recurrence: r.Recurrence}
	var err error
	if result.ProjectionID == "" {
		return result, errors.New("missing projection")
	}
	if r.StartsOn != "" {
		result.StartsOn, err = parseISODatePointer(r.StartsOn)
		if err != nil {
			return result, err
		}
	}
	if r.EndsOn != "" {
		result.EndsOn, err = parseISODatePointer(r.EndsOn)
		if err != nil {
			return result, err
		}
	}
	if r.StartsAt != "" {
		result.StartsAt, err = parseRFC3339Pointer(r.StartsAt)
		if err != nil {
			return result, err
		}
	}
	if r.EndsAt != "" {
		result.EndsAt, err = parseRFC3339Pointer(r.EndsAt)
		if err != nil {
			return result, err
		}
	}
	if result.AllDay != (result.StartsOn != nil) || result.AllDay && (result.StartsAt != nil || result.EndsAt != nil || result.Timezone != "") || !result.AllDay && (result.StartsAt == nil || result.StartsOn != nil || result.EndsOn != nil || result.Timezone == "") {
		return result, errors.New("mixed interval")
	}
	return result, nil
}

func (r *Router) applyCalendarProposal(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.CalendarProposals == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "calendar scheduling is unavailable")
		return
	}
	var body calendarApplyRequest
	if !decodeCalendarRequest(w, request, &body, "invalid_calendar_proposal") {
		return
	}
	if body.ExpectedProposalVersion < 1 || strings.TrimSpace(body.Reason) == "" {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_calendar_proposal", "proposal version and reason are required")
		return
	}
	result, err := r.dependencies.CalendarProposals.Apply(request.Context(), calendar.ApplyCommand{Principal: principal, ProposalID: request.PathValue("id"), AcceptedOptionalChangeIDs: body.AcceptedOptionalChangeIDs, OverrideReason: strings.TrimSpace(body.Reason), ExpectedProposalVersion: body.ExpectedProposalVersion})
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (r *Router) previewCalendarDependency(w http.ResponseWriter, request *http.Request) {
	r.calendarDependency(w, request, true)
}
func (r *Router) createCalendarDependency(w http.ResponseWriter, request *http.Request) {
	r.calendarDependency(w, request, false)
}
func (r *Router) calendarDependency(w http.ResponseWriter, request *http.Request, preview bool) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.CalendarDependencies == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "calendar dependencies are unavailable")
		return
	}
	var body calendarDependencyRequest
	if !decodeCalendarRequest(w, request, &body, "invalid_calendar_dependency") {
		return
	}
	command := calendar.CreateDependencyCommand{Principal: principal, PredecessorID: body.PredecessorID, SuccessorID: body.SuccessorID, Type: body.Type, LeadLagMinutes: body.LeadLagMinutes}
	var result any
	var err error
	if preview {
		result, err = r.dependencies.CalendarDependencies.Preview(request.Context(), command)
	} else {
		result, err = r.dependencies.CalendarDependencies.Create(request.Context(), command)
	}
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	status := http.StatusOK
	if !preview {
		status = http.StatusCreated
	}
	writeJSON(w, status, result)
}
func (r *Router) deleteCalendarDependency(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.CalendarDependencies == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "calendar dependencies are unavailable")
		return
	}
	version, err := strconv.ParseInt(request.URL.Query().Get("expected_version"), 10, 64)
	if err != nil || version < 1 {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_calendar_dependency", "expected version is required")
		return
	}
	if err = r.dependencies.CalendarDependencies.Delete(request.Context(), calendar.DeleteDependencyCommand{Principal: principal, DependencyID: request.PathValue("id"), ExpectedVersion: version}); err != nil {
		writeDomainError(w, request, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) getCalendarConflictPolicies(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPolicyPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.CalendarConfigurationQueries == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "calendar policy queries are unavailable")
		return
	}
	scopeValue, err := conflictPolicyScopeFromQuery(request)
	if err != nil {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_calendar_policy", "calendar policy scope is invalid")
		return
	}
	result, err := r.dependencies.CalendarConfigurationQueries.ListConflictPolicies(request.Context(), principal, scopeValue)
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (r *Router) putCalendarConflictPolicies(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPolicyPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.CalendarConfiguration == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "calendar policy service is unavailable")
		return
	}
	var body calendarConflictPoliciesRequest
	if !decodeCalendarRequest(w, request, &body, "invalid_calendar_policy") {
		return
	}
	var effective time.Time
	var err error
	if body.EffectiveFrom != "" {
		effective, err = time.Parse(time.RFC3339, body.EffectiveFrom)
		if err != nil {
			writeError(request.Context(), w, http.StatusBadRequest, "invalid_calendar_policy", "effective_from must be RFC3339")
			return
		}
	}
	result, err := r.dependencies.CalendarConfiguration.ReplaceConflictPolicies(request.Context(), calendar.ReplaceConflictPoliciesCommand{Principal: principal, Scope: body.Scope, Rules: body.Rules, ExpectedVersion: body.ExpectedVersion, EffectiveFrom: effective, ActorID: principal.ID, Source: "http"})
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func conflictPolicyScopeFromQuery(request *http.Request) (calendar.ConflictPolicyScope, error) {
	v := calendar.ConflictPolicyScope{Type: calendar.ConflictPolicyScopeType(request.URL.Query().Get("scope_type")), TeamID: request.URL.Query().Get("team_id"), TechnicianID: request.URL.Query().Get("technician_id")}
	if v.Type == "" {
		v.Type = calendar.ConflictScopeMSP
	}
	if v.Type != calendar.ConflictScopeMSP && v.Type != calendar.ConflictScopeTeam && v.Type != calendar.ConflictScopeTechnician {
		return v, errors.New("invalid scope")
	}
	return v, nil
}

func (r *Router) getCalendarCustomDateFields(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPolicyPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.CalendarConfigurationQueries == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "custom date field queries are unavailable")
		return
	}
	result, err := r.dependencies.CalendarConfigurationQueries.ListCustomDateFields(request.Context(), principal)
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (r *Router) putCalendarCustomDateField(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPolicyPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.CalendarConfiguration == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "custom date field service is unavailable")
		return
	}
	var body calendarCustomDateFieldRequest
	if !decodeCalendarRequest(w, request, &body, "invalid_custom_date_field") {
		return
	}
	result, err := r.dependencies.CalendarConfiguration.UpsertCustomDateField(request.Context(), calendar.UpsertCustomDateCommand{Principal: principal, ID: body.ID, ObjectType: body.ObjectType, FieldID: body.FieldID, Label: body.Label, Category: body.Category, FieldType: body.FieldType, SchedulingMode: body.SchedulingMode, CapacityBearing: body.CapacityBearing, TimezoneSource: body.TimezoneSource, PlannedEffortSource: body.PlannedEffortSource, ExpectedVersion: body.ExpectedVersion, ActorID: principal.ID, Source: "http"})
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (r *Router) getCalendarNotificationPreferences(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.CalendarPreferences == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "calendar preferences are unavailable")
		return
	}
	result, err := r.dependencies.CalendarPreferences.Get(request.Context(), principal)
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (r *Router) putCalendarNotificationPreferences(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.CalendarPreferences == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "calendar preferences are unavailable")
		return
	}
	var body calendarPreferenceRequest
	if !decodeCalendarRequest(w, request, &body, "invalid_calendar_preferences") {
		return
	}
	result, err := r.dependencies.CalendarPreferences.Replace(request.Context(), notifications.ReplaceCalendarPreferenceCommand{Principal: principal, Rules: body.Rules, ExpectedVersion: body.ExpectedVersion})
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (r *Router) getObjectCustomDateValues(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.CalendarCustomDateQueries == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "custom date value queries are unavailable")
		return
	}
	result, err := r.dependencies.CalendarCustomDateQueries.ListCustomDateValues(request.Context(), principal, customfields.ObjectType(request.PathValue("type")), request.PathValue("id"))
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, customDateValueResponses(result))
}
func (r *Router) putObjectCustomDateValue(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.CalendarCustomDateValues == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "custom date values are unavailable")
		return
	}
	var body calendarCustomDateValueRequest
	if !decodeCalendarRequest(w, request, &body, "invalid_custom_date_value") {
		return
	}
	var dateValue, timestamp *time.Time
	var err error
	if body.DateValue != "" {
		dateValue, err = parseISODatePointer(body.DateValue)
	}
	if err == nil && body.TimestampValue != "" {
		timestamp, err = parseRFC3339Pointer(body.TimestampValue)
	}
	if err != nil || (dateValue == nil) == (timestamp == nil) {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_custom_date_value", "exactly one valid date value is required")
		return
	}
	result, err := r.dependencies.CalendarCustomDateValues.Set(request.Context(), customfields.SetDateCommand{Principal: principal, ObjectType: customfields.ObjectType(request.PathValue("type")), ObjectID: request.PathValue("id"), FieldID: body.FieldID, DateValue: dateValue, TimestampValue: timestamp, Timezone: body.Timezone, ExpectedVersion: body.ExpectedVersion, ActorID: principal.ID, Source: "http", IdempotencyKey: body.IdempotencyKey})
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeETag(w, result.Version)
	writeJSON(w, http.StatusOK, customDateValueResponse(result))
}

func (r *Router) calendarPrincipal(w http.ResponseWriter, request *http.Request) (authorization.Principal, bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return authorization.Principal{}, false
	}
	return principal, true
}
func (r *Router) calendarPolicyPrincipal(w http.ResponseWriter, request *http.Request) (authorization.Principal, bool) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return principal, false
	}
	principal.Scope.ClientID = ""
	if err := authorization.Authorize(principal, "calendar.policy.manage", scope.Target{MSPID: principal.Scope.MSPID}); err != nil {
		writeDomainError(w, request, err)
		return principal, false
	}
	return principal, true
}

func decodeCalendarRequest(w http.ResponseWriter, request *http.Request, target any, code string) bool {
	contentType := strings.TrimSpace(request.Header.Get("Content-Type"))
	mediaType, _, err := mime.ParseMediaType(contentType)
	if contentType == "" || err != nil || !strings.EqualFold(mediaType, "application/json") {
		writeError(request.Context(), w, http.StatusUnsupportedMediaType, "unsupported_media_type", "request content type must be application/json")
		return false
	}
	body := http.MaxBytesReader(w, request.Body, calendarRequestLimitBytes)
	raw, err := io.ReadAll(body)
	if err != nil {
		writeError(request.Context(), w, http.StatusBadRequest, code, "request body is invalid")
		return false
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		writeError(request.Context(), w, http.StatusBadRequest, code, "request body must contain one JSON object")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		writeError(request.Context(), w, http.StatusBadRequest, code, "request body is invalid")
		return false
	}
	if err = decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(request.Context(), w, http.StatusBadRequest, code, "request body must contain one JSON object")
		return false
	}
	return true
}
func parseISODatePointer(value string) (*time.Time, error) {
	if len(value) != len("2006-01-02") {
		return nil, errors.New("date must use YYYY-MM-DD")
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return nil, errors.New("invalid date")
	}
	return &parsed, nil
}
func parseRFC3339Pointer(value string) (*time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

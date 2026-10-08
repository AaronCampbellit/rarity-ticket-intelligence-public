package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
)

type workforceWindowRequest struct {
	Weekday         int `json:"weekday"`
	StartsMinute    int `json:"starts_minute"`
	EndsMinute      int `json:"ends_minute"`
	CapacityPercent int `json:"capacity_percent"`
}
type workforceExceptionRequest struct {
	ExceptionOn     string                   `json:"exception_on"`
	State           workforce.ExceptionState `json:"state"`
	AllDay          bool                     `json:"all_day"`
	StartsMinute    int                      `json:"starts_minute"`
	EndsMinute      int                      `json:"ends_minute"`
	CapacityPercent int                      `json:"capacity_percent"`
	Reason          string                   `json:"reason"`
	ExpectedVersion int64                    `json:"expected_version"`
	IdempotencyKey  string                   `json:"idempotency_key"`
}
type publishScheduleRequest struct {
	TechnicianID     string                      `json:"technician_id"`
	Timezone         string                      `json:"timezone"`
	EffectiveFrom    string                      `json:"effective_from"`
	EffectiveThrough string                      `json:"effective_through"`
	ExpectedVersion  int64                       `json:"expected_version"`
	IdempotencyKey   string                      `json:"idempotency_key"`
	Windows          []workforceWindowRequest    `json:"windows"`
	Exceptions       []workforceExceptionRequest `json:"exceptions"`
}
type requestPTORequest struct {
	TechnicianID   string `json:"technician_id"`
	PTOType        string `json:"pto_type"`
	AllDay         bool   `json:"all_day"`
	StartsOn       string `json:"starts_on"`
	EndsOn         string `json:"ends_on"`
	StartsAt       string `json:"starts_at"`
	EndsAt         string `json:"ends_at"`
	Timezone       string `json:"timezone"`
	IdempotencyKey string `json:"idempotency_key"`
}
type decidePTORequest struct {
	Decision        workforce.PTOState `json:"decision"`
	Reason          string             `json:"reason"`
	ExpectedVersion int64              `json:"expected_version"`
	IdempotencyKey  string             `json:"idempotency_key"`
}
type cancelPTORequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	IdempotencyKey  string `json:"idempotency_key"`
}

func (r *Router) registerWorkforceScheduleRoutes() {
	r.mux.HandleFunc("GET /api/v1/workforce/schedules", r.listWorkforceSchedules)
	r.mux.HandleFunc("POST /api/v1/workforce/schedules", r.publishWorkforceSchedule)
	r.mux.HandleFunc("POST /api/v1/workforce/schedules/{id}/exceptions", r.addWorkforceScheduleException)
	r.mux.HandleFunc("GET /api/v1/workforce/pto", r.listWorkforcePTO)
	r.mux.HandleFunc("POST /api/v1/workforce/pto", r.requestWorkforcePTO)
	r.mux.HandleFunc("POST /api/v1/workforce/pto/{id}/decision", r.decideWorkforcePTO)
	r.mux.HandleFunc("POST /api/v1/workforce/pto/{id}/cancel", r.cancelWorkforcePTO)
}

func (r *Router) listWorkforceSchedules(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.WorkforceQueries == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "workforce schedule queries are unavailable")
		return
	}
	window, err := optionalCalendarWindow(request)
	if err != nil {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_workforce_schedule", "workforce window is invalid")
		return
	}
	result, err := r.dependencies.WorkforceQueries.ListSchedules(request.Context(), principal, calendarQueryValues(request.URL.Query()["technician_ids"]), window)
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, scheduleResponses(result))
}
func (r *Router) publishWorkforceSchedule(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.WorkforceSchedules == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "workforce schedules are unavailable")
		return
	}
	var body publishScheduleRequest
	if !decodeCalendarRequest(w, request, &body, "invalid_workforce_schedule") {
		return
	}
	effective, err := parseISODatePointer(body.EffectiveFrom)
	if err != nil {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_workforce_schedule", "effective_from must be an ISO date")
		return
	}
	var through *time.Time
	if body.EffectiveThrough != "" {
		through, err = parseISODatePointer(body.EffectiveThrough)
		if err != nil {
			writeError(request.Context(), w, http.StatusBadRequest, "invalid_workforce_schedule", "effective_through must be an ISO date")
			return
		}
	}
	windows := make([]workforce.WeeklyWindow, len(body.Windows))
	for i, value := range body.Windows {
		windows[i] = workforce.WeeklyWindow{Weekday: time.Weekday(value.Weekday), StartsMinute: value.StartsMinute, EndsMinute: value.EndsMinute, CapacityPercent: value.CapacityPercent}
	}
	exceptions := make([]workforce.ScheduleException, len(body.Exceptions))
	for i, value := range body.Exceptions {
		date, parseErr := parseISODatePointer(value.ExceptionOn)
		if parseErr != nil {
			writeError(request.Context(), w, http.StatusBadRequest, "invalid_workforce_schedule", "exception_on must be an ISO date")
			return
		}
		exceptions[i] = workforce.ScheduleException{ExceptionOn: *date, State: value.State, AllDay: value.AllDay, StartsMinute: value.StartsMinute, EndsMinute: value.EndsMinute, CapacityPercent: value.CapacityPercent, Reason: value.Reason}
	}
	result, err := r.dependencies.WorkforceSchedules.Publish(request.Context(), workforce.PublishScheduleCommand{Principal: principal, TechnicianID: body.TechnicianID, Timezone: body.Timezone, EffectiveFrom: *effective, EffectiveThrough: through, ExpectedVersion: body.ExpectedVersion, Windows: windows, Exceptions: exceptions, ActorID: principal.ID, Source: "http", IdempotencyKey: body.IdempotencyKey})
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeETag(w, result.Version)
	writeJSON(w, http.StatusCreated, scheduleResponse(result))
}
func (r *Router) addWorkforceScheduleException(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.WorkforceScheduleExceptions == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "workforce schedule exceptions are unavailable")
		return
	}
	var body workforceExceptionRequest
	if !decodeCalendarRequest(w, request, &body, "invalid_workforce_schedule_exception") {
		return
	}
	date, err := parseISODatePointer(body.ExceptionOn)
	if err != nil || body.ExpectedVersion < 1 {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_workforce_schedule_exception", "exception date and expected version are required")
		return
	}
	result, err := r.dependencies.WorkforceScheduleExceptions.AddException(request.Context(), workforce.AddScheduleExceptionCommand{
		Principal:       principal,
		ScheduleID:      request.PathValue("id"),
		ExpectedVersion: body.ExpectedVersion,
		Exception: workforce.ScheduleException{
			ExceptionOn:     *date,
			State:           body.State,
			AllDay:          body.AllDay,
			StartsMinute:    body.StartsMinute,
			EndsMinute:      body.EndsMinute,
			CapacityPercent: body.CapacityPercent,
			Reason:          body.Reason,
		},
		ActorID:        principal.ID,
		Source:         "http",
		IdempotencyKey: body.IdempotencyKey,
	})
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeETag(w, result.Version)
	writeJSON(w, http.StatusOK, scheduleResponse(result))
}

func (r *Router) listWorkforcePTO(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.WorkforceQueries == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "PTO queries are unavailable")
		return
	}
	window, err := optionalCalendarWindow(request)
	if err != nil {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_pto_query", "PTO window is invalid")
		return
	}
	result, err := r.dependencies.WorkforceQueries.ListPTO(request.Context(), principal, calendarQueryValues(request.URL.Query()["technician_ids"]), window)
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, ptoResponses(result))
}
func (r *Router) requestWorkforcePTO(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.WorkforcePTO == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "PTO service is unavailable")
		return
	}
	var body requestPTORequest
	if !decodeCalendarRequest(w, request, &body, "invalid_pto_request") {
		return
	}
	startsOn, endsOn, startsAt, endsAt, err := parseRequestInterval(body.StartsOn, body.EndsOn, body.StartsAt, body.EndsAt)
	if err != nil || body.AllDay != (startsOn != nil) {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_pto_request", "PTO interval is invalid")
		return
	}
	result, err := r.dependencies.WorkforcePTO.Request(request.Context(), workforce.RequestPTOCommand{Principal: principal, TechnicianID: body.TechnicianID, PTOType: body.PTOType, AllDay: body.AllDay, StartsOn: startsOn, EndsOn: endsOn, StartsAt: startsAt, EndsAt: endsAt, Timezone: body.Timezone, ActorID: principal.ID, Source: "http", IdempotencyKey: body.IdempotencyKey})
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeETag(w, result.Version)
	writeJSON(w, http.StatusCreated, ptoResponse(result))
}
func (r *Router) decideWorkforcePTO(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.WorkforcePTO == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "PTO service is unavailable")
		return
	}
	var body decidePTORequest
	if !decodeCalendarRequest(w, request, &body, "invalid_pto_decision") {
		return
	}
	if body.ExpectedVersion < 1 {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_pto_decision", "expected version is required")
		return
	}
	result, err := r.dependencies.WorkforcePTO.Decide(request.Context(), workforce.DecidePTOCommand{Principal: principal, RequestID: request.PathValue("id"), Decision: body.Decision, Reason: body.Reason, ExpectedVersion: body.ExpectedVersion, ActorID: principal.ID, Source: "http", IdempotencyKey: body.IdempotencyKey})
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeETag(w, result.Version)
	writeJSON(w, http.StatusOK, ptoResponse(result))
}
func (r *Router) cancelWorkforcePTO(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.WorkforcePTO == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "PTO service is unavailable")
		return
	}
	var body cancelPTORequest
	if !decodeCalendarRequest(w, request, &body, "invalid_pto_cancel") {
		return
	}
	if body.ExpectedVersion < 1 {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_pto_cancel", "expected version is required")
		return
	}
	result, err := r.dependencies.WorkforcePTO.Cancel(request.Context(), workforce.CancelPTOCommand{Principal: principal, RequestID: request.PathValue("id"), ExpectedVersion: body.ExpectedVersion, ActorID: principal.ID, Source: "http", IdempotencyKey: body.IdempotencyKey})
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeETag(w, result.Version)
	writeJSON(w, http.StatusOK, ptoResponse(result))
}

func parseRequestInterval(startsOnValue, endsOnValue, startsAtValue, endsAtValue string) (startsOn, endsOn, startsAt, endsAt *time.Time, err error) {
	if startsOnValue != "" {
		startsOn, err = parseISODatePointer(startsOnValue)
	}
	if err == nil && endsOnValue != "" {
		endsOn, err = parseISODatePointer(endsOnValue)
	}
	if err == nil && startsAtValue != "" {
		startsAt, err = parseRFC3339Pointer(startsAtValue)
	}
	if err == nil && endsAtValue != "" {
		endsAt, err = parseRFC3339Pointer(endsAtValue)
	}
	if err != nil || (startsOn != nil && startsAt != nil) || (endsOn != nil && endsAt != nil) || startsOn == nil && startsAt == nil {
		return nil, nil, nil, nil, errors.New("invalid interval")
	}
	return
}
func optionalCalendarWindow(request *http.Request) (calendar.QueryWindow, error) {
	startValue, endValue := request.URL.Query().Get("start"), request.URL.Query().Get("end")
	if startValue == "" && endValue == "" {
		return calendar.QueryWindow{}, nil
	}
	if startValue == "" || endValue == "" {
		return calendar.QueryWindow{}, errors.New("both bounds required")
	}
	start, err := time.Parse(time.RFC3339, startValue)
	if err != nil {
		return calendar.QueryWindow{}, err
	}
	end, err := time.Parse(time.RFC3339, endValue)
	if err != nil || !end.After(start) || end.Sub(start) > 366*24*time.Hour {
		return calendar.QueryWindow{}, errors.New("invalid window")
	}
	return calendar.QueryWindow{Start: start.UTC(), End: end.UTC()}, nil
}

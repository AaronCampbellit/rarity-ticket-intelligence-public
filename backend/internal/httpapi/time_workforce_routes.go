package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/timeentries"
)

type startTimerRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
}

type timerVersionRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason,omitempty"`
}

type amendTimeEntryRequest struct {
	ExpectedVersion int64     `json:"expected_version"`
	StartedAt       time.Time `json:"started_at"`
	EndedAt         time.Time `json:"ended_at"`
	Billable        bool      `json:"billable"`
	Note            string    `json:"note"`
	Reason          string    `json:"reason"`
}

type reverseTimeEntryRequest struct {
	ExpectedVersion int64                   `json:"expected_version"`
	Reason          string                  `json:"reason"`
	Replacement     timeentries.Replacement `json:"replacement"`
}

type createLaborRoleRequest struct {
	Key               string    `json:"key"`
	Name              string    `json:"name"`
	InternalCostMinor int64     `json:"internal_cost_minor"`
	BillRateMinor     int64     `json:"bill_rate_minor"`
	Currency          string    `json:"currency"`
	EffectiveFrom     time.Time `json:"effective_from"`
}

type versionLaborRoleRequest struct {
	ExpectedVersion   int64      `json:"expected_version"`
	Name              string     `json:"name"`
	InternalCostMinor int64      `json:"internal_cost_minor"`
	BillRateMinor     int64      `json:"bill_rate_minor"`
	Currency          string     `json:"currency"`
	EffectiveFrom     time.Time  `json:"effective_from"`
	EffectiveUntil    *time.Time `json:"effective_until,omitempty"`
	Enabled           bool       `json:"enabled"`
	Reason            string     `json:"reason"`
}

func (r *Router) registerTimeWorkforceRoutes() {
	r.mux.HandleFunc("GET /api/v1/labor-roles", r.listLaborRoles)
	r.mux.HandleFunc(
		"GET /api/v1/admin/labor-roles",
		r.listLaborRolesForManagement,
	)
	r.mux.HandleFunc(
		"POST /api/v1/admin/labor-roles",
		r.createLaborRole,
	)
	r.mux.HandleFunc(
		"POST /api/v1/admin/labor-roles/{id}/versions",
		r.versionLaborRole,
	)
	r.mux.HandleFunc("GET /api/v1/timesheets/week", r.listTimesheetWeek)
	r.mux.HandleFunc("GET /api/v1/time-entries/{id}", r.getTimesheetEntry)
	r.mux.HandleFunc(
		"POST /api/v1/time-entries/{action}",
		r.mutateTimesheetEntry,
	)
	r.mux.HandleFunc(
		"GET /api/v1/work-records/{id}/timers",
		r.listTicketTimers,
	)
	r.mux.HandleFunc(
		"POST /api/v1/work-records/{id}/timers",
		r.startTicketTimer,
	)
	r.mux.HandleFunc("POST /api/v1/timers/{action}", r.mutateTicketTimer)
}

func (r *Router) getTimesheetEntry(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Timesheets == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "timesheet service is unavailable")
		return
	}
	found, err := r.dependencies.Timesheets.Get(request.Context(), timeentries.GetCommand{
		Principal: principal, Target: targetFor(principal), EntryID: request.PathValue("id"),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, found)
}

func (r *Router) listLaborRolesForManagement(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.laborRoleManagementPrincipal(writer, request)
	if !ok {
		return
	}
	found, err := r.dependencies.LaborRoles.ListForManagement(
		request.Context(),
		principal,
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, found)
}

func (r *Router) createLaborRole(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.laborRoleManagementPrincipal(writer, request)
	if !ok {
		return
	}
	var body createLaborRoleRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	found, err := r.dependencies.LaborRoles.Create(
		request.Context(),
		timeentries.CreateLaborRoleCommand{
			Principal: principal, Key: body.Key, Name: body.Name,
			InternalCostMinor: body.InternalCostMinor,
			BillRateMinor:     body.BillRateMinor,
			Currency:          body.Currency,
			EffectiveFrom:     body.EffectiveFrom,
			ActorID:           principal.ID,
			Source:            source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, found.Version)
	writeJSON(writer, http.StatusCreated, found)
}

func (r *Router) versionLaborRole(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.laborRoleManagementPrincipal(writer, request)
	if !ok {
		return
	}
	var body versionLaborRoleRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	found, err := r.dependencies.LaborRoles.Version(
		request.Context(),
		timeentries.VersionLaborRoleCommand{
			Principal: principal, LaborRoleID: request.PathValue("id"),
			ExpectedVersion: body.ExpectedVersion,
			Name:            body.Name, InternalCostMinor: body.InternalCostMinor,
			BillRateMinor: body.BillRateMinor, Currency: body.Currency,
			EffectiveFrom:  body.EffectiveFrom,
			EffectiveUntil: body.EffectiveUntil, Enabled: body.Enabled,
			Reason: body.Reason, ActorID: principal.ID,
			Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, found.Version)
	writeJSON(writer, http.StatusCreated, found)
}

func (r *Router) laborRoleManagementPrincipal(
	writer http.ResponseWriter,
	request *http.Request,
) (authorization.Principal, bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(
			request.Context(),
			writer,
			http.StatusUnauthorized,
			"unauthenticated",
			"authentication required",
		)
		return authorization.Principal{}, false
	}
	if r.dependencies.LaborRoles == nil {
		writeError(
			request.Context(),
			writer,
			http.StatusNotImplemented,
			"not_implemented",
			"labor role service is unavailable",
		)
		return authorization.Principal{}, false
	}
	return principal, true
}

func (r *Router) listTimesheetWeek(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(
			request.Context(),
			writer,
			http.StatusUnauthorized,
			"unauthenticated",
			"authentication required",
		)
		return
	}
	if r.dependencies.Timesheets == nil {
		writeError(
			request.Context(),
			writer,
			http.StatusNotImplemented,
			"not_implemented",
			"timesheet service is unavailable",
		)
		return
	}
	var anchor time.Time
	if value := strings.TrimSpace(request.URL.Query().Get("anchor")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			writeError(
				request.Context(),
				writer,
				http.StatusUnprocessableEntity,
				"validation_failed",
				"anchor must be an RFC3339 timestamp",
			)
			return
		}
		anchor = parsed
	}
	found, err := r.dependencies.Timesheets.ListWeek(
		request.Context(),
		timeentries.ListWeekCommand{
			Principal: principal,
			Target:    targetFor(principal),
			TechnicianID: strings.TrimSpace(
				request.URL.Query().Get("technician_id"),
			),
			Anchor: anchor,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, found)
}

func (r *Router) mutateTimesheetEntry(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(
			request.Context(),
			writer,
			http.StatusUnauthorized,
			"unauthenticated",
			"authentication required",
		)
		return
	}
	if r.dependencies.Timesheets == nil {
		writeError(
			request.Context(),
			writer,
			http.StatusNotImplemented,
			"not_implemented",
			"timesheet service is unavailable",
		)
		return
	}
	entryID, action, ok := strings.Cut(request.PathValue("action"), ":")
	if !ok || strings.TrimSpace(entryID) == "" {
		writeError(
			request.Context(),
			writer,
			http.StatusNotFound,
			"not_found",
			"resource not found",
		)
		return
	}
	switch action {
	case "amend":
		var body amendTimeEntryRequest
		if !decodeRequest(writer, request, &body) {
			return
		}
		found, err := r.dependencies.Timesheets.Amend(
			request.Context(),
			timeentries.AmendCommand{
				Principal: principal, Target: targetFor(principal),
				EntryID: entryID, ExpectedVersion: body.ExpectedVersion,
				StartedAt: body.StartedAt, EndedAt: body.EndedAt,
				Billable: body.Billable, Note: body.Note,
				Reason: body.Reason, ActorID: principal.ID,
				Source: source(request),
			},
		)
		if err != nil {
			writeDomainError(writer, request, err)
			return
		}
		writeETag(writer, found.Entry.Version)
		writeJSON(writer, http.StatusOK, found)
	case "reverse":
		var body reverseTimeEntryRequest
		if !decodeRequest(writer, request, &body) {
			return
		}
		found, err := r.dependencies.Timesheets.ReverseAndReplace(
			request.Context(),
			timeentries.ReverseCommand{
				Principal: principal, Target: targetFor(principal),
				EntryID: entryID, ExpectedVersion: body.ExpectedVersion,
				Reason: body.Reason, ActorID: principal.ID,
				Source: source(request), Replacement: body.Replacement,
			},
		)
		if err != nil {
			writeDomainError(writer, request, err)
			return
		}
		writeETag(writer, found.Original.Entry.Version)
		writeJSON(writer, http.StatusOK, found)
	default:
		writeError(
			request.Context(),
			writer,
			http.StatusNotFound,
			"not_found",
			"resource not found",
		)
	}
}

func (r *Router) listLaborRoles(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(
			request.Context(),
			writer,
			http.StatusUnauthorized,
			"unauthenticated",
			"authentication required",
		)
		return
	}
	if r.dependencies.LaborRoles == nil {
		writeError(
			request.Context(),
			writer,
			http.StatusNotImplemented,
			"not_implemented",
			"labor role service is unavailable",
		)
		return
	}
	found, err := r.dependencies.LaborRoles.List(
		request.Context(),
		principal,
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, found)
}

func (r *Router) listTicketTimers(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(
			request.Context(),
			writer,
			http.StatusUnauthorized,
			"unauthenticated",
			"authentication required",
		)
		return
	}
	if r.dependencies.Timers == nil {
		writeError(
			request.Context(),
			writer,
			http.StatusNotImplemented,
			"not_implemented",
			"timer service is unavailable",
		)
		return
	}
	found, err := r.dependencies.Timers.List(
		request.Context(),
		principal,
		request.PathValue("id"),
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, found)
}

func (r *Router) startTicketTimer(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(
			request.Context(),
			writer,
			http.StatusUnauthorized,
			"unauthenticated",
			"authentication required",
		)
		return
	}
	if r.dependencies.Timers == nil {
		writeError(
			request.Context(),
			writer,
			http.StatusNotImplemented,
			"not_implemented",
			"timer service is unavailable",
		)
		return
	}
	var body startTimerRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	found, err := r.dependencies.Timers.Start(
		request.Context(),
		timeentries.StartTimerCommand{
			Principal: principal, WorkRecordID: request.PathValue("id"),
			IdempotencyKey: body.IdempotencyKey,
			ActorID:        principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, found.Version)
	writeJSON(writer, http.StatusCreated, found)
}

func (r *Router) mutateTicketTimer(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(
			request.Context(),
			writer,
			http.StatusUnauthorized,
			"unauthenticated",
			"authentication required",
		)
		return
	}
	if r.dependencies.Timers == nil {
		writeError(
			request.Context(),
			writer,
			http.StatusNotImplemented,
			"not_implemented",
			"timer service is unavailable",
		)
		return
	}
	timerID, action, ok := strings.Cut(request.PathValue("action"), ":")
	if !ok || strings.TrimSpace(timerID) == "" {
		writeError(
			request.Context(),
			writer,
			http.StatusNotFound,
			"not_found",
			"resource not found",
		)
		return
	}
	var body timerVersionRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	var (
		found timeentries.TimerSession
		err   error
	)
	switch action {
	case "stop":
		found, err = r.dependencies.Timers.Stop(
			request.Context(),
			timeentries.StopTimerCommand{
				Principal: principal, ID: timerID,
				ExpectedVersion: body.ExpectedVersion,
				ActorID:         principal.ID, Source: source(request),
			},
		)
	case "discard":
		found, err = r.dependencies.Timers.Discard(
			request.Context(),
			timeentries.DiscardTimerCommand{
				Principal: principal, ID: timerID,
				ExpectedVersion: body.ExpectedVersion,
				Reason:          body.Reason,
				ActorID:         principal.ID, Source: source(request),
			},
		)
	default:
		writeError(
			request.Context(),
			writer,
			http.StatusNotFound,
			"not_found",
			"resource not found",
		)
		return
	}
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, found.Version)
	writeJSON(writer, http.StatusOK, found)
}

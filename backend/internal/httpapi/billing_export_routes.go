package httpapi

import (
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/billingexport"
)

type decideTimeEntryApprovalRequest struct {
	ExpectedVersion int64                       `json:"expected_version"`
	Decision        billingexport.ApprovalState `json:"decision"`
	Reason          string                      `json:"reason"`
}

type createBillingExportRequest struct {
	From    time.Time `json:"from"`
	Through time.Time `json:"through"`
}

func (r *Router) registerBillingExportRoutes() {
	r.mux.HandleFunc(
		"GET /api/v1/time-entries/approvals",
		r.listTimeEntryApprovals,
	)
	r.mux.HandleFunc(
		"POST /api/v1/time-entries/{id}/approval",
		r.decideTimeEntryApproval,
	)
	r.mux.HandleFunc("POST /api/v1/billing-exports", r.createBillingExport)
}

func (r *Router) listTimeEntryApprovals(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.BillingExports == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "billing export is unavailable")
		return
	}
	state := billingexport.ApprovalState(request.URL.Query().Get("state"))
	if state == "" {
		state = billingexport.PendingApproval
	}
	limit := 100
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	entries, err := r.dependencies.BillingExports.ListApprovals(
		request.Context(),
		billingexport.ListApprovalsCommand{
			Principal: principal, Target: targetFor(principal),
			State: state, Limit: limit,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, entries)
}

func (r *Router) decideTimeEntryApproval(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.BillingExports == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "billing export is unavailable")
		return
	}
	var body decideTimeEntryApprovalRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	entry, err := r.dependencies.BillingExports.DecideApproval(
		request.Context(),
		billingexport.ApprovalCommand{
			Principal: principal, Target: targetFor(principal),
			EntryID:         request.PathValue("id"),
			ExpectedVersion: body.ExpectedVersion,
			Decision:        body.Decision, Reason: body.Reason,
			ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, entry.Version)
	writeJSON(writer, http.StatusOK, entry)
}

func (r *Router) createBillingExport(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.BillingExports == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "billing export is unavailable")
		return
	}
	var body createBillingExportRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.BillingExports.Export(
		request.Context(),
		billingexport.ExportCommand{
			Principal: principal, Target: targetFor(principal),
			From: body.From, Through: body.Through,
			ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
	writer.Header().Set("Content-Disposition", `attachment; filename="rarity-billing-export.csv"`)
	writer.Header().Set("X-Rarity-Export-ID", result.ID)
	writer.Header().Set("X-Rarity-Export-SHA256", hex.EncodeToString(result.SHA256[:]))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(result.CSV)
}

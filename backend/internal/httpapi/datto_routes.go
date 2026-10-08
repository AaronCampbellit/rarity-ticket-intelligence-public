package httpapi

import (
	"net/http"
	"strconv"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/datto"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type dattoDecisionRequest struct {
	Decision datto.ReconciliationDecision `json:"decision"`
	Reason   string                       `json:"reason"`
}

type dattoSiteMappingRequest struct {
	SiteID   string `json:"site_id"`
	ClientID string `json:"client_id"`
	Reason   string `json:"reason"`
}

type dattoCandidateListResponse struct {
	Items []datto.ReconciliationCandidate `json:"items"`
}

func (r *Router) registerDattoRoutes() {
	r.mux.HandleFunc(
		"POST /api/v1/integrations/datto/{connection_id}/sync",
		r.queueDattoSync,
	)
	r.mux.HandleFunc(
		"POST /api/v1/integrations/datto/{connection_id}/site-mappings",
		r.mapDattoSite,
	)
	r.mux.HandleFunc(
		"GET /api/v1/integrations/datto/{connection_id}/status",
		r.dattoProgress,
	)
	r.mux.HandleFunc(
		"GET /api/v1/integrations/datto/reconciliation",
		r.listDattoCandidates,
	)
	r.mux.HandleFunc(
		"POST /api/v1/integrations/datto/reconciliation/{candidate_id}/decide",
		r.decideDattoCandidate,
	)
}

func (r *Router) mapDattoSite(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.dattoPrincipal(writer, request)
	if !ok {
		return
	}
	var body dattoSiteMappingRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.Datto.MapSite(
		request.Context(),
		datto.MapSiteCommand{
			Principal: principal, ConnectionID: request.PathValue("connection_id"),
			SiteID: body.SiteID, ClientID: body.ClientID, Reason: body.Reason,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) listDattoCandidates(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.dattoPrincipal(writer, request)
	if !ok {
		return
	}
	limit := 50
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(
				request.Context(), writer, http.StatusUnprocessableEntity,
				"validation_failed", "limit must be between 1 and 100",
			)
			return
		}
		limit = parsed
	}
	result, err := r.dependencies.Datto.ListCandidates(
		request.Context(),
		datto.ListCandidatesCommand{
			Principal: principal, Target: targetFor(principal), Limit: limit,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(
		writer, http.StatusOK,
		dattoCandidateListResponse{Items: result},
	)
}

func (r *Router) queueDattoSync(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.dattoPrincipal(writer, request)
	if !ok {
		return
	}
	result, err := r.dependencies.Datto.QueueManualSync(
		request.Context(),
		datto.ManualSyncCommand{
			Principal: principal,
			Target: scope.Target{
				MSPID: principal.Scope.MSPID,
			},
			ConnectionID: request.PathValue("connection_id"),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusAccepted, result)
}

func (r *Router) dattoProgress(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.dattoPrincipal(writer, request)
	if !ok {
		return
	}
	result, err := r.dependencies.Datto.Progress(
		request.Context(),
		datto.ProgressCommand{
			Principal: principal,
			Target: scope.Target{
				MSPID: principal.Scope.MSPID,
			},
			ConnectionID: request.PathValue("connection_id"),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) decideDattoCandidate(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.dattoPrincipal(writer, request)
	if !ok {
		return
	}
	var body dattoDecisionRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.Datto.Decide(
		request.Context(),
		datto.DecisionCommand{
			Principal: principal, CandidateID: request.PathValue("candidate_id"),
			Decision: body.Decision, Reason: body.Reason,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) dattoPrincipal(
	writer http.ResponseWriter,
	request *http.Request,
) (authorization.Principal, bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(
			request.Context(), writer, http.StatusUnauthorized,
			"unauthenticated", "authentication required",
		)
		return authorization.Principal{}, false
	}
	if r.dependencies.Datto == nil {
		writeError(
			request.Context(), writer, http.StatusNotImplemented,
			"not_implemented", "Datto management is unavailable",
		)
		return authorization.Principal{}, false
	}
	return principal, true
}

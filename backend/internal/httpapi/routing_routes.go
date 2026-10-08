package httpapi

import (
	"net/http"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/routing"
)

type publishRoutingRulesRequest struct {
	ExpectedVersion int64                       `json:"expected_version,omitempty"`
	Rules           []publishRoutingRuleRequest `json:"rules"`
}

type publishRoutingRuleRequest struct {
	Position   int    `json:"position"`
	ClientID   string `json:"client_id,omitempty"`
	RecordType string `json:"record_type,omitempty"`
	Priority   string `json:"priority,omitempty"`
	QueueID    string `json:"queue_id"`
}

func (r *Router) registerRoutingRoutes() {
	r.mux.HandleFunc("GET /api/v1/routing-rules/versions", r.getRoutingRules)
	r.mux.HandleFunc("POST /api/v1/routing-rules/versions", r.publishRoutingRules)
}

func (r *Router) getRoutingRules(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Routing == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "routing management is unavailable")
		return
	}
	found, err := r.dependencies.Routing.Current(request.Context(), principal)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, found.Version)
	writeJSON(writer, http.StatusOK, found)
}

func (r *Router) publishRoutingRules(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Routing == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "routing management is unavailable")
		return
	}
	var body publishRoutingRulesRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	rules := make([]routing.Rule, len(body.Rules))
	for index, rule := range body.Rules {
		rules[index] = routing.Rule{
			Position: rule.Position, ClientID: rule.ClientID,
			RecordType: rule.RecordType, Priority: rule.Priority, QueueID: rule.QueueID,
		}
	}
	published, err := r.dependencies.Routing.Publish(
		request.Context(),
		routing.PublishCommand{
			Principal: principal, ExpectedVersion: body.ExpectedVersion,
			Rules: rules, ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, published.Version)
	writeJSON(writer, http.StatusCreated, published)
}

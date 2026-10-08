package httpapi

import (
	"net/http"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/integrationhealth"
)

func (r *Router) registerIntegrationHealthRoutes() {
	r.mux.HandleFunc("GET /api/v1/integrations/health", r.integrationHealth)
}

func (r *Router) integrationHealth(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.IntegrationHealth == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "integration health is unavailable")
		return
	}
	snapshot, err := r.dependencies.IntegrationHealth.Snapshot(
		request.Context(),
		integrationhealth.SnapshotCommand{Principal: principal},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, snapshot)
}

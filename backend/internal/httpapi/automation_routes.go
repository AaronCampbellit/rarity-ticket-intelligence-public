package httpapi

import (
	"net/http"
	"strconv"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/automation"
)

func (r *Router) registerAutomationRoutes() {
	r.mux.HandleFunc(
		"GET /api/v1/automation/definitions",
		r.listAutomationDefinitions,
	)
	r.mux.HandleFunc(
		"GET /api/v1/automation/dead-letters",
		r.listAutomationDeadLetters,
	)
	r.mux.HandleFunc(
		"POST /api/v1/automation/definitions",
		r.createAutomationDefinition,
	)
	r.mux.HandleFunc(
		"POST /api/v1/automation/definitions/{id}/versions/{version}/revisions",
		r.reviseAutomationDefinition,
	)
	r.mux.HandleFunc(
		"POST /api/v1/automation/definitions/{id}/versions/{version}/publish",
		r.publishAutomationDefinition,
	)
	r.mux.HandleFunc(
		"POST /api/v1/automation/connections",
		r.createAutomationConnection,
	)
	r.mux.HandleFunc(
		"POST /api/v1/automation/dead-letters/{id}/actions",
		r.actOnAutomationDeadLetter,
	)
}

func (r *Router) listAutomationDefinitions(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.automationManagementPrincipal(writer, request)
	if !ok {
		return
	}
	result, err := r.dependencies.AutomationManagement.ListDefinitions(
		request.Context(), principal,
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) listAutomationDeadLetters(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(
			request.Context(), writer, http.StatusUnauthorized,
			"unauthenticated", "authentication required",
		)
		return
	}
	if r.dependencies.AutomationDeadLetters == nil {
		writeError(
			request.Context(), writer, http.StatusNotImplemented,
			"not_implemented", "automation dead-letter service is unavailable",
		)
		return
	}
	result, err := r.dependencies.AutomationDeadLetters.List(
		request.Context(), principal,
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) automationManagementPrincipal(
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
	if r.dependencies.AutomationManagement == nil {
		writeError(
			request.Context(), writer, http.StatusNotImplemented,
			"not_implemented", "automation management service is unavailable",
		)
		return authorization.Principal{}, false
	}
	return principal, true
}

func automationVersion(writer http.ResponseWriter, request *http.Request) (int64, bool) {
	version, err := strconv.ParseInt(request.PathValue("version"), 10, 64)
	if err != nil || version < 1 {
		writeError(
			request.Context(), writer, http.StatusUnprocessableEntity,
			"validation_failed", "automation version is invalid",
		)
		return 0, false
	}
	return version, true
}

func (r *Router) createAutomationDefinition(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.automationManagementPrincipal(writer, request)
	if !ok {
		return
	}
	var body AutomationDefinitionRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.AutomationManagement.CreateDefinition(
		request.Context(),
		automation.CreateDefinitionCommand{
			Principal: principal, Name: body.Name, Trigger: body.Trigger,
			Capabilities: body.Capabilities, Steps: body.Steps,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, 1)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) reviseAutomationDefinition(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.automationManagementPrincipal(writer, request)
	if !ok {
		return
	}
	version, ok := automationVersion(writer, request)
	if !ok {
		return
	}
	var body AutomationRevisionRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.AutomationManagement.ReviseDefinition(
		request.Context(),
		automation.ReviseDefinitionCommand{
			Principal: principal, ID: request.PathValue("id"),
			Version: version, ExpectedRecordVersion: body.ExpectedVersion,
			Trigger: body.Trigger, Capabilities: body.Capabilities,
			Steps: body.Steps,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, body.ExpectedVersion+1)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) publishAutomationDefinition(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.automationManagementPrincipal(writer, request)
	if !ok {
		return
	}
	version, ok := automationVersion(writer, request)
	if !ok {
		return
	}
	var body AutomationPublishRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.AutomationManagement.PublishDefinition(
		request.Context(),
		automation.PublishDefinitionCommand{
			Principal: principal, ID: request.PathValue("id"),
			Version: version, ExpectedRecordVersion: body.ExpectedVersion,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, body.ExpectedVersion+1)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) createAutomationConnection(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.automationManagementPrincipal(writer, request)
	if !ok {
		return
	}
	var body AutomationConnectionRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.AutomationManagement.CreateExternalConnection(
		request.Context(),
		automation.CreateExternalConnectionCommand{
			Principal: principal, Name: body.Name, Endpoint: body.Endpoint,
			SigningSecretRef: body.SigningSecretRef,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, 1)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) actOnAutomationDeadLetter(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(
			request.Context(), writer, http.StatusUnauthorized,
			"unauthenticated", "authentication required",
		)
		return
	}
	if r.dependencies.AutomationDeadLetters == nil {
		writeError(
			request.Context(), writer, http.StatusNotImplemented,
			"not_implemented", "automation dead-letter service is unavailable",
		)
		return
	}
	var body AutomationDeadLetterActionRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.AutomationDeadLetters.Act(
		request.Context(),
		automation.DeadLetterCommand{
			Principal: principal,
			ID:        request.PathValue("id"),
			Action:    body.Action,
			Reason:    body.Reason,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

package httpapi

import (
	"encoding/hex"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/attachments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
)

func (r *Router) registerSalesRoutes() {
	r.mux.HandleFunc("POST /api/v1/prospects", r.createProspect)
	r.mux.HandleFunc("GET /api/v1/prospects", r.listProspects)
	r.mux.HandleFunc("POST /api/v1/pipelines", r.createPipeline)
	r.mux.HandleFunc("GET /api/v1/pipelines", r.listPipelines)
	r.mux.HandleFunc("POST /api/v1/opportunities", r.createOpportunity)
	r.mux.HandleFunc("GET /api/v1/opportunities", r.listOpportunities)
	r.mux.HandleFunc("GET /api/v1/opportunities/{id}", r.getOpportunity)
	r.mux.HandleFunc("GET /api/v1/opportunity-forecast", r.opportunityForecast)
	r.mux.HandleFunc("PATCH /api/v1/opportunities/{id}", r.transitionOpportunity)
	r.mux.HandleFunc("PUT /api/v1/opportunities/{id}/custom-fields", r.replaceOpportunityCustomFields)
	r.mux.HandleFunc("PUT /api/v1/opportunities/{id}/participants", r.replaceOpportunityParticipants)
	r.mux.HandleFunc("GET /api/v1/opportunities/{id}/activities", r.listOpportunityActivities)
	r.mux.HandleFunc("POST /api/v1/opportunities/{id}/activities", r.createOpportunityActivity)
	r.mux.HandleFunc("GET /api/v1/opportunities/{id}/attachments", r.listOpportunityAttachments)
	r.mux.HandleFunc("POST /api/v1/opportunities/{id}/attachments", r.uploadOpportunityAttachment)
	r.mux.HandleFunc("POST /api/v1/opportunities/{id}/tasks", r.createOpportunityTask)
	r.mux.HandleFunc("GET /api/v1/opportunities/{id}/tasks", r.listOpportunityTasks)
	r.mux.HandleFunc("POST /api/v1/proposals", r.createProposal)
	r.mux.HandleFunc("GET /api/v1/proposals", r.listProposals)
	r.mux.HandleFunc("GET /api/v1/proposals/{id}", r.getProposal)
	r.mux.HandleFunc("GET /api/v1/proposal-versions/{id}", r.getProposalVersion)
	r.mux.HandleFunc("POST /api/v1/proposals/{id}/versions", r.issueProposalVersion)
	r.mux.HandleFunc("GET /api/v1/proposal-versions/{id}/internal-approval", r.getInternalApproval)
	r.mux.HandleFunc("POST /api/v1/proposal-versions/{id}/internal-approval", r.decideInternalApproval)
	r.mux.HandleFunc("POST /api/v1/proposal-versions/{id}/acceptance-grants", r.issueAcceptanceGrant)
	r.mux.HandleFunc("POST /api/v1/proposal-versions/{id}/accept", r.acceptProposalVersion)
	r.mux.HandleFunc("POST /api/v1/opportunities/{id}/conversion-preview", r.previewConversion)
	r.mux.HandleFunc("POST /api/v1/opportunities/{id}/convert", r.convertOpportunity)
}

func (r *Router) replaceOpportunityParticipants(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Sales == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "sales service is unavailable")
		return
	}
	var body ReplaceOpportunityParticipantsRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	version, present, err := parseExpectedVersionETag(request)
	if err != nil {
		writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "If-Match is invalid")
		return
	}
	if present {
		if body.ExpectedVersion > 0 && body.ExpectedVersion != version {
			writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "expected version does not match If-Match")
			return
		}
		body.ExpectedVersion = version
	}
	result, err := r.dependencies.Sales.ReplaceOpportunityParticipants(
		request.Context(),
		sales.ReplaceOpportunityParticipantsCommand{
			Principal: principal, ID: sales.OpportunityID(request.PathValue("id")),
			ExpectedVersion: body.ExpectedVersion, TeamID: body.TeamID,
			ContactIDs: body.ContactIDs, ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) listOpportunityAttachments(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Attachments == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "attachment service is unavailable")
		return
	}
	found, err := r.dependencies.Attachments.ListOpportunity(
		request.Context(), principal, request.PathValue("id"), 100,
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	result := make([]AttachmentResponse, 0, len(found))
	for _, attachment := range found {
		result = append(result, attachmentResponse(attachment))
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) uploadOpportunityAttachment(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Attachments == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "attachment service is unavailable")
		return
	}
	if request.ContentLength < 0 || request.ContentLength > maxAttachmentBytes {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "attachment size is invalid")
		return
	}
	contentType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "attachment content type is invalid")
		return
	}
	attachment, err := r.dependencies.Attachments.Upload(
		request.Context(),
		attachments.UploadCommand{
			Principal: principal, Target: targetFor(principal),
			OpportunityID: request.PathValue("id"),
			Filename:      request.Header.Get("X-Rarity-Filename"),
			ContentType:   contentType, SizeBytes: request.ContentLength,
			ActorID: principal.ID, Source: source(request),
		},
		http.MaxBytesReader(writer, request.Body, maxAttachmentBytes+1),
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, attachment.Version)
	writeJSON(writer, http.StatusCreated, attachmentResponse(attachment))
}

func attachmentResponse(attachment attachments.Attachment) AttachmentResponse {
	return AttachmentResponse{
		ID: attachment.ID, WorkRecordID: attachment.WorkRecordID,
		OpportunityID: attachment.OpportunityID, Filename: attachment.Filename,
		ContentType: attachment.ContentType, SizeBytes: attachment.SizeBytes,
		SHA256:  hex.EncodeToString(attachment.SHA256[:]),
		Version: attachment.Version, CreatedAt: attachment.CreatedAt,
	}
}

func (r *Router) replaceOpportunityCustomFields(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Sales == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "sales service is unavailable")
		return
	}
	var body ReplaceOpportunityCustomFieldsRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	version, present, err := parseExpectedVersionETag(request)
	if err != nil {
		writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "If-Match is invalid")
		return
	}
	if present {
		if body.ExpectedVersion > 0 && body.ExpectedVersion != version {
			writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "expected version does not match If-Match")
			return
		}
		body.ExpectedVersion = version
	}
	result, err := r.dependencies.Sales.ReplaceOpportunityCustomFields(
		request.Context(),
		sales.ReplaceOpportunityCustomFieldsCommand{
			Principal: principal, ID: sales.OpportunityID(request.PathValue("id")),
			ExpectedVersion: body.ExpectedVersion, Fields: body.Fields,
			ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) listOpportunityTasks(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Tasks == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "task service is unavailable")
		return
	}
	result, err := r.dependencies.Tasks.ListOpportunityTasks(
		request.Context(), principal, request.PathValue("id"),
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) listProspects(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Sales == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "sales service is unavailable")
		return
	}
	limit := 100
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeDomainError(writer, request, sales.ErrInvalidSalesRecord)
			return
		}
		limit = parsed
	}
	result, err := r.dependencies.Sales.ListProspects(request.Context(), principal, limit)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) listPipelines(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Sales == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "sales service is unavailable")
		return
	}
	result, err := r.dependencies.Sales.ListPipelines(
		request.Context(), principal,
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) getInternalApproval(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Proposals == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "proposal service is unavailable")
		return
	}
	result, err := r.dependencies.Proposals.GetInternalApproval(
		request.Context(), principal, targetFor(principal), request.PathValue("id"),
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) getProposalVersion(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.ProposalVersionQueries == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "proposal service is unavailable")
		return
	}
	result, err := r.dependencies.ProposalVersionQueries.GetProposalVersion(
		request.Context(), principal, targetFor(principal), request.PathValue("id"),
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) getProposal(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Proposals == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "proposal service is unavailable")
		return
	}
	result, err := r.dependencies.Proposals.GetProposal(
		request.Context(), principal, targetFor(principal), request.PathValue("id"),
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) listProposals(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Proposals == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "proposal service is unavailable")
		return
	}
	query := request.URL.Query()
	limit := 0
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeDomainError(writer, request, sales.ErrInvalidProposal)
			return
		}
		limit = parsed
	}
	var beforeUpdatedAt time.Time
	if raw := query.Get("before_updated_at"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeDomainError(writer, request, sales.ErrInvalidProposal)
			return
		}
		beforeUpdatedAt = parsed
	}
	result, err := r.dependencies.Proposals.ListProposals(
		request.Context(), principal, targetFor(principal),
		sales.ProposalListFilter{
			State:           sales.ProposalState(query.Get("state")),
			OpportunityID:   query.Get("opportunity_id"),
			BeforeUpdatedAt: beforeUpdatedAt, BeforeID: query.Get("before_id"),
			Limit: limit,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) getOpportunity(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Sales == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "sales service is unavailable")
		return
	}
	result, err := r.dependencies.Sales.GetOpportunity(
		request.Context(), principal, sales.OpportunityID(request.PathValue("id")),
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) listOpportunities(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Sales == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "sales service is unavailable")
		return
	}
	query := request.URL.Query()
	limit := 0
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeDomainError(writer, request, sales.ErrInvalidSalesRecord)
			return
		}
		limit = parsed
	}
	var beforeUpdatedAt time.Time
	if raw := query.Get("before_updated_at"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeDomainError(writer, request, sales.ErrInvalidSalesRecord)
			return
		}
		beforeUpdatedAt = parsed
	}
	result, err := r.dependencies.Sales.ListOpportunities(
		request.Context(), principal, sales.OpportunityListFilter{
			PipelineID:      query.Get("pipeline_id"),
			StageID:         sales.PipelineStageID(query.Get("stage_id")),
			BeforeUpdatedAt: beforeUpdatedAt,
			BeforeID:        sales.OpportunityID(query.Get("before_id")), Limit: limit,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) opportunityForecast(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Sales == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "sales service is unavailable")
		return
	}
	result, err := r.dependencies.Sales.Forecast(
		request.Context(), principal, request.URL.Query().Get("pipeline_id"),
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) createProposal(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Proposals == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "proposal service is unavailable")
		return
	}
	var body CreateProposalRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.Proposals.CreateProposal(
		request.Context(),
		sales.CreateProposalCommand{
			Principal: principal, Target: targetFor(principal),
			OpportunityID: body.OpportunityID, DisplayID: body.DisplayID,
			ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) issueAcceptanceGrant(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Proposals == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "proposal service is unavailable")
		return
	}
	var body IssueAcceptanceGrantRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.Proposals.IssueAcceptanceGrant(
		request.Context(),
		sales.IssueAcceptanceGrantCommand{
			Principal: principal, Target: targetFor(principal),
			ProposalVersionID: request.PathValue("id"),
			SignerName:        body.SignerName, SignerEmail: body.SignerEmail,
			Evidence: body.Evidence, ExpiresAt: body.ExpiresAt,
			ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) decideInternalApproval(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Proposals == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "proposal service is unavailable")
		return
	}
	var body DecideInternalApprovalRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	version, present, err := parseExpectedVersionETag(request)
	if err != nil {
		writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "If-Match is invalid")
		return
	}
	if present {
		if body.ExpectedVersion > 0 && body.ExpectedVersion != version {
			writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "expected version does not match If-Match")
			return
		}
		body.ExpectedVersion = version
	}
	result, err := r.dependencies.Proposals.DecideInternalApproval(
		request.Context(),
		sales.DecideInternalApprovalCommand{
			Principal: principal, Target: targetFor(principal),
			ProposalVersionID: request.PathValue("id"),
			ExpectedVersion:   body.ExpectedVersion, Decision: body.Decision,
			Reason: body.Reason, ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) createOpportunityTask(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Tasks == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "task service is unavailable")
		return
	}
	var body CreateTaskRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.Tasks.Create(
		request.Context(),
		tasks.CreateCommand{
			Principal: principal,
			Parent: tasks.Ref{
				Type: tasks.ParentOpportunity, ID: request.PathValue("id"),
			},
			ParentTaskID: body.ParentTaskID, Title: body.Title,
			OwnerID: body.OwnerID, EstimateMinutes: body.EstimateMinutes,
			ActorID: principal.ID, Source: source(request), TagIDs: body.TagIDs, ClassificationPolicy: tagging.CreationRequireMeaningful,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) createOpportunityActivity(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Sales == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "sales service is unavailable")
		return
	}
	var body CreateOpportunityActivityRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.Sales.CreateOpportunityActivity(
		request.Context(),
		sales.CreateOpportunityActivityCommand{
			Principal: principal, Target: targetFor(principal),
			OpportunityID: sales.OpportunityID(request.PathValue("id")),
			Kind:          body.Kind, Summary: body.Summary, Details: body.Details,
			OccurredAt: body.OccurredAt, ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) listOpportunityActivities(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Sales == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "sales service is unavailable")
		return
	}
	limit := 50
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "limit is invalid")
			return
		}
		limit = parsed
	}
	result, err := r.dependencies.Sales.ListOpportunityActivities(
		request.Context(), principal, sales.OpportunityID(request.PathValue("id")), limit,
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) createOpportunity(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Sales == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "sales service is unavailable")
		return
	}
	var body CreateOpportunityRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.Sales.CreateOpportunity(
		request.Context(),
		sales.CreateOpportunityCommand{
			Principal: principal, ClientID: body.ClientID, ProspectID: body.ProspectID,
			PipelineID: body.PipelineID, StageID: body.StageID,
			DisplayID: body.DisplayID, Name: body.Name,
			Description: body.Description,
			Amount:      sales.Money{Minor: body.AmountMinor, Currency: body.Currency},
			OwnerID:     body.OwnerID, ExpectedCloseOn: body.ExpectedCloseOn,
			ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) createProspect(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Sales == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "sales service is unavailable")
		return
	}
	var body CreateProspectRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.Sales.CreateProspect(request.Context(), sales.CreateProspectCommand{
		Principal: principal, DisplayID: body.DisplayID, Name: body.Name,
		Email: body.Email, Phone: body.Phone, ActorID: principal.ID, Source: source(request),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) createPipeline(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Sales == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "sales service is unavailable")
		return
	}
	var body CreatePipelineRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.Sales.CreatePipeline(request.Context(), sales.CreatePipelineCommand{
		Principal: principal, Key: body.Key, Name: body.Name, Stages: body.Stages,
		ActorID: principal.ID, Source: source(request),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) transitionOpportunity(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Sales == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "sales service is unavailable")
		return
	}
	var body TransitionOpportunityRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	version, present, err := parseExpectedVersionETag(request)
	if err != nil {
		writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "If-Match is invalid")
		return
	}
	if present {
		if body.ExpectedVersion > 0 && body.ExpectedVersion != version {
			writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "expected version does not match If-Match")
			return
		}
		body.ExpectedVersion = version
	}
	result, err := r.dependencies.Sales.TransitionOpportunity(request.Context(), sales.TransitionCommand{
		Principal: principal, Target: targetFor(principal),
		ID:              sales.OpportunityID(request.PathValue("id")),
		ExpectedVersion: body.ExpectedVersion, StageID: body.StageID,
		ActorID: principal.ID, Source: source(request), Reason: body.Reason,
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) issueProposalVersion(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Proposals == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "proposal service is unavailable")
		return
	}
	var body IssueProposalVersionRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.Proposals.IssueVersion(request.Context(), sales.IssueVersionCommand{
		Principal: principal, Target: targetFor(principal), ProposalID: request.PathValue("id"),
		ExpectedProposalVersion: body.ExpectedVersion, Currency: body.Currency,
		Lines: body.Lines, ApprovalRule: body.ApprovalRule, ExpiresAt: body.ExpiresAt,
		ActorID: principal.ID, Source: source(request),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) acceptProposalVersion(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Proposals == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "proposal service is unavailable")
		return
	}
	var body AcceptProposalVersionRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	var (
		result sales.Acceptance
		err    error
	)
	switch strings.ToLower(strings.TrimSpace(body.Method)) {
	case string(sales.AcceptanceElectronic):
		result, err = r.dependencies.Proposals.AcceptElectronically(request.Context(), sales.ElectronicAcceptanceCommand{
			Principal: principal, Target: targetFor(principal),
			ProposalVersionID: request.PathValue("id"),
			AcceptanceGrant:   body.AcceptanceGrant,
			Source:            source(request),
		})
	case string(sales.AcceptanceOffline):
		result, err = r.dependencies.Proposals.RecordOfflineAcceptance(request.Context(), sales.OfflineAcceptanceCommand{
			Principal: principal, Target: targetFor(principal),
			ProposalVersionID: request.PathValue("id"), SignerName: body.SignerName,
			SignerEmail: body.SignerEmail, RecordedBy: principal.ID,
			AcceptedAt: body.AcceptedAt, Source: source(request),
		})
	default:
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "acceptance method is invalid")
		return
	}
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) previewConversion(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Conversions == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "conversion service is unavailable")
		return
	}
	var body ConvertOpportunityRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	command, valid := conversionPreviewCommand(principal, request, body)
	if !valid {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "conversion request is incomplete")
		return
	}
	result, err := r.dependencies.Conversions.Preview(request.Context(), command)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) convertOpportunity(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Conversions == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "conversion service is unavailable")
		return
	}
	var body ConvertOpportunityRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	preview, valid := conversionPreviewCommand(principal, request, body)
	if !valid || strings.TrimSpace(body.IdempotencyKey) == "" ||
		strings.TrimSpace(body.PreviewHash) == "" {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "conversion request is incomplete")
		return
	}
	result, err := r.dependencies.Conversions.Convert(request.Context(), projects.ConversionCommand{
		ConversionPreviewCommand: preview,
		PreviewHash:              body.PreviewHash, IdempotencyKey: body.IdempotencyKey,
		TagIDs: body.TagIDs,
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	status := http.StatusCreated
	if result.AlreadyExists {
		status = http.StatusOK
	}
	writeJSON(writer, status, ConversionResultResponse{
		ProjectID: result.ProjectID, ClientID: result.ClientID,
		ConversionID: result.ConversionID, AlreadyExists: result.AlreadyExists,
	})
}

func conversionPreviewCommand(
	principal authorization.Principal,
	request *http.Request,
	body ConvertOpportunityRequest,
) (projects.ConversionPreviewCommand, bool) {
	valid := body.ExpectedVersion > 0 &&
		strings.TrimSpace(body.AcceptedProposalVersionID) != "" &&
		strings.TrimSpace(body.ProjectDisplayID) != "" &&
		strings.TrimSpace(body.ProjectName) != "" &&
		len(body.Phases) > 0
	phases := make([]projects.PhaseMapping, 0, len(body.Phases))
	for _, phase := range body.Phases {
		phases = append(phases, projects.PhaseMapping{
			Name: phase.Name, OwnerID: phase.OwnerID,
			ParticipatingTeams: append([]string(nil), phase.ParticipatingTeams...),
			ProposalLineIDs:    append([]string(nil), phase.ProposalLineIDs...),
			PlannedStart:       phase.PlannedStart, PlannedEnd: phase.PlannedEnd,
		})
	}
	return projects.ConversionPreviewCommand{
		Principal: principal, Target: targetFor(principal),
		OpportunityID:              sales.OpportunityID(request.PathValue("id")),
		AcceptedProposalVersionID:  body.AcceptedProposalVersionID,
		ExpectedOpportunityVersion: body.ExpectedVersion,
		ExistingClientID:           body.ExistingClientID,
		CreateClientFromProspect:   body.CreateClientFromProspect,
		ProjectDisplayID:           body.ProjectDisplayID, ProjectName: body.ProjectName,
		ProjectOwnerID: body.ProjectOwnerID,
		PlannedStart:   body.PlannedStart, PlannedEnd: body.PlannedEnd,
		Phases: phases, SelectedTaskIDs: body.SelectedTaskIDs,
		TaskVersions: body.TaskVersions, ActorID: principal.ID, Source: source(request),
	}, valid
}

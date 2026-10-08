package httpapi

import (
	"encoding/hex"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/attachments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/collaboration"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/comments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/links"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mentions"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/timeentries"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

func (r *Router) registerWorkRecordRoutes() {
	r.mux.HandleFunc("GET /api/v1/work-records", r.listWorkRecords)
	r.mux.HandleFunc("GET /api/v1/work-records/{id}", r.getWorkRecord)
	r.mux.HandleFunc("POST /api/v1/work-records", r.createWorkRecord)
	r.mux.HandleFunc("POST /api/v1/work-records/{id}/assign", r.assignWorkRecord)
	r.mux.HandleFunc("POST /api/v1/work-records/{id}/transition", r.transitionWorkRecord)
	r.mux.HandleFunc("POST /api/v1/work-records/{id}/priority", r.changeWorkRecordPriority)
	r.mux.HandleFunc("POST /api/v1/work-records/{id}/sla/override", r.overrideWorkRecordSLA)
	r.mux.HandleFunc("POST /api/v1/work-records/{id}/route", r.routeWorkRecord)
	r.mux.HandleFunc("POST /api/v1/work-records/{id}/merge", r.mergeWorkRecord)
	r.mux.HandleFunc("POST /api/v1/work-records/{id}/participants", r.addWorkParticipant)
	r.mux.HandleFunc("POST /api/v1/work-records/{id}/participants/{participant_id}/remove", r.removeWorkParticipant)
	r.mux.HandleFunc("POST /api/v1/work-records/{id}/comments", r.createComment)
	r.mux.HandleFunc("POST /api/v1/work-records/{id}/time-entries", r.createTimeEntry)
	r.mux.HandleFunc("POST /api/v1/work-records/{id}/attachments", r.uploadAttachment)
	r.mux.HandleFunc("POST /api/v1/relationships", r.createRelationship)
	r.mux.HandleFunc("POST /api/v1/work-records/{id}/tasks", r.createTask)
}

func (r *Router) getWorkRecord(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.WorkRecordQueries == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "work-record query service is unavailable")
		return
	}
	record, err := r.dependencies.WorkRecordQueries.Get(
		request.Context(),
		workrecords.GetCommand{
			Principal: principal, Target: targetFor(principal),
			ID: request.PathValue("id"),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, record.Version)
	writeJSON(writer, http.StatusOK, record)
}

func (r *Router) listWorkRecords(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.WorkRecordQueries == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "work-record query service is unavailable")
		return
	}
	query := request.URL.Query()
	limit := 0
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeDomainError(writer, request, workrecords.ErrInvalid)
			return
		}
		limit = parsed
	}
	var beforeUpdatedAt time.Time
	if raw := query.Get("before_updated_at"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeDomainError(writer, request, workrecords.ErrInvalid)
			return
		}
		beforeUpdatedAt = parsed
	}
	records, err := r.dependencies.WorkRecordQueries.List(
		request.Context(),
		workrecords.ListCommand{
			Principal: principal, Target: targetFor(principal),
			Status: query.Get("status"), QueueID: query.Get("queue_id"),
			Text: query.Get("text"), Priority: query.Get("priority"), Ownership: query.Get("ownership"),
			PrimaryOwnerID:  query.Get("owner_id"),
			BeforeUpdatedAt: beforeUpdatedAt,
			BeforeID:        query.Get("before_id"),
			Limit:           limit,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, records)
}

func (r *Router) overrideWorkRecordSLA(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.SLAOverrides == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "SLA override service is unavailable")
		return
	}
	var body OverrideSLARequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.SLAOverrides.Override(
		request.Context(),
		workrecords.SLAOverrideCommand{
			Principal: principal, Target: targetFor(principal),
			WorkRecordID:       request.PathValue("id"),
			ExpectedVersion:    body.ExpectedVersion,
			SLAExpectedVersion: body.SLAExpectedVersion,
			ResponseDueAt:      body.ResponseDueAt, ResolutionDueAt: body.ResolutionDueAt,
			Reason: body.Reason,
			Actor: workrecords.Actor{
				Type: "technician", ID: principal.ID, Source: source(request),
			},
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) changeWorkRecordPriority(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.WorkPriorities == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "work priority service is unavailable")
		return
	}
	var body ChangeWorkRecordPriorityRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.WorkPriorities.Change(
		request.Context(),
		workrecords.PriorityCommand{
			Principal: principal, Target: targetFor(principal),
			WorkRecordID: request.PathValue("id"), ExpectedVersion: body.ExpectedVersion,
			Priority: body.Priority, Reason: body.Reason,
			Actor: workrecords.Actor{
				Type: "technician", ID: principal.ID, Source: source(request),
			},
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) transitionWorkRecord(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.WorkTransitions == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "work transition service is unavailable")
		return
	}
	var body TransitionWorkRecordRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.WorkTransitions.Transition(
		request.Context(),
		workrecords.TransitionCommand{
			Principal: principal, Target: targetFor(principal),
			WorkRecordID: request.PathValue("id"), ExpectedVersion: body.ExpectedVersion,
			ToStatus: body.ToStatus, Reason: body.Reason,
			Actor: workrecords.Actor{
				Type: "technician", ID: principal.ID, Source: source(request),
			},
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

const maxAttachmentBytes = int64(250 << 20)

func (r *Router) uploadAttachment(writer http.ResponseWriter, request *http.Request) {
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
	body := http.MaxBytesReader(writer, request.Body, maxAttachmentBytes+1)
	attachment, err := r.dependencies.Attachments.Upload(
		request.Context(),
		attachments.UploadCommand{
			Principal: principal, Target: targetFor(principal),
			WorkRecordID: request.PathValue("id"),
			Filename:     request.Header.Get("X-Rarity-Filename"),
			ContentType:  contentType, SizeBytes: request.ContentLength,
			ActorID: principal.ID, Source: source(request),
		},
		body,
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, attachment.Version)
	writeJSON(writer, http.StatusCreated, AttachmentResponse{
		ID: attachment.ID, WorkRecordID: attachment.WorkRecordID,
		Filename: attachment.Filename, ContentType: attachment.ContentType,
		SizeBytes: attachment.SizeBytes,
		SHA256:    hex.EncodeToString(attachment.SHA256[:]),
		Version:   attachment.Version, CreatedAt: attachment.CreatedAt,
	})
}

func (r *Router) addWorkParticipant(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.WorkParticipants == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "work participant service is unavailable")
		return
	}
	var body AddWorkParticipantRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.WorkParticipants.Add(
		request.Context(),
		workrecords.AddParticipantCommand{
			Principal: principal, Target: targetFor(principal),
			WorkRecordID:    request.PathValue("id"),
			ExpectedVersion: body.ExpectedVersion,
			TechnicianID:    body.TechnicianID, Role: body.Role,
			Actor: workrecords.Actor{
				Type: "technician", ID: principal.ID, Source: source(request),
			},
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Record.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) removeWorkParticipant(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.WorkParticipants == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "work participant service is unavailable")
		return
	}
	var body RemoveWorkParticipantRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.WorkParticipants.Remove(
		request.Context(),
		workrecords.RemoveParticipantCommand{
			Principal: principal, Target: targetFor(principal),
			WorkRecordID:       request.PathValue("id"),
			ExpectedVersion:    body.ExpectedVersion,
			ParticipantID:      request.PathValue("participant_id"),
			ParticipantVersion: body.ParticipantVersion,
			Actor: workrecords.Actor{
				Type: "technician", ID: principal.ID, Source: source(request),
			},
			Reason: body.Reason,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Record.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) createTask(writer http.ResponseWriter, request *http.Request) {
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
			Principal: principal, WorkRecordID: request.PathValue("id"),
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

func (r *Router) createRelationship(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Relationships == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "relationship service is unavailable")
		return
	}
	var body CreateRelationshipRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	target := targetFor(principal)
	result, err := r.dependencies.Relationships.Create(
		request.Context(),
		links.CreateCommand{
			Principal: principal,
			Source: links.Ref{
				Type: body.SourceType, ID: body.SourceID,
				MSPID: target.MSPID, ClientID: target.ClientID,
			},
			Target: links.Ref{
				Type: body.TargetType, ID: body.TargetID,
				MSPID: target.MSPID, ClientID: target.ClientID,
			},
			LinkType: body.RelationshipType, ActorID: principal.ID,
			RequestSource: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) createTimeEntry(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	var body CreateTimeEntryRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	if body.TimeCapture != nil {
		if r.dependencies.TimeCaptureEntries == nil {
			writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "time-capture service is unavailable")
			return
		}
		result, err := r.dependencies.TimeCaptureEntries.Create(
			request.Context(),
			timeentries.CaptureCommand{
				Principal: principal, WorkRecordID: request.PathValue("id"),
				CaptureID:              body.TimeCapture.ID,
				ExpectedCaptureVersion: body.TimeCapture.ExpectedVersion,
				LaborRoleID:            body.TimeCapture.LaborRoleID,
				Billable:               body.TimeCapture.Billable,
				Note:                   body.Note,
				ActorID:                principal.ID, Source: source(request),
				TagIDs: body.TagIDs, ClassificationPolicy: tagging.CreationRequireMeaningful,
			},
		)
		if err != nil {
			writeDomainError(writer, request, err)
			return
		}
		writeETag(writer, result.Version)
		writeJSON(writer, http.StatusCreated, result)
		return
	}
	if r.dependencies.TimeEntries == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "time-entry service is unavailable")
		return
	}
	result, err := r.dependencies.TimeEntries.Create(
		request.Context(),
		timeentries.CreateCommand{
			Principal: principal, Target: targetFor(principal),
			WorkRecordID: request.PathValue("id"), TaskID: body.TaskID,
			TechnicianID: body.TechnicianID, StartedAt: body.StartedAt,
			EndedAt: body.EndedAt, Billable: body.Billable, Note: body.Note,
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

func (r *Router) createComment(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	var body CreateCommentRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	if body.Visibility == comments.Internal {
		if r.dependencies.Collaboration == nil {
			writeError(request.Context(), writer, http.StatusServiceUnavailable, "service_unavailable", "internal collaboration is unavailable")
			return
		}
		if body.TimeCapture != nil {
			writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "time capture is not supported on internal collaboration comments")
			return
		}
		tokens, valid := optionalMentionTokens(body.Tokens)
		confirmations, confirmationsValid := optionalTeamConfirmations(body.ConfirmedTeamSnapshots)
		if !valid || !confirmationsValid {
			writeError(request.Context(), writer, http.StatusUnprocessableEntity, "mention_token_invalid", "mention tokens are invalid")
			return
		}
		key := strings.TrimSpace(body.IdempotencyKey)
		if key == "" {
			if r.dependencies.NewID == nil {
				writeError(request.Context(), writer, http.StatusServiceUnavailable, "service_unavailable", "internal collaboration is unavailable")
				return
			}
			key = r.dependencies.NewID()
		}
		result, err := r.dependencies.Collaboration.CreateComment(request.Context(), collaboration.CreateCommand{
			Principal: principal, Parent: collaboration.ParentRef{Type: mentions.ParentWorkRecord, ID: request.PathValue("id")},
			Body: body.Body, Tokens: tokens, ConfirmedTeamSnapshots: confirmations,
			IdempotencyKey: key, Source: source(request),
		})
		if err != nil {
			writeDomainError(writer, request, err)
			return
		}
		writeETag(writer, result.Version)
		writeJSON(writer, http.StatusCreated, collaborationResponse(result, false, false))
		return
	}
	if body.Visibility != comments.ClientVisible {
		writeDomainError(writer, request, comments.ErrInvalid)
		return
	}
	if len(body.Tokens) != 0 || len(body.ConfirmedTeamSnapshots) != 0 {
		writeDomainError(writer, request, collaboration.ErrPublicMentionsForbidden)
		return
	}
	if r.dependencies.Comments == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "comment service is unavailable")
		return
	}
	var timeCapture *comments.TimeCapture
	if body.TimeCapture != nil {
		timeCapture = &comments.TimeCapture{
			ID:              body.TimeCapture.ID,
			ExpectedVersion: body.TimeCapture.ExpectedVersion,
			LaborRoleID:     body.TimeCapture.LaborRoleID,
			Billable:        body.TimeCapture.Billable,
			TagIDs:          body.TimeCapture.TagIDs,
		}
	}
	result, err := r.dependencies.Comments.Create(
		request.Context(),
		comments.CreateCommand{
			Principal: principal, Target: targetFor(principal),
			WorkRecordID: request.PathValue("id"),
			Visibility:   body.Visibility, Body: body.Body,
			ActorID: principal.ID, Source: source(request),
			TimeCapture: timeCapture,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) mergeWorkRecord(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.WorkMerges == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "merge service is unavailable")
		return
	}
	var body MergeWorkRecordRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.WorkMerges.Merge(
		request.Context(),
		workrecords.MergeCommand{
			Principal: principal, Target: targetFor(principal),
			WinnerID: request.PathValue("id"), WinnerVersion: body.WinnerVersion,
			DuplicateID: body.DuplicateID, DuplicateVersion: body.DuplicateVersion,
			Actor: workrecords.Actor{
				Type: "technician", ID: principal.ID, Source: source(request),
			},
			Reason: body.Reason,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Duplicate.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) assignWorkRecord(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.WorkAssignments == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "assignment service is unavailable")
		return
	}
	var body AssignWorkRecordRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.WorkAssignments.Assign(
		request.Context(),
		workrecords.AssignCommand{
			Principal: principal, Target: targetFor(principal),
			WorkRecordID:    request.PathValue("id"),
			ExpectedVersion: body.ExpectedVersion, OwnerID: body.OwnerID,
			Reason: body.Reason,
			Actor: workrecords.Actor{
				Type: "technician", ID: principal.ID, Source: source(request),
			},
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) routeWorkRecord(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.WorkQueues == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "queue service is unavailable")
		return
	}
	var body RouteWorkRecordRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	target := targetFor(principal)
	result, err := r.dependencies.WorkQueues.Transfer(
		request.Context(),
		workrecords.QueueCommand{
			Principal: principal, Target: target,
			WorkRecordID:    request.PathValue("id"),
			ExpectedVersion: body.ExpectedVersion,
			Queue:           workrecords.QueueRef{ID: body.QueueID, MSPID: target.MSPID},
			Actor: workrecords.Actor{
				Type: "technician", ID: principal.ID, Source: source(request),
			},
			Reason: body.Reason,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) createWorkRecord(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(
			request.Context(), writer, http.StatusUnauthorized,
			"unauthenticated", "authentication required",
		)
		return
	}
	if r.dependencies.WorkRecords == nil {
		writeError(
			request.Context(), writer, http.StatusNotImplemented,
			"not_implemented", "work-record service is unavailable",
		)
		return
	}
	var body CreateWorkRecordRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.WorkRecords.Create(
		request.Context(),
		workrecords.CreateCommand{
			Principal: principal,
			Target:    targetFor(principal),
			Actor: workrecords.Actor{
				Type: "technician", ID: principal.ID, Source: source(request),
			},
			DisplayID:   body.DisplayID,
			Type:        body.Type,
			Title:       body.Title,
			Description: body.Description,
			Status:      body.Status,
			Priority:    body.Priority,
			ServiceID:   body.ServiceID,
			ContractID:  body.ContractID,
			TagIDs:      body.TagIDs, ClassificationPolicy: tagging.CreationRequireMeaningful,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

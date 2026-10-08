package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/collaboration"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mentions"
)

type CollaborationActions interface {
	PutDetails(context.Context, collaboration.UpsertCommand) (collaboration.Source, error)
	CreateComment(context.Context, collaboration.CreateCommand) (collaboration.Source, error)
	CreateNote(context.Context, collaboration.CreateCommand) (collaboration.Source, error)
	Edit(context.Context, collaboration.UpsertCommand) (collaboration.Source, error)
	Redact(context.Context, collaboration.RedactCommand) (collaboration.Source, error)
}

type InternalContentActions interface {
	ListInternalContent(context.Context, collaboration.ListQuery) ([]collaboration.ListedSource, error)
}

type mentionTokenDTO struct {
	ID         string `json:"id"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Label      string `json:"label"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
}

type teamConfirmation struct {
	TeamVersion       int64    `json:"team_version"`
	EligibleMemberIDs []string `json:"eligible_member_ids"`
}

type internalContentRequest struct {
	Body                   string                       `json:"body"`
	Tokens                 *[]mentionTokenDTO           `json:"tokens"`
	ConfirmedTeamSnapshots *map[string]teamConfirmation `json:"confirmed_team_snapshots"`
	ExpectedVersion        int64                        `json:"expected_version"`
	IdempotencyKey         string                       `json:"idempotency_key"`
}

type internalContentEditRequest struct {
	ParentType             string                       `json:"parent_type"`
	ParentID               string                       `json:"parent_id"`
	SourceKind             string                       `json:"source_kind"`
	Body                   string                       `json:"body"`
	Tokens                 *[]mentionTokenDTO           `json:"tokens"`
	ConfirmedTeamSnapshots *map[string]teamConfirmation `json:"confirmed_team_snapshots"`
	ExpectedVersion        int64                        `json:"expected_version"`
	IdempotencyKey         string                       `json:"idempotency_key"`
}

type internalContentRedactRequest struct {
	ParentType      string `json:"parent_type"`
	ParentID        string `json:"parent_id"`
	SourceKind      string `json:"source_kind"`
	ExpectedVersion int64  `json:"expected_version"`
	IdempotencyKey  string `json:"idempotency_key"`
}

type internalContentResponse struct {
	ID             string                        `json:"id"`
	ParentType     mentions.ParentType           `json:"parent_type"`
	ParentID       string                        `json:"parent_id"`
	SourceKind     mentions.SourceKind           `json:"source_kind"`
	Body           string                        `json:"body"`
	Tokens         []mentions.Token              `json:"tokens"`
	AuthorID       string                        `json:"author_id"`
	LifecycleState collaboration.SourceLifecycle `json:"lifecycle_state"`
	Version        int64                         `json:"version"`
	CreatedAt      time.Time                     `json:"created_at"`
	UpdatedAt      time.Time                     `json:"updated_at"`
	RedactedAt     *time.Time                    `json:"redacted_at,omitempty"`
	ReadOnly       bool                          `json:"read_only"`
	Legacy         bool                          `json:"legacy"`
}

func validRequestUUID(value string) bool {
	if value == "" || value != strings.TrimSpace(value) {
		return false
	}
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func (r *Router) registerCollaborationRoutes() {
	for _, resource := range []string{"work-records", "tasks", "projects"} {
		r.mux.HandleFunc("GET /api/v1/"+resource+"/{id}/internal-content", r.listInternalContent)
		r.mux.HandleFunc("PUT /api/v1/"+resource+"/{id}/internal-details", r.putInternalDetails)
		r.mux.HandleFunc("POST /api/v1/"+resource+"/{id}/internal-comments", r.createInternalComment)
		r.mux.HandleFunc("POST /api/v1/"+resource+"/{id}/notes", r.createInternalNote)
	}
	r.mux.HandleFunc("PATCH /api/v1/internal-content/{source_id}", r.editInternalContent)
	r.mux.HandleFunc("POST /api/v1/internal-content/{source_id}/redact", r.redactInternalContent)
}

func parentFromRequest(request *http.Request) (collaboration.ParentRef, bool) {
	var parentType mentions.ParentType
	switch {
	case strings.HasPrefix(request.URL.Path, "/api/v1/work-records/"):
		parentType = mentions.ParentWorkRecord
	case strings.HasPrefix(request.URL.Path, "/api/v1/tasks/"):
		parentType = mentions.ParentTask
	case strings.HasPrefix(request.URL.Path, "/api/v1/projects/"):
		parentType = mentions.ParentProject
	default:
		return collaboration.ParentRef{}, false
	}
	if !validRequestUUID(request.PathValue("id")) {
		return collaboration.ParentRef{}, false
	}
	return collaboration.ParentRef{Type: parentType, ID: request.PathValue("id")}, true
}

func (r *Router) listInternalContent(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	parent, valid := parentFromRequest(request)
	if !valid {
		writeError(request.Context(), writer, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if r.dependencies.InternalContent == nil {
		writeError(request.Context(), writer, http.StatusServiceUnavailable, "service_unavailable", "internal content is unavailable")
		return
	}
	rows, err := r.dependencies.InternalContent.ListInternalContent(request.Context(), collaboration.ListQuery{Principal: principal, Parent: parent})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	response := make([]internalContentResponse, 0, len(rows))
	for _, row := range rows {
		response = append(response, collaborationResponse(row.Source, row.ReadOnly, row.Legacy))
	}
	writeJSON(writer, http.StatusOK, response)
}

func (r *Router) putInternalDetails(writer http.ResponseWriter, request *http.Request) {
	r.mutateParentContent(writer, request, mentions.SourceDetails)
}

func (r *Router) createInternalComment(writer http.ResponseWriter, request *http.Request) {
	r.mutateParentContent(writer, request, mentions.SourceComment)
}

func (r *Router) createInternalNote(writer http.ResponseWriter, request *http.Request) {
	r.mutateParentContent(writer, request, mentions.SourceNote)
}

func (r *Router) mutateParentContent(writer http.ResponseWriter, request *http.Request, kind mentions.SourceKind) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Collaboration == nil {
		writeError(request.Context(), writer, http.StatusServiceUnavailable, "service_unavailable", "internal collaboration is unavailable")
		return
	}
	parent, valid := parentFromRequest(request)
	if !valid {
		writeError(request.Context(), writer, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	var body internalContentRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	tokens, confirmations, valid := internalMutationValues(body.Tokens, body.ConfirmedTeamSnapshots)
	if !valid || strings.TrimSpace(body.IdempotencyKey) == "" || body.ExpectedVersion < 0 || (kind != mentions.SourceDetails && body.ExpectedVersion != 0) {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "mention_token_invalid", "internal content request is invalid")
		return
	}
	var result collaboration.Source
	var err error
	if kind == mentions.SourceDetails {
		result, err = r.dependencies.Collaboration.PutDetails(request.Context(), collaboration.UpsertCommand{Principal: principal, Parent: parent, Kind: kind, Body: body.Body, Tokens: tokens, ConfirmedTeamSnapshots: confirmations, ExpectedVersion: body.ExpectedVersion, IdempotencyKey: body.IdempotencyKey, Source: source(request)})
	} else {
		command := collaboration.CreateCommand{Principal: principal, Parent: parent, Body: body.Body, Tokens: tokens, ConfirmedTeamSnapshots: confirmations, IdempotencyKey: body.IdempotencyKey, Source: source(request)}
		if kind == mentions.SourceComment {
			result, err = r.dependencies.Collaboration.CreateComment(request.Context(), command)
		} else {
			result, err = r.dependencies.Collaboration.CreateNote(request.Context(), command)
		}
	}
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	status := http.StatusOK
	if result.Version == 1 {
		status = http.StatusCreated
	}
	writeJSON(writer, status, collaborationResponse(result, false, false))
}

func (r *Router) editInternalContent(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Collaboration == nil {
		writeError(request.Context(), writer, http.StatusServiceUnavailable, "service_unavailable", "internal collaboration is unavailable")
		return
	}
	var body internalContentEditRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	parentType, kind, valid := parseParentAndSourceKind(body.ParentType, body.SourceKind)
	tokens, confirmations, contentValid := internalMutationValues(body.Tokens, body.ConfirmedTeamSnapshots)
	if !valid || !contentValid || !validRequestUUID(body.ParentID) || !validRequestUUID(request.PathValue("source_id")) || strings.TrimSpace(body.IdempotencyKey) == "" || body.ExpectedVersion < 1 {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "mention_token_invalid", "internal content request is invalid")
		return
	}
	result, err := r.dependencies.Collaboration.Edit(request.Context(), collaboration.UpsertCommand{Principal: principal, Parent: collaboration.ParentRef{Type: parentType, ID: body.ParentID}, SourceID: request.PathValue("source_id"), Kind: kind, Body: body.Body, Tokens: tokens, ConfirmedTeamSnapshots: confirmations, ExpectedVersion: body.ExpectedVersion, IdempotencyKey: body.IdempotencyKey, Source: source(request)})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, collaborationResponse(result, false, false))
}

func (r *Router) redactInternalContent(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Collaboration == nil {
		writeError(request.Context(), writer, http.StatusServiceUnavailable, "service_unavailable", "internal collaboration is unavailable")
		return
	}
	var body internalContentRedactRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	parentType, kind, valid := parseParentAndSourceKind(body.ParentType, body.SourceKind)
	if !valid || !validRequestUUID(body.ParentID) || !validRequestUUID(request.PathValue("source_id")) || strings.TrimSpace(body.IdempotencyKey) == "" || body.ExpectedVersion < 1 {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "mention_token_invalid", "internal content request is invalid")
		return
	}
	result, err := r.dependencies.Collaboration.Redact(request.Context(), collaboration.RedactCommand{Principal: principal, Parent: collaboration.ParentRef{Type: parentType, ID: body.ParentID}, SourceID: request.PathValue("source_id"), Kind: kind, ExpectedVersion: body.ExpectedVersion, IdempotencyKey: body.IdempotencyKey, Source: source(request)})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, collaborationResponse(result, false, false))
}

func internalMutationValues(tokenValues *[]mentionTokenDTO, confirmationValues *map[string]teamConfirmation) ([]mentions.Token, map[string]mentions.TeamConfirmation, bool) {
	if tokenValues == nil || confirmationValues == nil {
		return nil, nil, false
	}
	tokens := make([]mentions.Token, 0, len(*tokenValues))
	for _, token := range *tokenValues {
		targetType := mentions.TargetType(token.TargetType)
		if !validRequestUUID(token.ID) || !validRequestUUID(token.TargetID) || token.Label == "" || token.Start < 0 || token.End <= token.Start || (targetType != mentions.TargetStaff && targetType != mentions.TargetTeam) {
			return nil, nil, false
		}
		tokens = append(tokens, mentions.Token{ID: token.ID, TargetType: targetType, TargetID: token.TargetID, Label: token.Label, Start: token.Start, End: token.End})
	}
	confirmations := make(map[string]mentions.TeamConfirmation, len(*confirmationValues))
	for teamID, confirmation := range *confirmationValues {
		if !validRequestUUID(teamID) || confirmation.TeamVersion < 1 || confirmation.EligibleMemberIDs == nil {
			return nil, nil, false
		}
		for _, memberID := range confirmation.EligibleMemberIDs {
			if !validRequestUUID(memberID) {
				return nil, nil, false
			}
		}
		confirmations[teamID] = mentions.TeamConfirmation{TeamVersion: confirmation.TeamVersion, EligibleMemberIDs: append([]string{}, confirmation.EligibleMemberIDs...)}
	}
	return tokens, confirmations, true
}

func optionalMentionTokens(values []mentionTokenDTO) ([]mentions.Token, bool) {
	copyValues := append([]mentionTokenDTO{}, values...)
	emptyConfirmations := map[string]teamConfirmation{}
	returnTokens, _, valid := internalMutationValues(&copyValues, &emptyConfirmations)
	return returnTokens, valid
}

func optionalTeamConfirmations(values map[string]teamConfirmation) (map[string]mentions.TeamConfirmation, bool) {
	if values == nil {
		return map[string]mentions.TeamConfirmation{}, true
	}
	emptyTokens := []mentionTokenDTO{}
	_, confirmations, valid := internalMutationValues(&emptyTokens, &values)
	return confirmations, valid
}

func parseParentAndSourceKind(parent, sourceKind string) (mentions.ParentType, mentions.SourceKind, bool) {
	parentType := mentions.ParentType(parent)
	kind := mentions.SourceKind(sourceKind)
	validParent := parentType == mentions.ParentWorkRecord || parentType == mentions.ParentTask || parentType == mentions.ParentProject
	validKind := kind == mentions.SourceDetails || kind == mentions.SourceComment || kind == mentions.SourceNote
	return parentType, kind, validParent && validKind
}

func collaborationResponse(source collaboration.Source, readOnly, legacy bool) internalContentResponse {
	tokens := source.Tokens
	if tokens == nil {
		tokens = []mentions.Token{}
	}
	return internalContentResponse{ID: source.ID, ParentType: source.Parent.Type, ParentID: source.Parent.ID, SourceKind: source.Kind, Body: source.Body, Tokens: tokens, AuthorID: source.AuthorID, LifecycleState: source.LifecycleState, Version: source.Version, CreatedAt: source.CreatedAt, UpdatedAt: source.UpdatedAt, RedactedAt: source.RedactedAt, ReadOnly: readOnly, Legacy: legacy}
}

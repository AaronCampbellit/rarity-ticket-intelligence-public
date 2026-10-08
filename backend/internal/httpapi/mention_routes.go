package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mentions"
)

type MentionActions interface {
	ListCandidates(context.Context, mentions.CandidateQuery) ([]mentions.Candidate, error)
	ListWidget(context.Context, mentions.WidgetQuery) (mentions.WidgetPage, error)
	ChangeState(context.Context, mentions.StateChange) (mentions.Item, error)
	ResolveDeepLink(context.Context, mentions.DeepLinkQuery) (mentions.DeepLink, error)
}

type mentionItemStateRequest struct {
	State           string `json:"state"`
	ExpectedVersion int64  `json:"expected_version"`
}

type mentionResolveRequest struct {
	ItemID          string `json:"item_id"`
	ExpectedVersion int64  `json:"expected_version"`
}

func (r *Router) registerMentionRoutes() {
	r.mux.HandleFunc("GET /api/v1/mentions/candidates", r.listMentionCandidates)
	r.mux.HandleFunc("GET /api/v1/mentions/widget", r.listMentionWidget)
	r.mux.HandleFunc("PATCH /api/v1/mentions/items/{id}", r.changeMentionItemState)
	r.mux.HandleFunc("POST /api/v1/mentions/occurrences/{id}/resolve", r.resolveMentionOccurrence)
}

func (r *Router) listMentionCandidates(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Mentions == nil {
		writeError(request.Context(), writer, http.StatusServiceUnavailable, "service_unavailable", "mentions are unavailable")
		return
	}
	parentType, sourceKind, valid := parseParentAndSourceKind(request.URL.Query().Get("parent_type"), request.URL.Query().Get("source_kind"))
	parentID := strings.TrimSpace(request.URL.Query().Get("parent_id"))
	search := strings.TrimSpace(request.URL.Query().Get("q"))
	exactType := mentions.TargetType(strings.TrimSpace(request.URL.Query().Get("target_type")))
	exactID := strings.TrimSpace(request.URL.Query().Get("target_id"))
	exactTypeSet, exactIDSet := exactType != "", exactID != ""
	if !valid || !validRequestUUID(parentID) || exactTypeSet != exactIDSet ||
		(exactIDSet && !validRequestUUID(exactID)) ||
		(exactTypeSet && (exactType != mentions.TargetTeam || search != "")) {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "mention candidate query is invalid")
		return
	}
	values, err := r.dependencies.Mentions.ListCandidates(request.Context(), mentions.CandidateQuery{AuthorID: principal.ID, Source: mentions.SourceRef{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID, ParentType: parentType, ParentID: parentID, SourceKind: sourceKind}, Search: search, ExactTargetType: exactType, ExactTargetID: exactID})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	if values == nil {
		values = []mentions.Candidate{}
	}
	writeJSON(writer, http.StatusOK, values)
}

func (r *Router) listMentionWidget(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Mentions == nil {
		writeError(request.Context(), writer, http.StatusServiceUnavailable, "service_unavailable", "mentions are unavailable")
		return
	}
	state := mentions.ItemState(request.URL.Query().Get("state"))
	limit, err := strconv.Atoi(request.URL.Query().Get("limit"))
	if err != nil || limit < 1 || limit > 50 || (state != mentions.Unread && state != mentions.Read && state != mentions.Archived) {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "mention widget query is invalid")
		return
	}
	page, err := r.dependencies.Mentions.ListWidget(request.Context(), mentions.WidgetQuery{Principal: principal, State: state, Cursor: request.URL.Query().Get("cursor"), Limit: limit})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	if page.Items == nil {
		page.Items = []mentions.WidgetItem{}
	}
	writeJSON(writer, http.StatusOK, page)
}

func (r *Router) changeMentionItemState(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Mentions == nil {
		writeError(request.Context(), writer, http.StatusServiceUnavailable, "service_unavailable", "mentions are unavailable")
		return
	}
	var body mentionItemStateRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	state := mentions.ItemState(body.State)
	if !validRequestUUID(request.PathValue("id")) || body.ExpectedVersion < 1 || (state != mentions.Unread && state != mentions.Read && state != mentions.Archived) {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "mention state request is invalid")
		return
	}
	item, err := r.dependencies.Mentions.ChangeState(request.Context(), mentions.StateChange{Principal: principal, ItemID: request.PathValue("id"), State: state, ExpectedVersion: body.ExpectedVersion})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, item.Version)
	writeJSON(writer, http.StatusOK, item)
}

func (r *Router) resolveMentionOccurrence(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Mentions == nil {
		writeError(request.Context(), writer, http.StatusServiceUnavailable, "service_unavailable", "mentions are unavailable")
		return
	}
	var body mentionResolveRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	if !validRequestUUID(request.PathValue("id")) || !validRequestUUID(body.ItemID) || body.ExpectedVersion < 1 {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "mention deep-link request is invalid")
		return
	}
	link, err := r.dependencies.Mentions.ResolveDeepLink(request.Context(), mentions.DeepLinkQuery{Principal: principal, ItemID: body.ItemID, OccurrenceID: request.PathValue("id"), ExpectedVersion: body.ExpectedVersion})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, link.ItemVersion)
	writeJSON(writer, http.StatusOK, link)
}

package httpapi

import (
	"net/http"
	"strconv"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

type createKnowledgeDraftRequest struct {
	DisplayID string   `json:"display_id"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	TagIDs    []string `json:"tag_ids"`
}

type reviseKnowledgeDraftRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Title           string `json:"title"`
	Body            string `json:"body"`
}

type publishKnowledgeRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

func (r *Router) registerKnowledgeRoutes() {
	r.mux.HandleFunc("GET /api/v1/knowledge/articles", r.listKnowledge)
	r.mux.HandleFunc("POST /api/v1/knowledge/articles", r.createKnowledgeDraft)
	r.mux.HandleFunc(
		"POST /api/v1/knowledge/articles/{id}/versions",
		r.reviseKnowledgeDraft,
	)
	r.mux.HandleFunc(
		"POST /api/v1/knowledge/articles/{id}/publish",
		r.publishKnowledge,
	)
	r.mux.HandleFunc("GET /api/v1/knowledge/articles/{id}", r.findKnowledge)
}

func (r *Router) listKnowledge(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Knowledge == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "knowledge is unavailable")
		return
	}
	limit := 50
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	articles, err := r.dependencies.Knowledge.List(
		request.Context(),
		knowledge.ListCommand{
			Principal: principal, Target: targetFor(principal),
			Query: request.URL.Query().Get("q"), Limit: limit,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, articles)
}

func (r *Router) createKnowledgeDraft(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Knowledge == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "knowledge is unavailable")
		return
	}
	var body createKnowledgeDraftRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	detail, err := r.dependencies.Knowledge.CreateDraft(
		request.Context(),
		knowledge.CreateDraftCommand{
			Principal: principal, Target: targetFor(principal),
			DisplayID: body.DisplayID, Title: body.Title, Body: body.Body,
			ActorID: principal.ID, Source: source(request), TagIDs: body.TagIDs, ClassificationPolicy: tagging.CreationRequireMeaningful,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, detail.Article.CurrentVersion)
	writeJSON(writer, http.StatusCreated, detail)
}

func (r *Router) reviseKnowledgeDraft(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Knowledge == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "knowledge is unavailable")
		return
	}
	var body reviseKnowledgeDraftRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	detail, err := r.dependencies.Knowledge.ReviseDraft(
		request.Context(),
		knowledge.ReviseDraftCommand{
			Principal: principal, Target: targetFor(principal),
			ArticleID:       request.PathValue("id"),
			ExpectedVersion: body.ExpectedVersion,
			Title:           body.Title, Body: body.Body,
			ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, detail.Article.CurrentVersion)
	writeJSON(writer, http.StatusCreated, detail)
}

func (r *Router) publishKnowledge(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Knowledge == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "knowledge is unavailable")
		return
	}
	var body publishKnowledgeRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	version, err := r.dependencies.Knowledge.Publish(
		request.Context(),
		knowledge.PublishCommand{
			Principal: principal, Target: targetFor(principal),
			ArticleID:       request.PathValue("id"),
			ExpectedVersion: body.ExpectedVersion,
			ActorID:         principal.ID, Source: source(request), Reason: body.Reason,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, version.Version)
	writeJSON(writer, http.StatusOK, version)
}

func (r *Router) findKnowledge(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Knowledge == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "knowledge is unavailable")
		return
	}
	detail, err := r.dependencies.Knowledge.Find(
		request.Context(),
		knowledge.FindCommand{
			Principal: principal, Target: targetFor(principal),
			ArticleID: request.PathValue("id"),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, detail.Article.CurrentVersion)
	writeJSON(writer, http.StatusOK, detail)
}

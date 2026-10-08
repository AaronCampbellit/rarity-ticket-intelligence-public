package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

const taggingRequestLimitBytes int64 = 1 << 20

type createTagGroupRequest struct {
	Label       string `json:"label"`
	Description string `json:"description"`
	Position    int    `json:"position"`
}
type updateTagGroupRequest struct {
	Label           string        `json:"label"`
	Description     string        `json:"description"`
	Position        int           `json:"position"`
	State           tagging.State `json:"state"`
	ExpectedVersion *int64        `json:"expected_version"`
}
type createTagRequest struct {
	GroupID     string   `json:"group_id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Color       string   `json:"color"`
	Synonyms    []string `json:"synonyms"`
}
type updateTagRequest struct {
	GroupID         string   `json:"group_id"`
	Label           string   `json:"label"`
	Description     string   `json:"description"`
	Color           string   `json:"color"`
	Synonyms        []string `json:"synonyms"`
	ExpectedVersion *int64   `json:"expected_version"`
}
type mergeTagRequest struct {
	SurvivorTagID   string `json:"survivor_tag_id"`
	Reason          string `json:"reason"`
	ExpectedVersion *int64 `json:"expected_version"`
}
type archiveTagRequest struct {
	ReplacementTagID string `json:"replacement_tag_id"`
	Reason           string `json:"reason"`
	ExpectedVersion  *int64 `json:"expected_version"`
}
type replaceObjectTagsRequest struct {
	TagIDs          []string `json:"tag_ids"`
	Reason          string   `json:"reason"`
	IdempotencyKey  string   `json:"idempotency_key"`
	ExpectedVersion *int64   `json:"expected_version"`
}
type bulkTagActionRequest struct {
	Items []bulkTagItemRequest `json:"items"`
}
type bulkTagItemRequest struct {
	ObjectType      tagging.ObjectType `json:"object_type"`
	ObjectID        string             `json:"object_id"`
	TagIDs          []string           `json:"tag_ids"`
	Reason          string             `json:"reason"`
	IdempotencyKey  string             `json:"idempotency_key"`
	ExpectedVersion int64              `json:"expected_version"`
}

type bulkTagActionResult struct {
	Target tagging.TargetRef     `json:"target"`
	Object *tagging.TaggedObject `json:"object,omitempty"`
	Error  *ErrorDetail          `json:"error,omitempty"`
}

type bulkTagActionResponse struct {
	Results []bulkTagActionResult `json:"results"`
}

type classificationAIPolicyRequest struct {
	AutomaticApplyEnabled   bool    `json:"automatic_apply_enabled"`
	AutomaticApplyThreshold float64 `json:"automatic_apply_threshold"`
	ModelProfileID          string  `json:"model_profile_id"`
	ExpectedVersion         *int64  `json:"expected_version"`
}
type classificationSuggestionDecisionRequest struct {
	TagID    string `json:"tag_id"`
	Decision string `json:"decision"`
}

func (r *Router) registerTaggingRoutes() {
	r.mux.HandleFunc("GET /api/v1/tag-groups", r.listTagGroups)
	r.mux.HandleFunc("POST /api/v1/tag-groups", r.createTagGroup)
	r.mux.HandleFunc("PATCH /api/v1/tag-groups/{id}", r.updateTagGroup)
	r.mux.HandleFunc("GET /api/v1/tags", r.listTags)
	r.mux.HandleFunc("POST /api/v1/tags", r.createTag)
	r.mux.HandleFunc("PATCH /api/v1/tags/{id}", r.updateTag)
	r.mux.HandleFunc("GET /api/v1/tags/{id}/impact", r.tagImpact)
	r.mux.HandleFunc("POST /api/v1/tags/{id}/merge", r.mergeTag)
	r.mux.HandleFunc("POST /api/v1/tags/{id}/archive", r.archiveTag)
	r.mux.HandleFunc("GET /api/v1/classification/health", r.classificationHealth)
	r.mux.HandleFunc("GET /api/v1/classification/migration-runs", r.classificationMigrationRuns)
	r.mux.HandleFunc("GET /api/v1/classification/ai-policy", r.classificationAIPolicy)
	r.mux.HandleFunc("PATCH /api/v1/classification/ai-policy", r.updateClassificationAIPolicy)
	r.mux.HandleFunc("POST /api/v1/objects/{object_type}/{id}/classification-suggestions", r.requestClassificationSuggestion)
	r.mux.HandleFunc("GET /api/v1/classification-suggestions/{id}", r.getClassificationSuggestion)
	r.mux.HandleFunc("POST /api/v1/classification-suggestions/{id}/decide", r.decideClassificationSuggestion)
	r.mux.HandleFunc("GET /api/v1/objects/{object_type}/{id}/tags", r.getObjectTags)
	r.mux.HandleFunc("GET /api/v1/objects/{object_type}/{id}/tag-history", r.objectTagHistory)
	r.mux.HandleFunc("PUT /api/v1/objects/{object_type}/{id}/tags", r.replaceObjectTags)
	r.mux.HandleFunc("POST /api/v1/tag-bulk-actions", r.bulkTagActions)
	for _, kind := range []tagging.ReportKind{tagging.ReportUsage, tagging.ReportCombinations, tagging.ReportTrends, tagging.ReportHealth, tagging.ReportRecurring} {
		kind := kind
		r.mux.HandleFunc("GET /api/v1/tag-reports/"+string(kind), func(writer http.ResponseWriter, request *http.Request) { r.tagReport(writer, request, kind) })
	}
	r.mux.HandleFunc("GET /api/v1/tag-reports/recurring-issues/{tag_id}/evidence", r.tagReportEvidence)
	r.mux.HandleFunc("GET /api/v1/tag-reports/options/technicians", func(w http.ResponseWriter, q *http.Request) {
		p, ok := r.principal(q)
		if !ok {
			writeError(q.Context(), w, http.StatusUnauthorized, "unauthenticated", "authentication required")
			return
		}
		if r.dependencies.TagReports == nil {
			writeError(q.Context(), w, http.StatusNotImplemented, "not_implemented", "tag reporting is unavailable")
			return
		}
		values, err := r.dependencies.TagReports.Technicians(q.Context(), p)
		if err != nil {
			writeDomainError(w, q, err)
			return
		}
		writeJSON(w, http.StatusOK, values)
	})
}

func (r *Router) tagReportEvidence(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.TagReports == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "classification reports are unavailable")
		return
	}
	query := request.URL.Query()
	from, fromErr := time.Parse("2006-01-02", query.Get("from"))
	to, toErr := time.Parse("2006-01-02", query.Get("to"))
	limit := 50
	var limitErr error
	if query.Get("limit") != "" {
		limit, limitErr = strconv.Atoi(query.Get("limit"))
	}
	tagIDs := []string{}
	for _, value := range query["tag_ids"] {
		for _, tagID := range strings.Split(value, ",") {
			if tagID = strings.TrimSpace(tagID); tagID != "" {
				tagIDs = append(tagIDs, tagID)
			}
		}
	}
	filter := tagging.ReportFilter{ClientID: principal.Scope.ClientID, ObjectType: tagging.ObjectType(query.Get("object_type")), GroupID: query.Get("group_id"), TagIDs: tagIDs, Match: query.Get("match"), Source: tagging.Source(query.Get("source")), Inheritance: query.Get("inheritance"), TechnicianID: query.Get("technician_id"), TeamID: query.Get("team_id"), Priority: query.Get("priority"), Status: query.Get("status"), From: from, To: to, Cursor: query.Get("cursor"), Limit: limit}
	if fromErr != nil || toErr != nil || limitErr != nil || tagging.ValidateReportFilter(filter) != nil || filter.ObjectType == "" {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "classification evidence filters are invalid")
		return
	}
	result, err := r.dependencies.TagReports.Evidence(request.Context(), principal, request.PathValue("tag_id"), filter)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) tagReport(writer http.ResponseWriter, request *http.Request, kind tagging.ReportKind) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.TagReports == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "classification reports are unavailable")
		return
	}
	if err := authorization.Authorize(principal, "classification.report", targetFor(principal)); err != nil {
		writeDomainError(writer, request, err)
		return
	}
	query := request.URL.Query()
	from, fromErr := time.Parse("2006-01-02", query.Get("from"))
	to, toErr := time.Parse("2006-01-02", query.Get("to"))
	limit := 50
	var limitErr error
	if query.Get("limit") != "" {
		limit, limitErr = strconv.Atoi(query.Get("limit"))
	}
	tagIDs := []string{}
	for _, value := range query["tag_ids"] {
		for _, id := range strings.Split(value, ",") {
			if id = strings.TrimSpace(id); id != "" {
				tagIDs = append(tagIDs, id)
			}
		}
	}
	filter := tagging.ReportFilter{
		ClientID: principal.Scope.ClientID, ObjectType: tagging.ObjectType(query.Get("object_type")), GroupID: query.Get("group_id"),
		TagIDs: tagIDs, Match: query.Get("match"), Source: tagging.Source(query.Get("source")), Inheritance: query.Get("inheritance"),
		TechnicianID: query.Get("technician_id"), TeamID: query.Get("team_id"), Priority: query.Get("priority"), Status: query.Get("status"),
		From: from, To: to, Cursor: query.Get("cursor"), Limit: limit,
	}
	if fromErr != nil || toErr != nil || limitErr != nil || tagging.ValidateReportFilter(filter) != nil {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "classification report filters are invalid")
		return
	}
	result, err := r.dependencies.TagReports.Query(request.Context(), principal, kind, filter)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) classificationSuggestionPrincipal(writer http.ResponseWriter, request *http.Request) (authorization.Principal, bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return authorization.Principal{}, false
	}
	if r.dependencies.TagClassificationSuggestions == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "classification suggestions are unavailable")
		return authorization.Principal{}, false
	}
	if err := authorization.Authorize(principal, "classification.apply", scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID}); err != nil {
		writeDomainError(writer, request, err)
		return authorization.Principal{}, false
	}
	return principal, true
}
func (r *Router) requestClassificationSuggestion(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.classificationSuggestionPrincipal(writer, request)
	if !ok {
		return
	}
	result, err := r.dependencies.TagClassificationSuggestions.Request(request.Context(), principal, taggingTarget(principal, request))
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusAccepted, result)
}
func (r *Router) getClassificationSuggestion(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.classificationSuggestionPrincipal(writer, request)
	if !ok {
		return
	}
	result, err := r.dependencies.TagClassificationSuggestions.GetByID(request.Context(), principal, request.PathValue("id"))
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}
func (r *Router) decideClassificationSuggestion(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.classificationSuggestionPrincipal(writer, request)
	if !ok {
		return
	}
	var body classificationSuggestionDecisionRequest
	if !decodeTaggingRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.TagClassificationSuggestions.DecideByID(request.Context(), principal, request.PathValue("id"), body.TagID, body.Decision)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) classificationAIPrincipal(writer http.ResponseWriter, request *http.Request) (authorization.Principal, bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return authorization.Principal{}, false
	}
	if r.dependencies.TagClassification == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "classification AI is unavailable")
		return authorization.Principal{}, false
	}
	if err := authorization.Authorize(principal, "classification.ai.manage", scope.Target{MSPID: principal.Scope.MSPID}); err != nil {
		writeDomainError(writer, request, err)
		return authorization.Principal{}, false
	}
	return principal, true
}

func (r *Router) classificationAIPolicy(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.classificationAIPrincipal(writer, request)
	if !ok {
		return
	}
	policy, err := r.dependencies.TagClassification.Policy(request.Context(), principal)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, policy.Version)
	writeJSON(writer, http.StatusOK, policy)
}

func (r *Router) updateClassificationAIPolicy(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.classificationAIPrincipal(writer, request)
	if !ok {
		return
	}
	var body classificationAIPolicyRequest
	if !decodeTaggingRequest(writer, request, &body) {
		return
	}
	version, ok := taggingExpectedVersion(writer, request, body.ExpectedVersion)
	if !ok {
		return
	}
	policy, err := r.dependencies.TagClassification.UpdatePolicy(request.Context(), tagging.UpdateClassificationPolicyCommand{
		Principal: principal, Enabled: body.AutomaticApplyEnabled, Threshold: body.AutomaticApplyThreshold,
		ModelProfileID: body.ModelProfileID, ExpectedVersion: version,
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, policy.Version)
	writeJSON(writer, http.StatusOK, policy)
}

func (r *Router) tagCatalogPrincipal(writer http.ResponseWriter, request *http.Request, manage bool) (authorization.Principal, bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return authorization.Principal{}, false
	}
	if r.dependencies.TagCatalog == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "classification catalog is unavailable")
		return authorization.Principal{}, false
	}
	capability := "classification.apply"
	if manage {
		capability = "classification.manage"
	} else if principal.Capabilities.Has("classification.manage") {
		capability = "classification.manage"
	}
	if err := authorization.Authorize(principal, capability, scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID}); err != nil {
		writeDomainError(writer, request, err)
		return authorization.Principal{}, false
	}
	return principal, true
}

func (r *Router) tagAssociationPrincipal(writer http.ResponseWriter, request *http.Request) (authorization.Principal, bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return authorization.Principal{}, false
	}
	if r.dependencies.TagAssociations == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "classification associations are unavailable")
		return authorization.Principal{}, false
	}
	if err := authorization.Authorize(principal, "classification.apply", scope.Target{
		MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
	}); err != nil {
		writeDomainError(writer, request, err)
		return authorization.Principal{}, false
	}
	return principal, true
}

func (r *Router) listTagGroups(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.tagCatalogPrincipal(writer, request, false)
	if !ok {
		return
	}
	catalog, err := r.dependencies.TagCatalog.List(request.Context(), tagging.ListCatalogCommand{Principal: principal})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, catalog.Groups)
}
func (r *Router) listTags(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.tagCatalogPrincipal(writer, request, false)
	if !ok {
		return
	}
	catalog, err := r.dependencies.TagCatalog.List(request.Context(), tagging.ListCatalogCommand{Principal: principal})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, catalog.Tags)
}
func (r *Router) createTagGroup(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.tagCatalogPrincipal(writer, request, true)
	if !ok {
		return
	}
	var body createTagGroupRequest
	if !decodeTaggingRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.TagCatalog.CreateGroup(request.Context(), tagging.CreateGroupCommand{Principal: principal, Label: body.Label, Description: body.Description, Position: body.Position})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}
func (r *Router) updateTagGroup(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.tagCatalogPrincipal(writer, request, true)
	if !ok {
		return
	}
	var body updateTagGroupRequest
	if !decodeTaggingRequest(writer, request, &body) {
		return
	}
	version, ok := taggingExpectedVersion(writer, request, body.ExpectedVersion)
	if !ok {
		return
	}
	result, err := r.dependencies.TagCatalog.UpdateGroup(request.Context(), tagging.UpdateGroupCommand{Principal: principal, ID: request.PathValue("id"), Label: body.Label, Description: body.Description, Position: body.Position, State: body.State, ExpectedVersion: version})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}
func (r *Router) createTag(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.tagCatalogPrincipal(writer, request, true)
	if !ok {
		return
	}
	var body createTagRequest
	if !decodeTaggingRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.TagCatalog.CreateTag(request.Context(), tagging.CreateTagCommand{Principal: principal, GroupID: body.GroupID, Label: body.Label, Description: body.Description, Color: body.Color, Synonyms: body.Synonyms})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}
func (r *Router) updateTag(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.tagCatalogPrincipal(writer, request, true)
	if !ok {
		return
	}
	var body updateTagRequest
	if !decodeTaggingRequest(writer, request, &body) {
		return
	}
	version, ok := taggingExpectedVersion(writer, request, body.ExpectedVersion)
	if !ok {
		return
	}
	result, err := r.dependencies.TagCatalog.UpdateTag(request.Context(), tagging.UpdateTagCommand{Principal: principal, ID: request.PathValue("id"), GroupID: body.GroupID, Label: body.Label, Description: body.Description, Color: body.Color, Synonyms: body.Synonyms, ExpectedVersion: version})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}
func (r *Router) tagImpact(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.tagCatalogPrincipal(writer, request, true)
	if !ok {
		return
	}
	query := request.URL.Query()
	result, err := r.dependencies.TagCatalog.PreviewImpact(request.Context(), tagging.ImpactCommand{Principal: principal, TagID: request.PathValue("id"), Operation: tagging.ImpactOperation(query.Get("operation")), ReplacementTagID: query.Get("replacement_tag_id")})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}
func (r *Router) mergeTag(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.tagCatalogPrincipal(writer, request, true)
	if !ok {
		return
	}
	var body mergeTagRequest
	if !decodeTaggingRequest(writer, request, &body) {
		return
	}
	if strings.TrimSpace(body.Reason) == "" {
		writeDomainError(writer, request, tagging.ErrReasonRequired)
		return
	}
	version, ok := taggingExpectedVersion(writer, request, body.ExpectedVersion)
	if !ok {
		return
	}
	result, err := r.dependencies.TagCatalog.Merge(request.Context(), tagging.MergeCommand{Principal: principal, TagID: request.PathValue("id"), SurvivorTagID: body.SurvivorTagID, ExpectedVersion: version, Reason: body.Reason})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}
func (r *Router) archiveTag(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.tagCatalogPrincipal(writer, request, true)
	if !ok {
		return
	}
	var body archiveTagRequest
	if !decodeTaggingRequest(writer, request, &body) {
		return
	}
	if strings.TrimSpace(body.Reason) == "" {
		writeDomainError(writer, request, tagging.ErrReasonRequired)
		return
	}
	version, ok := taggingExpectedVersion(writer, request, body.ExpectedVersion)
	if !ok {
		return
	}
	result, err := r.dependencies.TagCatalog.Archive(request.Context(), tagging.ArchiveCommand{Principal: principal, TagID: request.PathValue("id"), ReplacementTagID: body.ReplacementTagID, ExpectedVersion: version, Reason: body.Reason})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}
func (r *Router) classificationHealth(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.tagCatalogPrincipal(writer, request, true)
	if !ok {
		return
	}
	result, err := r.dependencies.TagCatalog.Health(request.Context(), tagging.HealthCommand{Principal: principal, Target: targetFor(principal)})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}
func (r *Router) classificationMigrationRuns(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.tagCatalogPrincipal(writer, request, true)
	if !ok {
		return
	}
	result, err := r.dependencies.TagCatalog.MigrationHistory(request.Context(), tagging.MigrationHistoryCommand{Principal: principal})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}
func (r *Router) getObjectTags(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.tagAssociationPrincipal(writer, request)
	if !ok {
		return
	}
	result, err := r.dependencies.TagAssociations.Get(request.Context(), tagging.GetCommand{Principal: principal, Target: taggingTarget(principal, request)})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.ObjectVersion)
	writeJSON(writer, http.StatusOK, result)
}
func (r *Router) objectTagHistory(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.tagAssociationPrincipal(writer, request)
	if !ok {
		return
	}
	result, err := r.dependencies.TagAssociations.History(request.Context(), tagging.HistoryCommand{Principal: principal, Target: taggingTarget(principal, request)})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}
func (r *Router) replaceObjectTags(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.tagAssociationPrincipal(writer, request)
	if !ok {
		return
	}
	var body replaceObjectTagsRequest
	if !decodeTaggingRequest(writer, request, &body) {
		return
	}
	version, ok := taggingExpectedVersion(writer, request, body.ExpectedVersion)
	if !ok {
		return
	}
	result, err := r.dependencies.TagAssociations.ReplaceDirect(request.Context(), tagging.ReplaceCommand{Principal: principal, Target: taggingTarget(principal, request), ExpectedObjectVersion: version, TagIDs: body.TagIDs, Source: tagging.SourceHuman, Reason: body.Reason, IdempotencyKey: body.IdempotencyKey, CorrelationID: serverTaggingCorrelationID(), CausationID: ""})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.ObjectVersion)
	writeJSON(writer, http.StatusOK, result)
}
func (r *Router) bulkTagActions(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.tagAssociationPrincipal(writer, request)
	if !ok {
		return
	}
	var body bulkTagActionRequest
	if !decodeTaggingRequest(writer, request, &body) {
		return
	}
	correlationID := serverTaggingCorrelationID()
	items := make([]tagging.ReplaceCommand, 0, len(body.Items))
	for _, item := range body.Items {
		items = append(items, tagging.ReplaceCommand{Principal: principal, Target: tagging.TargetRef{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID, ObjectType: item.ObjectType, ObjectID: item.ObjectID}, ExpectedObjectVersion: item.ExpectedVersion, TagIDs: item.TagIDs, Source: tagging.SourceHuman, Reason: item.Reason, IdempotencyKey: item.IdempotencyKey, CorrelationID: correlationID, CausationID: ""})
	}
	result, err := r.dependencies.TagAssociations.Bulk(request.Context(), tagging.BulkCommand{Principal: principal, Items: items})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, bulkTagActionResponse{Results: bulkTagActionResults(result)})
}

func taggingTarget(principal authorization.Principal, request *http.Request) tagging.TargetRef {
	return tagging.TargetRef{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID, ObjectType: tagging.ObjectType(request.PathValue("object_type")), ObjectID: request.PathValue("id")}
}
func serverTaggingCorrelationID() string {
	return id.New()
}

func bulkTagActionResults(results []tagging.BulkResult) []bulkTagActionResult {
	response := make([]bulkTagActionResult, 0, len(results))
	for _, result := range results {
		item := bulkTagActionResult{Target: result.Target, Object: result.Object}
		if result.Cause != nil {
			detail, ok := taggingErrorDetail(result.Cause)
			if !ok {
				detail = ErrorDetail{Code: "internal_error", Message: "request could not be completed"}
			}
			item.Error = &detail
		}
		response = append(response, item)
	}
	return response
}

func taggingErrorDetail(err error) (ErrorDetail, bool) {
	switch {
	case errors.Is(err, scope.ErrNotFound):
		return ErrorDetail{Code: "not_found", Message: "resource not found"}, true
	case errors.Is(err, authorization.ErrForbidden):
		return ErrorDetail{Code: "forbidden", Message: "action is not permitted"}, true
	case errors.Is(err, object.ErrVersionConflict):
		return ErrorDetail{Code: "version_conflict", Message: "resource changed; refresh and retry"}, true
	case errors.Is(err, tagging.ErrMeaningfulTagRequired):
		return ErrorDetail{Code: "classification_required", Message: "a meaningful classification tag is required"}, true
	case errors.Is(err, tagging.ErrInactiveTarget):
		return ErrorDetail{Code: "tag_archived", Message: "classification tag is no longer active"}, true
	case errors.Is(err, tagging.ErrDuplicateTerm):
		return ErrorDetail{Code: "tag_ambiguous", Message: "classification tag label or synonym is ambiguous"}, true
	case errors.Is(err, tagging.ErrSystemManaged):
		return ErrorDetail{Code: "system_managed", Message: "system-managed classification cannot be changed"}, true
	default:
		return ErrorDetail{}, false
	}
}

func taggingExpectedVersion(writer http.ResponseWriter, request *http.Request, supplied *int64) (int64, bool) {
	version, present, err := parseExpectedVersionETag(request)
	if err != nil || !present {
		writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "If-Match is required and must be valid")
		return 0, false
	}
	if supplied != nil && (*supplied < 1 || *supplied != version) {
		writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "expected version does not match If-Match")
		return 0, false
	}
	return version, true
}

func decodeTaggingRequest(writer http.ResponseWriter, request *http.Request, target any) bool {
	contentType := strings.TrimSpace(request.Header.Get("Content-Type"))
	mediaType, _, err := mime.ParseMediaType(contentType)
	if contentType == "" || err != nil || !strings.EqualFold(mediaType, "application/json") {
		writeError(request.Context(), writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "request content type must be application/json")
		return false
	}
	body := http.MaxBytesReader(writer, request.Body, taggingRequestLimitBytes)
	raw, err := io.ReadAll(body)
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			writeError(request.Context(), writer, http.StatusRequestEntityTooLarge, "validation_failed", "request body is too large")
		} else {
			writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "request body is invalid")
		}
		return false
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "request body must contain one JSON object")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "request body is invalid")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "request body must contain one JSON value")
		return false
	}
	return true
}

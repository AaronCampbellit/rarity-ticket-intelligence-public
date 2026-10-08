package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

type taggingCatalogStub struct {
	catalog     tagging.Catalog
	group       tagging.Group
	tag         tagging.Tag
	err         error
	groupUpdate tagging.UpdateGroupCommand
	merge       tagging.MergeCommand
	method      string
}

func (s *taggingCatalogStub) List(context.Context, tagging.ListCatalogCommand) (tagging.Catalog, error) {
	s.method = "List"
	return s.catalog, s.err
}
func (s *taggingCatalogStub) Health(context.Context, tagging.HealthCommand) (tagging.Health, error) {
	s.method = "Health"
	return tagging.Health{}, s.err
}
func (s *taggingCatalogStub) MigrationHistory(context.Context, tagging.MigrationHistoryCommand) ([]tagging.MigrationRun, error) {
	s.method = "MigrationHistory"
	return nil, s.err
}
func (s *taggingCatalogStub) CreateGroup(context.Context, tagging.CreateGroupCommand) (tagging.Group, error) {
	s.method = "CreateGroup"
	return s.group, s.err
}
func (s *taggingCatalogStub) UpdateGroup(_ context.Context, command tagging.UpdateGroupCommand) (tagging.Group, error) {
	s.method = "UpdateGroup"
	s.groupUpdate = command
	return s.group, s.err
}
func (s *taggingCatalogStub) CreateTag(context.Context, tagging.CreateTagCommand) (tagging.Tag, error) {
	s.method = "CreateTag"
	return s.tag, s.err
}
func (s *taggingCatalogStub) UpdateTag(context.Context, tagging.UpdateTagCommand) (tagging.Tag, error) {
	s.method = "UpdateTag"
	return s.tag, s.err
}
func (s *taggingCatalogStub) PreviewImpact(context.Context, tagging.ImpactCommand) (tagging.Impact, error) {
	s.method = "PreviewImpact"
	return tagging.Impact{}, s.err
}
func (s *taggingCatalogStub) Merge(_ context.Context, command tagging.MergeCommand) (tagging.Tag, error) {
	s.method = "Merge"
	s.merge = command
	return s.tag, s.err
}
func (s *taggingCatalogStub) Archive(context.Context, tagging.ArchiveCommand) (tagging.Tag, error) {
	s.method = "Archive"
	return s.tag, s.err
}

type taggingAssociationStub struct {
	object  tagging.TaggedObject
	history []tagging.HistoryEntry
	err     error
	get     tagging.GetCommand
	replace tagging.ReplaceCommand
	bulk    tagging.BulkCommand
	results []tagging.BulkResult
	method  string
}

type classificationActionsStub struct {
	policy tagging.ClassificationPolicy
	err    error
}

func (s *classificationActionsStub) Policy(context.Context, authorization.Principal) (tagging.ClassificationPolicy, error) {
	return s.policy, s.err
}
func (s *classificationActionsStub) UpdatePolicy(context.Context, tagging.UpdateClassificationPolicyCommand) (tagging.ClassificationPolicy, error) {
	return s.policy, s.err
}

type classificationSuggestionActionsStub struct {
	record        tagging.ClassificationSuggestionRecord
	requestTarget tagging.TargetRef
	decision      string
}

func (s *classificationSuggestionActionsStub) Request(_ context.Context, _ authorization.Principal, target tagging.TargetRef) (tagging.ClassificationSuggestionRecord, error) {
	s.requestTarget = target
	return s.record, nil
}
func (s *classificationSuggestionActionsStub) GetByID(context.Context, authorization.Principal, string) (tagging.ClassificationSuggestionRecord, error) {
	return s.record, nil
}
func (s *classificationSuggestionActionsStub) DecideByID(_ context.Context, _ authorization.Principal, _ string, _ string, decision string) (tagging.ClassificationSuggestionRecord, error) {
	s.decision = decision
	return s.record, nil
}

type taggingReportActionsStub struct {
	kind   tagging.ReportKind
	filter tagging.ReportFilter
}

func (s *taggingReportActionsStub) Query(_ context.Context, _ authorization.Principal, kind tagging.ReportKind, filter tagging.ReportFilter) (tagging.Report, error) {
	s.kind, s.filter = kind, filter
	return tagging.Report{Kind: kind, Rows: []tagging.ReportRow{}, ProjectionAsOf: filter.To}, nil
}
func (s *taggingReportActionsStub) Evidence(context.Context, authorization.Principal, string, tagging.ReportFilter) (tagging.EvidencePage, error) {
	return tagging.EvidencePage{Items: []tagging.EvidenceRef{}}, nil
}
func (s *taggingReportActionsStub) Technicians(context.Context, authorization.Principal) ([]tagging.TechnicianOption, error) {
	return []tagging.TechnicianOption{}, nil
}

func TestTagReportRoutesRequireCapabilityAndParseBoundedFilters(t *testing.T) {
	reports := &taggingReportActionsStub{}
	handler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return taggingPrincipal("classification.report"), nil
		},
		TagReports: reports,
	})
	for _, kind := range []string{"usage", "combinations", "trends", "classification-health", "recurring-issues"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/tag-reports/"+kind+"?from=2026-08-01&to=2026-08-08&object_type=task&group_id=group&tag_ids=one,two&match=all&source=human&inheritance=direct&limit=25", nil)
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", kind, response.Code, response.Body.String())
		}
		if string(reports.kind) != kind || reports.filter.ClientID != "client-id" || reports.filter.ObjectType != tagging.ObjectTask || reports.filter.Match != "all" || reports.filter.Limit != 25 || len(reports.filter.TagIDs) != 2 {
			t.Fatalf("%s parsed kind=%s filter=%+v", kind, reports.kind, reports.filter)
		}
	}
}

func (s *taggingAssociationStub) Get(_ context.Context, command tagging.GetCommand) (tagging.TaggedObject, error) {
	s.method = "Get"
	s.get = command
	return s.object, s.err
}
func (s *taggingAssociationStub) History(context.Context, tagging.HistoryCommand) ([]tagging.HistoryEntry, error) {
	s.method = "History"
	return s.history, s.err
}
func (s *taggingAssociationStub) ReplaceDirect(_ context.Context, command tagging.ReplaceCommand) (tagging.TaggedObject, error) {
	s.method = "ReplaceDirect"
	s.replace = command
	return s.object, s.err
}
func (s *taggingAssociationStub) Bulk(_ context.Context, command tagging.BulkCommand) ([]tagging.BulkResult, error) {
	s.method = "Bulk"
	s.bulk = command
	return s.results, s.err
}

func TestTaggingAssociationRejectsForgedEvidenceHeadersAndSharesBulkCorrelation(t *testing.T) {
	associations := &taggingAssociationStub{object: tagging.TaggedObject{ObjectVersion: 2}}
	handler := taggingRouter(&taggingCatalogStub{}, associations, "classification.apply")
	request := httptest.NewRequest(http.MethodPut, "/api/v1/objects/work_record/work-id/tags", strings.NewReader(`{"tag_ids":["tag"],"reason":"reason","idempotency_key":"one"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"1"`)
	request.Header.Set("X-Correlation-ID", "forged-correlation")
	request.Header.Set("X-Causation-ID", "forged-causation")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if associations.replace.CorrelationID == "forged-correlation" || associations.replace.CausationID != "" || associations.replace.CorrelationID == "" {
		t.Fatalf("untrusted headers reached association command: %+v", associations.replace)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/tag-bulk-actions", strings.NewReader(`{"items":[{"object_type":"work_record","object_id":"one","tag_ids":["tag"],"reason":"reason","idempotency_key":"one","expected_version":1},{"object_type":"work_record","object_id":"two","tag_ids":["tag"],"reason":"reason","idempotency_key":"two","expected_version":1}]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Correlation-ID", "forged-correlation")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if len(associations.bulk.Items) != 2 || associations.bulk.Items[0].CorrelationID == "forged-correlation" || associations.bulk.Items[0].CorrelationID == "" || associations.bulk.Items[0].CorrelationID != associations.bulk.Items[1].CorrelationID || associations.bulk.Items[0].CausationID != "" || associations.bulk.Items[1].CausationID != "" {
		t.Fatalf("bulk evidence=%+v", associations.bulk.Items)
	}
}

func TestClassificationAIPolicyRouteRequiresDedicatedManagementCapability(t *testing.T) {
	actions := &classificationActionsStub{policy: tagging.ClassificationPolicy{Enabled: false, Threshold: .95, Version: 1}}
	handler := NewRouter(Dependencies{Principal: func(*http.Request) (authorization.Principal, error) {
		return authorization.Principal{ID: "tech-id", Scope: scope.Principal{MSPID: "msp-id"}, Capabilities: authorization.NewCapabilitySet("classification.apply")}, nil
	}, TagClassification: actions})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/classification/ai-policy", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/classification/ai-policy", nil)
	request.Header.Set("Authorization", "Bearer test")
	// A Client-scoped grant cannot govern the MSP-global policy.
	handler = NewRouter(Dependencies{Principal: func(*http.Request) (authorization.Principal, error) {
		return authorization.Principal{ID: "tech-id", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"}, Capabilities: authorization.NewCapabilitySet("classification.ai.manage")}, nil
	}, TagClassification: actions})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("Client-scoped status=%d body=%s", response.Code, response.Body.String())
	}
	// The same capability is valid only in the trusted MSP-global context.
	handler = NewRouter(Dependencies{Principal: func(*http.Request) (authorization.Principal, error) {
		return authorization.Principal{ID: "tech-id", Scope: scope.Principal{MSPID: "msp-id"}, Capabilities: authorization.NewCapabilitySet("classification.ai.manage")}, nil
	}, TagClassification: actions})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestClassificationSuggestionRoutesRequireApplyAndUseTrustedTargetScope(t *testing.T) {
	actions := &classificationSuggestionActionsStub{record: tagging.ClassificationSuggestionRecord{ID: "suggestion", Version: 1}}
	principal := func(*http.Request) (authorization.Principal, error) {
		return authorization.Principal{ID: "tech-id", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"}, Capabilities: authorization.NewCapabilitySet("classification.apply")}, nil
	}
	handler := NewRouter(Dependencies{Principal: principal, TagClassificationSuggestions: actions})
	for _, test := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodPost, "/api/v1/objects/task/task-id/classification-suggestions", "", http.StatusAccepted},
		{http.MethodGet, "/api/v1/classification-suggestions/suggestion", "", http.StatusOK},
		{http.MethodPost, "/api/v1/classification-suggestions/suggestion/decide", `{"tag_id":"tag","decision":"accepted"}`, http.StatusOK},
	} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, response.Code, response.Body.String())
		}
	}
	if actions.requestTarget.MSPID != "msp-id" || actions.requestTarget.ClientID != "client-id" || actions.requestTarget.ObjectType != tagging.ObjectTask || actions.decision != "accepted" {
		t.Fatalf("target=%+v decision=%q", actions.requestTarget, actions.decision)
	}
}

func TestTaggingAssociationRouteRequiresApplyAndHidesCrossClientTarget(t *testing.T) {
	for _, test := range []struct {
		name         string
		capabilities []string
		err          error
		want         int
		code         string
	}{
		{"missing_apply", []string{"work_record.read"}, nil, http.StatusForbidden, "forbidden"},
		{"cross_client", []string{"classification.apply"}, scope.ErrNotFound, http.StatusNotFound, "not_found"},
	} {
		t.Run(test.name, func(t *testing.T) {
			associations := &taggingAssociationStub{err: test.err}
			response := httptest.NewRecorder()
			taggingRouter(&taggingCatalogStub{}, associations, test.capabilities...).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/objects/work_record/other-client-object/tags", nil))
			var payload ErrorResponse
			_ = json.NewDecoder(response.Body).Decode(&payload)
			if response.Code != test.want || payload.Error.Code != test.code {
				t.Fatalf("status=%d error=%+v", response.Code, payload.Error)
			}
		})
	}
}

func TestTaggingRejectsInvalidOptionalBodyVersions(t *testing.T) {
	handler := taggingRouter(&taggingCatalogStub{group: tagging.Group{Version: 2}}, &taggingAssociationStub{}, "classification.manage")
	for _, body := range []string{
		`{"label":"Operations","position":1,"expected_version":0}`,
		`{"label":"Operations","position":1,"expected_version":-1}`,
		`{"label":"Operations","position":1,"expected_version":2}`,
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPatch, "/api/v1/tag-groups/group", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("If-Match", `"1"`)
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, response.Code, response.Body.String())
		}
	}
}

func TestTaggingMapsSystemManagedLifecycleErrors(t *testing.T) {
	for _, endpoint := range []struct{ method, path, body string }{
		{http.MethodPatch, "/api/v1/tag-groups/system", `{"label":"System","position":1}`},
		{http.MethodPatch, "/api/v1/tags/unclassified", `{"group_id":"system","label":"Unclassified"}`},
		{http.MethodPost, "/api/v1/tags/unclassified/merge", `{"survivor_tag_id":"other","reason":"reason"}`},
		{http.MethodPost, "/api/v1/tags/unclassified/archive", `{"reason":"reason"}`},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(endpoint.body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("If-Match", `"1"`)
		taggingRouter(&taggingCatalogStub{err: tagging.ErrSystemManaged}, &taggingAssociationStub{}, "classification.manage").ServeHTTP(response, request)
		var payload ErrorResponse
		_ = json.NewDecoder(response.Body).Decode(&payload)
		if response.Code < 400 || response.Code >= 500 || payload.Error.Code != "system_managed" {
			t.Fatalf("endpoint=%s status=%d error=%+v", endpoint.path, response.Code, payload.Error)
		}
	}
}

func TestTaggingBulkReturnsStructuredStableItemErrorsAndDerivedCommands(t *testing.T) {
	associations := &taggingAssociationStub{
		results: []tagging.BulkResult{
			{Target: tagging.TargetRef{ObjectID: "success"}, Object: &tagging.TaggedObject{ObjectVersion: 2}},
			{Target: tagging.TargetRef{ObjectID: "stale"}, Cause: object.ErrVersionConflict},
			{Target: tagging.TargetRef{ObjectID: "forbidden"}, Cause: authorization.ErrForbidden},
			{Target: tagging.TargetRef{ObjectID: "cross-client"}, Cause: scope.ErrNotFound},
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tag-bulk-actions", strings.NewReader(`{"items":[{"object_type":"work_record","object_id":"success","tag_ids":["tag"],"reason":"reason","idempotency_key":"success","expected_version":1},{"object_type":"work_record","object_id":"stale","tag_ids":["tag"],"reason":"reason","idempotency_key":"stale","expected_version":1},{"object_type":"work_record","object_id":"forbidden","tag_ids":["tag"],"reason":"reason","idempotency_key":"forbidden","expected_version":1},{"object_type":"work_record","object_id":"cross-client","tag_ids":["tag"],"reason":"reason","idempotency_key":"cross-client","expected_version":1}]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	taggingRouter(&taggingCatalogStub{}, associations, "classification.apply").ServeHTTP(response, request)
	var body struct {
		Results []struct {
			Error *ErrorDetail `json:"error"`
		} `json:"results"`
	}
	_ = json.NewDecoder(response.Body).Decode(&body)
	if response.Code != http.StatusOK || len(body.Results) != 4 || body.Results[0].Error != nil || body.Results[1].Error.Code != "version_conflict" || body.Results[2].Error.Code != "forbidden" || body.Results[3].Error.Code != "not_found" {
		t.Fatalf("status=%d body=%+v", response.Code, body)
	}
	if len(associations.bulk.Items) != 4 || associations.bulk.Items[0].Target.ClientID != "client-id" || associations.bulk.Items[0].Source != tagging.SourceHuman || associations.bulk.Items[0].CorrelationID == "" || associations.bulk.Items[0].CorrelationID != associations.bulk.Items[3].CorrelationID {
		t.Fatalf("derived bulk command=%+v", associations.bulk)
	}
}

func TestTaggingRegistersEveryGovernedRouteWithItsMethod(t *testing.T) {
	catalog := &taggingCatalogStub{}
	associations := &taggingAssociationStub{}
	handler := taggingRouter(catalog, associations, "classification.manage", "classification.apply")
	for _, route := range []struct{ method, path, body, action string }{
		{http.MethodGet, "/api/v1/tag-groups", "", "List"},
		{http.MethodPost, "/api/v1/tag-groups", `{}`, "CreateGroup"},
		{http.MethodPatch, "/api/v1/tag-groups/group", `{}`, "UpdateGroup"},
		{http.MethodGet, "/api/v1/tags", "", "List"},
		{http.MethodPost, "/api/v1/tags", `{}`, "CreateTag"},
		{http.MethodPatch, "/api/v1/tags/tag", `{}`, "UpdateTag"},
		{http.MethodGet, "/api/v1/tags/tag/impact?operation=merge", "", "PreviewImpact"},
		{http.MethodPost, "/api/v1/tags/tag/merge", `{"survivor_tag_id":"other","reason":"reason"}`, "Merge"},
		{http.MethodPost, "/api/v1/tags/tag/archive", `{"reason":"reason"}`, "Archive"},
		{http.MethodGet, "/api/v1/classification/health", "", "Health"},
		{http.MethodGet, "/api/v1/classification/migration-runs", "", "MigrationHistory"},
		{http.MethodGet, "/api/v1/objects/work_record/object/tags", "", "Get"},
		{http.MethodGet, "/api/v1/objects/work_record/object/tag-history", "", "History"},
		{http.MethodPut, "/api/v1/objects/work_record/object/tags", `{"tag_ids":["tag"],"reason":"reason","idempotency_key":"key"}`, "ReplaceDirect"},
		{http.MethodPost, "/api/v1/tag-bulk-actions", `{"items":[{"object_type":"work_record","object_id":"object","tag_ids":["tag"],"reason":"reason","idempotency_key":"key","expected_version":1}]}`, "Bulk"},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
		if route.body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		request.Header.Set("If-Match", `"1"`)
		handler.ServeHTTP(response, request)
		if response.Code == http.StatusNotFound || response.Code == http.StatusMethodNotAllowed {
			t.Fatalf("route %s %s was not mapped: status=%d", route.method, route.path, response.Code)
		}
		if catalog.method != route.action && associations.method != route.action {
			t.Fatalf("route %s %s invoked catalog=%q association=%q, want %q", route.method, route.path, catalog.method, associations.method, route.action)
		}
	}
}
func (s *taggingAssociationStub) RequireMeaningful(context.Context, tagging.GuardCommand) error {
	return s.err
}

func taggingPrincipal(capabilities ...string) authorization.Principal {
	return authorization.Principal{
		ID:           "actor-id",
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet(capabilities...),
	}
}

func taggingRouter(catalog *taggingCatalogStub, associations *taggingAssociationStub, capabilities ...string) http.Handler {
	return NewRouter(Dependencies{
		Principal:  func(*http.Request) (authorization.Principal, error) { return taggingPrincipal(capabilities...), nil },
		TagCatalog: catalog, TagAssociations: associations,
	})
}

func TestTaggingRejectsUnknownFieldsAndOversizedBodies(t *testing.T) {
	catalog := &taggingCatalogStub{}
	handler := taggingRouter(catalog, &taggingAssociationStub{}, "classification.manage")
	for name, body := range map[string]string{
		"unknown": `{"label":"Operations","position":1,"msp_id":"forged"}`,
		"large":   `{"label":"` + strings.Repeat("x", 1<<20) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/tag-groups", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			handler.ServeHTTP(response, request)
			want := http.StatusUnprocessableEntity
			if name == "large" {
				want = http.StatusRequestEntityTooLarge
			}
			if response.Code != want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestTaggingUsesETagsAndRejectsMismatchedIfMatch(t *testing.T) {
	catalog := &taggingCatalogStub{catalog: tagging.Catalog{Groups: []tagging.Group{{ID: "group-id", Version: 7}}}, group: tagging.Group{ID: "group-id", Version: 8}}
	handler := taggingRouter(catalog, &taggingAssociationStub{}, "classification.manage")
	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/tag-groups", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), "group-id") {
		t.Fatalf("status=%d body=%s", get.Code, get.Body.String())
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/tag-groups/group-id", strings.NewReader(`{"label":"Operations","position":2,"expected_version":7}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"7"`)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("ETag") != `"8"` || catalog.groupUpdate.ExpectedVersion != 7 {
		t.Fatalf("status=%d etag=%q command=%+v", response.Code, response.Header().Get("ETag"), catalog.groupUpdate)
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPatch, "/api/v1/tag-groups/group-id", strings.NewReader(`{"label":"Operations","position":2,"expected_version":7}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"6"`)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTaggingDerivesAssociationScopeAndPreservesInheritedResolution(t *testing.T) {
	assignments := &taggingAssociationStub{object: tagging.TaggedObject{
		Target:        tagging.TargetRef{MSPID: "msp-id", ClientID: "client-id", ObjectType: tagging.ObjectTask, ObjectID: "task-id"},
		ObjectVersion: 4,
		Inherited:     []tagging.Assignment{{Inherited: true, Tag: tagging.Tag{ID: "project-tag", State: tagging.StateActive}}},
		Effective:     []tagging.Assignment{{Inherited: true, Tag: tagging.Tag{ID: "project-tag", State: tagging.StateActive}}},
	}}
	handler := taggingRouter(&taggingCatalogStub{}, assignments, "classification.apply")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/objects/task/task-id/tags", nil))
	if response.Code != http.StatusOK || response.Header().Get("ETag") != `"4"` || !strings.Contains(response.Body.String(), "project-tag") || assignments.get.Target.ClientID != "client-id" {
		t.Fatalf("status=%d etag=%q body=%s command=%+v", response.Code, response.Header().Get("ETag"), response.Body.String(), assignments.get)
	}

	response = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/objects/task/task-id/tags", strings.NewReader(`{"tag_ids":["direct-tag"],"reason":"correct label","idempotency_key":"repeatable","client_id":"other-client","source":"migration","correlation_id":"forged"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"4"`)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPut, "/api/v1/objects/task/task-id/tags", strings.NewReader(`{"tag_ids":["direct-tag"],"reason":"correct label","idempotency_key":"repeatable"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"4"`)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || assignments.replace.Target.ClientID != "client-id" || assignments.replace.Source != tagging.SourceHuman || assignments.replace.CorrelationID == "" || assignments.replace.Principal.ID != "actor-id" {
		t.Fatalf("status=%d command=%+v", response.Code, assignments.replace)
	}
}

func TestTaggingExposesCanonicalMergedTagSurvivorFromAssociationResolution(t *testing.T) {
	const retiredTagID = "retired-merged-tag"
	const survivorTagID = "canonical-active-survivor"
	associations := &taggingAssociationStub{object: tagging.TaggedObject{
		Target:        tagging.TargetRef{MSPID: "msp-id", ClientID: "client-id", ObjectType: tagging.ObjectWorkRecord, ObjectID: "work-id"},
		ObjectVersion: 5,
		Direct: []tagging.Assignment{{
			Tag:    tagging.Tag{ID: survivorTagID, Label: "Canonical tag", State: tagging.StateActive},
			Source: tagging.SourceHuman,
		}},
		Effective: []tagging.Assignment{{
			Tag:    tagging.Tag{ID: survivorTagID, Label: "Canonical tag", State: tagging.StateActive},
			Source: tagging.SourceHuman,
		}},
	}}
	response := httptest.NewRecorder()
	taggingRouter(&taggingCatalogStub{}, associations, "classification.apply").ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/objects/work_record/work-id/tags", nil),
	)
	if response.Code != http.StatusOK || response.Header().Get("ETag") != `"5"` ||
		!strings.Contains(response.Body.String(), survivorTagID) ||
		strings.Contains(response.Body.String(), retiredTagID) {
		t.Fatalf("status=%d etag=%q body=%s", response.Code, response.Header().Get("ETag"), response.Body.String())
	}
}

func TestTaggingMapsStableErrorsWithoutCrossClientDisclosure(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		code   string
		status int
	}{
		{"classification_required", tagging.ErrMeaningfulTagRequired, "classification_required", http.StatusUnprocessableEntity},
		{"tag_archived", tagging.ErrInactiveTarget, "tag_archived", http.StatusConflict},
		{"tag_ambiguous", tagging.ErrDuplicateTerm, "tag_ambiguous", http.StatusConflict},
		{"version_conflict", object.ErrVersionConflict, "version_conflict", http.StatusConflict},
		{"not_found", scope.ErrNotFound, "not_found", http.StatusNotFound},
		{"forbidden", authorization.ErrForbidden, "forbidden", http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := taggingRouter(&taggingCatalogStub{err: test.err}, &taggingAssociationStub{}, "classification.manage")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/tag-groups", nil))
			var payload ErrorResponse
			_ = json.NewDecoder(bytes.NewReader(response.Body.Bytes())).Decode(&payload)
			if response.Code != test.status || payload.Error.Code != test.code {
				t.Fatalf("status=%d error=%+v", response.Code, payload.Error)
			}
		})
	}
}

func TestTaggingRequiresReasonForLifecycleMutations(t *testing.T) {
	catalog := &taggingCatalogStub{tag: tagging.Tag{ID: "tag-id", Version: 2}}
	handler := taggingRouter(catalog, &taggingAssociationStub{}, "classification.manage")
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tags/tag-id/merge", strings.NewReader(`{"survivor_tag_id":"survivor"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"1"`)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || catalog.merge.Reason != "" {
		t.Fatalf("status=%d command=%+v", response.Code, catalog.merge)
	}
}

func TestTaggingRequiresCapabilities(t *testing.T) {
	handler := taggingRouter(&taggingCatalogStub{}, &taggingAssociationStub{}, "work_record.read")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/tag-groups", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/collaboration"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mentions"
)

type collaborationActionsStub struct {
	create collaboration.CreateCommand
	upsert collaboration.UpsertCommand
	redact collaboration.RedactCommand
	listed collaboration.ListQuery
	err    error
}

func (s *collaborationActionsStub) ListInternalContent(_ context.Context, query collaboration.ListQuery) ([]collaboration.ListedSource, error) {
	s.listed = query
	return []collaboration.ListedSource{}, s.err
}
func (s *collaborationActionsStub) PutDetails(_ context.Context, command collaboration.UpsertCommand) (collaboration.Source, error) {
	s.upsert = command
	return collaboration.Source{ID: "details-1", Parent: command.Parent, Kind: command.Kind, Body: command.Body, Tokens: command.Tokens, Version: 1}, s.err
}
func (s *collaborationActionsStub) CreateComment(_ context.Context, command collaboration.CreateCommand) (collaboration.Source, error) {
	s.create = command
	return collaboration.Source{ID: "comment-1", Parent: command.Parent, Kind: mentions.SourceComment, Body: command.Body, Tokens: command.Tokens, Version: 1}, s.err
}
func (s *collaborationActionsStub) CreateNote(_ context.Context, command collaboration.CreateCommand) (collaboration.Source, error) {
	s.create = command
	return collaboration.Source{ID: "note-1", Parent: command.Parent, Kind: mentions.SourceNote, Body: command.Body, Tokens: command.Tokens, Version: 1}, s.err
}
func (s *collaborationActionsStub) Edit(_ context.Context, command collaboration.UpsertCommand) (collaboration.Source, error) {
	s.upsert = command
	return collaboration.Source{ID: command.SourceID, Parent: command.Parent, Kind: command.Kind, Body: command.Body, Tokens: command.Tokens, Version: command.ExpectedVersion + 1}, s.err
}
func (s *collaborationActionsStub) Redact(_ context.Context, command collaboration.RedactCommand) (collaboration.Source, error) {
	s.redact = command
	return collaboration.Source{ID: command.SourceID, Parent: command.Parent, Kind: command.Kind, LifecycleState: collaboration.SourceRedacted, Tokens: []mentions.Token{}, Version: command.ExpectedVersion + 1}, s.err
}

func TestInternalContentRoutesCoverAllParentTypesAndTrustedScope(t *testing.T) {
	for _, parent := range []struct {
		path string
		kind mentions.ParentType
	}{{"work-records", mentions.ParentWorkRecord}, {"tasks", mentions.ParentTask}, {"projects", mentions.ParentProject}} {
		t.Run(parent.path, func(t *testing.T) {
			actions := &collaborationActionsStub{}
			handler := NewRouter(Dependencies{Principal: mentionRoutePrincipal, Collaboration: actions, InternalContent: actions})

			list := httptest.NewRecorder()
			handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/"+parent.path+"/"+testParentUUID+"/internal-content", nil))
			if list.Code != http.StatusOK || actions.listed.Principal.Scope.MSPID != "msp-trusted" || actions.listed.Parent.Type != parent.kind || list.Body.String() != "[]\n" {
				t.Fatalf("list status=%d query=%+v body=%s", list.Code, actions.listed, list.Body.String())
			}

			body := `{"body":"Hello @Mira","tokens":[{"id":"` + testTokenUUID + `","target_type":"staff","target_id":"` + testTargetUUID + `","label":"@Mira","start":6,"end":11}],"confirmed_team_snapshots":{},"expected_version":0,"idempotency_key":"request-1"}`
			create := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/"+parent.path+"/"+testParentUUID+"/internal-comments", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			handler.ServeHTTP(create, request)
			if create.Code != http.StatusCreated || actions.create.Principal.Scope.MSPID != "msp-trusted" || actions.create.Principal.Scope.ClientID != "client-trusted" || actions.create.Parent.Type != parent.kind || actions.create.Parent.ID != testParentUUID || actions.create.Body != "Hello @Mira" || len(actions.create.Tokens) != 1 {
				t.Fatalf("create status=%d command=%+v body=%s", create.Code, actions.create, create.Body.String())
			}
		})
	}
}

func TestInternalContentMutationKindsAndStrictBodies(t *testing.T) {
	actions := &collaborationActionsStub{}
	handler := NewRouter(Dependencies{Principal: mentionRoutePrincipal, Collaboration: actions})
	body := `{"body":"plain","tokens":[],"confirmed_team_snapshots":{},"expected_version":0,"idempotency_key":"request-1"}`

	for _, test := range []struct {
		method, path string
		want         mentions.SourceKind
	}{
		{http.MethodPut, "/api/v1/projects/" + testParentUUID + "/internal-details", mentions.SourceDetails},
		{http.MethodPost, "/api/v1/tasks/" + testParentUUID + "/notes", mentions.SourceNote},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusCreated || (actions.upsert.Kind != test.want && actions.create.Body == "") {
			t.Fatalf("path=%s status=%d upsert=%+v create=%+v body=%s", test.path, response.Code, actions.upsert, actions.create, response.Body.String())
		}
	}

	editBody := `{"parent_type":"task","parent_id":"` + testParentUUID + `","source_kind":"comment","body":"edited","tokens":[],"confirmed_team_snapshots":{},"expected_version":2,"idempotency_key":"request-2"}`
	edit := httptest.NewRecorder()
	editRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/internal-content/"+testSourceUUID, strings.NewReader(editBody))
	editRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(edit, editRequest)
	if edit.Code != http.StatusOK || actions.upsert.SourceID != testSourceUUID || actions.upsert.ExpectedVersion != 2 || actions.upsert.Parent.Type != mentions.ParentTask {
		t.Fatalf("edit status=%d command=%+v body=%s", edit.Code, actions.upsert, edit.Body.String())
	}

	redact := httptest.NewRecorder()
	redactRequest := httptest.NewRequest(http.MethodPost, "/api/v1/internal-content/"+testSourceUUID+"/redact", strings.NewReader(`{"parent_type":"project","parent_id":"`+testParentUUID+`","source_kind":"note","expected_version":3,"idempotency_key":"request-4"}`))
	redactRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(redact, redactRequest)
	if redact.Code != http.StatusOK || actions.redact.SourceID != testSourceUUID || actions.redact.Parent.Type != mentions.ParentProject || actions.redact.Kind != mentions.SourceNote || actions.redact.ExpectedVersion != 3 {
		t.Fatalf("redact status=%d command=%+v body=%s", redact.Code, actions.redact, redact.Body.String())
	}

	invalid := httptest.NewRecorder()
	invalidRequest := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+testParentUUID+"/notes", strings.NewReader(`{"body":"x","tokens":null,"confirmed_team_snapshots":{},"expected_version":0,"idempotency_key":"request-3"}`))
	invalidRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(invalid, invalidRequest)
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("null tokens status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestInternalContentPartialTeamErrorPreservesSubmissionWithoutEcho(t *testing.T) {
	actions := &collaborationActionsStub{err: mentions.ErrTeamConfirmationRequired}
	handler := NewRouter(Dependencies{Principal: mentionRoutePrincipal, Collaboration: actions})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/work-records/"+testParentUUID+"/internal-comments", strings.NewReader(`{"body":"Secret @Ops","tokens":[{"id":"`+testTokenUUID+`","target_type":"team","target_id":"`+testTeamUUID+`","label":"@Ops","start":7,"end":11}],"confirmed_team_snapshots":{"`+testTeamUUID+`":{"team_version":2,"eligible_member_ids":["`+testMemberUUID+`"]}},"expected_version":0,"idempotency_key":"request-1"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"mention_team_confirmation_stale"`) || strings.Contains(response.Body.String(), "Secret") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if actions.create.Body != "Secret @Ops" || len(actions.create.Tokens) != 1 || actions.create.Tokens[0].TargetID != testTeamUUID {
		t.Fatalf("submission was not preserved: %+v", actions.create)
	}
}

func TestInternalContentRejectsVisibilityAndUnknownFields(t *testing.T) {
	actions := &collaborationActionsStub{}
	handler := NewRouter(Dependencies{Principal: mentionRoutePrincipal, Collaboration: actions})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+testParentUUID+"/notes", strings.NewReader(`{"body":"x","tokens":[],"confirmed_team_snapshots":{},"expected_version":0,"idempotency_key":"request-1","visibility":"client"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || actions.create.Body != "" {
		t.Fatalf("status=%d command=%+v body=%s", response.Code, actions.create, response.Body.String())
	}
}

func TestInternalContentRoutesRejectMalformedUUIDsBeforeMutation(t *testing.T) {
	validBody := `{"body":"Hello @Mira","tokens":[{"id":"` + testTokenUUID + `","target_type":"staff","target_id":"` + testTargetUUID + `","label":"@Mira","start":6,"end":11}],"confirmed_team_snapshots":{},"expected_version":0,"idempotency_key":"request"}`
	nonCanonicalUUID := "123e4567-e89b-12d3-a456-426614174000"
	tests := []struct {
		name, method, target, body string
	}{
		{name: "parent path", method: http.MethodPost, target: "/api/v1/work-records/not-a-uuid/internal-comments", body: validBody},
		{name: "token", method: http.MethodPost, target: "/api/v1/work-records/" + testParentUUID + "/internal-comments", body: strings.Replace(validBody, testTokenUUID, "not-a-uuid", 1)},
		{name: "whitespace token", method: http.MethodPost, target: "/api/v1/work-records/" + testParentUUID + "/internal-comments", body: strings.Replace(validBody, testTokenUUID, " "+testTokenUUID+" ", 1)},
		{name: "target", method: http.MethodPost, target: "/api/v1/work-records/" + testParentUUID + "/internal-comments", body: strings.Replace(validBody, testTargetUUID, "not-a-uuid", 1)},
		{name: "confirmation Team", method: http.MethodPost, target: "/api/v1/work-records/" + testParentUUID + "/internal-comments", body: `{"body":"plain","tokens":[],"confirmed_team_snapshots":{"not-a-uuid":{"team_version":1,"eligible_member_ids":[]}},"expected_version":0,"idempotency_key":"request"}`},
		{name: "confirmation member", method: http.MethodPost, target: "/api/v1/work-records/" + testParentUUID + "/internal-comments", body: `{"body":"plain","tokens":[],"confirmed_team_snapshots":{"` + testTeamUUID + `":{"team_version":1,"eligible_member_ids":["not-a-uuid"]}},"expected_version":0,"idempotency_key":"request"}`},
		{name: "edit source", method: http.MethodPatch, target: "/api/v1/internal-content/not-a-uuid", body: `{"parent_type":"task","parent_id":"` + testParentUUID + `","source_kind":"comment","body":"plain","tokens":[],"confirmed_team_snapshots":{},"expected_version":1,"idempotency_key":"request"}`},
		{name: "edit parent", method: http.MethodPatch, target: "/api/v1/internal-content/" + testSourceUUID, body: `{"parent_type":"task","parent_id":"not-a-uuid","source_kind":"comment","body":"plain","tokens":[],"confirmed_team_snapshots":{},"expected_version":1,"idempotency_key":"request"}`},
		{name: "URN parent", method: http.MethodPost, target: "/api/v1/work-records/urn:uuid:" + nonCanonicalUUID + "/internal-comments", body: validBody},
		{name: "compact parent", method: http.MethodPost, target: "/api/v1/work-records/123e4567e89b12d3a456426614174000/internal-comments", body: validBody},
		{name: "braced parent", method: http.MethodPost, target: "/api/v1/work-records/%7B" + nonCanonicalUUID + "%7D/internal-comments", body: validBody},
		{name: "uppercase parent", method: http.MethodPost, target: "/api/v1/work-records/123E4567-E89B-12D3-A456-426614174000/internal-comments", body: validBody},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actions := &collaborationActionsStub{}
			handler := NewRouter(Dependencies{Principal: mentionRoutePrincipal, Collaboration: actions})
			request := httptest.NewRequest(test.method, test.target, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code < 400 || response.Code >= 500 || !strings.Contains(response.Body.String(), "invalid") && !strings.Contains(response.Body.String(), "not found") {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if actions.create.Parent.ID != "" || actions.upsert.SourceID != "" {
				t.Fatalf("malformed UUID reached collaboration action: %+v", actions)
			}
		})
	}
}

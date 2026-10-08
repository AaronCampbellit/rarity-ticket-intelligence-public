package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/collaboration"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mentions"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	psastore "github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
)

type mentionActionsStub struct {
	candidateQuery mentions.CandidateQuery
	candidates     []mentions.Candidate
	widgetQuery    mentions.WidgetQuery
	stateChange    mentions.StateChange
	deepLinkQuery  mentions.DeepLinkQuery
	err            error
}

const (
	testParentUUID     = "00000000-0000-0000-0000-000000000101"
	testSourceUUID     = "00000000-0000-0000-0000-000000000102"
	testItemUUID       = "00000000-0000-0000-0000-000000000103"
	testOccurrenceUUID = "00000000-0000-0000-0000-000000000104"
	testTokenUUID      = "00000000-0000-0000-0000-000000000105"
	testTargetUUID     = "00000000-0000-0000-0000-000000000106"
	testTeamUUID       = "00000000-0000-0000-0000-000000000107"
	testMemberUUID     = "00000000-0000-0000-0000-000000000108"
)

func (s *mentionActionsStub) ListCandidates(_ context.Context, query mentions.CandidateQuery) ([]mentions.Candidate, error) {
	s.candidateQuery = query
	return append([]mentions.Candidate(nil), s.candidates...), s.err
}

func (s *mentionActionsStub) ListWidget(_ context.Context, query mentions.WidgetQuery) (mentions.WidgetPage, error) {
	s.widgetQuery = query
	return mentions.WidgetPage{Items: []mentions.WidgetItem{}}, s.err
}

func (s *mentionActionsStub) ChangeState(_ context.Context, change mentions.StateChange) (mentions.Item, error) {
	s.stateChange = change
	return mentions.Item{ID: change.ItemID, State: change.State, Version: change.ExpectedVersion + 1}, s.err
}

func (s *mentionActionsStub) ResolveDeepLink(_ context.Context, query mentions.DeepLinkQuery) (mentions.DeepLink, error) {
	s.deepLinkQuery = query
	return mentions.DeepLink{Href: "/work-records/work-1?source=source-1", ParentType: "work_record", ParentID: "work-1", SourceID: "source-1", SourceAvailable: true, ItemVersion: query.ExpectedVersion + 1}, s.err
}

func mentionRoutePrincipal(*http.Request) (authorization.Principal, error) {
	return authorization.Principal{ID: "staff-1", Scope: scope.Principal{MSPID: "msp-trusted", ClientID: "client-trusted"}}, nil
}

func TestMentionCandidatesUseAuthenticatedPrincipalScope(t *testing.T) {
	actions := &mentionActionsStub{candidates: []mentions.Candidate{
		{TargetType: mentions.TargetStaff, ID: "staff-2", Label: "Mira", Version: 2},
		{TargetType: mentions.TargetTeam, ID: "team-1", Label: "NOC", EligibleCount: 2, ExcludedCount: 1, EligibleMemberIDs: []string{"staff-2", "staff-3"}, Version: 4},
	}}
	handler := NewRouter(Dependencies{Principal: mentionRoutePrincipal, Mentions: actions})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/mentions/candidates?parent_type=work_record&parent_id="+testParentUUID+"&source_kind=comment&q=mir&msp_id=forged&client_id=forged&author_id=forged", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	got := actions.candidateQuery
	if got.AuthorID != "staff-1" || got.Source.MSPID != "msp-trusted" || got.Source.ClientID != "client-trusted" || got.Source.ParentID != testParentUUID || got.Search != "mir" {
		t.Fatalf("query contains untrusted scope or wrong values: %+v", got)
	}
	var body []map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body) != 2 {
		t.Fatalf("candidate response = %s error=%v", response.Body.String(), err)
	}
	if _, leaked := body[0]["eligible_member_ids"]; leaked {
		t.Fatalf("staff candidate leaked a team snapshot: %s", response.Body.String())
	}
	eligible, ok := body[1]["eligible_member_ids"].([]any)
	if !ok || len(eligible) != 2 || eligible[0] != "staff-2" || eligible[1] != "staff-3" {
		t.Fatalf("team candidate omitted exact snake_case snapshot: %s", response.Body.String())
	}
}

func TestMentionCandidatesAcceptOnlyPairedExactTeamIdentity(t *testing.T) {
	actions := &mentionActionsStub{}
	handler := NewRouter(Dependencies{Principal: mentionRoutePrincipal, Mentions: actions})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/mentions/candidates?parent_type=task&parent_id="+testParentUUID+"&source_kind=note&target_type=team&target_id="+testTeamUUID, nil))
	if response.Code != http.StatusOK || actions.candidateQuery.ExactTargetType != mentions.TargetTeam || actions.candidateQuery.ExactTargetID != testTeamUUID || actions.candidateQuery.Search != "" {
		t.Fatalf("exact status=%d query=%+v body=%s", response.Code, actions.candidateQuery, response.Body.String())
	}
	for _, target := range []string{
		"/api/v1/mentions/candidates?parent_type=task&parent_id=" + testParentUUID + "&source_kind=note&target_type=team",
		"/api/v1/mentions/candidates?parent_type=task&parent_id=" + testParentUUID + "&source_kind=note&target_id=" + testTeamUUID,
		"/api/v1/mentions/candidates?parent_type=task&parent_id=" + testParentUUID + "&source_kind=note&target_type=staff&target_id=" + testTargetUUID,
		"/api/v1/mentions/candidates?parent_type=task&parent_id=" + testParentUUID + "&source_kind=note&q=old&target_type=team&target_id=" + testTeamUUID,
	} {
		invalid := httptest.NewRecorder()
		handler.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, target, nil))
		if invalid.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid exact target %q status=%d body=%s", target, invalid.Code, invalid.Body.String())
		}
	}
}

func TestMentionCandidatesReturnNonNullEmptyArray(t *testing.T) {
	actions := &mentionActionsStub{}
	handler := NewRouter(Dependencies{Principal: mentionRoutePrincipal, Mentions: actions})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/mentions/candidates?parent_type=work_record&parent_id="+testParentUUID+"&source_kind=comment", nil))
	if response.Code != http.StatusOK || response.Body.String() != "[]\n" {
		t.Fatalf("empty candidates must be a non-null array: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestMentionWidgetStateAndDeepLinkRoutes(t *testing.T) {
	actions := &mentionActionsStub{}
	handler := NewRouter(Dependencies{Principal: mentionRoutePrincipal, Mentions: actions})

	widget := httptest.NewRecorder()
	handler.ServeHTTP(widget, httptest.NewRequest(http.MethodGet, "/api/v1/mentions/widget?state=unread&limit=20&cursor=signed", nil))
	if widget.Code != http.StatusOK || actions.widgetQuery.Principal.ID != "staff-1" || actions.widgetQuery.State != mentions.Unread || actions.widgetQuery.Limit != 20 || actions.widgetQuery.Cursor != "signed" || !strings.Contains(widget.Body.String(), `"items":[]`) {
		t.Fatalf("widget status=%d query=%+v body=%s", widget.Code, actions.widgetQuery, widget.Body.String())
	}

	state := httptest.NewRecorder()
	stateRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/mentions/items/"+testItemUUID, strings.NewReader(`{"state":"archived","expected_version":4}`))
	stateRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(state, stateRequest)
	if state.Code != http.StatusOK || actions.stateChange.ItemID != testItemUUID || actions.stateChange.State != mentions.Archived || actions.stateChange.ExpectedVersion != 4 || actions.stateChange.Principal.ID != "staff-1" {
		t.Fatalf("state status=%d command=%+v body=%s", state.Code, actions.stateChange, state.Body.String())
	}

	resolve := httptest.NewRecorder()
	resolveRequest := httptest.NewRequest(http.MethodPost, "/api/v1/mentions/occurrences/"+testOccurrenceUUID+"/resolve", strings.NewReader(`{"item_id":"`+testItemUUID+`","expected_version":5}`))
	resolveRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(resolve, resolveRequest)
	if resolve.Code != http.StatusOK || actions.deepLinkQuery.OccurrenceID != testOccurrenceUUID || actions.deepLinkQuery.ItemID != testItemUUID || actions.deepLinkQuery.ExpectedVersion != 5 || actions.deepLinkQuery.Principal.ID != "staff-1" {
		t.Fatalf("resolve status=%d query=%+v body=%s", resolve.Code, actions.deepLinkQuery, resolve.Body.String())
	}
}

func TestMentionRoutesRejectInvalidInputsAndHideDeepLinkAuthorization(t *testing.T) {
	tests := []struct {
		name, method, target, body, code string
		status                           int
		err                              error
	}{
		{"invalid enum", http.MethodGet, "/api/v1/mentions/widget?state=bogus&limit=20", "", "validation_failed", http.StatusUnprocessableEntity, nil},
		{"invalid limit", http.MethodGet, "/api/v1/mentions/widget?state=unread&limit=51", "", "validation_failed", http.StatusUnprocessableEntity, nil},
		{"unknown state field", http.MethodPatch, "/api/v1/mentions/items/" + testItemUUID, `{"state":"read","expected_version":1,"recipient_id":"forged"}`, "validation_failed", http.StatusUnprocessableEntity, nil},
		{"trailing state value", http.MethodPatch, "/api/v1/mentions/items/" + testItemUUID, `{"state":"read","expected_version":1}{}`, "validation_failed", http.StatusUnprocessableEntity, nil},
		{"version conflict", http.MethodPatch, "/api/v1/mentions/items/" + testItemUUID, `{"state":"read","expected_version":1}`, "version_conflict", http.StatusConflict, scope.ErrNotFound},
		{"hidden deep link", http.MethodPost, "/api/v1/mentions/occurrences/" + testOccurrenceUUID + "/resolve", `{"item_id":"` + testItemUUID + `","expected_version":1}`, "not_found", http.StatusNotFound, scope.ErrNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actions := &mentionActionsStub{err: test.err}
			if test.name == "version conflict" {
				actions.err = object.ErrVersionConflict
			}
			handler := NewRouter(Dependencies{Principal: mentionRoutePrincipal, Mentions: actions})
			request := httptest.NewRequest(test.method, test.target, strings.NewReader(test.body))
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestMentionRoutesRejectMalformedUUIDsBeforeRepositoryCalls(t *testing.T) {
	tests := []struct {
		name, method, target, body string
	}{
		{name: "candidate parent", method: http.MethodGet, target: "/api/v1/mentions/candidates?parent_type=work_record&parent_id=not-a-uuid&source_kind=comment"},
		{name: "candidate exact Team", method: http.MethodGet, target: "/api/v1/mentions/candidates?parent_type=work_record&parent_id=" + testParentUUID + "&source_kind=comment&target_type=team&target_id=not-a-uuid"},
		{name: "item state", method: http.MethodPatch, target: "/api/v1/mentions/items/not-a-uuid", body: `{"state":"read","expected_version":1}`},
		{name: "occurrence", method: http.MethodPost, target: "/api/v1/mentions/occurrences/not-a-uuid/resolve", body: `{"item_id":"` + testItemUUID + `","expected_version":1}`},
		{name: "deep-link item", method: http.MethodPost, target: "/api/v1/mentions/occurrences/" + testOccurrenceUUID + "/resolve", body: `{"item_id":"not-a-uuid","expected_version":1}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actions := &mentionActionsStub{}
			handler := NewRouter(Dependencies{Principal: mentionRoutePrincipal, Mentions: actions})
			request := httptest.NewRequest(test.method, test.target, strings.NewReader(test.body))
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"code":"validation_failed"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if actions.candidateQuery.Source.ParentID != "" || actions.stateChange.ItemID != "" || actions.deepLinkQuery.ItemID != "" {
				t.Fatalf("malformed UUID reached actions: %+v", actions)
			}
		})
	}
}

func TestMentionHTTPCreateWidgetStateDeepLinkAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for live mention HTTP verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	mspID, clientID, authorID, recipientID, outsiderID := id.New(), id.New(), id.New(), id.New(), id.New()
	roleID, authorAssignment, recipientAssignment, workID, teamID := id.New(), id.New(), id.New(), id.New(), id.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, execErr := pool.Exec(ctx, query, args...); execErr != nil {
			t.Fatalf("fixture: %v", execErr)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'mention-http',$3,$3)`, mspID, "MENTION-HTTP-"+mspID, authorID)
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$2,$3,'client',$4,$4)`, clientID, mspID, "CLIENT-"+clientID, authorID)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$4,$5,'Author'),($2,$4,$6,'Recipient'),($3,$4,$7,'Outsider')`, authorID, recipientID, outsiderID, mspID, authorID+"@example.test", recipientID+"@example.test", outsiderID+"@example.test")
	exec(`INSERT INTO roles(id,msp_id,key,name) VALUES($1,$2,$3,'Mention HTTP access')`, roleID, mspID, "mention-http-"+roleID)
	exec(`INSERT INTO role_capabilities(role_id,msp_id,capability) VALUES($1,$2,'mention.create'),($1,$2,'mention.read'),($1,$2,'work_record.read'),($1,$2,'work_record.edit')`, roleID, mspID)
	exec(`INSERT INTO role_assignments(id,msp_id,client_id,technician_id,role_id,granted_by) VALUES($1,$2,$3,$4,$6,$4),($5,$2,$3,$7,$6,$4)`, authorAssignment, mspID, clientID, authorID, recipientAssignment, roleID, recipientID)
	exec(`INSERT INTO work_records(id,msp_id,client_id,display_id,record_type,title,status,priority,created_by,updated_by) VALUES($1,$2,$3,$4,'incident','VPN outage','new','normal',$5,$5)`, workID, mspID, clientID, "TICKET-"+workID, authorID)
	exec(`INSERT INTO teams(id,msp_id,key,name) VALUES($1,$2,$3,'NOC')`, teamID, mspID, "noc-"+teamID)
	exec(`INSERT INTO team_memberships(team_id,technician_id,msp_id,created_at,created_by,updated_at,updated_by) VALUES($1,$2,$3,now(),$4,now(),$4),($1,$5,$3,now(),$4,now(),$4)`, teamID, recipientID, mspID, authorID, outsiderID)

	now := func() time.Time { return time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC) }
	collaborationRepository := psastore.NewCollaborationRepositoryFromPool(pool)
	mentionRepository := psastore.NewMentionRepositoryFromPool(pool)
	current := authorization.Principal{ID: authorID, Scope: scope.Principal{MSPID: mspID, ClientID: clientID}}
	handler := NewRouter(Dependencies{
		Principal:       func(*http.Request) (authorization.Principal, error) { return current, nil },
		Collaboration:   collaboration.NewService(collaborationRepository, mentions.NewService(now, id.New), now, id.New),
		InternalContent: collaboration.NewListService(mentionRepository),
		Mentions:        mentions.NewQueryService(mentionRepository, []byte("live HTTP cursor signing key with 32 bytes"), now),
		NewID:           id.New,
	})

	candidates := httptest.NewRecorder()
	handler.ServeHTTP(candidates, httptest.NewRequest(http.MethodGet, "/api/v1/mentions/candidates?parent_type=work_record&parent_id="+workID+"&source_kind=comment&q=recip", nil))
	if candidates.Code != http.StatusOK || !strings.Contains(candidates.Body.String(), recipientID) || strings.Contains(candidates.Body.String(), outsiderID) {
		t.Fatalf("candidates status=%d body=%s", candidates.Code, candidates.Body.String())
	}
	teamCandidates := httptest.NewRecorder()
	handler.ServeHTTP(teamCandidates, httptest.NewRequest(http.MethodGet, "/api/v1/mentions/candidates?parent_type=work_record&parent_id="+workID+"&source_kind=comment&q=noc", nil))
	var teamBody []mentions.Candidate
	if err = json.Unmarshal(teamCandidates.Body.Bytes(), &teamBody); err != nil || teamCandidates.Code != http.StatusOK || len(teamBody) != 1 ||
		teamBody[0].ID != teamID || teamBody[0].EligibleCount != 1 || teamBody[0].ExcludedCount != 1 ||
		len(teamBody[0].EligibleMemberIDs) != 1 || teamBody[0].EligibleMemberIDs[0] != recipientID {
		t.Fatalf("team candidates status=%d body=%s error=%v", teamCandidates.Code, teamCandidates.Body.String(), err)
	}
	exec(`UPDATE teams SET name='Network Operations' WHERE id=$1 AND msp_id=$2`, teamID, mspID)
	exactTeam := httptest.NewRecorder()
	handler.ServeHTTP(exactTeam, httptest.NewRequest(http.MethodGet, "/api/v1/mentions/candidates?parent_type=work_record&parent_id="+workID+"&source_kind=comment&target_type=team&target_id="+teamID, nil))
	if exactTeam.Code != http.StatusOK || !strings.Contains(exactTeam.Body.String(), `"label":"Network Operations"`) || !strings.Contains(exactTeam.Body.String(), `"id":"`+teamID+`"`) {
		t.Fatalf("renamed exact Team status=%d body=%s", exactTeam.Code, exactTeam.Body.String())
	}

	createBody := `{"body":"@Recipient investigate","tokens":[{"id":"` + id.New() + `","target_type":"staff","target_id":"` + recipientID + `","label":"@Recipient","start":0,"end":10}],"confirmed_team_snapshots":{},"expected_version":0,"idempotency_key":"` + id.New() + `"}`
	created := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/work-records/"+workID+"/internal-comments", strings.NewReader(createBody))
	createRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(created, createRequest)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}

	listed := httptest.NewRecorder()
	handler.ServeHTTP(listed, httptest.NewRequest(http.MethodGet, "/api/v1/work-records/"+workID+"/internal-content", nil))
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"tokens":[`) {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}

	current = authorization.Principal{ID: recipientID, Scope: scope.Principal{MSPID: mspID, ClientID: clientID}}
	widget := httptest.NewRecorder()
	handler.ServeHTTP(widget, httptest.NewRequest(http.MethodGet, "/api/v1/mentions/widget?state=unread&limit=20", nil))
	var page mentions.WidgetPage
	if widget.Code != http.StatusOK || json.Unmarshal(widget.Body.Bytes(), &page) != nil || len(page.Items) != 1 {
		t.Fatalf("widget status=%d body=%s", widget.Code, widget.Body.String())
	}
	item := page.Items[0]
	state := httptest.NewRecorder()
	stateRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/mentions/items/"+item.ID, strings.NewReader(`{"state":"read","expected_version":`+strconv.FormatInt(item.Version, 10)+`}`))
	stateRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(state, stateRequest)
	if state.Code != http.StatusOK {
		t.Fatalf("state status=%d body=%s", state.Code, state.Body.String())
	}
	var changed mentions.Item
	if err = json.Unmarshal(state.Body.Bytes(), &changed); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	resolved := httptest.NewRecorder()
	resolveRequest := httptest.NewRequest(http.MethodPost, "/api/v1/mentions/occurrences/"+item.LatestOccurrenceID+"/resolve", strings.NewReader(`{"item_id":"`+item.ID+`","expected_version":`+strconv.FormatInt(changed.Version, 10)+`}`))
	resolveRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(resolved, resolveRequest)
	if resolved.Code != http.StatusOK || !strings.Contains(resolved.Body.String(), "#/work?parentID="+workID) {
		t.Fatalf("resolve status=%d body=%s", resolved.Code, resolved.Body.String())
	}

	current = authorization.Principal{ID: authorID, Scope: scope.Principal{MSPID: mspID, ClientID: id.New()}}
	crossScope := httptest.NewRecorder()
	handler.ServeHTTP(crossScope, httptest.NewRequest(http.MethodGet, "/api/v1/mentions/candidates?parent_type=work_record&parent_id="+workID+"&source_kind=comment", nil))
	if crossScope.Code != http.StatusNotFound || strings.Contains(crossScope.Body.String(), workID) {
		t.Fatalf("cross-scope status=%d body=%s", crossScope.Code, crossScope.Body.String())
	}

	current = authorization.Principal{ID: outsiderID, Scope: scope.Principal{MSPID: mspID, ClientID: clientID}}
	hidden := httptest.NewRecorder()
	hiddenRequest := httptest.NewRequest(http.MethodPost, "/api/v1/mentions/occurrences/"+item.LatestOccurrenceID+"/resolve", strings.NewReader(`{"item_id":"`+item.ID+`","expected_version":`+strconv.FormatInt(changed.Version+1, 10)+`}`))
	hiddenRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(hidden, hiddenRequest)
	if hidden.Code != http.StatusNotFound || strings.Contains(hidden.Body.String(), workID) {
		t.Fatalf("hidden status=%d body=%s", hidden.Code, hidden.Body.String())
	}
}

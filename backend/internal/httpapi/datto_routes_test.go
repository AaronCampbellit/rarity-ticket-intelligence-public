package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/datto"
)

type dattoActions struct {
	manual   datto.ManualSyncCommand
	mapping  datto.MapSiteCommand
	progress datto.ProgressCommand
	list     datto.ListCandidatesCommand
	decision datto.DecisionCommand
}

func (a *dattoActions) MapSite(
	_ context.Context,
	command datto.MapSiteCommand,
) (datto.SiteMapping, error) {
	a.mapping = command
	return datto.SiteMapping{
		ID: "mapping-id", ConnectionID: command.ConnectionID,
		SiteID: command.SiteID, ClientID: command.ClientID,
	}, nil
}

func (a *dattoActions) QueueManualSync(
	_ context.Context,
	command datto.ManualSyncCommand,
) (datto.ManualSyncRequest, error) {
	a.manual = command
	return datto.ManualSyncRequest{
		ID: "request-id", ConnectionID: command.ConnectionID,
	}, nil
}

func (a *dattoActions) Progress(
	_ context.Context,
	command datto.ProgressCommand,
) (datto.SyncProgress, error) {
	a.progress = command
	return datto.SyncProgress{
		ConnectionID: command.ConnectionID,
		State:        "running",
		RunID:        "run-id",
		HealthState:  "healthy",
	}, nil
}

func (a *dattoActions) Decide(
	_ context.Context,
	command datto.DecisionCommand,
) (datto.CandidateDecision, error) {
	a.decision = command
	return datto.CandidateDecision{
		ID: "decision-id", CandidateID: command.CandidateID,
		Decision: command.Decision, Reason: command.Reason,
	}, nil
}

func (a *dattoActions) ListCandidates(
	_ context.Context,
	command datto.ListCandidatesCommand,
) ([]datto.ReconciliationCandidate, error) {
	a.list = command
	return []datto.ReconciliationCandidate{{
		ID: "candidate-id", ClientID: "client-id",
		AssetID: "asset-id", State: "pending",
	}}, nil
}

func TestDattoManualSyncRouteUsesTrustedMSPScope(t *testing.T) {
	actions := &dattoActions{}
	handler := NewRouter(Dependencies{
		Principal: testMSPPrincipal, Datto: actions,
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/integrations/datto/connection-id/sync",
		nil,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted ||
		actions.manual.Principal.ID != "actor-id" ||
		actions.manual.Target.MSPID != "msp-id" ||
		actions.manual.Target.ClientID != "" ||
		actions.manual.ConnectionID != "connection-id" ||
		!strings.Contains(response.Body.String(), `"id":"request-id"`) {
		t.Fatalf(
			"manual sync response=%d command=%+v body=%s",
			response.Code, actions.manual, response.Body.String(),
		)
	}
}

func TestDattoSiteMappingRouteUsesTrustedMSPAndExplicitClient(t *testing.T) {
	actions := &dattoActions{}
	handler := NewRouter(Dependencies{
		Principal: testMSPPrincipal, Datto: actions,
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/integrations/datto/connection-id/site-mappings",
		bytes.NewBufferString(
			`{"site_id":"site-id","client_id":"client-id","reason":"verified site"}`,
		),
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated ||
		actions.mapping.Principal.ID != "actor-id" ||
		actions.mapping.ConnectionID != "connection-id" ||
		actions.mapping.SiteID != "site-id" ||
		actions.mapping.ClientID != "client-id" ||
		actions.mapping.Reason != "verified site" ||
		!strings.Contains(response.Body.String(), `"id":"mapping-id"`) {
		t.Fatalf(
			"mapping response=%d command=%+v body=%s",
			response.Code, actions.mapping, response.Body.String(),
		)
	}
}

func TestDattoProgressRouteReturnsStableAPIShape(t *testing.T) {
	actions := &dattoActions{}
	handler := NewRouter(Dependencies{
		Principal: testMSPPrincipal, Datto: actions,
	})
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/integrations/datto/connection-id/status",
		nil,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK ||
		actions.progress.ConnectionID != "connection-id" ||
		actions.progress.Target.MSPID != "msp-id" ||
		!strings.Contains(response.Body.String(), `"state":"running"`) ||
		strings.Contains(response.Body.String(), `"MSPID"`) {
		t.Fatalf(
			"progress response=%d command=%+v body=%s",
			response.Code, actions.progress, response.Body.String(),
		)
	}
}

func TestDattoDecisionRouteUsesTrustedPrincipalAndCandidatePath(t *testing.T) {
	actions := &dattoActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, Datto: actions,
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/integrations/datto/reconciliation/candidate-id/decide",
		bytes.NewBufferString(
			`{"decision":"choose_datto","reason":"verified authority"}`,
		),
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK ||
		actions.decision.Principal.ID != "actor-id" ||
		actions.decision.CandidateID != "candidate-id" ||
		actions.decision.Decision != datto.DecisionChooseDatto ||
		actions.decision.Reason != "verified authority" ||
		!strings.Contains(response.Body.String(), `"id":"decision-id"`) {
		t.Fatalf(
			"decision response=%d command=%+v body=%s",
			response.Code, actions.decision, response.Body.String(),
		)
	}
}

func TestDattoCandidateListRouteUsesTrustedScopeAndBoundedLimit(t *testing.T) {
	actions := &dattoActions{}
	handler := NewRouter(Dependencies{
		Principal: testPrincipal, Datto: actions,
	})
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/integrations/datto/reconciliation?limit=25",
		nil,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK ||
		actions.list.Principal.ID != "actor-id" ||
		actions.list.Target.MSPID != "msp-id" ||
		actions.list.Target.ClientID != "client-id" ||
		actions.list.Limit != 25 ||
		!strings.Contains(response.Body.String(), `"items":[`) ||
		!strings.Contains(response.Body.String(), `"id":"candidate-id"`) {
		t.Fatalf(
			"candidate list response=%d command=%+v body=%s",
			response.Code, actions.list, response.Body.String(),
		)
	}
}

func testMSPPrincipal(
	request *http.Request,
) (principal authorization.Principal, err error) {
	principal, err = testPrincipal(request)
	principal.Scope.ClientID = ""
	return principal, err
}

package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/views"
)

type savedViewActionsStub struct {
	list views.ListCommand
	save views.SaveCommand
}

func (s *savedViewActionsStub) Save(
	_ context.Context,
	command views.SaveCommand,
) (views.View, error) {
	s.save = command
	return views.View{}, nil
}

func (s *savedViewActionsStub) Resolve(
	context.Context,
	views.ResolveCommand,
) (views.Resolved, error) {
	return views.Resolved{}, nil
}

func TestSaveCalendarLensRemovesActiveClientScope(t *testing.T) {
	actions := &savedViewActionsStub{}
	handler := NewRouter(Dependencies{Principal: func(*http.Request) (authorization.Principal, error) {
		return authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp", ClientID: "active-client"}, Capabilities: authorization.NewCapabilitySet("view.save")}, nil
	}, Views: actions})
	response := performCalendarJSON(handler, http.MethodPost, "/api/v1/views", `{"kind":"calendar_lens","name":"All work","query":{"lens":"week"},"audience":{"type":"private"}}`)
	if response.Code != http.StatusCreated || actions.save.Principal.Scope.ClientID != "" {
		t.Fatalf("status=%d command=%+v body=%s", response.Code, actions.save, response.Body.String())
	}
}

func (s *savedViewActionsStub) List(
	_ context.Context,
	command views.ListCommand,
) ([]views.View, error) {
	s.list = command
	return []views.View{{
		ID: "view-id", Kind: views.SavedSearch, Name: "Urgent work",
		Query:    map[string]any{"priority": "urgent"},
		Audience: views.Audience{Type: views.Private},
	}}, nil
}

func TestListViewsUsesAuthenticatedClientScope(t *testing.T) {
	actions := &savedViewActionsStub{}
	handler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{
				ID: "tech", Scope: scope.Principal{
					MSPID: "msp", ClientID: "client",
				},
				Capabilities: authorization.NewCapabilitySet("search.read"),
			}, nil
		},
		Views: actions,
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodGet, "/api/v1/views?kind=saved_search", nil,
		),
	)
	if response.Code != http.StatusOK ||
		actions.list.Target.ClientID != "client" ||
		actions.list.Kind != views.SavedSearch ||
		!strings.Contains(response.Body.String(), "Urgent work") {
		t.Fatalf(
			"status=%d command=%+v body=%s",
			response.Code, actions.list, response.Body.String(),
		)
	}
}

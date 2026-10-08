package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/auditlog"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type auditActionsStub struct {
	principal authorization.Principal
	limit     int
}

func (a *auditActionsStub) List(
	_ context.Context,
	principal authorization.Principal,
	limit int,
) ([]auditlog.Entry, error) {
	a.principal, a.limit = principal, limit
	return []auditlog.Entry{{
		ID: "audit-id", Action: "role.capabilities_replaced",
		SubjectType: "role", SubjectID: "role-id", SubjectVersion: 2,
	}}, nil
}

func TestAuditRouteUsesTrustedPrincipalAndBoundedLimit(t *testing.T) {
	actions := &auditActionsStub{}
	handler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{
				ID: "admin", Scope: scope.Principal{MSPID: "msp"},
				Capabilities: authorization.NewCapabilitySet("audit.read"),
			}, nil
		},
		Audit: actions,
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit?limit=75", nil),
	)
	if response.Code != http.StatusOK || actions.principal.ID != "admin" ||
		actions.limit != 75 ||
		!strings.Contains(response.Body.String(), "role.capabilities_replaced") {
		t.Fatalf(
			"status=%d limit=%d body=%s",
			response.Code, actions.limit, response.Body.String(),
		)
	}
}

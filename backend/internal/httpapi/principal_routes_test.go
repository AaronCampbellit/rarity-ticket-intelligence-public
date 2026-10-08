package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
)

func TestCurrentPrincipalReturnsOnlySafeNavigationGrants(t *testing.T) {
	handler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{
				ID: "technician-id",
				Capabilities: authorization.NewCapabilitySet(
					"secret.capability", "role.manage", "work_record.read",
					"routing.manage", "sla.manage",
				),
			}, nil
		},
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"id":"technician-id"`) ||
		!strings.Contains(response.Body.String(), `"role-settings"`) ||
		!strings.Contains(response.Body.String(), `"service-desk-settings"`) ||
		!strings.Contains(response.Body.String(), `"work"`) ||
		!strings.Contains(response.Body.String(), `"routing.manage"`) ||
		!strings.Contains(response.Body.String(), `"sla.manage"`) ||
		strings.Contains(response.Body.String(), "secret.capability") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCurrentPrincipalReturnsSalesAndDattoAdministrationNavigation(t *testing.T) {
	tests := []struct {
		capability string
		navigation string
	}{
		{capability: "opportunity.read", navigation: "prospects"},
		{capability: "prospect.create", navigation: "prospects"},
		{capability: "pipeline.create", navigation: "pipeline-settings"},
		{capability: "timesheet.read_own", navigation: "timesheets"},
		{
			capability: "integration.datto.reconcile",
			navigation: "datto-reconciliation",
		},
	}
	for _, test := range tests {
		t.Run(test.capability, func(t *testing.T) {
			handler := NewRouter(Dependencies{
				Principal: func(*http.Request) (authorization.Principal, error) {
					return authorization.Principal{
						ID:           "technician-id",
						Capabilities: authorization.NewCapabilitySet(test.capability),
					}, nil
				},
			})
			response := httptest.NewRecorder()
			handler.ServeHTTP(
				response,
				httptest.NewRequest(http.MethodGet, "/api/v1/me", nil),
			)
			if response.Code != http.StatusOK ||
				!strings.Contains(response.Body.String(), `"`+test.navigation+`"`) {
				t.Fatalf(
					"capability=%s status=%d body=%s",
					test.capability,
					response.Code,
					response.Body.String(),
				)
			}
		})
	}
}

func TestCurrentPrincipalReturnsSafeUICapabilitiesForAuthorizedActions(t *testing.T) {
	actionCapabilities := []string{
		"asset.create",
		"asset.lifecycle",
		"asset.update",
		"attachment.create",
		"change_order.update",
		"client.create",
		"client_resource.create",
		"client_resource.lifecycle",
		"client_resource.update",
		"comment.internal.create",
		"comment.public.create",
		"contact.create",
		"contact.lifecycle",
		"contact.update",
		"contract.create",
		"contract.lifecycle",
		"contract.update",
		"knowledge.edit",
		"knowledge.publish",
		"knowledge.read",
		"location.create",
		"location.lifecycle",
		"location.update",
		"opportunity.activity.create",
		"opportunity.create",
		"opportunity.read",
		"opportunity.transition",
		"opportunity.update",
		"project.create",
		"project.edit",
		"project.read",
		"project.resource.plan",
		"prospect.create",
		"proposal.acceptance.record",
		"proposal.approve",
		"proposal.create",
		"proposal.issue",
		"service.create",
		"service.lifecycle",
		"service.update",
		"search.read",
		"task.create",
		"time_entry.create",
		"work_record.assign",
		"work_record.create",
		"work_record.edit",
		"work_record.read",
		"work_record.route",
		"work_record.transition",
	}
	handler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{
				ID: "technician-id",
				Capabilities: authorization.NewCapabilitySet(
					append(actionCapabilities, "secret.capability")...,
				),
			}, nil
		},
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/me", nil),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	for _, capability := range actionCapabilities {
		if !strings.Contains(response.Body.String(), `"`+capability+`"`) {
			t.Errorf("response omitted authorized UI capability %q: %s",
				capability, response.Body.String())
		}
	}
	if strings.Contains(response.Body.String(), "secret.capability") {
		t.Fatalf("response exposed unknown capability: %s", response.Body.String())
	}
}

func TestCurrentPrincipalExposesClassificationNavigationAndCapabilities(t *testing.T) {
	handler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{ID: "administrator", Capabilities: authorization.NewCapabilitySet(
				"classification.manage", "classification.report", "classification.ai.manage",
			)}, nil
		},
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	for _, value := range []string{"classification-settings", "classification-insights", "classification.manage", "classification.report", "classification.ai.manage"} {
		if !strings.Contains(response.Body.String(), `"`+value+`"`) {
			t.Errorf("response omitted %q: %s", value, response.Body.String())
		}
	}
}

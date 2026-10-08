package httpapi

import (
	"net/http"
	"slices"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
)

type principalResponse struct {
	ID           string   `json:"id"`
	Navigation   []string `json:"navigation"`
	Capabilities []string `json:"capabilities"`
}

func (r *Router) registerPrincipalRoutes() {
	r.mux.HandleFunc("GET /api/v1/me", r.getCurrentPrincipal)
}

func (r *Router) getCurrentPrincipal(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	writeJSON(writer, http.StatusOK, principalResponse{
		ID: principal.ID, Navigation: principalNavigation(principal),
		Capabilities: principalUICapabilities(principal),
	})
}

var navigationCapabilities = map[string][]string{
	"work":                    {"work_record.read"},
	"calendar":                {"calendar.read"},
	"timesheets":              {"timesheet.read_own", "timesheet.review", "time_entry.update_own", "time_entry.amend"},
	"sales":                   {"opportunity.read"},
	"proposal":                {"proposal.read"},
	"conversion":              {"opportunity.convert"},
	"project":                 {"project.read"},
	"ai-assist":               {"ai.assist"},
	"ai-settings":             {"ai.manage"},
	"teams-settings":          {"integration.manage"},
	"sessions":                {"organization.read"},
	"recovery-access":         {"organization.manage"},
	"role-settings":           {"role.manage"},
	"audit":                   {"audit.read"},
	"service-desk-settings":   {"workflow.publish", "routing.manage", "sla.manage", "notification.manage"},
	"setup":                   {"organization.manage"},
	"operations":              {"integration.read"},
	"automation":              {"automation.manage", "automation.dead_letter.manage"},
	"service-keys":            {"service_key.manage"},
	"webhooks":                {"integration.read", "integration.manage"},
	"datto-reconciliation":    {"integration.datto.reconcile"},
	"datto-settings":          {"integration.manage"},
	"forwarding-settings":     {"integration.manage"},
	"graph-settings":          {"integration.manage"},
	"knowledge":               {"knowledge.read"},
	"billing":                 {"time_entry.approve", "time_entry.export"},
	"directory-settings":      {"organization.read", "organization.manage", "client.create"},
	"client-resources":        {"search.read"},
	"pipeline-settings":       {"pipeline.create"},
	"prospects":               {"opportunity.read", "prospect.create"},
	"classification-settings": {"classification.manage", "classification.ai.manage"},
	"classification-insights": {"classification.report"},
}

var uiActionCapabilities = []string{
	"calendar.read", "calendar.schedule", "calendar.commitment.manage", "calendar.policy.manage", "calendar.workforce.manage", "view.save", "view.share",
	"asset.create",
	"asset.lifecycle",
	"asset.update",
	"attachment.create",
	"change_order.update",
	"classification.manage",
	"classification.apply",
	"classification.report",
	"classification.ai.manage",
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

func principalNavigation(principal authorization.Principal) []string {
	navigation := make([]string, 0, len(navigationCapabilities))
	for page, capabilities := range navigationCapabilities {
		if slices.ContainsFunc(capabilities, principal.Capabilities.Has) {
			navigation = append(navigation, page)
		}
	}
	slices.Sort(navigation)
	return navigation
}

func principalUICapabilities(principal authorization.Principal) []string {
	known := make(map[string]struct{})
	for _, capabilities := range navigationCapabilities {
		for _, capability := range capabilities {
			known[capability] = struct{}{}
		}
	}
	for _, capability := range uiActionCapabilities {
		known[capability] = struct{}{}
	}
	capabilities := make([]string, 0, len(known))
	for capability := range known {
		if principal.Capabilities.Has(capability) {
			capabilities = append(capabilities, capability)
		}
	}
	slices.Sort(capabilities)
	return capabilities
}

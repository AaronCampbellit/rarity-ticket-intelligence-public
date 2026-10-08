package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/routing"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
)

func (r *Router) registerAIWorkspaceRoutes() {
	r.mux.HandleFunc("GET /api/v1/ai/workspace/conversations", r.listAIWorkspaceConversations)
	r.mux.HandleFunc("POST /api/v1/ai/workspace/conversations", r.createAIWorkspaceConversation)
	r.mux.HandleFunc("GET /api/v1/ai/workspace/conversations/{id}", r.getAIWorkspaceConversation)
	r.mux.HandleFunc("POST /api/v1/ai/workspace/conversations/{id}/archive", r.archiveAIWorkspaceConversation)
	r.mux.HandleFunc("POST /api/v1/ai/workspace/conversations/{id}/messages", r.createAIWorkspaceMessage)
	r.mux.HandleFunc("POST /api/v1/ai/workspace/conversations/{id}/tools/read", r.runAIWorkspaceReadTool)
	r.mux.HandleFunc("POST /api/v1/ai/workspace/conversations/{id}/proposals", r.proposeAIWorkspaceAction)
	r.mux.HandleFunc("POST /api/v1/ai/workspace/proposals/{id}/confirm", r.confirmAIWorkspaceProposal)
	r.mux.HandleFunc("POST /api/v1/ai/workspace/proposals/{id}/reject", r.rejectAIWorkspaceProposal)
}

func (r *Router) listAIWorkspaceConversations(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.aiWorkspacePrincipal(writer, request)
	if !ok || !r.requireAIWorkspaceConversations(writer, request) {
		return
	}
	limit := 30
	if raw := strings.TrimSpace(request.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "limit is invalid")
			return
		}
		limit = parsed
	}
	conversations, err := r.dependencies.AIWorkspaceConversations.List(
		request.Context(), principal,
		request.URL.Query().Get("include_archived") == "true",
		limit,
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, conversations)
}

func (r *Router) createAIWorkspaceConversation(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.aiWorkspacePrincipal(writer, request)
	if !ok || !r.requireAIWorkspaceConversations(writer, request) {
		return
	}
	var body struct {
		Title string `json:"title"`
	}
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	conversation, err := r.dependencies.AIWorkspaceConversations.Create(request.Context(), principal, body.Title)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writer.Header().Set("Location", "/api/v1/ai/workspace/conversations/"+conversation.ID)
	writeJSON(writer, http.StatusCreated, conversation)
}

func (r *Router) getAIWorkspaceConversation(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.aiWorkspacePrincipal(writer, request)
	if !ok || !r.requireAIWorkspaceConversations(writer, request) {
		return
	}
	conversation, err := r.dependencies.AIWorkspaceConversations.Get(
		request.Context(), principal, request.PathValue("id"),
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	messages, err := r.dependencies.AIWorkspaceConversations.Messages(
		request.Context(), principal, conversation.ID, 500,
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"conversation": conversation,
		"messages":     messages,
	})
}

func (r *Router) archiveAIWorkspaceConversation(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.aiWorkspacePrincipal(writer, request)
	if !ok || !r.requireAIWorkspaceConversations(writer, request) {
		return
	}
	var body struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	if err := r.dependencies.AIWorkspaceConversations.Archive(
		request.Context(), principal, request.PathValue("id"), body.ExpectedVersion,
	); err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (r *Router) createAIWorkspaceMessage(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.aiWorkspacePrincipal(writer, request)
	if !ok || !r.requireAIWorkspaceConversations(writer, request) ||
		!r.requireAIWorkspaceTools(writer, request) {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" || len(text) > 16_000 {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "message is invalid")
		return
	}
	conversationID := request.PathValue("id")
	if _, err := r.dependencies.AIWorkspaceConversations.Get(request.Context(), principal, conversationID); err != nil {
		writeDomainError(writer, request, err)
		return
	}
	userMessage, err := r.dependencies.AIWorkspaceConversations.Append(
		request.Context(), principal, conversationID, aiassist.MessageRoleUser, text, nil,
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	plan := aiassist.PlanWorkspaceMessage(text)
	if plan.Kind != aiassist.WorkspacePlanAction {
		var found bool
		plan, found, err = r.rehydrateAIWorkspaceTagSelection(
			request.Context(), principal, conversationID, plan, text,
		)
		if err != nil {
			writeDomainError(writer, request, err)
			return
		}
		if !found {
			plan = aiassist.PlanWorkspaceMessage(text)
		}
	}
	switch plan.Kind {
	case aiassist.WorkspacePlanClarification:
		r.writeAIWorkspaceMessageResponse(
			writer, request, principal, conversationID, userMessage,
			plan.Reply, nil, nil,
		)
		return
	case aiassist.WorkspacePlanAction:
		switch plan.ToolName {
		case "ticket.get", "ticket.search", "project.search", "project.get",
			"knowledge.search", "knowledge.get", "prospect.list",
			"opportunity.list", "opportunity.get", "proposal.list", "proposal.get":
			r.runAIWorkspaceOperationalRead(
				writer, request, principal, conversationID, userMessage, plan,
			)
		case "ticket.transition", "ticket.priority", "ticket.note", "ticket.reply":
			r.proposeAIWorkspaceExistingTicketAction(
				writer, request, principal, conversationID, userMessage, plan,
			)
		case "client.create":
			r.proposeAIWorkspaceClientAction(
				writer, request, principal, conversationID, userMessage, plan,
			)
		case "project.create":
			r.proposeAIWorkspaceProjectAction(
				writer, request, principal, conversationID, userMessage, plan,
			)
		case "task.create":
			r.proposeAIWorkspaceTaskAction(
				writer, request, principal, conversationID, userMessage, plan,
			)
		case "client_resource.list":
			r.runAIWorkspaceClientResourceList(
				writer, request, principal, conversationID, userMessage, plan,
			)
		case "location.create", "contact.create", "asset.create", "service.create", "contract.create":
			r.proposeAIWorkspaceClientResourceAction(
				writer, request, principal, conversationID, userMessage, plan,
			)
		case "location.update", "location.deactivate", "location.reactivate",
			"contact.update", "contact.deactivate", "contact.reactivate",
			"asset.update", "asset.deactivate", "asset.reactivate",
			"service.update", "service.deactivate", "service.reactivate",
			"contract.update", "contract.deactivate", "contract.reactivate":
			r.proposeAIWorkspaceClientResourceAction(
				writer, request, principal, conversationID, userMessage, plan,
			)
		case "knowledge.draft.create", "knowledge.draft.revise",
			"prospect.create", "ticket.route", "ticket.create", "ticket.assign",
			"opportunity.transition", "opportunity.activity.create",
			"proposal.create", "knowledge.publish":
			r.proposeAIWorkspaceOperationalWrite(
				writer, request, principal, conversationID, userMessage, plan,
			)
		default:
			writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "message action is unsupported")
		}
		return
	}
	audience := aiassist.ProductAudienceAllUsers
	if principal.Capabilities.Has("setup.manage") {
		audience = aiassist.ProductAudienceAdministrators
	}
	input, _ := json.Marshal(map[string]any{"query": text, "audience": audience})
	result, err := r.dependencies.AIWorkspaceTools.RunRead(
		request.Context(), principal,
		aiassist.ToolRequest{Name: "product.help", ConversationID: conversationID, MessageID: userMessage.ID, Input: input},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	r.writeAIWorkspaceMessageResponse(
		writer, request, principal, conversationID, userMessage,
		result.Summary, result.Data, nil,
	)
}

func (r *Router) rehydrateAIWorkspaceTagSelection(
	ctx context.Context,
	principal authorization.Principal,
	conversationID string,
	parsed aiassist.WorkspaceMessagePlan,
	text string,
) (aiassist.WorkspaceMessagePlan, bool, error) {
	if parsed.Kind == aiassist.WorkspacePlanAction || len(workspaceTagIDs(text)) == 0 {
		return parsed, false, nil
	}
	messages, err := r.dependencies.AIWorkspaceConversations.Messages(ctx, principal, conversationID, 3)
	if err != nil {
		return aiassist.WorkspaceMessagePlan{}, false, err
	}
	if len(messages) < 3 {
		return parsed, false, nil
	}
	previousUser := messages[len(messages)-3]
	clarification := messages[len(messages)-2]
	if previousUser.Role != aiassist.MessageRoleUser || clarification.Role != aiassist.MessageRoleAssistant {
		return parsed, false, nil
	}
	plan := aiassist.PlanWorkspaceMessage(previousUser.SafeText)
	if plan.Kind != aiassist.WorkspacePlanAction || len(plan.TagIDs) != 0 {
		return parsed, false, nil
	}
	wantsProjectTags := plan.ToolName == "project.create" && clarification.SafeText == "Which classification tag IDs should I apply to this project?"
	wantsTaskTags := plan.ToolName == "task.create" && clarification.SafeText == "Which classification tag IDs should I apply to this task?"
	wantsTicketTags := plan.ToolName == "ticket.create" && clarification.SafeText == "Which classification tag IDs should I apply to this ticket?"
	if !wantsProjectTags && !wantsTaskTags && !wantsTicketTags {
		return parsed, false, nil
	}
	plan.TagIDs = workspaceTagIDs(text)
	return plan, true, nil
}

func workspaceTagIDs(text string) []string {
	parts := strings.FieldsFunc(strings.TrimSpace(text), func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\t' || r == ' '
	})
	if len(parts) == 0 {
		return nil
	}
	tagIDs := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			tagIDs = append(tagIDs, trimmed)
		}
	}
	return tagIDs
}

func (r *Router) runAIWorkspaceOperationalRead(
	writer http.ResponseWriter,
	request *http.Request,
	principal authorization.Principal,
	conversationID string,
	userMessage aiassist.Message,
	plan aiassist.WorkspaceMessagePlan,
) {
	input, needsClientID, ok := aiWorkspaceOperationalReadInput(plan)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "message action is unsupported")
		return
	}
	if needsClientID {
		client, resolved := r.resolveAIWorkspaceClient(
			writer, request, principal, conversationID, userMessage, plan.ClientName,
		)
		if !resolved {
			return
		}
		input["client_id"] = client.ID
	}
	rawInput, _ := json.Marshal(input)
	result, err := r.dependencies.AIWorkspaceTools.RunRead(
		request.Context(), principal,
		aiassist.ToolRequest{
			Name: plan.ToolName, ConversationID: conversationID,
			MessageID: userMessage.ID, Input: rawInput,
		},
	)
	if err != nil {
		if aiWorkspaceAmbiguousReference(err) {
			r.writeAIWorkspaceMessageResponse(
				writer, request, principal, conversationID, userMessage,
				aiWorkspaceAmbiguousReferenceReply(plan), nil, nil,
			)
			return
		}
		writeAIWorkspaceToolError(writer, request, err)
		return
	}
	r.writeAIWorkspaceMessageResponse(
		writer, request, principal, conversationID, userMessage,
		result.Summary, result.Data, nil,
	)
}

func aiWorkspaceOperationalReadInput(
	plan aiassist.WorkspaceMessagePlan,
) (map[string]any, bool, bool) {
	switch plan.ToolName {
	case "ticket.get":
		return map[string]any{"id": plan.Reference}, true, true
	case "ticket.search":
		return map[string]any{"query": plan.Query, "limit": plan.Limit}, true, true
	case "project.search":
		return map[string]any{"client": plan.ClientName, "limit": plan.Limit}, false, true
	case "project.get":
		return map[string]any{"client": plan.ClientName, "project": plan.Reference}, false, true
	case "knowledge.search":
		return map[string]any{"client": plan.ClientName, "query": plan.Query, "limit": plan.Limit}, false, true
	case "knowledge.get":
		return map[string]any{"client": plan.ClientName, "article": plan.Reference}, false, true
	case "prospect.list":
		return map[string]any{"limit": plan.Limit}, false, true
	case "opportunity.list":
		return map[string]any{"client": plan.ClientName}, false, true
	case "opportunity.get":
		return map[string]any{
			"client": plan.ClientName, "opportunity": plan.OpportunityRef,
		}, false, true
	case "proposal.list":
		return map[string]any{"client": plan.ClientName}, false, true
	case "proposal.get":
		return map[string]any{
			"client": plan.ClientName, "proposal": plan.ProposalRef,
		}, false, true
	default:
		return nil, false, false
	}
}

func (r *Router) proposeAIWorkspaceExistingTicketAction(
	writer http.ResponseWriter,
	request *http.Request,
	principal authorization.Principal,
	conversationID string,
	userMessage aiassist.Message,
	plan aiassist.WorkspaceMessagePlan,
) {
	client, ok := r.resolveAIWorkspaceClient(
		writer, request, principal, conversationID, userMessage, plan.ClientName,
	)
	if !ok {
		return
	}
	input := map[string]any{
		"client_id": client.ID, "id": plan.TicketRef,
		"expected_version": plan.ExpectedVersion,
	}
	switch plan.ToolName {
	case "ticket.transition":
		input["status"], input["reason"] = plan.Value, plan.Reason
	case "ticket.priority":
		input["priority"], input["reason"] = plan.Value, plan.Reason
	case "ticket.note", "ticket.reply":
		input["body"] = plan.Body
	default:
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "message action is unsupported")
		return
	}
	rawInput, _ := json.Marshal(input)
	proposal, err := r.dependencies.AIWorkspaceTools.Propose(
		request.Context(), principal,
		aiassist.ToolRequest{
			Name: plan.ToolName, ConversationID: conversationID,
			MessageID: userMessage.ID, Input: rawInput,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	label := plan.ToolName
	if plan.ToolName == "ticket.reply" {
		label = "client-visible reply"
	}
	r.writeAIWorkspaceMessageResponse(
		writer, request, principal, conversationID, userMessage,
		"I prepared an exact "+label+" preview from the values you supplied. Review it before confirming.",
		map[string]any{"proposal_id": proposal.ID, "tool_name": proposal.ToolName},
		&proposal,
	)
}

func (r *Router) proposeAIWorkspaceOperationalWrite(
	writer http.ResponseWriter,
	request *http.Request,
	principal authorization.Principal,
	conversationID string,
	userMessage aiassist.Message,
	plan aiassist.WorkspaceMessagePlan,
) {
	if plan.ToolName == "ticket.create" && len(plan.TagIDs) == 0 {
		r.writeAIWorkspaceMessageResponse(writer, request, principal, conversationID, userMessage, "Which classification tag IDs should I apply to this ticket?", nil, nil)
		return
	}
	input, ok := aiWorkspaceOperationalWriteInput(plan)
	if !ok {
		writeError(
			request.Context(), writer, http.StatusUnprocessableEntity,
			"validation_failed", "message action is unsupported",
		)
		return
	}
	rawInput, _ := json.Marshal(input)
	proposal, err := r.dependencies.AIWorkspaceTools.Propose(
		request.Context(), principal,
		aiassist.ToolRequest{
			Name: plan.ToolName, ConversationID: conversationID,
			MessageID: userMessage.ID, Input: rawInput,
		},
	)
	if err != nil {
		if aiWorkspaceAmbiguousReference(err) {
			r.writeAIWorkspaceMessageResponse(
				writer, request, principal, conversationID, userMessage,
				aiWorkspaceAmbiguousReferenceReply(plan), nil, nil,
			)
			return
		}
		writeAIWorkspaceToolError(writer, request, err)
		return
	}
	r.writeAIWorkspaceMessageResponse(
		writer, request, principal, conversationID, userMessage,
		"I prepared an exact "+plan.ToolName+" preview from the values you supplied. Review it before confirming.",
		map[string]any{"proposal_id": proposal.ID, "tool_name": proposal.ToolName},
		&proposal,
	)
}

func aiWorkspaceOperationalWriteInput(
	plan aiassist.WorkspaceMessagePlan,
) (map[string]any, bool) {
	switch plan.ToolName {
	case "knowledge.draft.create":
		return map[string]any{
			"client": plan.ClientName, "display_id": plan.ArticleDisplayID,
			"title": plan.ArticleTitle, "body": plan.ArticleBody,
		}, true
	case "knowledge.draft.revise":
		return map[string]any{
			"client": plan.ClientName, "article": plan.ArticleRef,
			"expected_version": plan.ExpectedVersion,
			"title":            plan.ArticleTitle, "body": plan.ArticleBody,
		}, true
	case "prospect.create":
		input := map[string]any{
			"display_id": plan.ProspectDisplayID, "name": plan.ProspectName,
		}
		if plan.Email != "" {
			input["email"] = plan.Email
		}
		if plan.Phone != "" {
			input["phone"] = plan.Phone
		}
		return input, true
	case "ticket.route":
		return map[string]any{
			"client": plan.ClientName, "ticket": plan.TicketRef,
			"queue": plan.QueueRef, "expected_version": plan.ExpectedVersion,
			"reason": plan.Reason,
		}, true
	case "ticket.create":
		input := map[string]any{
			"client": plan.ClientName, "display_id": plan.TicketDisplayID,
			"type": plan.TicketType, "title": plan.TicketTitle,
			"description": plan.TicketDescription, "status": plan.TicketStatus,
			"priority": plan.TicketPriority,
			"tag_ids":  plan.TagIDs,
		}
		if plan.ServiceRef != "" {
			input["service"] = plan.ServiceRef
		}
		if plan.ContractRef != "" {
			input["contract"] = plan.ContractRef
		}
		return input, true
	case "ticket.assign":
		return map[string]any{
			"client": plan.ClientName, "ticket": plan.TicketRef,
			"technician":       plan.TechnicianRef,
			"expected_version": plan.ExpectedVersion, "reason": plan.Reason,
		}, true
	case "opportunity.transition":
		return map[string]any{
			"client": plan.ClientName, "opportunity": plan.OpportunityRef,
			"stage": plan.StageRef, "expected_version": plan.ExpectedVersion,
			"reason": plan.Reason,
		}, true
	case "opportunity.activity.create":
		return map[string]any{
			"client": plan.ClientName, "opportunity": plan.OpportunityRef,
			"kind": plan.ActivityKind, "summary": plan.ActivitySummary,
			"details": plan.ActivityDetails,
		}, true
	case "proposal.create":
		return map[string]any{
			"client": plan.ClientName, "opportunity": plan.OpportunityRef,
			"display_id": plan.ProposalDisplayID,
		}, true
	case "knowledge.publish":
		return map[string]any{
			"client": plan.ClientName, "article": plan.ArticleRef,
			"expected_version": plan.ExpectedVersion, "reason": plan.Reason,
		}, true
	default:
		return nil, false
	}
}

func aiWorkspaceAmbiguousReference(err error) bool {
	return errors.Is(err, projects.ErrAmbiguousReference) ||
		errors.Is(err, knowledge.ErrAmbiguousReference) ||
		errors.Is(err, clientresources.ErrAmbiguousResource) ||
		errors.Is(err, organizations.ErrClientReferenceAmbiguous) ||
		errors.Is(err, sales.ErrAmbiguousReference)
}

func aiWorkspaceAmbiguousReferenceReply(plan aiassist.WorkspaceMessagePlan) string {
	switch plan.ToolName {
	case "project.get":
		return "More than one Project matches " + plan.Reference + " for " +
			plan.ClientName + ". Use the Project display ID and try again."
	case "knowledge.get":
		return "More than one Knowledge article matches " + plan.Reference +
			" for " + plan.ClientName +
			". Use the Article display ID and try again."
	case "knowledge.draft.revise":
		return "More than one Knowledge article matches " + plan.ArticleRef +
			" for " + plan.ClientName +
			". Use the Article display ID and try again."
	default:
		return "More than one exact object matches. Use its display ID and try again."
	}
}

func writeAIWorkspaceAmbiguousReference(
	writer http.ResponseWriter,
	request *http.Request,
) {
	writeError(
		request.Context(), writer, http.StatusConflict,
		"ambiguous_reference",
		"More than one exact object matches. Use its display ID and try again.",
	)
}

func writeAIWorkspaceToolError(
	writer http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case aiWorkspaceAmbiguousReference(err):
		writeAIWorkspaceAmbiguousReference(writer, request)
	case errors.Is(err, routing.ErrNoRoute),
		errors.Is(err, routing.ErrInvalidRules),
		errors.Is(err, workflow.ErrNoWorkflow),
		errors.Is(err, workflow.ErrInvalidConfiguration),
		errors.Is(err, sla.ErrNoPolicy),
		errors.Is(err, sla.ErrInvalidPolicy),
		errors.Is(err, sla.ErrInvalidCalendar):
		writeError(
			request.Context(), writer, http.StatusConflict,
			"configuration_required",
			"Ticket routing, workflow, or SLA configuration is required.",
		)
	default:
		writeDomainError(writer, request, err)
	}
}

func (r *Router) runAIWorkspaceClientResourceList(
	writer http.ResponseWriter,
	request *http.Request,
	principal authorization.Principal,
	conversationID string,
	userMessage aiassist.Message,
	plan aiassist.WorkspaceMessagePlan,
) {
	input, _ := json.Marshal(map[string]any{"client": plan.ClientName, "kind": plan.ClientResourceKind})
	result, err := r.dependencies.AIWorkspaceTools.RunRead(
		request.Context(), principal,
		aiassist.ToolRequest{Name: plan.ToolName, ConversationID: conversationID, MessageID: userMessage.ID, Input: input},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	r.writeAIWorkspaceMessageResponse(
		writer, request, principal, conversationID, userMessage,
		result.Summary, result.Data, nil,
	)
}

func (r *Router) proposeAIWorkspaceClientResourceAction(
	writer http.ResponseWriter,
	request *http.Request,
	principal authorization.Principal,
	conversationID string,
	userMessage aiassist.Message,
	plan aiassist.WorkspaceMessagePlan,
) {
	input, ok := aiWorkspaceClientResourceInput(plan)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "message action is unsupported")
		return
	}
	rawInput, _ := json.Marshal(input)
	proposal, err := r.dependencies.AIWorkspaceTools.Propose(
		request.Context(), principal,
		aiassist.ToolRequest{Name: plan.ToolName, ConversationID: conversationID, MessageID: userMessage.ID, Input: rawInput},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	r.writeAIWorkspaceMessageResponse(
		writer, request, principal, conversationID, userMessage,
		"I prepared an exact "+plan.ToolName+" preview from the values you supplied. Review it before confirming.",
		map[string]any{"proposal_id": proposal.ID, "tool_name": proposal.ToolName}, &proposal,
	)
}

func aiWorkspaceClientResourceInput(plan aiassist.WorkspaceMessagePlan) (map[string]any, bool) {
	if strings.HasSuffix(plan.ToolName, ".deactivate") || strings.HasSuffix(plan.ToolName, ".reactivate") {
		return map[string]any{"client": plan.ClientName, "resource": plan.ClientResourceDisplayID, "reason": plan.Reason}, true
	}
	if strings.HasSuffix(plan.ToolName, ".update") {
		if plan.ClientResourcePatchField == "" {
			return nil, false
		}
		value := any(plan.ClientResourcePatchValue)
		if plan.ClientResourcePatchClear {
			value = ""
		}
		return map[string]any{
			"client": plan.ClientName, "resource": plan.ClientResourceDisplayID,
			"patch": map[string]any{plan.ClientResourcePatchField: value}, "reason": plan.Reason,
		}, true
	}
	input := map[string]any{"client": plan.ClientName, "display_id": plan.ClientResourceDisplayID}
	switch plan.ToolName {
	case "location.create":
		input["name"] = plan.ClientResourceName
	case "contact.create":
		input["display_name"] = plan.ClientResourceName
		if plan.LocationRef != "" {
			input["location"] = plan.LocationRef
		}
		if plan.Email != "" {
			input["email"] = plan.Email
		}
		if plan.Phone != "" {
			input["phone"] = plan.Phone
		}
	case "asset.create":
		input["name"], input["asset_type"] = plan.ClientResourceName, plan.AssetType
		if plan.LocationRef != "" {
			input["location"] = plan.LocationRef
		}
		if plan.SourceSystem != "" {
			input["source_system"] = plan.SourceSystem
		}
		if plan.ExternalID != "" {
			input["external_id"] = plan.ExternalID
		}
	case "service.create":
		input["name"] = plan.ClientResourceName
		if plan.Criticality != "" {
			input["criticality"] = plan.Criticality
		}
	case "contract.create":
		input["name"], input["starts_on"] = plan.ClientResourceName, plan.StartsOn
		if plan.EndsOn != "" {
			input["ends_on"] = plan.EndsOn
		}
	default:
		return nil, false
	}
	return input, true
}

type aiWorkspaceClientIdentityConflictChecker interface {
	HasClientIdentityConflict(
		context.Context,
		authorization.Principal,
		string,
		string,
	) (bool, error)
}

func (r *Router) proposeAIWorkspaceClientAction(
	writer http.ResponseWriter,
	request *http.Request,
	principal authorization.Principal,
	conversationID string,
	userMessage aiassist.Message,
	plan aiassist.WorkspaceMessagePlan,
) {
	conflicts, ok := r.dependencies.Organizations.(aiWorkspaceClientIdentityConflictChecker)
	if !ok {
		writeError(
			request.Context(), writer, http.StatusServiceUnavailable,
			"service_unavailable", "client identity check is unavailable",
		)
		return
	}
	conflict, err := conflicts.HasClientIdentityConflict(
		request.Context(), principal, plan.ClientName, plan.ClientDisplayID,
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	if conflict {
		r.writeAIWorkspaceMessageResponse(
			writer, request, principal, conversationID, userMessage,
			"A Client with that name or display ID already exists. Check the values and try again.",
			nil, nil,
		)
		return
	}
	input, _ := json.Marshal(map[string]any{
		"display_id": plan.ClientDisplayID,
		"name":       plan.ClientName,
	})
	proposal, err := r.dependencies.AIWorkspaceTools.Propose(
		request.Context(), principal,
		aiassist.ToolRequest{
			Name: plan.ToolName, ConversationID: conversationID,
			MessageID: userMessage.ID, Input: input,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	r.writeAIWorkspaceMessageResponse(
		writer, request, principal, conversationID, userMessage,
		"I prepared an exact Client preview from the name and display ID you supplied. Review it before confirming.",
		map[string]any{"proposal_id": proposal.ID, "tool_name": proposal.ToolName},
		&proposal,
	)
}

func (r *Router) proposeAIWorkspaceProjectAction(
	writer http.ResponseWriter,
	request *http.Request,
	principal authorization.Principal,
	conversationID string,
	userMessage aiassist.Message,
	plan aiassist.WorkspaceMessagePlan,
) {
	client, ok := r.resolveAIWorkspaceClient(
		writer, request, principal, conversationID, userMessage, plan.ClientName,
	)
	if !ok {
		return
	}
	if len(plan.TagIDs) == 0 {
		r.writeAIWorkspaceMessageResponse(writer, request, principal, conversationID, userMessage, "Which classification tag IDs should I apply to this project?", nil, nil)
		return
	}
	input, _ := json.Marshal(map[string]any{
		"client_id": client.ID,
		"name":      plan.ProjectName,
		"tasks":     plan.Tasks,
		"tag_ids":   plan.TagIDs,
	})
	proposal, err := r.dependencies.AIWorkspaceTools.Propose(
		request.Context(), principal,
		aiassist.ToolRequest{
			Name: plan.ToolName, ConversationID: conversationID,
			MessageID: userMessage.ID, Input: input,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	r.writeAIWorkspaceMessageResponse(
		writer, request, principal, conversationID, userMessage,
		"I prepared an exact project preview from the title, client, and tasks you supplied. Review it before confirming.",
		map[string]any{"proposal_id": proposal.ID, "tool_name": proposal.ToolName},
		&proposal,
	)
}

func (r *Router) resolveAIWorkspaceClient(
	writer http.ResponseWriter,
	request *http.Request,
	principal authorization.Principal,
	conversationID string,
	userMessage aiassist.Message,
	clientReference string,
) (organizations.Client, bool) {
	if r.dependencies.Directory == nil {
		writeError(request.Context(), writer, http.StatusServiceUnavailable, "service_unavailable", "client directory is unavailable")
		return organizations.Client{}, false
	}
	directory, err := r.dependencies.Directory.List(
		request.Context(),
		organizations.ListDirectoryCommand{
			Principal: principal,
			Target:    scope.Target{MSPID: principal.Scope.MSPID},
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return organizations.Client{}, false
	}
	client, err := organizations.ResolveClientReference(clientReference, directory.Clients)
	switch {
	case errors.Is(err, organizations.ErrClientReferenceNotFound):
		r.writeAIWorkspaceMessageResponse(
			writer, request, principal, conversationID, userMessage,
			"I couldn't find an authorized Client matching "+clientReference+". Check the Client name or display ID and try again.",
			nil, nil,
		)
		return organizations.Client{}, false
	case errors.Is(err, organizations.ErrClientReferenceAmbiguous):
		r.writeAIWorkspaceMessageResponse(
			writer, request, principal, conversationID, userMessage,
			"More than one authorized Client matches "+clientReference+". Use the Client display ID and try again.",
			nil, nil,
		)
		return organizations.Client{}, false
	case err != nil:
		writeDomainError(writer, request, err)
		return organizations.Client{}, false
	}
	return client, true
}

func (r *Router) proposeAIWorkspaceTaskAction(
	writer http.ResponseWriter,
	request *http.Request,
	principal authorization.Principal,
	conversationID string,
	userMessage aiassist.Message,
	plan aiassist.WorkspaceMessagePlan,
) {
	client, ok := r.resolveAIWorkspaceClient(
		writer, request, principal, conversationID, userMessage, plan.ClientName,
	)
	if !ok {
		return
	}
	if r.dependencies.ProjectQueries == nil {
		writeError(request.Context(), writer, http.StatusServiceUnavailable, "service_unavailable", "project directory is unavailable")
		return
	}
	matches, err := r.dependencies.ProjectQueries.ResolveReference(
		request.Context(), principal,
		scope.Target{
			MSPID: principal.Scope.MSPID, ClientID: client.ID,
		},
		plan.ProjectRef,
	)
	if errors.Is(err, projects.ErrAmbiguousReference) {
		r.writeAIWorkspaceMessageResponse(
			writer, request, principal, conversationID, userMessage,
			"More than one Project matches "+plan.ProjectRef+". Use the Project display ID and try again.",
			nil, nil,
		)
		return
	}
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	if len(matches) == 0 {
		r.writeAIWorkspaceMessageResponse(
			writer, request, principal, conversationID, userMessage,
			"I couldn't find "+plan.ProjectRef+" for "+plan.ClientName+". Check the Project name or display ID and try again.",
			nil, nil,
		)
		return
	}
	if len(matches) > 1 {
		r.writeAIWorkspaceMessageResponse(
			writer, request, principal, conversationID, userMessage,
			"More than one Project matches "+plan.ProjectRef+". Use the Project display ID and try again.",
			nil, nil,
		)
		return
	}
	if len(plan.TagIDs) == 0 {
		r.writeAIWorkspaceMessageResponse(writer, request, principal, conversationID, userMessage, "Which classification tag IDs should I apply to this task?", nil, nil)
		return
	}
	input, _ := json.Marshal(map[string]any{
		"client_id":   client.ID,
		"project_id":  string(matches[0].ID),
		"project_ref": plan.ProjectRef,
		"title":       plan.TaskTitle,
		"tag_ids":     plan.TagIDs,
	})
	proposal, err := r.dependencies.AIWorkspaceTools.Propose(
		request.Context(), principal,
		aiassist.ToolRequest{
			Name: plan.ToolName, ConversationID: conversationID,
			MessageID: userMessage.ID, Input: input,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	r.writeAIWorkspaceMessageResponse(
		writer, request, principal, conversationID, userMessage,
		"I prepared an exact task preview from the Project, Client, and task title you supplied. Review it before confirming.",
		map[string]any{"proposal_id": proposal.ID, "tool_name": proposal.ToolName},
		&proposal,
	)
}

func (r *Router) writeAIWorkspaceMessageResponse(
	writer http.ResponseWriter,
	request *http.Request,
	principal authorization.Principal,
	conversationID string,
	userMessage aiassist.Message,
	reply string,
	references map[string]any,
	proposal *aiassist.ActionProposal,
) {
	assistantMessage, err := r.dependencies.AIWorkspaceConversations.Append(
		request.Context(), principal, conversationID, aiassist.MessageRoleAssistant,
		reply, references,
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	response := map[string]any{
		"user_message": userMessage, "assistant_message": assistantMessage,
	}
	if proposal != nil {
		response["proposal"] = proposal
	}
	writeJSON(writer, http.StatusCreated, response)
}

func (r *Router) runAIWorkspaceReadTool(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.aiWorkspacePrincipal(writer, request)
	if !ok || !r.requireAIWorkspaceConversations(writer, request) ||
		!r.requireAIWorkspaceTools(writer, request) {
		return
	}
	var body struct {
		ToolName string          `json:"tool_name"`
		Input    json.RawMessage `json:"input"`
	}
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	conversationID := request.PathValue("id")
	if _, err := r.dependencies.AIWorkspaceConversations.Get(request.Context(), principal, conversationID); err != nil {
		writeDomainError(writer, request, err)
		return
	}
	result, err := r.dependencies.AIWorkspaceTools.RunRead(
		request.Context(), principal,
		aiassist.ToolRequest{Name: body.ToolName, ConversationID: conversationID, Input: body.Input},
	)
	if err != nil {
		if aiWorkspaceAmbiguousReference(err) {
			writeAIWorkspaceAmbiguousReference(writer, request)
			return
		}
		writeAIWorkspaceToolError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) proposeAIWorkspaceAction(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.aiWorkspacePrincipal(writer, request)
	if !ok || !r.requireAIWorkspaceConversations(writer, request) ||
		!r.requireAIWorkspaceTools(writer, request) {
		return
	}
	var body struct {
		ToolName  string          `json:"tool_name"`
		MessageID string          `json:"message_id"`
		Input     json.RawMessage `json:"input"`
	}
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	conversationID := request.PathValue("id")
	if _, err := r.dependencies.AIWorkspaceConversations.Get(request.Context(), principal, conversationID); err != nil {
		writeDomainError(writer, request, err)
		return
	}
	proposal, err := r.dependencies.AIWorkspaceTools.Propose(
		request.Context(), principal,
		aiassist.ToolRequest{
			Name: body.ToolName, ConversationID: conversationID,
			MessageID: body.MessageID, Input: body.Input,
		},
	)
	if err != nil {
		if aiWorkspaceAmbiguousReference(err) {
			writeAIWorkspaceAmbiguousReference(writer, request)
			return
		}
		writeAIWorkspaceToolError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, proposal)
}

func (r *Router) confirmAIWorkspaceProposal(writer http.ResponseWriter, request *http.Request) {
	r.mutateAIWorkspaceProposal(writer, request, true)
}

func (r *Router) rejectAIWorkspaceProposal(writer http.ResponseWriter, request *http.Request) {
	r.mutateAIWorkspaceProposal(writer, request, false)
}

func (r *Router) mutateAIWorkspaceProposal(writer http.ResponseWriter, request *http.Request, confirm bool) {
	principal, ok := r.aiWorkspacePrincipal(writer, request)
	if !ok || !r.requireAIWorkspaceTools(writer, request) {
		return
	}
	var body struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	if body.ExpectedVersion <= 0 {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "expected version is required")
		return
	}
	if !confirm {
		if err := r.dependencies.AIWorkspaceTools.Reject(
			request.Context(), principal, request.PathValue("id"), body.ExpectedVersion,
		); err != nil {
			writeDomainError(writer, request, err)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	result, err := r.dependencies.AIWorkspaceTools.Confirm(
		request.Context(), principal, request.PathValue("id"), body.ExpectedVersion,
	)
	if err != nil {
		writeAIWorkspaceToolError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) aiWorkspacePrincipal(
	writer http.ResponseWriter,
	request *http.Request,
) (authorization.Principal, bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return authorization.Principal{}, false
	}
	target := scope.Target{MSPID: principal.Scope.MSPID}
	if err := authorization.Authorize(principal, "ai.assist", target); err != nil {
		writeDomainError(writer, request, err)
		return authorization.Principal{}, false
	}
	return principal, true
}

func (r *Router) requireAIWorkspaceConversations(writer http.ResponseWriter, request *http.Request) bool {
	if r.dependencies.AIWorkspaceConversations == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "AI workspace is unavailable")
		return false
	}
	return true
}

func (r *Router) requireAIWorkspaceTools(writer http.ResponseWriter, request *http.Request) bool {
	if r.dependencies.AIWorkspaceTools == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "AI workspace tools are unavailable")
		return false
	}
	return true
}

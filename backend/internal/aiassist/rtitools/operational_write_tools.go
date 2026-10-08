package rtitools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

type KnowledgeDraftActions interface {
	ResolveDraftReference(context.Context, knowledge.ResolveReferenceCommand) ([]knowledge.Article, error)
	FindDraft(context.Context, knowledge.FindCommand) (knowledge.ArticleDetail, error)
	CreateDraft(context.Context, knowledge.CreateDraftCommand) (knowledge.ArticleDetail, error)
	ReviseDraft(context.Context, knowledge.ReviseDraftCommand) (knowledge.ArticleDetail, error)
}

type ProspectCreateActions interface {
	ResolveProspectReference(context.Context, authorization.Principal, scope.Target, string) ([]sales.Prospect, error)
	CreateProspect(context.Context, sales.CreateProspectCommand) (sales.Prospect, error)
}

type TicketRouteActions interface {
	Preflight(context.Context, workrecords.QueuePreflightCommand) (workrecords.QueuePreflight, error)
	Transfer(context.Context, workrecords.QueueCommand) (workrecords.Record, error)
}

type knowledgeDraftCreateRequest struct {
	Client    string `json:"client"`
	DisplayID string `json:"display_id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
}

type knowledgeDraftCreatePrepared struct {
	ClientID        string `json:"client_id"`
	ClientReference string `json:"client_reference"`
	ClientName      string `json:"client_name"`
	ClientDisplayID string `json:"client_display_id"`
	ArticleID       string `json:"article_id"`
	DisplayID       string `json:"display_id"`
	Title           string `json:"title"`
	Body            string `json:"body"`
}

type knowledgeDraftReviseRequest struct {
	Client          string `json:"client"`
	Article         string `json:"article"`
	ExpectedVersion int64  `json:"expected_version"`
	Title           string `json:"title"`
	Body            string `json:"body"`
}

type knowledgeDraftRevisePrepared struct {
	ClientID         string          `json:"client_id"`
	ClientReference  string          `json:"client_reference"`
	ClientName       string          `json:"client_name"`
	ClientDisplayID  string          `json:"client_display_id"`
	ArticleID        string          `json:"article_id"`
	ArticleReference string          `json:"article_reference"`
	ArticleDisplayID string          `json:"article_display_id"`
	ArticleTitle     string          `json:"article_title"`
	ArticleBody      string          `json:"article_body"`
	ArticleState     knowledge.State `json:"article_state"`
	ExpectedVersion  int64           `json:"expected_version"`
	Title            string          `json:"title"`
	Body             string          `json:"body"`
}

type prospectCreateRequest struct {
	DisplayID string  `json:"display_id"`
	Name      string  `json:"name"`
	Email     *string `json:"email,omitempty"`
	Phone     *string `json:"phone,omitempty"`
}

type prospectCreatePrepared struct {
	ProspectID string  `json:"prospect_id"`
	DisplayID  string  `json:"display_id"`
	Name       string  `json:"name"`
	Email      *string `json:"email,omitempty"`
	Phone      *string `json:"phone,omitempty"`
}

type ticketRouteRequest struct {
	Client          string `json:"client"`
	Ticket          string `json:"ticket"`
	Queue           string `json:"queue"`
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

type ticketRoutePrepared struct {
	ClientID             string `json:"client_id"`
	ClientReference      string `json:"client_reference"`
	ClientName           string `json:"client_name"`
	ClientDisplayID      string `json:"client_display_id"`
	TicketID             string `json:"ticket_id"`
	TicketReference      string `json:"ticket_reference"`
	TicketDisplayID      string `json:"ticket_display_id"`
	TicketTitle          string `json:"ticket_title"`
	CurrentQueueID       string `json:"current_queue_id,omitempty"`
	CurrentQueueClientID string `json:"current_queue_client_id,omitempty"`
	CurrentQueueKey      string `json:"current_queue_key,omitempty"`
	CurrentQueueName     string `json:"current_queue_name,omitempty"`
	CurrentQueueVersion  int64  `json:"current_queue_version,omitempty"`
	QueueID              string `json:"queue_id"`
	QueueClientID        string `json:"queue_client_id,omitempty"`
	QueueReference       string `json:"queue_reference"`
	QueueKey             string `json:"queue_key"`
	QueueName            string `json:"queue_name"`
	QueueVersion         int64  `json:"queue_version"`
	ExpectedVersion      int64  `json:"expected_version"`
	Reason               string `json:"reason"`
}

func NewKnowledgeDraftCreateTool(
	directory ActiveClientResolver,
	actions KnowledgeDraftActions,
	newID func() string,
) aiassist.Tool {
	return &functionalTool{
		name: "knowledge.draft.create", capability: "knowledge.edit", kind: aiassist.ToolWrite,
		prepare: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (json.RawMessage, error) {
			request, err := decode[knowledgeDraftCreateRequest](raw)
			if err != nil || directory == nil || actions == nil || newID == nil {
				return nil, aiassist.ErrInvalidTool
			}
			request.Client = strings.TrimSpace(request.Client)
			request.DisplayID = strings.TrimSpace(request.DisplayID)
			request.Title = strings.TrimSpace(request.Title)
			request.Body = strings.TrimSpace(request.Body)
			if request.Client == "" || request.DisplayID == "" || request.Title == "" || request.Body == "" {
				return nil, aiassist.ErrInvalidTool
			}
			client, err := resolveActiveResourceClient(ctx, directory, principal, "knowledge.edit", request.Client)
			if err != nil {
				return nil, aiassist.ErrInvalidTool
			}
			if err := ensureKnowledgeDraftIdentityAvailable(
				ctx, actions, principal, client.ID, request.DisplayID, request.Title,
			); err != nil {
				return nil, aiassist.ErrInvalidTool
			}
			articleID := strings.TrimSpace(newID())
			if articleID == "" {
				return nil, aiassist.ErrInvalidTool
			}
			return marshalPrepared(knowledgeDraftCreatePrepared{
				ClientID: client.ID, ClientReference: request.Client, ClientName: client.Name,
				ClientDisplayID: client.DisplayID, ArticleID: articleID,
				DisplayID: request.DisplayID, Title: request.Title, Body: request.Body,
			})
		},
		validate: validateKnowledgeDraftCreate,
		resolve:  resolveClientResourceScope,
		preview: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (aiassist.Preview, error) {
			input, err := validKnowledgeDraftCreate(raw)
			if err != nil {
				return aiassist.Preview{}, err
			}
			client, err := resolveActiveResourceClient(ctx, directory, principal, "knowledge.edit", input.ClientReference)
			if err != nil || client.ID != input.ClientID || client.Name != input.ClientName ||
				client.DisplayID != input.ClientDisplayID {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			if err := ensureKnowledgeDraftIdentityAvailable(
				ctx, actions, principal, input.ClientID, input.DisplayID, input.Title,
			); err != nil {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			return aiassist.Preview{
				Summary:    "Create knowledge draft " + input.DisplayID + " for " + input.ClientName,
				TargetType: "knowledge_article", TargetID: input.ArticleID,
				Changes: map[string]aiassist.Change{
					"display_id": {Before: nil, After: input.DisplayID},
					"title":      {Before: nil, After: input.Title},
					"body":       {Before: nil, After: input.Body},
					"state":      {Before: nil, After: string(knowledge.Draft)},
				},
			}, nil
		},
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, correlationID string) (aiassist.ToolResult, error) {
			input, err := validKnowledgeDraftCreate(raw)
			if err != nil || strings.TrimSpace(correlationID) == "" || actions == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			detail, err := actions.CreateDraft(ctx, knowledge.CreateDraftCommand{
				Principal: principal, Target: target(principal, input.ClientID),
				ArticleID: input.ArticleID, DisplayID: input.DisplayID, Title: input.Title, Body: input.Body,
				ActorID: principal.ID, Source: "ai_workspace", CorrelationID: correlationID,
			})
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{Summary: "Created knowledge draft " + detail.Article.DisplayID, Data: map[string]any{
				"article_id": detail.Article.ID, "display_id": detail.Article.DisplayID,
				"title": detail.Article.Title, "current_version": detail.Article.CurrentVersion,
			}}, nil
		},
	}
}

func NewKnowledgeDraftReviseTool(
	directory ActiveClientResolver,
	actions KnowledgeDraftActions,
) aiassist.Tool {
	return &functionalTool{
		name: "knowledge.draft.revise", capability: "knowledge.edit", kind: aiassist.ToolWrite,
		prepare: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (json.RawMessage, error) {
			request, err := decode[knowledgeDraftReviseRequest](raw)
			if err != nil || directory == nil || actions == nil {
				return nil, aiassist.ErrInvalidTool
			}
			request.Client, request.Article = strings.TrimSpace(request.Client), strings.TrimSpace(request.Article)
			request.Title, request.Body = strings.TrimSpace(request.Title), strings.TrimSpace(request.Body)
			if request.Client == "" || request.Article == "" || request.ExpectedVersion < 1 ||
				request.Title == "" || request.Body == "" {
				return nil, aiassist.ErrInvalidTool
			}
			client, err := resolveActiveResourceClient(ctx, directory, principal, "knowledge.edit", request.Client)
			if err != nil {
				return nil, aiassist.ErrInvalidTool
			}
			detail, err := exactDraftArticle(ctx, actions, principal, client.ID, request.Article)
			if err != nil {
				if errors.Is(err, knowledge.ErrAmbiguousReference) {
					return nil, err
				}
				return nil, aiassist.ErrInvalidTool
			}
			if detail.Article.CurrentVersion != request.ExpectedVersion {
				return nil, object.ErrVersionConflict
			}
			return marshalPrepared(knowledgeDraftRevisePrepared{
				ClientID: client.ID, ClientReference: request.Client, ClientName: client.Name, ClientDisplayID: client.DisplayID,
				ArticleID: detail.Article.ID, ArticleReference: request.Article, ArticleDisplayID: detail.Article.DisplayID,
				ArticleTitle: detail.Article.Title, ArticleBody: detail.Version.Body, ArticleState: detail.Article.State,
				ExpectedVersion: request.ExpectedVersion, Title: request.Title, Body: request.Body,
			})
		},
		validate: validateKnowledgeDraftRevise,
		resolve:  resolveClientResourceScope,
		preview: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (aiassist.Preview, error) {
			input, err := validKnowledgeDraftRevise(raw)
			if err != nil {
				return aiassist.Preview{}, err
			}
			client, err := resolveActiveResourceClient(ctx, directory, principal, "knowledge.edit", input.ClientReference)
			if err != nil || client.ID != input.ClientID || client.Name != input.ClientName ||
				client.DisplayID != input.ClientDisplayID {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			detail, err := exactDraftArticle(ctx, actions, principal, input.ClientID, input.ArticleReference)
			if err != nil {
				return aiassist.Preview{}, err
			}
			if detail.Article.ID != input.ArticleID || detail.Article.DisplayID != input.ArticleDisplayID {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			if detail.Article.CurrentVersion != input.ExpectedVersion {
				return aiassist.Preview{}, object.ErrVersionConflict
			}
			return knowledgeDraftRevisePreview(input, detail), nil
		},
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, correlationID string) (aiassist.ToolResult, error) {
			input, err := validKnowledgeDraftRevise(raw)
			if err != nil || strings.TrimSpace(correlationID) == "" || actions == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			detail, err := actions.ReviseDraft(ctx, knowledge.ReviseDraftCommand{
				Principal: principal, Target: target(principal, input.ClientID), ArticleID: input.ArticleID,
				ExpectedVersion: input.ExpectedVersion, Title: input.Title, Body: input.Body,
				ActorID: principal.ID, Source: "ai_workspace", CorrelationID: correlationID,
			})
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{Summary: "Revised knowledge draft " + detail.Article.DisplayID, Data: map[string]any{
				"article_id": detail.Article.ID, "display_id": detail.Article.DisplayID,
				"title": detail.Article.Title, "current_version": detail.Article.CurrentVersion,
			}}, nil
		},
	}
}

func NewProspectCreateTool(actions ProspectCreateActions, newID func() string) aiassist.Tool {
	return &functionalTool{
		name: "prospect.create", capability: "prospect.create", kind: aiassist.ToolWrite,
		prepare: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (json.RawMessage, error) {
			request, err := decode[prospectCreateRequest](raw)
			if err != nil || actions == nil || newID == nil {
				return nil, aiassist.ErrInvalidTool
			}
			request.DisplayID, request.Name = strings.TrimSpace(request.DisplayID), strings.TrimSpace(request.Name)
			request.Email, request.Phone = normalizedOptional(request.Email), normalizedOptional(request.Phone)
			if request.DisplayID == "" || request.Name == "" ||
				(request.Email != nil && *request.Email == "") || (request.Phone != nil && *request.Phone == "") {
				return nil, aiassist.ErrInvalidTool
			}
			if err := ensureProspectIdentityAvailable(ctx, actions, principal, request.DisplayID, request.Name); err != nil {
				return nil, err
			}
			prospectID := strings.TrimSpace(newID())
			if prospectID == "" {
				return nil, aiassist.ErrInvalidTool
			}
			return marshalPrepared(prospectCreatePrepared{
				ProspectID: prospectID, DisplayID: request.DisplayID, Name: request.Name,
				Email: request.Email, Phone: request.Phone,
			})
		},
		validate: validateProspectCreate,
		resolve: func(principal authorization.Principal, _ json.RawMessage) (scope.Target, error) {
			if strings.TrimSpace(principal.Scope.MSPID) == "" {
				return scope.Target{}, aiassist.ErrInvalidTool
			}
			return scope.Target{MSPID: principal.Scope.MSPID}, nil
		},
		preview: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (aiassist.Preview, error) {
			input, err := validProspectCreate(raw)
			if err != nil {
				return aiassist.Preview{}, err
			}
			if err := ensureProspectIdentityAvailable(ctx, actions, principal, input.DisplayID, input.Name); err != nil {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			changes := map[string]aiassist.Change{
				"display_id": {Before: nil, After: input.DisplayID},
				"name":       {Before: nil, After: input.Name},
				"state":      {Before: nil, After: "active"},
			}
			if input.Email != nil {
				changes["email"] = aiassist.Change{Before: nil, After: *input.Email}
			}
			if input.Phone != nil {
				changes["phone"] = aiassist.Change{Before: nil, After: *input.Phone}
			}
			return aiassist.Preview{
				Summary:    "Create prospect " + input.DisplayID,
				TargetType: "prospect", TargetID: input.ProspectID, Changes: changes,
			}, nil
		},
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, correlationID string) (aiassist.ToolResult, error) {
			input, err := validProspectCreate(raw)
			if err != nil || strings.TrimSpace(correlationID) == "" || actions == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			command := sales.CreateProspectCommand{
				Principal: principal, ProspectID: input.ProspectID,
				DisplayID: input.DisplayID, Name: input.Name,
				ActorID: principal.ID, Source: "ai_workspace", CorrelationID: correlationID,
			}
			if input.Email != nil {
				command.Email = *input.Email
			}
			if input.Phone != nil {
				command.Phone = *input.Phone
			}
			prospect, err := actions.CreateProspect(ctx, command)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{Summary: "Created prospect " + prospect.DisplayID, Data: map[string]any{
				"prospect_id": prospect.ID, "display_id": prospect.DisplayID,
				"name": prospect.Name, "version": prospect.Version,
			}}, nil
		},
	}
}

func NewTicketRouteTool(directory ActiveClientResolver, actions TicketRouteActions) aiassist.Tool {
	return &functionalTool{
		name: "ticket.route", capability: "work_record.route", kind: aiassist.ToolWrite,
		prepare: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (json.RawMessage, error) {
			request, err := decode[ticketRouteRequest](raw)
			if err != nil || directory == nil || actions == nil {
				return nil, aiassist.ErrInvalidTool
			}
			request.Client, request.Ticket = strings.TrimSpace(request.Client), strings.TrimSpace(request.Ticket)
			request.Queue, request.Reason = strings.TrimSpace(request.Queue), strings.TrimSpace(request.Reason)
			if request.Client == "" || request.Ticket == "" || request.Queue == "" ||
				request.ExpectedVersion < 1 || request.Reason == "" {
				return nil, aiassist.ErrInvalidTool
			}
			client, err := resolveActiveResourceClient(ctx, directory, principal, "work_record.route", request.Client)
			if err != nil {
				return nil, aiassist.ErrInvalidTool
			}
			preflight, err := actions.Preflight(ctx, workrecords.QueuePreflightCommand{
				Principal: principal, Target: target(principal, client.ID),
				WorkRecordReference: request.Ticket, QueueReference: request.Queue,
				ExpectedVersion: request.ExpectedVersion,
			})
			if err != nil {
				return nil, err
			}
			return marshalPrepared(ticketRoutePrepared{
				ClientID: client.ID, ClientReference: request.Client, ClientName: client.Name, ClientDisplayID: client.DisplayID,
				TicketID: preflight.Record.ID, TicketReference: request.Ticket,
				TicketDisplayID: preflight.Record.DisplayID, TicketTitle: preflight.Record.Title,
				CurrentQueueID: preflight.CurrentQueue.ID, CurrentQueueClientID: preflight.CurrentQueue.ClientID,
				CurrentQueueKey: preflight.CurrentQueue.Key, CurrentQueueName: preflight.CurrentQueue.Name,
				CurrentQueueVersion: preflight.CurrentQueue.Version, QueueID: preflight.Queue.ID,
				QueueClientID:  preflight.Queue.ClientID,
				QueueReference: request.Queue, QueueKey: preflight.Queue.Key,
				QueueName: preflight.Queue.Name, QueueVersion: preflight.Queue.Version,
				ExpectedVersion: request.ExpectedVersion, Reason: request.Reason,
			})
		},
		validate: validateTicketRoute,
		resolve:  resolveClientResourceScope,
		preview: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (aiassist.Preview, error) {
			input, err := validTicketRoute(raw)
			if err != nil {
				return aiassist.Preview{}, err
			}
			client, err := resolveActiveResourceClient(ctx, directory, principal, "work_record.route", input.ClientReference)
			if err != nil || client.ID != input.ClientID || client.Name != input.ClientName ||
				client.DisplayID != input.ClientDisplayID {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			preflight, err := actions.Preflight(ctx, workrecords.QueuePreflightCommand{
				Principal: principal, Target: target(principal, input.ClientID),
				WorkRecordReference: input.TicketReference, QueueReference: input.QueueReference,
				ExpectedVersion: input.ExpectedVersion,
			})
			if err != nil {
				return aiassist.Preview{}, err
			}
			if preflight.Record.ID != input.TicketID || preflight.Record.DisplayID != input.TicketDisplayID ||
				preflight.Record.Title != input.TicketTitle || preflight.Record.QueueID != input.CurrentQueueID ||
				preflight.CurrentQueue.ID != input.CurrentQueueID ||
				preflight.CurrentQueue.ClientID != input.CurrentQueueClientID ||
				preflight.CurrentQueue.Key != input.CurrentQueueKey ||
				preflight.CurrentQueue.Name != input.CurrentQueueName ||
				preflight.CurrentQueue.Version != input.CurrentQueueVersion ||
				preflight.Queue.ID != input.QueueID || preflight.Queue.ClientID != input.QueueClientID ||
				preflight.Queue.Key != input.QueueKey ||
				preflight.Queue.Name != input.QueueName || preflight.Queue.Version != input.QueueVersion {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			return ticketRoutePreview(input), nil
		},
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, correlationID string) (aiassist.ToolResult, error) {
			input, err := validTicketRoute(raw)
			if err != nil || strings.TrimSpace(correlationID) == "" || actions == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			record, err := actions.Transfer(ctx, workrecords.QueueCommand{
				Principal: principal, Target: target(principal, input.ClientID),
				WorkRecordID: input.TicketID, ExpectedVersion: input.ExpectedVersion,
				Queue: workrecords.QueueRef{
					ID: input.QueueID, MSPID: principal.Scope.MSPID, ClientID: input.QueueClientID,
					Key: input.QueueKey, Name: input.QueueName, Version: input.QueueVersion,
				},
				Actor:  workrecords.Actor{Type: "technician", ID: principal.ID, Source: "ai_workspace"},
				Reason: input.Reason, CorrelationID: correlationID,
			})
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{Summary: "Routed ticket " + input.TicketDisplayID + " to " + input.QueueName, Data: map[string]any{
				"ticket_id": record.ID, "display_id": record.DisplayID,
				"queue_id": record.QueueID, "version": record.Version,
			}}, nil
		},
	}
}

func validKnowledgeDraftCreate(raw json.RawMessage) (knowledgeDraftCreatePrepared, error) {
	input, err := decode[knowledgeDraftCreatePrepared](raw)
	if err != nil || strings.TrimSpace(input.ClientID) == "" || strings.TrimSpace(input.ClientReference) == "" ||
		strings.TrimSpace(input.ClientName) == "" || strings.TrimSpace(input.ClientDisplayID) == "" ||
		strings.TrimSpace(input.ArticleID) == "" || strings.TrimSpace(input.DisplayID) == "" ||
		strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Body) == "" {
		return knowledgeDraftCreatePrepared{}, aiassist.ErrInvalidTool
	}
	return input, nil
}

func validateKnowledgeDraftCreate(raw json.RawMessage) error {
	_, err := validKnowledgeDraftCreate(raw)
	return err
}

func validKnowledgeDraftRevise(raw json.RawMessage) (knowledgeDraftRevisePrepared, error) {
	input, err := decode[knowledgeDraftRevisePrepared](raw)
	if err != nil || strings.TrimSpace(input.ClientID) == "" || strings.TrimSpace(input.ClientReference) == "" ||
		strings.TrimSpace(input.ClientName) == "" || strings.TrimSpace(input.ClientDisplayID) == "" ||
		strings.TrimSpace(input.ArticleID) == "" || strings.TrimSpace(input.ArticleReference) == "" ||
		strings.TrimSpace(input.ArticleDisplayID) == "" || strings.TrimSpace(input.ArticleTitle) == "" ||
		strings.TrimSpace(input.ArticleBody) == "" ||
		input.ArticleState != knowledge.Draft || input.ExpectedVersion < 1 ||
		strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Body) == "" {
		return knowledgeDraftRevisePrepared{}, aiassist.ErrInvalidTool
	}
	return input, nil
}

func validateKnowledgeDraftRevise(raw json.RawMessage) error {
	_, err := validKnowledgeDraftRevise(raw)
	return err
}

func exactDraftArticle(
	ctx context.Context,
	actions KnowledgeDraftActions,
	principal authorization.Principal,
	clientID string,
	reference string,
) (knowledge.ArticleDetail, error) {
	target := target(principal, clientID)
	matches, err := actions.ResolveDraftReference(ctx, knowledge.ResolveReferenceCommand{
		Principal: principal, Target: target, Reference: reference,
	})
	if err != nil {
		return knowledge.ArticleDetail{}, err
	}
	if len(matches) > 1 {
		return knowledge.ArticleDetail{}, knowledge.ErrAmbiguousReference
	}
	if len(matches) == 0 || strings.TrimSpace(matches[0].ID) == "" {
		return knowledge.ArticleDetail{}, scope.ErrNotFound
	}
	detail, err := actions.FindDraft(ctx, knowledge.FindCommand{
		Principal: principal, Target: target, ArticleID: matches[0].ID,
	})
	if err != nil {
		return knowledge.ArticleDetail{}, err
	}
	if detail.Article.ID != matches[0].ID || detail.Article.MSPID != target.MSPID ||
		detail.Article.ClientID != target.ClientID || detail.Article.State != knowledge.Draft ||
		detail.Article.ClientVisible || detail.Version.State != knowledge.Draft ||
		detail.Version.Version != detail.Article.CurrentVersion {
		return knowledge.ArticleDetail{}, scope.ErrNotFound
	}
	return detail, nil
}

func ensureKnowledgeDraftIdentityAvailable(
	ctx context.Context,
	actions KnowledgeDraftActions,
	principal authorization.Principal,
	clientID string,
	displayID string,
	title string,
) error {
	for _, reference := range []string{displayID, title} {
		matches, err := actions.ResolveDraftReference(ctx, knowledge.ResolveReferenceCommand{
			Principal: principal, Target: target(principal, clientID), Reference: reference,
		})
		if err != nil {
			return err
		}
		if len(matches) != 0 {
			return aiassist.ErrInvalidTool
		}
	}
	return nil
}

func knowledgeDraftRevisePreview(input knowledgeDraftRevisePrepared, detail knowledge.ArticleDetail) aiassist.Preview {
	return aiassist.Preview{
		Summary:    fmt.Sprintf("Revise knowledge draft %s version %d", detail.Article.DisplayID, input.ExpectedVersion),
		TargetType: "knowledge_article", TargetID: input.ArticleID, TargetVersion: input.ExpectedVersion,
		Changes: map[string]aiassist.Change{
			"client": {
				Before: nil,
				After: map[string]string{
					"display_id": input.ClientDisplayID, "name": input.ClientName,
				},
			},
			"article": {
				Before: map[string]any{
					"display_id": detail.Article.DisplayID,
					"title":      detail.Article.Title,
					"version":    detail.Article.CurrentVersion,
				},
				After: map[string]any{
					"display_id": detail.Article.DisplayID,
					"title":      input.Title,
					"version":    detail.Article.CurrentVersion + 1,
				},
			},
			"title": {Before: detail.Article.Title, After: input.Title},
			"body":  {Before: detail.Version.Body, After: input.Body},
		},
	}
}

func normalizedOptional(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	return &normalized
}

func validProspectCreate(raw json.RawMessage) (prospectCreatePrepared, error) {
	input, err := decode[prospectCreatePrepared](raw)
	if err != nil || strings.TrimSpace(input.ProspectID) == "" ||
		strings.TrimSpace(input.DisplayID) == "" || strings.TrimSpace(input.Name) == "" ||
		(input.Email != nil && strings.TrimSpace(*input.Email) == "") ||
		(input.Phone != nil && strings.TrimSpace(*input.Phone) == "") {
		return prospectCreatePrepared{}, aiassist.ErrInvalidTool
	}
	return input, nil
}

func validateProspectCreate(raw json.RawMessage) error {
	_, err := validProspectCreate(raw)
	return err
}

func ensureProspectIdentityAvailable(
	ctx context.Context,
	actions ProspectCreateActions,
	principal authorization.Principal,
	displayID string,
	name string,
) error {
	target := scope.Target{MSPID: principal.Scope.MSPID}
	for _, reference := range []string{displayID, name} {
		matches, err := actions.ResolveProspectReference(ctx, principal, target, reference)
		if err != nil {
			return err
		}
		if len(matches) != 0 {
			return aiassist.ErrInvalidTool
		}
	}
	return nil
}

func validTicketRoute(raw json.RawMessage) (ticketRoutePrepared, error) {
	input, err := decode[ticketRoutePrepared](raw)
	if err != nil || strings.TrimSpace(input.ClientID) == "" || strings.TrimSpace(input.ClientReference) == "" ||
		strings.TrimSpace(input.ClientName) == "" || strings.TrimSpace(input.ClientDisplayID) == "" ||
		strings.TrimSpace(input.TicketID) == "" || strings.TrimSpace(input.TicketReference) == "" ||
		strings.TrimSpace(input.TicketDisplayID) == "" || strings.TrimSpace(input.TicketTitle) == "" ||
		strings.TrimSpace(input.QueueID) == "" || strings.TrimSpace(input.QueueReference) == "" ||
		strings.TrimSpace(input.QueueKey) == "" || strings.TrimSpace(input.QueueName) == "" ||
		(input.QueueClientID != "" && input.QueueClientID != input.ClientID) ||
		(input.CurrentQueueID == "" &&
			(input.CurrentQueueClientID != "" || input.CurrentQueueKey != "" ||
				input.CurrentQueueName != "" || input.CurrentQueueVersion != 0)) ||
		(input.CurrentQueueID != "" &&
			(strings.TrimSpace(input.CurrentQueueKey) == "" ||
				strings.TrimSpace(input.CurrentQueueName) == "" ||
				input.CurrentQueueVersion < 1 ||
				(input.CurrentQueueClientID != "" && input.CurrentQueueClientID != input.ClientID))) ||
		input.QueueVersion < 1 || input.ExpectedVersion < 1 || strings.TrimSpace(input.Reason) == "" {
		return ticketRoutePrepared{}, aiassist.ErrInvalidTool
	}
	return input, nil
}

func validateTicketRoute(raw json.RawMessage) error {
	_, err := validTicketRoute(raw)
	return err
}

func ticketRoutePreview(input ticketRoutePrepared) aiassist.Preview {
	return aiassist.Preview{
		Summary:    "Route ticket " + input.TicketDisplayID + " to " + input.QueueName,
		TargetType: "work_record", TargetID: input.TicketID, TargetVersion: input.ExpectedVersion,
		Changes: map[string]aiassist.Change{
			"client": {
				Before: nil,
				After: map[string]string{
					"display_id": input.ClientDisplayID, "name": input.ClientName,
				},
			},
			"ticket": {
				Before: map[string]any{
					"display_id": input.TicketDisplayID,
					"title":      input.TicketTitle, "version": input.ExpectedVersion,
				},
				After: map[string]any{
					"display_id": input.TicketDisplayID,
					"title":      input.TicketTitle, "version": input.ExpectedVersion + 1,
				},
			},
			"queue": {
				Before: queuePreviewFacts(
					input.CurrentQueueID, input.CurrentQueueKey,
					input.CurrentQueueName, input.CurrentQueueVersion,
				),
				After: queuePreviewFacts(
					input.QueueID, input.QueueKey, input.QueueName, input.QueueVersion,
				),
			},
			"reason": {Before: nil, After: input.Reason},
		},
	}
}

func queuePreviewFacts(id, key, name string, version int64) any {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	return map[string]any{
		"id": id, "key": key, "name": name, "version": version,
	}
}

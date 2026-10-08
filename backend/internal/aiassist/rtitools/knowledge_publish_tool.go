package rtitools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

// KnowledgePublishActions is the deliberately narrow ordinary Knowledge
// boundary used by the AI publication adapter. It neither composes content nor
// exposes client-visible or delivery controls.
type KnowledgePublishActions interface {
	ResolvePublicationReference(context.Context, knowledge.ResolveReferenceCommand) ([]knowledge.Article, error)
	FindPublicationDraft(context.Context, knowledge.FindCommand) (knowledge.ArticleDetail, error)
	Publish(context.Context, knowledge.PublishCommand) (knowledge.Version, error)
}

// knowledgePublishRequest is the complete public schema. Strict decoding
// rejects client visibility, recipients, delivery, and internal identities.
type knowledgePublishRequest struct {
	Client          string `json:"client"`
	Article         string `json:"article"`
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

// knowledgePublishPrepared holds only canonical identities, version fences,
// and the supplied audit reason. It is never a public tool schema.
type knowledgePublishPrepared struct {
	ClientID         string `json:"client_id"`
	ClientReference  string `json:"client_reference"`
	ClientDisplayID  string `json:"client_display_id"`
	ClientName       string `json:"client_name"`
	ClientVersion    int64  `json:"client_version"`
	ArticleID        string `json:"article_id"`
	ArticleReference string `json:"article_reference"`
	ArticleDisplayID string `json:"article_display_id"`
	ArticleTitle     string `json:"article_title"`
	ExpectedVersion  int64  `json:"expected_version"`
	Reason           string `json:"reason"`
}

func NewKnowledgePublishTool(
	directory ActiveClientResolver,
	actions KnowledgePublishActions,
) aiassist.Tool {
	const capability = "knowledge.publish"
	return &functionalTool{
		name: "knowledge.publish", capability: capability, kind: aiassist.ToolWrite,
		prepare: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (json.RawMessage, error) {
			request, err := decode[knowledgePublishRequest](raw)
			if err != nil || directory == nil || actions == nil {
				return nil, aiassist.ErrInvalidTool
			}
			request.Client, request.Article, request.Reason = strings.TrimSpace(request.Client), strings.TrimSpace(request.Article), strings.TrimSpace(request.Reason)
			if request.Client == "" || request.Article == "" || request.ExpectedVersion < 1 || request.Reason == "" {
				return nil, aiassist.ErrInvalidTool
			}
			client, err := resolveActiveResourceClient(ctx, directory, principal, capability, request.Client)
			if err != nil {
				return nil, err
			}
			input, err := prepareKnowledgePublication(ctx, actions, principal, client, request)
			if err != nil {
				return nil, err
			}
			return marshalPrepared(input)
		},
		validate: validateKnowledgePublishPrepared,
		resolve:  resolveClientResourceScope,
		preview: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (aiassist.Preview, error) {
			input, err := validKnowledgePublishPrepared(raw)
			if err != nil {
				return aiassist.Preview{}, err
			}
			if err := recheckKnowledgePublication(ctx, directory, actions, principal, capability, input); err != nil {
				return aiassist.Preview{}, err
			}
			return knowledgePublicationPreview(input), nil
		},
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, correlationID string) (aiassist.ToolResult, error) {
			input, err := validKnowledgePublishPrepared(raw)
			if err != nil || actions == nil || strings.TrimSpace(correlationID) == "" {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			if err := recheckKnowledgePublication(ctx, directory, actions, principal, capability, input); err != nil {
				return aiassist.ToolResult{}, err
			}
			version, err := actions.Publish(ctx, knowledge.PublishCommand{
				Principal: principal, Target: target(principal, input.ClientID),
				ArticleID: input.ArticleID, ExpectedVersion: input.ExpectedVersion,
				ExpectedClientVersion: input.ClientVersion, ActorID: principal.ID,
				Source: "ai_workspace", Reason: input.Reason, CorrelationID: correlationID,
			})
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{Summary: "Published internal knowledge article " + input.ArticleDisplayID, Data: map[string]any{
				"article_id": input.ArticleID, "display_id": input.ArticleDisplayID,
				"state": string(version.State), "version": version.Version,
			}}, nil
		},
	}
}

func prepareKnowledgePublication(ctx context.Context, actions KnowledgePublishActions, principal authorization.Principal, client organizations.Client, request knowledgePublishRequest) (knowledgePublishPrepared, error) {
	if !validPublicationClient(client, principal) {
		return knowledgePublishPrepared{}, aiassist.ErrInvalidTool
	}
	detail, err := exactPublicationDraft(ctx, actions, principal, client.ID, request.Article)
	if err != nil {
		return knowledgePublishPrepared{}, err
	}
	if detail.Article.CurrentVersion != request.ExpectedVersion {
		return knowledgePublishPrepared{}, object.ErrVersionConflict
	}
	return knowledgePublishPrepared{
		ClientID: client.ID, ClientReference: request.Client, ClientDisplayID: client.DisplayID,
		ClientName: client.Name, ClientVersion: client.Version, ArticleID: detail.Article.ID,
		ArticleReference: request.Article, ArticleDisplayID: detail.Article.DisplayID,
		ArticleTitle: detail.Article.Title, ExpectedVersion: request.ExpectedVersion, Reason: request.Reason,
	}, nil
}

func recheckKnowledgePublication(ctx context.Context, directory ActiveClientResolver, actions KnowledgePublishActions, principal authorization.Principal, capability string, input knowledgePublishPrepared) error {
	if directory == nil || actions == nil {
		return aiassist.ErrInvalidTool
	}
	client, err := resolveActiveResourceClient(ctx, directory, principal, capability, input.ClientReference)
	if err != nil || !samePublicationClient(input, client) {
		return aiassist.ErrProposalStale
	}
	detail, err := exactPublicationDraft(ctx, actions, principal, input.ClientID, input.ArticleReference)
	if err != nil {
		return err
	}
	if detail.Article.ID != input.ArticleID || detail.Article.DisplayID != input.ArticleDisplayID ||
		detail.Article.Title != input.ArticleTitle || detail.Article.CurrentVersion != input.ExpectedVersion ||
		detail.Version.Version != input.ExpectedVersion {
		return object.ErrVersionConflict
	}
	return nil
}

func exactPublicationDraft(ctx context.Context, actions KnowledgePublishActions, principal authorization.Principal, clientID, reference string) (knowledge.ArticleDetail, error) {
	target := target(principal, clientID)
	matches, err := actions.ResolvePublicationReference(ctx, knowledge.ResolveReferenceCommand{Principal: principal, Target: target, Reference: reference})
	if err != nil {
		return knowledge.ArticleDetail{}, err
	}
	if len(matches) > 1 {
		return knowledge.ArticleDetail{}, knowledge.ErrAmbiguousReference
	}
	if len(matches) == 0 || strings.TrimSpace(matches[0].ID) == "" {
		return knowledge.ArticleDetail{}, scope.ErrNotFound
	}
	detail, err := actions.FindPublicationDraft(ctx, knowledge.FindCommand{Principal: principal, Target: target, ArticleID: matches[0].ID})
	if err != nil {
		return knowledge.ArticleDetail{}, err
	}
	if detail.Article.ID != matches[0].ID || detail.Article.MSPID != target.MSPID || detail.Article.ClientID != target.ClientID ||
		strings.TrimSpace(detail.Article.DisplayID) == "" || strings.TrimSpace(detail.Article.Title) == "" ||
		detail.Article.State != knowledge.Draft || detail.Article.ClientVisible || detail.Article.CurrentVersion < 1 ||
		detail.Version.ArticleID != detail.Article.ID || detail.Version.State != knowledge.Draft || detail.Version.Version != detail.Article.CurrentVersion ||
		strings.TrimSpace(detail.Version.Body) == "" || detail.Version.PublishedAt != nil {
		return knowledge.ArticleDetail{}, scope.ErrNotFound
	}
	return detail, nil
}

func validPublicationClient(client organizations.Client, principal authorization.Principal) bool {
	return client.MSPID == principal.Scope.MSPID && client.ClientID == client.ID && client.LifecycleState == "active" && strings.TrimSpace(client.ID) != "" &&
		strings.TrimSpace(client.DisplayID) != "" && strings.TrimSpace(client.Name) != "" && client.Version > 0
}

func samePublicationClient(input knowledgePublishPrepared, client organizations.Client) bool {
	return validPublicationClient(client, authorization.Principal{Scope: scope.Principal{MSPID: client.MSPID}}) &&
		client.ID == input.ClientID && client.DisplayID == input.ClientDisplayID && client.Name == input.ClientName && client.Version == input.ClientVersion
}

func validateKnowledgePublishPrepared(raw json.RawMessage) error {
	_, err := validKnowledgePublishPrepared(raw)
	return err
}

func validKnowledgePublishPrepared(raw json.RawMessage) (knowledgePublishPrepared, error) {
	input, err := decode[knowledgePublishPrepared](raw)
	if err != nil || strings.TrimSpace(input.ClientID) == "" || strings.TrimSpace(input.ClientReference) == "" ||
		strings.TrimSpace(input.ClientDisplayID) == "" || strings.TrimSpace(input.ClientName) == "" || input.ClientVersion < 1 ||
		strings.TrimSpace(input.ArticleID) == "" || strings.TrimSpace(input.ArticleReference) == "" ||
		strings.TrimSpace(input.ArticleDisplayID) == "" || strings.TrimSpace(input.ArticleTitle) == "" ||
		input.ExpectedVersion < 1 || strings.TrimSpace(input.Reason) == "" {
		return knowledgePublishPrepared{}, aiassist.ErrInvalidTool
	}
	return input, nil
}

func knowledgePublicationPreview(input knowledgePublishPrepared) aiassist.Preview {
	return aiassist.Preview{
		Summary:    "Publish internal knowledge article " + input.ArticleDisplayID + " (internal only; no external delivery)",
		TargetType: "knowledge_article", TargetID: input.ArticleID, TargetVersion: input.ExpectedVersion,
		Changes: map[string]aiassist.Change{
			"state":    {Before: "draft", After: "published"},
			"version":  {Before: input.ExpectedVersion, After: input.ExpectedVersion},
			"title":    {Before: input.ArticleTitle, After: input.ArticleTitle},
			"reason":   {Before: nil, After: input.Reason},
			"delivery": {Before: nil, After: "internal only; no external delivery"},
		},
	}
}

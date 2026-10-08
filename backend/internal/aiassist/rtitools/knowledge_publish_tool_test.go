package rtitools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

const knowledgePublishPublicRequest = `{"client":"Northwind Legal","article":"KB-2042","expected_version":2,"reason":"Reviewed and approved for internal use"}`

type knowledgePublishActionsStub struct {
	matches      []knowledge.Article
	detail       knowledge.ArticleDetail
	resolveCalls int
	findCalls    int
	published    knowledge.PublishCommand
	publishCalls int
}

func (s *knowledgePublishActionsStub) ResolvePublicationReference(
	_ context.Context,
	_ knowledge.ResolveReferenceCommand,
) ([]knowledge.Article, error) {
	s.resolveCalls++
	return s.matches, nil
}

func (s *knowledgePublishActionsStub) FindPublicationDraft(
	_ context.Context,
	_ knowledge.FindCommand,
) (knowledge.ArticleDetail, error) {
	s.findCalls++
	return s.detail, nil
}

func (s *knowledgePublishActionsStub) Publish(
	_ context.Context,
	command knowledge.PublishCommand,
) (knowledge.Version, error) {
	s.publishCalls++
	s.published = command
	return knowledge.Version{
		ArticleID: command.ArticleID, Version: command.ExpectedVersion,
		State: knowledge.Published,
	}, nil
}

func knowledgePublishFixture() *knowledgePublishActionsStub {
	article := knowledge.Article{
		ID: "article-1", MSPID: "msp-1", ClientID: "client-1",
		DisplayID: "KB-2042", Title: "VPN recovery", State: knowledge.Draft,
		CurrentVersion: 2,
	}
	return &knowledgePublishActionsStub{
		matches: []knowledge.Article{article},
		detail: knowledge.ArticleDetail{
			Article: article,
			Version: knowledge.Version{
				ArticleID: article.ID, Version: 2, Body: "Internal recovery steps",
				State: knowledge.Draft,
			},
		},
	}
}

func knowledgePublishPrincipal() authorization.Principal {
	return authorization.Principal{
		ID: "technician-1", Scope: scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet("knowledge.publish"),
	}
}

func TestKnowledgePublishToolPreviewsInternalPublicationAndDelegatesOnlyOnConfirmation(t *testing.T) {
	directory, actions := activeResourceDirectory(), knowledgePublishFixture()
	directory.resolved.Version = 4
	tool := NewKnowledgePublishTool(directory, actions)
	principal := knowledgePublishPrincipal()
	store := &composedProposalStore{}
	registry, err := aiassist.NewRegistry(
		[]aiassist.Tool{tool}, store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil },
		func() time.Time { return time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC) },
		func() string { return "knowledge-publication-proposal" },
		&composedTargetAuthorizer{},
	)
	if err != nil {
		t.Fatal(err)
	}

	proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{
		Name: "knowledge.publish", ConversationID: "conversation-1",
		Input: json.RawMessage(knowledgePublishPublicRequest),
	})
	if err != nil {
		t.Fatalf("Propose() error=%v", err)
	}
	if actions.publishCalls != 0 || proposal.Preview.TargetID != "article-1" ||
		proposal.Preview.TargetVersion != 2 ||
		proposal.Preview.Changes["state"] != (aiassist.Change{Before: "draft", After: "published"}) ||
		proposal.Preview.Changes["version"] != (aiassist.Change{Before: int64(2), After: int64(2)}) ||
		proposal.Preview.Changes["reason"] != (aiassist.Change{Before: nil, After: "Reviewed and approved for internal use"}) ||
		proposal.Preview.Changes["delivery"] != (aiassist.Change{Before: nil, After: "internal only; no external delivery"}) {
		t.Fatalf("proposal=%+v publishes=%d", proposal, actions.publishCalls)
	}
	result, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version)
	if err != nil {
		t.Fatalf("Confirm() error=%v", err)
	}
	if actions.publishCalls != 1 || actions.published.ArticleID != "article-1" ||
		actions.published.ExpectedVersion != 2 || actions.published.ExpectedClientVersion != 4 ||
		actions.published.Reason != "Reviewed and approved for internal use" ||
		actions.published.Source != "ai_workspace" || actions.published.ActorID != principal.ID ||
		actions.published.CorrelationID == "" || result.Data["state"] != "published" {
		t.Fatalf("publish=%+v calls=%d result=%+v", actions.published, actions.publishCalls, result)
	}
}

func TestKnowledgePublishToolRejectsClosedPublicSchemaBeforeLookup(t *testing.T) {
	for _, raw := range []string{
		`{"client":"Northwind Legal","article":"KB-2042","expected_version":2,"reason":"Reviewed","client_visible":true}`,
		`{"client":"Northwind Legal","article":"KB-2042","expected_version":2,"reason":"Reviewed","recipient":"client@example.com"}`,
		`{"client":"Northwind Legal","article":"KB-2042","expected_version":2,"reason":"Reviewed","delivery":"email"}`,
		`{"client":"Northwind Legal","article":"KB-2042","expected_version":2,"reason":"Reviewed","article_id":"article-1"}`,
		`{"client":"Northwind Legal","article":"KB-2042","expected_version":2,"reason":"Reviewed","unknown":"value"}`,
	} {
		directory, actions := activeResourceDirectory(), knowledgePublishFixture()
		tool := NewKnowledgePublishTool(directory, actions)
		_, err := tool.(aiassist.ToolInputPreparer).Prepare(
			context.Background(), knowledgePublishPrincipal(), json.RawMessage(raw),
		)
		if !errors.Is(err, aiassist.ErrInvalidTool) || len(directory.resolveCalls) != 0 || actions.resolveCalls != 0 || actions.findCalls != 0 || actions.publishCalls != 0 {
			t.Fatalf("Prepare(%s) error=%v directory=%d resolve=%d find=%d publish=%d", raw, err, len(directory.resolveCalls), actions.resolveCalls, actions.findCalls, actions.publishCalls)
		}
	}
}

func TestKnowledgePublishToolRejectsUnsafeOrStaleDraftsBeforeProposal(t *testing.T) {
	for _, mutate := range []struct {
		name  string
		apply func(*knowledgePublishActionsStub)
	}{
		{name: "client visible", apply: func(actions *knowledgePublishActionsStub) { actions.detail.Article.ClientVisible = true }},
		{name: "empty body", apply: func(actions *knowledgePublishActionsStub) { actions.detail.Version.Body = " \n " }},
		{name: "already published", apply: func(actions *knowledgePublishActionsStub) {
			actions.detail.Article.State, actions.detail.Version.State = knowledge.Published, knowledge.Published
		}},
		{name: "ambiguous article", apply: func(actions *knowledgePublishActionsStub) {
			actions.matches = append(actions.matches, actions.matches[0])
		}},
		{name: "stale version", apply: func(actions *knowledgePublishActionsStub) {
			actions.detail.Article.CurrentVersion, actions.detail.Version.Version = 3, 3
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			actions := knowledgePublishFixture()
			mutate.apply(actions)
			tool := NewKnowledgePublishTool(activeResourceDirectory(), actions)
			_, err := tool.(aiassist.ToolInputPreparer).Prepare(
				context.Background(), knowledgePublishPrincipal(), json.RawMessage(knowledgePublishPublicRequest),
			)
			if err == nil || actions.publishCalls != 0 {
				t.Fatalf("Prepare() error=%v publishes=%d", err, actions.publishCalls)
			}
		})
	}
}

func TestKnowledgePublishToolRejectsConfirmationDriftBeforePublication(t *testing.T) {
	for _, mutate := range []struct {
		name  string
		apply func(*resourceDirectoryStub, *knowledgePublishActionsStub, *authorization.Principal)
	}{
		{name: "inactive client", apply: func(directory *resourceDirectoryStub, _ *knowledgePublishActionsStub, _ *authorization.Principal) {
			directory.err = scope.ErrNotFound
		}},
		{name: "article changed", apply: func(_ *resourceDirectoryStub, actions *knowledgePublishActionsStub, _ *authorization.Principal) {
			actions.detail.Article.CurrentVersion, actions.detail.Version.Version = 3, 3
		}},
		{name: "publication capability lost", apply: func(_ *resourceDirectoryStub, _ *knowledgePublishActionsStub, principal *authorization.Principal) {
			principal.Capabilities = authorization.NewCapabilitySet()
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			directory, actions, principal := activeResourceDirectory(), knowledgePublishFixture(), knowledgePublishPrincipal()
			directory.resolved.Version = 4
			tool := NewKnowledgePublishTool(directory, actions)
			store := &composedProposalStore{}
			registry, err := aiassist.NewRegistry(
				[]aiassist.Tool{tool}, store,
				func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil },
				func() time.Time { return time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC) },
				func() string { return "proposal-id" }, &composedTargetAuthorizer{},
			)
			if err != nil {
				t.Fatal(err)
			}
			proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{Name: "knowledge.publish", ConversationID: "conversation-1", Input: json.RawMessage(knowledgePublishPublicRequest)})
			if err != nil {
				t.Fatal(err)
			}
			mutate.apply(directory, actions, &principal)
			if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); err == nil || actions.publishCalls != 0 {
				t.Fatalf("Confirm() error=%v publishes=%d", err, actions.publishCalls)
			}
		})
	}
}

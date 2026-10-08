package rtitools

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

type operationalKnowledgeStub struct {
	matches []knowledge.Article
	detail  knowledge.ArticleDetail
	create  knowledge.CreateDraftCommand
	revise  knowledge.ReviseDraftCommand
	writes  int
}

func (s *operationalKnowledgeStub) ResolveDraftReference(_ context.Context, _ knowledge.ResolveReferenceCommand) ([]knowledge.Article, error) {
	return s.matches, nil
}
func (s *operationalKnowledgeStub) FindDraft(_ context.Context, _ knowledge.FindCommand) (knowledge.ArticleDetail, error) {
	return s.detail, nil
}
func (s *operationalKnowledgeStub) CreateDraft(_ context.Context, command knowledge.CreateDraftCommand) (knowledge.ArticleDetail, error) {
	s.writes++
	s.create = command
	return knowledge.ArticleDetail{Article: knowledge.Article{
		ID: command.ArticleID, DisplayID: command.DisplayID, Title: command.Title, CurrentVersion: 1,
	}}, nil
}
func (s *operationalKnowledgeStub) ReviseDraft(_ context.Context, command knowledge.ReviseDraftCommand) (knowledge.ArticleDetail, error) {
	s.writes++
	s.revise = command
	return knowledge.ArticleDetail{Article: knowledge.Article{
		ID: command.ArticleID, DisplayID: s.detail.Article.DisplayID, Title: command.Title,
		CurrentVersion: command.ExpectedVersion + 1,
	}}, nil
}

type operationalProspectStub struct {
	matches []sales.Prospect
	command sales.CreateProspectCommand
	writes  int
}

func (s *operationalProspectStub) ResolveProspectReference(context.Context, authorization.Principal, scope.Target, string) ([]sales.Prospect, error) {
	return s.matches, nil
}
func (s *operationalProspectStub) CreateProspect(_ context.Context, command sales.CreateProspectCommand) (sales.Prospect, error) {
	s.writes++
	s.command = command
	return sales.Prospect{ID: command.ProspectID, DisplayID: command.DisplayID, Name: command.Name, Email: command.Email, Phone: command.Phone, Version: 1}, nil
}

type operationalRouteStub struct {
	preflight workrecords.QueuePreflight
	command   workrecords.QueueCommand
	writes    int
}

func (s *operationalRouteStub) Preflight(context.Context, workrecords.QueuePreflightCommand) (workrecords.QueuePreflight, error) {
	return s.preflight, nil
}
func (s *operationalRouteStub) Transfer(_ context.Context, command workrecords.QueueCommand) (workrecords.Record, error) {
	s.writes++
	s.command = command
	record := s.preflight.Record
	record.QueueID = command.Queue.ID
	record.Version++
	return record, nil
}

func operationalPrincipal(capabilities ...string) authorization.Principal {
	return authorization.Principal{
		ID: "technician-1", Scope: scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet(capabilities...),
	}
}

func TestKnowledgeDraftCreatePreparesTrustedIdentityAndExactPreview(t *testing.T) {
	directory := activeResourceDirectory()
	actions := &operationalKnowledgeStub{}
	tool := NewKnowledgeDraftCreateTool(directory, actions, func() string { return "article-id" })
	principal := operationalPrincipal("knowledge.edit")
	prepared, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), principal, json.RawMessage(
		`{"client":"Northwind Legal","display_id":"KB-200","title":"VPN recovery","body":"Exact body."}`,
	))
	if err != nil {
		t.Fatalf("Prepare() error=%v", err)
	}
	if strings.Contains(string(prepared), `"actor"`) || strings.Contains(string(prepared), `"source"`) ||
		strings.Contains(string(prepared), `"correlation"`) {
		t.Fatalf("prepared public input contains trusted execution metadata: %s", prepared)
	}
	preview, err := tool.Preview(context.Background(), principal, prepared)
	if err != nil {
		t.Fatalf("Preview() error=%v", err)
	}
	if tool.RequiredCapability() != "knowledge.edit" || preview.TargetType != "knowledge_article" ||
		preview.TargetID != "article-id" || preview.Changes["display_id"].After != "KB-200" ||
		preview.Changes["title"].After != "VPN recovery" || preview.Changes["body"].After != "Exact body." {
		t.Fatalf("preview=%+v", preview)
	}
	if _, err := tool.Execute(context.Background(), principal, prepared, "proposal-correlation"); err != nil {
		t.Fatalf("Execute() error=%v", err)
	}
	if actions.create.ArticleID != "article-id" || actions.create.ActorID != principal.ID ||
		actions.create.Source != "ai_workspace" || actions.create.CorrelationID != "proposal-correlation" {
		t.Fatalf("command=%+v", actions.create)
	}
}

func TestKnowledgeDraftReviseResolvesCanonicalArticleAndRejectsDrift(t *testing.T) {
	directory := activeResourceDirectory()
	detail := knowledge.ArticleDetail{
		Article: knowledge.Article{
			ID: "article-id", MSPID: "msp-1", ClientID: "client-1", DisplayID: "KB-200",
			Title: "Old title", State: knowledge.Draft, CurrentVersion: 3,
		},
		Version: knowledge.Version{ArticleID: "article-id", Version: 3, Body: "Old body.", State: knowledge.Draft},
	}
	actions := &operationalKnowledgeStub{matches: []knowledge.Article{detail.Article}, detail: detail}
	tool := NewKnowledgeDraftReviseTool(directory, actions)
	principal := operationalPrincipal("knowledge.edit")
	prepared, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), principal, json.RawMessage(
		`{"client":"Northwind Legal","article":"KB-200","expected_version":3,"title":"New title","body":"New body."}`,
	))
	if err != nil {
		t.Fatalf("Prepare() error=%v", err)
	}
	preview, err := tool.Preview(context.Background(), principal, prepared)
	if err != nil || preview.TargetID != "article-id" || preview.TargetVersion != 3 ||
		preview.Changes["title"].Before != "Old title" || preview.Changes["body"].After != "New body." {
		t.Fatalf("preview=%+v error=%v", preview, err)
	}
	if !reflect.DeepEqual(
		preview.Changes["client"].After,
		map[string]string{"display_id": "NW-100", "name": "Northwind Legal"},
	) || !reflect.DeepEqual(
		preview.Changes["article"].Before,
		map[string]any{"display_id": "KB-200", "title": "Old title", "version": int64(3)},
	) {
		t.Fatalf("preview lacks canonical Client/Article identity: %+v", preview.Changes)
	}
	actions.detail.Article.CurrentVersion = 4
	actions.detail.Version.Version = 4
	if _, err := tool.Preview(context.Background(), principal, prepared); !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("drift Preview() error=%v", err)
	}
	if actions.writes != 0 {
		t.Fatalf("writes=%d", actions.writes)
	}
}

func TestProspectCreatePreservesOmittedOptionalContactData(t *testing.T) {
	actions := &operationalProspectStub{}
	tool := NewProspectCreateTool(actions, func() string { return "prospect-id" })
	principal := operationalPrincipal("prospect.create")
	prepared, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), principal, json.RawMessage(
		`{"display_id":"PRO-200","name":"Exact Prospect"}`,
	))
	if err != nil {
		t.Fatalf("Prepare() error=%v", err)
	}
	if strings.Contains(string(prepared), `"email"`) || strings.Contains(string(prepared), `"phone"`) {
		t.Fatalf("prepared input inferred contact data: %s", prepared)
	}
	preview, err := tool.Preview(context.Background(), principal, prepared)
	if err != nil || preview.TargetID != "prospect-id" || len(preview.Changes) != 3 {
		t.Fatalf("preview=%+v error=%v", preview, err)
	}
	if _, err := tool.Execute(context.Background(), principal, prepared, "proposal-correlation"); err != nil {
		t.Fatalf("Execute() error=%v", err)
	}
	if actions.command.Email != "" || actions.command.Phone != "" ||
		actions.command.ProspectID != "prospect-id" ||
		actions.command.CorrelationID != "proposal-correlation" {
		t.Fatalf("command=%+v", actions.command)
	}
}

func TestTicketRouteResolvesCanonicalClientTicketQueueAndRepreviewsDrift(t *testing.T) {
	directory := activeResourceDirectory()
	route := &operationalRouteStub{preflight: workrecords.QueuePreflight{
		Record: workrecords.Record{Envelope: object.Envelope{
			ID: "ticket-id", MSPID: "msp-1", ClientID: "client-1", DisplayID: "INC-200", Version: 5,
		}, Title: "VPN unavailable", QueueID: "triage"},
		CurrentQueue: workrecords.QueueRef{
			ID: "triage", MSPID: "msp-1", Key: "triage",
			Name: "Triage", Version: 4,
		},
		Queue: workrecords.QueueRef{ID: "service-desk", MSPID: "msp-1", Key: "service-desk", Name: "Service Desk", Version: 2},
	}}
	tool := NewTicketRouteTool(directory, route)
	principal := operationalPrincipal("work_record.route")
	prepared, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), principal, json.RawMessage(
		`{"client":"Northwind Legal","ticket":"INC-200","queue":"Service Desk","expected_version":5,"reason":"Escalate specialist issue"}`,
	))
	if err != nil {
		t.Fatalf("Prepare() error=%v", err)
	}
	preview, err := tool.Preview(context.Background(), principal, prepared)
	if err != nil || preview.TargetID != "ticket-id" || preview.TargetVersion != 5 ||
		preview.Changes["reason"].After != "Escalate specialist issue" {
		t.Fatalf("preview=%+v error=%v", preview, err)
	}
	if !reflect.DeepEqual(
		preview.Changes["client"].After,
		map[string]string{"display_id": "NW-100", "name": "Northwind Legal"},
	) || !reflect.DeepEqual(
		preview.Changes["ticket"].Before,
		map[string]any{"display_id": "INC-200", "title": "VPN unavailable", "version": int64(5)},
	) || !reflect.DeepEqual(
		preview.Changes["queue"].Before,
		map[string]any{"id": "triage", "key": "triage", "name": "Triage", "version": int64(4)},
	) || !reflect.DeepEqual(
		preview.Changes["queue"].After,
		map[string]any{"id": "service-desk", "key": "service-desk", "name": "Service Desk", "version": int64(2)},
	) {
		t.Fatalf("preview lacks canonical Client/Ticket/Queue facts: %+v", preview.Changes)
	}
	if _, err := tool.Execute(context.Background(), principal, prepared, "proposal-correlation"); err != nil {
		t.Fatalf("Execute() error=%v", err)
	}
	if route.command.WorkRecordID != "ticket-id" || route.command.Queue.ID != "service-desk" ||
		route.command.Queue.ClientID != "" || route.command.Actor.ID != principal.ID ||
		route.command.Actor.Source != "ai_workspace" ||
		route.command.CorrelationID != "proposal-correlation" {
		t.Fatalf("command=%+v", route.command)
	}
	route.preflight.Queue.Name = "Escalations"
	if changed, err := tool.Preview(context.Background(), principal, prepared); err == nil {
		t.Fatalf("confirmation preview did not detect queue drift: %+v", changed)
	}
	if route.writes != 1 {
		t.Fatalf("writes=%d", route.writes)
	}
}

func TestOperationalWriteToolsRejectUnknownAndUserTrustedFields(t *testing.T) {
	principal := operationalPrincipal("knowledge.edit", "prospect.create", "work_record.route")
	tests := []struct {
		tool aiassist.Tool
		raw  string
	}{
		{NewKnowledgeDraftCreateTool(activeResourceDirectory(), &operationalKnowledgeStub{}, func() string { return "id" }), `{"client":"Northwind Legal","display_id":"KB-1","title":"T","body":"B","article_id":"spoof"}`},
		{NewProspectCreateTool(&operationalProspectStub{}, func() string { return "id" }), `{"display_id":"PRO-1","name":"P","correlation_id":"spoof"}`},
		{NewTicketRouteTool(activeResourceDirectory(), &operationalRouteStub{}), `{"client":"Northwind Legal","ticket":"INC-1","queue":"Q","expected_version":1,"reason":"R","ticket_id":"spoof"}`},
	}
	for _, test := range tests {
		if _, err := test.tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), principal, json.RawMessage(test.raw)); err == nil {
			t.Fatalf("%s accepted trusted/unknown field", test.tool.Name())
		}
	}
}

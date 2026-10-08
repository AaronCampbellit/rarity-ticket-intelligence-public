package rtitools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/comments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/search"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

type functionalTool struct {
	name       string
	capability string
	kind       aiassist.ToolKind
	prepare    func(context.Context, authorization.Principal, json.RawMessage) (json.RawMessage, error)
	validate   func(json.RawMessage) error
	resolve    func(authorization.Principal, json.RawMessage) (scope.Target, error)
	preview    func(context.Context, authorization.Principal, json.RawMessage) (aiassist.Preview, error)
	execute    func(context.Context, authorization.Principal, json.RawMessage, string) (aiassist.ToolResult, error)
}

func (t *functionalTool) Name() string               { return t.name }
func (t *functionalTool) Version() int               { return 1 }
func (t *functionalTool) Kind() aiassist.ToolKind    { return t.kind }
func (t *functionalTool) RequiredCapability() string { return t.capability }
func (t *functionalTool) Prepare(ctx context.Context, principal authorization.Principal, input json.RawMessage) (json.RawMessage, error) {
	if t.prepare == nil {
		return input, nil
	}
	return t.prepare(ctx, principal, input)
}
func (t *functionalTool) Validate(input json.RawMessage) error { return t.validate(input) }
func (t *functionalTool) ResolveScope(_ context.Context, principal authorization.Principal, input json.RawMessage) (scope.Target, error) {
	return t.resolve(principal, input)
}
func (t *functionalTool) Preview(ctx context.Context, principal authorization.Principal, input json.RawMessage) (aiassist.Preview, error) {
	return t.preview(ctx, principal, input)
}
func (t *functionalTool) Execute(ctx context.Context, principal authorization.Principal, input json.RawMessage, correlationID string) (aiassist.ToolResult, error) {
	return t.execute(ctx, principal, input, correlationID)
}

type WorkRecordGetter interface {
	Get(context.Context, workrecords.GetCommand) (workrecords.Record, error)
}

type WorkRecordTransitioner interface {
	Transition(context.Context, workrecords.TransitionCommand) (workrecords.Record, error)
}

type WorkRecordPrioritizer interface {
	Change(context.Context, workrecords.PriorityCommand) (workrecords.Record, error)
}

type WorkRecordSearcher interface {
	Search(context.Context, search.Query) ([]search.Result, error)
}

type CommentCreator interface {
	Create(context.Context, comments.CreateCommand) (comments.Comment, error)
}

type ProjectWorkspaceCreator interface {
	CreateProject(
		context.Context,
		projects.AIWorkspaceCreateCommand,
	) (projects.AIWorkspaceCreateResult, error)
}

type scopedInput struct {
	ClientID string `json:"client_id"`
}

type ticketGetInput struct {
	ClientID string `json:"client_id"`
	ID       string `json:"id"`
}

type ticketSearchInput struct {
	ClientID string `json:"client_id"`
	Query    string `json:"query"`
	Limit    int    `json:"limit"`
}

type ticketTransitionInput struct {
	ClientID        string `json:"client_id"`
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
	Status          string `json:"status"`
	Reason          string `json:"reason"`
}

type ticketPriorityInput struct {
	ClientID        string `json:"client_id"`
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
	Priority        string `json:"priority"`
	Reason          string `json:"reason"`
}

type ticketCommentInput struct {
	ClientID        string `json:"client_id"`
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
	Body            string `json:"body"`
}

func NewTicketGetTool(actions WorkRecordGetter) aiassist.Tool {
	return &functionalTool{
		name: "ticket.get", capability: "work_record.read", kind: aiassist.ToolRead,
		validate: func(raw json.RawMessage) error {
			input, err := decode[ticketGetInput](raw)
			if err != nil || strings.TrimSpace(input.ClientID) == "" || strings.TrimSpace(input.ID) == "" {
				return aiassist.ErrInvalidTool
			}
			return nil
		},
		resolve: resolveScoped,
		preview: noPreview,
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, _ string) (aiassist.ToolResult, error) {
			input, err := decode[ticketGetInput](raw)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			record, err := actions.Get(ctx, workrecords.GetCommand{
				Principal: principal, Target: target(principal, input.ClientID), ID: input.ID,
			})
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{Summary: record.DisplayID + " " + record.Title, Data: recordData(record)}, nil
		},
	}
}

func NewTicketSearchTool(actions WorkRecordSearcher) aiassist.Tool {
	return &functionalTool{
		name: "ticket.search", capability: "search.read", kind: aiassist.ToolRead,
		validate: func(raw json.RawMessage) error {
			input, err := decode[ticketSearchInput](raw)
			if err != nil || len(strings.TrimSpace(input.Query)) < 2 || input.Limit < 1 || input.Limit > 20 {
				return aiassist.ErrInvalidTool
			}
			return nil
		},
		resolve: resolveScoped,
		preview: noPreview,
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, _ string) (aiassist.ToolResult, error) {
			input, _ := decode[ticketSearchInput](raw)
			results, err := actions.Search(ctx, search.Query{
				Principal: principal, Target: target(principal, input.ClientID),
				Text: input.Query, Limit: input.Limit,
			})
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			filtered := make([]search.Result, 0, len(results))
			for _, result := range results {
				if result.ObjectType == "work_record" || result.ObjectType == "incident" ||
					result.ObjectType == "request" || result.ObjectType == "change" ||
					result.ObjectType == "problem" {
					filtered = append(filtered, result)
				}
			}
			return aiassist.ToolResult{
				Summary: fmt.Sprintf("Found %d tickets", len(filtered)),
				Data:    map[string]any{"tickets": filtered},
			}, nil
		},
	}
}

func NewTicketTransitionTool(getter WorkRecordGetter, actions WorkRecordTransitioner) aiassist.Tool {
	return &functionalTool{
		name: "ticket.transition", capability: "work_record.transition", kind: aiassist.ToolWrite,
		validate: validateTicketTransition,
		resolve:  resolveScoped,
		preview: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (aiassist.Preview, error) {
			input, _ := decode[ticketTransitionInput](raw)
			record, err := getRecord(ctx, getter, principal, input.ClientID, input.ID, input.ExpectedVersion)
			if err != nil {
				return aiassist.Preview{}, err
			}
			return aiassist.Preview{
				Summary:    "Change " + record.DisplayID + " status to " + input.Status,
				TargetType: "work_record", TargetID: record.ID, TargetVersion: record.Version,
				Changes: map[string]aiassist.Change{"status": {Before: record.Status, After: input.Status}},
			}, nil
		},
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, correlationID string) (aiassist.ToolResult, error) {
			input, _ := decode[ticketTransitionInput](raw)
			record, err := actions.Transition(ctx, workrecords.TransitionCommand{
				Principal: principal, Target: target(principal, input.ClientID),
				WorkRecordID: input.ID, ExpectedVersion: input.ExpectedVersion,
				ToStatus: input.Status, Reason: input.Reason, CausationID: correlationID,
				Actor: workrecords.Actor{Type: "technician", ID: principal.ID, Source: "ai_workspace"},
			})
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{Summary: "Updated " + record.DisplayID, Data: recordData(record)}, nil
		},
	}
}

func NewTicketPriorityTool(getter WorkRecordGetter, actions WorkRecordPrioritizer) aiassist.Tool {
	return &functionalTool{
		name: "ticket.priority", capability: "work_record.edit", kind: aiassist.ToolWrite,
		validate: func(raw json.RawMessage) error {
			input, err := decode[ticketPriorityInput](raw)
			if err != nil || !validExistingTarget(input.ClientID, input.ID, input.ExpectedVersion) ||
				strings.TrimSpace(input.Priority) == "" || strings.TrimSpace(input.Reason) == "" {
				return aiassist.ErrInvalidTool
			}
			return nil
		},
		resolve: resolveScoped,
		preview: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (aiassist.Preview, error) {
			input, _ := decode[ticketPriorityInput](raw)
			record, err := getRecord(ctx, getter, principal, input.ClientID, input.ID, input.ExpectedVersion)
			if err != nil {
				return aiassist.Preview{}, err
			}
			return aiassist.Preview{
				Summary:    "Change " + record.DisplayID + " priority to " + input.Priority,
				TargetType: "work_record", TargetID: record.ID, TargetVersion: record.Version,
				Changes: map[string]aiassist.Change{"priority": {Before: record.Priority, After: input.Priority}},
			}, nil
		},
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, correlationID string) (aiassist.ToolResult, error) {
			input, _ := decode[ticketPriorityInput](raw)
			record, err := actions.Change(ctx, workrecords.PriorityCommand{
				Principal: principal, Target: target(principal, input.ClientID),
				WorkRecordID: input.ID, ExpectedVersion: input.ExpectedVersion,
				Priority: input.Priority, Reason: input.Reason, CausationID: correlationID,
				Actor: workrecords.Actor{Type: "technician", ID: principal.ID, Source: "ai_workspace"},
			})
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{Summary: "Updated " + record.DisplayID, Data: recordData(record)}, nil
		},
	}
}

func NewTicketNoteTool(getter WorkRecordGetter, actions CommentCreator) aiassist.Tool {
	return newTicketCommentTool("ticket.note", "comment.internal.create", comments.Internal, getter, actions)
}

func NewTicketReplyTool(getter WorkRecordGetter, actions CommentCreator) aiassist.Tool {
	return newTicketCommentTool("ticket.reply", "comment.public.create", comments.ClientVisible, getter, actions)
}

func newTicketCommentTool(
	name, capability string,
	visibility comments.Visibility,
	getter WorkRecordGetter,
	actions CommentCreator,
) aiassist.Tool {
	label := "internal note"
	if visibility == comments.ClientVisible {
		label = "client-visible reply"
	}
	return &functionalTool{
		name: name, capability: capability, kind: aiassist.ToolWrite,
		validate: func(raw json.RawMessage) error {
			input, err := decode[ticketCommentInput](raw)
			if err != nil || !validExistingTarget(input.ClientID, input.ID, input.ExpectedVersion) ||
				strings.TrimSpace(input.Body) == "" {
				return aiassist.ErrInvalidTool
			}
			return nil
		},
		resolve: resolveScoped,
		preview: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (aiassist.Preview, error) {
			input, _ := decode[ticketCommentInput](raw)
			record, err := getRecord(ctx, getter, principal, input.ClientID, input.ID, input.ExpectedVersion)
			if err != nil {
				return aiassist.Preview{}, err
			}
			return aiassist.Preview{
				Summary:    "Add " + label + " to " + record.DisplayID,
				TargetType: "work_record", TargetID: record.ID, TargetVersion: record.Version,
				Changes: map[string]aiassist.Change{label: {Before: nil, After: input.Body}},
			}, nil
		},
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, correlationID string) (aiassist.ToolResult, error) {
			input, _ := decode[ticketCommentInput](raw)
			comment, err := actions.Create(ctx, comments.CreateCommand{
				Principal: principal, Target: target(principal, input.ClientID),
				WorkRecordID: input.ID, Visibility: visibility, Body: input.Body,
				ActorID: principal.ID, ActorType: "technician", Source: "ai_workspace",
				CausationID: correlationID,
			})
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{
				Summary: "Added " + label,
				Data:    map[string]any{"comment_id": comment.ID, "visibility": comment.Visibility},
			}, nil
		},
	}
}

func validateTicketTransition(raw json.RawMessage) error {
	input, err := decode[ticketTransitionInput](raw)
	if err != nil || !validExistingTarget(input.ClientID, input.ID, input.ExpectedVersion) ||
		strings.TrimSpace(input.Status) == "" || strings.TrimSpace(input.Reason) == "" {
		return aiassist.ErrInvalidTool
	}
	return nil
}

func validExistingTarget(clientID, id string, version int64) bool {
	return strings.TrimSpace(clientID) != "" && strings.TrimSpace(id) != "" && version > 0
}

func validWorkRecordType(recordType workrecords.Type) bool {
	return recordType == workrecords.Incident || recordType == workrecords.Request ||
		recordType == workrecords.Change || recordType == workrecords.Problem
}

func getRecord(
	ctx context.Context,
	getter WorkRecordGetter,
	principal authorization.Principal,
	clientID, id string,
	expectedVersion int64,
) (workrecords.Record, error) {
	record, err := getter.Get(ctx, workrecords.GetCommand{
		Principal: principal, Target: target(principal, clientID), ID: id,
	})
	if err != nil {
		return workrecords.Record{}, err
	}
	if err := object.RequireVersion(record.Version, expectedVersion); err != nil {
		return workrecords.Record{}, err
	}
	return record, nil
}

func recordData(record workrecords.Record) map[string]any {
	return map[string]any{
		"id": record.ID, "display_id": record.DisplayID, "type": record.Type,
		"title": record.Title, "status": record.Status, "priority": record.Priority,
		"queue_id": record.QueueID, "owner_id": record.PrimaryOwnerID,
		"version": record.Version, "updated_at": record.UpdatedAt,
	}
}

func target(principal authorization.Principal, clientID string) scope.Target {
	return scope.Target{MSPID: principal.Scope.MSPID, ClientID: strings.TrimSpace(clientID)}
}

func resolveScoped(principal authorization.Principal, raw json.RawMessage) (scope.Target, error) {
	var input scopedInput
	if err := json.Unmarshal(raw, &input); err != nil || strings.TrimSpace(input.ClientID) == "" {
		return scope.Target{}, aiassist.ErrInvalidTool
	}
	return target(principal, input.ClientID), nil
}

func noPreview(context.Context, authorization.Principal, json.RawMessage) (aiassist.Preview, error) {
	return aiassist.Preview{}, nil
}

func validateAs[T any](raw json.RawMessage) error {
	_, err := decode[T](raw)
	return err
}

func decode[T any](raw json.RawMessage) (T, error) {
	var value T
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, aiassist.ErrInvalidTool
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return value, aiassist.ErrInvalidTool
	}
	return value, nil
}

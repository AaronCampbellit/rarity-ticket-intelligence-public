package aiassist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrInvalidTool       = errors.New("invalid AI tool")
	ErrUnknownTool       = errors.New("unknown AI tool")
	ErrProposalConflict  = errors.New("AI action proposal conflict")
	ErrProposalExpired   = errors.New("AI action proposal expired")
	ErrProposalStale     = errors.New("AI action proposal target changed")
	ErrProposalForbidden = errors.New("AI action proposal forbidden")
)

type ToolKind string

const (
	ToolRead  ToolKind = "read"
	ToolWrite ToolKind = "write"
)

type Change struct {
	Before any `json:"before"`
	After  any `json:"after"`
}

type PreviewTarget struct {
	ID        string `json:"id"`
	ClientID  string `json:"client_id"`
	Kind      string `json:"kind"`
	DisplayID string `json:"display_id"`
	Name      string `json:"name"`
}

type Preview struct {
	Summary        string            `json:"summary"`
	TargetType     string            `json:"target_type"`
	TargetID       string            `json:"target_id"`
	TargetVersion  int64             `json:"target_version,omitempty"`
	LocationTarget *PreviewTarget    `json:"location_target,omitempty"`
	Changes        map[string]Change `json:"changes,omitempty"`
}

type ToolResult struct {
	Summary string         `json:"summary"`
	Data    map[string]any `json:"data,omitempty"`
}

type ToolRequest struct {
	Name           string
	ConversationID string
	MessageID      string
	Input          json.RawMessage
}

type Tool interface {
	Name() string
	Version() int
	Kind() ToolKind
	Validate(json.RawMessage) error
	ResolveScope(context.Context, authorization.Principal, json.RawMessage) (scope.Target, error)
	RequiredCapability() string
	Preview(context.Context, authorization.Principal, json.RawMessage) (Preview, error)
	Execute(context.Context, authorization.Principal, json.RawMessage, string) (ToolResult, error)
}

type ToolInputPreparer interface {
	Prepare(
		context.Context,
		authorization.Principal,
		json.RawMessage,
	) (json.RawMessage, error)
}

type ProposalState string

const (
	ProposalPending   ProposalState = "pending"
	ProposalConfirmed ProposalState = "confirmed"
	ProposalRejected  ProposalState = "rejected"
	ProposalExpired   ProposalState = "expired"
	ProposalFailed    ProposalState = "failed"
)

type ActionProposal struct {
	ID                 string          `json:"id"`
	MSPID              string          `json:"-"`
	ClientID           string          `json:"client_id,omitempty"`
	TargetClientID     string          `json:"target_client_id,omitempty"`
	ConversationID     string          `json:"conversation_id,omitempty"`
	MessageID          string          `json:"message_id,omitempty"`
	PrincipalID        string          `json:"-"`
	ToolName           string          `json:"tool_name"`
	ToolVersion        int             `json:"tool_version"`
	NormalizedInput    json.RawMessage `json:"-"`
	Preview            Preview         `json:"preview"`
	RequiredCapability string          `json:"required_capability"`
	ExpiresAt          time.Time       `json:"expires_at"`
	State              ProposalState   `json:"state"`
	ConfirmedAt        *time.Time      `json:"confirmed_at,omitempty"`
	RejectedAt         *time.Time      `json:"rejected_at,omitempty"`
	FailedAt           *time.Time      `json:"failed_at,omitempty"`
	Result             *ToolResult     `json:"result,omitempty"`
	ErrorCode          string          `json:"error_code,omitempty"`
	CorrelationID      string          `json:"-"`
	Version            int64           `json:"version"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type ProposalStore interface {
	CreateProposal(context.Context, ActionProposal) error
	GetProposal(context.Context, scope.Target, string, string) (ActionProposal, error)
	ConfirmProposal(context.Context, scope.Target, string, string, int64, time.Time) (ActionProposal, error)
	CompleteProposal(context.Context, scope.Target, string, int64, ToolResult, time.Time) error
	FailProposal(context.Context, scope.Target, string, int64, string, time.Time) error
	RejectProposal(context.Context, scope.Target, string, string, int64, time.Time) error
	ExpireProposal(context.Context, scope.Target, string, string, int64, time.Time) error
}

type PrincipalLoader func(context.Context, authorization.Principal) (authorization.Principal, error)

type ExecutionTargetAuthorizer interface {
	AuthorizeExecutionTarget(
		context.Context,
		authorization.Principal,
		scope.Target,
	) error
}

type Registry struct {
	tools                    map[string]Tool
	store                    ProposalStore
	loadPrincipal            PrincipalLoader
	authorizeExecutionTarget ExecutionTargetAuthorizer
	now                      func() time.Time
	newID                    func() string
	proposalLifetime         time.Duration
}

func NewRegistry(
	tools []Tool,
	store ProposalStore,
	loadPrincipal PrincipalLoader,
	now func() time.Time,
	newID func() string,
	authorizeExecutionTarget ExecutionTargetAuthorizer,
) (*Registry, error) {
	if store == nil || loadPrincipal == nil || now == nil || newID == nil ||
		authorizeExecutionTarget == nil {
		return nil, ErrInvalidTool
	}
	registry := &Registry{
		tools: make(map[string]Tool, len(tools)), store: store,
		loadPrincipal:            loadPrincipal,
		authorizeExecutionTarget: authorizeExecutionTarget,
		now:                      now, newID: newID,
		proposalLifetime: 10 * time.Minute,
	}
	for _, tool := range tools {
		if tool == nil || strings.TrimSpace(tool.Name()) == "" || tool.Version() <= 0 ||
			(tool.Kind() != ToolRead && tool.Kind() != ToolWrite) ||
			strings.TrimSpace(tool.RequiredCapability()) == "" {
			return nil, ErrInvalidTool
		}
		if _, exists := registry.tools[tool.Name()]; exists {
			return nil, fmt.Errorf("%w: duplicate %s", ErrInvalidTool, tool.Name())
		}
		registry.tools[tool.Name()] = tool
	}
	return registry, nil
}

func (r *Registry) RunRead(
	ctx context.Context,
	principal authorization.Principal,
	request ToolRequest,
) (ToolResult, error) {
	tool, input, _, err := r.authorizeRequest(ctx, principal, request)
	if err != nil {
		return ToolResult{}, err
	}
	if tool.Kind() != ToolRead {
		return ToolResult{}, ErrInvalidTool
	}
	return tool.Execute(ctx, principal, input, r.newID())
}

func (r *Registry) Propose(
	ctx context.Context,
	principal authorization.Principal,
	request ToolRequest,
) (ActionProposal, error) {
	tool, input, resolvedTarget, err := r.authorizeRequest(ctx, principal, request)
	if err != nil {
		return ActionProposal{}, err
	}
	if tool.Kind() != ToolWrite {
		return ActionProposal{}, ErrInvalidTool
	}
	if strings.TrimSpace(request.ConversationID) == "" {
		return ActionProposal{}, ErrInvalidTool
	}
	preview, err := tool.Preview(ctx, principal, input)
	if err != nil {
		return ActionProposal{}, err
	}
	if !validPreview(preview) {
		return ActionProposal{}, ErrInvalidTool
	}
	now := r.now().UTC()
	accessTarget := scope.Target{
		MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
	}
	executionTarget := resolvedTarget
	proposal := ActionProposal{
		ID: r.newID(), MSPID: accessTarget.MSPID, ClientID: accessTarget.ClientID,
		TargetClientID: executionTarget.ClientID,
		ConversationID: request.ConversationID, MessageID: request.MessageID,
		PrincipalID: principal.ID, ToolName: tool.Name(), ToolVersion: tool.Version(),
		NormalizedInput: input, Preview: preview,
		RequiredCapability: tool.RequiredCapability(),
		ExpiresAt:          now.Add(r.proposalLifetime), State: ProposalPending,
		CorrelationID: r.newID(), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if strings.TrimSpace(proposal.ID) == "" || strings.TrimSpace(proposal.CorrelationID) == "" {
		return ActionProposal{}, ErrInvalidTool
	}
	if err := r.store.CreateProposal(ctx, proposal); err != nil {
		return ActionProposal{}, err
	}
	return proposal, nil
}

func (r *Registry) Confirm(
	ctx context.Context,
	caller authorization.Principal,
	id string,
	expectedVersion int64,
) (ToolResult, error) {
	if strings.TrimSpace(caller.ID) == "" || strings.TrimSpace(caller.Scope.MSPID) == "" ||
		strings.TrimSpace(id) == "" || expectedVersion <= 0 {
		return ToolResult{}, ErrProposalForbidden
	}
	accessTarget := scope.Target{MSPID: caller.Scope.MSPID, ClientID: caller.Scope.ClientID}
	proposal, err := r.store.GetProposal(ctx, accessTarget, id, caller.ID)
	if err != nil {
		return ToolResult{}, err
	}
	if proposal.Version != expectedVersion || proposal.State != ProposalPending {
		return ToolResult{}, ErrProposalConflict
	}
	now := r.now().UTC()
	if !proposal.ExpiresAt.After(now) {
		_ = r.store.ExpireProposal(ctx, accessTarget, id, caller.ID, expectedVersion, now)
		return ToolResult{}, ErrProposalExpired
	}
	tool, exists := r.tools[proposal.ToolName]
	if !exists || tool.Kind() != ToolWrite || tool.Version() != proposal.ToolVersion ||
		tool.RequiredCapability() != proposal.RequiredCapability {
		return ToolResult{}, ErrInvalidTool
	}
	principal, err := r.loadPrincipal(ctx, caller)
	if err != nil {
		return ToolResult{}, err
	}
	if principal.ID != caller.ID || principal.Scope != caller.Scope {
		return ToolResult{}, ErrProposalForbidden
	}
	resolvedTarget, err := tool.ResolveScope(ctx, principal, proposal.NormalizedInput)
	if err != nil {
		return ToolResult{}, err
	}
	executionTarget := scope.Target{
		MSPID: proposal.MSPID, ClientID: proposal.TargetClientID,
	}
	if resolvedTarget != executionTarget {
		return ToolResult{}, ErrProposalStale
	}
	if err := authorization.Authorize(
		principal, proposal.RequiredCapability, executionTarget,
	); err != nil {
		return ToolResult{}, err
	}
	if err := r.authorizeExecutionTarget.AuthorizeExecutionTarget(
		ctx, principal, executionTarget,
	); err != nil {
		return ToolResult{}, err
	}
	currentPreview, err := tool.Preview(ctx, principal, proposal.NormalizedInput)
	if err != nil {
		return ToolResult{}, err
	}
	if !equalJSONPreview(currentPreview, proposal.Preview) {
		return ToolResult{}, ErrProposalStale
	}
	confirmed, err := r.store.ConfirmProposal(
		ctx, accessTarget, id, caller.ID, expectedVersion, now,
	)
	if err != nil {
		return ToolResult{}, err
	}
	result, err := tool.Execute(ctx, principal, confirmed.NormalizedInput, confirmed.CorrelationID)
	if err != nil {
		_ = r.store.FailProposal(
			ctx, accessTarget, id, confirmed.Version, "tool_execution_failed", r.now().UTC(),
		)
		return ToolResult{}, err
	}
	if err := r.store.CompleteProposal(
		ctx, accessTarget, id, confirmed.Version, result, r.now().UTC(),
	); err != nil {
		return ToolResult{}, err
	}
	return result, nil
}

func (r *Registry) Reject(
	ctx context.Context,
	caller authorization.Principal,
	id string,
	expectedVersion int64,
) error {
	if strings.TrimSpace(caller.ID) == "" || strings.TrimSpace(caller.Scope.MSPID) == "" ||
		strings.TrimSpace(id) == "" || expectedVersion <= 0 {
		return ErrProposalForbidden
	}
	target := scope.Target{MSPID: caller.Scope.MSPID, ClientID: caller.Scope.ClientID}
	return r.store.RejectProposal(ctx, target, id, caller.ID, expectedVersion, r.now().UTC())
}

func (r *Registry) authorizeRequest(
	ctx context.Context,
	principal authorization.Principal,
	request ToolRequest,
) (Tool, json.RawMessage, scope.Target, error) {
	if r == nil || strings.TrimSpace(principal.ID) == "" {
		return nil, nil, scope.Target{}, ErrInvalidTool
	}
	tool, exists := r.tools[request.Name]
	if !exists {
		return nil, nil, scope.Target{}, ErrUnknownTool
	}
	input, err := normalizeJSON(request.Input)
	if err != nil {
		return nil, nil, scope.Target{}, ErrInvalidTool
	}
	if preparer, ok := tool.(ToolInputPreparer); ok {
		input, err = preparer.Prepare(ctx, principal, input)
		if err != nil {
			return nil, nil, scope.Target{}, err
		}
		input, err = normalizeJSON(input)
	}
	if err != nil || tool.Validate(input) != nil {
		return nil, nil, scope.Target{}, ErrInvalidTool
	}
	target, err := tool.ResolveScope(ctx, principal, input)
	if err != nil {
		return nil, nil, scope.Target{}, err
	}
	if err := authorization.Authorize(principal, tool.RequiredCapability(), target); err != nil {
		return nil, nil, scope.Target{}, err
	}
	if err := r.authorizeExecutionTarget.AuthorizeExecutionTarget(
		ctx, principal, target,
	); err != nil {
		return nil, nil, scope.Target{}, err
	}
	return tool, input, target, nil
}

func normalizeJSON(input json.RawMessage) (json.RawMessage, error) {
	var compact bytes.Buffer
	if len(input) == 0 || json.Compact(&compact, input) != nil {
		return nil, ErrInvalidTool
	}
	var value any
	if err := json.Unmarshal(compact.Bytes(), &value); err != nil {
		return nil, ErrInvalidTool
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, ErrInvalidTool
	}
	return append(json.RawMessage(nil), compact.Bytes()...), nil
}

func validPreview(preview Preview) bool {
	return strings.TrimSpace(preview.Summary) != "" &&
		strings.TrimSpace(preview.TargetType) != "" &&
		strings.TrimSpace(preview.TargetID) != "" &&
		preview.TargetVersion >= 0
}

func equalJSONPreview(current Preview, prepared Preview) bool {
	currentJSON, err := json.Marshal(current)
	if err != nil {
		return false
	}
	preparedJSON, err := json.Marshal(prepared)
	if err != nil {
		return false
	}
	return bytes.Equal(currentJSON, preparedJSON)
}

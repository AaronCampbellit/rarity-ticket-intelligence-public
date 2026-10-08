package rtitools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type OpportunityOperationalQueries interface {
	GetOpportunityInTarget(
		context.Context,
		authorization.Principal,
		scope.Target,
		sales.OpportunityID,
	) (sales.Opportunity, error)
	ListOpportunitiesInTarget(
		context.Context,
		authorization.Principal,
		scope.Target,
		sales.OpportunityListFilter,
	) ([]sales.Opportunity, error)
	ResolveOpportunityReference(
		context.Context,
		authorization.Principal,
		scope.Target,
		string,
		int,
	) ([]sales.Opportunity, error)
	ResolveStageReference(
		context.Context,
		authorization.Principal,
		scope.Target,
		sales.OpportunityID,
		string,
		int,
	) ([]sales.PipelineStage, error)
}

type ProposalOperationalQueries interface {
	GetProposal(
		context.Context,
		authorization.Principal,
		scope.Target,
		string,
	) (sales.Proposal, error)
	ListProposals(
		context.Context,
		authorization.Principal,
		scope.Target,
		sales.ProposalListFilter,
	) ([]sales.Proposal, error)
	ResolveProposalReference(
		context.Context,
		authorization.Principal,
		scope.Target,
		string,
		int,
	) ([]sales.Proposal, error)
}

type opportunityListRequest struct {
	Client     string  `json:"client"`
	PipelineID *string `json:"pipeline_id,omitempty"`
	StageID    *string `json:"stage_id,omitempty"`
	Limit      *int    `json:"limit,omitempty"`
}

type opportunityListInput struct {
	ClientID   string                `json:"client_id"`
	PipelineID string                `json:"pipeline_id,omitempty"`
	StageID    sales.PipelineStageID `json:"stage_id,omitempty"`
	Limit      int                   `json:"limit"`
}

type opportunityGetRequest struct {
	Client      string `json:"client"`
	Opportunity string `json:"opportunity"`
}

type proposalListRequest struct {
	Client        string               `json:"client"`
	State         *sales.ProposalState `json:"state,omitempty"`
	OpportunityID *string              `json:"opportunity_id,omitempty"`
	Limit         *int                 `json:"limit,omitempty"`
}

type proposalListInput struct {
	ClientID      string              `json:"client_id"`
	State         sales.ProposalState `json:"state,omitempty"`
	OpportunityID string              `json:"opportunity_id,omitempty"`
	Limit         int                 `json:"limit"`
}

type proposalGetRequest struct {
	Client   string `json:"client"`
	Proposal string `json:"proposal"`
}

func NewOpportunityListTool(
	directory ActiveClientResolver,
	queries OpportunityOperationalQueries,
) aiassist.Tool {
	return &functionalTool{
		name: "opportunity.list", capability: "opportunity.read", kind: aiassist.ToolRead,
		prepare: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
		) (json.RawMessage, error) {
			request, err := decode[opportunityListRequest](raw)
			if err != nil || queries == nil {
				return nil, aiassist.ErrInvalidTool
			}
			limit, ok := operationalReadLimit(request.Limit)
			pipelineID, pipelineOK := optionalSalesFilter(request.PipelineID)
			stageID, stageOK := optionalSalesFilter(request.StageID)
			if !ok || !pipelineOK || !stageOK {
				return nil, aiassist.ErrInvalidTool
			}
			client, err := resolveActiveResourceClient(
				ctx, directory, principal, "opportunity.read", request.Client,
			)
			if err != nil {
				return nil, aiassist.ErrInvalidTool
			}
			return marshalPrepared(opportunityListInput{
				ClientID: client.ID, PipelineID: pipelineID,
				StageID: sales.PipelineStageID(stageID), Limit: limit,
			})
		},
		validate: validateOpportunityList,
		resolve:  resolveClientResourceScope,
		preview:  noPreview,
		execute: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
			_ string,
		) (aiassist.ToolResult, error) {
			input, err := validOpportunityList(raw)
			if err != nil || queries == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			target := target(principal, input.ClientID)
			opportunities, err := queries.ListOpportunitiesInTarget(
				ctx, principal, target, sales.OpportunityListFilter{
					PipelineID: input.PipelineID, StageID: input.StageID,
					Limit: input.Limit,
				},
			)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			result := make([]map[string]any, 0, len(opportunities))
			for _, opportunity := range opportunities {
				safe, err := safeOpportunityResult(
					ctx, queries, principal, target, opportunity,
				)
				if err != nil {
					return aiassist.ToolResult{}, err
				}
				result = append(result, safe)
			}
			return aiassist.ToolResult{
				Summary: "Listed opportunities",
				Data:    map[string]any{"opportunities": result},
			}, nil
		},
	}
}

func NewOpportunityGetTool(
	directory ActiveClientResolver,
	queries OpportunityOperationalQueries,
) aiassist.Tool {
	return &functionalTool{
		name: "opportunity.get", capability: "opportunity.read", kind: aiassist.ToolRead,
		prepare: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
		) (json.RawMessage, error) {
			request, err := decode[opportunityGetRequest](raw)
			if err != nil || queries == nil ||
				strings.TrimSpace(request.Opportunity) == "" {
				return nil, aiassist.ErrInvalidTool
			}
			client, err := resolveActiveResourceClient(
				ctx, directory, principal, "opportunity.read", request.Client,
			)
			if err != nil {
				return nil, aiassist.ErrInvalidTool
			}
			return marshalPrepared(operationalGetInput{
				ClientID: client.ID, Reference: strings.TrimSpace(request.Opportunity),
			})
		},
		validate: validateOperationalGet,
		resolve:  resolveClientResourceScope,
		preview:  noPreview,
		execute: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
			_ string,
		) (aiassist.ToolResult, error) {
			input, err := validOperationalGet(raw)
			if err != nil || queries == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			target := target(principal, input.ClientID)
			matches, err := queries.ResolveOpportunityReference(
				ctx, principal, target, input.Reference, 2,
			)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			opportunityID, err := uniqueOpportunityID(matches)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			opportunity, err := queries.GetOpportunityInTarget(
				ctx, principal, target, opportunityID,
			)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			safe, err := safeOpportunityResult(
				ctx, queries, principal, target, opportunity,
			)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{
				Summary: "Found opportunity " + opportunity.DisplayID,
				Data:    map[string]any{"opportunity": safe},
			}, nil
		},
	}
}

func NewProposalListTool(
	directory ActiveClientResolver,
	queries ProposalOperationalQueries,
) aiassist.Tool {
	return &functionalTool{
		name: "proposal.list", capability: "proposal.read", kind: aiassist.ToolRead,
		prepare: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
		) (json.RawMessage, error) {
			request, err := decode[proposalListRequest](raw)
			if err != nil || queries == nil {
				return nil, aiassist.ErrInvalidTool
			}
			limit, ok := operationalReadLimit(request.Limit)
			opportunityID, opportunityOK := optionalSalesFilter(request.OpportunityID)
			if !ok || !opportunityOK ||
				(request.State != nil && !validProposalReadState(*request.State)) {
				return nil, aiassist.ErrInvalidTool
			}
			client, err := resolveActiveResourceClient(
				ctx, directory, principal, "proposal.read", request.Client,
			)
			if err != nil {
				return nil, aiassist.ErrInvalidTool
			}
			var state sales.ProposalState
			if request.State != nil {
				state = *request.State
			}
			return marshalPrepared(proposalListInput{
				ClientID: client.ID, State: state,
				OpportunityID: opportunityID, Limit: limit,
			})
		},
		validate: validateProposalList,
		resolve:  resolveClientResourceScope,
		preview:  noPreview,
		execute: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
			_ string,
		) (aiassist.ToolResult, error) {
			input, err := validProposalList(raw)
			if err != nil || queries == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			proposals, err := queries.ListProposals(
				ctx, principal, target(principal, input.ClientID),
				sales.ProposalListFilter{
					State: input.State, OpportunityID: input.OpportunityID,
					Limit: input.Limit,
				},
			)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			result := make([]map[string]any, 0, len(proposals))
			for _, proposal := range proposals {
				result = append(result, safeProposalResult(proposal))
			}
			return aiassist.ToolResult{
				Summary: "Listed proposals",
				Data:    map[string]any{"proposals": result},
			}, nil
		},
	}
}

func NewProposalGetTool(
	directory ActiveClientResolver,
	queries ProposalOperationalQueries,
) aiassist.Tool {
	return &functionalTool{
		name: "proposal.get", capability: "proposal.read", kind: aiassist.ToolRead,
		prepare: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
		) (json.RawMessage, error) {
			request, err := decode[proposalGetRequest](raw)
			if err != nil || queries == nil ||
				strings.TrimSpace(request.Proposal) == "" {
				return nil, aiassist.ErrInvalidTool
			}
			client, err := resolveActiveResourceClient(
				ctx, directory, principal, "proposal.read", request.Client,
			)
			if err != nil {
				return nil, aiassist.ErrInvalidTool
			}
			return marshalPrepared(operationalGetInput{
				ClientID: client.ID, Reference: strings.TrimSpace(request.Proposal),
			})
		},
		validate: validateOperationalGet,
		resolve:  resolveClientResourceScope,
		preview:  noPreview,
		execute: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
			_ string,
		) (aiassist.ToolResult, error) {
			input, err := validOperationalGet(raw)
			if err != nil || queries == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			target := target(principal, input.ClientID)
			matches, err := queries.ResolveProposalReference(
				ctx, principal, target, input.Reference, 2,
			)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			proposalID, err := uniqueProposalID(matches)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			proposal, err := queries.GetProposal(
				ctx, principal, target, proposalID,
			)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{
				Summary: "Found proposal " + proposal.DisplayID,
				Data:    map[string]any{"proposal": safeProposalResult(proposal)},
			}, nil
		},
	}
}

func optionalSalesFilter(value *string) (string, bool) {
	if value == nil {
		return "", true
	}
	trimmed := strings.TrimSpace(*value)
	return trimmed, trimmed != ""
}

func validateOpportunityList(raw json.RawMessage) error {
	_, err := validOpportunityList(raw)
	return err
}

func validOpportunityList(
	raw json.RawMessage,
) (opportunityListInput, error) {
	input, err := decode[opportunityListInput](raw)
	if err != nil || strings.TrimSpace(input.ClientID) == "" ||
		input.Limit < 1 || input.Limit > 50 {
		return opportunityListInput{}, aiassist.ErrInvalidTool
	}
	input.PipelineID = strings.TrimSpace(input.PipelineID)
	input.StageID = sales.PipelineStageID(strings.TrimSpace(string(input.StageID)))
	return input, nil
}

func validateProposalList(raw json.RawMessage) error {
	_, err := validProposalList(raw)
	return err
}

func validProposalList(raw json.RawMessage) (proposalListInput, error) {
	input, err := decode[proposalListInput](raw)
	if err != nil || strings.TrimSpace(input.ClientID) == "" ||
		input.Limit < 1 || input.Limit > 50 ||
		(input.State != "" && !validProposalReadState(input.State)) {
		return proposalListInput{}, aiassist.ErrInvalidTool
	}
	input.OpportunityID = strings.TrimSpace(input.OpportunityID)
	return input, nil
}

func validProposalReadState(state sales.ProposalState) bool {
	return state == sales.ProposalDraft ||
		state == sales.ProposalIssued ||
		state == sales.ProposalAccepted
}

func uniqueOpportunityID(
	matches []sales.Opportunity,
) (sales.OpportunityID, error) {
	if len(matches) > 1 {
		return "", sales.ErrAmbiguousReference
	}
	if len(matches) == 0 || matches[0].ID == "" {
		return "", scope.ErrNotFound
	}
	return matches[0].ID, nil
}

func uniqueProposalID(matches []sales.Proposal) (string, error) {
	if len(matches) > 1 {
		return "", sales.ErrAmbiguousReference
	}
	if len(matches) == 0 || strings.TrimSpace(matches[0].ID) == "" {
		return "", scope.ErrNotFound
	}
	return matches[0].ID, nil
}

func safeOpportunityResult(
	ctx context.Context,
	queries OpportunityOperationalQueries,
	principal authorization.Principal,
	target scope.Target,
	opportunity sales.Opportunity,
) (map[string]any, error) {
	if opportunity.ID == "" || opportunity.StageID == "" {
		return nil, scope.ErrNotFound
	}
	stages, err := queries.ResolveStageReference(
		ctx, principal, target, opportunity.ID, string(opportunity.StageID), 2,
	)
	if err != nil {
		return nil, err
	}
	if len(stages) > 1 {
		return nil, sales.ErrAmbiguousReference
	}
	if len(stages) == 0 || strings.TrimSpace(stages[0].Name) == "" {
		return nil, scope.ErrNotFound
	}
	return map[string]any{
		"display_id": opportunity.DisplayID,
		"name":       opportunity.Name,
		"stage":      stages[0].Name,
		"version":    opportunity.Version,
	}, nil
}

func safeProposalResult(proposal sales.Proposal) map[string]any {
	return map[string]any{
		"display_id":     proposal.DisplayID,
		"state":          proposal.State,
		"opportunity_id": proposal.OpportunityID,
		"version":        proposal.Version,
	}
}

package rtitools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

// ProposalOperationalActions composes the explicit Opportunity reference
// boundary with the ordinary Proposal service. The AI adapter can only create
// an empty draft; proposal composition and high-impact lifecycle actions stay
// outside this boundary.
type ProposalOperationalActions interface {
	ResolveOpportunityReference(
		context.Context,
		authorization.Principal,
		scope.Target,
		string,
		int,
	) ([]sales.Opportunity, error)
	GetOpportunityInTarget(
		context.Context,
		authorization.Principal,
		scope.Target,
		sales.OpportunityID,
	) (sales.Opportunity, error)
	PreflightCreateProposal(
		context.Context,
		sales.CreateProposalCommand,
	) (sales.ProposalDraftPreflight, error)
	CreateProposal(context.Context, sales.CreateProposalCommand) (sales.Proposal, error)
}

// proposalCreateRequest is the complete public contract. Strict decoding
// ensures callers cannot smuggle line items, pricing, approval, issuance,
// signer, acceptance, conversion, or internal identity fields through it.
type proposalCreateRequest struct {
	Client      string `json:"client"`
	Opportunity string `json:"opportunity"`
	DisplayID   string `json:"display_id"`
}

type proposalCreatePrepared struct {
	MSPID              string              `json:"msp_id"`
	ClientID           string              `json:"client_id"`
	ClientReference    string              `json:"client_reference"`
	ClientDisplayID    string              `json:"client_display_id"`
	ClientName         string              `json:"client_name"`
	ClientVersion      int64               `json:"client_version"`
	OpportunityID      sales.OpportunityID `json:"opportunity_id"`
	OpportunityRef     string              `json:"opportunity_reference"`
	OpportunityDisplay string              `json:"opportunity_display_id"`
	OpportunityName    string              `json:"opportunity_name"`
	OpportunityVersion int64               `json:"opportunity_version"`
	DisplayID          string              `json:"display_id"`
}

func NewProposalCreateTool(
	directory ActiveClientResolver,
	actions ProposalOperationalActions,
) aiassist.Tool {
	const capability = "proposal.create"
	return &functionalTool{
		name: "proposal.create", capability: capability, kind: aiassist.ToolWrite,
		prepare: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (json.RawMessage, error) {
			request, err := decode[proposalCreateRequest](raw)
			if err != nil || directory == nil || actions == nil {
				return nil, aiassist.ErrInvalidTool
			}
			request.Client = strings.TrimSpace(request.Client)
			request.Opportunity = strings.TrimSpace(request.Opportunity)
			request.DisplayID = strings.TrimSpace(request.DisplayID)
			if request.Client == "" || request.Opportunity == "" || request.DisplayID == "" {
				return nil, aiassist.ErrInvalidTool
			}
			client, err := resolveActiveResourceClient(ctx, directory, principal, capability, request.Client)
			if err != nil {
				return nil, err
			}
			input, err := prepareProposalDraft(ctx, actions, principal, client, request)
			if err != nil {
				return nil, err
			}
			return marshalPrepared(input)
		},
		validate: validateProposalCreatePrepared,
		resolve:  resolveClientResourceScope,
		preview: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (aiassist.Preview, error) {
			input, err := validProposalCreatePrepared(raw)
			if err != nil {
				return aiassist.Preview{}, err
			}
			if err := recheckProposalDraft(ctx, directory, actions, principal, capability, input); err != nil {
				return aiassist.Preview{}, err
			}
			return proposalCreatePreview(input), nil
		},
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, _ string) (aiassist.ToolResult, error) {
			input, err := validProposalCreatePrepared(raw)
			if err != nil || actions == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			if err := recheckProposalDraft(ctx, directory, actions, principal, capability, input); err != nil {
				return aiassist.ToolResult{}, err
			}
			proposal, err := actions.CreateProposal(ctx, proposalCreateCommand(principal, input))
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{
				Summary: "Created draft proposal " + proposal.DisplayID,
				Data: map[string]any{
					"id": proposal.ID, "display_id": proposal.DisplayID,
					"opportunity_id": proposal.OpportunityID, "state": proposal.State,
					"version": proposal.Version,
				},
			}, nil
		},
	}
}

func prepareProposalDraft(
	ctx context.Context,
	actions ProposalOperationalActions,
	principal authorization.Principal,
	client organizations.Client,
	request proposalCreateRequest,
) (proposalCreatePrepared, error) {
	if client.MSPID != principal.Scope.MSPID || client.ID == "" || strings.TrimSpace(client.DisplayID) == "" || strings.TrimSpace(client.Name) == "" || client.Version < 1 {
		return proposalCreatePrepared{}, aiassist.ErrInvalidTool
	}
	target := target(principal, client.ID)
	matches, err := actions.ResolveOpportunityReference(ctx, principal, target, request.Opportunity, 2)
	if err != nil {
		return proposalCreatePrepared{}, err
	}
	opportunityID, err := uniqueOpportunityID(matches)
	if err != nil {
		return proposalCreatePrepared{}, err
	}
	opportunity, err := actions.GetOpportunityInTarget(ctx, principal, target, opportunityID)
	if err != nil || !validProposalOpportunity(opportunity, principal.Scope.MSPID, client.ID, opportunityID) {
		if err != nil {
			return proposalCreatePrepared{}, err
		}
		return proposalCreatePrepared{}, scope.ErrNotFound
	}
	preflight, err := actions.PreflightCreateProposal(ctx, sales.CreateProposalCommand{
		Principal: principal, Target: target, OpportunityID: string(opportunityID),
		DisplayID: request.DisplayID, ActorID: principal.ID, Source: "ai_workspace",
	})
	if err != nil {
		return proposalCreatePrepared{}, err
	}
	input := proposalCreatePrepared{
		MSPID: client.MSPID, ClientID: client.ID, ClientReference: request.Client, ClientDisplayID: client.DisplayID,
		ClientName: strings.TrimSpace(client.Name), ClientVersion: client.Version,
		OpportunityID: opportunity.ID, OpportunityRef: request.Opportunity,
		OpportunityDisplay: opportunity.DisplayID, OpportunityName: opportunity.Name,
		OpportunityVersion: opportunity.Version, DisplayID: request.DisplayID,
	}
	if !matchesProposalDraftPreflight(input, preflight) {
		return proposalCreatePrepared{}, aiassist.ErrProposalStale
	}
	return input, nil
}

func recheckProposalDraft(
	ctx context.Context,
	directory ActiveClientResolver,
	actions ProposalOperationalActions,
	principal authorization.Principal,
	capability string,
	input proposalCreatePrepared,
) error {
	if directory == nil || actions == nil {
		return aiassist.ErrInvalidTool
	}
	client, err := resolveActiveResourceClient(ctx, directory, principal, capability, input.ClientReference)
	if err != nil || !sameProposalDraftClient(input, client) {
		return aiassist.ErrProposalStale
	}
	target := target(principal, input.ClientID)
	matches, err := actions.ResolveOpportunityReference(ctx, principal, target, input.OpportunityRef, 2)
	if err != nil {
		return aiassist.ErrProposalStale
	}
	opportunityID, err := uniqueOpportunityID(matches)
	if err != nil || opportunityID != input.OpportunityID {
		return aiassist.ErrProposalStale
	}
	opportunity, err := actions.GetOpportunityInTarget(ctx, principal, target, input.OpportunityID)
	if err != nil || !sameProposalDraftOpportunity(input, opportunity) {
		return aiassist.ErrProposalStale
	}
	preflight, err := actions.PreflightCreateProposal(ctx, proposalCreateCommand(principal, input))
	if err != nil || !matchesProposalDraftPreflight(input, preflight) {
		return aiassist.ErrProposalStale
	}
	return nil
}

func proposalCreateCommand(principal authorization.Principal, input proposalCreatePrepared) sales.CreateProposalCommand {
	return sales.CreateProposalCommand{
		Principal: principal, Target: target(principal, input.ClientID), OpportunityID: string(input.OpportunityID),
		ExpectedClientVersion:      input.ClientVersion,
		ExpectedOpportunityVersion: input.OpportunityVersion,
		DisplayID:                  input.DisplayID, ActorID: principal.ID, Source: "ai_workspace",
	}
}

func validateProposalCreatePrepared(raw json.RawMessage) error {
	_, err := validProposalCreatePrepared(raw)
	return err
}

func validProposalCreatePrepared(raw json.RawMessage) (proposalCreatePrepared, error) {
	input, err := decode[proposalCreatePrepared](raw)
	if err != nil || strings.TrimSpace(input.MSPID) == "" || strings.TrimSpace(input.ClientID) == "" || strings.TrimSpace(input.ClientReference) == "" ||
		strings.TrimSpace(input.ClientDisplayID) == "" || strings.TrimSpace(input.ClientName) == "" || input.ClientVersion < 1 ||
		input.OpportunityID == "" || strings.TrimSpace(input.OpportunityRef) == "" ||
		strings.TrimSpace(input.OpportunityDisplay) == "" || strings.TrimSpace(input.OpportunityName) == "" ||
		input.OpportunityVersion < 1 || strings.TrimSpace(input.DisplayID) == "" {
		return proposalCreatePrepared{}, aiassist.ErrInvalidTool
	}
	return input, nil
}

func validProposalOpportunity(opportunity sales.Opportunity, mspID, clientID string, id sales.OpportunityID) bool {
	return opportunity.ID == id && opportunity.MSPID == mspID && opportunity.ClientID == clientID &&
		strings.TrimSpace(opportunity.DisplayID) != "" && strings.TrimSpace(opportunity.Name) != "" &&
		opportunity.Version > 0
}

func sameProposalDraftClient(input proposalCreatePrepared, client organizations.Client) bool {
	return client.MSPID == input.MSPID && client.ID == input.ClientID && client.DisplayID == input.ClientDisplayID &&
		strings.TrimSpace(client.Name) == input.ClientName && client.Version == input.ClientVersion
}

func sameProposalDraftOpportunity(input proposalCreatePrepared, opportunity sales.Opportunity) bool {
	return validProposalOpportunity(opportunity, input.MSPID, input.ClientID, input.OpportunityID) &&
		opportunity.DisplayID == input.OpportunityDisplay && opportunity.Name == input.OpportunityName &&
		opportunity.Version == input.OpportunityVersion
}

func matchesProposalDraftPreflight(input proposalCreatePrepared, preflight sales.ProposalDraftPreflight) bool {
	return preflight.DisplayID == input.DisplayID && sameProposalDraftOpportunity(input, preflight.Opportunity)
}

func proposalCreatePreview(input proposalCreatePrepared) aiassist.Preview {
	return aiassist.Preview{
		Summary:    "Create draft proposal " + input.DisplayID + " for " + input.OpportunityDisplay,
		TargetType: "opportunity", TargetID: string(input.OpportunityID), TargetVersion: input.OpportunityVersion,
		Changes: map[string]aiassist.Change{
			"proposal_draft": {Before: nil, After: map[string]any{
				"display_id": input.DisplayID, "opportunity_display_id": input.OpportunityDisplay,
				"state": string(sales.ProposalDraft), "version": int64(1),
			}},
		},
	}
}

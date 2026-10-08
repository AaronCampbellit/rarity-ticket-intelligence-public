package rtitools

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type OpportunityOperationalActions interface {
	ListPipelines(
		context.Context,
		authorization.Principal,
	) ([]sales.Pipeline, error)
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
	ResolveStageReference(
		context.Context,
		authorization.Principal,
		scope.Target,
		sales.OpportunityID,
		string,
		int,
	) ([]sales.PipelineStage, error)
	TransitionOpportunity(
		context.Context,
		sales.TransitionCommand,
	) (sales.Opportunity, error)
	CreateOpportunityActivity(
		context.Context,
		sales.CreateOpportunityActivityCommand,
	) (sales.OpportunityActivity, error)
}

type opportunityTransitionRequest struct {
	Client          string `json:"client"`
	Opportunity     string `json:"opportunity"`
	Stage           string `json:"stage"`
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

type opportunityCanonicalPrepared struct {
	ClientID             string                `json:"client_id"`
	ClientReference      string                `json:"client_reference"`
	ClientDisplayID      string                `json:"client_display_id"`
	ClientName           string                `json:"client_name"`
	ClientVersion        int64                 `json:"client_version"`
	OpportunityID        sales.OpportunityID   `json:"opportunity_id"`
	OpportunityReference string                `json:"opportunity_reference"`
	OpportunityDisplayID string                `json:"opportunity_display_id"`
	OpportunityName      string                `json:"opportunity_name"`
	PipelineID           string                `json:"pipeline_id"`
	PipelineKey          string                `json:"pipeline_key"`
	PipelineName         string                `json:"pipeline_name"`
	PipelineVersion      int64                 `json:"pipeline_version"`
	StageID              sales.PipelineStageID `json:"stage_id"`
	ExpectedVersion      int64                 `json:"expected_version"`
}

type opportunityTransitionPrepared struct {
	opportunityCanonicalPrepared
	CurrentStage         sales.PipelineStage `json:"current_stage"`
	DestinationStage     sales.PipelineStage `json:"destination_stage"`
	DestinationReference string              `json:"destination_reference"`
	Reason               string              `json:"reason"`
}

type opportunityActivityCreateRequest struct {
	Client      string `json:"client"`
	Opportunity string `json:"opportunity"`
	Kind        string `json:"kind"`
	Summary     string `json:"summary"`
	Details     string `json:"details"`
}

type opportunityActivityCreatePrepared struct {
	opportunityCanonicalPrepared
	Stage   sales.PipelineStage `json:"stage"`
	Kind    string              `json:"kind"`
	Summary string              `json:"summary"`
	Details string              `json:"details"`
}

func NewOpportunityTransitionTool(
	directory ActiveClientResolver,
	actions OpportunityOperationalActions,
) aiassist.Tool {
	const capability = "opportunity.transition"
	return &functionalTool{
		name: "opportunity.transition", capability: capability, kind: aiassist.ToolWrite,
		prepare: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
		) (json.RawMessage, error) {
			request, err := decode[opportunityTransitionRequest](raw)
			if err != nil || directory == nil || actions == nil {
				return nil, aiassist.ErrInvalidTool
			}
			request.Client = strings.TrimSpace(request.Client)
			request.Opportunity = strings.TrimSpace(request.Opportunity)
			request.Stage = strings.TrimSpace(request.Stage)
			request.Reason = strings.TrimSpace(request.Reason)
			if request.Client == "" || request.Opportunity == "" || request.Stage == "" ||
				request.ExpectedVersion < 1 || request.Reason == "" ||
				prohibitedOpportunityActionLanguage(request.Stage, request.Reason) {
				return nil, aiassist.ErrInvalidTool
			}
			client, err := resolveActiveResourceClient(
				ctx, directory, principal, capability, request.Client,
			)
			if err != nil {
				return nil, err
			}
			canonical, opportunity, err := prepareCanonicalOpportunity(
				ctx, actions, principal, client, request.Client,
				request.Opportunity, request.ExpectedVersion,
			)
			if err != nil {
				return nil, err
			}
			target := target(principal, client.ID)
			current, err := resolveUniqueOpportunityStage(
				ctx, actions, principal, target, opportunity.ID,
				string(opportunity.StageID),
			)
			if err != nil {
				return nil, err
			}
			destination, err := resolveUniqueOpportunityStage(
				ctx, actions, principal, target, opportunity.ID, request.Stage,
			)
			if err != nil {
				return nil, err
			}
			stageReference := strings.TrimSpace(request.Stage)
			if _, err := uuid.Parse(stageReference); err == nil && strings.EqualFold(
				stageReference,
				strings.TrimSpace(string(destination.ID)),
			) {
				return nil, aiassist.ErrInvalidTool
			}
			if err := validateOpportunityTransitionEligibility(opportunity, current, destination); err != nil {
				return nil, err
			}
			return marshalPrepared(opportunityTransitionPrepared{
				opportunityCanonicalPrepared: canonical,
				CurrentStage:                 current, DestinationStage: destination,
				DestinationReference: request.Stage, Reason: request.Reason,
			})
		},
		validate: validateOpportunityTransitionPrepared,
		resolve:  resolveClientResourceScope,
		preview: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
		) (aiassist.Preview, error) {
			input, err := validOpportunityTransitionPrepared(raw)
			if err != nil {
				return aiassist.Preview{}, err
			}
			opportunity, err := reloadCanonicalOpportunity(
				ctx, directory, actions, principal, capability,
				input.opportunityCanonicalPrepared,
			)
			if err != nil {
				return aiassist.Preview{}, err
			}
			target := target(principal, input.ClientID)
			current, err := resolveUniqueOpportunityStage(
				ctx, actions, principal, target, input.OpportunityID,
				string(input.StageID),
			)
			if err != nil || !reflect.DeepEqual(current, input.CurrentStage) {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			destination, err := resolveUniqueOpportunityStage(
				ctx, actions, principal, target, input.OpportunityID,
				input.DestinationReference,
			)
			if err != nil || !reflect.DeepEqual(destination, input.DestinationStage) {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			if err := validateOpportunityTransitionEligibility(opportunity, current, destination); err != nil {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			return opportunityTransitionPreview(input), nil
		},
		execute: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
			_ string,
		) (aiassist.ToolResult, error) {
			input, err := validOpportunityTransitionPrepared(raw)
			if err != nil || actions == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			updated, err := actions.TransitionOpportunity(ctx, sales.TransitionCommand{
				Principal: principal, Target: target(principal, input.ClientID),
				ID: input.OpportunityID, ExpectedVersion: input.ExpectedVersion,
				ExpectedClientVersion:           input.ClientVersion,
				ExpectedPipelineVersion:         input.PipelineVersion,
				ExpectedCurrentStageVersion:     input.CurrentStage.Version,
				ExpectedDestinationStageVersion: input.DestinationStage.Version,
				StageID:                         input.DestinationStage.ID, ActorID: principal.ID,
				Source: "ai_workspace", Reason: input.Reason,
			})
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{
				Summary: "Moved opportunity " + input.OpportunityDisplayID + " to " + input.DestinationStage.Name,
				Data: map[string]any{
					"display_id": input.OpportunityDisplayID,
					"stage":      input.DestinationStage.Name, "version": updated.Version,
				},
			}, nil
		},
	}
}

func NewOpportunityActivityCreateTool(
	directory ActiveClientResolver,
	actions OpportunityOperationalActions,
) aiassist.Tool {
	const capability = "opportunity.activity.create"
	return &functionalTool{
		name: "opportunity.activity.create", capability: capability, kind: aiassist.ToolWrite,
		prepare: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
		) (json.RawMessage, error) {
			request, err := decode[opportunityActivityCreateRequest](raw)
			if err != nil || directory == nil || actions == nil {
				return nil, aiassist.ErrInvalidTool
			}
			request.Client = strings.TrimSpace(request.Client)
			request.Opportunity = strings.TrimSpace(request.Opportunity)
			request.Kind = strings.ToLower(strings.TrimSpace(request.Kind))
			request.Summary = strings.TrimSpace(request.Summary)
			request.Details = strings.TrimSpace(request.Details)
			if request.Client == "" || request.Opportunity == "" ||
				!validOpportunityActivityKind(request.Kind) || request.Summary == "" ||
				request.Details == "" ||
				prohibitedOpportunityActionLanguage(request.Summary, request.Details) {
				return nil, aiassist.ErrInvalidTool
			}
			client, err := resolveActiveResourceClient(
				ctx, directory, principal, capability, request.Client,
			)
			if err != nil {
				return nil, err
			}
			canonical, opportunity, err := prepareCanonicalOpportunity(
				ctx, actions, principal, client, request.Client,
				request.Opportunity, 0,
			)
			if err != nil {
				return nil, err
			}
			stage, err := resolveUniqueOpportunityStage(
				ctx, actions, principal, target(principal, client.ID), opportunity.ID,
				string(opportunity.StageID),
			)
			if err != nil {
				return nil, err
			}
			return marshalPrepared(opportunityActivityCreatePrepared{
				opportunityCanonicalPrepared: canonical,
				Stage:                        stage,
				Kind:                         request.Kind, Summary: request.Summary, Details: request.Details,
			})
		},
		validate: validateOpportunityActivityPrepared,
		resolve:  resolveClientResourceScope,
		preview: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
		) (aiassist.Preview, error) {
			input, err := validOpportunityActivityPrepared(raw)
			if err != nil {
				return aiassist.Preview{}, err
			}
			if _, err := reloadCanonicalOpportunity(
				ctx, directory, actions, principal, capability,
				input.opportunityCanonicalPrepared,
			); err != nil {
				return aiassist.Preview{}, err
			}
			stage, err := resolveUniqueOpportunityStage(
				ctx, actions, principal, target(principal, input.ClientID),
				input.OpportunityID, string(input.StageID),
			)
			if err != nil || !reflect.DeepEqual(stage, input.Stage) {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			return opportunityActivityPreview(input), nil
		},
		execute: func(
			ctx context.Context,
			principal authorization.Principal,
			raw json.RawMessage,
			_ string,
		) (aiassist.ToolResult, error) {
			input, err := validOpportunityActivityPrepared(raw)
			if err != nil || actions == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			activity, err := actions.CreateOpportunityActivity(
				ctx, sales.CreateOpportunityActivityCommand{
					Principal: principal, Target: target(principal, input.ClientID),
					OpportunityID:              input.OpportunityID,
					ExpectedClientVersion:      input.ClientVersion,
					ExpectedOpportunityVersion: input.ExpectedVersion,
					ExpectedPipelineVersion:    input.PipelineVersion,
					ExpectedStageVersion:       input.Stage.Version,
					Kind:                       input.Kind,
					Summary:                    input.Summary, Details: input.Details,
					ActorID: principal.ID, Source: "ai_workspace",
				},
			)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{
				Summary: "Added " + input.Kind + " activity to " + input.OpportunityDisplayID,
				Data: map[string]any{
					"activity_id": activity.ID, "opportunity_display_id": input.OpportunityDisplayID,
					"kind": activity.Kind, "summary": activity.Summary, "details": activity.Details,
				},
			}, nil
		},
	}
}

func prepareCanonicalOpportunity(
	ctx context.Context,
	actions OpportunityOperationalActions,
	principal authorization.Principal,
	client organizations.Client,
	clientReference string,
	opportunityReference string,
	expectedVersion int64,
) (opportunityCanonicalPrepared, sales.Opportunity, error) {
	if client.ID == "" || client.DisplayID == "" || strings.TrimSpace(client.Name) == "" ||
		client.Version < 1 {
		return opportunityCanonicalPrepared{}, sales.Opportunity{}, aiassist.ErrInvalidTool
	}
	target := target(principal, client.ID)
	matches, err := actions.ResolveOpportunityReference(
		ctx, principal, target, opportunityReference, 2,
	)
	if err != nil {
		return opportunityCanonicalPrepared{}, sales.Opportunity{}, err
	}
	opportunityID, err := uniqueOpportunityID(matches)
	if err != nil {
		return opportunityCanonicalPrepared{}, sales.Opportunity{}, err
	}
	opportunity, err := actions.GetOpportunityInTarget(
		ctx, principal, target, opportunityID,
	)
	if err != nil {
		return opportunityCanonicalPrepared{}, sales.Opportunity{}, err
	}
	if !validCanonicalOpportunity(opportunity, principal.Scope.MSPID, client.ID, opportunityID) {
		return opportunityCanonicalPrepared{}, sales.Opportunity{}, scope.ErrNotFound
	}
	if expectedVersion > 0 {
		if err := object.RequireVersion(opportunity.Version, expectedVersion); err != nil {
			return opportunityCanonicalPrepared{}, sales.Opportunity{}, err
		}
	} else {
		expectedVersion = opportunity.Version
	}
	pipeline, err := resolveOpportunityPipeline(ctx, actions, principal, opportunity.PipelineID)
	if err != nil {
		return opportunityCanonicalPrepared{}, sales.Opportunity{}, err
	}
	return opportunityCanonicalPrepared{
		ClientID: client.ID, ClientReference: clientReference,
		ClientDisplayID: client.DisplayID, ClientName: strings.TrimSpace(client.Name),
		ClientVersion: client.Version, OpportunityID: opportunity.ID,
		OpportunityReference: opportunityReference,
		OpportunityDisplayID: opportunity.DisplayID, OpportunityName: opportunity.Name,
		PipelineID: opportunity.PipelineID, PipelineKey: pipeline.Key,
		PipelineName: pipeline.Name, PipelineVersion: pipeline.Version,
		StageID:         opportunity.StageID,
		ExpectedVersion: expectedVersion,
	}, opportunity, nil
}

func reloadCanonicalOpportunity(
	ctx context.Context,
	directory ActiveClientResolver,
	actions OpportunityOperationalActions,
	principal authorization.Principal,
	capability string,
	input opportunityCanonicalPrepared,
) (sales.Opportunity, error) {
	client, err := resolveActiveResourceClient(
		ctx, directory, principal, capability, input.ClientReference,
	)
	if err != nil || client.ID != input.ClientID || client.DisplayID != input.ClientDisplayID ||
		strings.TrimSpace(client.Name) != input.ClientName || client.Version != input.ClientVersion {
		return sales.Opportunity{}, aiassist.ErrProposalStale
	}
	target := target(principal, input.ClientID)
	matches, err := actions.ResolveOpportunityReference(
		ctx, principal, target, input.OpportunityReference, 2,
	)
	if err != nil {
		return sales.Opportunity{}, aiassist.ErrProposalStale
	}
	opportunityID, err := uniqueOpportunityID(matches)
	if err != nil || opportunityID != input.OpportunityID {
		return sales.Opportunity{}, aiassist.ErrProposalStale
	}
	opportunity, err := actions.GetOpportunityInTarget(
		ctx, principal, target, input.OpportunityID,
	)
	if err != nil {
		return sales.Opportunity{}, err
	}
	if opportunity.Version != input.ExpectedVersion {
		return sales.Opportunity{}, object.ErrVersionConflict
	}
	if !validCanonicalOpportunity(
		opportunity, principal.Scope.MSPID, input.ClientID, input.OpportunityID,
	) || opportunity.DisplayID != input.OpportunityDisplayID ||
		opportunity.Name != input.OpportunityName || opportunity.PipelineID != input.PipelineID ||
		opportunity.StageID != input.StageID {
		return sales.Opportunity{}, aiassist.ErrProposalStale
	}
	pipeline, err := resolveOpportunityPipeline(ctx, actions, principal, input.PipelineID)
	if err != nil || pipeline.Key != input.PipelineKey || pipeline.Name != input.PipelineName ||
		pipeline.Version != input.PipelineVersion {
		return sales.Opportunity{}, aiassist.ErrProposalStale
	}
	return opportunity, nil
}

func resolveOpportunityPipeline(
	ctx context.Context,
	actions OpportunityOperationalActions,
	principal authorization.Principal,
	pipelineID string,
) (sales.Pipeline, error) {
	pipelines, err := actions.ListPipelines(ctx, principal)
	if err != nil {
		return sales.Pipeline{}, err
	}
	var found sales.Pipeline
	for _, pipeline := range pipelines {
		if pipeline.ID != pipelineID {
			continue
		}
		if found.ID != "" {
			return sales.Pipeline{}, sales.ErrAmbiguousReference
		}
		found = pipeline
	}
	if found.ID == "" || found.MSPID != principal.Scope.MSPID ||
		strings.TrimSpace(found.Key) == "" || strings.TrimSpace(found.Name) == "" ||
		found.Version < 1 {
		return sales.Pipeline{}, scope.ErrNotFound
	}
	return found, nil
}

func validCanonicalOpportunity(
	opportunity sales.Opportunity,
	mspID string,
	clientID string,
	opportunityID sales.OpportunityID,
) bool {
	return opportunity.ID == opportunityID && opportunity.MSPID == mspID &&
		opportunity.ClientID == clientID && opportunity.DisplayID != "" &&
		strings.TrimSpace(opportunity.Name) != "" && opportunity.PipelineID != "" &&
		opportunity.StageID != "" && opportunity.Version > 0
}

func resolveUniqueOpportunityStage(
	ctx context.Context,
	actions OpportunityOperationalActions,
	principal authorization.Principal,
	target scope.Target,
	opportunityID sales.OpportunityID,
	reference string,
) (sales.PipelineStage, error) {
	matches, err := actions.ResolveStageReference(
		ctx, principal, target, opportunityID, reference, 2,
	)
	if err != nil {
		return sales.PipelineStage{}, err
	}
	if len(matches) > 1 {
		return sales.PipelineStage{}, sales.ErrAmbiguousReference
	}
	if len(matches) == 0 || matches[0].ID == "" || matches[0].PipelineID == "" ||
		strings.TrimSpace(matches[0].Key) == "" || strings.TrimSpace(matches[0].Name) == "" ||
		matches[0].Version < 1 {
		return sales.PipelineStage{}, scope.ErrNotFound
	}
	return matches[0], nil
}

func validateOpportunityTransitionEligibility(
	opportunity sales.Opportunity,
	current sales.PipelineStage,
	destination sales.PipelineStage,
) error {
	if current.ID != opportunity.StageID || current.PipelineID != opportunity.PipelineID ||
		destination.PipelineID != opportunity.PipelineID ||
		!containsOpportunityStage(current.AllowedNext, destination.ID) {
		return sales.ErrTransitionNotAllowed
	}
	for _, field := range destination.RequiredFields {
		if strings.TrimSpace(opportunity.Fields[field]) == "" {
			return sales.ErrStageRequirements
		}
	}
	if destination.RequiresProposal && !opportunity.ProposalIssued {
		return sales.ErrProposalRequired
	}
	if destination.RequiresApproval && !opportunity.ApprovalGranted {
		return sales.ErrApprovalRequired
	}
	return nil
}

func containsOpportunityStage(
	allowed []sales.PipelineStageID,
	id sales.PipelineStageID,
) bool {
	for _, candidate := range allowed {
		if candidate == id {
			return true
		}
	}
	return false
}

func validateOpportunityTransitionPrepared(raw json.RawMessage) error {
	_, err := validOpportunityTransitionPrepared(raw)
	return err
}

func validOpportunityTransitionPrepared(
	raw json.RawMessage,
) (opportunityTransitionPrepared, error) {
	input, err := decode[opportunityTransitionPrepared](raw)
	if err != nil || !validOpportunityCanonicalPrepared(input.opportunityCanonicalPrepared) ||
		input.DestinationReference == "" || input.Reason == "" ||
		input.CurrentStage.ID != input.StageID || input.CurrentStage.PipelineID != input.PipelineID ||
		input.CurrentStage.Version < 1 || input.DestinationStage.ID == "" ||
		input.DestinationStage.PipelineID != input.PipelineID || input.DestinationStage.Version < 1 ||
		prohibitedOpportunityActionLanguage(input.DestinationReference, input.Reason) {
		return opportunityTransitionPrepared{}, aiassist.ErrInvalidTool
	}
	return input, nil
}

func validateOpportunityActivityPrepared(raw json.RawMessage) error {
	_, err := validOpportunityActivityPrepared(raw)
	return err
}

func validOpportunityActivityPrepared(
	raw json.RawMessage,
) (opportunityActivityCreatePrepared, error) {
	input, err := decode[opportunityActivityCreatePrepared](raw)
	if err != nil || !validOpportunityCanonicalPrepared(input.opportunityCanonicalPrepared) ||
		input.Stage.ID != input.StageID || input.Stage.PipelineID != input.PipelineID || input.Stage.Version < 1 ||
		!validOpportunityActivityKind(input.Kind) || strings.TrimSpace(input.Summary) == "" ||
		strings.TrimSpace(input.Details) == "" ||
		prohibitedOpportunityActionLanguage(input.Summary, input.Details) {
		return opportunityActivityCreatePrepared{}, aiassist.ErrInvalidTool
	}
	return input, nil
}

func validOpportunityCanonicalPrepared(input opportunityCanonicalPrepared) bool {
	return input.ClientID != "" && input.ClientReference != "" &&
		input.ClientDisplayID != "" && input.ClientName != "" && input.ClientVersion > 0 &&
		input.OpportunityID != "" && input.OpportunityReference != "" &&
		input.OpportunityDisplayID != "" && input.OpportunityName != "" &&
		input.PipelineID != "" && input.PipelineKey != "" && input.PipelineName != "" &&
		input.PipelineVersion > 0 && input.StageID != "" && input.ExpectedVersion > 0
}

func validOpportunityActivityKind(kind string) bool {
	switch kind {
	case "note", "call", "email", "meeting":
		return true
	default:
		return false
	}
}

func opportunityTransitionPreview(input opportunityTransitionPrepared) aiassist.Preview {
	return aiassist.Preview{
		Summary:    "Move opportunity " + input.OpportunityDisplayID + " to " + input.DestinationStage.Name,
		TargetType: "opportunity", TargetID: string(input.OpportunityID),
		TargetVersion: input.ExpectedVersion,
		Changes: map[string]aiassist.Change{
			"client": {Before: nil, After: map[string]any{
				"display_id": input.ClientDisplayID, "name": input.ClientName,
				"version": input.ClientVersion,
			}},
			"opportunity": {
				Before: map[string]any{
					"display_id": input.OpportunityDisplayID, "name": input.OpportunityName,
					"version": input.ExpectedVersion,
				},
				After: map[string]any{
					"display_id": input.OpportunityDisplayID, "name": input.OpportunityName,
					"version": input.ExpectedVersion + 1,
				},
			},
			"pipeline": {Before: nil, After: map[string]any{
				"key": input.PipelineKey, "name": input.PipelineName,
				"version": input.PipelineVersion,
			}},
			"stage": {
				Before: opportunityStagePreview(input.CurrentStage),
				After:  opportunityStagePreview(input.DestinationStage),
			},
			"reason": {Before: nil, After: input.Reason},
		},
	}
}

func opportunityActivityPreview(input opportunityActivityCreatePrepared) aiassist.Preview {
	return aiassist.Preview{
		Summary:    "Add " + input.Kind + " activity to " + input.OpportunityDisplayID,
		TargetType: "opportunity", TargetID: string(input.OpportunityID),
		TargetVersion: input.ExpectedVersion,
		Changes: map[string]aiassist.Change{
			"client": {Before: nil, After: map[string]any{
				"display_id": input.ClientDisplayID, "name": input.ClientName,
				"version": input.ClientVersion,
			}},
			"opportunity": {Before: nil, After: map[string]any{
				"display_id": input.OpportunityDisplayID, "name": input.OpportunityName,
				"version": input.ExpectedVersion,
			}},
			"pipeline": {Before: nil, After: map[string]any{
				"key": input.PipelineKey, "name": input.PipelineName,
				"version": input.PipelineVersion,
			}},
			"kind":    {Before: nil, After: input.Kind},
			"summary": {Before: nil, After: input.Summary},
			"details": {Before: nil, After: input.Details},
		},
	}
}

func opportunityStagePreview(stage sales.PipelineStage) map[string]any {
	return map[string]any{
		"id": string(stage.ID), "key": stage.Key, "name": stage.Name,
		"version": stage.Version,
	}
}

func prohibitedOpportunityActionLanguage(values ...string) bool {
	blocked := map[string]struct{}{
		"convert": {}, "converted": {}, "converting": {}, "conversion": {},
		"amount": {}, "pricing": {}, "price": {}, "probability": {},
		"credential": {}, "credentials": {}, "password": {}, "secret": {},
		"integration": {}, "integrations": {}, "contract": {}, "contractual": {},
		"payment": {}, "payments": {}, "invoice": {}, "invoices": {},
	}
	for _, value := range values {
		for _, word := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}) {
			if _, found := blocked[word]; found {
				return true
			}
		}
	}
	return false
}

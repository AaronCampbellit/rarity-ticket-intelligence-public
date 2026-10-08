package sales

import (
	"context"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type TransitionMutation struct {
	Opportunity           Opportunity
	PreviousStage         PipelineStageID
	ExpectedClientVersion int64
	Pipeline              Pipeline
	CurrentStage          PipelineStage
	DestinationStage      PipelineStage
	Audit                 mutation.AuditRecord
	Event                 mutation.EventRecord
}

type CreateProspectMutation struct {
	Prospect Prospect
	Audit    mutation.AuditRecord
	Event    mutation.EventRecord
}

type CreatePipelineMutation struct {
	Pipeline Pipeline
	Audit    mutation.AuditRecord
	Event    mutation.EventRecord
}

type CreateOpportunityMutation struct {
	Opportunity Opportunity
	Audit       mutation.AuditRecord
	Event       mutation.EventRecord
}

type CreateOpportunityActivityMutation struct {
	Activity                   OpportunityActivity
	ExpectedClientVersion      int64
	ExpectedOpportunityVersion int64
	Pipeline                   Pipeline
	Stage                      PipelineStage
	Audit                      mutation.AuditRecord
	Event                      mutation.EventRecord
}

type ReplaceOpportunityFieldsMutation struct {
	Opportunity Opportunity
	Audit       mutation.AuditRecord
	Event       mutation.EventRecord
}

type ReplaceOpportunityParticipantsMutation struct {
	Opportunity Opportunity
	Audit       mutation.AuditRecord
	Event       mutation.EventRecord
}

type Repository interface {
	FindOpportunity(context.Context, scope.Target, OpportunityID) (Opportunity, error)
	ListOpportunities(context.Context, scope.Target, OpportunityListFilter) ([]Opportunity, error)
	FindOpportunitiesByReference(context.Context, scope.Target, string, int) ([]Opportunity, error)
	FindStagesByReference(context.Context, scope.Target, OpportunityID, string, int) ([]PipelineStage, error)
	FindStage(context.Context, string, PipelineStageID) (PipelineStage, error)
	TransitionAtomic(context.Context, TransitionMutation) error
	ReplaceOpportunityFieldsAtomic(context.Context, ReplaceOpportunityFieldsMutation) error
	ReplaceOpportunityParticipantsAtomic(context.Context, ReplaceOpportunityParticipantsMutation) error
	CreateProspectAtomic(context.Context, CreateProspectMutation) error
	ListProspects(context.Context, string, int) ([]Prospect, error)
	FindProspectsByReference(context.Context, string, string, int) ([]Prospect, error)
	CreatePipelineAtomic(context.Context, CreatePipelineMutation) error
	ListPipelines(context.Context, string) ([]Pipeline, error)
	CreateOpportunityAtomic(context.Context, CreateOpportunityMutation) error
	CreateOpportunityActivityAtomic(context.Context, CreateOpportunityActivityMutation) error
	ListOpportunityActivities(context.Context, scope.Target, OpportunityID, int) ([]OpportunityActivity, error)
	Forecast(context.Context, scope.Target, string) ([]ForecastBucket, error)
}

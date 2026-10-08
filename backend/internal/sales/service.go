package sales

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrInvalidSalesRecord       = errors.New("invalid sales record")
	ErrStageNotFound            = errors.New("pipeline stage not found")
	ErrStageRequirements        = errors.New("pipeline stage requirements not met")
	ErrTransitionNotAllowed     = errors.New("pipeline transition not allowed")
	ErrProposalRequired         = errors.New("issued proposal required")
	ErrApprovalRequired         = errors.New("approval required")
	ErrProspectIdentityConflict = errors.New("prospect identity conflict")
	ErrAmbiguousReference       = errors.New("ambiguous sales reference")
)

type Service struct {
	repository Repository
	now        func() time.Time
	newID      func() string
}

func NewService(repository Repository, now func() time.Time, newID func() string) *Service {
	return &Service{repository: repository, now: now, newID: newID}
}

type TransitionCommand struct {
	Principal                       authorization.Principal
	Target                          scope.Target
	ID                              OpportunityID
	ExpectedVersion                 int64
	ExpectedClientVersion           int64
	ExpectedPipelineVersion         int64
	ExpectedCurrentStageVersion     int64
	ExpectedDestinationStageVersion int64
	StageID                         PipelineStageID
	ActorID                         string
	Source                          string
	Reason                          string
}

type CreateOpportunityCommand struct {
	Principal       authorization.Principal
	ClientID        string
	ProspectID      string
	PipelineID      string
	StageID         PipelineStageID
	DisplayID       string
	Name            string
	Description     string
	Amount          Money
	OwnerID         string
	ExpectedCloseOn string
	ActorID         string
	Source          string
}

type CreateOpportunityActivityCommand struct {
	Principal                  authorization.Principal
	Target                     scope.Target
	OpportunityID              OpportunityID
	ExpectedClientVersion      int64
	ExpectedOpportunityVersion int64
	ExpectedPipelineVersion    int64
	ExpectedStageVersion       int64
	Kind                       string
	Summary                    string
	Details                    string
	OccurredAt                 time.Time
	ActorID                    string
	Source                     string
}

type ReplaceOpportunityCustomFieldsCommand struct {
	Principal       authorization.Principal
	ID              OpportunityID
	ExpectedVersion int64
	Fields          map[string]string
	ActorID         string
	Source          string
}

type ReplaceOpportunityParticipantsCommand struct {
	Principal       authorization.Principal
	ID              OpportunityID
	ExpectedVersion int64
	TeamID          string
	ContactIDs      []string
	ActorID         string
	Source          string
}

func (s *Service) ReplaceOpportunityParticipants(
	ctx context.Context,
	command ReplaceOpportunityParticipantsCommand,
) (Opportunity, error) {
	target := scope.Target{
		MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
	}
	contactIDs, valid := normalizeOpportunityContactIDs(command.ContactIDs)
	if target.MSPID == "" || target.ClientID == "" || command.ID == "" ||
		command.ExpectedVersion < 1 || strings.TrimSpace(command.ActorID) == "" ||
		strings.TrimSpace(command.Source) == "" || s.now == nil || s.newID == nil ||
		!valid {
		return Opportunity{}, ErrInvalidSalesRecord
	}
	if err := authorization.Authorize(command.Principal, "opportunity.update", target); err != nil {
		return Opportunity{}, err
	}
	opportunity, err := s.repository.FindOpportunity(ctx, target, command.ID)
	if err != nil {
		return Opportunity{}, err
	}
	if opportunity.MSPID != target.MSPID || opportunity.ClientID != target.ClientID {
		return Opportunity{}, scope.ErrNotFound
	}
	if err := object.RequireVersion(opportunity.Version, command.ExpectedVersion); err != nil {
		return Opportunity{}, err
	}
	opportunity.TeamID = strings.TrimSpace(command.TeamID)
	opportunity.ContactIDs = contactIDs
	opportunity.Version++
	opportunity.UpdatedAt = s.now().UTC()
	opportunity.UpdatedBy = command.ActorID
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := ReplaceOpportunityParticipantsMutation{
		Opportunity: opportunity,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: opportunity.UpdatedAt,
			MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "opportunity.participants.replaced", SubjectType: "opportunity",
			SubjectID: string(opportunity.ID), SubjectVersion: opportunity.Version,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "opportunity.participants.replaced",
			SchemaVersion: 1, OccurredAt: opportunity.UpdatedAt,
			MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "opportunity", SubjectID: string(opportunity.ID),
			SubjectVersion: opportunity.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.ReplaceOpportunityParticipantsAtomic(ctx, accepted); err != nil {
		return Opportunity{}, err
	}
	return opportunity, nil
}

func normalizeOpportunityContactIDs(input []string) ([]string, bool) {
	if len(input) > 50 {
		return nil, false
	}
	seen := make(map[string]struct{}, len(input))
	found := make([]string, 0, len(input))
	for _, raw := range input {
		id := strings.TrimSpace(raw)
		if id == "" {
			return nil, false
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		found = append(found, id)
	}
	return found, true
}

func (s *Service) ReplaceOpportunityCustomFields(
	ctx context.Context,
	command ReplaceOpportunityCustomFieldsCommand,
) (Opportunity, error) {
	target := scope.Target{
		MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
	}
	fields, valid := normalizeOpportunityCustomFields(command.Fields)
	if target.MSPID == "" || target.ClientID == "" || command.ID == "" ||
		command.ExpectedVersion < 1 || strings.TrimSpace(command.ActorID) == "" ||
		strings.TrimSpace(command.Source) == "" || s.now == nil || s.newID == nil ||
		!valid {
		return Opportunity{}, ErrInvalidSalesRecord
	}
	if err := authorization.Authorize(command.Principal, "opportunity.update", target); err != nil {
		return Opportunity{}, err
	}
	opportunity, err := s.repository.FindOpportunity(ctx, target, command.ID)
	if err != nil {
		return Opportunity{}, err
	}
	if opportunity.MSPID != target.MSPID || opportunity.ClientID != target.ClientID {
		return Opportunity{}, scope.ErrNotFound
	}
	if err := object.RequireVersion(opportunity.Version, command.ExpectedVersion); err != nil {
		return Opportunity{}, err
	}
	opportunity.CustomFields = fields
	if opportunity.Fields == nil {
		opportunity.Fields = make(map[FieldKey]string)
	}
	for key := range opportunity.Fields {
		if !reservedOpportunityField(string(key)) {
			delete(opportunity.Fields, key)
		}
	}
	for key, value := range fields {
		opportunity.Fields[FieldKey(key)] = value
	}
	opportunity.Version++
	opportunity.UpdatedAt = s.now().UTC()
	opportunity.UpdatedBy = command.ActorID
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := ReplaceOpportunityFieldsMutation{
		Opportunity: opportunity,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: opportunity.UpdatedAt,
			MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "opportunity.custom_fields.replaced", SubjectType: "opportunity",
			SubjectID: string(opportunity.ID), SubjectVersion: opportunity.Version,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "opportunity.custom_fields.replaced",
			SchemaVersion: 1, OccurredAt: opportunity.UpdatedAt,
			MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "opportunity", SubjectID: string(opportunity.ID),
			SubjectVersion: opportunity.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.ReplaceOpportunityFieldsAtomic(ctx, accepted); err != nil {
		return Opportunity{}, err
	}
	return opportunity, nil
}

func normalizeOpportunityCustomFields(input map[string]string) (map[string]string, bool) {
	if len(input) > 50 {
		return nil, false
	}
	found := make(map[string]string, len(input))
	for rawKey, rawValue := range input {
		key, value := strings.TrimSpace(rawKey), strings.TrimSpace(rawValue)
		if len(key) < 1 || len(key) > 64 || len(value) > 4000 ||
			reservedOpportunityField(key) || !validOpportunityFieldKey(key) {
			return nil, false
		}
		found[key] = value
	}
	return found, true
}

func reservedOpportunityField(key string) bool {
	switch key {
	case "display_id", "name", "description", "expected_close_on", "owner_id":
		return true
	default:
		return false
	}
}

func validOpportunityFieldKey(key string) bool {
	for index, value := range key {
		if (value >= 'a' && value <= 'z') || (index > 0 && value >= '0' && value <= '9') ||
			(index > 0 && value == '_') {
			continue
		}
		return false
	}
	return true
}

func (s *Service) CreateOpportunityActivity(
	ctx context.Context,
	command CreateOpportunityActivityCommand,
) (OpportunityActivity, error) {
	kind := strings.ToLower(strings.TrimSpace(command.Kind))
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		}
	}
	if target.MSPID == "" || command.OpportunityID == "" ||
		command.ExpectedClientVersion < 0 || command.ExpectedOpportunityVersion < 0 ||
		command.ExpectedPipelineVersion < 0 || command.ExpectedStageVersion < 0 ||
		!validActivityKind(kind) || strings.TrimSpace(command.Summary) == "" ||
		command.ActorID == "" || command.Source == "" || s.newID == nil {
		return OpportunityActivity{}, ErrInvalidSalesRecord
	}
	if err := authorization.Authorize(command.Principal, "opportunity.activity.create", target); err != nil {
		return OpportunityActivity{}, err
	}
	opportunity, err := s.repository.FindOpportunity(ctx, target, command.OpportunityID)
	if err != nil {
		return OpportunityActivity{}, err
	}
	if opportunity.MSPID != target.MSPID || opportunity.ClientID != target.ClientID {
		return OpportunityActivity{}, scope.ErrNotFound
	}
	if command.ExpectedOpportunityVersion > 0 {
		if err := object.RequireVersion(opportunity.Version, command.ExpectedOpportunityVersion); err != nil {
			return OpportunityActivity{}, err
		}
	}
	stage, err := s.repository.FindStage(ctx, opportunity.PipelineID, opportunity.StageID)
	if err != nil {
		return OpportunityActivity{}, err
	}
	if command.ExpectedStageVersion > 0 {
		if err := object.RequireVersion(stage.Version, command.ExpectedStageVersion); err != nil {
			return OpportunityActivity{}, err
		}
	}
	pipeline, err := s.findOpportunityPipeline(ctx, target.MSPID, opportunity.PipelineID)
	if err != nil {
		return OpportunityActivity{}, err
	}
	if command.ExpectedPipelineVersion > 0 {
		if err := object.RequireVersion(pipeline.Version, command.ExpectedPipelineVersion); err != nil {
			return OpportunityActivity{}, err
		}
	}
	now := s.now().UTC()
	occurredAt := command.OccurredAt.UTC()
	if command.OccurredAt.IsZero() {
		occurredAt = now
	}
	activityID, auditID, eventID, correlationID :=
		s.newID(), s.newID(), s.newID(), s.newID()
	activity := OpportunityActivity{
		ID: activityID, MSPID: target.MSPID, ClientID: target.ClientID,
		OpportunityID: command.OpportunityID, Kind: kind,
		Summary: strings.TrimSpace(command.Summary),
		Details: strings.TrimSpace(command.Details), OccurredAt: occurredAt,
		CreatedAt: now, CreatedBy: command.ActorID,
	}
	accepted := CreateOpportunityActivityMutation{
		Activity: activity, ExpectedClientVersion: command.ExpectedClientVersion,
		ExpectedOpportunityVersion: opportunity.Version, Pipeline: pipeline, Stage: stage,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "opportunity.activity.created", SubjectType: "opportunity_activity",
			SubjectID: activityID, SubjectVersion: 1, Source: command.Source,
			CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "opportunity.activity.created", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "opportunity_activity", SubjectID: activityID,
			SubjectVersion: 1, Source: command.Source, CorrelationID: correlationID,
		},
	}
	if err := s.repository.CreateOpportunityActivityAtomic(ctx, accepted); err != nil {
		return OpportunityActivity{}, err
	}
	return activity, nil
}

func validActivityKind(kind string) bool {
	switch kind {
	case "note", "call", "email", "meeting":
		return true
	default:
		return false
	}
}

func (s *Service) Forecast(
	ctx context.Context,
	principal authorization.Principal,
	pipelineID string,
) ([]ForecastBucket, error) {
	if principal.Scope.MSPID == "" {
		return nil, ErrInvalidSalesRecord
	}
	target := scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID}
	if err := authorization.Authorize(principal, "opportunity.read", target); err != nil {
		return nil, err
	}
	return s.repository.Forecast(ctx, target, strings.TrimSpace(pipelineID))
}

func (s *Service) GetOpportunity(
	ctx context.Context,
	principal authorization.Principal,
	id OpportunityID,
) (Opportunity, error) {
	return s.GetOpportunityInTarget(ctx, principal, scope.Target{
		MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
	}, id)
}

func (s *Service) GetOpportunityInTarget(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	id OpportunityID,
) (Opportunity, error) {
	if target.MSPID == "" || id == "" {
		return Opportunity{}, ErrInvalidSalesRecord
	}
	if err := authorization.Authorize(principal, "opportunity.read", target); err != nil {
		return Opportunity{}, err
	}
	return s.repository.FindOpportunity(ctx, target, id)
}

func (s *Service) ListOpportunities(
	ctx context.Context,
	principal authorization.Principal,
	filter OpportunityListFilter,
) ([]Opportunity, error) {
	return s.ListOpportunitiesInTarget(ctx, principal, scope.Target{
		MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
	}, filter)
}

func (s *Service) ListOpportunitiesInTarget(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	filter OpportunityListFilter,
) ([]Opportunity, error) {
	if target.MSPID == "" ||
		filter.BeforeUpdatedAt.IsZero() != (filter.BeforeID == "") {
		return nil, ErrInvalidSalesRecord
	}
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	filter.PipelineID = strings.TrimSpace(filter.PipelineID)
	if err := authorization.Authorize(principal, "opportunity.read", target); err != nil {
		return nil, err
	}
	return s.repository.ListOpportunities(ctx, target, filter)
}

func (s *Service) ResolveOpportunityReference(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	reference string,
	limit int,
) ([]Opportunity, error) {
	reference = strings.TrimSpace(reference)
	if target.MSPID == "" || target.ClientID == "" || reference == "" ||
		limit < 1 || limit > 2 || s.repository == nil {
		return nil, ErrInvalidSalesRecord
	}
	if err := authorization.Authorize(principal, "opportunity.read", target); err != nil {
		return nil, err
	}
	return s.repository.FindOpportunitiesByReference(ctx, target, reference, limit)
}

func (s *Service) ResolveStageReference(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	opportunityID OpportunityID,
	reference string,
	limit int,
) ([]PipelineStage, error) {
	reference = strings.TrimSpace(reference)
	if target.MSPID == "" || target.ClientID == "" || opportunityID == "" ||
		reference == "" || limit < 1 || limit > 2 || s.repository == nil {
		return nil, ErrInvalidSalesRecord
	}
	if err := authorization.Authorize(principal, "opportunity.read", target); err != nil {
		return nil, err
	}
	return s.repository.FindStagesByReference(
		ctx, target, opportunityID, reference, limit,
	)
}

func (s *Service) ListOpportunityActivities(
	ctx context.Context,
	principal authorization.Principal,
	opportunityID OpportunityID,
	limit int,
) ([]OpportunityActivity, error) {
	if principal.Scope.MSPID == "" || opportunityID == "" {
		return nil, ErrInvalidSalesRecord
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	target := scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID}
	if err := authorization.Authorize(principal, "opportunity.read", target); err != nil {
		return nil, err
	}
	if _, err := s.repository.FindOpportunity(ctx, target, opportunityID); err != nil {
		return nil, err
	}
	return s.repository.ListOpportunityActivities(ctx, target, opportunityID, limit)
}

func (s *Service) CreateOpportunity(
	ctx context.Context,
	command CreateOpportunityCommand,
) (Opportunity, error) {
	clientID, prospectID := strings.TrimSpace(command.ClientID), strings.TrimSpace(command.ProspectID)
	if command.Principal.Scope.MSPID == "" ||
		(clientID == "") == (prospectID == "") ||
		strings.TrimSpace(command.PipelineID) == "" || command.StageID == "" ||
		strings.TrimSpace(command.DisplayID) == "" ||
		strings.TrimSpace(command.Name) == "" ||
		command.Amount.Minor < 0 || len(strings.TrimSpace(command.Amount.Currency)) != 3 ||
		command.ActorID == "" || command.Source == "" || s.newID == nil {
		return Opportunity{}, ErrInvalidSalesRecord
	}
	if clientID == "" && command.Principal.Scope.ClientID != "" {
		return Opportunity{}, scope.ErrNotFound
	}
	target := scope.Target{
		MSPID: command.Principal.Scope.MSPID, ClientID: clientID,
	}
	if err := authorization.Authorize(
		command.Principal, "opportunity.create", target,
	); err != nil {
		return Opportunity{}, err
	}
	stage, err := s.repository.FindStage(ctx, strings.TrimSpace(command.PipelineID), command.StageID)
	if err != nil {
		return Opportunity{}, err
	}
	if stage.PipelineID != strings.TrimSpace(command.PipelineID) {
		return Opportunity{}, ErrStageNotFound
	}
	fields := map[FieldKey]string{
		"display_id":        strings.TrimSpace(command.DisplayID),
		"name":              strings.TrimSpace(command.Name),
		"description":       strings.TrimSpace(command.Description),
		"expected_close_on": strings.TrimSpace(command.ExpectedCloseOn),
		"owner_id":          strings.TrimSpace(command.OwnerID),
	}
	if fields["expected_close_on"] != "" {
		if _, err := time.Parse(time.DateOnly, fields["expected_close_on"]); err != nil {
			return Opportunity{}, ErrInvalidSalesRecord
		}
	}
	for _, field := range stage.RequiredFields {
		if strings.TrimSpace(fields[field]) == "" {
			return Opportunity{}, ErrStageRequirements
		}
	}
	now := s.now().UTC()
	opportunityID, auditID, eventID, correlationID :=
		s.newID(), s.newID(), s.newID(), s.newID()
	opportunity := Opportunity{
		ID: OpportunityID(opportunityID), MSPID: target.MSPID,
		ClientID: clientID, ProspectID: prospectID,
		PipelineID: strings.TrimSpace(command.PipelineID), StageID: command.StageID,
		DisplayID: strings.TrimSpace(command.DisplayID),
		Name:      strings.TrimSpace(command.Name),
		Amount: Money{
			Minor:    command.Amount.Minor,
			Currency: strings.ToUpper(strings.TrimSpace(command.Amount.Currency)),
		},
		Fields: fields, Version: 1, UpdatedAt: now, UpdatedBy: command.ActorID,
	}
	accepted := CreateOpportunityMutation{
		Opportunity: opportunity,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: clientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "opportunity.created", SubjectType: "opportunity",
			SubjectID: opportunityID, SubjectVersion: 1,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "opportunity.created", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: clientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "opportunity", SubjectID: opportunityID,
			SubjectVersion: 1, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.CreateOpportunityAtomic(ctx, accepted); err != nil {
		return Opportunity{}, err
	}
	return opportunity, nil
}

func (s *Service) TransitionOpportunity(ctx context.Context, command TransitionCommand) (Opportunity, error) {
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		}
	}
	reason := strings.TrimSpace(command.Reason)
	if command.ID == "" || command.StageID == "" || reason == "" ||
		command.ExpectedClientVersion < 0 || command.ExpectedPipelineVersion < 0 ||
		command.ExpectedCurrentStageVersion < 0 || command.ExpectedDestinationStageVersion < 0 ||
		command.ExpectedVersion < 1 || command.ActorID == "" || command.Source == "" {
		return Opportunity{}, ErrInvalidSalesRecord
	}
	if err := authorization.Authorize(command.Principal, "opportunity.transition", target); err != nil {
		return Opportunity{}, err
	}
	opportunity, err := s.repository.FindOpportunity(ctx, target, command.ID)
	if err != nil {
		return Opportunity{}, err
	}
	if opportunity.MSPID != target.MSPID || opportunity.ClientID != target.ClientID {
		return Opportunity{}, scope.ErrNotFound
	}
	if err := object.RequireVersion(opportunity.Version, command.ExpectedVersion); err != nil {
		return Opportunity{}, err
	}
	current, err := s.repository.FindStage(ctx, opportunity.PipelineID, opportunity.StageID)
	if err != nil {
		return Opportunity{}, err
	}
	next, err := s.repository.FindStage(ctx, opportunity.PipelineID, command.StageID)
	if err != nil {
		return Opportunity{}, err
	}
	if current.PipelineID != opportunity.PipelineID || next.PipelineID != opportunity.PipelineID ||
		!containsStage(current.AllowedNext, next.ID) {
		return Opportunity{}, ErrTransitionNotAllowed
	}
	if command.ExpectedCurrentStageVersion > 0 {
		if err := object.RequireVersion(current.Version, command.ExpectedCurrentStageVersion); err != nil {
			return Opportunity{}, err
		}
	}
	if command.ExpectedDestinationStageVersion > 0 {
		if err := object.RequireVersion(next.Version, command.ExpectedDestinationStageVersion); err != nil {
			return Opportunity{}, err
		}
	}
	pipeline, err := s.findOpportunityPipeline(ctx, target.MSPID, opportunity.PipelineID)
	if err != nil {
		return Opportunity{}, err
	}
	if command.ExpectedPipelineVersion > 0 {
		if err := object.RequireVersion(pipeline.Version, command.ExpectedPipelineVersion); err != nil {
			return Opportunity{}, err
		}
	}
	for _, field := range next.RequiredFields {
		if strings.TrimSpace(opportunity.Fields[field]) == "" {
			return Opportunity{}, ErrStageRequirements
		}
	}
	if next.RequiresProposal && !opportunity.ProposalIssued {
		return Opportunity{}, ErrProposalRequired
	}
	if next.RequiresApproval && !opportunity.ApprovalGranted {
		return Opportunity{}, ErrApprovalRequired
	}
	now := s.now().UTC()
	previous := opportunity.StageID
	opportunity.StageID = next.ID
	opportunity.Version++
	opportunity.UpdatedAt = now
	opportunity.UpdatedBy = command.ActorID
	auditID := s.newID()
	eventID := s.newID()
	correlationID := s.newID()
	accepted := TransitionMutation{
		Opportunity: opportunity, PreviousStage: previous,
		ExpectedClientVersion: command.ExpectedClientVersion,
		Pipeline:              pipeline, CurrentStage: current, DestinationStage: next,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "opportunity.stage.changed", SubjectType: "opportunity",
			SubjectID: string(opportunity.ID), SubjectVersion: opportunity.Version,
			Source: command.Source, Reason: reason, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "opportunity.stage.changed", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "opportunity", SubjectID: string(opportunity.ID),
			SubjectVersion: opportunity.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.TransitionAtomic(ctx, accepted); err != nil {
		return Opportunity{}, err
	}
	return opportunity, nil
}

func (s *Service) findOpportunityPipeline(
	ctx context.Context,
	mspID string,
	pipelineID string,
) (Pipeline, error) {
	pipelines, err := s.repository.ListPipelines(ctx, mspID)
	if err != nil {
		return Pipeline{}, err
	}
	for _, pipeline := range pipelines {
		if pipeline.ID == pipelineID && pipeline.MSPID == mspID && pipeline.Version > 0 {
			return pipeline, nil
		}
	}
	return Pipeline{}, scope.ErrNotFound
}

type CreateProspectCommand struct {
	Principal     authorization.Principal
	ProspectID    string
	DisplayID     string
	Name          string
	Email         string
	Phone         string
	ActorID       string
	Source        string
	CorrelationID string
}

func (s *Service) ListProspects(
	ctx context.Context,
	principal authorization.Principal,
	limit int,
) ([]Prospect, error) {
	if principal.Scope.MSPID == "" || principal.Scope.ClientID != "" || limit < 0 {
		return nil, ErrInvalidSalesRecord
	}
	if limit == 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	return s.listProspectsAt(
		ctx, principal, scope.Target{MSPID: principal.Scope.MSPID}, limit, 500,
	)
}

// ResolveProspectReference performs a bounded, exact MSP-wide identity lookup.
// Anything other than one result remains indistinguishable from not found at
// the AI tool boundary.
func (s *Service) ResolveProspectReference(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	reference string,
) ([]Prospect, error) {
	if target.MSPID == "" || target.ClientID != "" ||
		normalizeProspectReference(reference) == "" || s.repository == nil {
		return nil, ErrInvalidSalesRecord
	}
	if err := authorization.Authorize(principal, "prospect.create", target); err != nil {
		return nil, err
	}
	return s.repository.FindProspectsByReference(ctx, target.MSPID, reference, 2)
}

func ProspectReferenceMatches(reference, name, displayID string) bool {
	normalized := normalizeProspectReference(reference)
	return normalized != "" &&
		(normalized == normalizeProspectReference(name) ||
			normalized == normalizeProspectReference(displayID))
}

func normalizeProspectReference(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

// ListProspectsAt supports explicit MSP-wide callers such as the AI workspace.
// A Prospect never belongs to a Client, so Client-scoped principals are denied
// by normal scope authorization rather than silently widening their view.
func (s *Service) ListProspectsAt(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	limit int,
) ([]Prospect, error) {
	if limit < 1 || target.MSPID == "" || target.ClientID != "" {
		return nil, ErrInvalidSalesRecord
	}
	return s.listProspectsAt(ctx, principal, target, limit, 50)
}

func (s *Service) listProspectsAt(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	limit int,
	maximum int,
) ([]Prospect, error) {
	if limit == 0 {
		limit = maximum
	}
	if limit > maximum {
		limit = maximum
	}
	if err := authorization.Authorize(principal, "opportunity.read", target); err != nil {
		return nil, err
	}
	return s.repository.ListProspects(ctx, target.MSPID, limit)
}

func (s *Service) CreateProspect(ctx context.Context, command CreateProspectCommand) (Prospect, error) {
	if command.Principal.Scope.MSPID == "" || command.Principal.Scope.ClientID != "" ||
		strings.TrimSpace(command.DisplayID) == "" || strings.TrimSpace(command.Name) == "" ||
		command.ActorID == "" || command.Source == "" {
		return Prospect{}, ErrInvalidSalesRecord
	}
	target := scope.Target{MSPID: command.Principal.Scope.MSPID}
	if err := authorization.Authorize(command.Principal, "prospect.create", target); err != nil {
		return Prospect{}, err
	}
	now := s.now().UTC()
	entityID := strings.TrimSpace(command.ProspectID)
	if entityID == "" {
		entityID = s.newID()
	}
	auditID, eventID := s.newID(), s.newID()
	correlationID := strings.TrimSpace(command.CorrelationID)
	if correlationID == "" {
		correlationID = s.newID()
	}
	prospect := Prospect{
		ID: entityID, MSPID: target.MSPID, DisplayID: strings.TrimSpace(command.DisplayID),
		Name: strings.TrimSpace(command.Name), Email: strings.TrimSpace(command.Email),
		Phone: strings.TrimSpace(command.Phone), Version: 1, CreatedAt: now, CreatedBy: command.ActorID,
	}
	accepted := CreateProspectMutation{
		Prospect: prospect,
		Audit:    mutation.AuditRecord{ID: auditID, OccurredAt: now, MSPID: target.MSPID, ActorType: "technician", ActorID: command.ActorID, Action: "prospect.created", SubjectType: "prospect", SubjectID: entityID, SubjectVersion: 1, Source: command.Source, CorrelationID: correlationID},
		Event:    mutation.EventRecord{EventID: eventID, EventType: "prospect.created", SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID, ActorType: "technician", ActorID: command.ActorID, SubjectType: "prospect", SubjectID: entityID, SubjectVersion: 1, Source: command.Source, CorrelationID: correlationID},
	}
	if err := s.repository.CreateProspectAtomic(ctx, accepted); err != nil {
		return Prospect{}, err
	}
	return prospect, nil
}

type CreatePipelineCommand struct {
	Principal authorization.Principal
	Key       string
	Name      string
	Stages    []PipelineStage
	ActorID   string
	Source    string
}

func (s *Service) ListPipelines(
	ctx context.Context,
	principal authorization.Principal,
) ([]Pipeline, error) {
	if principal.Scope.MSPID == "" || principal.Scope.ClientID != "" {
		return nil, ErrInvalidSalesRecord
	}
	if err := authorization.Authorize(
		principal, "opportunity.read",
		scope.Target{MSPID: principal.Scope.MSPID},
	); err != nil {
		return nil, err
	}
	return s.repository.ListPipelines(ctx, principal.Scope.MSPID)
}

func (s *Service) CreatePipeline(ctx context.Context, command CreatePipelineCommand) (Pipeline, error) {
	if command.Principal.Scope.MSPID == "" || command.Principal.Scope.ClientID != "" ||
		strings.TrimSpace(command.Key) == "" || strings.TrimSpace(command.Name) == "" ||
		len(command.Stages) == 0 || command.ActorID == "" || command.Source == "" {
		return Pipeline{}, ErrInvalidSalesRecord
	}
	target := scope.Target{MSPID: command.Principal.Scope.MSPID}
	if err := authorization.Authorize(command.Principal, "pipeline.create", target); err != nil {
		return Pipeline{}, err
	}
	pipelineID := s.newID()
	positions := make(map[int]struct{}, len(command.Stages))
	keys := make(map[string]struct{}, len(command.Stages))
	hasClosedWon := false
	for index := range command.Stages {
		stage := &command.Stages[index]
		stage.Key = strings.TrimSpace(stage.Key)
		stage.Name = strings.TrimSpace(stage.Name)
		if stage.ID == "" || stage.Key == "" || stage.Name == "" ||
			stage.Position < 1 || stage.Probability > 100 ||
			!validForecastCategory(stage.Category) {
			return Pipeline{}, ErrInvalidSalesRecord
		}
		if _, exists := positions[stage.Position]; exists {
			return Pipeline{}, ErrInvalidSalesRecord
		}
		if _, exists := keys[stage.Key]; exists {
			return Pipeline{}, ErrInvalidSalesRecord
		}
		positions[stage.Position] = struct{}{}
		keys[stage.Key] = struct{}{}
		hasClosedWon = hasClosedWon || stage.Category == ClosedWon
		stage.PipelineID = pipelineID
		stage.Version = 1
	}
	if !hasClosedWon {
		return Pipeline{}, ErrInvalidSalesRecord
	}
	now := s.now().UTC()
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	pipeline := Pipeline{ID: pipelineID, MSPID: target.MSPID, Key: strings.TrimSpace(command.Key), Name: strings.TrimSpace(command.Name), Stages: command.Stages, Version: 1, CreatedAt: now, CreatedBy: command.ActorID}
	accepted := CreatePipelineMutation{
		Pipeline: pipeline,
		Audit:    mutation.AuditRecord{ID: auditID, OccurredAt: now, MSPID: target.MSPID, ActorType: "technician", ActorID: command.ActorID, Action: "pipeline.created", SubjectType: "pipeline", SubjectID: pipelineID, SubjectVersion: 1, Source: command.Source, CorrelationID: correlationID},
		Event:    mutation.EventRecord{EventID: eventID, EventType: "pipeline.created", SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID, ActorType: "technician", ActorID: command.ActorID, SubjectType: "pipeline", SubjectID: pipelineID, SubjectVersion: 1, Source: command.Source, CorrelationID: correlationID},
	}
	if err := s.repository.CreatePipelineAtomic(ctx, accepted); err != nil {
		return Pipeline{}, err
	}
	return pipeline, nil
}

func validForecastCategory(category ForecastCategory) bool {
	switch category {
	case PipelineCategory, Weighted, Committed, ClosedWon, ClosedLost:
		return true
	default:
		return false
	}
}

func containsStage(stages []PipelineStageID, wanted PipelineStageID) bool {
	for _, stage := range stages {
		if stage == wanted {
			return true
		}
	}
	return false
}

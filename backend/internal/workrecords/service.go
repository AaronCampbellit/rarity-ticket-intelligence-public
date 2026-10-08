// Package workrecords owns the shared Incident, Request, Change, and Problem
// application boundary.
package workrecords

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/routing"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
)

var (
	ErrInvalid           = errors.New("invalid work record")
	ErrDisplayIDConflict = errors.New("work record display ID conflict")
)

type Type string

const (
	Incident Type = "incident"
	Request  Type = "request"
	Change   Type = "change"
	Problem  Type = "problem"
)

type Actor struct {
	Type   string
	ID     string
	Source string
}

type Record struct {
	object.Envelope
	Type                               Type
	Title                              string
	Description                        string
	Status                             string
	Priority                           string
	ServiceID                          string
	ContractID                         string
	QueueID                            string
	PrimaryOwnerID                     string
	DeletedAt                          *time.Time
	DeletedBy                          string
	MergedIntoID                       string
	ScheduledStartsAt, ScheduledEndsAt *time.Time
	ScheduleTimezone                   string
	SchedulingMode                     calendar.SchedulingMode
	PlannedEffortMinutes               int64
	DueOn, FollowUpOn                  *time.Time
	ScheduleRecurrence                 *calendar.RecurrenceRule
}

type ScheduleMutation struct {
	RecordID, MSPID, ClientID                    string
	ExpectedVersion                              int64
	Interval                                     calendar.TypedInterval
	Mode                                         calendar.SchedulingMode
	PlannedMinutes                               int64
	Recurrence                                   *calendar.RecurrenceRule
	OccurrenceKey, OccurrenceScope, ProjectionID string
}

func ValidateScheduleMutation(principal authorization.Principal, accepted ScheduleMutation) error {
	if accepted.RecordID == "" || accepted.MSPID == "" || accepted.ClientID == "" || accepted.ExpectedVersion < 1 || accepted.Mode == calendar.Informational || accepted.PlannedMinutes <= 0 {
		return ErrInvalid
	}
	if err := authorization.Authorize(principal, "work_record.edit", scope.Target{MSPID: accepted.MSPID, ClientID: accepted.ClientID}); err != nil {
		return err
	}
	if err := accepted.Interval.Validate(false); err != nil || accepted.Interval.Empty() {
		return ErrInvalid
	}
	if accepted.Recurrence != nil && accepted.Recurrence.Validate() != nil {
		return ErrInvalid
	}
	return nil
}

type CreateCommand struct {
	RecordID                string
	Principal               authorization.Principal
	Target                  scope.Target
	Actor                   Actor
	DisplayID               string
	Type                    Type
	Title                   string
	Description             string
	Status                  string
	Priority                string
	ServiceID               string
	ContractID              string
	ExpectedClientVersion   int64
	ExpectedServiceVersion  int64
	ExpectedContractVersion int64
	ExpectedSelection       *CreateSelectionFence
	TagIDs                  []string
	ClassificationPolicy    tagging.CreationPolicy
}

type CreateMutation struct {
	Record                  Record
	Routing                 RoutingSelection
	Workflow                workflow.Selection
	SLA                     AppliedSLA
	ExpectedClientVersion   int64
	ExpectedServiceVersion  int64
	ExpectedContractVersion int64
	Audit                   mutation.AuditRecord
	Event                   mutation.EventRecord
	InitialTags             tagging.InitialAssignmentSet
}

type AppliedSLA struct {
	ID                  string
	PolicyID            string
	PolicyVersion       int64
	CalendarID          string
	CalendarVersion     int64
	CalendarDefinition  sla.CalendarDefinition
	ResponseWarningAt   time.Time
	ResponseDueAt       time.Time
	ResolutionWarningAt time.Time
	ResolutionDueAt     time.Time
	RespondedAt         *time.Time
	ResolvedAt          *time.Time
	PausedAt            *time.Time
	PausedSeconds       int64
	PauseStates         []string
	ResponseState       sla.State
	ResolutionState     sla.State
	Version             int64
	SelectionTrace      []sla.PolicyTraceEntry
}

type RoutingSelection struct {
	RuleSetID      string
	RuleSetVersion int64
	DecidedAt      time.Time
	Decision       routing.Decision
}

type CreateSelectionFence struct {
	RuleSetID, QueueID, WorkflowID, SLAPolicyID, CalendarID            string
	RuleSetVersion, WorkflowVersion, SLAPolicyVersion, CalendarVersion int64
}

type CreatePreflight struct {
	Target     scope.Target
	Routing    RoutingSelection
	Workflow   workflow.Selection
	SLA        AppliedSLA
	ServiceID  string
	ContractID string
	Fence      CreateSelectionFence
}

type Repository interface {
	EnsureDisplayIDAvailable(context.Context, scope.Target, string) error
	ValidateReferences(context.Context, scope.Target, ContextReferences, time.Time) error
	CreateAtomic(context.Context, CreateMutation) error
}

type ContextReferences struct {
	ServiceID  string
	ContractID string
}

type WorkflowSource interface {
	ListPublished(context.Context, scope.Target) ([]workflow.Published, error)
}

type RoutingSource interface {
	LoadCurrent(context.Context, string) (routing.RuleSet, error)
}

type PolicySource interface {
	ListPolicies(context.Context, scope.Target) ([]sla.PublishedPolicy, error)
}

type Service struct {
	repository     Repository
	routingSource  RoutingSource
	workflowSource WorkflowSource
	policySource   PolicySource
	now            func() time.Time
	newID          func() string
	creation       *tagging.CreationPreparer
}

func NewService(
	repository Repository,
	routingSource RoutingSource,
	workflowSource WorkflowSource,
	policySource PolicySource,
	now func() time.Time,
	newID func() string,
	creation ...*tagging.CreationPreparer,
) *Service {
	service := &Service{
		repository: repository, routingSource: routingSource,
		workflowSource: workflowSource, policySource: policySource,
		now: now, newID: newID,
	}
	if len(creation) > 0 {
		service.creation = creation[0]
	}
	return service
}

func (s *Service) Create(ctx context.Context, command CreateCommand) (Record, error) {
	return s.create(ctx, command, tagging.SourceHuman)
}

// CreateDattoIncident is the narrow trusted entry point for Datto alert
// materialization. It fixes the trusted source and fallback policy; callers of
// the ordinary Create API cannot choose either value.
func (s *Service) CreateDattoIncident(ctx context.Context, command CreateCommand) (Record, error) {
	command.ClassificationPolicy = tagging.CreationAllowFallback
	command.Actor.Type = "integration"
	command.Actor.Source = "datto"
	return s.create(ctx, command, tagging.SourceIntegration)
}

func (s *Service) create(ctx context.Context, command CreateCommand, tagSource tagging.Source) (Record, error) {
	if s == nil || s.repository == nil || s.routingSource == nil ||
		s.workflowSource == nil || s.policySource == nil || s.now == nil ||
		s.newID == nil {
		return Record{}, ErrInvalid
	}
	target, err := authorizeCreate(command)
	if err != nil {
		return Record{}, err
	}
	initial, err := s.initialTags(ctx, target, command, tagSource)
	if err != nil {
		return Record{}, err
	}
	now := s.now().UTC()
	preflight, err := s.prepareCreate(ctx, command, now)
	if err != nil {
		return Record{}, err
	}
	if command.ExpectedSelection != nil && *command.ExpectedSelection != preflight.Fence {
		return Record{}, object.ErrVersionConflict
	}
	recordID := strings.TrimSpace(command.RecordID)
	if recordID == "" {
		recordID = s.newID()
	}
	slaID := s.newID()
	auditID := s.newID()
	eventID := s.newID()
	correlationID := s.newID()
	record := Record{
		Envelope: object.Envelope{
			ID: recordID, ObjectType: "work_record", MSPID: target.MSPID,
			ClientID: target.ClientID, DisplayID: strings.TrimSpace(command.DisplayID),
			LifecycleState: "active", Version: 1,
			CreatedAt: now, CreatedBy: command.Actor.ID,
			UpdatedAt: now, UpdatedBy: command.Actor.ID,
		},
		Type: command.Type, Title: strings.TrimSpace(command.Title),
		Description: strings.TrimSpace(command.Description),
		Status:      strings.TrimSpace(command.Status), Priority: strings.TrimSpace(command.Priority),
		ServiceID: preflight.ServiceID, ContractID: preflight.ContractID,
		QueueID: preflight.Routing.Decision.QueueID,
	}
	appliedSLA := preflight.SLA
	appliedSLA.ID = slaID
	accepted := CreateMutation{
		Record: record, Routing: preflight.Routing, Workflow: preflight.Workflow, SLA: appliedSLA,
		ExpectedClientVersion:   command.ExpectedClientVersion,
		ExpectedServiceVersion:  command.ExpectedServiceVersion,
		ExpectedContractVersion: command.ExpectedContractVersion,
		InitialTags:             initial.WithProvenance(tagging.InitialAssignmentProvenance{ActorType: command.Actor.Type, ActorID: command.Actor.ID, OccurredAt: now, CorrelationID: correlationID}),
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: record.MSPID, ClientID: record.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			Action: "work_record.created", SubjectType: record.ObjectType,
			SubjectID: record.ID, SubjectVersion: record.Version,
			Source: command.Actor.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "work_record.created", SchemaVersion: 1,
			OccurredAt: now, MSPID: record.MSPID, ClientID: record.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			SubjectType: record.ObjectType, SubjectID: record.ID,
			SubjectVersion: record.Version, Source: command.Actor.Source,
			CorrelationID: correlationID,
			Data: map[string]any{
				"type": string(record.Type), "title": record.Title,
				"status": record.Status, "priority": record.Priority,
				"queue_id": record.QueueID, "service_id": record.ServiceID,
				"contract_id": record.ContractID,
			},
		},
	}
	if err := s.repository.CreateAtomic(ctx, accepted); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *Service) initialTags(ctx context.Context, target scope.Target, command CreateCommand, source ...tagging.Source) (tagging.InitialAssignmentSet, error) {
	if s.creation == nil && command.ClassificationPolicy == "" && len(command.TagIDs) == 0 {
		return tagging.InitialAssignmentSet{}, nil
	}
	tagSource := tagging.SourceHuman
	if len(source) == 1 {
		tagSource = source[0]
	}
	return s.creation.Prepare(ctx, tagging.PrepareCreationCommand{MSPID: target.MSPID, ClientID: target.ClientID, ObjectType: tagging.ObjectWorkRecord, TagIDs: command.TagIDs, Source: tagSource, Policy: command.ClassificationPolicy})
}

func (s *Service) PreflightCreate(ctx context.Context, command CreateCommand) (CreatePreflight, error) {
	if _, err := authorizeCreate(command); err != nil {
		return CreatePreflight{}, err
	}
	return s.prepareCreate(ctx, command, s.now().UTC())
}

func authorizeCreate(command CreateCommand) (scope.Target, error) {
	target, err := createTarget(command)
	if err != nil {
		return scope.Target{}, err
	}
	if err := authorization.Authorize(command.Principal, "work_record.create", target); err != nil {
		return scope.Target{}, err
	}
	return target, nil
}

func createTarget(command CreateCommand) (scope.Target, error) {
	if !supportedType(command.Type) ||
		command.ExpectedClientVersion < 0 || command.ExpectedServiceVersion < 0 ||
		command.ExpectedContractVersion < 0 ||
		command.Actor.Type == "" ||
		command.Actor.ID == "" ||
		command.Actor.Source == "" ||
		strings.TrimSpace(command.DisplayID) == "" ||
		strings.TrimSpace(command.Title) == "" ||
		strings.TrimSpace(command.Status) == "" ||
		strings.TrimSpace(command.Priority) == "" {
		return scope.Target{}, ErrInvalid
	}
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		}
	}
	if target.ClientID == "" {
		return scope.Target{}, ErrInvalid
	}
	return target, nil
}

func (s *Service) prepareCreate(
	ctx context.Context,
	command CreateCommand,
	evaluatedAt time.Time,
) (CreatePreflight, error) {
	target, err := createTarget(command)
	if err != nil {
		return CreatePreflight{}, err
	}
	now := evaluatedAt.UTC()
	if err := s.repository.EnsureDisplayIDAvailable(
		ctx, target, strings.TrimSpace(command.DisplayID),
	); err != nil {
		return CreatePreflight{}, err
	}
	if err := s.repository.ValidateReferences(ctx, target, ContextReferences{
		ServiceID:  strings.TrimSpace(command.ServiceID),
		ContractID: strings.TrimSpace(command.ContractID),
	}, now); err != nil {
		return CreatePreflight{}, err
	}
	ruleSet, err := s.routingSource.LoadCurrent(ctx, target.MSPID)
	if err != nil {
		return CreatePreflight{}, err
	}
	if strings.TrimSpace(ruleSet.ID) == "" || ruleSet.Version < 1 {
		return CreatePreflight{}, ErrInvalid
	}
	routingEngine, err := routing.NewEngine(ruleSet.Rules)
	if err != nil {
		return CreatePreflight{}, err
	}
	route, err := routingEngine.Route(routing.Input{
		ClientID: target.ClientID, RecordType: string(command.Type),
		Priority: strings.TrimSpace(command.Priority),
	})
	if err != nil {
		return CreatePreflight{}, err
	}
	routingSelection := RoutingSelection{
		RuleSetID: ruleSet.ID, RuleSetVersion: ruleSet.Version,
		DecidedAt: now, Decision: route,
	}
	published, err := s.workflowSource.ListPublished(ctx, target)
	if err != nil {
		return CreatePreflight{}, err
	}
	engine, err := workflow.NewEngine(published)
	if err != nil {
		return CreatePreflight{}, err
	}
	selection, err := engine.Select(workflow.Input{
		ClientID: target.ClientID, RecordType: string(command.Type),
		Priority: strings.TrimSpace(command.Priority), QueueID: route.QueueID,
		Source:      command.Actor.Source,
		EvaluatedAt: now,
	})
	if err != nil {
		return CreatePreflight{}, err
	}
	var definition *workflow.Definition
	for index := range published {
		if published[index].ID == selection.WorkflowID &&
			published[index].Version == selection.Version {
			definition = &published[index].Definition
			break
		}
	}
	if definition == nil || definition.Validate() != nil ||
		!definition.HasState(strings.TrimSpace(command.Status)) {
		return CreatePreflight{}, ErrInvalid
	}
	policies, err := s.policySource.ListPolicies(ctx, target)
	if err != nil {
		return CreatePreflight{}, err
	}
	policyEngine, err := sla.NewPolicyEngine(policies)
	if err != nil {
		return CreatePreflight{}, err
	}
	policySelection, err := policyEngine.Select(sla.PolicyInput{
		ClientID: target.ClientID, RecordType: string(command.Type),
		Priority: strings.TrimSpace(command.Priority), QueueID: route.QueueID,
		ServiceID:  strings.TrimSpace(command.ServiceID),
		ContractID: strings.TrimSpace(command.ContractID),
	})
	if err != nil {
		return CreatePreflight{}, err
	}
	policy := policySelection.Policy
	calendar, err := policy.Calendar.Definition.Calendar()
	if err != nil {
		return CreatePreflight{}, err
	}
	responseDuration := time.Duration(policy.ResponseTargetSeconds) * time.Second
	resolutionDuration := time.Duration(policy.ResolutionTargetSeconds) * time.Second
	responseDueAt, err := sla.AddBusinessTime(calendar, now, responseDuration)
	if err != nil {
		return CreatePreflight{}, err
	}
	resolutionDueAt, err := sla.AddBusinessTime(calendar, now, resolutionDuration)
	if err != nil {
		return CreatePreflight{}, err
	}
	responseWarningAt, err := sla.AddBusinessTime(
		calendar, now, responseDuration*time.Duration(policy.WarningPercent)/100,
	)
	if err != nil {
		return CreatePreflight{}, err
	}
	resolutionWarningAt, err := sla.AddBusinessTime(
		calendar, now, resolutionDuration*time.Duration(policy.WarningPercent)/100,
	)
	if err != nil {
		return CreatePreflight{}, err
	}
	if strings.TrimSpace(policy.ID) == "" || policy.Version < 1 ||
		strings.TrimSpace(policy.Calendar.ID) == "" || policy.Calendar.Version < 1 {
		return CreatePreflight{}, ErrInvalid
	}
	responseState, resolutionState := sla.Running, sla.Running
	var pausedAt *time.Time
	if contains(policy.PauseStates, strings.TrimSpace(command.Status)) {
		responseState, resolutionState = sla.Paused, sla.Paused
		pausedAt = &now
	}
	appliedSLA := AppliedSLA{
		PolicyID: policy.ID, PolicyVersion: policy.Version,
		CalendarID: policy.Calendar.ID, CalendarVersion: policy.Calendar.Version,
		CalendarDefinition: policy.Calendar.Definition,
		ResponseWarningAt:  responseWarningAt, ResponseDueAt: responseDueAt,
		ResolutionWarningAt: resolutionWarningAt, ResolutionDueAt: resolutionDueAt,
		PausedAt: pausedAt, PauseStates: append([]string(nil), policy.PauseStates...),
		ResponseState: responseState, ResolutionState: resolutionState,
		Version: 1, SelectionTrace: policySelection.Trace,
	}
	preflight := CreatePreflight{
		Target: target, Routing: routingSelection, Workflow: selection, SLA: appliedSLA,
		ServiceID: strings.TrimSpace(command.ServiceID), ContractID: strings.TrimSpace(command.ContractID),
	}
	preflight.Fence = CreateSelectionFence{
		RuleSetID: routingSelection.RuleSetID, RuleSetVersion: routingSelection.RuleSetVersion,
		QueueID:    routingSelection.Decision.QueueID,
		WorkflowID: selection.WorkflowID, WorkflowVersion: selection.Version,
		SLAPolicyID: appliedSLA.PolicyID, SLAPolicyVersion: appliedSLA.PolicyVersion,
		CalendarID: appliedSLA.CalendarID, CalendarVersion: appliedSLA.CalendarVersion,
	}
	return preflight, nil
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func supportedType(recordType Type) bool {
	switch recordType {
	case Incident, Request, Change, Problem:
		return true
	default:
		return false
	}
}

package calendar

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrInvalidConflictPolicy          = errors.New("invalid calendar conflict policy")
	ErrConflictPolicyVersionConflict  = errors.New("calendar conflict policy version conflict")
	ErrInvalidCustomDateConfiguration = errors.New("invalid custom date configuration")
	ErrCustomDateFieldVersionConflict = errors.New("custom date field version conflict")
)

type ConflictPolicyScopeType string

const (
	ConflictScopeMSP        ConflictPolicyScopeType = "msp"
	ConflictScopeTeam       ConflictPolicyScopeType = "team"
	ConflictScopeTechnician ConflictPolicyScopeType = "technician"
)

type ConflictPolicyScope struct {
	Type         ConflictPolicyScopeType `json:"type"`
	TeamID       string                  `json:"team_id,omitempty"`
	TechnicianID string                  `json:"technician_id,omitempty"`
}

type ConflictPolicyRule struct {
	Kind     ConflictKind     `json:"kind"`
	Severity ConflictSeverity `json:"severity"`
}

type ReplaceConflictPoliciesCommand struct {
	Principal       authorization.Principal
	Scope           ConflictPolicyScope
	Rules           []ConflictPolicyRule
	ExpectedVersion int64
	EffectiveFrom   time.Time
	ActorID, Source string
}

type ConflictPolicyMutation struct {
	ID, MSPID     string
	Scope         ConflictPolicyScope
	Rules         []ConflictPolicyRule
	Version       int64
	EffectiveFrom time.Time
	Audit         mutation.AuditRecord
	Event         mutation.EventRecord
}

type FieldType string

const (
	FieldDate     FieldType = "date"
	FieldDateTime FieldType = "datetime"
)

type CustomDateField struct {
	ID                  string         `json:"id"`
	MSPID               string         `json:"-"`
	ObjectType          string         `json:"object_type"`
	FieldID             string         `json:"field_id"`
	Label               string         `json:"label"`
	Category            string         `json:"category"`
	FieldType           FieldType      `json:"field_type"`
	SchedulingMode      SchedulingMode `json:"scheduling_mode"`
	CapacityBearing     bool           `json:"capacity_bearing"`
	TimezoneSource      string         `json:"timezone_source,omitempty"`
	PlannedEffortSource string         `json:"planned_effort_source,omitempty"`
	Version             int64          `json:"version"`
}

func (field CustomDateField) Validate() error {
	field.ObjectType = strings.TrimSpace(field.ObjectType)
	field.FieldID = strings.TrimSpace(field.FieldID)
	field.Label = strings.TrimSpace(field.Label)
	field.Category = strings.TrimSpace(field.Category)
	field.TimezoneSource = strings.TrimSpace(field.TimezoneSource)
	field.PlannedEffortSource = strings.TrimSpace(field.PlannedEffortSource)
	if !internalid.ValidCanonical(field.ID) || !internalid.ValidCanonical(field.MSPID) || field.Version < 1 || !validCustomDateDefinition(field) {
		return ErrInvalidCustomDateConfiguration
	}
	return nil
}

type UpsertCustomDateCommand struct {
	Principal                                authorization.Principal
	ID, ObjectType, FieldID, Label, Category string
	FieldType                                FieldType
	SchedulingMode                           SchedulingMode
	CapacityBearing                          bool
	TimezoneSource, PlannedEffortSource      string
	ExpectedVersion                          int64
	ActorID, Source                          string
}

type CustomDateFieldMutation struct {
	Field           CustomDateField
	ExpectedVersion int64
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type ConfigurationRepository interface {
	CurrentConflictPolicyVersion(context.Context, string, ConflictPolicyScope) (int64, error)
	ReplaceConflictPoliciesAtomic(context.Context, ConflictPolicyMutation) error
	FindCustomDateField(context.Context, string, string, string) (CustomDateField, error)
	UpsertCustomDateFieldAtomic(context.Context, CustomDateFieldMutation) error
}

type ConfigurationService struct {
	repository ConfigurationRepository
	now        func() time.Time
	newID      func() string
}

func NewConfigurationService(repository ConfigurationRepository, now func() time.Time, newID func() string) *ConfigurationService {
	return &ConfigurationService{repository: repository, now: now, newID: newID}
}

func (s *ConfigurationService) ReplaceConflictPolicies(ctx context.Context, command ReplaceConflictPoliciesCommand) ([]ConflictPolicy, error) {
	actor := command.Principal.ID
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || !validConfigurationPrincipal(command.Principal) || command.ExpectedVersion < 0 || strings.TrimSpace(command.Source) == "" || len(command.Rules) == 0 || (command.ActorID != "" && command.ActorID != actor) || !validPolicyScope(command.Scope) {
		return nil, ErrInvalidConflictPolicy
	}
	if err := authorization.Authorize(command.Principal, "calendar.policy.manage", scope.Target{MSPID: command.Principal.Scope.MSPID}); err != nil {
		return nil, err
	}
	rules := append([]ConflictPolicyRule(nil), command.Rules...)
	seen := map[ConflictKind]struct{}{}
	for _, rule := range rules {
		if !knownConflictKind(rule.Kind) || !validConflictSeverity(rule.Severity) {
			return nil, ErrInvalidConflictPolicy
		}
		if _, exists := seen[rule.Kind]; exists {
			return nil, ErrInvalidConflictPolicy
		}
		seen[rule.Kind] = struct{}{}
	}
	current, err := s.repository.CurrentConflictPolicyVersion(ctx, command.Principal.Scope.MSPID, command.Scope)
	if err != nil {
		return nil, err
	}
	if current != command.ExpectedVersion {
		return nil, ErrConflictPolicyVersionConflict
	}
	now := s.now().UTC()
	effective := command.EffectiveFrom.UTC()
	if command.EffectiveFrom.IsZero() {
		effective = now
	}
	id, auditID, eventID, correlationID := s.newID(), s.newID(), s.newID(), s.newID()
	if !allCanonicalIDs(id, auditID, eventID, correlationID) {
		return nil, ErrInvalidConflictPolicy
	}
	version := current + 1
	accepted := ConflictPolicyMutation{ID: id, MSPID: command.Principal.Scope.MSPID, Scope: command.Scope, Rules: rules, Version: version, EffectiveFrom: effective,
		Audit: mutation.AuditRecord{ID: auditID, OccurredAt: now, MSPID: command.Principal.Scope.MSPID, ActorType: "technician", ActorID: actor, Action: "calendar.conflict_policies.replaced", SubjectType: "calendar_conflict_policy", SubjectID: id, SubjectVersion: version, Source: command.Source, CorrelationID: correlationID},
		Event: mutation.EventRecord{EventID: eventID, EventType: "calendar.conflict_policies.replaced", SchemaVersion: 1, OccurredAt: now, MSPID: command.Principal.Scope.MSPID, ActorType: "technician", ActorID: actor, SubjectType: "calendar_conflict_policy", SubjectID: id, SubjectVersion: version, Source: command.Source, CorrelationID: correlationID, Data: map[string]any{"scope_type": command.Scope.Type, "team_id": command.Scope.TeamID, "technician_id": command.Scope.TechnicianID, "rules": rules}},
	}
	if err := s.repository.ReplaceConflictPoliciesAtomic(ctx, accepted); err != nil {
		return nil, err
	}
	result := make([]ConflictPolicy, 0, len(rules))
	for _, rule := range rules {
		result = append(result, ConflictPolicy{ID: id, MSPID: accepted.MSPID, ScopeType: accepted.Scope.Type, TeamID: accepted.Scope.TeamID, TechnicianID: accepted.Scope.TechnicianID, Kind: rule.Kind, Severity: rule.Severity, Version: version})
	}
	return result, nil
}

func (s *ConfigurationService) UpsertCustomDateField(ctx context.Context, command UpsertCustomDateCommand) (CustomDateField, error) {
	actor := command.Principal.ID
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || !validConfigurationPrincipal(command.Principal) || command.ExpectedVersion < 0 || strings.TrimSpace(command.Source) == "" || (command.ActorID != "" && command.ActorID != actor) {
		return CustomDateField{}, ErrInvalidCustomDateConfiguration
	}
	if err := authorization.Authorize(command.Principal, "calendar.policy.manage", scope.Target{MSPID: command.Principal.Scope.MSPID}); err != nil {
		return CustomDateField{}, err
	}
	command.ObjectType = strings.TrimSpace(command.ObjectType)
	command.FieldID = strings.TrimSpace(command.FieldID)
	command.Label = strings.TrimSpace(command.Label)
	command.Category = strings.TrimSpace(command.Category)
	command.TimezoneSource = strings.TrimSpace(command.TimezoneSource)
	command.PlannedEffortSource = strings.TrimSpace(command.PlannedEffortSource)
	if !validCustomDateCommand(command) {
		return CustomDateField{}, ErrInvalidCustomDateConfiguration
	}
	current, err := s.repository.FindCustomDateField(ctx, command.Principal.Scope.MSPID, command.ObjectType, command.FieldID)
	if err != nil {
		return CustomDateField{}, err
	}
	if current.Version != command.ExpectedVersion {
		return CustomDateField{}, ErrCustomDateFieldVersionConflict
	}
	id := current.ID
	if id == "" {
		id = command.ID
	}
	if id == "" {
		id = s.newID()
	}
	if !internalid.ValidCanonical(id) {
		return CustomDateField{}, ErrInvalidCustomDateConfiguration
	}
	found := CustomDateField{ID: id, MSPID: command.Principal.Scope.MSPID, ObjectType: command.ObjectType, FieldID: command.FieldID, Label: command.Label, Category: command.Category, FieldType: command.FieldType, SchedulingMode: command.SchedulingMode, CapacityBearing: command.CapacityBearing, TimezoneSource: command.TimezoneSource, PlannedEffortSource: command.PlannedEffortSource, Version: current.Version + 1}
	now := s.now().UTC()
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	if !allCanonicalIDs(auditID, eventID, correlationID) {
		return CustomDateField{}, ErrInvalidCustomDateConfiguration
	}
	accepted := CustomDateFieldMutation{Field: found, ExpectedVersion: command.ExpectedVersion,
		Audit: mutation.AuditRecord{ID: auditID, OccurredAt: now, MSPID: found.MSPID, ActorType: "technician", ActorID: actor, Action: "calendar.custom_date_field.upserted", SubjectType: "calendar_custom_date_field", SubjectID: found.ID, SubjectVersion: found.Version, Source: command.Source, CorrelationID: correlationID},
		Event: mutation.EventRecord{EventID: eventID, EventType: "calendar.custom_date_field.upserted", SchemaVersion: 1, OccurredAt: now, MSPID: found.MSPID, ActorType: "technician", ActorID: actor, SubjectType: "calendar_custom_date_field", SubjectID: found.ID, SubjectVersion: found.Version, Source: command.Source, CorrelationID: correlationID, Data: map[string]any{"object_type": found.ObjectType, "field_id": found.FieldID, "configuration_version": found.Version}},
	}
	if err := s.repository.UpsertCustomDateFieldAtomic(ctx, accepted); err != nil {
		return CustomDateField{}, err
	}
	return found, nil
}

func validPolicyScope(value ConflictPolicyScope) bool {
	switch value.Type {
	case ConflictScopeMSP:
		return value.TeamID == "" && value.TechnicianID == ""
	case ConflictScopeTeam:
		return internalid.ValidCanonical(value.TeamID) && value.TechnicianID == ""
	case ConflictScopeTechnician:
		return value.TeamID == "" && internalid.ValidCanonical(value.TechnicianID)
	default:
		return false
	}
}

func validCustomDateCommand(command UpsertCustomDateCommand) bool {
	return validCustomDateDefinition(CustomDateField{
		ObjectType: command.ObjectType, FieldID: command.FieldID, Label: command.Label, Category: command.Category,
		FieldType: command.FieldType, SchedulingMode: command.SchedulingMode, CapacityBearing: command.CapacityBearing,
		TimezoneSource: command.TimezoneSource, PlannedEffortSource: command.PlannedEffortSource,
	})
}

func validCustomDateDefinition(field CustomDateField) bool {
	validObjects := map[string]bool{"work_record": true, "task": true, "project": true, "asset": true, "knowledge_article": true, "time_entry": true}
	if !validObjects[field.ObjectType] || !rolePattern.MatchString(field.FieldID) || field.Label == "" || field.Category == "" || !validSchedulingMode(field.SchedulingMode) {
		return false
	}
	if field.FieldType == FieldDate {
		return field.TimezoneSource == "" && !field.CapacityBearing && field.PlannedEffortSource == "" && (field.SchedulingMode == Informational || field.SchedulingMode == FixedBlock)
	}
	if field.FieldType != FieldDateTime || !validTimezoneSource(field.TimezoneSource) {
		return false
	}
	if field.CapacityBearing {
		return (field.ObjectType == "work_record" || field.ObjectType == "task") && field.SchedulingMode != Informational && field.PlannedEffortSource != ""
	}
	return field.PlannedEffortSource == ""
}

func validTimezoneSource(value string) bool {
	if value == "object" || value == "client" || value == "msp" {
		return true
	}
	return validTimezone(value)
}
func validConfigurationPrincipal(value authorization.Principal) bool {
	return internalid.ValidCanonical(value.ID) && internalid.ValidCanonical(value.Scope.MSPID) && value.Scope.ClientID == ""
}
func allCanonicalIDs(values ...string) bool {
	for _, value := range values {
		if !internalid.ValidCanonical(value) {
			return false
		}
	}
	return true
}

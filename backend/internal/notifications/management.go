package notifications

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidPolicy = errors.New("invalid notification policy")

type PolicyConditions struct {
	ClientID              string                `json:"client_id,omitempty"`
	ContractID            string                `json:"contract_id,omitempty"`
	WorkflowID            string                `json:"workflow_id,omitempty"`
	QueueID               string                `json:"queue_id,omitempty"`
	TeamID                string                `json:"team_id,omitempty"`
	DepartmentID          string                `json:"department_id,omitempty"`
	RecordType            string                `json:"record_type,omitempty"`
	Priority              string                `json:"priority,omitempty"`
	CalendarChangeClasses []CalendarChangeClass `json:"calendar_change_classes,omitempty"`
	CalendarUrgencies     []CalendarUrgency     `json:"calendar_urgencies,omitempty"`
}

type Destination struct {
	Channel               Channel `json:"channel"`
	RecipientRef          string  `json:"recipient_ref"`
	ContentClassification string  `json:"content_classification"`
}

type PublishedPolicy struct {
	ID                 string           `json:"id"`
	MSPID              string           `json:"msp_id"`
	ClientID           string           `json:"client_id,omitempty"`
	Key                string           `json:"key"`
	Name               string           `json:"name"`
	EventType          string           `json:"event_type"`
	Version            int64            `json:"version"`
	Conditions         PolicyConditions `json:"conditions,omitempty"`
	Destinations       []Destination    `json:"destinations"`
	QuietPeriodSeconds int              `json:"quiet_period_seconds"`
	CriticalBypass     bool             `json:"critical_bypass"`
	Enabled            bool             `json:"enabled"`
	Priority           int              `json:"priority"`
	StableOrder        int              `json:"stable_order"`
	PublishedAt        time.Time        `json:"published_at"`
	PublishedBy        string           `json:"published_by"`
}

type PublishPolicyCommand struct {
	Principal          authorization.Principal
	Target             scope.Target
	PolicyID           string
	ExpectedVersion    int64
	Key                string
	Name               string
	EventType          string
	Conditions         PolicyConditions
	Destinations       []Destination
	QuietPeriodSeconds int
	CriticalBypass     bool
	Enabled            bool
	Priority           int
	StableOrder        int
	ActorID            string
	Source             string
}

type PolicyPublishMutation struct {
	Policy          PublishedPolicy
	ExpectedVersion int64
	Created         bool
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type PolicyManagementRepository interface {
	ListPolicies(context.Context, scope.Target) ([]PublishedPolicy, error)
	ValidateDestinations(context.Context, scope.Target, []Destination) error
	PublishPolicyAtomic(context.Context, PolicyPublishMutation) error
}

type PolicyManagementService struct {
	repository PolicyManagementRepository
	now        func() time.Time
	newID      func() string
}

func NewPolicyManagementService(
	repository PolicyManagementRepository,
	now func() time.Time,
	newID func() string,
) *PolicyManagementService {
	return &PolicyManagementService{repository: repository, now: now, newID: newID}
}

func (s *PolicyManagementService) ListPolicies(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
) ([]PublishedPolicy, error) {
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
		}
	}
	if err := authorization.Authorize(principal, "notification.manage", target); err != nil {
		return nil, err
	}
	return s.repository.ListPolicies(ctx, target)
}

func (s *PolicyManagementService) PublishPolicy(
	ctx context.Context,
	command PublishPolicyCommand,
) (PublishedPolicy, error) {
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID:    command.Principal.Scope.MSPID,
			ClientID: command.Principal.Scope.ClientID,
		}
	}
	if target.MSPID == "" || command.ExpectedVersion < 0 ||
		strings.TrimSpace(command.Key) == "" || strings.TrimSpace(command.Name) == "" ||
		strings.TrimSpace(command.EventType) == "" || command.QuietPeriodSeconds < 0 ||
		command.StableOrder < 0 || command.ActorID == "" || command.Source == "" {
		return PublishedPolicy{}, ErrInvalidPolicy
	}
	if err := authorization.Authorize(command.Principal, "notification.manage", target); err != nil {
		return PublishedPolicy{}, err
	}
	eventType := strings.TrimSpace(command.EventType)
	conditions := normalizeConditions(command.Conditions)
	if conditions.ClientID != "" && conditions.ClientID != target.ClientID {
		return PublishedPolicy{}, scope.ErrNotFound
	}
	if eventType == CalendarNotificationEventType && !validCalendarConditions(conditions) {
		return PublishedPolicy{}, ErrInvalidPolicy
	}
	destinations, err := normalizeDestinations(command.Destinations)
	if err != nil {
		return PublishedPolicy{}, err
	}
	if eventType == MentionOccurred {
		for _, destination := range destinations {
			if destination.Channel != Email && destination.Channel != Teams {
				return PublishedPolicy{}, ErrInvalidPolicy
			}
		}
	}
	if eventType == CalendarNotificationEventType {
		for _, destination := range destinations {
			if (destination.Channel != InApp && destination.Channel != Email) || destination.RecipientRef != CalendarAssigneeRecipientRef {
				return PublishedPolicy{}, ErrInvalidPolicy
			}
		}
	}
	current, err := s.repository.ListPolicies(ctx, target)
	if err != nil {
		return PublishedPolicy{}, err
	}
	created := command.ExpectedVersion == 0
	policyID, version := command.PolicyID, int64(1)
	if created {
		if policyID != "" {
			return PublishedPolicy{}, ErrInvalidPolicy
		}
		policyID = s.newID()
	} else {
		found := false
		for _, policy := range current {
			if policy.ID != policyID {
				continue
			}
			if policy.ClientID != target.ClientID {
				return PublishedPolicy{}, scope.ErrNotFound
			}
			if err := object.RequireVersion(policy.Version, command.ExpectedVersion); err != nil {
				return PublishedPolicy{}, err
			}
			version, found = policy.Version+1, true
			break
		}
		if !found {
			return PublishedPolicy{}, scope.ErrNotFound
		}
	}
	published := PublishedPolicy{
		ID: policyID, MSPID: target.MSPID, ClientID: target.ClientID,
		Key: strings.TrimSpace(command.Key), Name: strings.TrimSpace(command.Name),
		EventType: eventType, Version: version,
		Conditions: conditions, Destinations: destinations,
		QuietPeriodSeconds: command.QuietPeriodSeconds,
		CriticalBypass:     command.CriticalBypass, Enabled: command.Enabled,
		Priority: command.Priority, StableOrder: command.StableOrder,
		PublishedAt: s.now().UTC(), PublishedBy: command.ActorID,
	}
	candidates := replacePolicy(current, published)
	if err := validatePolicySet(candidates); err != nil {
		return PublishedPolicy{}, err
	}
	if err := s.repository.ValidateDestinations(ctx, target, destinations); err != nil {
		return PublishedPolicy{}, err
	}
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := PolicyPublishMutation{
		Policy: published, ExpectedVersion: command.ExpectedVersion, Created: created,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: published.PublishedAt,
			MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "notification_policy.published", SubjectType: "notification_policy",
			SubjectID: policyID, SubjectVersion: version,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "notification_policy.published",
			SchemaVersion: 1, OccurredAt: published.PublishedAt,
			MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "notification_policy", SubjectID: policyID,
			SubjectVersion: version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.PublishPolicyAtomic(ctx, accepted); err != nil {
		return PublishedPolicy{}, err
	}
	return published, nil
}

func normalizeConditions(value PolicyConditions) PolicyConditions {
	value.ClientID = strings.TrimSpace(value.ClientID)
	value.ContractID = strings.TrimSpace(value.ContractID)
	value.WorkflowID = strings.TrimSpace(value.WorkflowID)
	value.QueueID = strings.TrimSpace(value.QueueID)
	value.TeamID = strings.TrimSpace(value.TeamID)
	value.DepartmentID = strings.TrimSpace(value.DepartmentID)
	value.RecordType = strings.TrimSpace(value.RecordType)
	value.Priority = strings.TrimSpace(value.Priority)
	value.CalendarChangeClasses = normalizeCalendarChangeClasses(value.CalendarChangeClasses)
	value.CalendarUrgencies = normalizeCalendarUrgencies(value.CalendarUrgencies)
	return value
}

func normalizeCalendarChangeClasses(values []CalendarChangeClass) []CalendarChangeClass {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[CalendarChangeClass]struct{}, len(values))
	result := make([]CalendarChangeClass, 0, len(values))
	for _, value := range values {
		value = CalendarChangeClass(strings.TrimSpace(string(value)))
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func normalizeCalendarUrgencies(values []CalendarUrgency) []CalendarUrgency {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[CalendarUrgency]struct{}, len(values))
	result := make([]CalendarUrgency, 0, len(values))
	for _, value := range values {
		value = CalendarUrgency(strings.TrimSpace(string(value)))
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func validCalendarConditions(value PolicyConditions) bool {
	for _, change := range value.CalendarChangeClasses {
		if change != CalendarSchedule && change != CalendarPTO && change != CalendarConflict && change != CalendarCancellation && change != CalendarReminder {
			return false
		}
	}
	for _, urgency := range value.CalendarUrgencies {
		if urgency != CalendarRoutine && urgency != CalendarImportant && urgency != CalendarUrgent {
			return false
		}
	}
	return true
}

func normalizeDestinations(values []Destination) ([]Destination, error) {
	if len(values) == 0 {
		return nil, ErrInvalidPolicy
	}
	result := make([]Destination, len(values))
	seen := map[string]struct{}{}
	for index, destination := range values {
		destination.RecipientRef = strings.TrimSpace(destination.RecipientRef)
		destination.ContentClassification = strings.TrimSpace(destination.ContentClassification)
		if destination.RecipientRef == "" || destination.ContentClassification == "" ||
			(destination.Channel != InApp && destination.Channel != Email &&
				destination.Channel != Teams && destination.Channel != Webhook) {
			return nil, ErrInvalidPolicy
		}
		key := string(destination.Channel) + "\x00" + destination.RecipientRef
		if _, duplicate := seen[key]; duplicate {
			return nil, ErrInvalidPolicy
		}
		seen[key] = struct{}{}
		result[index] = destination
	}
	return result, nil
}

func replacePolicy(current []PublishedPolicy, replacement PublishedPolicy) []PublishedPolicy {
	result := make([]PublishedPolicy, 0, len(current)+1)
	replaced := false
	for _, policy := range current {
		if policy.ID == replacement.ID {
			result = append(result, replacement)
			replaced = true
		} else {
			result = append(result, policy)
		}
	}
	if !replaced {
		result = append(result, replacement)
	}
	return result
}

func validatePolicySet(policies []PublishedPolicy) error {
	orders := map[[2]int]struct{}{}
	keys := map[string]struct{}{}
	for _, policy := range policies {
		if policy.ID == "" || policy.Version < 1 || policy.EventType == "" ||
			policy.QuietPeriodSeconds < 0 || policy.StableOrder < 0 {
			return ErrInvalidPolicy
		}
		if _, duplicate := keys[policy.Key]; duplicate {
			return ErrInvalidPolicy
		}
		keys[policy.Key] = struct{}{}
		if policy.Enabled {
			order := [2]int{policy.Priority, policy.StableOrder}
			if _, duplicate := orders[order]; duplicate {
				return ErrInvalidPolicy
			}
			orders[order] = struct{}{}
		}
		if _, err := normalizeDestinations(policy.Destinations); err != nil {
			return err
		}
	}
	sort.Slice(policies, func(i, j int) bool {
		if policies[i].Priority == policies[j].Priority {
			return policies[i].StableOrder < policies[j].StableOrder
		}
		return policies[i].Priority > policies[j].Priority
	})
	return nil
}

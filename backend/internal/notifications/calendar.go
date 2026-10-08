package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
)

var ErrInvalidCalendarPreference = errors.New("invalid calendar notification preference")
var ErrInvalidCalendarPlanningEvent = errors.New("invalid canonical calendar planning event")

type CalendarEventClass string
type CalendarChangeClass string
type CalendarUrgency string

const (
	CalendarNotificationEventType = "calendar.schedule_changed"
	CalendarAssigneeRecipientRef  = "calendar.assignee"

	CalendarSchedule     CalendarChangeClass = "schedule"
	CalendarPTO          CalendarChangeClass = "pto"
	CalendarConflict     CalendarChangeClass = "conflict"
	CalendarCancellation CalendarChangeClass = "cancellation"
	CalendarReminder     CalendarChangeClass = "reminder"

	CalendarRoutine   CalendarUrgency = "routine"
	CalendarImportant CalendarUrgency = "important"
	CalendarUrgent    CalendarUrgency = "urgent"

	CalendarScheduleChanged CalendarEventClass  = CalendarNotificationEventType
	CalendarAssigned        CalendarChangeClass = "assigned"
	CalendarRescheduled     CalendarChangeClass = "rescheduled"
	CalendarCancelled       CalendarChangeClass = "cancelled"
	CalendarCommitmentDue   CalendarChangeClass = "commitment_due"
	CalendarPTODecided      CalendarChangeClass = "pto_decided"
	CalendarCascadeApplied  CalendarChangeClass = "cascade_applied"
	CalendarNormal          CalendarUrgency     = CalendarRoutine
)

type CalendarPreferenceRule struct {
	EventClass  CalendarEventClass  `json:"event_class"`
	ChangeClass CalendarChangeClass `json:"change_class"`
	Urgency     CalendarUrgency     `json:"urgency"`
	Channel     Channel             `json:"channel"`
	Enabled     bool                `json:"enabled"`
}

type CalendarPreference struct {
	TechnicianID string                   `json:"technician_id"`
	Rules        []CalendarPreferenceRule `json:"rules"`
	Version      int64                    `json:"version"`
}

type CalendarSourceReference struct {
	Type           string `json:"type"`
	ID             string `json:"id"`
	ClientID       string `json:"client_id,omitempty"`
	EventRole      string `json:"event_role"`
	SourceRevision int64  `json:"source_revision"`
}

type CalendarDeliveryPayload struct {
	CorrelationID string                    `json:"correlation_id"`
	ChangeClass   CalendarChangeClass       `json:"change_class"`
	Urgency       CalendarUrgency           `json:"urgency"`
	Sources       []CalendarSourceReference `json:"source_refs"`
	ActionPath    string                    `json:"action_path"`
}

type canonicalCalendarPlanningData struct {
	RecipientID string                    `json:"recipient_id"`
	ChangeClass CalendarChangeClass       `json:"change_class"`
	Urgency     CalendarUrgency           `json:"urgency"`
	Sources     []CalendarSourceReference `json:"source_refs"`
	ActionPath  string                    `json:"action_path"`
}

func (event *PlanningEvent) DecodeCanonicalCalendarData(data []byte) error {
	if event == nil || event.Type != CalendarNotificationEventType {
		return ErrInvalidCalendarPlanningEvent
	}
	var decoded canonicalCalendarPlanningData
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCalendarPlanningEvent, err)
	}
	event.CalendarRecipientID = decoded.RecipientID
	event.CalendarChangeClass = decoded.ChangeClass
	event.CalendarUrgency = decoded.Urgency
	event.CalendarSources = append([]CalendarSourceReference(nil), decoded.Sources...)
	event.CalendarActionPath = decoded.ActionPath
	if !validCalendarPlanningEvent(*event) {
		return ErrInvalidCalendarPlanningEvent
	}
	return nil
}

func validCalendarPlanningEvent(event PlanningEvent) bool {
	if event.Type != CalendarNotificationEventType || strings.TrimSpace(event.MSPID) == "" ||
		strings.TrimSpace(event.CorrelationID) == "" || strings.TrimSpace(event.CalendarRecipientID) == "" ||
		!validCanonicalCalendarChangeClass(event.CalendarChangeClass) || !validCanonicalCalendarUrgency(event.CalendarUrgency) ||
		len(event.CalendarSources) == 0 || !strings.HasPrefix(event.CalendarActionPath, "/") {
		return false
	}
	for _, source := range event.CalendarSources {
		if strings.TrimSpace(source.Type) == "" || strings.TrimSpace(source.ID) == "" ||
			strings.TrimSpace(source.EventRole) == "" || source.SourceRevision < 1 {
			return false
		}
	}
	return true
}

func ValidateCalendarDeliveryPayload(mspID, recipientID string, payload *CalendarDeliveryPayload) error {
	if payload == nil || !validCalendarPlanningEvent(PlanningEvent{
		Type: CalendarNotificationEventType, MSPID: mspID, CorrelationID: payload.CorrelationID,
		CalendarRecipientID: recipientID, CalendarChangeClass: payload.ChangeClass, CalendarUrgency: payload.Urgency,
		CalendarSources: payload.Sources, CalendarActionPath: payload.ActionPath,
	}) {
		return ErrInvalidCalendarPlanningEvent
	}
	return nil
}

func ValidateCalendarPlanningDecision(decision PlannedDecision) ([]PlanningEvent, error) {
	calendarIntent := decision.Event.Type == CalendarNotificationEventType || hasCalendarPlanningFields(decision.Event) || len(decision.Events) > 0
	for _, delivery := range decision.Deliveries {
		calendarIntent = calendarIntent || delivery.CalendarPayload != nil
	}
	if !calendarIntent {
		return nil, nil
	}
	if decision.Event.Type != CalendarNotificationEventType || strings.TrimSpace(decision.Event.ID) == "" || !validCalendarPlanningEvent(decision.Event) {
		return nil, ErrInvalidCalendarPlanningEvent
	}
	events := append([]PlanningEvent(nil), decision.Events...)
	if len(events) == 0 {
		events = []PlanningEvent{decision.Event}
	}
	eventIDs := make(map[string]struct{}, len(events))
	var sources []CalendarSourceReference
	for _, event := range events {
		if strings.TrimSpace(event.ID) == "" || !validCalendarPlanningEvent(event) || !sameCalendarDigest(event, decision.Event) {
			return nil, ErrInvalidCalendarPlanningEvent
		}
		if _, duplicate := eventIDs[event.ID]; duplicate {
			return nil, ErrInvalidCalendarPlanningEvent
		}
		eventIDs[event.ID] = struct{}{}
		sources = mergeCalendarSources(sources, event.CalendarSources)
	}
	if _, found := eventIDs[decision.Event.ID]; !found || decision.Event.ClientID != commonCalendarSourceClient(sources) || !calendarSourcesEqual(decision.Event.CalendarSources, sources) {
		return nil, ErrInvalidCalendarPlanningEvent
	}
	for _, delivery := range decision.Deliveries {
		if _, found := eventIDs[delivery.EventID]; !found || delivery.MSPID != decision.Event.MSPID ||
			delivery.ClientID != decision.Event.ClientID || delivery.RecipientTechnicianID != decision.Event.CalendarRecipientID ||
			(delivery.Channel != InApp && delivery.Channel != Email) || delivery.RecipientRef != CalendarAssigneeRecipientRef ||
			delivery.CalendarPayload == nil {
			return nil, ErrInvalidCalendarPlanningEvent
		}
		payload := delivery.CalendarPayload
		if payload.CorrelationID != decision.Event.CorrelationID || payload.ChangeClass != decision.Event.CalendarChangeClass ||
			payload.Urgency != decision.Event.CalendarUrgency || payload.ActionPath != decision.Event.CalendarActionPath ||
			!calendarSourcesEqual(payload.Sources, sources) || ValidateCalendarDeliveryPayload(delivery.MSPID, delivery.RecipientTechnicianID, payload) != nil {
			return nil, ErrInvalidCalendarPlanningEvent
		}
	}
	return events, nil
}

func hasCalendarPlanningFields(event PlanningEvent) bool {
	return event.CalendarRecipientID != "" || event.CalendarChangeClass != "" ||
		event.CalendarUrgency != "" || len(event.CalendarSources) > 0 || event.CalendarActionPath != ""
}

func sameCalendarDigest(left, right PlanningEvent) bool {
	return left.Type == CalendarNotificationEventType && left.MSPID == right.MSPID &&
		left.CorrelationID == right.CorrelationID && left.CalendarRecipientID == right.CalendarRecipientID &&
		left.CalendarChangeClass == right.CalendarChangeClass && left.CalendarUrgency == right.CalendarUrgency &&
		left.CalendarActionPath == right.CalendarActionPath
}

func calendarSourcesEqual(left, right []CalendarSourceReference) bool {
	left, right = mergeCalendarSources(nil, left), mergeCalendarSources(nil, right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func validCanonicalCalendarChangeClass(value CalendarChangeClass) bool {
	return value == CalendarSchedule || value == CalendarPTO || value == CalendarConflict || value == CalendarCancellation || value == CalendarReminder
}

func validCanonicalCalendarUrgency(value CalendarUrgency) bool {
	return value == CalendarRoutine || value == CalendarImportant || value == CalendarUrgent
}

type ScheduleChange struct {
	AssigneeID  string
	ChangeClass CalendarChangeClass
	Urgency     CalendarUrgency
	ClientName  string
	SourceTitle string
	Source      calendar.SourceRef
}

type AppliedScheduleEvent struct {
	MSPID         string
	CorrelationID string
	Changes       []ScheduleChange
}

type CalendarDeliveryPlan struct {
	DeduplicationKey string              `json:"deduplication_key"`
	RecipientID      string              `json:"recipient_id"`
	EventClass       CalendarEventClass  `json:"event_class"`
	ChangeClass      CalendarChangeClass `json:"change_class"`
	Urgency          CalendarUrgency     `json:"urgency"`
	Channel          Channel             `json:"channel"`
	Body             string              `json:"body"`
}

type CalendarPermissionChecker interface {
	CanViewCalendarSource(context.Context, string, calendar.SourceRef) (bool, error)
}

type CalendarPreferenceReader interface {
	CalendarPreference(context.Context, string, string) (CalendarPreference, error)
}

type CalendarPlanner struct {
	permissions CalendarPermissionChecker
	preferences CalendarPreferenceReader
}

func NewCalendarPlanner(permissions CalendarPermissionChecker, preferences CalendarPreferenceReader) *CalendarPlanner {
	return &CalendarPlanner{permissions: permissions, preferences: preferences}
}

func (p *CalendarPlanner) Plan(ctx context.Context, event AppliedScheduleEvent) ([]CalendarDeliveryPlan, error) {
	if p == nil || p.permissions == nil || strings.TrimSpace(event.MSPID) == "" || strings.TrimSpace(event.CorrelationID) == "" {
		return nil, fmt.Errorf("invalid calendar notification event")
	}
	type groupKey struct {
		recipient string
		change    CalendarChangeClass
		urgency   CalendarUrgency
	}
	groups := map[groupKey][]string{}
	for _, change := range event.Changes {
		if strings.TrimSpace(change.AssigneeID) == "" || !validCanonicalCalendarChangeClass(change.ChangeClass) {
			continue
		}
		urgency := change.Urgency
		if urgency == "" {
			urgency = CalendarRoutine
		}
		if !validCanonicalCalendarUrgency(urgency) {
			continue
		}
		allowed, err := p.permissions.CanViewCalendarSource(ctx, change.AssigneeID, change.Source)
		if err != nil {
			return nil, err
		}
		detail := "a calendar item"
		if allowed {
			detail = strings.TrimSpace(change.SourceTitle)
			if detail == "" {
				detail = "a calendar item"
			}
			if client := strings.TrimSpace(change.ClientName); client != "" {
				detail = client + ": " + detail
			}
		}
		key := groupKey{recipient: change.AssigneeID, change: change.ChangeClass, urgency: urgency}
		groups[key] = append(groups[key], detail)
	}
	keys := make([]groupKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].recipient != keys[j].recipient {
			return keys[i].recipient < keys[j].recipient
		}
		if keys[i].change != keys[j].change {
			return keys[i].change < keys[j].change
		}
		return keys[i].urgency < keys[j].urgency
	})
	plans := make([]CalendarDeliveryPlan, 0, len(keys))
	for _, key := range keys {
		preference := CalendarPreference{}
		if p.preferences != nil {
			var err error
			preference, err = p.preferences.CalendarPreference(ctx, event.MSPID, key.recipient)
			if err != nil {
				return nil, err
			}
		}
		details := groups[key]
		body := fmt.Sprintf("%s: %s", strings.ReplaceAll(string(key.change), "_", " "), details[0])
		if len(details) > 1 {
			body = fmt.Sprintf("%d calendar changes: %s", len(details), strings.Join(details[:min(len(details), 10)], "; "))
		}
		for _, channel := range []Channel{InApp, Email} {
			if !preference.ChannelEnabled(key.change, key.urgency, channel) {
				continue
			}
			plans = append(plans, CalendarDeliveryPlan{
				DeduplicationKey: strings.Join([]string{event.CorrelationID, key.recipient, string(key.change), string(channel)}, ":"),
				RecipientID:      key.recipient, EventClass: CalendarScheduleChanged, ChangeClass: key.change,
				Urgency: key.urgency, Channel: channel, Body: body,
			})
		}
	}
	return plans, nil
}

func (p CalendarPreference) ChannelEnabled(change CalendarChangeClass, urgency CalendarUrgency, channel Channel) bool {
	for _, rule := range p.Rules {
		if rule.EventClass == CalendarScheduleChanged && rule.ChangeClass == change && rule.Urgency == urgency && rule.Channel == channel {
			return rule.Enabled
		}
	}
	return channel == InApp || channel == Email
}

type CalendarPreferenceRepository interface {
	GetCalendarPreference(context.Context, string, string) (CalendarPreference, error)
	ReplaceCalendarPreference(context.Context, CalendarPreferenceMutation) (CalendarPreference, error)
}

type CalendarPreferenceMutation struct {
	MSPID           string
	Preference      CalendarPreference
	ExpectedVersion int64
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type CalendarPreferenceService struct {
	repository CalendarPreferenceRepository
	now        func() time.Time
	newID      func() string
}

func NewCalendarPreferenceService(repository CalendarPreferenceRepository, now func() time.Time, newID func() string) *CalendarPreferenceService {
	return &CalendarPreferenceService{repository: repository, now: now, newID: newID}
}

type ReplaceCalendarPreferenceCommand struct {
	Principal       authorization.Principal
	Rules           []CalendarPreferenceRule
	ExpectedVersion int64
}

func (s *CalendarPreferenceService) Get(ctx context.Context, principal authorization.Principal) (CalendarPreference, error) {
	if s == nil || s.repository == nil || strings.TrimSpace(principal.ID) == "" || strings.TrimSpace(principal.Scope.MSPID) == "" {
		return CalendarPreference{}, ErrInvalidCalendarPreference
	}
	return s.repository.GetCalendarPreference(ctx, principal.Scope.MSPID, principal.ID)
}

func (s *CalendarPreferenceService) Replace(ctx context.Context, command ReplaceCalendarPreferenceCommand) (CalendarPreference, error) {
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || strings.TrimSpace(command.Principal.ID) == "" || strings.TrimSpace(command.Principal.Scope.MSPID) == "" || command.ExpectedVersion < 0 {
		return CalendarPreference{}, ErrInvalidCalendarPreference
	}
	seen := map[string]struct{}{}
	for _, rule := range command.Rules {
		if !validCalendarRule(rule) {
			return CalendarPreference{}, ErrInvalidCalendarPreference
		}
		key := strings.Join([]string{string(rule.EventClass), string(rule.ChangeClass), string(rule.Urgency), string(rule.Channel)}, "\x00")
		if _, duplicate := seen[key]; duplicate {
			return CalendarPreference{}, ErrInvalidCalendarPreference
		}
		seen[key] = struct{}{}
	}
	preference := CalendarPreference{TechnicianID: command.Principal.ID, Rules: append([]CalendarPreferenceRule(nil), command.Rules...), Version: command.ExpectedVersion + 1}
	now, auditID, eventID, correlationID := s.now().UTC(), s.newID(), s.newID(), s.newID()
	return s.repository.ReplaceCalendarPreference(ctx, CalendarPreferenceMutation{
		MSPID: command.Principal.Scope.MSPID, Preference: preference, ExpectedVersion: command.ExpectedVersion,
		Audit: mutation.AuditRecord{ID: auditID, OccurredAt: now, MSPID: command.Principal.Scope.MSPID, ActorType: "technician", ActorID: command.Principal.ID, Action: "calendar.notification_preferences.replaced", SubjectType: "calendar_notification_preferences", SubjectID: command.Principal.ID, SubjectVersion: preference.Version, Source: "http", CorrelationID: correlationID},
		Event: mutation.EventRecord{EventID: eventID, EventType: "calendar.notification_preferences.replaced", SchemaVersion: 1, OccurredAt: now, MSPID: command.Principal.Scope.MSPID, ActorType: "technician", ActorID: command.Principal.ID, SubjectType: "calendar_notification_preferences", SubjectID: command.Principal.ID, SubjectVersion: preference.Version, Source: "http", CorrelationID: correlationID},
	})
}

func validCalendarRule(rule CalendarPreferenceRule) bool {
	eventOK := rule.EventClass == CalendarScheduleChanged
	changeOK := rule.ChangeClass == CalendarSchedule || rule.ChangeClass == CalendarPTO || rule.ChangeClass == CalendarConflict || rule.ChangeClass == CalendarCancellation || rule.ChangeClass == CalendarReminder
	urgencyOK := rule.Urgency == CalendarRoutine || rule.Urgency == CalendarImportant || rule.Urgency == CalendarUrgent
	channelOK := rule.Channel == InApp || rule.Channel == Email
	return eventOK && changeOK && urgencyOK && channelOK
}

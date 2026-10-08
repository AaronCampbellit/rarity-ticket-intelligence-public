package notifications

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/observability"
)

type PlanningEvent struct {
	ID                    string
	Type                  string
	MSPID                 string
	ClientID              string
	WorkRecordID          string
	SubjectType           string
	SubjectID             string
	MentionOccurrenceID   string
	RecipientTechnicianID string
	OccurredAt            time.Time
	Critical              bool
	RecordType            string
	Priority              string
	QueueID               string
	TeamID                string
	DepartmentID          string
	WorkflowID            string
	ContractID            string
	CorrelationID         string
	CalendarRecipientID   string
	CalendarChangeClass   CalendarChangeClass
	CalendarUrgency       CalendarUrgency
	CalendarSources       []CalendarSourceReference
	CalendarActionPath    string
}

type DeliveryState string

const (
	Pending    DeliveryState = "pending"
	Suppressed DeliveryState = "suppressed"
)

type PlannedDelivery struct {
	ID                    string
	PolicyID              string
	PolicyVersion         int64
	EventID               string
	MSPID                 string
	ClientID              string
	WorkRecordID          string
	SubjectType           string
	SubjectID             string
	MentionOccurrenceID   string
	RecipientTechnicianID string
	Channel               Channel
	RecipientRef          string
	ContentClassification string
	State                 DeliveryState
	SuppressionReason     string
	PlannedAt             time.Time
	CalendarPayload       *CalendarDeliveryPayload
}

type PlannedDecision struct {
	Event      PlanningEvent
	Events     []PlanningEvent
	Deliveries []PlannedDelivery
	PlannedAt  time.Time
}

type PlanningResult struct {
	Events     int
	Pending    int
	Suppressed int
}

type PlannerRepository interface {
	ClaimEvents(context.Context, int, time.Time) ([]PlanningEvent, error)
	ListPoliciesForEvent(context.Context, PlanningEvent) ([]PublishedPolicy, error)
	RecentDeliveries(context.Context, PlanningEvent) ([]RecentDelivery, error)
	PlanAtomic(context.Context, PlannedDecision) (bool, error)
}

type MentionPreferenceReader interface {
	PreferenceForMention(context.Context, PlanningEvent) (RecipientPreference, error)
}

type Planner struct {
	repository PlannerRepository
	now        func() time.Time
	newID      func() string
	telemetry  *observability.MentionTelemetry
}

func NewPlanner(repository PlannerRepository, now func() time.Time, newID func() string) *Planner {
	return &Planner{repository: repository, now: now, newID: newID}
}

func (p *Planner) WithTelemetry(telemetry *observability.MentionTelemetry) *Planner {
	if p != nil {
		p.telemetry = telemetry
	}
	return p
}

func (p *Planner) RunOnce(ctx context.Context, limit int) (PlanningResult, error) {
	now := p.now().UTC()
	events, err := p.repository.ClaimEvents(ctx, limit, now)
	if err != nil {
		return PlanningResult{}, err
	}
	result := PlanningResult{}
	for _, group := range planningEventGroups(events) {
		event := group[0]
		decision := PlannedDecision{Event: event, PlannedAt: now}
		plannedEvents := 1
		pending, suppressed := 0, 0
		seen := map[string]struct{}{}
		if event.Type == CalendarNotificationEventType {
			var claimed []PlanningEvent
			event, claimed, err = mergeCalendarPlanningEvents(group)
			if err != nil {
				return result, err
			}
			decision.Event, decision.Events = event, claimed
			plannedEvents = len(claimed)
			policies, err := p.repository.ListPoliciesForEvent(ctx, event)
			if err != nil {
				return result, err
			}
			preference := CalendarPreference{TechnicianID: event.CalendarRecipientID}
			if reader, ok := p.repository.(CalendarPreferenceReader); ok {
				preference, err = reader.CalendarPreference(ctx, event.MSPID, event.CalendarRecipientID)
				if err != nil {
					return result, err
				}
			}
			for _, policy := range policies {
				if !publishedPolicyMatches(policy, event) {
					continue
				}
				for _, destination := range policy.Destinations {
					if destination.RecipientRef != CalendarAssigneeRecipientRef || (destination.Channel != InApp && destination.Channel != Email) || !preference.ChannelEnabled(event.CalendarChangeClass, event.CalendarUrgency, destination.Channel) {
						continue
					}
					key := strings.Join([]string{event.CorrelationID, event.CalendarRecipientID, string(event.CalendarChangeClass), string(destination.Channel), policy.ID, fmt.Sprint(policy.Version)}, "\x00")
					if _, duplicate := seen[key]; duplicate {
						continue
					}
					seen[key] = struct{}{}
					decision.Deliveries = append(decision.Deliveries, PlannedDelivery{
						ID: p.newID(), PolicyID: policy.ID, PolicyVersion: policy.Version,
						EventID: event.ID, MSPID: event.MSPID, ClientID: event.ClientID,
						RecipientTechnicianID: event.CalendarRecipientID,
						Channel:               destination.Channel, RecipientRef: destination.RecipientRef, ContentClassification: destination.ContentClassification,
						State: Pending, PlannedAt: now,
						CalendarPayload: &CalendarDeliveryPayload{
							CorrelationID: event.CorrelationID, ChangeClass: event.CalendarChangeClass, Urgency: event.CalendarUrgency,
							Sources: append([]CalendarSourceReference(nil), event.CalendarSources...), ActionPath: event.CalendarActionPath,
						},
					})
					pending++
				}
			}
		} else {
			for _, recipientEvent := range group {
				if recipientEvent.Type == MentionOccurred && recipientEvent.RecipientTechnicianID == "" {
					continue
				}
				policies, err := p.repository.ListPoliciesForEvent(ctx, event)
				if err != nil {
					return result, err
				}
				recent, err := p.repository.RecentDeliveries(ctx, event)
				if err != nil {
					return result, err
				}
				for _, policy := range policies {
					if !publishedPolicyMatches(policy, recipientEvent) {
						continue
					}
					if recipientEvent.Type == MentionOccurred {
						preference := DefaultMentionPreference(recipientEvent.RecipientTechnicianID)
						if reader, ok := p.repository.(MentionPreferenceReader); ok {
							preference, err = reader.PreferenceForMention(ctx, recipientEvent)
							if err != nil {
								return result, err
							}
						}
						for _, destination := range policy.Destinations {
							if destination.Channel != Email && destination.Channel != Teams {
								continue
							}
							if (destination.Channel == Email && !preference.EmailEnabled) || (destination.Channel == Teams && !preference.TeamsEnabled) {
								continue
							}
							key := recipientEvent.RecipientTechnicianID + "\x00" + string(destination.Channel)
							if _, duplicate := seen[key]; duplicate {
								continue
							}
							seen[key] = struct{}{}
							state, reason := Pending, ""
							if !(recipientEvent.Critical && policy.CriticalBypass) && InRecipientQuietHours(now, preference) {
								state, reason = Suppressed, "quiet_hours"
							}
							decision.Deliveries = append(decision.Deliveries, PlannedDelivery{
								ID: p.newID(), PolicyID: policy.ID, PolicyVersion: policy.Version,
								EventID: recipientEvent.ID, MSPID: recipientEvent.MSPID, ClientID: recipientEvent.ClientID,
								WorkRecordID: recipientEvent.WorkRecordID, SubjectType: recipientEvent.SubjectType, SubjectID: recipientEvent.SubjectID,
								MentionOccurrenceID: recipientEvent.MentionOccurrenceID, RecipientTechnicianID: recipientEvent.RecipientTechnicianID,
								Channel: destination.Channel, RecipientRef: destination.RecipientRef, ContentClassification: destination.ContentClassification,
								State: state, SuppressionReason: reason, PlannedAt: now,
							})
							if state == Suppressed {
								suppressed++
							} else {
								pending++
							}
						}
						continue
					}
					evaluated := Evaluate(Policy{
						ID: policy.ID, EventType: policy.EventType,
						Channels:       destinationChannels(policy.Destinations),
						QuietPeriod:    time.Duration(policy.QuietPeriodSeconds) * time.Second,
						CriticalBypass: policy.CriticalBypass,
					}, Event{
						Type: event.Type, WorkRecordID: event.WorkRecordID,
						OccurredAt: event.OccurredAt, Critical: event.Critical,
					}, recent)
					state, reason := Pending, ""
					if evaluated.Suppressed {
						state, reason = Suppressed, "quiet_period"
					}
					for _, destination := range policy.Destinations {
						decision.Deliveries = append(decision.Deliveries, PlannedDelivery{
							ID: p.newID(), PolicyID: policy.ID, PolicyVersion: policy.Version,
							EventID: event.ID, MSPID: event.MSPID, ClientID: event.ClientID,
							WorkRecordID: event.WorkRecordID, Channel: destination.Channel,
							RecipientRef:          destination.RecipientRef,
							ContentClassification: destination.ContentClassification,
							State:                 state, SuppressionReason: reason, PlannedAt: now,
						})
						if state == Suppressed {
							suppressed++
						} else {
							pending++
						}
					}
				}
			}
		}
		owned, err := p.repository.PlanAtomic(ctx, decision)
		if err != nil {
			return result, err
		}
		if !owned {
			continue
		}
		result.Pending += pending
		result.Suppressed += suppressed
		if event.Type == MentionOccurred {
			for _, delivery := range decision.Deliveries {
				outcome := "pending"
				if delivery.State == Suppressed {
					outcome = delivery.SuppressionReason
				}
				p.telemetry.Count(observability.MentionMetric{
					Name: "notification", ParentType: delivery.SubjectType,
					Channel: string(delivery.Channel), Outcome: outcome,
					MSPID: delivery.MSPID, ClientID: delivery.ClientID,
					ObjectID:     delivery.SubjectID,
					OccurrenceID: delivery.MentionOccurrenceID,
				})
			}
		}
		result.Events += plannedEvents
	}
	return result, nil
}

func planningEventGroups(events []PlanningEvent) [][]PlanningEvent {
	groups := make([][]PlanningEvent, 0, len(events))
	calendarGroups := map[string]int{}
	for start := 0; start < len(events); {
		if events[start].Type == CalendarNotificationEventType {
			key := strings.Join([]string{
				events[start].MSPID, events[start].CorrelationID, events[start].CalendarRecipientID,
				string(events[start].CalendarChangeClass), string(events[start].CalendarUrgency), events[start].CalendarActionPath,
			}, "\x00")
			if index, found := calendarGroups[key]; found {
				groups[index] = append(groups[index], events[start])
			} else {
				calendarGroups[key] = len(groups)
				groups = append(groups, []PlanningEvent{events[start]})
			}
			start++
			continue
		}
		end := start + 1
		for end < len(events) && events[end].ID == events[start].ID {
			end++
		}
		groups = append(groups, events[start:end])
		start = end
	}
	return groups
}

func mergeCalendarPlanningEvents(group []PlanningEvent) (PlanningEvent, []PlanningEvent, error) {
	if len(group) == 0 {
		return PlanningEvent{}, nil, ErrInvalidCalendarPlanningEvent
	}
	claimed := make([]PlanningEvent, 0, len(group))
	claimedByID := map[string]int{}
	for _, event := range group {
		if !validCalendarPlanningEvent(event) || strings.TrimSpace(event.ID) == "" {
			return PlanningEvent{}, nil, ErrInvalidCalendarPlanningEvent
		}
		if index, duplicate := claimedByID[event.ID]; duplicate {
			claimed[index].CalendarSources = mergeCalendarSources(claimed[index].CalendarSources, event.CalendarSources)
			continue
		}
		claimedByID[event.ID] = len(claimed)
		event.CalendarSources = mergeCalendarSources(nil, event.CalendarSources)
		claimed = append(claimed, event)
	}
	merged := claimed[0]
	merged.CalendarSources = nil
	for _, event := range claimed {
		merged.CalendarSources = mergeCalendarSources(merged.CalendarSources, event.CalendarSources)
	}
	merged.ClientID = commonCalendarSourceClient(merged.CalendarSources)
	return merged, claimed, nil
}

func mergeCalendarSources(left, right []CalendarSourceReference) []CalendarSourceReference {
	byKey := make(map[string]CalendarSourceReference, len(left)+len(right))
	for _, source := range append(append([]CalendarSourceReference(nil), left...), right...) {
		key := strings.Join([]string{source.Type, source.ID, source.ClientID, source.EventRole, fmt.Sprint(source.SourceRevision)}, "\x00")
		byKey[key] = source
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]CalendarSourceReference, 0, len(keys))
	for _, key := range keys {
		result = append(result, byKey[key])
	}
	return result
}

func commonCalendarSourceClient(sources []CalendarSourceReference) string {
	if len(sources) == 0 {
		return ""
	}
	clientID := sources[0].ClientID
	for _, source := range sources[1:] {
		if source.ClientID != clientID {
			return ""
		}
	}
	return clientID
}

func destinationChannels(destinations []Destination) []Channel {
	result := make([]Channel, len(destinations))
	for index, destination := range destinations {
		result[index] = destination.Channel
	}
	return result
}

func publishedPolicyMatches(policy PublishedPolicy, event PlanningEvent) bool {
	if !policy.Enabled || policy.EventType != event.Type {
		return false
	}
	conditions := policy.Conditions
	matches := (policy.ClientID == "" || policy.ClientID == event.ClientID) &&
		(conditions.ClientID == "" || conditions.ClientID == event.ClientID) &&
		(conditions.ContractID == "" || conditions.ContractID == event.ContractID) &&
		(conditions.WorkflowID == "" || conditions.WorkflowID == event.WorkflowID) &&
		(conditions.QueueID == "" || conditions.QueueID == event.QueueID) &&
		(conditions.TeamID == "" || conditions.TeamID == event.TeamID) &&
		(conditions.DepartmentID == "" || conditions.DepartmentID == event.DepartmentID) &&
		(conditions.RecordType == "" || conditions.RecordType == event.RecordType) &&
		(conditions.Priority == "" || conditions.Priority == event.Priority)
	if !matches || event.Type != CalendarNotificationEventType {
		return matches
	}
	return calendarChangeClassMatches(conditions.CalendarChangeClasses, event.CalendarChangeClass) &&
		calendarUrgencyMatches(conditions.CalendarUrgencies, event.CalendarUrgency)
}

func calendarChangeClassMatches(values []CalendarChangeClass, event CalendarChangeClass) bool {
	for _, value := range values {
		if value == event {
			return true
		}
	}
	return len(values) == 0
}

func calendarUrgencyMatches(values []CalendarUrgency, event CalendarUrgency) bool {
	for _, value := range values {
		if value == event {
			return true
		}
	}
	return len(values) == 0
}

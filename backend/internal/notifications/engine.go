// Package notifications evaluates delivery policy while keeping channel
// payloads within their approved content classification.
package notifications

import (
	"encoding/json"
	"errors"
	"time"
)

type Channel string

const (
	InApp   Channel = "in_app"
	Email   Channel = "email"
	Teams   Channel = "teams"
	Webhook Channel = "webhook"
)

type Policy struct {
	ID             string
	EventType      string
	Channels       []Channel
	QuietPeriod    time.Duration
	CriticalBypass bool
}

type Event struct {
	Type         string
	WorkRecordID string
	OccurredAt   time.Time
	Critical     bool
}

type RecentDelivery struct {
	PolicyID     string
	WorkRecordID string
	DeliveredAt  time.Time
}

type Delivery struct {
	PolicyID     string
	WorkRecordID string
	Channel      Channel
}

type Decision struct {
	Matched    bool
	Suppressed bool
	Reason     string
	Deliveries []Delivery
}

func Evaluate(policy Policy, event Event, recent []RecentDelivery) Decision {
	if policy.ID == "" || policy.EventType != event.Type || event.WorkRecordID == "" {
		return Decision{Reason: "policy did not match event"}
	}
	if !(event.Critical && policy.CriticalBypass) && policy.QuietPeriod > 0 {
		for _, delivery := range recent {
			if delivery.PolicyID == policy.ID &&
				delivery.WorkRecordID == event.WorkRecordID &&
				!delivery.DeliveredAt.Before(event.OccurredAt.Add(-policy.QuietPeriod)) {
				return Decision{
					Matched: true, Suppressed: true,
					Reason: "duplicate delivery suppressed by quiet period",
				}
			}
		}
	}
	deliveries := make([]Delivery, 0, len(policy.Channels))
	for _, channel := range policy.Channels {
		deliveries = append(deliveries, Delivery{
			PolicyID: policy.ID, WorkRecordID: event.WorkRecordID, Channel: channel,
		})
	}
	return Decision{
		Matched: true, Reason: "policy matched", Deliveries: deliveries,
	}
}

var ErrInvalidSummary = errors.New("invalid work summary")

type WorkSummary struct {
	DisplayID        string
	Priority         string
	Status           string
	Subject          string
	SLAState         string
	AuthenticatedURL string
	InternalNote     string
	EmailBody        string
}

type teamsCard struct {
	Type     string `json:"type"`
	Version  string `json:"version"`
	Ticket   string `json:"ticket"`
	Priority string `json:"priority"`
	Status   string `json:"status"`
	Subject  string `json:"subject"`
	SLAState string `json:"sla_state"`
	URL      string `json:"url"`
}

func BuildTeamsCard(summary WorkSummary) ([]byte, error) {
	if summary.DisplayID == "" ||
		summary.Priority == "" ||
		summary.Status == "" ||
		summary.Subject == "" ||
		summary.AuthenticatedURL == "" {
		return nil, ErrInvalidSummary
	}
	return json.Marshal(teamsCard{
		Type: "AdaptiveCard", Version: "1.5",
		Ticket: summary.DisplayID, Priority: summary.Priority,
		Status: summary.Status, Subject: summary.Subject,
		SLAState: summary.SLAState, URL: summary.AuthenticatedURL,
	})
}

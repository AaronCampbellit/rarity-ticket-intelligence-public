// Package mutation defines the audit and event records that accompany an
// accepted business-state mutation.
package mutation

import "time"

type AuditRecord struct {
	ID                   string
	OccurredAt           time.Time
	MSPID                string
	ClientID             string
	ActorType            string
	ActorID              string
	Action               string
	SubjectType          string
	SubjectID            string
	SubjectVersion       int64
	Source               string
	Reason               string
	CorrelationID        string
	SafeDiff             any
	AuthorizationContext map[string]any
}

type EventRecord struct {
	EventID        string
	EventType      string
	SchemaVersion  int
	OccurredAt     time.Time
	MSPID          string
	ClientID       string
	ActorType      string
	ActorID        string
	SubjectType    string
	SubjectID      string
	SubjectVersion int64
	Source         string
	CorrelationID  string
	CausationID    string
	Data           map[string]any
}

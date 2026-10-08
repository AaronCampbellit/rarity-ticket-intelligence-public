// Package calendar defines the typed, source-backed calendar domain model.
package calendar

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidProjection = errors.New("invalid calendar projection")

type SchedulingMode string

const (
	FixedBlock       SchedulingMode = "fixed_block"
	EffortAllocation SchedulingMode = "effort_allocation"
	Informational    SchedulingMode = "informational"
)

type TerminalState string

const (
	Active    TerminalState = "active"
	Completed TerminalState = "completed"
	Cancelled TerminalState = "cancelled"
)

var sourceTypes = map[string]struct{}{
	"work_record":           {},
	"task":                  {},
	"project":               {},
	"phase":                 {},
	"milestone":             {},
	"resource_plan":         {},
	"technician_schedule":   {},
	"pto":                   {},
	"maintenance_window":    {},
	"commercial_commitment": {},
	"custom_date":           {},
}

var builtInSourceRoles = map[string]map[string]struct{}{
	"work_record": {
		"scheduled_work": {}, "due": {}, "follow_up": {},
		"sla_response_deadline": {}, "sla_resolution_deadline": {},
	},
	"task":                  {"scheduled_work": {}, "due": {}},
	"project":               {"planned_start": {}, "planned_end": {}},
	"phase":                 {"planned_start": {}, "planned_end": {}},
	"milestone":             {"milestone": {}},
	"resource_plan":         {"allocation": {}},
	"technician_schedule":   {"availability": {}},
	"pto":                   {"unavailability": {}},
	"maintenance_window":    {"maintenance": {}},
	"commercial_commitment": {"effective": {}, "notice": {}, "renewal": {}, "expiration": {}},
}

type SourceRef struct {
	MSPID    string `json:"msp_id,omitempty"`
	ClientID string `json:"client_id,omitempty"`
	Type     string `json:"type,omitempty"`
	ID       string `json:"id,omitempty"`
}

func (s SourceRef) Validate() error {
	if strings.TrimSpace(s.MSPID) == "" || strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("%w: source tenant and id are required", ErrInvalidProjection)
	}
	if _, ok := sourceTypes[s.Type]; !ok {
		return fmt.Errorf("%w: source type %q is not a typed calendar source", ErrInvalidProjection, s.Type)
	}
	return nil
}

func (s SourceRef) ScopeTarget() scope.Target {
	return scope.Target{MSPID: s.MSPID, ClientID: s.ClientID}
}

type FilterDimensions struct {
	TechnicianIDs []string `json:"technician_ids,omitempty"`
	OwnerIDs      []string `json:"owner_ids,omitempty"`
	ClientIDs     []string `json:"client_ids,omitempty"`
	TeamIDs       []string `json:"team_ids,omitempty"`
	TechnologyIDs []string `json:"technology_ids,omitempty"`
	AssetIDs      []string `json:"asset_ids,omitempty"`
	ProjectIDs    []string `json:"project_ids,omitempty"`
	PhaseIDs      []string `json:"phase_ids,omitempty"`
	SLAIDs        []string `json:"sla_ids,omitempty"`
	TicketTypes   []string `json:"ticket_types,omitempty"`
	TagIDs        []string `json:"tag_ids,omitempty"`
	Priorities    []string `json:"priorities,omitempty"`
}

type HealthInputs struct {
	Blocked           bool       `json:"blocked,omitempty"`
	DependencyBlocked bool       `json:"dependency_blocked,omitempty"`
	HardConstraint    bool       `json:"hard_constraint,omitempty"`
	CapacityShortage  bool       `json:"capacity_shortage,omitempty"`
	DependencyDelay   bool       `json:"dependency_delay,omitempty"`
	ScheduleVariance  bool       `json:"schedule_variance,omitempty"`
	SLAAtRisk         bool       `json:"sla_at_risk,omitempty"`
	DueAt             *time.Time `json:"due_at,omitempty"`
	RiskAt            *time.Time `json:"risk_at,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
}

// Filter carries the authenticated query boundary and optional dimensions.
// ClientIDs nil means all clients authorized for Principal, not an active-client default.
type Filter struct {
	Principal       authorization.Principal
	Target          scope.Target
	Dimensions      FilterDimensions
	TechnicianIDs   []string
	OwnerIDs        []string
	TeamIDs         []string
	ClientIDs       []string
	TechnologyIDs   []string
	ProjectIDs      []string
	PhaseIDs        []string
	SLAIDs          []string
	TicketTypes     []string
	TagIDs          []string
	Priorities      []string
	HealthStates    []HealthState
	ConflictsOnly   bool
	SchedulingModes []SchedulingMode
	TerminalStates  []TerminalState
	EventRoles      []string
	SourceTypes     []string
}

type QueryWindow struct {
	Start time.Time
	End   time.Time
}

type Projection struct {
	ID              string
	EventRole       string
	SourceRoleKey   string
	Source          SourceRef
	SourceRevision  int64
	Title           string
	AllDay          bool
	StartsOn        *time.Time
	EndsOn          *time.Time
	StartsAt        *time.Time
	EndsAt          *time.Time
	Timezone        string
	SchedulingMode  SchedulingMode
	CapacityBearing bool
	OwnerID         string
	AssigneeID      string
	PlannedMinutes  int64
	Recurrence      *RecurrenceRule
	Dimensions      FilterDimensions
	HealthInputs    HealthInputs
	TerminalState   TerminalState
}

func (p Projection) Validate() error {
	if err := p.Source.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.EventRole) == "" || strings.TrimSpace(p.Title) == "" || p.SourceRevision <= 0 {
		return fmt.Errorf("%w: id, event role, title, and positive source revision are required", ErrInvalidProjection)
	}
	if !validProjectionRole(p.Source.Type, p.EventRole, p.SourceRoleKey) {
		return fmt.Errorf("%w: role %q is not registered for source type %q", ErrInvalidProjection, p.EventRole, p.Source.Type)
	}
	if !validSchedulingMode(p.SchedulingMode) {
		return fmt.Errorf("%w: unknown scheduling mode %q", ErrInvalidProjection, p.SchedulingMode)
	}
	if p.PlannedMinutes < 0 {
		return fmt.Errorf("%w: planned effort cannot be negative", ErrInvalidProjection)
	}
	if p.CapacityBearing && (p.SchedulingMode == Informational || strings.TrimSpace(p.AssigneeID) == "" || p.PlannedMinutes <= 0) {
		return fmt.Errorf("%w: capacity events need an assignee, positive effort, and a schedulable mode", ErrInvalidProjection)
	}
	if p.TerminalState != "" && p.TerminalState != Active && p.TerminalState != Completed && p.TerminalState != Cancelled {
		return fmt.Errorf("%w: unknown terminal state %q", ErrInvalidProjection, p.TerminalState)
	}
	if p.AllDay {
		if p.StartsOn == nil || p.StartsAt != nil || p.EndsAt != nil || p.Timezone != "" || !isDate(*p.StartsOn) {
			return fmt.Errorf("%w: all-day events require date fields only", ErrInvalidProjection)
		}
		if p.EndsOn != nil && (!isDate(*p.EndsOn) || p.EndsOn.Before(*p.StartsOn)) {
			return fmt.Errorf("%w: invalid all-day end date", ErrInvalidProjection)
		}
	} else {
		if p.StartsAt == nil || p.StartsOn != nil || p.EndsOn != nil || !validTimezone(p.Timezone) {
			return fmt.Errorf("%w: timed events require timestamps and an IANA timezone", ErrInvalidProjection)
		}
		if p.EndsAt != nil && !p.EndsAt.After(*p.StartsAt) {
			return fmt.Errorf("%w: timed event end must follow its start", ErrInvalidProjection)
		}
	}
	if p.Recurrence != nil {
		if err := p.Recurrence.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidProjection, err)
		}
	}
	return nil
}

func validProjectionRole(sourceType, role, sourceRoleKey string) bool {
	if sourceType == "custom_date" {
		return role != "event" && rolePattern.MatchString(role) && rolePattern.MatchString(sourceRoleKey)
	}
	roles, ok := builtInSourceRoles[sourceType]
	if !ok {
		return false
	}
	_, ok = roles[role]
	return ok
}

func validRoleDefinition(sourceType, role string) bool {
	if sourceType == "custom_date" {
		return role != "event" && rolePattern.MatchString(role)
	}
	return validProjectionRole(sourceType, role, "")
}

func validSchedulingMode(mode SchedulingMode) bool {
	return mode == FixedBlock || mode == EffortAllocation || mode == Informational
}

func validTimezone(name string) bool {
	if strings.TrimSpace(name) == "" || name == "Local" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

func isDate(value time.Time) bool {
	return value.Location() == time.UTC && value.Hour() == 0 && value.Minute() == 0 &&
		value.Second() == 0 && value.Nanosecond() == 0
}

package projects

import (
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type ProjectID string
type PhaseID string

type Project struct {
	ID                        ProjectID
	MSPID                     string
	ClientID                  string
	DisplayID                 string
	Name                      string
	OriginalProposalVersionID string
	LifecycleState            string
	PlannedStart              time.Time
	PlannedEnd                time.Time
	OriginalBaseline          BudgetBaseline
	CurrentBaseline           BudgetBaseline
	Version                   int64
	Phases                    []Phase
	SupportsMilestones        bool
	SupportsTaskDependencies  bool
	CreatedAt                 time.Time
	CreatedBy                 string
}

type Phase struct {
	ID                 PhaseID    `json:"id"`
	ProjectID          ProjectID  `json:"project_id"`
	MSPID              string     `json:"msp_id"`
	ClientID           string     `json:"client_id"`
	Position           int        `json:"position"`
	Name               string     `json:"name"`
	State              string     `json:"state"`
	OwnerID            string     `json:"owner_id,omitempty"`
	ParticipatingTeams []string   `json:"participating_teams"`
	PlannedStart       time.Time  `json:"planned_start"`
	PlannedEnd         time.Time  `json:"planned_end"`
	ActualStart        *time.Time `json:"actual_start,omitempty"`
	ActualEnd          *time.Time `json:"actual_end,omitempty"`
	PlannedMinutes     int64      `json:"planned_minutes"`
	Budget             Money      `json:"budget"`
	Deliverables       []string   `json:"deliverables"`
	CompletionCriteria []string   `json:"completion_criteria"`
	Version            int64      `json:"version"`
}

type Money struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
}

type ResourcePlan struct {
	ID             string    `json:"id"`
	ProjectID      ProjectID `json:"project_id"`
	PhaseID        PhaseID   `json:"phase_id"`
	MSPID          string    `json:"msp_id"`
	ClientID       string    `json:"client_id"`
	RoleID         string    `json:"role_id,omitempty"`
	TeamID         string    `json:"team_id,omitempty"`
	StartsOn       time.Time `json:"starts_on"`
	EndsOn         time.Time `json:"ends_on"`
	PlannedMinutes int64     `json:"planned_minutes"`
	Version        int64     `json:"version"`
}

type ScheduleMutation struct {
	SourceType, SourceID, MSPID, ClientID        string
	ExpectedVersion                              int64
	EventRole                                    string
	Interval                                     calendar.TypedInterval
	Recurrence                                   *calendar.RecurrenceRule
	OccurrenceKey, OccurrenceScope, ProjectionID string
}

func ValidateScheduleMutation(principal authorization.Principal, accepted ScheduleMutation) error {
	if accepted.SourceID == "" || accepted.MSPID == "" || accepted.ClientID == "" || accepted.ExpectedVersion < 1 || (accepted.SourceType != "milestone" && accepted.SourceType != "project" && accepted.SourceType != "phase" && accepted.SourceType != "resource_plan") {
		return ErrInvalidMilestone
	}
	if err := authorization.Authorize(principal, "project.edit", scope.Target{MSPID: accepted.MSPID, ClientID: accepted.ClientID}); err != nil {
		return err
	}
	if err := accepted.Interval.Validate(false); err != nil || accepted.Interval.Empty() {
		return ErrInvalidMilestone
	}
	if accepted.Recurrence != nil && accepted.Recurrence.Validate() != nil {
		return ErrInvalidMilestone
	}
	return nil
}

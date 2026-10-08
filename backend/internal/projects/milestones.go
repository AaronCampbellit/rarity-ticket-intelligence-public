package projects

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrInvalidMilestone           = errors.New("invalid milestone")
	ErrInvalidMilestoneTransition = errors.New("invalid milestone transition")
)

type Milestone struct {
	ID, MSPID, ClientID, Name, Description, Priority, Status, OwnerID, CreatedBy, UpdatedBy string
	ProjectID                                                                               ProjectID
	PhaseID                                                                                 PhaseID
	DueOn                                                                                   time.Time
	AllDay                                                                                  bool
	StartsOn, EndsOn, StartsAt, EndsAt                                                      *time.Time
	Timezone                                                                                string
	Recurrence                                                                              *calendar.RecurrenceRule
	Version                                                                                 int64
	CreatedAt, UpdatedAt                                                                    time.Time
}

type CreateMilestoneCommand struct {
	Principal                                                             authorization.Principal
	ProjectID                                                             ProjectID
	PhaseID                                                               PhaseID
	Name, Description, Priority, OwnerID, ActorID, Source, IdempotencyKey string
	DueOn                                                                 time.Time
	AllDay                                                                bool
	StartsOn, EndsOn, StartsAt, EndsAt                                    *time.Time
	Timezone                                                              string
	Recurrence                                                            *calendar.RecurrenceRule
}

type UpdateMilestoneCommand struct {
	Principal                       authorization.Principal
	Milestone                       Milestone
	ExpectedVersion                 int64
	ActorID, Source, IdempotencyKey string
}
type TransitionMilestoneCommand struct {
	Principal                                 authorization.Principal
	MilestoneID                               string
	ExpectedVersion                           int64
	ToStatus, ActorID, Source, IdempotencyKey string
}

type MilestoneMutation struct {
	Milestone                                     Milestone
	Audit                                         mutation.AuditRecord
	Event                                         mutation.EventRecord
	IdempotencyKey, RequestFingerprint, RequestID string
}

type MilestoneRepository interface {
	FindMilestoneProject(context.Context, scope.Target, ProjectID) (Project, error)
	FindMilestonePhase(context.Context, scope.Target, ProjectID, PhaseID) (Phase, error)
	FindMilestone(context.Context, scope.Target, string) (Milestone, error)
	CreateMilestoneAtomic(context.Context, MilestoneMutation) error
	UpdateMilestoneAtomic(context.Context, MilestoneMutation, int64) error
}

type MilestoneService struct {
	repository MilestoneRepository
	now        func() time.Time
	newID      func() string
}

func NewMilestoneService(r MilestoneRepository, now func() time.Time, newID func() string) *MilestoneService {
	return &MilestoneService{r, now, newID}
}

func (s *MilestoneService) Create(ctx context.Context, c CreateMilestoneCommand) (Milestone, error) {
	actor := c.Principal.ID
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || !validMilestonePrincipal(c.Principal) || !internalid.ValidCanonical(string(c.ProjectID)) || c.PhaseID != "" && !internalid.ValidCanonical(string(c.PhaseID)) || c.OwnerID != "" && !internalid.ValidCanonical(c.OwnerID) || strings.TrimSpace(c.Name) == "" || c.DueOn.IsZero() || actor == "" || (c.ActorID != "" && c.ActorID != actor) || c.Source == "" || !mutation.ValidIdempotencyKey(c.IdempotencyKey) {
		return Milestone{}, ErrInvalidMilestone
	}
	interval := calendar.TypedInterval{AllDay: c.AllDay, StartsOn: c.StartsOn, EndsOn: c.EndsOn, StartsAt: c.StartsAt, EndsAt: c.EndsAt, Timezone: c.Timezone}
	if err := interval.Validate(false); err != nil {
		return Milestone{}, ErrInvalidMilestone
	}
	if c.Recurrence != nil {
		if interval.Empty() || c.Recurrence.Validate() != nil {
			return Milestone{}, ErrInvalidMilestone
		}
	}
	lookup := scope.Target{MSPID: c.Principal.Scope.MSPID, ClientID: c.Principal.Scope.ClientID}
	p, err := s.repository.FindMilestoneProject(ctx, lookup, c.ProjectID)
	if err != nil {
		return Milestone{}, err
	}
	target := scope.Target{MSPID: p.MSPID, ClientID: p.ClientID}
	if err = authorization.Authorize(c.Principal, "project.edit", target); err != nil {
		return Milestone{}, err
	}
	if p.ID != c.ProjectID || p.MSPID != target.MSPID || p.ClientID == "" {
		return Milestone{}, scope.ErrNotFound
	}
	if c.PhaseID != "" {
		ph, e := s.repository.FindMilestonePhase(ctx, target, c.ProjectID, c.PhaseID)
		if e != nil {
			return Milestone{}, e
		}
		if ph.ID != c.PhaseID || ph.ProjectID != c.ProjectID || ph.MSPID != p.MSPID || ph.ClientID != p.ClientID {
			return Milestone{}, scope.ErrNotFound
		}
	}
	now := s.now().UTC()
	allDay := c.AllDay
	if interval.Empty() {
		allDay = true
	}
	m := Milestone{ID: s.newID(), MSPID: p.MSPID, ClientID: p.ClientID, ProjectID: p.ID, PhaseID: c.PhaseID, Name: strings.TrimSpace(c.Name), Description: strings.TrimSpace(c.Description), Priority: defaultString(c.Priority, "normal"), Status: "planned", OwnerID: c.OwnerID, DueOn: dateOnly(c.DueOn), AllDay: allDay, StartsOn: calendar.NormalizeDatePointer(c.StartsOn), EndsOn: calendar.NormalizeDatePointer(c.EndsOn), StartsAt: c.StartsAt, EndsAt: c.EndsAt, Timezone: c.Timezone, Recurrence: c.Recurrence, Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: actor, UpdatedBy: actor}
	fingerprint, err := mutation.Fingerprint(struct {
		ProjectID                            ProjectID
		PhaseID                              PhaseID
		Name, Description, Priority, OwnerID string
		DueOn                                time.Time
		AllDay                               bool
		StartsOn, EndsOn, StartsAt, EndsAt   *time.Time
		Timezone                             string
		Recurrence                           *calendar.RecurrenceRule
	}{m.ProjectID, m.PhaseID, m.Name, m.Description, m.Priority, m.OwnerID, m.DueOn, m.AllDay, m.StartsOn, m.EndsOn, m.StartsAt, m.EndsAt, m.Timezone, m.Recurrence})
	if err != nil {
		return Milestone{}, err
	}
	a, e, cor := s.newID(), s.newID(), s.newID()
	accepted := MilestoneMutation{Milestone: m, Audit: mutation.AuditRecord{ID: a, OccurredAt: now, MSPID: m.MSPID, ClientID: m.ClientID, ActorType: "technician", ActorID: actor, Action: "project.milestone.created", SubjectType: "project_milestone", SubjectID: m.ID, SubjectVersion: 1, Source: c.Source, CorrelationID: cor}, Event: mutation.EventRecord{EventID: e, EventType: "project.milestone.created", SchemaVersion: 1, OccurredAt: now, MSPID: m.MSPID, ClientID: m.ClientID, ActorType: "technician", ActorID: actor, SubjectType: "project_milestone", SubjectID: m.ID, SubjectVersion: 1, Source: c.Source, CorrelationID: cor, Data: map[string]any{"project_id": string(m.ProjectID), "phase_id": string(m.PhaseID)}}, IdempotencyKey: c.IdempotencyKey, RequestFingerprint: fingerprint, RequestID: s.newID()}
	if err = s.repository.CreateMilestoneAtomic(ctx, accepted); err != nil {
		if replay, decodeErr := mutation.DecodeReplay(err, &m); replay {
			return m, decodeErr
		}
		return Milestone{}, err
	}
	return m, nil
}

func (s *MilestoneService) Update(ctx context.Context, c UpdateMilestoneCommand) (Milestone, error) {
	input := c.Milestone
	actor := c.Principal.ID
	if s == nil || s.repository == nil || !validMilestonePrincipal(c.Principal) || c.ExpectedVersion < 1 || !internalid.ValidCanonical(input.ID) || input.MSPID != "" && !internalid.ValidCanonical(input.MSPID) || input.ClientID != "" && !internalid.ValidCanonical(input.ClientID) || input.ProjectID != "" && !internalid.ValidCanonical(string(input.ProjectID)) || input.PhaseID != "" && !internalid.ValidCanonical(string(input.PhaseID)) || input.OwnerID != "" && !internalid.ValidCanonical(input.OwnerID) || strings.TrimSpace(input.Name) == "" || input.DueOn.IsZero() || actor == "" || (c.ActorID != "" && c.ActorID != actor) || c.Source == "" || !mutation.ValidIdempotencyKey(c.IdempotencyKey) {
		return Milestone{}, ErrInvalidMilestone
	}
	lookup := scope.Target{MSPID: c.Principal.Scope.MSPID, ClientID: c.Principal.Scope.ClientID}
	m, err := s.repository.FindMilestone(ctx, lookup, input.ID)
	if err != nil {
		return Milestone{}, err
	}
	target := scope.Target{MSPID: m.MSPID, ClientID: m.ClientID}
	if err := authorization.Authorize(c.Principal, "project.edit", target); err != nil {
		return Milestone{}, err
	}
	p, err := s.repository.FindMilestoneProject(ctx, target, m.ProjectID)
	if err != nil {
		return Milestone{}, err
	}
	if p.MSPID != m.MSPID || p.ClientID != m.ClientID {
		return Milestone{}, scope.ErrNotFound
	}
	if input.PhaseID != "" && input.PhaseID != m.PhaseID {
		return Milestone{}, ErrInvalidMilestone
	}
	if m.PhaseID != "" {
		ph, e := s.repository.FindMilestonePhase(ctx, target, m.ProjectID, m.PhaseID)
		if e != nil {
			return Milestone{}, e
		}
		if ph.ProjectID != m.ProjectID || ph.MSPID != m.MSPID || ph.ClientID != m.ClientID {
			return Milestone{}, scope.ErrNotFound
		}
	}
	interval := calendar.TypedInterval{AllDay: input.AllDay, StartsOn: input.StartsOn, EndsOn: input.EndsOn, StartsAt: input.StartsAt, EndsAt: input.EndsAt, Timezone: input.Timezone}
	if err = interval.Validate(false); err != nil {
		return Milestone{}, ErrInvalidMilestone
	}
	if input.Recurrence != nil && (interval.Empty() || input.Recurrence.Validate() != nil) {
		return Milestone{}, ErrInvalidMilestone
	}
	m.Version = c.ExpectedVersion + 1
	m.Name = strings.TrimSpace(input.Name)
	m.Description = strings.TrimSpace(input.Description)
	m.Priority = defaultString(input.Priority, m.Priority)
	m.OwnerID = input.OwnerID
	m.DueOn = dateOnly(input.DueOn)
	m.AllDay = input.AllDay
	if interval.Empty() {
		m.AllDay = true
	}
	m.StartsOn = calendar.NormalizeDatePointer(input.StartsOn)
	m.EndsOn = calendar.NormalizeDatePointer(input.EndsOn)
	m.StartsAt = input.StartsAt
	m.EndsAt = input.EndsAt
	m.Timezone = input.Timezone
	m.Recurrence = input.Recurrence
	m.UpdatedAt = s.now().UTC()
	m.UpdatedBy = actor
	fingerprint, err := mutation.Fingerprint(struct {
		ID                                   string
		Expected                             int64
		Name, Description, Priority, OwnerID string
		DueOn                                time.Time
		AllDay                               bool
		StartsOn, EndsOn, StartsAt, EndsAt   *time.Time
		Timezone                             string
		Recurrence                           *calendar.RecurrenceRule
	}{m.ID, c.ExpectedVersion, m.Name, m.Description, m.Priority, m.OwnerID, m.DueOn, m.AllDay, m.StartsOn, m.EndsOn, m.StartsAt, m.EndsAt, m.Timezone, m.Recurrence})
	if err != nil {
		return Milestone{}, err
	}
	a, e, cor := s.newID(), s.newID(), s.newID()
	accepted := MilestoneMutation{Milestone: m, Audit: mutation.AuditRecord{ID: a, OccurredAt: m.UpdatedAt, MSPID: m.MSPID, ClientID: m.ClientID, ActorType: "technician", ActorID: actor, Action: "project.milestone.updated", SubjectType: "project_milestone", SubjectID: m.ID, SubjectVersion: m.Version, Source: c.Source, CorrelationID: cor}, Event: mutation.EventRecord{EventID: e, EventType: "project.milestone.updated", SchemaVersion: 1, OccurredAt: m.UpdatedAt, MSPID: m.MSPID, ClientID: m.ClientID, ActorType: "technician", ActorID: actor, SubjectType: "project_milestone", SubjectID: m.ID, SubjectVersion: m.Version, Source: c.Source, CorrelationID: cor}, IdempotencyKey: c.IdempotencyKey, RequestFingerprint: fingerprint, RequestID: s.newID()}
	if err = s.repository.UpdateMilestoneAtomic(ctx, accepted, c.ExpectedVersion); err != nil {
		if replay, decodeErr := mutation.DecodeReplay(err, &m); replay {
			return m, decodeErr
		}
		return Milestone{}, err
	}
	return m, nil
}
func (s *MilestoneService) Transition(ctx context.Context, c TransitionMilestoneCommand) (Milestone, error) {
	actor := c.Principal.ID
	if s == nil || s.repository == nil || !validMilestonePrincipal(c.Principal) || !internalid.ValidCanonical(c.MilestoneID) || c.ExpectedVersion < 1 || actor == "" || (c.ActorID != "" && c.ActorID != actor) || c.Source == "" || !mutation.ValidIdempotencyKey(c.IdempotencyKey) {
		return Milestone{}, ErrInvalidMilestone
	}
	lookup := scope.Target{MSPID: c.Principal.Scope.MSPID, ClientID: c.Principal.Scope.ClientID}
	m, err := s.repository.FindMilestone(ctx, lookup, c.MilestoneID)
	if err != nil {
		return Milestone{}, err
	}
	if authorization.Authorize(c.Principal, "project.edit", scope.Target{MSPID: m.MSPID, ClientID: m.ClientID}) != nil {
		return Milestone{}, authorization.ErrForbidden
	}
	if m.Version == c.ExpectedVersion && !validMilestoneTransition(m.Status, c.ToStatus) {
		return Milestone{}, ErrInvalidMilestoneTransition
	}
	m.Status = c.ToStatus
	m.Version = c.ExpectedVersion + 1
	m.UpdatedAt = s.now().UTC()
	m.UpdatedBy = actor
	fp, err := mutation.Fingerprint(struct {
		ID, To   string
		Expected int64
	}{m.ID, c.ToStatus, c.ExpectedVersion})
	if err != nil {
		return Milestone{}, err
	}
	a, e, cor := s.newID(), s.newID(), s.newID()
	accepted := MilestoneMutation{Milestone: m, Audit: mutation.AuditRecord{ID: a, OccurredAt: m.UpdatedAt, MSPID: m.MSPID, ClientID: m.ClientID, ActorType: "technician", ActorID: actor, Action: "project.milestone.transitioned", SubjectType: "project_milestone", SubjectID: m.ID, SubjectVersion: m.Version, Source: c.Source, CorrelationID: cor}, Event: mutation.EventRecord{EventID: e, EventType: "project.milestone.transitioned", SchemaVersion: 1, OccurredAt: m.UpdatedAt, MSPID: m.MSPID, ClientID: m.ClientID, ActorType: "technician", ActorID: actor, SubjectType: "project_milestone", SubjectID: m.ID, SubjectVersion: m.Version, Source: c.Source, CorrelationID: cor, Data: map[string]any{"status": m.Status}}, IdempotencyKey: c.IdempotencyKey, RequestFingerprint: fp, RequestID: s.newID()}
	if err = s.repository.UpdateMilestoneAtomic(ctx, accepted, c.ExpectedVersion); err != nil {
		if replay, de := mutation.DecodeReplay(err, &m); replay {
			return m, de
		}
		return Milestone{}, err
	}
	return m, nil
}
func validMilestonePrincipal(p authorization.Principal) bool {
	return internalid.ValidCanonical(p.ID) && internalid.ValidCanonical(p.Scope.MSPID) && (p.Scope.ClientID == "" || internalid.ValidCanonical(p.Scope.ClientID))
}
func validMilestoneTransition(from, to string) bool {
	switch from {
	case "planned":
		return to == "in_progress" || to == "blocked" || to == "cancelled"
	case "in_progress":
		return to == "blocked" || to == "completed" || to == "cancelled"
	case "blocked":
		return to == "in_progress" || to == "cancelled"
	}
	return false
}
func defaultString(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return strings.TrimSpace(v)
}
func dateOnly(v time.Time) time.Time {
	y, m, d := v.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

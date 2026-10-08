package commitments

import (
	"context"
	"errors"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"strings"
	"time"
)

type ScopeType string

const (
	ScopeClient  ScopeType = "client"
	ScopeService ScopeType = "service"
	ScopeAsset   ScopeType = "asset"
)

type ConflictPolicy string

const (
	PolicyInformational     ConflictPolicy = "informational"
	PolicyWarning           ConflictPolicy = "warning"
	PolicyOverrideableBlock ConflictPolicy = "overrideable_block"
	PolicyHardBlock         ConflictPolicy = "hard_block"
)

var (
	ErrInvalidMaintenance           = errors.New("invalid maintenance")
	ErrCommitmentVersionConflict    = errors.New("commitment version conflict")
	ErrDuplicateMaintenanceScope    = errors.New("duplicate maintenance scope")
	ErrInvalidMaintenanceTransition = errors.New("invalid maintenance transition")
)

type ScopeRef struct {
	Type ScopeType
	ID   string
}
type ResolvedScope struct {
	ID, ClientID string
	Type         ScopeType
	ResourceID   string
}
type MaintenanceWindow struct {
	ID, MSPID, Title, Description, Timezone, Status, OwnerID, CreatedBy, UpdatedBy string
	StartsAt, EndsAt                                                               time.Time
	AllDay                                                                         bool
	StartsOn, EndsOn                                                               *time.Time
	Recurrence                                                                     *calendar.RecurrenceRule
	Protected                                                                      bool
	ConflictPolicy                                                                 ConflictPolicy
	Version                                                                        int64
	Scopes                                                                         []ResolvedScope
	CreatedAt, UpdatedAt                                                           time.Time
}
type CreateMaintenanceCommand struct {
	Principal                                                              authorization.Principal
	Title, Description, Timezone, OwnerID, ActorID, Source, IdempotencyKey string
	StartsAt, EndsAt                                                       time.Time
	AllDay                                                                 bool
	StartsOn, EndsOn                                                       *time.Time
	Recurrence                                                             *calendar.RecurrenceRule
	Protected                                                              bool
	ConflictPolicy                                                         ConflictPolicy
	Scopes                                                                 []ScopeRef
}
type UpdateMaintenanceCommand struct {
	Principal                                                              authorization.Principal
	WindowID                                                               string
	ExpectedVersion                                                        int64
	Title, Description, Timezone, OwnerID, ActorID, Source, IdempotencyKey string
	StartsAt, EndsAt                                                       time.Time
	AllDay                                                                 bool
	StartsOn, EndsOn                                                       *time.Time
	Recurrence                                                             *calendar.RecurrenceRule
	Protected                                                              bool
	ConflictPolicy                                                         ConflictPolicy
	Scopes                                                                 []ScopeRef
}
type TransitionMaintenanceCommand struct {
	Principal                                 authorization.Principal
	WindowID                                  string
	ExpectedVersion                           int64
	ToStatus, ActorID, Source, IdempotencyKey string
}
type MaintenanceMutation struct {
	Window                                        MaintenanceWindow
	Audit                                         mutation.AuditRecord
	Event                                         mutation.EventRecord
	IdempotencyKey, RequestFingerprint, RequestID string
}
type MaintenanceRepository interface {
	ResolveMaintenanceScopes(context.Context, string, []ScopeRef) ([]ResolvedScope, error)
	FindMaintenance(context.Context, string, string) (MaintenanceWindow, error)
	CreateMaintenanceAtomic(context.Context, MaintenanceMutation) error
	UpdateMaintenanceAtomic(context.Context, MaintenanceMutation, int64) error
	TransitionMaintenanceAtomic(context.Context, MaintenanceMutation, int64) error
}
type MaintenanceService struct {
	repository MaintenanceRepository
	now        func() time.Time
	newID      func() string
}

func NewMaintenanceService(r MaintenanceRepository, n func() time.Time, i func() string) *MaintenanceService {
	return &MaintenanceService{r, n, i}
}
func (s *MaintenanceService) Create(ctx context.Context, c CreateMaintenanceCommand) (MaintenanceWindow, error) {
	if c.ConflictPolicy == "" {
		c.ConflictPolicy = PolicyWarning
	}
	actor := c.Principal.ID
	interval := calendar.TypedInterval{AllDay: c.AllDay, StartsOn: c.StartsOn, EndsOn: c.EndsOn, StartsAt: timePointer(c.StartsAt), EndsAt: timePointer(c.EndsAt), Timezone: c.Timezone}
	if c.AllDay {
		interval.StartsAt = nil
		interval.EndsAt = nil
	}
	if s == nil || s.repository == nil || !validCommitmentPrincipal(c.Principal) || c.OwnerID != "" && !internalid.ValidCanonical(c.OwnerID) || strings.TrimSpace(c.Title) == "" || actor == "" || (c.ActorID != "" && c.ActorID != actor) || c.Source == "" || !mutation.ValidIdempotencyKey(c.IdempotencyKey) || len(c.Scopes) == 0 || !validPolicy(c.ConflictPolicy) || interval.Validate(true) != nil || interval.Empty() || c.Recurrence != nil && c.Recurrence.Validate() != nil {
		return MaintenanceWindow{}, ErrInvalidMaintenance
	}
	if err := authorization.Authorize(c.Principal, "calendar.commitment.manage", scope.Target{MSPID: c.Principal.Scope.MSPID}); err != nil {
		return MaintenanceWindow{}, err
	}
	if err := validateScopeRefs(c.Scopes); err != nil {
		return MaintenanceWindow{}, err
	}
	resolved, err := s.repository.ResolveMaintenanceScopes(ctx, c.Principal.Scope.MSPID, c.Scopes)
	if err != nil {
		return MaintenanceWindow{}, err
	}
	if len(resolved) != len(c.Scopes) {
		return MaintenanceWindow{}, scope.ErrNotFound
	}
	for _, v := range resolved {
		if v.ClientID == "" || !validScope(v.Type) {
			return MaintenanceWindow{}, scope.ErrNotFound
		}
	}
	for index := range resolved {
		if resolved[index].ID == "" {
			resolved[index].ID = s.newID()
		}
	}
	now := s.now().UTC()
	w := MaintenanceWindow{ID: s.newID(), MSPID: c.Principal.Scope.MSPID, Title: strings.TrimSpace(c.Title), Description: strings.TrimSpace(c.Description), Timezone: c.Timezone, StartsAt: c.StartsAt.UTC(), EndsAt: c.EndsAt.UTC(), AllDay: c.AllDay, StartsOn: calendar.NormalizeDatePointer(c.StartsOn), EndsOn: calendar.NormalizeDatePointer(c.EndsOn), Recurrence: c.Recurrence, Protected: c.Protected, ConflictPolicy: c.ConflictPolicy, Status: "planned", OwnerID: c.OwnerID, Version: 1, Scopes: resolved, CreatedAt: now, UpdatedAt: now, CreatedBy: actor, UpdatedBy: actor}
	fp, err := mutation.Fingerprint(struct {
		Title, Description, Timezone, Owner string
		AllDay                              bool
		StartsOn, EndsOn                    *time.Time
		StartsAt, EndsAt                    time.Time
		Protected                           bool
		Policy                              ConflictPolicy
		Scopes                              []ScopeRef
		Recurrence                          *calendar.RecurrenceRule
	}{w.Title, w.Description, w.Timezone, w.OwnerID, w.AllDay, w.StartsOn, w.EndsOn, w.StartsAt, w.EndsAt, w.Protected, w.ConflictPolicy, c.Scopes, w.Recurrence})
	if err != nil {
		return MaintenanceWindow{}, err
	}
	a, e, cor := s.newID(), s.newID(), s.newID()
	m := MaintenanceMutation{Window: w, Audit: mutation.AuditRecord{ID: a, OccurredAt: now, MSPID: w.MSPID, ActorType: "technician", ActorID: actor, Action: "maintenance.window.created", SubjectType: "maintenance_window", SubjectID: w.ID, SubjectVersion: 1, Source: c.Source, CorrelationID: cor}, Event: mutation.EventRecord{EventID: e, EventType: "maintenance.window.created", SchemaVersion: 1, OccurredAt: now, MSPID: w.MSPID, ActorType: "technician", ActorID: actor, SubjectType: "maintenance_window", SubjectID: w.ID, SubjectVersion: 1, Source: c.Source, CorrelationID: cor, Data: map[string]any{"protected": w.Protected, "conflict_policy": w.ConflictPolicy}}, IdempotencyKey: c.IdempotencyKey, RequestFingerprint: fp, RequestID: s.newID()}
	if err = s.repository.CreateMaintenanceAtomic(ctx, m); err != nil {
		if replay, de := mutation.DecodeReplay(err, &w); replay {
			return w, de
		}
		return MaintenanceWindow{}, err
	}
	return w, nil
}
func (s *MaintenanceService) Update(ctx context.Context, c UpdateMaintenanceCommand) (MaintenanceWindow, error) {
	if c.ConflictPolicy == "" {
		c.ConflictPolicy = PolicyWarning
	}
	actor := c.Principal.ID
	interval := calendar.TypedInterval{AllDay: c.AllDay, StartsOn: c.StartsOn, EndsOn: c.EndsOn, StartsAt: timePointer(c.StartsAt), EndsAt: timePointer(c.EndsAt), Timezone: c.Timezone}
	if c.AllDay {
		interval.StartsAt = nil
		interval.EndsAt = nil
	}
	if s == nil || s.repository == nil || !validCommitmentPrincipal(c.Principal) || !internalid.ValidCanonical(c.WindowID) || c.OwnerID != "" && !internalid.ValidCanonical(c.OwnerID) || c.ExpectedVersion < 1 || strings.TrimSpace(c.Title) == "" || actor == "" || (c.ActorID != "" && c.ActorID != actor) || c.Source == "" || !mutation.ValidIdempotencyKey(c.IdempotencyKey) || len(c.Scopes) == 0 || !validPolicy(c.ConflictPolicy) || interval.Validate(true) != nil || interval.Empty() || c.Recurrence != nil && c.Recurrence.Validate() != nil {
		return MaintenanceWindow{}, ErrInvalidMaintenance
	}
	if err := authorization.Authorize(c.Principal, "calendar.commitment.manage", scope.Target{MSPID: c.Principal.Scope.MSPID}); err != nil {
		return MaintenanceWindow{}, err
	}
	current, err := s.repository.FindMaintenance(ctx, c.Principal.Scope.MSPID, c.WindowID)
	if err != nil {
		return MaintenanceWindow{}, err
	}
	if err := validateScopeRefs(c.Scopes); err != nil {
		return MaintenanceWindow{}, err
	}
	resolved, err := s.repository.ResolveMaintenanceScopes(ctx, c.Principal.Scope.MSPID, c.Scopes)
	if err != nil {
		return MaintenanceWindow{}, err
	}
	if len(resolved) != len(c.Scopes) {
		return MaintenanceWindow{}, scope.ErrNotFound
	}
	for i := range resolved {
		if resolved[i].ClientID == "" || !validScope(resolved[i].Type) {
			return MaintenanceWindow{}, scope.ErrNotFound
		}
		if resolved[i].ID == "" {
			resolved[i].ID = s.newID()
		}
	}
	now := s.now().UTC()
	w := current
	w.Title = strings.TrimSpace(c.Title)
	w.Description = strings.TrimSpace(c.Description)
	w.Timezone = c.Timezone
	w.StartsAt = c.StartsAt.UTC()
	w.EndsAt = c.EndsAt.UTC()
	w.AllDay = c.AllDay
	w.StartsOn = calendar.NormalizeDatePointer(c.StartsOn)
	w.EndsOn = calendar.NormalizeDatePointer(c.EndsOn)
	w.Recurrence = c.Recurrence
	w.Protected = c.Protected
	w.ConflictPolicy = c.ConflictPolicy
	w.OwnerID = c.OwnerID
	w.Version = c.ExpectedVersion + 1
	w.Scopes = resolved
	w.UpdatedAt = now
	w.UpdatedBy = actor
	fp, err := mutation.Fingerprint(struct {
		ID                                  string
		Expected                            int64
		Title, Description, Timezone, Owner string
		AllDay                              bool
		StartsOn, EndsOn                    *time.Time
		StartsAt, EndsAt                    time.Time
		Protected                           bool
		Policy                              ConflictPolicy
		Scopes                              []ScopeRef
		Recurrence                          *calendar.RecurrenceRule
	}{w.ID, c.ExpectedVersion, w.Title, w.Description, w.Timezone, w.OwnerID, w.AllDay, w.StartsOn, w.EndsOn, w.StartsAt, w.EndsAt, w.Protected, w.ConflictPolicy, c.Scopes, w.Recurrence})
	if err != nil {
		return MaintenanceWindow{}, err
	}
	a, e, cor := s.newID(), s.newID(), s.newID()
	m := MaintenanceMutation{Window: w, Audit: mutation.AuditRecord{ID: a, OccurredAt: now, MSPID: w.MSPID, ActorType: "technician", ActorID: actor, Action: "maintenance.window.updated", SubjectType: "maintenance_window", SubjectID: w.ID, SubjectVersion: w.Version, Source: c.Source, CorrelationID: cor}, Event: mutation.EventRecord{EventID: e, EventType: "maintenance.window.updated", SchemaVersion: 1, OccurredAt: now, MSPID: w.MSPID, ActorType: "technician", ActorID: actor, SubjectType: "maintenance_window", SubjectID: w.ID, SubjectVersion: w.Version, Source: c.Source, CorrelationID: cor, Data: map[string]any{"protected": w.Protected, "conflict_policy": w.ConflictPolicy}}, IdempotencyKey: c.IdempotencyKey, RequestFingerprint: fp, RequestID: s.newID()}
	if err = s.repository.UpdateMaintenanceAtomic(ctx, m, c.ExpectedVersion); err != nil {
		if replay, de := mutation.DecodeReplay(err, &w); replay {
			return w, de
		}
		return MaintenanceWindow{}, err
	}
	return w, nil
}
func (s *MaintenanceService) Transition(ctx context.Context, c TransitionMaintenanceCommand) (MaintenanceWindow, error) {
	actor := c.Principal.ID
	if s == nil || s.repository == nil || !validCommitmentPrincipal(c.Principal) || !internalid.ValidCanonical(c.WindowID) || c.ExpectedVersion < 1 || actor == "" || (c.ActorID != "" && c.ActorID != actor) || c.Source == "" || !mutation.ValidIdempotencyKey(c.IdempotencyKey) {
		return MaintenanceWindow{}, ErrInvalidMaintenance
	}
	w, err := s.repository.FindMaintenance(ctx, c.Principal.Scope.MSPID, c.WindowID)
	if err != nil {
		return MaintenanceWindow{}, err
	}
	if authorization.Authorize(c.Principal, "calendar.commitment.manage", scope.Target{MSPID: w.MSPID}) != nil {
		return MaintenanceWindow{}, authorization.ErrForbidden
	}
	if w.Version == c.ExpectedVersion && !validMaintenanceTransition(w.Status, c.ToStatus) {
		return MaintenanceWindow{}, ErrInvalidMaintenanceTransition
	}
	w.Status = c.ToStatus
	w.Version = c.ExpectedVersion + 1
	w.UpdatedAt = s.now().UTC()
	w.UpdatedBy = actor
	fp, _ := mutation.Fingerprint(struct {
		ID, To   string
		Expected int64
	}{w.ID, c.ToStatus, c.ExpectedVersion})
	a, e, cor := s.newID(), s.newID(), s.newID()
	m := MaintenanceMutation{Window: w, Audit: mutation.AuditRecord{ID: a, OccurredAt: w.UpdatedAt, MSPID: w.MSPID, ActorType: "technician", ActorID: actor, Action: "maintenance.window.transitioned", SubjectType: "maintenance_window", SubjectID: w.ID, SubjectVersion: w.Version, Source: c.Source, CorrelationID: cor}, Event: mutation.EventRecord{EventID: e, EventType: "maintenance.window.transitioned", SchemaVersion: 1, OccurredAt: w.UpdatedAt, MSPID: w.MSPID, ActorType: "technician", ActorID: actor, SubjectType: "maintenance_window", SubjectID: w.ID, SubjectVersion: w.Version, Source: c.Source, CorrelationID: cor, Data: map[string]any{"status": w.Status}}, IdempotencyKey: c.IdempotencyKey, RequestFingerprint: fp, RequestID: s.newID()}
	if err = s.repository.TransitionMaintenanceAtomic(ctx, m, c.ExpectedVersion); err != nil {
		if replay, de := mutation.DecodeReplay(err, &w); replay {
			return w, de
		}
		return MaintenanceWindow{}, err
	}
	return w, nil
}
func validMaintenanceTransition(from, to string) bool {
	switch from {
	case "planned":
		return to == "active" || to == "cancelled"
	case "active":
		return to == "completed" || to == "cancelled"
	}
	return false
}
func timePointer(v time.Time) *time.Time {
	if v.IsZero() {
		return nil
	}
	return &v
}
func validateScopeRefs(refs []ScopeRef) error {
	seen := map[string]struct{}{}
	for _, r := range refs {
		if !validScope(r.Type) || !internalid.ValidCanonical(r.ID) {
			return ErrInvalidMaintenance
		}
		k := string(r.Type) + ":" + r.ID
		if _, ok := seen[k]; ok {
			return ErrDuplicateMaintenanceScope
		}
		seen[k] = struct{}{}
	}
	return nil
}
func validCommitmentPrincipal(p authorization.Principal) bool {
	return internalid.ValidCanonical(p.ID) && internalid.ValidCanonical(p.Scope.MSPID) && (p.Scope.ClientID == "" || internalid.ValidCanonical(p.Scope.ClientID))
}
func validPolicy(v ConflictPolicy) bool {
	return v == PolicyInformational || v == PolicyWarning || v == PolicyOverrideableBlock || v == PolicyHardBlock
}
func validScope(v ScopeType) bool { return v == ScopeClient || v == ScopeService || v == ScopeAsset }

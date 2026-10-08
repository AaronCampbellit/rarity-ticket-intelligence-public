package workforce

import (
	"context"
	"errors"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"strings"
	"time"
)

type PTOState string

const (
	Requested PTOState = "requested"
	Approved  PTOState = "approved"
	Rejected  PTOState = "rejected"
	Cancelled PTOState = "cancelled"
)

var (
	ErrInvalidPTO           = errors.New("invalid pto")
	ErrInvalidPTOTransition = errors.New("invalid pto transition")
)

type PTORequest struct {
	ID, MSPID, TechnicianID, PTOType, ManagerID, DecidedBy, DecisionReason, CreatedBy, UpdatedBy, Timezone string
	StartsOn, EndsOn, StartsAt, EndsAt                                                                     *time.Time
	AllDay                                                                                                 bool
	State                                                                                                  PTOState
	Version                                                                                                int64
	CreatedAt, UpdatedAt                                                                                   time.Time
}

func (p PTORequest) ReducesCapacity() bool   { return p.State == Approved }
func (p PTORequest) CapacityTentative() bool { return p.State == Requested }

type RequestPTOCommand struct {
	Principal                                                        authorization.Principal
	TechnicianID, PTOType, Timezone, ActorID, Source, IdempotencyKey string
	StartsOn, EndsOn, StartsAt, EndsAt                               *time.Time
	AllDay                                                           bool
}
type DecidePTOCommand struct {
	Principal                               authorization.Principal
	RequestID                               string
	Decision                                PTOState
	Reason, ActorID, Source, IdempotencyKey string
	ExpectedVersion                         int64
}
type CancelPTOCommand struct {
	Principal                                  authorization.Principal
	RequestID, ActorID, Source, IdempotencyKey string
	ExpectedVersion                            int64
}
type PTOMutation struct {
	Request                                                  PTORequest
	Audit                                                    mutation.AuditRecord
	Event                                                    mutation.EventRecord
	Authority, IdempotencyKey, RequestFingerprint, RequestID string
}
type PTORepository interface {
	TechnicianInMSP(context.Context, string, string) (bool, error)
	ActiveManager(context.Context, string, string) (string, error)
	FindPTO(context.Context, string, string) (PTORequest, error)
	CreatePTOAtomic(context.Context, PTOMutation) error
	UpdatePTOAtomic(context.Context, PTOMutation, int64) error
}
type PTOService struct {
	repository PTORepository
	now        func() time.Time
	newID      func() string
}

func NewPTOService(r PTORepository, n func() time.Time, i func() string) *PTOService {
	return &PTOService{r, n, i}
}
func (s *PTOService) Request(ctx context.Context, c RequestPTOCommand) (PTORequest, error) {
	actor := c.Principal.ID
	if s == nil || s.repository == nil || !validWorkforcePrincipal(c.Principal) || !internalid.ValidCanonical(c.TechnicianID) || actor == "" || (c.ActorID != "" && c.ActorID != actor) || c.Source == "" || !mutation.ValidIdempotencyKey(c.IdempotencyKey) || !validPTOType(c.PTOType) || !validPeriod(c.AllDay, c.StartsOn, c.EndsOn, c.StartsAt, c.EndsAt, c.Timezone) {
		return PTORequest{}, ErrInvalidPTO
	}
	if c.Principal.ID != c.TechnicianID {
		return PTORequest{}, authorization.ErrForbidden
	}
	ok, err := s.repository.TechnicianInMSP(ctx, c.Principal.Scope.MSPID, c.TechnicianID)
	if err != nil {
		return PTORequest{}, err
	}
	if !ok {
		return PTORequest{}, scope.ErrNotFound
	}
	manager, err := s.repository.ActiveManager(ctx, c.Principal.Scope.MSPID, c.TechnicianID)
	if err != nil {
		return PTORequest{}, err
	}
	now := s.now().UTC()
	p := PTORequest{ID: s.newID(), MSPID: c.Principal.Scope.MSPID, TechnicianID: c.TechnicianID, PTOType: strings.TrimSpace(c.PTOType), ManagerID: manager, Timezone: c.Timezone, StartsOn: normalizeDate(c.StartsOn), EndsOn: normalizeDate(c.EndsOn), StartsAt: c.StartsAt, EndsAt: c.EndsAt, AllDay: c.AllDay, State: Requested, Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: actor, UpdatedBy: actor}
	fingerprint, err := mutation.Fingerprint(struct {
		TechnicianID, PTOType, Timezone    string
		StartsOn, EndsOn, StartsAt, EndsAt *time.Time
		AllDay                             bool
	}{p.TechnicianID, p.PTOType, p.Timezone, p.StartsOn, p.EndsOn, p.StartsAt, p.EndsAt, p.AllDay})
	if err != nil {
		return PTORequest{}, err
	}
	m := s.ptoMutation(p, actor, c.Source, "pto.requested")
	m.Authority = "requester"
	m.IdempotencyKey = c.IdempotencyKey
	m.RequestFingerprint = fingerprint
	m.RequestID = s.newID()
	if err = s.repository.CreatePTOAtomic(ctx, m); err != nil {
		if replay, decodeErr := mutation.DecodeReplay(err, &p); replay {
			return p, decodeErr
		}
		return PTORequest{}, err
	}
	return p, nil
}
func (s *PTOService) Decide(ctx context.Context, c DecidePTOCommand) (PTORequest, error) {
	actor := c.Principal.ID
	if !validWorkforcePrincipal(c.Principal) || actor == "" || (c.ActorID != "" && c.ActorID != actor) {
		return PTORequest{}, authorization.ErrForbidden
	}
	if !internalid.ValidCanonical(c.RequestID) || c.ExpectedVersion < 1 || c.Source == "" || !mutation.ValidIdempotencyKey(c.IdempotencyKey) || (c.Decision != Approved && c.Decision != Rejected) || (c.Decision == Rejected && strings.TrimSpace(c.Reason) == "") {
		return PTORequest{}, ErrInvalidPTO
	}
	p, err := s.repository.FindPTO(ctx, c.Principal.Scope.MSPID, c.RequestID)
	if err != nil {
		return PTORequest{}, err
	}
	if p.MSPID != c.Principal.Scope.MSPID {
		return PTORequest{}, scope.ErrNotFound
	}
	if p.Version == c.ExpectedVersion && p.State != Requested {
		return PTORequest{}, ErrInvalidPTOTransition
	}
	manager, err := s.repository.ActiveManager(ctx, p.MSPID, p.TechnicianID)
	if err != nil {
		return PTORequest{}, err
	}
	admin := authorization.Authorize(c.Principal, "calendar.workforce.manage", scope.Target{MSPID: p.MSPID}) == nil
	if actor != manager && !admin {
		return PTORequest{}, authorization.ErrForbidden
	}
	p.State = c.Decision
	p.DecidedBy = actor
	p.DecisionReason = strings.TrimSpace(c.Reason)
	p.Version = c.ExpectedVersion + 1
	p.UpdatedAt = s.now().UTC()
	p.UpdatedBy = actor
	fingerprint, err := mutation.Fingerprint(struct {
		RequestID string
		Decision  PTOState
		Reason    string
		Expected  int64
	}{c.RequestID, c.Decision, p.DecisionReason, c.ExpectedVersion})
	if err != nil {
		return PTORequest{}, err
	}
	m := s.ptoMutation(p, actor, c.Source, "pto."+string(c.Decision))
	m.Authority = "decision"
	m.IdempotencyKey = c.IdempotencyKey
	m.RequestFingerprint = fingerprint
	m.RequestID = s.newID()
	if err = s.repository.UpdatePTOAtomic(ctx, m, c.ExpectedVersion); err != nil {
		if replay, decodeErr := mutation.DecodeReplay(err, &p); replay {
			return p, decodeErr
		}
		return PTORequest{}, err
	}
	return p, nil
}
func (s *PTOService) Cancel(ctx context.Context, c CancelPTOCommand) (PTORequest, error) {
	actor := c.Principal.ID
	if s == nil || s.repository == nil || !validWorkforcePrincipal(c.Principal) || !internalid.ValidCanonical(c.RequestID) || c.ExpectedVersion < 1 || actor == "" || (c.ActorID != "" && c.ActorID != actor) || c.Source == "" || !mutation.ValidIdempotencyKey(c.IdempotencyKey) {
		return PTORequest{}, authorization.ErrForbidden
	}
	p, err := s.repository.FindPTO(ctx, c.Principal.Scope.MSPID, c.RequestID)
	if err != nil {
		return PTORequest{}, err
	}
	if p.MSPID != c.Principal.Scope.MSPID {
		return PTORequest{}, scope.ErrNotFound
	}
	if actor != p.TechnicianID {
		return PTORequest{}, authorization.ErrForbidden
	}
	if p.Version == c.ExpectedVersion && p.State != Requested && p.State != Approved {
		return PTORequest{}, ErrInvalidPTOTransition
	}
	p.State = Cancelled
	p.Version = c.ExpectedVersion + 1
	p.UpdatedAt = s.now().UTC()
	p.UpdatedBy = actor
	fingerprint, err := mutation.Fingerprint(struct {
		RequestID string
		Expected  int64
	}{c.RequestID, c.ExpectedVersion})
	if err != nil {
		return PTORequest{}, err
	}
	m := s.ptoMutation(p, actor, c.Source, "pto.cancelled")
	m.Authority = "requester"
	m.IdempotencyKey = c.IdempotencyKey
	m.RequestFingerprint = fingerprint
	m.RequestID = s.newID()
	if err = s.repository.UpdatePTOAtomic(ctx, m, c.ExpectedVersion); err != nil {
		if replay, decodeErr := mutation.DecodeReplay(err, &p); replay {
			return p, decodeErr
		}
		return PTORequest{}, err
	}
	return p, nil
}
func normalizeDate(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	d := date(*v)
	return &d
}
func (s *PTOService) ptoMutation(p PTORequest, actor, source, event string) PTOMutation {
	a, e, cor := s.newID(), s.newID(), s.newID()
	return PTOMutation{Request: p, Audit: mutation.AuditRecord{ID: a, OccurredAt: p.UpdatedAt, MSPID: p.MSPID, ActorType: "technician", ActorID: actor, Action: event, SubjectType: "pto_request", SubjectID: p.ID, SubjectVersion: p.Version, Source: source, CorrelationID: cor}, Event: mutation.EventRecord{EventID: e, EventType: event, SchemaVersion: 1, OccurredAt: p.UpdatedAt, MSPID: p.MSPID, ActorType: "technician", ActorID: actor, SubjectType: "pto_request", SubjectID: p.ID, SubjectVersion: p.Version, Source: source, CorrelationID: cor, Data: map[string]any{"technician_id": p.TechnicianID, "state": p.State}}}
}
func validPeriod(all bool, so, eo, sa, ea *time.Time, tz string) bool {
	if all {
		return so != nil && sa == nil && ea == nil && tz == "" && (eo == nil || !eo.Before(*so))
	}
	if so != nil || eo != nil || sa == nil || ea == nil || !ea.After(*sa) {
		return false
	}
	_, err := time.LoadLocation(tz)
	return err == nil
}
func validPTOType(value string) bool {
	switch value {
	case "vacation", "sick", "personal", "training", "other":
		return true
	}
	return false
}

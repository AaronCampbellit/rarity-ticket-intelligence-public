package workforce

import (
	"context"
	"errors"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidSchedule          = errors.New("invalid schedule")
	ErrScheduleOverlap          = errors.New("schedule windows overlap")
	ErrWorkforceVersionConflict = errors.New("workforce version conflict")
	ErrScheduleEffectiveOverlap = errors.New("schedule effective range overlaps or is not increasing")
)

type ExceptionState string

const (
	Available   ExceptionState = "available"
	Unavailable ExceptionState = "unavailable"
)

type ScheduleException struct {
	ID                                        string
	ExceptionOn                               time.Time
	State                                     ExceptionState
	AllDay                                    bool
	StartsMinute, EndsMinute, CapacityPercent int
	Reason                                    string
	Version                                   int64
}

type WeeklyWindow struct {
	ID                                        string
	Weekday                                   time.Weekday
	StartsMinute, EndsMinute, CapacityPercent int
}
type Schedule struct {
	ID, MSPID, TechnicianID, Timezone, CreatedBy string
	EffectiveFrom                                time.Time
	EffectiveThrough                             *time.Time
	Version                                      int64
	Windows                                      []WeeklyWindow
	Exceptions                                   []ScheduleException
	CreatedAt                                    time.Time
}
type PublishScheduleCommand struct {
	Principal                                               authorization.Principal
	TechnicianID, Timezone, ActorID, Source, IdempotencyKey string
	EffectiveFrom                                           time.Time
	EffectiveThrough                                        *time.Time
	ExpectedVersion                                         int64
	Windows                                                 []WeeklyWindow
	Exceptions                                              []ScheduleException
}
type AddScheduleExceptionCommand struct {
	Principal       authorization.Principal
	ScheduleID      string
	ExpectedVersion int64
	Exception       ScheduleException
	ActorID         string
	Source          string
	IdempotencyKey  string
}
type ScheduleMutation struct {
	Schedule                                      Schedule
	ExpectedVersion                               int64
	Audit                                         mutation.AuditRecord
	Event                                         mutation.EventRecord
	IdempotencyKey, RequestFingerprint, RequestID string
}
type ScheduleExceptionMutation struct {
	Schedule                                      Schedule
	Exception                                     ScheduleException
	ExpectedVersion                               int64
	Audit                                         mutation.AuditRecord
	Event                                         mutation.EventRecord
	IdempotencyKey, RequestFingerprint, RequestID string
}
type ScheduleRepository interface {
	TechnicianInMSP(context.Context, string, string) (bool, error)
	CurrentScheduleVersion(context.Context, string, string) (int64, error)
	CurrentSchedule(context.Context, string, string) (Schedule, error)
	FindSchedule(context.Context, string, string) (Schedule, error)
	PublishScheduleAtomic(context.Context, ScheduleMutation) error
	AddScheduleExceptionAtomic(context.Context, ScheduleExceptionMutation) error
}

func (s *ScheduleService) AddException(ctx context.Context, c AddScheduleExceptionCommand) (Schedule, error) {
	actor := c.Principal.ID
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || !validWorkforcePrincipal(c.Principal) || !internalid.ValidCanonical(c.ScheduleID) || c.ExpectedVersion < 1 || actor == "" || (c.ActorID != "" && c.ActorID != actor) || c.Source == "" || !mutation.ValidIdempotencyKey(c.IdempotencyKey) {
		return Schedule{}, ErrInvalidSchedule
	}
	if err := authorization.Authorize(c.Principal, "calendar.workforce.manage", scope.Target{MSPID: c.Principal.Scope.MSPID}); err != nil {
		return Schedule{}, err
	}
	schedule, err := s.repository.FindSchedule(ctx, c.Principal.Scope.MSPID, c.ScheduleID)
	if err != nil {
		return Schedule{}, err
	}
	if schedule.Version != c.ExpectedVersion {
		return Schedule{}, ErrWorkforceVersionConflict
	}
	exception := c.Exception
	exception.ExceptionOn = date(exception.ExceptionOn)
	if exception.ExceptionOn.IsZero() || exception.ExceptionOn.Before(date(schedule.EffectiveFrom)) || schedule.EffectiveThrough != nil && exception.ExceptionOn.After(date(*schedule.EffectiveThrough)) || exception.State != Available && exception.State != Unavailable {
		return Schedule{}, ErrInvalidSchedule
	}
	if exception.AllDay {
		if exception.StartsMinute != 0 || exception.EndsMinute != 0 {
			return Schedule{}, ErrInvalidSchedule
		}
	} else if exception.StartsMinute < 0 || exception.EndsMinute > 1440 || exception.EndsMinute <= exception.StartsMinute {
		return Schedule{}, ErrInvalidSchedule
	}
	if exception.CapacityPercent == 0 && exception.State == Available {
		exception.CapacityPercent = 100
	}
	if exception.CapacityPercent < 0 || exception.CapacityPercent > 100 || exception.State == Available && exception.CapacityPercent == 0 {
		return Schedule{}, ErrInvalidSchedule
	}
	for _, existing := range schedule.Exceptions {
		if !date(existing.ExceptionOn).Equal(exception.ExceptionOn) {
			continue
		}
		if existing.AllDay || exception.AllDay || exception.StartsMinute < existing.EndsMinute && existing.StartsMinute < exception.EndsMinute {
			return Schedule{}, ErrScheduleOverlap
		}
	}
	exception.ID = s.newID()
	exception.Version = 1
	now := s.now().UTC()
	result := schedule
	result.Version = c.ExpectedVersion + 1
	result.Exceptions = append(append([]ScheduleException(nil), schedule.Exceptions...), exception)
	fingerprintException := exception
	fingerprintException.ID = ""
	fingerprintException.Version = 0
	fingerprint, err := mutation.Fingerprint(struct {
		ScheduleID      string
		ExpectedVersion int64
		Exception       ScheduleException
	}{schedule.ID, c.ExpectedVersion, fingerprintException})
	if err != nil {
		return Schedule{}, err
	}
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := ScheduleExceptionMutation{
		Schedule:        result,
		Exception:       exception,
		ExpectedVersion: c.ExpectedVersion,
		Audit:           mutation.AuditRecord{ID: auditID, OccurredAt: now, MSPID: schedule.MSPID, ActorType: "technician", ActorID: actor, Action: "technician.schedule.exception.added", SubjectType: "technician_schedule", SubjectID: schedule.ID, SubjectVersion: result.Version, Source: c.Source, CorrelationID: correlationID},
		Event:           mutation.EventRecord{EventID: eventID, EventType: "technician.schedule.exception.added", SchemaVersion: 1, OccurredAt: now, MSPID: schedule.MSPID, ActorType: "technician", ActorID: actor, SubjectType: "technician_schedule", SubjectID: schedule.ID, SubjectVersion: result.Version, Source: c.Source, CorrelationID: correlationID, Data: map[string]any{"technician_id": schedule.TechnicianID, "exception_id": exception.ID}},
		IdempotencyKey:  c.IdempotencyKey, RequestFingerprint: fingerprint, RequestID: s.newID(),
	}
	if err = s.repository.AddScheduleExceptionAtomic(ctx, accepted); err != nil {
		if replay, decodeErr := mutation.DecodeReplay(err, &result); replay {
			return result, decodeErr
		}
		return Schedule{}, err
	}
	return result, nil
}

type ScheduleService struct {
	repository ScheduleRepository
	now        func() time.Time
	newID      func() string
}

func NewScheduleService(r ScheduleRepository, n func() time.Time, i func() string) *ScheduleService {
	return &ScheduleService{r, n, i}
}
func (s *ScheduleService) Publish(ctx context.Context, c PublishScheduleCommand) (Schedule, error) {
	actor := c.Principal.ID
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || !validWorkforcePrincipal(c.Principal) || !internalid.ValidCanonical(c.TechnicianID) || actor == "" || (c.ActorID != "" && c.ActorID != actor) || c.Source == "" || !mutation.ValidIdempotencyKey(c.IdempotencyKey) || c.EffectiveFrom.IsZero() || len(c.Windows) == 0 {
		return Schedule{}, ErrInvalidSchedule
	}
	c.ActorID = actor
	if err := authorization.Authorize(c.Principal, "calendar.workforce.manage", scope.Target{MSPID: c.Principal.Scope.MSPID}); err != nil {
		return Schedule{}, err
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return Schedule{}, ErrInvalidSchedule
	}
	w := append([]WeeklyWindow(nil), c.Windows...)
	sort.Slice(w, func(i, j int) bool {
		if w[i].Weekday != w[j].Weekday {
			return w[i].Weekday < w[j].Weekday
		}
		return w[i].StartsMinute < w[j].StartsMinute
	})
	for i := range w {
		if w[i].Weekday < time.Sunday || w[i].Weekday > time.Saturday || w[i].StartsMinute < 0 || w[i].EndsMinute > 1440 || w[i].EndsMinute <= w[i].StartsMinute {
			return Schedule{}, ErrInvalidSchedule
		}
		if w[i].CapacityPercent == 0 {
			w[i].CapacityPercent = 100
		}
		if w[i].CapacityPercent < 1 || w[i].CapacityPercent > 100 {
			return Schedule{}, ErrInvalidSchedule
		}
		if i > 0 && w[i-1].Weekday == w[i].Weekday && w[i].StartsMinute < w[i-1].EndsMinute {
			return Schedule{}, ErrScheduleOverlap
		}
		w[i].ID = s.newID()
	}
	exceptions := append([]ScheduleException(nil), c.Exceptions...)
	sort.Slice(exceptions, func(i, j int) bool {
		if exceptions[i].ExceptionOn.Equal(exceptions[j].ExceptionOn) {
			return exceptions[i].StartsMinute < exceptions[j].StartsMinute
		}
		return exceptions[i].ExceptionOn.Before(exceptions[j].ExceptionOn)
	})
	for i := range exceptions {
		e := &exceptions[i]
		e.ExceptionOn = date(e.ExceptionOn)
		if e.ExceptionOn.Before(date(c.EffectiveFrom)) || c.EffectiveThrough != nil && e.ExceptionOn.After(date(*c.EffectiveThrough)) || (e.State != Available && e.State != Unavailable) {
			return Schedule{}, ErrInvalidSchedule
		}
		if e.AllDay {
			if e.StartsMinute != 0 || e.EndsMinute != 0 {
				return Schedule{}, ErrInvalidSchedule
			}
		} else if e.StartsMinute < 0 || e.EndsMinute > 1440 || e.EndsMinute <= e.StartsMinute {
			return Schedule{}, ErrInvalidSchedule
		}
		if e.CapacityPercent == 0 && e.State == Available {
			e.CapacityPercent = 100
		}
		if e.CapacityPercent < 0 || e.CapacityPercent > 100 || e.State == Available && e.CapacityPercent == 0 {
			return Schedule{}, ErrInvalidSchedule
		}
		if i > 0 && exceptions[i-1].ExceptionOn.Equal(e.ExceptionOn) && (exceptions[i-1].AllDay || e.AllDay || e.StartsMinute < exceptions[i-1].EndsMinute) {
			return Schedule{}, ErrScheduleOverlap
		}
		e.ID = s.newID()
		e.Version = 1
	}
	if c.EffectiveThrough != nil && date(*c.EffectiveThrough).Before(date(c.EffectiveFrom)) {
		return Schedule{}, ErrInvalidSchedule
	}
	ok, err := s.repository.TechnicianInMSP(ctx, c.Principal.Scope.MSPID, c.TechnicianID)
	if err != nil {
		return Schedule{}, err
	}
	if !ok {
		return Schedule{}, scope.ErrNotFound
	}
	currentSchedule, err := s.repository.CurrentSchedule(ctx, c.Principal.Scope.MSPID, c.TechnicianID)
	if err != nil {
		return Schedule{}, err
	}
	current := currentSchedule.Version
	if current == c.ExpectedVersion && current > 0 && !date(c.EffectiveFrom).After(date(currentSchedule.EffectiveFrom)) {
		return Schedule{}, ErrScheduleEffectiveOverlap
	}
	now := s.now().UTC()
	found := Schedule{ID: s.newID(), MSPID: c.Principal.Scope.MSPID, TechnicianID: c.TechnicianID, Timezone: strings.TrimSpace(c.Timezone), EffectiveFrom: date(c.EffectiveFrom), EffectiveThrough: calendarDate(c.EffectiveThrough), Version: c.ExpectedVersion + 1, Windows: w, Exceptions: exceptions, CreatedAt: now, CreatedBy: actor}
	fingerprintWindows := append([]WeeklyWindow(nil), found.Windows...)
	for i := range fingerprintWindows {
		fingerprintWindows[i].ID = ""
	}
	fingerprintExceptions := append([]ScheduleException(nil), found.Exceptions...)
	for i := range fingerprintExceptions {
		fingerprintExceptions[i].ID = ""
		fingerprintExceptions[i].Version = 0
	}
	fingerprint, err := mutation.Fingerprint(struct {
		TechnicianID, Timezone string
		EffectiveFrom          time.Time
		EffectiveThrough       *time.Time
		Windows                []WeeklyWindow
		Exceptions             []ScheduleException
	}{found.TechnicianID, found.Timezone, found.EffectiveFrom, found.EffectiveThrough, fingerprintWindows, fingerprintExceptions})
	if err != nil {
		return Schedule{}, err
	}
	a, e, cor := s.newID(), s.newID(), s.newID()
	accepted := ScheduleMutation{Schedule: found, ExpectedVersion: c.ExpectedVersion, Audit: mutation.AuditRecord{ID: a, OccurredAt: now, MSPID: found.MSPID, ActorType: "technician", ActorID: actor, Action: "technician.schedule.published", SubjectType: "technician_schedule", SubjectID: found.ID, SubjectVersion: found.Version, Source: c.Source, CorrelationID: cor}, Event: mutation.EventRecord{EventID: e, EventType: "technician.schedule.published", SchemaVersion: 1, OccurredAt: now, MSPID: found.MSPID, ActorType: "technician", ActorID: actor, SubjectType: "technician_schedule", SubjectID: found.ID, SubjectVersion: found.Version, Source: c.Source, CorrelationID: cor, Data: map[string]any{"technician_id": found.TechnicianID}}, IdempotencyKey: c.IdempotencyKey, RequestFingerprint: fingerprint, RequestID: s.newID()}
	if err = s.repository.PublishScheduleAtomic(ctx, accepted); err != nil {
		if replay, decodeErr := mutation.DecodeReplay(err, &found); replay {
			return found, decodeErr
		}
		return Schedule{}, err
	}
	return found, nil
}
func calendarDate(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	d := date(*v)
	return &d
}
func date(v time.Time) time.Time {
	y, m, d := v.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
func validWorkforcePrincipal(p authorization.Principal) bool {
	return internalid.ValidCanonical(p.ID) && internalid.ValidCanonical(p.Scope.MSPID) && (p.Scope.ClientID == "" || internalid.ValidCanonical(p.Scope.ClientID))
}

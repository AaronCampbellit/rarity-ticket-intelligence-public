package customfields

import (
	"context"
	"errors"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type ObjectType string

const (
	ObjectWorkRecord       ObjectType = "work_record"
	ObjectTask             ObjectType = "task"
	ObjectProject          ObjectType = "project"
	ObjectAsset            ObjectType = "asset"
	ObjectKnowledgeArticle ObjectType = "knowledge_article"
	ObjectTimeEntry        ObjectType = "time_entry"
)

type ValueKind string

const (
	DateKind      ValueKind = "date"
	TimestampKind ValueKind = "timestamp"
)

var (
	ErrInvalidCustomDate         = errors.New("invalid custom date")
	ErrCustomDateVersionConflict = errors.New("custom date version conflict")
	ErrCustomDateBindingConflict = errors.New("custom date definition or source changed")
)

type DateDefinition struct {
	ID, MSPID, InternalKey, Label, LifecycleState string
	ObjectType                                    ObjectType
	ValueKind                                     ValueKind
	Version                                       int64
}
type SourceRecord struct {
	ID, MSPID, ClientID string
	Type                ObjectType
	Version             int64
}
type DateValue struct {
	ID, FieldID, MSPID, ClientID, ObjectID, Timezone, CreatedBy, UpdatedBy string
	ObjectType                                                             ObjectType
	SourceRevision, Version                                                int64
	DateValue, TimestampValue                                              *time.Time
	CreatedAt, UpdatedAt                                                   time.Time
}
type SetDateCommand struct {
	Principal                                                    authorization.Principal
	ObjectType                                                   ObjectType
	ObjectID, FieldID, Timezone, ActorID, Source, IdempotencyKey string
	DateValue, TimestampValue                                    *time.Time
	ExpectedVersion                                              int64
}
type DateMutation struct {
	Definition                                    DateDefinition
	Source                                        SourceRecord
	Value                                         DateValue
	ExpectedVersion                               int64
	Audit                                         mutation.AuditRecord
	Event                                         mutation.EventRecord
	IdempotencyKey, RequestFingerprint, RequestID string
}
type DateRepository interface {
	FindDateDefinition(context.Context, string, string, ObjectType) (DateDefinition, error)
	FindDateSource(context.Context, scope.Target, ObjectType, string) (SourceRecord, error)
	FindDateValue(context.Context, scope.Target, string, ObjectType, string) (DateValue, error)
	SetDateAtomic(context.Context, DateMutation) error
}
type DateService struct {
	repository DateRepository
	now        func() time.Time
	newID      func() string
}

func NewDateService(r DateRepository, n func() time.Time, i func() string) *DateService {
	return &DateService{r, n, i}
}
func (s *DateService) Set(ctx context.Context, c SetDateCommand) (DateValue, error) {
	actor := c.Principal.ID
	if s == nil || s.repository == nil || !validObject(c.ObjectType) || !validDatePrincipal(c.Principal) || !internalid.ValidCanonical(c.ObjectID) || !internalid.ValidCanonical(c.FieldID) || actor == "" || (c.ActorID != "" && c.ActorID != actor) || c.Source == "" || !mutation.ValidIdempotencyKey(c.IdempotencyKey) || ((c.DateValue == nil) == (c.TimestampValue == nil)) || c.ExpectedVersion < 0 {
		return DateValue{}, ErrInvalidCustomDate
	}
	if c.TimestampValue != nil {
		if _, err := time.LoadLocation(c.Timezone); err != nil {
			return DateValue{}, ErrInvalidCustomDate
		}
	} else if c.Timezone != "" {
		return DateValue{}, ErrInvalidCustomDate
	}
	def, err := s.repository.FindDateDefinition(ctx, c.Principal.Scope.MSPID, c.FieldID, c.ObjectType)
	if err != nil {
		return DateValue{}, err
	}
	if def.MSPID != c.Principal.Scope.MSPID || def.ObjectType != c.ObjectType || def.LifecycleState != "active" {
		return DateValue{}, scope.ErrNotFound
	}
	if (def.ValueKind == DateKind) != (c.DateValue != nil) {
		return DateValue{}, ErrInvalidCustomDate
	}
	lookup := scope.Target{MSPID: c.Principal.Scope.MSPID, ClientID: c.Principal.Scope.ClientID}
	src, err := s.repository.FindDateSource(ctx, lookup, c.ObjectType, c.ObjectID)
	if err != nil {
		return DateValue{}, err
	}
	if src.ID != c.ObjectID || src.Type != c.ObjectType || src.MSPID != c.Principal.Scope.MSPID {
		return DateValue{}, scope.ErrNotFound
	}
	target := scope.Target{MSPID: src.MSPID, ClientID: src.ClientID}
	if err = authorization.Authorize(c.Principal, editCapability(c.ObjectType), target); err != nil {
		return DateValue{}, err
	}
	now := s.now().UTC()
	valueID, createdAt, createdBy := s.newID(), now, actor
	if c.ExpectedVersion > 0 {
		current, loadErr := s.repository.FindDateValue(ctx, target, def.ID, src.Type, src.ID)
		if loadErr != nil {
			return DateValue{}, loadErr
		}
		if current.MSPID != src.MSPID || current.ClientID != src.ClientID {
			return DateValue{}, ErrCustomDateVersionConflict
		}
		valueID, createdAt, createdBy = current.ID, current.CreatedAt, current.CreatedBy
	}
	version := c.ExpectedVersion + 1
	dateValue := calendar.NormalizeDatePointer(c.DateValue)
	var timestampValue *time.Time
	if c.TimestampValue != nil {
		utc := c.TimestampValue.UTC()
		timestampValue = &utc
	}
	value := DateValue{ID: valueID, FieldID: def.ID, MSPID: src.MSPID, ClientID: src.ClientID, ObjectType: src.Type, ObjectID: src.ID, Timezone: c.Timezone, SourceRevision: src.Version, Version: version, DateValue: dateValue, TimestampValue: timestampValue, CreatedAt: createdAt, UpdatedAt: now, CreatedBy: createdBy, UpdatedBy: actor}
	fingerprint, err := mutation.Fingerprint(struct {
		ObjectType                  ObjectType
		ObjectID, FieldID, Timezone string
		DateValue, TimestampValue   *time.Time
		ExpectedVersion             int64
	}{value.ObjectType, value.ObjectID, value.FieldID, value.Timezone, value.DateValue, value.TimestampValue, c.ExpectedVersion})
	if err != nil {
		return DateValue{}, err
	}
	a, e, cor := s.newID(), s.newID(), s.newID()
	m := DateMutation{Definition: def, Source: src, Value: value, ExpectedVersion: c.ExpectedVersion, Audit: mutation.AuditRecord{ID: a, OccurredAt: now, MSPID: src.MSPID, ClientID: src.ClientID, ActorType: "technician", ActorID: actor, Action: "custom_date.set", SubjectType: string(src.Type), SubjectID: src.ID, SubjectVersion: src.Version, Source: c.Source, CorrelationID: cor}, Event: mutation.EventRecord{EventID: e, EventType: "custom_date.set", SchemaVersion: 1, OccurredAt: now, MSPID: src.MSPID, ClientID: src.ClientID, ActorType: "technician", ActorID: actor, SubjectType: string(src.Type), SubjectID: src.ID, SubjectVersion: src.Version, Source: c.Source, CorrelationID: cor, Data: map[string]any{"field_id": def.ID, "definition_version": def.Version, "value_version": version}}, IdempotencyKey: c.IdempotencyKey, RequestFingerprint: fingerprint, RequestID: s.newID()}
	if err = s.repository.SetDateAtomic(ctx, m); err != nil {
		if replay, decodeErr := mutation.DecodeReplay(err, &value); replay {
			return value, decodeErr
		}
		return DateValue{}, err
	}
	return value, nil
}
func validObject(v ObjectType) bool {
	switch v {
	case ObjectWorkRecord, ObjectTask, ObjectProject, ObjectAsset, ObjectKnowledgeArticle, ObjectTimeEntry:
		return true
	}
	return false
}
func editCapability(v ObjectType) string {
	switch v {
	case ObjectWorkRecord:
		return "work_record.edit"
	case ObjectTask:
		return "task.edit"
	case ObjectProject:
		return "project.edit"
	case ObjectAsset:
		return "asset.edit"
	case ObjectKnowledgeArticle:
		return "knowledge.edit"
	case ObjectTimeEntry:
		return "time_entry.edit"
	}
	return ""
}
func validDatePrincipal(p authorization.Principal) bool {
	return internalid.ValidCanonical(p.ID) && internalid.ValidCanonical(p.Scope.MSPID) && (p.Scope.ClientID == "" || internalid.ValidCanonical(p.Scope.ClientID))
}

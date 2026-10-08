package customfields

import (
	"context"
	"errors"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"testing"
	"time"
)

const (
	dateActorID  = "00000000-0000-4000-8000-000000000301"
	dateMSPID    = "00000000-0000-4000-8000-000000000302"
	dateClientID = "00000000-0000-4000-8000-000000000303"
	dateFieldID  = "00000000-0000-4000-8000-000000000304"
	dateObjectID = "00000000-0000-4000-8000-000000000305"
	dateValueID  = "00000000-0000-4000-8000-000000000306"
)

type dateRepositoryStub struct {
	definition    DateDefinition
	source        SourceRecord
	value         DateValue
	mutation      DateMutation
	sourceVisible bool
}

func (r *dateRepositoryStub) FindDateDefinition(context.Context, string, string, ObjectType) (DateDefinition, error) {
	return r.definition, nil
}
func (r *dateRepositoryStub) FindDateSource(context.Context, scope.Target, ObjectType, string) (SourceRecord, error) {
	if !r.sourceVisible {
		return SourceRecord{}, scope.ErrNotFound
	}
	return r.source, nil
}
func (r *dateRepositoryStub) FindDateValue(context.Context, scope.Target, string, ObjectType, string) (DateValue, error) {
	return r.value, nil
}
func (r *dateRepositoryStub) SetDateAtomic(_ context.Context, m DateMutation) error {
	r.mutation = m
	if m.ExpectedVersion > 0 && r.value.Version != m.ExpectedVersion {
		return ErrCustomDateVersionConflict
	}
	return nil
}
func datePrincipal(cap string) authorization.Principal {
	return authorization.Principal{ID: dateActorID, Scope: scope.Principal{MSPID: dateMSPID, ClientID: dateClientID}, Capabilities: authorization.NewCapabilitySet(cap)}
}
func TestCustomDateValueRequiresExactSourceAuthorization(t *testing.T) {
	s := NewDateService(&dateRepositoryStub{sourceVisible: false}, time.Now, func() string { return dateValueID })
	d := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	_, err := s.Set(context.Background(), SetDateCommand{Principal: datePrincipal("work_record.edit"), ObjectType: ObjectWorkRecord, ObjectID: dateObjectID, FieldID: dateFieldID, DateValue: &d, ActorID: dateActorID, Source: "api", IdempotencyKey: "date-foreign"})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("error=%v", err)
	}
}
func TestCustomDateEnforcesDefinitionKindAndSixTypedSources(t *testing.T) {
	d := time.Now()
	r := &dateRepositoryStub{sourceVisible: true, definition: DateDefinition{ID: dateFieldID, MSPID: dateMSPID, ObjectType: ObjectWorkRecord, ValueKind: DateKind, LifecycleState: "active", Version: 1}, source: SourceRecord{ID: dateObjectID, MSPID: dateMSPID, ClientID: dateClientID, Type: ObjectWorkRecord, Version: 3}}
	s := NewDateService(r, time.Now, func() string { return dateValueID })
	if _, err := s.Set(context.Background(), SetDateCommand{Principal: datePrincipal("work_record.edit"), ObjectType: ObjectType("generic_event"), ObjectID: dateObjectID, FieldID: dateFieldID, DateValue: &d, ActorID: dateActorID, Source: "api", IdempotencyKey: "date-generic"}); !errors.Is(err, ErrInvalidCustomDate) {
		t.Fatalf("generic source=%v", err)
	}
	ts := d.Add(time.Hour)
	if _, err := s.Set(context.Background(), SetDateCommand{Principal: datePrincipal("work_record.edit"), ObjectType: ObjectWorkRecord, ObjectID: dateObjectID, FieldID: dateFieldID, TimestampValue: &ts, Timezone: "UTC", ActorID: dateActorID, Source: "api", IdempotencyKey: "date-kind"}); !errors.Is(err, ErrInvalidCustomDate) {
		t.Fatalf("kind mismatch=%v", err)
	}
}

func TestCustomDateUpdateUsesExistingIdentityAndRejectsStaleVersion(t *testing.T) {
	d := time.Now()
	r := &dateRepositoryStub{sourceVisible: true, definition: DateDefinition{ID: dateFieldID, MSPID: dateMSPID, ObjectType: ObjectWorkRecord, ValueKind: DateKind, LifecycleState: "active", Version: 1}, source: SourceRecord{ID: dateObjectID, MSPID: dateMSPID, ClientID: dateClientID, Type: ObjectWorkRecord, Version: 4}, value: DateValue{ID: dateValueID, FieldID: dateFieldID, MSPID: dateMSPID, ClientID: dateClientID, ObjectType: ObjectWorkRecord, ObjectID: dateObjectID, Version: 2}}
	s := NewDateService(r, time.Now, func() string { return "00000000-0000-4000-8000-000000000307" })
	found, err := s.Set(context.Background(), SetDateCommand{Principal: datePrincipal("work_record.edit"), ObjectType: ObjectWorkRecord, ObjectID: dateObjectID, FieldID: dateFieldID, DateValue: &d, ExpectedVersion: 2, ActorID: dateActorID, Source: "api", IdempotencyKey: "date-update"})
	if err != nil || found.ID != dateValueID {
		t.Fatalf("found=%+v err=%v", found, err)
	}
	r.value.Version = 3
	if _, err = s.Set(context.Background(), SetDateCommand{Principal: datePrincipal("work_record.edit"), ObjectType: ObjectWorkRecord, ObjectID: dateObjectID, FieldID: dateFieldID, DateValue: &d, ExpectedVersion: 2, ActorID: dateActorID, Source: "api", IdempotencyKey: "date-stale"}); !errors.Is(err, ErrCustomDateVersionConflict) {
		t.Fatalf("stale error=%v", err)
	}
}

func TestCustomDateUsesAuthenticatedActorAndNormalizesDate(t *testing.T) {
	d := time.Date(2026, 9, 1, 23, 45, 0, 0, time.FixedZone("west", -7*60*60))
	r := &dateRepositoryStub{sourceVisible: true, definition: DateDefinition{ID: dateFieldID, MSPID: dateMSPID, ObjectType: ObjectWorkRecord, ValueKind: DateKind, LifecycleState: "active", Version: 3}, source: SourceRecord{ID: dateObjectID, MSPID: dateMSPID, ClientID: dateClientID, Type: ObjectWorkRecord, Version: 4}}
	s := NewDateService(r, time.Now, func() string { return dateValueID })
	if _, err := s.Set(context.Background(), SetDateCommand{Principal: datePrincipal("work_record.edit"), ObjectType: ObjectWorkRecord, ObjectID: dateObjectID, FieldID: dateFieldID, DateValue: &d, ActorID: "00000000-0000-4000-8000-000000000399", Source: "api", IdempotencyKey: "date-impersonation"}); !errors.Is(err, ErrInvalidCustomDate) {
		t.Fatalf("impersonation error=%v", err)
	}
	found, err := s.Set(context.Background(), SetDateCommand{Principal: datePrincipal("work_record.edit"), ObjectType: ObjectWorkRecord, ObjectID: dateObjectID, FieldID: dateFieldID, DateValue: &d, Source: "api", IdempotencyKey: "date-normalized"})
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if found.DateValue == nil || !found.DateValue.Equal(want) || r.mutation.Definition.Version != 3 || r.mutation.Source.Version != 4 {
		t.Fatalf("found=%+v mutation=%+v", found, r.mutation)
	}
}

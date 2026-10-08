package calendar

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

const (
	configurationMSP   = "11111111-1111-4111-8111-111111111111"
	configurationActor = "22222222-2222-4222-8222-222222222222"
)

type configurationRepositoryStub struct {
	policyVersion int64
	field         CustomDateField
	policies      ConflictPolicyMutation
	custom        CustomDateFieldMutation
}

func (s *configurationRepositoryStub) CurrentConflictPolicyVersion(context.Context, string, ConflictPolicyScope) (int64, error) {
	return s.policyVersion, nil
}
func (s *configurationRepositoryStub) ReplaceConflictPoliciesAtomic(_ context.Context, mutation ConflictPolicyMutation) error {
	s.policies = mutation
	return nil
}
func (s *configurationRepositoryStub) FindCustomDateField(context.Context, string, string, string) (CustomDateField, error) {
	return s.field, nil
}
func (s *configurationRepositoryStub) UpsertCustomDateFieldAtomic(_ context.Context, mutation CustomDateFieldMutation) error {
	s.custom = mutation
	return nil
}

func TestCustomDateConfigurationCannotInventTimeForDateField(t *testing.T) {
	service := NewConfigurationService(&configurationRepositoryStub{}, configurationFixedNow, configurationSequentialIDs())
	_, err := service.UpsertCustomDateField(context.Background(), UpsertCustomDateCommand{
		Principal: configurationPrincipal(), ObjectType: "task", FieldID: "follow_up_on", FieldType: FieldDate,
		Label: "Follow up", Category: "operations", SchedulingMode: FixedBlock, TimezoneSource: "America/Chicago",
		Source: "test", ExpectedVersion: 0,
	})
	if !errors.Is(err, ErrInvalidCustomDateConfiguration) {
		t.Fatalf("error = %v, want invalid custom date configuration", err)
	}
}

func TestConflictPolicyReplacementAcceptsOnlyKnownKindsAndSeverities(t *testing.T) {
	repository := &configurationRepositoryStub{}
	service := NewConfigurationService(repository, configurationFixedNow, configurationSequentialIDs())
	for index, severity := range []ConflictSeverity{ConflictInfo, ConflictWarning, ConflictOverrideable, ConflictHard} {
		_, err := service.ReplaceConflictPolicies(context.Background(), ReplaceConflictPoliciesCommand{
			Principal: configurationPrincipal(), Scope: ConflictPolicyScope{Type: ConflictScopeMSP},
			Rules: []ConflictPolicyRule{{Kind: ConflictKindList[index], Severity: severity}}, ExpectedVersion: int64(index), Source: "test",
		})
		if err != nil {
			t.Fatalf("severity %q: %v", severity, err)
		}
		repository.policyVersion++
	}
	_, err := service.ReplaceConflictPolicies(context.Background(), ReplaceConflictPoliciesCommand{
		Principal: configurationPrincipal(), Scope: ConflictPolicyScope{Type: ConflictScopeMSP},
		Rules: []ConflictPolicyRule{{Kind: "private_note", Severity: ConflictHard}}, ExpectedVersion: 4, Source: "test",
	})
	if !errors.Is(err, ErrInvalidConflictPolicy) {
		t.Fatalf("unknown kind error = %v", err)
	}
}

func TestConfigurationWritesAuditAndOutboxWithoutBackfill(t *testing.T) {
	repository := &configurationRepositoryStub{}
	service := NewConfigurationService(repository, configurationFixedNow, configurationSequentialIDs())
	found, err := service.UpsertCustomDateField(context.Background(), UpsertCustomDateCommand{
		Principal: configurationPrincipal(), ObjectType: "task", FieldID: "scheduled_follow_up", FieldType: FieldDateTime,
		Label: "Scheduled follow up", Category: "operations", SchedulingMode: EffortAllocation, CapacityBearing: true,
		TimezoneSource: "client", PlannedEffortSource: "task.estimate_minutes", Source: "test", ExpectedVersion: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if found.Version != 1 || repository.custom.Audit.Action != "calendar.custom_date_field.upserted" || repository.custom.Event.EventType != "calendar.custom_date_field.upserted" {
		t.Fatalf("field/mutation = %+v / %+v", found, repository.custom)
	}
	if _, exists := repository.custom.Event.Data["backfill"]; exists {
		t.Fatal("configuration event must not request backfill")
	}
}

func TestCustomDateFieldValidationRejectsMissingPlannedEffortSource(t *testing.T) {
	field := CustomDateField{ID: "00000000-0000-4000-8000-000000000001", MSPID: configurationMSP, ObjectType: "task", FieldID: "scheduled", Label: "Scheduled", Category: "operations", FieldType: FieldDateTime, SchedulingMode: EffortAllocation, CapacityBearing: true, TimezoneSource: "client", Version: 1}
	if !errors.Is(field.Validate(), ErrInvalidCustomDateConfiguration) {
		t.Fatalf("validation=%v", field.Validate())
	}
}

func configurationPrincipal() authorization.Principal {
	return authorization.Principal{ID: configurationActor, Scope: scope.Principal{MSPID: configurationMSP}, Capabilities: authorization.NewCapabilitySet("calendar.policy.manage")}
}

func configurationFixedNow() time.Time { return time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC) }

func configurationSequentialIDs() func() string {
	index := 0
	return func() string {
		index++
		return fmt.Sprintf("00000000-0000-4000-8000-%012d", index)
	}
}

package calendar

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestRoleRegistryRejectsDuplicateAndGenericRoles(t *testing.T) {
	registry := NewRoleRegistry()
	definition := EventRoleDefinition{
		SourceType: "task", Role: "scheduled_work",
		SchedulingMode: FixedBlock, CapacityBearing: true,
		DependencyEligible: true,
	}
	if err := registry.Register(definition); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(registry.Register(definition), ErrDuplicateRole) {
		t.Fatal("duplicate role was accepted")
	}
	if err := registry.Register(EventRoleDefinition{
		SourceType: "generic_event", Role: "event",
	}); !errors.Is(err, ErrInvalidRole) {
		t.Fatal("generic event role was accepted")
	}
}

func TestRoleRegistryRejectsInvalidRoleContracts(t *testing.T) {
	tests := []EventRoleDefinition{
		{SourceType: "task", Role: "", SchedulingMode: Informational},
		{SourceType: "task", Role: "Due Date", SchedulingMode: Informational},
		{SourceType: "unknown", Role: "due", SchedulingMode: Informational},
		{SourceType: "task", Role: "due", SchedulingMode: SchedulingMode("movable")},
		{SourceType: "task", Role: "due", SchedulingMode: Informational, CapacityBearing: true},
		{SourceType: "task", Role: "due", SchedulingMode: Informational, DependencyEligible: true},
		{SourceType: "task", Role: "event", SchedulingMode: Informational},
	}
	for _, definition := range tests {
		registry := NewRoleRegistry()
		err := registry.Register(definition)
		if !errors.Is(err, ErrInvalidRole) {
			t.Errorf("Register(%+v) = %v, want ErrInvalidRole", definition, err)
		}
	}
}

func TestProductionRoleRegistryMakesMilestonesDependencyEligible(t *testing.T) {
	registry, err := NewProductionRoleRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	definition, ok := registry.DefinitionForProjection(Projection{Source: SourceRef{Type: "milestone"}, EventRole: "milestone", AllDay: true, StartsOn: &date, SchedulingMode: Informational})
	if !ok || !definition.DependencyEligible {
		t.Fatalf("milestone definition=%+v found=%v", definition, ok)
	}
}

func TestRoleRegistryDefinitionsAreStableCopies(t *testing.T) {
	registry := NewRoleRegistry()
	for _, definition := range []EventRoleDefinition{
		{SourceType: "task", Role: "due", SchedulingMode: Informational},
		{SourceType: "project", Role: "planned_start", SchedulingMode: Informational},
		{SourceType: "task", Role: "scheduled_work", SchedulingMode: EffortAllocation, CapacityBearing: true},
	} {
		if err := registry.Register(definition); err != nil {
			t.Fatal(err)
		}
	}
	found := registry.Definitions()
	want := []EventRoleDefinition{
		{SourceType: "project", Role: "planned_start", SchedulingMode: Informational},
		{SourceType: "task", Role: "due", SchedulingMode: Informational},
		{SourceType: "task", Role: "scheduled_work", SchedulingMode: EffortAllocation, CapacityBearing: true},
	}
	if !reflect.DeepEqual(found, want) {
		t.Fatalf("Definitions() = %+v, want %+v", found, want)
	}
	found[0].Role = "mutated"
	if registry.Definitions()[0].Role != "planned_start" {
		t.Fatal("Definitions exposed mutable registry storage")
	}
}

func TestZeroValueRoleRegistryCanRegister(t *testing.T) {
	var registry RoleRegistry
	definition := EventRoleDefinition{SourceType: "task", Role: "due", SchedulingMode: Informational}
	if err := registry.Register(definition); err != nil {
		t.Fatalf("zero-value Register() = %v", err)
	}
	if found := registry.Definitions(); !reflect.DeepEqual(found, []EventRoleDefinition{definition}) {
		t.Fatalf("Definitions() = %+v, want %+v", found, []EventRoleDefinition{definition})
	}
}

func TestRoleRegistryRejectsSourceRoleMismatch(t *testing.T) {
	registry := NewRoleRegistry()
	for _, definition := range []EventRoleDefinition{
		{SourceType: "task", Role: "milestone", SchedulingMode: Informational},
		{SourceType: "project", Role: "due", SchedulingMode: Informational},
		{SourceType: "maintenance_window", Role: "event", SchedulingMode: Informational},
	} {
		if err := registry.Register(definition); !errors.Is(err, ErrInvalidRole) {
			t.Errorf("Register(%+v) = %v, want ErrInvalidRole", definition, err)
		}
	}
}

func TestRoleRegistryValidatesConfiguredCustomDateProjection(t *testing.T) {
	registry := NewRoleRegistry()
	definition := EventRoleDefinition{SourceType: "custom_date", Role: "warranty_expiration", SourceRoleKey: "field_warranty", SchedulingMode: Informational, ReadOnly: true}
	if err := registry.Register(definition); err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	projection := Projection{
		ID: "custom-1", Source: SourceRef{MSPID: "msp", Type: "custom_date", ID: "value-1"},
		EventRole: "warranty_expiration", SourceRoleKey: "field_warranty", SourceRevision: 1,
		Title: "Warranty", AllDay: true, StartsOn: &date, SchedulingMode: Informational,
	}
	if err := registry.ValidateProjection(projection); err != nil {
		t.Fatalf("ValidateProjection() = %v", err)
	}
	projection.EventRole = "contract_expiration"
	if !errors.Is(registry.ValidateProjection(projection), ErrInvalidRole) {
		t.Fatalf("unregistered custom role accepted: %v", registry.ValidateProjection(projection))
	}
}

func TestCustomDateExpansionRequiresMatchingConfiguredRoleIdentity(t *testing.T) {
	registry := NewRoleRegistry()
	definition := EventRoleDefinition{
		SourceType: "custom_date", Role: "warranty_expiration", SourceRoleKey: "field_warranty",
		SchedulingMode: Informational, ReadOnly: true,
	}
	if err := registry.Register(definition); err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	projection := Projection{
		ID: "custom-1", Source: SourceRef{MSPID: "msp", Type: "custom_date", ID: "value-1"},
		EventRole: "warranty_expiration", SourceRoleKey: "field_warranty", SourceRevision: 1,
		Title: "Warranty", AllDay: true, StartsOn: &date, SchedulingMode: Informational,
	}
	window := QueryWindow{Start: date, End: date.AddDate(0, 0, 1)}
	if _, err := ExpandOccurrences(projection, window, nil); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("plain ExpandOccurrences() = %v, want unbound custom role rejected", err)
	}
	found, err := ExpandOccurrencesWithRegistry(registry, projection, window, nil)
	if err != nil || len(found) != 1 {
		t.Fatalf("ExpandOccurrencesWithRegistry() = (%+v, %v), want configured custom event", found, err)
	}
	projection.SourceRoleKey = "field_other"
	if _, err := ExpandOccurrencesWithRegistry(registry, projection, window, nil); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("mismatched configured key = %v, want ErrInvalidRole", err)
	}
	projection.EventRole = "contract_expiration"
	projection.SourceRoleKey = "field_contract"
	if _, err := ExpandOccurrencesWithRegistry(registry, projection, window, nil); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("unregistered configured identity = %v, want ErrInvalidRole", err)
	}
}

func TestRoleRegistryDefinitionsOrderCustomFieldIdentity(t *testing.T) {
	registry := NewRoleRegistry()
	for _, key := range []string{"field_z", "field_a"} {
		if err := registry.Register(EventRoleDefinition{
			SourceType: "custom_date", Role: "expiration", SourceRoleKey: key,
			SchedulingMode: Informational,
		}); err != nil {
			t.Fatal(err)
		}
	}
	found := registry.Definitions()
	if len(found) != 2 || found[0].SourceRoleKey != "field_a" || found[1].SourceRoleKey != "field_z" {
		t.Fatalf("Definitions() identities = %+v, want stable field-key order", found)
	}
}

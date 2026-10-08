package calendar

import (
	"context"
	"testing"
)

type adapterStub struct{ sourceType string }

type projectingAdapterStub struct {
	sourceType string
	projected  []Projection
	refs       []SourceRef
	revisions  map[string]int64
	err        error
}

func (a *projectingAdapterStub) SourceType() string { return a.sourceType }
func (a *projectingAdapterStub) Project(_ context.Context, ref SourceRef) ([]Projection, error) {
	a.refs = append(a.refs, ref)
	if a.err != nil {
		return nil, a.err
	}
	values := append([]Projection(nil), a.projected...)
	for i := range values {
		values[i].Source = ref
		if revision := a.revisions[ref.ID]; revision > 0 {
			values[i].SourceRevision = revision
		}
	}
	return values, nil
}

func (a adapterStub) SourceType() string                                       { return a.sourceType }
func (a adapterStub) Project(context.Context, SourceRef) ([]Projection, error) { return nil, nil }

func TestAdapterRegistryRejectsGenericAndDuplicateSources(t *testing.T) {
	registry := NewAdapterRegistry()
	if err := registry.Register(adapterStub{sourceType: "generic_event"}); err == nil {
		t.Fatal("generic source adapter was accepted")
	}
	if err := registry.Register(adapterStub{sourceType: "task"}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(adapterStub{sourceType: "task"}); err == nil {
		t.Fatal("duplicate source adapter was accepted")
	}
	if got, ok := registry.ForSource("task"); !ok || got.SourceType() != "task" {
		t.Fatalf("task adapter lookup = %#v, %v", got, ok)
	}
}

func TestProductionRoleDefinitionsCoverEveryTypedSourceRole(t *testing.T) {
	roles := ProductionRoleDefinitions()
	want := map[string][]string{
		"work_record": {"due", "follow_up", "scheduled_work", "sla_resolution_deadline", "sla_response_deadline"},
		"task":        {"due", "scheduled_work"}, "project": {"planned_end", "planned_start"},
		"phase": {"planned_end", "planned_start"}, "milestone": {"milestone"},
		"technician_schedule": {"availability"}, "pto": {"unavailability"},
		"maintenance_window":    {"maintenance"},
		"commercial_commitment": {"effective", "expiration", "notice", "renewal"},
	}
	got := map[string][]string{}
	for _, role := range roles {
		got[role.SourceType] = append(got[role.SourceType], role.Role)
	}
	for source, expected := range want {
		actual := got[source]
		if len(actual) != len(expected) {
			t.Fatalf("%s roles = %v, want %v", source, actual, expected)
		}
		for i := range expected {
			if actual[i] != expected[i] {
				t.Fatalf("%s roles = %v, want %v", source, actual, expected)
			}
		}
	}
}

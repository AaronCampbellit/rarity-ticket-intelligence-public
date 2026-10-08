package calendar

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrInvalidAdapter = errors.New("invalid calendar source adapter")

type SourceAdapter interface {
	SourceType() string
	Project(context.Context, SourceRef) ([]Projection, error)
}

type AdapterRegistry struct{ adapters map[string]SourceAdapter }

func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{adapters: map[string]SourceAdapter{}}
}
func (r *AdapterRegistry) Register(adapter SourceAdapter) error {
	if r == nil || adapter == nil {
		return fmt.Errorf("%w: nil adapter", ErrInvalidAdapter)
	}
	typ := strings.TrimSpace(adapter.SourceType())
	if typ == "generic_event" {
		return fmt.Errorf("%w: generic events are forbidden", ErrInvalidAdapter)
	}
	if _, ok := sourceTypes[typ]; !ok {
		return fmt.Errorf("%w: unknown source %q", ErrInvalidAdapter, typ)
	}
	if r.adapters == nil {
		r.adapters = map[string]SourceAdapter{}
	}
	if _, exists := r.adapters[typ]; exists {
		return fmt.Errorf("%w: duplicate source %q", ErrInvalidAdapter, typ)
	}
	r.adapters[typ] = adapter
	return nil
}
func (r *AdapterRegistry) ForSource(sourceType string) (SourceAdapter, bool) {
	if r == nil {
		return nil, false
	}
	a, ok := r.adapters[sourceType]
	return a, ok
}
func (r *AdapterRegistry) SourceTypes() []string {
	if r == nil {
		return nil
	}
	values := make([]string, 0, len(r.adapters))
	for typ := range r.adapters {
		values = append(values, typ)
	}
	sort.Strings(values)
	return values
}

func ProductionRoleDefinitions() []EventRoleDefinition {
	definitions := []EventRoleDefinition{
		{SourceType: "work_record", Role: "scheduled_work", SchedulingMode: FixedBlock, CapacityBearing: true, DependencyEligible: true},
		{SourceType: "work_record", Role: "due", SchedulingMode: Informational, ReadOnly: true},
		{SourceType: "work_record", Role: "follow_up", SchedulingMode: Informational, ReadOnly: true},
		{SourceType: "work_record", Role: "sla_response_deadline", SchedulingMode: Informational, ReadOnly: true},
		{SourceType: "work_record", Role: "sla_resolution_deadline", SchedulingMode: Informational, ReadOnly: true},
		{SourceType: "task", Role: "scheduled_work", SchedulingMode: FixedBlock, CapacityBearing: true, DependencyEligible: true},
		{SourceType: "task", Role: "due", SchedulingMode: Informational, ReadOnly: true},
		{SourceType: "project", Role: "planned_start", SchedulingMode: Informational},
		{SourceType: "project", Role: "planned_end", SchedulingMode: Informational},
		{SourceType: "phase", Role: "planned_start", SchedulingMode: Informational},
		{SourceType: "phase", Role: "planned_end", SchedulingMode: Informational},
		{SourceType: "milestone", Role: "milestone", SchedulingMode: Informational, DependencyEligible: true},
		{SourceType: "resource_plan", Role: "allocation", SchedulingMode: EffortAllocation, DependencyEligible: true},
		{SourceType: "technician_schedule", Role: "availability", SchedulingMode: Informational, ReadOnly: true},
		{SourceType: "pto", Role: "unavailability", SchedulingMode: Informational, ReadOnly: true},
		{SourceType: "maintenance_window", Role: "maintenance", SchedulingMode: Informational, ReadOnly: true},
		{SourceType: "commercial_commitment", Role: "effective", SchedulingMode: Informational, ReadOnly: true},
		{SourceType: "commercial_commitment", Role: "notice", SchedulingMode: Informational, ReadOnly: true},
		{SourceType: "commercial_commitment", Role: "renewal", SchedulingMode: Informational, ReadOnly: true},
		{SourceType: "commercial_commitment", Role: "expiration", SchedulingMode: Informational, ReadOnly: true},
	}
	sort.Slice(definitions, func(i, j int) bool {
		if definitions[i].SourceType == definitions[j].SourceType {
			return definitions[i].Role < definitions[j].Role
		}
		return definitions[i].SourceType < definitions[j].SourceType
	})
	return definitions
}

func NewProductionRoleRegistry(custom []EventRoleDefinition) (*RoleRegistry, error) {
	r := NewRoleRegistry()
	for _, definition := range append(ProductionRoleDefinitions(), custom...) {
		if err := r.Register(definition); err != nil {
			return nil, err
		}
	}
	return r, nil
}

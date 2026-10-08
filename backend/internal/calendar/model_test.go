package calendar

import (
	"errors"
	"testing"
	"time"
)

func TestProjectionValidatePreservesDateSemantics(t *testing.T) {
	date := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	allDay := Projection{
		ID:        "projection-1",
		Source:    SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "task-1"},
		EventRole: "due", SourceRevision: 1, Title: "Due",
		AllDay: true, StartsOn: &date, SchedulingMode: Informational,
	}
	if err := allDay.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	allDay.StartsAt = &date
	if !errors.Is(allDay.Validate(), ErrInvalidProjection) {
		t.Fatal("all-day projection accepted an invented timestamp")
	}
}

func TestProjectionValidateRequiresTypedSourceAndRealTimezone(t *testing.T) {
	start := time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC)
	base := Projection{
		ID:        "projection-1",
		Source:    SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "task-1"},
		EventRole: "scheduled_work", SourceRevision: 1, Title: "Work",
		StartsAt: &start, Timezone: "America/New_York", SchedulingMode: FixedBlock,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid timed projection: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Projection)
	}{
		{"generic source", func(p *Projection) { p.Source.Type = "generic_event" }},
		{"unknown source", func(p *Projection) { p.Source.Type = "calendar_item" }},
		{"missing source id", func(p *Projection) { p.Source.ID = "" }},
		{"fixed offset zone", func(p *Projection) { p.Timezone = "UTC-05:00" }},
		{"missing timezone", func(p *Projection) { p.Timezone = "" }},
		{"mixed date and time", func(p *Projection) { d := start; p.StartsOn = &d }},
		{"negative effort", func(p *Projection) { p.PlannedMinutes = -1 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			candidate := base
			tc.mutate(&candidate)
			if !errors.Is(candidate.Validate(), ErrInvalidProjection) {
				t.Fatalf("Validate() = %v, want ErrInvalidProjection", candidate.Validate())
			}
		})
	}
}

func TestProjectionValidateCapacityRules(t *testing.T) {
	start := time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC)
	base := Projection{
		ID:        "projection-1",
		Source:    SourceRef{MSPID: "msp", Type: "task", ID: "task-1"},
		EventRole: "scheduled_work", SourceRevision: 1, Title: "Work",
		StartsAt: &start, Timezone: "UTC", SchedulingMode: FixedBlock,
		CapacityBearing: true, AssigneeID: "tech-1", PlannedMinutes: 30,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid capacity projection: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Projection)
	}{
		{"informational capacity", func(p *Projection) { p.SchedulingMode = Informational }},
		{"capacity without assignee", func(p *Projection) { p.AssigneeID = "" }},
		{"capacity without positive effort", func(p *Projection) { p.PlannedMinutes = 0 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			candidate := base
			tc.mutate(&candidate)
			if !errors.Is(candidate.Validate(), ErrInvalidProjection) {
				t.Fatalf("Validate() = %v, want ErrInvalidProjection", candidate.Validate())
			}
		})
	}
}

func TestProjectionValidateTerminalAndWindowShapes(t *testing.T) {
	start := time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	base := Projection{
		ID:        "projection-1",
		Source:    SourceRef{MSPID: "msp", Type: "maintenance_window", ID: "maintenance-1"},
		EventRole: "maintenance", SourceRevision: 2, Title: "Maintenance",
		StartsAt: &start, EndsAt: &end, Timezone: "UTC", SchedulingMode: FixedBlock,
		TerminalState: Active,
	}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	badEnd := start
	base.EndsAt = &badEnd
	if !errors.Is(base.Validate(), ErrInvalidProjection) {
		t.Fatal("non-positive timed window accepted")
	}
	base.EndsAt = &end
	base.TerminalState = TerminalState("deleted")
	if !errors.Is(base.Validate(), ErrInvalidProjection) {
		t.Fatal("unknown terminal state accepted")
	}
}

func TestExpandOccurrencesRejectsMissingProjectionIDsBeforeTheyCanCollide(t *testing.T) {
	date := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	projections := []Projection{
		{Source: SourceRef{MSPID: "msp", Type: "task", ID: "task-1"}, EventRole: "due", SourceRevision: 1, Title: "Due 1", AllDay: true, StartsOn: &date, SchedulingMode: Informational},
		{Source: SourceRef{MSPID: "msp", Type: "task", ID: "task-2"}, EventRole: "due", SourceRevision: 1, Title: "Due 2", AllDay: true, StartsOn: &date, SchedulingMode: Informational},
	}
	for _, projection := range projections {
		if !errors.Is(projection.Validate(), ErrInvalidProjection) {
			t.Fatalf("Validate() = %v, want missing projection ID rejected", projection.Validate())
		}
		if found, err := ExpandOccurrences(projection, QueryWindow{Start: date, End: date.AddDate(0, 0, 1)}, nil); !errors.Is(err, ErrInvalidProjection) || len(found) != 0 {
			t.Fatalf("ExpandOccurrences() = (%+v, %v), want no colliding derived identity", found, err)
		}
	}
}

func TestProjectionValidateEnforcesCanonicalSourceRolePairs(t *testing.T) {
	date := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	validPairs := map[string][]string{
		"work_record":           {"scheduled_work", "due", "follow_up", "sla_response_deadline", "sla_resolution_deadline"},
		"task":                  {"scheduled_work", "due"},
		"project":               {"planned_start", "planned_end"},
		"phase":                 {"planned_start", "planned_end"},
		"milestone":             {"milestone"},
		"technician_schedule":   {"availability"},
		"pto":                   {"unavailability"},
		"maintenance_window":    {"maintenance"},
		"commercial_commitment": {"effective", "notice", "renewal", "expiration"},
	}
	for sourceType, roles := range validPairs {
		for _, role := range roles {
			projection := Projection{
				ID: "projection-1", Source: SourceRef{MSPID: "msp", Type: sourceType, ID: "source-1"},
				EventRole: role, SourceRevision: 1, Title: "Event", AllDay: true,
				StartsOn: &date, SchedulingMode: Informational,
			}
			if err := projection.Validate(); err != nil {
				t.Errorf("Validate(%s/%s) = %v", sourceType, role, err)
			}
		}
	}

	invalid := []Projection{
		{ID: "p1", Source: SourceRef{MSPID: "msp", Type: "task", ID: "task-1"}, EventRole: "milestone", SourceRevision: 1, Title: "Bad", AllDay: true, StartsOn: &date, SchedulingMode: Informational},
		{ID: "p2", Source: SourceRef{MSPID: "msp", Type: "project", ID: "project-1"}, EventRole: "due", SourceRevision: 1, Title: "Bad", AllDay: true, StartsOn: &date, SchedulingMode: Informational},
		{ID: "p3", Source: SourceRef{MSPID: "msp", Type: "maintenance_window", ID: "maintenance-1"}, EventRole: "event", SourceRevision: 1, Title: "Bad", AllDay: true, StartsOn: &date, SchedulingMode: Informational},
		{ID: "p4", Source: SourceRef{MSPID: "msp", Type: "custom_date", ID: "value-1"}, EventRole: "event", SourceRoleKey: "field_1", SourceRevision: 1, Title: "Bad", AllDay: true, StartsOn: &date, SchedulingMode: Informational},
		{ID: "p5", Source: SourceRef{MSPID: "msp", Type: "custom_date", ID: "value-1"}, EventRole: "warranty_expiration", SourceRevision: 1, Title: "Bad", AllDay: true, StartsOn: &date, SchedulingMode: Informational},
	}
	for _, projection := range invalid {
		if !errors.Is(projection.Validate(), ErrInvalidProjection) {
			t.Errorf("Validate(%s/%s) = %v, want source/role mismatch rejected", projection.Source.Type, projection.EventRole, projection.Validate())
		}
	}

	custom := Projection{
		ID: "custom-1", Source: SourceRef{MSPID: "msp", Type: "custom_date", ID: "value-1"},
		EventRole: "warranty_expiration", SourceRoleKey: "field_warranty", SourceRevision: 1,
		Title: "Warranty", AllDay: true, StartsOn: &date, SchedulingMode: Informational,
	}
	if err := custom.Validate(); err != nil {
		t.Fatalf("configured custom-date role rejected: %v", err)
	}
}

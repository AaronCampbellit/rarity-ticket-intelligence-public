package calendar

import (
	"reflect"
	"testing"
	"time"
)

func TestCalendarDemoSeedIsDeterministicAndCoversTypedScenarios(t *testing.T) {
	input := DemoSeedInput{MSPID: "msp", ClientIDs: []string{"a", "b"}, TechnicianIDs: []string{"one", "two"}, Now: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}
	seed, err := BuildDemoSeed(input)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := BuildDemoSeed(input)
	if !reflect.DeepEqual(seed, again) {
		t.Fatal("fixture is not deterministic")
	}
	types, health, modes := map[string]bool{}, map[HealthState]bool{}, map[SchedulingMode]bool{}
	for _, row := range seed.Records {
		types[row.SourceType] = true
		health[row.ExpectedHealth] = true
		modes[row.SchedulingMode] = true
		if row.AllDay && (row.StartsAt != "" || row.Timezone != "") {
			t.Fatal("invented clock time")
		}
		if row.Recurrence != nil {
			if err = row.Recurrence.Validate(); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, kind := range []string{"work_record", "task", "project", "milestone", "pto", "maintenance_window", "commercial_commitment", "technician_schedule", "custom_date"} {
		if !types[kind] {
			t.Fatal(kind)
		}
	}
	for _, state := range []HealthState{HealthBlocked, HealthOverdue, HealthAtRisk, HealthOnTrack} {
		if !health[state] {
			t.Fatal(state)
		}
	}
	if len(modes) != 3 {
		t.Fatalf("modes=%v", modes)
	}
	if _, err = BuildDemoSeed(DemoSeedInput{}); err == nil {
		t.Fatal("accepted missing fixture scope")
	}
}

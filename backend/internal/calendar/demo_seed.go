package calendar

import (
	"fmt"
	"time"
)

// DemoSeedInput defines an isolated, deterministic development fixture. Building
// it performs no writes and does not attach fake projections to real source rows.
type DemoSeedInput struct {
	MSPID         string    `json:"msp_id"`
	ClientIDs     []string  `json:"client_ids"`
	TechnicianIDs []string  `json:"technician_ids"`
	Now           time.Time `json:"now"`
}
type DemoSeed struct {
	Input   DemoSeedInput     `json:"input"`
	Records []DemoSourceInput `json:"records"`
}

// DemoSourceInput is a source fixture description, not a persistence API. A
// harness must create the typed source through its ordinary domain service.
type DemoSourceInput struct {
	Key              string           `json:"key"`
	SourceType       string           `json:"source_type"`
	ClientID         string           `json:"client_id,omitempty"`
	TechnicianID     string           `json:"technician_id,omitempty"`
	Title            string           `json:"title"`
	AllDay           bool             `json:"all_day"`
	StartsOn         string           `json:"starts_on,omitempty"`
	EndsOn           string           `json:"ends_on,omitempty"`
	StartsAt         string           `json:"starts_at,omitempty"`
	EndsAt           string           `json:"ends_at,omitempty"`
	Timezone         string           `json:"timezone,omitempty"`
	SchedulingMode   SchedulingMode   `json:"scheduling_mode"`
	ExpectedHealth   HealthState      `json:"expected_health"`
	SourceBlocked    bool             `json:"source_blocked,omitempty"`
	CapacityShortage bool             `json:"capacity_shortage,omitempty"`
	PTOState         string           `json:"pto_state,omitempty"`
	ConflictSeverity ConflictSeverity `json:"conflict_severity,omitempty"`
	Recurrence       *RecurrenceRule  `json:"recurrence,omitempty"`
	DependsOn        string           `json:"depends_on,omitempty"`
}

func BuildDemoSeed(input DemoSeedInput) (DemoSeed, error) {
	if input.MSPID == "" || len(input.ClientIDs) != 2 || len(input.TechnicianIDs) != 2 || input.ClientIDs[0] == input.ClientIDs[1] || input.TechnicianIDs[0] == input.TechnicianIDs[1] || input.Now.IsZero() {
		return DemoSeed{}, fmt.Errorf("two distinct clients, two distinct technicians, MSP and reference time are required")
	}
	for _, id := range append(append([]string{}, input.ClientIDs...), input.TechnicianIDs...) {
		if id == "" {
			return DemoSeed{}, fmt.Errorf("fixture IDs must not be empty")
		}
	}
	input.Now = input.Now.UTC()
	result := DemoSeed{Input: input, Records: []DemoSourceInput{}}
	types := []string{"work_record", "task", "project", "milestone", "pto", "maintenance_window", "commercial_commitment", "technician_schedule", "custom_date"}
	for i, kind := range types {
		start := input.Now.Add(time.Duration(i+1) * 24 * time.Hour)
		row := DemoSourceInput{Key: fmt.Sprintf("demo-%02d", i), SourceType: kind, ClientID: input.ClientIDs[i%2], TechnicianID: input.TechnicianIDs[i%2], Title: "Demo " + kind, SchedulingMode: FixedBlock, ExpectedHealth: HealthOnTrack}
		switch i % 4 {
		case 0:
			row.SourceBlocked = true
			row.ExpectedHealth = HealthBlocked
		case 1:
			start = input.Now.Add(-24 * time.Hour)
			row.ExpectedHealth = HealthOverdue
		case 2:
			row.CapacityShortage = true
			row.ExpectedHealth = HealthAtRisk
		}
		if kind == "project" || kind == "milestone" || kind == "commercial_commitment" || kind == "custom_date" {
			row.AllDay = true
			row.StartsOn = start.Format("2006-01-02")
			row.EndsOn = start.AddDate(0, 0, 1).Format("2006-01-02")
			row.SchedulingMode = Informational
		} else {
			row.StartsAt = start.Format(time.RFC3339)
			row.EndsAt = start.Add(time.Hour).Format(time.RFC3339)
			row.Timezone = []string{"America/Chicago", "Europe/London"}[i%2]
		}
		if kind == "task" {
			row.SchedulingMode = EffortAllocation
			row.DependsOn = "demo-00"
			row.ClientID = input.ClientIDs[0]
			row.ExpectedHealth = HealthBlocked
		}
		if kind == "pto" {
			row.PTOState = "requested"
			row.ClientID = ""
		}
		if kind == "technician_schedule" {
			row.ClientID = ""
			row.SchedulingMode = Informational
		}
		if kind == "maintenance_window" {
			row.Recurrence = &RecurrenceRule{Frequency: Weekly, Interval: 1, Weekdays: []time.Weekday{time.Monday, time.Wednesday}, Count: 12}
			row.ConflictSeverity = ConflictHard
		}
		result.Records = append(result.Records, row)
	}
	for _, state := range []string{"approved", "rejected", "cancelled"} {
		row := result.Records[4]
		row.Key = "pto-" + state
		row.PTOState = state
		result.Records = append(result.Records, row)
	}
	for _, severity := range []ConflictSeverity{ConflictWarning, ConflictOverrideable} {
		row := result.Records[5]
		row.Key = "maintenance-" + string(severity)
		row.ConflictSeverity = severity
		result.Records = append(result.Records, row)
	}
	return result, nil
}

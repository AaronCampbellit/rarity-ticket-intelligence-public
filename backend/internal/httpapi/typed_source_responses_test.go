package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/commitments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
)

func TestScheduleResponseGoldenUsesISODateAndOmitsInternalFields(t *testing.T) {
	effectiveThrough := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	value := workforce.Schedule{ID: "schedule-1", MSPID: "secret-msp", TechnicianID: "tech-1", Timezone: "America/New_York", EffectiveFrom: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), EffectiveThrough: &effectiveThrough, Version: 2, CreatedBy: "secret-actor", Windows: []workforce.WeeklyWindow{{ID: "window-1", Weekday: time.Monday, StartsMinute: 540, EndsMinute: 1020, CapacityPercent: 100}}, Exceptions: []workforce.ScheduleException{{ID: "exception-1", ExceptionOn: time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), State: workforce.Unavailable, AllDay: true, Reason: "training", Version: 1}}}
	payload, err := json.Marshal(scheduleResponse(value))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"schedule-1","technician_id":"tech-1","timezone":"America/New_York","effective_from":"2026-08-01","effective_through":"2026-08-31","version":2,"windows":[{"id":"window-1","weekday":1,"starts_minute":540,"ends_minute":1020,"capacity_percent":100}],"exceptions":[{"id":"exception-1","exception_on":"2026-08-12","state":"unavailable","all_day":true,"starts_minute":0,"ends_minute":0,"capacity_percent":0,"reason":"training","version":1}]}`
	if string(payload) != want {
		t.Fatalf("payload=%s\nwant=%s", payload, want)
	}
	if strings.Contains(string(payload), "secret-msp") || strings.Contains(string(payload), "secret-actor") || strings.Contains(string(payload), "T00:00:00") {
		t.Fatalf("internal or timestamp date leaked: %s", payload)
	}
}

func TestTypedSourceResponsesAreAllowlistedAndUseStableDateEncodings(t *testing.T) {
	date := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	endDate := date.AddDate(0, 0, 1)
	start := time.Date(2026, 8, 12, 14, 30, 0, 0, time.FixedZone("EDT", -4*60*60))
	end := start.Add(time.Hour)
	created := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		value     any
		required  []string
		forbidden []string
	}{
		{"pto", ptoResponse(workforce.PTORequest{ID: "pto-1", MSPID: "secret-msp", TechnicianID: "tech-1", PTOType: "training", ManagerID: "secret-manager", DecidedBy: "secret-decider", DecisionReason: "secret-reason", CreatedBy: "secret-created", UpdatedBy: "secret-updated", StartsOn: &date, EndsOn: &endDate, AllDay: true, State: workforce.Approved, Version: 2, CreatedAt: created, UpdatedAt: created}), []string{`"starts_on":"2026-08-12"`, `"state":"approved"`}, []string{"secret-", "ManagerID", "MSPID"}},
		{"milestone", milestoneResponse(projects.Milestone{ID: "milestone-1", MSPID: "secret-msp", ClientID: "client-1", ProjectID: projects.ProjectID("project-1"), Name: "Cutover", DueOn: date, StartsAt: &start, EndsAt: &end, Timezone: "America/New_York", CreatedBy: "secret-created", UpdatedBy: "secret-updated", Version: 1}), []string{`"due_on":"2026-08-12"`, `"starts_at":"2026-08-12T14:30:00-04:00"`}, []string{"secret-", "MSPID", "CreatedBy"}},
		{"maintenance", maintenanceResponse(commitments.MaintenanceWindow{ID: "window-1", MSPID: "secret-msp", Title: "Upgrade", StartsAt: start, EndsAt: end, Timezone: "America/New_York", CreatedBy: "secret-created", UpdatedBy: "secret-updated", Version: 1}), []string{`"starts_at":"2026-08-12T14:30:00-04:00"`, `"timezone":"America/New_York"`}, []string{"secret-", "MSPID", "CreatedBy"}},
		{"commercial", commercialResponse(commitments.CommercialCommitment{ID: "commercial-1", MSPID: "secret-msp", ClientID: "client-1", Title: "Renewal", EffectiveOn: date, ExpirationOn: endDate, CreatedBy: "secret-created", UpdatedBy: "secret-updated", Version: 1}), []string{`"effective_on":"2026-08-12"`, `"expiration_on":"2026-08-13"`}, []string{"secret-", "MSPID", "CreatedBy"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range test.required {
				if !strings.Contains(string(payload), required) {
					t.Errorf("missing %s in %s", required, payload)
				}
			}
			for _, forbidden := range test.forbidden {
				if strings.Contains(string(payload), forbidden) {
					t.Errorf("leaked %s in %s", forbidden, payload)
				}
			}
		})
	}
}

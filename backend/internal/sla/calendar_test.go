package sla

import (
	"testing"
	"time"
)

func TestAddBusinessTimeCarriesAcrossWeekendAndHoliday(t *testing.T) {
	location := time.FixedZone("Central", -5*60*60)
	schedule := weekdaySchedule(9, 17)
	friday := time.Date(2026, time.July, 31, 16, 0, 0, 0, location)

	deadline, err := AddBusinessTime(Calendar{
		Location: location, Weekly: schedule,
	}, friday, 2*time.Hour)
	if err != nil {
		t.Fatalf("AddBusinessTime() error = %v", err)
	}
	want := time.Date(2026, time.August, 3, 10, 0, 0, 0, location)
	if !deadline.Equal(want) {
		t.Fatalf("deadline = %v, want %v", deadline, want)
	}

	deadline, err = AddBusinessTime(Calendar{
		Location: location, Weekly: schedule,
		Holidays: map[string]struct{}{"2026-08-03": {}},
	}, friday, 2*time.Hour)
	if err != nil {
		t.Fatalf("holiday AddBusinessTime() error = %v", err)
	}
	want = time.Date(2026, time.August, 4, 10, 0, 0, 0, location)
	if !deadline.Equal(want) {
		t.Fatalf("holiday deadline = %v, want %v", deadline, want)
	}
}

func TestEvaluateReportsWarningBreachAndMetStates(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	if state := Evaluate(Target{
		StartedAt: now.Add(-8 * time.Hour), DueAt: now.Add(2 * time.Hour),
		WarningAt: now.Add(-time.Minute),
	}, now); state != Warning {
		t.Fatalf("warning state = %q", state)
	}
	if state := Evaluate(Target{StartedAt: now.Add(-time.Hour), DueAt: now.Add(-time.Second)}, now); state != Breached {
		t.Fatalf("breach state = %q", state)
	}
	metAt := now.Add(-time.Minute)
	if state := Evaluate(Target{StartedAt: now.Add(-time.Hour), DueAt: now, MetAt: &metAt}, now); state != Met {
		t.Fatalf("met state = %q", state)
	}
	lateAt := now.Add(time.Minute)
	if state := Evaluate(Target{StartedAt: now.Add(-time.Hour), DueAt: now, MetAt: &lateAt}, now); state != Breached {
		t.Fatalf("late completion state = %q", state)
	}
}

func TestBusinessTimeBetweenExcludesOffHoursAndHolidays(t *testing.T) {
	location := time.FixedZone("Central", -5*60*60)
	calendar := Calendar{
		Location: location, Weekly: weekdaySchedule(9, 17),
		Holidays: map[string]struct{}{"2026-08-03": {}},
	}
	start := time.Date(2026, time.July, 31, 16, 0, 0, 0, location)
	end := time.Date(2026, time.August, 4, 10, 30, 0, 0, location)
	elapsed, err := BusinessTimeBetween(calendar, start, end)
	if err != nil {
		t.Fatalf("BusinessTimeBetween() error = %v", err)
	}
	if elapsed != 2*time.Hour+30*time.Minute {
		t.Fatalf("business elapsed = %v", elapsed)
	}
}

func weekdaySchedule(startHour, endHour int) map[time.Weekday][]Window {
	schedule := map[time.Weekday][]Window{}
	for day := time.Monday; day <= time.Friday; day++ {
		schedule[day] = []Window{{StartMinute: startHour * 60, EndMinute: endHour * 60}}
	}
	return schedule
}

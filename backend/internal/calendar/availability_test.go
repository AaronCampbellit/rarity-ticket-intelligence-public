package calendar

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
)

type availabilityRepositoryStub struct {
	input AvailabilityInput
	err   error
}

func (s *availabilityRepositoryStub) LoadAvailabilityInput(context.Context, string, string, QueryWindow) (AvailabilityInput, error) {
	return s.input, s.err
}

func TestAvailabilityAppliesApprovedButNotRequestedPTO(t *testing.T) {
	zone := "America/Chicago"
	service := NewAvailabilityService(&availabilityRepositoryStub{input: AvailabilityInput{
		Schedules: []workforce.Schedule{availabilityWeekdaySchedule(zone, time.Monday, 9*60, 17*60)},
		PTO: []workforce.PTORequest{
			{State: workforce.Requested, StartsAt: availabilityLocalInstant(t, "2026-08-10T09:00", zone), EndsAt: availabilityLocalInstant(t, "2026-08-10T11:00", zone)},
			{State: workforce.Approved, StartsAt: availabilityLocalInstant(t, "2026-08-10T13:00", zone), EndsAt: availabilityLocalInstant(t, "2026-08-10T15:00", zone)},
		},
	}})
	found, err := service.Resolve(context.Background(), "msp", "tech-1", availabilityDayWindow(t, 2026, 8, 10, zone))
	if err != nil {
		t.Fatal(err)
	}
	if found.AvailableMinutes != 360 || found.TentativeMinutes != 120 {
		t.Fatalf("availability = %+v", found)
	}
}

func TestAvailabilityAppliesAdjustmentsInAuthoritativeOrder(t *testing.T) {
	zone := "America/Chicago"
	day := availabilityDayWindow(t, 2026, 8, 10, zone)
	service := NewAvailabilityService(&availabilityRepositoryStub{input: AvailabilityInput{
		Schedules: []workforce.Schedule{availabilityWeekdaySchedule(zone, time.Monday, 9*60, 17*60)},
		Adjustments: []AvailabilityAdjustment{
			{Kind: HolidayAdjustment, State: AvailabilityUnavailable, Interval: availabilityLocalRange(t, "2026-08-10T09:00", "2026-08-10T17:00", zone)},
			{Kind: ManualAdjustment, State: AvailabilityAvailable, Interval: availabilityLocalRange(t, "2026-08-10T10:00", "2026-08-10T14:00", zone), CapacityPercent: 100},
			{Kind: ProtectedMaintenanceAdjustment, State: AvailabilityUnavailable, Interval: availabilityLocalRange(t, "2026-08-10T12:00", "2026-08-10T13:00", zone)},
		},
	}})
	found, err := service.Resolve(context.Background(), "msp", "tech-1", day)
	if err != nil {
		t.Fatal(err)
	}
	if found.AvailableMinutes != 180 {
		t.Fatalf("available minutes = %d, want 180", found.AvailableMinutes)
	}
}

func TestAvailabilityIncludesScheduleEffectiveThroughLocalDate(t *testing.T) {
	zone := "America/Chicago"
	schedule := availabilityWeekdaySchedule(zone, time.Monday, 9*60, 17*60)
	through := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	schedule.EffectiveThrough = &through
	service := NewAvailabilityService(&availabilityRepositoryStub{input: AvailabilityInput{Schedules: []workforce.Schedule{schedule}}})
	found, err := service.Resolve(context.Background(), "msp", "tech-1", availabilityDayWindow(t, 2026, 8, 10, zone))
	if err != nil {
		t.Fatal(err)
	}
	if found.AvailableMinutes != 480 {
		t.Fatalf("available minutes=%d, want 480 on effective-through date", found.AvailableMinutes)
	}
}

func TestAvailabilitySkipsNonexistentSpringGapWindow(t *testing.T) {
	zone := "America/Chicago"
	service := NewAvailabilityService(&availabilityRepositoryStub{input: AvailabilityInput{Schedules: []workforce.Schedule{availabilityWeekdaySchedule(zone, time.Sunday, 2*60+30, 3*60+30)}}})
	found, err := service.Resolve(context.Background(), "msp", "tech-1", availabilityDayWindow(t, 2026, 3, 8, zone))
	if err != nil {
		t.Fatal(err)
	}
	if found.AvailableMinutes != 0 || len(found.Segments) != 0 {
		t.Fatalf("spring-gap availability=%+v", found)
	}
}

func TestAvailabilityFallbackFoldUsesEarliestExactStart(t *testing.T) {
	zone := "America/Chicago"
	service := NewAvailabilityService(&availabilityRepositoryStub{input: AvailabilityInput{Schedules: []workforce.Schedule{availabilityWeekdaySchedule(zone, time.Sunday, 1*60+30, 2*60+30)}}})
	found, err := service.Resolve(context.Background(), "msp", "tech-1", availabilityDayWindow(t, 2026, 11, 1, zone))
	if err != nil {
		t.Fatal(err)
	}
	if found.AvailableMinutes != 120 || len(found.Segments) != 1 {
		t.Fatalf("fallback availability=%+v", found)
	}
	location, _ := time.LoadLocation(zone)
	if got := found.Segments[0].Interval.Start.In(location).Format("15:04 -07:00"); got != "01:30 -05:00" {
		t.Fatalf("fallback start=%s", got)
	}
}

func TestAvailabilityGapsReturnsExactMergedUncoveredIntervals(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	gaps := AvailabilityGaps([]AvailabilitySegment{
		{Interval: TimeInterval{Start: start, End: start.Add(time.Hour)}, CapacityPercent: 100},
		{Interval: TimeInterval{Start: start.Add(90 * time.Minute), End: start.Add(2 * time.Hour)}, CapacityPercent: 100},
		{Interval: TimeInterval{Start: start.Add(2 * time.Hour), End: start.Add(3 * time.Hour)}, CapacityPercent: 100},
	}, TimeInterval{Start: start, End: start.Add(3 * time.Hour)})
	if len(gaps) != 1 || !gaps[0].Start.Equal(start.Add(time.Hour)) || !gaps[0].End.Equal(start.Add(90*time.Minute)) {
		t.Fatalf("gaps=%+v", gaps)
	}
}

func availabilityWeekdaySchedule(zone string, weekday time.Weekday, start, end int) workforce.Schedule {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return workforce.Schedule{Timezone: zone, EffectiveFrom: from, Version: 1, Windows: []workforce.WeeklyWindow{{Weekday: weekday, StartsMinute: start, EndsMinute: end, CapacityPercent: 100}}}
}

func availabilityLocalInstant(t *testing.T, value, zone string) *time.Time {
	t.Helper()
	location, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := time.ParseInLocation("2006-01-02T15:04", value, location)
	if err != nil {
		t.Fatal(err)
	}
	parsed = parsed.UTC()
	return &parsed
}

func availabilityLocalRange(t *testing.T, start, end, zone string) TimeInterval {
	return TimeInterval{Start: *availabilityLocalInstant(t, start, zone), End: *availabilityLocalInstant(t, end, zone)}
}

func availabilityDayWindow(t *testing.T, year int, month time.Month, day int, zone string) QueryWindow {
	t.Helper()
	location, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(year, month, day, 0, 0, 0, 0, location)
	return QueryWindow{Start: start.UTC(), End: start.AddDate(0, 0, 1).UTC()}
}

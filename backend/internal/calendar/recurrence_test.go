package calendar

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestExpandOccurrencesPreservesLocalTimeAcrossDST(t *testing.T) {
	start := mustTime(t, "2026-03-01T09:00:00-05:00")
	projection := recurringProjection(start, "America/New_York", RecurrenceRule{
		Frequency: Weekly, Interval: 1, Weekdays: []time.Weekday{time.Sunday},
		Count: 3,
	})
	found, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-03-01T00:00:00Z"),
		End:   mustTime(t, "2026-03-20T00:00:00Z"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantOffsets := []string{"-05:00", "-04:00", "-04:00"}
	if len(found) != len(wantOffsets) {
		t.Fatalf("occurrences = %d, want %d", len(found), len(wantOffsets))
	}
	for i, occurrence := range found {
		local := occurrence.StartsAt.In(mustLocation(t, "America/New_York"))
		if local.Hour() != 9 {
			t.Fatalf("local hour = %d, want 9", local.Hour())
		}
		if got := local.Format("-07:00"); got != wantOffsets[i] {
			t.Fatalf("offset[%d] = %s, want %s", i, got, wantOffsets[i])
		}
	}
}

func TestExpandOccurrencesAppliesCancelAndMoveExceptions(t *testing.T) {
	start := mustTime(t, "2026-08-10T09:00:00Z")
	projection := recurringProjection(start, "UTC", RecurrenceRule{
		Frequency: Daily, Interval: 1, Count: 3,
	})
	secondKey := "2026-08-11T09:00:00"
	thirdKey := "2026-08-12T09:00:00"
	moved := mustTime(t, "2026-08-13T11:00:00Z")
	found, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-08-10T00:00:00Z"),
		End:   mustTime(t, "2026-08-15T00:00:00Z"),
	}, []RecurrenceException{
		{OriginalLocalKey: secondKey, State: ExceptionCancelled},
		{OriginalLocalKey: thirdKey, State: ExceptionRescheduled, StartsAt: &moved},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("occurrences = %d, want 2", len(found))
	}
	if found[1].OriginalLocalKey != thirdKey || !found[1].StartsAt.Equal(moved) {
		t.Fatalf("moved occurrence lost identity: %+v", found[1])
	}
	if found[1].ID != projection.ID+":"+thirdKey {
		t.Fatalf("moved ID = %q, want stable original-key identity", found[1].ID)
	}
}

func TestExpandOccurrencesSplitsThisAndFutureWithoutMovingPastOccurrences(t *testing.T) {
	start := mustTime(t, "2026-08-10T09:00:00Z")
	projection := recurringProjection(start, "UTC", RecurrenceRule{Frequency: Daily, Interval: 1, Count: 4})
	moved := mustTime(t, "2026-08-11T11:00:00Z")
	found, err := ExpandOccurrences(projection, QueryWindow{Start: mustTime(t, "2026-08-10T00:00:00Z"), End: mustTime(t, "2026-08-15T00:00:00Z")}, []RecurrenceException{{OriginalLocalKey: "2026-08-11T09:00:00", OccurrenceScope: ThisAndFuture, State: ExceptionRescheduled, StartsAt: &moved}})
	if err != nil {
		t.Fatal(err)
	}
	wantHours := []int{9, 11, 11, 11}
	if len(found) != len(wantHours) {
		t.Fatalf("occurrences=%+v", found)
	}
	for i, want := range wantHours {
		if found[i].StartsAt.Hour() != want {
			t.Fatalf("occurrence %d starts=%v want hour=%d", i, found[i].StartsAt, want)
		}
	}
}

func TestExpandOccurrencesFutureSplitUsesWallTimeAcrossDST(t *testing.T) {
	start := mustTime(t, "2026-03-01T09:00:00-05:00")
	projection := recurringProjection(start, "America/New_York", RecurrenceRule{Frequency: Weekly, Interval: 1, Weekdays: []time.Weekday{time.Sunday}, Count: 3})
	moved := mustTime(t, "2026-03-08T11:00:00-04:00")
	found, err := ExpandOccurrences(projection, QueryWindow{Start: mustTime(t, "2026-03-01T00:00:00Z"), End: mustTime(t, "2026-03-30T00:00:00Z")}, []RecurrenceException{{OriginalLocalKey: "2026-03-01T09:00:00", OccurrenceScope: ThisAndFuture, State: ExceptionRescheduled, StartsAt: &moved}})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 3 {
		t.Fatalf("occurrences=%+v", found)
	}
	location := mustLocation(t, "America/New_York")
	wantDates := []string{"2026-03-08T11:00:00-04:00", "2026-03-15T11:00:00-04:00", "2026-03-22T11:00:00-04:00"}
	for i, want := range wantDates {
		if got := found[i].StartsAt.In(location).Format(time.RFC3339); got != want {
			t.Fatalf("start[%d]=%s want=%s", i, got, want)
		}
	}
}

func TestExpandOccurrencesMonthlySkipsMissingMonthDays(t *testing.T) {
	start := mustTime(t, "2026-01-31T09:00:00Z")
	projection := recurringProjection(start, "UTC", RecurrenceRule{Frequency: Monthly, Interval: 1, Count: 4})
	found, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-01-01T00:00:00Z"), End: mustTime(t, "2026-06-01T00:00:00Z"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-01-31T09:00:00", "2026-03-31T09:00:00", "2026-05-31T09:00:00"}
	if len(found) != len(want) {
		t.Fatalf("keys = %v, want %v", occurrenceKeys(found), want)
	}
	for i := range want {
		if found[i].OriginalLocalKey != want[i] {
			t.Fatalf("keys = %v, want %v", occurrenceKeys(found), want)
		}
	}
}

func TestExpandOccurrencesSupportsDateOnlySeriesAndOverrides(t *testing.T) {
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 2)
	projection := Projection{
		ID: "pto-1", Source: SourceRef{MSPID: "msp", Type: "pto", ID: "pto-1"},
		EventRole: "unavailability", SourceRevision: 1, Title: "PTO", AllDay: true,
		StartsOn: &start, EndsOn: &end, SchedulingMode: Informational,
		Recurrence: &RecurrenceRule{Frequency: Daily, Interval: 2, Count: 3},
	}
	movedStart := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	movedEnd := movedStart.AddDate(0, 0, 1)
	found, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-08-09T00:00:00Z"), End: mustTime(t, "2026-08-20T00:00:00Z"),
	}, []RecurrenceException{{
		OriginalLocalKey: "2026-08-12", State: ExceptionOverridden,
		StartsOn: &movedStart, EndsOn: &movedEnd,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 3 || !found[1].AllDay || found[1].StartsAt.IsZero() == false {
		t.Fatalf("date occurrences = %+v", found)
	}
	var overridden *Occurrence
	for i := range found {
		if found[i].OriginalLocalKey == "2026-08-12" {
			overridden = &found[i]
		}
	}
	if overridden == nil || !overridden.StartsOn.Equal(movedStart) || !overridden.EndsOn.Equal(movedEnd) {
		t.Fatalf("override lost original identity/date shape: %+v", overridden)
	}
}

func TestExpandOccurrencesStopsAtQueryHorizonBeforeFarUntil(t *testing.T) {
	start := mustTime(t, "2026-01-01T09:00:00Z")
	until := mustTime(t, "9999-12-30T09:00:00Z")
	projection := recurringProjection(start, "UTC", RecurrenceRule{Frequency: Daily, Interval: 1, Until: &until})
	found, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-01-01T00:00:00Z"), End: mustTime(t, "2026-01-03T00:00:00Z"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := occurrenceKeys(found), []string{"2026-01-01T09:00:00", "2026-01-02T09:00:00"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
}

func TestExpandOccurrencesLargeCountClipsToNarrowWindow(t *testing.T) {
	projection := recurringProjection(mustTime(t, "2026-01-01T09:00:00Z"), "UTC", RecurrenceRule{
		Frequency: Daily, Interval: 1, Count: maxExpandedOccurrences,
	})
	found, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-01-01T00:00:00Z"), End: mustTime(t, "2026-01-02T00:00:00Z"),
	}, nil)
	if err != nil || len(found) != 1 {
		t.Fatalf("ExpandOccurrences() = (%d, %v), want one clipped occurrence", len(found), err)
	}
}

func TestExpandOccurrencesValidatesFarExceptionWithoutExpandingThroughIt(t *testing.T) {
	start := mustTime(t, "2026-01-01T09:00:00Z")
	until := mustTime(t, "9999-12-30T09:00:00Z")
	moved := mustTime(t, "2026-01-02T11:00:00Z")
	projection := recurringProjection(start, "UTC", RecurrenceRule{Frequency: Daily, Interval: 1, Until: &until})
	found, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-01-01T00:00:00Z"), End: mustTime(t, "2026-01-03T00:00:00Z"),
	}, []RecurrenceException{{
		OriginalLocalKey: "9999-12-30T09:00:00", State: ExceptionRescheduled, StartsAt: &moved,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 3 || found[2].OriginalLocalKey != "9999-12-30T09:00:00" || !found[2].StartsAt.Equal(moved) {
		t.Fatalf("far moved exception = %+v", found)
	}
}

func TestExpandOccurrencesRejectsExceptionsBeyondCountOrUntil(t *testing.T) {
	start := mustTime(t, "2026-01-01T09:00:00Z")
	until := mustTime(t, "2026-01-02T09:00:00Z")
	window := QueryWindow{Start: mustTime(t, "2026-01-01T00:00:00Z"), End: mustTime(t, "2026-01-02T00:00:00Z")}
	tests := []Projection{
		recurringProjection(start, "UTC", RecurrenceRule{Frequency: Daily, Interval: 1, Count: 2}),
		recurringProjection(start, "UTC", RecurrenceRule{Frequency: Daily, Interval: 1, Until: &until}),
	}
	for _, projection := range tests {
		_, err := ExpandOccurrences(projection, window, []RecurrenceException{{
			OriginalLocalKey: "2026-01-03T09:00:00", State: ExceptionCancelled,
		}})
		if !errors.Is(err, ErrInvalidRecurrence) {
			t.Errorf("ExpandOccurrences(%+v) = %v, want exception beyond rule bound rejected", projection.Recurrence, err)
		}
	}
}

func TestExpandOccurrencesPreservesEndWallClockAcrossDST(t *testing.T) {
	start := mustTime(t, "2026-03-01T01:00:00-05:00")
	end := mustTime(t, "2026-03-01T03:00:00-05:00")
	projection := recurringProjection(start, "America/New_York", RecurrenceRule{
		Frequency: Weekly, Interval: 1, Weekdays: []time.Weekday{time.Sunday}, Count: 2,
	})
	projection.EndsAt = &end
	found, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-03-01T00:00:00Z"), End: mustTime(t, "2026-03-10T00:00:00Z"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	location := mustLocation(t, "America/New_York")
	if got := found[1].EndsAt.In(location).Format("2006-01-02T15:04:05-07:00"); got != "2026-03-08T03:00:00-04:00" {
		t.Fatalf("DST occurrence end = %s, want preserved 03:00 wall clock", got)
	}
}

func TestExpandOccurrencesSkipsNonexistentLocalStart(t *testing.T) {
	start := mustTime(t, "2026-03-01T02:30:00-05:00")
	projection := recurringProjection(start, "America/New_York", RecurrenceRule{
		Frequency: Weekly, Interval: 1, Weekdays: []time.Weekday{time.Sunday}, Count: 3,
	})
	found, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-03-01T00:00:00Z"), End: mustTime(t, "2026-03-24T00:00:00Z"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-03-01T02:30:00", "2026-03-15T02:30:00", "2026-03-22T02:30:00"}
	if got := occurrenceKeys(found); !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %v, want nonexistent spring-gap start skipped", got)
	}
}

func TestExpandOccurrencesExceptionDefaultEndUsesWallDuration(t *testing.T) {
	start := mustTime(t, "2026-03-01T01:00:00-05:00")
	end := mustTime(t, "2026-03-01T03:00:00-05:00")
	moved := mustTime(t, "2026-03-08T01:00:00-05:00")
	projection := recurringProjection(start, "America/New_York", RecurrenceRule{Frequency: Weekly, Interval: 1, Weekdays: []time.Weekday{time.Sunday}, Count: 1})
	projection.EndsAt = &end
	found, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-03-08T00:00:00Z"), End: mustTime(t, "2026-03-09T00:00:00Z"),
	}, []RecurrenceException{{OriginalLocalKey: "2026-03-01T01:00:00", State: ExceptionRescheduled, StartsAt: &moved}})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("occurrences = %+v", found)
	}
	if got := found[0].EndsAt.In(mustLocation(t, "America/New_York")).Format("2006-01-02T15:04:05-07:00"); got != "2026-03-08T03:00:00-04:00" {
		t.Fatalf("exception default end = %s, want preserved two-hour wall duration", got)
	}
}

func TestExpandOccurrencesNonRecurringEventKeepsExactFallbackInstants(t *testing.T) {
	start := mustTime(t, "2026-11-01T01:30:00-04:00")
	end := mustTime(t, "2026-11-01T01:30:00-05:00")
	projection := Projection{
		ID: "fallback-1", Source: SourceRef{MSPID: "msp", Type: "task", ID: "task-1"},
		EventRole: "scheduled_work", SourceRevision: 1, Title: "Fallback work",
		StartsAt: &start, EndsAt: &end, Timezone: "America/New_York", SchedulingMode: FixedBlock,
	}
	found, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-11-01T04:00:00Z"), End: mustTime(t, "2026-11-01T08:00:00Z"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || !found[0].StartsAt.Equal(start) || !found[0].EndsAt.Equal(end) {
		t.Fatalf("non-recurring fallback event = %+v, want exact source instants", found)
	}
}

func TestExpandOccurrencesRecurringFallbackFoldUsesAuthoritativeDuration(t *testing.T) {
	start := mustTime(t, "2026-11-01T01:30:00-04:00")
	end := mustTime(t, "2026-11-01T01:30:00-05:00")
	projection := recurringProjection(start, "America/New_York", RecurrenceRule{
		Frequency: Weekly, Interval: 1, Weekdays: []time.Weekday{time.Sunday}, Count: 2,
	})
	projection.EndsAt = &end
	found, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-11-01T04:00:00Z"), End: mustTime(t, "2026-11-09T08:00:00Z"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 || !found[0].StartsAt.Equal(start) || !found[0].EndsAt.Equal(end) {
		t.Fatalf("authoritative fallback occurrence = %+v", found)
	}
	location := mustLocation(t, "America/New_York")
	if got := found[1].StartsAt.In(location).Format("2006-01-02T15:04:05-07:00"); got != "2026-11-08T01:30:00-05:00" {
		t.Fatalf("future fallback start = %s", got)
	}
	if got := found[1].EndsAt.In(location).Format("2006-01-02T15:04:05-07:00"); got != "2026-11-08T02:30:00-05:00" {
		t.Fatalf("future fallback end = %s, want deterministic absolute-duration fallback", got)
	}
}

func TestExpandOccurrencesRejectsTooManyExceptions(t *testing.T) {
	start := mustTime(t, "2026-01-01T09:00:00Z")
	until := mustTime(t, "2060-01-01T09:00:00Z")
	projection := recurringProjection(start, "UTC", RecurrenceRule{Frequency: Daily, Interval: 1, Until: &until})
	exceptions := make([]RecurrenceException, maxExpandedOccurrences+1)
	for i := range exceptions {
		exceptions[i] = RecurrenceException{
			OriginalLocalKey: start.AddDate(0, 0, i).Format("2006-01-02T15:04:05"),
			State:            ExceptionCancelled,
		}
	}
	_, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-01-01T00:00:00Z"), End: mustTime(t, "2026-01-02T00:00:00Z"),
	}, exceptions)
	if !errors.Is(err, ErrExpansionLimit) {
		t.Fatalf("ExpandOccurrences() = %v, want oversized exception input rejected", err)
	}
}

func TestExpandOccurrencesEnforcesOutputLimitAfterMovedExceptions(t *testing.T) {
	start := mustTime(t, "2026-01-01T09:00:00Z")
	until := mustTime(t, "2060-01-01T09:00:00Z")
	moved := mustTime(t, "2026-01-01T12:00:00Z")
	projection := recurringProjection(start, "UTC", RecurrenceRule{Frequency: Daily, Interval: 1, Until: &until})
	exceptions := make([]RecurrenceException, maxExpandedOccurrences)
	for i := range exceptions {
		exceptions[i] = RecurrenceException{
			OriginalLocalKey: start.AddDate(0, 0, i+1).Format("2006-01-02T15:04:05"),
			State:            ExceptionRescheduled,
			StartsAt:         &moved,
		}
	}
	_, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-01-01T00:00:00Z"), End: mustTime(t, "2026-01-02T00:00:00Z"),
	}, exceptions)
	if !errors.Is(err, ErrExpansionLimit) {
		t.Fatalf("ExpandOccurrences() = %v, want total output limit enforced", err)
	}
}

func TestExpandOccurrencesManyCountExceptionsHaveBoundedValidationWork(t *testing.T) {
	start := mustTime(t, "2026-01-01T09:00:00Z")
	projection := recurringProjection(start, "UTC", RecurrenceRule{
		Frequency: Daily, Interval: 1, Count: maxExpandedOccurrences,
	})
	exceptions := make([]RecurrenceException, 2_000)
	for i := range exceptions {
		exceptions[i] = RecurrenceException{
			OriginalLocalKey: start.AddDate(0, 0, 8_000+i).Format("2006-01-02T15:04:05"),
			State:            ExceptionCancelled,
		}
	}
	started := time.Now()
	found, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-01-01T00:00:00Z"), End: mustTime(t, "2026-01-02T00:00:00Z"),
	}, exceptions)
	if err != nil || len(found) != 1 {
		t.Fatalf("ExpandOccurrences() = (%d, %v)", len(found), err)
	}
	if elapsed := time.Since(started); elapsed > 750*time.Millisecond {
		t.Fatalf("many-exception validation took %v, want one bounded count index", elapsed)
	}
}

func TestExpandOccurrencesValidatesCountExceptionsAcrossFrequencies(t *testing.T) {
	tests := []struct {
		name       string
		projection Projection
		key        string
	}{
		{
			name: "daily",
			projection: recurringProjection(mustTime(t, "2026-01-01T09:00:00Z"), "UTC", RecurrenceRule{
				Frequency: Daily, Interval: 2, Count: 4,
			}),
			key: "2026-01-07T09:00:00",
		},
		{
			name: "weekly",
			projection: recurringProjection(mustTime(t, "2026-01-05T09:00:00Z"), "UTC", RecurrenceRule{
				Frequency: Weekly, Interval: 1, Weekdays: []time.Weekday{time.Monday, time.Wednesday}, Count: 4,
			}),
			key: "2026-01-14T09:00:00",
		},
		{
			name: "monthly skip",
			projection: recurringProjection(mustTime(t, "2026-01-31T09:00:00Z"), "UTC", RecurrenceRule{
				Frequency: Monthly, Interval: 1, Count: 3,
			}),
			key: "2026-05-31T09:00:00",
		},
		{
			name: "yearly leap skip",
			projection: recurringProjection(mustTime(t, "2024-02-29T09:00:00Z"), "UTC", RecurrenceRule{
				Frequency: Yearly, Interval: 1, Count: 3,
			}),
			key: "2032-02-29T09:00:00",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			found, err := ExpandOccurrences(tc.projection, QueryWindow{
				Start: *tc.projection.StartsAt, End: tc.projection.StartsAt.Add(24 * time.Hour),
			}, []RecurrenceException{{OriginalLocalKey: tc.key, State: ExceptionCancelled}})
			if err != nil || len(found) != 1 {
				t.Fatalf("ExpandOccurrences() = (%+v, %v), want far valid Count exception accepted", found, err)
			}
		})
	}
}

func TestExpandOccurrencesClipsMovedAndOrdinaryOccurrencesToQuery(t *testing.T) {
	start := mustTime(t, "2026-08-01T09:00:00Z")
	projection := recurringProjection(start, "UTC", RecurrenceRule{Frequency: Daily, Interval: 1, Count: 3})
	moved := mustTime(t, "2026-08-10T09:00:00Z")
	found, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-08-09T00:00:00Z"), End: mustTime(t, "2026-08-11T00:00:00Z"),
	}, []RecurrenceException{{OriginalLocalKey: "2026-08-02T09:00:00", State: ExceptionRescheduled, StartsAt: &moved}})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].OriginalLocalKey != "2026-08-02T09:00:00" {
		t.Fatalf("clipped occurrences = %+v", found)
	}
}

func TestRecurrenceValidationRejectsUnboundedOrInvalidRules(t *testing.T) {
	until := mustTime(t, "2026-12-01T00:00:00Z")
	rules := []RecurrenceRule{
		{Frequency: Daily, Interval: 0, Count: 1},
		{Frequency: Daily, Interval: 1, Count: 1, Until: &until},
		{Frequency: Weekly, Interval: 1, Count: 2},
		{Frequency: Weekly, Interval: 1, Count: 2, Weekdays: []time.Weekday{time.Monday, time.Monday}},
		{Frequency: Monthly, Interval: 1, Count: 2, Weekdays: []time.Weekday{time.Monday}},
		{Frequency: Frequency("hourly"), Interval: 1, Count: 2},
		{Frequency: Daily, Interval: 1, Count: maxExpandedOccurrences + 1},
	}
	for _, rule := range rules {
		if !errors.Is(rule.Validate(), ErrInvalidRecurrence) {
			t.Errorf("Validate(%+v) = %v, want ErrInvalidRecurrence", rule, rule.Validate())
		}
	}
}

func TestExpandOccurrencesRejectsInvalidOrExcessiveQueryWindows(t *testing.T) {
	projection := recurringProjection(mustTime(t, "2026-01-01T09:00:00Z"), "UTC", RecurrenceRule{
		Frequency: Daily, Interval: 1,
	})
	tests := []QueryWindow{
		{},
		{Start: mustTime(t, "2026-01-02T00:00:00Z"), End: mustTime(t, "2026-01-01T00:00:00Z")},
		{Start: mustTime(t, "2020-01-01T00:00:00Z"), End: mustTime(t, "2040-01-01T00:00:00Z")},
	}
	for _, window := range tests {
		if _, err := ExpandOccurrences(projection, window, nil); !errors.Is(err, ErrInvalidQueryWindow) {
			t.Errorf("ExpandOccurrences(window=%+v) = %v, want ErrInvalidQueryWindow", window, err)
		}
	}
}

func TestExpandOccurrencesRejectsMalformedExceptions(t *testing.T) {
	projection := recurringProjection(mustTime(t, "2026-08-10T09:00:00Z"), "UTC", RecurrenceRule{
		Frequency: Daily, Interval: 1, Count: 2,
	})
	date := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	tests := [][]RecurrenceException{
		{{OriginalLocalKey: "", State: ExceptionCancelled}},
		{{OriginalLocalKey: "2026-08-11T09:00:00", State: RecurrenceExceptionState("moved")}},
		{{OriginalLocalKey: "2026-08-11T09:00:00", State: ExceptionRescheduled}},
		{{OriginalLocalKey: "2026-08-11T09:00:00", State: ExceptionRescheduled, StartsOn: &date}},
		{{OriginalLocalKey: "2026-08-11T09:00:00", State: ExceptionCancelled}, {OriginalLocalKey: "2026-08-11T09:00:00", State: ExceptionCancelled}},
	}
	window := QueryWindow{Start: mustTime(t, "2026-08-01T00:00:00Z"), End: mustTime(t, "2026-09-01T00:00:00Z")}
	for _, exceptions := range tests {
		if _, err := ExpandOccurrences(projection, window, exceptions); !errors.Is(err, ErrInvalidRecurrence) {
			t.Errorf("ExpandOccurrences(exceptions=%+v) = %v, want ErrInvalidRecurrence", exceptions, err)
		}
	}
}

func recurringProjection(start time.Time, timezone string, rule RecurrenceRule) Projection {
	end := start.Add(time.Hour)
	return Projection{
		ID: "projection-1", Source: SourceRef{MSPID: "msp", Type: "task", ID: "task-1"},
		EventRole: "scheduled_work", SourceRevision: 1, Title: "Work",
		StartsAt: &start, EndsAt: &end, Timezone: timezone, SchedulingMode: FixedBlock,
		Recurrence: &rule,
	}
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	found, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func mustLocation(t *testing.T, value string) *time.Location {
	t.Helper()
	found, err := time.LoadLocation(value)
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func occurrenceKeys(found []Occurrence) []string {
	keys := make([]string, len(found))
	for i := range found {
		keys[i] = found[i].OriginalLocalKey
	}
	return keys
}

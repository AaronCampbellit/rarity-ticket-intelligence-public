// Package sla calculates explainable response and resolution timers against
// configured business calendars.
package sla

import (
	"errors"
	"sort"
	"time"
)

var ErrInvalidCalendar = errors.New("invalid business calendar")

type Window struct {
	StartMinute int `json:"start_minute"`
	EndMinute   int `json:"end_minute"`
}

type Calendar struct {
	Location *time.Location
	Weekly   map[time.Weekday][]Window
	Holidays map[string]struct{}
}

func AddBusinessTime(calendar Calendar, startedAt time.Time, duration time.Duration) (time.Time, error) {
	if duration <= 0 || calendar.Location == nil || len(calendar.Weekly) == 0 {
		return time.Time{}, ErrInvalidCalendar
	}
	current := startedAt.In(calendar.Location)
	remaining := duration
	for dayCount := 0; dayCount < 3660; dayCount++ {
		dayStart := time.Date(current.Year(), current.Month(), current.Day(), 0, 0, 0, 0, calendar.Location)
		dateKey := dayStart.Format("2006-01-02")
		if _, holiday := calendar.Holidays[dateKey]; !holiday {
			windows := append([]Window(nil), calendar.Weekly[dayStart.Weekday()]...)
			sort.Slice(windows, func(i, j int) bool { return windows[i].StartMinute < windows[j].StartMinute })
			for _, window := range windows {
				if window.StartMinute < 0 ||
					window.EndMinute > 24*60 ||
					window.EndMinute <= window.StartMinute {
					return time.Time{}, ErrInvalidCalendar
				}
				windowStart := dayStart.Add(time.Duration(window.StartMinute) * time.Minute)
				windowEnd := dayStart.Add(time.Duration(window.EndMinute) * time.Minute)
				if !current.Before(windowEnd) {
					continue
				}
				if current.Before(windowStart) {
					current = windowStart
				}
				available := windowEnd.Sub(current)
				if remaining <= available {
					return current.Add(remaining), nil
				}
				remaining -= available
				current = windowEnd
			}
		}
		current = dayStart.AddDate(0, 0, 1)
	}
	return time.Time{}, ErrInvalidCalendar
}

func BusinessTimeBetween(
	calendar Calendar,
	startedAt time.Time,
	endedAt time.Time,
) (time.Duration, error) {
	if calendar.Location == nil || len(calendar.Weekly) == 0 || endedAt.Before(startedAt) {
		return 0, ErrInvalidCalendar
	}
	start := startedAt.In(calendar.Location)
	end := endedAt.In(calendar.Location)
	if start.Equal(end) {
		return 0, nil
	}
	var elapsed time.Duration
	currentDay := time.Date(
		start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, calendar.Location,
	)
	for dayCount := 0; dayCount < 3660 && currentDay.Before(end); dayCount++ {
		if _, holiday := calendar.Holidays[currentDay.Format("2006-01-02")]; !holiday {
			windows := append([]Window(nil), calendar.Weekly[currentDay.Weekday()]...)
			sort.Slice(windows, func(i, j int) bool {
				return windows[i].StartMinute < windows[j].StartMinute
			})
			for index, window := range windows {
				if window.StartMinute < 0 || window.EndMinute > 24*60 ||
					window.EndMinute <= window.StartMinute ||
					index > 0 && windows[index-1].EndMinute > window.StartMinute {
					return 0, ErrInvalidCalendar
				}
				windowStart := currentDay.Add(time.Duration(window.StartMinute) * time.Minute)
				windowEnd := currentDay.Add(time.Duration(window.EndMinute) * time.Minute)
				overlapStart := windowStart
				if start.After(overlapStart) {
					overlapStart = start
				}
				overlapEnd := windowEnd
				if end.Before(overlapEnd) {
					overlapEnd = end
				}
				if overlapEnd.After(overlapStart) {
					elapsed += overlapEnd.Sub(overlapStart)
				}
			}
		}
		currentDay = currentDay.AddDate(0, 0, 1)
	}
	if currentDay.Before(end) {
		return 0, ErrInvalidCalendar
	}
	return elapsed, nil
}

type State string

const (
	Running   State = "running"
	Paused    State = "paused"
	Warning   State = "warning"
	Breached  State = "breached"
	Met       State = "met"
	Cancelled State = "cancelled"
)

type Target struct {
	StartedAt time.Time
	WarningAt time.Time
	DueAt     time.Time
	MetAt     *time.Time
	Paused    bool
}

func Evaluate(target Target, now time.Time) State {
	if target.MetAt != nil {
		if target.MetAt.After(target.DueAt) {
			return Breached
		}
		return Met
	}
	if target.Paused {
		return Paused
	}
	if !now.Before(target.DueAt) {
		return Breached
	}
	if !target.WarningAt.IsZero() && !now.Before(target.WarningAt) {
		return Warning
	}
	return Running
}

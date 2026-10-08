package calendar

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
)

var ErrInvalidAvailabilityWindow = errors.New("invalid calendar availability window")

// TimeInterval is a half-open instant interval [Start, End).
type TimeInterval struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func (v TimeInterval) valid() bool { return !v.Start.IsZero() && v.End.After(v.Start) }

type AvailabilityState string

const (
	AvailabilityAvailable   AvailabilityState = "available"
	AvailabilityUnavailable AvailabilityState = "unavailable"
)

type AvailabilityAdjustmentKind string

const (
	HolidayAdjustment              AvailabilityAdjustmentKind = "holiday"
	TrainingAdjustment             AvailabilityAdjustmentKind = "training"
	ManualAdjustment               AvailabilityAdjustmentKind = "manual_override"
	ProtectedMaintenanceAdjustment AvailabilityAdjustmentKind = "protected_maintenance"
)

type AvailabilityAdjustment struct {
	Kind            AvailabilityAdjustmentKind
	State           AvailabilityState
	Interval        TimeInterval
	CapacityPercent int
	Source          SafeSourceRef
	Policy          *ConflictPolicyEvidence
}

type AvailabilitySegment struct {
	Interval        TimeInterval
	CapacityPercent int
}

type AvailabilityInput struct {
	Schedules   []workforce.Schedule
	PTO         []workforce.PTORequest
	Adjustments []AvailabilityAdjustment
}

type ResolvedAvailability struct {
	Segments         []AvailabilitySegment
	Tentative        []TimeInterval
	AvailableMinutes int64
	TentativeMinutes int64
}

type AvailabilityRepository interface {
	LoadAvailabilityInput(context.Context, string, string, QueryWindow) (AvailabilityInput, error)
}

type AvailabilityService struct{ repository AvailabilityRepository }

func NewAvailabilityService(repository AvailabilityRepository) *AvailabilityService {
	return &AvailabilityService{repository: repository}
}

func (s *AvailabilityService) Resolve(ctx context.Context, mspID, technicianID string, window QueryWindow) (ResolvedAvailability, error) {
	if s == nil || s.repository == nil || strings.TrimSpace(mspID) == "" || strings.TrimSpace(technicianID) == "" || !window.End.After(window.Start) {
		return ResolvedAvailability{}, ErrInvalidAvailabilityWindow
	}
	input, err := s.repository.LoadAvailabilityInput(ctx, mspID, technicianID, window)
	if err != nil {
		return ResolvedAvailability{}, err
	}
	segments, scheduleTimezone, err := expandScheduleWindows(input.Schedules, window)
	if err != nil {
		return ResolvedAvailability{}, err
	}
	base := append([]AvailabilitySegment(nil), segments...)

	approved := make([]TimeInterval, 0)
	tentative := make([]TimeInterval, 0)
	for _, request := range input.PTO {
		interval, ok := ptoInterval(request, scheduleTimezone, window)
		if !ok {
			continue
		}
		switch request.State {
		case workforce.Approved:
			approved = append(approved, interval)
		case workforce.Requested:
			tentative = append(tentative, interval)
		}
	}
	for _, interval := range mergeTimeIntervals(approved) {
		segments = subtractAvailability(segments, interval)
	}

	adjustments := append([]AvailabilityAdjustment(nil), input.Adjustments...)
	for _, schedule := range input.Schedules {
		adjustments = append(adjustments, scheduleExceptionAdjustments(schedule)...)
	}
	for _, kind := range []AvailabilityAdjustmentKind{HolidayAdjustment, TrainingAdjustment, ManualAdjustment, ProtectedMaintenanceAdjustment} {
		for _, adjustment := range adjustments {
			if adjustment.Kind != kind || !adjustment.Interval.valid() {
				continue
			}
			if adjustment.State == AvailabilityAvailable {
				percent := adjustment.CapacityPercent
				if percent == 0 {
					percent = 100
				}
				segments = overlayAvailability(segments, AvailabilitySegment{Interval: intersectInterval(adjustment.Interval, TimeInterval{Start: window.Start, End: window.End}), CapacityPercent: percent})
			} else {
				segments = subtractAvailability(segments, adjustment.Interval)
			}
		}
	}
	segments = normalizeAvailability(segments)
	tentative = intersectionsWithAvailability(mergeTimeIntervals(tentative), base)
	return ResolvedAvailability{
		Segments: segments, Tentative: tentative,
		AvailableMinutes: availabilityMinutes(segments), TentativeMinutes: intervalMinutes(tentative),
	}, nil
}

func expandScheduleWindows(schedules []workforce.Schedule, window QueryWindow) ([]AvailabilitySegment, string, error) {
	if len(schedules) == 0 {
		return nil, "", nil
	}
	ordered := append([]workforce.Schedule(nil), schedules...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Version > ordered[j].Version })
	defaultTimezone := ordered[0].Timezone
	if _, err := time.LoadLocation(defaultTimezone); err != nil || defaultTimezone == "Local" {
		return nil, "", ErrInvalidAvailabilityWindow
	}
	chosen := map[string]workforce.Schedule{}
	for _, schedule := range ordered {
		location, err := time.LoadLocation(schedule.Timezone)
		if err != nil || schedule.Timezone == "Local" {
			return nil, "", ErrInvalidAvailabilityWindow
		}
		first := localDate(window.Start.In(location)).AddDate(0, 0, -1)
		last := localDate(window.End.In(location)).AddDate(0, 0, 1)
		for day := first; !day.After(last); day = day.AddDate(0, 0, 1) {
			key := day.Format("2006-01-02")
			effectiveFrom := schedule.EffectiveFrom.Format("2006-01-02")
			if _, exists := chosen[key]; exists || key < effectiveFrom || schedule.EffectiveThrough != nil && key > schedule.EffectiveThrough.Format("2006-01-02") {
				continue
			}
			chosen[key] = schedule
		}
	}
	segments := make([]AvailabilitySegment, 0)
	keys := make([]string, 0, len(chosen))
	for key := range chosen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		schedule := chosen[key]
		location, _ := time.LoadLocation(schedule.Timezone)
		day, _ := time.ParseInLocation("2006-01-02", key, location)
		for _, weekly := range schedule.Windows {
			if weekly.Weekday != day.Weekday() || weekly.EndsMinute <= weekly.StartsMinute {
				continue
			}
			start, startExact := exactScheduleLocalMinute(day, weekly.StartsMinute, location)
			end, endExact := exactScheduleLocalMinute(day, weekly.EndsMinute, location)
			if !startExact || !endExact || !end.After(start) {
				continue
			}
			interval := intersectInterval(TimeInterval{Start: start.UTC(), End: end.UTC()}, TimeInterval{Start: window.Start, End: window.End})
			if !interval.valid() {
				continue
			}
			percent := weekly.CapacityPercent
			if percent == 0 {
				percent = 100
			}
			segments = append(segments, AvailabilitySegment{Interval: interval, CapacityPercent: percent})
		}
	}
	return normalizeAvailability(segments), defaultTimezone, nil
}

func scheduleExceptionAdjustments(schedule workforce.Schedule) []AvailabilityAdjustment {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return nil
	}
	result := make([]AvailabilityAdjustment, 0, len(schedule.Exceptions))
	for _, exception := range schedule.Exceptions {
		day := time.Date(exception.ExceptionOn.Year(), exception.ExceptionOn.Month(), exception.ExceptionOn.Day(), 0, 0, 0, 0, location)
		start, end := day, day.AddDate(0, 0, 1)
		if !exception.AllDay {
			var startExact, endExact bool
			start, startExact = exactScheduleLocalMinute(day, exception.StartsMinute, location)
			end, endExact = exactScheduleLocalMinute(day, exception.EndsMinute, location)
			if !startExact || !endExact || !end.After(start) {
				continue
			}
		}
		kind := ManualAdjustment
		reason := strings.ToLower(strings.TrimSpace(exception.Reason))
		if strings.HasPrefix(reason, "holiday") {
			kind = HolidayAdjustment
		}
		if strings.HasPrefix(reason, "training") {
			kind = TrainingAdjustment
		}
		state := AvailabilityUnavailable
		if exception.State == workforce.Available {
			state = AvailabilityAvailable
		}
		result = append(result, AvailabilityAdjustment{Kind: kind, State: state, Interval: TimeInterval{Start: start.UTC(), End: end.UTC()}, CapacityPercent: exception.CapacityPercent})
	}
	return result
}

func ptoInterval(request workforce.PTORequest, fallbackTimezone string, window QueryWindow) (TimeInterval, bool) {
	if request.AllDay {
		zone := request.Timezone
		if zone == "" {
			zone = fallbackTimezone
		}
		location, err := time.LoadLocation(zone)
		if err != nil || request.StartsOn == nil {
			return TimeInterval{}, false
		}
		start := time.Date(request.StartsOn.Year(), request.StartsOn.Month(), request.StartsOn.Day(), 0, 0, 0, 0, location)
		endDate := request.StartsOn
		if request.EndsOn != nil {
			endDate = request.EndsOn
		}
		end := time.Date(endDate.Year(), endDate.Month(), endDate.Day(), 0, 0, 0, 0, location).AddDate(0, 0, 1)
		interval := intersectInterval(TimeInterval{Start: start.UTC(), End: end.UTC()}, TimeInterval{Start: window.Start, End: window.End})
		return interval, interval.valid()
	}
	if request.StartsAt == nil || request.EndsAt == nil {
		return TimeInterval{}, false
	}
	interval := intersectInterval(TimeInterval{Start: request.StartsAt.UTC(), End: request.EndsAt.UTC()}, TimeInterval{Start: window.Start, End: window.End})
	return interval, interval.valid()
}

func overlayAvailability(segments []AvailabilitySegment, added AvailabilitySegment) []AvailabilitySegment {
	if !added.Interval.valid() || added.CapacityPercent < 1 || added.CapacityPercent > 100 {
		return segments
	}
	segments = subtractAvailability(segments, added.Interval)
	return append(segments, added)
}

func subtractAvailability(segments []AvailabilitySegment, removed TimeInterval) []AvailabilitySegment {
	if !removed.valid() {
		return segments
	}
	result := make([]AvailabilitySegment, 0, len(segments)+1)
	for _, segment := range segments {
		if !overlaps(segment.Interval, removed) {
			result = append(result, segment)
			continue
		}
		if segment.Interval.Start.Before(removed.Start) {
			result = append(result, AvailabilitySegment{Interval: TimeInterval{Start: segment.Interval.Start, End: minTime(segment.Interval.End, removed.Start)}, CapacityPercent: segment.CapacityPercent})
		}
		if removed.End.Before(segment.Interval.End) {
			result = append(result, AvailabilitySegment{Interval: TimeInterval{Start: maxTime(segment.Interval.Start, removed.End), End: segment.Interval.End}, CapacityPercent: segment.CapacityPercent})
		}
	}
	return normalizeAvailability(result)
}

func normalizeAvailability(input []AvailabilitySegment) []AvailabilitySegment {
	segments := make([]AvailabilitySegment, 0, len(input))
	for _, segment := range input {
		if segment.Interval.valid() && segment.CapacityPercent > 0 {
			segments = append(segments, segment)
		}
	}
	sort.Slice(segments, func(i, j int) bool { return segments[i].Interval.Start.Before(segments[j].Interval.Start) })
	result := make([]AvailabilitySegment, 0, len(segments))
	for _, segment := range segments {
		if len(result) > 0 && result[len(result)-1].CapacityPercent == segment.CapacityPercent && !segment.Interval.Start.After(result[len(result)-1].Interval.End) {
			if segment.Interval.End.After(result[len(result)-1].Interval.End) {
				result[len(result)-1].Interval.End = segment.Interval.End
			}
			continue
		}
		result = append(result, segment)
	}
	return result
}

func intersectionsWithAvailability(intervals []TimeInterval, availability []AvailabilitySegment) []TimeInterval {
	result := []TimeInterval{}
	for _, interval := range intervals {
		for _, segment := range availability {
			if v := intersectInterval(interval, segment.Interval); v.valid() {
				result = append(result, v)
			}
		}
	}
	return mergeTimeIntervals(result)
}

func mergeTimeIntervals(input []TimeInterval) []TimeInterval {
	intervals := append([]TimeInterval(nil), input...)
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].Start.Before(intervals[j].Start) })
	result := []TimeInterval{}
	for _, interval := range intervals {
		if !interval.valid() {
			continue
		}
		if len(result) > 0 && !interval.Start.After(result[len(result)-1].End) {
			if interval.End.After(result[len(result)-1].End) {
				result[len(result)-1].End = interval.End
			}
			continue
		}
		result = append(result, interval)
	}
	return result
}

func availabilityMinutes(segments []AvailabilitySegment) int64 {
	var total int64
	for _, segment := range segments {
		total += int64(segment.Interval.End.Sub(segment.Interval.Start)/time.Minute) * int64(segment.CapacityPercent) / 100
	}
	return total
}
func intervalMinutes(intervals []TimeInterval) int64 {
	var total int64
	for _, v := range intervals {
		total += int64(v.End.Sub(v.Start) / time.Minute)
	}
	return total
}
func localDate(v time.Time) time.Time {
	y, m, d := v.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, v.Location())
}
func localMinute(day time.Time, minute int, location *time.Location) time.Time {
	if minute == 1440 {
		return time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, location)
	}
	return time.Date(day.Year(), day.Month(), day.Day(), minute/60, minute%60, 0, 0, location)
}

func exactScheduleLocalMinute(day time.Time, minute int, location *time.Location) (time.Time, bool) {
	if minute == 1440 {
		day = day.AddDate(0, 0, 1)
		minute = 0
	}
	year, month, date := day.Date()
	pseudo := time.Date(year, month, date, minute/60, minute%60, 0, 0, time.UTC)
	seed := time.Date(year, month, date, minute/60, minute%60, 0, 0, location)
	offsets := map[int]struct{}{}
	for _, delta := range []time.Duration{-48 * time.Hour, -24 * time.Hour, -6 * time.Hour, -3 * time.Hour, 0, 3 * time.Hour, 6 * time.Hour, 24 * time.Hour, 48 * time.Hour} {
		_, offset := seed.Add(delta).Zone()
		offsets[offset] = struct{}{}
	}
	var earliest time.Time
	for offset := range offsets {
		candidate := pseudo.Add(-time.Duration(offset) * time.Second)
		local := candidate.In(location)
		if local.Year() != year || local.Month() != month || local.Day() != date || local.Hour() != minute/60 || local.Minute() != minute%60 || local.Second() != 0 {
			continue
		}
		if earliest.IsZero() || candidate.Before(earliest) {
			earliest = candidate
		}
	}
	return earliest, !earliest.IsZero()
}

func AvailabilityGaps(segments []AvailabilitySegment, target TimeInterval) []TimeInterval {
	if !target.valid() {
		return nil
	}
	covered := make([]TimeInterval, 0, len(segments))
	for _, segment := range segments {
		if segment.CapacityPercent <= 0 {
			continue
		}
		if interval := intersectInterval(segment.Interval, target); interval.valid() {
			covered = append(covered, interval)
		}
	}
	covered = mergeTimeIntervals(covered)
	cursor := target.Start
	gaps := []TimeInterval{}
	for _, interval := range covered {
		if interval.Start.After(cursor) {
			gaps = append(gaps, TimeInterval{Start: cursor, End: interval.Start})
		}
		if interval.End.After(cursor) {
			cursor = interval.End
		}
	}
	if cursor.Before(target.End) {
		gaps = append(gaps, TimeInterval{Start: cursor, End: target.End})
	}
	return gaps
}
func overlaps(a, b TimeInterval) bool { return a.Start.Before(b.End) && b.Start.Before(a.End) }
func intersectInterval(a, b TimeInterval) TimeInterval {
	return TimeInterval{Start: maxTime(a.Start, b.Start), End: minTime(a.End, b.End)}
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

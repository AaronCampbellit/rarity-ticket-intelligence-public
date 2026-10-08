package calendar

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	ErrInvalidRecurrence  = errors.New("invalid calendar recurrence")
	ErrInvalidQueryWindow = errors.New("invalid calendar query window")
	ErrExpansionLimit     = errors.New("calendar recurrence expansion limit exceeded")
)

const (
	maxExpandedOccurrences = 10_000
	maxExpansionIterations = 1_000_000
	maxQueryWindow         = 10 * 366 * 24 * time.Hour
)

type Frequency string

const (
	Daily   Frequency = "daily"
	Weekly  Frequency = "weekly"
	Monthly Frequency = "monthly"
	Yearly  Frequency = "yearly"
)

type RecurrenceRule struct {
	Frequency Frequency      `json:"frequency"`
	Interval  int            `json:"interval"`
	Weekdays  []time.Weekday `json:"weekdays,omitempty"`
	Count     int            `json:"count,omitempty"`
	Until     *time.Time     `json:"until,omitempty"`
}

func (r RecurrenceRule) Validate() error {
	if r.Frequency != Daily && r.Frequency != Weekly && r.Frequency != Monthly && r.Frequency != Yearly {
		return fmt.Errorf("%w: unsupported frequency %q", ErrInvalidRecurrence, r.Frequency)
	}
	if r.Interval <= 0 || r.Count < 0 || r.Count > maxExpandedOccurrences || (r.Count > 0 && r.Until != nil) {
		return fmt.Errorf("%w: interval must be positive and count/until are exclusive", ErrInvalidRecurrence)
	}
	if r.Until != nil && r.Until.IsZero() {
		return fmt.Errorf("%w: until cannot be zero", ErrInvalidRecurrence)
	}
	if r.Frequency != Weekly && len(r.Weekdays) != 0 {
		return fmt.Errorf("%w: weekdays are supported only for weekly recurrence", ErrInvalidRecurrence)
	}
	if r.Frequency == Weekly {
		if len(r.Weekdays) == 0 || len(r.Weekdays) > 7 {
			return fmt.Errorf("%w: weekly recurrence needs weekdays", ErrInvalidRecurrence)
		}
		seen := make(map[time.Weekday]struct{}, len(r.Weekdays))
		for _, weekday := range r.Weekdays {
			if weekday < time.Sunday || weekday > time.Saturday {
				return fmt.Errorf("%w: invalid weekday %d", ErrInvalidRecurrence, weekday)
			}
			if _, exists := seen[weekday]; exists {
				return fmt.Errorf("%w: duplicate weekday %d", ErrInvalidRecurrence, weekday)
			}
			seen[weekday] = struct{}{}
		}
	}
	return nil
}

type RecurrenceExceptionState string

const (
	ExceptionCancelled   RecurrenceExceptionState = "cancelled"
	ExceptionRescheduled RecurrenceExceptionState = "rescheduled"
	ExceptionOverridden  RecurrenceExceptionState = "overridden"
)

type RecurrenceException struct {
	OriginalLocalKey string
	OccurrenceScope  OccurrenceScope
	State            RecurrenceExceptionState
	StartsOn         *time.Time
	EndsOn           *time.Time
	StartsAt         *time.Time
	EndsAt           *time.Time
}

type Occurrence struct {
	ID               string
	ProjectionID     string
	OriginalLocalKey string
	AllDay           bool
	StartsOn         *time.Time
	EndsOn           *time.Time
	StartsAt         time.Time
	EndsAt           time.Time
	Timezone         string
}

// ExpandOccurrences expands a projection only far enough to answer a bounded
// query and preserves each occurrence's original local key across exceptions.
func ExpandOccurrences(projection Projection, window QueryWindow, exceptions []RecurrenceException) ([]Occurrence, error) {
	if projection.Source.Type == "custom_date" {
		return nil, fmt.Errorf("%w: custom-date expansion requires a configured role registry", ErrInvalidRole)
	}
	return expandOccurrences(projection, window, exceptions)
}

// ExpandOccurrencesWithRegistry binds dynamic custom-date projections to the
// exact configured role and field identity before expansion.
func ExpandOccurrencesWithRegistry(registry *RoleRegistry, projection Projection, window QueryWindow, exceptions []RecurrenceException) ([]Occurrence, error) {
	if registry == nil {
		return nil, fmt.Errorf("%w: role registry is required", ErrInvalidRole)
	}
	if err := registry.ValidateProjection(projection); err != nil {
		return nil, err
	}
	return expandOccurrences(projection, window, exceptions)
}

func expandOccurrences(projection Projection, window QueryWindow, exceptions []RecurrenceException) ([]Occurrence, error) {
	if window.Start.IsZero() || window.End.IsZero() || !window.End.After(window.Start) || window.End.Sub(window.Start) > maxQueryWindow {
		return nil, ErrInvalidQueryWindow
	}
	if err := projection.Validate(); err != nil {
		return nil, err
	}
	if projection.Recurrence == nil {
		if len(exceptions) != 0 {
			return nil, fmt.Errorf("%w: exceptions require a recurrence", ErrInvalidRecurrence)
		}
		occurrence := occurrenceFromProjection(projection)
		if overlapsWindow(occurrence, window) {
			return []Occurrence{occurrence}, nil
		}
		return nil, nil
	}
	if err := projection.Recurrence.Validate(); err != nil {
		return nil, err
	}
	if len(exceptions) > maxExpandedOccurrences {
		return nil, ErrExpansionLimit
	}

	exceptionByKey, err := validateExceptions(projection, exceptions)
	if err != nil {
		return nil, err
	}
	var countIndex map[string]Occurrence
	if projection.Recurrence.Count > 0 && len(exceptionByKey) > 0 {
		countIndex, err = indexCountOccurrences(projection)
		if err != nil {
			return nil, err
		}
	}
	iterator, err := newRecurrenceIterator(projection)
	if err != nil {
		return nil, err
	}

	found := make([]Occurrence, 0)
	seenExceptions := make(map[string]struct{}, len(exceptionByKey))
	generated := 0
	futureSplits := make([]RecurrenceException, 0)
	for _, exception := range exceptionByKey {
		if exception.OccurrenceScope == ThisAndFuture {
			futureSplits = append(futureSplits, exception)
		}
	}
	sort.Slice(futureSplits, func(i, j int) bool { return futureSplits[i].OriginalLocalKey < futureSplits[j].OriginalLocalKey })
	for iterations := 0; iterations < maxExpansionIterations; iterations++ {
		candidate, ok := iterator.next()
		if !ok {
			break
		}
		if projection.Recurrence.Until != nil && candidate.afterUntil(*projection.Recurrence.Until) {
			break
		}
		if !candidate.beforeHorizon(window.End) {
			break
		}

		occurrence, valid := candidate.occurrence(projection)
		if !valid {
			continue
		}
		generated++
		if projection.Recurrence.Count > 0 && generated > projection.Recurrence.Count {
			break
		}
		for splitIndex := len(futureSplits) - 1; splitIndex >= 0; splitIndex-- {
			split := futureSplits[splitIndex]
			if split.OriginalLocalKey >= occurrence.OriginalLocalKey {
				continue
			}
			origin, originErr := occurrenceForExceptionKey(projection, split.OriginalLocalKey, countIndex)
			if originErr != nil {
				return nil, originErr
			}
			occurrence, originErr = applyFutureSplit(occurrence, origin, split)
			if originErr != nil {
				return nil, originErr
			}
			break
		}
		if exception, exists := exceptionByKey[occurrence.OriginalLocalKey]; exists {
			seenExceptions[occurrence.OriginalLocalKey] = struct{}{}
			if exception.State == ExceptionCancelled {
				if generationComplete(projection, generated) {
					break
				}
				continue
			}
			occurrence, err = applyException(occurrence, exception, projection)
			if err != nil {
				return nil, err
			}
		}
		if overlapsWindow(occurrence, window) {
			found = append(found, occurrence)
			if len(found) > maxExpandedOccurrences {
				return nil, ErrExpansionLimit
			}
		}
		if generationComplete(projection, generated) {
			break
		}
		if iterations == maxExpansionIterations-1 {
			return nil, ErrExpansionLimit
		}
	}
	for key, exception := range exceptionByKey {
		if _, seen := seenExceptions[key]; seen {
			continue
		}
		occurrence, err := occurrenceForExceptionKey(projection, key, countIndex)
		if err != nil {
			return nil, err
		}
		if exception.State == ExceptionCancelled {
			continue
		}
		occurrence, err = applyException(occurrence, exception, projection)
		if err != nil {
			return nil, err
		}
		if overlapsWindow(occurrence, window) {
			found = append(found, occurrence)
			if len(found) > maxExpandedOccurrences {
				return nil, ErrExpansionLimit
			}
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		left, right := occurrenceSortTime(found[i]), occurrenceSortTime(found[j])
		if left.Equal(right) {
			return found[i].ID < found[j].ID
		}
		return left.Before(right)
	})
	return found, nil
}

type recurrenceCandidate struct {
	allDay  bool
	date    time.Time
	instant time.Time
}

func (c recurrenceCandidate) afterUntil(until time.Time) bool {
	if c.allDay {
		return c.date.After(until)
	}
	return c.instant.After(until)
}

func (c recurrenceCandidate) beforeHorizon(horizon time.Time) bool {
	if c.allDay {
		return c.date.Before(horizon)
	}
	return c.instant.Before(horizon)
}

func (c recurrenceCandidate) key(location *time.Location) string {
	if c.allDay {
		return c.date.Format("2006-01-02")
	}
	return c.instant.In(location).Format("2006-01-02T15:04:05")
}

func (c recurrenceCandidate) occurrence(projection Projection) (Occurrence, bool) {
	location := time.UTC
	if !projection.AllDay {
		location, _ = time.LoadLocation(projection.Timezone)
	}
	occurrence := Occurrence{
		ProjectionID: projection.ID,
		AllDay:       projection.AllDay,
		Timezone:     projection.Timezone,
	}
	occurrence.OriginalLocalKey = c.key(location)
	occurrence.ID = projection.ID + ":" + occurrence.OriginalLocalKey
	if projection.AllDay {
		start := c.date
		occurrence.StartsOn = &start
		if projection.EndsOn != nil {
			days := int(projection.EndsOn.Sub(*projection.StartsOn) / (24 * time.Hour))
			end := start.AddDate(0, 0, days)
			occurrence.EndsOn = &end
		}
	} else {
		occurrence.StartsAt = c.instant
		if projection.EndsAt != nil {
			end, ok := recurringWallEnd(c.instant, projection)
			if !ok {
				return Occurrence{}, false
			}
			occurrence.EndsAt = end
		}
	}
	return occurrence, true
}

type recurrenceIterator struct {
	projection Projection
	rule       RecurrenceRule
	location   *time.Location
	startLocal time.Time
	period     int
	weekdays   []time.Weekday
	weekdayAt  int
	weekBase   time.Time
}

func newRecurrenceIterator(projection Projection) (*recurrenceIterator, error) {
	location := time.UTC
	var start time.Time
	if projection.AllDay {
		start = *projection.StartsOn
	} else {
		var err error
		location, err = time.LoadLocation(projection.Timezone)
		if err != nil {
			return nil, fmt.Errorf("%w: timezone: %v", ErrInvalidRecurrence, err)
		}
		start = projection.StartsAt.In(location)
	}
	iterator := &recurrenceIterator{projection: projection, rule: *projection.Recurrence, location: location, startLocal: start}
	if iterator.rule.Frequency == Weekly {
		iterator.weekdays = append([]time.Weekday(nil), iterator.rule.Weekdays...)
		sort.Slice(iterator.weekdays, func(i, j int) bool { return iterator.weekdays[i] < iterator.weekdays[j] })
		iterator.weekBase = start.AddDate(0, 0, -int(start.Weekday()))
	}
	return iterator, nil
}

func (i *recurrenceIterator) next() (recurrenceCandidate, bool) {
	for {
		var local time.Time
		var exact bool
		switch i.rule.Frequency {
		case Daily:
			date := calendarDate(i.startLocal).AddDate(0, 0, i.period*i.rule.Interval)
			local, exact = exactLocalDateTime(date.Year(), date.Month(), date.Day(), i.startLocal, i.location)
			i.period++
		case Weekly:
			weekday := i.weekdays[i.weekdayAt]
			date := calendarDate(i.weekBase).AddDate(0, 0, i.period*i.rule.Interval*7+int(weekday))
			local, exact = exactLocalDateTime(date.Year(), date.Month(), date.Day(), i.startLocal, i.location)
			i.weekdayAt++
			if i.weekdayAt == len(i.weekdays) {
				i.weekdayAt = 0
				i.period++
			}
			if local.Before(i.startLocal) {
				continue
			}
		case Monthly:
			year, month := addMonths(i.startLocal.Year(), i.startLocal.Month(), i.period*i.rule.Interval)
			i.period++
			if i.startLocal.Day() > daysInMonth(year, month) {
				continue
			}
			local, exact = exactLocalDateTime(year, month, i.startLocal.Day(), i.startLocal, i.location)
		case Yearly:
			year := i.startLocal.Year() + i.period*i.rule.Interval
			i.period++
			if i.startLocal.Day() > daysInMonth(year, i.startLocal.Month()) {
				continue
			}
			local, exact = exactLocalDateTime(year, i.startLocal.Month(), i.startLocal.Day(), i.startLocal, i.location)
		default:
			return recurrenceCandidate{}, false
		}
		if !exact {
			continue
		}
		if sameLocalDateTime(local, i.startLocal) {
			local = i.startLocal
		}
		if i.projection.AllDay {
			return recurrenceCandidate{allDay: true, date: time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)}, true
		}
		return recurrenceCandidate{instant: local}, true
	}
}

func calendarDate(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func exactLocalDateTime(year int, month time.Month, day int, clock time.Time, location *time.Location) (time.Time, bool) {
	value := time.Date(year, month, day, clock.Hour(), clock.Minute(), clock.Second(), clock.Nanosecond(), location)
	return value, value.Year() == year && value.Month() == month && value.Day() == day &&
		value.Hour() == clock.Hour() && value.Minute() == clock.Minute() &&
		value.Second() == clock.Second() && value.Nanosecond() == clock.Nanosecond()
}

func sameLocalDateTime(left, right time.Time) bool {
	return left.Year() == right.Year() && left.Month() == right.Month() && left.Day() == right.Day() &&
		left.Hour() == right.Hour() && left.Minute() == right.Minute() && left.Second() == right.Second() &&
		left.Nanosecond() == right.Nanosecond()
}

func addMonths(year int, month time.Month, offset int) (int, time.Month) {
	zeroBased := year*12 + int(month) - 1 + offset
	return zeroBased / 12, time.Month(zeroBased%12 + 1)
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func validateExceptions(projection Projection, exceptions []RecurrenceException) (map[string]RecurrenceException, error) {
	found := make(map[string]RecurrenceException, len(exceptions))
	location := time.UTC
	if !projection.AllDay {
		location, _ = time.LoadLocation(projection.Timezone)
	}
	for _, exception := range exceptions {
		if exception.OccurrenceScope == "" {
			exception.OccurrenceScope = ThisOccurrence
		}
		if exception.OccurrenceScope != ThisOccurrence && exception.OccurrenceScope != ThisAndFuture {
			return nil, fmt.Errorf("%w: invalid exception occurrence scope", ErrInvalidRecurrence)
		}
		if exception.OccurrenceScope == ThisAndFuture && exception.State != ExceptionRescheduled {
			return nil, fmt.Errorf("%w: future split must be rescheduled", ErrInvalidRecurrence)
		}
		if exception.OriginalLocalKey == "" {
			return nil, fmt.Errorf("%w: exception original key is required", ErrInvalidRecurrence)
		}
		if _, duplicate := found[exception.OriginalLocalKey]; duplicate {
			return nil, fmt.Errorf("%w: duplicate exception key", ErrInvalidRecurrence)
		}
		_, err := parseOriginalKey(exception.OriginalLocalKey, projection.AllDay, location)
		if err != nil {
			return nil, err
		}
		if err := validateExceptionShape(projection.AllDay, exception); err != nil {
			return nil, err
		}
		found[exception.OriginalLocalKey] = exception
	}
	return found, nil
}

func applyFutureSplit(occurrence, origin Occurrence, split RecurrenceException) (Occurrence, error) {
	if occurrence.AllDay {
		if origin.StartsOn == nil || split.StartsOn == nil {
			return Occurrence{}, ErrInvalidRecurrence
		}
		days := int(split.StartsOn.Sub(*origin.StartsOn) / (24 * time.Hour))
		start := occurrence.StartsOn.AddDate(0, 0, days)
		occurrence.StartsOn = &start
		if occurrence.EndsOn != nil {
			end := occurrence.EndsOn.AddDate(0, 0, days)
			occurrence.EndsOn = &end
		}
		return occurrence, nil
	}
	if split.StartsAt == nil || origin.StartsAt.IsZero() {
		return Occurrence{}, ErrInvalidRecurrence
	}
	location, err := time.LoadLocation(occurrence.Timezone)
	if err != nil {
		return Occurrence{}, ErrInvalidRecurrence
	}
	wallDelta := pseudoWallTime(split.StartsAt.In(location)).Sub(pseudoWallTime(origin.StartsAt.In(location)))
	localStart := occurrence.StartsAt.In(location)
	shiftedStart := pseudoWallTime(localStart).Add(wallDelta)
	start, exact := exactLocalDateTime(shiftedStart.Year(), shiftedStart.Month(), shiftedStart.Day(), shiftedStart, location)
	if !exact {
		return Occurrence{}, fmt.Errorf("%w: future split enters a nonexistent local wall time", ErrInvalidRecurrence)
	}
	occurrence.StartsAt = start
	if !occurrence.EndsAt.IsZero() {
		localEnd := occurrence.EndsAt.In(location)
		shiftedEnd := pseudoWallTime(localEnd).Add(wallDelta)
		end, endExact := exactLocalDateTime(shiftedEnd.Year(), shiftedEnd.Month(), shiftedEnd.Day(), shiftedEnd, location)
		if !endExact || !end.After(start) {
			return Occurrence{}, fmt.Errorf("%w: future split produces an invalid local end", ErrInvalidRecurrence)
		}
		occurrence.EndsAt = end
	}
	return occurrence, nil
}

func parseOriginalKey(key string, allDay bool, location *time.Location) (time.Time, error) {
	format := "2006-01-02T15:04:05"
	if allDay {
		format = "2006-01-02"
	}
	value, err := time.ParseInLocation(format, key, location)
	if err != nil || value.Format(format) != key {
		return time.Time{}, fmt.Errorf("%w: malformed original local key %q", ErrInvalidRecurrence, key)
	}
	return value, nil
}

func validateExceptionShape(allDay bool, exception RecurrenceException) error {
	switch exception.State {
	case ExceptionCancelled:
		if exception.StartsOn != nil || exception.EndsOn != nil || exception.StartsAt != nil || exception.EndsAt != nil {
			return fmt.Errorf("%w: cancelled exceptions cannot carry replacement dates", ErrInvalidRecurrence)
		}
	case ExceptionRescheduled, ExceptionOverridden:
		if allDay {
			if exception.StartsOn == nil || !isDate(*exception.StartsOn) || exception.StartsAt != nil || exception.EndsAt != nil ||
				(exception.EndsOn != nil && (!isDate(*exception.EndsOn) || exception.EndsOn.Before(*exception.StartsOn))) {
				return fmt.Errorf("%w: all-day exception needs date fields", ErrInvalidRecurrence)
			}
		} else if exception.StartsAt == nil || exception.StartsOn != nil || exception.EndsOn != nil ||
			(exception.EndsAt != nil && !exception.EndsAt.After(*exception.StartsAt)) {
			return fmt.Errorf("%w: timed exception needs timestamp fields", ErrInvalidRecurrence)
		}
	default:
		return fmt.Errorf("%w: unknown exception state %q", ErrInvalidRecurrence, exception.State)
	}
	return nil
}

func applyException(occurrence Occurrence, exception RecurrenceException, projection Projection) (Occurrence, error) {
	if occurrence.AllDay {
		start := *exception.StartsOn
		occurrence.StartsOn = &start
		if exception.EndsOn != nil {
			end := *exception.EndsOn
			occurrence.EndsOn = &end
		} else if projection.EndsOn != nil {
			days := int(projection.EndsOn.Sub(*projection.StartsOn) / (24 * time.Hour))
			end := start.AddDate(0, 0, days)
			occurrence.EndsOn = &end
		} else {
			occurrence.EndsOn = nil
		}
		return occurrence, nil
	}
	occurrence.StartsAt = *exception.StartsAt
	if exception.EndsAt != nil {
		occurrence.EndsAt = *exception.EndsAt
	} else if projection.EndsAt != nil {
		end, ok := recurringWallEnd(occurrence.StartsAt, projection)
		if !ok {
			return Occurrence{}, fmt.Errorf("%w: exception default end is not a valid local wall time", ErrInvalidRecurrence)
		}
		occurrence.EndsAt = end
	} else {
		occurrence.EndsAt = time.Time{}
	}
	return occurrence, nil
}

func generationComplete(projection Projection, generated int) bool {
	if projection.Recurrence.Count > 0 {
		return generated >= projection.Recurrence.Count
	}
	return false
}

func occurrenceForExceptionKey(projection Projection, key string, countIndex map[string]Occurrence) (Occurrence, error) {
	location := time.UTC
	if !projection.AllDay {
		location, _ = time.LoadLocation(projection.Timezone)
	}
	original, err := parseOriginalKey(key, projection.AllDay, location)
	if err != nil {
		return Occurrence{}, err
	}
	candidate := recurrenceCandidate{allDay: projection.AllDay}
	if projection.AllDay {
		candidate.date = original
	} else {
		candidate.instant = original
	}
	if projection.Recurrence.Count > 0 {
		occurrence, matches := countIndex[key]
		if !matches {
			return Occurrence{}, fmt.Errorf("%w: exception does not identify an occurrence", ErrInvalidRecurrence)
		}
		return occurrence, nil
	} else if !candidateMatchesRule(projection, candidate) ||
		(projection.Recurrence.Until != nil && candidate.afterUntil(*projection.Recurrence.Until)) {
		return Occurrence{}, fmt.Errorf("%w: exception does not identify an occurrence", ErrInvalidRecurrence)
	}
	occurrence, valid := candidate.occurrence(projection)
	if !valid {
		return Occurrence{}, fmt.Errorf("%w: exception identifies a nonexistent local occurrence", ErrInvalidRecurrence)
	}
	return occurrence, nil
}

func indexCountOccurrences(projection Projection) (map[string]Occurrence, error) {
	iterator, err := newRecurrenceIterator(projection)
	if err != nil {
		return nil, err
	}
	index := make(map[string]Occurrence, projection.Recurrence.Count)
	generated := 0
	for iterations := 0; iterations < maxExpansionIterations; iterations++ {
		candidate, ok := iterator.next()
		if !ok {
			return index, nil
		}
		if projection.Recurrence.Until != nil && candidate.afterUntil(*projection.Recurrence.Until) {
			return index, nil
		}
		occurrence, valid := candidate.occurrence(projection)
		if !valid {
			continue
		}
		generated++
		index[occurrence.OriginalLocalKey] = occurrence
		if generated >= projection.Recurrence.Count {
			return index, nil
		}
	}
	return nil, ErrExpansionLimit
}

func candidateMatchesRule(projection Projection, candidate recurrenceCandidate) bool {
	location := time.UTC
	var start, value time.Time
	if projection.AllDay {
		start = *projection.StartsOn
		value = candidate.date
	} else {
		location, _ = time.LoadLocation(projection.Timezone)
		start = projection.StartsAt.In(location)
		value = candidate.instant.In(location)
		if start.Hour() != value.Hour() || start.Minute() != value.Minute() ||
			start.Second() != value.Second() || start.Nanosecond() != value.Nanosecond() {
			return false
		}
	}
	if value.Before(start) {
		return false
	}
	rule := projection.Recurrence
	switch rule.Frequency {
	case Daily:
		days := civilDayNumber(value) - civilDayNumber(start)
		return days >= 0 && days%int64(rule.Interval) == 0
	case Weekly:
		startWeek := civilDayNumber(start) - int64(start.Weekday())
		valueWeek := civilDayNumber(value) - int64(value.Weekday())
		weeks := (valueWeek - startWeek) / 7
		if weeks < 0 || weeks%int64(rule.Interval) != 0 {
			return false
		}
		for _, weekday := range rule.Weekdays {
			if value.Weekday() == weekday {
				return true
			}
		}
		return false
	case Monthly:
		months := (value.Year()-start.Year())*12 + int(value.Month()-start.Month())
		return months >= 0 && months%rule.Interval == 0 && value.Day() == start.Day()
	case Yearly:
		years := value.Year() - start.Year()
		return years >= 0 && years%rule.Interval == 0 && value.Month() == start.Month() && value.Day() == start.Day()
	default:
		return false
	}
}

func civilDayNumber(value time.Time) int64 {
	year := value.Year()
	month := int(value.Month())
	if month <= 2 {
		year--
	}
	era := year / 400
	yearOfEra := year - era*400
	monthPrime := month - 3
	if monthPrime < 0 {
		monthPrime += 12
	}
	dayOfYear := (153*monthPrime+2)/5 + value.Day() - 1
	dayOfEra := yearOfEra*365 + yearOfEra/4 - yearOfEra/100 + dayOfYear
	return int64(era*146097 + dayOfEra)
}

func occurrenceFromProjection(projection Projection) Occurrence {
	location := time.UTC
	occurrence := Occurrence{
		ProjectionID: projection.ID,
		AllDay:       projection.AllDay,
		Timezone:     projection.Timezone,
	}
	candidate := recurrenceCandidate{allDay: projection.AllDay}
	if projection.AllDay {
		candidate.date = *projection.StartsOn
		start := *projection.StartsOn
		occurrence.StartsOn = &start
		if projection.EndsOn != nil {
			end := *projection.EndsOn
			occurrence.EndsOn = &end
		}
	} else {
		location, _ = time.LoadLocation(projection.Timezone)
		candidate.instant = *projection.StartsAt
		occurrence.StartsAt = *projection.StartsAt
		if projection.EndsAt != nil {
			occurrence.EndsAt = *projection.EndsAt
		}
	}
	occurrence.OriginalLocalKey = candidate.key(location)
	occurrence.ID = projection.ID + ":" + occurrence.OriginalLocalKey
	return occurrence
}

// recurringWallEnd applies the original event's local wall-clock duration to
// a new local start. A nonexistent end in a DST gap makes the occurrence
// invalid instead of accepting time.Date's normalization.
func recurringWallEnd(start time.Time, projection Projection) (time.Time, bool) {
	location, err := time.LoadLocation(projection.Timezone)
	if err != nil || projection.StartsAt == nil || projection.EndsAt == nil {
		return time.Time{}, false
	}
	originalStart := projection.StartsAt.In(location)
	originalEnd := projection.EndsAt.In(location)
	wallDuration := pseudoWallTime(originalEnd).Sub(pseudoWallTime(originalStart))
	if wallDuration <= 0 {
		authoritativeDuration := projection.EndsAt.Sub(*projection.StartsAt)
		if authoritativeDuration <= 0 {
			return time.Time{}, false
		}
		return start.Add(authoritativeDuration), true
	}
	localStart := start.In(location)
	wallEnd := pseudoWallTime(localStart).Add(wallDuration)
	end, exact := exactLocalDateTime(wallEnd.Year(), wallEnd.Month(), wallEnd.Day(), wallEnd, location)
	return end, exact && end.After(start)
}

func pseudoWallTime(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), value.Hour(), value.Minute(), value.Second(), value.Nanosecond(), time.UTC)
}

func overlapsWindow(occurrence Occurrence, window QueryWindow) bool {
	if occurrence.AllDay {
		start := *occurrence.StartsOn
		end := start.AddDate(0, 0, 1)
		if occurrence.EndsOn != nil && occurrence.EndsOn.After(start) {
			end = *occurrence.EndsOn
		}
		return start.Before(window.End) && end.After(window.Start)
	}
	if occurrence.EndsAt.IsZero() {
		return !occurrence.StartsAt.Before(window.Start) && occurrence.StartsAt.Before(window.End)
	}
	return occurrence.StartsAt.Before(window.End) && occurrence.EndsAt.After(window.Start)
}

func occurrenceSortTime(occurrence Occurrence) time.Time {
	if occurrence.AllDay {
		return *occurrence.StartsOn
	}
	return occurrence.StartsAt
}

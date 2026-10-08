package projects

import (
	"context"
	"errors"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidCapacityWindow = errors.New("invalid capacity window")

type Capacity struct {
	Name       string
	Available  time.Duration
	Scheduled  time.Duration
	Actual     time.Duration
	Remaining  time.Duration
	Overbooked time.Duration
}

type CapacityView struct {
	Window    CapacityWindow
	Resources map[string]Capacity
}

func CalculateCapacity(available, scheduled, actual time.Duration) Capacity {
	anchor := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	availableEnd := anchor.Add(available)
	planningEnd := availableEnd
	if used := scheduled + actual; used > available {
		planningEnd = anchor.Add(used)
	}
	input := calendar.CapacityInput{Window: calendar.QueryWindow{Start: anchor, End: planningEnd}}
	if available > 0 {
		input.Available = []calendar.AvailabilitySegment{{Interval: calendar.TimeInterval{Start: anchor, End: availableEnd}, CapacityPercent: 100}}
	}
	if scheduled > 0 {
		input.Events = append(input.Events, calendar.CapacityEvent{Assigned: true, Mode: calendar.FixedBlock, PlannedMinutes: int64(scheduled / time.Minute), Interval: calendar.TimeInterval{Start: anchor, End: anchor.Add(scheduled)}})
	}
	if actual > 0 {
		input.Events = append(input.Events, calendar.CapacityEvent{Assigned: true, Mode: calendar.FixedBlock, PlannedMinutes: int64(actual / time.Minute), Interval: calendar.TimeInterval{Start: anchor.Add(scheduled), End: anchor.Add(scheduled + actual)}})
	}
	unified := calendar.CalculateCapacity(input)
	result := Capacity{Available: available, Scheduled: scheduled, Actual: actual, Remaining: time.Duration(unified.RemainingMinutes) * time.Minute, Overbooked: time.Duration(unified.OverbookedMinutes) * time.Minute}
	return result
}

type CapacityService struct {
	repository CapacityRepository
}

func NewCapacityService(repository CapacityRepository) *CapacityService {
	return &CapacityService{repository: repository}
}

func (s *CapacityService) Calculate(
	ctx context.Context,
	target scope.Target,
	window CapacityWindow,
	resourceIDs []string,
) (CapacityView, error) {
	if target.MSPID == "" || !window.End.After(window.Start) ||
		len(resourceIDs) == 0 {
		return CapacityView{}, ErrInvalidCapacityWindow
	}
	rows, err := s.repository.LoadCapacity(ctx, target, window, resourceIDs)
	if err != nil {
		return CapacityView{}, err
	}
	view := CapacityView{Window: window, Resources: make(map[string]Capacity, len(rows))}
	for _, row := range rows {
		capacity := CalculateCapacity(row.Available, row.Scheduled, row.Actual)
		capacity.Name = row.Name
		view.Resources[row.ResourceID] = capacity
	}
	return view, nil
}

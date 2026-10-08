package calendar

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
)

var ErrInvalidCapacityInput = errors.New("invalid calendar capacity input")

type CapacityEvent struct {
	ID, ClientID   string
	Assigned       bool
	Mode           SchedulingMode
	PlannedMinutes int64
	Interval       TimeInterval
	Source         SafeSourceRef
}

type DependencyConstraint struct {
	ID       string
	Type     DependencyType
	Unmet    bool
	Interval TimeInterval
	Related  SafeSourceRef
}

type CapacityInput struct {
	Window                      QueryWindow
	Available                   []AvailabilitySegment
	TentativeUnavailableMinutes int64
	Events                      []CapacityEvent
	Dependencies                []DependencyConstraint
}

type AllocationSegment struct {
	EventID  string         `json:"event_id"`
	Mode     SchedulingMode `json:"mode"`
	Interval TimeInterval   `json:"interval"`
	Minutes  int64          `json:"minutes"`
	Related  SafeSourceRef  `json:"related,omitempty"`
}

type CapacitySummary struct {
	AvailableMinutes            int64               `json:"available_minutes"`
	FixedMinutes                int64               `json:"fixed_minutes"`
	AllocatedMinutes            int64               `json:"allocated_minutes"`
	CommittedMinutes            int64               `json:"committed_minutes"`
	RemainingMinutes            int64               `json:"remaining_minutes"`
	OverbookedMinutes           int64               `json:"overbooked_minutes"`
	TentativeUnavailableMinutes int64               `json:"tentative_unavailable_minutes"`
	UnscheduledMinutes          int64               `json:"unscheduled_minutes"`
	DependencyBlockedMinutes    int64               `json:"dependency_blocked_minutes"`
	Segments                    []AllocationSegment `json:"segments"`
}

func CalculateCapacity(input CapacityInput) CapacitySummary {
	window := TimeInterval{Start: input.Window.Start, End: input.Window.End}
	available := make([]AvailabilitySegment, 0, len(input.Available))
	for _, segment := range input.Available {
		segment.Interval = intersectInterval(segment.Interval, window)
		if segment.CapacityPercent == 0 {
			segment.CapacityPercent = 100
		}
		if segment.Interval.valid() && segment.CapacityPercent > 0 && segment.CapacityPercent <= 100 {
			available = append(available, segment)
		}
	}
	available = normalizeAvailability(available)
	summary := CapacitySummary{AvailableMinutes: availabilityMinutes(available), TentativeUnavailableMinutes: input.TentativeUnavailableMinutes}
	blocked := []TimeInterval{}
	for _, dependency := range input.Dependencies {
		if dependency.Unmet {
			if interval := intersectInterval(dependency.Interval, window); interval.valid() {
				blocked = append(blocked, interval)
			}
		}
	}
	summary.DependencyBlockedMinutes = intervalMinutes(mergeTimeIntervals(blocked))
	free := append([]AvailabilitySegment(nil), available...)

	for _, event := range input.Events {
		if event.Mode == Informational || event.PlannedMinutes <= 0 {
			continue
		}
		if !event.Assigned || !event.Interval.valid() {
			summary.UnscheduledMinutes += event.PlannedMinutes
			continue
		}
		if event.Mode != FixedBlock {
			continue
		}
		interval := intersectInterval(event.Interval, window)
		if !interval.valid() {
			summary.UnscheduledMinutes += event.PlannedMinutes
			continue
		}
		minutes := int64(interval.End.Sub(interval.Start) / time.Minute)
		summary.FixedMinutes += minutes
		summary.Segments = append(summary.Segments, AllocationSegment{EventID: event.ID, Mode: FixedBlock, Interval: interval, Minutes: minutes, Related: event.Source})
		free = subtractAvailability(free, interval)
	}

	sort.SliceStable(free, func(i, j int) bool { return free[i].Interval.Start.Before(free[j].Interval.Start) })
	for _, event := range input.Events {
		if event.Mode != EffortAllocation || event.PlannedMinutes <= 0 || !event.Assigned || !event.Interval.valid() {
			continue
		}
		rangeInterval := intersectInterval(event.Interval, window)
		remaining := event.PlannedMinutes
		if !rangeInterval.valid() {
			summary.UnscheduledMinutes += remaining
			continue
		}
		for remaining > 0 {
			index := -1
			candidate := TimeInterval{}
			for candidateIndex := range free {
				intersected := intersectInterval(free[candidateIndex].Interval, rangeInterval)
				if intersected.valid() {
					index, candidate = candidateIndex, intersected
					break
				}
			}
			if index < 0 {
				break
			}
			capacityMinutes := int64(candidate.End.Sub(candidate.Start)/time.Minute) * int64(free[index].CapacityPercent) / 100
			if capacityMinutes < 1 {
				free = subtractAvailability(free, candidate)
				continue
			}
			allocated := capacityMinutes
			if allocated > remaining {
				allocated = remaining
			}
			wallMinutes := allocated * 100 / int64(free[index].CapacityPercent)
			if allocated*100%int64(free[index].CapacityPercent) != 0 {
				wallMinutes++
			}
			end := candidate.Start.Add(time.Duration(wallMinutes) * time.Minute)
			if end.After(candidate.End) {
				end = candidate.End
			}
			summary.Segments = append(summary.Segments, AllocationSegment{EventID: event.ID, Mode: EffortAllocation, Interval: TimeInterval{Start: candidate.Start, End: end}, Minutes: allocated, Related: event.Source})
			summary.AllocatedMinutes += allocated
			remaining -= allocated
			free = subtractAvailability(free, TimeInterval{Start: candidate.Start, End: end})
		}
		summary.UnscheduledMinutes += remaining
	}
	summary.CommittedMinutes = summary.FixedMinutes + summary.AllocatedMinutes
	if summary.CommittedMinutes > summary.AvailableMinutes {
		summary.OverbookedMinutes = summary.CommittedMinutes - summary.AvailableMinutes
	} else {
		summary.RemainingMinutes = summary.AvailableMinutes - summary.CommittedMinutes
	}
	sort.SliceStable(summary.Segments, func(i, j int) bool {
		if summary.Segments[i].Interval.Start.Equal(summary.Segments[j].Interval.Start) {
			return summary.Segments[i].Mode == EffortAllocation
		}
		return summary.Segments[i].Interval.Start.Before(summary.Segments[j].Interval.Start)
	})
	return summary
}

type CapacityRepository interface {
	LoadCapacityInputs(context.Context, authorization.Principal, QueryWindow, []string) (map[string]CapacityInput, error)
}

type CapacityService struct{ repository CapacityRepository }

func NewCapacityService(repository CapacityRepository) *CapacityService {
	return &CapacityService{repository: repository}
}

// Calculate deliberately receives the authenticated principal rather than an
// active-client target. An MSP principal therefore aggregates work across all
// clients the repository authorizes for that principal.
func (s *CapacityService) Calculate(ctx context.Context, principal authorization.Principal, window QueryWindow, technicianIDs []string) (map[string]CapacitySummary, error) {
	if s == nil || s.repository == nil || strings.TrimSpace(principal.Scope.MSPID) == "" || !window.End.After(window.Start) || len(technicianIDs) == 0 {
		return nil, ErrInvalidCapacityInput
	}
	inputs, err := s.repository.LoadCapacityInputs(ctx, principal, window, technicianIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]CapacitySummary, len(inputs))
	for _, technicianID := range technicianIDs {
		input, ok := inputs[technicianID]
		if !ok {
			continue
		}
		if input.Window.Start.IsZero() {
			input.Window = window
		}
		result[technicianID] = CalculateCapacity(input)
	}
	return result, nil
}

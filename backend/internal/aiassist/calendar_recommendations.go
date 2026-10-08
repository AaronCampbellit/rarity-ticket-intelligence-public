package aiassist

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidCalendarRecommendation = errors.New("invalid calendar recommendation")

type CalendarRecommendationContext struct {
	MSPID                            string                             `json:"-"`
	ProjectionID                     string                             `json:"projection_id"`
	AuthorizedTechnicianIDs          []string                           `json:"authorized_technician_ids"`
	RequiredSkillIDs                 []string                           `json:"required_skill_ids"`
	AvailableIntervals               []calendar.TimeInterval            `json:"available_intervals"`
	AvailableIntervalsByTechnicianID map[string][]calendar.TimeInterval `json:"available_intervals_by_technician_id"`
	WorkloadMinutes                  map[string]int64                   `json:"workload_minutes"`
	Priority                         string                             `json:"priority"`
	RequiredTechnologyIDs            []string                           `json:"required_technology_ids"`
	DependencyConstraints            []string                           `json:"dependency_constraints"`
	Health                           calendar.HealthState               `json:"health"`
	Conflicts                        []calendar.Conflict                `json:"conflicts"`
}

type AllocationRequest struct {
	TechnicianID   string `json:"technician_id"`
	PlannedMinutes int64  `json:"planned_minutes"`
}

type CalendarProviderCandidate struct {
	TechnicianID string             `json:"technician_id"`
	StartsAt     *time.Time         `json:"starts_at"`
	EndsAt       *time.Time         `json:"ends_at"`
	Timezone     string             `json:"timezone"`
	Allocation   *AllocationRequest `json:"allocation,omitempty"`
	Explanation  string             `json:"explanation"`
	Tradeoffs    []string           `json:"tradeoffs"`
}

type CalendarPreviewRequest struct {
	ProjectionID string     `json:"projection_id"`
	StartsAt     *time.Time `json:"starts_at,omitempty"`
	EndsAt       *time.Time `json:"ends_at,omitempty"`
	Timezone     string     `json:"timezone,omitempty"`
}

// CalendarPreviewSummary exposes deterministic safety findings without
// returning the proposal ID/version pair accepted by the apply endpoint.
type CalendarPreviewSummary struct {
	Conflicts      []calendar.Conflict             `json:"conflicts"`
	BlockedSources []calendar.CascadeBlockedSource `json:"blocked_sources"`
	Capacity       []calendar.CapacityImpact       `json:"capacity"`
	Health         []calendar.HealthImpact         `json:"health"`
	Notifications  []calendar.NotificationImpact   `json:"notifications"`
	RequiresReason bool                            `json:"requires_reason"`
}

type RecommendationCandidate struct {
	TechnicianID   string                 `json:"technician_id"`
	StartsAt       *time.Time             `json:"starts_at,omitempty"`
	EndsAt         *time.Time             `json:"ends_at,omitempty"`
	Allocation     *AllocationRequest     `json:"allocation,omitempty"`
	Explanation    string                 `json:"explanation"`
	Tradeoffs      []string               `json:"tradeoffs"`
	PreviewRequest CalendarPreviewRequest `json:"preview_request"`
	Preview        CalendarPreviewSummary `json:"preview"`
}

type CalendarRecommendation struct {
	Candidates []RecommendationCandidate `json:"candidates"`
}

type RecommendCalendarCommand struct {
	Principal    authorization.Principal
	ProjectionID string
}

type CalendarRecommendationContextLoader interface {
	LoadCalendarRecommendationContext(context.Context, authorization.Principal, string) (CalendarRecommendationContext, error)
}

type CalendarRecommendationProvider interface {
	RecommendCalendar(context.Context, CalendarRecommendationContext) ([]CalendarProviderCandidate, error)
}

type CalendarProposalPreviewer interface {
	Preview(context.Context, calendar.PreviewCommand) (calendar.SchedulingProposal, error)
}

type CalendarRecommendationService struct {
	contexts CalendarRecommendationContextLoader
	provider CalendarRecommendationProvider
	previews CalendarProposalPreviewer
}

func NewCalendarRecommendationService(contexts CalendarRecommendationContextLoader, provider CalendarRecommendationProvider, previews CalendarProposalPreviewer) *CalendarRecommendationService {
	return &CalendarRecommendationService{contexts: contexts, provider: provider, previews: previews}
}

func (s *CalendarRecommendationService) Recommend(ctx context.Context, command RecommendCalendarCommand) (CalendarRecommendation, error) {
	result := CalendarRecommendation{Candidates: []RecommendationCandidate{}}
	if s == nil || s.contexts == nil || s.provider == nil || s.previews == nil || !internalid.ValidCanonical(command.ProjectionID) || strings.TrimSpace(command.Principal.ID) == "" || strings.TrimSpace(command.Principal.Scope.MSPID) == "" {
		return result, ErrInvalidCalendarRecommendation
	}
	if err := authorization.Authorize(command.Principal, "calendar.ai.recommend", scope.Target{MSPID: command.Principal.Scope.MSPID}); err != nil {
		return result, err
	}
	safeContext, err := s.contexts.LoadCalendarRecommendationContext(ctx, command.Principal, command.ProjectionID)
	if err != nil {
		return result, err
	}
	if safeContext.ProjectionID != command.ProjectionID || (safeContext.MSPID != "" && safeContext.MSPID != command.Principal.Scope.MSPID) {
		return result, ErrInvalidCalendarRecommendation
	}
	returned, err := s.provider.RecommendCalendar(ctx, safeContext)
	if err != nil {
		return result, err
	}
	authorized := make(map[string]struct{}, len(safeContext.AuthorizedTechnicianIDs))
	for _, id := range safeContext.AuthorizedTechnicianIDs {
		authorized[id] = struct{}{}
	}
	for _, candidate := range returned {
		if _, ok := authorized[candidate.TechnicianID]; !ok || candidate.Allocation != nil || candidate.StartsAt == nil || candidate.EndsAt == nil || !candidate.EndsAt.After(*candidate.StartsAt) {
			continue
		}
		if _, err := time.LoadLocation(candidate.Timezone); err != nil {
			continue
		}
		request := calendar.RequestedChange{ProjectionID: command.ProjectionID, StartsAt: candidate.StartsAt, EndsAt: candidate.EndsAt, Timezone: candidate.Timezone}
		preview, err := s.previews.Preview(ctx, calendar.PreviewCommand{Principal: command.Principal, PrimaryChange: request})
		if err != nil || !previewMatchesCalendarCandidate(preview, candidate) {
			continue
		}
		result.Candidates = append(result.Candidates, RecommendationCandidate{
			TechnicianID: candidate.TechnicianID, StartsAt: candidate.StartsAt, EndsAt: candidate.EndsAt,
			Allocation: candidate.Allocation, Explanation: strings.TrimSpace(candidate.Explanation), Tradeoffs: append([]string(nil), candidate.Tradeoffs...),
			PreviewRequest: CalendarPreviewRequest{ProjectionID: command.ProjectionID, StartsAt: candidate.StartsAt, EndsAt: candidate.EndsAt, Timezone: candidate.Timezone},
			Preview:        CalendarPreviewSummary{Conflicts: preview.Conflicts, BlockedSources: preview.BlockedSources, Capacity: preview.Capacity, Health: preview.Health, Notifications: preview.Notifications, RequiresReason: preview.RequiresReason},
		})
	}
	return result, nil
}

func previewMatchesCalendarCandidate(preview calendar.SchedulingProposal, candidate CalendarProviderCandidate) bool {
	for _, change := range preview.Changes {
		if !change.Required || change.Requested.ProjectionID == "" || change.Requested.ProjectionID != change.Schedule.ProjectionID ||
			change.Schedule.TechnicianID != candidate.TechnicianID || candidate.StartsAt == nil || candidate.EndsAt == nil {
			continue
		}
		if change.Schedule.Interval.Start.Equal(candidate.StartsAt.UTC()) && change.Schedule.Interval.End.Equal(candidate.EndsAt.UTC()) {
			return true
		}
	}
	return false
}

package aiassist

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

const calendarRecommendationProjectionID = "00000000-0000-4000-8000-000000000101"

type calendarContextStub struct{ context CalendarRecommendationContext }

func (s calendarContextStub) LoadCalendarRecommendationContext(context.Context, authorization.Principal, string) (CalendarRecommendationContext, error) {
	return s.context, nil
}

type recordingCalendarContextStub struct{ calls int }

func (s *recordingCalendarContextStub) LoadCalendarRecommendationContext(context.Context, authorization.Principal, string) (CalendarRecommendationContext, error) {
	s.calls++
	return CalendarRecommendationContext{}, nil
}

func TestCalendarRecommendationRejectsMalformedProjectionIDBeforeRepositoryLookup(t *testing.T) {
	contexts := &recordingCalendarContextStub{}
	service := NewCalendarRecommendationService(contexts, calendarProviderStub{}, &calendarPreviewStub{})
	_, err := service.Recommend(context.Background(), RecommendCalendarCommand{
		Principal: calendarRecommendationPrincipal(), ProjectionID: "not-a-canonical-uuid",
	})
	if !errors.Is(err, ErrInvalidCalendarRecommendation) || contexts.calls != 0 {
		t.Fatalf("error=%v context calls=%d", err, contexts.calls)
	}
}

type calendarProviderStub struct{ candidates []CalendarProviderCandidate }

func (s calendarProviderStub) RecommendCalendar(context.Context, CalendarRecommendationContext) ([]CalendarProviderCandidate, error) {
	return s.candidates, nil
}

type calendarPreviewStub struct {
	calls        int
	technicianID string
}

func (s *calendarPreviewStub) Preview(_ context.Context, command calendar.PreviewCommand) (calendar.SchedulingProposal, error) {
	s.calls++
	technicianID := s.technicianID
	if technicianID == "" {
		technicianID = "tech-1"
	}
	return calendar.SchedulingProposal{ID: "preview-1", Changes: []calendar.ProposedChange{{Required: true, Requested: command.PrimaryChange, Schedule: calendar.ProposedSchedule{ProjectionID: command.PrimaryChange.ProjectionID, TechnicianID: technicianID, Interval: calendar.TimeInterval{Start: command.PrimaryChange.StartsAt.UTC(), End: command.PrimaryChange.EndsAt.UTC()}}}}, Version: 1}, nil
}

func TestCalendarAIRejectsCandidateWhenDeterministicPreviewKeepsAnotherTechnician(t *testing.T) {
	starts := time.Date(2026, 8, 17, 14, 0, 0, 0, time.UTC)
	ends := starts.Add(time.Hour)
	preview := &calendarPreviewStub{technicianID: "tech-current"}
	service := NewCalendarRecommendationService(
		calendarContextStub{context: CalendarRecommendationContext{ProjectionID: calendarRecommendationProjectionID, AuthorizedTechnicianIDs: []string{"tech-recommended"}}},
		calendarProviderStub{candidates: []CalendarProviderCandidate{{TechnicianID: "tech-recommended", StartsAt: &starts, EndsAt: &ends, Timezone: "UTC", Explanation: "Available"}}},
		preview,
	)
	found, err := service.Recommend(context.Background(), RecommendCalendarCommand{Principal: calendarRecommendationPrincipal(), ProjectionID: calendarRecommendationProjectionID})
	if err != nil || len(found.Candidates) != 0 || preview.calls != 1 {
		t.Fatalf("recommendation=%+v preview calls=%d error=%v", found, preview.calls, err)
	}
}

func TestCalendarAIProducesPreviewedRecommendationButCannotApply(t *testing.T) {
	starts := time.Date(2026, 8, 17, 14, 0, 0, 0, time.UTC)
	ends := starts.Add(time.Hour)
	preview := &calendarPreviewStub{}
	service := NewCalendarRecommendationService(
		calendarContextStub{context: CalendarRecommendationContext{ProjectionID: calendarRecommendationProjectionID, AuthorizedTechnicianIDs: []string{"tech-1"}}},
		calendarProviderStub{candidates: []CalendarProviderCandidate{{TechnicianID: "tech-1", StartsAt: &starts, EndsAt: &ends, Timezone: "UTC", Explanation: "Balances workload"}}},
		preview,
	)
	found, err := service.Recommend(context.Background(), RecommendCalendarCommand{Principal: calendarRecommendationPrincipal(), ProjectionID: calendarRecommendationProjectionID})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Candidates) != 1 || found.Candidates[0].PreviewRequest.ProjectionID != calendarRecommendationProjectionID || preview.calls != 1 {
		t.Fatalf("recommendation=%+v preview calls=%d", found, preview.calls)
	}
	encoded, err := json.Marshal(found)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "preview-1") || strings.Contains(string(encoded), `"version"`) || strings.Contains(string(encoded), `"expires_at"`) {
		t.Fatalf("recommendation leaked applyable proposal token: %s", encoded)
	}
}

func TestCalendarAIRejectsUnauthorizedProviderCandidate(t *testing.T) {
	starts := time.Date(2026, 8, 17, 14, 0, 0, 0, time.UTC)
	ends := starts.Add(time.Hour)
	preview := &calendarPreviewStub{}
	service := NewCalendarRecommendationService(
		calendarContextStub{context: CalendarRecommendationContext{ProjectionID: calendarRecommendationProjectionID, AuthorizedTechnicianIDs: []string{"tech-1"}}},
		calendarProviderStub{candidates: []CalendarProviderCandidate{{TechnicianID: "tech-secret", StartsAt: &starts, EndsAt: &ends, Timezone: "UTC"}}},
		preview,
	)
	found, err := service.Recommend(context.Background(), RecommendCalendarCommand{Principal: calendarRecommendationPrincipal(), ProjectionID: calendarRecommendationProjectionID})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Candidates) != 0 || preview.calls != 0 {
		t.Fatalf("unsafe recommendation=%+v preview calls=%d", found, preview.calls)
	}
}

func TestCalendarAIRejectsUnsupportedAllocationCandidate(t *testing.T) {
	starts := time.Date(2026, 8, 17, 14, 0, 0, 0, time.UTC)
	ends := starts.Add(time.Hour)
	preview := &calendarPreviewStub{}
	service := NewCalendarRecommendationService(
		calendarContextStub{context: CalendarRecommendationContext{ProjectionID: calendarRecommendationProjectionID, AuthorizedTechnicianIDs: []string{"tech-1"}}},
		calendarProviderStub{candidates: []CalendarProviderCandidate{{TechnicianID: "tech-1", StartsAt: &starts, EndsAt: &ends, Timezone: "UTC", Allocation: &AllocationRequest{TechnicianID: "tech-1", PlannedMinutes: 30}}}},
		preview,
	)
	found, err := service.Recommend(context.Background(), RecommendCalendarCommand{Principal: calendarRecommendationPrincipal(), ProjectionID: calendarRecommendationProjectionID})
	if err != nil || len(found.Candidates) != 0 || preview.calls != 0 {
		t.Fatalf("unsupported allocation recommendation=%+v preview calls=%d error=%v", found, preview.calls, err)
	}
}

func calendarRecommendationPrincipal() authorization.Principal {
	return authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp"}, Capabilities: authorization.NewCapabilitySet("calendar.ai.recommend", "calendar.schedule")}
}

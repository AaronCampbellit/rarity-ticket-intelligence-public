package psa

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
)

func TestCalendarRecommendationWindowIsBoundedAndNeverStartsInPast(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	future := now.Add(72 * time.Hour)
	window := calendarRecommendationWindow(calendar.Projection{StartsAt: &future}, now)
	if !window.Start.Equal(future.Add(-24*time.Hour)) || window.End.Sub(window.Start) != 14*24*time.Hour {
		t.Fatalf("future window=%+v", window)
	}
	past := now.Add(-time.Hour)
	window = calendarRecommendationWindow(calendar.Projection{StartsAt: &past}, now)
	if !window.Start.Equal(now) || window.End.Sub(window.Start) != 14*24*time.Hour {
		t.Fatalf("past window=%+v", window)
	}
}

func TestCalendarAIContextRepositoryFailsClosedWithoutTrustedDependencies(t *testing.T) {
	_, err := NewCalendarAIContextRepository(nil, time.Now).LoadCalendarRecommendationContext(
		context.Background(), authorization.Principal{}, "projection",
	)
	if !errors.Is(err, aiassist.ErrInvalidCalendarRecommendation) {
		t.Fatalf("LoadCalendarRecommendationContext() error=%v", err)
	}
}

package calendar

import (
	"context"
	"errors"
	"fmt"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
)

// This opt-in measurement isolates service behavior over an in-memory repository.
// PostgreSQL and deployment capacity require separate retained measurements.
type performanceCalendarRepository struct {
	rows         []QueryProjection
	clients      []string
	byTechnician map[string][]QueryProjection
}

func (r *performanceCalendarRepository) AuthorizedCalendarClientIDs(context.Context, authorization.Principal) ([]string, error) {
	return r.clients, nil
}
func (r *performanceCalendarRepository) ListCalendarProjections(_ context.Context, _ authorization.Principal, clients []string, window QueryWindow, filter Filter, visibility QueryVisibility, limit int) ([]QueryProjection, error) {
	if visibility == QueryVisibilityBusy {
		return nil, nil
	}
	allowed := map[string]bool{}
	for _, id := range clients {
		allowed[id] = true
	}
	found := make([]QueryProjection, 0, limit)
	rows := r.rows
	if len(filter.TechnicianIDs) == 1 {
		rows = r.byTechnician[filter.TechnicianIDs[0]]
	}
	for _, row := range rows {
		p := row.Projection
		if allowed[p.Source.ClientID] && p.StartsAt.Before(window.End) && p.EndsAt.After(window.Start) && matchesCalendarFilter(row, filter) {
			found = append(found, row)
			if len(found) == limit {
				break
			}
		}
	}
	return found, nil
}
func TestCalendarInteractivePerformanceFixture(t *testing.T) {
	if os.Getenv("CALENDAR_PERFORMANCE") != "1" {
		t.Skip("set CALENDAR_PERFORMANCE=1 for the 100,000-projection measurement")
	}
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	repo := &performanceCalendarRepository{byTechnician: map[string][]QueryProjection{}}
	for i := 0; i < 1000; i++ {
		repo.clients = append(repo.clients, fmt.Sprintf("client-%04d", i))
	}
	for i := 0; i < 100000; i++ {
		row := queryProjection(fmt.Sprintf("event-%06d", i), repo.clients[i%1000], start.Add(time.Duration(i%90)*24*time.Hour))
		row.Projection.AssigneeID = fmt.Sprintf("tech-%02d", i%50)
		repo.rows = append(repo.rows, row)
		repo.byTechnician[row.Projection.AssigneeID] = append(repo.byTechnician[row.Projection.AssigneeID], row)
	}
	service := NewQueryService(repo, queryAuthorizerStub{}, nil)
	// Broad density fails explicitly instead of silently returning a partial view.
	_, err := service.List(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: start.AddDate(0, 0, 90)}, Limit: 250})
	if !errors.Is(err, ErrResultTooLarge) {
		t.Fatalf("broad density error=%v", err)
	}
	durations := make([]time.Duration, 50)
	failures := make([]error, 50)
	var wg sync.WaitGroup
	for i := range durations {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			began := time.Now()
			page, err := service.List(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: start.AddDate(0, 0, 90)}, Filter: Filter{TechnicianIDs: []string{fmt.Sprintf("tech-%02d", i)}}, Limit: 250})
			durations[i] = time.Since(began)
			if err == nil && (len(page.Events) != 250 || page.NextCursor == "") {
				err = fmt.Errorf("incomplete pagination contract: %d events", len(page.Events))
			}
			failures[i] = err
		}(i)
	}
	wg.Wait()
	for _, err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[47]
	t.Logf("100000 projections, 1000 clients, 50 concurrent technicians, 90 days: p95=%s", p95)
	if p95 > 500*time.Millisecond {
		t.Fatalf("interactive service fixture p95 %s exceeds 500ms", p95)
	}
	projections := map[string]Projection{}
	edges := []Dependency{}
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("node-%03d", i)
		projections[key] = dependencyProjection(key, "msp", "client", i)
		if i > 0 {
			edges = append(edges, Dependency{ID: fmt.Sprintf("edge-%03d", i), MSPID: "msp", ClientID: "client", PredecessorID: fmt.Sprintf("node-%03d", i-1), SuccessorID: key, Type: FinishToStart})
		}
	}
	moveStart, moveEnd := start.Add(24*time.Hour), start.Add(25*time.Hour)
	began := time.Now()
	_, err = BuildCascadeImpact("node-000", ScheduleInterval{StartsAt: &moveStart, EndsAt: &moveEnd}, projections, edges)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("100-node cascade: %s", time.Since(began))
}

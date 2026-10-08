package calendar

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
)

type liveRepositoryStub struct {
	clients []string
	page    LiveRepositoryPage
	got     []string
}

func (r *liveRepositoryStub) AuthorizedCalendarClientIDs(context.Context, authorization.Principal) ([]string, error) {
	return append([]string(nil), r.clients...), nil
}
func (r *liveRepositoryStub) ListCalendarChangesAfter(_ context.Context, _ string, clients []string, cursor uint64, _ int) (LiveRepositoryPage, error) {
	r.got = append([]string(nil), clients...)
	if cursor < r.page.OldestCursor && cursor != 0 {
		return LiveRepositoryPage{Expired: true, LatestCursor: r.page.LatestCursor}, nil
	}
	return r.page, nil
}

func TestCalendarLiveExpiredCursorRequiresRefetch(t *testing.T) {
	repo := &liveRepositoryStub{clients: []string{"client-a"}, page: LiveRepositoryPage{OldestCursor: 10, LatestCursor: 20}}
	service := NewLiveService(repo, queryAuthorizerStub{}, nil)
	found, err := service.ListAfter(context.Background(), LiveRequest{Principal: calendarPrincipal(), Cursor: encodeLiveCursor(5), Limit: 10})
	if err != nil || !found.RefetchRequired || len(found.Changes) != 0 || found.Cursor == "" {
		t.Fatalf("page=%+v err=%v", found, err)
	}
}

func TestCalendarLiveMissingCursorRequiresRefetchAndReturnsReconnectCursor(t *testing.T) {
	repo := &liveRepositoryStub{clients: []string{"client-a"}, page: LiveRepositoryPage{LatestCursor: 20}}
	found, err := NewLiveService(repo, queryAuthorizerStub{}, nil).ListAfter(context.Background(), LiveRequest{Principal: calendarPrincipal(), Limit: 10})
	if err != nil || !found.RefetchRequired || found.Cursor != encodeLiveCursor(20) || len(found.Changes) != 0 {
		t.Fatalf("page=%+v err=%v", found, err)
	}
}

func TestCalendarLiveFutureCursorRequiresRefetchAndResetsToLatest(t *testing.T) {
	repo := &liveRepositoryStub{clients: []string{"client-a"}, page: LiveRepositoryPage{LatestCursor: 20}}
	found, err := NewLiveService(repo, queryAuthorizerStub{}, nil).ListAfter(context.Background(), LiveRequest{Principal: calendarPrincipal(), Cursor: encodeLiveCursor(99), Limit: 10})
	if err != nil || !found.RefetchRequired || found.Cursor != encodeLiveCursor(20) || len(found.Changes) != 0 {
		t.Fatalf("page=%+v err=%v", found, err)
	}
}

func TestCalendarLiveRedactsUpsertAndReturnsSafeRemoveHint(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	row := queryProjection("secret", "client-secret", start)
	repo := &liveRepositoryStub{clients: []string{"client-secret"}, page: LiveRepositoryPage{LatestCursor: 12, Changes: []LiveRepositoryChange{{Cursor: 11, Type: LiveUpsert, Projection: &row}, {Cursor: 12, Type: LiveRemove, Source: row.Projection.Source, EventRole: row.Projection.EventRole}}}}
	auth := queryAuthorizerStub{hidden: map[string]bool{"source-secret": true}, workforce: map[string]bool{"tech": true}}
	found, err := NewLiveService(repo, auth, nil).ListAfter(context.Background(), LiveRequest{Principal: calendarPrincipal(), Cursor: encodeLiveCursor(10), Limit: 10})
	if err != nil || len(found.Changes) != 1 || found.Changes[0].Event.Privacy != PrivacyBusy || !found.RefetchRequired {
		t.Fatalf("page=%+v err=%v", found, err)
	}
}

func TestCalendarLiveRemoveRequiresCurrentSourceOrWorkforcePermission(t *testing.T) {
	change := LiveRepositoryChange{Cursor: 11, Type: LiveRemove, Source: SourceRef{MSPID: "msp", ClientID: "client-a", Type: "task", ID: "hidden"}, EventRole: "due"}
	repo := &liveRepositoryStub{clients: []string{"client-a"}, page: LiveRepositoryPage{LatestCursor: 11, Changes: []LiveRepositoryChange{change}}}
	found, err := NewLiveService(repo, queryAuthorizerStub{hidden: map[string]bool{"hidden": true}}, nil).ListAfter(context.Background(), LiveRequest{Principal: calendarPrincipal(), Cursor: encodeLiveCursor(10), Limit: 10})
	if err != nil || len(found.Changes) != 0 || !found.RefetchRequired || found.Cursor != encodeLiveCursor(11) {
		t.Fatalf("page=%+v err=%v", found, err)
	}
}

func TestCalendarLiveCursorAdvancesOnlyThroughReturnedRepositoryPage(t *testing.T) {
	repo := &liveRepositoryStub{clients: []string{"client-a"}, page: LiveRepositoryPage{LatestCursor: 20, Changes: []LiveRepositoryChange{{Cursor: 11, Type: LiveRemove, Source: SourceRef{MSPID: "msp", ClientID: "client-a", Type: "task", ID: "source"}, EventRole: "due"}}}}
	found, err := NewLiveService(repo, queryAuthorizerStub{}, nil).ListAfter(context.Background(), LiveRequest{Principal: calendarPrincipal(), Cursor: encodeLiveCursor(10), Limit: 1})
	if err != nil || found.Cursor != encodeLiveCursor(11) {
		t.Fatalf("page=%+v err=%v", found, err)
	}
}

func TestCalendarLiveRecurringUpsertRequiresRefetchInsteadOfExpandingAtSeriesOrigin(t *testing.T) {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	row := queryProjection("recurring", "client-a", start)
	row.Projection.Recurrence = &RecurrenceRule{Frequency: Daily, Interval: 1}
	repo := &liveRepositoryStub{clients: []string{"client-a"}, page: LiveRepositoryPage{LatestCursor: 11, Changes: []LiveRepositoryChange{{Cursor: 11, Type: LiveUpsert, Projection: &row}}}}
	found, err := NewLiveService(repo, queryAuthorizerStub{}, nil).ListAfter(context.Background(), LiveRequest{Principal: calendarPrincipal(), Cursor: encodeLiveCursor(10), Limit: 10})
	if err != nil || !found.RefetchRequired || len(found.Changes) != 0 || found.Cursor != encodeLiveCursor(11) {
		t.Fatalf("page=%+v err=%v", found, err)
	}
}

func TestCalendarLiveInaccessibleUpsertDoesNotExposeChangeVolume(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	row := queryProjection("private", "client-a", start)
	repo := &liveRepositoryStub{clients: []string{"client-a"}, page: LiveRepositoryPage{LatestCursor: 11, Changes: []LiveRepositoryChange{{Cursor: 11, Type: LiveUpsert, Projection: &row}}}}
	found, err := NewLiveService(repo, queryAuthorizerStub{hidden: map[string]bool{"source-private": true}}, nil).ListAfter(context.Background(), LiveRequest{Principal: calendarPrincipal(), Cursor: encodeLiveCursor(10), Limit: 10})
	if err != nil || len(found.Changes) != 0 || found.Cursor != encodeLiveCursor(11) || found.RefetchRequired {
		t.Fatalf("private live change leaked: page=%+v err=%v", found, err)
	}
}

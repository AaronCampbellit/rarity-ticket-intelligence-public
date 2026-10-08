package psa

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

func TestTaggingProjectionRepositoryUsesLockedOrderedCursor(t *testing.T) {
	tx := &fakeSalesTx{queryResult: &fakeRows{}}
	result, err := NewTaggingProjectionRepository(&fakeSalesDB{tx: tx}).ProjectTagEvents(context.Background(), 500)
	if err != nil {
		t.Fatalf("ProjectTagEvents() error = %v", err)
	}
	if result.Processed != 0 || !tx.committed {
		t.Fatalf("result=%+v committed=%t", result, tx.committed)
	}
	joined := strings.Join(tx.queries, "\n")
	for _, required := range []string{
		"pg_advisory_xact_lock",
		"tag_projection_cursors",
		"event.occurred_at > cursor.last_occurred_at",
		"event.occurred_at = cursor.last_occurred_at AND event.id > cursor.last_event_id",
		"ORDER BY event.occurred_at, event.id",
		"LIMIT $1",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("projection SQL missing %q:\n%s", required, joined)
		}
	}
}

func TestClassificationReportScopesUsageAndReturnsProjectionWatermark(t *testing.T) {
	asOf := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{
		queryRow: fakeRow{scan: func(destinations ...any) { *destinations[0].(*time.Time) = asOf }},
		queryRows: &fakeRows{scans: []func(...any){func(destinations ...any) {
			*destinations[0].(*string) = "tag"
			*destinations[1].(*tagging.ObjectType) = tagging.ObjectTask
			*destinations[2].(*string) = "2026-08-08"
			*destinations[3].(*int64) = 3
		}}},
	}
	report, err := NewTaggingProjectionRepository(db).ClassificationReport(context.Background(), "msp", tagging.ReportUsage, tagging.ReportFilter{
		ClientID: "client", From: asOf.Add(-24 * time.Hour), To: asOf, Limit: 50,
	})
	if err != nil || len(report.Rows) != 1 || report.ProjectionAsOf != asOf {
		t.Fatalf("ClassificationReport()=%+v error=%v", report, err)
	}
	if !strings.Contains(db.query, "msp_id=$1::uuid") || !strings.Contains(db.query, "client_id=$2::uuid") || !strings.Contains(db.query, "ORDER BY") {
		t.Fatalf("report query lacks stable scoped shape: %s", db.query)
	}
}

func TestReportCursorBindsTheCompleteQuery(t *testing.T) {
	filter := tagging.ReportFilter{ClientID: "client", From: time.Now().UTC().Add(-time.Hour), To: time.Now().UTC(), Status: "new", Limit: 50}
	hash := reportQueryHash("msp", tagging.ReportUsage, "", filter)
	filter.Status = "waiting"
	if hash == reportQueryHash("msp", tagging.ReportUsage, "", filter) {
		t.Fatal("edited filter reused query hash")
	}
	if _, err := decodeReportCursor("edited"); err == nil {
		t.Fatal("malformed cursor accepted")
	}
}

func TestTaggingProjectionRepositoryProjectsUsageAndInheritedTaskEffects(t *testing.T) {
	at := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	tx := &fakeSalesTx{queryRow: fakeRow{scan: func(destinations ...any) { *destinations[0].(*string) = `{}` }}, queryResult: &fakeRows{scans: []func(...any){func(destinations ...any) {
		*destinations[0].(*string) = "event"
		*destinations[1].(*string) = "msp"
		*destinations[2].(*string) = "client"
		*destinations[3].(*string) = "project"
		*destinations[4].(*string) = "project"
		*destinations[5].(*string) = "tag-b"
		*destinations[6].(*string) = "tag-b"
		*destinations[7].(*string) = "added"
		*destinations[8].(*string) = "human"
		*destinations[9].(*time.Time) = at
		*destinations[10].(*string) = `{}`
		*destinations[11].(*string) = `{}`
	}}}}
	// Other-tag and current-task lookups both return no rows in this unit test;
	// their SQL shape proves projection derives instead of copying assignments.
	result, err := NewTaggingProjectionRepository(&fakeSalesDB{tx: tx}).ProjectTagEvents(context.Background(), 10)
	if err != nil {
		t.Fatalf("ProjectTagEvents() error = %v", err)
	}
	if result.Processed != 1 {
		t.Fatalf("processed=%d, want 1", result.Processed)
	}
	joined := strings.Join(tx.queries, "\n")
	for _, required := range []string{"tag_usage_daily", "FROM tasks task", "assignment_source", "last_occurred_at"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("projection SQL missing %q:\n%s", required, joined)
		}
	}
}

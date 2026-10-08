package psa_test

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	psa "github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/views"
)

func TestCalendarQueryPrivacyLiveAndLensPostgreSQL(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	const (
		msp                 = "019fdb80-0000-7000-8000-000000000001"
		fullActor           = "019fdb80-0000-7000-8000-000000000002"
		busyActor           = "019fdb80-0000-7000-8000-000000000003"
		technician          = "019fdb80-0000-7000-8000-000000000004"
		clientA             = "019fdb80-0000-7000-8000-000000000011"
		clientB             = "019fdb80-0000-7000-8000-000000000012"
		clientC             = "019fdb80-0000-7000-8000-000000000013"
		fullRole            = "019fdb80-0000-7000-8000-000000000021"
		busyRole            = "019fdb80-0000-7000-8000-000000000022"
		projectionA         = "019fdb80-0000-7000-8000-000000000031"
		projectionB         = "019fdb80-0000-7000-8000-000000000032"
		projectionC         = "019fdb80-0000-7000-8000-000000000033"
		projectionRecurring = "019fdb80-0000-7000-8000-000000000034"
		sourceA             = "019fdb80-0000-7000-8000-000000000041"
		sourceB             = "019fdb80-0000-7000-8000-000000000042"
		sourceC             = "019fdb80-0000-7000-8000-000000000043"
		sourceRecurring     = "019fdb80-0000-7000-8000-000000000044"
		lensID              = "019fdb80-0000-7000-8000-000000000051"
	)
	cleanup := func() {
		for _, statement := range []string{
			`DELETE FROM saved_searches WHERE msp_id='` + msp + `'`,
			`DELETE FROM calendar_live_changes WHERE msp_id='` + msp + `'`,
			`DELETE FROM calendar_event_projections WHERE msp_id='` + msp + `'`,
			`DELETE FROM role_assignments WHERE msp_id='` + msp + `'`,
			`DELETE FROM role_capabilities WHERE msp_id='` + msp + `'`,
			`DELETE FROM mention_role_assignment_history WHERE msp_id='` + msp + `'`,
			`DELETE FROM mention_role_capability_history WHERE msp_id='` + msp + `'`,
			`DELETE FROM mention_access_revision_history WHERE msp_id='` + msp + `'`,
			`DELETE FROM mention_access_revisions WHERE msp_id='` + msp + `'`,
			`DELETE FROM roles WHERE msp_id='` + msp + `'`,
			`DELETE FROM client_organizations WHERE msp_id='` + msp + `'`,
			`DELETE FROM technicians WHERE msp_id='` + msp + `'`,
			`DELETE FROM msp_organizations WHERE id='` + msp + `'`} {
			_, _ = pool.Exec(context.Background(), statement)
		}
	}
	cleanup()
	t.Cleanup(cleanup)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, query, args...); e != nil {
			t.Fatalf("seed: %v\n%s", e, query)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,'CAL-Q','Calendar query',$2,$2)`, msp, fullActor)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name,lifecycle_state) VALUES($1,$4,'full@example.test','Full viewer','active'),($2,$4,'busy@example.test','Busy viewer','active'),($3,$4,'tech@example.test','Scheduled technician','active')`, fullActor, busyActor, technician, msp)
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$4,'A','Client A',$5,$5),($2,$4,'B','Client B',$5,$5),($3,$4,'C','Client C',$5,$5)`, clientA, clientB, clientC, msp, fullActor)
	exec(`INSERT INTO roles(id,msp_id,key,name) VALUES($1,$3,'calendar-full','Calendar full'),($2,$3,'calendar-busy','Calendar busy')`, fullRole, busyRole, msp)
	exec(`INSERT INTO role_capabilities(role_id,msp_id,capability) VALUES($1,$3,'calendar.read'),($1,$3,'project.read'),($1,$3,'view.save'),($1,$3,'view.share'),($2,$3,'calendar.read'),($2,$3,'calendar.workforce.manage')`, fullRole, busyRole, msp)
	exec(`INSERT INTO role_assignments(id,msp_id,client_id,technician_id,role_id,granted_by) VALUES
('019fdb80-0000-7000-8000-000000000061',$1,$2,$4,$5,$4),
('019fdb80-0000-7000-8000-000000000062',$1,$3,$4,$5,$4),
('019fdb80-0000-7000-8000-000000000063',$1,NULL,$6,$7,$4)`, msp, clientA, clientC, fullActor, fullRole, busyActor, busyRole)
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	exec(`INSERT INTO calendar_event_projections(id,msp_id,client_id,source_type,source_id,event_role,source_revision,title,starts_at,ends_at,timezone,all_day,scheduling_mode,capacity_bearing,assignee_id,planned_minutes,terminal_state,recurrence_rule) VALUES
($1,$5,$6,'project',$9,'planned_start',1,'A project',$13,$14,'UTC',false,'fixed_block',true,$8,60,'active',NULL),
($2,$5,$7,'project',$10,'planned_start',1,'B project',$13,$14,'UTC',false,'fixed_block',true,$8,60,'active',NULL),
($3,$5,$11,'project',$12,'planned_start',1,'C project',$13,$14,'UTC',false,'fixed_block',true,$8,60,'active',NULL),
($4,$5,$6,'project',$15,'planned_start',1,'Recurring A',$13,$14,'UTC',false,'fixed_block',true,$8,60,'active','{"frequency":"daily","interval":1,"count":2}'::jsonb)`, projectionA, projectionB, projectionC, projectionRecurring, msp, clientA, clientB, technician, sourceA, sourceB, clientC, sourceC, start, start.Add(time.Hour), sourceRecurring)
	repository := psa.NewCalendarRepositoryFromPool(pool)
	full := authorization.Principal{ID: fullActor, Scope: scope.Principal{MSPID: msp, ClientID: clientB}, Capabilities: authorization.NewCapabilitySet("calendar.read", "project.read", "view.save", "view.share")}
	for _, sourceType := range []string{"task", "custom_date"} {
		allowed, sourceErr := repository.CanReadCalendarSource(ctx, full, calendar.SourceRef{MSPID: msp, ClientID: clientA, Type: sourceType, ID: "019fdb80-0000-7000-8000-000000000099"})
		if sourceErr != nil || allowed {
			t.Fatalf("missing %s authorization allowed=%v err=%v", sourceType, allowed, sourceErr)
		}
	}
	query := calendar.NewQueryService(repository, repository, nil)
	page, err := query.List(ctx, calendar.QueryRequest{Principal: full, Window: calendar.QueryWindow{Start: start, End: start.Add(48 * time.Hour)}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	clients := map[string]bool{}
	for _, event := range page.Events {
		clients[event.Source.ClientID] = true
		if event.Privacy != calendar.PrivacyFull || event.ProjectionID == "" || event.SourceRevision != 1 {
			t.Fatalf("full viewer got %+v", event)
		}
	}
	if len(page.Events) != 4 || !clients[clientA] || !clients[clientC] || clients[clientB] {
		t.Fatalf("events=%+v", page.Events)
	}
	emptyRows, err := repository.ListCalendarProjections(ctx, full, []string{clientA, clientC}, calendar.QueryWindow{Start: start, End: start.Add(48 * time.Hour)}, calendar.Filter{ClientIDs: []string{}}, calendar.QueryVisibilityFull, 100)
	if err != nil || len(emptyRows) != 0 {
		t.Fatalf("explicit-empty SQL client filter rows=%d err=%v", len(emptyRows), err)
	}
	busy := authorization.Principal{ID: busyActor, Scope: scope.Principal{MSPID: msp}, Capabilities: authorization.NewCapabilitySet("calendar.read", "calendar.workforce.manage")}
	busyPage, err := query.List(ctx, calendar.QueryRequest{Principal: busy, Window: calendar.QueryWindow{Start: start, End: start.Add(2 * time.Hour)}, Filter: calendar.Filter{ClientIDs: []string{clientA}}, Limit: 100})
	// Client is redacted from Busy events, so a Client filter must not become a
	// hidden-source membership oracle or remove workforce-visible busy blocks.
	if err != nil || len(busyPage.Events) != 4 {
		t.Fatalf("busy=%+v err=%v", busyPage, err)
	}
	for _, event := range busyPage.Events {
		if event.Privacy != calendar.PrivacyBusy || event.Title != "Busy" || event.Source.ID != "" || event.ProjectionID != "" || event.OccurrenceKey != "" || event.SourceRevision != 0 || event.Recurrence != nil || len(event.HealthReasons) != 0 || len(event.Tags) != 0 {
			t.Fatalf("unsafe=%+v", event)
		}
	}
	exec(`INSERT INTO calendar_live_changes(msp_id,client_id,client_scope_key,projection_id,source_type,source_id,event_role,change_type,source_revision) VALUES($1,$2,'client:'||$2::uuid::text,$3,'project',$4,'planned_start','upserted',1)`, msp, clientA, projectionA, sourceA)
	var latest uint64
	if err = pool.QueryRow(ctx, `SELECT max(cursor) FROM calendar_live_changes WHERE msp_id=$1`, msp).Scan(&latest); err != nil {
		t.Fatal(err)
	}
	live := calendar.NewLiveService(repository, repository, nil)
	livePage, err := live.ListAfter(ctx, calendar.LiveRequest{Principal: full, Cursor: calendarTestLiveCursor(latest - 1), Limit: 10})
	if err != nil || livePage.RefetchRequired || len(livePage.Changes) != 1 || livePage.Changes[0].Event == nil {
		t.Fatalf("live=%+v err=%v", livePage, err)
	}
	viewService := views.NewService(psa.NewViewRepositoryFromPool(pool), func() string { return lensID })
	saved, err := viewService.Save(ctx, views.SaveCommand{Principal: full, OwnerID: fullActor, Kind: views.KindCalendarLens, Name: "Cross-client", Query: map[string]any{"lens": "week", "client_ids": []string{clientA, clientB, clientC}}, Audience: views.Audience{Type: views.MSP}})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := viewService.Resolve(ctx, views.ResolveCommand{Principal: full, ViewID: saved.ID})
	if err != nil {
		t.Fatal(err)
	}
	resolvedClients := resolved.Query["client_ids"].([]string)
	if len(resolvedClients) != 2 || resolvedClients[0] != clientA || resolvedClients[1] != clientC {
		t.Fatalf("lens=%#v", resolved.Query)
	}
}

func calendarTestLiveCursor(value uint64) string {
	raw := make([]byte, 8)
	binary.BigEndian.PutUint64(raw, value)
	return base64.RawURLEncoding.EncodeToString(raw)
}

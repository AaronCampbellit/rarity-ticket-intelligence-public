package psa

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

func TestWorkRecordReferenceLookupsAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for real ticket lookup rows")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	withAISecondWaveFixture(t, ctx, pool, func(f aiSecondWaveFixture) {
		_, err := pool.Exec(ctx, `UPDATE work_records SET
scheduled_starts_at='2026-09-04T14:00:00Z', scheduled_ends_at='2026-09-04T15:00:00Z',
schedule_timezone='America/Chicago', scheduling_mode='fixed_block', planned_effort_minutes=60,
due_on='2026-09-05', follow_up_on='2026-09-06' WHERE id=$1`, f.workRecordID)
		if err != nil {
			t.Fatal(err)
		}
		repo := NewWorkRecordRepositoryFromPool(pool)
		target := scope.Target{MSPID: f.mspID, ClientID: f.clientID}
		want, err := repo.Find(ctx, target, f.workRecordID)
		if err != nil {
			t.Fatal(err)
		}
		for name, resolve := range map[string]func(context.Context, scope.Target, string, int) ([]workrecords.Record, error){
			"queue":      repo.ResolveWorkRecordForQueue,
			"assignment": repo.ResolveWorkRecordForAssignment,
		} {
			t.Run(name, func(t *testing.T) {
				found, err := resolve(ctx, target, want.DisplayID, 2)
				if err != nil {
					t.Fatal(err)
				}
				if len(found) != 1 || !reflect.DeepEqual(found[0], want) {
					t.Fatalf("lookup=%+v, want full record %+v", found, want)
				}
				found, err = resolve(ctx, scope.Target{MSPID: f.mspID, ClientID: f.actorID}, want.DisplayID, 2)
				if err != nil || len(found) != 0 {
					t.Fatalf("cross-client lookup=%+v, err=%v", found, err)
				}
			})
		}
	})
}

func TestWorkRecordPaginationAndFiltersAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for worklist pagination")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	withAISecondWaveFixture(t, ctx, pool, func(f aiSecondWaveFixture) {
		_, err := pool.Exec(ctx, `INSERT INTO work_records
(id,msp_id,client_id,display_id,record_type,title,description,status,priority,created_by,updated_by,updated_at)
SELECT gen_random_uuid(),$1,$2,'PAGE-'||n,'incident','Page ticket '||n,'synthetic pagination fixture','new','normal',$3,$3,'2026-09-04T12:00:00Z'::timestamptz
FROM generate_series(1,105) n`, f.mspID, f.clientID, f.actorID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `UPDATE work_records SET updated_at='2020-01-01',title='Older 100% unresolved ticket',status='in_progress',priority='high',primary_owner_id=$2 WHERE id=$1`, f.workRecordID, f.technicianOneID)
		if err != nil {
			t.Fatal(err)
		}
		repo := NewWorkRecordRepositoryFromPool(pool)
		filter := workrecords.ListFilter{Target: scope.Target{MSPID: f.mspID, ClientID: f.clientID}, Limit: 100}
		first, err := repo.List(ctx, filter)
		if err != nil || len(first) != 100 {
			t.Fatalf("first page count=%d err=%v", len(first), err)
		}
		last := first[len(first)-1]
		filter.BeforeUpdatedAt, filter.BeforeID = last.UpdatedAt, last.ID
		second, err := repo.List(ctx, filter)
		if err != nil || len(second) != 6 {
			t.Fatalf("second page count=%d err=%v", len(second), err)
		}
		seen := make(map[string]bool)
		for _, record := range append(first, second...) {
			if seen[record.ID] {
				t.Fatal("duplicate cursor result")
			}
			seen[record.ID] = true
		}
		if !seen[f.workRecordID] {
			t.Fatal("old unresolved work missing")
		}
		filter.BeforeUpdatedAt, filter.BeforeID = time.Time{}, ""
		filter.Text, filter.Status, filter.Priority, filter.Ownership = "100% unresolved", "in_progress", "high", "assigned"
		found, err := repo.List(ctx, filter)
		if err != nil || len(found) != 1 || found[0].ID != f.workRecordID {
			t.Fatalf("older filtered match=%+v err=%v", found, err)
		}
		filter.Ownership = "unassigned"
		found, err = repo.List(ctx, filter)
		if err != nil || len(found) != 0 {
			t.Fatalf("ownership filter=%+v err=%v", found, err)
		}
		filter.Ownership = "assigned"
		filter.Target.ClientID = f.actorID
		found, err = repo.List(ctx, filter)
		if err != nil || len(found) != 0 {
			t.Fatalf("cross-client filter=%+v err=%v", found, err)
		}
	})
}

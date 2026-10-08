package psa

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
)

func TestMentionInvalidationHistoricalIntakePagesNonMatchingEventsAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for mention invalidation intake verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	mspID, actorID := id.New(), id.New()
	if _, err = pool.Exec(ctx, `INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'mention intake',$3,$3)`, mspID, "MENTION-INTAKE-"+mspID, actorID); err != nil {
		t.Fatalf("insert MSP fixture: %v", err)
	}
	var baseline int64
	if err = pool.QueryRow(ctx, `SELECT COALESCE(max(mention_invalidation_sequence),0) FROM event_outbox`).Scan(&baseline); err != nil {
		t.Fatalf("load outbox baseline: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE mention_invalidation_event_cursor SET last_sequence=$1,updated_at=now() WHERE singleton`, baseline); err != nil {
		t.Fatalf("set intake baseline: %v", err)
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE event_outbox DISABLE TRIGGER event_outbox_queue_mention_invalidation`); err != nil {
		t.Fatalf("disable transactional intake trigger: %v", err)
	}
	triggerDisabled := true
	t.Cleanup(func() {
		if triggerDisabled {
			_, _ = pool.Exec(context.Background(), `ALTER TABLE event_outbox ENABLE TRIGGER event_outbox_queue_mention_invalidation`)
		}
	})

	activeEventIDs := []string{id.New(), id.New(), id.New()}
	for _, eventID := range activeEventIDs {
		if _, err = pool.Exec(ctx, `INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source,data) VALUES($1,'internal_collaboration_source.saved',1,now(),$2,'technician',$3,'internal_collaboration_source',$4,1,$1,'integration','{"lifecycle_state":"active"}'::jsonb)`, eventID, mspID, actorID, id.New()); err != nil {
			t.Fatalf("insert active saved event: %v", err)
		}
	}
	relevantEventID := id.New()
	if _, err = pool.Exec(ctx, `INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source) VALUES($1,'technician.disabled',1,now(),$2,'technician',$3,'technician',$4,1,$1,'integration')`, relevantEventID, mspID, actorID, id.New()); err != nil {
		t.Fatalf("insert relevant event: %v", err)
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE event_outbox ENABLE TRIGGER event_outbox_queue_mention_invalidation`); err != nil {
		t.Fatalf("enable transactional intake trigger: %v", err)
	}
	triggerDisabled = false

	var thirdSequence, relevantSequence int64
	if err = pool.QueryRow(ctx, `SELECT mention_invalidation_sequence FROM event_outbox WHERE event_id=$1`, activeEventIDs[2]).Scan(&thirdSequence); err != nil {
		t.Fatalf("load third active sequence: %v", err)
	}
	if err = pool.QueryRow(ctx, `SELECT mention_invalidation_sequence FROM event_outbox WHERE event_id=$1`, relevantEventID).Scan(&relevantSequence); err != nil {
		t.Fatalf("load relevant sequence: %v", err)
	}
	repository := NewMentionRepositoryFromPool(pool)
	if _, err = repository.ClaimAccessInvalidations(ctx, 3, time.Now().UTC(), time.Minute); err != nil {
		t.Fatalf("claim first raw page: %v", err)
	}
	var cursor int64
	if err = pool.QueryRow(ctx, `SELECT last_sequence FROM mention_invalidation_event_cursor WHERE singleton`).Scan(&cursor); err != nil {
		t.Fatalf("load first cursor: %v", err)
	}
	if cursor != thirdSequence {
		t.Fatalf("first raw page cursor=%d want third nonmatching sequence %d", cursor, thirdSequence)
	}
	var relevantClaims int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM mention_invalidation_event_claims WHERE event_id=$1`, relevantEventID).Scan(&relevantClaims); err != nil || relevantClaims != 0 {
		t.Fatalf("relevant event queued before its raw page: claims=%d err=%v", relevantClaims, err)
	}

	if _, err = repository.ClaimAccessInvalidations(ctx, 3, time.Now().UTC(), time.Minute); err != nil {
		t.Fatalf("claim second raw page: %v", err)
	}
	if err = pool.QueryRow(ctx, `SELECT last_sequence FROM mention_invalidation_event_cursor WHERE singleton`).Scan(&cursor); err != nil {
		t.Fatalf("load second cursor: %v", err)
	}
	if cursor != relevantSequence {
		t.Fatalf("second raw page cursor=%d want relevant sequence %d", cursor, relevantSequence)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM mention_invalidation_event_claims WHERE event_id=$1`, relevantEventID).Scan(&relevantClaims); err != nil || relevantClaims != 1 {
		t.Fatalf("relevant event was skipped after nonmatching page: claims=%d err=%v", relevantClaims, err)
	}
}

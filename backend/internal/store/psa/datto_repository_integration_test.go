package psa

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/datto"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
)

func TestDattoNewAssetFallbackAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for Datto fallback verification")
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
	msp, client, actor, connection, group, canonical, meaningful := id.New(), id.New(), id.New(), id.New(), id.New(), id.New(), id.New()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO msp_organizations (id,display_id,name,created_by,updated_by) VALUES ($1,$2,'Datto live',$3,$3)`, msp, "DATTO-"+msp, actor)
	exec(`INSERT INTO client_organizations (id,msp_id,display_id,name,created_by,updated_by) VALUES ($1,$2,$3,'Datto client',$4,$4)`, client, msp, "CLIENT-"+client, actor)
	exec(`INSERT INTO tag_groups (id,msp_id,internal_key,label,position,created_by,updated_by) VALUES ($1,$2,'datto-live','Datto',1,$3,$3)`, group, msp, actor)
	exec(`INSERT INTO tags (id,msp_id,group_id,internal_key,label,system_tag,created_by,updated_by) VALUES ($1,$2,$3,'taxonomy.system.unclassified','Unclassified',true,$4,$4)`, canonical, msp, group, actor)
	exec(`INSERT INTO tags (id,msp_id,group_id,internal_key,label,created_by,updated_by) VALUES ($1,$2,$3,'datto.live.meaningful','Server',$4,$4)`, meaningful, msp, group, actor)
	exec(`INSERT INTO datto_connections (id,msp_id,name,credential_secret_ref,created_by,updated_by) VALUES ($1,$2,'Datto','env://RARITY_DATTO_CREDENTIAL_TEST',$3,$3)`, connection, msp, actor)
	exec(`INSERT INTO datto_site_mappings (id,connection_id,msp_id,datto_site_id,client_id,mapped_at,mapped_by) VALUES ($1,$2,$3,'site-1',$4,now(),$5)`, id.New(), connection, msp, client, actor)
	repo := NewDattoRepositoryFromPool(pool, graphTestProvider(t), id.New)
	page := datto.SyncPage{Assets: []datto.RemoteAsset{{ExternalID: "asset-1", SiteID: "site-1", Hostname: "host-1", SourcePayloadRef: "datto/live", SourceUpdatedAt: time.Now().UTC()}}}
	if err := repo.Apply(ctx, connection, page, false); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	var assetID string
	var assignments, events int
	if err := pool.QueryRow(ctx, `SELECT object_id::text FROM object_tag_assignments WHERE msp_id=$1 AND object_type='asset' AND tag_id=$2`, msp, canonical).Scan(&assetID); err != nil {
		t.Fatalf("canonical fallback: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM object_tag_assignments WHERE object_id=$1`, assetID).Scan(&assignments); err != nil || assignments != 1 {
		t.Fatalf("assignment count=%d err=%v", assignments, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tag_assignment_events WHERE object_id=$1 AND assignment_source='system_fallback'`, assetID).Scan(&events); err != nil || events != 1 {
		t.Fatalf("fallback events=%d err=%v", events, err)
	}
	// Simulate a technician replacing fallback with a meaningful tag. Resync must
	// not recreate the fallback, because the asset insert did not create a row.
	exec(`DELETE FROM object_tag_assignments WHERE object_id=$1`, assetID)
	exec(`INSERT INTO object_tag_assignments (id,msp_id,client_id,object_type,object_id,object_version,tag_id,assignment_source,assigned_by,evidence,version) VALUES ($1,$2,$3,'asset',$4,1,$5,'human',$6,'{}',1)`, id.New(), msp, client, assetID, meaningful, actor)
	if err := repo.Apply(ctx, connection, page, false); err != nil {
		t.Fatalf("resync Apply: %v", err)
	}
	var fallbackAfterResync int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM object_tag_assignments WHERE object_id=$1 AND tag_id=$2`, assetID, canonical).Scan(&fallbackAfterResync); err != nil || fallbackAfterResync != 0 {
		t.Fatalf("resync fallback count=%d err=%v", fallbackAfterResync, err)
	}
}

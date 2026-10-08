package psa

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

func TestPostgresClientResourceContactQueryAcceptance(t *testing.T) {
	ctx, pool := openClientResourcePostgresTest(t)
	fixture := seedClientResourcePostgresScope(t, ctx, pool)
	resourceID := uuid.NewString()
	if err := NewContactRepositoryFromPool(pool).CreateAtomic(
		ctx,
		postgresResourceCreateMutation(
			fixture, clientresources.ContactKind, resourceID, "",
		),
	); err != nil {
		t.Fatalf("create Contact fixture: %v", err)
	}

	items, err := NewClientResourceCatalogRepositoryFromPool(pool).QueryResources(
		ctx,
		scope.Target{MSPID: fixture.mspID, ClientID: fixture.clientID},
		clientresources.Query{
			Kind:    clientresources.ContactKind,
			Literal: "Task 11 Contact",
			Limit:   25,
		},
	)
	if err != nil {
		t.Fatalf("query Contact resources: %v", err)
	}
	if len(items) != 1 || items[0].ID != resourceID ||
		items[0].Name != "Task 11 Contact" {
		t.Fatalf("Contact query items=%+v, want exact fixture", items)
	}
}

func TestPostgresClientResourceConcurrencyAcceptance(t *testing.T) {
	ctx, pool := openClientResourcePostgresTest(t)

	t.Run("resource create serializes after Client deactivation", func(t *testing.T) {
		fixture := seedClientResourcePostgresScope(t, ctx, pool)
		resourceID := uuid.NewString()
		accepted := postgresResourceCreateMutation(
			fixture, clientresources.LocationKind, resourceID, "",
		)

		deactivation, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin Client deactivation: %v", err)
		}
		released := false
		defer func() {
			if !released {
				_ = deactivation.Rollback(context.Background())
			}
		}()
		var blockerPID int32
		if err := deactivation.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
			t.Fatalf("read Client deactivation backend PID: %v", err)
		}
		tag, err := deactivation.Exec(ctx, `
UPDATE client_organizations
SET lifecycle_state = 'inactive', version = version + 1,
    updated_at = $3, updated_by = $4
WHERE msp_id = $1 AND id = $2 AND lifecycle_state = 'active'
`, fixture.mspID, fixture.clientID, fixture.now, fixture.actorID)
		if err != nil {
			t.Fatalf("stage Client deactivation: %v", err)
		}
		if tag.RowsAffected() != 1 {
			t.Fatalf("staged Client deactivation rows=%d, want 1", tag.RowsAffected())
		}

		writerCtx, cancelWriter := context.WithTimeout(ctx, 15*time.Second)
		defer cancelWriter()
		result := make(chan error, 1)
		go func() {
			result <- NewLocationRepositoryFromPool(pool).CreateAtomic(
				writerCtx, accepted,
			)
		}()

		waitCtx, cancelWait := context.WithTimeout(ctx, 10*time.Second)
		waitErr := waitForPostgresBlocker(waitCtx, pool, blockerPID, 1)
		cancelWait()
		if waitErr != nil {
			_ = deactivation.Rollback(context.Background())
			released = true
			cancelWriter()
			<-result
			t.Fatalf("observe resource create waiting on Client lifecycle row: %v", waitErr)
		}
		t.Log("observed resource creation waiting on the uncommitted Client deactivation")
		if err := deactivation.Commit(ctx); err != nil {
			released = true
			cancelWriter()
			<-result
			t.Fatalf("commit Client deactivation: %v", err)
		}
		released = true

		if err := <-result; !errors.Is(err, scope.ErrNotFound) {
			t.Fatalf("resource create error=%v, want inactive Client not found", err)
		}
		assertNoPostgresMutationFacts(
			t, ctx, pool, "locations", resourceID,
			accepted.Audit.ID, accepted.Event.EventID,
		)
	})

	t.Run("knowledge draft serializes after Client deactivation", func(t *testing.T) {
		fixture := seedClientResourcePostgresScope(t, ctx, pool)
		articleID := uuid.NewString()
		accepted := postgresKnowledgeDraftMutation(fixture, articleID)
		deactivation, blockerPID := beginPostgresClientDeactivation(
			t, ctx, pool, fixture,
		)
		released := false
		defer func() {
			if !released {
				_ = deactivation.Rollback(context.Background())
			}
		}()

		writerCtx, cancelWriter := context.WithTimeout(ctx, 15*time.Second)
		defer cancelWriter()
		result := make(chan error, 1)
		go func() {
			result <- NewKnowledgeRepositoryFromPool(pool).CreateDraftAtomic(
				writerCtx, accepted,
			)
		}()
		waitCtx, cancelWait := context.WithTimeout(ctx, 10*time.Second)
		waitErr := waitForPostgresBlocker(waitCtx, pool, blockerPID, 1)
		cancelWait()
		if waitErr != nil {
			t.Fatalf("observe knowledge draft waiting on Client lifecycle row: %v", waitErr)
		}
		if err := deactivation.Commit(ctx); err != nil {
			t.Fatalf("commit Client deactivation: %v", err)
		}
		released = true
		if err := <-result; !errors.Is(err, scope.ErrNotFound) {
			t.Fatalf("knowledge draft error=%v, want inactive Client not found", err)
		}
		assertNoPostgresMutationFacts(
			t, ctx, pool, "knowledge_articles", articleID,
			accepted.Audit.ID, accepted.Event.EventID,
		)
	})

	t.Run("ticket route serializes after Client deactivation", func(t *testing.T) {
		fixture := seedClientResourcePostgresScope(t, ctx, pool)
		recordID, queueID := seedPostgresRoutableTicket(t, ctx, pool, fixture)
		accepted := postgresQueueMutation(fixture, recordID, queueID)
		deactivation, blockerPID := beginPostgresClientDeactivation(
			t, ctx, pool, fixture,
		)
		released := false
		defer func() {
			if !released {
				_ = deactivation.Rollback(context.Background())
			}
		}()

		writerCtx, cancelWriter := context.WithTimeout(ctx, 15*time.Second)
		defer cancelWriter()
		result := make(chan error, 1)
		go func() {
			result <- NewWorkRecordRepositoryFromPool(pool).TransferQueueAtomic(
				writerCtx, accepted,
			)
		}()
		waitCtx, cancelWait := context.WithTimeout(ctx, 10*time.Second)
		waitErr := waitForPostgresBlocker(waitCtx, pool, blockerPID, 1)
		cancelWait()
		if waitErr != nil {
			t.Fatalf("observe ticket route waiting on Client lifecycle row: %v", waitErr)
		}
		if err := deactivation.Commit(ctx); err != nil {
			t.Fatalf("commit Client deactivation: %v", err)
		}
		released = true
		if err := <-result; !errors.Is(err, scope.ErrNotFound) {
			t.Fatalf("ticket route error=%v, want inactive Client not found", err)
		}
		assertPostgresFactCount(
			t, ctx, pool, "audit_ledger", recordID,
			"work_record.queue.changed", 0,
		)
		assertPostgresFactCount(
			t, ctx, pool, "event_outbox", recordID,
			"work_record.queue.changed", 0,
		)
	})

	t.Run("two same-version writers produce one success and one conflict", func(t *testing.T) {
		fixture := seedClientResourcePostgresScope(t, ctx, pool)
		resourceID := uuid.NewString()
		locationRepository := NewLocationRepositoryFromPool(pool)
		if err := locationRepository.CreateAtomic(
			ctx,
			postgresResourceCreateMutation(
				fixture, clientresources.LocationKind, resourceID, "",
			),
		); err != nil {
			t.Fatalf("seed Location: %v", err)
		}

		names := []string{"North Office", "South Office"}
		type writerResult struct {
			name string
			err  error
		}
		results := make(chan writerResult, len(names))
		start := make(chan struct{})
		writerCtx, cancelWriters := context.WithTimeout(ctx, 15*time.Second)
		defer cancelWriters()
		for _, name := range names {
			name := name
			go func() {
				<-start
				_, err := locationRepository.UpdateLocation(
					writerCtx,
					postgresResourceUpdateMutation(
						fixture, clientresources.LocationKind, resourceID,
						clientresources.UpdatePatch{Name: &name},
					),
				)
				results <- writerResult{
					name: name,
					err:  err,
				}
			}()
		}
		close(start)

		succeeded, conflicted := "", ""
		for range names {
			outcome := <-results
			switch {
			case outcome.err == nil:
				succeeded = outcome.name
			case errors.Is(outcome.err, object.ErrVersionConflict):
				conflicted = outcome.name
			default:
				t.Fatalf("%s writer error=%v", outcome.name, outcome.err)
			}
		}
		if succeeded == "" || conflicted == "" || succeeded == conflicted {
			t.Fatalf(
				"same-version results succeeded=%q conflicted=%q",
				succeeded, conflicted,
			)
		}
		var storedName string
		var storedVersion int64
		if err := pool.QueryRow(ctx, `
SELECT name, version
FROM locations
WHERE msp_id = $1 AND client_id = $2 AND id = $3
`, fixture.mspID, fixture.clientID, resourceID).Scan(
			&storedName, &storedVersion,
		); err != nil {
			t.Fatalf("load winning Location update: %v", err)
		}
		if storedName != succeeded || storedVersion != 2 {
			t.Fatalf(
				"winning Location name=%q version=%d, want %q version 2",
				storedName, storedVersion, succeeded,
			)
		}
		assertPostgresFactCount(
			t, ctx, pool, "audit_ledger", resourceID, "location.updated", 1,
		)
		assertPostgresFactCount(
			t, ctx, pool, "event_outbox", resourceID, "location.updated", 1,
		)
	})

	for _, dependent := range []clientresources.Kind{
		clientresources.ContactKind,
		clientresources.AssetKind,
	} {
		dependent := dependent
		t.Run("Location deactivation serializes with dependent "+string(dependent)+" mutation", func(t *testing.T) {
			fixture := seedClientResourcePostgresScope(t, ctx, pool)
			locationID := uuid.NewString()
			dependentID := uuid.NewString()
			locationRepository := NewLocationRepositoryFromPool(pool)
			if err := locationRepository.CreateAtomic(
				ctx,
				postgresResourceCreateMutation(
					fixture, clientresources.LocationKind, locationID, "",
				),
			); err != nil {
				t.Fatalf("seed Location: %v", err)
			}
			if err := postgresDependentRepository(pool, dependent).CreateAtomic(
				ctx,
				postgresResourceCreateMutation(
					fixture, dependent, dependentID, locationID,
				),
			); err != nil {
				t.Fatalf("seed dependent %s: %v", dependent, err)
			}

			blocker, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin dependent %s blocker: %v", dependent, err)
			}
			blockerReleased := false
			defer func() {
				if !blockerReleased {
					_ = blocker.Rollback(context.Background())
				}
			}()
			var blockerPID int32
			if err := blocker.QueryRow(
				ctx, "SELECT pg_backend_pid()",
			).Scan(&blockerPID); err != nil {
				t.Fatalf("read dependent %s blocker PID: %v", dependent, err)
			}
			var lockedDependentID string
			if err := blocker.QueryRow(
				ctx,
				"SELECT id::text FROM "+postgresResourceTable(dependent)+
					" WHERE msp_id = $1 AND client_id = $2 AND id = $3 FOR UPDATE",
				fixture.mspID, fixture.clientID, dependentID,
			).Scan(&lockedDependentID); err != nil {
				t.Fatalf("lock dependent %s row: %v", dependent, err)
			}
			if lockedDependentID != dependentID {
				t.Fatalf(
					"locked dependent %s id=%q, want %q",
					dependent, lockedDependentID, dependentID,
				)
			}

			deactivation := postgresResourceLifecycleMutation(
				fixture, clientresources.LocationKind, locationID,
			)
			var (
				update    clientresources.UpdateMutation
				updateRun func(context.Context, clientresources.UpdateMutation) error
			)
			switch dependent {
			case clientresources.ContactKind:
				displayName := "Updated Contact"
				update = postgresResourceUpdateMutation(
					fixture, dependent, dependentID,
					clientresources.UpdatePatch{DisplayName: &displayName},
				)
				updateRun = func(
					ctx context.Context,
					accepted clientresources.UpdateMutation,
				) error {
					_, err := NewContactRepositoryFromPool(pool).UpdateContact(
						ctx, accepted,
					)
					return err
				}
			case clientresources.AssetKind:
				name := "UPDATED-ASSET"
				update = postgresResourceUpdateMutation(
					fixture, dependent, dependentID,
					clientresources.UpdatePatch{Name: &name},
				)
				updateRun = func(
					ctx context.Context,
					accepted clientresources.UpdateMutation,
				) error {
					_, err := NewAssetRepositoryFromPool(pool).UpdateAsset(
						ctx, accepted,
					)
					return err
				}
			}

			type raceResult struct {
				writer string
				err    error
			}
			results := make(chan raceResult, 2)
			writerCtx, cancelWriters := context.WithTimeout(ctx, 15*time.Second)
			defer cancelWriters()
			go func() {
				_, err := locationRepository.DeactivateLocation(
					writerCtx, deactivation,
				)
				results <- raceResult{writer: "location", err: err}
			}()

			waitCtx, cancelWait := context.WithTimeout(ctx, 10*time.Second)
			deactivationPID, waitErr := waitForPostgresBlockedPID(
				waitCtx, pool, blockerPID, "FROM "+postgresResourceTable(dependent),
			)
			cancelWait()
			if waitErr != nil {
				_ = blocker.Rollback(context.Background())
				blockerReleased = true
				cancelWriters()
				<-results
				t.Fatalf(
					"observe Location deactivation waiting on %s row: %v",
					dependent, waitErr,
				)
			}
			t.Logf(
				"observed Location deactivation PID %d waiting on locked %s row",
				deactivationPID, dependent,
			)

			go func() {
				results <- raceResult{
					writer: string(dependent),
					err:    updateRun(writerCtx, update),
				}
			}()
			waitCtx, cancelWait = context.WithTimeout(ctx, 10*time.Second)
			updatePID, waitErr := waitForPostgresBlockedPID(
				waitCtx, pool, deactivationPID, "FROM locations",
			)
			cancelWait()
			if waitErr != nil {
				_ = blocker.Rollback(context.Background())
				blockerReleased = true
				cancelWriters()
				for range 2 {
					<-results
				}
				t.Fatalf(
					"observe dependent %s mutation waiting on Location lock: %v",
					dependent, waitErr,
				)
			}
			t.Logf(
				"observed dependent %s mutation PID %d waiting on Location deactivation PID %d",
				dependent, updatePID, deactivationPID,
			)
			if err := blocker.Rollback(ctx); err != nil {
				blockerReleased = true
				cancelWriters()
				for range 2 {
					<-results
				}
				t.Fatalf("release dependent %s blocker: %v", dependent, err)
			}
			blockerReleased = true

			outcomes := map[string]error{}
			for range 2 {
				outcome := <-results
				outcomes[outcome.writer] = outcome.err
			}
			if err := outcomes[string(dependent)]; err != nil {
				t.Fatalf("dependent %s mutation error=%v", dependent, err)
			}
			if err := outcomes["location"]; !errors.Is(
				err, clientresources.ErrResourceInUse,
			) {
				t.Fatalf(
					"Location deactivation error=%v, want resource in use",
					err,
				)
			}

			var locationState string
			var locationVersion, dependentVersion int64
			if err := pool.QueryRow(ctx, `
SELECT lifecycle_state, version
FROM locations
WHERE msp_id = $1 AND client_id = $2 AND id = $3
`, fixture.mspID, fixture.clientID, locationID).Scan(
				&locationState, &locationVersion,
			); err != nil {
				t.Fatalf("load Location after race: %v", err)
			}
			if err := pool.QueryRow(
				ctx,
				"SELECT version FROM "+postgresResourceTable(dependent)+
					" WHERE msp_id = $1 AND client_id = $2 AND id = $3",
				fixture.mspID, fixture.clientID, dependentID,
			).Scan(&dependentVersion); err != nil {
				t.Fatalf("load dependent %s after race: %v", dependent, err)
			}
			if locationState != "active" || locationVersion != 1 ||
				dependentVersion != 2 {
				t.Fatalf(
					"serialized state Location=%s/%d %s_version=%d",
					locationState, locationVersion, dependent, dependentVersion,
				)
			}
		})
	}

	t.Run("resource audit and outbox rollback is atomic", func(t *testing.T) {
		fixture := seedClientResourcePostgresScope(t, ctx, pool)
		resourceID := uuid.NewString()
		accepted := postgresResourceCreateMutation(
			fixture, clientresources.LocationKind, resourceID, "",
		)
		accepted.Event.SubjectVersion = 0

		err := NewLocationRepositoryFromPool(pool).CreateAtomic(ctx, accepted)
		if err == nil {
			t.Fatal("CreateAtomic() succeeded with invalid outbox subject version")
		}
		assertNoPostgresMutationFacts(
			t, ctx, pool, "locations", resourceID,
			accepted.Audit.ID, accepted.Event.EventID,
		)
	})
}

type clientResourcePostgresFixture struct {
	mspID    string
	clientID string
	actorID  string
	now      time.Time
}

func openClientResourcePostgresTest(
	t *testing.T,
) (context.Context, *pgxpool.Pool) {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL concurrency acceptance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse test database configuration: %v", err)
	}
	if config.MaxConns < 8 {
		config.MaxConns = 8
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}
	return ctx, pool
}

func seedClientResourcePostgresScope(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) clientResourcePostgresFixture {
	t.Helper()
	fixture := clientResourcePostgresFixture{
		mspID: uuid.NewString(), clientID: uuid.NewString(),
		actorID: uuid.NewString(), now: time.Now().UTC(),
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO msp_organizations (
  id, display_id, name, lifecycle_state, version,
  created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, 'Task 11 Test MSP', 'active', 1, $3, $4, $3, $4)
`, fixture.mspID, "TASK11-MSP-"+fixture.mspID, fixture.now, fixture.actorID); err != nil {
		t.Fatalf("seed MSP: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO client_organizations (
  id, msp_id, display_id, name, lifecycle_state, version,
  created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, $3, 'Task 11 Test Client', 'active', 1, $4, $5, $4, $5)
`, fixture.clientID, fixture.mspID, "TASK11-CLIENT-"+fixture.clientID,
		fixture.now, fixture.actorID); err != nil {
		t.Fatalf("seed Client: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(), 30*time.Second,
		)
		defer cancel()
		if err := cleanupClientResourcePostgresFixture(
			cleanupCtx, pool, fixture.mspID,
		); err != nil {
			t.Errorf("clean Task 11 PostgreSQL fixture: %v", err)
		}
	})
	return fixture
}

func cleanupClientResourcePostgresFixture(
	ctx context.Context,
	pool *pgxpool.Pool,
	mspID string,
) error {
	if _, err := uuid.Parse(mspID); err != nil {
		return fmt.Errorf("invalid fixture MSP ID: %w", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	statements := []string{
		"ALTER TABLE audit_ledger DISABLE TRIGGER audit_ledger_append_only",
		"DELETE FROM event_outbox WHERE msp_id = $1",
		"DELETE FROM audit_ledger WHERE msp_id = $1",
		"DELETE FROM knowledge_article_versions WHERE msp_id = $1",
		"DELETE FROM knowledge_articles WHERE msp_id = $1",
		"DELETE FROM work_records WHERE msp_id = $1",
		"DELETE FROM queues WHERE msp_id = $1",
		"DELETE FROM assets WHERE msp_id = $1",
		"DELETE FROM contacts WHERE msp_id = $1",
		"DELETE FROM contracts WHERE msp_id = $1",
		"DELETE FROM services WHERE msp_id = $1",
		"DELETE FROM locations WHERE msp_id = $1",
		"DELETE FROM client_organizations WHERE msp_id = $1",
		"DELETE FROM msp_organizations WHERE id = $1",
		"ALTER TABLE audit_ledger ENABLE TRIGGER audit_ledger_append_only",
	}
	for _, statement := range statements {
		args := []any(nil)
		if strings.Contains(statement, "$1") {
			args = []any{mspID}
		}
		if _, err := tx.Exec(ctx, statement, args...); err != nil {
			return fmt.Errorf("%s: %w", statement, err)
		}
	}
	return tx.Commit(ctx)
}

func beginPostgresClientDeactivation(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture clientResourcePostgresFixture,
) (pgx.Tx, int32) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin Client deactivation: %v", err)
	}
	var blockerPID int32
	if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("read Client deactivation backend PID: %v", err)
	}
	tag, err := tx.Exec(ctx, `
UPDATE client_organizations
SET lifecycle_state = 'inactive', version = version + 1,
    updated_at = $3, updated_by = $4
WHERE msp_id = $1 AND id = $2 AND lifecycle_state = 'active'
`, fixture.mspID, fixture.clientID, fixture.now, fixture.actorID)
	if err != nil || tag.RowsAffected() != 1 {
		_ = tx.Rollback(context.Background())
		t.Fatalf("stage Client deactivation rows=%d error=%v", tag.RowsAffected(), err)
	}
	return tx, blockerPID
}

func postgresKnowledgeDraftMutation(
	fixture clientResourcePostgresFixture,
	articleID string,
) knowledge.DraftMutation {
	audit, event := postgresOperationalFacts(
		fixture, articleID, "knowledge.draft.created",
		"knowledge_article", 1,
	)
	return knowledge.DraftMutation{
		Created: true,
		Article: knowledge.Article{
			ID: articleID, MSPID: fixture.mspID, ClientID: fixture.clientID,
			DisplayID: "TASK11-KB-" + articleID, Title: "Task 11 Recovery",
			State: knowledge.Draft, CurrentVersion: 1,
			UpdatedAt: fixture.now, UpdatedBy: fixture.actorID,
		},
		Version: knowledge.Version{
			ArticleID: articleID, Version: 1, Body: "Recovery steps",
			State: knowledge.Draft, CreatedAt: fixture.now,
			CreatedBy: fixture.actorID,
		},
		Audit: audit, Event: event,
	}
}

func seedPostgresRoutableTicket(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture clientResourcePostgresFixture,
) (string, string) {
	t.Helper()
	recordID, queueID := uuid.NewString(), uuid.NewString()
	if _, err := pool.Exec(ctx, `
INSERT INTO queues (id, msp_id, client_id, key, name, version)
VALUES ($1, $2, $3, 'task11-route', 'Task 11 Route', 1)
`, queueID, fixture.mspID, fixture.clientID); err != nil {
		t.Fatalf("seed route queue: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO work_records (
  id, msp_id, client_id, display_id, record_type, title, status,
  priority, lifecycle_state, version, created_at, created_by,
  updated_at, updated_by
) VALUES (
  $1, $2, $3, $4, 'incident', 'Task 11 ticket', 'new',
  'normal', 'active', 1, $5, $6, $5, $6
)
`, recordID, fixture.mspID, fixture.clientID,
		"TASK11-TICKET-"+recordID, fixture.now, fixture.actorID); err != nil {
		t.Fatalf("seed routable ticket: %v", err)
	}
	return recordID, queueID
}

func postgresQueueMutation(
	fixture clientResourcePostgresFixture,
	recordID string,
	queueID string,
) workrecords.QueueMutation {
	at := fixture.now.Add(time.Second)
	audit, event := postgresOperationalFacts(
		fixture, recordID, "work_record.queue.changed", "work_record", 2,
	)
	audit.OccurredAt, event.OccurredAt = at, at
	return workrecords.QueueMutation{
		Record: workrecords.Record{
			Envelope: object.Envelope{
				ID: recordID, MSPID: fixture.mspID, ClientID: fixture.clientID,
				Version: 2, UpdatedAt: at, UpdatedBy: fixture.actorID,
			},
			QueueID: queueID,
		},
		Queue: workrecords.QueueRef{
			ID: queueID, MSPID: fixture.mspID, ClientID: fixture.clientID,
			Key: "task11-route", Name: "Task 11 Route", Version: 1,
		},
		Audit: audit, Event: event,
	}
}

func postgresOperationalFacts(
	fixture clientResourcePostgresFixture,
	subjectID string,
	action string,
	subjectType string,
	version int64,
) (mutation.AuditRecord, mutation.EventRecord) {
	correlationID := uuid.NewString()
	audit := mutation.AuditRecord{
		ID: uuid.NewString(), OccurredAt: fixture.now,
		MSPID: fixture.mspID, ClientID: fixture.clientID,
		ActorType: "technician", ActorID: fixture.actorID,
		Action: action, SubjectType: subjectType, SubjectID: subjectID,
		SubjectVersion: version, Source: "ai_workspace",
		Reason:        "Task 11 concurrency acceptance",
		CorrelationID: correlationID,
	}
	event := mutation.EventRecord{
		EventID: uuid.NewString(), EventType: action, SchemaVersion: 1,
		OccurredAt: fixture.now, MSPID: fixture.mspID,
		ClientID: fixture.clientID, ActorType: "technician",
		ActorID: fixture.actorID, SubjectType: subjectType,
		SubjectID: subjectID, SubjectVersion: version,
		CorrelationID: correlationID, Source: "ai_workspace",
	}
	return audit, event
}

func postgresResourceCreateMutation(
	fixture clientResourcePostgresFixture,
	kind clientresources.Kind,
	resourceID string,
	locationID string,
) clientresources.CreateMutation {
	displayID := "TASK11-" + strings.ToUpper(string(kind)) + "-" + resourceID
	envelope := object.Envelope{
		ID: resourceID, ObjectType: string(kind),
		MSPID: fixture.mspID, ClientID: fixture.clientID,
		DisplayID: displayID, LifecycleState: "active", Version: 1,
		CreatedAt: fixture.now, CreatedBy: fixture.actorID,
		UpdatedAt: fixture.now, UpdatedBy: fixture.actorID,
	}
	action := string(kind) + ".created"
	accepted := clientresources.CreateMutation{
		Kind: string(kind), Object: envelope,
		Audit: postgresResourceAudit(
			fixture, kind, resourceID, 1, action,
		),
		Event: postgresResourceEvent(
			fixture, kind, resourceID, 1, action,
		),
	}
	accepted.Event.CorrelationID = accepted.Audit.CorrelationID
	switch kind {
	case clientresources.LocationKind:
		accepted.Payload = clientresources.Location{
			Envelope: envelope, Name: "Headquarters",
		}
	case clientresources.ContactKind:
		accepted.Payload = clientresources.Contact{
			Envelope: envelope, LocationID: locationID,
			DisplayName: "Task 11 Contact",
			Email:       "task11@example.test", Phone: "555-0111",
		}
	case clientresources.AssetKind:
		accepted.Payload = clientresources.Asset{
			Envelope: envelope, LocationID: locationID,
			Name: "TASK11-ASSET", AssetType: "server",
			Provenance: clientresources.Provenance{
				Authority: clientresources.TechnicianConfirmed,
			},
		}
	}
	return accepted
}

func postgresResourceUpdateMutation(
	fixture clientResourcePostgresFixture,
	kind clientresources.Kind,
	resourceID string,
	patch clientresources.UpdatePatch,
) clientresources.UpdateMutation {
	action := string(kind) + ".updated"
	accepted := clientresources.UpdateMutation{
		Target: scope.Target{
			MSPID: fixture.mspID, ClientID: fixture.clientID,
		},
		ResourceID: resourceID, ExpectedVersion: 1, Patch: patch,
		ActorID: fixture.actorID, UpdatedAt: fixture.now.Add(time.Second),
		Audit: postgresResourceAudit(
			fixture, kind, resourceID, 2, action,
		),
		Event: postgresResourceEvent(
			fixture, kind, resourceID, 2, action,
		),
	}
	accepted.Event.CorrelationID = accepted.Audit.CorrelationID
	return accepted
}

func postgresResourceLifecycleMutation(
	fixture clientResourcePostgresFixture,
	kind clientresources.Kind,
	resourceID string,
) clientresources.LifecycleMutation {
	action := string(kind) + ".deactivated"
	accepted := clientresources.LifecycleMutation{
		Target: scope.Target{
			MSPID: fixture.mspID, ClientID: fixture.clientID,
		},
		ResourceID: resourceID, ExpectedVersion: 1,
		FromState: "active", ToState: "inactive",
		ActorID: fixture.actorID, UpdatedAt: fixture.now.Add(time.Second),
		Audit: postgresResourceAudit(
			fixture, kind, resourceID, 2, action,
		),
		Event: postgresResourceEvent(
			fixture, kind, resourceID, 2, action,
		),
	}
	accepted.Event.CorrelationID = accepted.Audit.CorrelationID
	return accepted
}

func postgresResourceAudit(
	fixture clientResourcePostgresFixture,
	kind clientresources.Kind,
	resourceID string,
	version int64,
	action string,
) mutation.AuditRecord {
	return mutation.AuditRecord{
		ID: uuid.NewString(), OccurredAt: fixture.now,
		MSPID: fixture.mspID, ClientID: fixture.clientID,
		ActorType: "technician", ActorID: fixture.actorID,
		Action: action, SubjectType: string(kind), SubjectID: resourceID,
		SubjectVersion: version, Source: "test", Reason: "Task 11 acceptance",
		CorrelationID: uuid.NewString(),
	}
}

func postgresResourceEvent(
	fixture clientResourcePostgresFixture,
	kind clientresources.Kind,
	resourceID string,
	version int64,
	action string,
) mutation.EventRecord {
	return mutation.EventRecord{
		EventID: uuid.NewString(), EventType: action, SchemaVersion: 1,
		OccurredAt: fixture.now, MSPID: fixture.mspID,
		ClientID: fixture.clientID, ActorType: "technician",
		ActorID: fixture.actorID, SubjectType: string(kind),
		SubjectID: resourceID, SubjectVersion: version, Source: "test",
		CorrelationID: uuid.NewString(),
	}
}

func postgresDependentRepository(
	pool *pgxpool.Pool,
	kind clientresources.Kind,
) clientresources.Repository {
	switch kind {
	case clientresources.ContactKind:
		return NewContactRepositoryFromPool(pool)
	case clientresources.AssetKind:
		return NewAssetRepositoryFromPool(pool)
	default:
		panic(fmt.Sprintf("unsupported dependent resource kind %q", kind))
	}
}

func postgresResourceTable(kind clientresources.Kind) string {
	switch kind {
	case clientresources.ContactKind:
		return "contacts"
	case clientresources.AssetKind:
		return "assets"
	default:
		panic(fmt.Sprintf("unsupported resource table kind %q", kind))
	}
}

func waitForPostgresBlocker(
	ctx context.Context,
	pool *pgxpool.Pool,
	blockerPID int32,
	want int,
) error {
	const query = `
SELECT count(*)
FROM pg_stat_activity AS waiting
WHERE waiting.datname = current_database()
  AND waiting.pid <> $1
  AND $1 = ANY(pg_blocking_pids(waiting.pid))
`
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	lastObserved := 0
	for {
		if err := pool.QueryRow(ctx, query, blockerPID).Scan(&lastObserved); err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf(
					"wait for %d blocked PostgreSQL writers (last observed %d): %w",
					want, lastObserved, ctx.Err(),
				)
			}
			return err
		}
		if lastObserved >= want {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"wait for %d blocked PostgreSQL writers (last observed %d): %w",
				want, lastObserved, ctx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func waitForPostgresBlockedPID(
	ctx context.Context,
	pool *pgxpool.Pool,
	blockerPID int32,
	queryFragment string,
) (int32, error) {
	const query = `
SELECT waiting.pid
FROM pg_stat_activity AS waiting
WHERE waiting.datname = current_database()
  AND waiting.pid <> $1
  AND $1 = ANY(pg_blocking_pids(waiting.pid))
  AND position($2 in waiting.query) > 0
ORDER BY waiting.query_start, waiting.pid
LIMIT 1
`
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waitingPID int32
		err := pool.QueryRow(
			ctx, query, blockerPID, queryFragment,
		).Scan(&waitingPID)
		if err == nil {
			return waitingPID, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			if ctx.Err() != nil {
				return 0, fmt.Errorf(
					"wait for PostgreSQL query containing %q blocked by PID %d: %w",
					queryFragment, blockerPID, ctx.Err(),
				)
			}
			return 0, err
		}
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf(
				"wait for PostgreSQL query containing %q blocked by PID %d: %w",
				queryFragment, blockerPID, ctx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func assertNoPostgresMutationFacts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	resourceTable string,
	resourceID string,
	auditID string,
	eventID string,
) {
	t.Helper()
	for _, record := range []struct {
		table  string
		column string
		id     string
	}{
		{resourceTable, "id", resourceID},
		{"audit_ledger", "id", auditID},
		{"event_outbox", "event_id", eventID},
	} {
		var count int
		if err := pool.QueryRow(
			ctx,
			"SELECT count(*) FROM "+record.table+
				" WHERE "+record.column+" = $1",
			record.id,
		).Scan(&count); err != nil {
			t.Fatalf("count %s rollback records: %v", record.table, err)
		}
		if count != 0 {
			t.Fatalf(
				"%s retained %d record(s) after rollback",
				record.table, count,
			)
		}
	}
}

func assertPostgresFactCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	table string,
	subjectID string,
	action string,
	want int,
) {
	t.Helper()
	actionColumn := "action"
	if table == "event_outbox" {
		actionColumn = "event_type"
	}
	var got int
	if err := pool.QueryRow(
		ctx,
		"SELECT count(*) FROM "+table+
			" WHERE subject_id = $1 AND "+actionColumn+" = $2",
		subjectID, action,
	).Scan(&got); err != nil {
		t.Fatalf("count %s %s facts: %v", table, action, err)
	}
	if got != want {
		t.Fatalf("%s %s facts=%d, want %d", table, action, got, want)
	}
}

package psa

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/datto"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestDattoRepositoryClaimsDueConnectionWithDecryptedCursor(t *testing.T) {
	provider := graphTestProvider(t)
	sealed, err := provider.Seal(
		context.Background(), "datto-cursor:connection-id",
		[]byte("cursor-1"),
	)
	if err != nil {
		t.Fatal(err)
	}
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*(destinations[0].(*string)) = "connection-id"
			*(destinations[1].(*string)) = "msp-id"
			*(destinations[2].(*string)) = "db://datto/connection-id/1/credential"
			*(destinations[3].(*int64)) = 900
			*(destinations[4].(*time.Time)) = time.Date(2026, time.July, 29, 20, 0, 0, 0, time.UTC)
			*(destinations[5].(*int)) = sealed.Version
			*(destinations[6].(*[]byte)) = sealed.Nonce
			*(destinations[7].(*[]byte)) = sealed.Ciphertext
			*(destinations[8].(*bool)) = true
			*(destinations[9].(*string)) = "manual-request-id"
		},
	}}}
	connections, err := NewDattoRepository(
		db, provider, func() string { return "id" },
	).Claim(context.Background(), 25, time.Now(), 30*time.Minute)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if len(connections) != 1 ||
		connections[0].Cursor != "cursor-1" ||
		!connections[0].Manual ||
		connections[0].ManualRequestID != "manual-request-id" ||
		connections[0].SyncInterval != 15*time.Minute ||
		connections[0].CredentialSecretRef != "db://datto/connection-id/1/credential" ||
		!strings.Contains(db.query, "configuration_generation") ||
		!strings.Contains(db.query, "$1::timestamptz - connection.sync_interval") ||
		!strings.Contains(db.query, "FOR UPDATE OF connection SKIP LOCKED") {
		t.Fatalf("connections=%+v query=%s", connections, db.query)
	}
}

func TestDattoRepositoryReactivationLocksClientAssetAndRetainedLocationBeforeUpdate(t *testing.T) {
	current := resourceDetail(clientresources.AssetKind, "inactive", 7)
	current.LocationID = "location-id"
	current.SourceSystem = "datto"
	current.ExternalID = "device-id"
	current.Authority = clientresources.Discovered
	tx := &fakeSalesTx{queryRows: []row{
		dattoAssetScopeRow("msp-id", "client-id", "resource"),
		resourceDetailRow(current),
		resourceDetailRow(current),
	}}
	repository := NewDattoRepository(
		&fakeSalesDB{tx: tx}, graphTestProvider(t),
		func() string { return "11111111-1111-4111-8111-111111111111" },
	)
	err := repository.Apply(context.Background(), "connection-id", datto.SyncPage{
		ObservedAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
		Assets:     []datto.RemoteAsset{{ExternalID: "device-id", SiteID: "site-id", Hostname: "WORKSTATION-1"}},
	}, false)
	if err != nil {
		t.Fatalf("Apply() error=%v", err)
	}
	assetCalls := callIndexesContaining(tx.calls, "SELECT id::text, 'asset'::text")
	locationCalls := callIndexesContaining(tx.calls, "FROM locations")
	if len(assetCalls) != 2 || len(locationCalls) != 1 ||
		assetCalls[0] >= locationCalls[0] || locationCalls[0] >= assetCalls[1] ||
		strings.Contains(tx.calls[assetCalls[0]], "FOR UPDATE") ||
		!strings.Contains(tx.calls[locationCalls[0]], "FOR SHARE") ||
		!strings.Contains(tx.calls[locationCalls[0]], "lifecycle_state = 'active'") ||
		!strings.Contains(tx.calls[assetCalls[1]], "FOR UPDATE") {
		t.Fatalf("Datto uses cyclic Asset/Location lock order: calls=%#v", tx.calls)
	}
	assertCallSubsequence(t, tx.calls,
		"INSERT INTO datto_asset_snapshots",
		"FROM datto_asset_snapshots snapshot",
		"FROM client_organizations",
		"SELECT id::text, 'asset'::text",
		"FROM locations",
		"SELECT id::text, 'asset'::text",
		"UPDATE assets asset\nSET name = COALESCE",
	)
	update := findCall(t, tx.calls, "UPDATE assets asset\nSET name = COALESCE")
	if !containsAll(update,
		"asset.id = $3", "asset.version = $4", "asset.lifecycle_state = $5",
		"version = asset.version + 1",
	) {
		t.Fatalf("Datto authoritative update lacks locked state predicate: %s", update)
	}
}

func TestDattoRepositorySkipsWhenAssetLocationChangesAfterLocationLock(t *testing.T) {
	snapshot := resourceDetail(clientresources.AssetKind, "inactive", 7)
	snapshot.LocationID = "old-location"
	snapshot.SourceSystem = "datto"
	snapshot.ExternalID = "device-id"
	snapshot.Authority = clientresources.Discovered
	locked := snapshot
	locked.LocationID = "changed-location"
	tx := &fakeSalesTx{queryRows: []row{
		dattoAssetScopeRow("msp-id", "client-id", "resource"),
		resourceDetailRow(snapshot),
		resourceDetailRow(locked),
	}}
	repository := NewDattoRepository(
		&fakeSalesDB{tx: tx}, graphTestProvider(t),
		func() string { return "11111111-1111-4111-8111-111111111111" },
	)
	err := repository.Apply(context.Background(), "connection-id", datto.SyncPage{
		ObservedAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
		Assets:     []datto.RemoteAsset{{ExternalID: "device-id", SiteID: "site-id"}},
	}, false)
	if err != nil {
		t.Fatalf("Apply() error=%v", err)
	}
	if strings.Contains(strings.Join(tx.calls, "\n"), "UPDATE assets asset\nSET name = COALESCE") ||
		!tx.committed || tx.rolledBack {
		t.Fatalf("changed retained relationship was not safely skipped: tx=%+v", tx)
	}
}

func TestDattoRepositorySkipsReactivationWhenRetainedLocationIsInactive(t *testing.T) {
	current := resourceDetail(clientresources.AssetKind, "inactive", 7)
	current.LocationID = "location-id"
	current.SourceSystem = "datto"
	current.ExternalID = "device-id"
	current.Authority = clientresources.Discovered
	tx := &fakeSalesTx{
		queryRows: []row{
			dattoAssetScopeRow("msp-id", "client-id", "resource"),
			resourceDetailRow(current),
		},
		zeroRowsAt: 3,
	}
	repository := NewDattoRepository(
		&fakeSalesDB{tx: tx}, graphTestProvider(t),
		func() string { return "11111111-1111-4111-8111-111111111111" },
	)
	err := repository.Apply(context.Background(), "connection-id", datto.SyncPage{
		ObservedAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
		Assets:     []datto.RemoteAsset{{ExternalID: "device-id", SiteID: "site-id", Hostname: "WORKSTATION-1"}},
	}, false)
	if err != nil {
		t.Fatalf("Apply() error=%v", err)
	}
	if strings.Contains(strings.Join(tx.calls, "\n"), "UPDATE assets asset\nSET name = COALESCE") {
		t.Fatalf("inactive retained Location allowed Datto reactivation: %v", tx.calls)
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("safe skipped relationship should preserve snapshot transaction: %+v", tx)
	}
}

func TestDattoRepositoryRollsBackUnexpectedAuthoritativeScopeLookupFailure(t *testing.T) {
	tx := &fakeSalesTx{queryRows: []row{fakeRow{err: errors.New("scope lookup failed")}}}
	repository := NewDattoRepository(
		&fakeSalesDB{tx: tx}, graphTestProvider(t),
		func() string { return "11111111-1111-4111-8111-111111111111" },
	)
	err := repository.Apply(context.Background(), "connection-id", datto.SyncPage{
		ObservedAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
		Assets:     []datto.RemoteAsset{{ExternalID: "device-id", SiteID: "site-id"}},
	}, false)
	if err == nil || err.Error() != "scope lookup failed" || !tx.rolledBack || tx.committed {
		t.Fatalf("scope lookup failure was hidden: error=%v tx=%+v", err, tx)
	}
}

func dattoAssetScopeRow(mspID, clientID, assetID string) row {
	return fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*string) = mspID
		*destinations[1].(*string) = clientID
		*destinations[2].(*string) = assetID
	}}
}

func assertCallSubsequence(t *testing.T, calls []string, fragments ...string) {
	t.Helper()
	next := 0
	for _, call := range calls {
		for next < len(fragments) && strings.Contains(call, fragments[next]) {
			next++
		}
	}
	if next != len(fragments) {
		t.Fatalf("calls missing ordered fragment %q: %#v", fragments[next], calls)
	}
}

func findCall(t *testing.T, calls []string, fragment string) string {
	t.Helper()
	for _, call := range calls {
		if strings.Contains(call, fragment) {
			return call
		}
	}
	t.Fatalf("call containing %q not found: %#v", fragment, calls)
	return ""
}

func TestNewDattoRepositoryFromPoolBuildsProductionAdapter(t *testing.T) {
	repository := NewDattoRepositoryFromPool(
		nil, graphTestProvider(t), func() string { return "id" },
	)
	if repository == nil || repository.db == nil {
		t.Fatal("production repository did not retain a pool adapter")
	}
}

func TestDattoRepositoryAppliesSnapshotsCandidatesAndStaleStateAtomically(t *testing.T) {
	current := resourceDetail(clientresources.AssetKind, "active", 7)
	current.SourceSystem = "datto"
	current.ExternalID = "device-id"
	current.Authority = clientresources.Discovered
	tx := &fakeSalesTx{queryRows: []row{
		dattoAssetScopeRow("msp-id", "client-id", "resource"),
		resourceDetailRow(current),
		resourceDetailRow(current),
	}}
	repository := NewDattoRepository(
		&fakeSalesDB{tx: tx}, graphTestProvider(t),
		func() string { return "11111111-1111-4111-8111-111111111111" },
	)
	err := repository.Apply(
		context.Background(), "connection-id",
		datto.SyncPage{Assets: []datto.RemoteAsset{{
			ExternalID: "device-id", SiteID: "site-id",
			Hostname: "WORKSTATION-1", SerialNumber: "SERIAL-1",
			MACAddresses:     []string{"00:11:22:33:44:55"},
			SourcePayloadRef: "datto/connection/devices-page-1.json",
			SourceUpdatedAt:  time.Now(),
		}}, Alerts: []datto.AlertObservation{{
			ExternalID: "alert-id", ExternalDeviceID: "device-id",
			SiteID: "site-id", Priority: "Critical",
			Title: "Gateway unreachable", Diagnostics: "Gateway unreachable",
			Fingerprint: "fingerprint", State: datto.AlertActive,
			ObservedAt: time.Now(), SourcePayloadRef: "datto/alerts-open.json",
		}}},
		true,
	)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("transaction state committed=%v rolledBack=%v", tx.committed, tx.rolledBack)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO datto_asset_snapshots",
		"FROM client_organizations",
		"UPDATE assets asset\nSET name = COALESCE",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
		"INSERT INTO datto_reconciliation_candidates",
		"INSERT INTO assets",
		"UPDATE datto_asset_snapshots",
		"INSERT INTO object_tag_assignments",
		"INSERT INTO tag_assignment_events",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
		"INSERT INTO datto_alert_queue",
		"UPDATE datto_asset_snapshots",
		"UPDATE assets asset\nSET lifecycle_state = 'inactive'",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
}

func TestDattoFallbackUsesCanonicalSystemTagAndRollsBackWhenItCannotWrite(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 1}
	err := insertSystemFallbackAssignment(context.Background(), tx, "connection-id", "device-id")
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("fallback error=%v, want not found when assignment insert affects zero rows", err)
	}
	if len(tx.queries) != 1 || !strings.Contains(tx.queries[0], "taxonomy.system.unclassified") {
		t.Fatalf("fallback queries=%v", tx.queries)
	}
}

func TestDattoRepositoryCompletesRunCursorAuditAndOutboxAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewDattoRepository(
		&fakeSalesDB{tx: tx}, graphTestProvider(t),
		func() string { return "11111111-1111-4111-8111-111111111111" },
	)
	err := repository.Complete(context.Background(), datto.SyncCompletion{
		RunID: "run-id", ConnectionID: "connection-id",
		CompletedAt: time.Date(2026, time.July, 29, 23, 0, 0, 0, time.UTC),
		NextCursor:  "cursor-2", AssetsSeen: 10, AssetsChanged: 2,
		RateLimit: datto.RateLimitState{Remaining: 500},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"UPDATE datto_sync_runs",
		"UPDATE datto_connections",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
	if !tx.committed || tx.rolledBack {
		t.Fatalf("transaction state committed=%v rolledBack=%v", tx.committed, tx.rolledBack)
	}
}

func TestDattoRepositoryQueuesManualSyncWithAuditAndOutbox(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewDattoRepository(
		&fakeSalesDB{tx: tx}, graphTestProvider(t),
		func() string { return "11111111-1111-4111-8111-111111111111" },
	)
	err := repository.QueueManualSync(
		context.Background(),
		datto.ManualSyncRequest{
			ID: "request-id", ConnectionID: "connection-id",
			MSPID: "msp-id", RequestedAt: time.Now(),
			RequestedBy: "technician-id",
		},
	)
	if err != nil {
		t.Fatalf("QueueManualSync() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"UPDATE datto_connections",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
}

func TestDattoRepositoryMapsSiteAndReleasesBlockedAlertsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewDattoRepository(
		&fakeSalesDB{tx: tx}, nil,
		func() string { return "11111111-1111-4111-8111-111111111111" },
	)
	err := repository.MapSite(
		context.Background(),
		datto.SiteMapping{
			ID: "mapping-id", ConnectionID: "connection-id",
			MSPID: "msp-id", SiteID: "site-id", ClientID: "client-id",
			Reason: "verified Datto site", MappedAt: time.Now(),
			MappedBy: "technician-id",
		},
	)
	if err != nil {
		t.Fatalf("MapSite() error=%v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO datto_site_mappings",
		"UPDATE datto_alert_queue",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
	if !tx.committed || tx.rolledBack {
		t.Fatalf(
			"transaction committed=%v rolledBack=%v",
			tx.committed, tx.rolledBack,
		)
	}
}

func TestDattoRepositoryProgressIncludesReviewAndAlertBacklogs(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		if len(destinations) != 14 {
			t.Fatalf("Progress() scan destinations=%d, want 14", len(destinations))
		}
		*(destinations[0].(*string)) = "connection-id"
		*(destinations[1].(*string)) = "idle"
		*(destinations[2].(*string)) = ""
		*(destinations[3].(*string)) = ""
		*(destinations[4].(**time.Time)) = nil
		*(destinations[5].(**time.Time)) = nil
		*(destinations[6].(*int)) = 10
		*(destinations[7].(*int)) = 2
		*(destinations[8].(*[]byte)) = []byte(`{"remaining":500}`)
		*(destinations[9].(*string)) = ""
		*(destinations[10].(*string)) = "healthy"
		*(destinations[11].(*int)) = 3
		*(destinations[12].(*int)) = 4
		*(destinations[13].(*int)) = 1
	}}}
	result, err := NewDattoRepository(
		db, nil, func() string { return "id" },
	).Progress(
		context.Background(),
		scope.Target{MSPID: "msp-id"},
		"connection-id",
	)
	if err != nil ||
		result.PendingCandidates != 3 ||
		result.PendingAlerts != 4 ||
		result.BlockedAlerts != 1 {
		t.Fatalf("Progress() result=%+v error=%v", result, err)
	}
}

func TestDattoRepositoryListsPendingCandidatesWithinTrustedScope(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*(destinations[0].(*string)) = "candidate-id"
			*(destinations[1].(*string)) = "snapshot-id"
			*(destinations[2].(*string)) = "msp-id"
			*(destinations[3].(*string)) = "client-id"
			*(destinations[4].(*string)) = "asset-id"
			*(destinations[5].(*[]byte)) = []byte(
				`{"hostname":"WORKSTATION-1"}`,
			)
			*(destinations[6].(*string)) = "pending"
		},
	}}}
	result, err := NewDattoRepository(
		db, nil, func() string { return "id" },
	).ListCandidates(
		context.Background(),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		25,
	)
	if err != nil || len(result) != 1 ||
		result[0].Evidence["hostname"] != "WORKSTATION-1" ||
		!strings.Contains(
			db.query,
			"candidate.client_id = NULLIF($2::text, '')::uuid",
		) ||
		!strings.Contains(db.query, "LIMIT $3") {
		t.Fatalf(
			"ListCandidates() result=%+v error=%v query=%s",
			result, err, db.query,
		)
	}
}

func TestDattoRepositoryClaimsOneAlertPerFingerprint(t *testing.T) {
	observedAt := time.Date(2026, time.July, 29, 21, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*(destinations[0].(*string)) = "queue-id"
			*(destinations[1].(*string)) = "connection-id"
			*(destinations[2].(*string)) = "msp-id"
			*(destinations[3].(*string)) = "client-id"
			*(destinations[4].(*string)) = "actor-id"
			*(destinations[5].(*string)) = "alert-id"
			*(destinations[6].(*string)) = "device-id"
			*(destinations[7].(*string)) = "site-id"
			*(destinations[8].(*string)) = "Critical"
			*(destinations[9].(*string)) = "Gateway unreachable"
			*(destinations[10].(*string)) = "No heartbeat"
			*(destinations[11].(*string)) = "fingerprint"
			*(destinations[12].(*datto.AlertState)) = datto.AlertActive
			*(destinations[13].(*time.Time)) = observedAt
			*(destinations[14].(*string)) = "datto/alerts-open.json"
		},
	}}}
	items, err := NewDattoRepository(
		db, nil, func() string { return "id" },
	).ClaimAlerts(
		context.Background(), 25, observedAt, 5*time.Minute,
	)
	if err != nil || len(items) != 1 ||
		items[0].Observation.ExternalID != "alert-id" ||
		!strings.Contains(db.query, "earlier.fingerprint = candidate.fingerprint") ||
		!strings.Contains(db.query, "SKIP LOCKED") {
		t.Fatalf("ClaimAlerts() items=%+v error=%v query=%s", items, err, db.query)
	}
}

func TestDattoRepositoryResolvesClearedAlertAsRecoveryOnly(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(*datto.AlertAction)) = datto.AlertAppendRecovery
		*(destinations[1].(*string)) = "incident-id"
	}}}
	resolution, err := NewDattoRepository(
		db, nil, func() string { return "id" },
	).ResolveQueuedAlert(
		context.Background(),
		datto.AlertQueueItem{
			ID: "queue-id", ConnectionID: "connection-id",
			Observation: datto.AlertObservation{
				ExternalID: "alert-id", Fingerprint: "fingerprint",
				State: datto.AlertCleared, ObservedAt: time.Now(),
			},
		},
		24*time.Hour,
	)
	if err != nil ||
		resolution.Action != datto.AlertAppendRecovery ||
		resolution.IncidentID != "incident-id" ||
		resolution.ResolveIncident ||
		!strings.Contains(db.query, "exact_any AS") ||
		!strings.Contains(db.query, "timer.resolved_at IS NULL") {
		t.Fatalf(
			"ResolveQueuedAlert() resolution=%+v error=%v query=%s",
			resolution, err, db.query,
		)
	}
}

func TestDattoRepositoryCompletesRecoveryEvidenceWithoutResolvingIncident(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewDattoRepository(
		&fakeSalesDB{tx: tx}, nil,
		func() string { return "11111111-1111-4111-8111-111111111111" },
	)
	item := datto.AlertQueueItem{
		ID: "queue-id", ConnectionID: "connection-id",
		MSPID: "msp-id", ClientID: "client-id", ActorID: "actor-id",
		Observation: datto.AlertObservation{
			ExternalID: "alert-id", Fingerprint: "fingerprint",
			State: datto.AlertCleared, ObservedAt: time.Now(),
			SourcePayloadRef: "datto/alerts-resolved.json",
		},
	}
	err := repository.CompleteAlert(
		context.Background(), item,
		datto.AlertResolution{
			Action: datto.AlertAppendRecovery, IncidentID: "incident-id",
		},
		time.Now(),
	)
	if err != nil {
		t.Fatalf("CompleteAlert() error=%v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO datto_alert_incidents",
		"INSERT INTO datto_alert_observations",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
		"UPDATE datto_alert_queue",
	)
	for _, query := range tx.queries {
		if strings.Contains(query, "UPDATE work_records") {
			t.Fatalf("recovery resolved incident: %s", query)
		}
	}
}

func TestDattoRepositoryReleasesAlertWithContiguousSQLParameters(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewDattoRepository(
		&fakeSalesDB{tx: tx}, nil, func() string { return "id" },
	)
	err := repository.ReleaseAlert(
		context.Background(),
		datto.AlertQueueItem{ID: "queue-id"},
		time.Now(),
		"alert_processing_failed",
	)
	if err != nil {
		t.Fatalf("ReleaseAlert() error=%v", err)
	}
	if len(tx.args) != 1 || len(tx.args[0]) != 2 ||
		!strings.Contains(tx.queries[0], "last_error_code = $2") {
		t.Fatalf("ReleaseAlert() query=%s args=%+v", tx.queries[0], tx.args)
	}
}

func TestDattoRepositoryDecidesCandidateAndAppliesChosenAuthorityAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewDattoRepository(
		&fakeSalesDB{tx: tx}, graphTestProvider(t),
		func() string { return "id" },
	)
	err := repository.DecideCandidate(
		context.Background(),
		datto.CandidateDecision{
			ID: "decision-id", CandidateID: "candidate-id",
			SnapshotID: "snapshot-id", MSPID: "msp-id",
			ClientID: "client-id", AssetID: "asset-id",
			Decision: datto.DecisionChooseDatto, Reason: "verified",
			DecidedAt: time.Now(), DecidedBy: "technician-id",
		},
	)
	if err != nil {
		t.Fatalf("DecideCandidate() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO datto_reconciliation_decisions",
		"UPDATE datto_reconciliation_candidates",
		"UPDATE assets",
		"UPDATE datto_asset_snapshots",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
}

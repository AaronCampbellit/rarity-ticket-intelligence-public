package psa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/datto"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
)

const dattoCursorPurposePrefix = "datto-cursor:"

type DattoRepository struct {
	db                database
	secrets           secrets.Provider
	newID             func() string
	legacyCredentials datto.CredentialResolver
}

var (
	_ datto.SyncQueue                      = (*DattoRepository)(nil)
	_ datto.SyncRepository                 = (*DattoRepository)(nil)
	_ datto.ManagementRepository           = (*DattoRepository)(nil)
	_ datto.AlertQueue                     = (*DattoRepository)(nil)
	_ datto.CredentialResolver             = (*DattoRepository)(nil)
	_ datto.ConnectionManagementRepository = (*DattoRepository)(nil)
)

func NewDattoRepository(
	db database,
	provider secrets.Provider,
	newID func() string,
) *DattoRepository {
	return &DattoRepository{db: db, secrets: provider, newID: newID}
}

func (r *DattoRepository) Claim(
	ctx context.Context,
	limit int,
	now time.Time,
	lease time.Duration,
) ([]datto.Connection, error) {
	rows, err := r.db.Query(ctx, `
WITH candidates AS (
  SELECT connection.id
  FROM datto_connections connection
  WHERE connection.enabled
    AND (
      connection.manual_request_id IS NOT NULL
      OR connection.last_completed_at IS NULL
      OR connection.last_completed_at <=
         $1::timestamptz - connection.sync_interval
    )
    AND (
      connection.lease_until IS NULL
      OR connection.lease_until <= $1
    )
  ORDER BY COALESCE(
    connection.last_completed_at, '-infinity'::timestamptz
  ), connection.id
  LIMIT $2
  FOR UPDATE OF connection SKIP LOCKED
),
leased AS (
  UPDATE datto_connections connection
  SET lease_until = $1 + ($3 * interval '1 microsecond'),
      last_started_at = $1,
      last_error_code = NULL
  FROM candidates
  WHERE connection.id = candidates.id
  RETURNING connection.*
)
SELECT id::text, msp_id::text,
       'db://datto/' || id::text || '/' ||
         configuration_generation::text || '/credential',
       extract(epoch FROM sync_interval)::bigint,
       COALESCE(last_completed_at, 'epoch'::timestamptz),
       COALESCE(cursor_key_version, 0),
       COALESCE(cursor_nonce, ''::bytea),
       COALESCE(cursor_ciphertext, ''::bytea),
       manual_request_id IS NOT NULL,
       COALESCE(manual_request_id::text, '')
FROM leased
`, now, limit, leaseMicroseconds(lease))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]datto.Connection, 0)
	for rows.Next() {
		var (
			connection      datto.Connection
			intervalSeconds int64
			keyVersion      int
			nonce           []byte
			ciphertext      []byte
		)
		if err := rows.Scan(
			&connection.ID, &connection.MSPID,
			&connection.CredentialSecretRef, &intervalSeconds,
			&connection.LastCompletedAt, &keyVersion, &nonce, &ciphertext,
			&connection.Manual, &connection.ManualRequestID,
		); err != nil {
			return nil, err
		}
		connection.SyncInterval = time.Duration(intervalSeconds) * time.Second
		if keyVersion > 0 {
			plaintext, err := r.secrets.Open(
				ctx, dattoCursorPurpose(connection.ID),
				secrets.SealedValue{
					Version: keyVersion, Nonce: nonce, Ciphertext: ciphertext,
				},
			)
			if err != nil {
				return nil, err
			}
			connection.Cursor = string(plaintext)
		}
		result = append(result, connection)
	}
	return result, rows.Err()
}

func (r *DattoRepository) Start(
	ctx context.Context,
	run datto.SyncRun,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	kind := run.Kind
	if run.Manual {
		kind = "manual"
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO datto_sync_runs (
  id, connection_id, kind, state, started_at, manual_request_id
) SELECT $1, $2, $3, 'running', $4, manual_request_id
FROM datto_connections
WHERE id = $2
`, run.ID, run.ConnectionID, kind, run.StartedAt); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE datto_connections
SET current_run_id = $2, last_started_at = $3,
    manual_requested_at = NULL, manual_requested_by = NULL,
    manual_request_id = NULL
WHERE id = $1 AND enabled AND lease_until IS NOT NULL
`, run.ConnectionID, run.ID, run.StartedAt)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return datto.ErrInvalidSync
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *DattoRepository) Apply(
	ctx context.Context,
	connectionID string,
	page datto.SyncPage,
	markMissing bool,
) error {
	observedAt := page.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	for _, remote := range page.Assets {
		macs, err := json.Marshal(remote.MACAddresses)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		snapshotID := r.newID()
		if _, err := tx.Exec(ctx, `
INSERT INTO datto_asset_snapshots (
  id, connection_id, msp_id, client_id, external_asset_id,
  datto_site_id, rarity_asset_id, hostname, serial_number,
  mac_addresses, source_payload_ref, source_updated_at,
  first_seen_at, last_seen_at, state, stale_since
)
SELECT $1, connection.id, connection.msp_id, mapping.client_id,
       $3, $4, exact.id, NULLIF($5, ''), NULLIF($6, ''),
       $7, $8, NULLIF($9, '0001-01-01 00:00:00+00'::timestamptz),
       $10, $10, 'active', NULL
FROM datto_connections connection
LEFT JOIN datto_site_mappings mapping
  ON mapping.connection_id = connection.id
 AND mapping.msp_id = connection.msp_id
 AND mapping.datto_site_id = $4
LEFT JOIN assets exact
  ON exact.msp_id = connection.msp_id
 AND exact.client_id = mapping.client_id
 AND exact.source_system = 'datto'
 AND exact.external_id = $3
WHERE connection.id = $2 AND connection.enabled
ON CONFLICT (connection_id, external_asset_id)
DO UPDATE SET
  client_id = EXCLUDED.client_id,
  datto_site_id = EXCLUDED.datto_site_id,
  rarity_asset_id = COALESCE(
    datto_asset_snapshots.rarity_asset_id, EXCLUDED.rarity_asset_id
  ),
  hostname = EXCLUDED.hostname,
  serial_number = EXCLUDED.serial_number,
  mac_addresses = EXCLUDED.mac_addresses,
  source_payload_ref = EXCLUDED.source_payload_ref,
  source_updated_at = EXCLUDED.source_updated_at,
  last_seen_at = EXCLUDED.last_seen_at,
  state = 'active',
  stale_since = NULL
		`, snapshotID, connectionID, remote.ExternalID, remote.SiteID,
			remote.Hostname, remote.SerialNumber, macs,
			remote.SourcePayloadRef, remote.SourceUpdatedAt, observedAt); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("upsert Datto asset snapshot: %w", err)
		}
		if _, err := syncLockedDattoAsset(
			ctx, tx, connectionID, remote.ExternalID, observedAt,
		); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO audit_ledger (
  id, occurred_at, msp_id, client_id, actor_type, actor_id, action,
  subject_type, subject_id, subject_version, source, correlation_id,
  safe_diff, authorization_context
)
SELECT md5(asset.id::text || ':' || $1::timestamptz::text || ':audit')::uuid,
       $1::timestamptz, asset.msp_id, asset.client_id, 'integration',
       asset.updated_by, 'datto.asset.synced', 'asset', asset.id,
       asset.version, 'datto',
       md5(asset.id::text || ':' || $1::timestamptz::text || ':correlation')::uuid,
       jsonb_build_object(
         'external_id', asset.external_id,
         'lifecycle_state', asset.lifecycle_state
       ),
       jsonb_build_object('connection_id', $2::uuid::text)
FROM assets asset
WHERE asset.updated_at = $1::timestamptz
  AND asset.source_system = 'datto'
  AND asset.external_id = $3
  AND asset.authority = 'discovered'
  AND EXISTS (
    SELECT 1
    FROM datto_asset_snapshots snapshot
    WHERE snapshot.connection_id = $2::uuid
      AND snapshot.external_asset_id = $3
      AND snapshot.rarity_asset_id = asset.id
  )
ON CONFLICT (id) DO NOTHING
		`, observedAt, connectionID, remote.ExternalID); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("write Datto asset sync audit: %w", err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO event_outbox (
  event_id, event_type, schema_version, occurred_at, msp_id, client_id,
  actor_type, actor_id, subject_type, subject_id, subject_version,
  correlation_id, source, data
)
SELECT md5(asset.id::text || ':' || $1::timestamptz::text || ':event')::uuid,
       'datto.asset.synced', 1, $1::timestamptz, asset.msp_id, asset.client_id,
       'integration', asset.updated_by, 'asset', asset.id, asset.version,
       md5(asset.id::text || ':' || $1::timestamptz::text || ':correlation')::uuid,
       'datto',
       jsonb_build_object(
         'external_id', asset.external_id,
         'lifecycle_state', asset.lifecycle_state
       )
FROM assets asset
WHERE asset.updated_at = $1::timestamptz
  AND asset.source_system = 'datto'
  AND asset.external_id = $2
  AND asset.authority = 'discovered'
  AND EXISTS (
    SELECT 1
    FROM datto_asset_snapshots snapshot
    WHERE snapshot.connection_id = $3
      AND snapshot.external_asset_id = $2
      AND snapshot.rarity_asset_id = asset.id
  )
ON CONFLICT (event_id) DO NOTHING
		`, observedAt, remote.ExternalID, connectionID); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("write Datto asset sync event: %w", err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO datto_reconciliation_candidates (
  id, snapshot_id, msp_id, client_id, rarity_asset_id,
  evidence, state, created_at
)
SELECT md5(snapshot.id::text || ':' || asset.id::text)::uuid,
       snapshot.id, snapshot.msp_id, snapshot.client_id, asset.id,
       jsonb_strip_nulls(jsonb_build_object(
         'hostname', CASE
           WHEN lower(asset.name) = lower(COALESCE(snapshot.hostname, ''))
           THEN snapshot.hostname
         END,
         'serial_number', CASE
           WHEN lower(NULLIF(asset.serial_number, '')) =
                lower(snapshot.serial_number)
           THEN snapshot.serial_number
         END,
         'mac_address', (
           SELECT remote_mac
           FROM jsonb_array_elements_text(snapshot.mac_addresses) remote_mac
           WHERE EXISTS (
             SELECT 1
             FROM jsonb_array_elements_text(asset.mac_addresses) local_mac
             WHERE lower(local_mac) = lower(remote_mac)
           )
           LIMIT 1
         )
       )), 'pending', $2
FROM datto_asset_snapshots snapshot
JOIN assets asset
  ON asset.msp_id = snapshot.msp_id
 AND asset.client_id = snapshot.client_id
 AND asset.lifecycle_state = 'active'
WHERE snapshot.connection_id = $1
  AND snapshot.external_asset_id = $3
  AND snapshot.rarity_asset_id IS NULL
  AND (
    lower(asset.name) = lower(COALESCE(snapshot.hostname, ''))
    OR (
      asset.serial_number <> ''
      AND lower(asset.serial_number) = lower(snapshot.serial_number)
    )
    OR EXISTS (
      SELECT 1
      FROM jsonb_array_elements_text(snapshot.mac_addresses) remote_mac
      JOIN LATERAL jsonb_array_elements_text(asset.mac_addresses) local_mac
        ON lower(local_mac) = lower(remote_mac)
    )
  )
ON CONFLICT (snapshot_id, rarity_asset_id)
DO UPDATE SET evidence = EXCLUDED.evidence
WHERE datto_reconciliation_candidates.state = 'pending'
		`, connectionID, observedAt, remote.ExternalID); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("write Datto reconciliation candidate: %w", err)
		}
		assetInsert, err := tx.Exec(ctx, `
INSERT INTO assets (
  id, msp_id, client_id, display_id, name, asset_type,
  lifecycle_state, version, created_at, created_by,
  updated_at, updated_by, source_system, external_id, authority,
  serial_number, mac_addresses
)
SELECT md5(snapshot.connection_id::text || ':' ||
           snapshot.external_asset_id)::uuid,
       snapshot.msp_id, snapshot.client_id,
       'DATTO-' || upper(substr(md5(snapshot.external_asset_id), 1, 12)),
       COALESCE(NULLIF(snapshot.hostname, ''), snapshot.external_asset_id),
       'managed_device', 'active', 1, $3, connection.created_by,
       $3, connection.created_by, 'datto',
       snapshot.external_asset_id, 'discovered',
       COALESCE(snapshot.serial_number, ''), snapshot.mac_addresses
FROM datto_asset_snapshots snapshot
JOIN datto_connections connection
  ON connection.id = snapshot.connection_id
WHERE snapshot.connection_id = $1
  AND snapshot.external_asset_id = $2
  AND snapshot.client_id IS NOT NULL
  AND snapshot.rarity_asset_id IS NULL
  AND NOT EXISTS (
    SELECT 1
    FROM datto_reconciliation_candidates candidate
    WHERE candidate.snapshot_id = snapshot.id
      AND candidate.state = 'pending'
  )
ON CONFLICT DO NOTHING
		`, connectionID, remote.ExternalID, observedAt)
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("materialize Datto asset: %w", err)
		}
		assetCreated := assetInsert.RowsAffected() == 1
		if _, err := tx.Exec(ctx, `
UPDATE datto_asset_snapshots snapshot
SET rarity_asset_id = asset.id
FROM assets asset
WHERE snapshot.connection_id = $1
  AND snapshot.external_asset_id = $2
  AND snapshot.rarity_asset_id IS NULL
  AND asset.msp_id = snapshot.msp_id
  AND asset.client_id = snapshot.client_id
  AND asset.source_system = 'datto'
  AND asset.external_id = snapshot.external_asset_id
		`, connectionID, remote.ExternalID); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("link Datto snapshot to asset: %w", err)
		}
		if assetCreated {
			if err := insertSystemFallbackAssignment(ctx, tx, connectionID, remote.ExternalID); err != nil {
				_ = tx.Rollback(ctx)
				return fmt.Errorf("write Datto fallback assignment: %w", err)
			}
		}
		auditID, eventID, correlationID := r.newID(), r.newID(), r.newID()
		if _, err := tx.Exec(ctx, `
INSERT INTO audit_ledger (
  id, occurred_at, msp_id, client_id, actor_type, actor_id, action,
  subject_type, subject_id, subject_version, source, correlation_id,
  safe_diff, authorization_context
)
SELECT $1, $2, asset.msp_id, asset.client_id,
       'integration', asset.created_by, 'asset.created',
       'asset', asset.id, asset.version, 'datto', $3,
       jsonb_build_object(
         'source_system', 'datto',
         'external_id', asset.external_id,
         'authority', asset.authority
       ),
       jsonb_build_object('connection_id', $4::text)
FROM assets asset
WHERE asset.source_system = 'datto'
  AND asset.external_id = $5
  AND asset.created_at = $2
  AND EXISTS (
    SELECT 1
    FROM datto_asset_snapshots snapshot
    WHERE snapshot.connection_id = $4::uuid
      AND snapshot.external_asset_id = $5
      AND snapshot.rarity_asset_id = asset.id
  )
`, auditID, observedAt, correlationID,
			connectionID, remote.ExternalID); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO event_outbox (
  event_id, event_type, schema_version, occurred_at, msp_id, client_id,
  actor_type, actor_id, subject_type, subject_id, subject_version,
  correlation_id, source, data
)
SELECT $1, 'asset.created', 1, $2, asset.msp_id, asset.client_id,
       'integration', asset.created_by, 'asset', asset.id,
       asset.version, $3, 'datto',
       jsonb_build_object(
         'source_system', 'datto',
         'external_id', asset.external_id,
         'authority', asset.authority
       )
FROM assets asset
WHERE asset.source_system = 'datto'
  AND asset.external_id = $4
  AND asset.created_at = $2
  AND EXISTS (
    SELECT 1
    FROM datto_asset_snapshots snapshot
    WHERE snapshot.connection_id = $5
      AND snapshot.external_asset_id = $4
      AND snapshot.rarity_asset_id = asset.id
  )
`, eventID, observedAt, correlationID,
			remote.ExternalID, connectionID); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
	}
	for _, alert := range page.Alerts {
		if _, err := tx.Exec(ctx, `
INSERT INTO datto_alert_queue (
  id, connection_id, msp_id, client_id, external_alert_id,
  external_device_id, datto_site_id, priority, title, diagnostics,
  fingerprint, alert_state, observed_at, source_payload_ref,
  processing_state, created_at
)
SELECT $1, connection.id, connection.msp_id, mapping.client_id,
       $3, NULLIF($4, ''), $5, $6, $7, $8, $9, $10, $11, $12,
       CASE WHEN mapping.client_id IS NULL
         THEN 'blocked_mapping'
         ELSE 'pending'
       END,
       $11
FROM datto_connections connection
LEFT JOIN datto_site_mappings mapping
  ON mapping.connection_id = connection.id
 AND mapping.msp_id = connection.msp_id
 AND mapping.datto_site_id = $5
WHERE connection.id = $2 AND connection.enabled
ON CONFLICT (
  connection_id, external_alert_id, alert_state, observed_at
) DO NOTHING
`, r.newID(), connectionID, alert.ExternalID,
			alert.ExternalDeviceID, alert.SiteID, alert.Priority,
			alert.Title, alert.Diagnostics, alert.Fingerprint,
			alert.State, alert.ObservedAt, alert.SourcePayloadRef); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
	}
	if markMissing {
		if _, err := tx.Exec(ctx, `
UPDATE datto_asset_snapshots
SET state = 'stale', stale_since = $2
WHERE connection_id = $1
  AND last_seen_at < $2
  AND state = 'active'
`, connectionID, observedAt); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE assets asset
SET lifecycle_state = 'inactive',
    updated_at = $2, updated_by = connection.created_by,
    version = asset.version + 1
FROM datto_asset_snapshots snapshot
JOIN datto_connections connection
  ON connection.id = snapshot.connection_id
WHERE snapshot.connection_id = $1
  AND snapshot.state = 'stale'
  AND snapshot.stale_since = $2
  AND snapshot.rarity_asset_id = asset.id
  AND asset.msp_id = snapshot.msp_id
  AND asset.client_id = snapshot.client_id
  AND asset.source_system = 'datto'
  AND asset.authority = 'discovered'
  AND asset.lifecycle_state = 'active'
`, connectionID, observedAt); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO audit_ledger (
  id, occurred_at, msp_id, client_id, actor_type, actor_id, action,
  subject_type, subject_id, subject_version, source, correlation_id,
  safe_diff, authorization_context
)
SELECT md5(asset.id::text || ':' || $1::timestamptz::text || ':stale:audit')::uuid,
       $1::timestamptz, asset.msp_id, asset.client_id, 'integration',
       asset.updated_by, 'datto.asset.stale', 'asset', asset.id,
       asset.version, 'datto',
       md5(asset.id::text || ':' || $1::timestamptz::text || ':stale:correlation')::uuid,
       jsonb_build_object(
         'external_id', asset.external_id,
         'lifecycle_state', 'inactive'
       ),
       jsonb_build_object('connection_id', $2::uuid::text)
FROM assets asset
WHERE asset.updated_at = $1::timestamptz
  AND asset.source_system = 'datto'
  AND asset.authority = 'discovered'
  AND asset.lifecycle_state = 'inactive'
  AND EXISTS (
    SELECT 1
    FROM datto_asset_snapshots snapshot
    WHERE snapshot.connection_id = $2::uuid
      AND snapshot.rarity_asset_id = asset.id
      AND snapshot.stale_since = $1::timestamptz
  )
ON CONFLICT (id) DO NOTHING
`, observedAt, connectionID); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO event_outbox (
  event_id, event_type, schema_version, occurred_at, msp_id, client_id,
  actor_type, actor_id, subject_type, subject_id, subject_version,
  correlation_id, source, data
)
SELECT md5(asset.id::text || ':' || $1::timestamptz::text || ':stale:event')::uuid,
       'datto.asset.stale', 1, $1::timestamptz, asset.msp_id, asset.client_id,
       'integration', asset.updated_by, 'asset', asset.id, asset.version,
       md5(asset.id::text || ':' || $1::timestamptz::text ||
           ':stale:correlation')::uuid,
       'datto',
       jsonb_build_object(
         'external_id', asset.external_id,
         'lifecycle_state', 'inactive'
       )
FROM assets asset
WHERE asset.updated_at = $1::timestamptz
  AND asset.source_system = 'datto'
  AND asset.authority = 'discovered'
  AND asset.lifecycle_state = 'inactive'
  AND EXISTS (
    SELECT 1
    FROM datto_asset_snapshots snapshot
    WHERE snapshot.connection_id = $2::uuid
      AND snapshot.rarity_asset_id = asset.id
      AND snapshot.stale_since = $1::timestamptz
  )
ON CONFLICT (event_id) DO NOTHING
`, observedAt, connectionID); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

// insertSystemFallbackAssignment is intentionally fixed SQL: Datto discovery
// is trusted automation and every newly materialized asset must be immediately
// classifiable, even before a technician applies a meaningful tag.
func insertSystemFallbackAssignment(ctx context.Context, tx transaction, connectionID, externalID string) error {
	tag, err := tx.Exec(ctx, `
WITH created AS (
  SELECT asset.id, asset.msp_id, asset.client_id, asset.version
  FROM assets asset
  JOIN datto_asset_snapshots snapshot
    ON snapshot.rarity_asset_id = asset.id
   AND snapshot.msp_id = asset.msp_id AND snapshot.client_id = asset.client_id
  WHERE snapshot.connection_id = $1 AND snapshot.external_asset_id = $2
    AND asset.source_system = 'datto' AND asset.authority = 'discovered'
)
INSERT INTO object_tag_assignments (
  id, msp_id, client_id, object_type, object_id, object_version,
  tag_id, assignment_source, assigned_at, assigned_by, evidence, version
)
SELECT md5(created.msp_id::text || ':initial-tag:asset:' || created.id::text || ':' || unclassified.id::text)::uuid,
       created.msp_id, created.client_id, 'asset', created.id, created.version,
       unclassified.id, 'system_fallback', now(), NULL,
       jsonb_build_object('datto_discovery', true), 1
FROM created
JOIN tags unclassified ON unclassified.msp_id = created.msp_id
  AND unclassified.internal_key = 'taxonomy.system.unclassified' AND unclassified.lifecycle_state = 'active'
ON CONFLICT (msp_id, object_type, object_id, tag_id) DO NOTHING
	`, connectionID, externalID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return scope.ErrNotFound
	}
	tag, err = tx.Exec(ctx, `
INSERT INTO tag_assignment_events (
  id, msp_id, client_id, assignment_id, object_type, object_id,
  target_version, tag_id, operation, assignment_source,
  actor_type, actor_id, occurred_at, evidence, idempotency_key, correlation_id
)
SELECT md5('datto-fallback-event:' || assignment.id::text)::uuid,
       assignment.msp_id, assignment.client_id, assignment.id, 'asset', assignment.object_id,
       assignment.object_version, assignment.tag_id, 'added', 'system_fallback',
       'system', NULL, now(), assignment.evidence,
       'datto-fallback-event:' || assignment.id::text,
       md5('datto-fallback-correlation:' || assignment.id::text)::uuid
FROM object_tag_assignments assignment
JOIN datto_asset_snapshots snapshot ON snapshot.rarity_asset_id = assignment.object_id
WHERE snapshot.connection_id = $1 AND snapshot.external_asset_id = $2
  AND assignment.object_type = 'asset' AND assignment.assignment_source = 'system_fallback'
ON CONFLICT (idempotency_key) DO NOTHING
`, connectionID, externalID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return scope.ErrNotFound
	}
	return nil
}

func syncLockedDattoAsset(
	ctx context.Context,
	tx transaction,
	connectionID string,
	externalID string,
	observedAt time.Time,
) (bool, error) {
	var mspID, clientID, assetID string
	err := tx.QueryRow(ctx, `
SELECT snapshot.msp_id::text,
       COALESCE(snapshot.client_id::text, ''),
       COALESCE(snapshot.rarity_asset_id::text, '')
FROM datto_asset_snapshots snapshot
WHERE snapshot.connection_id = $1
  AND snapshot.external_asset_id = $2
`, connectionID, externalID).Scan(&mspID, &clientID, &assetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if clientID == "" || assetID == "" {
		return false, nil
	}
	if err := lockActiveClient(ctx, tx, mspID, clientID); err != nil {
		if errors.Is(err, scope.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	target := scope.Target{MSPID: mspID, ClientID: clientID}
	// Discover the retained Location without locking the Asset, then follow
	// the global Client -> Location -> Asset order and revalidate the snapshot.
	snapshot, err := loadClientResourceSnapshot(
		ctx, tx, clientresources.AssetKind, target, assetID,
	)
	if errors.Is(err, scope.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if snapshot.SourceSystem != "datto" || snapshot.ExternalID != externalID ||
		snapshot.Authority != clientresources.Discovered {
		return false, nil
	}
	if snapshot.LocationID != "" {
		if err := lockActiveLocation(ctx, tx, mspID, clientID, snapshot.LocationID); err != nil {
			if errors.Is(err, scope.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
	}
	current, err := loadLockedClientResource(
		ctx, tx, clientresources.AssetKind, target, assetID,
	)
	if errors.Is(err, scope.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !sameResourceLockSnapshot(snapshot, current) ||
		current.SourceSystem != "datto" || current.ExternalID != externalID ||
		current.Authority != clientresources.Discovered {
		return false, nil
	}
	tag, err := tx.Exec(ctx, `
UPDATE assets asset
SET name = COALESCE(NULLIF(snapshot.hostname, ''), asset.name),
    serial_number = COALESCE(snapshot.serial_number, ''),
    mac_addresses = snapshot.mac_addresses,
    lifecycle_state = 'active',
    updated_at = $6, updated_by = connection.created_by,
    version = asset.version + 1
FROM datto_asset_snapshots snapshot
JOIN datto_connections connection
  ON connection.id = snapshot.connection_id
WHERE snapshot.connection_id = $1
  AND snapshot.external_asset_id = $2
  AND snapshot.rarity_asset_id = asset.id
  AND asset.id = $3::uuid
  AND asset.version = $4
  AND asset.lifecycle_state = $5
  AND asset.msp_id = snapshot.msp_id
  AND asset.client_id = snapshot.client_id
  AND asset.source_system = 'datto'
  AND asset.authority = 'discovered'
  AND (
    asset.name IS DISTINCT FROM
      COALESCE(NULLIF(snapshot.hostname, ''), asset.name)
    OR asset.serial_number IS DISTINCT FROM
      COALESCE(snapshot.serial_number, '')
    OR asset.mac_addresses IS DISTINCT FROM snapshot.mac_addresses
    OR asset.lifecycle_state <> 'active'
  )
`, connectionID, externalID, assetID, current.Version,
		current.LifecycleState, observedAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *DattoRepository) Complete(
	ctx context.Context,
	completion datto.SyncCompletion,
) error {
	if r.secrets == nil || strings.TrimSpace(completion.NextCursor) == "" {
		return datto.ErrInvalidSync
	}
	sealed, err := r.secrets.Seal(
		ctx, dattoCursorPurpose(completion.ConnectionID),
		[]byte(completion.NextCursor),
	)
	if err != nil {
		return err
	}
	rateLimit, err := json.Marshal(completion.RateLimit)
	if err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE datto_sync_runs
SET state = 'succeeded', completed_at = $3,
    assets_seen = $4, assets_changed = $5,
    rate_limit_state = $6, error_code = NULL
WHERE id = $1 AND connection_id = $2 AND state = 'running'
`, completion.RunID, completion.ConnectionID, completion.CompletedAt,
		completion.AssetsSeen, completion.AssetsChanged, rateLimit)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return datto.ErrInvalidSync
	}
	tag, err = tx.Exec(ctx, `
UPDATE datto_connections
SET cursor_secret_ref = COALESCE(
      cursor_secret_ref, 'local://datto-cursor/' || id::text
    ),
    cursor_key_version = $4, cursor_nonce = $5,
    cursor_ciphertext = $6, last_completed_at = $3,
    health_state = 'healthy', last_error_code = NULL,
    lease_until = NULL, current_run_id = NULL,
    updated_at = $3, version = version + 1
WHERE id = $1 AND current_run_id = $2
`, completion.ConnectionID, completion.RunID, completion.CompletedAt,
		sealed.Version, sealed.Nonce, sealed.Ciphertext)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return datto.ErrInvalidSync
	}
	if err := r.writeCompletionFacts(
		ctx, tx, completion, rateLimit,
	); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *DattoRepository) writeCompletionFacts(
	ctx context.Context,
	tx transaction,
	completion datto.SyncCompletion,
	rateLimit []byte,
) error {
	auditID, eventID, correlationID := r.newID(), r.newID(), r.newID()
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_ledger (
  id, occurred_at, msp_id, actor_type, actor_id, action,
  subject_type, subject_id, subject_version, source, correlation_id,
  safe_diff, authorization_context
)
SELECT $1, $2, connection.msp_id, 'integration', connection.id,
       'datto.sync.completed', 'datto_connection', connection.id,
       connection.version, 'datto', $3,
       jsonb_build_object(
         'run_id', $4::text,
         'assets_seen', $5::integer,
         'assets_changed', $6::integer,
         'rate_limit', $7::jsonb
       ),
       jsonb_build_object('connection_id', connection.id)
FROM datto_connections connection
WHERE connection.id = $8
`, auditID, completion.CompletedAt, correlationID, completion.RunID,
		completion.AssetsSeen, completion.AssetsChanged, rateLimit,
		completion.ConnectionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO event_outbox (
  event_id, event_type, schema_version, occurred_at, msp_id,
  actor_type, actor_id, subject_type, subject_id, subject_version,
  correlation_id, source, data
)
SELECT $1, 'datto.sync.completed', 1, $2, connection.msp_id,
       'integration', connection.id, 'datto_connection', connection.id,
       connection.version, $3, 'datto',
       jsonb_build_object(
         'run_id', $4::text,
         'assets_seen', $5::integer,
         'assets_changed', $6::integer
       )
FROM datto_connections connection
WHERE connection.id = $7
`, eventID, completion.CompletedAt, correlationID, completion.RunID,
		completion.AssetsSeen, completion.AssetsChanged,
		completion.ConnectionID); err != nil {
		return err
	}
	return nil
}

func (r *DattoRepository) Fail(
	ctx context.Context,
	failure datto.SyncFailure,
) error {
	rateLimit, err := json.Marshal(failure.RateLimit)
	if err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE datto_sync_runs
SET state = 'failed', completed_at = $2,
    error_code = $3, rate_limit_state = $4
WHERE id = $1 AND state = 'running'
`, failure.RunID, failure.FailedAt, failure.ErrorCode, rateLimit); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE datto_connections connection
SET health_state = 'degraded', last_error_code = $3,
    lease_until = NULL, current_run_id = NULL,
    updated_at = $2, version = version + 1
FROM datto_sync_runs run
WHERE run.id = $1 AND connection.id = run.connection_id
`, failure.RunID, failure.FailedAt, failure.ErrorCode); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *DattoRepository) ReleaseClaim(
	ctx context.Context,
	connectionID string,
	at time.Time,
	errorCode string,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE datto_connections
SET health_state = 'degraded', last_error_code = $3,
    lease_until = NULL, current_run_id = NULL,
    updated_at = $2, version = version + 1
WHERE id = $1 AND lease_until IS NOT NULL
`, connectionID, at, errorCode)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return datto.ErrInvalidSync
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *DattoRepository) ClaimAlerts(
	ctx context.Context,
	limit int,
	now time.Time,
	lease time.Duration,
) ([]datto.AlertQueueItem, error) {
	rows, err := r.db.Query(ctx, `
WITH candidates AS (
  SELECT candidate.id
  FROM datto_alert_queue candidate
  WHERE candidate.client_id IS NOT NULL
    AND (
      candidate.processing_state = 'pending'
      OR (
        candidate.processing_state = 'processing'
        AND candidate.lease_until <= $1
      )
    )
    AND NOT EXISTS (
      SELECT 1
      FROM datto_alert_queue earlier
      WHERE earlier.connection_id = candidate.connection_id
        AND earlier.fingerprint = candidate.fingerprint
        AND earlier.id <> candidate.id
        AND (
          earlier.processing_state = 'processing'
          OR (
            earlier.processing_state = 'pending'
            AND (earlier.observed_at, earlier.id) <
                (candidate.observed_at, candidate.id)
          )
        )
    )
  ORDER BY candidate.observed_at, candidate.id
  LIMIT $2
  FOR UPDATE OF candidate SKIP LOCKED
),
leased AS (
  UPDATE datto_alert_queue queued
  SET processing_state = 'processing',
      attempt_count = attempt_count + 1,
      lease_until = $1 + ($3 * interval '1 microsecond'),
      last_error_code = NULL
  FROM candidates
  WHERE queued.id = candidates.id
  RETURNING queued.*
)
SELECT queued.id::text, queued.connection_id::text,
       queued.msp_id::text, queued.client_id::text,
       connection.created_by::text,
       queued.external_alert_id,
       COALESCE(queued.external_device_id, ''),
       queued.datto_site_id, queued.priority, queued.title,
       queued.diagnostics, queued.fingerprint, queued.alert_state,
       queued.observed_at, queued.source_payload_ref
FROM leased queued
JOIN datto_connections connection ON connection.id = queued.connection_id
ORDER BY queued.observed_at, queued.id
`, now, limit, leaseMicroseconds(lease))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]datto.AlertQueueItem, 0)
	for rows.Next() {
		var item datto.AlertQueueItem
		if err := rows.Scan(
			&item.ID, &item.ConnectionID, &item.MSPID,
			&item.ClientID, &item.ActorID,
			&item.Observation.ExternalID,
			&item.Observation.ExternalDeviceID,
			&item.Observation.SiteID, &item.Observation.Priority,
			&item.Observation.Title, &item.Observation.Diagnostics,
			&item.Observation.Fingerprint, &item.Observation.State,
			&item.Observation.ObservedAt,
			&item.Observation.SourcePayloadRef,
		); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *DattoRepository) ResolveQueuedAlert(
	ctx context.Context,
	item datto.AlertQueueItem,
	window time.Duration,
) (datto.AlertResolution, error) {
	if window <= 0 {
		window = 24 * time.Hour
	}
	var (
		action     datto.AlertAction
		incidentID string
	)
	err := r.db.QueryRow(ctx, `
WITH exact_any AS (
  SELECT linked.incident_id
  FROM datto_alert_incidents linked
  JOIN work_records incident
    ON incident.id = linked.incident_id
   AND incident.msp_id = linked.msp_id
   AND incident.client_id = linked.client_id
   AND incident.lifecycle_state = 'active'
   AND incident.deleted_at IS NULL
  WHERE linked.connection_id = $1
    AND linked.external_alert_id = $2
  LIMIT 1
),
exact_open AS (
  SELECT exact_any.incident_id
  FROM exact_any
  JOIN work_record_slas timer
    ON timer.work_record_id = exact_any.incident_id
   AND timer.resolved_at IS NULL
  LIMIT 1
),
similar AS (
  SELECT linked.incident_id
  FROM datto_alert_incidents linked
  JOIN work_records incident
    ON incident.id = linked.incident_id
   AND incident.msp_id = linked.msp_id
   AND incident.client_id = linked.client_id
   AND incident.lifecycle_state = 'active'
   AND incident.deleted_at IS NULL
  JOIN work_record_slas timer
    ON timer.work_record_id = incident.id
   AND timer.msp_id = incident.msp_id
   AND timer.client_id = incident.client_id
   AND timer.resolved_at IS NULL
  WHERE linked.connection_id = $1
    AND linked.fingerprint = $3
    AND linked.last_observed_at >= $4
  ORDER BY linked.last_observed_at DESC, linked.id
  LIMIT 1
)
SELECT CASE
         WHEN EXISTS (SELECT 1 FROM exact_any) AND $5 = 'cleared'
           THEN 'append_recovery'
         WHEN EXISTS (SELECT 1 FROM exact_open)
           THEN 'update_incident'
         WHEN $5 = 'cleared'
           THEN 'none'
         WHEN EXISTS (SELECT 1 FROM similar)
           THEN 'update_incident'
         ELSE 'create_incident'
       END,
       COALESCE(
         CASE WHEN $5 = 'cleared'
           THEN (SELECT incident_id::text FROM exact_any)
           ELSE (SELECT incident_id::text FROM exact_open)
         END,
         (SELECT incident_id::text FROM similar),
         CASE WHEN $5 = 'active' THEN $6 ELSE '' END
       )
`, item.ConnectionID, item.Observation.ExternalID,
		item.Observation.Fingerprint,
		item.Observation.ObservedAt.Add(-window),
		item.Observation.State, item.ID).Scan(&action, &incidentID)
	if err != nil {
		return datto.AlertResolution{}, err
	}
	return datto.AlertResolution{
		Action: action, IncidentID: incidentID, ResolveIncident: false,
	}, nil
}

func (r *DattoRepository) CompleteAlert(
	ctx context.Context,
	item datto.AlertQueueItem,
	resolution datto.AlertResolution,
	completedAt time.Time,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if resolution.Action != datto.AlertNoAction {
		if _, err := tx.Exec(ctx, `
INSERT INTO datto_alert_incidents (
  id, connection_id, msp_id, client_id, external_alert_id,
  fingerprint, incident_id, alert_state,
  first_observed_at, last_observed_at
)
VALUES (
  md5($1 || ':' || $2)::uuid, $1, $3, $4, $2,
  $5, $6, $7, $8, $8
)
ON CONFLICT (connection_id, external_alert_id)
DO UPDATE SET
  fingerprint = EXCLUDED.fingerprint,
  incident_id = EXCLUDED.incident_id,
  alert_state = EXCLUDED.alert_state,
  last_observed_at = GREATEST(
    datto_alert_incidents.last_observed_at,
    EXCLUDED.last_observed_at
  )
`, item.ConnectionID, item.Observation.ExternalID,
			item.MSPID, item.ClientID, item.Observation.Fingerprint,
			resolution.IncidentID, item.Observation.State,
			item.Observation.ObservedAt); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO datto_alert_observations (
  id, datto_alert_incident_id, external_observation_id,
  observed_at, state, recovery, source_payload_ref
)
SELECT $1, linked.id, $1, $2, $3, $4, $5
FROM datto_alert_incidents linked
WHERE linked.connection_id = $6
  AND linked.external_alert_id = $7
ON CONFLICT (
  datto_alert_incident_id, external_observation_id
) DO NOTHING
`, item.ID, item.Observation.ObservedAt, item.Observation.State,
			resolution.Action == datto.AlertAppendRecovery,
			item.Observation.SourcePayloadRef, item.ConnectionID,
			item.Observation.ExternalID); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		action := "datto.alert.observed"
		eventType := "datto.alert.observed"
		if resolution.Action == datto.AlertAppendRecovery {
			action = "datto.alert.recovered"
			eventType = "datto.alert.recovered"
		}
		auditID, eventID, correlationID := r.newID(), r.newID(), r.newID()
		if _, err := tx.Exec(ctx, `
INSERT INTO audit_ledger (
  id, occurred_at, msp_id, client_id, actor_type, actor_id, action,
  subject_type, subject_id, subject_version, source, correlation_id,
  safe_diff, authorization_context
)
SELECT $1, $2, incident.msp_id, incident.client_id,
       'integration', $3, $4, 'work_record', incident.id,
       incident.version, 'datto', $5,
       jsonb_build_object(
         'external_alert_id', $6::text,
         'alert_state', $7::text,
         'recovery', $8::boolean
       ),
       jsonb_build_object('connection_id', $9::text)
FROM work_records incident
WHERE incident.id = $10 AND incident.msp_id = $11
  AND incident.client_id = $12
`, auditID, completedAt, item.ActorID, action, correlationID,
			item.Observation.ExternalID, item.Observation.State,
			resolution.Action == datto.AlertAppendRecovery,
			item.ConnectionID, resolution.IncidentID,
			item.MSPID, item.ClientID); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO event_outbox (
  event_id, event_type, schema_version, occurred_at, msp_id, client_id,
  actor_type, actor_id, subject_type, subject_id, subject_version,
  correlation_id, source, data
)
SELECT $1, $2, 1, $3, incident.msp_id, incident.client_id,
       'integration', $4, 'work_record', incident.id,
       incident.version, $5, 'datto',
       jsonb_build_object(
         'external_alert_id', $6::text,
         'alert_state', $7::text,
         'recovery', $8::boolean
       )
FROM work_records incident
WHERE incident.id = $9 AND incident.msp_id = $10
  AND incident.client_id = $11
`, eventID, eventType, completedAt, item.ActorID, correlationID,
			item.Observation.ExternalID, item.Observation.State,
			resolution.Action == datto.AlertAppendRecovery,
			resolution.IncidentID, item.MSPID, item.ClientID); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
	}
	tag, err := tx.Exec(ctx, `
UPDATE datto_alert_queue
SET processing_state = 'processed', processed_at = $2,
    lease_until = NULL, last_error_code = NULL
WHERE id = $1 AND processing_state = 'processing'
`, item.ID, completedAt)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return datto.ErrInvalidSync
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *DattoRepository) ReleaseAlert(
	ctx context.Context,
	item datto.AlertQueueItem,
	at time.Time,
	errorCode string,
) error {
	_ = at
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE datto_alert_queue
SET processing_state = CASE
      WHEN attempt_count >= 10 THEN 'failed'
      ELSE 'pending'
    END,
    lease_until = NULL, last_error_code = $2
WHERE id = $1 AND processing_state = 'processing'
`, item.ID, errorCode)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return datto.ErrInvalidSync
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *DattoRepository) QueueManualSync(
	ctx context.Context,
	request datto.ManualSyncRequest,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE datto_connections
SET manual_requested_at = $4, manual_requested_by = $5,
    manual_request_id = $1, lease_until = NULL,
    last_error_code = NULL, updated_at = $4, version = version + 1
WHERE id = $2 AND msp_id = $3 AND enabled
  AND manual_request_id IS NULL
`, request.ID, request.ConnectionID, request.MSPID,
		request.RequestedAt, request.RequestedBy)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return scope.ErrNotFound
	}
	auditID, eventID, correlationID := r.newID(), r.newID(), r.newID()
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_ledger (
  id, occurred_at, msp_id, actor_type, actor_id, action,
  subject_type, subject_id, subject_version, source, correlation_id,
  safe_diff, authorization_context
)
SELECT $1, $2, connection.msp_id, 'technician', $3,
       'datto.sync.requested', 'datto_connection', connection.id,
       connection.version, 'api', $4,
       jsonb_build_object('manual_request_id', $5::text),
       jsonb_build_object('capability', 'integration.manage')
FROM datto_connections connection
WHERE connection.id = $6 AND connection.msp_id = $7
`, auditID, request.RequestedAt, request.RequestedBy,
		correlationID, request.ID, request.ConnectionID, request.MSPID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO event_outbox (
  event_id, event_type, schema_version, occurred_at, msp_id,
  actor_type, actor_id, subject_type, subject_id, subject_version,
  correlation_id, source, data
)
SELECT $1, 'datto.sync.requested', 1, $2, connection.msp_id,
       'technician', $3, 'datto_connection', connection.id,
       connection.version, $4, 'api',
       jsonb_build_object('manual_request_id', $5::text)
FROM datto_connections connection
WHERE connection.id = $6 AND connection.msp_id = $7
`, eventID, request.RequestedAt, request.RequestedBy,
		correlationID, request.ID, request.ConnectionID, request.MSPID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *DattoRepository) MapSite(
	ctx context.Context,
	mapping datto.SiteMapping,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO datto_site_mappings (
  id, connection_id, msp_id, datto_site_id, client_id,
  mapped_at, mapped_by, version
)
SELECT $1, connection.id, connection.msp_id, $4, client.id,
       $6, $7, 1
FROM datto_connections connection
JOIN client_organizations client
  ON client.id = $5 AND client.msp_id = connection.msp_id
 AND client.lifecycle_state = 'active'
WHERE connection.id = $2 AND connection.msp_id = $3
  AND connection.enabled
ON CONFLICT (connection_id, datto_site_id)
DO UPDATE SET
  client_id = EXCLUDED.client_id,
  mapped_at = EXCLUDED.mapped_at,
  mapped_by = EXCLUDED.mapped_by,
  version = datto_site_mappings.version + 1
`, mapping.ID, mapping.ConnectionID, mapping.MSPID,
		mapping.SiteID, mapping.ClientID, mapping.MappedAt,
		mapping.MappedBy)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return scope.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
UPDATE datto_alert_queue
SET client_id = $4, processing_state = 'pending',
    last_error_code = NULL
WHERE connection_id = $1 AND msp_id = $2
  AND datto_site_id = $3
  AND processing_state = 'blocked_mapping'
`, mapping.ConnectionID, mapping.MSPID, mapping.SiteID,
		mapping.ClientID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	auditID, eventID, correlationID := r.newID(), r.newID(), r.newID()
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_ledger (
  id, occurred_at, msp_id, client_id, actor_type, actor_id, action,
  subject_type, subject_id, subject_version, source, reason,
  correlation_id, safe_diff, authorization_context
)
SELECT $1, $2, connection.msp_id, $3, 'technician', $4,
       'datto.site.mapped', 'datto_connection', connection.id,
       connection.version, 'api', $5, $6,
       jsonb_build_object(
         'datto_site_id', $7::text,
         'client_id', $3::text
       ),
       jsonb_build_object('capability', 'integration.manage')
FROM datto_connections connection
WHERE connection.id = $8 AND connection.msp_id = $9
`, auditID, mapping.MappedAt, mapping.ClientID, mapping.MappedBy,
		mapping.Reason, correlationID, mapping.SiteID,
		mapping.ConnectionID, mapping.MSPID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO event_outbox (
  event_id, event_type, schema_version, occurred_at, msp_id, client_id,
  actor_type, actor_id, subject_type, subject_id, subject_version,
  correlation_id, source, data
)
SELECT $1, 'datto.site.mapped', 1, $2, connection.msp_id, $3,
       'technician', $4, 'datto_connection', connection.id,
       connection.version, $5, 'api',
       jsonb_build_object(
         'datto_site_id', $6::text,
         'client_id', $3::text
       )
FROM datto_connections connection
WHERE connection.id = $7 AND connection.msp_id = $8
`, eventID, mapping.MappedAt, mapping.ClientID, mapping.MappedBy,
		correlationID, mapping.SiteID, mapping.ConnectionID,
		mapping.MSPID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *DattoRepository) Progress(
	ctx context.Context,
	target scope.Target,
	connectionID string,
) (datto.SyncProgress, error) {
	var (
		result    datto.SyncProgress
		rateLimit []byte
	)
	err := r.db.QueryRow(ctx, `
SELECT connection.id::text,
       COALESCE(run.state, CASE
         WHEN connection.manual_request_id IS NOT NULL THEN 'queued'
         ELSE 'idle'
       END),
       COALESCE(run.id::text, ''), COALESCE(run.kind, ''),
       run.started_at, run.completed_at,
       COALESCE(run.assets_seen, 0), COALESCE(run.assets_changed, 0),
       COALESCE(run.rate_limit_state, '{}'::jsonb),
       COALESCE(run.error_code, connection.last_error_code, ''),
       connection.health_state,
       (
         SELECT count(*)::integer
         FROM datto_reconciliation_candidates candidate
         JOIN datto_asset_snapshots snapshot
           ON snapshot.id = candidate.snapshot_id
         WHERE snapshot.connection_id = connection.id
           AND candidate.state = 'pending'
       ),
       (
         SELECT count(*)::integer
         FROM datto_alert_queue alert
         WHERE alert.connection_id = connection.id
           AND alert.processing_state IN ('pending', 'processing')
       ),
       (
         SELECT count(*)::integer
         FROM datto_alert_queue alert
         WHERE alert.connection_id = connection.id
           AND alert.processing_state = 'blocked_mapping'
       )
FROM datto_connections connection
LEFT JOIN datto_sync_runs run
  ON run.id = COALESCE(
    connection.current_run_id,
    (
      SELECT history.id
      FROM datto_sync_runs history
      WHERE history.connection_id = connection.id
      ORDER BY history.started_at DESC, history.id DESC
      LIMIT 1
    )
  )
WHERE connection.id = $1 AND connection.msp_id = $2
`, connectionID, target.MSPID).Scan(
		&result.ConnectionID, &result.State, &result.RunID, &result.Kind,
		&result.StartedAt, &result.CompletedAt, &result.AssetsSeen,
		&result.AssetsChanged, &rateLimit, &result.LastErrorCode,
		&result.HealthState, &result.PendingCandidates,
		&result.PendingAlerts, &result.BlockedAlerts,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return datto.SyncProgress{}, scope.ErrNotFound
	}
	if err != nil {
		return datto.SyncProgress{}, err
	}
	if err := json.Unmarshal(rateLimit, &result.RateLimit); err != nil {
		return datto.SyncProgress{}, err
	}
	return result, nil
}

func (r *DattoRepository) ListCandidates(
	ctx context.Context,
	target scope.Target,
	limit int,
) ([]datto.ReconciliationCandidate, error) {
	rows, err := r.db.Query(ctx, `
SELECT candidate.id::text, candidate.snapshot_id::text,
       candidate.msp_id::text, candidate.client_id::text,
       candidate.rarity_asset_id::text, candidate.evidence,
       candidate.state
FROM datto_reconciliation_candidates candidate
WHERE candidate.msp_id = $1
  AND (
    NULLIF($2::text, '') IS NULL
    OR candidate.client_id = NULLIF($2::text, '')::uuid
  )
  AND candidate.state = 'pending'
ORDER BY candidate.created_at, candidate.id
LIMIT $3
`, target.MSPID, target.ClientID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]datto.ReconciliationCandidate, 0)
	for rows.Next() {
		var (
			candidate datto.ReconciliationCandidate
			evidence  []byte
		)
		if err := rows.Scan(
			&candidate.ID, &candidate.SnapshotID, &candidate.MSPID,
			&candidate.ClientID, &candidate.AssetID, &evidence,
			&candidate.State,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(evidence, &candidate.Evidence); err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, rows.Err()
}

func (r *DattoRepository) FindCandidate(
	ctx context.Context,
	mspID string,
	candidateID string,
) (datto.ReconciliationCandidate, error) {
	var (
		candidate datto.ReconciliationCandidate
		evidence  []byte
	)
	err := r.db.QueryRow(ctx, `
SELECT candidate.id::text, candidate.snapshot_id::text,
       candidate.msp_id::text, candidate.client_id::text,
       candidate.rarity_asset_id::text, candidate.evidence,
       candidate.state
FROM datto_reconciliation_candidates candidate
WHERE candidate.id = $1 AND candidate.msp_id = $2
  AND candidate.state = 'pending'
`, candidateID, mspID).Scan(
		&candidate.ID, &candidate.SnapshotID, &candidate.MSPID,
		&candidate.ClientID, &candidate.AssetID, &evidence,
		&candidate.State,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return datto.ReconciliationCandidate{}, scope.ErrNotFound
	}
	if err != nil {
		return datto.ReconciliationCandidate{}, err
	}
	if err := json.Unmarshal(evidence, &candidate.Evidence); err != nil {
		return datto.ReconciliationCandidate{}, err
	}
	return candidate, nil
}

func (r *DattoRepository) DecideCandidate(
	ctx context.Context,
	decision datto.CandidateDecision,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO datto_reconciliation_decisions (
  id, snapshot_id, msp_id, client_id, rarity_asset_id,
  decision, reason, decided_at, decided_by
)
SELECT $1, candidate.snapshot_id, candidate.msp_id,
       candidate.client_id, candidate.rarity_asset_id,
       $7, $8, $9, $10
FROM datto_reconciliation_candidates candidate
WHERE candidate.id = $2 AND candidate.snapshot_id = $3
  AND candidate.msp_id = $4 AND candidate.client_id = $5
  AND candidate.rarity_asset_id = $6
  AND candidate.state = 'pending'
`, decision.ID, decision.CandidateID, decision.SnapshotID,
		decision.MSPID, decision.ClientID, decision.AssetID,
		decision.Decision, decision.Reason, decision.DecidedAt,
		decision.DecidedBy)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return scope.ErrNotFound
	}
	tag, err = tx.Exec(ctx, `
UPDATE datto_reconciliation_candidates
SET state = 'decided', decided_at = $2
WHERE id = $1 AND state = 'pending'
`, decision.CandidateID, decision.DecidedAt)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return scope.ErrNotFound
	}
	if decision.Decision != datto.DecisionKeepSeparate {
		tag, err = tx.Exec(ctx, `
UPDATE assets asset
SET source_system = 'datto',
    external_id = snapshot.external_asset_id,
    name = CASE WHEN $5 = 'choose_datto'
      THEN COALESCE(NULLIF(snapshot.hostname, ''), asset.name)
      ELSE asset.name
    END,
    serial_number = CASE WHEN $5 = 'choose_datto'
      THEN COALESCE(snapshot.serial_number, '')
      ELSE asset.serial_number
    END,
    mac_addresses = CASE WHEN $5 = 'choose_datto'
      THEN snapshot.mac_addresses
      ELSE asset.mac_addresses
    END,
    updated_at = $6, updated_by = $7, version = version + 1
FROM datto_asset_snapshots snapshot
WHERE asset.id = $1 AND asset.msp_id = $2 AND asset.client_id = $3
  AND snapshot.id = $4 AND snapshot.msp_id = asset.msp_id
  AND snapshot.client_id = asset.client_id
`, decision.AssetID, decision.MSPID, decision.ClientID,
			decision.SnapshotID, decision.Decision,
			decision.DecidedAt, decision.DecidedBy)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if tag.RowsAffected() != 1 {
			_ = tx.Rollback(ctx)
			return scope.ErrNotFound
		}
		tag, err = tx.Exec(ctx, `
UPDATE datto_asset_snapshots
SET rarity_asset_id = $2
WHERE id = $1 AND msp_id = $3 AND client_id = $4
`, decision.SnapshotID, decision.AssetID,
			decision.MSPID, decision.ClientID)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if tag.RowsAffected() != 1 {
			_ = tx.Rollback(ctx)
			return scope.ErrNotFound
		}
	}
	if err := r.writeDecisionFacts(ctx, tx, decision); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *DattoRepository) writeDecisionFacts(
	ctx context.Context,
	tx transaction,
	decision datto.CandidateDecision,
) error {
	auditID, eventID, correlationID := r.newID(), r.newID(), r.newID()
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_ledger (
  id, occurred_at, msp_id, client_id, actor_type, actor_id, action,
  subject_type, subject_id, subject_version, source, reason,
  correlation_id, safe_diff, authorization_context
)
SELECT $1, $2, asset.msp_id, asset.client_id, 'technician', $3,
       'datto.asset.reconciled', 'asset', asset.id, asset.version,
       'api', $4, $5,
       jsonb_build_object(
         'candidate_id', $6::text,
         'decision', $7::text
       ),
       jsonb_build_object('capability', 'integration.datto.reconcile')
FROM assets asset
WHERE asset.id = $8 AND asset.msp_id = $9 AND asset.client_id = $10
`, auditID, decision.DecidedAt, decision.DecidedBy, decision.Reason,
		correlationID, decision.CandidateID, decision.Decision,
		decision.AssetID, decision.MSPID, decision.ClientID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO event_outbox (
  event_id, event_type, schema_version, occurred_at, msp_id, client_id,
  actor_type, actor_id, subject_type, subject_id, subject_version,
  correlation_id, source, data
)
SELECT $1, 'datto.asset.reconciled', 1, $2,
       asset.msp_id, asset.client_id, 'technician', $3,
       'asset', asset.id, asset.version, $4, 'api',
       jsonb_build_object(
         'candidate_id', $5::text,
         'decision', $6::text
       )
FROM assets asset
WHERE asset.id = $7 AND asset.msp_id = $8 AND asset.client_id = $9
`, eventID, decision.DecidedAt, decision.DecidedBy, correlationID,
		decision.CandidateID, decision.Decision, decision.AssetID,
		decision.MSPID, decision.ClientID); err != nil {
		return err
	}
	return nil
}

func dattoCursorPurpose(connectionID string) string {
	return dattoCursorPurposePrefix + connectionID
}

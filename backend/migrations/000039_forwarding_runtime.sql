-- +goose Up
ALTER TABLE inbound_events
    DROP CONSTRAINT inbound_events_webhook_connection_required,
    ADD COLUMN forwarding_connection_id uuid,
    ADD CONSTRAINT inbound_events_forwarding_connection_id_msp_id_fkey
        FOREIGN KEY (forwarding_connection_id, msp_id)
        REFERENCES forwarding_intake_connections(id, msp_id),
    ADD CONSTRAINT inbound_events_source_connection_required
        CHECK (
            (
                source = 'inbound_webhook'
                AND connection_id IS NOT NULL
                AND forwarding_connection_id IS NULL
            )
            OR (
                source = 'forwarded_email'
                AND forwarding_connection_id IS NOT NULL
                AND connection_id IS NULL
            )
            OR (
                source NOT IN ('inbound_webhook', 'forwarded_email')
                AND connection_id IS NULL
                AND forwarding_connection_id IS NULL
            )
        );

DROP INDEX inbound_events_global_external_id_idx;

CREATE UNIQUE INDEX inbound_events_global_external_id_idx
    ON inbound_events (msp_id, source, external_id)
    WHERE client_id IS NULL
      AND source NOT IN ('inbound_webhook', 'forwarded_email');

CREATE UNIQUE INDEX inbound_events_forwarding_external_id_idx
    ON inbound_events (forwarding_connection_id, external_id)
    WHERE source = 'forwarded_email';

CREATE TABLE forwarding_rate_limit_windows (
  connection_id uuid NOT NULL REFERENCES forwarding_intake_connections(id),
  sender_key text NOT NULL,
  window_started_at timestamptz NOT NULL,
  message_count integer NOT NULL CHECK (message_count > 0),
  PRIMARY KEY (connection_id, sender_key, window_started_at)
);

CREATE INDEX forwarding_rate_limit_windows_expiry_idx
  ON forwarding_rate_limit_windows (window_started_at);

-- +goose Down
DROP TABLE forwarding_rate_limit_windows;
DROP INDEX inbound_events_forwarding_external_id_idx;
DROP INDEX inbound_events_global_external_id_idx;

CREATE UNIQUE INDEX inbound_events_global_external_id_idx
    ON inbound_events (msp_id, source, external_id)
    WHERE client_id IS NULL;

ALTER TABLE inbound_events
    DROP CONSTRAINT inbound_events_source_connection_required,
    DROP CONSTRAINT inbound_events_forwarding_connection_id_msp_id_fkey,
    DROP COLUMN forwarding_connection_id,
    ADD CONSTRAINT inbound_events_webhook_connection_required
        CHECK (
            (source = 'inbound_webhook' AND connection_id IS NOT NULL)
            OR (source <> 'inbound_webhook' AND connection_id IS NULL)
        );

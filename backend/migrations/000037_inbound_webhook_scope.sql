-- +goose Up
ALTER TABLE inbound_events
    ADD COLUMN connection_id uuid,
    ADD CONSTRAINT inbound_events_connection_id_msp_id_fkey
        FOREIGN KEY (connection_id, msp_id)
        REFERENCES webhook_connections(id, msp_id),
    ADD CONSTRAINT inbound_events_webhook_connection_required
        CHECK (
            (source = 'inbound_webhook' AND connection_id IS NOT NULL)
            OR (source <> 'inbound_webhook' AND connection_id IS NULL)
        );

DROP INDEX inbound_events_client_external_id_idx;

CREATE UNIQUE INDEX inbound_events_client_external_id_idx
    ON inbound_events (msp_id, client_id, source, external_id)
    WHERE client_id IS NOT NULL AND source <> 'inbound_webhook';

CREATE UNIQUE INDEX inbound_events_webhook_external_id_idx
    ON inbound_events (connection_id, external_id)
    WHERE source = 'inbound_webhook';

-- +goose Down
DROP INDEX inbound_events_webhook_external_id_idx;
DROP INDEX inbound_events_client_external_id_idx;

CREATE UNIQUE INDEX inbound_events_client_external_id_idx
    ON inbound_events (msp_id, client_id, source, external_id)
    WHERE client_id IS NOT NULL;

ALTER TABLE inbound_events
    DROP CONSTRAINT inbound_events_webhook_connection_required,
    DROP CONSTRAINT inbound_events_connection_id_msp_id_fkey,
    DROP COLUMN connection_id;

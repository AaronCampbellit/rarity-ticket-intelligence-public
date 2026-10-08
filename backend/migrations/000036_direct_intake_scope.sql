-- +goose Up
ALTER TABLE inbound_events
    DROP CONSTRAINT inbound_events_msp_id_source_external_id_key;

CREATE UNIQUE INDEX inbound_events_client_external_id_idx
    ON inbound_events (msp_id, client_id, source, external_id)
    WHERE client_id IS NOT NULL;

CREATE UNIQUE INDEX inbound_events_global_external_id_idx
    ON inbound_events (msp_id, source, external_id)
    WHERE client_id IS NULL;

-- +goose Down
DROP INDEX inbound_events_global_external_id_idx;
DROP INDEX inbound_events_client_external_id_idx;

ALTER TABLE inbound_events
    ADD CONSTRAINT inbound_events_msp_id_source_external_id_key
    UNIQUE (msp_id, source, external_id);

-- +goose Up
ALTER TABLE notification_deliveries
  ADD CONSTRAINT notification_deliveries_id_msp_calendar_key_unique
    UNIQUE (id,msp_id,calendar_delivery_key);

ALTER TABLE notification_calendar_delivery_payloads
  ADD COLUMN calendar_delivery_key text;

UPDATE notification_calendar_delivery_payloads payload
SET calendar_delivery_key=delivery.calendar_delivery_key
FROM notification_deliveries delivery
WHERE delivery.id=payload.delivery_id
  AND delivery.msp_id=payload.msp_id;

UPDATE notification_deliveries delivery
SET state='failed',failure_code='invalid_calendar_delivery_key'
WHERE delivery.calendar_delivery_key IS NULL
  AND delivery.state='pending'
  AND EXISTS (
    SELECT 1
    FROM notification_calendar_delivery_payloads payload
    WHERE payload.delivery_id=delivery.id
      AND payload.msp_id=delivery.msp_id
  );

DELETE FROM notification_calendar_delivery_payloads
WHERE calendar_delivery_key IS NULL;

ALTER TABLE notification_calendar_delivery_payloads
  ALTER COLUMN calendar_delivery_key SET NOT NULL,
  ADD CONSTRAINT notification_calendar_payload_key_nonblank
    CHECK (btrim(calendar_delivery_key) <> ''),
  ADD CONSTRAINT notification_calendar_payload_delivery_key_fk
    FOREIGN KEY (delivery_id,msp_id,calendar_delivery_key)
    REFERENCES notification_deliveries(id,msp_id,calendar_delivery_key)
    ON DELETE CASCADE;

-- +goose Down
ALTER TABLE notification_calendar_delivery_payloads
  DROP CONSTRAINT notification_calendar_payload_delivery_key_fk,
  DROP CONSTRAINT notification_calendar_payload_key_nonblank,
  DROP COLUMN calendar_delivery_key;

ALTER TABLE notification_deliveries
  DROP CONSTRAINT notification_deliveries_id_msp_calendar_key_unique;

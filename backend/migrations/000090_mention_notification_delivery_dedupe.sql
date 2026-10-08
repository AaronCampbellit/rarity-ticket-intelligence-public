-- +goose Up
ALTER TABLE notification_deliveries
  DROP CONSTRAINT notification_delivery_dedupe_uniq;

CREATE UNIQUE INDEX notification_delivery_legacy_dedupe_uniq
  ON notification_deliveries (policy_id, event_id, channel, recipient_ref)
  WHERE recipient_technician_id IS NULL;

WITH ranked_mentions AS (
  SELECT id,
         row_number() OVER (
           PARTITION BY event_id, recipient_technician_id, channel
           ORDER BY planned_at, id
         ) AS duplicate_number
  FROM notification_deliveries
  WHERE recipient_technician_id IS NOT NULL
)
DELETE FROM notification_deliveries delivery
USING ranked_mentions ranked
WHERE delivery.id = ranked.id AND ranked.duplicate_number > 1;

CREATE UNIQUE INDEX notification_delivery_mention_dedupe_uniq
  ON notification_deliveries (event_id, recipient_technician_id, channel)
  WHERE recipient_technician_id IS NOT NULL;

-- +goose Down
DROP INDEX notification_delivery_mention_dedupe_uniq;
DROP INDEX notification_delivery_legacy_dedupe_uniq;

WITH ranked_deliveries AS (
  SELECT id,
         row_number() OVER (
           PARTITION BY policy_id, event_id, channel, recipient_ref
           ORDER BY planned_at, id
         ) AS duplicate_number
  FROM notification_deliveries
)
DELETE FROM notification_deliveries delivery
USING ranked_deliveries ranked
WHERE delivery.id = ranked.id AND ranked.duplicate_number > 1;

ALTER TABLE notification_deliveries
  ADD CONSTRAINT notification_delivery_dedupe_uniq
  UNIQUE (policy_id, event_id, channel, recipient_ref);

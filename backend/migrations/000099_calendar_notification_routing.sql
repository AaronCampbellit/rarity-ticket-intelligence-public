-- +goose Up
ALTER TABLE notification_deliveries
  ADD CONSTRAINT notification_deliveries_id_msp_unique UNIQUE (id,msp_id);

ALTER TABLE event_outbox
  ADD CONSTRAINT event_outbox_id_msp_unique UNIQUE (event_id,msp_id);

ALTER TABLE notification_deliveries
  ADD COLUMN calendar_delivery_key text
    CHECK (calendar_delivery_key IS NULL OR btrim(calendar_delivery_key) <> '');

ALTER TABLE notification_deliveries
  ADD CONSTRAINT notification_delivery_event_msp_fk
    FOREIGN KEY (event_id,msp_id) REFERENCES event_outbox(event_id,msp_id),
  ADD CONSTRAINT notification_delivery_calendar_key_required
    CHECK (recipient_ref <> 'calendar.assignee' OR calendar_delivery_key IS NOT NULL);

CREATE UNIQUE INDEX notification_delivery_calendar_key_uniq
  ON notification_deliveries (msp_id,calendar_delivery_key)
  WHERE calendar_delivery_key IS NOT NULL;

CREATE TABLE notification_calendar_delivery_payloads (
  delivery_id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  recipient_technician_id uuid NOT NULL,
  correlation_id uuid NOT NULL,
  change_class text NOT NULL
    CHECK (change_class IN ('schedule', 'pto', 'conflict', 'cancellation', 'reminder')),
  urgency text NOT NULL
    CHECK (urgency IN ('routine', 'important', 'urgent')),
  source_refs jsonb NOT NULL CHECK (jsonb_typeof(source_refs)='array'),
  action_path text NOT NULL CHECK (action_path LIKE '/%'),
  FOREIGN KEY (delivery_id,msp_id) REFERENCES notification_deliveries(id,msp_id) ON DELETE CASCADE,
  FOREIGN KEY (recipient_technician_id,msp_id) REFERENCES technicians(id,msp_id)
);

CREATE TABLE recipient_notifications (
  id uuid PRIMARY KEY,
  delivery_id uuid NOT NULL UNIQUE,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  recipient_technician_id uuid NOT NULL,
  event_id uuid NOT NULL,
  deduplication_key text NOT NULL CHECK (btrim(deduplication_key) <> ''),
  title text NOT NULL CHECK (btrim(title) <> ''),
  body text NOT NULL CHECK (btrim(body) <> ''),
  action_path text NOT NULL CHECK (action_path LIKE '/%'),
  content_classification text NOT NULL CHECK (btrim(content_classification) <> ''),
  created_at timestamptz NOT NULL DEFAULT now(),
  read_at timestamptz,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  FOREIGN KEY (delivery_id,msp_id) REFERENCES notification_deliveries(id,msp_id),
  FOREIGN KEY (event_id,msp_id) REFERENCES event_outbox(event_id,msp_id),
  FOREIGN KEY (recipient_technician_id,msp_id) REFERENCES technicians(id,msp_id),
  UNIQUE (msp_id, recipient_technician_id, deduplication_key)
);

CREATE INDEX recipient_notifications_pagination_idx
  ON recipient_notifications (msp_id, recipient_technician_id, created_at DESC, id DESC);

CREATE INDEX recipient_notifications_unread_idx
  ON recipient_notifications (msp_id, recipient_technician_id, created_at DESC, id DESC)
  WHERE read_at IS NULL;

CREATE INDEX recipient_notifications_version_idx
  ON recipient_notifications (msp_id, recipient_technician_id, id, version);

-- +goose Down
DROP TABLE recipient_notifications;
DROP TABLE notification_calendar_delivery_payloads;
DROP INDEX notification_delivery_calendar_key_uniq;
ALTER TABLE notification_deliveries
  DROP CONSTRAINT notification_delivery_event_msp_fk,
  DROP CONSTRAINT notification_delivery_calendar_key_required,
  DROP COLUMN calendar_delivery_key;
ALTER TABLE event_outbox DROP CONSTRAINT event_outbox_id_msp_unique;
ALTER TABLE notification_deliveries DROP CONSTRAINT notification_deliveries_id_msp_unique;

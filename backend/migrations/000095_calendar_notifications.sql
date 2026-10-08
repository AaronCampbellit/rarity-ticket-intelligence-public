-- +goose Up
CREATE TABLE calendar_notification_preference_sets (
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  technician_id uuid NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (msp_id, technician_id),
  FOREIGN KEY (technician_id, msp_id) REFERENCES technicians(id, msp_id)
);

CREATE TABLE calendar_notification_preference_rules (
  msp_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  event_class text NOT NULL
    CHECK (event_class IN ('calendar.schedule_changed')),
  change_class text NOT NULL
    CHECK (change_class IN ('schedule', 'pto', 'conflict', 'cancellation', 'reminder')),
  urgency text NOT NULL CHECK (urgency IN ('routine', 'important', 'urgent')),
  channel text NOT NULL CHECK (channel IN ('in_app', 'email')),
  enabled boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (msp_id, technician_id, event_class, change_class, urgency, channel),
  FOREIGN KEY (msp_id, technician_id)
    REFERENCES calendar_notification_preference_sets(msp_id, technician_id)
    ON DELETE CASCADE
);

ALTER TABLE calendar_reminder_facts
  ADD COLUMN source_revision bigint,
  ADD COLUMN threshold text;

-- Preserve every legacy reminder identity while binding it to the revision
-- that was current when this migration ran. Thresholds are expressed as a
-- semantic offset from the projected event; the standard due reminder maps
-- to the same 24h key used by the new evaluator.
UPDATE calendar_reminder_facts fact
SET source_revision = projection.source_revision,
    threshold = CASE
      WHEN fact.reminder_kind = 'due' AND
        EXTRACT(epoch FROM ((CASE WHEN projection.all_day
          THEN projection.starts_on::timestamp AT TIME ZONE 'UTC'
          ELSE projection.starts_at END) - fact.remind_at)) = 86400
        THEN '24h'
      ELSE fact.reminder_kind || ':' ||
        EXTRACT(epoch FROM ((CASE WHEN projection.all_day
          THEN projection.starts_on::timestamp AT TIME ZONE 'UTC'
          ELSE projection.starts_at END) - fact.remind_at))::text || 's'
    END
FROM calendar_event_projections projection
WHERE projection.id = fact.projection_id
  AND projection.msp_id = fact.msp_id
  AND projection.client_scope_key = fact.client_scope_key;

ALTER TABLE calendar_reminder_facts
  ALTER COLUMN source_revision SET NOT NULL,
  ALTER COLUMN threshold SET NOT NULL,
  ADD CONSTRAINT calendar_reminder_facts_source_revision_check CHECK (source_revision > 0),
  ADD CONSTRAINT calendar_reminder_facts_threshold_check CHECK (btrim(threshold) <> '');

-- The core migration used schedule time as the deduplication identity. Task 10
-- instead binds delivery to the exact source revision and threshold so a
-- legitimate reschedule can notify again without allowing evaluator retries.
-- +goose StatementBegin
DO $$
DECLARE
  old_constraint text;
BEGIN
  SELECT constraint_row.conname
    INTO old_constraint
  FROM pg_constraint constraint_row
  JOIN pg_class relation_row ON relation_row.oid = constraint_row.conrelid
  WHERE relation_row.relname = 'calendar_reminder_facts'
    AND constraint_row.contype = 'u'
    AND pg_get_constraintdef(constraint_row.oid) LIKE '%msp_id, projection_id, occurrence_key, reminder_kind, remind_at%'
  LIMIT 1;
  IF old_constraint IS NOT NULL THEN
    EXECUTE format('ALTER TABLE calendar_reminder_facts DROP CONSTRAINT %I', old_constraint);
  END IF;
END;
$$;
-- +goose StatementEnd

CREATE UNIQUE INDEX calendar_reminder_facts_revision_dedupe_idx
  ON calendar_reminder_facts
  (msp_id, projection_id, occurrence_key, threshold, source_revision);

-- +goose Down
DROP INDEX calendar_reminder_facts_revision_dedupe_idx;

WITH ranked_reminders AS (
  SELECT id,
    row_number() OVER (
      PARTITION BY msp_id, projection_id, occurrence_key, reminder_kind, remind_at
      ORDER BY source_revision DESC, id ASC
    ) AS retention_rank
  FROM calendar_reminder_facts
)
DELETE FROM calendar_reminder_facts fact
USING ranked_reminders ranked
WHERE fact.id = ranked.id
  AND ranked.retention_rank > 1;

ALTER TABLE calendar_reminder_facts
  DROP CONSTRAINT calendar_reminder_facts_threshold_check,
  DROP CONSTRAINT calendar_reminder_facts_source_revision_check,
  DROP COLUMN threshold,
  DROP COLUMN source_revision,
  ADD CONSTRAINT calendar_reminder_facts_schedule_dedupe_unique
    UNIQUE (msp_id, projection_id, occurrence_key, reminder_kind, remind_at);

DROP TABLE calendar_notification_preference_rules;
DROP TABLE calendar_notification_preference_sets;

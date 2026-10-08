-- +goose Up
CREATE TABLE notification_policy_versions (
  policy_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  version bigint NOT NULL CHECK (version > 0),
  event_type text NOT NULL,
  conditions jsonb NOT NULL DEFAULT '{}'::jsonb,
  destinations jsonb NOT NULL,
  quiet_period_seconds integer NOT NULL DEFAULT 0 CHECK (quiet_period_seconds >= 0),
  critical_bypass boolean NOT NULL DEFAULT false,
  enabled boolean NOT NULL DEFAULT true,
  priority integer NOT NULL DEFAULT 0,
  stable_order integer NOT NULL DEFAULT 0 CHECK (stable_order >= 0),
  published_at timestamptz NOT NULL,
  published_by uuid NOT NULL,
  PRIMARY KEY (policy_id, version),
  FOREIGN KEY (policy_id, msp_id) REFERENCES notification_policies(id, msp_id)
);

INSERT INTO notification_policy_versions (
  policy_id, msp_id, version, event_type, conditions, destinations,
  quiet_period_seconds, critical_bypass, enabled, priority, stable_order,
  published_at, published_by
)
SELECT
  id, msp_id, version, event_type, conditions,
  COALESCE((
    SELECT jsonb_agg(
      CASE
        WHEN jsonb_typeof(entry.value) = 'object' THEN entry.value
        ELSE jsonb_build_object(
          'channel', entry.value #>> '{}',
          'recipient_ref',
            CASE entry.value #>> '{}'
              WHEN 'teams' THEN 'unconfigured'
              WHEN 'webhook' THEN 'unconfigured'
              ELSE 'audience:default'
            END,
          'content_classification',
            CASE entry.value #>> '{}'
              WHEN 'in_app' THEN 'internal'
              ELSE 'restricted'
            END
        )
      END
    )
    FROM jsonb_array_elements(channels) entry
  ), '[]'::jsonb),
  quiet_period_seconds, critical_bypass, enabled, 0, 0,
  now(), '00000000-0000-0000-0000-000000000000'::uuid
FROM notification_policies;

CREATE TRIGGER notification_policy_versions_immutable
BEFORE UPDATE OR DELETE ON notification_policy_versions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

ALTER TABLE notification_policies
  ADD COLUMN priority integer NOT NULL DEFAULT 0,
  ADD COLUMN stable_order integer NOT NULL DEFAULT 0 CHECK (stable_order >= 0);

ALTER TABLE notification_deliveries
  ADD COLUMN policy_version bigint,
  ADD COLUMN suppression_reason text,
  ADD COLUMN planned_at timestamptz NOT NULL DEFAULT now();

UPDATE notification_deliveries delivery
SET policy_version = policy.version
FROM notification_policies policy
WHERE policy.id = delivery.policy_id;

ALTER TABLE notification_deliveries
  ALTER COLUMN policy_version SET NOT NULL,
  ADD CONSTRAINT notification_delivery_policy_version_fk
    FOREIGN KEY (policy_id, policy_version)
    REFERENCES notification_policy_versions(policy_id, version),
  ADD CONSTRAINT notification_delivery_dedupe_uniq
    UNIQUE (policy_id, event_id, channel, recipient_ref);

CREATE TABLE notification_event_plans (
  event_id uuid PRIMARY KEY REFERENCES event_outbox(event_id),
  planned_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE notification_event_plans;
ALTER TABLE notification_deliveries
  DROP CONSTRAINT notification_delivery_dedupe_uniq,
  DROP CONSTRAINT notification_delivery_policy_version_fk,
  DROP COLUMN planned_at,
  DROP COLUMN suppression_reason,
  DROP COLUMN policy_version;
ALTER TABLE notification_policies DROP COLUMN stable_order, DROP COLUMN priority;
DROP TRIGGER notification_policy_versions_immutable ON notification_policy_versions;
DROP TABLE notification_policy_versions;

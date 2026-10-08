-- +goose Up
-- Migration 80 gained these optimistic-classification tables before its first
-- supported release, but an early constrained-demo database had already
-- recorded the reserved version. Keep the final schema available to any
-- installation that applied the original migration body.
CREATE TABLE IF NOT EXISTS classification_object_versions (
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  object_type text NOT NULL,
  object_id uuid NOT NULL,
  version bigint NOT NULL CHECK (version > 0),
  UNIQUE NULLS NOT DISTINCT (msp_id, client_id, object_type, object_id),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  CHECK (object_type IN ('work_record', 'task', 'project', 'asset', 'knowledge_article', 'time_entry')),
  CHECK (client_id IS NOT NULL OR object_type = 'knowledge_article')
);

CREATE TABLE IF NOT EXISTS classification_tag_operations (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  object_type text NOT NULL,
  object_id uuid NOT NULL,
  idempotency_key text NOT NULL,
  accepted_object_version bigint NOT NULL CHECK (accepted_object_version > 0),
  response jsonb NOT NULL,
  accepted_at timestamptz NOT NULL,
  UNIQUE NULLS NOT DISTINCT (msp_id, client_id, object_type, object_id, idempotency_key),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  CHECK (object_type IN ('work_record', 'task', 'project', 'asset', 'knowledge_article', 'time_entry')),
  CHECK (client_id IS NOT NULL OR object_type = 'knowledge_article'),
  CHECK (jsonb_typeof(response) = 'object')
);

INSERT INTO role_capabilities (role_id, msp_id, capability)
SELECT role.id, role.msp_id, required.capability
FROM roles AS role
CROSS JOIN (
  VALUES
    ('classification.manage'),
    ('classification.apply'),
    ('classification.report'),
    ('classification.ai.manage')
) AS required(capability)
WHERE role.key = 'global-admin'
  AND role.system_role
ON CONFLICT DO NOTHING;

-- +goose Down
-- These objects are part of migration 80 on fresh installations. Removing
-- them here would make rollback depend on which historical upgrade path ran.
SELECT 1;

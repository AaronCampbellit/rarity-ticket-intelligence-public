-- +goose Up
CREATE TABLE tag_groups (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  internal_key text NOT NULL,
  label text NOT NULL,
  normalized_label text GENERATED ALWAYS AS (lower(btrim(label))) STORED,
  description text NOT NULL DEFAULT '',
  position integer NOT NULL DEFAULT 1 CHECK (position > 0),
  lifecycle_state text NOT NULL DEFAULT 'active',
  system_group boolean NOT NULL DEFAULT false,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, internal_key),
  UNIQUE (msp_id, normalized_label),
  CHECK (btrim(internal_key) <> ''),
  CHECK (btrim(label) <> ''),
  CHECK (lifecycle_state IN ('active', 'archived'))
);

CREATE TABLE tags (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  group_id uuid NOT NULL,
  internal_key text NOT NULL,
  label text NOT NULL,
  normalized_label text GENERATED ALWAYS AS (lower(btrim(label))) STORED,
  description text NOT NULL DEFAULT '',
  color text,
  lifecycle_state text NOT NULL DEFAULT 'active',
  merged_into_id uuid,
  system_tag boolean NOT NULL DEFAULT false,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, internal_key),
  FOREIGN KEY (group_id, msp_id) REFERENCES tag_groups(id, msp_id),
  FOREIGN KEY (merged_into_id, msp_id) REFERENCES tags(id, msp_id),
  CHECK (btrim(internal_key) <> ''),
  CHECK (btrim(label) <> ''),
  CHECK (lifecycle_state IN ('active', 'merged', 'archived')),
  CHECK (
    (lifecycle_state = 'merged' AND merged_into_id IS NOT NULL)
    OR (lifecycle_state <> 'merged' AND merged_into_id IS NULL)
  ),
  CHECK (merged_into_id IS NULL OR merged_into_id <> id),
  CHECK (color IS NULL OR color ~ '^#[0-9A-Fa-f]{6}$')
);

CREATE UNIQUE INDEX tags_label_unique
  ON tags (msp_id, normalized_label);

CREATE INDEX tags_group_state_idx
  ON tags (msp_id, group_id, lifecycle_state, normalized_label);

CREATE TABLE tag_synonyms (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  tag_id uuid NOT NULL,
  label text NOT NULL,
  normalized_label text GENERATED ALWAYS AS (lower(btrim(label))) STORED,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, normalized_label),
  FOREIGN KEY (tag_id, msp_id) REFERENCES tags(id, msp_id) ON DELETE CASCADE,
  CHECK (btrim(label) <> '')
);

-- +goose StatementBegin
CREATE FUNCTION enforce_tag_term_unambiguous()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  normalized_term text := lower(btrim(NEW.label));
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.msp_id::text, 0));

  IF TG_TABLE_NAME = 'tags' AND EXISTS (
    SELECT 1
    FROM tag_synonyms
    WHERE msp_id = NEW.msp_id
      AND normalized_label = normalized_term
  ) THEN
    RAISE EXCEPTION 'tag labels and synonyms must be unambiguous within an MSP';
  END IF;

  IF TG_TABLE_NAME = 'tag_synonyms' AND EXISTS (
    SELECT 1
    FROM tags
    WHERE msp_id = NEW.msp_id
      AND normalized_label = normalized_term
  ) THEN
    RAISE EXCEPTION 'tag labels and synonyms must be unambiguous within an MSP';
  END IF;

  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER tag_terms_unambiguous
BEFORE INSERT OR UPDATE OF msp_id, label ON tags
FOR EACH ROW EXECUTE FUNCTION enforce_tag_term_unambiguous();

CREATE TRIGGER tag_terms_unambiguous
BEFORE INSERT OR UPDATE OF msp_id, label ON tag_synonyms
FOR EACH ROW EXECUTE FUNCTION enforce_tag_term_unambiguous();

-- +goose StatementBegin
CREATE FUNCTION protect_tag_internal_key()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.internal_key IS DISTINCT FROM OLD.internal_key THEN
    RAISE EXCEPTION 'tag internal keys are immutable';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER tag_groups_internal_key_immutable
BEFORE UPDATE OF internal_key ON tag_groups
FOR EACH ROW EXECUTE FUNCTION protect_tag_internal_key();

CREATE TRIGGER tags_internal_key_immutable
BEFORE UPDATE OF internal_key ON tags
FOR EACH ROW EXECUTE FUNCTION protect_tag_internal_key();

CREATE TABLE object_tag_assignments (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  object_type text NOT NULL,
  object_id uuid NOT NULL,
  object_version bigint NOT NULL CHECK (object_version > 0),
  tag_id uuid NOT NULL,
  assignment_source text NOT NULL,
  assigned_at timestamptz NOT NULL DEFAULT now(),
  assigned_by uuid,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  inherited_from_assignment_id uuid,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, object_type, object_id, tag_id),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (tag_id, msp_id)
    REFERENCES tags(id, msp_id),
  FOREIGN KEY (inherited_from_assignment_id, msp_id)
    REFERENCES object_tag_assignments(id, msp_id),
  CHECK (object_type IN ('work_record', 'task', 'project', 'asset', 'knowledge_article', 'time_entry')),
  CHECK (client_id IS NOT NULL OR object_type = 'knowledge_article'),
  CHECK (
    assignment_source IN (
      'human',
      'ai_confirmed',
      'ai_automatic',
      'automation',
      'integration',
      'migration',
      'system_fallback'
    )
  ),
  CHECK (
    (assignment_source = 'system_fallback' AND assigned_by IS NULL)
    OR assignment_source <> 'system_fallback'
  ),
  CHECK (inherited_from_assignment_id IS NULL OR inherited_from_assignment_id <> id)
);

CREATE INDEX object_tag_assignments_object_idx
  ON object_tag_assignments (msp_id, client_id, object_type, object_id);

CREATE INDEX object_tag_assignments_tag_idx
  ON object_tag_assignments (msp_id, tag_id, object_type, assigned_at DESC);

CREATE TABLE tag_assignment_events (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  assignment_id uuid NOT NULL,
  object_type text NOT NULL,
  object_id uuid NOT NULL,
  target_version bigint NOT NULL CHECK (target_version > 0),
  tag_id uuid NOT NULL,
  operation text NOT NULL,
  assignment_source text NOT NULL,
  actor_type text NOT NULL,
  actor_id uuid,
  occurred_at timestamptz NOT NULL DEFAULT now(),
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  idempotency_key text NOT NULL,
  correlation_id uuid NOT NULL,
  causation_id uuid,
  UNIQUE (idempotency_key),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (tag_id, msp_id)
    REFERENCES tags(id, msp_id),
  CHECK (object_type IN ('work_record', 'task', 'project', 'asset', 'knowledge_article', 'time_entry')),
  CHECK (client_id IS NOT NULL OR object_type = 'knowledge_article'),
  CHECK (operation IN ('added', 'removed')),
  CHECK (
    assignment_source IN (
      'human',
      'ai_confirmed',
      'ai_automatic',
      'automation',
      'integration',
      'migration',
      'system_fallback'
    )
  ),
  CHECK (actor_type IN ('technician', 'system', 'ai', 'automation', 'integration'))
);

CREATE INDEX tag_assignment_events_object_time_idx
  ON tag_assignment_events (msp_id, object_type, object_id, occurred_at DESC);

CREATE INDEX tag_assignment_events_tag_time_idx
  ON tag_assignment_events (msp_id, tag_id, occurred_at DESC);

-- Classification optimistic versions must remain independent from object
-- domain versions, particularly knowledge_articles.current_version.
CREATE TABLE classification_object_versions (
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

CREATE TABLE classification_tag_operations (
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

-- +goose StatementBegin
CREATE FUNCTION reject_tag_assignment_event_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  RAISE EXCEPTION 'tag assignment events are append-only';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER tag_assignment_events_append_only
BEFORE UPDATE OR DELETE ON tag_assignment_events
FOR EACH ROW EXECUTE FUNCTION reject_tag_assignment_event_mutation();

CREATE TABLE tag_ai_policies (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  automatic_apply_enabled boolean NOT NULL DEFAULT false,
  automatic_apply_threshold numeric(4,3) NOT NULL DEFAULT 0.950,
  model_profile_id uuid,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id),
  FOREIGN KEY (model_profile_id, msp_id)
    REFERENCES ai_model_profiles(id, msp_id),
  CHECK (automatic_apply_threshold BETWEEN 0.500 AND 1.000)
);

CREATE TABLE tag_ai_suggestions (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  object_type text NOT NULL,
  object_id uuid NOT NULL,
  object_version bigint NOT NULL CHECK (object_version > 0),
  model_profile_id uuid,
  status text NOT NULL DEFAULT 'pending',
  requested_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz,
  decided_at timestamptz,
  decided_by uuid,
  correlation_id uuid NOT NULL,
  source_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  explanation text NOT NULL DEFAULT '',
  UNIQUE (id, msp_id),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (model_profile_id, msp_id)
    REFERENCES ai_model_profiles(id, msp_id),
  CHECK (object_type IN ('work_record', 'task', 'project', 'asset', 'knowledge_article', 'time_entry')),
  CHECK (client_id IS NOT NULL OR object_type = 'knowledge_article'),
  CHECK (status IN ('pending', 'completed', 'accepted', 'rejected', 'superseded', 'failed')),
  CHECK (completed_at IS NULL OR completed_at >= requested_at),
  CHECK ((decided_at IS NULL) = (decided_by IS NULL))
);

CREATE INDEX tag_ai_suggestions_object_idx
  ON tag_ai_suggestions (msp_id, object_type, object_id, requested_at DESC);

CREATE TABLE tag_ai_suggestion_items (
  suggestion_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  tag_id uuid NOT NULL,
  confidence numeric(4,3) NOT NULL,
  rank integer NOT NULL CHECK (rank > 0),
  rationale text NOT NULL DEFAULT '',
  disposition text NOT NULL DEFAULT 'suggested',
  PRIMARY KEY (suggestion_id, tag_id),
  UNIQUE (suggestion_id, rank),
  FOREIGN KEY (suggestion_id, msp_id)
    REFERENCES tag_ai_suggestions(id, msp_id) ON DELETE CASCADE,
  FOREIGN KEY (tag_id, msp_id)
    REFERENCES tags(id, msp_id),
  CHECK (confidence BETWEEN 0.000 AND 1.000),
  CHECK (disposition IN ('suggested', 'accepted', 'rejected', 'automatically_applied'))
);

CREATE TABLE tag_projection_cursors (
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  projection_name text NOT NULL,
  last_occurred_at timestamptz,
  last_event_id uuid,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (msp_id, projection_name),
  CHECK ((last_occurred_at IS NULL) = (last_event_id IS NULL))
);

CREATE TABLE tag_usage_daily (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  usage_date date NOT NULL,
  tag_id uuid NOT NULL,
  object_type text NOT NULL,
  assignment_source text NOT NULL,
  added_count bigint NOT NULL DEFAULT 0 CHECK (added_count >= 0),
  removed_count bigint NOT NULL DEFAULT 0 CHECK (removed_count >= 0),
  active_count bigint NOT NULL DEFAULT 0 CHECK (active_count >= 0),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (tag_id, msp_id)
    REFERENCES tags(id, msp_id),
  CHECK (object_type IN ('work_record', 'task', 'project', 'asset', 'knowledge_article', 'time_entry'))
);

CREATE UNIQUE INDEX tag_usage_daily_client_unique
  ON tag_usage_daily (
    msp_id,
    client_id,
    usage_date,
    tag_id,
    object_type,
    assignment_source
  )
  WHERE client_id IS NOT NULL;

CREATE UNIQUE INDEX tag_usage_daily_msp_unique
  ON tag_usage_daily (
    msp_id,
    usage_date,
    tag_id,
    object_type,
    assignment_source
  )
  WHERE client_id IS NULL;

CREATE TABLE tag_cooccurrence_daily (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  usage_date date NOT NULL,
  left_tag_id uuid NOT NULL,
  right_tag_id uuid NOT NULL,
  object_type text NOT NULL,
  object_count bigint NOT NULL DEFAULT 0 CHECK (object_count >= 0),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (left_tag_id, msp_id)
    REFERENCES tags(id, msp_id),
  FOREIGN KEY (right_tag_id, msp_id)
    REFERENCES tags(id, msp_id),
  CHECK (left_tag_id < right_tag_id),
  CHECK (object_type IN ('work_record', 'task', 'project', 'asset', 'knowledge_article', 'time_entry'))
);

CREATE UNIQUE INDEX tag_cooccurrence_daily_client_unique
  ON tag_cooccurrence_daily (
    msp_id,
    client_id,
    usage_date,
    left_tag_id,
    right_tag_id,
    object_type
  )
  WHERE client_id IS NOT NULL;

CREATE UNIQUE INDEX tag_cooccurrence_daily_msp_unique
  ON tag_cooccurrence_daily (
    msp_id,
    usage_date,
    left_tag_id,
    right_tag_id,
    object_type
  )
  WHERE client_id IS NULL;

CREATE TABLE classification_migration_runs (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  started_at timestamptz NOT NULL,
  completed_at timestamptz,
  status text NOT NULL,
  rows_discovered bigint NOT NULL DEFAULT 0 CHECK (rows_discovered >= 0),
  rows_migrated bigint NOT NULL DEFAULT 0 CHECK (rows_migrated >= 0),
  fallback_assignments bigint NOT NULL DEFAULT 0 CHECK (fallback_assignments >= 0),
  category_source_present boolean NOT NULL DEFAULT false,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id),
  CHECK (status IN ('running', 'completed', 'failed')),
  CHECK (completed_at IS NULL OR completed_at >= started_at),
  CHECK (
    (status = 'running' AND completed_at IS NULL)
    OR (status <> 'running' AND completed_at IS NOT NULL)
  )
);

CREATE TABLE classification_migration_mappings (
  id uuid PRIMARY KEY,
  migration_run_id uuid NOT NULL,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  source_type text NOT NULL,
  source_value text NOT NULL,
  tag_id uuid NOT NULL,
  rows_mapped bigint NOT NULL DEFAULT 0 CHECK (rows_mapped >= 0),
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  UNIQUE (id, msp_id),
  UNIQUE (migration_run_id, source_type, source_value),
  FOREIGN KEY (migration_run_id, msp_id)
    REFERENCES classification_migration_runs(id, msp_id) ON DELETE CASCADE,
  FOREIGN KEY (tag_id, msp_id)
    REFERENCES tags(id, msp_id),
  CHECK (source_type IN ('legacy_tag', 'category'))
);

INSERT INTO tag_groups (
  id,
  msp_id,
  internal_key,
  label,
  description,
  position,
  lifecycle_state,
  system_group,
  created_by,
  updated_by
)
SELECT
  md5(msp.id::text || ':classification:system-group')::uuid,
  msp.id,
  'taxonomy.system',
  'System',
  'System-managed classification tags.',
  1,
  'active',
  true,
  msp.created_by,
  msp.updated_by
FROM msp_organizations AS msp;

INSERT INTO tags (
  id,
  msp_id,
  group_id,
  internal_key,
  label,
  description,
  lifecycle_state,
  system_tag,
  created_by,
  updated_by
)
SELECT
  md5(msp.id::text || ':classification:unclassified')::uuid,
  msp.id,
  md5(msp.id::text || ':classification:system-group')::uuid,
  'taxonomy.system.unclassified',
  'Unclassified',
  'Required fallback until a governed tag is selected.',
  'active',
  true,
  msp.created_by,
  msp.updated_by
FROM msp_organizations AS msp;

INSERT INTO tag_ai_policies (
  id,
  msp_id,
  automatic_apply_enabled,
  automatic_apply_threshold,
  created_by,
  updated_by
)
SELECT
  md5(msp.id::text || ':classification:ai-policy')::uuid,
  msp.id,
  false,
  0.950,
  msp.created_by,
  msp.updated_by
FROM msp_organizations AS msp;

WITH supported_objects AS (
  SELECT id, msp_id, client_id, version AS object_version, 'work_record'::text AS object_type FROM work_records
  UNION ALL
  SELECT id, msp_id, client_id, version, 'task'::text FROM tasks
  UNION ALL
  SELECT id, msp_id, client_id, version, 'project'::text FROM projects
  UNION ALL
  SELECT id, msp_id, client_id, version, 'asset'::text FROM assets
  UNION ALL
  SELECT id, msp_id, client_id, current_version, 'knowledge_article'::text FROM knowledge_articles
  UNION ALL
  SELECT id, msp_id, client_id, version, 'time_entry'::text FROM time_entries
)
INSERT INTO object_tag_assignments (
  id,
  msp_id,
  client_id,
  object_type,
  object_id,
  object_version,
  tag_id,
  assignment_source,
  evidence
)
SELECT
  md5(
    object.msp_id::text
    || ':classification:fallback:'
    || object.object_type
    || ':'
    || object.id::text
  )::uuid,
  object.msp_id,
  object.client_id,
  object.object_type,
  object.id,
  object.object_version,
  md5(object.msp_id::text || ':classification:unclassified')::uuid,
  'system_fallback',
  jsonb_build_object('migration', '000080_tagging_classification')
FROM supported_objects AS object;

INSERT INTO tag_assignment_events (
  id,
  msp_id,
  client_id,
  assignment_id,
  object_type,
  object_id,
  target_version,
  tag_id,
  operation,
  assignment_source,
  actor_type,
  occurred_at,
  evidence,
  idempotency_key,
  correlation_id
)
SELECT
  md5(assignment.id::text || ':added')::uuid,
  assignment.msp_id,
  assignment.client_id,
  assignment.id,
  assignment.object_type,
  assignment.object_id,
  assignment.object_version,
  assignment.tag_id,
  'added',
  assignment.assignment_source,
  'system',
  assignment.assigned_at,
  assignment.evidence,
  'migration:000080:fallback:' || assignment.id::text,
  md5(assignment.msp_id::text || ':classification:migration:000080')::uuid
FROM object_tag_assignments AS assignment
WHERE assignment.assignment_source = 'system_fallback';

WITH object_counts AS (
  SELECT
    msp.id AS msp_id,
    (
      (SELECT count(*) FROM work_records WHERE work_records.msp_id = msp.id)
      + (SELECT count(*) FROM tasks WHERE tasks.msp_id = msp.id)
      + (SELECT count(*) FROM projects WHERE projects.msp_id = msp.id)
      + (SELECT count(*) FROM assets WHERE assets.msp_id = msp.id)
      + (SELECT count(*) FROM knowledge_articles WHERE knowledge_articles.msp_id = msp.id)
      + (SELECT count(*) FROM time_entries WHERE time_entries.msp_id = msp.id)
    )::bigint AS object_count
  FROM msp_organizations AS msp
)
INSERT INTO classification_migration_runs (
  id,
  msp_id,
  started_at,
  completed_at,
  status,
  rows_discovered,
  rows_migrated,
  fallback_assignments,
  category_source_present,
  evidence
)
SELECT
  md5(counts.msp_id::text || ':classification:migration:000080')::uuid,
  counts.msp_id,
  now(),
  now(),
  'completed',
  counts.object_count,
  counts.object_count,
  counts.object_count,
  false,
  jsonb_build_object(
    'migration', '000080_tagging_classification',
    'strategy', 'full_cutover',
    'source', 'system_fallback'
  )
FROM object_counts AS counts;

-- Existing installations receive the same governed-classification authority
-- as a first-run bootstrap. This migration is unreleased, so the upgrade is
-- deliberately kept with the schema that introduces the capabilities.
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
DELETE FROM role_capabilities AS capability
USING roles AS role
WHERE capability.role_id = role.id
  AND capability.msp_id = role.msp_id
  AND role.key = 'global-admin'
  AND role.system_role
  AND capability.capability IN (
    'classification.manage',
    'classification.apply',
    'classification.report',
    'classification.ai.manage'
  );
DROP TABLE classification_migration_mappings;
DROP TABLE classification_migration_runs;
DROP TABLE tag_cooccurrence_daily;
DROP TABLE tag_usage_daily;
DROP TABLE tag_projection_cursors;
DROP TABLE tag_ai_suggestion_items;
DROP TABLE tag_ai_suggestions;
DROP TABLE tag_ai_policies;
DROP TRIGGER tag_assignment_events_append_only ON tag_assignment_events;
DROP FUNCTION reject_tag_assignment_event_mutation();
DROP TABLE classification_tag_operations;
DROP TABLE classification_object_versions;
DROP TABLE tag_assignment_events;
DROP TABLE object_tag_assignments;
DROP TRIGGER tags_internal_key_immutable ON tags;
DROP TRIGGER tag_groups_internal_key_immutable ON tag_groups;
DROP FUNCTION protect_tag_internal_key();
DROP TRIGGER tag_terms_unambiguous ON tag_synonyms;
DROP TRIGGER tag_terms_unambiguous ON tags;
DROP FUNCTION enforce_tag_term_unambiguous();
DROP TABLE tag_synonyms;
DROP TABLE tags;
DROP TABLE tag_groups;

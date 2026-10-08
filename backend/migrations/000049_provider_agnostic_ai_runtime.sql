-- +goose Up
CREATE TABLE ai_provider_connections (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  name text NOT NULL,
  adapter_type text NOT NULL CHECK (adapter_type IN ('ollama', 'openai_compatible')),
  network_mode text NOT NULL CHECK (network_mode IN ('local', 'remote')),
  base_url text NOT NULL,
  credential_version integer,
  credential_nonce bytea,
  credential_ciphertext bytea,
  enabled boolean NOT NULL DEFAULT false,
  timeout_seconds integer NOT NULL CHECK (timeout_seconds BETWEEN 1 AND 3600),
  request_limit_bytes bigint NOT NULL DEFAULT 1048576
    CHECK (request_limit_bytes BETWEEN 1024 AND 5242880),
  response_limit_bytes bigint NOT NULL DEFAULT 5242880
    CHECK (response_limit_bytes BETWEEN 1024 AND 10485760),
  disclosure_accepted_at timestamptz,
  disclosure_accepted_by uuid,
  local_network_acknowledged_at timestamptz,
  local_network_acknowledged_by uuid,
  health_state text NOT NULL DEFAULT 'pending'
    CHECK (health_state IN ('pending', 'healthy', 'degraded', 'failed', 'disabled')),
  last_tested_at timestamptz,
  last_succeeded_at timestamptz,
  last_error_code text,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL,
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL,
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, name),
  CHECK (
    (credential_version IS NULL AND credential_nonce IS NULL AND credential_ciphertext IS NULL)
    OR
    (credential_version IS NOT NULL AND credential_nonce IS NOT NULL AND credential_ciphertext IS NOT NULL)
  ),
  CHECK (
    (disclosure_accepted_at IS NULL AND disclosure_accepted_by IS NULL)
    OR
    (disclosure_accepted_at IS NOT NULL AND disclosure_accepted_by IS NOT NULL)
  ),
  CHECK (
    network_mode <> 'local'
    OR (local_network_acknowledged_at IS NOT NULL AND local_network_acknowledged_by IS NOT NULL)
  )
);

-- +goose StatementBegin
CREATE FUNCTION default_ai_provider_connection_timeout()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.timeout_seconds IS NULL THEN
    NEW.timeout_seconds := CASE NEW.network_mode
      WHEN 'local' THEN 900
      WHEN 'remote' THEN 300
    END;
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER ai_provider_connections_default_timeout
BEFORE INSERT OR UPDATE OF network_mode, timeout_seconds ON ai_provider_connections
FOR EACH ROW EXECUTE FUNCTION default_ai_provider_connection_timeout();

CREATE TABLE ai_model_profiles (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  connection_id uuid NOT NULL,
  provider_model_id text NOT NULL,
  display_name text NOT NULL,
  supported_features text[] NOT NULL DEFAULT '{}'::text[],
  context_limit bigint NOT NULL CHECK (context_limit > 0),
  output_limit bigint NOT NULL CHECK (output_limit > 0 AND output_limit <= context_limit),
  zero_cost boolean NOT NULL DEFAULT false,
  input_cost_per_million_minor bigint CHECK (input_cost_per_million_minor >= 0),
  output_cost_per_million_minor bigint CHECK (output_cost_per_million_minor >= 0),
  enabled boolean NOT NULL DEFAULT false,
  discovered_at timestamptz NOT NULL,
  last_discovered_at timestamptz NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (connection_id, provider_model_id),
  FOREIGN KEY (connection_id, msp_id)
    REFERENCES ai_provider_connections(id, msp_id),
  CHECK (supported_features <@ ARRAY['summary', 'reply_draft', 'similar_suggestions']::text[])
  ,CHECK (zero_cost OR (input_cost_per_million_minor IS NULL OR input_cost_per_million_minor >= 0))
  ,CHECK (NOT zero_cost OR (COALESCE(input_cost_per_million_minor, 0) = 0 AND COALESCE(output_cost_per_million_minor, 0) = 0))
);

CREATE TABLE ai_policy_legacy_provider_configurations (
  policy_id uuid PRIMARY KEY REFERENCES ai_policies(id) ON DELETE CASCADE,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  was_enabled boolean NOT NULL,
  provider text,
  model text,
  provider_connection_id uuid,
  provider_disclosure_accepted_at timestamptz,
  provider_disclosure_accepted_by uuid,
  migrated_at timestamptz NOT NULL DEFAULT now(),
  migration_reason text NOT NULL
);

INSERT INTO ai_policy_legacy_provider_configurations (
  policy_id, msp_id, was_enabled, provider, model, provider_connection_id,
  provider_disclosure_accepted_at, provider_disclosure_accepted_by, migration_reason
)
SELECT
  id, msp_id, enabled, provider, model, provider_connection_id,
  provider_disclosure_accepted_at, provider_disclosure_accepted_by,
  'provider-agnostic runtime migration requires an explicitly selected model profile'
FROM ai_policies;

UPDATE ai_policies SET enabled = false WHERE enabled;

-- +goose StatementBegin
DO $$
DECLARE
  legacy_constraint_name text;
BEGIN
  FOR legacy_constraint_name IN
    SELECT constraint_row.conname
    FROM pg_constraint constraint_row
    WHERE constraint_row.conrelid = 'ai_policies'::regclass
      AND constraint_row.contype = 'c'
      AND (
        pg_get_constraintdef(constraint_row.oid) LIKE '%allowed_features <@%'
        OR pg_get_constraintdef(constraint_row.oid) LIKE '%provider_connection_id IS NOT NULL%'
      )
  LOOP
    EXECUTE format('ALTER TABLE ai_policies DROP CONSTRAINT %I', legacy_constraint_name);
  END LOOP;
END;
$$;
-- +goose StatementEnd

ALTER TABLE ai_policies
  DROP CONSTRAINT ai_policies_provider_connection_id_msp_id_fkey,
  DROP COLUMN provider,
  DROP COLUMN model,
  DROP COLUMN provider_connection_id,
  DROP COLUMN provider_disclosure_accepted_at,
  DROP COLUMN provider_disclosure_accepted_by,
  ADD COLUMN summary_model_profile_id uuid,
  ADD COLUMN reply_draft_model_profile_id uuid,
  ADD COLUMN similar_suggestions_model_profile_id uuid,
  ADD COLUMN cost_limit_enabled boolean NOT NULL DEFAULT false,
  ADD COLUMN allow_unmetered_unknown boolean NOT NULL DEFAULT false,
  ADD CONSTRAINT ai_policies_allowed_features_check
    CHECK (allowed_features <@ ARRAY['summary', 'reply_draft', 'similar_suggestions']::text[]),
  ADD CONSTRAINT ai_policies_summary_model_profile_msp_fkey
    FOREIGN KEY (summary_model_profile_id, msp_id) REFERENCES ai_model_profiles(id, msp_id),
  ADD CONSTRAINT ai_policies_reply_draft_model_profile_msp_fkey
    FOREIGN KEY (reply_draft_model_profile_id, msp_id) REFERENCES ai_model_profiles(id, msp_id),
  ADD CONSTRAINT ai_policies_similar_suggestions_model_profile_msp_fkey
    FOREIGN KEY (similar_suggestions_model_profile_id, msp_id) REFERENCES ai_model_profiles(id, msp_id),
  ADD CONSTRAINT ai_policies_enabled_configured_check CHECK (
    NOT enabled OR (
      cardinality(allowed_features) > 0
      AND (NOT ('summary' = ANY(allowed_features)) OR summary_model_profile_id IS NOT NULL)
      AND (NOT ('reply_draft' = ANY(allowed_features)) OR reply_draft_model_profile_id IS NOT NULL)
      AND (NOT ('similar_suggestions' = ANY(allowed_features)) OR similar_suggestions_model_profile_id IS NOT NULL)
    )
  );

-- +goose StatementBegin
CREATE FUNCTION validate_ai_policy_model_profiles()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  selected_profile_id uuid;
  selected_feature text;
BEGIN
  IF TG_OP = 'UPDATE'
    AND NEW.enabled IS NOT DISTINCT FROM OLD.enabled
    AND NEW.msp_id IS NOT DISTINCT FROM OLD.msp_id
    AND NEW.summary_model_profile_id IS NOT DISTINCT FROM OLD.summary_model_profile_id
    AND NEW.reply_draft_model_profile_id IS NOT DISTINCT FROM OLD.reply_draft_model_profile_id
    AND NEW.similar_suggestions_model_profile_id IS NOT DISTINCT FROM OLD.similar_suggestions_model_profile_id
  THEN
    RETURN NEW;
  END IF;

  FOR selected_profile_id, selected_feature IN
    SELECT * FROM (VALUES
      (NEW.summary_model_profile_id, 'summary'::text),
      (NEW.reply_draft_model_profile_id, 'reply_draft'::text),
      (NEW.similar_suggestions_model_profile_id, 'similar_suggestions'::text)
    ) AS configured(model_profile_id, feature)
  LOOP
    IF selected_profile_id IS NOT NULL AND NOT EXISTS (
      SELECT 1
      FROM ai_model_profiles profile
      JOIN ai_provider_connections connection
        ON connection.id = profile.connection_id AND connection.msp_id = profile.msp_id
      WHERE profile.id = selected_profile_id
        AND profile.msp_id = NEW.msp_id
        AND selected_feature = ANY(profile.supported_features)
        AND (NOT NEW.enabled OR (
          profile.enabled
          AND connection.enabled
          AND connection.disclosure_accepted_at IS NOT NULL
          AND connection.disclosure_accepted_by IS NOT NULL
        ))
    ) THEN
      RAISE EXCEPTION 'AI policy model profile must support its configured feature and be enabled with its connection when the policy is enabled';
    END IF;
  END LOOP;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER ai_policies_model_profiles_valid
AFTER INSERT OR UPDATE ON ai_policies
DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW EXECUTE FUNCTION validate_ai_policy_model_profiles();

CREATE TABLE ai_generation_jobs (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  work_record_id uuid NOT NULL,
  requested_by uuid NOT NULL,
  feature text NOT NULL CHECK (feature IN ('summary', 'reply_draft', 'similar_suggestions')),
  model_profile_id uuid NOT NULL,
  relevant_input_names text[] NOT NULL DEFAULT '{}'::text[],
  state text NOT NULL DEFAULT 'queued'
    CHECK (state IN ('queued', 'running', 'completed', 'failed', 'cancelled')),
  attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  max_attempts integer NOT NULL DEFAULT 3 CHECK (max_attempts BETWEEN 1 AND 10),
  lease_token uuid,
  lease_until timestamptz,
  reserved_cost_minor bigint NOT NULL DEFAULT 0 CHECK (reserved_cost_minor >= 0),
  execution_fingerprint text,
  execution_policy_version bigint,
  execution_model_version bigint,
  execution_connection_version bigint,
  execution_work_updated_at timestamptz,
  execution_candidate_fingerprint text,
  cancellation_requested_at timestamptz,
  cancellation_requested_by uuid,
  cancelled_at timestamptz,
  safe_error_code text,
  recommendation_id uuid UNIQUE REFERENCES ai_recommendations(id),
  idempotency_key text NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  completed_at timestamptz,
  UNIQUE (id, msp_id, client_id),
  UNIQUE (idempotency_key),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (work_record_id, msp_id, client_id)
    REFERENCES work_records(id, msp_id, client_id),
  FOREIGN KEY (model_profile_id, msp_id) REFERENCES ai_model_profiles(id, msp_id),
  CHECK ((lease_token IS NULL) = (lease_until IS NULL)),
  CHECK (
    (execution_fingerprint IS NULL AND execution_policy_version IS NULL
      AND execution_model_version IS NULL AND execution_connection_version IS NULL
      AND execution_work_updated_at IS NULL AND execution_candidate_fingerprint IS NULL)
    OR
    (execution_fingerprint IS NOT NULL AND execution_policy_version IS NOT NULL
      AND execution_model_version IS NOT NULL AND execution_connection_version IS NOT NULL
      AND execution_work_updated_at IS NOT NULL AND execution_candidate_fingerprint IS NOT NULL)
  ),
  CHECK ((cancellation_requested_at IS NULL) = (cancellation_requested_by IS NULL)),
  CHECK (attempt <= max_attempts),
  CHECK (state <> 'completed' OR recommendation_id IS NOT NULL),
  CHECK (state <> 'cancelled' OR cancelled_at IS NOT NULL)
);

CREATE INDEX ai_generation_jobs_claim_idx
  ON ai_generation_jobs (state, next_attempt_at, lease_until, created_at, id);

-- +goose StatementBegin
CREATE FUNCTION validate_ai_generation_job_model_profile()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'UPDATE'
    AND NEW.msp_id IS NOT DISTINCT FROM OLD.msp_id
    AND NEW.feature IS NOT DISTINCT FROM OLD.feature
    AND NEW.model_profile_id IS NOT DISTINCT FROM OLD.model_profile_id
  THEN
    RETURN NEW;
  END IF;

  IF NOT EXISTS (
    SELECT 1
    FROM ai_model_profiles profile
    JOIN ai_provider_connections connection
      ON connection.id = profile.connection_id AND connection.msp_id = profile.msp_id
    WHERE profile.id = NEW.model_profile_id
      AND profile.msp_id = NEW.msp_id
      AND NEW.feature = ANY(profile.supported_features)
      AND profile.enabled
      AND connection.enabled
      AND connection.disclosure_accepted_at IS NOT NULL
      AND connection.disclosure_accepted_by IS NOT NULL
  ) THEN
    RAISE EXCEPTION 'AI generation job model profile must support the requested feature and be enabled with its connection';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER ai_generation_jobs_model_profile_valid
AFTER INSERT OR UPDATE ON ai_generation_jobs
DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW EXECUTE FUNCTION validate_ai_generation_job_model_profile();

-- +goose StatementBegin
CREATE FUNCTION protect_ai_model_profile_references()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  connection_enabled boolean;
BEGIN
  SELECT enabled INTO connection_enabled
  FROM ai_provider_connections
  WHERE id = NEW.connection_id AND msp_id = NEW.msp_id;

  IF EXISTS (
    SELECT 1
    FROM ai_policies policy
    WHERE policy.enabled
      AND policy.msp_id = NEW.msp_id
      AND (
        (policy.summary_model_profile_id = NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'summary' = ANY(NEW.supported_features)))
        OR (policy.reply_draft_model_profile_id = NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'reply_draft' = ANY(NEW.supported_features)))
        OR (policy.similar_suggestions_model_profile_id = NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'similar_suggestions' = ANY(NEW.supported_features)))
      )
  ) OR EXISTS (
    SELECT 1
    FROM ai_generation_jobs job
    WHERE job.model_profile_id = NEW.id
      AND job.msp_id = NEW.msp_id
      AND job.state IN ('queued', 'running')
      AND NOT (NEW.enabled AND connection_enabled AND job.feature = ANY(NEW.supported_features))
  ) THEN
    RAISE EXCEPTION 'cannot make an AI model profile incompatible while enabled policies or active jobs reference it';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER ai_model_profiles_references_valid
AFTER UPDATE OF enabled, supported_features, connection_id, msp_id ON ai_model_profiles
DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW EXECUTE FUNCTION protect_ai_model_profile_references();

-- +goose StatementBegin
CREATE FUNCTION reset_ai_provider_disclosure_on_identity_change()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.adapter_type IS DISTINCT FROM OLD.adapter_type
    OR NEW.network_mode IS DISTINCT FROM OLD.network_mode
    OR NEW.base_url IS DISTINCT FROM OLD.base_url
  THEN
    NEW.disclosure_accepted_at := NULL;
    NEW.disclosure_accepted_by := NULL;
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER ai_provider_connections_reset_disclosure
BEFORE UPDATE OF adapter_type, network_mode, base_url ON ai_provider_connections
FOR EACH ROW EXECUTE FUNCTION reset_ai_provider_disclosure_on_identity_change();

-- +goose StatementBegin
CREATE FUNCTION protect_ai_provider_connection_references()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF (
    NOT NEW.enabled
    OR NEW.disclosure_accepted_at IS NULL
    OR NEW.disclosure_accepted_by IS NULL
  ) AND (
    EXISTS (
      SELECT 1
      FROM ai_policies policy
      JOIN ai_model_profiles profile
        ON profile.id IN (
          policy.summary_model_profile_id,
          policy.reply_draft_model_profile_id,
          policy.similar_suggestions_model_profile_id
        )
        AND profile.msp_id = policy.msp_id
      WHERE policy.enabled
        AND policy.msp_id = NEW.msp_id
        AND profile.connection_id = NEW.id
    )
    OR EXISTS (
      SELECT 1
      FROM ai_generation_jobs job
      JOIN ai_model_profiles profile
        ON profile.id = job.model_profile_id AND profile.msp_id = job.msp_id
      WHERE job.msp_id = NEW.msp_id
        AND job.state IN ('queued', 'running')
        AND profile.connection_id = NEW.id
    )
  ) THEN
    RAISE EXCEPTION 'cannot disable or remove disclosure acceptance from an AI provider connection while enabled policies or active jobs reference it';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER ai_provider_connections_references_valid
AFTER UPDATE OF enabled, adapter_type, network_mode, base_url,
  disclosure_accepted_at, disclosure_accepted_by ON ai_provider_connections
DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW EXECUTE FUNCTION protect_ai_provider_connection_references();

-- +goose StatementBegin
CREATE FUNCTION protect_ai_generation_job_relevant_input_names()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.relevant_input_names IS DISTINCT FROM OLD.relevant_input_names THEN
    RAISE EXCEPTION 'AI generation job relevant input names are immutable';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER ai_generation_jobs_relevant_input_names_immutable
BEFORE UPDATE ON ai_generation_jobs
FOR EACH ROW EXECUTE FUNCTION protect_ai_generation_job_relevant_input_names();

ALTER TABLE ai_usage_records ALTER COLUMN input_units DROP NOT NULL;
ALTER TABLE ai_usage_records ALTER COLUMN input_units DROP DEFAULT;
ALTER TABLE ai_usage_records ALTER COLUMN output_units DROP NOT NULL;
ALTER TABLE ai_usage_records ALTER COLUMN output_units DROP DEFAULT;
ALTER TABLE ai_usage_records ALTER COLUMN cost_minor DROP NOT NULL;
ALTER TABLE ai_usage_records ALTER COLUMN cost_minor DROP DEFAULT;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM ai_usage_records
    WHERE input_units IS NULL OR output_units IS NULL OR cost_minor IS NULL
  ) THEN
    RAISE EXCEPTION 'cannot roll back provider-agnostic AI runtime while usage values are unknown';
  END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE ai_usage_records
  ALTER COLUMN input_units SET DEFAULT 0,
  ALTER COLUMN input_units SET NOT NULL,
  ALTER COLUMN output_units SET DEFAULT 0,
  ALTER COLUMN output_units SET NOT NULL,
  ALTER COLUMN cost_minor SET DEFAULT 0,
  ALTER COLUMN cost_minor SET NOT NULL;

DROP INDEX ai_generation_jobs_claim_idx;
DROP TRIGGER ai_provider_connections_references_valid ON ai_provider_connections;
DROP FUNCTION protect_ai_provider_connection_references();
DROP TRIGGER ai_provider_connections_reset_disclosure ON ai_provider_connections;
DROP FUNCTION reset_ai_provider_disclosure_on_identity_change();
DROP TRIGGER ai_model_profiles_references_valid ON ai_model_profiles;
DROP FUNCTION protect_ai_model_profile_references();
DROP TRIGGER ai_generation_jobs_relevant_input_names_immutable ON ai_generation_jobs;
DROP FUNCTION protect_ai_generation_job_relevant_input_names();
DROP TRIGGER ai_generation_jobs_model_profile_valid ON ai_generation_jobs;
DROP FUNCTION validate_ai_generation_job_model_profile();
DROP TABLE ai_generation_jobs;

DROP TRIGGER ai_policies_model_profiles_valid ON ai_policies;

ALTER TABLE ai_policies
  DROP CONSTRAINT ai_policies_enabled_configured_check,
  DROP CONSTRAINT ai_policies_allowed_features_check,
  DROP CONSTRAINT ai_policies_similar_suggestions_model_profile_msp_fkey,
  DROP CONSTRAINT ai_policies_reply_draft_model_profile_msp_fkey,
  DROP CONSTRAINT ai_policies_summary_model_profile_msp_fkey,
  DROP COLUMN allow_unmetered_unknown,
  DROP COLUMN cost_limit_enabled,
  DROP COLUMN similar_suggestions_model_profile_id,
  DROP COLUMN reply_draft_model_profile_id,
  DROP COLUMN summary_model_profile_id,
  ADD COLUMN provider text,
  ADD COLUMN model text,
  ADD COLUMN provider_connection_id uuid,
  ADD COLUMN provider_disclosure_accepted_at timestamptz,
  ADD COLUMN provider_disclosure_accepted_by uuid;

DROP FUNCTION validate_ai_policy_model_profiles();

-- restore legacy provider policy configuration before reinstating the old enabled check
UPDATE ai_policies policy
SET
  enabled = archived.was_enabled,
  provider = archived.provider,
  model = archived.model,
  provider_connection_id = archived.provider_connection_id,
  provider_disclosure_accepted_at = archived.provider_disclosure_accepted_at,
  provider_disclosure_accepted_by = archived.provider_disclosure_accepted_by
FROM ai_policy_legacy_provider_configurations archived
WHERE archived.policy_id = policy.id;

UPDATE ai_policies policy
SET enabled = false
WHERE policy.enabled
  AND NOT EXISTS (
    SELECT 1
    FROM ai_policy_legacy_provider_configurations archived
    WHERE archived.policy_id = policy.id
  );

ALTER TABLE ai_policies
  ADD CONSTRAINT ai_policies_provider_connection_id_msp_id_fkey
    FOREIGN KEY (provider_connection_id, msp_id) REFERENCES connections(id, msp_id),
  ADD CONSTRAINT ai_policies_allowed_features_check
    CHECK (allowed_features <@ ARRAY['summary', 'reply_draft', 'similar_suggestions']::text[]),
  ADD CONSTRAINT ai_policies_check CHECK (
    NOT enabled OR (
      provider IS NOT NULL
      AND model IS NOT NULL
      AND provider_connection_id IS NOT NULL
      AND provider_disclosure_accepted_at IS NOT NULL
      AND provider_disclosure_accepted_by IS NOT NULL
      AND cardinality(allowed_features) > 0
    )
  );

DROP TABLE ai_model_profiles;
DROP TRIGGER ai_provider_connections_default_timeout ON ai_provider_connections;
DROP FUNCTION default_ai_provider_connection_timeout();
DROP TABLE ai_provider_connections;
DROP TABLE ai_policy_legacy_provider_configurations;

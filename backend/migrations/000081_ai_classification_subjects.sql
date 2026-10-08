-- +goose Up
-- Generalize the durable AI queue without changing the existing work-record
-- API contract: historical jobs retain work_record_id and gain a typed subject.
ALTER TABLE ai_generation_jobs
  ADD COLUMN subject_type text,
  ADD COLUMN subject_id uuid;

UPDATE ai_generation_jobs
SET subject_type = 'work_record', subject_id = work_record_id
WHERE subject_type IS NULL;

ALTER TABLE ai_generation_jobs
  ALTER COLUMN subject_type SET NOT NULL,
  ALTER COLUMN subject_id SET NOT NULL,
  ALTER COLUMN work_record_id DROP NOT NULL,
  ALTER COLUMN client_id DROP NOT NULL,
  ADD CONSTRAINT ai_generation_jobs_subject_type_check
    CHECK (subject_type IN ('work_record', 'task', 'project', 'asset', 'knowledge_article', 'time_entry')),
  ADD CONSTRAINT ai_generation_jobs_subject_legacy_work_record_check
    CHECK ((subject_type = 'work_record') = (work_record_id IS NOT NULL)),
  ADD CONSTRAINT ai_generation_jobs_subject_scope_check
    CHECK (client_id IS NOT NULL OR subject_type = 'knowledge_article');

CREATE INDEX ai_generation_jobs_subject_idx
  ON ai_generation_jobs (msp_id, client_id, subject_type, subject_id, created_at DESC);

ALTER TABLE tag_ai_suggestions
  ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  ADD COLUMN ai_generation_job_id uuid UNIQUE REFERENCES ai_generation_jobs(id),
  ADD COLUMN provider_evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN application_state text NOT NULL DEFAULT 'not_queued'
    CHECK (application_state IN ('not_queued', 'queued', 'applying', 'applied', 'skipped', 'failed')),
  ADD COLUMN application_lease_token uuid,
  ADD COLUMN application_lease_until timestamptz,
  ADD CONSTRAINT tag_ai_suggestions_application_lease_check CHECK (
    (application_state = 'applying') =
      (application_lease_token IS NOT NULL AND application_lease_until IS NOT NULL)
  );

-- Classification results live in tag_ai_suggestion_items rather than the
-- text recommendation table. Preserve the completed-state invariant while
-- allowing that typed result identity.
-- +goose StatementBegin
DO $$
DECLARE
  completed_constraint text;
BEGIN
  SELECT conname INTO completed_constraint
  FROM pg_constraint
  WHERE conrelid = 'ai_generation_jobs'::regclass
    AND contype = 'c'
    AND pg_get_constraintdef(oid) LIKE '%state <>%completed%recommendation_id IS NOT NULL%';
  IF completed_constraint IS NOT NULL THEN
    EXECUTE format('ALTER TABLE ai_generation_jobs DROP CONSTRAINT %I', completed_constraint);
  END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE ai_generation_jobs
  ADD CONSTRAINT ai_generation_jobs_completed_result_check
    CHECK (state <> 'completed' OR recommendation_id IS NOT NULL OR feature = 'classification');

-- Existing constraints list the original feature set. Recreate those narrow
-- allowlists with classification explicitly included rather than weakening
-- them to arbitrary text.
ALTER TABLE ai_model_profiles
  DROP CONSTRAINT ai_model_profiles_supported_features_check,
  ADD CONSTRAINT ai_model_profiles_supported_features_check
    CHECK (supported_features <@ ARRAY['summary', 'reply_draft', 'similar_suggestions', 'classification']::text[]);

ALTER TABLE ai_policies
  DROP CONSTRAINT ai_policies_allowed_features_check,
  DROP CONSTRAINT ai_policies_enabled_configured_check,
  ADD COLUMN classification_model_profile_id uuid,
  ADD CONSTRAINT ai_policies_allowed_features_check
    CHECK (allowed_features <@ ARRAY['summary', 'reply_draft', 'similar_suggestions', 'classification']::text[]),
  ADD CONSTRAINT ai_policies_classification_model_profile_msp_fkey
    FOREIGN KEY (classification_model_profile_id, msp_id) REFERENCES ai_model_profiles(id, msp_id),
  ADD CONSTRAINT ai_policies_enabled_configured_check CHECK (
    NOT enabled OR (
      cardinality(allowed_features) > 0
      AND (NOT ('summary' = ANY(allowed_features)) OR summary_model_profile_id IS NOT NULL)
      AND (NOT ('reply_draft' = ANY(allowed_features)) OR reply_draft_model_profile_id IS NOT NULL)
      AND (NOT ('similar_suggestions' = ANY(allowed_features)) OR similar_suggestions_model_profile_id IS NOT NULL)
      AND (NOT ('classification' = ANY(allowed_features)) OR classification_model_profile_id IS NOT NULL)
    )
  );

ALTER TABLE ai_generation_jobs
  DROP CONSTRAINT ai_generation_jobs_feature_check,
  ADD CONSTRAINT ai_generation_jobs_feature_check
    CHECK (feature IN ('summary', 'reply_draft', 'similar_suggestions', 'classification'));

-- Extend the existing management fences to the new policy reference. These
-- functions are recreated in-place so every policy/model/connection mutation
-- remains safe while classification jobs are queued or running.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION validate_ai_policy_model_profiles()
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
    AND NEW.classification_model_profile_id IS NOT DISTINCT FROM OLD.classification_model_profile_id
  THEN
    RETURN NEW;
  END IF;
  FOR selected_profile_id, selected_feature IN
    SELECT * FROM (VALUES
      (NEW.summary_model_profile_id, 'summary'::text),
      (NEW.reply_draft_model_profile_id, 'reply_draft'::text),
      (NEW.similar_suggestions_model_profile_id, 'similar_suggestions'::text),
      (NEW.classification_model_profile_id, 'classification'::text)
    ) AS configured(model_profile_id, feature)
  LOOP
    IF selected_profile_id IS NOT NULL AND NOT EXISTS (
      SELECT 1 FROM ai_model_profiles profile
      JOIN ai_provider_connections connection
        ON connection.id = profile.connection_id AND connection.msp_id = profile.msp_id
      WHERE profile.id = selected_profile_id AND profile.msp_id = NEW.msp_id
        AND selected_feature = ANY(profile.supported_features)
        AND (NOT NEW.enabled OR (profile.enabled AND connection.enabled
          AND connection.disclosure_accepted_at IS NOT NULL
          AND connection.disclosure_accepted_by IS NOT NULL))
    ) THEN
      RAISE EXCEPTION 'AI policy model profile must support its configured feature and be enabled with its connection when the policy is enabled';
    END IF;
  END LOOP;
  RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION protect_ai_model_profile_references()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  connection_enabled boolean;
BEGIN
  SELECT enabled INTO connection_enabled FROM ai_provider_connections
  WHERE id = NEW.connection_id AND msp_id = NEW.msp_id;
  IF EXISTS (
    SELECT 1 FROM ai_policies policy
    WHERE policy.enabled AND policy.msp_id = NEW.msp_id AND (
      (policy.summary_model_profile_id = NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'summary' = ANY(NEW.supported_features)))
      OR (policy.reply_draft_model_profile_id = NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'reply_draft' = ANY(NEW.supported_features)))
      OR (policy.similar_suggestions_model_profile_id = NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'similar_suggestions' = ANY(NEW.supported_features)))
      OR (policy.classification_model_profile_id = NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'classification' = ANY(NEW.supported_features)))
    )
  ) OR EXISTS (
    SELECT 1 FROM ai_generation_jobs job
    WHERE job.model_profile_id = NEW.id AND job.msp_id = NEW.msp_id
      AND job.state IN ('queued', 'running')
      AND NOT (NEW.enabled AND connection_enabled AND job.feature = ANY(NEW.supported_features))
  ) THEN
    RAISE EXCEPTION 'cannot make an AI model profile incompatible while enabled policies or active jobs reference it';
  END IF;
  RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION protect_ai_provider_connection_references()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF (NOT NEW.enabled OR NEW.disclosure_accepted_at IS NULL OR NEW.disclosure_accepted_by IS NULL) AND (
    EXISTS (
      SELECT 1 FROM ai_policies policy
      JOIN ai_model_profiles profile ON profile.id IN (
        policy.summary_model_profile_id, policy.reply_draft_model_profile_id,
        policy.similar_suggestions_model_profile_id, policy.classification_model_profile_id
      ) AND profile.msp_id = policy.msp_id
      WHERE policy.enabled AND policy.msp_id = NEW.msp_id AND profile.connection_id = NEW.id
    ) OR EXISTS (
      SELECT 1 FROM ai_generation_jobs job
      JOIN ai_model_profiles profile ON profile.id = job.model_profile_id AND profile.msp_id = job.msp_id
      WHERE job.msp_id = NEW.msp_id AND job.state IN ('queued', 'running') AND profile.connection_id = NEW.id
    )
  ) THEN
    RAISE EXCEPTION 'cannot disable or undisclose an AI provider connection while enabled policies or active jobs reference it';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Decisions are historical facts. The item disposition is a convenient
-- current projection; this ledger preserves each human decision immutably.
CREATE TABLE tag_ai_suggestion_decisions (
  id uuid PRIMARY KEY,
  suggestion_id uuid NOT NULL,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  tag_id uuid NOT NULL,
  decision text NOT NULL CHECK (decision IN ('accepted', 'dismissed', 'automatically_applied')),
  decided_by uuid,
  reason text NOT NULL DEFAULT '',
  occurred_at timestamptz NOT NULL DEFAULT now(),
  correlation_id uuid NOT NULL,
  FOREIGN KEY (suggestion_id, msp_id) REFERENCES tag_ai_suggestions(id, msp_id),
  FOREIGN KEY (tag_id, msp_id) REFERENCES tags(id, msp_id),
  FOREIGN KEY (decided_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK ((decision = 'automatically_applied') = (decided_by IS NULL))
);

-- +goose StatementBegin
CREATE FUNCTION reject_tag_ai_suggestion_decision_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  RAISE EXCEPTION 'tag AI suggestion decisions are append-only';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER tag_ai_suggestion_decisions_append_only
BEFORE UPDATE OR DELETE ON tag_ai_suggestion_decisions
FOR EACH ROW EXECUTE FUNCTION reject_tag_ai_suggestion_decision_mutation();

-- +goose Down
DROP TRIGGER tag_ai_suggestion_decisions_append_only ON tag_ai_suggestion_decisions;
DROP FUNCTION reject_tag_ai_suggestion_decision_mutation();
DROP TABLE tag_ai_suggestion_decisions;
-- This release is an intentionally destructive, full migration. Remove every
-- classification runtime record before restoring the older non-classification
-- constraints; this also handles global knowledge jobs whose client is NULL.
DELETE FROM tag_ai_suggestions;
DELETE FROM ai_generation_jobs WHERE feature = 'classification';
UPDATE ai_policies
SET classification_model_profile_id = NULL,
    allowed_features = array_remove(allowed_features, 'classification'),
    enabled = CASE WHEN cardinality(array_remove(allowed_features, 'classification')) = 0 THEN false ELSE enabled END;
UPDATE ai_model_profiles
SET supported_features = array_remove(supported_features, 'classification');
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION validate_ai_policy_model_profiles()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE selected_profile_id uuid; selected_feature text;
BEGIN
  IF TG_OP = 'UPDATE' AND NEW.enabled IS NOT DISTINCT FROM OLD.enabled AND NEW.msp_id IS NOT DISTINCT FROM OLD.msp_id
    AND NEW.summary_model_profile_id IS NOT DISTINCT FROM OLD.summary_model_profile_id
    AND NEW.reply_draft_model_profile_id IS NOT DISTINCT FROM OLD.reply_draft_model_profile_id
    AND NEW.similar_suggestions_model_profile_id IS NOT DISTINCT FROM OLD.similar_suggestions_model_profile_id THEN RETURN NEW; END IF;
  FOR selected_profile_id, selected_feature IN SELECT * FROM (VALUES
    (NEW.summary_model_profile_id, 'summary'::text), (NEW.reply_draft_model_profile_id, 'reply_draft'::text),
    (NEW.similar_suggestions_model_profile_id, 'similar_suggestions'::text)) AS configured(model_profile_id, feature)
  LOOP
    IF selected_profile_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM ai_model_profiles profile JOIN ai_provider_connections connection ON connection.id=profile.connection_id AND connection.msp_id=profile.msp_id WHERE profile.id=selected_profile_id AND profile.msp_id=NEW.msp_id AND selected_feature=ANY(profile.supported_features) AND (NOT NEW.enabled OR (profile.enabled AND connection.enabled AND connection.disclosure_accepted_at IS NOT NULL AND connection.disclosure_accepted_by IS NOT NULL))) THEN RAISE EXCEPTION 'AI policy model profile must support its configured feature and be enabled with its connection when the policy is enabled'; END IF;
  END LOOP; RETURN NEW;
END; $$;

CREATE OR REPLACE FUNCTION protect_ai_model_profile_references()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE connection_enabled boolean;
BEGIN
  SELECT enabled INTO connection_enabled FROM ai_provider_connections WHERE id=NEW.connection_id AND msp_id=NEW.msp_id;
  IF EXISTS (
    SELECT 1 FROM ai_policies policy WHERE policy.enabled AND policy.msp_id=NEW.msp_id AND (
      (policy.summary_model_profile_id=NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'summary'=ANY(NEW.supported_features)))
      OR (policy.reply_draft_model_profile_id=NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'reply_draft'=ANY(NEW.supported_features)))
      OR (policy.similar_suggestions_model_profile_id=NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'similar_suggestions'=ANY(NEW.supported_features)))
    )
  ) OR EXISTS (
    SELECT 1 FROM ai_generation_jobs job WHERE job.model_profile_id=NEW.id AND job.msp_id=NEW.msp_id
      AND job.state IN ('queued','running') AND NOT (NEW.enabled AND connection_enabled AND job.feature=ANY(NEW.supported_features))
  ) THEN RAISE EXCEPTION 'cannot make an AI model profile incompatible while enabled policies or active jobs reference it'; END IF;
  RETURN NEW;
END; $$;

CREATE OR REPLACE FUNCTION protect_ai_provider_connection_references()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF (NOT NEW.enabled OR NEW.disclosure_accepted_at IS NULL OR NEW.disclosure_accepted_by IS NULL) AND (
    EXISTS (
      SELECT 1 FROM ai_policies policy
      JOIN ai_model_profiles profile ON profile.id IN (policy.summary_model_profile_id,policy.reply_draft_model_profile_id,policy.similar_suggestions_model_profile_id) AND profile.msp_id=policy.msp_id
      WHERE policy.enabled AND policy.msp_id=NEW.msp_id AND profile.connection_id=NEW.id
    ) OR EXISTS (
      SELECT 1 FROM ai_generation_jobs job JOIN ai_model_profiles profile ON profile.id=job.model_profile_id AND profile.msp_id=job.msp_id
      WHERE job.msp_id=NEW.msp_id AND job.state IN ('queued','running') AND profile.connection_id=NEW.id
    )
  ) THEN RAISE EXCEPTION 'cannot disable or undisclose an AI provider connection while enabled policies or active jobs reference it'; END IF;
  RETURN NEW;
END; $$;
-- +goose StatementEnd
ALTER TABLE ai_generation_jobs
  DROP CONSTRAINT ai_generation_jobs_completed_result_check,
  DROP CONSTRAINT ai_generation_jobs_feature_check,
  ADD CONSTRAINT ai_generation_jobs_feature_check
    CHECK (feature IN ('summary', 'reply_draft', 'similar_suggestions')),
  ADD CONSTRAINT ai_generation_jobs_completed_result_check
    CHECK (state <> 'completed' OR recommendation_id IS NOT NULL);
ALTER TABLE ai_policies
  DROP CONSTRAINT ai_policies_enabled_configured_check,
  DROP CONSTRAINT ai_policies_classification_model_profile_msp_fkey,
  DROP CONSTRAINT ai_policies_allowed_features_check,
  DROP COLUMN classification_model_profile_id,
  ADD CONSTRAINT ai_policies_allowed_features_check
    CHECK (allowed_features <@ ARRAY['summary', 'reply_draft', 'similar_suggestions']::text[]),
  ADD CONSTRAINT ai_policies_enabled_configured_check CHECK (
    NOT enabled OR (
      cardinality(allowed_features) > 0
      AND (NOT ('summary' = ANY(allowed_features)) OR summary_model_profile_id IS NOT NULL)
      AND (NOT ('reply_draft' = ANY(allowed_features)) OR reply_draft_model_profile_id IS NOT NULL)
      AND (NOT ('similar_suggestions' = ANY(allowed_features)) OR similar_suggestions_model_profile_id IS NOT NULL)
    )
  );
ALTER TABLE ai_model_profiles
  DROP CONSTRAINT ai_model_profiles_supported_features_check,
  ADD CONSTRAINT ai_model_profiles_supported_features_check
    CHECK (supported_features <@ ARRAY['summary', 'reply_draft', 'similar_suggestions']::text[]);
DROP INDEX ai_generation_jobs_subject_idx;
ALTER TABLE tag_ai_suggestions
  DROP CONSTRAINT tag_ai_suggestions_application_lease_check,
  DROP COLUMN application_lease_until,
  DROP COLUMN application_lease_token,
  DROP COLUMN application_state,
  DROP COLUMN updated_at,
  DROP COLUMN provider_evidence,
  DROP COLUMN ai_generation_job_id,
  DROP COLUMN version;
ALTER TABLE ai_generation_jobs
  DROP CONSTRAINT ai_generation_jobs_subject_scope_check,
  DROP CONSTRAINT ai_generation_jobs_subject_legacy_work_record_check,
  DROP CONSTRAINT ai_generation_jobs_subject_type_check,
  ALTER COLUMN client_id SET NOT NULL,
  ALTER COLUMN work_record_id SET NOT NULL,
  DROP COLUMN subject_id,
  DROP COLUMN subject_type;

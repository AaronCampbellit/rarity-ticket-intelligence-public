-- +goose Up
ALTER TABLE ai_model_profiles
  DROP CONSTRAINT ai_model_profiles_supported_features_check,
  ADD CONSTRAINT ai_model_profiles_supported_features_check
    CHECK (supported_features <@ ARRAY['summary', 'reply_draft', 'similar_suggestions', 'classification', 'calendar_recommendation']::text[]);

ALTER TABLE ai_policies
  DROP CONSTRAINT ai_policies_allowed_features_check,
  DROP CONSTRAINT ai_policies_enabled_configured_check,
  ADD COLUMN calendar_recommendation_model_profile_id uuid,
  ADD CONSTRAINT ai_policies_allowed_features_check
    CHECK (allowed_features <@ ARRAY['summary', 'reply_draft', 'similar_suggestions', 'classification', 'calendar_recommendation']::text[]),
  ADD CONSTRAINT ai_policies_calendar_recommendation_model_profile_msp_fkey
    FOREIGN KEY (calendar_recommendation_model_profile_id, msp_id) REFERENCES ai_model_profiles(id, msp_id),
  ADD CONSTRAINT ai_policies_enabled_configured_check CHECK (
    NOT enabled OR (
      cardinality(allowed_features) > 0
      AND (NOT ('summary' = ANY(allowed_features)) OR summary_model_profile_id IS NOT NULL)
      AND (NOT ('reply_draft' = ANY(allowed_features)) OR reply_draft_model_profile_id IS NOT NULL)
      AND (NOT ('similar_suggestions' = ANY(allowed_features)) OR similar_suggestions_model_profile_id IS NOT NULL)
      AND (NOT ('classification' = ANY(allowed_features)) OR classification_model_profile_id IS NOT NULL)
      AND (NOT ('calendar_recommendation' = ANY(allowed_features)) OR calendar_recommendation_model_profile_id IS NOT NULL)
    )
  );

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION validate_ai_policy_model_profiles()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE selected_profile_id uuid; selected_feature text;
BEGIN
  IF TG_OP = 'UPDATE' AND NEW.enabled IS NOT DISTINCT FROM OLD.enabled AND NEW.msp_id IS NOT DISTINCT FROM OLD.msp_id
    AND NEW.summary_model_profile_id IS NOT DISTINCT FROM OLD.summary_model_profile_id
    AND NEW.reply_draft_model_profile_id IS NOT DISTINCT FROM OLD.reply_draft_model_profile_id
    AND NEW.similar_suggestions_model_profile_id IS NOT DISTINCT FROM OLD.similar_suggestions_model_profile_id
    AND NEW.classification_model_profile_id IS NOT DISTINCT FROM OLD.classification_model_profile_id
    AND NEW.calendar_recommendation_model_profile_id IS NOT DISTINCT FROM OLD.calendar_recommendation_model_profile_id THEN RETURN NEW; END IF;
  FOR selected_profile_id, selected_feature IN SELECT * FROM (VALUES
    (NEW.summary_model_profile_id, 'summary'::text), (NEW.reply_draft_model_profile_id, 'reply_draft'::text),
    (NEW.similar_suggestions_model_profile_id, 'similar_suggestions'::text), (NEW.classification_model_profile_id, 'classification'::text),
    (NEW.calendar_recommendation_model_profile_id, 'calendar_recommendation'::text)) AS configured(model_profile_id, feature)
  LOOP
    IF selected_profile_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM ai_model_profiles profile JOIN ai_provider_connections connection ON connection.id=profile.connection_id AND connection.msp_id=profile.msp_id WHERE profile.id=selected_profile_id AND profile.msp_id=NEW.msp_id AND selected_feature=ANY(profile.supported_features) AND (selected_feature <> 'calendar_recommendation' OR profile.zero_cost) AND (NOT NEW.enabled OR (profile.enabled AND connection.enabled AND connection.disclosure_accepted_at IS NOT NULL AND connection.disclosure_accepted_by IS NOT NULL))) THEN RAISE EXCEPTION 'AI policy model profile must support its configured feature and calendar recommendations require a zero-cost model'; END IF;
  END LOOP; RETURN NEW;
END; $$;

CREATE OR REPLACE FUNCTION protect_ai_model_profile_references()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE connection_enabled boolean;
BEGIN
  SELECT enabled INTO connection_enabled FROM ai_provider_connections WHERE id=NEW.connection_id AND msp_id=NEW.msp_id;
  IF EXISTS (SELECT 1 FROM ai_policies policy WHERE policy.enabled AND policy.msp_id=NEW.msp_id AND (
    (policy.summary_model_profile_id=NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'summary'=ANY(NEW.supported_features)))
    OR (policy.reply_draft_model_profile_id=NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'reply_draft'=ANY(NEW.supported_features)))
    OR (policy.similar_suggestions_model_profile_id=NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'similar_suggestions'=ANY(NEW.supported_features)))
    OR (policy.classification_model_profile_id=NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'classification'=ANY(NEW.supported_features)))
    OR (policy.calendar_recommendation_model_profile_id=NEW.id AND NOT (NEW.enabled AND NEW.zero_cost AND connection_enabled AND 'calendar_recommendation'=ANY(NEW.supported_features)))
  )) OR EXISTS (SELECT 1 FROM ai_generation_jobs job WHERE job.model_profile_id=NEW.id AND job.msp_id=NEW.msp_id AND job.state IN ('queued','running') AND NOT (NEW.enabled AND connection_enabled AND job.feature=ANY(NEW.supported_features))) THEN
    RAISE EXCEPTION 'cannot make an AI model profile incompatible while enabled policies or active jobs reference it';
  END IF; RETURN NEW;
END; $$;

CREATE OR REPLACE FUNCTION protect_ai_provider_connection_references()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF (NOT NEW.enabled OR NEW.disclosure_accepted_at IS NULL OR NEW.disclosure_accepted_by IS NULL) AND (
    EXISTS (SELECT 1 FROM ai_policies policy JOIN ai_model_profiles profile ON profile.id IN (policy.summary_model_profile_id,policy.reply_draft_model_profile_id,policy.similar_suggestions_model_profile_id,policy.classification_model_profile_id,policy.calendar_recommendation_model_profile_id) AND profile.msp_id=policy.msp_id WHERE policy.enabled AND policy.msp_id=NEW.msp_id AND profile.connection_id=NEW.id)
    OR EXISTS (SELECT 1 FROM ai_generation_jobs job JOIN ai_model_profiles profile ON profile.id=job.model_profile_id AND profile.msp_id=job.msp_id WHERE job.msp_id=NEW.msp_id AND job.state IN ('queued','running') AND profile.connection_id=NEW.id)
  ) THEN RAISE EXCEPTION 'cannot disable or undisclose an AI provider connection while enabled policies or active jobs reference it'; END IF;
  RETURN NEW;
END; $$;
-- +goose StatementEnd

-- +goose Down
UPDATE ai_policies
SET calendar_recommendation_model_profile_id = NULL,
    allowed_features = array_remove(allowed_features, 'calendar_recommendation'),
    enabled = CASE WHEN cardinality(array_remove(allowed_features, 'calendar_recommendation')) = 0 THEN false ELSE enabled END;
UPDATE ai_model_profiles SET supported_features = array_remove(supported_features, 'calendar_recommendation');

-- Restore the classification-era trigger functions before dropping the column.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION validate_ai_policy_model_profiles()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE selected_profile_id uuid; selected_feature text;
BEGIN
  IF TG_OP = 'UPDATE' AND NEW.enabled IS NOT DISTINCT FROM OLD.enabled AND NEW.msp_id IS NOT DISTINCT FROM OLD.msp_id AND NEW.summary_model_profile_id IS NOT DISTINCT FROM OLD.summary_model_profile_id AND NEW.reply_draft_model_profile_id IS NOT DISTINCT FROM OLD.reply_draft_model_profile_id AND NEW.similar_suggestions_model_profile_id IS NOT DISTINCT FROM OLD.similar_suggestions_model_profile_id AND NEW.classification_model_profile_id IS NOT DISTINCT FROM OLD.classification_model_profile_id THEN RETURN NEW; END IF;
  FOR selected_profile_id, selected_feature IN SELECT * FROM (VALUES (NEW.summary_model_profile_id,'summary'::text),(NEW.reply_draft_model_profile_id,'reply_draft'::text),(NEW.similar_suggestions_model_profile_id,'similar_suggestions'::text),(NEW.classification_model_profile_id,'classification'::text)) AS configured(model_profile_id,feature) LOOP
    IF selected_profile_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM ai_model_profiles profile JOIN ai_provider_connections connection ON connection.id=profile.connection_id AND connection.msp_id=profile.msp_id WHERE profile.id=selected_profile_id AND profile.msp_id=NEW.msp_id AND selected_feature=ANY(profile.supported_features) AND (NOT NEW.enabled OR (profile.enabled AND connection.enabled AND connection.disclosure_accepted_at IS NOT NULL AND connection.disclosure_accepted_by IS NOT NULL))) THEN RAISE EXCEPTION 'AI policy model profile must support its configured feature and be enabled with its connection when the policy is enabled'; END IF;
  END LOOP; RETURN NEW;
END; $$;
CREATE OR REPLACE FUNCTION protect_ai_model_profile_references()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE connection_enabled boolean;
BEGIN
  SELECT enabled INTO connection_enabled FROM ai_provider_connections WHERE id=NEW.connection_id AND msp_id=NEW.msp_id;
  IF EXISTS (SELECT 1 FROM ai_policies policy WHERE policy.enabled AND policy.msp_id=NEW.msp_id AND ((policy.summary_model_profile_id=NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'summary'=ANY(NEW.supported_features))) OR (policy.reply_draft_model_profile_id=NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'reply_draft'=ANY(NEW.supported_features))) OR (policy.similar_suggestions_model_profile_id=NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'similar_suggestions'=ANY(NEW.supported_features))) OR (policy.classification_model_profile_id=NEW.id AND NOT (NEW.enabled AND connection_enabled AND 'classification'=ANY(NEW.supported_features))))) OR EXISTS (SELECT 1 FROM ai_generation_jobs job WHERE job.model_profile_id=NEW.id AND job.msp_id=NEW.msp_id AND job.state IN ('queued','running') AND NOT (NEW.enabled AND connection_enabled AND job.feature=ANY(NEW.supported_features))) THEN RAISE EXCEPTION 'cannot make an AI model profile incompatible while enabled policies or active jobs reference it'; END IF;
  RETURN NEW;
END; $$;
CREATE OR REPLACE FUNCTION protect_ai_provider_connection_references()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF (NOT NEW.enabled OR NEW.disclosure_accepted_at IS NULL OR NEW.disclosure_accepted_by IS NULL) AND (EXISTS (SELECT 1 FROM ai_policies policy JOIN ai_model_profiles profile ON profile.id IN (policy.summary_model_profile_id,policy.reply_draft_model_profile_id,policy.similar_suggestions_model_profile_id,policy.classification_model_profile_id) AND profile.msp_id=policy.msp_id WHERE policy.enabled AND policy.msp_id=NEW.msp_id AND profile.connection_id=NEW.id) OR EXISTS (SELECT 1 FROM ai_generation_jobs job JOIN ai_model_profiles profile ON profile.id=job.model_profile_id AND profile.msp_id=job.msp_id WHERE job.msp_id=NEW.msp_id AND job.state IN ('queued','running') AND profile.connection_id=NEW.id)) THEN RAISE EXCEPTION 'cannot disable or undisclose an AI provider connection while enabled policies or active jobs reference it'; END IF;
  RETURN NEW;
END; $$;
-- +goose StatementEnd

ALTER TABLE ai_policies
  DROP CONSTRAINT ai_policies_enabled_configured_check,
  DROP CONSTRAINT ai_policies_calendar_recommendation_model_profile_msp_fkey,
  DROP CONSTRAINT ai_policies_allowed_features_check,
  DROP COLUMN calendar_recommendation_model_profile_id,
  ADD CONSTRAINT ai_policies_allowed_features_check CHECK (allowed_features <@ ARRAY['summary', 'reply_draft', 'similar_suggestions', 'classification']::text[]),
  ADD CONSTRAINT ai_policies_enabled_configured_check CHECK (NOT enabled OR (cardinality(allowed_features) > 0 AND (NOT ('summary'=ANY(allowed_features)) OR summary_model_profile_id IS NOT NULL) AND (NOT ('reply_draft'=ANY(allowed_features)) OR reply_draft_model_profile_id IS NOT NULL) AND (NOT ('similar_suggestions'=ANY(allowed_features)) OR similar_suggestions_model_profile_id IS NOT NULL) AND (NOT ('classification'=ANY(allowed_features)) OR classification_model_profile_id IS NOT NULL)));
ALTER TABLE ai_model_profiles
  DROP CONSTRAINT ai_model_profiles_supported_features_check,
  ADD CONSTRAINT ai_model_profiles_supported_features_check CHECK (supported_features <@ ARRAY['summary', 'reply_draft', 'similar_suggestions', 'classification']::text[]);

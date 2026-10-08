-- +goose Up
CREATE TABLE ai_policies (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL UNIQUE REFERENCES msp_organizations(id),
  enabled boolean NOT NULL DEFAULT false,
  provider text,
  model text,
  provider_connection_id uuid,
  provider_disclosure_accepted_at timestamptz,
  provider_disclosure_accepted_by uuid,
  allowed_features text[] NOT NULL DEFAULT '{}'::text[],
  monthly_cost_limit_minor bigint NOT NULL DEFAULT 0 CHECK (monthly_cost_limit_minor >= 0),
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  FOREIGN KEY (provider_connection_id, msp_id) REFERENCES connections(id, msp_id),
  CHECK (allowed_features <@ ARRAY['summary', 'reply_draft', 'similar_suggestions']::text[]),
  CHECK (
    NOT enabled OR (
      provider IS NOT NULL
      AND model IS NOT NULL
      AND provider_connection_id IS NOT NULL
      AND provider_disclosure_accepted_at IS NOT NULL
      AND provider_disclosure_accepted_by IS NOT NULL
      AND cardinality(allowed_features) > 0
    )
  )
);

CREATE TABLE ai_recommendations (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid NOT NULL,
  work_record_id uuid NOT NULL,
  feature text NOT NULL,
  provider text NOT NULL,
  model text NOT NULL,
  prompt_version text NOT NULL,
  relevant_inputs jsonb NOT NULL DEFAULT '[]'::jsonb,
  output_ref text NOT NULL,
  candidate_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
  confidence numeric(5,4),
  state text NOT NULL DEFAULT 'pending_human',
  generated_at timestamptz NOT NULL,
  generated_by uuid NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (id, msp_id, client_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  CHECK (feature IN ('summary', 'reply_draft', 'similar_suggestions')),
  CHECK (confidence IS NULL OR confidence BETWEEN 0 AND 1),
  CHECK (state IN ('pending_human', 'accepted', 'rejected'))
);

CREATE TABLE ai_recommendation_decisions (
  id uuid PRIMARY KEY,
  recommendation_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  decision text NOT NULL,
  reason text NOT NULL,
  decided_at timestamptz NOT NULL,
  decided_by uuid NOT NULL,
  applied boolean NOT NULL DEFAULT false,
  sent boolean NOT NULL DEFAULT false,
  UNIQUE (recommendation_id),
  FOREIGN KEY (recommendation_id, msp_id, client_id)
    REFERENCES ai_recommendations(id, msp_id, client_id),
  CHECK (decision IN ('accepted', 'rejected')),
  CHECK (NOT applied),
  CHECK (NOT sent)
);

CREATE TABLE ai_usage_records (
  id uuid PRIMARY KEY,
  recommendation_id uuid NOT NULL REFERENCES ai_recommendations(id),
  msp_id uuid NOT NULL,
  provider text NOT NULL,
  model text NOT NULL,
  input_units bigint NOT NULL DEFAULT 0 CHECK (input_units >= 0),
  output_units bigint NOT NULL DEFAULT 0 CHECK (output_units >= 0),
  cost_minor bigint NOT NULL DEFAULT 0 CHECK (cost_minor >= 0),
  recorded_at timestamptz NOT NULL
);

CREATE INDEX ai_usage_monthly_idx
  ON ai_usage_records (msp_id, recorded_at);

-- +goose Down
DROP TABLE ai_usage_records;
DROP TABLE ai_recommendation_decisions;
DROP TABLE ai_recommendations;
DROP TABLE ai_policies;

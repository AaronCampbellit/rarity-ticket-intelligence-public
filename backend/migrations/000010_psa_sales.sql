-- +goose Up
CREATE TABLE prospects (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  display_id text NOT NULL,
  name text NOT NULL,
  email text,
  phone text,
  lifecycle_state text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, display_id)
);

CREATE TABLE pipelines (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  key text NOT NULL,
  name text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, key)
);

CREATE TABLE pipeline_stages (
  id uuid PRIMARY KEY,
  pipeline_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  key text NOT NULL,
  name text NOT NULL,
  position integer NOT NULL CHECK (position > 0),
  probability integer NOT NULL CHECK (probability BETWEEN 0 AND 100),
  forecast_category text NOT NULL,
  required_fields jsonb NOT NULL DEFAULT '[]'::jsonb,
  allowed_next_stage_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
  requires_proposal boolean NOT NULL DEFAULT false,
  requires_approval boolean NOT NULL DEFAULT false,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (id, msp_id),
  UNIQUE (pipeline_id, key),
  UNIQUE (pipeline_id, position),
  FOREIGN KEY (pipeline_id, msp_id) REFERENCES pipelines(id, msp_id),
  CHECK (forecast_category IN ('pipeline', 'weighted', 'committed', 'closed_won', 'closed_lost'))
);

CREATE TABLE opportunities (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  prospect_id uuid,
  pipeline_id uuid NOT NULL,
  stage_id uuid NOT NULL,
  display_id text NOT NULL,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  amount_minor bigint NOT NULL DEFAULT 0 CHECK (amount_minor >= 0),
  currency char(3) NOT NULL,
  owner_id uuid,
  expected_close_on date,
  committed boolean NOT NULL DEFAULT false,
  lifecycle_state text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, display_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (prospect_id, msp_id) REFERENCES prospects(id, msp_id),
  FOREIGN KEY (pipeline_id, msp_id) REFERENCES pipelines(id, msp_id),
  FOREIGN KEY (stage_id, msp_id) REFERENCES pipeline_stages(id, msp_id),
  FOREIGN KEY (owner_id, msp_id) REFERENCES technicians(id, msp_id),
  CHECK ((client_id IS NULL) <> (prospect_id IS NULL))
);

CREATE INDEX opportunities_scope_stage_idx
  ON opportunities (msp_id, client_id, pipeline_id, stage_id, updated_at DESC);

CREATE TABLE proposals (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid,
  prospect_id uuid,
  opportunity_id uuid NOT NULL,
  display_id text NOT NULL,
  current_version bigint NOT NULL DEFAULT 0 CHECK (current_version >= 0),
  state text NOT NULL DEFAULT 'draft',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, display_id),
  FOREIGN KEY (opportunity_id, msp_id) REFERENCES opportunities(id, msp_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (prospect_id, msp_id) REFERENCES prospects(id, msp_id),
  CHECK ((client_id IS NULL) <> (prospect_id IS NULL)),
  CHECK (state IN ('draft', 'issued', 'accepted', 'declined', 'expired'))
);

CREATE TABLE proposal_versions (
  id uuid PRIMARY KEY,
  proposal_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  version bigint NOT NULL CHECK (version > 0),
  currency char(3) NOT NULL,
  subtotal_minor bigint NOT NULL CHECK (subtotal_minor >= 0),
  tax_minor bigint NOT NULL CHECK (tax_minor >= 0),
  total_minor bigint NOT NULL CHECK (total_minor >= 0),
  cost_minor bigint NOT NULL CHECK (cost_minor >= 0),
  margin_minor bigint NOT NULL,
  terms text NOT NULL DEFAULT '',
  issued_at timestamptz NOT NULL DEFAULT now(),
  issued_by uuid NOT NULL,
  expires_at timestamptz,
  pdf_snapshot_id uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (proposal_id, version),
  FOREIGN KEY (proposal_id, msp_id) REFERENCES proposals(id, msp_id),
  CHECK (total_minor = subtotal_minor + tax_minor)
);

CREATE TRIGGER proposal_versions_immutable
BEFORE UPDATE OR DELETE ON proposal_versions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

CREATE TABLE proposal_lines (
  id uuid PRIMARY KEY,
  proposal_version_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  position integer NOT NULL CHECK (position > 0),
  line_type text NOT NULL,
  description text NOT NULL,
  quantity numeric(18,4) NOT NULL CHECK (quantity >= 0),
  unit_price_minor bigint NOT NULL CHECK (unit_price_minor >= 0),
  unit_cost_minor bigint NOT NULL CHECK (unit_cost_minor >= 0),
  tax_treatment text NOT NULL,
  recurrence text,
  UNIQUE (proposal_version_id, position),
  FOREIGN KEY (proposal_version_id, msp_id) REFERENCES proposal_versions(id, msp_id),
  CHECK (line_type IN ('fixed_fee', 'time_and_materials', 'product_license', 'recurring_service'))
);

CREATE TABLE approvals (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  proposal_version_id uuid NOT NULL,
  approval_type text NOT NULL,
  state text NOT NULL DEFAULT 'pending',
  approver_id uuid,
  signer_name text,
  signer_email text,
  decision_at timestamptz,
  recorded_by uuid,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  pdf_snapshot_id uuid,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (proposal_version_id, approval_type),
  FOREIGN KEY (proposal_version_id, msp_id) REFERENCES proposal_versions(id, msp_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  CHECK (approval_type IN ('internal', 'customer_electronic', 'customer_offline')),
  CHECK (state IN ('pending', 'approved', 'rejected', 'cancelled'))
);

-- +goose Down
DROP TABLE approvals;
DROP TABLE proposal_lines;
DROP TABLE proposal_versions;
DROP TABLE proposals;
DROP TABLE opportunities;
DROP TABLE pipeline_stages;
DROP TABLE pipelines;
DROP TABLE prospects;

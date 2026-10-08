-- +goose Up
CREATE TABLE projects (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  display_id text NOT NULL,
  name text NOT NULL,
  original_proposal_version_id uuid NOT NULL,
  lifecycle_state text NOT NULL DEFAULT 'planned',
  planned_start date,
  planned_end date,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id, client_id),
  UNIQUE (msp_id, client_id, display_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (original_proposal_version_id, msp_id)
    REFERENCES proposal_versions(id, msp_id),
  CHECK (planned_end IS NULL OR planned_start IS NULL OR planned_end >= planned_start)
);

CREATE TABLE phases (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  name text NOT NULL,
  position integer NOT NULL CHECK (position > 0),
  state text NOT NULL DEFAULT 'planned',
  planned_start date,
  planned_end date,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (id, msp_id, client_id),
  UNIQUE (project_id, position),
  FOREIGN KEY (project_id, msp_id, client_id) REFERENCES projects(id, msp_id, client_id)
);

CREATE TABLE resource_plans (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  phase_id uuid,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  role_id uuid,
  team_id uuid,
  starts_on date NOT NULL,
  ends_on date NOT NULL,
  planned_minutes integer NOT NULL CHECK (planned_minutes >= 0),
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (id, msp_id, client_id),
  FOREIGN KEY (project_id, msp_id, client_id) REFERENCES projects(id, msp_id, client_id),
  FOREIGN KEY (phase_id, msp_id, client_id) REFERENCES phases(id, msp_id, client_id),
  FOREIGN KEY (role_id, msp_id) REFERENCES roles(id, msp_id),
  FOREIGN KEY (team_id, msp_id) REFERENCES teams(id, msp_id),
  CHECK (ends_on >= starts_on),
  CHECK ((role_id IS NOT NULL)::integer + (team_id IS NOT NULL)::integer = 1)
);

CREATE TABLE project_budgets (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  phase_id uuid,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  budget_type text NOT NULL,
  currency char(3) NOT NULL,
  labor_minutes bigint NOT NULL DEFAULT 0 CHECK (labor_minutes >= 0),
  labor_cost_minor bigint NOT NULL DEFAULT 0 CHECK (labor_cost_minor >= 0),
  nonlabor_cost_minor bigint NOT NULL DEFAULT 0 CHECK (nonlabor_cost_minor >= 0),
  revenue_minor bigint NOT NULL DEFAULT 0 CHECK (revenue_minor >= 0),
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (project_id, phase_id, budget_type),
  FOREIGN KEY (project_id, msp_id, client_id) REFERENCES projects(id, msp_id, client_id),
  FOREIGN KEY (phase_id, msp_id, client_id) REFERENCES phases(id, msp_id, client_id),
  CHECK (budget_type IN ('original', 'current'))
);

CREATE TABLE cost_actuals (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  phase_id uuid,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  cost_type text NOT NULL,
  description text NOT NULL,
  currency char(3) NOT NULL,
  amount_minor bigint NOT NULL CHECK (amount_minor >= 0),
  committed boolean NOT NULL DEFAULT false,
  incurred_at timestamptz NOT NULL,
  source_ref jsonb NOT NULL DEFAULT '{}'::jsonb,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (id, msp_id, client_id),
  FOREIGN KEY (project_id, msp_id, client_id) REFERENCES projects(id, msp_id, client_id),
  FOREIGN KEY (phase_id, msp_id, client_id) REFERENCES phases(id, msp_id, client_id)
);

CREATE TABLE change_orders (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  display_id text NOT NULL,
  state text NOT NULL DEFAULT 'draft',
  current_version bigint NOT NULL DEFAULT 0 CHECK (current_version >= 0),
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id, client_id),
  UNIQUE (project_id, display_id),
  FOREIGN KEY (project_id, msp_id, client_id) REFERENCES projects(id, msp_id, client_id),
  CHECK (state IN ('draft', 'issued', 'approved', 'rejected', 'applied', 'cancelled'))
);

CREATE TABLE change_order_versions (
  id uuid PRIMARY KEY,
  change_order_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  version bigint NOT NULL CHECK (version > 0),
  description text NOT NULL,
  currency char(3) NOT NULL,
  revenue_delta_minor bigint NOT NULL,
  cost_delta_minor bigint NOT NULL,
  labor_delta_minutes bigint NOT NULL,
  issued_at timestamptz NOT NULL DEFAULT now(),
  issued_by uuid NOT NULL,
  UNIQUE (id, msp_id, client_id),
  UNIQUE (change_order_id, version),
  FOREIGN KEY (change_order_id, msp_id, client_id)
    REFERENCES change_orders(id, msp_id, client_id)
);

CREATE TRIGGER change_order_versions_immutable
BEFORE UPDATE OR DELETE ON change_order_versions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

CREATE TABLE change_order_decisions (
  id uuid PRIMARY KEY,
  change_order_id uuid NOT NULL,
  change_order_version_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  previous_state text NOT NULL,
  decision text NOT NULL,
  override boolean NOT NULL DEFAULT false,
  reason text,
  decided_at timestamptz NOT NULL,
  decided_by uuid NOT NULL,
  UNIQUE (change_order_version_id),
  FOREIGN KEY (change_order_id, msp_id, client_id)
    REFERENCES change_orders(id, msp_id, client_id),
  FOREIGN KEY (change_order_version_id, msp_id, client_id)
    REFERENCES change_order_versions(id, msp_id, client_id),
  CHECK (previous_state = 'issued'),
  CHECK (decision IN ('approved', 'rejected')),
  CHECK (NOT override OR length(btrim(reason)) > 0)
);

CREATE TABLE change_order_applications (
  id uuid PRIMARY KEY,
  change_order_id uuid NOT NULL,
  change_order_version_id uuid NOT NULL,
  project_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  applied_at timestamptz NOT NULL,
  applied_by uuid NOT NULL,
  UNIQUE (change_order_version_id),
  FOREIGN KEY (change_order_id, msp_id, client_id)
    REFERENCES change_orders(id, msp_id, client_id),
  FOREIGN KEY (change_order_version_id, msp_id, client_id)
    REFERENCES change_order_versions(id, msp_id, client_id),
  FOREIGN KEY (project_id, msp_id, client_id)
    REFERENCES projects(id, msp_id, client_id)
);

CREATE TRIGGER change_order_decisions_immutable
BEFORE UPDATE OR DELETE ON change_order_decisions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

CREATE TRIGGER change_order_applications_immutable
BEFORE UPDATE OR DELETE ON change_order_applications
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

CREATE TABLE opportunity_conversions (
  id uuid PRIMARY KEY,
  opportunity_id uuid NOT NULL,
  proposal_version_id uuid NOT NULL,
  project_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  request_key text NOT NULL,
  preview_hash bytea NOT NULL,
  conversion_snapshot jsonb NOT NULL,
  converted_at timestamptz NOT NULL DEFAULT now(),
  converted_by uuid NOT NULL,
  UNIQUE (opportunity_id),
  UNIQUE (msp_id, request_key),
  FOREIGN KEY (opportunity_id, msp_id) REFERENCES opportunities(id, msp_id),
  FOREIGN KEY (proposal_version_id, msp_id) REFERENCES proposal_versions(id, msp_id),
  FOREIGN KEY (project_id, msp_id, client_id) REFERENCES projects(id, msp_id, client_id)
);

-- +goose Down
DROP TABLE opportunity_conversions;
DROP TABLE change_order_applications;
DROP TABLE change_order_decisions;
DROP TABLE change_order_versions;
DROP TABLE change_orders;
DROP TABLE cost_actuals;
DROP TABLE project_budgets;
DROP TABLE resource_plans;
DROP TABLE phases;
DROP TABLE projects;

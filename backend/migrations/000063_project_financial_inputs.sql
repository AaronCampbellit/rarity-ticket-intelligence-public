-- +goose Up
CREATE TABLE technician_labor_cost_rates (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  currency char(3) NOT NULL,
  hourly_rate_minor bigint NOT NULL CHECK (hourly_rate_minor >= 0),
  effective_at timestamptz NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, technician_id, effective_at),
  FOREIGN KEY (technician_id, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id)
);

CREATE INDEX technician_labor_cost_rate_lookup
  ON technician_labor_cost_rates
    (msp_id, technician_id, effective_at DESC);

CREATE TRIGGER technician_labor_cost_rates_immutable
BEFORE UPDATE OR DELETE ON technician_labor_cost_rates
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

CREATE TABLE recognized_billable_work (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  phase_id uuid,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  description text NOT NULL CHECK (length(btrim(description)) > 0),
  currency char(3) NOT NULL,
  amount_minor bigint NOT NULL CHECK (amount_minor >= 0),
  recognized_at timestamptz NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  UNIQUE (id, msp_id, client_id),
  FOREIGN KEY (project_id, msp_id, client_id)
    REFERENCES projects(id, msp_id, client_id),
  FOREIGN KEY (phase_id, msp_id, client_id)
    REFERENCES phases(id, msp_id, client_id),
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id)
);

CREATE INDEX recognized_billable_work_project_lookup
  ON recognized_billable_work
    (msp_id, client_id, project_id, recognized_at, id);

CREATE TRIGGER recognized_billable_work_immutable
BEFORE UPDATE OR DELETE ON recognized_billable_work
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

-- +goose Down
DROP TABLE recognized_billable_work;
DROP TABLE technician_labor_cost_rates;

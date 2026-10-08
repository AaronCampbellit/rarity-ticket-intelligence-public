-- +goose Up
ALTER TABLE phases
  ADD CONSTRAINT phases_calendar_scope_uniq
  UNIQUE (id, project_id, msp_id, client_id);

CREATE TABLE project_milestones (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  project_id uuid NOT NULL,
  phase_id uuid,
  name text NOT NULL CHECK (btrim(name) <> ''),
  description text NOT NULL DEFAULT '',
  priority text NOT NULL DEFAULT 'normal' CHECK (btrim(priority) <> ''),
  due_on date NOT NULL,
  starts_on date,
  ends_on date,
  starts_at timestamptz,
  ends_at timestamptz,
  timezone text,
  all_day boolean NOT NULL DEFAULT true,
  recurrence_rule jsonb,
  status text NOT NULL DEFAULT 'planned'
    CHECK (status IN ('planned', 'in_progress', 'blocked', 'completed', 'cancelled')),
  owner_id uuid,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id, client_id),
  FOREIGN KEY (project_id, msp_id, client_id)
    REFERENCES projects(id, msp_id, client_id) ON DELETE CASCADE,
  FOREIGN KEY (phase_id, project_id, msp_id, client_id)
    REFERENCES phases(id, project_id, msp_id, client_id),
  FOREIGN KEY (owner_id, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (updated_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (calendar_recurrence_rule_is_valid(recurrence_rule)),
  CHECK (
    (all_day AND starts_on IS NULL AND ends_on IS NULL
      AND starts_at IS NULL AND ends_at IS NULL AND timezone IS NULL)
    OR
    (all_day AND starts_on IS NOT NULL AND starts_at IS NULL
      AND ends_at IS NULL AND timezone IS NULL)
    OR
    (NOT all_day AND starts_on IS NULL AND ends_on IS NULL
      AND starts_at IS NOT NULL AND calendar_timezone_is_valid(timezone))
  ),
  CHECK (ends_on IS NULL OR ends_on >= starts_on),
  CHECK (ends_at IS NULL OR ends_at > starts_at)
);

CREATE INDEX project_milestones_project_window_idx
  ON project_milestones (msp_id, client_id, project_id, due_on, starts_on, starts_at)
  WHERE status NOT IN ('completed', 'cancelled');

CREATE TABLE maintenance_windows (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  title text NOT NULL CHECK (btrim(title) <> ''),
  description text NOT NULL DEFAULT '',
  starts_on date,
  ends_on date,
  starts_at timestamptz,
  ends_at timestamptz,
  timezone text,
  all_day boolean NOT NULL,
  recurrence_rule jsonb,
  protected boolean NOT NULL DEFAULT false,
  conflict_policy text NOT NULL DEFAULT 'warning'
    CHECK (conflict_policy IN ('informational', 'warning', 'overrideable_block', 'hard_block')),
  status text NOT NULL DEFAULT 'planned'
    CHECK (status IN ('planned', 'active', 'completed', 'cancelled')),
  owner_id uuid,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  FOREIGN KEY (owner_id, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (updated_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (calendar_recurrence_rule_is_valid(recurrence_rule)),
  CHECK (
    (all_day AND starts_on IS NOT NULL AND starts_at IS NULL
      AND ends_at IS NULL AND timezone IS NULL)
    OR
    (NOT all_day AND starts_on IS NULL AND ends_on IS NULL
      AND starts_at IS NOT NULL AND ends_at IS NOT NULL
      AND calendar_timezone_is_valid(timezone))
  ),
  CHECK (ends_on IS NULL OR ends_on >= starts_on),
  CHECK (ends_at IS NULL OR ends_at > starts_at)
);

CREATE INDEX maintenance_windows_timed_idx
  ON maintenance_windows (msp_id, starts_at, ends_at)
  WHERE NOT all_day AND status IN ('planned', 'active');
CREATE INDEX maintenance_windows_all_day_idx
  ON maintenance_windows (msp_id, starts_on, ends_on)
  WHERE all_day AND status IN ('planned', 'active');

CREATE TABLE maintenance_window_scopes (
  id uuid PRIMARY KEY,
  maintenance_window_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  scope_type text NOT NULL,
  service_id uuid,
  asset_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  UNIQUE NULLS NOT DISTINCT
    (maintenance_window_id, msp_id, client_id, scope_type, service_id, asset_id),
  FOREIGN KEY (maintenance_window_id, msp_id)
    REFERENCES maintenance_windows(id, msp_id) ON DELETE CASCADE,
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (service_id, msp_id, client_id)
    REFERENCES services(id, msp_id, client_id),
  FOREIGN KEY (asset_id, msp_id, client_id)
    REFERENCES assets(id, msp_id, client_id),
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (scope_type IN ('client', 'service', 'asset')),
  CHECK (
    (scope_type = 'client' AND service_id IS NULL AND asset_id IS NULL)
    OR (scope_type = 'service' AND service_id IS NOT NULL AND asset_id IS NULL)
    OR (scope_type = 'asset' AND service_id IS NULL AND asset_id IS NOT NULL)
  )
);

CREATE INDEX maintenance_window_scopes_lookup_idx
  ON maintenance_window_scopes
    (msp_id, client_id, scope_type, service_id, asset_id, maintenance_window_id);

CREATE TABLE commercial_commitments (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  commitment_type text NOT NULL,
  title text NOT NULL CHECK (btrim(title) <> ''),
  description text NOT NULL DEFAULT '',
  vendor_name text NOT NULL CHECK (btrim(vendor_name) <> ''),
  external_reference text NOT NULL DEFAULT '',
  effective_on date NOT NULL,
  notice_on date,
  renewal_on date,
  expiration_on date NOT NULL,
  recurrence_rule jsonb,
  quantity numeric(18,4) NOT NULL DEFAULT 0 CHECK (quantity >= 0),
  cost numeric(18,2) CHECK (cost IS NULL OR cost >= 0),
  currency char(3),
  service_id uuid,
  asset_id uuid,
  contract_id uuid,
  status text NOT NULL DEFAULT 'active'
    CHECK (status IN ('active', 'renewed', 'expired', 'cancelled')),
  owner_id uuid NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id, client_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (owner_id, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (service_id, msp_id, client_id)
    REFERENCES services(id, msp_id, client_id),
  FOREIGN KEY (asset_id, msp_id, client_id)
    REFERENCES assets(id, msp_id, client_id),
  FOREIGN KEY (contract_id, msp_id, client_id)
    REFERENCES contracts(id, msp_id, client_id),
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (updated_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (commitment_type IN ('renewal', 'license')),
  CHECK (calendar_recurrence_rule_is_valid(recurrence_rule)),
  CHECK (expiration_on >= effective_on),
  CHECK (notice_on IS NULL OR notice_on BETWEEN effective_on AND expiration_on),
  CHECK (renewal_on IS NULL OR renewal_on BETWEEN effective_on AND expiration_on),
  CHECK ((cost IS NULL AND currency IS NULL)
    OR (cost IS NOT NULL AND currency ~ '^[A-Z]{3}$'))
);

CREATE INDEX commercial_commitments_date_idx
  ON commercial_commitments
    (msp_id, client_id, commitment_type, expiration_on, renewal_on)
  WHERE status = 'active';

-- +goose Down
DROP TABLE commercial_commitments;
DROP TABLE maintenance_window_scopes;
DROP TABLE maintenance_windows;
DROP TABLE project_milestones;
ALTER TABLE phases DROP CONSTRAINT phases_calendar_scope_uniq;

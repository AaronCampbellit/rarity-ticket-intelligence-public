-- +goose Up
ALTER TABLE teams
  ADD COLUMN workforce_manager_id uuid,
  ADD CONSTRAINT teams_workforce_manager_fk
    FOREIGN KEY (workforce_manager_id, msp_id) REFERENCES technicians(id, msp_id);

CREATE TABLE technician_schedule_versions (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  technician_id uuid NOT NULL,
  timezone text NOT NULL CHECK (calendar_timezone_is_valid(timezone)),
  effective_from date NOT NULL,
  effective_through date,
  version bigint NOT NULL CHECK (version > 0),
  lifecycle_state text NOT NULL DEFAULT 'active'
    CHECK (lifecycle_state IN ('active', 'superseded', 'cancelled')),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (id, msp_id, technician_id),
  UNIQUE (msp_id, technician_id, version),
  FOREIGN KEY (technician_id, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (effective_through IS NULL OR effective_through >= effective_from)
);

CREATE INDEX technician_schedule_versions_effective_idx
  ON technician_schedule_versions
    (msp_id, technician_id, effective_from, effective_through)
  WHERE lifecycle_state = 'active';

CREATE TABLE technician_schedule_windows (
  id uuid PRIMARY KEY,
  schedule_version_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  weekday smallint NOT NULL CHECK (weekday BETWEEN 0 AND 6),
  starts_local time without time zone NOT NULL,
  ends_local time without time zone NOT NULL,
  capacity_percent smallint NOT NULL DEFAULT 100
    CHECK (capacity_percent BETWEEN 1 AND 100),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (schedule_version_id, weekday, starts_local, ends_local),
  FOREIGN KEY (schedule_version_id, msp_id, technician_id)
    REFERENCES technician_schedule_versions(id, msp_id, technician_id)
    ON DELETE CASCADE,
  CHECK (ends_local > starts_local)
);

CREATE INDEX technician_schedule_windows_lookup_idx
  ON technician_schedule_windows
    (msp_id, technician_id, weekday, starts_local, ends_local);

CREATE TABLE technician_schedule_exceptions (
  id uuid PRIMARY KEY,
  schedule_version_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  exception_on date NOT NULL,
  availability_state text NOT NULL
    CHECK (availability_state IN ('available', 'unavailable')),
  all_day boolean NOT NULL,
  starts_local time without time zone,
  ends_local time without time zone,
  capacity_percent smallint NOT NULL DEFAULT 100
    CHECK (capacity_percent BETWEEN 0 AND 100),
  reason text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE NULLS NOT DISTINCT
    (schedule_version_id, msp_id, technician_id, exception_on, starts_local, ends_local),
  FOREIGN KEY (schedule_version_id, msp_id, technician_id)
    REFERENCES technician_schedule_versions(id, msp_id, technician_id)
    ON DELETE CASCADE,
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (updated_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK ((all_day AND starts_local IS NULL AND ends_local IS NULL)
    OR (NOT all_day AND starts_local IS NOT NULL AND ends_local IS NOT NULL
      AND ends_local > starts_local)),
  CHECK (availability_state <> 'available' OR capacity_percent > 0)
);

CREATE INDEX technician_schedule_exceptions_lookup_idx
  ON technician_schedule_exceptions (msp_id, technician_id, exception_on);

CREATE TABLE pto_requests (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  technician_id uuid NOT NULL,
  starts_on date,
  ends_on date,
  starts_at timestamptz,
  ends_at timestamptz,
  timezone text,
  all_day boolean NOT NULL,
  pto_type text NOT NULL
    CHECK (pto_type IN ('vacation', 'sick', 'personal', 'training', 'other')),
  state text NOT NULL
    CHECK (state IN ('requested', 'approved', 'rejected', 'cancelled')),
  manager_id uuid,
  decided_by uuid,
  decided_at timestamptz,
  decision_reason text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  FOREIGN KEY (technician_id, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (manager_id, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (decided_by, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (updated_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (
    (all_day AND starts_on IS NOT NULL AND starts_at IS NULL
      AND ends_at IS NULL AND timezone IS NULL)
    OR
    (NOT all_day AND starts_on IS NULL AND ends_on IS NULL
      AND starts_at IS NOT NULL AND ends_at IS NOT NULL
      AND calendar_timezone_is_valid(timezone))
  ),
  CHECK (ends_on IS NULL OR ends_on >= starts_on),
  CHECK (ends_at IS NULL OR ends_at > starts_at),
  CHECK ((decided_by IS NULL) = (decided_at IS NULL)),
  CHECK (state NOT IN ('approved', 'rejected') OR decided_by IS NOT NULL),
  CHECK (state <> 'rejected' OR btrim(decision_reason) <> '')
);

CREATE INDEX pto_requests_technician_window_idx
  ON pto_requests (msp_id, technician_id, starts_at, ends_at)
  WHERE NOT all_day AND state IN ('requested', 'approved');
CREATE INDEX pto_requests_technician_date_idx
  ON pto_requests (msp_id, technician_id, starts_on, ends_on)
  WHERE all_day AND state IN ('requested', 'approved');
CREATE INDEX pto_requests_pending_idx
  ON pto_requests (msp_id, manager_id, created_at)
  WHERE state = 'requested';

CREATE TABLE calendar_conflict_policies (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  scope_type text NOT NULL CHECK (scope_type IN ('msp', 'team', 'technician')),
  team_id uuid,
  technician_id uuid,
  approved_pto_rule text NOT NULL DEFAULT 'hard_block'
    CHECK (approved_pto_rule IN ('informational', 'warning', 'overrideable_block', 'hard_block')),
  non_working_time_rule text NOT NULL DEFAULT 'hard_block'
    CHECK (non_working_time_rule IN ('informational', 'warning', 'overrideable_block', 'hard_block')),
  protected_maintenance_rule text NOT NULL DEFAULT 'hard_block'
    CHECK (protected_maintenance_rule IN ('informational', 'warning', 'overrideable_block', 'hard_block')),
  ordinary_overbooking_rule text NOT NULL DEFAULT 'overrideable_block'
    CHECK (ordinary_overbooking_rule IN ('informational', 'warning', 'overrideable_block', 'hard_block')),
  effective_from timestamptz NOT NULL DEFAULT now(),
  effective_through timestamptz,
  version bigint NOT NULL CHECK (version > 0),
  lifecycle_state text NOT NULL DEFAULT 'active'
    CHECK (lifecycle_state IN ('active', 'superseded', 'cancelled')),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE NULLS NOT DISTINCT (msp_id, scope_type, team_id, technician_id, version),
  FOREIGN KEY (team_id, msp_id) REFERENCES teams(id, msp_id),
  FOREIGN KEY (technician_id, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (updated_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (
    (scope_type = 'msp' AND team_id IS NULL AND technician_id IS NULL)
    OR (scope_type = 'team' AND team_id IS NOT NULL AND technician_id IS NULL)
    OR (scope_type = 'technician' AND team_id IS NULL AND technician_id IS NOT NULL)
  ),
  CHECK (effective_through IS NULL OR effective_through > effective_from)
);

CREATE INDEX calendar_conflict_policies_effective_idx
  ON calendar_conflict_policies (msp_id, scope_type, effective_from, effective_through)
  WHERE lifecycle_state = 'active';

-- +goose Down
DROP TABLE calendar_conflict_policies;
DROP TABLE pto_requests;
DROP TABLE technician_schedule_exceptions;
DROP TABLE technician_schedule_windows;
DROP TABLE technician_schedule_versions;
ALTER TABLE teams DROP CONSTRAINT teams_workforce_manager_fk;
ALTER TABLE teams DROP COLUMN workforce_manager_id;

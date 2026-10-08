-- +goose Up
CREATE TABLE locations (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  display_id text NOT NULL,
  name text NOT NULL,
  lifecycle_state text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id, client_id),
  UNIQUE (msp_id, client_id, display_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id)
);

CREATE TABLE contacts (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  location_id uuid,
  display_id text NOT NULL,
  display_name text NOT NULL,
  email text,
  phone text,
  lifecycle_state text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id, client_id),
  UNIQUE (msp_id, client_id, display_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (location_id, msp_id, client_id)
    REFERENCES locations(id, msp_id, client_id)
);

CREATE TABLE departments (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  key text NOT NULL,
  name text NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, key)
);

CREATE TABLE teams (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  department_id uuid,
  key text NOT NULL,
  name text NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, key),
  FOREIGN KEY (department_id, msp_id) REFERENCES departments(id, msp_id)
);

CREATE TABLE queues (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  department_id uuid,
  team_id uuid,
  key text NOT NULL,
  name text NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, client_id, key),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (department_id, msp_id) REFERENCES departments(id, msp_id),
  FOREIGN KEY (team_id, msp_id) REFERENCES teams(id, msp_id)
);

CREATE TABLE assets (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  location_id uuid,
  display_id text NOT NULL,
  name text NOT NULL,
  asset_type text NOT NULL,
  lifecycle_state text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id, client_id),
  UNIQUE (msp_id, client_id, display_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (location_id, msp_id, client_id)
    REFERENCES locations(id, msp_id, client_id)
);

CREATE TABLE services (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  display_id text NOT NULL,
  name text NOT NULL,
  lifecycle_state text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id, client_id),
  UNIQUE (msp_id, client_id, display_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id)
);

CREATE TABLE contracts (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  display_id text NOT NULL,
  name text NOT NULL,
  starts_on date NOT NULL,
  ends_on date,
  lifecycle_state text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id, client_id),
  UNIQUE (msp_id, client_id, display_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  CHECK (ends_on IS NULL OR ends_on >= starts_on)
);

CREATE TABLE work_records (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  display_id text NOT NULL,
  record_type text NOT NULL,
  title text NOT NULL,
  description text NOT NULL DEFAULT '',
  status text NOT NULL,
  priority text NOT NULL,
  queue_id uuid,
  primary_owner_id uuid,
  contact_id uuid,
  location_id uuid,
  asset_id uuid,
  service_id uuid,
  contract_id uuid,
  lifecycle_state text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  deleted_at timestamptz,
  deleted_by uuid,
  merged_into_id uuid,
  UNIQUE (id, msp_id, client_id),
  UNIQUE (msp_id, client_id, display_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (queue_id, msp_id) REFERENCES queues(id, msp_id),
  FOREIGN KEY (primary_owner_id, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (contact_id, msp_id, client_id) REFERENCES contacts(id, msp_id, client_id),
  FOREIGN KEY (location_id, msp_id, client_id) REFERENCES locations(id, msp_id, client_id),
  FOREIGN KEY (asset_id, msp_id, client_id) REFERENCES assets(id, msp_id, client_id),
  FOREIGN KEY (service_id, msp_id, client_id) REFERENCES services(id, msp_id, client_id),
  FOREIGN KEY (contract_id, msp_id, client_id) REFERENCES contracts(id, msp_id, client_id),
  FOREIGN KEY (merged_into_id, msp_id, client_id)
    REFERENCES work_records(id, msp_id, client_id),
  CHECK (record_type IN ('incident', 'request', 'change', 'problem')),
  CHECK ((deleted_at IS NULL) = (deleted_by IS NULL)),
  CHECK (merged_into_id IS NULL OR merged_into_id <> id)
);

CREATE INDEX work_records_worklist_idx
  ON work_records (msp_id, client_id, status, priority, updated_at DESC)
  WHERE deleted_at IS NULL;
CREATE INDEX work_records_queue_idx
  ON work_records (msp_id, queue_id, status, updated_at DESC)
  WHERE deleted_at IS NULL;

CREATE TABLE tasks (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  parent_type text NOT NULL,
  parent_id uuid NOT NULL,
  work_record_id uuid,
  parent_task_id uuid,
  title text NOT NULL,
  status text NOT NULL,
  position integer NOT NULL CHECK (position > 0),
  owner_id uuid,
  estimate_minutes integer CHECK (estimate_minutes IS NULL OR estimate_minutes >= 0),
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id, client_id),
  UNIQUE (msp_id, client_id, parent_type, parent_id, parent_task_id, position),
  FOREIGN KEY (work_record_id, msp_id, client_id)
    REFERENCES work_records(id, msp_id, client_id),
  FOREIGN KEY (parent_task_id, msp_id, client_id)
    REFERENCES tasks(id, msp_id, client_id),
  FOREIGN KEY (owner_id, msp_id) REFERENCES technicians(id, msp_id)
  ,CHECK (parent_type IN ('work_record', 'opportunity', 'project', 'phase'))
);

CREATE TABLE task_movement_history (
  id uuid PRIMARY KEY,
  task_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  from_parent_type text NOT NULL,
  from_parent_id uuid NOT NULL,
  to_parent_type text NOT NULL,
  to_parent_id uuid NOT NULL,
  previous_version bigint NOT NULL,
  accepted_version bigint NOT NULL,
  moved_at timestamptz NOT NULL,
  moved_by uuid NOT NULL,
  correlation_id uuid NOT NULL,
  FOREIGN KEY (task_id, msp_id, client_id) REFERENCES tasks(id, msp_id, client_id),
  CHECK (accepted_version = previous_version + 1)
);

CREATE TABLE comments (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  work_record_id uuid NOT NULL,
  author_id uuid NOT NULL,
  visibility text NOT NULL,
  body text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  edited_at timestamptz,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (id, msp_id, client_id),
  FOREIGN KEY (work_record_id, msp_id, client_id)
    REFERENCES work_records(id, msp_id, client_id),
  FOREIGN KEY (author_id, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (visibility IN ('internal', 'client'))
);

CREATE TABLE attachments (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  work_record_id uuid NOT NULL,
  comment_id uuid,
  storage_key text NOT NULL,
  filename text NOT NULL,
  content_type text NOT NULL,
  size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
  sha256 bytea NOT NULL,
  uploaded_by uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (id, msp_id, client_id),
  UNIQUE (msp_id, storage_key),
  FOREIGN KEY (work_record_id, msp_id, client_id)
    REFERENCES work_records(id, msp_id, client_id),
  FOREIGN KEY (comment_id, msp_id, client_id)
    REFERENCES comments(id, msp_id, client_id),
  FOREIGN KEY (uploaded_by, msp_id) REFERENCES technicians(id, msp_id)
);

CREATE TABLE time_entries (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  work_record_id uuid NOT NULL,
  task_id uuid,
  technician_id uuid NOT NULL,
  started_at timestamptz NOT NULL,
  ended_at timestamptz NOT NULL,
  duration_seconds integer NOT NULL CHECK (duration_seconds > 0),
  billable boolean NOT NULL DEFAULT false,
  note text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  UNIQUE (id, msp_id, client_id),
  FOREIGN KEY (work_record_id, msp_id, client_id)
    REFERENCES work_records(id, msp_id, client_id),
  FOREIGN KEY (task_id, msp_id, client_id)
    REFERENCES tasks(id, msp_id, client_id),
  FOREIGN KEY (technician_id, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (ended_at > started_at)
);

CREATE TABLE object_links (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  source_type text NOT NULL,
  source_id uuid NOT NULL,
  target_type text NOT NULL,
  target_id uuid NOT NULL,
  link_type text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  UNIQUE (msp_id, client_id, source_type, source_id, target_type, target_id, link_type),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  CHECK (source_id <> target_id OR source_type <> target_type)
);

-- +goose Down
DROP TABLE object_links;
DROP TABLE time_entries;
DROP TABLE attachments;
DROP TABLE comments;
DROP TABLE task_movement_history;
DROP TABLE tasks;
DROP TABLE work_records;
DROP TABLE contracts;
DROP TABLE services;
DROP TABLE assets;
DROP TABLE queues;
DROP TABLE teams;
DROP TABLE departments;
DROP TABLE contacts;
DROP TABLE locations;

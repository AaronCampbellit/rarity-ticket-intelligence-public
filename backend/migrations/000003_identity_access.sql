-- +goose Up
CREATE TABLE technicians (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  email text NOT NULL,
  display_name text NOT NULL,
  lifecycle_state text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, email),
  CHECK (lifecycle_state IN ('active', 'inactive', 'archived'))
);

CREATE TABLE external_identities (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  provider text NOT NULL,
  issuer text NOT NULL,
  subject text NOT NULL,
  tenant_id text NOT NULL,
  email_at_link text,
  linked_at timestamptz NOT NULL DEFAULT now(),
  last_authenticated_at timestamptz,
  UNIQUE (msp_id, issuer, subject),
  FOREIGN KEY (technician_id, msp_id)
    REFERENCES technicians(id, msp_id)
);

CREATE TABLE roles (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  key text NOT NULL,
  name text NOT NULL,
  system_role boolean NOT NULL DEFAULT false,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, key)
);

CREATE TABLE role_capabilities (
  role_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  capability text NOT NULL,
  PRIMARY KEY (role_id, capability),
  FOREIGN KEY (role_id, msp_id)
    REFERENCES roles(id, msp_id)
    ON DELETE CASCADE
);

CREATE TABLE role_assignments (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  technician_id uuid NOT NULL,
  role_id uuid NOT NULL,
  granted_at timestamptz NOT NULL DEFAULT now(),
  granted_by uuid NOT NULL,
  expires_at timestamptz,
  UNIQUE (msp_id, client_id, technician_id, role_id),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (technician_id, msp_id)
    REFERENCES technicians(id, msp_id),
  FOREIGN KEY (role_id, msp_id)
    REFERENCES roles(id, msp_id)
);

CREATE INDEX role_assignments_technician_scope_idx
  ON role_assignments (msp_id, technician_id, client_id);

CREATE TABLE sessions (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  token_hash bytea NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  revoked_by uuid,
  user_agent_hash bytea,
  ip_prefix inet,
  FOREIGN KEY (technician_id, msp_id)
    REFERENCES technicians(id, msp_id),
  CHECK (expires_at > created_at),
  CHECK ((revoked_at IS NULL) = (revoked_by IS NULL))
);

CREATE INDEX sessions_active_technician_idx
  ON sessions (msp_id, technician_id, expires_at)
  WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE sessions;
DROP TABLE role_assignments;
DROP TABLE role_capabilities;
DROP TABLE roles;
DROP TABLE external_identities;
DROP TABLE technicians;

-- +goose Up
CREATE TABLE break_glass_accounts (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  username text NOT NULL CHECK (username = lower(btrim(username)) AND username <> ''),
  password_hash text NOT NULL CHECK (password_hash LIKE '$2%$%'),
  allowed_cidrs cidr[] NOT NULL DEFAULT '{}',
  enabled boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  last_used_at timestamptz,
  last_used_ip inet,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (msp_id, username),
  FOREIGN KEY (technician_id, msp_id)
    REFERENCES technicians(id, msp_id),
  FOREIGN KEY (created_by, msp_id)
    REFERENCES technicians(id, msp_id)
);

CREATE INDEX break_glass_accounts_enabled_idx
  ON break_glass_accounts (msp_id, username)
  WHERE enabled;

-- +goose Down
DROP TABLE break_glass_accounts;

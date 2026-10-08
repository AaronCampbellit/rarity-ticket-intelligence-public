-- +goose Up
CREATE TABLE team_memberships (
  team_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  lifecycle_state text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL,
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL,
  updated_by uuid NOT NULL,
  PRIMARY KEY (team_id, technician_id),
  FOREIGN KEY (team_id, msp_id) REFERENCES teams(id, msp_id),
  FOREIGN KEY (technician_id, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (lifecycle_state IN ('active', 'inactive'))
);

CREATE INDEX team_memberships_active_technician_idx
  ON team_memberships (msp_id, technician_id, team_id)
  WHERE lifecycle_state = 'active';

INSERT INTO role_capabilities (role_id, msp_id, capability)
SELECT role.id, role.msp_id, required.capability
FROM roles AS role
CROSS JOIN (
  VALUES ('mention.create'), ('mention.read')
) AS required(capability)
WHERE role.key = 'global-admin'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_capabilities AS capability
USING roles AS role
WHERE capability.role_id = role.id
  AND capability.msp_id = role.msp_id
  AND role.key = 'global-admin'
  AND capability.capability IN ('mention.create', 'mention.read');

DROP TABLE team_memberships;

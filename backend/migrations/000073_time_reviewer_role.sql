-- +goose Up
INSERT INTO roles (
  id, msp_id, key, name, system_role, version, created_at, updated_at
)
SELECT
  md5(msp.id::text || ':time_reviewer')::uuid,
  msp.id,
  'time_reviewer',
  'Time reviewer',
  true,
  1,
  now(),
  now()
FROM msp_organizations msp;

INSERT INTO role_capabilities (role_id, msp_id, capability)
SELECT
  role.id,
  role.msp_id,
  capability
FROM roles role
CROSS JOIN unnest(ARRAY[
  'time_entry.read_scoped',
  'time_entry.amend',
  'time_entry.approve',
  'timesheet.review'
]::text[]) capability
WHERE role.key = 'time_reviewer'
  AND role.system_role
  AND role.id = md5(role.msp_id::text || ':time_reviewer')::uuid
ON CONFLICT DO NOTHING;

INSERT INTO role_capabilities (role_id, msp_id, capability)
SELECT
  role.id,
  role.msp_id,
  capability
FROM roles role
CROSS JOIN unnest(ARRAY[
  'time_entry.read_scoped',
  'time_entry.update_own',
  'time_entry.amend',
  'timesheet.read_own',
  'timesheet.review'
]::text[]) capability
WHERE role.key = 'global_admin'
  AND role.system_role
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_capabilities capability
USING roles role
WHERE capability.role_id = role.id
  AND capability.msp_id = role.msp_id
  AND role.key = 'global_admin'
  AND role.system_role
  AND capability.capability IN (
    'time_entry.read_scoped',
    'time_entry.update_own',
    'time_entry.amend',
    'timesheet.read_own',
    'timesheet.review'
  );

DELETE FROM role_assignments assignment
USING roles role
WHERE assignment.role_id = role.id
  AND assignment.msp_id = role.msp_id
  AND role.key = 'time_reviewer'
  AND role.system_role
  AND role.id = md5(role.msp_id::text || ':time_reviewer')::uuid;

DELETE FROM roles
WHERE key = 'time_reviewer'
  AND system_role
  AND id = md5(msp_id::text || ':time_reviewer')::uuid;

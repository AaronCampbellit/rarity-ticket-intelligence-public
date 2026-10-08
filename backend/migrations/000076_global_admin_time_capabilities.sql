-- +goose Up
INSERT INTO role_capabilities (role_id, msp_id, capability)
SELECT
  role.id,
  role.msp_id,
  capability
FROM roles role
CROSS JOIN unnest(ARRAY[
  'time_entry.approve',
  'time_entry.create',
  'time_entry.export',
  'time_entry.read_scoped',
  'time_entry.update_own',
  'time_entry.amend',
  'timesheet.read_own',
  'timesheet.review'
]::text[]) capability
WHERE role.key = 'global-admin'
  AND role.system_role
ON CONFLICT DO NOTHING;

-- +goose Down
-- These grants are part of the Global Administrator baseline for fresh
-- installations, so removing them during a rollback would create drift.
SELECT 1;

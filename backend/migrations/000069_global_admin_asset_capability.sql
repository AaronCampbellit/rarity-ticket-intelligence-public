-- +goose Up
INSERT INTO role_capabilities (role_id, msp_id, capability)
SELECT id, msp_id, 'asset.create'
FROM roles
WHERE key = 'global-admin'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_capabilities AS capability
USING roles AS role
WHERE capability.role_id = role.id
  AND capability.msp_id = role.msp_id
  AND role.key = 'global-admin'
  AND capability.capability = 'asset.create';

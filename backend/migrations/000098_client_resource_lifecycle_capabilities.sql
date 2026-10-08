-- +goose Up
INSERT INTO role_capabilities (role_id, msp_id, capability)
SELECT role.id, role.msp_id, capability.name
FROM roles AS role
CROSS JOIN (
  VALUES
    ('location.update'), ('location.lifecycle'),
    ('contact.update'), ('contact.lifecycle'),
    ('asset.update'), ('asset.lifecycle'),
    ('service.update'), ('service.lifecycle'),
    ('contract.update'), ('contract.lifecycle')
) AS capability(name)
WHERE role.key = 'global-admin'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_capabilities AS role_capability
USING roles AS role
WHERE role_capability.role_id = role.id
  AND role_capability.msp_id = role.msp_id
  AND role.key = 'global-admin'
  AND role_capability.capability IN (
    'location.update', 'location.lifecycle',
    'contact.update', 'contact.lifecycle',
    'asset.update', 'asset.lifecycle',
    'service.update', 'service.lifecycle',
    'contract.update', 'contract.lifecycle'
  );

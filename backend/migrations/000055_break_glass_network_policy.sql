-- +goose Up
ALTER TABLE break_glass_accounts
  ADD CONSTRAINT break_glass_accounts_restricted_network
  CHECK (cardinality(allowed_cidrs) > 0);

-- +goose Down
ALTER TABLE break_glass_accounts
  DROP CONSTRAINT break_glass_accounts_restricted_network;

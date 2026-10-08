-- +goose Up
ALTER TABLE opportunities
  ADD COLUMN custom_fields jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD CONSTRAINT opportunities_custom_fields_object
    CHECK (jsonb_typeof(custom_fields) = 'object');

-- +goose Down
ALTER TABLE opportunities DROP COLUMN custom_fields;

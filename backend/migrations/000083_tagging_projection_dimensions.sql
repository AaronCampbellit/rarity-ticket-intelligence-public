-- +goose Up
ALTER TABLE tag_projection_effects
  ADD COLUMN dimensions jsonb NOT NULL DEFAULT '{}'::jsonb;

CREATE INDEX tag_projection_effects_dimensions_idx
  ON tag_projection_effects USING gin (dimensions);

-- +goose Down
DROP INDEX tag_projection_effects_dimensions_idx;
ALTER TABLE tag_projection_effects DROP COLUMN dimensions;

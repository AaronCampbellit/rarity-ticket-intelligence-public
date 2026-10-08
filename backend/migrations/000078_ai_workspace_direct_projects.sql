-- +goose Up
-- A directly requested project has no proposal provenance. Persisting NULL is
-- more accurate than inventing a proposal version solely to satisfy a legacy
-- conversion-only invariant.
ALTER TABLE projects
  ALTER COLUMN original_proposal_version_id DROP NOT NULL;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM projects
    WHERE original_proposal_version_id IS NULL
  ) THEN
    RAISE EXCEPTION
      'cannot restore required proposal provenance while direct projects exist';
  END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE projects
  ALTER COLUMN original_proposal_version_id SET NOT NULL;

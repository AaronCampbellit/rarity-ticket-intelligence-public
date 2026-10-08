-- +goose Up
ALTER TABLE proposal_versions
  ADD COLUMN requires_internal_approval boolean NOT NULL DEFAULT false;

ALTER TABLE proposal_lines
  ADD COLUMN discount_minor bigint NOT NULL DEFAULT 0 CHECK (discount_minor >= 0),
  ADD COLUMN tax_minor bigint NOT NULL DEFAULT 0 CHECK (tax_minor >= 0),
  ADD COLUMN planned_minutes bigint NOT NULL DEFAULT 0 CHECK (planned_minutes >= 0);

CREATE TABLE proposal_pdf_snapshots (
  id uuid PRIMARY KEY,
  proposal_version_id uuid NOT NULL UNIQUE,
  sha256 char(64) NOT NULL UNIQUE,
  content bytea NOT NULL,
  stored_at timestamptz NOT NULL DEFAULT now(),
  CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  CHECK (octet_length(content) > 0)
);

CREATE TRIGGER proposal_pdf_snapshots_immutable
BEFORE UPDATE OR DELETE ON proposal_pdf_snapshots
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

-- +goose Down
DROP TABLE proposal_pdf_snapshots;

ALTER TABLE proposal_lines
  DROP COLUMN planned_minutes,
  DROP COLUMN tax_minor,
  DROP COLUMN discount_minor;

ALTER TABLE proposal_versions
  DROP COLUMN requires_internal_approval;

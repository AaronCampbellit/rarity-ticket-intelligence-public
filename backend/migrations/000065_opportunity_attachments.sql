-- +goose Up
ALTER TABLE opportunities
  ADD CONSTRAINT opportunities_attachment_scope_unique
    UNIQUE (id, msp_id, client_id);

ALTER TABLE attachments
  ALTER COLUMN work_record_id DROP NOT NULL,
  ADD COLUMN opportunity_id uuid,
  ADD CONSTRAINT attachments_one_parent
    CHECK (
      num_nonnulls(work_record_id, opportunity_id) = 1
      AND (opportunity_id IS NULL OR comment_id IS NULL)
    ),
  ADD CONSTRAINT attachments_opportunity_scope_fk
    FOREIGN KEY (opportunity_id, msp_id, client_id)
    REFERENCES opportunities(id, msp_id, client_id);

CREATE INDEX attachments_opportunity_lookup
  ON attachments (msp_id, client_id, opportunity_id, created_at DESC, id DESC)
  WHERE opportunity_id IS NOT NULL;

-- +goose Down
DROP INDEX attachments_opportunity_lookup;
DELETE FROM attachments WHERE opportunity_id IS NOT NULL;
ALTER TABLE attachments
  DROP CONSTRAINT attachments_opportunity_scope_fk,
  DROP CONSTRAINT attachments_one_parent,
  DROP COLUMN opportunity_id,
  ALTER COLUMN work_record_id SET NOT NULL;
ALTER TABLE opportunities
  DROP CONSTRAINT opportunities_attachment_scope_unique;

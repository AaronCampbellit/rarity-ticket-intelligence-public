-- +goose Up
ALTER TABLE time_entries
  ALTER COLUMN work_record_id DROP NOT NULL,
  ADD CONSTRAINT time_entries_work_or_task_parent_check
    CHECK (work_record_id IS NOT NULL OR task_id IS NOT NULL);

-- +goose Down
DELETE FROM time_entries WHERE work_record_id IS NULL;
ALTER TABLE time_entries
  DROP CONSTRAINT time_entries_work_or_task_parent_check,
  ALTER COLUMN work_record_id SET NOT NULL;

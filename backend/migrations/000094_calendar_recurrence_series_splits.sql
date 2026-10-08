-- +goose Up
ALTER TABLE calendar_recurrence_exceptions
  ADD COLUMN occurrence_scope text NOT NULL DEFAULT 'this_occurrence'
  CHECK (occurrence_scope IN ('this_occurrence', 'this_and_future'));

ALTER TABLE calendar_event_projections
  DROP CONSTRAINT calendar_event_projections_source_type_check,
  ADD CONSTRAINT calendar_event_projections_source_type_check CHECK (source_type IN (
    'work_record', 'task', 'project', 'phase', 'milestone', 'resource_plan',
    'technician_schedule', 'pto', 'maintenance_window',
    'commercial_commitment', 'custom_date'
  ));

ALTER TABLE calendar_proposal_changes
  DROP CONSTRAINT calendar_proposal_changes_source_type_check,
  ADD CONSTRAINT calendar_proposal_changes_source_type_check CHECK (source_type IN (
    'work_record', 'task', 'project', 'phase', 'milestone', 'resource_plan',
    'technician_schedule', 'pto', 'maintenance_window',
    'commercial_commitment', 'custom_date'
  ));

-- +goose Down
ALTER TABLE calendar_recurrence_exceptions
  DROP COLUMN occurrence_scope;

-- Deliberately keep the expanded source-type constraints. Contracting either
-- allowlist would make rollback fail as soon as a resource-plan projection or
-- proposal change exists, and would strand otherwise valid domain data.

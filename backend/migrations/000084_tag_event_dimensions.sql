-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION tag_object_dimensions(p_msp uuid,p_type text,p_id uuid) RETURNS jsonb
LANGUAGE sql STABLE AS $$
SELECT jsonb_strip_nulls(jsonb_build_object(
 'technician_id',COALESCE(
  (SELECT primary_owner_id::text FROM work_records WHERE p_type='work_record' AND id=p_id AND msp_id=p_msp),
  (SELECT owner_id::text FROM tasks WHERE p_type='task' AND id=p_id AND msp_id=p_msp),
  (SELECT technician_id::text FROM time_entries WHERE p_type='time_entry' AND id=p_id AND msp_id=p_msp),
  (SELECT min(owner_id::text) FROM phases WHERE p_type='project' AND project_id=p_id AND msp_id=p_msp)),
 'priority',COALESCE(
  (SELECT priority FROM work_records WHERE p_type='work_record' AND id=p_id AND msp_id=p_msp),
  (SELECT work.priority FROM time_entries entry JOIN work_records work ON work.id=entry.work_record_id AND work.msp_id=entry.msp_id WHERE p_type='time_entry' AND entry.id=p_id AND entry.msp_id=p_msp)),
 'status',COALESCE(
  (SELECT status FROM work_records WHERE p_type='work_record' AND id=p_id AND msp_id=p_msp),
  (SELECT status FROM tasks WHERE p_type='task' AND id=p_id AND msp_id=p_msp),
  (SELECT lifecycle_state FROM projects WHERE p_type='project' AND id=p_id AND msp_id=p_msp),
  (SELECT lifecycle_state FROM assets WHERE p_type='asset' AND id=p_id AND msp_id=p_msp),
  (SELECT state FROM knowledge_articles WHERE p_type='knowledge_article' AND id=p_id AND msp_id=p_msp)),
 'team_ids',COALESCE((SELECT jsonb_agg(team_id::text) FROM (
  SELECT queue.team_id FROM work_records work JOIN queues queue ON queue.id=work.queue_id AND queue.msp_id=work.msp_id WHERE p_type='work_record' AND work.id=p_id AND work.msp_id=p_msp AND queue.team_id IS NOT NULL
  UNION SELECT queue.team_id FROM time_entries entry JOIN work_records work ON work.id=entry.work_record_id AND work.msp_id=entry.msp_id JOIN queues queue ON queue.id=work.queue_id AND queue.msp_id=work.msp_id WHERE p_type='time_entry' AND entry.id=p_id AND entry.msp_id=p_msp AND queue.team_id IS NOT NULL
  UNION SELECT queue.team_id FROM tasks task JOIN work_records work ON task.parent_type='work_record' AND work.id=task.parent_id AND work.msp_id=task.msp_id JOIN queues queue ON queue.id=work.queue_id AND queue.msp_id=work.msp_id WHERE p_type='task' AND task.id=p_id AND task.msp_id=p_msp AND queue.team_id IS NOT NULL
  UNION SELECT ppt.team_id FROM tasks task JOIN phase_participating_teams ppt ON task.parent_type='phase' AND ppt.phase_id=task.parent_id WHERE p_type='task' AND task.id=p_id AND task.msp_id=p_msp
  UNION SELECT plan.team_id FROM resource_plans plan WHERE p_type='project' AND plan.project_id=p_id AND plan.msp_id=p_msp AND plan.team_id IS NOT NULL
 ) teams),'[]'::jsonb)))
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION stamp_tag_event_dimensions() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 NEW.evidence=COALESCE(NEW.evidence,'{}'::jsonb)||jsonb_build_object('projection_dimensions',tag_object_dimensions(NEW.msp_id,NEW.object_type,NEW.object_id));
 IF NEW.object_type='project' THEN
  NEW.evidence=NEW.evidence||jsonb_build_object('project_task_dimensions',COALESCE((
   SELECT jsonb_object_agg(task.id::text,tag_object_dimensions(task.msp_id,'task',task.id))
   FROM tasks task LEFT JOIN phases phase ON task.parent_type='phase' AND phase.id=task.parent_id AND phase.msp_id=task.msp_id
   WHERE task.msp_id=NEW.msp_id AND task.client_id IS NOT DISTINCT FROM NEW.client_id
    AND ((task.parent_type='project' AND task.parent_id=NEW.object_id) OR (task.parent_type='phase' AND phase.project_id=NEW.object_id))
  ),'{}'::jsonb));
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd

CREATE TRIGGER tag_assignment_event_dimensions_before_insert BEFORE INSERT ON tag_assignment_events
FOR EACH ROW EXECUTE FUNCTION stamp_tag_event_dimensions();

-- +goose Down
DROP TRIGGER tag_assignment_event_dimensions_before_insert ON tag_assignment_events;
DROP FUNCTION stamp_tag_event_dimensions();
DROP FUNCTION tag_object_dimensions(uuid,text,uuid);

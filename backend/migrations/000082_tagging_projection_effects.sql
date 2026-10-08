-- +goose Up
ALTER TABLE tag_usage_daily
  DROP CONSTRAINT tag_usage_daily_active_count_check;

ALTER TABLE tag_cooccurrence_daily
  DROP CONSTRAINT tag_cooccurrence_daily_object_count_check;

CREATE TABLE tag_projection_effects (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  source_event_id uuid NOT NULL,
  occurred_at timestamptz NOT NULL,
  object_type text NOT NULL,
  object_id uuid NOT NULL,
  tag_id uuid NOT NULL,
  operation text NOT NULL,
  effect_order smallint NOT NULL,
  assignment_source text NOT NULL,
  inherited boolean NOT NULL,
  origin_object_type text NOT NULL,
  origin_object_id uuid NOT NULL,
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (source_event_id) REFERENCES tag_assignment_events(id),
  FOREIGN KEY (tag_id, msp_id) REFERENCES tags(id, msp_id),
  UNIQUE (
    source_event_id, object_type, object_id, tag_id,
    operation, assignment_source, inherited
  ),
  CHECK (object_type IN ('work_record','task','project','asset','knowledge_article','time_entry')),
  CHECK (origin_object_type IN ('work_record','task','project','asset','knowledge_article','time_entry')),
  CHECK (operation IN ('added','removed')),
  CHECK ((operation = 'removed' AND effect_order = 0) OR (operation = 'added' AND effect_order = 1)),
  CHECK (client_id IS NOT NULL OR object_type = 'knowledge_article')
);

CREATE INDEX tag_projection_effects_object_time_idx
  ON tag_projection_effects (
    msp_id, client_id, object_type, object_id, occurred_at, effect_order, id
  );

CREATE INDEX tag_projection_effects_tag_time_idx
  ON tag_projection_effects (msp_id, client_id, tag_id, occurred_at, id);

-- +goose Down
DROP TABLE tag_projection_effects;

UPDATE tag_usage_daily SET active_count = GREATEST(active_count, 0);
ALTER TABLE tag_usage_daily
  ADD CONSTRAINT tag_usage_daily_active_count_check CHECK (active_count >= 0);

UPDATE tag_cooccurrence_daily SET object_count = GREATEST(object_count, 0);
ALTER TABLE tag_cooccurrence_daily
  ADD CONSTRAINT tag_cooccurrence_daily_object_count_check CHECK (object_count >= 0);

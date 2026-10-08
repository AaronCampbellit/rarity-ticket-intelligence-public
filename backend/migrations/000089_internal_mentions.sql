-- +goose Up
CREATE TABLE mention_access_revisions (
  msp_id uuid PRIMARY KEY REFERENCES msp_organizations(id),
  revision bigint NOT NULL CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO mention_access_revisions(msp_id,revision)
SELECT id,1 FROM msp_organizations;

CREATE TABLE mention_access_revision_history (
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  revision bigint NOT NULL CHECK (revision > 0),
  created_at timestamptz NOT NULL,
  PRIMARY KEY (msp_id,revision)
);

INSERT INTO mention_access_revision_history(msp_id,revision,created_at)
SELECT msp_id,revision,updated_at FROM mention_access_revisions;

-- One authorization revision is allocated per MSP per transaction. The
-- transaction-local JSON map also makes multi-MSP maintenance transactions
-- safe without introducing an unbounded coordination table.
-- +goose StatementBegin
CREATE FUNCTION begin_mention_authorization_revision(p_msp_id uuid)
RETURNS bigint
LANGUAGE plpgsql
AS $$
DECLARE
  revisions jsonb;
  next_revision bigint;
BEGIN
  revisions := COALESCE(
    NULLIF(current_setting('rarity.mention_authorization_revisions',true),'')::jsonb,
    '{}'::jsonb
  );
  IF revisions ? p_msp_id::text THEN
    RETURN (revisions->>p_msp_id::text)::bigint;
  END IF;
  INSERT INTO mention_access_revisions(msp_id,revision,updated_at)
  VALUES(p_msp_id,1,now())
  ON CONFLICT (msp_id) DO UPDATE
    SET revision=mention_access_revisions.revision+1,updated_at=now()
  RETURNING revision INTO next_revision;
  INSERT INTO mention_access_revision_history(msp_id,revision,created_at)
  VALUES(p_msp_id,next_revision,clock_timestamp())
  ON CONFLICT (msp_id,revision) DO NOTHING;
  PERFORM set_config(
    'rarity.mention_authorization_revisions',
    (revisions||jsonb_build_object(p_msp_id::text,next_revision))::text,
    true
  );
  RETURN next_revision;
END;
$$;
-- +goose StatementEnd

-- Mention creation takes this row lock before any parent or authorization
-- evidence lock. An authorization mutation takes the same lock while bumping
-- the revision, so commit order, rather than wall-clock timestamps, decides
-- which side of a revoke boundary an occurrence belongs to.
-- +goose StatementBegin
CREATE FUNCTION lock_mention_authorization_revision(p_msp_id uuid)
RETURNS bigint
LANGUAGE plpgsql
AS $$
DECLARE
  revisions jsonb;
  current_revision bigint;
BEGIN
  revisions := COALESCE(
    NULLIF(current_setting('rarity.mention_authorization_revisions',true),'')::jsonb,
    '{}'::jsonb
  );
  IF revisions ? p_msp_id::text THEN
    RETURN (revisions->>p_msp_id::text)::bigint;
  END IF;
  INSERT INTO mention_access_revisions(msp_id,revision,updated_at)
  VALUES(p_msp_id,1,now())
  ON CONFLICT (msp_id) DO NOTHING;
  SELECT revision INTO current_revision
  FROM mention_access_revisions
  WHERE msp_id=p_msp_id
  FOR UPDATE;
  PERFORM set_config(
    'rarity.mention_authorization_revisions',
    (revisions||jsonb_build_object(p_msp_id::text,current_revision))::text,
    true
  );
  RETURN current_revision;
END;
$$;
-- +goose StatementEnd

CREATE TABLE mention_role_assignment_history (
  assignment_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  client_id uuid,
  role_id uuid NOT NULL,
  expires_at timestamptz,
  valid_from_revision bigint NOT NULL CHECK (valid_from_revision > 0),
  valid_through_revision bigint,
  PRIMARY KEY (assignment_id,valid_from_revision),
  CHECK (valid_through_revision IS NULL OR valid_through_revision >= valid_from_revision)
);

CREATE INDEX mention_role_assignment_history_access_idx
  ON mention_role_assignment_history
    (msp_id,technician_id,role_id,valid_from_revision,valid_through_revision,client_id);

CREATE TABLE mention_role_capability_history (
  role_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  capability text NOT NULL,
  valid_from_revision bigint NOT NULL CHECK (valid_from_revision > 0),
  valid_through_revision bigint,
  PRIMARY KEY (role_id,capability,valid_from_revision),
  CHECK (capability IN ('mention.read','work_record.read','project.read')),
  CHECK (valid_through_revision IS NULL OR valid_through_revision >= valid_from_revision)
);

CREATE INDEX mention_role_capability_history_access_idx
  ON mention_role_capability_history
    (msp_id,role_id,capability,valid_from_revision,valid_through_revision);

INSERT INTO mention_role_assignment_history(
  assignment_id,msp_id,technician_id,client_id,role_id,expires_at,
  valid_from_revision
)
SELECT id,msp_id,technician_id,client_id,role_id,expires_at,1
FROM role_assignments;

INSERT INTO mention_role_capability_history(
  role_id,msp_id,capability,valid_from_revision
)
SELECT role_id,msp_id,capability,1
FROM role_capabilities
WHERE capability IN ('mention.read','work_record.read','project.read');

-- +goose StatementBegin
CREATE FUNCTION track_mention_role_assignment_history()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  boundary_revision bigint;
  affected_msp_id uuid;
BEGIN
  affected_msp_id := CASE WHEN TG_OP='DELETE' THEN OLD.msp_id ELSE NEW.msp_id END;
  boundary_revision := begin_mention_authorization_revision(affected_msp_id);
  IF TG_OP IN ('UPDATE','DELETE') THEN
    UPDATE mention_role_assignment_history
    SET valid_through_revision=boundary_revision
    WHERE assignment_id=OLD.id AND msp_id=OLD.msp_id
      AND valid_through_revision IS NULL;
  END IF;
  IF TG_OP IN ('INSERT','UPDATE') THEN
    INSERT INTO mention_role_assignment_history(
      assignment_id,msp_id,technician_id,client_id,role_id,expires_at,
      valid_from_revision
    ) VALUES(
      NEW.id,NEW.msp_id,NEW.technician_id,NEW.client_id,NEW.role_id,
      NEW.expires_at,boundary_revision
    )
    ON CONFLICT (assignment_id,valid_from_revision) DO UPDATE
      SET technician_id=EXCLUDED.technician_id,client_id=EXCLUDED.client_id,
          role_id=EXCLUDED.role_id,expires_at=EXCLUDED.expires_at,
          valid_through_revision=NULL;
  END IF;
  RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER role_assignments_mention_history
BEFORE INSERT OR UPDATE OR DELETE ON role_assignments
FOR EACH ROW EXECUTE FUNCTION track_mention_role_assignment_history();

-- +goose StatementBegin
CREATE FUNCTION track_mention_role_capability_history()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  boundary_revision bigint;
  affected_msp_id uuid;
  affected_capability text;
BEGIN
  affected_msp_id := CASE WHEN TG_OP='DELETE' THEN OLD.msp_id ELSE NEW.msp_id END;
  affected_capability := CASE WHEN TG_OP='DELETE' THEN OLD.capability ELSE NEW.capability END;
  IF affected_capability NOT IN ('mention.read','work_record.read','project.read')
     AND NOT (TG_OP='UPDATE' AND OLD.capability IN ('mention.read','work_record.read','project.read')) THEN
    RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
  END IF;
  boundary_revision := begin_mention_authorization_revision(affected_msp_id);
  IF TG_OP IN ('UPDATE','DELETE')
     AND OLD.capability IN ('mention.read','work_record.read','project.read') THEN
    UPDATE mention_role_capability_history
    SET valid_through_revision=boundary_revision
    WHERE role_id=OLD.role_id AND msp_id=OLD.msp_id
      AND capability=OLD.capability AND valid_through_revision IS NULL;
  END IF;
  IF TG_OP IN ('INSERT','UPDATE')
     AND NEW.capability IN ('mention.read','work_record.read','project.read') THEN
    INSERT INTO mention_role_capability_history(
      role_id,msp_id,capability,valid_from_revision
    ) VALUES(NEW.role_id,NEW.msp_id,NEW.capability,boundary_revision)
    ON CONFLICT (role_id,capability,valid_from_revision) DO UPDATE
      SET valid_through_revision=NULL;
  END IF;
  RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER role_capabilities_mention_history
BEFORE INSERT OR UPDATE OR DELETE ON role_capabilities
FOR EACH ROW EXECUTE FUNCTION track_mention_role_capability_history();

CREATE TABLE internal_collaboration_sources (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  parent_type text NOT NULL,
  parent_id uuid NOT NULL,
  source_kind text NOT NULL,
  body text NOT NULL,
  mention_tokens jsonb NOT NULL DEFAULT '[]'::jsonb,
  author_id uuid NOT NULL,
  lifecycle_state text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  redacted_at timestamptz,
  UNIQUE (id, msp_id, client_id),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (author_id, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (parent_type IN ('work_record', 'task', 'project')),
  CHECK (source_kind IN ('details', 'comment', 'note')),
  CHECK (jsonb_typeof(mention_tokens) = 'array'),
  CHECK (lifecycle_state IN ('active', 'redacted')),
  CHECK ((lifecycle_state = 'redacted') = (redacted_at IS NOT NULL))
);

CREATE UNIQUE INDEX internal_collaboration_sources_live_details_idx
  ON internal_collaboration_sources (msp_id, client_id, parent_type, parent_id)
  WHERE source_kind = 'details' AND lifecycle_state = 'active';

CREATE INDEX internal_collaboration_sources_parent_idx
  ON internal_collaboration_sources
    (msp_id, client_id, parent_type, parent_id, created_at DESC);

CREATE TABLE mention_occurrences (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  source_id uuid NOT NULL,
  source_revision bigint NOT NULL CHECK (source_revision > 0),
  parent_type text NOT NULL,
  parent_id uuid NOT NULL,
  token_id uuid NOT NULL,
  author_id uuid NOT NULL,
  target_type text NOT NULL,
  target_id uuid NOT NULL,
  mentioned_at timestamptz NOT NULL,
  correlation_id uuid NOT NULL,
  authorization_revision bigint NOT NULL DEFAULT 1 CHECK (authorization_revision > 0),
  causation_id uuid,
  UNIQUE (id, msp_id, client_id),
  FOREIGN KEY (source_id, msp_id, client_id)
    REFERENCES internal_collaboration_sources(id, msp_id, client_id),
  FOREIGN KEY (author_id, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (parent_type IN ('work_record', 'task', 'project')),
  CHECK (target_type IN ('technician', 'team'))
);

CREATE INDEX mention_occurrences_parent_time_idx
  ON mention_occurrences
    (msp_id, client_id, parent_type, parent_id, mentioned_at DESC);

CREATE TABLE mention_recipient_resolutions (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  occurrence_id uuid NOT NULL,
  recipient_id uuid NOT NULL,
  decision text NOT NULL,
  resolution_path text NOT NULL,
  contributing_team_ids uuid[] NOT NULL DEFAULT '{}'::uuid[],
  decided_at timestamptz NOT NULL,
  reason_code text NOT NULL,
  UNIQUE (occurrence_id, recipient_id),
  FOREIGN KEY (occurrence_id, msp_id, client_id)
    REFERENCES mention_occurrences(id, msp_id, client_id),
  FOREIGN KEY (recipient_id, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (decision IN ('eligible', 'excluded')),
  CHECK (resolution_path IN ('direct', 'team', 'both')),
  CHECK (btrim(reason_code) <> '')
);

CREATE TRIGGER mention_occurrences_append_only
BEFORE UPDATE OR DELETE ON mention_occurrences
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

CREATE TRIGGER mention_recipient_resolutions_append_only
BEFORE UPDATE OR DELETE ON mention_recipient_resolutions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

CREATE TABLE mention_items (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  recipient_id uuid NOT NULL,
  parent_type text NOT NULL,
  parent_id uuid NOT NULL,
  latest_occurrence_id uuid NOT NULL,
  authorization_revision bigint NOT NULL DEFAULT 1 CHECK (authorization_revision > 0),
  state text NOT NULL DEFAULT 'unread',
  read_at timestamptz,
  archived_at timestamptz,
  last_mentioned_at timestamptz NOT NULL,
  suppressed_at timestamptz,
  suppression_reason text,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (id, msp_id, client_id),
  UNIQUE (msp_id, recipient_id, parent_type, parent_id),
  FOREIGN KEY (recipient_id, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (latest_occurrence_id, msp_id, client_id)
    REFERENCES mention_occurrences(id, msp_id, client_id),
  CHECK (parent_type IN ('work_record', 'task', 'project')),
  CHECK (state IN ('unread', 'read', 'archived')),
  CHECK ((suppressed_at IS NULL) = (suppression_reason IS NULL))
);

CREATE INDEX mention_items_recipient_widget_idx
  ON mention_items (msp_id, recipient_id, state, last_mentioned_at DESC)
  WHERE suppressed_at IS NULL;

ALTER TABLE notification_deliveries
  ADD COLUMN mention_occurrence_id uuid,
  ADD COLUMN recipient_technician_id uuid,
  ADD CONSTRAINT notification_delivery_mention_occurrence_fk
    FOREIGN KEY (mention_occurrence_id, msp_id, client_id)
    REFERENCES mention_occurrences(id, msp_id, client_id),
  ADD CONSTRAINT notification_delivery_recipient_technician_fk
    FOREIGN KEY (recipient_technician_id, msp_id)
    REFERENCES technicians(id, msp_id),
  ADD CONSTRAINT notification_delivery_mention_client_scope_check
    CHECK (mention_occurrence_id IS NULL OR client_id IS NOT NULL);

CREATE INDEX notification_deliveries_recipient_idx
  ON notification_deliveries
    (msp_id, recipient_technician_id, state, next_attempt_at)
  WHERE recipient_technician_id IS NOT NULL;

CREATE TABLE notification_recipient_preferences (
  msp_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  event_type text NOT NULL,
  email_enabled boolean NOT NULL DEFAULT true,
  teams_enabled boolean NOT NULL DEFAULT true,
  time_zone text NOT NULL DEFAULT 'UTC',
  quiet_hours_start time,
  quiet_hours_end time,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (msp_id, technician_id, event_type),
  FOREIGN KEY (technician_id, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (btrim(event_type) <> ''),
  CHECK (btrim(time_zone) <> ''),
  CHECK ((quiet_hours_start IS NULL) = (quiet_hours_end IS NULL))
);

ALTER TABLE event_outbox
  ADD COLUMN mention_invalidation_sequence bigint GENERATED ALWAYS AS IDENTITY;

CREATE INDEX event_outbox_mention_invalidation_idx
  ON event_outbox (mention_invalidation_sequence);

CREATE TABLE mention_invalidation_event_cursor (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  last_sequence bigint NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
  updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO mention_invalidation_event_cursor(singleton) VALUES(true);

CREATE TABLE mention_invalidation_event_claims (
  event_id uuid PRIMARY KEY REFERENCES event_outbox(event_id),
  event_sequence bigint NOT NULL UNIQUE,
  last_item_id uuid,
  completed_at timestamptz,
  attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX mention_invalidation_event_claims_pending_idx
  ON mention_invalidation_event_claims (event_sequence)
  WHERE completed_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION queue_mention_invalidation_event()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.event_type = ANY (ARRAY[
	    'role.unassigned',
    'role.capabilities_replaced',
    'technician.status.changed',
    'technician.disabled',
    'client.access.changed',
    'client.access.removed',
    'project.visibility.changed',
    'work_record.client.transferred',
    'task.client.transferred',
    'project.client.transferred',
    'internal_collaboration_source.saved',
    'internal_collaboration_source.redacted',
    'work_record.merged',
    'work_record.deleted',
    'task.deleted',
    'project.deleted'
	  ]) AND (
	    NEW.event_type <> 'internal_collaboration_source.saved'
	    OR NEW.data->>'lifecycle_state' = 'redacted'
	  ) AND EXISTS (
	    SELECT 1 FROM mention_access_loss_markers marker
	    WHERE marker.event_id=NEW.event_id
	  ) THEN
    INSERT INTO mention_invalidation_event_claims(event_id,event_sequence)
    VALUES(NEW.event_id,NEW.mention_invalidation_sequence)
    ON CONFLICT (event_id) DO NOTHING;
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER event_outbox_queue_mention_invalidation
AFTER INSERT ON event_outbox
FOR EACH ROW EXECUTE FUNCTION queue_mention_invalidation_event();

CREATE INDEX mention_items_invalidation_scan_idx
  ON mention_items (msp_id, id);

CREATE INDEX mention_items_invalidation_parent_idx
  ON mention_items (msp_id, client_id, parent_type, parent_id, id);

CREATE INDEX mention_items_invalidation_recipient_idx
  ON mention_items (msp_id, recipient_id, id);

CREATE TABLE mention_access_invalidations (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  mention_item_id uuid NOT NULL,
  recipient_id uuid NOT NULL,
  parent_type text NOT NULL,
  parent_id uuid NOT NULL,
  snapshot_occurrence_id uuid NOT NULL,
  access_loss_confirmed boolean NOT NULL DEFAULT false,
  invalidated_at timestamptz NOT NULL,
  reason_code text NOT NULL,
  correlation_id uuid NOT NULL,
  causation_id uuid,
  lease_token uuid,
  lease_until timestamptz,
  completed_at timestamptz,
  attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  safe_error_code text,
  FOREIGN KEY (mention_item_id, msp_id, client_id)
    REFERENCES mention_items(id, msp_id, client_id) ON UPDATE CASCADE,
  FOREIGN KEY (snapshot_occurrence_id) REFERENCES mention_occurrences(id),
  FOREIGN KEY (recipient_id, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  CHECK (parent_type IN ('work_record', 'task', 'project')),
  CHECK (btrim(reason_code) <> ''),
  CHECK ((lease_token IS NULL) = (lease_until IS NULL)),
  UNIQUE (causation_id, mention_item_id)
);

CREATE INDEX mention_access_invalidations_recipient_idx
  ON mention_access_invalidations
    (msp_id, recipient_id, invalidated_at DESC);

CREATE INDEX mention_access_invalidations_confirmed_item_idx
  ON mention_access_invalidations
    (msp_id,client_id,mention_item_id,snapshot_occurrence_id)
  WHERE access_loss_confirmed;

CREATE TABLE mention_access_loss_markers (
  event_id uuid PRIMARY KEY REFERENCES event_outbox(event_id) ON DELETE CASCADE,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  boundary_revision bigint NOT NULL CHECK (boundary_revision > 0),
  scope_kind text NOT NULL,
  client_id uuid,
  recipient_id uuid,
  role_id uuid,
  parent_type text,
  parent_id uuid,
  source_id uuid,
  lost_capabilities text[] NOT NULL DEFAULT '{}'::text[],
  definitive_loss boolean NOT NULL DEFAULT false,
  last_item_id uuid,
  lease_token uuid,
  lease_until timestamptz,
  completed_at timestamptz,
  attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (scope_kind IN ('msp','client','recipient','role','object','source')),
  CHECK (parent_type IS NULL OR parent_type IN ('work_record','task','project')),
  CHECK ((lease_token IS NULL) = (lease_until IS NULL))
);

CREATE INDEX mention_access_loss_markers_scope_idx
  ON mention_access_loss_markers
    (msp_id,scope_kind,client_id,recipient_id,role_id,parent_type,parent_id,event_id)
  WHERE completed_at IS NULL;

CREATE INDEX mention_access_loss_markers_pending_revision_idx
  ON mention_access_loss_markers(msp_id,boundary_revision,event_id)
  WHERE completed_at IS NULL;

CREATE INDEX mention_access_loss_markers_pending_source_idx
  ON mention_access_loss_markers(msp_id,source_id,event_id)
  WHERE completed_at IS NULL AND scope_kind='source';

-- History uses half-open revision intervals. A row closed at revision N is
-- absent from the post-mutation authorization graph at N.
-- +goose StatementBegin
CREATE FUNCTION mention_has_capability_at_revision(
  p_recipient_id uuid,
  p_msp_id uuid,
  p_client_id uuid,
  p_capability text,
  p_revision bigint
) RETURNS boolean
LANGUAGE sql
STABLE
AS $$
SELECT EXISTS (
  SELECT 1
  FROM mention_role_assignment_history assignment
  JOIN mention_access_revision_history boundary
    ON boundary.msp_id=assignment.msp_id
   AND boundary.revision=p_revision
  JOIN mention_role_capability_history capability
    ON capability.role_id=assignment.role_id
   AND capability.msp_id=assignment.msp_id
   AND capability.capability=p_capability
   AND capability.valid_from_revision<=p_revision
   AND (capability.valid_through_revision IS NULL
        OR capability.valid_through_revision>p_revision)
  WHERE assignment.msp_id=p_msp_id
    AND assignment.technician_id=p_recipient_id
    AND (assignment.client_id IS NULL OR assignment.client_id=p_client_id)
    AND assignment.valid_from_revision<=p_revision
    AND (assignment.valid_through_revision IS NULL
         OR assignment.valid_through_revision>p_revision)
    AND (assignment.expires_at IS NULL OR assignment.expires_at>boundary.created_at)
);
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION mention_has_effective_access_at_revision(
  p_recipient_id uuid,
  p_msp_id uuid,
  p_client_id uuid,
  p_parent_type text,
  p_parent_id uuid,
  p_revision bigint
) RETURNS boolean
LANGUAGE sql
STABLE
AS $$
SELECT EXISTS (
  SELECT 1
  FROM technicians technician
  WHERE technician.id=p_recipient_id
    AND technician.msp_id=p_msp_id
    AND technician.lifecycle_state='active'
    AND mention_has_capability_at_revision(
      p_recipient_id,p_msp_id,p_client_id,'mention.read',p_revision
    )
    AND (
      (p_parent_type='work_record'
       AND EXISTS (
         SELECT 1 FROM work_records work
         WHERE work.id=p_parent_id AND work.msp_id=p_msp_id
           AND work.client_id=p_client_id AND work.deleted_at IS NULL
       )
       AND mention_has_capability_at_revision(
         p_recipient_id,p_msp_id,p_client_id,'work_record.read',p_revision
       ))
      OR (p_parent_type='project'
       AND EXISTS (
         SELECT 1 FROM projects project
         WHERE project.id=p_parent_id AND project.msp_id=p_msp_id
           AND project.client_id=p_client_id
       )
       AND mention_has_capability_at_revision(
         p_recipient_id,p_msp_id,p_client_id,'project.read',p_revision
       ))
      OR (p_parent_type='task' AND EXISTS (
        SELECT 1
        FROM tasks task
        WHERE task.id=p_parent_id AND task.msp_id=p_msp_id
          AND task.client_id=p_client_id
          AND (
            (task.work_record_id IS NOT NULL
             AND EXISTS (
               SELECT 1 FROM work_records work
               WHERE work.id=task.work_record_id AND work.msp_id=task.msp_id
                 AND work.client_id=task.client_id AND work.deleted_at IS NULL
             )
             AND mention_has_capability_at_revision(
               p_recipient_id,task.msp_id,task.client_id,'work_record.read',p_revision
             ))
            OR (task.parent_type='project'
             AND EXISTS (
               SELECT 1 FROM projects project
               WHERE project.id=task.parent_id AND project.msp_id=task.msp_id
                 AND project.client_id=task.client_id
             )
             AND mention_has_capability_at_revision(
               p_recipient_id,task.msp_id,task.client_id,'project.read',p_revision
             ))
            OR (task.parent_type='phase'
             AND EXISTS (
               SELECT 1 FROM phases phase
               JOIN projects project
                 ON project.id=phase.project_id AND project.msp_id=phase.msp_id
                AND project.client_id=phase.client_id
               WHERE phase.id=task.parent_id AND phase.msp_id=task.msp_id
                 AND phase.client_id=task.client_id
             )
             AND mention_has_capability_at_revision(
               p_recipient_id,task.msp_id,task.client_id,'project.read',p_revision
             ))
          )
      ))
    )
);
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION capture_mention_access_loss_marker()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  boundary_revision bigint;
  marker_scope text;
  marker_client_id uuid;
  marker_recipient_id uuid;
  marker_role_id uuid;
  marker_parent_type text;
  marker_parent_id uuid;
  marker_source_id uuid;
  removed_capabilities text[] := '{}'::text[];
  is_definitive boolean := false;
BEGIN
  -- Grants and membership-only changes cannot revoke object visibility.
  IF NEW.event_type IN ('role.assigned','team.members.replaced')
     OR (NEW.event_type='internal_collaboration_source.saved'
         AND NEW.data->>'lifecycle_state'<>'redacted') THEN
    RETURN NEW;
  END IF;

  IF NEW.event_type NOT IN (
    'role.unassigned','role.capabilities_replaced',
    'technician.status.changed','technician.disabled',
    'client.access.changed','client.access.removed',
    'project.visibility.changed','work_record.client.transferred',
    'task.client.transferred','project.client.transferred',
    'internal_collaboration_source.saved','internal_collaboration_source.redacted',
    'work_record.merged','work_record.deleted','task.deleted','project.deleted'
  ) THEN
    RETURN NEW;
  END IF;

  boundary_revision := lock_mention_authorization_revision(NEW.msp_id);

  IF NEW.event_type='role.capabilities_replaced' THEN
    marker_scope := 'role';
    marker_role_id := NEW.subject_id;
    SELECT COALESCE(array_agg(previous.capability ORDER BY previous.capability),'{}'::text[])
      INTO removed_capabilities
    FROM mention_role_capability_history previous
    WHERE previous.msp_id=NEW.msp_id
      AND previous.role_id=NEW.subject_id
      AND previous.capability IN ('mention.read','work_record.read','project.read')
      AND previous.valid_from_revision<boundary_revision
      AND (previous.valid_through_revision IS NULL
           OR previous.valid_through_revision>=boundary_revision)
      AND NOT EXISTS (
        SELECT 1 FROM mention_role_capability_history current
        WHERE current.msp_id=previous.msp_id
          AND current.role_id=previous.role_id
          AND current.capability=previous.capability
          AND current.valid_from_revision<=boundary_revision
          AND (current.valid_through_revision IS NULL
               OR current.valid_through_revision>boundary_revision)
      );
    IF cardinality(removed_capabilities)=0 THEN RETURN NEW; END IF;
  ELSIF NEW.event_type='role.unassigned' THEN
    marker_scope := 'recipient';
    SELECT technician_id,client_id,role_id
      INTO marker_recipient_id,marker_client_id,marker_role_id
    FROM mention_role_assignment_history
    WHERE assignment_id=NEW.subject_id AND msp_id=NEW.msp_id
      AND valid_through_revision=boundary_revision
    ORDER BY valid_from_revision DESC LIMIT 1;
    IF marker_recipient_id IS NULL THEN RETURN NEW; END IF;
  ELSIF NEW.event_type IN ('technician.status.changed','technician.disabled') THEN
    marker_scope := 'recipient';
    marker_recipient_id := NEW.subject_id;
    is_definitive := true;
  ELSIF NEW.event_type IN ('client.access.changed','client.access.removed') THEN
    marker_scope := 'recipient';
    marker_client_id := NEW.client_id;
    marker_recipient_id := COALESCE(NULLIF(NEW.data->>'technician_id','')::uuid,NEW.subject_id);
    is_definitive := true;
  ELSIF NEW.event_type IN ('internal_collaboration_source.saved','internal_collaboration_source.redacted') THEN
    marker_scope := 'source';
    marker_source_id := NEW.subject_id;
    marker_client_id := NEW.client_id;
    marker_parent_type := NULLIF(NEW.data->>'parent_type','');
    marker_parent_id := NULLIF(NEW.data->>'parent_id','')::uuid;
    is_definitive := true;
  ELSE
    marker_scope := 'object';
    marker_client_id := NEW.client_id;
    marker_parent_type := NEW.subject_type;
    marker_parent_id := NEW.subject_id;
    is_definitive := true;
  END IF;

  INSERT INTO mention_access_loss_markers(
    event_id,msp_id,boundary_revision,scope_kind,client_id,recipient_id,
    role_id,parent_type,parent_id,source_id,lost_capabilities,
    definitive_loss,created_at,updated_at
  ) VALUES(
    NEW.event_id,NEW.msp_id,boundary_revision,marker_scope,marker_client_id,
    marker_recipient_id,marker_role_id,marker_parent_type,marker_parent_id,
    marker_source_id,removed_capabilities,is_definitive,NEW.occurred_at,NEW.occurred_at
  )
  ON CONFLICT (event_id) DO NOTHING;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER event_outbox_capture_mention_access_loss_marker
AFTER INSERT ON event_outbox
FOR EACH ROW EXECUTE FUNCTION capture_mention_access_loss_marker();

-- +goose StatementBegin
CREATE FUNCTION mention_access_loss_marker_applies(
  p_marker_event_id uuid,
  p_item_id uuid
) RETURNS boolean
LANGUAGE sql
STABLE
AS $$
SELECT EXISTS (
  SELECT 1
  FROM mention_access_loss_markers marker
  JOIN event_outbox event ON event.event_id=marker.event_id
  JOIN mention_items item ON item.id=p_item_id AND item.msp_id=marker.msp_id
  WHERE marker.event_id=p_marker_event_id
    AND item.authorization_revision<=marker.boundary_revision
    AND (
      event.event_type IN (
        'work_record.client.transferred','task.client.transferred',
        'project.client.transferred'
      )
      OR marker.client_id IS NULL OR item.client_id=marker.client_id
    )
    AND (
      marker.scope_kind='msp'
      OR (marker.scope_kind='client' AND item.client_id=marker.client_id)
      OR (marker.scope_kind='recipient'
          AND item.recipient_id=marker.recipient_id)
      OR (marker.scope_kind='role' AND EXISTS (
        SELECT 1
        FROM mention_role_assignment_history assignment
        WHERE assignment.msp_id=item.msp_id
          AND assignment.technician_id=item.recipient_id
          AND assignment.role_id=marker.role_id
          AND (assignment.client_id IS NULL OR assignment.client_id=item.client_id)
          AND assignment.valid_from_revision<marker.boundary_revision
          AND (assignment.valid_through_revision IS NULL
               OR assignment.valid_through_revision>=marker.boundary_revision)
      ))
      OR (marker.scope_kind='source' AND EXISTS (
        SELECT 1 FROM mention_occurrences occurrence
        WHERE occurrence.id=item.latest_occurrence_id
          AND occurrence.source_id=marker.source_id
      ))
      OR (marker.scope_kind='object' AND (
        (item.parent_type=marker.parent_type AND item.parent_id=marker.parent_id)
        OR (marker.parent_type='work_record' AND item.parent_type='task'
            AND EXISTS (
              SELECT 1 FROM tasks task
              WHERE task.id=item.parent_id AND task.msp_id=item.msp_id
                AND (
                  task.work_record_id=marker.parent_id
                  OR (event.event_type='work_record.merged'
                      AND task.work_record_id::text=event.data->>'winner_id')
                )
            ))
        OR (marker.parent_type='project' AND item.parent_type='task'
            AND EXISTS (
              SELECT 1 FROM tasks task
              WHERE task.id=item.parent_id AND task.msp_id=item.msp_id
                AND (
                  (task.parent_type='project' AND task.parent_id=marker.parent_id)
                  OR (task.parent_type='phase' AND EXISTS (
                    SELECT 1 FROM phases phase
                    WHERE phase.id=task.parent_id AND phase.msp_id=task.msp_id
                      AND phase.project_id=marker.parent_id
                  ))
                )
            ))
      ))
    )
);
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION mention_access_loss_marker_applies(uuid,uuid);
DROP TRIGGER event_outbox_capture_mention_access_loss_marker ON event_outbox;
DROP FUNCTION capture_mention_access_loss_marker();
DROP FUNCTION mention_has_effective_access_at_revision(uuid,uuid,uuid,text,uuid,bigint);
DROP FUNCTION mention_has_capability_at_revision(uuid,uuid,uuid,text,bigint);
DROP TABLE mention_access_loss_markers;
DROP TABLE mention_access_invalidations;
DROP INDEX mention_items_invalidation_recipient_idx;
DROP INDEX mention_items_invalidation_parent_idx;
DROP INDEX mention_items_invalidation_scan_idx;
DROP TRIGGER event_outbox_queue_mention_invalidation ON event_outbox;
DROP FUNCTION queue_mention_invalidation_event();
DROP TABLE mention_invalidation_event_claims;
DROP TABLE mention_invalidation_event_cursor;
DROP INDEX event_outbox_mention_invalidation_idx;
ALTER TABLE event_outbox DROP COLUMN mention_invalidation_sequence;
DROP TABLE notification_recipient_preferences;
DROP INDEX notification_deliveries_recipient_idx;
ALTER TABLE notification_deliveries
  DROP CONSTRAINT notification_delivery_mention_client_scope_check,
  DROP CONSTRAINT notification_delivery_recipient_technician_fk,
  DROP CONSTRAINT notification_delivery_mention_occurrence_fk,
  DROP COLUMN recipient_technician_id,
  DROP COLUMN mention_occurrence_id;
DROP TABLE mention_items;
DROP TRIGGER mention_recipient_resolutions_append_only ON mention_recipient_resolutions;
DROP TRIGGER mention_occurrences_append_only ON mention_occurrences;
DROP TABLE mention_recipient_resolutions;
DROP TABLE mention_occurrences;
DROP TABLE internal_collaboration_sources;
DROP TRIGGER role_capabilities_mention_history ON role_capabilities;
DROP FUNCTION track_mention_role_capability_history();
DROP TRIGGER role_assignments_mention_history ON role_assignments;
DROP FUNCTION track_mention_role_assignment_history();
DROP TABLE mention_role_capability_history;
DROP TABLE mention_role_assignment_history;
DROP FUNCTION lock_mention_authorization_revision(uuid);
DROP FUNCTION begin_mention_authorization_revision(uuid);
DROP TABLE mention_access_revision_history;
DROP TABLE mention_access_revisions;

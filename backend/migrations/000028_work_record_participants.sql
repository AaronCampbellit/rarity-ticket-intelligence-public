-- +goose Up
CREATE TABLE work_record_participants (
  id uuid PRIMARY KEY,
  work_record_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  role text NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  added_at timestamptz NOT NULL,
  added_by uuid NOT NULL,
  removed_at timestamptz,
  removed_by uuid,
  UNIQUE (id, msp_id, client_id),
  FOREIGN KEY (work_record_id, msp_id, client_id)
    REFERENCES work_records(id, msp_id, client_id),
  FOREIGN KEY (technician_id, msp_id)
    REFERENCES technicians(id, msp_id),
  CHECK (role IN ('collaborator', 'reviewer', 'escalation', 'watcher')),
  CHECK ((removed_at IS NULL) = (removed_by IS NULL)),
  CHECK (removed_at IS NULL OR removed_at >= added_at)
);

CREATE UNIQUE INDEX work_record_participants_active_idx
  ON work_record_participants (work_record_id, technician_id, role)
  WHERE removed_at IS NULL;

CREATE INDEX work_record_participants_work_idx
  ON work_record_participants (msp_id, client_id, work_record_id, role)
  WHERE removed_at IS NULL;

-- +goose Down
DROP TABLE work_record_participants;

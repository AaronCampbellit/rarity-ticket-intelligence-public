-- +goose Up
CREATE TABLE technician_availability_windows (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  starts_at timestamptz NOT NULL,
  ends_at timestamptz NOT NULL,
  available_minutes bigint NOT NULL CHECK (available_minutes >= 0),
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  FOREIGN KEY (technician_id, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (ends_at > starts_at),
  CHECK (available_minutes <= EXTRACT(EPOCH FROM (ends_at - starts_at)) / 60)
);

CREATE INDEX technician_availability_window_lookup
  ON technician_availability_windows (msp_id, technician_id, starts_at, ends_at);

-- +goose Down
DROP TABLE technician_availability_windows;

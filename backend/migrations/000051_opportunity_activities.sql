-- +goose Up
CREATE TABLE opportunity_activities (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  opportunity_id uuid NOT NULL,
  kind text NOT NULL,
  summary text NOT NULL,
  details text NOT NULL DEFAULT '',
  occurred_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  FOREIGN KEY (opportunity_id, msp_id) REFERENCES opportunities(id, msp_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  CHECK (kind IN ('note', 'call', 'email', 'meeting'))
);

CREATE INDEX opportunity_activities_timeline_idx
  ON opportunity_activities (msp_id, opportunity_id, occurred_at DESC, id DESC);

-- +goose Down
DROP TABLE opportunity_activities;

-- +goose Up
CREATE TABLE tag_report_snapshots (
 id uuid PRIMARY KEY,
 msp_id uuid NOT NULL REFERENCES msp_organizations(id),
 client_id uuid NOT NULL,
 report_kind text NOT NULL,
 query_hash text NOT NULL,
 payload jsonb NOT NULL,
 projection_as_of timestamptz NOT NULL,
 expires_at timestamptz NOT NULL DEFAULT (now()+interval '30 minutes'),
 FOREIGN KEY (client_id,msp_id) REFERENCES client_organizations(id,msp_id)
);
CREATE INDEX tag_report_snapshots_expiry_idx ON tag_report_snapshots(expires_at);

-- +goose Down
DROP TABLE tag_report_snapshots;

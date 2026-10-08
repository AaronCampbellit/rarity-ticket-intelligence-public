-- +goose Up
CREATE TABLE routing_rule_sets (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  current_version bigint NOT NULL CHECK (current_version > 0),
  created_at timestamptz NOT NULL,
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL,
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id)
);

CREATE TABLE routing_rule_set_versions (
  rule_set_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  version bigint NOT NULL CHECK (version > 0),
  published_at timestamptz NOT NULL,
  published_by uuid NOT NULL,
  PRIMARY KEY (rule_set_id, version),
  FOREIGN KEY (rule_set_id, msp_id)
    REFERENCES routing_rule_sets(id, msp_id)
);

CREATE TABLE routing_rule_versions (
  rule_set_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  version bigint NOT NULL,
  rule_id uuid NOT NULL,
  position integer NOT NULL CHECK (position > 0),
  client_id uuid,
  record_type text,
  priority text,
  queue_id uuid NOT NULL,
  PRIMARY KEY (rule_set_id, version, rule_id),
  UNIQUE (rule_set_id, version, rule_id, queue_id),
  UNIQUE (rule_set_id, version, position),
  FOREIGN KEY (rule_set_id, version)
    REFERENCES routing_rule_set_versions(rule_set_id, version),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (queue_id, msp_id)
    REFERENCES queues(id, msp_id),
  CHECK (record_type IS NULL OR record_type IN ('incident', 'request', 'change', 'problem')),
  CHECK (priority IS NULL OR length(btrim(priority)) > 0)
);

CREATE TRIGGER routing_rule_set_versions_immutable
BEFORE UPDATE OR DELETE ON routing_rule_set_versions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

CREATE TRIGGER routing_rule_versions_immutable
BEFORE UPDATE OR DELETE ON routing_rule_versions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

CREATE TABLE work_record_routing (
  work_record_id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  rule_set_id uuid NOT NULL,
  rule_set_version bigint NOT NULL,
  rule_id uuid NOT NULL,
  queue_id uuid NOT NULL,
  decided_at timestamptz NOT NULL,
  explanation text NOT NULL,
  FOREIGN KEY (work_record_id, msp_id, client_id)
    REFERENCES work_records(id, msp_id, client_id),
  FOREIGN KEY (rule_set_id, rule_set_version, rule_id, queue_id)
    REFERENCES routing_rule_versions(rule_set_id, version, rule_id, queue_id),
  FOREIGN KEY (queue_id, msp_id)
    REFERENCES queues(id, msp_id)
);

-- +goose Down
DROP TABLE work_record_routing;
DROP TRIGGER routing_rule_versions_immutable ON routing_rule_versions;
DROP TRIGGER routing_rule_set_versions_immutable ON routing_rule_set_versions;
DROP TABLE routing_rule_versions;
DROP TABLE routing_rule_set_versions;
DROP TABLE routing_rule_sets;

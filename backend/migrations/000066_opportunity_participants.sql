-- +goose Up
ALTER TABLE opportunities
  ADD COLUMN team_id uuid,
  ADD CONSTRAINT opportunities_team_fk
    FOREIGN KEY (team_id, msp_id) REFERENCES teams(id, msp_id);

CREATE TABLE opportunity_contacts (
  opportunity_id uuid NOT NULL,
  contact_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  position integer NOT NULL CHECK (position > 0),
  PRIMARY KEY (opportunity_id, contact_id),
  UNIQUE (opportunity_id, position),
  FOREIGN KEY (opportunity_id, msp_id, client_id)
    REFERENCES opportunities(id, msp_id, client_id),
  FOREIGN KEY (contact_id, msp_id, client_id)
    REFERENCES contacts(id, msp_id, client_id)
);

CREATE INDEX opportunity_contacts_scope_lookup
  ON opportunity_contacts (msp_id, client_id, opportunity_id, position);

-- +goose Down
DROP TABLE opportunity_contacts;
ALTER TABLE opportunities DROP COLUMN team_id;

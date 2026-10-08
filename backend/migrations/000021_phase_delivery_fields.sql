-- +goose Up
ALTER TABLE phases
  ADD COLUMN owner_id uuid,
  ADD COLUMN actual_start timestamptz,
  ADD COLUMN actual_end timestamptz,
  ADD COLUMN planned_minutes bigint NOT NULL DEFAULT 0 CHECK (planned_minutes >= 0),
  ADD COLUMN budget_minor bigint NOT NULL DEFAULT 0 CHECK (budget_minor >= 0),
  ADD COLUMN budget_currency char(3),
  ADD COLUMN deliverables jsonb NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN completion_criteria jsonb NOT NULL DEFAULT '[]'::jsonb,
  ADD FOREIGN KEY (owner_id, msp_id) REFERENCES technicians(id, msp_id),
  ADD CHECK (actual_end IS NULL OR actual_start IS NULL OR actual_end >= actual_start),
  ADD CHECK (
    (budget_minor = 0 AND budget_currency IS NULL)
    OR (budget_currency IS NOT NULL AND budget_currency ~ '^[A-Z]{3}$')
  ),
  ADD CHECK (jsonb_typeof(deliverables) = 'array'),
  ADD CHECK (jsonb_typeof(completion_criteria) = 'array');

CREATE TABLE phase_participating_teams (
  phase_id uuid NOT NULL,
  team_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  position integer NOT NULL CHECK (position > 0),
  PRIMARY KEY (phase_id, team_id),
  UNIQUE (phase_id, position),
  FOREIGN KEY (phase_id, msp_id, client_id) REFERENCES phases(id, msp_id, client_id),
  FOREIGN KEY (team_id, msp_id) REFERENCES teams(id, msp_id)
);

-- +goose Down
DROP TABLE phase_participating_teams;

ALTER TABLE phases
  DROP COLUMN completion_criteria,
  DROP COLUMN deliverables,
  DROP COLUMN budget_currency,
  DROP COLUMN budget_minor,
  DROP COLUMN planned_minutes,
  DROP COLUMN actual_end,
  DROP COLUMN actual_start,
  DROP COLUMN owner_id;

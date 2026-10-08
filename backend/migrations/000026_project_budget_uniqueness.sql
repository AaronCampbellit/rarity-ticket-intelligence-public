-- +goose Up
CREATE UNIQUE INDEX project_budgets_project_type_unique
  ON project_budgets (project_id, budget_type)
  WHERE phase_id IS NULL;

-- +goose Down
DROP INDEX project_budgets_project_type_unique;

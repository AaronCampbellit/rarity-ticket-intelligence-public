-- +goose Up
-- Migration 80 shipped with a one-run-per-MSP constraint. Migration history
-- now records distinct cutover/audit runs, so discover and remove only the
-- unique constraint whose complete key is msp_id. The constraint name is not
-- assumed because PostgreSQL installations may have renamed it.
-- +goose StatementBegin
DO $$
DECLARE
  legacy_constraint text;
BEGIN
  SELECT constraint_record.conname
  INTO legacy_constraint
  FROM pg_constraint constraint_record
  JOIN LATERAL unnest(constraint_record.conkey)
    WITH ORDINALITY AS key_column(attnum, position) ON true
  JOIN pg_attribute attribute
    ON attribute.attrelid = constraint_record.conrelid
   AND attribute.attnum = key_column.attnum
  WHERE constraint_record.conrelid = 'classification_migration_runs'::regclass
    AND constraint_record.contype = 'u'
  GROUP BY constraint_record.oid, constraint_record.conname
  HAVING array_agg(attribute.attname ORDER BY key_column.position)
    = ARRAY['msp_id']::name[]
  LIMIT 1;

  IF legacy_constraint IS NOT NULL THEN
    EXECUTE format(
      'ALTER TABLE classification_migration_runs DROP CONSTRAINT %I',
      legacy_constraint
    );
  END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE classification_migration_runs
  ADD CONSTRAINT classification_migration_runs_msp_id_key UNIQUE (msp_id);

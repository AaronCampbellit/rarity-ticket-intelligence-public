-- +goose Up
DROP TRIGGER automation_versions_immutable ON automation_versions;

-- +goose StatementBegin
CREATE FUNCTION protect_automation_version_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'UPDATE'
    AND OLD.state = 'draft'
    AND NEW.state = 'published'
    AND OLD.id IS NOT DISTINCT FROM NEW.id
    AND OLD.automation_id IS NOT DISTINCT FROM NEW.automation_id
    AND OLD.msp_id IS NOT DISTINCT FROM NEW.msp_id
    AND OLD.version IS NOT DISTINCT FROM NEW.version
    AND OLD.trigger_event_type IS NOT DISTINCT FROM NEW.trigger_event_type
    AND OLD.client_scopes IS NOT DISTINCT FROM NEW.client_scopes
    AND OLD.capabilities IS NOT DISTINCT FROM NEW.capabilities
    AND OLD.definition_json IS NOT DISTINCT FROM NEW.definition_json
    AND OLD.max_depth IS NOT DISTINCT FROM NEW.max_depth
    AND OLD.published_at IS NULL
    AND OLD.published_by IS NULL
    AND NEW.published_at IS NOT NULL
    AND NEW.published_by IS NOT NULL
  THEN
    RETURN NEW;
  END IF;

  RAISE EXCEPTION
    'published automation versions are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER automation_versions_immutable
BEFORE UPDATE OR DELETE ON automation_versions
FOR EACH ROW EXECUTE FUNCTION protect_automation_version_mutation();

-- +goose Down
DROP TRIGGER automation_versions_immutable ON automation_versions;
DROP FUNCTION protect_automation_version_mutation();

CREATE TRIGGER automation_versions_immutable
BEFORE UPDATE OR DELETE ON automation_versions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

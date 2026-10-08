-- +goose Up
ALTER TABLE knowledge_articles
  ADD COLUMN client_visible boolean NOT NULL DEFAULT false,
  ADD CONSTRAINT knowledge_articles_internal_only CHECK (NOT client_visible);

DROP TRIGGER knowledge_article_versions_immutable ON knowledge_article_versions;

-- +goose StatementBegin
CREATE FUNCTION protect_knowledge_article_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF OLD.published_at IS NOT NULL THEN
    RAISE EXCEPTION 'published knowledge versions are immutable';
  END IF;
  IF NEW.article_id IS DISTINCT FROM OLD.article_id
     OR NEW.msp_id IS DISTINCT FROM OLD.msp_id
     OR NEW.version IS DISTINCT FROM OLD.version
     OR NEW.body IS DISTINCT FROM OLD.body
     OR NEW.created_at IS DISTINCT FROM OLD.created_at
     OR NEW.created_by IS DISTINCT FROM OLD.created_by THEN
    RAISE EXCEPTION 'knowledge version content is immutable';
  END IF;
  IF NEW.published_at IS NULL OR NEW.published_by IS NULL THEN
    RAISE EXCEPTION 'knowledge version update must publish the draft';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER knowledge_article_versions_protected
BEFORE UPDATE OR DELETE ON knowledge_article_versions
FOR EACH ROW EXECUTE FUNCTION protect_knowledge_article_version();

-- +goose Down
DROP TRIGGER knowledge_article_versions_protected ON knowledge_article_versions;
DROP FUNCTION protect_knowledge_article_version();
CREATE TRIGGER knowledge_article_versions_immutable
BEFORE UPDATE OR DELETE ON knowledge_article_versions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();
ALTER TABLE knowledge_articles
  DROP CONSTRAINT knowledge_articles_internal_only,
  DROP COLUMN client_visible;

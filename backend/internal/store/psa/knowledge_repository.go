package psa

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/objectidentity"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

type KnowledgeRepository struct {
	db database
}

var _ knowledge.Repository = (*KnowledgeRepository)(nil)

func NewKnowledgeRepository(db database) *KnowledgeRepository {
	return &KnowledgeRepository{db: db}
}

func (r *KnowledgeRepository) List(
	ctx context.Context,
	target scope.Target,
	query string,
	limit int,
) ([]knowledge.Article, error) {
	rows, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, client_id::text, display_id, title, state,
       current_version, client_visible, updated_at, updated_by::text
FROM knowledge_articles
WHERE msp_id = $1 AND client_id = $2::uuid AND NOT client_visible
  AND (
    $3 = ''
    OR display_id ILIKE '%' || $3 || '%'
    OR title ILIKE '%' || $3 || '%'
  )
ORDER BY updated_at DESC, id
LIMIT $4
`, target.MSPID, target.ClientID, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]knowledge.Article, 0)
	for rows.Next() {
		var item knowledge.Article
		var state string
		if err := rows.Scan(
			&item.ID, &item.MSPID, &item.ClientID, &item.DisplayID,
			&item.Title, &state, &item.CurrentVersion, &item.ClientVisible,
			&item.UpdatedAt, &item.UpdatedBy,
		); err != nil {
			return nil, err
		}
		item.State = knowledge.State(state)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *KnowledgeRepository) FindByReference(
	ctx context.Context,
	target scope.Target,
	reference string,
	limit int,
) ([]knowledge.Article, error) {
	if limit < 1 || limit > 2 {
		limit = 2
	}
	normalized := normalizedReference(reference)
	rows, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, client_id::text, display_id, title, state,
       current_version, client_visible, updated_at, updated_by::text
FROM knowledge_articles
WHERE msp_id = $1 AND client_id = $2::uuid AND NOT client_visible
  AND (
    lower(btrim(regexp_replace(
      translate(display_id, $4, repeat(' ', char_length($4))),
      '[[:space:]]+', ' ', 'g'
    ))) = $3
    OR lower(btrim(regexp_replace(
      translate(title, $4, repeat(' ', char_length($4))),
      '[[:space:]]+', ' ', 'g'
    ))) = $3
  )
ORDER BY CASE WHEN lower(btrim(regexp_replace(
  translate(display_id, $4, repeat(' ', char_length($4))),
  '[[:space:]]+', ' ', 'g'
))) = $3 THEN 0 ELSE 1 END, id
LIMIT $5
`, target.MSPID, target.ClientID, normalized, unicodeReferenceWhitespace, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]knowledge.Article, 0, limit)
	for rows.Next() {
		var item knowledge.Article
		var state string
		if err := rows.Scan(
			&item.ID, &item.MSPID, &item.ClientID, &item.DisplayID,
			&item.Title, &state, &item.CurrentVersion, &item.ClientVisible,
			&item.UpdatedAt, &item.UpdatedBy,
		); err != nil {
			return nil, err
		}
		item.State = knowledge.State(state)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return exactReferenceMatches(
		result, reference,
		func(item knowledge.Article) string { return item.DisplayID },
		func(item knowledge.Article) string { return item.Title },
	), nil
}

func (r *KnowledgeRepository) Find(
	ctx context.Context,
	target scope.Target,
	articleID string,
) (knowledge.ArticleDetail, error) {
	return scanKnowledgeDetail(r.db.QueryRow(ctx, `
SELECT article.id::text, article.msp_id::text, article.client_id::text,
       article.display_id, article.title, article.state,
       article.current_version, article.client_visible,
       article.updated_at, article.updated_by::text,
       version.article_id::text, version.version, version.body,
       version.created_at, version.created_by::text,
       version.published_at, COALESCE(version.published_by::text, '')
FROM knowledge_articles article
JOIN knowledge_article_versions version
  ON version.article_id = article.id
 AND version.version = article.current_version
WHERE article.id = $1 AND article.msp_id = $2
  AND article.client_id = $3::uuid
  AND NOT article.client_visible
`, articleID, target.MSPID, target.ClientID))
}

func (r *KnowledgeRepository) LoadDraft(
	ctx context.Context,
	target scope.Target,
	articleID string,
	version int64,
) (knowledge.Article, knowledge.Version, error) {
	detail, err := scanKnowledgeDetail(r.db.QueryRow(ctx, `
SELECT article.id::text, article.msp_id::text, article.client_id::text,
       article.display_id, article.title, article.state,
       article.current_version, article.client_visible,
       article.updated_at, article.updated_by::text,
       draft.article_id::text, draft.version, draft.body,
       draft.created_at, draft.created_by::text,
       draft.published_at, COALESCE(draft.published_by::text, '')
FROM knowledge_articles article
JOIN knowledge_article_versions draft
  ON draft.article_id = article.id AND draft.version = $4
WHERE article.id = $1 AND article.msp_id = $2
  AND article.client_id = $3::uuid
  AND article.current_version = $4
  AND article.state = 'draft' AND draft.published_at IS NULL
  AND NOT article.client_visible
`, articleID, target.MSPID, target.ClientID, version))
	return detail.Article, detail.Version, err
}

func scanKnowledgeDetail(value row) (knowledge.ArticleDetail, error) {
	var detail knowledge.ArticleDetail
	var articleState string
	err := value.Scan(
		&detail.Article.ID, &detail.Article.MSPID, &detail.Article.ClientID,
		&detail.Article.DisplayID, &detail.Article.Title, &articleState,
		&detail.Article.CurrentVersion, &detail.Article.ClientVisible,
		&detail.Article.UpdatedAt, &detail.Article.UpdatedBy,
		&detail.Version.ArticleID, &detail.Version.Version, &detail.Version.Body,
		&detail.Version.CreatedAt, &detail.Version.CreatedBy,
		&detail.Version.PublishedAt, &detail.Version.PublishedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return knowledge.ArticleDetail{}, scope.ErrNotFound
	}
	if err != nil {
		return knowledge.ArticleDetail{}, err
	}
	detail.Article.State = knowledge.State(articleState)
	detail.Version.State = knowledge.Draft
	if detail.Version.PublishedAt != nil {
		detail.Version.State = knowledge.Published
	}
	return detail, nil
}

func (r *KnowledgeRepository) CreateDraftAtomic(
	ctx context.Context,
	accepted knowledge.DraftMutation,
) error {
	return r.writeDraft(ctx, accepted, true)
}

func (r *KnowledgeRepository) ReviseDraftAtomic(
	ctx context.Context,
	accepted knowledge.DraftMutation,
) error {
	return r.writeDraft(ctx, accepted, false)
}

func (r *KnowledgeRepository) writeDraft(
	ctx context.Context,
	accepted knowledge.DraftMutation,
	created bool,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	article := accepted.Article
	if err := lockActiveClient(
		ctx, tx, article.MSPID, article.ClientID,
	); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if created {
		err := objectidentity.Enforce(
			ctx,
			"knowledge_article_identity:"+article.MSPID+":"+article.ClientID,
			article.Title,
			article.DisplayID,
			`SELECT title, display_id
FROM knowledge_articles
WHERE msp_id = $1 AND client_id = $2::uuid`,
			[]any{article.MSPID, article.ClientID},
			func(ctx context.Context, query string, args ...any) error {
				_, err := tx.Exec(ctx, query, args...)
				return err
			},
			func(ctx context.Context, query string, args ...any) (objectidentity.Rows, error) {
				return tx.Query(ctx, query, args...)
			},
		)
		if errors.Is(err, objectidentity.ErrConflict) {
			_ = tx.Rollback(ctx)
			return knowledge.ErrIdentityConflict
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO knowledge_articles (
  id, msp_id, client_id, display_id, title, state, current_version,
  client_visible, created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, $3, $4, $5, 'draft', 1, false, $6, $7, $6, $7)
`, article.ID, article.MSPID, article.ClientID, article.DisplayID,
			article.Title, article.UpdatedAt, article.UpdatedBy); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
	} else {
		tag, err := tx.Exec(ctx, `
UPDATE knowledge_articles
SET title = $4, state = 'draft', current_version = current_version + 1,
    updated_at = $5, updated_by = $6
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND current_version = $7 AND state = 'draft' AND NOT client_visible
`, article.ID, article.MSPID, article.ClientID, article.Title,
			article.UpdatedAt, article.UpdatedBy, article.CurrentVersion-1)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if tag.RowsAffected() != 1 {
			_ = tx.Rollback(ctx)
			return object.ErrVersionConflict
		}
	}
	version := accepted.Version
	if _, err := tx.Exec(ctx, `
INSERT INTO knowledge_article_versions (
  article_id, msp_id, version, body, created_at, created_by
) VALUES ($1, $2, $3, $4, $5, $6)
`, version.ArticleID, article.MSPID, version.Version,
		version.Body, version.CreatedAt, version.CreatedBy); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if created {
		if err := insertInitialTagAssignments(ctx, tx, tagging.TargetRef{MSPID: article.MSPID, ClientID: article.ClientID, ObjectType: tagging.ObjectKnowledgeArticle, ObjectID: article.ID}, article.CurrentVersion, accepted.InitialTags); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
	}
	if err := writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *KnowledgeRepository) PublishAtomic(
	ctx context.Context,
	accepted knowledge.PublishMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	article := accepted.Article
	if err := lockActiveClientAtVersion(
		ctx, tx, article.MSPID, article.ClientID, accepted.ExpectedClientVersion,
	); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE knowledge_articles
SET state = 'published', updated_at = $5, updated_by = $6
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND current_version = $4 AND state = 'draft' AND NOT client_visible
`, article.ID, article.MSPID, article.ClientID, article.CurrentVersion,
		article.UpdatedAt, article.UpdatedBy)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return object.ErrVersionConflict
	}
	version := accepted.Version
	tag, err = tx.Exec(ctx, `
UPDATE knowledge_article_versions
SET published_at = $4, published_by = $5
WHERE article_id = $1 AND msp_id = $2 AND version = $3
  AND published_at IS NULL
`, version.ArticleID, article.MSPID, version.Version,
		version.PublishedAt, version.PublishedBy)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return object.ErrVersionConflict
	}
	if err := writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

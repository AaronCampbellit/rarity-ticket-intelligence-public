package psa

import (
	"context"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/search"
)

type SearchRepository struct {
	db database
}

var _ search.Repository = (*SearchRepository)(nil)

func NewSearchRepository(db database) *SearchRepository {
	return &SearchRepository{db: db}
}

func (r *SearchRepository) Search(
	ctx context.Context,
	target scope.Target,
	text string,
	limit int,
) ([]search.Result, error) {
	found, err := r.db.Query(ctx, `
SELECT id, object_type, client_id, title, snippet
FROM (
  SELECT id::text AS id, 'work_record'::text AS object_type,
         client_id::text AS client_id, title,
         left(description, 240) AS snippet, updated_at AS changed_at
  FROM work_records
  WHERE msp_id = $1 AND client_id = $2
    AND lifecycle_state = 'active' AND deleted_at IS NULL
    AND strpos(lower(display_id || ' ' || title || ' ' || description), lower($3)) > 0
  UNION ALL
  SELECT id::text, 'contact', client_id::text, display_name,
         COALESCE(email, ''), updated_at
  FROM contacts
  WHERE msp_id = $1 AND client_id = $2 AND lifecycle_state = 'active'
    AND strpos(lower(display_id || ' ' || display_name || ' ' ||
        COALESCE(email, '') || ' ' || COALESCE(phone, '')), lower($3)) > 0
  UNION ALL
  SELECT id::text, 'location', client_id::text, name, display_id, updated_at
  FROM locations
  WHERE msp_id = $1 AND client_id = $2 AND lifecycle_state = 'active'
    AND strpos(lower(display_id || ' ' || name), lower($3)) > 0
  UNION ALL
  SELECT id::text, 'asset', client_id::text, name,
         display_id || ' ' || asset_type, updated_at
  FROM assets
  WHERE msp_id = $1 AND client_id = $2 AND lifecycle_state = 'active'
    AND strpos(lower(display_id || ' ' || name || ' ' || asset_type), lower($3)) > 0
  UNION ALL
  SELECT id::text, 'service', client_id::text, name, display_id, updated_at
  FROM services
  WHERE msp_id = $1 AND client_id = $2 AND lifecycle_state = 'active'
    AND strpos(lower(display_id || ' ' || name), lower($3)) > 0
  UNION ALL
  SELECT id::text, 'contract', client_id::text, name, display_id, updated_at
  FROM contracts
  WHERE msp_id = $1 AND client_id = $2 AND lifecycle_state = 'active'
    AND starts_on <= CURRENT_DATE AND (ends_on IS NULL OR ends_on >= CURRENT_DATE)
    AND strpos(lower(display_id || ' ' || name), lower($3)) > 0
) AS matches
ORDER BY changed_at DESC, object_type, id
LIMIT $4
`, target.MSPID, target.ClientID, text, limit)
	if err != nil {
		return nil, err
	}
	defer found.Close()
	results := make([]search.Result, 0)
	for found.Next() {
		var result search.Result
		if err := found.Scan(
			&result.ID, &result.ObjectType, &result.ClientID,
			&result.Title, &result.Snippet,
		); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err := found.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

package psa

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/views"
)

type ViewRepository struct {
	db database
}

var _ views.Repository = (*ViewRepository)(nil)

func NewViewRepository(db database) *ViewRepository {
	return &ViewRepository{db: db}
}

func (r *ViewRepository) Save(ctx context.Context, view views.View) error {
	document := cloneViewDocument(view.Query)
	delete(document, "__view_kind")
	if view.Kind == views.KindCalendarLens {
		document["__view_kind"] = string(views.KindCalendarLens)
	}
	query, err := json.Marshal(document)
	if err != nil {
		return err
	}
	table, documentColumn := "saved_searches", "query"
	if view.Kind == views.Dashboard {
		table, documentColumn = "dashboards", "layout"
	} else if view.Kind != views.SavedSearch && view.Kind != views.KindCalendarLens {
		return views.ErrInvalid
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	statement := `INSERT INTO ` + table + ` (
  id, msp_id, owner_id, name, ` + documentColumn + `,
  audience_type, audience_id, version
) VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::uuid, $8)`
	if _, err := tx.Exec(
		ctx, statement, view.ID, view.MSPID, view.OwnerID, view.Name, query,
		view.Audience.Type, view.Audience.ID, view.Version,
	); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *ViewRepository) Find(ctx context.Context, mspID, viewID string) (views.View, error) {
	var (
		view     views.View
		document []byte
	)
	err := r.db.QueryRow(ctx, `
SELECT id::text, msp_id::text, owner_id::text, kind, name, document,
       audience_type, COALESCE(audience_id::text, ''), version
FROM (
  SELECT id, msp_id, owner_id, CASE WHEN query->>'__view_kind'='calendar_lens' THEN 'calendar_lens' ELSE 'saved_search' END AS kind, name,
         query AS document, audience_type, audience_id, version
  FROM saved_searches
  WHERE id = $1 AND msp_id = $2
  UNION ALL
  SELECT id, msp_id, owner_id, 'dashboard'::text AS kind, name,
         layout AS document, audience_type, audience_id, version
  FROM dashboards
  WHERE id = $1 AND msp_id = $2
) scoped_view
LIMIT 1
`, viewID, mspID).Scan(
		&view.ID, &view.MSPID, &view.OwnerID, &view.Kind, &view.Name,
		&document, &view.Audience.Type, &view.Audience.ID, &view.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return views.View{}, scope.ErrNotFound
	}
	if err != nil {
		return views.View{}, err
	}
	if err := json.Unmarshal(document, &view.Query); err != nil {
		return views.View{}, err
	}
	delete(view.Query, "__view_kind")
	return view, nil
}

func (r *ViewRepository) List(
	ctx context.Context,
	mspID string,
	kind views.Kind,
) ([]views.View, error) {
	table, documentColumn := "saved_searches", "query"
	if kind == views.Dashboard {
		table, documentColumn = "dashboards", "layout"
	} else if kind != views.SavedSearch && kind != views.KindCalendarLens {
		return nil, views.ErrInvalid
	}
	rows, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, owner_id::text, name, `+documentColumn+`,
       audience_type, COALESCE(audience_id::text, ''), version
FROM `+table+`
WHERE msp_id = $1`+viewKindPredicate(kind)+`
ORDER BY name, id
`, mspID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []views.View
	for rows.Next() {
		var view views.View
		var document []byte
		view.Kind = kind
		if err := rows.Scan(
			&view.ID, &view.MSPID, &view.OwnerID, &view.Name, &document,
			&view.Audience.Type, &view.Audience.ID, &view.Version,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(document, &view.Query); err != nil {
			return nil, err
		}
		delete(view.Query, "__view_kind")
		found = append(found, view)
	}
	return found, rows.Err()
}

func viewKindPredicate(kind views.Kind) string {
	if kind == views.KindCalendarLens {
		return " AND query->>'__view_kind'='calendar_lens'"
	}
	if kind == views.SavedSearch {
		return " AND COALESCE(query->>'__view_kind','')<>'calendar_lens'"
	}
	return ""
}
func cloneViewDocument(query map[string]any) map[string]any {
	result := make(map[string]any, len(query)+1)
	for k, v := range query {
		result[k] = v
	}
	return result
}

func (r *ViewRepository) AuthorizedViewClientIDs(ctx context.Context, principal authorization.Principal) ([]string, error) {
	if r == nil || r.db == nil || strings.TrimSpace(principal.Scope.MSPID) == "" || strings.TrimSpace(principal.ID) == "" {
		return nil, views.ErrInvalid
	}
	rows, err := r.db.Query(ctx, authorizedCalendarClientsSQL, principal.Scope.MSPID, principal.ID, "calendar.read")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

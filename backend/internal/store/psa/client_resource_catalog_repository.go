package psa

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func (r *ClientResourceCatalogRepository) GetCatalog(ctx context.Context, target scope.Target, id string) (clientresources.Summary, error) {
	var item clientresources.Summary
	err := r.db.QueryRow(ctx, `
SELECT id::text, 'asset'::text, display_id, name, asset_type,
       COALESCE(location_id::text, ''), version
FROM assets
WHERE id = $1 AND msp_id = $2 AND client_id = $3::uuid
  AND lifecycle_state = 'active'
`, id, target.MSPID, target.ClientID).Scan(
		&item.ID, &item.Kind, &item.DisplayID, &item.Name,
		&item.Detail, &item.LocationID, &item.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return clientresources.Summary{}, scope.ErrNotFound
	}
	return item, err
}

type ClientResourceCatalogRepository struct {
	db database
}

var _ clientresources.CatalogRepository = (*ClientResourceCatalogRepository)(nil)
var _ clientresources.QueryRepository = (*ClientResourceCatalogRepository)(nil)

func NewClientResourceCatalogRepository(
	db database,
) *ClientResourceCatalogRepository {
	return &ClientResourceCatalogRepository{db: db}
}

func (r *ClientResourceCatalogRepository) ListCatalog(
	ctx context.Context,
	target scope.Target,
	limit int,
	lifecycle clientresources.CatalogLifecycle,
	inactiveKinds []clientresources.Kind,
) ([]clientresources.Summary, error) {
	allowed := make(map[clientresources.Kind]bool, len(inactiveKinds))
	for _, kind := range inactiveKinds {
		allowed[kind] = true
	}
	rows, err := r.db.Query(ctx, `
SELECT id::text, kind, display_id, name, detail, location_id,
       lifecycle_state, authority, version
FROM (
  SELECT id, 'location'::text AS kind, display_id, name,
         ''::text AS detail, ''::text AS location_id, lifecycle_state,
         ''::text AS authority, version, updated_at
  FROM locations
  WHERE msp_id = $1 AND client_id = $2::uuid
    AND ($4 = 'all' OR lifecycle_state = $4)
    AND (lifecycle_state = 'active' OR $5)
  UNION ALL
  SELECT id, 'contact', display_id, display_name,
         ''::text, COALESCE(location_id::text, ''), lifecycle_state,
         ''::text, version, updated_at
  FROM contacts
  WHERE msp_id = $1 AND client_id = $2::uuid
    AND ($4 = 'all' OR lifecycle_state = $4)
    AND (lifecycle_state = 'active' OR $6)
  UNION ALL
  SELECT id, 'asset', display_id, name, asset_type,
         COALESCE(location_id::text, ''), lifecycle_state,
         authority::text, version, updated_at
  FROM assets
  WHERE msp_id = $1 AND client_id = $2::uuid
    AND ($4 = 'all' OR lifecycle_state = $4)
    AND (lifecycle_state = 'active' OR $7)
  UNION ALL
  SELECT id, 'service', display_id, name, criticality,
         ''::text, lifecycle_state, ''::text, version, updated_at
  FROM services
  WHERE msp_id = $1 AND client_id = $2::uuid
    AND ($4 = 'all' OR lifecycle_state = $4)
    AND (lifecycle_state = 'active' OR $8)
  UNION ALL
  SELECT id, 'contract', display_id, name,
         starts_on::text || COALESCE(' through ' || ends_on::text, ''),
         ''::text, lifecycle_state, ''::text, version, updated_at
  FROM contracts
  WHERE msp_id = $1 AND client_id = $2::uuid
    AND ($4 = 'all' OR lifecycle_state = $4)
    AND (lifecycle_state = 'active' OR $9)
) catalog
ORDER BY updated_at DESC, kind, display_id, id
LIMIT $3
`, target.MSPID, target.ClientID, limit, lifecycle,
		allowed[clientresources.LocationKind],
		allowed[clientresources.ContactKind],
		allowed[clientresources.AssetKind],
		allowed[clientresources.ServiceKind],
		allowed[clientresources.ContractKind],
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]clientresources.Summary, 0)
	for rows.Next() {
		var item clientresources.Summary
		if err := rows.Scan(
			&item.ID, &item.Kind, &item.DisplayID, &item.Name,
			&item.Detail, &item.LocationID, &item.LifecycleState,
			&item.Authority, &item.Version,
		); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *ClientResourceCatalogRepository) QueryResources(
	ctx context.Context,
	target scope.Target,
	query clientresources.Query,
) ([]clientresources.ResourceDetail, error) {
	sql, ok := clientResourceQuerySQL(query.Kind)
	if !ok {
		return nil, clientresources.ErrInvalidCatalog
	}
	rows, err := r.db.Query(ctx, sql,
		target.MSPID, target.ClientID, query.Literal, query.IncludeInactive, query.Limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	resources := make([]clientresources.ResourceDetail, 0)
	for rows.Next() {
		resource, err := scanClientResourceDetail(rows)
		if err != nil {
			return nil, err
		}
		resources = append(resources, resource)
	}
	return resources, rows.Err()
}

func (r *ClientResourceCatalogRepository) GetResource(
	ctx context.Context,
	target scope.Target,
	kind clientresources.Kind,
	id string,
	includeInactive bool,
) (clientresources.ResourceDetail, error) {
	sql, ok := clientResourceGetSQL(kind)
	if !ok {
		return clientresources.ResourceDetail{}, clientresources.ErrInvalidCatalog
	}
	resource, err := scanClientResourceDetail(r.db.QueryRow(
		ctx, sql, target.MSPID, target.ClientID, id, includeInactive,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return clientresources.ResourceDetail{}, scope.ErrNotFound
	}
	return resource, err
}

func (r *ClientResourceCatalogRepository) ResolveResource(
	ctx context.Context,
	target scope.Target,
	kind clientresources.Kind,
	reference string,
	includeInactive bool,
) ([]clientresources.ResourceDetail, error) {
	sql, ok := clientResourceResolveSQL(kind)
	if !ok {
		return nil, clientresources.ErrInvalidCatalog
	}
	rows, err := r.db.Query(
		ctx, sql, target.MSPID, target.ClientID, reference, includeInactive,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	resources := make([]clientresources.ResourceDetail, 0, 2)
	for rows.Next() {
		resource, err := scanClientResourceDetail(rows)
		if err != nil {
			return nil, err
		}
		resources = append(resources, resource)
	}
	return resources, rows.Err()
}

func clientResourceQuerySQL(kind clientresources.Kind) (string, bool) {
	base, ok := clientResourceSelectSQL(kind)
	if !ok {
		return "", false
	}
	_, nameColumn, ok := clientResourceIdentitySQL(kind)
	if !ok {
		return "", false
	}
	return base + `
WHERE msp_id = $1 AND client_id = $2::uuid
  AND EXISTS (
    SELECT 1 FROM client_organizations
    WHERE id = $2::uuid AND msp_id = $1 AND lifecycle_state = 'active'
  )
  AND ($4 OR lifecycle_state = 'active')
  AND ($3 = '' OR strpos(
    regexp_replace(lower(display_id || ' ' || ` + nameColumn + `), '\s+', ' ', 'g'),
    regexp_replace(lower($3), '\s+', ' ', 'g')
  ) > 0)
ORDER BY ` + nameColumn + `, display_id, id
LIMIT $5
`, true
}

func clientResourceGetSQL(kind clientresources.Kind) (string, bool) {
	base, ok := clientResourceSelectSQL(kind)
	if !ok {
		return "", false
	}
	return base + `
WHERE id = $3::uuid AND msp_id = $1 AND client_id = $2::uuid
  AND EXISTS (
    SELECT 1 FROM client_organizations
    WHERE id = $2::uuid AND msp_id = $1 AND lifecycle_state = 'active'
  )
  AND ($4 OR lifecycle_state = 'active')
`, true
}

func clientResourceResolveSQL(kind clientresources.Kind) (string, bool) {
	base, ok := clientResourceSelectSQL(kind)
	if !ok {
		return "", false
	}
	table, nameColumn, ok := clientResourceIdentitySQL(kind)
	if !ok {
		return "", false
	}
	return base + `
WHERE msp_id = $1 AND client_id = $2::uuid
  AND EXISTS (
    SELECT 1 FROM client_organizations
    WHERE id = $2::uuid AND msp_id = $1 AND lifecycle_state = 'active'
  )
  AND ($4 OR lifecycle_state = 'active')
  AND (
    display_id = $3
    OR (
      regexp_replace(lower(` + nameColumn + `), '\s+', ' ', 'g') =
        regexp_replace(lower(btrim($3)), '\s+', ' ', 'g')
      AND NOT EXISTS (
        SELECT 1 FROM ` + table + ` exact
        WHERE exact.msp_id = $1 AND exact.client_id = $2::uuid
          AND ($4 OR exact.lifecycle_state = 'active')
          AND exact.display_id = $3
      )
    )
  )
ORDER BY display_id, id
LIMIT 2
`, true
}

func clientResourceIdentitySQL(
	kind clientresources.Kind,
) (table string, nameColumn string, ok bool) {
	switch kind {
	case clientresources.LocationKind:
		return "locations", "name", true
	case clientresources.ContactKind:
		return "contacts", "display_name", true
	case clientresources.AssetKind:
		return "assets", "name", true
	case clientresources.ServiceKind:
		return "services", "name", true
	case clientresources.ContractKind:
		return "contracts", "name", true
	default:
		return "", "", false
	}
}

func clientResourceSelectSQL(kind clientresources.Kind) (string, bool) {
	switch kind {
	case clientresources.LocationKind:
		return `
SELECT id::text, 'location'::text, display_id, name,
       ''::text, ''::text, version, lifecycle_state,
       ''::text, ''::text, ''::text, ''::text, ''::text, ''::text,
       ''::text, NULL::date, NULL::date
FROM locations
`, true
	case clientresources.ContactKind:
		return `
SELECT id::text, 'contact'::text, display_id, display_name AS name,
       COALESCE(email, ''), COALESCE(location_id::text, ''), version, lifecycle_state,
       COALESCE(email, ''), COALESCE(phone, ''), ''::text, ''::text, ''::text, ''::text,
       ''::text, NULL::date, NULL::date
FROM contacts
`, true
	case clientresources.AssetKind:
		return `
SELECT id::text, 'asset'::text, display_id, name,
       asset_type, COALESCE(location_id::text, ''), version, lifecycle_state,
       ''::text, ''::text, asset_type, source_system, external_id, authority,
       ''::text, NULL::date, NULL::date
FROM assets
`, true
	case clientresources.ServiceKind:
		return `
SELECT id::text, 'service'::text, display_id, name,
       criticality, ''::text, version, lifecycle_state,
       ''::text, ''::text, ''::text, ''::text, ''::text, ''::text,
       criticality, NULL::date, NULL::date
FROM services
`, true
	case clientresources.ContractKind:
		return `
SELECT id::text, 'contract'::text, display_id, name,
       starts_on::text || COALESCE(' through ' || ends_on::text, ''), ''::text,
       version, lifecycle_state,
       ''::text, ''::text, ''::text, ''::text, ''::text, ''::text,
       ''::text, starts_on, ends_on
FROM contracts
`, true
	default:
		return "", false
	}
}

func scanClientResourceDetail(scanner interface {
	Scan(...any) error
}) (clientresources.ResourceDetail, error) {
	var resource clientresources.ResourceDetail
	err := scanner.Scan(
		&resource.ID, &resource.Kind, &resource.DisplayID, &resource.Name,
		&resource.Detail, &resource.LocationID, &resource.Version,
		&resource.Summary.LifecycleState, &resource.Email, &resource.Phone,
		&resource.AssetType, &resource.SourceSystem, &resource.ExternalID,
		&resource.Summary.Authority, &resource.Criticality, &resource.StartsOn, &resource.EndsOn,
	)
	return resource, err
}

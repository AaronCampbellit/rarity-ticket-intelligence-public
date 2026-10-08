package psa

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

const taggingProjectionName = "tagging-daily-v1"

const (
	reportSnapshotMaxRows            = 50_000
	reportSnapshotMaxBytes     int64 = 64 << 20
	reportSnapshotGlobalRows         = 250_000
	reportSnapshotGlobalBytes  int64 = 256 << 20
	reportSnapshotBatchRows          = 200
	reportSnapshotCleanupBatch       = 100
	reportSnapshotBuildWait          = 2 * time.Minute
)

type TaggingProjectionRepository struct{ db database }

var _ tagging.ProjectionRepository = (*TaggingProjectionRepository)(nil)
var _ tagging.ReportRepository = (*TaggingProjectionRepository)(nil)

func NewTaggingProjectionRepository(db database) *TaggingProjectionRepository {
	return &TaggingProjectionRepository{db: db}
}

func (r *TaggingProjectionRepository) ClassificationTechnicians(ctx context.Context, mspID, clientID string) ([]tagging.TechnicianOption, error) {
	result, err := r.db.Query(ctx, `SELECT DISTINCT technician.id::text,technician.display_name FROM technicians technician WHERE technician.msp_id=$1::uuid AND (EXISTS(SELECT 1 FROM work_records work WHERE work.msp_id=$1::uuid AND work.client_id=$2::uuid AND work.primary_owner_id=technician.id) OR EXISTS(SELECT 1 FROM time_entries entry WHERE entry.msp_id=$1::uuid AND entry.client_id=$2::uuid AND entry.technician_id=technician.id)) ORDER BY technician.display_name,technician.id`, mspID, clientID)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	values := []tagging.TechnicianOption{}
	for result.Next() {
		var value tagging.TechnicianOption
		if err := result.Scan(&value.ID, &value.Label); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, result.Err()
}

type projectionEvent struct {
	id, mspID, clientID, objectType, objectID string
	rawTagID, tagID, operation, source        string
	dimensions                                string
	taskDimensions                            map[string]json.RawMessage
	at                                        time.Time
}

func (r *TaggingProjectionRepository) ProjectTagEvents(ctx context.Context, limit int) (tagging.ProjectionResult, error) {
	if r == nil || r.db == nil || limit < 1 || limit > 500 {
		return tagging.ProjectionResult{}, tagging.ErrInvalidReportFilter
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return tagging.ProjectionResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('tagging-daily-v1', 0))`); err != nil {
		return tagging.ProjectionResult{}, err
	}
	if _, err = tx.Exec(ctx, `
INSERT INTO tag_projection_cursors (msp_id, projection_name)
SELECT DISTINCT event.msp_id, $1
FROM tag_assignment_events event
ON CONFLICT (msp_id, projection_name) DO NOTHING
`, taggingProjectionName); err != nil {
		return tagging.ProjectionResult{}, err
	}
	rows, err := tx.Query(ctx, `
SELECT event.id::text, event.msp_id::text, COALESCE(event.client_id::text,''),
       event.object_type, event.object_id::text,
       event.tag_id::text,COALESCE(canonical.id,event.tag_id)::text, event.operation,
       event.assignment_source, event.occurred_at,
       COALESCE(event.evidence->'projection_dimensions','{}'::jsonb)::text,
       COALESCE(event.evidence->'project_task_dimensions','{}'::jsonb)::text
FROM tag_assignment_events event
JOIN tag_projection_cursors cursor
  ON cursor.msp_id=event.msp_id AND cursor.projection_name=$2
LEFT JOIN LATERAL (
  WITH RECURSIVE chain(id,depth) AS (
    SELECT event.tag_id,0
    UNION ALL
    SELECT tag.merged_into_id,chain.depth+1
    FROM chain JOIN tags tag ON tag.id=chain.id AND tag.msp_id=event.msp_id
    WHERE tag.merged_into_id IS NOT NULL AND chain.depth<50
  ) SELECT id FROM chain ORDER BY depth DESC LIMIT 1
) canonical ON true
WHERE cursor.last_occurred_at IS NULL
   OR event.occurred_at > cursor.last_occurred_at
   OR (event.occurred_at = cursor.last_occurred_at AND event.id > cursor.last_event_id)
ORDER BY event.occurred_at, event.id
LIMIT $1
`, limit, taggingProjectionName)
	if err != nil {
		return tagging.ProjectionResult{}, err
	}
	events := make([]projectionEvent, 0, limit)
	for rows.Next() {
		var event projectionEvent
		var taskDimensions string
		if err := rows.Scan(&event.id, &event.mspID, &event.clientID, &event.objectType, &event.objectID, &event.rawTagID, &event.tagID, &event.operation, &event.source, &event.at, &event.dimensions, &taskDimensions); err != nil {
			rows.Close()
			return tagging.ProjectionResult{}, err
		}
		_ = json.Unmarshal([]byte(taskDimensions), &event.taskDimensions)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return tagging.ProjectionResult{}, err
	}
	rows.Close()

	result := tagging.ProjectionResult{Processed: len(events)}
	latest := map[string]projectionEvent{}
	for _, event := range events {
		switch event.objectType {
		case "task":
			effects, err := taskEffectiveTransition(ctx, tx, event, event.objectID)
			if err != nil {
				return tagging.ProjectionResult{}, err
			}
			for _, effect := range effects {
				if err := insertProjectionEffect(ctx, tx, effect); err != nil {
					return tagging.ProjectionResult{}, err
				}
				if effect.inherited {
					result.InheritedEffects++
				}
			}
		case "project":
			if err := insertProjectionEffect(ctx, tx, directProjectionEffect(event)); err != nil {
				return tagging.ProjectionResult{}, err
			}
			tasks, err := currentProjectTaskIDs(ctx, tx, event)
			if err != nil {
				return tagging.ProjectionResult{}, err
			}
			for _, taskID := range tasks {
				effects, err := taskEffectiveTransition(ctx, tx, event, taskID)
				if err != nil {
					return tagging.ProjectionResult{}, err
				}
				for _, effect := range effects {
					if err := insertProjectionEffect(ctx, tx, effect); err != nil {
						return tagging.ProjectionResult{}, err
					}
					if effect.inherited {
						result.InheritedEffects++
					}
				}
			}
		default:
			if err := insertProjectionEffect(ctx, tx, directProjectionEffect(event)); err != nil {
				return tagging.ProjectionResult{}, err
			}
		}
		latest[event.mspID] = event
		if event.at.After(result.ProjectionAsOf) {
			result.ProjectionAsOf = event.at
		}
	}
	for mspID := range latest {
		if err := rebuildTaggingAggregates(ctx, tx, mspID); err != nil {
			return tagging.ProjectionResult{}, err
		}
	}
	for mspID, event := range latest {
		if _, err := tx.Exec(ctx, `
UPDATE tag_projection_cursors
SET last_occurred_at=$3, last_event_id=$4::uuid, updated_at=now()
WHERE msp_id=$1::uuid AND projection_name=$2
`, mspID, taggingProjectionName, event.at, event.id); err != nil {
			return tagging.ProjectionResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return tagging.ProjectionResult{}, err
	}
	if !result.ProjectionAsOf.IsZero() {
		result.LagSeconds = max(0, int64(time.Since(result.ProjectionAsOf).Seconds()))
	}
	return result, nil
}

type projectionEffect struct {
	event      projectionEvent
	objectType string
	objectID   string
	tagID      string
	operation  string
	source     string
	inherited  bool
	dimensions string
}

func directProjectionEffect(event projectionEvent) projectionEffect {
	return projectionEffect{event: event, objectType: event.objectType, objectID: event.objectID, tagID: event.rawTagID, operation: event.operation, source: event.source, dimensions: event.dimensions}
}

type effectiveSource struct {
	present   bool
	source    string
	inherited bool
}

func taskEffectiveTransition(ctx context.Context, tx transaction, event projectionEvent, taskID string) ([]projectionEffect, error) {
	projectID, err := currentTaskProjectID(ctx, tx, event.mspID, event.clientID, taskID)
	if err != nil {
		return nil, err
	}
	directBefore, err := canonicalTagStateBefore(ctx, tx, event, "task", taskID)
	if err != nil {
		return nil, err
	}
	projectBefore := effectiveSource{}
	if projectID != "" {
		projectBefore, err = canonicalTagStateBefore(ctx, tx, event, "project", projectID)
		if err != nil {
			return nil, err
		}
		projectBefore.inherited = projectBefore.present
	}
	directAfter, projectAfter := directBefore, projectBefore
	if event.objectType == "task" && event.objectID == taskID {
		directAfter = effectiveSource{present: event.operation == "added", source: event.source}
	}
	if event.objectType == "project" && event.objectID == projectID {
		projectAfter = effectiveSource{present: event.operation == "added", source: event.source, inherited: event.operation == "added"}
	}
	before := projectBefore
	if directBefore.present {
		before = directBefore
	}
	after := projectAfter
	if directAfter.present {
		after = directAfter
	}
	if before == after {
		return nil, nil
	}
	effects := []projectionEffect{}
	if before.present {
		effects = append(effects, projectionEffect{event: event, objectType: "task", objectID: taskID, tagID: event.rawTagID, operation: "removed", source: before.source, inherited: before.inherited, dimensions: effectDimensions(event, taskID)})
	}
	if after.present {
		effects = append(effects, projectionEffect{event: event, objectType: "task", objectID: taskID, tagID: event.rawTagID, operation: "added", source: after.source, inherited: after.inherited, dimensions: effectDimensions(event, taskID)})
	}
	return effects, nil
}

func effectDimensions(event projectionEvent, taskID string) string {
	if event.objectType == "task" {
		return event.dimensions
	}
	if value := event.taskDimensions[taskID]; len(value) > 0 {
		return string(value)
	}
	return `{}`
}

func currentTaskProjectID(ctx context.Context, tx transaction, mspID, clientID, taskID string) (string, error) {
	var projectID string
	err := tx.QueryRow(ctx, `
SELECT COALESCE(CASE WHEN task.parent_type='project' THEN task.parent_id ELSE phase.project_id END::text,'')
FROM tasks task
LEFT JOIN phases phase ON phase.id=task.parent_id AND task.parent_type='phase'
 AND phase.msp_id=task.msp_id AND phase.client_id=task.client_id
WHERE task.id=$1::uuid AND task.msp_id=$2::uuid AND task.client_id=$3::uuid
`, taskID, mspID, clientID).Scan(&projectID)
	return projectID, err
}

func canonicalTagStateBefore(ctx context.Context, tx transaction, event projectionEvent, objectType, objectID string) (effectiveSource, error) {
	var operation, source string
	err := tx.QueryRow(ctx, `
WITH RECURSIVE chain(root_id,current_id,depth) AS (
  SELECT id,id,0 FROM tags WHERE msp_id=$1::uuid
  UNION ALL
  SELECT chain.root_id,tag.merged_into_id,chain.depth+1
  FROM chain JOIN tags tag ON tag.id=chain.current_id AND tag.msp_id=$1::uuid
  WHERE tag.merged_into_id IS NOT NULL AND chain.depth<50
), survivor AS (
  SELECT DISTINCT ON (root_id) root_id,current_id
  FROM chain ORDER BY root_id,depth DESC
)
SELECT history.operation,history.assignment_source
FROM tag_assignment_events history
JOIN survivor ON survivor.root_id=history.tag_id
WHERE history.msp_id=$1::uuid
  AND history.client_id IS NOT DISTINCT FROM NULLIF($2,'')::uuid
  AND history.object_type=$3 AND history.object_id=$4::uuid
  AND survivor.current_id=$5::uuid
  AND (history.occurred_at<$6 OR (history.occurred_at=$6 AND
       (CASE history.operation WHEN 'removed' THEN 0 ELSE 1 END,
        history.id) < (CASE $8 WHEN 'removed' THEN 0 ELSE 1 END,$7::uuid)))
ORDER BY history.occurred_at DESC,
         CASE history.operation WHEN 'removed' THEN 0 ELSE 1 END DESC,
         history.id DESC LIMIT 1
`, event.mspID, event.clientID, objectType, objectID, event.tagID, event.at, event.id, event.operation).Scan(&operation, &source)
	if err != nil {
		// No earlier event is the normal absent state. The local row abstraction
		// deliberately avoids leaking pgx types into the projection domain.
		if errors.Is(err, pgx.ErrNoRows) {
			return effectiveSource{}, nil
		}
		return effectiveSource{}, err
	}
	return effectiveSource{present: operation == "added", source: source}, nil
}

func insertProjectionEffect(ctx context.Context, tx transaction, effect projectionEffect) error {
	order := 1
	if effect.operation == "removed" {
		order = 0
	}
	dimensions := effect.dimensions
	if dimensions == "" {
		dimensions = `{}`
	}
	_, err := tx.Exec(ctx, `
INSERT INTO tag_projection_effects (
 id,msp_id,client_id,source_event_id,occurred_at,object_type,object_id,
 tag_id,operation,effect_order,assignment_source,inherited,origin_object_type,origin_object_id,dimensions
) VALUES (
 md5($1||':'||$5||':'||$6||':'||$7||':'||$8||':'||$9::smallint::text)::uuid,
 $2::uuid,NULLIF($3,'')::uuid,$1::uuid,$4,$5,$6::uuid,$7::uuid,$8,$9::smallint,$10,$11,$12,$13::uuid,$14::jsonb
)
ON CONFLICT (source_event_id,object_type,object_id,tag_id,operation,assignment_source,inherited) DO NOTHING
`, effect.event.id, effect.event.mspID, effect.event.clientID, effect.event.at, effect.objectType, effect.objectID, effect.tagID, effect.operation, order, effect.source, effect.inherited, effect.event.objectType, effect.event.objectID, dimensions)
	return err
}

func rebuildTaggingAggregates(ctx context.Context, tx transaction, mspID string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM tag_usage_daily WHERE msp_id=$1::uuid`, mspID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
WITH RECURSIVE chain(root_id,current_id,depth) AS (
 SELECT id,id,0 FROM tags WHERE msp_id=$1::uuid
 UNION ALL
 SELECT chain.root_id,tag.merged_into_id,chain.depth+1 FROM chain
 JOIN tags tag ON tag.id=chain.current_id AND tag.msp_id=$1::uuid
 WHERE tag.merged_into_id IS NOT NULL AND chain.depth<50
), survivor AS (
 SELECT DISTINCT ON (root_id) root_id,current_id FROM chain ORDER BY root_id,depth DESC
), daily AS (
 SELECT effect.msp_id,effect.client_id,effect.occurred_at::date usage_date,
        survivor.current_id tag_id,effect.object_type,
        CASE WHEN effect.inherited THEN 'inherited:'||effect.assignment_source ELSE effect.assignment_source END assignment_source,
        count(*) FILTER(WHERE effect.operation='added') added_count,
        count(*) FILTER(WHERE effect.operation='removed') removed_count,
        sum(CASE WHEN effect.operation='added' THEN 1 ELSE -1 END) active_count
 FROM tag_projection_effects effect JOIN survivor ON survivor.root_id=effect.tag_id
 WHERE effect.msp_id=$1::uuid
 GROUP BY effect.msp_id,effect.client_id,effect.occurred_at::date,survivor.current_id,effect.object_type,effect.inherited,effect.assignment_source
)
INSERT INTO tag_usage_daily (id,msp_id,client_id,usage_date,tag_id,object_type,assignment_source,added_count,removed_count,active_count)
SELECT md5(msp_id::text||':'||COALESCE(client_id::text,'')||':'||usage_date::text||':'||tag_id::text||':'||object_type||':'||assignment_source)::uuid,
       msp_id,client_id,usage_date,tag_id,object_type,assignment_source,added_count,removed_count,active_count
FROM daily
`, mspID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM tag_cooccurrence_daily WHERE msp_id=$1::uuid`, mspID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
WITH RECURSIVE chain(root_id,current_id,depth) AS (
 SELECT id,id,0 FROM tags WHERE msp_id=$1::uuid
 UNION ALL
 SELECT chain.root_id,tag.merged_into_id,chain.depth+1 FROM chain
 JOIN tags tag ON tag.id=chain.current_id AND tag.msp_id=$1::uuid
 WHERE tag.merged_into_id IS NOT NULL AND chain.depth<50
), survivor AS (
 SELECT DISTINCT ON (root_id) root_id,current_id FROM chain ORDER BY root_id,depth DESC
), canonical_effect AS (
 SELECT effect.*,survivor.current_id canonical_tag_id
 FROM tag_projection_effects effect JOIN survivor ON survivor.root_id=effect.tag_id
 WHERE effect.msp_id=$1::uuid
), pair_delta AS (
 SELECT effect.msp_id,effect.client_id,effect.occurred_at::date usage_date,
        LEAST(effect.canonical_tag_id,other.canonical_tag_id) left_tag_id,
        GREATEST(effect.canonical_tag_id,other.canonical_tag_id) right_tag_id,
        effect.object_type,
        CASE WHEN effect.operation='added' THEN 1 ELSE -1 END delta
 FROM canonical_effect effect
 JOIN LATERAL (
   SELECT DISTINCT ON (prior.canonical_tag_id) prior.canonical_tag_id,prior.operation
   FROM canonical_effect prior
   WHERE prior.client_id IS NOT DISTINCT FROM effect.client_id
     AND prior.object_type=effect.object_type AND prior.object_id=effect.object_id
     AND (prior.occurred_at,prior.effect_order,prior.id)<(effect.occurred_at,effect.effect_order,effect.id)
   ORDER BY prior.canonical_tag_id,prior.occurred_at DESC,prior.effect_order DESC,prior.id DESC
 ) other ON other.operation='added' AND other.canonical_tag_id<>effect.canonical_tag_id
), daily AS (
 SELECT msp_id,client_id,usage_date,left_tag_id,right_tag_id,object_type,sum(delta) object_count
 FROM pair_delta GROUP BY msp_id,client_id,usage_date,left_tag_id,right_tag_id,object_type
 HAVING sum(delta)<>0
)
INSERT INTO tag_cooccurrence_daily (id,msp_id,client_id,usage_date,left_tag_id,right_tag_id,object_type,object_count)
SELECT md5(msp_id::text||':'||COALESCE(client_id::text,'')||':'||usage_date::text||':'||left_tag_id::text||':'||right_tag_id::text||':'||object_type)::uuid,
       msp_id,client_id,usage_date,left_tag_id,right_tag_id,object_type,object_count
FROM daily
`, mspID)
	return err
}

func currentProjectTaskIDs(ctx context.Context, tx transaction, event projectionEvent) ([]string, error) {
	rows, err := tx.Query(ctx, `
SELECT task.id::text
FROM tasks task
LEFT JOIN phases phase
  ON phase.id=task.parent_id AND task.parent_type='phase'
 AND phase.msp_id=task.msp_id AND phase.client_id=task.client_id
WHERE task.msp_id=$1::uuid AND task.client_id=$2::uuid
  AND ((task.parent_type='project' AND task.parent_id=$3::uuid)
    OR (task.parent_type='phase' AND phase.project_id=$3::uuid))
ORDER BY task.id
`, event.mspID, event.clientID, event.objectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *TaggingProjectionRepository) ClassificationReport(ctx context.Context, mspID string, kind tagging.ReportKind, filter tagging.ReportFilter) (tagging.Report, error) {
	report := tagging.Report{Kind: kind, Rows: []tagging.ReportRow{}}
	if err := r.db.QueryRow(ctx, `
SELECT COALESCE(max(last_occurred_at),'1970-01-01'::timestamptz)
FROM tag_projection_cursors
WHERE msp_id=$1::uuid AND projection_name=$2
`, mspID, taggingProjectionName).Scan(&report.ProjectionAsOf); err != nil {
		return tagging.Report{}, err
	}
	offset := 0
	queryHash := reportQueryHash(mspID, kind, "", filter)
	if filter.Cursor != "" {
		cursor, err := decodeReportCursor(filter.Cursor)
		if err != nil || cursor.QueryHash != queryHash || cursor.Offset < 0 || cursor.AsOf.After(report.ProjectionAsOf) {
			return tagging.Report{}, tagging.ErrInvalidReportFilter
		}
		offset = cursor.Offset
		report.ProjectionAsOf = cursor.AsOf
		if cursor.SnapshotID != "" {
			return r.reportSnapshotPage(ctx, mspID, filter.ClientID, kind, queryHash, cursor, filter.Limit)
		}
	}
	requestedLimit := filter.Limit
	var (
		resultRows rows
		err        error
	)
	switch kind {
	case tagging.ReportUsage:
		resultRows, err = r.usageReportRows(ctx, mspID, filter, report.ProjectionAsOf, offset)
	case tagging.ReportTrends:
		resultRows, err = r.trendReportRows(ctx, mspID, filter, report.ProjectionAsOf, offset)
	case tagging.ReportCombinations:
		resultRows, err = r.combinationReportRows(ctx, mspID, filter, report.ProjectionAsOf, offset)
	case tagging.ReportHealth:
		resultRows, err = r.healthReportRows(ctx, mspID, filter, report.ProjectionAsOf, offset)
	case tagging.ReportRecurring:
		resultRows, err = r.recurringReportRows(ctx, mspID, filter, report.ProjectionAsOf, offset)
	default:
		return tagging.Report{}, tagging.ErrInvalidReportFilter
	}
	if err != nil {
		return tagging.Report{}, err
	}
	defer resultRows.Close()
	for resultRows.Next() {
		row, scanErr := scanClassificationReportRow(resultRows, kind, filter, mspID, report.ProjectionAsOf, requestedLimit)
		err = scanErr
		if err != nil {
			return tagging.Report{}, err
		}
		report.Rows = append(report.Rows, row)
	}
	if err := resultRows.Err(); err != nil {
		return tagging.Report{}, err
	}
	if len(report.Rows) > requestedLimit {
		resultRows.Close()
		snapshotID, err := r.materializeReportSnapshot(ctx, mspID, filter.ClientID, string(kind), queryHash, report.ProjectionAsOf, func(limit int) (rows, error) {
			materializationFilter := filter
			materializationFilter.Limit = limit
			switch kind {
			case tagging.ReportUsage:
				return r.usageReportRows(ctx, mspID, materializationFilter, report.ProjectionAsOf, 0)
			case tagging.ReportTrends:
				return r.trendReportRows(ctx, mspID, materializationFilter, report.ProjectionAsOf, 0)
			case tagging.ReportCombinations:
				return r.combinationReportRows(ctx, mspID, materializationFilter, report.ProjectionAsOf, 0)
			case tagging.ReportHealth:
				return r.healthReportRows(ctx, mspID, materializationFilter, report.ProjectionAsOf, 0)
			case tagging.ReportRecurring:
				return r.recurringReportRows(ctx, mspID, materializationFilter, report.ProjectionAsOf, 0)
			default:
				return nil, tagging.ErrInvalidReportFilter
			}
		}, func(source rows) (any, error) {
			return scanClassificationReportRow(source, kind, filter, mspID, report.ProjectionAsOf, requestedLimit)
		})
		if err != nil {
			return tagging.Report{}, err
		}
		return r.reportSnapshotPage(ctx, mspID, filter.ClientID, kind, queryHash, reportCursor{Offset: 0, AsOf: report.ProjectionAsOf, QueryHash: queryHash, SnapshotID: snapshotID}, requestedLimit)
	}
	return report, nil
}

func (r *TaggingProjectionRepository) ClassificationEvidence(ctx context.Context, mspID, tagID string, filter tagging.ReportFilter) (tagging.EvidencePage, error) {
	page := tagging.EvidencePage{Items: []tagging.EvidenceRef{}}
	if err := r.db.QueryRow(ctx, `SELECT COALESCE(max(last_occurred_at),'1970-01-01'::timestamptz) FROM tag_projection_cursors WHERE msp_id=$1::uuid AND projection_name=$2`, mspID, taggingProjectionName).Scan(&page.ProjectionAsOf); err != nil {
		return tagging.EvidencePage{}, err
	}
	offset := 0
	queryHash := reportQueryHash(mspID, tagging.ReportRecurring, tagID, filter)
	if filter.Cursor != "" {
		cursor, err := decodeReportCursor(filter.Cursor)
		if err != nil || cursor.QueryHash != queryHash || cursor.Offset < 0 || cursor.AsOf.After(page.ProjectionAsOf) {
			return tagging.EvidencePage{}, tagging.ErrInvalidReportFilter
		}
		offset, page.ProjectionAsOf = cursor.Offset, cursor.AsOf
		if cursor.SnapshotID != "" {
			return r.evidenceSnapshotPage(ctx, mspID, filter.ClientID, tagID, queryHash, cursor, filter.Limit)
		}
	}
	requestedLimit := filter.Limit
	result, err := r.classificationEvidenceRows(ctx, mspID, tagID, filter, page.ProjectionAsOf, offset)
	if err != nil {
		return tagging.EvidencePage{}, err
	}
	defer result.Close()
	for result.Next() {
		var item tagging.EvidenceRef
		if err := result.Scan(&item.ObjectType, &item.ObjectID, &item.Label, &item.ClientID, &item.ParentObjectID); err != nil {
			return tagging.EvidencePage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err := result.Err(); err != nil {
		return tagging.EvidencePage{}, err
	}
	if len(page.Items) > requestedLimit {
		result.Close()
		kind := "evidence:" + tagID
		snapshotID, err := r.materializeReportSnapshot(ctx, mspID, filter.ClientID, kind, queryHash, page.ProjectionAsOf, func(limit int) (rows, error) {
			materializationFilter := filter
			materializationFilter.Limit = limit
			return r.classificationEvidenceRows(ctx, mspID, tagID, materializationFilter, page.ProjectionAsOf, 0)
		}, func(source rows) (any, error) {
			var item tagging.EvidenceRef
			if err := source.Scan(&item.ObjectType, &item.ObjectID, &item.Label, &item.ClientID, &item.ParentObjectID); err != nil {
				return nil, err
			}
			return item, nil
		})
		if err != nil {
			return tagging.EvidencePage{}, err
		}
		return r.evidenceSnapshotPage(ctx, mspID, filter.ClientID, tagID, queryHash, reportCursor{Offset: 0, AsOf: page.ProjectionAsOf, QueryHash: queryHash, SnapshotID: snapshotID}, requestedLimit)
	}
	return page, nil
}

func (r *TaggingProjectionRepository) evidenceSnapshotPage(ctx context.Context, mspID, clientID, tagID, queryHash string, cursor reportCursor, limit int) (tagging.EvidencePage, error) {
	var asOf time.Time
	result, err := r.db.Query(ctx, `SELECT row.payload,snapshot.projection_as_of
FROM tag_report_snapshots snapshot
JOIN tag_report_snapshot_rows row ON row.snapshot_id=snapshot.id
WHERE snapshot.id=$1::uuid AND snapshot.msp_id=$2::uuid AND snapshot.client_id=$3::uuid
  AND snapshot.report_kind=$4 AND snapshot.query_hash=$5
  AND snapshot.materialization_state='ready' AND snapshot.expires_at>now()
  AND row.ordinal >= $6
ORDER BY row.ordinal LIMIT $7`, cursor.SnapshotID, mspID, clientID, "evidence:"+tagID, queryHash, cursor.Offset, limit+1)
	if err != nil {
		return tagging.EvidencePage{}, tagging.ErrInvalidReportFilter
	}
	defer result.Close()
	page := tagging.EvidencePage{Items: []tagging.EvidenceRef{}}
	for result.Next() {
		var payload []byte
		if err := result.Scan(&payload, &asOf); err != nil {
			return tagging.EvidencePage{}, tagging.ErrInvalidReportFilter
		}
		var item tagging.EvidenceRef
		if json.Unmarshal(payload, &item) != nil {
			return tagging.EvidencePage{}, tagging.ErrInvalidReportFilter
		}
		page.Items = append(page.Items, item)
	}
	if result.Err() != nil || len(page.Items) == 0 && cursor.Offset > 0 {
		return tagging.EvidencePage{}, tagging.ErrInvalidReportFilter
	}
	page.ProjectionAsOf = asOf
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor = encodeReportCursor(reportCursor{Offset: cursor.Offset + limit, AsOf: asOf, QueryHash: queryHash, SnapshotID: cursor.SnapshotID})
	}
	return page, nil
}

type reportCursor struct {
	Offset     int       `json:"offset"`
	AsOf       time.Time `json:"as_of"`
	QueryHash  string    `json:"query_hash"`
	SnapshotID string    `json:"snapshot_id,omitempty"`
}

func (r *TaggingProjectionRepository) reportSnapshotPage(ctx context.Context, mspID, clientID string, kind tagging.ReportKind, queryHash string, cursor reportCursor, limit int) (tagging.Report, error) {
	var asOf time.Time
	result, err := r.db.Query(ctx, `SELECT row.payload,snapshot.projection_as_of
FROM tag_report_snapshots snapshot
JOIN tag_report_snapshot_rows row ON row.snapshot_id=snapshot.id
WHERE snapshot.id=$1::uuid AND snapshot.msp_id=$2::uuid AND snapshot.client_id=$3::uuid
  AND snapshot.report_kind=$4 AND snapshot.query_hash=$5
  AND snapshot.materialization_state='ready' AND snapshot.expires_at>now()
  AND row.ordinal >= $6
ORDER BY row.ordinal LIMIT $7`, cursor.SnapshotID, mspID, clientID, string(kind), queryHash, cursor.Offset, limit+1)
	if err != nil {
		return tagging.Report{}, tagging.ErrInvalidReportFilter
	}
	defer result.Close()
	report := tagging.Report{Kind: kind, Rows: []tagging.ReportRow{}}
	for result.Next() {
		var payload []byte
		if err := result.Scan(&payload, &asOf); err != nil {
			return tagging.Report{}, tagging.ErrInvalidReportFilter
		}
		var row tagging.ReportRow
		if json.Unmarshal(payload, &row) != nil {
			return tagging.Report{}, tagging.ErrInvalidReportFilter
		}
		report.Rows = append(report.Rows, row)
	}
	if result.Err() != nil || len(report.Rows) == 0 && cursor.Offset > 0 {
		return tagging.Report{}, tagging.ErrInvalidReportFilter
	}
	report.ProjectionAsOf = asOf
	if len(report.Rows) > limit {
		report.Rows = report.Rows[:limit]
		report.NextCursor = encodeReportCursor(reportCursor{Offset: cursor.Offset + limit, AsOf: asOf, QueryHash: queryHash, SnapshotID: cursor.SnapshotID})
	}
	return report, nil
}

func scanClassificationReportRow(source rows, kind tagging.ReportKind, filter tagging.ReportFilter, mspID string, asOf time.Time, pageLimit int) (tagging.ReportRow, error) {
	var row tagging.ReportRow
	var err error
	switch kind {
	case tagging.ReportUsage:
		err = source.Scan(&row.TagID, &row.ObjectType, &row.Date, &row.Count)
	case tagging.ReportTrends:
		err = source.Scan(&row.TagID, &row.ObjectType, &row.Date, &row.AddedCount, &row.RemovedCount, &row.ActiveCount)
		row.Count = row.ActiveCount
	case tagging.ReportCombinations:
		err = source.Scan(&row.LeftTagID, &row.RightTagID, &row.ObjectType, &row.Date, &row.Count)
	case tagging.ReportHealth:
		err = source.Scan(&row.ObjectType, &row.Source, &row.Count, &row.AgeSeconds)
	case tagging.ReportRecurring:
		err = source.Scan(&row.TagID, &row.ObjectType, &row.Count, &row.ObjectIDs, &row.EvidenceCursor)
		if err == nil && row.Count > int64(len(row.ObjectIDs)) {
			evidenceFilter := filter
			evidenceFilter.ObjectType = row.ObjectType
			evidenceFilter.Limit = pageLimit
			row.EvidenceCursor = encodeReportCursor(reportCursor{Offset: len(row.ObjectIDs), AsOf: asOf, QueryHash: reportQueryHash(mspID, tagging.ReportRecurring, row.TagID, evidenceFilter)})
		}
	default:
		err = tagging.ErrInvalidReportFilter
	}
	return row, err
}

type reportSnapshotQuery func(limit int) (rows, error)
type reportSnapshotScan func(rows) (any, error)

// materializeReportSnapshot publishes immutable continuation rows only after
// the complete bounded result has been stored. Building snapshots reserve
// their worst-case quota and are never readable by continuation requests.
func (r *TaggingProjectionRepository) materializeReportSnapshot(ctx context.Context, mspID, clientID, kind, queryHash string, asOf time.Time, query reportSnapshotQuery, scan reportSnapshotScan) (string, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		_ = tx.Rollback(ctx)
		return "", err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('tag-report-snapshot-global',0))`); err != nil {
		return fail(err)
	}
	// Expired parents still participate in the materialization-key unique
	// index, so remove the requested key independently of backlog ordering.
	if _, err := tx.Exec(ctx, `
DELETE FROM tag_report_snapshots
WHERE msp_id=$1::uuid AND client_id=$2::uuid AND report_kind=$3
  AND query_hash=$4 AND projection_as_of=$5 AND expires_at<=now()
`, mspID, clientID, kind, queryHash, asOf); err != nil {
		return fail(err)
	}
	// Bound every delete/lock operation while draining all currently expired
	// storage during this maintenance opportunity.
	for {
		if deleted, err := tx.Exec(ctx, `
WITH expired AS (
  SELECT id FROM tag_report_snapshots
  WHERE expires_at <= now()
  ORDER BY expires_at,id
  FOR UPDATE SKIP LOCKED
  LIMIT $1
)
DELETE FROM tag_report_snapshots snapshot USING expired WHERE snapshot.id=expired.id
`, reportSnapshotCleanupBatch); err != nil {
			return fail(err)
		} else if deleted.RowsAffected() < reportSnapshotCleanupBatch {
			break
		}
	}
	var snapshotID, state string
	err = tx.QueryRow(ctx, `
SELECT id::text,materialization_state FROM tag_report_snapshots
WHERE msp_id=$1::uuid AND client_id=$2::uuid AND report_kind=$3
  AND query_hash=$4 AND projection_as_of=$5 AND expires_at>now()
`, mspID, clientID, kind, queryHash, asOf).Scan(&snapshotID, &state)
	if err == nil {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return "", commitErr
		}
		if state == "building" {
			return r.waitForReadyReportSnapshot(ctx, snapshotID)
		}
		if state != "ready" {
			return "", tagging.ErrReportCapacityExceeded
		}
		return snapshotID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fail(err)
	}
	var globalRows, globalBytes int64
	if err := tx.QueryRow(ctx, `
SELECT COALESCE(sum(total_rows),0),COALESCE(sum(payload_bytes),0)
FROM tag_report_snapshots WHERE expires_at>now()
`).Scan(&globalRows, &globalBytes); err != nil {
		return fail(err)
	}
	if globalRows > reportSnapshotGlobalRows-reportSnapshotMaxRows || globalBytes > reportSnapshotGlobalBytes-reportSnapshotMaxBytes {
		return fail(tagging.ErrReportCapacityExceeded)
	}
	err = tx.QueryRow(ctx, `
INSERT INTO tag_report_snapshots(
  id,msp_id,client_id,report_kind,query_hash,payload,projection_as_of,
  total_rows,payload_bytes,materialization_state,expires_at
) VALUES(md5(random()::text||clock_timestamp()::text)::uuid,$1::uuid,$2::uuid,$3,$4,NULL,$5,$6,$7,'building',now()+interval '30 minutes')
RETURNING id::text
`, mspID, clientID, kind, queryHash, asOf, reportSnapshotMaxRows, reportSnapshotMaxBytes).Scan(&snapshotID)
	if err != nil {
		return fail(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}

	result, err := query(reportSnapshotMaxRows)
	if err != nil {
		r.deleteReportSnapshot(ctx, snapshotID)
		return "", err
	}
	defer result.Close()
	batch := make([]json.RawMessage, 0, reportSnapshotBatchRows)
	var ordinal, payloadBytes int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		value, marshalErr := json.Marshal(batch)
		if marshalErr != nil {
			return marshalErr
		}
		batchTx, beginErr := r.db.Begin(ctx)
		if beginErr != nil {
			return beginErr
		}
		base := ordinal - int64(len(batch))
		_, execErr := batchTx.Exec(ctx, `
INSERT INTO tag_report_snapshot_rows(snapshot_id,ordinal,payload,payload_bytes)
SELECT $1::uuid,$2::bigint+item.ordinality-1,item.value,octet_length(item.value::text)
FROM jsonb_array_elements($3::jsonb) WITH ORDINALITY item(value,ordinality)
`, snapshotID, base, value)
		if execErr != nil {
			_ = batchTx.Rollback(ctx)
			return execErr
		}
		if commitErr := batchTx.Commit(ctx); commitErr != nil {
			_ = batchTx.Rollback(ctx)
			return commitErr
		}
		batch = batch[:0]
		return nil
	}
	for result.Next() {
		if ordinal >= reportSnapshotMaxRows {
			result.Close()
			r.deleteReportSnapshot(ctx, snapshotID)
			return "", tagging.ErrReportCapacityExceeded
		}
		item, scanErr := scan(result)
		if scanErr != nil {
			r.deleteReportSnapshot(ctx, snapshotID)
			return "", scanErr
		}
		payload, marshalErr := json.Marshal(item)
		if marshalErr != nil {
			r.deleteReportSnapshot(ctx, snapshotID)
			return "", marshalErr
		}
		if int64(len(payload)) > reportSnapshotMaxBytes-payloadBytes {
			result.Close()
			r.deleteReportSnapshot(ctx, snapshotID)
			return "", tagging.ErrReportCapacityExceeded
		}
		payloadBytes += int64(len(payload))
		ordinal++
		batch = append(batch, payload)
		if len(batch) == cap(batch) {
			if err := flush(); err != nil {
				r.deleteReportSnapshot(ctx, snapshotID)
				return "", err
			}
		}
	}
	if err := result.Err(); err != nil {
		r.deleteReportSnapshot(ctx, snapshotID)
		return "", err
	}
	if err := flush(); err != nil {
		r.deleteReportSnapshot(ctx, snapshotID)
		return "", err
	}
	finalTx, err := r.db.Begin(ctx)
	if err != nil {
		r.deleteReportSnapshot(ctx, snapshotID)
		return "", err
	}
	var storedRows, storedBytes int64
	if err := finalTx.QueryRow(ctx, `
SELECT count(*),COALESCE(sum(payload_bytes),0)
FROM tag_report_snapshot_rows WHERE snapshot_id=$1::uuid
`, snapshotID).Scan(&storedRows, &storedBytes); err != nil {
		_ = finalTx.Rollback(ctx)
		r.deleteReportSnapshot(ctx, snapshotID)
		return "", err
	}
	if storedRows != ordinal || storedRows > reportSnapshotMaxRows || storedBytes > reportSnapshotMaxBytes {
		_ = finalTx.Rollback(ctx)
		r.deleteReportSnapshot(ctx, snapshotID)
		return "", tagging.ErrReportCapacityExceeded
	}
	tag, err := finalTx.Exec(ctx, `
UPDATE tag_report_snapshots
SET total_rows=$2,payload_bytes=$3,materialization_state='ready',expires_at=now()+interval '30 minutes'
WHERE id=$1::uuid AND materialization_state='building'
`, snapshotID, storedRows, storedBytes)
	if err != nil || tag.RowsAffected() != 1 {
		_ = finalTx.Rollback(ctx)
		r.deleteReportSnapshot(ctx, snapshotID)
		if err != nil {
			return "", err
		}
		return "", tagging.ErrReportCapacityExceeded
	}
	if err := finalTx.Commit(ctx); err != nil {
		r.deleteReportSnapshot(ctx, snapshotID)
		return "", err
	}
	return snapshotID, nil
}

func (r *TaggingProjectionRepository) waitForReadyReportSnapshot(ctx context.Context, snapshotID string) (string, error) {
	timer := time.NewTimer(reportSnapshotBuildWait)
	defer timer.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var state string
		var active bool
		err := r.db.QueryRow(ctx, `
SELECT materialization_state,expires_at>now()
FROM tag_report_snapshots WHERE id=$1::uuid
`, snapshotID).Scan(&state, &active)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", tagging.ErrReportCapacityExceeded
			}
			return "", err
		}
		if state == "ready" {
			if active {
				return snapshotID, nil
			}
			return "", tagging.ErrReportCapacityExceeded
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
			return "", tagging.ErrReportCapacityExceeded
		case <-ticker.C:
		}
	}
}

func (r *TaggingProjectionRepository) deleteReportSnapshot(ctx context.Context, snapshotID string) {
	tx, err := r.db.Begin(context.WithoutCancel(ctx))
	if err != nil {
		return
	}
	if _, err := tx.Exec(context.WithoutCancel(ctx), `DELETE FROM tag_report_snapshots WHERE id=$1::uuid`, snapshotID); err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return
	}
	_ = tx.Commit(context.WithoutCancel(ctx))
}

func reportQueryHash(mspID string, kind tagging.ReportKind, target string, filter tagging.ReportFilter) string {
	filter.Cursor = ""
	value, _ := json.Marshal(struct {
		MSPID  string
		Kind   tagging.ReportKind
		Target string
		Filter tagging.ReportFilter
	}{mspID, kind, target, filter})
	return fmt.Sprintf("%x", sha256.Sum256(value))
}

func reportMaterializationLimit(pageLimit int) any {
	if pageLimit == 0 {
		return nil
	}
	return pageLimit + 1
}

func encodeReportCursor(cursor reportCursor) string {
	value, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(value)
}

func decodeReportCursor(value string) (reportCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return reportCursor{}, err
	}
	var cursor reportCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.AsOf.IsZero() || cursor.QueryHash == "" {
		return reportCursor{}, tagging.ErrInvalidReportFilter
	}
	return cursor, nil
}

func effectReportBase(mspID string, filter tagging.ReportFilter, asOf time.Time) ([]any, string, string) {
	toExclusive := filter.To.AddDate(0, 0, 1)
	snapshotExclusive := toExclusive
	if asOf.Before(toExclusive) {
		snapshotExclusive = asOf.Add(time.Microsecond)
	}
	args := []any{mspID, filter.ClientID, filter.From, toExclusive, snapshotExclusive}
	objectClauses := []string{"true"}
	activeClauses := []string{"true"}
	if filter.ObjectType != "" {
		args = append(args, filter.ObjectType.String())
		objectClauses = append(objectClauses, fmt.Sprintf("object.object_type=$%d", len(args)))
	}
	if filter.TechnicianID != "" {
		args = append(args, filter.TechnicianID)
		objectClauses = append(objectClauses, fmt.Sprintf("object.dimensions->>'technician_id'=$%d", len(args)))
	}
	if filter.TeamID != "" {
		args = append(args, filter.TeamID)
		objectClauses = append(objectClauses, fmt.Sprintf("object.dimensions->'team_ids' ? $%d", len(args)))
	}
	if filter.Priority != "" {
		args = append(args, filter.Priority)
		objectClauses = append(objectClauses, fmt.Sprintf("object.dimensions->>'priority'=$%d", len(args)))
	}
	if filter.Status != "" {
		args = append(args, filter.Status)
		objectClauses = append(objectClauses, fmt.Sprintf("object.dimensions->>'status'=$%d", len(args)))
	}
	if len(filter.TagIDs) > 0 {
		args = append(args, filter.TagIDs)
		n := len(args)
		switch filter.Match {
		case "all":
			objectClauses = append(objectClauses, fmt.Sprintf("(SELECT count(DISTINCT selected.canonical_tag_id) FROM active selected WHERE selected.object_type=object.object_type AND selected.object_id=object.object_id AND selected.canonical_tag_id=ANY($%d::uuid[]))=cardinality($%d::uuid[])", n, n))
		case "none":
			objectClauses = append(objectClauses, fmt.Sprintf("NOT EXISTS(SELECT 1 FROM active selected WHERE selected.object_type=object.object_type AND selected.object_id=object.object_id AND selected.canonical_tag_id=ANY($%d::uuid[]))", n))
		default:
			objectClauses = append(objectClauses, fmt.Sprintf("EXISTS(SELECT 1 FROM active selected WHERE selected.object_type=object.object_type AND selected.object_id=object.object_id AND selected.canonical_tag_id=ANY($%d::uuid[]))", n))
		}
	}
	if filter.Source != "" {
		args = append(args, filter.Source.String())
		activeClauses = append(activeClauses, fmt.Sprintf("active.assignment_source=$%d", len(args)))
	}
	if filter.Inheritance == "direct" {
		activeClauses = append(activeClauses, "active.inherited=false")
	} else if filter.Inheritance == "inherited" {
		activeClauses = append(activeClauses, "active.inherited=true")
	}
	if filter.GroupID != "" {
		args = append(args, filter.GroupID)
		activeClauses = append(activeClauses, fmt.Sprintf("tag.group_id=$%d::uuid", len(args)))
	}
	cte := fmt.Sprintf(`WITH RECURSIVE chain(root_id,current_id,depth) AS (
 SELECT id,id,0 FROM tags WHERE msp_id=$1::uuid
 UNION ALL SELECT chain.root_id,tag.merged_into_id,chain.depth+1 FROM chain
 JOIN tags tag ON tag.id=chain.current_id AND tag.msp_id=$1::uuid
 WHERE tag.merged_into_id IS NOT NULL AND chain.depth<50
), survivor AS (
 SELECT DISTINCT ON(root_id) root_id,current_id FROM chain ORDER BY root_id,depth DESC
), canonical_effect AS (
 SELECT effect.*,survivor.current_id canonical_tag_id
 FROM tag_projection_effects effect JOIN survivor ON survivor.root_id=effect.tag_id
 WHERE effect.msp_id=$1::uuid AND effect.client_id=$2::uuid
   AND $3::timestamptz IS NOT NULL AND $4::timestamptz IS NOT NULL
   AND effect.occurred_at < $5::timestamptz
), latest AS (
 SELECT DISTINCT ON(object_type,object_id,canonical_tag_id) * FROM canonical_effect
 ORDER BY object_type,object_id,canonical_tag_id,occurred_at DESC,effect_order DESC,id DESC
), active AS (
 SELECT * FROM latest WHERE operation='added'
), object AS (
 SELECT DISTINCT ON(object_type,object_id) object_type,object_id,dimensions
 FROM canonical_effect ORDER BY object_type,object_id,occurred_at DESC,effect_order DESC,id DESC
), eligible_object AS (
 SELECT object.object_type,object.object_id FROM object
 LEFT JOIN work_records work ON object.object_type='work_record' AND work.id=object.object_id AND work.msp_id=$1::uuid AND work.client_id=$2::uuid
 LEFT JOIN queues work_queue ON work_queue.id=work.queue_id AND work_queue.msp_id=work.msp_id
 LEFT JOIN tasks task ON object.object_type='task' AND task.id=object.object_id AND task.msp_id=$1::uuid AND task.client_id=$2::uuid
 LEFT JOIN work_records task_work ON task.parent_type='work_record' AND task_work.id=task.parent_id AND task_work.msp_id=task.msp_id AND task_work.client_id=task.client_id
 LEFT JOIN queues task_queue ON task_queue.id=task_work.queue_id AND task_queue.msp_id=task_work.msp_id
 LEFT JOIN time_entries entry ON object.object_type='time_entry' AND entry.id=object.object_id AND entry.msp_id=$1::uuid AND entry.client_id=$2::uuid
 LEFT JOIN work_records entry_work ON entry_work.id=entry.work_record_id AND entry_work.msp_id=entry.msp_id AND entry_work.client_id=entry.client_id
 LEFT JOIN queues entry_queue ON entry_queue.id=entry_work.queue_id AND entry_queue.msp_id=entry_work.msp_id
 LEFT JOIN projects project ON object.object_type='project' AND project.id=object.object_id AND project.msp_id=$1::uuid AND project.client_id=$2::uuid
 LEFT JOIN assets asset ON object.object_type='asset' AND asset.id=object.object_id AND asset.msp_id=$1::uuid AND asset.client_id=$2::uuid
 LEFT JOIN knowledge_articles article ON object.object_type='knowledge_article' AND article.id=object.object_id AND article.msp_id=$1::uuid AND article.client_id=$2::uuid
 WHERE %s
)`, strings.Join(objectClauses, " AND "))
	return args, cte, strings.Join(activeClauses, " AND ")
}

func (r *TaggingProjectionRepository) classificationEvidenceRows(ctx context.Context, mspID, tagID string, filter tagging.ReportFilter, asOf time.Time, offset int) (rows, error) {
	args, cte, activeWhere := effectReportBase(mspID, filter, asOf)
	args = append(args, tagID, reportMaterializationLimit(filter.Limit), offset)
	return r.db.Query(ctx, fmt.Sprintf(`%s
SELECT active.object_type,active.object_id::text,
 COALESCE(work.display_id||' · '||work.title,task.title,project.name,asset.name,article.title,'Time entry '||active.object_id::text),
 COALESCE(COALESCE(work.client_id,task.client_id,project.client_id,asset.client_id,entry.client_id)::text,''),
 COALESCE(CASE WHEN task.parent_type='project' THEN task.parent_id ELSE phase.project_id END::text,'')
FROM active JOIN eligible_object object USING(object_type,object_id)
JOIN tags tag ON tag.id=active.canonical_tag_id AND tag.msp_id=$1::uuid
LEFT JOIN work_records work ON active.object_type='work_record' AND work.id=active.object_id AND work.msp_id=$1::uuid
LEFT JOIN tasks task ON active.object_type='task' AND task.id=active.object_id AND task.msp_id=$1::uuid
LEFT JOIN phases phase ON task.parent_type='phase' AND phase.id=task.parent_id AND phase.msp_id=task.msp_id
LEFT JOIN projects project ON active.object_type='project' AND project.id=active.object_id AND project.msp_id=$1::uuid
LEFT JOIN assets asset ON active.object_type='asset' AND asset.id=active.object_id AND asset.msp_id=$1::uuid
LEFT JOIN knowledge_articles article ON active.object_type='knowledge_article' AND article.id=active.object_id AND article.msp_id=$1::uuid
LEFT JOIN time_entries entry ON active.object_type='time_entry' AND entry.id=active.object_id AND entry.msp_id=$1::uuid
WHERE active.canonical_tag_id=$%d::uuid AND active.object_type=$6 AND %s
ORDER BY active.object_type,active.object_id
LIMIT $%d OFFSET $%d`, cte, len(args)-2, activeWhere, len(args)-1, len(args)), args...)
}

func (r *TaggingProjectionRepository) usageReportRows(ctx context.Context, mspID string, filter tagging.ReportFilter, asOf time.Time, offset int) (rows, error) {
	args, cte, activeWhere := effectReportBase(mspID, filter, asOf)
	args = append(args, reportMaterializationLimit(filter.Limit), offset)
	return r.db.Query(ctx, fmt.Sprintf(`%s
SELECT active.canonical_tag_id::text,active.object_type,($4::timestamptz-interval '1 day')::date::text,count(DISTINCT active.object_id)
FROM active JOIN eligible_object object USING(object_type,object_id)
JOIN tags tag ON tag.id=active.canonical_tag_id AND tag.msp_id=$1::uuid
WHERE %s
GROUP BY active.canonical_tag_id,active.object_type
ORDER BY count(DISTINCT active.object_id) DESC,active.canonical_tag_id,active.object_type
LIMIT $%d OFFSET $%d`, cte, activeWhere, len(args)-1, len(args)), args...)
}

func (r *TaggingProjectionRepository) trendReportRows(ctx context.Context, mspID string, filter tagging.ReportFilter, asOf time.Time, offset int) (rows, error) {
	args, cte, activeWhere := effectReportBase(mspID, filter, asOf)
	args = append(args, reportMaterializationLimit(filter.Limit), offset)
	return r.db.Query(ctx, fmt.Sprintf(`%s
SELECT event.canonical_tag_id::text,event.object_type,event.occurred_at::date::text,
 count(*) FILTER(WHERE event.operation='added'),count(*) FILTER(WHERE event.operation='removed'),
 sum(CASE WHEN event.operation='added' THEN 1 ELSE -1 END)
FROM canonical_effect event JOIN eligible_object object USING(object_type,object_id)
JOIN tags tag ON tag.id=event.canonical_tag_id AND tag.msp_id=$1::uuid
LEFT JOIN active ON active.id=event.id
WHERE event.occurred_at >= $3 AND event.occurred_at < $4 AND %s
GROUP BY event.canonical_tag_id,event.object_type,event.occurred_at::date
ORDER BY event.occurred_at::date DESC,event.canonical_tag_id,event.object_type
LIMIT $%d OFFSET $%d`, cte, strings.ReplaceAll(activeWhere, "active.", "event."), len(args)-1, len(args)), args...)
}

func (r *TaggingProjectionRepository) combinationReportRows(ctx context.Context, mspID string, filter tagging.ReportFilter, asOf time.Time, offset int) (rows, error) {
	args, cte, activeWhere := effectReportBase(mspID, filter, asOf)
	args = append(args, reportMaterializationLimit(filter.Limit), offset)
	return r.db.Query(ctx, fmt.Sprintf(`%s
SELECT left_tag.canonical_tag_id::text,right_tag.canonical_tag_id::text,left_tag.object_type,
 ($4::timestamptz-interval '1 day')::date::text,count(DISTINCT left_tag.object_id)
FROM active left_tag JOIN active right_tag ON right_tag.object_type=left_tag.object_type AND right_tag.object_id=left_tag.object_id AND right_tag.canonical_tag_id>left_tag.canonical_tag_id
JOIN eligible_object object ON object.object_type=left_tag.object_type AND object.object_id=left_tag.object_id
JOIN tags left_def ON left_def.id=left_tag.canonical_tag_id AND left_def.msp_id=$1::uuid
JOIN tags right_def ON right_def.id=right_tag.canonical_tag_id AND right_def.msp_id=$1::uuid
WHERE %s AND %s
GROUP BY left_tag.canonical_tag_id,right_tag.canonical_tag_id,left_tag.object_type
ORDER BY count(DISTINCT left_tag.object_id) DESC,left_tag.canonical_tag_id,right_tag.canonical_tag_id,left_tag.object_type
LIMIT $%d OFFSET $%d`, cte,
		strings.ReplaceAll(strings.ReplaceAll(activeWhere, "active.", "left_tag."), "tag.", "left_def."),
		strings.ReplaceAll(strings.ReplaceAll(activeWhere, "active.", "right_tag."), "tag.", "right_def."), len(args)-1, len(args)), args...)
}

func (r *TaggingProjectionRepository) healthReportRows(ctx context.Context, mspID string, filter tagging.ReportFilter, asOf time.Time, offset int) (rows, error) {
	args, cte, activeWhere := effectReportBase(mspID, filter, asOf)
	args = append(args, reportMaterializationLimit(filter.Limit), offset)
	return r.db.Query(ctx, fmt.Sprintf(`%s
 , filtered_active AS (
 SELECT active.*,tag.internal_key FROM active JOIN eligible_object object USING(object_type,object_id)
 JOIN tags tag ON tag.id=active.canonical_tag_id AND tag.msp_id=$1::uuid WHERE %s
 ), object_health AS (
 SELECT object_type,object_id,
   CASE WHEN bool_or(internal_key<>'taxonomy.system.unclassified') THEN 'meaningful' ELSE 'unclassified' END state,
   min(occurred_at) FILTER(WHERE internal_key='taxonomy.system.unclassified') unclassified_since
 FROM filtered_active GROUP BY object_type,object_id
 )
SELECT object_type,state,count(*),
 CASE WHEN state='unclassified' THEN EXTRACT(EPOCH FROM (LEAST($4::timestamptz,$5::timestamptz)-min(unclassified_since)))::bigint ELSE 0 END
FROM object_health GROUP BY object_type,state ORDER BY object_type,state
LIMIT $%d OFFSET $%d`, cte, activeWhere, len(args)-1, len(args)), args...)
}

func (r *TaggingProjectionRepository) recurringReportRows(ctx context.Context, mspID string, filter tagging.ReportFilter, asOf time.Time, offset int) (rows, error) {
	args, cte, activeWhere := effectReportBase(mspID, filter, asOf)
	args = append(args, reportMaterializationLimit(filter.Limit), offset)
	return r.db.Query(ctx, fmt.Sprintf(`%s
SELECT active.canonical_tag_id::text,active.object_type,count(DISTINCT active.object_id),
 ARRAY[]::text[],''::text
FROM active JOIN eligible_object object USING(object_type,object_id)
JOIN tags tag ON tag.id=active.canonical_tag_id AND tag.msp_id=$1::uuid
WHERE tag.internal_key<>'taxonomy.system.unclassified' AND %s
GROUP BY active.canonical_tag_id,active.object_type HAVING count(DISTINCT active.object_id)>1
ORDER BY count(DISTINCT active.object_id) DESC,active.canonical_tag_id,active.object_type
LIMIT $%d OFFSET $%d`, cte, activeWhere, len(args)-1, len(args)), args...)
}

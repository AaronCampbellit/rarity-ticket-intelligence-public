package psa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

type ClassificationMigrationRepository struct{ db database }

type classificationPreflightSession struct {
	tx       transaction
	snapshot tagging.ClassificationPreflightSnapshot
}

var _ tagging.MigrationRepository = (*ClassificationMigrationRepository)(nil)

func NewClassificationMigrationRepository(db database) *ClassificationMigrationRepository {
	return &ClassificationMigrationRepository{db: db}
}

func (r *ClassificationMigrationRepository) BeginClassificationPreflight(ctx context.Context, mspDisplayID string) (tagging.ClassificationPreflightSession, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (tagging.ClassificationPreflightSession, error) {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`); err != nil {
		return fail(err)
	}
	// A preflight is an operator-only cutover fence. SHARE locks allow reads but
	// hold all relevant classification writes and schema changes until the
	// exact evidence row commits.
	if _, err := tx.Exec(ctx, `LOCK TABLE msp_organizations,work_records,tasks,projects,phases,assets,knowledge_articles,time_entries,object_tag_assignments,tags,tag_projection_cursors,tag_assignment_events,tag_ai_policies,classification_migration_runs IN SHARE MODE`); err != nil {
		return fail(err)
	}
	snapshot, err := loadClassificationPreflightSnapshot(ctx, tx, mspDisplayID)
	if err != nil {
		return fail(err)
	}
	return &classificationPreflightSession{tx: tx, snapshot: snapshot}, nil
}

func loadClassificationPreflightSnapshot(ctx context.Context, db transaction, mspDisplayID string) (tagging.ClassificationPreflightSnapshot, error) {
	var snapshot tagging.ClassificationPreflightSnapshot
	err := db.QueryRow(ctx, `SELECT id::text,display_id FROM msp_organizations WHERE display_id=$1`, mspDisplayID).Scan(&snapshot.MSPID, &snapshot.MSPDisplayID)
	if errors.Is(err, pgx.ErrNoRows) {
		return tagging.ClassificationPreflightSnapshot{}, scope.ErrNotFound
	}
	if err != nil {
		return tagging.ClassificationPreflightSnapshot{}, err
	}
	snapshot.ByObjectType = make(map[tagging.ObjectType]tagging.ClassificationObjectTotals, len(tagging.SupportedObjectTypes))
	rows, err := db.Query(ctx, classificationPreflightTotalsSQL, snapshot.MSPID)
	if err != nil {
		return tagging.ClassificationPreflightSnapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var objectType tagging.ObjectType
		var totals tagging.ClassificationObjectTotals
		if err := rows.Scan(&objectType, &totals.Total, &totals.Meaningful, &totals.Unclassified, &totals.Invalid); err != nil {
			return tagging.ClassificationPreflightSnapshot{}, err
		}
		snapshot.ByObjectType[objectType] = totals
	}
	if err := rows.Err(); err != nil {
		return tagging.ClassificationPreflightSnapshot{}, err
	}
	if err := db.QueryRow(ctx, `
SELECT count(*)
FROM object_tag_assignments assignment
JOIN tags tag ON tag.id=assignment.tag_id AND tag.msp_id=assignment.msp_id
WHERE assignment.msp_id=$1::uuid AND tag.lifecycle_state IN ('merged','archived')
`, snapshot.MSPID).Scan(&snapshot.UnresolvedRetiredReferences); err != nil {
		return tagging.ClassificationPreflightSnapshot{}, err
	}
	var projectionAsOf time.Time
	if err := db.QueryRow(ctx, `
WITH cursor AS (
 SELECT last_occurred_at,last_event_id
 FROM tag_projection_cursors
 WHERE msp_id=$1::uuid AND projection_name='tagging-daily-v1'
), pending AS (
 SELECT count(*) count
 FROM tag_assignment_events event
 LEFT JOIN cursor ON true
 WHERE event.msp_id=$1::uuid AND (
   cursor.last_occurred_at IS NULL OR
   (event.occurred_at,event.id)>(cursor.last_occurred_at,cursor.last_event_id)
 )
)
SELECT COALESCE((SELECT last_occurred_at FROM cursor),'1970-01-01'::timestamptz),
       CASE WHEN (SELECT last_occurred_at FROM cursor) IS NULL THEN 0
            ELSE GREATEST(0,extract(epoch FROM now()-(SELECT last_occurred_at FROM cursor))::bigint)
       END,
       (SELECT count FROM pending)
`, snapshot.MSPID).Scan(&projectionAsOf, &snapshot.Projection.LagSeconds, &snapshot.Projection.PendingEvents); err != nil {
		return tagging.ClassificationPreflightSnapshot{}, err
	}
	if projectionAsOf.After(time.Unix(1, 0)) {
		snapshot.Projection.AsOf = projectionAsOf
	}
	if err := db.QueryRow(ctx, `
SELECT automatic_apply_enabled,automatic_apply_threshold::float8,
       COALESCE(model_profile_id::text,''),version
FROM tag_ai_policies WHERE msp_id=$1::uuid
`, snapshot.MSPID).Scan(
		&snapshot.AIPolicy.AutomaticApplyEnabled,
		&snapshot.AIPolicy.AutomaticApplyThreshold,
		&snapshot.AIPolicy.ModelProfileID,
		&snapshot.AIPolicy.Version,
	); err != nil {
		return tagging.ClassificationPreflightSnapshot{}, err
	}
	if err := db.QueryRow(ctx, `
SELECT EXISTS (
 SELECT 1 FROM information_schema.columns
 WHERE table_schema=current_schema() AND column_name='category'
   AND table_name=ANY(ARRAY['work_records','tasks','projects','assets','knowledge_articles','time_entries'])
)
`).Scan(&snapshot.DatabaseCategoryColumnsPresent); err != nil {
		return tagging.ClassificationPreflightSnapshot{}, err
	}
	err = db.QueryRow(ctx, `
SELECT id::text FROM classification_migration_runs
WHERE msp_id=$1::uuid AND status='completed' AND NOT category_source_present
  AND evidence->>'migration'='000080_tagging_classification'
  AND evidence->>'strategy'='full_cutover'
ORDER BY completed_at DESC,id DESC LIMIT 1
`, snapshot.MSPID).Scan(&snapshot.MigrationRunID)
	if errors.Is(err, pgx.ErrNoRows) {
		snapshot.TaskOneBackfillVerified = false
	} else if err != nil {
		return tagging.ClassificationPreflightSnapshot{}, err
	} else {
		snapshot.TaskOneBackfillVerified = true
	}
	return snapshot, nil
}

func (s *classificationPreflightSession) Snapshot() tagging.ClassificationPreflightSnapshot {
	return s.snapshot
}

func (s *classificationPreflightSession) RecordVerifiedNoOp(ctx context.Context, report tagging.ClassificationPreflightReport) error {
	evidence, err := json.Marshal(map[string]any{
		"verified_no_op":                             true,
		"verified_no_op_at":                          report.VerifiedNoOpAt,
		"verified_supported_object_total":            report.Totals.Total,
		"verified_meaningful_total":                  report.Totals.Meaningful,
		"verified_unclassified_total":                report.Totals.Unclassified,
		"verified_invalid_total":                     report.Totals.Invalid,
		"verified_unresolved_retired_references":     report.UnresolvedRetiredReferences,
		"verified_projection_pending_events":         report.Projection.PendingEvents,
		"verified_database_category_columns_present": report.DatabaseCategoryColumnsPresent,
		"verified_task_one_unclassified_backfill":    report.TaskOneBackfillVerified,
	})
	if err != nil {
		return err
	}
	var runID string
	err = s.tx.QueryRow(ctx, `
UPDATE classification_migration_runs
SET evidence=evidence||$2::jsonb
WHERE id=$1::uuid AND msp_id=$3::uuid AND status='completed' AND NOT category_source_present
  AND evidence->>'migration'='000080_tagging_classification'
  AND evidence->>'strategy'='full_cutover'
RETURNING id::text
`, s.snapshot.MigrationRunID, evidence, report.MSPID).Scan(&runID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("record classification preflight: %w", scope.ErrNotFound)
		}
		return err
	}
	if runID == "" {
		return scope.ErrNotFound
	}
	return nil
}

func (s *classificationPreflightSession) Commit(ctx context.Context) error {
	return s.tx.Commit(ctx)
}

func (s *classificationPreflightSession) Rollback(ctx context.Context) error {
	err := s.tx.Rollback(ctx)
	if errors.Is(err, pgx.ErrTxClosed) {
		return nil
	}
	return err
}

const classificationPreflightTotalsSQL = `
WITH object_types(object_type) AS (VALUES
 ('work_record'::text),('task'),('project'),('asset'),('knowledge_article'),('time_entry')
), supported AS (
 SELECT 'work_record'::text object_type,id,msp_id,client_id FROM work_records WHERE msp_id=$1::uuid
 UNION ALL SELECT 'task',id,msp_id,client_id FROM tasks WHERE msp_id=$1::uuid
 UNION ALL SELECT 'project',id,msp_id,client_id FROM projects WHERE msp_id=$1::uuid
 UNION ALL SELECT 'asset',id,msp_id,client_id FROM assets WHERE msp_id=$1::uuid
 UNION ALL SELECT 'knowledge_article',id,msp_id,client_id FROM knowledge_articles WHERE msp_id=$1::uuid
 UNION ALL SELECT 'time_entry',id,msp_id,client_id FROM time_entries WHERE msp_id=$1::uuid
), candidates AS (
 SELECT supported.object_type,supported.id,tag.lifecycle_state,tag.internal_key
 FROM supported
 JOIN object_tag_assignments assignment
   ON assignment.msp_id=supported.msp_id
  AND assignment.client_id IS NOT DISTINCT FROM supported.client_id
  AND assignment.object_type=supported.object_type AND assignment.object_id=supported.id
 JOIN tags tag ON tag.id=assignment.tag_id AND tag.msp_id=assignment.msp_id
 UNION ALL
 SELECT 'task',task.id,tag.lifecycle_state,tag.internal_key
 FROM tasks task
 LEFT JOIN phases phase ON task.parent_type='phase' AND phase.id=task.parent_id
   AND phase.msp_id=task.msp_id AND phase.client_id=task.client_id
 JOIN projects project ON project.msp_id=task.msp_id AND project.client_id=task.client_id
  AND ((task.parent_type='project' AND task.parent_id=project.id)
    OR (task.parent_type='phase' AND phase.project_id=project.id))
 JOIN object_tag_assignments assignment
   ON assignment.msp_id=project.msp_id AND assignment.client_id=project.client_id
  AND assignment.object_type='project' AND assignment.object_id=project.id
 JOIN tags tag ON tag.id=assignment.tag_id AND tag.msp_id=assignment.msp_id
 WHERE task.msp_id=$1::uuid
), classified AS (
 SELECT supported.object_type,supported.id,
        COALESCE(bool_or(candidates.lifecycle_state='active' AND candidates.internal_key<>'taxonomy.system.unclassified'),false) meaningful,
        COALESCE(bool_or(candidates.lifecycle_state='active' AND candidates.internal_key='taxonomy.system.unclassified'),false) unclassified
 FROM supported LEFT JOIN candidates
   ON candidates.object_type=supported.object_type AND candidates.id=supported.id
 GROUP BY supported.object_type,supported.id
), totals AS (
 SELECT object_type,count(*) total,
        count(*) FILTER(WHERE meaningful) meaningful,
        count(*) FILTER(WHERE NOT meaningful AND unclassified) unclassified,
        count(*) FILTER(WHERE NOT meaningful AND NOT unclassified) invalid
 FROM classified GROUP BY object_type
)
SELECT object_types.object_type,COALESCE(total,0),COALESCE(meaningful,0),
       COALESCE(unclassified,0),COALESCE(invalid,0)
FROM object_types LEFT JOIN totals USING(object_type)
ORDER BY object_types.object_type
`

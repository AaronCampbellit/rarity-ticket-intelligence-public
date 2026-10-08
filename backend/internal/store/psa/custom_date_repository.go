package psa

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/customfields"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type CustomDateRepository struct{ db database }

var _ customfields.DateRepository = (*CustomDateRepository)(nil)

func NewCustomDateRepository(db database) *CustomDateRepository { return &CustomDateRepository{db: db} }
func (r *CustomDateRepository) FindDateDefinition(ctx context.Context, mspID, fieldID string, objectType customfields.ObjectType) (customfields.DateDefinition, error) {
	if !internalid.ValidCanonical(mspID) || !internalid.ValidCanonical(fieldID) {
		return customfields.DateDefinition{}, customfields.ErrInvalidCustomDate
	}
	var d customfields.DateDefinition
	err := r.db.QueryRow(ctx, `SELECT id::text,msp_id::text,object_type,internal_key,label,value_kind,lifecycle_state,version FROM calendar_custom_date_fields WHERE id=$1::uuid AND msp_id=$2::uuid AND object_type=$3 AND lifecycle_state='active'`, fieldID, mspID, objectType).Scan(&d.ID, &d.MSPID, &d.ObjectType, &d.InternalKey, &d.Label, &d.ValueKind, &d.LifecycleState, &d.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return customfields.DateDefinition{}, scope.ErrNotFound
	}
	return d, err
}
func (r *CustomDateRepository) FindDateSource(ctx context.Context, target scope.Target, objectType customfields.ObjectType, id string) (customfields.SourceRecord, error) {
	if !validTargetUUIDs(target) || !internalid.ValidCanonical(id) {
		return customfields.SourceRecord{}, customfields.ErrInvalidCustomDate
	}
	table, version, err := customDateSourceTable(objectType)
	if err != nil {
		return customfields.SourceRecord{}, err
	}
	var found customfields.SourceRecord
	query := fmt.Sprintf(`SELECT source.id::text,source.msp_id::text,COALESCE(source.client_id::text,''),$4::text,source.%s FROM %s source WHERE source.id=$1::uuid AND source.msp_id=$2::uuid AND (NULLIF($3,'')::uuid IS NULL OR source.client_id=$3::uuid)`, version, table)
	err = r.db.QueryRow(ctx, query, id, target.MSPID, target.ClientID, objectType).Scan(&found.ID, &found.MSPID, &found.ClientID, &found.Type, &found.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return customfields.SourceRecord{}, scope.ErrNotFound
	}
	return found, err
}
func (r *CustomDateRepository) FindDateValue(ctx context.Context, target scope.Target, fieldID string, objectType customfields.ObjectType, objectID string) (customfields.DateValue, error) {
	if !validTargetUUIDs(target) || !internalid.ValidCanonical(fieldID) || !internalid.ValidCanonical(objectID) {
		return customfields.DateValue{}, customfields.ErrInvalidCustomDate
	}
	var v customfields.DateValue
	err := r.db.QueryRow(ctx, `SELECT id::text,field_id::text,msp_id::text,COALESCE(client_id::text,''),object_type,object_id::text,source_revision,COALESCE(timezone,''),version,date_value,timestamp_value,created_at,created_by::text,updated_at,updated_by::text FROM object_custom_date_values WHERE field_id=$1::uuid AND msp_id=$2::uuid AND object_type=$3 AND object_id=$4::uuid AND (NULLIF($5,'')::uuid IS NULL OR client_id=$5::uuid)`, fieldID, target.MSPID, objectType, objectID, target.ClientID).Scan(&v.ID, &v.FieldID, &v.MSPID, &v.ClientID, &v.ObjectType, &v.ObjectID, &v.SourceRevision, &v.Timezone, &v.Version, &v.DateValue, &v.TimestampValue, &v.CreatedAt, &v.CreatedBy, &v.UpdatedAt, &v.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return customfields.DateValue{}, scope.ErrNotFound
	}
	return v, err
}
func customDateSourceTable(v customfields.ObjectType) (string, string, error) {
	switch v {
	case customfields.ObjectWorkRecord:
		return "work_records", "version", nil
	case customfields.ObjectTask:
		return "tasks", "version", nil
	case customfields.ObjectProject:
		return "projects", "version", nil
	case customfields.ObjectAsset:
		return "assets", "version", nil
	case customfields.ObjectKnowledgeArticle:
		return "knowledge_articles", "current_version", nil
	case customfields.ObjectTimeEntry:
		return "time_entries", "version", nil
	}
	return "", "", customfields.ErrInvalidCustomDate
}
func (r *CustomDateRepository) SetDateAtomic(ctx context.Context, accepted customfields.DateMutation) error {
	if !validDateMutationUUIDs(accepted) {
		return customfields.ErrInvalidCustomDate
	}
	return r.withTransaction(ctx, func(tx transaction) error {
		v := accepted.Value
		table, versionColumn, err := customDateSourceTable(v.ObjectType)
		if err != nil {
			return err
		}
		if err = claimCalendarDomainRequest(ctx, tx, accepted.RequestID, v.MSPID, v.ClientID, "custom_date.set", accepted.IdempotencyKey, accepted.RequestFingerprint, v, v.UpdatedAt); err != nil {
			return err
		}

		var lockedKind customfields.ValueKind
		var lockedLifecycle string
		var lockedDefinitionVersion int64
		err = tx.QueryRow(ctx, `SELECT value_kind,lifecycle_state,version FROM calendar_custom_date_fields WHERE id=$1::uuid AND msp_id=$2::uuid AND object_type=$3 FOR SHARE`, accepted.Definition.ID, accepted.Definition.MSPID, accepted.Definition.ObjectType).Scan(&lockedKind, &lockedLifecycle, &lockedDefinitionVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return customfields.ErrCustomDateBindingConflict
		}
		if err != nil {
			return err
		}
		if lockedKind != accepted.Definition.ValueKind || lockedLifecycle != "active" || lockedLifecycle != accepted.Definition.LifecycleState || lockedDefinitionVersion != accepted.Definition.Version {
			return customfields.ErrCustomDateBindingConflict
		}

		var lockedID, lockedMSP, lockedClient string
		var lockedSourceVersion int64
		lockSource := fmt.Sprintf(`SELECT id::text,msp_id::text,COALESCE(client_id::text,''),%s FROM %s WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid FOR SHARE`, versionColumn, table)
		err = tx.QueryRow(ctx, lockSource, accepted.Source.ID, accepted.Source.MSPID, accepted.Source.ClientID).Scan(&lockedID, &lockedMSP, &lockedClient, &lockedSourceVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return customfields.ErrCustomDateBindingConflict
		}
		if err != nil {
			return err
		}
		if lockedID != accepted.Source.ID || lockedMSP != accepted.Source.MSPID || lockedClient != accepted.Source.ClientID || lockedSourceVersion != accepted.Source.Version ||
			v.FieldID != accepted.Definition.ID || v.ObjectType != accepted.Definition.ObjectType || v.ObjectID != accepted.Source.ID || v.SourceRevision != accepted.Source.Version {
			return customfields.ErrCustomDateBindingConflict
		}
		if (lockedKind == customfields.DateKind) != (v.DateValue != nil) || (lockedKind == customfields.TimestampKind) != (v.TimestampValue != nil) {
			return customfields.ErrCustomDateBindingConflict
		}

		var tagRows int64
		if accepted.ExpectedVersion == 0 {
			query := `INSERT INTO object_custom_date_values(id,field_id,msp_id,client_id,object_type,object_id,source_revision,date_value,timestamp_value,timezone,version,created_at,created_by,updated_at,updated_by)
SELECT $1::uuid,$2::uuid,$3::uuid,NULLIF($4,'')::uuid,$6,$5::uuid,$7,$8,$9,NULLIF($10,''),1,$12,$13::uuid,$14,$15::uuid
WHERE $11::bigint=1 AND EXISTS(SELECT 1 FROM technicians actor WHERE actor.id=$13::uuid AND actor.msp_id=$3::uuid AND actor.lifecycle_state='active')`
			tag, e := tx.Exec(ctx, query, v.ID, v.FieldID, v.MSPID, v.ClientID, v.ObjectID, v.ObjectType, v.SourceRevision, v.DateValue, v.TimestampValue, v.Timezone, v.Version, v.CreatedAt, v.CreatedBy, v.UpdatedAt, v.UpdatedBy)
			if e != nil {
				var postgres *pgconn.PgError
				if errors.As(e, &postgres) && postgres.Code == "23505" {
					return customfields.ErrCustomDateVersionConflict
				}
				return e
			}
			tagRows = tag.RowsAffected()
		} else {
			query := `UPDATE object_custom_date_values value SET source_revision=$7,date_value=$8,timestamp_value=$9,timezone=NULLIF($10,''),version=value.version+1,updated_at=$14,updated_by=$15::uuid WHERE value.id=$1::uuid AND value.field_id=$2::uuid AND value.msp_id=$3::uuid AND value.object_type=$6 AND value.object_id=$5::uuid AND value.version=$11 AND value.client_id IS NOT DISTINCT FROM NULLIF($4,'')::uuid AND $12::timestamptz IS NOT NULL AND $13::uuid IS NOT NULL AND EXISTS(SELECT 1 FROM technicians actor WHERE actor.id=$15::uuid AND actor.msp_id=$3::uuid AND actor.lifecycle_state='active')`
			tag, e := tx.Exec(ctx, query, v.ID, v.FieldID, v.MSPID, v.ClientID, v.ObjectID, v.ObjectType, v.SourceRevision, v.DateValue, v.TimestampValue, v.Timezone, accepted.ExpectedVersion, v.CreatedAt, v.CreatedBy, v.UpdatedAt, v.UpdatedBy)
			if e != nil {
				return e
			}
			tagRows = tag.RowsAffected()
		}
		if tagRows != 1 {
			if accepted.ExpectedVersion > 0 {
				return classifyScopedVersion(ctx, tx, "object_custom_date_values", v.ID, v.MSPID, v.ClientID, accepted.ExpectedVersion, customfields.ErrCustomDateVersionConflict)
			}
			return scope.ErrNotFound
		}
		if err = writeAuditOnly(ctx, tx, accepted.Audit); err != nil {
			return err
		}
		return writeEventOnly(ctx, tx, accepted.Event)
	})
}

func validDateMutationUUIDs(accepted customfields.DateMutation) bool {
	v := accepted.Value
	return internalid.ValidCanonical(accepted.Definition.ID) && internalid.ValidCanonical(accepted.Definition.MSPID) && internalid.ValidCanonical(accepted.Source.ID) && internalid.ValidCanonical(accepted.Source.MSPID) &&
		(accepted.Source.ClientID == "" || internalid.ValidCanonical(accepted.Source.ClientID)) && internalid.ValidCanonical(v.ID) && internalid.ValidCanonical(v.FieldID) && internalid.ValidCanonical(v.MSPID) &&
		(v.ClientID == "" || internalid.ValidCanonical(v.ClientID)) && internalid.ValidCanonical(v.ObjectID) && internalid.ValidCanonical(v.CreatedBy) && internalid.ValidCanonical(v.UpdatedBy) && validCalendarFactUUIDs(accepted.RequestID, accepted.Audit, accepted.Event)
}
func (r *CustomDateRepository) withTransaction(ctx context.Context, fn func(transaction) error) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

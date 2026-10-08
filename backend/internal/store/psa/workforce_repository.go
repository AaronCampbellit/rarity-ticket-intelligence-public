package psa

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
)

type WorkforceRepository struct{ db database }

var _ workforce.ScheduleRepository = (*WorkforceRepository)(nil)
var _ workforce.PTORepository = (*WorkforceRepository)(nil)

func NewWorkforceRepository(db database) *WorkforceRepository { return &WorkforceRepository{db: db} }

func (r *WorkforceRepository) TechnicianInMSP(ctx context.Context, mspID, technicianID string) (bool, error) {
	if !internalid.ValidCanonical(mspID) || !internalid.ValidCanonical(technicianID) {
		return false, workforce.ErrInvalidSchedule
	}
	var found bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM technicians WHERE id=$1::uuid AND msp_id=$2::uuid AND lifecycle_state='active')`, technicianID, mspID).Scan(&found)
	return found, err
}
func (r *WorkforceRepository) CurrentScheduleVersion(ctx context.Context, mspID, technicianID string) (int64, error) {
	if !internalid.ValidCanonical(mspID) || !internalid.ValidCanonical(technicianID) {
		return 0, workforce.ErrInvalidSchedule
	}
	var version int64
	err := r.db.QueryRow(ctx, `SELECT COALESCE(MAX(version),0) FROM technician_schedule_versions WHERE msp_id=$1::uuid AND technician_id=$2::uuid`, mspID, technicianID).Scan(&version)
	return version, err
}
func (r *WorkforceRepository) CurrentSchedule(ctx context.Context, mspID, technicianID string) (workforce.Schedule, error) {
	if !internalid.ValidCanonical(mspID) || !internalid.ValidCanonical(technicianID) {
		return workforce.Schedule{}, workforce.ErrInvalidSchedule
	}
	var s workforce.Schedule
	err := r.db.QueryRow(ctx, `SELECT id::text,msp_id::text,technician_id::text,timezone,effective_from,effective_through,version,created_at,created_by::text FROM technician_schedule_versions WHERE msp_id=$1::uuid AND technician_id=$2::uuid ORDER BY version DESC LIMIT 1`, mspID, technicianID).Scan(&s.ID, &s.MSPID, &s.TechnicianID, &s.Timezone, &s.EffectiveFrom, &s.EffectiveThrough, &s.Version, &s.CreatedAt, &s.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return workforce.Schedule{}, nil
	}
	return s, err
}

func (r *WorkforceRepository) FindSchedule(ctx context.Context, mspID, scheduleID string) (workforce.Schedule, error) {
	if !internalid.ValidCanonical(mspID) || !internalid.ValidCanonical(scheduleID) {
		return workforce.Schedule{}, workforce.ErrInvalidSchedule
	}
	var schedule workforce.Schedule
	err := r.db.QueryRow(ctx, `SELECT id::text,msp_id::text,technician_id::text,timezone,effective_from,effective_through,version,created_at,created_by::text FROM technician_schedule_versions WHERE id=$1::uuid AND msp_id=$2::uuid AND lifecycle_state='active'`, scheduleID, mspID).Scan(&schedule.ID, &schedule.MSPID, &schedule.TechnicianID, &schedule.Timezone, &schedule.EffectiveFrom, &schedule.EffectiveThrough, &schedule.Version, &schedule.CreatedAt, &schedule.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return workforce.Schedule{}, scope.ErrNotFound
	}
	if err != nil {
		return workforce.Schedule{}, err
	}
	windowRows, err := r.db.Query(ctx, `SELECT id::text,weekday,(extract(hour FROM starts_local)*60+extract(minute FROM starts_local))::integer,(extract(hour FROM ends_local)*60+extract(minute FROM ends_local))::integer,capacity_percent FROM technician_schedule_windows WHERE schedule_version_id=$1::uuid AND msp_id=$2::uuid AND technician_id=$3::uuid ORDER BY weekday,starts_local,id`, schedule.ID, schedule.MSPID, schedule.TechnicianID)
	if err != nil {
		return workforce.Schedule{}, err
	}
	for windowRows.Next() {
		var window workforce.WeeklyWindow
		var weekday int
		if err = windowRows.Scan(&window.ID, &weekday, &window.StartsMinute, &window.EndsMinute, &window.CapacityPercent); err != nil {
			windowRows.Close()
			return workforce.Schedule{}, err
		}
		window.Weekday = time.Weekday(weekday)
		schedule.Windows = append(schedule.Windows, window)
	}
	windowRows.Close()
	if err = windowRows.Err(); err != nil {
		return workforce.Schedule{}, err
	}
	exceptionRows, err := r.db.Query(ctx, `SELECT id::text,exception_on,availability_state,all_day,COALESCE((extract(hour FROM starts_local)*60+extract(minute FROM starts_local))::integer,0),COALESCE((extract(hour FROM ends_local)*60+extract(minute FROM ends_local))::integer,0),capacity_percent,reason,version FROM technician_schedule_exceptions WHERE schedule_version_id=$1::uuid AND msp_id=$2::uuid AND technician_id=$3::uuid ORDER BY exception_on,starts_local,id`, schedule.ID, schedule.MSPID, schedule.TechnicianID)
	if err != nil {
		return workforce.Schedule{}, err
	}
	for exceptionRows.Next() {
		var exception workforce.ScheduleException
		if err = exceptionRows.Scan(&exception.ID, &exception.ExceptionOn, &exception.State, &exception.AllDay, &exception.StartsMinute, &exception.EndsMinute, &exception.CapacityPercent, &exception.Reason, &exception.Version); err != nil {
			exceptionRows.Close()
			return workforce.Schedule{}, err
		}
		schedule.Exceptions = append(schedule.Exceptions, exception)
	}
	exceptionRows.Close()
	if err = exceptionRows.Err(); err != nil {
		return workforce.Schedule{}, err
	}
	return schedule, nil
}

func (r *WorkforceRepository) PublishScheduleAtomic(ctx context.Context, accepted workforce.ScheduleMutation) error {
	if !validScheduleMutationUUIDs(accepted) {
		return workforce.ErrInvalidSchedule
	}
	return r.withTransaction(ctx, func(tx transaction) error {
		if err := claimCalendarDomainRequest(ctx, tx, accepted.RequestID, accepted.Schedule.MSPID, "", "schedule.publish", accepted.IdempotencyKey, accepted.RequestFingerprint, accepted.Schedule, accepted.Schedule.CreatedAt); err != nil {
			return err
		}
		var current int64
		var currentEffective time.Time
		err := tx.QueryRow(ctx, `SELECT version,effective_from FROM technician_schedule_versions WHERE msp_id=$1::uuid AND technician_id=$2::uuid ORDER BY version DESC LIMIT 1 FOR UPDATE`, accepted.Schedule.MSPID, accepted.Schedule.TechnicianID).Scan(&current, &currentEffective)
		if errors.Is(err, pgx.ErrNoRows) {
			current = 0
			err = nil
		}
		if err != nil {
			return err
		}
		if current != accepted.ExpectedVersion {
			return workforce.ErrWorkforceVersionConflict
		}
		if current > 0 && !accepted.Schedule.EffectiveFrom.After(currentEffective) {
			return workforce.ErrScheduleEffectiveOverlap
		}
		s := accepted.Schedule
		tag, err := tx.Exec(ctx, `INSERT INTO technician_schedule_versions(id,msp_id,technician_id,timezone,effective_from,effective_through,version,lifecycle_state,created_at,created_by)
SELECT $1::uuid,$2::uuid,technician.id,$4,$5,$6,$7,'active',$8,$9::uuid FROM technicians technician
WHERE technician.id=$3::uuid AND technician.msp_id=$2::uuid AND technician.lifecycle_state='active'
AND EXISTS(SELECT 1 FROM technicians actor WHERE actor.id=$9::uuid AND actor.msp_id=$2::uuid AND actor.lifecycle_state='active')`, s.ID, s.MSPID, s.TechnicianID, s.Timezone, s.EffectiveFrom, s.EffectiveThrough, s.Version, s.CreatedAt, s.CreatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return scope.ErrNotFound
		}
		if current > 0 {
			if _, err = tx.Exec(ctx, `UPDATE technician_schedule_versions SET lifecycle_state='superseded',effective_through=LEAST(COALESCE(effective_through,$3::date),$3::date) WHERE msp_id=$1::uuid AND technician_id=$2::uuid AND version=$4`, s.MSPID, s.TechnicianID, s.EffectiveFrom.AddDate(0, 0, -1), current); err != nil {
				return err
			}
		}
		for _, w := range s.Windows {
			if _, err = tx.Exec(ctx, `INSERT INTO technician_schedule_windows(id,schedule_version_id,msp_id,technician_id,weekday,starts_local,ends_local,capacity_percent,created_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,make_time($6/60,$6%60,0),CASE WHEN $7=1440 THEN time '24:00:00' ELSE make_time($7/60,$7%60,0) END,$8,$9)`, w.ID, s.ID, s.MSPID, s.TechnicianID, int(w.Weekday), w.StartsMinute, w.EndsMinute, w.CapacityPercent, s.CreatedAt); err != nil {
				return err
			}
		}
		for _, e := range s.Exceptions {
			if _, err = tx.Exec(ctx, `INSERT INTO technician_schedule_exceptions(id,schedule_version_id,msp_id,technician_id,exception_on,availability_state,all_day,starts_local,ends_local,capacity_percent,reason,version,created_at,created_by,updated_at,updated_by) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,CASE WHEN $7 THEN NULL ELSE make_time($8/60,$8%60,0) END,CASE WHEN $7 THEN NULL WHEN $9=1440 THEN time '24:00:00' ELSE make_time($9/60,$9%60,0) END,$10,$11,1,$12,$13::uuid,$12,$13::uuid)`, e.ID, s.ID, s.MSPID, s.TechnicianID, e.ExceptionOn, e.State, e.AllDay, e.StartsMinute, e.EndsMinute, e.CapacityPercent, e.Reason, s.CreatedAt, s.CreatedBy); err != nil {
				return err
			}
		}
		if err = writeAuditOnly(ctx, tx, accepted.Audit); err != nil {
			return err
		}
		return writeEventOnly(ctx, tx, accepted.Event)
	})
}

func (r *WorkforceRepository) AddScheduleExceptionAtomic(ctx context.Context, accepted workforce.ScheduleExceptionMutation) error {
	if !validScheduleExceptionMutationUUIDs(accepted) {
		return workforce.ErrInvalidSchedule
	}
	return r.withTransaction(ctx, func(tx transaction) error {
		if err := claimCalendarDomainRequest(ctx, tx, accepted.RequestID, accepted.Schedule.MSPID, "", "schedule.exception.add", accepted.IdempotencyKey, accepted.RequestFingerprint, accepted.Schedule, accepted.Audit.OccurredAt); err != nil {
			return err
		}
		var technicianID string
		var version int64
		var effectiveFrom time.Time
		var effectiveThrough *time.Time
		err := tx.QueryRow(ctx, `SELECT technician_id::text,version,effective_from,effective_through FROM technician_schedule_versions WHERE id=$1::uuid AND msp_id=$2::uuid AND lifecycle_state='active' FOR UPDATE`, accepted.Schedule.ID, accepted.Schedule.MSPID).Scan(&technicianID, &version, &effectiveFrom, &effectiveThrough)
		if errors.Is(err, pgx.ErrNoRows) {
			return scope.ErrNotFound
		}
		if err != nil {
			return err
		}
		if version != accepted.ExpectedVersion {
			return workforce.ErrWorkforceVersionConflict
		}
		if accepted.Schedule.Version != accepted.ExpectedVersion+1 {
			return workforce.ErrInvalidSchedule
		}
		exception := accepted.Exception
		if exception.ExceptionOn.Before(effectiveFrom) || effectiveThrough != nil && exception.ExceptionOn.After(*effectiveThrough) {
			return workforce.ErrInvalidSchedule
		}
		tag, err := tx.Exec(ctx, `UPDATE technician_schedule_versions SET version=version+1 WHERE id=$1::uuid AND msp_id=$2::uuid AND version=$3`, accepted.Schedule.ID, accepted.Schedule.MSPID, accepted.ExpectedVersion)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return workforce.ErrWorkforceVersionConflict
		}
		tag, err = tx.Exec(ctx, `INSERT INTO technician_schedule_exceptions(id,schedule_version_id,msp_id,technician_id,exception_on,availability_state,all_day,starts_local,ends_local,capacity_percent,reason,version,created_at,created_by,updated_at,updated_by)
SELECT $1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,CASE WHEN $7 THEN NULL ELSE make_time($8/60,$8%60,0) END,CASE WHEN $7 THEN NULL WHEN $9=1440 THEN time '24:00:00' ELSE make_time($9/60,$9%60,0) END,$10,$11,1,$12,$13::uuid,$12,$13::uuid
WHERE EXISTS(SELECT 1 FROM technicians actor WHERE actor.id=$13::uuid AND actor.msp_id=$3::uuid AND actor.lifecycle_state='active')
AND NOT EXISTS(SELECT 1 FROM technician_schedule_exceptions existing WHERE existing.schedule_version_id=$2::uuid AND existing.msp_id=$3::uuid AND existing.technician_id=$4::uuid AND existing.exception_on=$5 AND (existing.all_day OR $7 OR existing.starts_local < CASE WHEN $9=1440 THEN time '24:00:00' ELSE make_time($9/60,$9%60,0) END AND existing.ends_local > make_time($8/60,$8%60,0)))`, exception.ID, accepted.Schedule.ID, accepted.Schedule.MSPID, technicianID, exception.ExceptionOn, exception.State, exception.AllDay, exception.StartsMinute, exception.EndsMinute, exception.CapacityPercent, exception.Reason, accepted.Audit.OccurredAt, accepted.Audit.ActorID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return workforce.ErrScheduleOverlap
		}
		if err = writeAuditOnly(ctx, tx, accepted.Audit); err != nil {
			return err
		}
		return writeEventOnly(ctx, tx, accepted.Event)
	})
}

func (r *WorkforceRepository) ActiveManager(ctx context.Context, mspID, technicianID string) (string, error) {
	if !internalid.ValidCanonical(mspID) || !internalid.ValidCanonical(technicianID) {
		return "", workforce.ErrInvalidPTO
	}
	var manager string
	err := r.db.QueryRow(ctx, `SELECT team.workforce_manager_id::text FROM team_memberships member JOIN teams team ON team.id=member.team_id AND team.msp_id=member.msp_id JOIN team_memberships manager ON manager.team_id=team.id AND manager.msp_id=team.msp_id AND manager.technician_id=team.workforce_manager_id AND manager.lifecycle_state='active' JOIN technicians manager_technician ON manager_technician.id=manager.technician_id AND manager_technician.msp_id=manager.msp_id AND manager_technician.lifecycle_state='active' WHERE member.technician_id=$1::uuid AND member.msp_id=$2::uuid AND member.lifecycle_state='active' AND team.workforce_manager_id IS NOT NULL ORDER BY member.team_id LIMIT 1`, technicianID, mspID).Scan(&manager)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return manager, err
}
func (r *WorkforceRepository) FindPTO(ctx context.Context, mspID, id string) (workforce.PTORequest, error) {
	if !internalid.ValidCanonical(mspID) || !internalid.ValidCanonical(id) {
		return workforce.PTORequest{}, workforce.ErrInvalidPTO
	}
	var p workforce.PTORequest
	err := r.db.QueryRow(ctx, `SELECT id::text,msp_id::text,technician_id::text,pto_type,COALESCE(manager_id::text,''),COALESCE(decided_by::text,''),decision_reason,created_by::text,updated_by::text,COALESCE(timezone,''),starts_on,ends_on,starts_at,ends_at,all_day,state,version,created_at,updated_at FROM pto_requests WHERE id=$1::uuid AND msp_id=$2::uuid`, id, mspID).Scan(&p.ID, &p.MSPID, &p.TechnicianID, &p.PTOType, &p.ManagerID, &p.DecidedBy, &p.DecisionReason, &p.CreatedBy, &p.UpdatedBy, &p.Timezone, &p.StartsOn, &p.EndsOn, &p.StartsAt, &p.EndsAt, &p.AllDay, &p.State, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return workforce.PTORequest{}, scope.ErrNotFound
	}
	return p, err
}

func (r *WorkforceRepository) CreatePTOAtomic(ctx context.Context, accepted workforce.PTOMutation) error {
	if !validPTOMutationUUIDs(accepted) || !validRepositoryPTOType(accepted.Request.PTOType) {
		return workforce.ErrInvalidPTO
	}
	return r.withTransaction(ctx, func(tx transaction) error {
		p := accepted.Request
		if err := claimCalendarDomainRequest(ctx, tx, accepted.RequestID, p.MSPID, "", "pto.request", accepted.IdempotencyKey, accepted.RequestFingerprint, p, p.CreatedAt); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO pto_requests(id,msp_id,technician_id,starts_on,ends_on,starts_at,ends_at,timezone,all_day,pto_type,state,manager_id,version,created_at,created_by,updated_at,updated_by)
SELECT $1::uuid,$2::uuid,technician.id,$4,$5,$6,$7,NULLIF($8,''),$9,$10,$11,NULLIF($12,'')::uuid,1,$13,$14::uuid,$15,$16::uuid FROM technicians technician
WHERE technician.id=$3::uuid AND technician.msp_id=$2::uuid AND technician.lifecycle_state='active'
AND (NULLIF($12,'')::uuid IS NULL OR EXISTS(SELECT 1 FROM technicians manager WHERE manager.id=$12::uuid AND manager.msp_id=$2::uuid AND manager.lifecycle_state='active')) AND $14::uuid=technician.id AND $16::uuid=technician.id`, p.ID, p.MSPID, p.TechnicianID, p.StartsOn, p.EndsOn, p.StartsAt, p.EndsAt, p.Timezone, p.AllDay, p.PTOType, p.State, p.ManagerID, p.CreatedAt, p.CreatedBy, p.UpdatedAt, p.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return scope.ErrNotFound
		}
		if err = writeAuditOnly(ctx, tx, accepted.Audit); err != nil {
			return err
		}
		return writeEventOnly(ctx, tx, accepted.Event)
	})
}

func (r *WorkforceRepository) UpdatePTOAtomic(ctx context.Context, accepted workforce.PTOMutation, expected int64) error {
	if !validPTOMutationUUIDs(accepted) || !validRepositoryPTOType(accepted.Request.PTOType) {
		return workforce.ErrInvalidPTO
	}
	return r.withTransaction(ctx, func(tx transaction) error {
		p := accepted.Request
		if err := claimCalendarDomainRequest(ctx, tx, accepted.RequestID, p.MSPID, "", "pto."+accepted.Authority, accepted.IdempotencyKey, accepted.RequestFingerprint, p, p.UpdatedAt); err != nil {
			return err
		}
		var technicianID string
		var state workforce.PTOState
		var currentVersion int64
		if err := tx.QueryRow(ctx, `SELECT technician_id::text,state,version FROM pto_requests WHERE id=$1::uuid AND msp_id=$2::uuid FOR UPDATE`, p.ID, p.MSPID).Scan(&technicianID, &state, &currentVersion); errors.Is(err, pgx.ErrNoRows) {
			return scope.ErrNotFound
		} else if err != nil {
			return err
		}
		if currentVersion != expected {
			return workforce.ErrWorkforceVersionConflict
		}
		if accepted.Authority == "requester" {
			if p.UpdatedBy != technicianID {
				return authorization.ErrForbidden
			}
		} else if accepted.Authority == "decision" {
			authorized, err := lockAndAuthorizePTODecision(ctx, tx, p.MSPID, technicianID, p.UpdatedBy, p.UpdatedAt)
			if err != nil {
				return err
			}
			if !authorized {
				return authorization.ErrForbidden
			}
		} else {
			return authorization.ErrForbidden
		}
		tag, err := tx.Exec(ctx, `UPDATE pto_requests SET state=$4,decided_by=NULLIF($5,'')::uuid,decided_at=CASE WHEN NULLIF($5,'') IS NULL THEN NULL ELSE $6::timestamptz END,decision_reason=$7,version=version+1,updated_at=$6::timestamptz,updated_by=$8::uuid WHERE id=$1::uuid AND msp_id=$2::uuid AND version=$3 AND EXISTS(SELECT 1 FROM technicians actor WHERE actor.id=$8::uuid AND actor.msp_id=$2::uuid AND actor.lifecycle_state='active')`, p.ID, p.MSPID, expected, p.State, p.DecidedBy, p.UpdatedAt, p.DecisionReason, p.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return classifyMSPVersion(ctx, tx, "pto_requests", p.ID, p.MSPID, expected, workforce.ErrWorkforceVersionConflict)
		}
		if err = writeAuditOnly(ctx, tx, accepted.Audit); err != nil {
			return err
		}
		if err = writeEventOnly(ctx, tx, accepted.Event); err != nil {
			return err
		}
		if accepted.Authority != "decision" || (p.State != workforce.Approved && p.State != workforce.Rejected) {
			return nil
		}
		seed := strings.Join([]string{p.MSPID, "pto", p.ID, fmt.Sprint(p.Version), p.TechnicianID, "pto"}, "\x00")
		payload := canonicalCalendarEventData{
			RecipientID: p.TechnicianID,
			ChangeClass: "pto",
			Urgency:     "routine",
			SourceRefs: []canonicalCalendarSourceRef{{
				Type: "pto", ID: p.ID, EventRole: "unavailability", SourceRevision: p.Version,
			}},
			ActionPath: "/calendar",
		}
		return writeCanonicalCalendarEvent(ctx, tx, schedulingFactID(seed, "calendar.schedule_changed"), accepted.Event.OccurredAt, p.MSPID, "", accepted.Event.ActorType, accepted.Event.ActorID, "pto_request", p.ID, p.Version, accepted.Event.CorrelationID, payload)
	})
}

func lockAndAuthorizePTODecision(ctx context.Context, tx transaction, mspID, requesterID, actorID string, at time.Time) (bool, error) {
	var actorActive bool
	if err := tx.QueryRow(ctx, `SELECT lifecycle_state='active' FROM technicians WHERE id=$1::uuid AND msp_id=$2::uuid FOR SHARE`, actorID, mspID).Scan(&actorActive); errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if !actorActive {
		return false, nil
	}
	rows, err := tx.Query(ctx, `SELECT team.id::text FROM team_memberships member JOIN teams team ON team.id=member.team_id AND team.msp_id=member.msp_id JOIN team_memberships manager ON manager.team_id=team.id AND manager.msp_id=team.msp_id AND manager.technician_id=team.workforce_manager_id WHERE member.msp_id=$1::uuid AND member.technician_id=$2::uuid ORDER BY team.id FOR SHARE OF member,team,manager`, mspID, requesterID)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var ignored string
		if err = rows.Scan(&ignored); err != nil {
			rows.Close()
			return false, err
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return false, err
	}
	rows, err = tx.Query(ctx, `SELECT assignment.id::text FROM role_assignments assignment JOIN role_capabilities capability ON capability.role_id=assignment.role_id AND capability.msp_id=assignment.msp_id WHERE assignment.msp_id=$1::uuid AND assignment.technician_id=$2::uuid AND assignment.client_id IS NULL ORDER BY assignment.id FOR SHARE OF assignment,capability`, mspID, actorID)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var ignored string
		if err = rows.Scan(&ignored); err != nil {
			rows.Close()
			return false, err
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return false, err
	}
	var ok bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM technicians actor WHERE actor.id=$2::uuid AND actor.msp_id=$1::uuid AND actor.lifecycle_state='active' AND (EXISTS(SELECT 1 FROM team_memberships member JOIN teams team ON team.id=member.team_id AND team.msp_id=member.msp_id JOIN team_memberships manager ON manager.team_id=team.id AND manager.msp_id=team.msp_id AND manager.technician_id=team.workforce_manager_id AND manager.lifecycle_state='active' WHERE member.msp_id=$1::uuid AND member.technician_id=$3::uuid AND member.lifecycle_state='active' AND team.workforce_manager_id=$2::uuid) OR EXISTS(SELECT 1 FROM role_assignments assignment JOIN role_capabilities capability ON capability.role_id=assignment.role_id AND capability.msp_id=assignment.msp_id AND capability.capability='calendar.workforce.manage' WHERE assignment.msp_id=$1::uuid AND assignment.technician_id=$2::uuid AND assignment.client_id IS NULL AND (assignment.expires_at IS NULL OR assignment.expires_at>$4))))`, mspID, actorID, requesterID, at).Scan(&ok)
	return ok, err
}

func (r *WorkforceRepository) withTransaction(ctx context.Context, fn func(transaction) error) error {
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

func nullableCalendarTime(v *time.Time) any {
	if v == nil {
		return nil
	}
	return *v
}

func validScheduleMutationUUIDs(accepted workforce.ScheduleMutation) bool {
	s := accepted.Schedule
	if !internalid.ValidCanonical(s.ID) || !internalid.ValidCanonical(s.MSPID) || !internalid.ValidCanonical(s.TechnicianID) || !internalid.ValidCanonical(s.CreatedBy) || !validCalendarFactUUIDs(accepted.RequestID, accepted.Audit, accepted.Event) {
		return false
	}
	for _, window := range s.Windows {
		if !internalid.ValidCanonical(window.ID) {
			return false
		}
	}
	for _, exception := range s.Exceptions {
		if !internalid.ValidCanonical(exception.ID) {
			return false
		}
	}
	return true
}
func validScheduleExceptionMutationUUIDs(accepted workforce.ScheduleExceptionMutation) bool {
	return internalid.ValidCanonical(accepted.Schedule.ID) && internalid.ValidCanonical(accepted.Schedule.MSPID) && internalid.ValidCanonical(accepted.Schedule.TechnicianID) && internalid.ValidCanonical(accepted.Exception.ID) && validCalendarFactUUIDs(accepted.RequestID, accepted.Audit, accepted.Event)
}
func validPTOMutationUUIDs(accepted workforce.PTOMutation) bool {
	p := accepted.Request
	return internalid.ValidCanonical(p.ID) && internalid.ValidCanonical(p.MSPID) && internalid.ValidCanonical(p.TechnicianID) &&
		(p.ManagerID == "" || internalid.ValidCanonical(p.ManagerID)) && (p.DecidedBy == "" || internalid.ValidCanonical(p.DecidedBy)) && internalid.ValidCanonical(p.CreatedBy) && internalid.ValidCanonical(p.UpdatedBy) && validCalendarFactUUIDs(accepted.RequestID, accepted.Audit, accepted.Event)
}
func validRepositoryPTOType(value string) bool {
	switch value {
	case "vacation", "sick", "personal", "training", "other":
		return true
	}
	return false
}

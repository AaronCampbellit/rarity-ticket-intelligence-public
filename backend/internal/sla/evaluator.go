package sla

import (
	"context"
	"errors"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
)

const evaluatorSystemActorID = "00000000-0000-0000-0000-000000000000"

type Timer struct {
	ID                  string
	MSPID               string
	ClientID            string
	WorkRecordID        string
	ResponseWarningAt   time.Time
	ResponseDueAt       time.Time
	ResolutionWarningAt time.Time
	ResolutionDueAt     time.Time
	RespondedAt         *time.Time
	ResolvedAt          *time.Time
	PausedAt            *time.Time
	ResponseState       State
	ResolutionState     State
	Version             int64
}

type EvaluationMutation struct {
	Timer  Timer
	Audits []mutation.AuditRecord
	Events []mutation.EventRecord
}

type EvaluatorRepository interface {
	ListDue(context.Context, time.Time, int) ([]Timer, error)
	UpdateEvaluation(context.Context, EvaluationMutation) error
}

type EvaluationResult struct {
	Evaluated int
	Updated   int
	Conflicts int
}

type Evaluator struct {
	repository EvaluatorRepository
	now        func() time.Time
	newID      func() string
}

func NewEvaluator(
	repository EvaluatorRepository,
	now func() time.Time,
	newID func() string,
) *Evaluator {
	return &Evaluator{repository: repository, now: now, newID: newID}
}

func (e *Evaluator) RunOnce(
	ctx context.Context,
	limit int,
) (EvaluationResult, error) {
	if limit < 1 {
		return EvaluationResult{}, ErrInvalidPolicy
	}
	now := e.now().UTC()
	timers, err := e.repository.ListDue(ctx, now, limit)
	if err != nil {
		return EvaluationResult{}, err
	}
	result := EvaluationResult{Evaluated: len(timers)}
	for _, timer := range timers {
		actions := evaluateTimer(&timer, now)
		if len(actions) == 0 {
			continue
		}
		timer.Version++
		correlationID := e.newID()
		accepted := EvaluationMutation{Timer: timer}
		for _, action := range actions {
			accepted.Audits = append(accepted.Audits, mutation.AuditRecord{
				ID: e.newID(), OccurredAt: now, MSPID: timer.MSPID,
				ClientID: timer.ClientID, ActorType: "system",
				ActorID: evaluatorSystemActorID, Action: action,
				SubjectType: "work_record_sla", SubjectID: timer.ID,
				SubjectVersion: timer.Version, Source: "worker",
				CorrelationID: correlationID,
			})
			accepted.Events = append(accepted.Events, mutation.EventRecord{
				EventID: e.newID(), EventType: action, SchemaVersion: 1,
				OccurredAt: now, MSPID: timer.MSPID, ClientID: timer.ClientID,
				ActorType: "system", ActorID: evaluatorSystemActorID,
				SubjectType: "work_record_sla", SubjectID: timer.ID,
				SubjectVersion: timer.Version, Source: "worker",
				CorrelationID: correlationID,
			})
		}
		if err := e.repository.UpdateEvaluation(ctx, accepted); err != nil {
			if errors.Is(err, object.ErrVersionConflict) {
				result.Conflicts++
				continue
			}
			return result, err
		}
		result.Updated++
	}
	return result, nil
}

func evaluateTimer(timer *Timer, now time.Time) []string {
	if timer.PausedAt != nil {
		return nil
	}
	actions := make([]string, 0, 2)
	response := Evaluate(Target{
		DueAt: timer.ResponseDueAt, WarningAt: timer.ResponseWarningAt,
		MetAt: timer.RespondedAt,
	}, now)
	if response != timer.ResponseState &&
		timer.ResponseState != Met && timer.ResponseState != Breached &&
		timer.ResponseState != Cancelled {
		timer.ResponseState = response
		if response == Warning {
			actions = append(actions, "sla.response.warning")
		} else if response == Breached {
			actions = append(actions, "sla.response.breached")
		}
	}
	resolution := Evaluate(Target{
		DueAt: timer.ResolutionDueAt, WarningAt: timer.ResolutionWarningAt,
		MetAt: timer.ResolvedAt,
	}, now)
	if resolution != timer.ResolutionState &&
		timer.ResolutionState != Met && timer.ResolutionState != Breached &&
		timer.ResolutionState != Cancelled {
		timer.ResolutionState = resolution
		if resolution == Warning {
			actions = append(actions, "sla.resolution.warning")
		} else if resolution == Breached {
			actions = append(actions, "sla.resolution.breached")
		}
	}
	return actions
}

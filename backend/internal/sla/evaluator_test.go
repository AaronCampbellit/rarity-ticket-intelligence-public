package sla

import (
	"context"
	"testing"
	"time"
)

type evaluatorRepository struct {
	timers    []Timer
	mutations []EvaluationMutation
}

func (r *evaluatorRepository) ListDue(
	context.Context,
	time.Time,
	int,
) ([]Timer, error) {
	return r.timers, nil
}

func (r *evaluatorRepository) UpdateEvaluation(
	_ context.Context,
	mutation EvaluationMutation,
) error {
	r.mutations = append(r.mutations, mutation)
	return nil
}

func TestEvaluatorEmitsWarningAndBreachFactsOnce(t *testing.T) {
	now := time.Date(2026, time.July, 30, 16, 0, 0, 0, time.UTC)
	repository := &evaluatorRepository{timers: []Timer{
		{
			ID: "warning", MSPID: "msp", ClientID: "client", WorkRecordID: "work-1",
			ResponseWarningAt: now.Add(-time.Minute), ResponseDueAt: now.Add(time.Hour),
			ResolutionWarningAt: now.Add(time.Hour), ResolutionDueAt: now.Add(2 * time.Hour),
			ResponseState: Running, ResolutionState: Running, Version: 1,
		},
		{
			ID: "breach", MSPID: "msp", ClientID: "client", WorkRecordID: "work-2",
			ResponseWarningAt: now.Add(-time.Hour), ResponseDueAt: now.Add(-time.Minute),
			ResolutionWarningAt: now.Add(-time.Hour), ResolutionDueAt: now.Add(-time.Minute),
			ResponseState: Warning, ResolutionState: Warning, Version: 4,
		},
	}}
	evaluator := NewEvaluator(repository, func() time.Time { return now }, sequenceEvaluatorIDs())
	result, err := evaluator.RunOnce(context.Background(), 100)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Evaluated != 2 || result.Updated != 2 || len(repository.mutations) != 2 {
		t.Fatalf("unexpected result=%+v mutations=%+v", result, repository.mutations)
	}
	if repository.mutations[0].Timer.ResponseState != Warning ||
		len(repository.mutations[0].Audits) != 1 ||
		repository.mutations[0].Audits[0].Action != "sla.response.warning" ||
		repository.mutations[0].Audits[0].ActorType != "system" ||
		repository.mutations[0].Audits[0].ActorID != "00000000-0000-0000-0000-000000000000" ||
		repository.mutations[0].Events[0].ActorID != "00000000-0000-0000-0000-000000000000" {
		t.Fatalf("warning mutation = %+v", repository.mutations[0])
	}
	if repository.mutations[1].Timer.ResponseState != Breached ||
		repository.mutations[1].Timer.ResolutionState != Breached ||
		len(repository.mutations[1].Events) != 2 {
		t.Fatalf("breach mutation = %+v", repository.mutations[1])
	}
}

func sequenceEvaluatorIDs() func() string {
	counter := 0
	return func() string {
		counter++
		return string(rune('a' + counter))
	}
}

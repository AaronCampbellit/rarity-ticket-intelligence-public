package calendar

import (
	"context"
	"testing"
	"time"
)

type reminderRepositoryStub struct {
	due     []ReminderCandidate
	claimed map[string]bool
}

func (s *reminderRepositoryStub) DueReminderCandidates(context.Context, string, time.Time, int) ([]ReminderCandidate, error) {
	return s.due, nil
}

func (s *reminderRepositoryStub) ClaimReminder(_ context.Context, key string, _ ReminderFact) (bool, error) {
	if s.claimed == nil {
		s.claimed = map[string]bool{}
	}
	if s.claimed[key] {
		return false, nil
	}
	s.claimed[key] = true
	return true, nil
}

func TestReminderEvaluatorEmitsEachThresholdOnce(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	repository := &reminderRepositoryStub{due: []ReminderCandidate{{
		MSPID: "msp", ProjectionID: "event-1", OccurrenceID: "occurrence-1", SourceRevision: 2,
		Threshold: "24h", DueAt: now.Add(24 * time.Hour), Timezone: "America/New_York",
		RecipientID: "tech-1", Active: true, Accessible: true,
	}}}
	service := NewReminderService(repository, func() time.Time { return now })
	first, err := service.EvaluateDue(context.Background(), "msp", 100)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.EvaluateDue(context.Background(), "msp", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 0 {
		t.Fatalf("first=%d second=%d, want 1 then 0", len(first), len(second))
	}
}

func TestReminderEvaluatorSkipsTerminalAndInaccessibleCandidates(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	repository := &reminderRepositoryStub{due: []ReminderCandidate{
		{MSPID: "msp", ProjectionID: "terminal", OccurrenceID: "one", SourceRevision: 1, Threshold: "24h", DueAt: now, Timezone: "UTC", RecipientID: "tech", Accessible: true},
		{MSPID: "msp", ProjectionID: "hidden", OccurrenceID: "two", SourceRevision: 1, Threshold: "24h", DueAt: now, Timezone: "UTC", RecipientID: "tech", Active: true},
	}}
	found, err := NewReminderService(repository, func() time.Time { return now }).EvaluateDue(context.Background(), "msp", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 || len(repository.claimed) != 0 {
		t.Fatalf("facts=%+v claims=%+v", found, repository.claimed)
	}
}

func TestReminderEvaluatorEmitsEligibleCanonicalFactWithoutPreferenceInput(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	repository := &reminderRepositoryStub{due: []ReminderCandidate{{
		MSPID: "msp", ProjectionID: "preference-disabled", OccurrenceID: "one",
		SourceRevision: 1, Threshold: "24h", DueAt: now, Timezone: "UTC",
		RecipientID: "tech", Active: true, Accessible: true,
	}}}
	found, err := NewReminderService(repository, func() time.Time { return now }).EvaluateDue(context.Background(), "msp", 100)
	if err != nil || len(found) != 1 || len(repository.claimed) != 1 {
		t.Fatalf("facts=%+v claims=%+v error=%v", found, repository.claimed, err)
	}
}

package calendar

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalidReminderEvaluation = errors.New("invalid calendar reminder evaluation")

const maximumReminderBatch = 1000

type ReminderCandidate struct {
	MSPID          string
	ProjectionID   string
	OccurrenceID   string
	SourceRevision int64
	Threshold      string
	DueAt          time.Time
	Timezone       string
	RecipientID    string
	Active         bool
	Accessible     bool
}

type ReminderFact struct {
	MSPID          string    `json:"msp_id"`
	ProjectionID   string    `json:"projection_id"`
	OccurrenceID   string    `json:"occurrence_id"`
	SourceRevision int64     `json:"source_revision"`
	Threshold      string    `json:"threshold"`
	DueAt          time.Time `json:"due_at"`
	Timezone       string    `json:"timezone"`
	RecipientID    string    `json:"recipient_id"`
	EvaluatedAt    time.Time `json:"evaluated_at"`
}

type ReminderRepository interface {
	DueReminderCandidates(context.Context, string, time.Time, int) ([]ReminderCandidate, error)
	// ClaimReminder atomically records the deduplication key and emits the
	// corresponding outbox fact. False means another evaluator already won.
	ClaimReminder(context.Context, string, ReminderFact) (bool, error)
}

type ReminderService struct {
	repository ReminderRepository
	now        func() time.Time
}

func NewReminderService(repository ReminderRepository, now func() time.Time) *ReminderService {
	return &ReminderService{repository: repository, now: now}
}

func (s *ReminderService) EvaluateDue(ctx context.Context, mspID string, limit int) ([]ReminderFact, error) {
	if s == nil || s.repository == nil || s.now == nil || strings.TrimSpace(mspID) == "" || limit < 1 || limit > maximumReminderBatch {
		return nil, ErrInvalidReminderEvaluation
	}
	now := s.now().UTC()
	candidates, err := s.repository.DueReminderCandidates(ctx, mspID, now, limit)
	if err != nil {
		return nil, err
	}
	if len(candidates) > limit {
		return nil, ErrInvalidReminderEvaluation
	}
	facts := make([]ReminderFact, 0, len(candidates))
	for _, candidate := range candidates {
		if !candidate.Active || !candidate.Accessible {
			continue
		}
		if candidate.MSPID != mspID || strings.TrimSpace(candidate.ProjectionID) == "" || strings.TrimSpace(candidate.OccurrenceID) == "" || candidate.SourceRevision < 1 || strings.TrimSpace(candidate.Threshold) == "" || candidate.DueAt.IsZero() || strings.TrimSpace(candidate.RecipientID) == "" {
			return nil, ErrInvalidReminderEvaluation
		}
		if _, err := time.LoadLocation(candidate.Timezone); err != nil {
			return nil, ErrInvalidReminderEvaluation
		}
		fact := ReminderFact{MSPID: mspID, ProjectionID: candidate.ProjectionID, OccurrenceID: candidate.OccurrenceID, SourceRevision: candidate.SourceRevision, Threshold: candidate.Threshold, DueAt: candidate.DueAt, Timezone: candidate.Timezone, RecipientID: candidate.RecipientID, EvaluatedAt: now}
		key := fmt.Sprintf("%s:%s:%s:%d", candidate.ProjectionID, candidate.OccurrenceID, candidate.Threshold, candidate.SourceRevision)
		owned, err := s.repository.ClaimReminder(ctx, key, fact)
		if err != nil {
			return facts, err
		}
		if owned {
			facts = append(facts, fact)
		}
	}
	return facts, nil
}

package aiassist

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidDecision = errors.New("invalid AI recommendation decision")

type HumanDecision string

const (
	DecisionAccepted HumanDecision = "accepted"
	DecisionRejected HumanDecision = "rejected"
)

const (
	RecommendationAccepted RecommendationState = "accepted"
	RecommendationRejected RecommendationState = "rejected"
)

type DecisionCommand struct {
	Principal authorization.Principal
	ID        string
	Decision  HumanDecision
	Reason    string
}

type DecisionMutation struct {
	Recommendation Recommendation
	DecisionID     string
	Decision       HumanDecision
	Reason         string
	DecidedAt      time.Time
	DecidedBy      string
	Applied        bool
	Sent           bool
	Audit          mutation.AuditRecord
	Event          mutation.EventRecord
}

// RecommendationView is the deliberately narrow technician review projection.
// It never includes provider configuration, prompt material, or context values.
type RecommendationView struct {
	ID, JobID, ClientID, WorkRecordID string
	Feature                           Feature
	Text                              string
	CandidateIDs, RelevantInputs      []string
	Confidence                        *float64
	State                             RecommendationState
	GeneratedAt                       time.Time
	Version                           int64
}

type RecommendationReadStore interface {
	GetForReview(context.Context, scope.Target, string) (RecommendationView, error)
}

type RecommendationReviewService struct {
	store RecommendationReadStore
}

func NewRecommendationReviewService(store RecommendationReadStore) *RecommendationReviewService {
	return &RecommendationReviewService{store: store}
}

func (s *RecommendationReviewService) Get(
	ctx context.Context,
	principal authorization.Principal,
	id string,
) (RecommendationView, error) {
	if s == nil || s.store == nil || strings.TrimSpace(id) == "" {
		return RecommendationView{}, scope.ErrNotFound
	}
	target := scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID}
	if err := authorization.Authorize(principal, "ai.assist", target); err != nil {
		return RecommendationView{}, err
	}
	return s.store.GetForReview(ctx, target, id)
}

type DecisionStore interface {
	Get(context.Context, scope.Target, string) (Recommendation, error)
	Decide(context.Context, DecisionMutation) error
}

type DecisionService struct {
	store DecisionStore
	now   func() time.Time
	newID func() string
}

func NewDecisionService(
	store DecisionStore,
	now func() time.Time,
	newID func() string,
) *DecisionService {
	return &DecisionService{store: store, now: now, newID: newID}
}

func (s *DecisionService) Decide(
	ctx context.Context,
	command DecisionCommand,
) (Recommendation, error) {
	if s.store == nil || s.now == nil || s.newID == nil ||
		strings.TrimSpace(command.ID) == "" || strings.TrimSpace(command.Reason) == "" {
		return Recommendation{}, ErrInvalidDecision
	}
	target := scope.Target{
		MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
	}
	recommendation, err := s.store.Get(ctx, target, command.ID)
	if err != nil {
		return Recommendation{}, err
	}
	if err := authorization.Authorize(
		command.Principal,
		"ai.assist",
		scope.Target{MSPID: recommendation.MSPID, ClientID: recommendation.ClientID},
	); err != nil {
		return Recommendation{}, err
	}
	if recommendation.State != RecommendationPendingHuman {
		return Recommendation{}, ErrInvalidDecision
	}
	switch command.Decision {
	case DecisionAccepted:
		recommendation.State = RecommendationAccepted
	case DecisionRejected:
		recommendation.State = RecommendationRejected
	default:
		return Recommendation{}, ErrInvalidDecision
	}
	now := s.now().UTC()
	decisionID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	action := "ai.recommendation." + string(command.Decision)
	accepted := DecisionMutation{
		Recommendation: recommendation, DecisionID: decisionID,
		Decision: command.Decision, Reason: command.Reason,
		DecidedAt: now, DecidedBy: command.Principal.ID,
		Applied: false, Sent: false,
		Audit: mutation.AuditRecord{
			ID: decisionID, OccurredAt: now, MSPID: recommendation.MSPID,
			ClientID: recommendation.ClientID, ActorType: "technician",
			ActorID: command.Principal.ID, Action: action,
			SubjectType: "ai_recommendation", SubjectID: recommendation.ID,
			SubjectVersion: 2, Source: "api", Reason: command.Reason,
			CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: action, SchemaVersion: 1,
			OccurredAt: now, MSPID: recommendation.MSPID,
			ClientID: recommendation.ClientID, ActorType: "technician",
			ActorID: command.Principal.ID, SubjectType: "ai_recommendation",
			SubjectID: recommendation.ID, SubjectVersion: 2,
			Source: "api", CorrelationID: correlationID,
		},
	}
	if err := s.store.Decide(ctx, accepted); err != nil {
		return Recommendation{}, err
	}
	return recommendation, nil
}

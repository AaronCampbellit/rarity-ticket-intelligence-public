package tagging

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidClassificationPreflight = errors.New("invalid classification preflight")
	ErrClassificationPreflightFailed  = errors.New("classification preflight failed")
)

var SupportedObjectTypes = []ObjectType{
	ObjectWorkRecord,
	ObjectTask,
	ObjectProject,
	ObjectAsset,
	ObjectKnowledgeArticle,
	ObjectTimeEntry,
}

type ClassificationObjectTotals struct {
	Total        int64 `json:"total"`
	Meaningful   int64 `json:"meaningful"`
	Unclassified int64 `json:"unclassified"`
	Invalid      int64 `json:"invalid"`
}

type ClassificationProjectionState struct {
	AsOf          time.Time `json:"as_of,omitempty"`
	LagSeconds    int64     `json:"lag_seconds"`
	PendingEvents int64     `json:"pending_events"`
}

type ClassificationPreflightPolicy struct {
	AutomaticApplyEnabled   bool    `json:"automatic_apply_enabled"`
	AutomaticApplyThreshold float64 `json:"automatic_apply_threshold"`
	ModelProfileID          string  `json:"model_profile_id,omitempty"`
	Version                 int64   `json:"version"`
}

type ClassificationPreflightSnapshot struct {
	MSPID                          string
	MSPDisplayID                   string
	MigrationRunID                 string
	ByObjectType                   map[ObjectType]ClassificationObjectTotals
	UnresolvedRetiredReferences    int64
	Projection                     ClassificationProjectionState
	AIPolicy                       ClassificationPreflightPolicy
	DatabaseCategoryColumnsPresent bool
	TaskOneBackfillVerified        bool
}

type ClassificationPreflightReport struct {
	MSPID                          string                                    `json:"msp_id"`
	MSPDisplayID                   string                                    `json:"msp_display_id"`
	Passed                         bool                                      `json:"passed"`
	ByObjectType                   map[ObjectType]ClassificationObjectTotals `json:"by_object_type"`
	Totals                         ClassificationObjectTotals                `json:"totals"`
	UnresolvedRetiredReferences    int64                                     `json:"unresolved_merged_or_archived_references"`
	Projection                     ClassificationProjectionState             `json:"projection"`
	AIPolicy                       ClassificationPreflightPolicy             `json:"ai_policy"`
	DatabaseCategoryColumnsPresent bool                                      `json:"database_category_columns_present"`
	TaskOneBackfillVerified        bool                                      `json:"task_one_unclassified_backfill_verified"`
	VerifiedNoOpAt                 time.Time                                 `json:"verified_no_op_at,omitempty"`
	Failures                       []string                                  `json:"failures"`
}

type MigrationRepository interface {
	BeginClassificationPreflight(context.Context, string) (ClassificationPreflightSession, error)
}

type ClassificationPreflightSession interface {
	Snapshot() ClassificationPreflightSnapshot
	RecordVerifiedNoOp(context.Context, ClassificationPreflightReport) error
	Commit(context.Context) error
	Rollback(context.Context) error
}

type MigrationService struct {
	repository MigrationRepository
	now        func() time.Time
}

func NewMigrationService(repository MigrationRepository, now func() time.Time) *MigrationService {
	return &MigrationService{repository: repository, now: now}
}

func (s *MigrationService) ClassificationPreflight(ctx context.Context, mspDisplayID string) (ClassificationPreflightReport, error) {
	if s == nil || s.repository == nil || s.now == nil || strings.TrimSpace(mspDisplayID) == "" {
		return ClassificationPreflightReport{}, ErrInvalidClassificationPreflight
	}
	session, err := s.repository.BeginClassificationPreflight(ctx, strings.TrimSpace(mspDisplayID))
	if err != nil {
		return ClassificationPreflightReport{}, err
	}
	defer session.Rollback(ctx) // no-op after a successful commit
	snapshot := session.Snapshot()
	if strings.TrimSpace(snapshot.MSPID) == "" || strings.TrimSpace(snapshot.MSPDisplayID) == "" ||
		snapshot.MSPDisplayID != strings.TrimSpace(mspDisplayID) || len(snapshot.ByObjectType) != len(SupportedObjectTypes) ||
		(snapshot.TaskOneBackfillVerified && strings.TrimSpace(snapshot.MigrationRunID) == "") {
		return ClassificationPreflightReport{}, ErrInvalidClassificationPreflight
	}
	report := ClassificationPreflightReport{
		MSPID: snapshot.MSPID, MSPDisplayID: snapshot.MSPDisplayID,
		ByObjectType:                make(map[ObjectType]ClassificationObjectTotals, len(SupportedObjectTypes)),
		UnresolvedRetiredReferences: snapshot.UnresolvedRetiredReferences,
		Projection:                  snapshot.Projection, AIPolicy: snapshot.AIPolicy,
		DatabaseCategoryColumnsPresent: snapshot.DatabaseCategoryColumnsPresent,
		TaskOneBackfillVerified:        snapshot.TaskOneBackfillVerified,
		Failures:                       []string{},
	}
	for _, objectType := range SupportedObjectTypes {
		totals, found := snapshot.ByObjectType[objectType]
		if !found || totals.Total < 0 || totals.Meaningful < 0 || totals.Unclassified < 0 || totals.Invalid < 0 ||
			totals.Meaningful+totals.Unclassified+totals.Invalid != totals.Total {
			return ClassificationPreflightReport{}, ErrInvalidClassificationPreflight
		}
		report.ByObjectType[objectType] = totals
		report.Totals.Total += totals.Total
		report.Totals.Meaningful += totals.Meaningful
		report.Totals.Unclassified += totals.Unclassified
		report.Totals.Invalid += totals.Invalid
	}
	if report.Totals.Invalid > 0 {
		report.Failures = append(report.Failures, "supported objects without an active effective tag")
	}
	if report.UnresolvedRetiredReferences > 0 {
		report.Failures = append(report.Failures, "unresolved merged or archived tag references")
	}
	if report.DatabaseCategoryColumnsPresent {
		report.Failures = append(report.Failures, "generic Category database column remains")
	}
	if !report.TaskOneBackfillVerified {
		report.Failures = append(report.Failures, "Task 1 Unclassified backfill is not verified")
	}
	if len(report.Failures) != 0 {
		return report, ErrClassificationPreflightFailed
	}
	report.Passed = true
	report.VerifiedNoOpAt = s.now().UTC()
	if err := session.RecordVerifiedNoOp(ctx, report); err != nil {
		return ClassificationPreflightReport{}, err
	}
	if err := session.Commit(ctx); err != nil {
		return ClassificationPreflightReport{}, err
	}
	return report, nil
}

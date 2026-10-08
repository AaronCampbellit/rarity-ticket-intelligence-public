package tagging

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type migrationRepositoryStub struct {
	snapshot   ClassificationPreflightSnapshot
	err        error
	recorded   *ClassificationPreflightReport
	operations []string
}

type migrationPreflightSessionStub struct {
	repository *migrationRepositoryStub
	closed     bool
}

func (r *migrationRepositoryStub) BeginClassificationPreflight(context.Context, string) (ClassificationPreflightSession, error) {
	if r.err != nil {
		return nil, r.err
	}
	return &migrationPreflightSessionStub{repository: r}, nil
}

func (s *migrationPreflightSessionStub) Snapshot() ClassificationPreflightSnapshot {
	return s.repository.snapshot
}
func (s *migrationPreflightSessionStub) RecordVerifiedNoOp(_ context.Context, report ClassificationPreflightReport) error {
	s.repository.operations = append(s.repository.operations, "record")
	s.repository.recorded = &report
	return nil
}
func (s *migrationPreflightSessionStub) Commit(context.Context) error {
	s.repository.operations = append(s.repository.operations, "commit")
	s.closed = true
	return nil
}
func (s *migrationPreflightSessionStub) Rollback(context.Context) error {
	if !s.closed {
		s.repository.operations = append(s.repository.operations, "rollback")
		s.closed = true
	}
	return nil
}

func completePreflightSnapshot() ClassificationPreflightSnapshot {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	return ClassificationPreflightSnapshot{
		MSPID: "msp-1", MSPDisplayID: "MSP-001",
		MigrationRunID: "task-one-run",
		ByObjectType: map[ObjectType]ClassificationObjectTotals{
			ObjectWorkRecord:       {Total: 3, Meaningful: 2, Unclassified: 1},
			ObjectTask:             {Total: 2, Meaningful: 2},
			ObjectProject:          {Total: 1, Meaningful: 1},
			ObjectAsset:            {Total: 1, Meaningful: 1},
			ObjectKnowledgeArticle: {Total: 1, Meaningful: 1},
			ObjectTimeEntry:        {Total: 1, Unclassified: 1},
		},
		Projection: ClassificationProjectionState{
			AsOf: now.Add(-45 * time.Second), LagSeconds: 45, PendingEvents: 2,
		},
		AIPolicy: ClassificationPreflightPolicy{
			AutomaticApplyEnabled: true, AutomaticApplyThreshold: .97,
			ModelProfileID: "model-1", Version: 4,
		},
		TaskOneBackfillVerified: true,
	}
}

func TestMigrationClassificationPreflightReportsExactTotalsAndRecordsVerifiedNoOp(t *testing.T) {
	repository := &migrationRepositoryStub{snapshot: completePreflightSnapshot()}
	service := NewMigrationService(repository, func() time.Time {
		return time.Date(2026, 8, 8, 12, 1, 0, 0, time.UTC)
	})

	report, err := service.ClassificationPreflight(context.Background(), "MSP-001")
	if err != nil {
		t.Fatalf("classification preflight: %v", err)
	}
	if !report.Passed || report.VerifiedNoOpAt.IsZero() || repository.recorded == nil {
		t.Fatalf("report=%+v recorded=%+v", report, repository.recorded)
	}
	if !reflect.DeepEqual(repository.operations, []string{"record", "commit"}) {
		t.Fatalf("preflight operations=%v want atomic record then commit", repository.operations)
	}
	want := ClassificationObjectTotals{Total: 9, Meaningful: 7, Unclassified: 2}
	if !reflect.DeepEqual(report.Totals, want) {
		t.Fatalf("totals=%+v want=%+v", report.Totals, want)
	}
	if len(report.ByObjectType) != 6 || report.Projection.PendingEvents != 2 ||
		!report.AIPolicy.AutomaticApplyEnabled || report.AIPolicy.AutomaticApplyThreshold != .97 ||
		report.DatabaseCategoryColumnsPresent || report.UnresolvedRetiredReferences != 0 {
		t.Fatalf("incomplete exact report: %+v", report)
	}
}

func TestMigrationClassificationPreflightRefusesMissingEffectiveTagsAndInvalidReferences(t *testing.T) {
	for name, mutate := range map[string]func(*ClassificationPreflightSnapshot){
		"missing effective classification": func(snapshot *ClassificationPreflightSnapshot) {
			totals := snapshot.ByObjectType[ObjectAsset]
			totals.Invalid = 1
			totals.Meaningful = 0
			snapshot.ByObjectType[ObjectAsset] = totals
		},
		"unresolved retired reference": func(snapshot *ClassificationPreflightSnapshot) {
			snapshot.UnresolvedRetiredReferences = 1
		},
		"generic category source": func(snapshot *ClassificationPreflightSnapshot) {
			snapshot.DatabaseCategoryColumnsPresent = true
		},
		"unverified task one backfill": func(snapshot *ClassificationPreflightSnapshot) {
			snapshot.TaskOneBackfillVerified = false
		},
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := completePreflightSnapshot()
			mutate(&snapshot)
			repository := &migrationRepositoryStub{snapshot: snapshot}
			report, err := NewMigrationService(repository, time.Now).ClassificationPreflight(context.Background(), "MSP-001")
			if !errors.Is(err, ErrClassificationPreflightFailed) || report.Passed || len(report.Failures) == 0 {
				t.Fatalf("report=%+v error=%v", report, err)
			}
			if repository.recorded != nil {
				t.Fatalf("failed preflight recorded a no-op run: %+v", repository.recorded)
			}
			if !reflect.DeepEqual(repository.operations, []string{"rollback"}) {
				t.Fatalf("failed preflight operations=%v want rollback only", repository.operations)
			}
		})
	}
}

func TestMigrationClassificationPreflightRejectsIncompleteRepositoryData(t *testing.T) {
	snapshot := completePreflightSnapshot()
	delete(snapshot.ByObjectType, ObjectTimeEntry)
	repository := &migrationRepositoryStub{snapshot: snapshot}
	_, err := NewMigrationService(repository, time.Now).ClassificationPreflight(context.Background(), "MSP-001")
	if !errors.Is(err, ErrInvalidClassificationPreflight) || repository.recorded != nil {
		t.Fatalf("error=%v recorded=%+v", err, repository.recorded)
	}
}

func TestClassificationPreflightJSONNamesDatabaseSchemaEvidencePrecisely(t *testing.T) {
	body, err := json.Marshal(ClassificationPreflightReport{DatabaseCategoryColumnsPresent: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"category_source_present"`) ||
		!strings.Contains(string(body), `"database_category_columns_present":true`) {
		t.Fatalf("preflight JSON does not distinguish database schema from repository source validation: %s", body)
	}
}

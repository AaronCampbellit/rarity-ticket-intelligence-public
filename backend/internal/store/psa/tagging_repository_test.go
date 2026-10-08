package psa

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

var _ tagging.AssociationRepository = (*TaggingRepository)(nil)

func taggingFacts(action, subjectID string, version int64) (
	mutation.AuditRecord,
	mutation.EventRecord,
) {
	at := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	return mutation.AuditRecord{
			ID: "audit-id", OccurredAt: at, MSPID: "msp-id",
			ActorType: "technician", ActorID: "actor-id", Action: action,
			SubjectType: "tag", SubjectID: subjectID,
			SubjectVersion: version, Source: "api",
			CorrelationID: "correlation-id",
		}, mutation.EventRecord{
			EventID: "event-id", EventType: action, SchemaVersion: 1,
			OccurredAt: at, MSPID: "msp-id", ActorType: "technician",
			ActorID: "actor-id", SubjectType: "tag",
			SubjectID: subjectID, SubjectVersion: version,
			Source: "api", CorrelationID: "correlation-id",
		}
}

func TestTaggingRepositoryRejectsAmbiguousTermsAcrossLabelsAndSynonyms(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*bool) = true
	}}}
	err := NewTaggingRepository(db).TermsAvailable(
		context.Background(),
		"msp-id",
		"",
		[]string{"vpn", "virtual private network"},
	)
	if !errors.Is(err, tagging.ErrDuplicateTerm) {
		t.Fatalf("TermsAvailable() error=%v, want duplicate", err)
	}
	if !strings.Contains(db.query, "FROM tags") ||
		!strings.Contains(db.query, "FROM tag_synonyms") ||
		!strings.Contains(db.query, "normalized_label = ANY") {
		t.Fatalf("term query does not span the global catalog: %s", db.query)
	}
}

func TestTaggingRepositoryWritesCatalogLifecycleAtomically(t *testing.T) {
	audit, event := taggingFacts(
		"classification.tag.created", "tag-id", 1,
	)
	tx := &fakeSalesTx{}
	err := NewTaggingRepository(&fakeSalesDB{tx: tx}).CreateTag(
		context.Background(),
		tagging.CatalogMutation{
			Tag: &tagging.Tag{
				ID: "tag-id", MSPID: "msp-id",
				InternalKey: "taxonomy.custom.tag-id",
				Label:       "Microsoft 365", GroupID: "group-id",
				State:    tagging.StateActive,
				Synonyms: []string{"M365", "Office 365"}, Version: 1,
			},
			Audit: audit,
			Event: event,
		},
	)
	if err != nil {
		t.Fatalf("CreateTag() error=%v", err)
	}
	assertQueryOrder(
		t,
		tx.queries,
		"INSERT INTO tags",
		"INSERT INTO tag_synonyms",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
	if !tx.committed || tx.rolledBack {
		t.Fatalf("committed=%v rolledBack=%v", tx.committed, tx.rolledBack)
	}
}

func TestTaggingRepositoryMergeMovesAssignmentsAndKeepsHistory(t *testing.T) {
	audit, event := taggingFacts(
		"classification.tag.merged", "retired-id", 3,
	)
	tx := &fakeSalesTx{
		queryRow: fakeRow{scan: func(destinations ...any) {
			*destinations[0].(*any) = int64(1)
		}},
	}
	err := NewTaggingRepository(&fakeSalesDB{tx: tx}).Merge(
		context.Background(),
		tagging.MergeMutation{
			Retired: tagging.Tag{
				ID: "retired-id", MSPID: "msp-id",
				State:           tagging.StateMerged,
				MergedIntoTagID: "survivor-id", Version: 3,
			},
			Survivor: tagging.Tag{
				ID: "survivor-id", MSPID: "msp-id",
				State: tagging.StateActive, Version: 4,
			},
			ExpectedVersion: 2,
			Reason:          "Duplicate",
			Audit:           audit,
			Event:           event,
		},
	)
	if err != nil {
		t.Fatalf("Merge() error=%v", err)
	}
	if len(tx.calls) == 0 ||
		!strings.Contains(tx.calls[0], "pg_advisory_xact_lock") {
		t.Fatalf("Merge() did not acquire the tag-mutation namespace first: %v", tx.calls)
	}
	assertQueryOrder(
		t,
		tx.queries,
		"SELECT id::text FROM tags",
		"SELECT id::text FROM tags",
		"INSERT INTO object_tag_assignments",
		"INSERT INTO tag_assignment_events",
		"INSERT INTO tag_assignment_events",
		"DELETE FROM object_tag_assignments",
		"UPDATE tags",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
	if !strings.Contains(tx.queries[2], "ON CONFLICT") ||
		!strings.Contains(tx.queries[3], "'removed'") ||
		!strings.Contains(tx.queries[4], "'added'") {
		t.Fatalf("merge assignment history is incomplete: %#v", tx.queries[2:5])
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("committed=%v rolledBack=%v", tx.committed, tx.rolledBack)
	}
}

func TestTaggingRepositoryArchiveAppliesFallbackBeforeRetiringTag(t *testing.T) {
	audit, event := taggingFacts(
		"classification.tag.archived", "tag-id", 5,
	)
	tx := &fakeSalesTx{
		queryRow: fakeRow{scan: func(destinations ...any) {
			*destinations[0].(*any) = int64(1)
		}},
	}
	err := NewTaggingRepository(&fakeSalesDB{tx: tx}).Archive(
		context.Background(),
		tagging.ArchiveMutation{
			Tag: tagging.Tag{
				ID: "tag-id", MSPID: "msp-id",
				State: tagging.StateArchived, Version: 5,
			},
			UnclassifiedTag: tagging.Tag{
				ID: "unclassified-id", MSPID: "msp-id",
				State: tagging.StateActive, SystemManaged: true,
			},
			ExpectedVersion: 4,
			Reason:          "Technology retired",
			Audit:           audit,
			Event:           event,
		},
	)
	if err != nil {
		t.Fatalf("Archive() error=%v", err)
	}
	if len(tx.calls) == 0 ||
		!strings.Contains(tx.calls[0], "pg_advisory_xact_lock") {
		t.Fatalf("Archive() did not acquire the tag-mutation namespace first: %v", tx.calls)
	}
	assertQueryOrder(
		t,
		tx.queries,
		"SELECT id::text FROM tags",
		"SELECT id::text FROM tags",
		"INSERT INTO object_tag_assignments",
		"INSERT INTO tag_assignment_events",
		"INSERT INTO tag_assignment_events",
		"DELETE FROM object_tag_assignments",
		"UPDATE tags",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
	if !strings.Contains(tx.queries[2], "NOT EXISTS") ||
		!strings.Contains(tx.queries[2], "system_fallback") {
		t.Fatalf("fallback query does not limit cascade to last meaningful tag: %s", tx.queries[2])
	}
	if !strings.Contains(tx.queries[4], "'removed'") ||
		!strings.Contains(tx.queries[5], "DELETE FROM object_tag_assignments") {
		t.Fatalf("archive did not preserve removal history before retiring the current assignment: %#v", tx.queries[4:6])
	}
}

func TestTaggingRepositoryHistoryUsesTaskParentAssociationIntervals(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	_, err := NewTaggingRepository(db).History(context.Background(), tagging.TargetRef{
		MSPID: "msp-id", ClientID: "client-id", ObjectType: tagging.ObjectTask, ObjectID: "task-id",
	})
	if err != nil {
		t.Fatalf("History() error=%v", err)
	}
	for _, fragment := range []string{
		"task_movement_history", "lead(movement.moved_at)", "task.created_at", "event.occurred_at >= interval.started_at",
		"direct_event.object_type = 'task'", "direct_event.tag_id = event.tag_id", "direct_event.occurred_at <= event.occurred_at",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("history query missing interval evidence %q: %s", fragment, db.query)
		}
	}
}

func TestTaggingRepositoryLocksTaskInheritedSourceBeforeMeaningfulAcceptance(t *testing.T) {
	tx := &fakeSalesTx{
		queryRow: fakeRow{scan: func(destinations ...any) {
			if len(destinations) == 2 {
				*destinations[0].(*string) = "project"
				*destinations[1].(*string) = "project-id"
			} else {
				*destinations[0].(*string) = "project-id"
			}
		}},
	}
	err := lockTaskInheritedSource(context.Background(), tx, tagging.TargetRef{MSPID: "msp-id", ClientID: "client-id", ObjectType: tagging.ObjectTask, ObjectID: "task-id"})
	if err != nil {
		t.Fatalf("lockTaskInheritedSource() error=%v", err)
	}
	queries := strings.Join(tx.calls, "\n")
	for _, fragment := range []string{"parent_type, parent_id", "FOR UPDATE"} {
		if !strings.Contains(queries, fragment) {
			t.Fatalf("inherited lock contract missing %q: %s", fragment, queries)
		}
	}
}

func TestLockTaskInheritedSourceSkipsParentsWithoutProjectInheritance(t *testing.T) {
	for _, parentType := range []string{"work_record", "opportunity"} {
		t.Run(parentType, func(t *testing.T) {
			tx := &fakeSalesTx{
				queryRow: fakeRow{scan: func(destinations ...any) {
					*destinations[0].(*string) = parentType
					*destinations[1].(*string) = "parent-id"
				}},
			}
			err := lockTaskInheritedSource(
				context.Background(),
				tx,
				tagging.TargetRef{
					MSPID:      "msp-id",
					ClientID:   "client-id",
					ObjectType: tagging.ObjectTask,
					ObjectID:   "task-id",
				},
			)
			if err != nil {
				t.Fatalf("lockTaskInheritedSource() error=%v", err)
			}
			if len(tx.calls) != 1 {
				t.Fatalf("query count=%d, want scoped Task lookup only", len(tx.calls))
			}
		})
	}
}

func TestLockTaskInheritedSourceHidesMissingAndUnsupportedParents(t *testing.T) {
	target := tagging.TargetRef{
		MSPID:      "msp-id",
		ClientID:   "client-id",
		ObjectType: tagging.ObjectTask,
		ObjectID:   "task-id",
	}
	t.Run("missing or cross-scope task", func(t *testing.T) {
		tx := &fakeSalesTx{queryRow: fakeRow{err: pgx.ErrNoRows}}
		if err := lockTaskInheritedSource(
			context.Background(), tx, target,
		); !errors.Is(err, scope.ErrNotFound) {
			t.Fatalf("lockTaskInheritedSource() error=%v, want scoped not found", err)
		}
	})
	t.Run("unsupported parent type", func(t *testing.T) {
		tx := &fakeSalesTx{
			queryRow: fakeRow{scan: func(destinations ...any) {
				*destinations[0].(*string) = "client"
				*destinations[1].(*string) = "parent-id"
			}},
		}
		if err := lockTaskInheritedSource(
			context.Background(), tx, target,
		); !errors.Is(err, scope.ErrNotFound) {
			t.Fatalf("lockTaskInheritedSource() error=%v, want scoped not found", err)
		}
	})
}

func TestLockTaskInheritedSourceHidesBrokenProjectAndPhaseParents(t *testing.T) {
	target := tagging.TargetRef{
		MSPID:      "msp-id",
		ClientID:   "client-id",
		ObjectType: tagging.ObjectTask,
		ObjectID:   "task-id",
	}
	for _, parentType := range []string{"project", "phase"} {
		t.Run(parentType, func(t *testing.T) {
			tx := &fakeSalesTx{
				queryRows: []row{
					fakeRow{scan: func(destinations ...any) {
						*destinations[0].(*string) = parentType
						*destinations[1].(*string) = "broken-or-cross-scope-parent"
					}},
					fakeRow{err: pgx.ErrNoRows},
				},
			}
			if err := lockTaskInheritedSource(
				context.Background(), tx, target,
			); !errors.Is(err, scope.ErrNotFound) {
				t.Fatalf("lockTaskInheritedSource() error=%v, want scoped not found", err)
			}
			if len(tx.calls) != 2 {
				t.Fatalf("query count=%d, want Task and scoped %s lookup", len(tx.calls), parentType)
			}
			for _, fragment := range []string{"msp_id=$2", "client_id=$3"} {
				if !strings.Contains(tx.query, fragment) {
					t.Fatalf("%s parent lookup missing %q scope: %s", parentType, fragment, tx.query)
				}
			}
			if parentType == "phase" {
				for _, fragment := range []string{
					"project.msp_id=phase.msp_id",
					"project.client_id=phase.client_id",
				} {
					if !strings.Contains(tx.query, fragment) {
						t.Fatalf("phase Project join missing %q scope: %s", fragment, tx.query)
					}
				}
			}
			if len(tx.queryArgs) != 3 ||
				tx.queryArgs[0] != "broken-or-cross-scope-parent" ||
				tx.queryArgs[1] != target.MSPID ||
				tx.queryArgs[2] != target.ClientID {
				t.Fatalf("%s parent lookup args=%v, want parent/MSP/Client", parentType, tx.queryArgs)
			}
		})
	}
}

func TestTagMutationNamespaceUsesOneMSPScopedAdvisoryLock(t *testing.T) {
	tx := &fakeSalesTx{
		queryRow: fakeRow{scan: func(destinations ...any) {
			*destinations[0].(*any) = int64(1)
		}},
	}
	if err := lockTagMutationNamespace(context.Background(), tx, "msp-id"); err != nil {
		t.Fatalf("lockTagMutationNamespace() error=%v", err)
	}
	if len(tx.calls) != 1 ||
		!strings.Contains(tx.calls[0], "pg_advisory_xact_lock") ||
		len(tx.queryArgs) != 1 ||
		tx.queryArgs[0] != "classification-tag-mutation:msp-id" {
		t.Fatalf("namespace lock query=%q args=%v", strings.Join(tx.calls, "\n"), tx.queryArgs)
	}
}

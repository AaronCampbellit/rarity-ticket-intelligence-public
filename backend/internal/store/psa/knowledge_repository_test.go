package psa

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestKnowledgeRepositoryListsOnlyScopedInternalMetadata(t *testing.T) {
	at := time.Date(2026, time.July, 30, 18, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*(destinations[0].(*string)) = "article-id"
			*(destinations[1].(*string)) = "msp-id"
			*(destinations[2].(*string)) = "client-id"
			*(destinations[3].(*string)) = "KB-100"
			*(destinations[4].(*string)) = "Reset token"
			*(destinations[5].(*string)) = "published"
			*(destinations[6].(*int64)) = 2
			*(destinations[7].(*bool)) = false
			*(destinations[8].(*time.Time)) = at
			*(destinations[9].(*string)) = "actor-id"
		},
	}}}
	articles, err := NewKnowledgeRepository(db).List(
		context.Background(),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		"token", 50,
	)
	if err != nil || len(articles) != 1 ||
		articles[0].State != knowledge.Published ||
		!strings.Contains(db.query, "NOT client_visible") ||
		!strings.Contains(db.query, "client_id = $2::uuid") {
		t.Fatalf("articles=%+v query=%s error=%v", articles, db.query, err)
	}
}

func TestKnowledgeRepositoryFindByReferenceIsExactDisplayIDFirstAndSQLBounded(t *testing.T) {
	at := time.Date(2026, time.July, 30, 18, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		knowledgeReferenceScan(at, "name-match", "KB-OTHER", " kb-\u00a0501 "),
		knowledgeReferenceScan(at, "article-id", " kb-\u00a0501 ", "Reset token"),
	}}}
	found, err := NewKnowledgeRepository(db).FindByReference(
		context.Background(),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		"\u2003KB-\u2003501\u2003", 2,
	)
	if err != nil || len(found) != 1 || found[0].ID != "article-id" {
		t.Fatalf("found=%+v error=%v", found, err)
	}
	for _, fragment := range []string{
		"translate(", "regexp_replace(", "[[:space:]]+",
		"CASE WHEN", "LIMIT $5", "NOT client_visible",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("exact knowledge query missing %q: %s", fragment, db.query)
		}
	}
	if len(db.args) != 5 || db.args[2] != "kb- 501" || db.args[4] != 2 {
		t.Fatalf("query args=%#v", db.args)
	}
}

func knowledgeReferenceScan(at time.Time, id, displayID, title string) func(...any) {
	return func(destinations ...any) {
		*(destinations[0].(*string)) = id
		*(destinations[1].(*string)) = "msp-id"
		*(destinations[2].(*string)) = "client-id"
		*(destinations[3].(*string)) = displayID
		*(destinations[4].(*string)) = title
		*(destinations[5].(*string)) = "draft"
		*(destinations[6].(*int64)) = 1
		*(destinations[7].(*bool)) = false
		*(destinations[8].(*time.Time)) = at
		*(destinations[9].(*string)) = "actor-id"
	}
}

func TestKnowledgeRepositoryFindByReferenceReturnsTwoNormalizedTitleMatchesForAmbiguity(t *testing.T) {
	at := time.Date(2026, time.July, 30, 18, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		knowledgeReferenceScan(at, "article-b", "KB-B", "\u00a0VPN\u2003Recovery "),
		knowledgeReferenceScan(at, "article-a", "KB-A", " vpn recovery"),
	}}}
	found, err := NewKnowledgeRepository(db).FindByReference(
		context.Background(),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		"VPN\tRECOVERY", 2,
	)
	if err != nil || len(found) != 2 {
		t.Fatalf("found=%+v error=%v", found, err)
	}
}

func TestKnowledgeRepositoryFindByReferenceScansMetadata(t *testing.T) {
	at := time.Date(2026, time.July, 30, 18, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*(destinations[0].(*string)) = "article-id"
			*(destinations[1].(*string)) = "msp-id"
			*(destinations[2].(*string)) = "client-id"
			*(destinations[3].(*string)) = "KB-501"
			*(destinations[4].(*string)) = "Reset token"
			*(destinations[5].(*string)) = "draft"
			*(destinations[6].(*int64)) = 1
			*(destinations[7].(*bool)) = false
			*(destinations[8].(*time.Time)) = at
			*(destinations[9].(*string)) = "actor-id"
		},
	}}}
	found, err := NewKnowledgeRepository(db).FindByReference(
		context.Background(),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		"KB-501", 2,
	)
	if err != nil || len(found) != 1 || found[0].ID != "article-id" ||
		found[0].State != knowledge.Draft {
		t.Fatalf("found=%+v error=%v", found, err)
	}
}

func TestKnowledgeRepositoryCreatesAndRevisesDraftsAtomically(t *testing.T) {
	at := time.Date(2026, time.July, 29, 18, 0, 0, 0, time.UTC)
	base := knowledge.DraftMutation{
		Created: true,
		Article: knowledge.Article{
			ID: "article", MSPID: "msp", ClientID: "client", DisplayID: "KB-1",
			Title: "Reset token", State: knowledge.Draft, CurrentVersion: 1,
			UpdatedAt: at, UpdatedBy: "actor",
		},
		Version: knowledge.Version{
			ArticleID: "article", Version: 1, Body: "Body", State: knowledge.Draft,
			CreatedAt: at, CreatedBy: "actor",
		},
		Audit: mutation.AuditRecord{
			ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor",
			Action: "knowledge.draft.created", SubjectType: "knowledge_article",
			SubjectID: "article", SubjectVersion: 1, Source: "api",
			CorrelationID: "correlation",
		},
		Event: mutation.EventRecord{
			EventID: "event", EventType: "knowledge.draft.created",
			SchemaVersion: 1, OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor",
			SubjectType: "knowledge_article", SubjectID: "article",
			SubjectVersion: 1, Source: "api", CorrelationID: "correlation",
		},
		InitialTags: testInitialTags(at),
	}
	tx := &fakeSalesTx{}
	repository := NewKnowledgeRepository(&fakeSalesDB{tx: tx})
	if err := repository.CreateDraftAtomic(context.Background(), base); err != nil {
		t.Fatalf("CreateDraftAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations",
		"pg_advisory_xact_lock",
		"SELECT title, display_id",
		"INSERT INTO knowledge_articles",
		"INSERT INTO knowledge_article_versions",
		"INSERT INTO object_tag_assignments", "INSERT INTO tag_assignment_events",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)

	tx = &fakeSalesTx{}
	repository = NewKnowledgeRepository(&fakeSalesDB{tx: tx})
	base.Created = false
	base.Article.CurrentVersion = 2
	base.Version.Version = 2
	base.Event.EventType = "knowledge.draft.revised"
	if err := repository.ReviseDraftAtomic(context.Background(), base); err != nil {
		t.Fatalf("ReviseDraftAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations",
		"UPDATE knowledge_articles",
		"INSERT INTO knowledge_article_versions",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	if !strings.Contains(tx.queries[1], "state = 'draft'") {
		t.Fatalf("revision update can overwrite published state: %s", tx.queries[1])
	}
}

func TestKnowledgeRepositoryCreateSerializesNormalizedAllLifecycleIdentityBeforeFacts(t *testing.T) {
	tx := &fakeSalesTx{queryResult: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*(destinations[0].(*string)) = "  RESET\u2003TOKEN "
			*(destinations[1].(*string)) = "KB-OLD"
		},
	}}}
	repository := NewKnowledgeRepository(&fakeSalesDB{tx: tx})
	mutation := knowledge.DraftMutation{
		Created: true,
		Article: knowledge.Article{
			ID: "article", MSPID: "msp", ClientID: "client", DisplayID: "KB-NEW",
			Title: "reset token", State: knowledge.Draft, CurrentVersion: 1,
			UpdatedAt: time.Now(), UpdatedBy: "actor",
		},
		Version: knowledge.Version{ArticleID: "article", Version: 1, Body: "Body", State: knowledge.Draft},
	}
	err := repository.CreateDraftAtomic(context.Background(), mutation)
	if !errors.Is(err, knowledge.ErrIdentityConflict) {
		t.Fatalf("CreateDraftAtomic() error=%v, want identity conflict", err)
	}
	if tx.committed || !tx.rolledBack || len(tx.queries) != 3 {
		t.Fatalf("conflict transaction=%+v", tx)
	}
	if !strings.Contains(tx.queries[0], "FROM client_organizations") ||
		!strings.Contains(tx.queries[1], "pg_advisory_xact_lock") ||
		!strings.Contains(tx.queries[2], "SELECT title, display_id") ||
		strings.Contains(tx.queries[2], "state =") ||
		strings.Contains(tx.queries[2], "client_visible") {
		t.Fatalf("identity operations=%v", tx.queries)
	}
}

func TestKnowledgeRepositoryRevisionStateRaceRollsBackBeforeNewVersionOrFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 2}
	repository := NewKnowledgeRepository(&fakeSalesDB{tx: tx})
	err := repository.ReviseDraftAtomic(context.Background(), knowledge.DraftMutation{
		Article: knowledge.Article{
			ID: "article", MSPID: "msp", ClientID: "client", DisplayID: "KB-1",
			Title: "Revised", State: knowledge.Draft, CurrentVersion: 4,
			UpdatedAt: time.Now(), UpdatedBy: "actor",
		},
		Version: knowledge.Version{
			ArticleID: "article", Version: 4, Body: "Revision body", State: knowledge.Draft,
		},
	})
	if !errors.Is(err, object.ErrVersionConflict) || !tx.rolledBack || tx.committed {
		t.Fatalf("race error=%v transaction=%+v", err, tx)
	}
	if len(tx.queries) != 2 {
		t.Fatalf("race wrote version/facts after state drift: %v", tx.queries)
	}
}

func TestKnowledgeRepositoryPublishesExactDraftAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 29, 18, 0, 0, 0, time.UTC)
	err := NewKnowledgeRepository(&fakeSalesDB{tx: tx}).PublishAtomic(
		context.Background(),
		knowledge.PublishMutation{
			ExpectedClientVersion: 4,
			Article: knowledge.Article{
				ID: "article", MSPID: "msp", ClientID: "client",
				State: knowledge.Published, CurrentVersion: 2,
				UpdatedAt: at, UpdatedBy: "actor",
			},
			Version: knowledge.Version{
				ArticleID: "article", Version: 2, Body: "Body",
				State: knowledge.Published, PublishedAt: &at, PublishedBy: "actor",
			},
			Audit: mutation.AuditRecord{
				ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client",
				ActorType: "technician", ActorID: "actor",
				Action: "knowledge.published", SubjectType: "knowledge_article",
				SubjectID: "article", SubjectVersion: 2, Source: "api",
				CorrelationID: "correlation",
			},
			Event: mutation.EventRecord{
				EventID: "event", EventType: "knowledge.published",
				SchemaVersion: 1, OccurredAt: at, MSPID: "msp", ClientID: "client",
				ActorType: "technician", ActorID: "actor",
				SubjectType: "knowledge_article", SubjectID: "article",
				SubjectVersion: 2, Source: "api", CorrelationID: "correlation",
			},
		},
	)
	if err != nil {
		t.Fatalf("PublishAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations",
		"UPDATE knowledge_articles",
		"UPDATE knowledge_article_versions",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	if !strings.Contains(tx.queries[0], "($3 = 0 OR version = $3)") ||
		len(tx.args[0]) != 3 || tx.args[0][2] != int64(4) {
		t.Fatalf("publication Client fence=%q args=%+v", tx.queries[0], tx.args)
	}
}

func TestKnowledgeRepositoryRejectsInactiveOrStaleClientBeforePublicationFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 1}
	err := NewKnowledgeRepository(&fakeSalesDB{tx: tx}).PublishAtomic(
		context.Background(),
		knowledge.PublishMutation{
			ExpectedClientVersion: 4,
			Article:               knowledge.Article{ID: "article", MSPID: "msp", ClientID: "client", CurrentVersion: 2},
			Version:               knowledge.Version{ArticleID: "article", Version: 2},
			Audit:                 validAudit(time.Now().UTC(), "knowledge.published", "knowledge_article", "article"),
			Event:                 validEvent(time.Now().UTC(), "knowledge.published", "knowledge_article", "article"),
		},
	)
	if !errors.Is(err, scope.ErrNotFound) || len(tx.queries) != 1 || tx.committed || !tx.rolledBack {
		t.Fatalf("PublishAtomic() error=%v queries=%d committed=%t rolled_back=%t", err, len(tx.queries), tx.committed, tx.rolledBack)
	}
}

func TestKnowledgeRepositoryFindIsCurrentVersionAndClientScoped(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: context.Canceled}}
	_, _ = NewKnowledgeRepository(db).Find(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"article",
	)
	if !strings.Contains(db.query, "article.client_id = $3::uuid") ||
		!strings.Contains(db.query, "version.version = article.current_version") ||
		!strings.Contains(db.query, "NOT article.client_visible") {
		t.Fatalf("knowledge read lacks exact scope/version boundary: %s", db.query)
	}
}

package psa

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/collaboration"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mentions"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
)

func TestMentionParentLocksPermitMSPGlobalAndHydrateExactClient(t *testing.T) {
	for _, parentType := range []mentions.ParentType{mentions.ParentWorkRecord, mentions.ParentProject, mentions.ParentTask} {
		t.Run(string(parentType), func(t *testing.T) {
			tx := &fakeSalesTx{queryRow: fakeRow{scan: func(destinations ...any) {
				*destinations[0].(*string) = "msp"
				*destinations[1].(*string) = "hydrated-client"
			}}}
			trusted, err := lockMentionParent(context.Background(), tx, mentions.SourceRef{MSPID: "msp", ParentType: parentType, ParentID: "parent", SourceKind: mentions.SourceComment})
			if err != nil || trusted.ClientID != "hydrated-client" {
				t.Fatalf("trusted=%+v err=%v", trusted, err)
			}
			if !strings.Contains(tx.query, "$3='' OR") || !strings.Contains(tx.query, "NULLIF($3,'')::uuid") {
				t.Fatalf("MSP-global parent lock still casts empty Client: %s", tx.query)
			}
		})
	}
}

func TestMentionAccessSQLUsesCurrentRolesParentVisibilityAndNotTeamsAsGrant(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	repository := NewMentionRepository(db)
	_, _ = repository.ListCandidates(context.Background(), mentions.CandidateQuery{AuthorID: "author", Source: mentions.SourceRef{MSPID: "msp", ClientID: "client", ParentType: mentions.ParentTask, ParentID: "task", SourceKind: mentions.SourceComment}})
	query := db.query
	for _, fragment := range []string{"role_assignments", "role_capabilities", "mention.create", "work_record.edit", "project.edit", "tasks", "expires_at"} {
		if !strings.Contains(query, fragment) {
			t.Errorf("candidate authorization SQL missing %q", fragment)
		}
	}
	if strings.Contains(query, "team_memberships") {
		t.Fatal("team membership used as access grant")
	}
}

func TestInternalCollaborationAccessUsesParentPermissionsNotMentionWidgetPermissions(t *testing.T) {
	query := internalCollaborationAccessPredicate("$1", "$2", "$3", "$4", "$5")
	for _, fragment := range []string{"work_record.read", "work_record.edit", "project.read", "project.edit"} {
		if !strings.Contains(query, fragment) {
			t.Errorf("internal collaboration access SQL missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"mention.read", "mention.create"} {
		if strings.Contains(query, forbidden) {
			t.Errorf("internal collaboration history incorrectly depends on %q: %s", forbidden, query)
		}
	}
}

func TestMentionCandidateSQLReturnsTheExactSortedEligibleTeamSnapshot(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*bool) = true
	}}, queryRows: &fakeRows{}}
	repository := NewMentionRepository(db)
	_, _ = repository.ListCandidates(context.Background(), mentions.CandidateQuery{
		AuthorID: "author", Source: mentions.SourceRef{MSPID: "msp", ClientID: "client", ParentType: mentions.ParentWorkRecord, ParentID: "work", SourceKind: mentions.SourceComment},
	})
	for _, fragment := range []string{"eligible_member_ids", "array_agg", "DISTINCT", "ORDER BY membership.technician_id::text", "membership.technician_id<>$5::uuid"} {
		if !strings.Contains(db.query, fragment) {
			t.Errorf("candidate team snapshot SQL missing %q", fragment)
		}
	}
}

func TestMentionCandidateSQLFiltersExactTeamIDBeforeTheResultCap(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*bool) = true
	}}, queryRows: &fakeRows{}}
	repository := NewMentionRepository(db)
	_, _ = repository.ListCandidates(context.Background(), mentions.CandidateQuery{
		AuthorID: "author", Source: mentions.SourceRef{MSPID: "msp", ClientID: "client", ParentType: mentions.ParentWorkRecord, ParentID: "work", SourceKind: mentions.SourceComment},
		ExactTargetType: mentions.TargetTeam, ExactTargetID: "team-1",
	})
	if !strings.Contains(db.query, "team.id=NULLIF($9,'')::uuid") || !strings.Contains(db.query, "LIMIT $7") {
		t.Fatalf("exact Team filter is not applied before cap: %s", db.query)
	}
	if len(db.args) != 9 || db.args[7] != string(mentions.TargetTeam) || db.args[8] != "team-1" {
		t.Fatalf("exact Team args=%+v", db.args)
	}
}

func TestMentionWidgetSQLIsRecipientScopedStableAndAuthorizationConsistent(t *testing.T) {
	tx := &fakeSalesTx{queryRow: fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*int) = 1
		*destinations[1].(*int) = 0
		*destinations[2].(*int) = 0
	}}, queryResult: &fakeRows{}}
	repository := NewMentionRepository(&fakeSalesDB{tx: tx})
	_, _ = repository.ListWidget(context.Background(), mentions.WidgetStoreQuery{MSPID: "msp", RecipientID: "staff", State: mentions.Unread, BeforeAt: time.Now(), BeforeID: "item", Limit: 25})
	joined := strings.Join(tx.calls, "\n")
	for _, fragment := range []string{"mention_items", "recipient_id", "mention.read", "(item.last_mentioned_at, item.id)", "ORDER BY", "internal_collaboration_sources", "mention_recipient_resolutions", "resolution_path", "mention_access_invalidations", "access_loss_confirmed", "snapshot_occurrence_id=item.latest_occurrence_id", "mention_access_loss_markers", "mention_invalidation_event_claims", "authorization_revision"} {
		if !strings.Contains(joined, fragment) {
			t.Errorf("widget SQL missing %q", fragment)
		}
	}
	if !tx.committed {
		t.Fatal("consistent snapshot not committed")
	}
}

func TestMentionPreviewUsesCurrentSourceTokenOffsetsNotOccurrenceSnapshots(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: context.Canceled}}
	repository := NewMentionRepository(db)
	_, _ = repository.LoadPreview(context.Background(), mentions.PreviewStoreQuery{
		MSPID: "msp", RecipientID: "staff", ItemID: "item",
	})
	for _, fragment := range []string{
		"jsonb_array_elements(source.mention_tokens)",
		"token->>'id'=occurrence.token_id::text",
		"(current_token.token->>'start')::int",
		"(current_token.token->>'end')::int",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Errorf("preview SQL missing current-token fragment %q: %s", fragment, db.query)
		}
	}
	for _, forbidden := range []string{
		"occurrence.token->>'start'",
		"occurrence.token->>'end'",
	} {
		if strings.Contains(db.query, forbidden) {
			t.Errorf("preview SQL reads stale occurrence offsets %q: %s", forbidden, db.query)
		}
	}
}

func TestMentionParentHrefUsesAuthenticatedApplicationRoutes(t *testing.T) {
	tests := []struct {
		parentType mentions.ParentType
		want       string
	}{
		{mentions.ParentWorkRecord, "#/work?parentID=parent-1"},
		{mentions.ParentTask, "#/work?parentID=parent-1"},
		{mentions.ParentProject, "#/project?parentID=parent-1"},
	}
	for _, test := range tests {
		if got := mentionParentHref(test.parentType, "parent-1"); got != test.want {
			t.Errorf("mentionParentHref(%s)=%q want %q", test.parentType, got, test.want)
		}
	}
}

func TestMentionDeepLinkReturnsAuthorizedClientAndExactSourceHash(t *testing.T) {
	tx := &fakeSalesTx{queryRows: []row{
		fakeRow{scan: func(destinations ...any) {
			*destinations[0].(*string) = "client-2"
			*destinations[1].(*string) = string(mentions.ParentTask)
			*destinations[2].(*string) = "task-1"
			*destinations[3].(*string) = "source-1"
			*destinations[4].(*string) = string(mentions.SourceComment)
			*destinations[5].(*string) = "active"
			*destinations[6].(*string) = "token-1"
			*destinations[7].(*int64) = 4
			*destinations[8].(*bool) = true
		}},
		fakeRow{scan: func(destinations ...any) {
			*destinations[0].(*int64) = 5
		}},
	}}
	repository := NewMentionRepository(&fakeSalesDB{tx: tx})
	link, err := repository.ResolveDeepLink(context.Background(), mentions.DeepLinkStoreQuery{
		MSPID: "msp", RecipientID: "staff", ItemID: "item-1",
		OccurrenceID: "occurrence-1", ExpectedVersion: 4, ResolvedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("ResolveDeepLink() error=%v", err)
	}
	if link.ClientID != "client-2" || link.Href != "#/work?parentID=task-1&mentionOccurrenceID=occurrence-1&sourceID=source-1" || !link.SourceAvailable || link.ItemVersion != 5 {
		t.Fatalf("ResolveDeepLink()=%+v", link)
	}
	encoded, err := json.Marshal(link)
	if err != nil || !strings.Contains(string(encoded), `"token_id":"token-1"`) {
		t.Fatalf("ResolveDeepLink() omitted authenticated token identity: %s err=%v", encoded, err)
	}
}

func TestMentionStateAndDeepLinkSQLAreOwnedVersionedAndAtomic(t *testing.T) {
	stateTx := &fakeSalesTx{queryRows: []row{
		fakeRow{err: pgx.ErrNoRows},
		fakeRow{scan: func(destinations ...any) { *destinations[0].(*int64) = 3 }},
	}}
	repository := NewMentionRepository(&fakeSalesDB{tx: stateTx})
	_, stateErr := repository.ChangeState(context.Background(), mentions.StateStoreChange{MSPID: "msp", RecipientID: "staff", ItemID: "item", State: mentions.Read, ExpectedVersion: 2, ChangedAt: time.Now()})
	if !errors.Is(stateErr, object.ErrVersionConflict) {
		t.Fatalf("state error=%v", stateErr)
	}
	joinedState := strings.Join(stateTx.calls, "\n")
	if !strings.Contains(joinedState, "UPDATE mention_items AS item") || !strings.Contains(joinedState, "item.recipient_id") || !strings.Contains(joinedState, "item.version=$") || !strings.Contains(joinedState, "item.suppressed_at IS NULL") {
		t.Fatalf("unsafe state SQL: %s", stateTx.query)
	}
	if !strings.Contains(stateTx.query, "mention.read") || !strings.Contains(stateTx.query, "suppressed_at IS NULL") {
		t.Fatalf("state miss classifier omitted current access boundary: %s", stateTx.query)
	}

	deepTx := &fakeSalesTx{queryRow: fakeRow{err: context.Canceled}}
	repository = NewMentionRepository(&fakeSalesDB{tx: deepTx})
	_, _ = repository.ResolveDeepLink(context.Background(), mentions.DeepLinkStoreQuery{MSPID: "msp", RecipientID: "staff", ItemID: "item", OccurrenceID: "occurrence", ExpectedVersion: 2, ResolvedAt: time.Now()})
	if !strings.Contains(deepTx.query, "latest_occurrence_id") || !strings.Contains(deepTx.query, "FOR UPDATE") {
		t.Fatalf("deep link does not lock exact occurrence: %s", deepTx.query)
	}
}

func TestMentionTeamResolutionLocksActiveMembershipEvidence(t *testing.T) {
	tx := &fakeSalesTx{queryResult: &fakeRows{}}
	_, _ = loadMentionTeamAccess(context.Background(), tx, mentions.SourceRef{MSPID: "msp", ClientID: "client", ParentType: mentions.ParentWorkRecord, ParentID: "work", SourceKind: mentions.SourceComment}, []string{"team"}, true)
	joined := strings.Join(tx.calls, "\n")
	if !strings.Contains(joined, "FROM team_memberships") || !strings.Contains(joined, "ORDER BY team_id,technician_id") || !strings.Contains(joined, "FOR SHARE") {
		t.Fatalf("active membership evidence is not deterministically locked: %s", joined)
	}
}

func TestMentionInvalidationClaimUsesActualEventsAndCheckpointedBoundedExpansion(t *testing.T) {
	tx := &fakeSalesTx{queryResult: &fakeRows{}}
	repository := NewMentionRepository(&fakeSalesDB{tx: tx})
	_, _ = repository.ClaimAccessInvalidations(context.Background(), 25, time.Now(), 5*time.Minute)
	joined := strings.Join(tx.queries, "\n")
	for _, fragment := range []string{
		"role.unassigned", "role.capabilities_replaced", "technician.status.changed",
		"client.access.removed", "project.visibility.changed",
		"work_record.client.transferred", "task.client.transferred",
		"project.client.transferred", "internal_collaboration_source.saved",
		"lifecycle_state", "redacted", "work_record.merged",
		"work_record.deleted", "task.deleted", "project.deleted",
		"mention_invalidation_event_cursor", "mention_invalidation_event_claims",
		"mention_invalidation_sequence", "last_item_id",
		"ON CONFLICT", "FOR UPDATE SKIP LOCKED", "lease_token",
		"lease_until", "completed_at IS NULL", "LIMIT $",
		"snapshot_occurrence_id", "access_loss_confirmed",
		"mention_access_loss_markers", "authorization_revision",
		"mention_has_effective_access_at_revision",
		"expired_assignments", "assignment.expires_at<=$2",
		"'role.unassigned'", "'assignment_expired'",
	} {
		if !strings.Contains(joined, fragment) {
			t.Errorf("invalidation claim SQL missing %q", fragment)
		}
	}
	if strings.Contains(joined, "event.occurred_at>=item.last_mentioned_at") {
		t.Fatalf("invalidation expansion relies on wall-clock ordering: %s", joined)
	}
	if strings.Contains(joined, "candidate.latest_occurrence_id,false") {
		t.Fatalf("bounded intake still persists per-item non-loss rows: %s", joined)
	}
	if revisionLock, expiryLock := strings.Index(joined, "begin_mention_authorization_revision"), strings.Index(joined, "expired_assignments AS MATERIALIZED"); revisionLock < 0 || expiryLock <= revisionLock {
		t.Fatalf("expiry sweep does not acquire the MSP revision before assignment rows: %s", joined)
	}
	for _, grantOnly := range []string{"team.members.replaced", "role.assigned"} {
		if strings.Contains(joined, grantOnly) {
			t.Fatalf("grant-only event %q is queued as access-loss work: %s", grantOnly, joined)
		}
	}
	if !tx.committed {
		t.Fatal("invalidation claim transaction was not committed")
	}
}

func TestMentionInvalidationHistoricalIntakePagesRawOutboxSequences(t *testing.T) {
	tx := &fakeSalesTx{queryResult: &fakeRows{}}
	repository := NewMentionRepository(&fakeSalesDB{tx: tx})
	_, _ = repository.ClaimAccessInvalidations(context.Background(), 25, time.Now(), 5*time.Minute)
	joined := strings.Join(tx.queries, "\n")
	pageStart := strings.Index(joined, "event_page AS MATERIALIZED")
	queueStart := strings.Index(joined, "queued AS")
	cursorStart := strings.Index(joined, "UPDATE mention_invalidation_event_cursor")
	if pageStart < 0 || queueStart <= pageStart || cursorStart <= queueStart {
		t.Fatalf("invalidation intake CTE boundaries missing: %s", joined)
	}
	rawPage := joined[pageStart:queueStart]
	queue := joined[queueStart:cursorStart]
	rawWhereStart := strings.Index(rawPage, "WHERE")
	if rawWhereStart < 0 {
		t.Fatalf("raw outbox page has no cursor predicate: %s", rawPage)
	}
	rawPredicate := rawPage[rawWhereStart:]
	if strings.Contains(rawPredicate, "event.event_type") || strings.Contains(rawPredicate, "lifecycle_state") {
		t.Fatalf("raw outbox page is filtered before the durable cursor advances: %s", rawPage)
	}
	for _, fragment := range []string{"event.event_type", "lifecycle_state", "FROM event_page event", "LIMIT $1"} {
		if !strings.Contains(queue, fragment) && fragment != "LIMIT $1" {
			t.Errorf("conditional invalidation queue missing %q: %s", fragment, queue)
		}
		if fragment == "LIMIT $1" && !strings.Contains(rawPage, fragment) {
			t.Errorf("raw outbox page missing %q: %s", fragment, rawPage)
		}
	}
	if !strings.Contains(joined[cursorStart:], "max(mention_invalidation_sequence) FROM event_page") {
		t.Fatal("durable cursor is not advanced from the raw outbox page")
	}
}

func TestMentionInvalidationExpansionIncludesTasksDependentOnParentEvents(t *testing.T) {
	tx := &fakeSalesTx{queryResult: &fakeRows{}}
	repository := NewMentionRepository(&fakeSalesDB{tx: tx})
	_, _ = repository.ClaimAccessInvalidations(context.Background(), 25, time.Now(), 5*time.Minute)
	joined := strings.Join(tx.queries, "\n")
	if !strings.Contains(joined, "mention_access_loss_marker_applies") {
		t.Fatalf("invalidation worker bypasses the shared object/dependent-task scope evaluator: %s", joined)
	}
}

func TestMentionInvalidationMarkerPagesRawMSPItemsBeforeScopeFiltering(t *testing.T) {
	tx := &fakeSalesTx{queryResult: &fakeRows{}}
	repository := NewMentionRepository(&fakeSalesDB{tx: tx})
	_, _ = repository.ClaimAccessInvalidations(context.Background(), 25, time.Now(), 5*time.Minute)
	joined := strings.Join(tx.queries, "\n")
	rawStart := strings.Index(joined, "raw_item_page AS MATERIALIZED")
	filteredStart := strings.Index(joined, "candidate_items AS MATERIALIZED")
	if rawStart < 0 || filteredStart <= rawStart {
		t.Fatalf("marker expansion does not page raw MSP items before scope filtering: %s", joined)
	}
	rawPage := joined[rawStart:filteredStart]
	if !strings.Contains(rawPage, "ORDER BY item.id") || !strings.Contains(rawPage, "LIMIT $1") {
		t.Fatalf("raw marker page is not physically bounded: %s", rawPage)
	}
	advanced := joined[filteredStart:]
	if !strings.Contains(advanced, "SELECT id FROM raw_item_page ORDER BY id DESC") || !strings.Contains(advanced, "count(*) FROM raw_item_page") {
		t.Fatalf("marker checkpoint does not advance from the raw page: %s", advanced)
	}
}

func TestMentionInvalidationSuppressesWithoutErasingStateAndCancelsPendingDelivery(t *testing.T) {
	tx := &fakeSalesTx{queryRows: []row{
		fakeRow{scan: func(destinations ...any) { *destinations[0].(*bool) = false }},
		fakeRow{scan: func(destinations ...any) {
			*destinations[0].(*int64) = 1
			*destinations[1].(*int64) = 2
		}},
	}}
	repository := NewMentionRepository(&fakeSalesDB{tx: tx})
	decision, err := repository.ProcessAccessInvalidation(context.Background(), mentions.AffectedItem{
		InvalidationID: "invalidation", LeaseToken: "lease", ID: "item-1",
	}, time.Now())
	if err != nil || !decision.ItemSuppressed || decision.DeliveriesCanceled != 2 {
		t.Fatalf("decision=%+v error=%v", decision, err)
	}
	joined := strings.Join(tx.calls, "\n")
	for _, fragment := range []string{
		"item.suppressed_at IS NULL", "snapshot_occurrence_id",
		"access_revoked", "state='pending'", "state='suppressed'",
		"mention_occurrence_id", "completed_at", "lease_token",
	} {
		if !strings.Contains(joined, fragment) {
			t.Errorf("invalidation completion SQL missing %q", fragment)
		}
	}
	if strings.Contains(joined, "state='unread'") || strings.Contains(joined, "suppressed_at=NULL") {
		t.Fatalf("invalidation overwrote recipient state or restored access: %s", joined)
	}
}

func TestRepeatedMentionInvalidationDoesNotCountAnAlreadySuppressedItemAgain(t *testing.T) {
	tx := &fakeSalesTx{queryRows: []row{
		fakeRow{scan: func(destinations ...any) { *destinations[0].(*bool) = false }},
		fakeRow{scan: func(destinations ...any) {
			*destinations[0].(*int64) = 0
			*destinations[1].(*int64) = 1
		}},
	}}
	repository := NewMentionRepository(&fakeSalesDB{tx: tx})
	decision, err := repository.ProcessAccessInvalidation(context.Background(), mentions.AffectedItem{
		InvalidationID: "invalidation", LeaseToken: "lease", ID: "item-1",
	}, time.Now())
	if err != nil || decision.ItemSuppressed || decision.DeliveriesCanceled != 1 {
		t.Fatalf("decision=%+v error=%v", decision, err)
	}
	if !strings.Contains(strings.Join(tx.calls, "\n"), "item.suppressed_at IS NULL") {
		t.Fatal("repeated invalidation can report an already-suppressed item as newly suppressed")
	}
}

func TestMentionMutationLocksTechnicianRoleAssignmentAndCapabilityEvidence(t *testing.T) {
	tx := &fakeSalesTx{queryResult: &fakeRows{}}
	unit := &collaborationTransaction{tx: tx}
	ref := mentions.SourceRef{MSPID: "msp", ClientID: "client", ParentType: mentions.ParentWorkRecord, ParentID: "work", SourceKind: mentions.SourceComment}
	_, _ = unit.LoadDirectAccess(context.Background(), ref, []string{"recipient"})
	joined := strings.Join(tx.calls, "\n")
	for _, fragment := range []string{
		"FROM technicians", "FROM role_assignments", "FROM roles", "FROM role_capabilities",
		"ORDER BY", "FOR SHARE",
	} {
		if !strings.Contains(joined, fragment) {
			t.Errorf("mention authorization evidence lock missing %q: %s", fragment, joined)
		}
	}
}

func TestMentionInvalidationAccessReturnCompletesWithoutAutoRestore(t *testing.T) {
	tx := &fakeSalesTx{queryRows: []row{
		fakeRow{scan: func(destinations ...any) { *destinations[0].(*bool) = true }},
		fakeRow{scan: func(destinations ...any) {
			*destinations[0].(*int64) = 0
			*destinations[1].(*int64) = 0
		}},
	}}
	repository := NewMentionRepository(&fakeSalesDB{tx: tx})
	decision, err := repository.ProcessAccessInvalidation(context.Background(), mentions.AffectedItem{
		InvalidationID: "invalidation", LeaseToken: "lease", ID: "item-1",
	}, time.Now())
	if err != nil || !decision.AccessAllowed || decision.ItemSuppressed {
		t.Fatalf("decision=%+v error=%v", decision, err)
	}
	joined := strings.Join(tx.calls, "\n")
	if strings.Contains(joined, "suppressed_at=NULL") || strings.Contains(joined, "suppression_reason=NULL") {
		t.Fatalf("access return auto-restored an old mention: %s", joined)
	}
}

func TestMentionReMentionClearsPriorAccessSuppression(t *testing.T) {
	accepted := collaboration.Mutation{
		Source:         collaboration.Source{ID: "source", MSPID: "msp", ClientID: "client", Parent: collaboration.ParentRef{Type: mentions.ParentWorkRecord, ID: "work"}, Kind: mentions.SourceComment, Body: "@Taylor", Tokens: []mentions.Token{{ID: "token", TargetType: mentions.TargetStaff, TargetID: "staff", Label: "@Taylor", Start: 0, End: 7}}, AuthorID: "author", LifecycleState: collaboration.SourceActive, Version: 2, UpdatedAt: time.Now()},
		Mentions:       mentions.PreparedMutation{Items: []mentions.Item{{ID: "item", MSPID: "msp", ClientID: "client", RecipientID: "staff", ParentType: mentions.ParentWorkRecord, ParentID: "work", LatestOccurrenceID: "occurrence", LastMentionedAt: time.Now()}}},
		IdempotencyKey: "idempotency",
	}
	tx := &fakeSalesTx{}
	unit := &collaborationTransaction{tx: tx, trusted: &mentions.SourceRef{MSPID: "msp", ClientID: "client", ParentType: mentions.ParentWorkRecord, ParentID: "work", SourceKind: mentions.SourceComment}}
	_ = unit.Save(context.Background(), accepted)
	joined := strings.Join(tx.queries, "\n")
	for _, fragment := range []string{"state = 'unread'", "suppressed_at = NULL", "suppression_reason = NULL"} {
		if !strings.Contains(joined, fragment) {
			t.Errorf("re-mention upsert missing %q", fragment)
		}
	}
}

func TestMentionReMentionMovesTheCollapsedItemToTheCurrentClient(t *testing.T) {
	accepted := collaboration.Mutation{
		Source:         collaboration.Source{ID: "source", MSPID: "msp", ClientID: "client-new", Parent: collaboration.ParentRef{Type: mentions.ParentWorkRecord, ID: "work"}, Kind: mentions.SourceComment, Body: "@Taylor", Tokens: []mentions.Token{{ID: "token", TargetType: mentions.TargetStaff, TargetID: "staff", Label: "@Taylor", Start: 0, End: 7}}, AuthorID: "author", LifecycleState: collaboration.SourceActive, Version: 2, UpdatedAt: time.Now()},
		Mentions:       mentions.PreparedMutation{Items: []mentions.Item{{ID: "item", MSPID: "msp", ClientID: "client-new", RecipientID: "staff", ParentType: mentions.ParentWorkRecord, ParentID: "work", LatestOccurrenceID: "occurrence-new", LastMentionedAt: time.Now()}}},
		IdempotencyKey: "idempotency",
	}
	tx := &fakeSalesTx{}
	unit := &collaborationTransaction{tx: tx, trusted: &mentions.SourceRef{MSPID: "msp", ClientID: "client-new", ParentType: mentions.ParentWorkRecord, ParentID: "work", SourceKind: mentions.SourceComment}}
	_ = unit.Save(context.Background(), accepted)
	joined := strings.Join(tx.queries, "\n")
	if !strings.Contains(joined, "client_id = EXCLUDED.client_id") {
		t.Fatalf("re-mention leaves collapsed item in its old Client: %s", joined)
	}
}

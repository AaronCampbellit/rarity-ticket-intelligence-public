package psa

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/collaboration"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mentions"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/observability"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

func TestCollaborationSavePersistsMentionProjectionAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	repository := NewCollaborationRepository(&fakeSalesDB{tx: tx})
	ref := mentions.SourceRef{MSPID: "msp", ClientID: "client", ParentType: mentions.ParentWorkRecord, ParentID: "work", SourceKind: mentions.SourceComment}
	err := repository.WithTransaction(context.Background(), ref, func(unit collaboration.Transaction) error {
		unit.(*collaborationTransaction).trusted = &ref
		return unit.Save(context.Background(), collaboration.Mutation{
			Source: collaboration.Source{ID: "source", MSPID: "msp", ClientID: "client", Parent: collaboration.ParentRef{Type: mentions.ParentWorkRecord, ID: "work"}, Kind: mentions.SourceComment, Body: "hello @Taylor", Tokens: []mentions.Token{{ID: "token", TargetType: mentions.TargetStaff, TargetID: "staff", Label: "Taylor", Start: 6, End: 13}}, AuthorID: "author", LifecycleState: collaboration.SourceActive, Version: 1, CreatedAt: at, UpdatedAt: at},
			Mentions: mentions.PreparedMutation{
				Occurrences: []mentions.Occurrence{{ID: "occurrence", MSPID: "msp", ClientID: "client", SourceID: "source", SourceRevision: 1, ParentType: mentions.ParentWorkRecord, ParentID: "work", TokenID: "token", AuthorID: "author", TargetType: mentions.TargetStaff, TargetID: "staff", MentionedAt: at, CorrelationID: "correlation"}},
				Resolutions: []mentions.ResolutionRecord{{ID: "resolution", MSPID: "msp", ClientID: "client", OccurrenceID: "occurrence", RecipientID: "staff", Decision: mentions.DecisionEligible, Path: mentions.ResolutionDirect, DecidedAt: at, ReasonCode: "eligible"}},
				Items:       []mentions.Item{{ID: "item", MSPID: "msp", ClientID: "client", RecipientID: "staff", ParentType: mentions.ParentWorkRecord, ParentID: "work", LatestOccurrenceID: "occurrence", State: mentions.Unread, LastMentionedAt: at, Version: 1}},
				Audits:      []mutation.AuditRecord{{ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client", ActorType: "technician", ActorID: "author", Action: "mention.occurred", SubjectType: "mention_occurrence", SubjectID: "occurrence", SubjectVersion: 1, Source: "api", CorrelationID: "correlation"}},
				Event:       &mutation.EventRecord{EventID: "event", EventType: "mention.occurred", SchemaVersion: 1, OccurredAt: at, MSPID: "msp", ClientID: "client", ActorType: "technician", ActorID: "author", SubjectType: "internal_collaboration_source", SubjectID: "source", SubjectVersion: 1, Source: "api", CorrelationID: "correlation"},
			}, IdempotencyKey: "request-key",
		})
	})
	if err != nil {
		t.Fatalf("WithTransaction() error=%v", err)
	}
	joined := strings.Join(tx.queries, "\n")
	for _, fragment := range []string{"lock_mention_authorization_revision", "INSERT INTO internal_collaboration_sources", "INSERT INTO mention_occurrences", "authorization_revision", "INSERT INTO mention_recipient_resolutions", "ON CONFLICT (msp_id, recipient_id, parent_type, parent_id)", "state = 'unread'", "INSERT INTO audit_ledger", "INSERT INTO event_outbox"} {
		if !strings.Contains(joined, fragment) {
			t.Errorf("atomic mention query missing %q", fragment)
		}
	}
	if !tx.committed {
		t.Fatal("transaction not committed")
	}
}

func TestMentionItemUpsertEmitsReMentionOnlyForExistingProjection(t *testing.T) {
	telemetry := observability.NewMentionTelemetry(nil)
	tx := &fakeSalesTx{queryRow: fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*int64) = 2
	}}}
	at := time.Date(2026, time.August, 8, 18, 0, 0, 0, time.UTC)
	ref := mentions.SourceRef{MSPID: "msp", ClientID: "client", ParentType: mentions.ParentProject, ParentID: "project", SourceKind: mentions.SourceNote}
	repository := NewCollaborationRepository(&fakeSalesDB{tx: tx}).WithTelemetry(telemetry)
	err := repository.WithTransaction(context.Background(), ref, func(transaction collaboration.Transaction) error {
		unit := transaction.(*collaborationTransaction)
		unit.trusted = &ref
		return unit.Save(context.Background(), collaboration.Mutation{
			Source:         collaboration.Source{ID: "source", MSPID: "msp", ClientID: "client", Parent: collaboration.ParentRef{Type: mentions.ParentProject, ID: "project"}, Kind: mentions.SourceNote, Body: "@Taylor", Tokens: []mentions.Token{{ID: "token", TargetType: mentions.TargetStaff, TargetID: "staff", Label: "@Taylor", Start: 0, End: 7}}, AuthorID: "author", LifecycleState: collaboration.SourceActive, Version: 1, CreatedAt: at, UpdatedAt: at},
			Mentions:       mentions.PreparedMutation{Items: []mentions.Item{{ID: "item", MSPID: "msp", ClientID: "client", RecipientID: "staff", ParentType: mentions.ParentProject, ParentID: "project", LatestOccurrenceID: "occurrence", LastMentionedAt: at}}},
			IdempotencyKey: "request",
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if telemetry.Value("remention", "project", "unread_again") != 1 {
		t.Fatal("existing mention item did not emit re-mention counter")
	}
}

func TestCollaborationTelemetryIsSilentWhenTheTransactionDoesNotCommit(t *testing.T) {
	for _, test := range []struct {
		name        string
		commitErr   error
		callbackErr error
	}{
		{name: "commit failure", commitErr: context.Canceled},
		{name: "callback rollback", callbackErr: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			telemetry := observability.NewMentionTelemetry(nil)
			tx := &fakeSalesTx{commitErr: test.commitErr}
			ref := mentions.SourceRef{MSPID: "msp", ClientID: "client", ParentType: mentions.ParentProject, ParentID: "project", SourceKind: mentions.SourceNote}
			repository := NewCollaborationRepository(&fakeSalesDB{tx: tx}).WithTelemetry(telemetry)
			err := repository.WithTransaction(context.Background(), ref, func(transaction collaboration.Transaction) error {
				unit := transaction.(*collaborationTransaction)
				unit.trusted = &ref
				if saveErr := unit.Save(context.Background(), collaboration.Mutation{
					Source:   collaboration.Source{ID: "source", MSPID: "msp", ClientID: "client", Parent: collaboration.ParentRef{Type: mentions.ParentProject, ID: "project"}, Kind: mentions.SourceNote, Body: "@Taylor", Tokens: []mentions.Token{}, AuthorID: "author", LifecycleState: collaboration.SourceActive, Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()},
					Mentions: mentions.PreparedMutation{Telemetry: []observability.MentionMetric{{Name: "occurrence_target", ParentType: "project", Outcome: "staff"}}}, IdempotencyKey: "request",
				}); saveErr != nil {
					return saveErr
				}
				return test.callbackErr
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("transaction error=%v", err)
			}
			if got := telemetry.Value("occurrence_target", "project", "staff"); got != 0 {
				t.Fatalf("non-committed transaction emitted telemetry=%d", got)
			}
		})
	}
}

func TestMentionPersistenceWidgetIdempotencyAndRevocationAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for internal mention PostgreSQL verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	mspID, clientID, authorID, recipientID := id.New(), id.New(), id.New(), id.New()
	outsiderID, roleID, authorAssignment, recipientAssignment, workID, teamID := id.New(), id.New(), id.New(), id.New(), id.New(), id.New()
	pipelineID, stageID, opportunityID, proposalID, proposalVersionID := id.New(), id.New(), id.New(), id.New(), id.New()
	projectID, taskID := id.New(), id.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, execErr := pool.Exec(ctx, query, args...); execErr != nil {
			t.Fatalf("fixture: %v", execErr)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'mentions',$3,$3)`, mspID, "MENTION-"+mspID, authorID)
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$2,$3,'client',$4,$4)`, clientID, mspID, "CLIENT-"+clientID, authorID)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$4,$5,'Author'),($2,$4,$6,'Recipient'),($3,$4,$7,'No Access')`, authorID, recipientID, outsiderID, mspID, authorID+"@example.test", recipientID+"@example.test", outsiderID+"@example.test")
	exec(`INSERT INTO roles(id,msp_id,key,name) VALUES($1,$2,$3,'Mention access')`, roleID, mspID, "mention-"+roleID)
	exec(`INSERT INTO role_capabilities(role_id,msp_id,capability) VALUES($1,$2,'mention.create'),($1,$2,'mention.read'),($1,$2,'work_record.read'),($1,$2,'work_record.edit'),($1,$2,'project.read'),($1,$2,'project.edit')`, roleID, mspID)
	exec(`INSERT INTO role_assignments(id,msp_id,client_id,technician_id,role_id,granted_by) VALUES($1,$2,$3,$4,$6,$4),($5,$2,$3,$7,$6,$4)`, authorAssignment, mspID, clientID, authorID, recipientAssignment, roleID, recipientID)
	exec(`INSERT INTO work_records(id,msp_id,client_id,display_id,record_type,title,status,priority,created_by,updated_by) VALUES($1,$2,$3,$4,'incident','VPN outage','new','normal',$5,$5)`, workID, mspID, clientID, "TICKET-"+workID, authorID)
	exec(`INSERT INTO pipelines(id,msp_id,key,name,created_by,updated_by) VALUES($1,$2,$3,'mentions',$4,$4)`, pipelineID, mspID, "mentions-"+pipelineID, authorID)
	exec(`INSERT INTO pipeline_stages(id,pipeline_id,msp_id,key,name,position,probability,forecast_category) VALUES($1,$2,$3,'open','Open',1,10,'weighted')`, stageID, pipelineID, mspID)
	exec(`INSERT INTO opportunities(id,msp_id,client_id,pipeline_id,stage_id,display_id,name,currency,created_by,updated_by) VALUES($1,$2,$3,$4,$5,$6,'mentions','USD',$7,$7)`, opportunityID, mspID, clientID, pipelineID, stageID, "OPP-"+opportunityID, authorID)
	exec(`INSERT INTO proposals(id,msp_id,client_id,opportunity_id,display_id,current_version,state,created_by,updated_by) VALUES($1,$2,$3,$4,$5,1,'draft',$6,$6)`, proposalID, mspID, clientID, opportunityID, "PROP-"+proposalID, authorID)
	exec(`INSERT INTO proposal_versions(id,proposal_id,msp_id,version,currency,subtotal_minor,tax_minor,total_minor,cost_minor,margin_minor,issued_by,pdf_snapshot_id) VALUES($1,$2,$3,1,'USD',0,0,0,0,0,$4,$5)`, proposalVersionID, proposalID, mspID, authorID, id.New())
	exec(`INSERT INTO projects(id,msp_id,client_id,display_id,name,original_proposal_version_id,created_by,updated_by) VALUES($1,$2,$3,$4,'Mentions',$5,$6,$6)`, projectID, mspID, clientID, "PROJECT-"+projectID, proposalVersionID, authorID)
	exec(`INSERT INTO tasks(id,msp_id,client_id,parent_type,parent_id,work_record_id,title,status,position,created_by,updated_by) VALUES($1,$2,$3,'work_record',$4,$4,'Mention task','new',1,$5,$5)`, taskID, mspID, clientID, workID, authorID)
	exec(`INSERT INTO teams(id,msp_id,key,name) VALUES($1,$2,$3,'NOC')`, teamID, mspID, "noc-"+teamID)
	exec(`INSERT INTO team_memberships(team_id,technician_id,msp_id,created_at,created_by,updated_at,updated_by) VALUES($1,$2,$3,now(),$4,now(),$4),($1,$5,$3,now(),$4,now(),$4)`, teamID, recipientID, mspID, authorID, outsiderID)

	repository := NewCollaborationRepositoryFromPool(pool)
	for _, parent := range []collaboration.ParentRef{{Type: mentions.ParentWorkRecord, ID: workID}, {Type: mentions.ParentProject, ID: projectID}, {Type: mentions.ParentTask, ID: taskID}} {
		for _, requestedClient := range []string{"", clientID} {
			ref := mentions.SourceRef{MSPID: mspID, ClientID: requestedClient, ParentType: parent.Type, ParentID: parent.ID, SourceKind: mentions.SourceComment}
			err = repository.WithTransaction(ctx, ref, func(tx collaboration.Transaction) error {
				loaded, loadErr := tx.LoadSourceForUpdate(ctx, ref, "")
				if loadErr == nil && loaded.ClientID != clientID {
					t.Fatalf("%s hydrated Client=%q, want %q", parent.Type, loaded.ClientID, clientID)
				}
				return loadErr
			})
			if err != nil {
				t.Fatalf("%s Client %q hydration: %v", parent.Type, requestedClient, err)
			}
		}
		wrong := mentions.SourceRef{MSPID: mspID, ClientID: id.New(), ParentType: parent.Type, ParentID: parent.ID, SourceKind: mentions.SourceComment}
		err = repository.WithTransaction(ctx, wrong, func(tx collaboration.Transaction) error {
			_, loadErr := tx.LoadSourceForUpdate(ctx, wrong, "")
			return loadErr
		})
		if !errors.Is(err, scope.ErrNotFound) {
			t.Fatalf("%s wrong Client error=%v", parent.Type, err)
		}
	}

	now := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	principal := authorization.Principal{ID: authorID, Scope: scope.Principal{MSPID: mspID}}
	service := collaboration.NewService(repository, mentions.NewService(func() time.Time { return now }, id.New), func() time.Time { return now }, id.New)
	for _, parent := range []collaboration.ParentRef{{Type: mentions.ParentProject, ID: projectID}, {Type: mentions.ParentTask, ID: taskID}} {
		if _, err = service.CreateNote(ctx, collaboration.CreateCommand{Principal: principal, Parent: parent, Body: "Internal global-author note", IdempotencyKey: id.New(), Source: "integration"}); err != nil {
			t.Fatalf("MSP-global authorized %s note: %v", parent.Type, err)
		}
	}
	command := collaboration.CreateCommand{Principal: principal, Parent: collaboration.ParentRef{Type: mentions.ParentWorkRecord, ID: workID}, Body: "@Recipient investigate the VPN", Tokens: []mentions.Token{{ID: id.New(), TargetType: mentions.TargetStaff, TargetID: recipientID, Label: "@Recipient", Start: 0, End: 10}}, IdempotencyKey: id.New(), Source: "integration"}
	created, err := service.CreateComment(ctx, command)
	if err != nil {
		t.Fatalf("create mention: %v", err)
	}
	replayed, err := service.CreateComment(ctx, command)
	if err != nil || replayed.ID != created.ID {
		t.Fatalf("idempotent replay=%+v err=%v", replayed, err)
	}
	firstEdit := collaboration.UpsertCommand{Principal: principal, Parent: command.Parent, SourceID: created.ID, Kind: mentions.SourceComment, Body: "@Recipient later VPN context", Tokens: append([]mentions.Token(nil), command.Tokens...), ExpectedVersion: 1, IdempotencyKey: id.New(), Source: "integration"}
	edited, err := service.Edit(ctx, firstEdit)
	if err != nil || edited.Version != 2 {
		t.Fatalf("first edit=%+v err=%v", edited, err)
	}
	if replayed, replayErr := service.CreateComment(ctx, command); !errors.Is(replayErr, object.ErrVersionConflict) || replayed.Body != "" {
		t.Fatalf("delayed create replay leaked=%+v err=%v", replayed, replayErr)
	}
	secondEdit := firstEdit
	secondEdit.Body, secondEdit.ExpectedVersion, secondEdit.IdempotencyKey = "@Recipient newest VPN context", 2, id.New()
	if _, err = service.Edit(ctx, secondEdit); err != nil {
		t.Fatalf("second edit: %v", err)
	}
	if replayed, replayErr := service.Edit(ctx, firstEdit); !errors.Is(replayErr, object.ErrVersionConflict) || replayed.Body != "" {
		t.Fatalf("delayed edit replay leaked=%+v err=%v", replayed, replayErr)
	}
	var occurrences int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM mention_occurrences WHERE source_id=$1`, created.ID).Scan(&occurrences); err != nil || occurrences != 1 {
		t.Fatalf("occurrences=%d err=%v", occurrences, err)
	}

	queries := mentions.NewQueryService(NewMentionRepositoryFromPool(pool), []byte("integration cursor signing key 32 bytes minimum"), func() time.Time { return now })
	teamCandidates, err := queries.ListCandidates(ctx, mentions.CandidateQuery{
		AuthorID: authorID,
		Source:   mentions.SourceRef{MSPID: mspID, ClientID: clientID, ParentType: mentions.ParentWorkRecord, ParentID: workID, SourceKind: mentions.SourceComment},
		Search:   "NOC",
	})
	if err != nil || len(teamCandidates) != 1 || teamCandidates[0].TargetType != mentions.TargetTeam ||
		teamCandidates[0].EligibleCount != 1 || teamCandidates[0].ExcludedCount != 1 ||
		len(teamCandidates[0].EligibleMemberIDs) != 1 || teamCandidates[0].EligibleMemberIDs[0] != recipientID {
		t.Fatalf("permission-safe team candidate snapshot=%+v err=%v", teamCandidates, err)
	}
	exec(`UPDATE teams SET name='Network Operations' WHERE id=$1 AND msp_id=$2`, teamID, mspID)
	exactTeam, err := queries.ListCandidates(ctx, mentions.CandidateQuery{
		AuthorID:        authorID,
		Source:          mentions.SourceRef{MSPID: mspID, ClientID: clientID, ParentType: mentions.ParentWorkRecord, ParentID: workID, SourceKind: mentions.SourceComment},
		ExactTargetType: mentions.TargetTeam,
		ExactTargetID:   teamID,
	})
	if err != nil || len(exactTeam) != 1 || exactTeam[0].ID != teamID || exactTeam[0].Label != "Network Operations" || exactTeam[0].EligibleCount != 1 {
		t.Fatalf("renamed exact Team=%+v err=%v", exactTeam, err)
	}
	recipient := authorization.Principal{ID: recipientID, Scope: scope.Principal{MSPID: mspID}}
	page, err := queries.ListWidget(ctx, mentions.WidgetQuery{Principal: recipient, State: mentions.Unread, Limit: 50})
	if err != nil || len(page.Items) != 1 || page.Counts.Unread != 1 || !strings.Contains(page.Items[0].Preview, "VPN") {
		t.Fatalf("widget=%+v err=%v", page, err)
	}
	item := page.Items[0]
	changed, err := queries.ChangeState(ctx, mentions.StateChange{Principal: recipient, ItemID: item.ID, State: mentions.Archived, ExpectedVersion: item.Version})
	if err != nil || changed.State != mentions.Archived || changed.Version != item.Version+1 {
		t.Fatalf("state change=%+v err=%v", changed, err)
	}
	if _, staleErr := queries.ChangeState(ctx, mentions.StateChange{Principal: recipient, ItemID: item.ID, State: mentions.Read, ExpectedVersion: item.Version}); !errors.Is(staleErr, object.ErrVersionConflict) {
		t.Fatalf("stale state error=%v", staleErr)
	}
	link, err := queries.ResolveDeepLink(ctx, mentions.DeepLinkQuery{Principal: recipient, ItemID: item.ID, OccurrenceID: item.LatestOccurrenceID, ExpectedVersion: changed.Version})
	if err != nil || !link.SourceAvailable || link.ItemVersion != changed.Version+1 {
		t.Fatalf("deep link=%+v err=%v", link, err)
	}

	second := command
	second.IdempotencyKey, second.Tokens[0].ID = id.New(), id.New()
	createdSecond, err := service.CreateComment(ctx, second)
	if err != nil || createdSecond.ID == created.ID {
		t.Fatalf("re-mention source=%+v err=%v", createdSecond, err)
	}
	page, err = queries.ListWidget(ctx, mentions.WidgetQuery{Principal: recipient, State: mentions.Unread, Limit: 50})
	if err != nil || len(page.Items) != 1 || page.Items[0].Version != link.ItemVersion+1 {
		t.Fatalf("re-mention widget=%+v err=%v", page, err)
	}
	delayedRedact := collaboration.RedactCommand{Principal: principal, Parent: command.Parent, SourceID: created.ID, Kind: mentions.SourceComment, ExpectedVersion: 3, IdempotencyKey: id.New(), Source: "integration"}
	if _, err = service.Redact(ctx, delayedRedact); err != nil {
		t.Fatalf("redact edited source: %v", err)
	}
	if replayed, replayErr := service.Edit(ctx, secondEdit); !errors.Is(replayErr, collaboration.ErrSourceRedacted) || replayed.Body != "" {
		t.Fatalf("pre-redaction edit claim leaked=%+v err=%v", replayed, replayErr)
	}
	if replayed, replayErr := service.CreateComment(ctx, command); !errors.Is(replayErr, object.ErrVersionConflict) || replayed.Body != "" {
		t.Fatalf("pre-redaction create claim leaked=%+v err=%v", replayed, replayErr)
	}
	var unsafeClaimData bool
	if err = pool.QueryRow(ctx, `SELECT COALESCE(bool_or(data ? 'body' OR data ? 'tokens' OR data ? 'mention_tokens'),false) FROM event_outbox WHERE subject_id=$1`, created.ID).Scan(&unsafeClaimData); err != nil || unsafeClaimData {
		t.Fatalf("idempotency claim contains source content=%v err=%v", unsafeClaimData, err)
	}

	teamCommand := command
	teamCommand.Body = "@NOC investigate the VPN"
	teamCommand.Tokens = []mentions.Token{{ID: id.New(), TargetType: mentions.TargetTeam, TargetID: teamID, Label: "@NOC", Start: 0, End: 4}}
	teamCommand.IdempotencyKey = id.New()
	if _, err = service.CreateComment(ctx, teamCommand); !errors.Is(err, mentions.ErrTeamConfirmationRequired) {
		t.Fatalf("partial team without confirmation error=%v", err)
	}
	teamCommand.ConfirmedTeamSnapshots = map[string]mentions.TeamConfirmation{teamID: {TeamVersion: 1, EligibleMemberIDs: []string{recipientID}}}
	lockReady, releaseLock, lockDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	ref := mentions.SourceRef{MSPID: mspID, ClientID: clientID, ParentType: mentions.ParentWorkRecord, ParentID: workID, SourceKind: mentions.SourceComment}
	go func() {
		lockDone <- repository.WithTransaction(ctx, ref, func(tx collaboration.Transaction) error {
			if _, lockErr := tx.LoadTeamAccess(ctx, ref, []string{teamID}); lockErr != nil {
				return lockErr
			}
			close(lockReady)
			<-releaseLock
			return nil
		})
	}()
	<-lockReady
	membershipWrite := make(chan error, 1)
	go func() {
		_, updateErr := pool.Exec(ctx, `UPDATE team_memberships SET lifecycle_state='inactive',version=version+1,updated_at=now(),updated_by=$3 WHERE team_id=$1 AND technician_id=$2`, teamID, outsiderID, authorID)
		membershipWrite <- updateErr
	}()
	select {
	case updateErr := <-membershipWrite:
		close(releaseLock)
		<-lockDone
		t.Fatalf("membership replacement bypassed mention evidence lock: %v", updateErr)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseLock)
	if lockErr := <-lockDone; lockErr != nil {
		t.Fatalf("release mention evidence lock: %v", lockErr)
	}
	if updateErr := <-membershipWrite; updateErr != nil {
		t.Fatalf("membership replacement after release: %v", updateErr)
	}
	exec(`UPDATE team_memberships SET lifecycle_state='active',version=version+1,updated_at=now(),updated_by=$3 WHERE team_id=$1 AND technician_id=$2`, teamID, outsiderID, authorID)
	if _, err = service.CreateComment(ctx, teamCommand); err != nil {
		t.Fatalf("confirmed partial team mention: %v", err)
	}
	var excluded int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM mention_recipient_resolutions resolution JOIN mention_occurrences occurrence ON occurrence.id=resolution.occurrence_id WHERE occurrence.target_id=$1 AND resolution.recipient_id=$2 AND resolution.decision='excluded'`, teamID, outsiderID).Scan(&excluded); err != nil || excluded != 1 {
		t.Fatalf("partial team exclusion=%d err=%v", excluded, err)
	}

	concurrent := command
	concurrent.IdempotencyKey = id.New()
	concurrent.Tokens = []mentions.Token{{ID: id.New(), TargetType: mentions.TargetStaff, TargetID: recipientID, Label: "@Recipient", Start: 0, End: 10}}
	type createResult struct {
		source collaboration.Source
		err    error
	}
	results := make(chan createResult, 2)
	for range 2 {
		go func() {
			source, createErr := service.CreateComment(ctx, concurrent)
			results <- createResult{source: source, err: createErr}
		}()
	}
	first, secondResult := <-results, <-results
	if first.err != nil || secondResult.err != nil || first.source.ID != secondResult.source.ID {
		t.Fatalf("concurrent replay first=%+v second=%+v", first, secondResult)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM mention_occurrences WHERE source_id=$1`, first.source.ID).Scan(&occurrences); err != nil || occurrences != 1 {
		t.Fatalf("concurrent occurrences=%d err=%v", occurrences, err)
	}

	page, err = queries.ListWidget(ctx, mentions.WidgetQuery{Principal: recipient, State: mentions.Unread, Limit: 50})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("post-concurrency widget=%+v err=%v", page, err)
	}
	latest := page.Items[0]
	redactCommand := collaboration.RedactCommand{Principal: principal, Parent: collaboration.ParentRef{Type: mentions.ParentWorkRecord, ID: workID}, SourceID: first.source.ID, Kind: mentions.SourceComment, ExpectedVersion: 1, IdempotencyKey: id.New(), Source: "integration"}
	if _, err = service.Redact(ctx, redactCommand); err != nil {
		t.Fatalf("redact latest source: %v", err)
	}
	if replayedRedaction, replayErr := service.Redact(ctx, redactCommand); replayErr != nil || replayedRedaction.LifecycleState != collaboration.SourceRedacted {
		t.Fatalf("exact redaction replay=%+v err=%v", replayedRedaction, replayErr)
	}
	preview, err := queries.LoadPreview(ctx, recipient, latest.ID)
	if !errors.Is(err, scope.ErrNotFound) || preview != "" {
		t.Fatalf("redacted preview=%q err=%v", preview, err)
	}
	if unavailable, resolveErr := queries.ResolveDeepLink(ctx, mentions.DeepLinkQuery{Principal: recipient, ItemID: latest.ID, OccurrenceID: latest.LatestOccurrenceID, ExpectedVersion: latest.Version}); !errors.Is(resolveErr, scope.ErrNotFound) {
		t.Fatalf("redacted deep link=%+v err=%v", unavailable, resolveErr)
	}
	itemVersion := latest.Version
	otherMSP := authorization.Principal{ID: recipientID, Scope: scope.Principal{MSPID: id.New()}}
	if _, err = queries.LoadPreview(ctx, otherMSP, latest.ID); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-MSP preview error=%v", err)
	}
	if _, err = queries.ChangeState(ctx, mentions.StateChange{Principal: authorization.Principal{ID: outsiderID, Scope: scope.Principal{MSPID: mspID}}, ItemID: latest.ID, State: mentions.Read, ExpectedVersion: itemVersion}); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-recipient state error=%v", err)
	}
	if _, err = queries.ChangeState(ctx, mentions.StateChange{Principal: otherMSP, ItemID: latest.ID, State: mentions.Read, ExpectedVersion: itemVersion}); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-MSP state error=%v", err)
	}
	exec(`UPDATE mention_items SET suppressed_at=now(),suppression_reason='test' WHERE id=$1`, latest.ID)
	if _, err = queries.ChangeState(ctx, mentions.StateChange{Principal: recipient, ItemID: latest.ID, State: mentions.Unread, ExpectedVersion: itemVersion}); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("suppressed state error=%v", err)
	}
	exec(`UPDATE mention_items SET suppressed_at=NULL,suppression_reason=NULL WHERE id=$1`, latest.ID)
	if replayedRedaction, replayErr := service.Redact(ctx, delayedRedact); replayErr != nil || replayedRedaction.Version != 4 || replayedRedaction.LifecycleState != collaboration.SourceRedacted {
		t.Fatalf("delayed redaction replay=%+v err=%v", replayedRedaction, replayErr)
	}

	invalidationWorker := mentions.NewInvalidationWorker(NewMentionRepositoryFromPool(pool), time.Now)
	drainInvalidations := func() mentions.InvalidationResult {
		t.Helper()
		var total mentions.InvalidationResult
		for range 512 {
			result, runErr := invalidationWorker.RunOnce(ctx, 50)
			if runErr != nil {
				t.Fatalf("drain invalidations: %v", runErr)
			}
			total.Claimed += result.Claimed
			total.Completed += result.Completed
			total.Suppressed += result.Suppressed
			total.DeliveriesCanceled += result.DeliveriesCanceled
			var pending int
			if queryErr := pool.QueryRow(ctx, `SELECT count(*) FROM mention_invalidation_event_claims WHERE completed_at IS NULL`).Scan(&pending); queryErr != nil {
				t.Fatalf("count pending invalidation events: %v", queryErr)
			}
			if pending == 0 {
				return total
			}
		}
		t.Fatal("invalidation event claims did not drain within the bounded test budget")
		return total
	}
	// Redaction is a definitive loss boundary. Drain it, then create a genuine
	// later mention so the role-revocation scenario has a fresh snapshot.
	drainInvalidations()
	postRedaction := command
	postRedaction.IdempotencyKey = id.New()
	postRedaction.Tokens = []mentions.Token{{ID: id.New(), TargetType: mentions.TargetStaff, TargetID: recipientID, Label: "@Recipient", Start: 0, End: 10}}
	if _, err = service.CreateComment(ctx, postRedaction); err != nil {
		t.Fatalf("create post-redaction mention: %v", err)
	}
	page, err = queries.ListWidget(ctx, mentions.WidgetQuery{Principal: recipient, State: mentions.Unread, Limit: 50})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("post-redaction widget=%+v err=%v", page, err)
	}
	latest = page.Items[0]
	// Seed lower-sorting nonmatching rows. The recipient-scoped marker must
	// advance through these in a raw bounded page instead of scanning past them
	// to find the first matching item in one transaction.
	for _, decoyItemID := range []string{
		"00000000-0000-0000-0000-000000000111",
		"00000000-0000-0000-0000-000000000112",
		"00000000-0000-0000-0000-000000000113",
	} {
		exec(`INSERT INTO mention_items(id,msp_id,client_id,recipient_id,parent_type,parent_id,latest_occurrence_id,authorization_revision,state,last_mentioned_at) VALUES($1,$2,$3,$4,'work_record',$5,$6,(SELECT authorization_revision FROM mention_items WHERE id=$7),'unread',now())`, decoyItemID, mspID, clientID, outsiderID, id.New(), latest.LatestOccurrenceID, latest.ID)
	}

	revokedEventID := id.New()
	revocationTx, beginErr := pool.Begin(ctx)
	if beginErr != nil {
		t.Fatalf("begin concurrent revocation: %v", beginErr)
	}
	if _, err = revocationTx.Exec(ctx, `SELECT begin_mention_authorization_revision($1::uuid)`, mspID); err != nil {
		_ = revocationTx.Rollback(ctx)
		t.Fatalf("lock authorization revision in revocation transaction: %v", err)
	}
	if _, err = revocationTx.Exec(ctx, `DELETE FROM role_assignments WHERE id=$1`, recipientAssignment); err != nil {
		_ = revocationTx.Rollback(ctx)
		t.Fatalf("delete recipient assignment in revocation transaction: %v", err)
	}
	// Deliberately predate the mention. Event wall clocks are not a valid
	// serialization boundary; commit order and the transaction-local snapshot are.
	if _, err = revocationTx.Exec(ctx, `INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source) VALUES($1,'role.unassigned',1,$2,$3,$6,'technician',$4,'role_assignment',$5,2,$1,'integration')`, revokedEventID, now.Add(-24*time.Hour), mspID, authorID, recipientAssignment, clientID); err != nil {
		_ = revocationTx.Rollback(ctx)
		t.Fatalf("insert transactional revocation event: %v", err)
	}
	concurrentMention := command
	concurrentMention.IdempotencyKey = id.New()
	concurrentMention.Tokens = []mentions.Token{{ID: id.New(), TargetType: mentions.TargetStaff, TargetID: recipientID, Label: "@Recipient", Start: 0, End: 10}}
	concurrentMentionResult := make(chan error, 1)
	go func() {
		_, createErr := service.CreateComment(ctx, concurrentMention)
		concurrentMentionResult <- createErr
	}()
	select {
	case createErr := <-concurrentMentionResult:
		_ = revocationTx.Rollback(ctx)
		t.Fatalf("mention creation bypassed uncommitted authorization change: %v", createErr)
	case <-time.After(100 * time.Millisecond):
	}
	if err = revocationTx.Commit(ctx); err != nil {
		t.Fatalf("commit concurrent revocation: %v", err)
	}
	if createErr := <-concurrentMentionResult; !errors.Is(createErr, mentions.ErrDirectTargetIneligible) {
		t.Fatalf("mention creation after winning revocation error=%v", createErr)
	}
	var compactMarkers, prematureInvalidations int
	var markerAfterSnapshot bool
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM mention_access_loss_markers WHERE event_id=$1),(SELECT count(*) FROM mention_access_invalidations WHERE causation_id=$1),marker.boundary_revision>=item.authorization_revision FROM mention_access_loss_markers marker JOIN mention_items item ON item.id=$2 WHERE marker.event_id=$1`, revokedEventID, latest.ID).Scan(&compactMarkers, &prematureInvalidations, &markerAfterSnapshot); err != nil {
		t.Fatalf("load compact revoke marker: %v", err)
	}
	if compactMarkers != 1 || prematureInvalidations != 0 || !markerAfterSnapshot {
		t.Fatalf("compact revoke marker count=%d premature invalidations=%d afterSnapshot=%v", compactMarkers, prematureInvalidations, markerAfterSnapshot)
	}
	firstSparsePage, sparseErr := invalidationWorker.RunOnce(ctx, 2)
	if sparseErr != nil || firstSparsePage.Claimed != 0 {
		t.Fatalf("first sparse marker page=%+v err=%v", firstSparsePage, sparseErr)
	}
	var sparseAdvanced, sparsePending bool
	if err = pool.QueryRow(ctx, `SELECT last_item_id IS NOT NULL,completed_at IS NULL FROM mention_access_loss_markers WHERE event_id=$1`, revokedEventID).Scan(&sparseAdvanced, &sparsePending); err != nil || !sparseAdvanced || !sparsePending {
		t.Fatalf("sparse marker checkpoint advanced=%v pending=%v err=%v", sparseAdvanced, sparsePending, err)
	}
	if _, err = queries.ChangeState(ctx, mentions.StateChange{Principal: recipient, ItemID: latest.ID, State: mentions.Unread, ExpectedVersion: latest.Version - 1}); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("revoked stale state disclosed version: %v", err)
	}
	page, err = queries.ListWidget(ctx, mentions.WidgetQuery{Principal: recipient, State: mentions.Read, Limit: 50})
	if err != nil || len(page.Items) != 0 || page.Counts.Read != 0 {
		t.Fatalf("revoked access leaked widget=%+v err=%v", page, err)
	}
	// Restore access before the worker runs. The decision captured in the revoke
	// transaction must still hide and suppress the old occurrence.
	exec(`INSERT INTO role_assignments(id,msp_id,client_id,technician_id,role_id,granted_by) VALUES($1,$2,$3,$4,$5,$6)`, recipientAssignment, mspID, clientID, recipientID, roleID, authorID)
	restoredEventID := id.New()
	exec(`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source) VALUES($1,'role.assigned',1,now(),$2,$3,'technician',$4,'role_assignment',$5,1,$1,'integration')`, restoredEventID, mspID, clientID, authorID, recipientAssignment)
	page, err = queries.ListWidget(ctx, mentions.WidgetQuery{Principal: recipient, State: mentions.Read, Limit: 50})
	if err != nil || len(page.Items) != 0 || page.Counts.Read != 0 {
		t.Fatalf("confirmed loss leaked after pre-worker regrant widget=%+v err=%v", page, err)
	}
	invalidated := drainInvalidations()
	if invalidated.Suppressed < 1 {
		t.Fatalf("transactionally confirmed revocation after regrant=%+v", invalidated)
	}
	var suppressed bool
	if err = pool.QueryRow(ctx, `SELECT suppressed_at IS NOT NULL FROM mention_items WHERE id=$1`, latest.ID).Scan(&suppressed); err != nil || !suppressed {
		t.Fatalf("access restoration revived old item=%v err=%v", suppressed, err)
	}
	newMention := command
	newMention.IdempotencyKey = id.New()
	newMention.Tokens = []mentions.Token{{ID: id.New(), TargetType: mentions.TargetStaff, TargetID: recipientID, Label: "@Recipient", Start: 0, End: 10}}
	if _, err = service.CreateComment(ctx, newMention); err != nil {
		t.Fatalf("new mention after access restoration: %v", err)
	}
	if err = pool.QueryRow(ctx, `SELECT suppressed_at IS NULL AND state='unread' FROM mention_items WHERE id=$1`, latest.ID).Scan(&suppressed); err != nil || !suppressed {
		t.Fatalf("new mention did not clear suppression=%v err=%v", suppressed, err)
	}

	// A cross-Client transfer retains the collapsed recipient/object identity.
	// The transfer invalidates the old projection, while a later authorized
	// mention moves that same item to the current Client and returns it unread.
	transferredWorkID, nextClientID := id.New(), id.New()
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$2,$3,'next client',$4,$4)`, nextClientID, mspID, "CLIENT-"+nextClientID, authorID)
	exec(`INSERT INTO work_records(id,msp_id,client_id,display_id,record_type,title,status,priority,created_by,updated_by) VALUES($1,$2,$3,$4,'request','Transfer mention','new','normal',$5,$5)`, transferredWorkID, mspID, clientID, "TICKET-"+transferredWorkID, authorID)
	transferMention := collaboration.CreateCommand{
		Principal: principal,
		Parent:    collaboration.ParentRef{Type: mentions.ParentWorkRecord, ID: transferredWorkID},
		Body:      "@Recipient transfer test",
		Tokens: []mentions.Token{{
			ID: id.New(), TargetType: mentions.TargetStaff, TargetID: recipientID,
			Label: "@Recipient", Start: 0, End: 10,
		}},
		IdempotencyKey: id.New(), Source: "integration",
	}
	if _, err = service.CreateComment(ctx, transferMention); err != nil {
		t.Fatalf("create pre-transfer mention: %v", err)
	}
	var transferredItemID string
	if err = pool.QueryRow(ctx, `SELECT id::text FROM mention_items WHERE msp_id=$1 AND recipient_id=$2 AND parent_type='work_record' AND parent_id=$3`, mspID, recipientID, transferredWorkID).Scan(&transferredItemID); err != nil {
		t.Fatalf("load pre-transfer mention item: %v", err)
	}
	exec(`UPDATE work_records SET client_id=$2,updated_by=$3 WHERE id=$1`, transferredWorkID, nextClientID, authorID)
	transferEventID := id.New()
	exec(`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source) VALUES($1,'work_record.client.transferred',1,now(),$2,$3,'technician',$4,'work_record',$5,2,$1,'integration')`, transferEventID, mspID, nextClientID, authorID, transferredWorkID)
	if transferred := drainInvalidations(); transferred.Suppressed < 1 {
		t.Fatalf("transfer invalidation=%+v", transferred)
	}
	if err = pool.QueryRow(ctx, `SELECT suppressed_at IS NOT NULL FROM mention_items WHERE id=$1`, transferredItemID).Scan(&suppressed); err != nil || !suppressed {
		t.Fatalf("old Client projection was not suppressed=%v err=%v", suppressed, err)
	}
	nextAuthorAssignment, nextRecipientAssignment := id.New(), id.New()
	exec(`INSERT INTO role_assignments(id,msp_id,client_id,technician_id,role_id,granted_by) VALUES($1,$2,$3,$4,$6,$4),($5,$2,$3,$7,$6,$4)`, nextAuthorAssignment, mspID, nextClientID, authorID, nextRecipientAssignment, roleID, recipientID)
	transferMention.IdempotencyKey = id.New()
	transferMention.Tokens[0].ID = id.New()
	if _, err = service.CreateComment(ctx, transferMention); err != nil {
		t.Fatalf("create post-transfer mention: %v", err)
	}
	var movedItemID, movedClientID, movedState string
	var movedSuppressionCleared bool
	if err = pool.QueryRow(ctx, `SELECT id::text,client_id::text,state,suppressed_at IS NULL FROM mention_items WHERE msp_id=$1 AND recipient_id=$2 AND parent_type='work_record' AND parent_id=$3`, mspID, recipientID, transferredWorkID).Scan(&movedItemID, &movedClientID, &movedState, &movedSuppressionCleared); err != nil {
		t.Fatalf("load post-transfer mention item: %v", err)
	}
	if movedItemID != transferredItemID || movedClientID != nextClientID || movedState != "unread" || !movedSuppressionCleared {
		t.Fatalf("post-transfer item id=%s client=%s state=%s suppressionCleared=%v", movedItemID, movedClientID, movedState, movedSuppressionCleared)
	}

	// Merge events identify both the tombstoned duplicate and the winner. That
	// lets the invalidation consumer find Task mention projections after the
	// merge transaction has already reparented those Tasks to the winner.
	mergeWinnerID, mergeDuplicateID, mergeTaskID := id.New(), id.New(), id.New()
	exec(`INSERT INTO work_records(id,msp_id,client_id,display_id,record_type,title,status,priority,created_by,updated_by) VALUES($1,$3,$4,$5,'incident','Merge winner','new','normal',$2,$2),($6,$3,$4,$7,'incident','Merge duplicate','new','normal',$2,$2)`, mergeWinnerID, authorID, mspID, clientID, "TICKET-"+mergeWinnerID, mergeDuplicateID, "TICKET-"+mergeDuplicateID)
	exec(`INSERT INTO tasks(id,msp_id,client_id,parent_type,parent_id,work_record_id,title,status,position,created_by,updated_by) VALUES($1,$2,$3,'work_record',$4,$4,'Merged task','new',1,$5,$5)`, mergeTaskID, mspID, clientID, mergeDuplicateID, authorID)
	mergeTaskMention := collaboration.CreateCommand{
		Principal: principal,
		Parent:    collaboration.ParentRef{Type: mentions.ParentTask, ID: mergeTaskID},
		Body:      "@Recipient merged task",
		Tokens: []mentions.Token{{
			ID: id.New(), TargetType: mentions.TargetStaff, TargetID: recipientID,
			Label: "@Recipient", Start: 0, End: 10,
		}},
		IdempotencyKey: id.New(), Source: "integration",
	}
	if _, err = service.CreateComment(ctx, mergeTaskMention); err != nil {
		t.Fatalf("create pre-merge Task mention: %v", err)
	}
	var mergeTaskItemID string
	if err = pool.QueryRow(ctx, `SELECT id::text FROM mention_items WHERE msp_id=$1 AND recipient_id=$2 AND parent_type='task' AND parent_id=$3`, mspID, recipientID, mergeTaskID).Scan(&mergeTaskItemID); err != nil {
		t.Fatalf("load pre-merge Task mention item: %v", err)
	}
	exec(`UPDATE tasks SET work_record_id=$2,parent_id=$2,updated_by=$3 WHERE id=$1`, mergeTaskID, mergeWinnerID, authorID)
	exec(`UPDATE work_records SET lifecycle_state='deleted',deleted_at=now(),deleted_by=$2,merged_into_id=$3,updated_by=$2 WHERE id=$1`, mergeDuplicateID, authorID, mergeWinnerID)
	mergeEventID := id.New()
	exec(`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source,data) VALUES($1,'work_record.merged',1,now(),$2,$3,'technician',$4,'work_record',$5,2,$1,'integration',jsonb_build_object('winner_id',$6::text))`, mergeEventID, mspID, clientID, authorID, mergeDuplicateID, mergeWinnerID)
	drainInvalidations()
	var mergeTaskInvalidations int
	var mergeTaskStillVisible bool
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM mention_access_invalidations invalidation WHERE invalidation.causation_id=$1 AND invalidation.mention_item_id=$2),item.suppressed_at IS NULL FROM mention_items item WHERE item.id=$2`, mergeEventID, mergeTaskItemID).Scan(&mergeTaskInvalidations, &mergeTaskStillVisible); err != nil {
		t.Fatalf("load post-merge Task marker result: %v", err)
	}
	if mergeTaskInvalidations != 0 || !mergeTaskStillVisible {
		t.Fatalf("post-merge Task confirmed losses=%d stillVisible=%v", mergeTaskInvalidations, mergeTaskStillVisible)
	}

	// The production merge path must own the revision before waiting on an
	// object row. This is the inverse-order deadlock regression: a mention also
	// takes revision then parent, so both operations serialize without a cycle.
	lockWinnerID, lockDuplicateID := id.New(), id.New()
	exec(`INSERT INTO work_records(id,msp_id,client_id,display_id,record_type,title,status,priority,created_by,updated_by) VALUES($1,$3,$4,$5,'incident','Lock winner','new','normal',$2,$2),($6,$3,$4,$7,'incident','Lock duplicate','new','normal',$2,$2)`, lockWinnerID, authorID, mspID, clientID, "TICKET-"+lockWinnerID, lockDuplicateID, "TICKET-"+lockDuplicateID)
	blocker, blockErr := pool.Begin(ctx)
	if blockErr != nil {
		t.Fatalf("begin merge blocker: %v", blockErr)
	}
	if _, blockErr = blocker.Exec(ctx, `SELECT 1 FROM work_records WHERE id=$1 FOR UPDATE`, lockWinnerID); blockErr != nil {
		_ = blocker.Rollback(ctx)
		t.Fatalf("lock merge winner: %v", blockErr)
	}
	mergeAt := time.Now().UTC()
	mergeCorrelation, mergeAudit, mergeOutbox := id.New(), id.New(), id.New()
	mergeDone := make(chan error, 1)
	go func() {
		mergeDone <- NewWorkRecordRepositoryFromPool(pool).MergeAtomic(ctx, workrecords.MergeMutation{
			Winner:           workrecords.Record{Envelope: object.Envelope{ID: lockWinnerID, MSPID: mspID, ClientID: clientID, Version: 2, UpdatedAt: mergeAt, UpdatedBy: authorID}},
			Duplicate:        workrecords.Record{Envelope: object.Envelope{ID: lockDuplicateID, MSPID: mspID, ClientID: clientID, LifecycleState: "deleted", Version: 2, UpdatedAt: mergeAt, UpdatedBy: authorID}, DeletedAt: &mergeAt, DeletedBy: authorID, MergedIntoID: lockWinnerID},
			ReparentChildren: true,
			Audit:            mutation.AuditRecord{ID: mergeAudit, OccurredAt: mergeAt, MSPID: mspID, ClientID: clientID, ActorType: "technician", ActorID: authorID, Action: "work_record.merged", SubjectType: "work_record", SubjectID: lockDuplicateID, SubjectVersion: 2, Source: "integration", Reason: "lock order", CorrelationID: mergeCorrelation},
			Event:            mutation.EventRecord{EventID: mergeOutbox, EventType: "work_record.merged", SchemaVersion: 1, OccurredAt: mergeAt, MSPID: mspID, ClientID: clientID, ActorType: "technician", ActorID: authorID, SubjectType: "work_record", SubjectID: lockDuplicateID, SubjectVersion: 2, Source: "integration", CorrelationID: mergeCorrelation, Data: map[string]any{"winner_id": lockWinnerID}},
		})
	}()
	revisionOwned := false
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		_, probeErr := pool.Exec(ctx, `SELECT revision FROM mention_access_revisions WHERE msp_id=$1 FOR UPDATE NOWAIT`, mspID)
		var postgresErr *pgconn.PgError
		if errors.As(probeErr, &postgresErr) && postgresErr.Code == "55P03" {
			revisionOwned = true
			break
		}
	}
	if !revisionOwned {
		_ = blocker.Rollback(ctx)
		t.Fatal("merge did not acquire mention revision before waiting on parent")
	}
	mergeMentionDone := make(chan error, 1)
	go func() {
		_, mentionErr := service.CreateComment(ctx, collaboration.CreateCommand{Principal: principal, Parent: collaboration.ParentRef{Type: mentions.ParentWorkRecord, ID: lockWinnerID}, Body: "lock-order mention", IdempotencyKey: id.New(), Source: "integration"})
		mergeMentionDone <- mentionErr
	}()
	if blockErr = blocker.Commit(ctx); blockErr != nil {
		t.Fatalf("release merge blocker: %v", blockErr)
	}
	select {
	case mergeErr := <-mergeDone:
		if mergeErr != nil {
			t.Fatalf("merge after parent release: %v", mergeErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("merge deadlocked with concurrent mention")
	}
	select {
	case mentionErr := <-mergeMentionDone:
		if mentionErr != nil {
			t.Fatalf("mention after serialized merge: %v", mentionErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mention deadlocked behind merge")
	}

	// Time-limited grants become explicit revisioned losses. This prevents an
	// unrelated later grant from making a pre-expiry mention reappear.
	expiringRecipientID, expiringAssignmentID := id.New(), id.New()
	expiresAt := time.Now().UTC().Add(time.Hour)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$2,$3,'Expiring Recipient')`, expiringRecipientID, mspID, expiringRecipientID+"@example.test")
	exec(`INSERT INTO role_assignments(id,msp_id,client_id,technician_id,role_id,granted_at,granted_by,expires_at) VALUES($1,$2,$3,$4,$5,now(),$6,$7)`, expiringAssignmentID, mspID, clientID, expiringRecipientID, roleID, authorID, expiresAt)
	expiryMention := collaboration.CreateCommand{Principal: principal, Parent: collaboration.ParentRef{Type: mentions.ParentWorkRecord, ID: workID}, Body: "@Expiring follow up", Tokens: []mentions.Token{{ID: id.New(), TargetType: mentions.TargetStaff, TargetID: expiringRecipientID, Label: "@Expiring", Start: 0, End: 9}}, IdempotencyKey: id.New(), Source: "integration"}
	if _, err = service.CreateComment(ctx, expiryMention); err != nil {
		t.Fatalf("create pre-expiry mention: %v", err)
	}
	var expiringItemID string
	if err = pool.QueryRow(ctx, `SELECT id::text FROM mention_items WHERE msp_id=$1 AND recipient_id=$2 AND parent_type='work_record' AND parent_id=$3`, mspID, expiringRecipientID, workID).Scan(&expiringItemID); err != nil {
		t.Fatalf("load pre-expiry item: %v", err)
	}
	expiryWorker := mentions.NewInvalidationWorker(NewMentionRepositoryFromPool(pool), func() time.Time { return expiresAt.Add(time.Minute) })
	expirySuppressed := false
	for range 20 {
		if _, runErr := expiryWorker.RunOnce(ctx, 25); runErr != nil {
			t.Fatalf("run expiry invalidation: %v", runErr)
		}
		if err = pool.QueryRow(ctx, `SELECT suppressed_at IS NOT NULL FROM mention_items WHERE id=$1`, expiringItemID).Scan(&expirySuppressed); err != nil {
			t.Fatalf("load expiry suppression: %v", err)
		}
		if expirySuppressed {
			break
		}
	}
	var remainingAssignment, expiryMarkers int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM role_assignments WHERE id=$1),(SELECT count(*) FROM mention_access_loss_markers marker JOIN event_outbox event ON event.event_id=marker.event_id WHERE event.subject_id=$1 AND event.data->>'reason'='assignment_expired')`, expiringAssignmentID).Scan(&remainingAssignment, &expiryMarkers); err != nil {
		t.Fatalf("load expiry evidence: %v", err)
	}
	if !expirySuppressed || remainingAssignment != 0 || expiryMarkers != 1 {
		t.Fatalf("expiry suppressed=%v remaining assignment=%d markers=%d", expirySuppressed, remainingAssignment, expiryMarkers)
	}
}

func TestCollaborationTransactionRollsBackCallbackFailure(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewCollaborationRepository(&fakeSalesDB{tx: tx})
	want := context.Canceled
	err := repository.WithTransaction(context.Background(), mentions.SourceRef{MSPID: "msp", ClientID: "client", ParentType: mentions.ParentProject, ParentID: "project", SourceKind: mentions.SourceNote}, func(collaboration.Transaction) error { return want })
	if err != want || tx.committed || !tx.rolledBack {
		t.Fatalf("err=%v committed=%v rolledBack=%v", err, tx.committed, tx.rolledBack)
	}
}

package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/automation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	psastore "github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
)

type automationCommentConnectionGate struct{}

func (automationCommentConnectionGate) Authorize(context.Context, string, string, string, automation.ActionKind) error {
	return nil
}

func TestProductionAutomationEngineResolvesPersistedCreatorForNativeInternalComment(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for automation collaboration verification")
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

	mspID, clientID, secondClientID, creatorID := id.New(), id.New(), id.New(), id.New()
	roleID, assignmentID, workID, secondWorkID := id.New(), id.New(), id.New(), id.New()
	otherMSPID, otherCreatorID := id.New(), id.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, execErr := pool.Exec(ctx, query, args...); execErr != nil {
			t.Fatalf("fixture: %v", execErr)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'automation mentions',$3,$3),($4,$5,'other msp',$6,$6)`, mspID, "AUTO-MENTION-"+mspID, creatorID, otherMSPID, "OTHER-"+otherMSPID, otherCreatorID)
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$3,$4,'client one',$5,$5),($2,$3,$6,'client two',$5,$5)`, clientID, secondClientID, mspID, "CLIENT-"+clientID, creatorID, "CLIENT-"+secondClientID)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$2,$3,'Creator'),($4,$5,$6,'Other MSP Creator')`, creatorID, mspID, creatorID+"@example.test", otherCreatorID, otherMSPID, otherCreatorID+"@example.test")
	exec(`INSERT INTO roles(id,msp_id,key,name) VALUES($1,$2,$3,'Automation creator mention access')`, roleID, mspID, "automation-mention-"+roleID)
	exec(`INSERT INTO role_capabilities(role_id,msp_id,capability) VALUES($1,$2,'mention.create'),($1,$2,'mention.read'),($1,$2,'work_record.read'),($1,$2,'work_record.edit')`, roleID, mspID)
	exec(`INSERT INTO role_assignments(id,msp_id,client_id,technician_id,role_id,granted_by) VALUES($1,$2,$3,$4,$5,$4)`, assignmentID, mspID, clientID, creatorID, roleID)
	exec(`INSERT INTO work_records(id,msp_id,client_id,display_id,record_type,title,status,priority,created_by,updated_by) VALUES($1,$3,$4,$5,'incident','Automation one','new','normal',$6,$6),($2,$3,$7,$8,'incident','Automation two','new','normal',$6,$6)`, workID, secondWorkID, mspID, clientID, "TICKET-"+workID, creatorID, secondClientID, "TICKET-"+secondWorkID)

	repository := psastore.NewAutomationRepositoryFromPool(pool)
	creator := authorization.Principal{ID: creatorID, Scope: scope.Principal{MSPID: mspID, ClientID: clientID}, Capabilities: authorization.NewCapabilitySet("automation.manage")}
	engine := automation.NewEngine(
		psastore.NewAutomationRuntimeRepositoryFromPool(pool, id.New),
		buildAutomationActionExecutor(pool), automationCommentConnectionGate{}, time.Now, id.New,
	)

	withoutInternalCapability := createAndReloadAutomationCommentDefinition(
		t, ctx, repository, creator, "creator comment without internal capability",
		[]string{"work_record.edit"},
	)
	assertAutomationCommentDenied(t, ctx, engine, automationCommentRunCommand(withoutInternalCapability, mspID, clientID, workID, "engine-missing-capability"), pool, mspID, workID, 0)
	assertAutomationNativeCommentFacts(t, ctx, pool, mspID, clientID, workID, creatorID, 0)

	definition := createAndReloadAutomationCommentDefinition(
		t, ctx, repository, creator, "creator comment",
		[]string{"comment.internal.create"},
	)
	if definition.ID == creatorID {
		t.Fatalf("definition ID unexpectedly equals creator technician ID: %s", definition.ID)
	}
	command := automationCommentRunCommand(definition, mspID, clientID, workID, "engine-success")
	first, err := engine.Run(ctx, command)
	if err != nil || len(first.ChangedObjectIDs) != 1 {
		t.Fatalf("first engine run=%+v err=%v", first, err)
	}
	second, err := engine.Run(ctx, command)
	if err != nil || !second.Replayed || second.RunID != first.RunID || len(second.ChangedObjectIDs) != 1 || second.ChangedObjectIDs[0] != first.ChangedObjectIDs[0] {
		t.Fatalf("engine replay=%+v err=%v first=%+v", second, err, first)
	}
	assertAutomationNativeCommentFacts(t, ctx, pool, mspID, clientID, workID, creatorID, 1)

	// An inactive persisted creator cannot be resolved.
	exec(`UPDATE technicians SET lifecycle_state='inactive',version=version+1 WHERE id=$1 AND msp_id=$2`, creatorID, mspID)
	assertAutomationCommentDenied(t, ctx, engine, automationCommentRunCommand(definition, mspID, clientID, workID, "engine-inactive"), pool, mspID, workID, 1)
	exec(`UPDATE technicians SET lifecycle_state='active',version=version+1 WHERE id=$1 AND msp_id=$2`, creatorID, mspID)

	// A currently revoked role reaches collaboration but fails its current
	// author/source authorization without creating another source.
	exec(`DELETE FROM role_assignments WHERE id=$1`, assignmentID)
	assertAutomationCommentDenied(t, ctx, engine, automationCommentRunCommand(definition, mspID, clientID, workID, "engine-revoked"), pool, mspID, workID, 1)

	// A definition legitimately scoped to another client still resolves the
	// same creator, but that creator has no current grant in that client.
	secondClientCreator := creator
	secondClientCreator.Scope.ClientID = secondClientID
	secondClientDefinition := createAndReloadAutomationCommentDefinition(t, ctx, repository, secondClientCreator, "cross-client comment", []string{"comment.internal.create"})
	assertAutomationCommentDenied(t, ctx, engine, automationCommentRunCommand(secondClientDefinition, mspID, secondClientID, secondWorkID, "engine-cross-client"), pool, mspID, secondWorkID, 0)

	// Missing and cross-MSP creator records fail at the resolver boundary.
	missingCreator := creator
	missingCreator.ID = id.New()
	missingDefinition := createAndReloadAutomationCommentDefinition(t, ctx, repository, missingCreator, "missing creator", []string{"comment.internal.create"})
	assertAutomationCommentDenied(t, ctx, engine, automationCommentRunCommand(missingDefinition, mspID, clientID, workID, "engine-missing"), pool, mspID, workID, 1)
	crossMSPCreator := creator
	crossMSPCreator.ID = otherCreatorID
	crossMSPDefinition := createAndReloadAutomationCommentDefinition(t, ctx, repository, crossMSPCreator, "cross MSP creator", []string{"comment.internal.create"})
	assertAutomationCommentDenied(t, ctx, engine, automationCommentRunCommand(crossMSPDefinition, mspID, clientID, workID, "engine-cross-msp"), pool, mspID, workID, 1)
}

func createAndReloadAutomationCommentDefinition(t *testing.T, ctx context.Context, repository *psastore.AutomationRepository, creator authorization.Principal, name string, capabilities []string) automation.Definition {
	t.Helper()
	management := automation.NewManagementService(repository, time.Now, id.New)
	created, err := management.CreateDefinition(ctx, automation.CreateDefinitionCommand{
		Principal: creator, Name: name, Trigger: automation.Trigger{EventType: "work_record.updated"},
		Capabilities: capabilities,
		Steps: []automation.Step{{ID: "comment-step", Kind: automation.StepAction, Action: &automation.Action{Kind: automation.ActionAddComment, Parameters: map[string]string{
			"visibility": "internal", "body": "Plain @Recipient text",
			"mention_targets": `[{"target_id":"forged"}]`,
		}}}},
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	published, err := management.PublishDefinition(ctx, automation.PublishDefinitionCommand{Principal: creator, ID: created.ID, Version: created.Version, ExpectedRecordVersion: 1})
	if err != nil {
		t.Fatalf("publish definition: %v", err)
	}
	loaded, err := repository.FindDefinition(ctx, scope.Target{MSPID: creator.Scope.MSPID, ClientID: creator.Scope.ClientID}, published.ID, published.Version)
	if err != nil || loaded.State != automation.Published {
		t.Fatalf("reload definition=%+v err=%v", loaded, err)
	}
	return loaded.Definition
}

func automationCommentRunCommand(definition automation.Definition, mspID, clientID, workID, idempotencyKey string) automation.RunCommand {
	return automation.RunCommand{
		Definition: definition,
		Event: automation.TriggerEvent{ID: id.New(), Type: definition.Trigger.EventType, MSPID: mspID, ClientID: clientID, InputSnapshot: map[string]string{
			"_subject_type": "work_record", "_subject_id": workID, "_subject_version": "1",
		}},
		IdempotencyKey: idempotencyKey, Attempt: 1, MaxAttempts: 1,
	}
}

func assertAutomationNativeCommentFacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, mspID, clientID, workID, creatorID string, wantSources int) {
	t.Helper()
	var sourceCount, occurrenceCount, resolutionCount, itemCount, deliveryCount, legacyCount int
	var tokens, authorID string
	if err := pool.QueryRow(ctx, `SELECT count(*),COALESCE(min(mention_tokens::text),''),COALESCE(min(author_id::text),'') FROM internal_collaboration_sources WHERE msp_id=$1 AND client_id=$2 AND parent_type='work_record' AND parent_id=$3`, mspID, clientID, workID).Scan(&sourceCount, &tokens, &authorID); err != nil {
		t.Fatalf("native source: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM mention_occurrences WHERE msp_id=$1`, mspID).Scan(&occurrenceCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM mention_recipient_resolutions WHERE msp_id=$1`, mspID).Scan(&resolutionCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM mention_items WHERE msp_id=$1`, mspID).Scan(&itemCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notification_deliveries WHERE msp_id=$1 AND mention_occurrence_id IS NOT NULL`, mspID).Scan(&deliveryCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM comments WHERE msp_id=$1 AND client_id=$2 AND work_record_id=$3`, mspID, clientID, workID).Scan(&legacyCount); err != nil {
		t.Fatal(err)
	}
	if sourceCount != wantSources || (wantSources > 0 && (tokens != "[]" || authorID != creatorID)) || occurrenceCount != 0 || resolutionCount != 0 || itemCount != 0 || deliveryCount != 0 || legacyCount != 0 {
		t.Fatalf("source=%d tokens=%q author=%q occurrences=%d resolutions=%d items=%d deliveries=%d legacy=%d", sourceCount, tokens, authorID, occurrenceCount, resolutionCount, itemCount, deliveryCount, legacyCount)
	}
}

func assertAutomationCommentDenied(t *testing.T, ctx context.Context, engine *automation.Engine, command automation.RunCommand, pool *pgxpool.Pool, mspID, workID string, wantSources int) {
	t.Helper()
	if _, err := engine.Run(ctx, command); !errors.Is(err, automation.ErrActionFailed) {
		t.Fatalf("denied engine run error=%v", err)
	}
	var sourceCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM internal_collaboration_sources WHERE msp_id=$1 AND parent_id=$2`, mspID, workID).Scan(&sourceCount); err != nil || sourceCount != wantSources {
		t.Fatalf("denied source count=%d want=%d err=%v", sourceCount, wantSources, err)
	}
}

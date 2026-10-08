package psa

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/automation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestAutomationRepositoryFindsDefinitionOnlyInActiveClientScope(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(*string)) = "automation-id"
		*(destinations[1].(*string)) = "msp-id"
		*(destinations[2].(*string)) = "Automation"
		*(destinations[3].(*int64)) = 3
		*(destinations[4].(*string)) = "version-id"
		*(destinations[5].(*int64)) = 2
		*(destinations[6].(*automation.DefinitionState)) = automation.Draft
		*(destinations[7].(*string)) = "work_record.created"
		*(destinations[8].(*[]string)) = []string{"client-id"}
		*(destinations[9].(*[]string)) = []string{"work_record.assign"}
		*(destinations[10].(*[]byte)) = []byte(`{"steps":[{
		  "id":"assign","kind":"action","action":{
		    "kind":"assign","parameters":{"owner_id":"owner-id"}
		  }
		}]}`)
	}}}
	managed, err := NewAutomationRepository(db).FindDefinition(
		context.Background(),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		"automation-id", 2,
	)
	if err != nil || managed.RecordVersion != 3 ||
		managed.Definition.Steps[0].Action.Kind != automation.ActionAssign ||
		!strings.Contains(db.query, "$3::uuid = ANY(version.client_scopes)") {
		t.Fatalf(
			"FindDefinition() managed=%+v error=%v query=%s",
			managed, err, db.query,
		)
	}
}

func TestAutomationRepositoryWritesDefinitionLifecycleAtomically(t *testing.T) {
	now := time.Now().UTC()
	base := automation.DefinitionMutation{
		Managed: automation.ManagedDefinition{
			Definition: automation.Definition{
				ID: "automation-id", MSPID: "msp-id", Version: 1,
				State:        automation.Draft,
				ClientScopes: []string{"client-id"},
				Capabilities: []string{"work_record.assign"},
				Trigger:      automation.Trigger{EventType: "work_record.created"},
				Steps: []automation.Step{{
					ID: "assign", Kind: automation.StepAction,
					Action: &automation.Action{
						Kind:       automation.ActionAssign,
						Parameters: map[string]string{"owner_id": "owner-id"},
					},
				}},
			},
			Name: "Automation", RecordVersion: 1,
			VersionID: "version-id",
		},
		VersionID: "version-id",
		Audit: mutation.AuditRecord{
			ID: "audit", OccurredAt: now, MSPID: "msp-id",
			ClientID: "client-id", ActorType: "technician",
			ActorID: "actor", Action: "automation.definition.created",
			SubjectType: "automation_definition",
			SubjectID:   "automation-id", SubjectVersion: 1,
			Source: "api", CorrelationID: "correlation",
		},
		Event: mutation.EventRecord{
			EventID: "event", EventType: "automation.definition.created",
			SchemaVersion: 1, OccurredAt: now, MSPID: "msp-id",
			ClientID: "client-id", ActorType: "technician",
			ActorID: "actor", SubjectType: "automation_definition",
			SubjectID: "automation-id", SubjectVersion: 1,
			Source: "api", CorrelationID: "correlation",
		},
	}
	for _, test := range []struct {
		name string
		run  func(*AutomationRepository, automation.DefinitionMutation) error
		want []string
	}{
		{
			name: "create",
			run: func(repository *AutomationRepository, mutation automation.DefinitionMutation) error {
				return repository.CreateDefinition(context.Background(), mutation)
			},
			want: []string{
				"INSERT INTO automation_definitions",
				"INSERT INTO automation_versions",
			},
		},
		{
			name: "revise",
			run: func(repository *AutomationRepository, mutation automation.DefinitionMutation) error {
				mutation.Managed.Version = 2
				mutation.Managed.RecordVersion = 2
				mutation.ExpectedRecordVersion = 1
				return repository.ReviseDefinition(context.Background(), mutation)
			},
			want: []string{
				"UPDATE automation_definitions",
				"INSERT INTO automation_versions",
			},
		},
		{
			name: "publish",
			run: func(repository *AutomationRepository, mutation automation.DefinitionMutation) error {
				mutation.Managed.State = automation.Published
				mutation.Managed.RecordVersion = 2
				mutation.ExpectedRecordVersion = 1
				return repository.PublishDefinition(context.Background(), mutation)
			},
			want: []string{
				"UPDATE automation_versions",
				"UPDATE automation_definitions",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeSalesTx{queryRow: fakeRow{scan: func(destinations ...any) {
				*(destinations[0].(*int)) = 0
			}}}
			err := test.run(
				NewAutomationRepository(&fakeSalesDB{tx: tx}), base,
			)
			if err != nil {
				t.Fatalf("lifecycle mutation error=%v", err)
			}
			assertQueryOrder(
				t, tx.queries,
				test.want[0], test.want[1],
				"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
			)
			if !tx.committed || tx.rolledBack {
				t.Fatalf(
					"committed=%v rolledBack=%v",
					tx.committed, tx.rolledBack,
				)
			}
		})
	}
}

func TestAutomationRepositoryRejectsInactiveCatalogReferencesBeforePublish(t *testing.T) {
	tx := &fakeSalesTx{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(*int)) = 1
	}}}
	mutation := automation.DefinitionMutation{Managed: automation.ManagedDefinition{
		Definition: automation.Definition{
			ID: "automation-id", MSPID: "msp-id", Version: 1,
			State: automation.Published, ClientScopes: []string{"client-id"},
			Capabilities: []string{"classification.apply"},
			Trigger:      automation.Trigger{EventType: "tag.added"},
			Steps: []automation.Step{{
				ID: "tag", Kind: automation.StepAction,
				Action: &automation.Action{Kind: automation.ActionAddTags, Parameters: map[string]string{
					"tag_ids": `["11111111-1111-4111-8111-111111111111"]`,
				}},
			}},
		},
		VersionID: "version-id",
	}, ExpectedRecordVersion: 1}

	err := NewAutomationRepository(&fakeSalesDB{tx: tx}).PublishDefinition(context.Background(), mutation)

	if !errors.Is(err, automation.ErrInactiveCatalogReference) || !tx.rolledBack || tx.committed ||
		len(tx.queries) != 0 || !strings.Contains(tx.query, "catalog.lifecycle_state = 'active'") {
		t.Fatalf("PublishDefinition() error=%v queries=%v validation=%s", err, tx.queries, tx.query)
	}
}

func TestAutomationRepositoryCreatesLeastPrivilegeExternalConnection(t *testing.T) {
	tx := &fakeSalesTx{}
	now := time.Now().UTC()
	err := NewAutomationRepository(
		&fakeSalesDB{tx: tx},
	).CreateConnection(context.Background(), automation.ConnectionMutation{
		Connection: automation.ExternalConnection{
			ID: "connection-id", MSPID: "msp-id", ClientID: "client-id",
			Name: "Workflow", Endpoint: "https://automation.example/hook",
			SigningSecretRef: "env://RARITY_AUTOMATION_HTTP_SECRET_PRIMARY",
		},
		Audit: mutation.AuditRecord{
			ID: "audit", OccurredAt: now, MSPID: "msp-id",
			ClientID: "client-id", ActorType: "technician",
			ActorID: "actor", Action: "automation.connection.created",
			SubjectType: "connection", SubjectID: "connection-id",
			SubjectVersion: 1, Source: "api", CorrelationID: "correlation",
		},
		Event: mutation.EventRecord{
			EventID: "event", EventType: "automation.connection.created",
			SchemaVersion: 1, OccurredAt: now, MSPID: "msp-id",
			ClientID: "client-id", ActorType: "technician",
			ActorID: "actor", SubjectType: "connection",
			SubjectID: "connection-id", SubjectVersion: 1,
			Source: "api", CorrelationID: "correlation",
		},
	})
	if err != nil {
		t.Fatalf("CreateConnection() error=%v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO connections",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	if !strings.Contains(tx.queries[0], "'automation.call_http'") ||
		!strings.Contains(tx.queries[0], "'automation.input.safe'") ||
		!strings.Contains(
			tx.queries[0],
			"jsonb_build_object('signing_secret', $5::text)",
		) {
		t.Fatalf("connection is not least privilege: %s", tx.queries[0])
	}
}

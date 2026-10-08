package automation

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type managementRepositoryStub struct {
	managed    ManagedDefinition
	created    DefinitionMutation
	revised    DefinitionMutation
	published  DefinitionMutation
	connection ConnectionMutation
}

func (r *managementRepositoryStub) ListDefinitions(
	context.Context,
	scope.Target,
) ([]ManagedDefinition, error) {
	return []ManagedDefinition{r.managed}, nil
}

func (r *managementRepositoryStub) FindDefinition(
	context.Context,
	scope.Target,
	string,
	int64,
) (ManagedDefinition, error) {
	return r.managed, nil
}

func (r *managementRepositoryStub) CreateDefinition(
	_ context.Context,
	mutation DefinitionMutation,
) error {
	r.created = mutation
	return nil
}

func (r *managementRepositoryStub) ReviseDefinition(
	_ context.Context,
	mutation DefinitionMutation,
) error {
	r.revised = mutation
	return nil
}

func (r *managementRepositoryStub) PublishDefinition(
	_ context.Context,
	mutation DefinitionMutation,
) error {
	r.published = mutation
	return nil
}

func (r *managementRepositoryStub) CreateConnection(
	_ context.Context,
	mutation ConnectionMutation,
) error {
	r.connection = mutation
	return nil
}

func TestManagementServiceCreatesPublishesAndRevisesTypedDefinition(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	repository := &managementRepositoryStub{}
	service := NewManagementService(
		repository, func() time.Time { return now },
		sequenceAutomationIDs(
			"automation-id", "version-id", "audit-id", "event-id", "correlation-id",
			"publish-audit-id", "publish-event-id", "publish-correlation-id",
			"revision-version-id", "revision-audit-id", "revision-event-id",
			"revision-correlation-id",
		),
	)
	principal := automationManagementPrincipal()
	steps := []Step{{ID: "assign", Kind: StepAction, Action: &Action{
		Kind:       ActionAssign,
		Parameters: map[string]string{"owner_id": "owner-id"},
	}}}

	created, err := service.CreateDefinition(
		context.Background(),
		CreateDefinitionCommand{
			Principal: principal, Name: "Assign critical incidents",
			Trigger:      Trigger{EventType: "work_record.created"},
			Capabilities: []string{"work_record.assign"},
			Steps:        steps,
		},
	)
	if err != nil || created.State != Draft ||
		created.ClientScopes[0] != "client-id" ||
		repository.created.VersionID != "version-id" ||
		repository.created.Audit.Action != "automation.definition.created" {
		t.Fatalf(
			"CreateDefinition() definition=%+v error=%v mutation=%+v",
			created, err, repository.created,
		)
	}

	repository.managed = ManagedDefinition{
		Definition: created, Name: "Assign critical incidents",
		RecordVersion: 1, VersionID: "version-id",
	}
	revised, err := service.ReviseDefinition(
		context.Background(),
		ReviseDefinitionCommand{
			Principal: principal, ID: "automation-id", Version: 1,
			ExpectedRecordVersion: 1,
			Trigger:               Trigger{EventType: "work_record.created"},
			Capabilities:          []string{"work_record.assign"},
			Steps:                 steps,
		},
	)
	if err == nil || revised.Version != 0 {
		t.Fatal("draft definition was revised before publication")
	}

	published, err := service.PublishDefinition(
		context.Background(),
		PublishDefinitionCommand{
			Principal: principal, ID: "automation-id", Version: 1,
			ExpectedRecordVersion: 1,
		},
	)
	if err != nil || published.State != Published ||
		repository.published.ExpectedRecordVersion != 1 ||
		repository.published.Audit.Action != "automation.definition.published" {
		t.Fatalf(
			"PublishDefinition() definition=%+v error=%v mutation=%+v",
			published, err, repository.published,
		)
	}

	repository.managed = ManagedDefinition{
		Definition: published, Name: "Assign critical incidents",
		RecordVersion: 2, VersionID: "version-id",
	}
	revised, err = service.ReviseDefinition(
		context.Background(),
		ReviseDefinitionCommand{
			Principal: principal, ID: "automation-id", Version: 1,
			ExpectedRecordVersion: 2,
			Trigger:               Trigger{EventType: "work_record.created"},
			Capabilities:          []string{"work_record.assign"},
			Steps:                 steps,
		},
	)
	if err != nil || revised.State != Draft || revised.Version != 2 ||
		repository.revised.VersionID != "revision-version-id" ||
		repository.revised.ExpectedRecordVersion != 2 {
		t.Fatalf(
			"ReviseDefinition() definition=%+v error=%v mutation=%+v",
			revised, err, repository.revised,
		)
	}
}

func TestManagementServiceCreatesClientScopedSafeExternalConnection(t *testing.T) {
	repository := &managementRepositoryStub{}
	service := NewManagementService(
		repository, time.Now,
		sequenceAutomationIDs(
			"connection-id", "audit-id", "event-id", "correlation-id",
		),
	)
	connection, err := service.CreateExternalConnection(
		context.Background(),
		CreateExternalConnectionCommand{
			Principal:        automationManagementPrincipal(),
			Name:             "Customer workflow",
			Endpoint:         "https://automation.example/hook",
			SigningSecretRef: "env://RARITY_AUTOMATION_HTTP_SECRET_PRIMARY",
		},
	)
	if err != nil || connection.ClientID != "client-id" ||
		connection.ID != "connection-id" ||
		repository.connection.Audit.Action != "automation.connection.created" {
		t.Fatalf(
			"CreateExternalConnection() connection=%+v error=%v mutation=%+v",
			connection, err, repository.connection,
		)
	}
	_, err = service.CreateExternalConnection(
		context.Background(),
		CreateExternalConnectionCommand{
			Principal: automationManagementPrincipal(),
			Name:      "Unsafe", Endpoint: "https://127.0.0.1/hook",
			SigningSecretRef: "env://RARITY_AUTOMATION_HTTP_SECRET_PRIMARY",
		},
	)
	if err == nil {
		t.Fatal("unsafe external connection accepted")
	}
}

func automationManagementPrincipal() authorization.Principal {
	return authorization.Principal{
		ID:    "technician-id",
		Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet(
			"automation.manage",
		),
	}
}

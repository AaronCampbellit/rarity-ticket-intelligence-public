package organizations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type directoryRepositoryStub struct {
	department DirectoryMutation
	team       DirectoryMutation
	queue      DirectoryMutation
	membership TeamMembershipMutation
	listed     Directory
	listTarget scope.Target
}

func (r *directoryRepositoryStub) CreateDepartmentAtomic(
	_ context.Context,
	mutation DirectoryMutation,
) error {
	r.department = mutation
	return nil
}

func (r *directoryRepositoryStub) CreateTeamAtomic(
	_ context.Context,
	mutation DirectoryMutation,
) error {
	r.team = mutation
	return nil
}

func (r *directoryRepositoryStub) CreateQueueAtomic(
	_ context.Context,
	mutation DirectoryMutation,
) error {
	r.queue = mutation
	return nil
}

func (r *directoryRepositoryStub) ReplaceTeamMembersAtomic(
	_ context.Context,
	mutation TeamMembershipMutation,
) error {
	r.membership = mutation
	return nil
}

func (r *directoryRepositoryStub) ListDirectory(
	_ context.Context,
	target scope.Target,
) (Directory, error) {
	r.listTarget = target
	return r.listed, nil
}

func TestDirectoryServiceCreatesAuditedHierarchy(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	ids := []string{
		"department-id", "department-audit", "department-event", "department-correlation",
		"team-id", "team-audit", "team-event", "team-correlation",
		"queue-id", "queue-audit", "queue-event", "queue-correlation",
	}
	repository := &directoryRepositoryStub{}
	service := NewDirectoryService(repository, func() time.Time { return now }, func() string {
		value := ids[0]
		ids = ids[1:]
		return value
	})
	principal := authorization.Principal{
		ID: "actor-id", Scope: scope.Principal{MSPID: "msp-id"},
		Capabilities: authorization.NewCapabilitySet("organization.manage"),
	}

	department, err := service.CreateDepartment(context.Background(), CreateDepartmentCommand{
		Principal: principal, Key: "service-desk", Name: "Service Desk",
		Reason: "initial structure", Source: "api",
	})
	if err != nil || department.ID != "department-id" ||
		repository.department.Audit.Action != "department.created" ||
		repository.department.Event.EventType != "department.created" {
		t.Fatalf("department=%+v mutation=%+v error=%v", department, repository.department, err)
	}
	team, err := service.CreateTeam(context.Background(), CreateTeamCommand{
		Principal: principal, DepartmentID: department.ID,
		Key: "tier-one", Name: "Tier One", Reason: "initial structure", Source: "api",
	})
	if err != nil || team.DepartmentID != department.ID ||
		repository.team.Audit.Action != "team.created" {
		t.Fatalf("team=%+v mutation=%+v error=%v", team, repository.team, err)
	}
	queue, err := service.CreateQueue(context.Background(), CreateQueueCommand{
		Principal: principal, ClientID: "client-id",
		DepartmentID: department.ID, TeamID: team.ID,
		Key: "triage", Name: "Triage", Reason: "initial structure", Source: "api",
	})
	if err != nil || queue.ClientID != "client-id" ||
		repository.queue.Audit.ClientID != "client-id" ||
		repository.queue.Event.EventType != "queue.created" {
		t.Fatalf("queue=%+v mutation=%+v error=%v", queue, repository.queue, err)
	}
}

func TestDirectoryServiceListsOnlyAuthorizedScope(t *testing.T) {
	repository := &directoryRepositoryStub{listed: Directory{
		Queues: []Queue{{ID: "queue-id", ClientID: "client-id"}},
	}}
	service := NewDirectoryService(repository, time.Now, func() string { return "id" })
	principal := authorization.Principal{
		ID: "actor-id",
		Scope: scope.Principal{
			MSPID: "msp-id", ClientID: "client-id",
		},
		Capabilities: authorization.NewCapabilitySet("organization.read"),
	}
	result, err := service.List(context.Background(), ListDirectoryCommand{
		Principal: principal,
	})
	if err != nil || len(result.Queues) != 1 ||
		repository.listTarget != (scope.Target{MSPID: "msp-id", ClientID: "client-id"}) {
		t.Fatalf("directory=%+v target=%+v error=%v", result, repository.listTarget, err)
	}
	_, err = service.CreateDepartment(context.Background(), CreateDepartmentCommand{
		Principal: principal, Key: "forbidden", Name: "Forbidden",
		Reason: "invalid", Source: "api",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("client-scoped department creation error=%v, want ErrForbidden", err)
	}
}

func TestDirectoryServiceLetsMSPGlobalManagersLoadMembershipDirectory(t *testing.T) {
	repository := &directoryRepositoryStub{}
	service := NewDirectoryService(repository, time.Now, func() string { return "id" })
	principal := authorization.Principal{
		ID: "actor-id", Scope: scope.Principal{MSPID: "msp-id"},
		Capabilities: authorization.NewCapabilitySet("organization.manage"),
	}
	if _, err := service.List(context.Background(), ListDirectoryCommand{Principal: principal}); err != nil {
		t.Fatalf("MSP-global manager list error=%v", err)
	}
	if repository.listTarget != (scope.Target{MSPID: "msp-id"}) {
		t.Fatalf("manager list target=%+v", repository.listTarget)
	}
}

func TestDirectoryServiceAuthorizesOnlyExactActiveClientTarget(t *testing.T) {
	active := Client{Envelope: object.Envelope{
		ID: "client-active", MSPID: "msp-id", ClientID: "client-active",
		LifecycleState: "active",
	}}
	repository := &directoryRepositoryStub{listed: Directory{Clients: []Client{active}}}
	service := NewDirectoryService(repository, time.Now, func() string { return "id" })
	principal := authorization.Principal{
		ID:    "actor-id",
		Scope: scope.Principal{MSPID: "msp-id"},
	}
	target := scope.Target{MSPID: "msp-id", ClientID: "client-active"}

	if err := service.AuthorizeExecutionTarget(
		context.Background(), principal, target,
	); err != nil {
		t.Fatalf("AuthorizeExecutionTarget() error=%v", err)
	}
	if repository.listTarget != target {
		t.Fatalf("directory target=%+v want=%+v", repository.listTarget, target)
	}

	for name, result := range map[string]Directory{
		"missing": {Clients: nil},
		"inactive": {Clients: []Client{{Envelope: object.Envelope{
			ID: "client-active", MSPID: "msp-id", ClientID: "client-active",
			LifecycleState: "inactive",
		}}}},
		"wrong ID": {Clients: []Client{{Envelope: object.Envelope{
			ID: "different-client", MSPID: "msp-id", ClientID: "different-client",
			LifecycleState: "active",
		}}}},
	} {
		t.Run(name, func(t *testing.T) {
			repository.listed = result
			err := service.AuthorizeExecutionTarget(
				context.Background(), principal, target,
			)
			if !errors.Is(err, scope.ErrNotFound) {
				t.Fatalf("AuthorizeExecutionTarget() error=%v, want ErrNotFound", err)
			}
		})
	}

	clientScoped := principal
	clientScoped.Scope.ClientID = "client-active"
	repository.listed = Directory{Clients: []Client{active}}
	if err := service.AuthorizeExecutionTarget(
		context.Background(), clientScoped, target,
	); err != nil {
		t.Fatalf("client-scoped target error=%v", err)
	}
	err := service.AuthorizeExecutionTarget(
		context.Background(), clientScoped,
		scope.Target{MSPID: "msp-id", ClientID: "different-client"},
	)
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-client target error=%v, want ErrNotFound", err)
	}

	if err := service.AuthorizeExecutionTarget(
		context.Background(), principal, scope.Target{MSPID: "msp-id"},
	); err != nil {
		t.Fatalf("MSP-global target error=%v", err)
	}
}

func TestDirectoryServiceResolvesActiveClientForSpecificCapabilityWithoutDirectoryRead(t *testing.T) {
	repository := &directoryRepositoryStub{listed: Directory{Clients: []Client{{
		Envelope: object.Envelope{ID: "client-1", MSPID: "msp-1", ClientID: "client-1", DisplayID: "NW-100", LifecycleState: "active"},
		Name:     "Northwind Legal",
	}}}}
	service := NewDirectoryService(repository, time.Now, func() string { return "id" })
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet("location.create"),
	}
	client, err := service.ResolveActiveClient(
		context.Background(), principal, "location.create", "Northwind Legal",
	)
	if err != nil || client.ID != "client-1" {
		t.Fatalf("ResolveActiveClient() client=%+v error=%v", client, err)
	}
	if repository.listTarget != (scope.Target{MSPID: "msp-1"}) {
		t.Fatalf("directory target=%+v", repository.listTarget)
	}
}

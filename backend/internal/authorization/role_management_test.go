package authorization

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type roleRepositoryStub struct {
	roles      []Role
	saved      Role
	assignment RoleAssignment
	audit      mutation.AuditRecord
	event      mutation.EventRecord
}

func (r *roleRepositoryStub) ListRoles(context.Context, string) ([]Role, error) {
	return r.roles, nil
}

func (r *roleRepositoryStub) ReplaceRoleCapabilities(
	_ context.Context,
	_ string,
	role Role,
	audit mutation.AuditRecord,
	event mutation.EventRecord,
) error {
	r.saved, r.audit, r.event = role, audit, event
	return nil
}

func (r *roleRepositoryStub) CreateRoleAtomic(
	_ context.Context,
	_ string,
	role Role,
	audit mutation.AuditRecord,
	event mutation.EventRecord,
) error {
	r.saved, r.audit, r.event = role, audit, event
	return nil
}

func (r *roleRepositoryStub) AssignRoleAtomic(
	_ context.Context,
	assignment RoleAssignment,
	audit mutation.AuditRecord,
	event mutation.EventRecord,
) error {
	r.assignment, r.audit, r.event = assignment, audit, event
	return nil
}

func TestRoleManagementReplacesCapabilitiesWithAuditAndEvent(t *testing.T) {
	repository := &roleRepositoryStub{roles: []Role{{
		ID: "role-id", Key: "technician", Version: 4,
		Capabilities: []string{"work_record.read"},
	}}}
	service := NewRoleManagementService(
		repository,
		func() time.Time { return time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC) },
		func() string { return "generated-id" },
	)
	updated, err := service.ReplaceCapabilities(
		context.Background(),
		Principal{
			ID: "admin-id", Scope: scope.Principal{MSPID: "msp-id"},
			Capabilities: NewCapabilitySet("role.manage"),
		},
		"role-id", 4,
		[]string{"work_record.update", "work_record.read", "work_record.read"},
		"Align service desk access",
	)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 5 || len(updated.Capabilities) != 2 ||
		updated.Capabilities[0] != "work_record.read" ||
		repository.audit.Reason != "Align service desk access" ||
		repository.event.EventType != "role.capabilities_replaced" {
		t.Fatalf("updated=%+v audit=%+v event=%+v", updated, repository.audit, repository.event)
	}
}

func TestRoleManagementRejectsStaleOrUnreasonedChanges(t *testing.T) {
	repository := &roleRepositoryStub{roles: []Role{{ID: "role-id", Version: 2}}}
	service := NewRoleManagementService(repository, time.Now, func() string { return "id" })
	principal := Principal{
		ID: "admin", Scope: scope.Principal{MSPID: "msp"},
		Capabilities: NewCapabilitySet("role.manage"),
	}
	if _, err := service.ReplaceCapabilities(
		context.Background(), principal, "role-id", 1,
		[]string{"work_record.read"}, "reason",
	); !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("stale error=%v", err)
	}
	if _, err := service.ReplaceCapabilities(
		context.Background(), principal, "role-id", 2,
		[]string{"work_record.read"}, "",
	); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("reason error=%v", err)
	}
}

func TestRoleManagementCreatesReasonedCustomRole(t *testing.T) {
	repository := &roleRepositoryStub{}
	service := NewRoleManagementService(
		repository,
		func() time.Time {
			return time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
		},
		func() string { return "generated-id" },
	)
	found, err := service.Create(
		context.Background(),
		Principal{
			ID: "admin", Scope: scope.Principal{MSPID: "msp"},
			Capabilities: NewCapabilitySet("role.manage"),
		},
		CreateRoleCommand{
			Key: "service_lead", Name: "Service lead",
			Capabilities: []string{"timesheet.review", "time_entry.approve"},
			Reason:       "Delegate review", ActorID: "admin", Source: "api",
		},
	)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if found.Key != "service_lead" || found.SystemRole ||
		found.Version != 1 || repository.audit.Reason != "Delegate review" {
		t.Fatalf("role=%+v audit=%+v", found, repository.audit)
	}
}

func TestAssignTimeReviewerPreservesClientScope(t *testing.T) {
	repository := &roleRepositoryStub{roles: []Role{{
		ID: "review-role", Key: "time_reviewer", Name: "Time reviewer",
		SystemRole: true, Version: 1,
	}}}
	service := NewRoleManagementService(
		repository,
		func() time.Time {
			return time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
		},
		func() string { return "assignment-id" },
	)
	assignment, err := service.Assign(
		context.Background(),
		Principal{
			ID: "admin", Scope: scope.Principal{MSPID: "msp"},
			Capabilities: NewCapabilitySet("role.manage"),
		},
		AssignRoleCommand{
			PrincipalID: "reviewer", RoleKey: "time_reviewer",
			ClientID: "client-1", Reason: "review client time",
			ActorID: "admin", Source: "api",
		},
	)
	if err != nil {
		t.Fatalf("Assign() error = %v", err)
	}
	if assignment.ClientID != "client-1" ||
		assignment.RoleID != "review-role" ||
		assignment.TechnicianID != "reviewer" ||
		repository.audit.Reason != "review client time" {
		t.Fatalf(
			"assignment=%+v audit=%+v",
			assignment,
			repository.audit,
		)
	}
}

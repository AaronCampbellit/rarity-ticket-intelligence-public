package organizations

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type membershipRepository struct {
	called   bool
	accepted TeamMembershipMutation
	err      error
}

func (r *membershipRepository) CreateDepartmentAtomic(context.Context, DirectoryMutation) error {
	return nil
}
func (r *membershipRepository) CreateTeamAtomic(context.Context, DirectoryMutation) error {
	return nil
}
func (r *membershipRepository) CreateQueueAtomic(context.Context, DirectoryMutation) error {
	return nil
}
func (r *membershipRepository) ListDirectory(context.Context, scope.Target) (Directory, error) {
	return Directory{}, nil
}
func (r *membershipRepository) ReplaceTeamMembersAtomic(_ context.Context, accepted TeamMembershipMutation) error {
	r.called = true
	r.accepted = accepted
	return r.err
}

func TestReplaceTeamMembersRequiresMSPGlobalOrganizationManage(t *testing.T) {
	for _, test := range []struct {
		name      string
		principal authorization.Principal
		want      error
	}{
		{
			name: "client scoped",
			principal: membershipPrincipal(
				scope.Principal{MSPID: "msp-1", ClientID: "client-1"},
				"organization.manage",
			),
			want: ErrForbidden,
		},
		{
			name:      "missing capability",
			principal: membershipPrincipal(scope.Principal{MSPID: "msp-1"}),
			want:      authorization.ErrForbidden,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &membershipRepository{}
			service := NewDirectoryService(repository, time.Now, func() string { return "id" })
			_, err := service.ReplaceTeamMembers(context.Background(), ReplaceTeamMembersCommand{
				Principal: test.principal, TeamID: "team-1",
				TechnicianIDs: []string{"tech-1"}, ExpectedVersion: 1,
				Reason: "Staffing change", Source: "organization.settings",
			})
			if !errors.Is(err, test.want) || repository.called {
				t.Fatalf("error=%v want=%v called=%v", err, test.want, repository.called)
			}
		})
	}
}

func TestReplaceTeamMembersNormalizesAndAuditsSnapshot(t *testing.T) {
	at := time.Date(2026, time.August, 8, 9, 30, 0, 0, time.UTC)
	ids := []string{"audit-1", "event-1", "correlation-1"}
	repository := &membershipRepository{}
	service := NewDirectoryService(repository, func() time.Time { return at }, func() string {
		value := ids[0]
		ids = ids[1:]
		return value
	})
	team, err := service.ReplaceTeamMembers(context.Background(), ReplaceTeamMembersCommand{
		Principal: membershipPrincipal(scope.Principal{MSPID: "msp-1"}, "organization.manage"),
		TeamID:    " team-1 ", TechnicianIDs: []string{" tech-2 ", "tech-1", "tech-2"},
		ExpectedVersion: 1, Reason: " Staffing change ", Source: " organization.settings ",
	})
	if err != nil {
		t.Fatalf("ReplaceTeamMembers() error=%v", err)
	}
	wantMembers := []string{"tech-1", "tech-2"}
	if team.ID != "team-1" || team.MSPID != "msp-1" || team.Version != 2 ||
		!reflect.DeepEqual(team.MemberIDs, wantMembers) {
		t.Fatalf("team=%+v want version 2 members %v", team, wantMembers)
	}
	accepted := repository.accepted
	if !repository.called || !reflect.DeepEqual(accepted.TechnicianIDs, wantMembers) ||
		!accepted.ChangedAt.Equal(at) || accepted.ChangedBy != "actor-1" {
		t.Fatalf("accepted=%+v", accepted)
	}
	if accepted.Audit.ID != "audit-1" || accepted.Audit.Action != "team.members.replaced" ||
		accepted.Audit.SubjectType != "team" || accepted.Audit.SubjectID != "team-1" ||
		accepted.Audit.SubjectVersion != 2 || accepted.Audit.Reason != "Staffing change" ||
		accepted.Audit.Source != "organization.settings" || accepted.Audit.ClientID != "" {
		t.Fatalf("audit=%+v", accepted.Audit)
	}
	if accepted.Event.EventID != "event-1" || accepted.Event.EventType != "team.members.replaced" ||
		accepted.Event.SubjectVersion != 2 || accepted.Event.CorrelationID != "correlation-1" ||
		accepted.Event.ClientID != "" || accepted.Event.Data["member_count"] != 2 ||
		!reflect.DeepEqual(accepted.Event.Data["member_ids"], wantMembers) {
		t.Fatalf("event=%+v", accepted.Event)
	}
}

func TestReplaceTeamMembersRejectsInvalidSnapshots(t *testing.T) {
	tooMany := make([]string, 501)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("tech-%03d", index)
	}
	valid := ReplaceTeamMembersCommand{
		Principal: membershipPrincipal(scope.Principal{MSPID: "msp-1"}, "organization.manage"),
		TeamID:    "team-1", TechnicianIDs: []string{"tech-1"}, ExpectedVersion: 1,
		Reason: "Staffing change", Source: "organization.settings",
	}
	for _, test := range []struct {
		name   string
		change func(*ReplaceTeamMembersCommand)
	}{
		{"missing team", func(command *ReplaceTeamMembersCommand) { command.TeamID = " " }},
		{"zero version", func(command *ReplaceTeamMembersCommand) { command.ExpectedVersion = 0 }},
		{"missing reason", func(command *ReplaceTeamMembersCommand) { command.Reason = " " }},
		{"missing source", func(command *ReplaceTeamMembersCommand) { command.Source = " " }},
		{"empty technician", func(command *ReplaceTeamMembersCommand) { command.TechnicianIDs = []string{" "} }},
		{"over member limit", func(command *ReplaceTeamMembersCommand) { command.TechnicianIDs = tooMany }},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := valid
			test.change(&command)
			repository := &membershipRepository{}
			service := NewDirectoryService(repository, time.Now, func() string { return "id" })
			if _, err := service.ReplaceTeamMembers(context.Background(), command); !errors.Is(err, ErrInvalid) || repository.called {
				t.Fatalf("error=%v called=%v", err, repository.called)
			}
		})
	}
}

func membershipPrincipal(principalScope scope.Principal, capabilities ...string) authorization.Principal {
	return authorization.Principal{
		ID: "actor-1", Scope: principalScope,
		Capabilities: authorization.NewCapabilitySet(capabilities...),
	}
}

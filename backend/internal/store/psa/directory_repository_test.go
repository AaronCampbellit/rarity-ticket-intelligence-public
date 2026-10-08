package psa

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
)

func TestDirectoryRepositoryCreatesHierarchyWithFactsAtomically(t *testing.T) {
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name      string
		mutation  organizations.DirectoryMutation
		create    func(*DirectoryRepository, context.Context, organizations.DirectoryMutation) error
		table     string
		reference string
	}{
		{
			name: "department", table: "departments",
			mutation: directoryMutation(at, "department", "department.created"),
			create: func(repository *DirectoryRepository, ctx context.Context, mutation organizations.DirectoryMutation) error {
				mutation.Department = organizations.Department{
					ID: "department", MSPID: "msp", Key: "service-desk",
					Name: "Service Desk", Version: 1,
				}
				return repository.CreateDepartmentAtomic(ctx, mutation)
			},
		},
		{
			name: "team", table: "teams", reference: "departments",
			mutation: directoryMutation(at, "team", "team.created"),
			create: func(repository *DirectoryRepository, ctx context.Context, mutation organizations.DirectoryMutation) error {
				mutation.Team = organizations.Team{
					ID: "team", MSPID: "msp", DepartmentID: "department",
					Key: "tier-one", Name: "Tier One", Version: 1,
				}
				return repository.CreateTeamAtomic(ctx, mutation)
			},
		},
		{
			name: "queue", table: "queues", reference: "teams",
			mutation: directoryMutation(at, "queue", "queue.created"),
			create: func(repository *DirectoryRepository, ctx context.Context, mutation organizations.DirectoryMutation) error {
				mutation.Queue = organizations.Queue{
					ID: "queue", MSPID: "msp", ClientID: "client",
					DepartmentID: "department", TeamID: "team",
					Key: "triage", Name: "Triage", Version: 1,
				}
				return repository.CreateQueueAtomic(ctx, mutation)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeSalesTx{}
			err := test.create(
				NewDirectoryRepository(&fakeSalesDB{tx: tx}),
				context.Background(),
				test.mutation,
			)
			if err != nil {
				t.Fatalf("create directory record: %v", err)
			}
			assertQueryOrder(
				t, tx.queries,
				"INSERT INTO "+test.table,
				"INSERT INTO audit_ledger",
				"INSERT INTO event_outbox",
			)
			if test.reference != "" &&
				!strings.Contains(tx.queries[0], test.reference) {
				t.Fatalf("directory insert does not validate %s: %s", test.reference, tx.queries[0])
			}
		})
	}
}

func TestDirectoryRepositoryListsGlobalHierarchyAndScopedQueues(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			values := []any{
				"client", "client", "msp", "client", "", "",
				"CLIENT-1", "Northwind", int64(2),
			}
			for index, value := range values {
				setScanDestination(destinations[index], value)
			}
		},
	}}}
	directory, err := NewDirectoryRepository(db).ListDirectory(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
	)
	if err != nil || len(directory.Clients) != 1 ||
		directory.Clients[0].DisplayID != "CLIENT-1" ||
		directory.Clients[0].Name != "Northwind" {
		t.Fatalf("directory=%+v error=%v", directory, err)
	}
	for _, fragment := range []string{
		"FROM client_organizations", "FROM departments", "FROM teams", "FROM queues",
		"client_id IS NULL OR client_id = NULLIF($2, '')::uuid",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("directory query missing %q: %s", fragment, db.query)
		}
	}
}

func TestDirectoryRepositoryListsGlobalMembershipChoicesWithoutClientDisclosure(t *testing.T) {
	globalDB := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			values := []any{
				"team", "team", "msp", "", "department", "",
				"tier-one", "Tier One", int64(4), []string{"tech-1"}, "", "",
			}
			for index, value := range values {
				setScanDestination(destinations[index], value)
			}
		},
		func(destinations ...any) {
			values := []any{
				"technician", "tech-1", "msp", "", "", "",
				"alex@example.test", "Alex Morgan", int64(2), []string{},
				"alex@example.test", "Alex Morgan",
			}
			for index, value := range values {
				setScanDestination(destinations[index], value)
			}
		},
	}}}
	global, err := NewDirectoryRepository(globalDB).ListDirectory(
		context.Background(), scope.Target{MSPID: "msp"},
	)
	if err != nil || len(global.Teams) != 1 || len(global.Technicians) != 1 ||
		!reflect.DeepEqual(global.Teams[0].MemberIDs, []string{"tech-1"}) ||
		global.Technicians[0].DisplayName != "Alex Morgan" {
		t.Fatalf("global directory=%+v error=%v", global, err)
	}
	for _, fragment := range []string{
		"FROM team_memberships", "lifecycle_state = 'active'",
		"'technician' AS kind", "$2 = ''",
	} {
		if !strings.Contains(globalDB.query, fragment) {
			t.Fatalf("global directory query missing %q: %s", fragment, globalDB.query)
		}
	}
}

func TestReplaceTeamMembersAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for team membership verification")
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

	mspID, otherMSPID, clientID := id.New(), id.New(), id.New()
	actorID, technicianOne, technicianTwo := id.New(), id.New(), id.New()
	if technicianOne > technicianTwo {
		technicianOne, technicianTwo = technicianTwo, technicianOne
	}
	inactiveTechnician, crossMSPTechnician := id.New(), id.New()
	departmentID, teamID, roleID, assignmentID := id.New(), id.New(), id.New(), id.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, queryErr := pool.Exec(ctx, query, args...); queryErr != nil {
			t.Fatalf("fixture: %v", queryErr)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'Membership MSP',$3,$3),($4,$5,'Other MSP',$3,$3)`, mspID, "MEMBERS-"+mspID, actorID, otherMSPID, "OTHER-"+otherMSPID)
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$2,$3,'Membership Client',$4,$4)`, clientID, mspID, "CLIENT-"+clientID, actorID)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name,lifecycle_state) VALUES
($1,$2,$3,'Membership administrator','active'),
($4,$2,$5,'Alex Morgan','active'),
($6,$2,$7,'Sam Rivera','active'),
($8,$2,$9,'Inactive User','inactive'),
($10,$11,$12,'Other MSP User','active')`, actorID, mspID, actorID+"@example.test", technicianOne, technicianOne+"@example.test", technicianTwo, technicianTwo+"@example.test", inactiveTechnician, inactiveTechnician+"@example.test", crossMSPTechnician, otherMSPID, crossMSPTechnician+"@example.test")
	exec(`INSERT INTO departments(id,msp_id,key,name) VALUES($1,$2,'service','Service')`, departmentID, mspID)
	exec(`INSERT INTO teams(id,msp_id,department_id,key,name) VALUES($1,$2,$3,'tier-one','Tier One')`, teamID, mspID, departmentID)
	exec(`INSERT INTO roles(id,msp_id,key,name,created_at,updated_at) VALUES($1,$2,'member-role','Member role',now(),now())`, roleID, mspID)
	exec(`INSERT INTO role_assignments(id,msp_id,technician_id,role_id,granted_by) VALUES($1,$2,$3,$4,$5)`, assignmentID, mspID, technicianOne, roleID, actorID)

	service := organizations.NewDirectoryService(
		NewDirectoryRepositoryFromPool(pool), time.Now, id.New,
	)
	principal := authorization.Principal{
		ID: actorID, Scope: scope.Principal{MSPID: mspID},
		Capabilities: authorization.NewCapabilitySet("organization.manage", "organization.read"),
	}
	replaced, err := service.ReplaceTeamMembers(ctx, organizations.ReplaceTeamMembersCommand{
		Principal: principal, TeamID: teamID,
		TechnicianIDs:   []string{technicianTwo, technicianOne, technicianTwo},
		ExpectedVersion: 1, Reason: "Staffing change", Source: "organization.settings",
	})
	if err != nil || replaced.Version != 2 ||
		!reflect.DeepEqual(replaced.MemberIDs, []string{technicianOne, technicianTwo}) {
		t.Fatalf("initial replacement=%+v error=%v", replaced, err)
	}
	var activeMembers []string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(array_agg(technician_id::text ORDER BY technician_id::text),'{}') FROM team_memberships WHERE team_id=$1 AND lifecycle_state='active'`, teamID).Scan(&activeMembers); err != nil || !reflect.DeepEqual(activeMembers, []string{technicianOne, technicianTwo}) {
		t.Fatalf("active members=%v error=%v", activeMembers, err)
	}
	var roleAssignments, auditCount, eventCount int
	var eventID string
	var eventMemberIDs []string
	var eventMemberCount int
	var unexpectedEventFields bool
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM role_assignments WHERE id=$1 AND technician_id=$2 AND role_id=$3`, assignmentID, technicianOne, roleID).Scan(&roleAssignments); err != nil || roleAssignments != 1 {
		t.Fatalf("role assignments=%d error=%v", roleAssignments, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_ledger WHERE msp_id=$1 AND action='team.members.replaced' AND subject_id=$2 AND subject_version=2`, mspID, teamID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("audit count=%d error=%v", auditCount, err)
	}
	if err := pool.QueryRow(ctx, `SELECT event_id::text,ARRAY(SELECT jsonb_array_elements_text(data->'member_ids') ORDER BY 1),(data->>'member_count')::int,(data - 'member_ids' - 'member_count') <> '{}'::jsonb FROM event_outbox WHERE msp_id=$1 AND event_type='team.members.replaced' AND subject_id=$2 AND subject_version=2`, mspID, teamID).Scan(&eventID, &eventMemberIDs, &eventMemberCount, &unexpectedEventFields); err != nil || !reflect.DeepEqual(eventMemberIDs, []string{technicianOne, technicianTwo}) || eventMemberCount != 2 || unexpectedEventFields {
		t.Fatalf("event id=%s members=%v count=%d unexpected=%v error=%v", eventID, eventMemberIDs, eventMemberCount, unexpectedEventFields, err)
	}

	for name, memberID := range map[string]string{
		"inactive":  inactiveTechnician,
		"cross MSP": crossMSPTechnician,
	} {
		t.Run(name, func(t *testing.T) {
			_, replaceErr := service.ReplaceTeamMembers(ctx, organizations.ReplaceTeamMembersCommand{
				Principal: principal, TeamID: teamID, TechnicianIDs: []string{memberID},
				ExpectedVersion: 2, Reason: "Invalid member", Source: "organization.settings",
			})
			if !errors.Is(replaceErr, scope.ErrNotFound) {
				t.Fatalf("error=%v want ErrNotFound", replaceErr)
			}
		})
	}
	if _, err := service.ReplaceTeamMembers(ctx, organizations.ReplaceTeamMembersCommand{
		Principal: principal, TeamID: teamID, TechnicianIDs: []string{technicianOne},
		ExpectedVersion: 1, Reason: "Stale change", Source: "organization.settings",
	}); !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("stale error=%v want ErrVersionConflict", err)
	}

	rollbackAuditID := id.New()
	rollbackMutation := organizations.TeamMembershipMutation{
		Team:          organizations.Team{ID: teamID, MSPID: mspID, Version: 3, MemberIDs: []string{technicianTwo}},
		TechnicianIDs: []string{technicianTwo}, ChangedAt: time.Now().UTC(), ChangedBy: actorID,
		Audit: mutation.AuditRecord{ID: rollbackAuditID, OccurredAt: time.Now().UTC(), MSPID: mspID, ActorType: "technician", ActorID: actorID, Action: "team.members.replaced", SubjectType: "team", SubjectID: teamID, SubjectVersion: 3, Source: "organization.settings", Reason: "Rollback proof", CorrelationID: id.New()},
		Event: mutation.EventRecord{EventID: eventID, EventType: "team.members.replaced", SchemaVersion: 1, OccurredAt: time.Now().UTC(), MSPID: mspID, ActorType: "technician", ActorID: actorID, SubjectType: "team", SubjectID: teamID, SubjectVersion: 3, Source: "organization.settings", CorrelationID: id.New(), Data: map[string]any{"member_ids": []string{technicianTwo}, "member_count": 1}},
	}
	rollbackMutation.Event.CorrelationID = rollbackMutation.Audit.CorrelationID
	if err := NewDirectoryRepositoryFromPool(pool).ReplaceTeamMembersAtomic(ctx, rollbackMutation); err == nil {
		t.Fatal("duplicate outbox event unexpectedly committed")
	}
	var teamVersion, rollbackAuditCount int
	if err := pool.QueryRow(ctx, `SELECT version FROM teams WHERE id=$1`, teamID).Scan(&teamVersion); err != nil || teamVersion != 2 {
		t.Fatalf("team version after rollback=%d error=%v", teamVersion, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_ledger WHERE id=$1`, rollbackAuditID).Scan(&rollbackAuditCount); err != nil || rollbackAuditCount != 0 {
		t.Fatalf("rollback audit count=%d error=%v", rollbackAuditCount, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE msp_id=$1 AND event_type='team.members.replaced'`, mspID).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("event count after rejected changes=%d error=%v", eventCount, err)
	}

	replaced, err = service.ReplaceTeamMembers(ctx, organizations.ReplaceTeamMembersCommand{
		Principal: principal, TeamID: teamID, TechnicianIDs: []string{technicianTwo},
		ExpectedVersion: 2, Reason: "Move Alex", Source: "organization.settings",
	})
	if err != nil || replaced.Version != 3 {
		t.Fatalf("removal replacement=%+v error=%v", replaced, err)
	}
	var firstState, secondState string
	if err := pool.QueryRow(ctx, `SELECT lifecycle_state FROM team_memberships WHERE team_id=$1 AND technician_id=$2`, teamID, technicianOne).Scan(&firstState); err != nil || firstState != "inactive" {
		t.Fatalf("removed state=%q error=%v", firstState, err)
	}
	if err := pool.QueryRow(ctx, `SELECT lifecycle_state FROM team_memberships WHERE team_id=$1 AND technician_id=$2`, teamID, technicianTwo).Scan(&secondState); err != nil || secondState != "active" {
		t.Fatalf("retained state=%q error=%v", secondState, err)
	}

	globalDirectory, err := NewDirectoryRepositoryFromPool(pool).ListDirectory(ctx, scope.Target{MSPID: mspID})
	if err != nil || len(globalDirectory.Teams) != 1 ||
		!reflect.DeepEqual(globalDirectory.Teams[0].MemberIDs, []string{technicianTwo}) ||
		len(globalDirectory.Technicians) != 3 {
		t.Fatalf("global directory=%+v error=%v", globalDirectory, err)
	}
	clientDirectory, err := NewDirectoryRepositoryFromPool(pool).ListDirectory(ctx, scope.Target{MSPID: mspID, ClientID: clientID})
	if err != nil || len(clientDirectory.Technicians) != 0 || len(clientDirectory.Teams) != 1 || len(clientDirectory.Teams[0].MemberIDs) != 0 {
		t.Fatalf("client directory disclosed membership data: %+v error=%v", clientDirectory, err)
	}
}

func directoryMutation(
	at time.Time,
	subjectID, action string,
) organizations.DirectoryMutation {
	return organizations.DirectoryMutation{
		Audit: mutation.AuditRecord{
			ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor", Action: action,
			SubjectType: subjectID, SubjectID: subjectID, SubjectVersion: 1,
			Source: "api", Reason: "initial structure", CorrelationID: "correlation",
		},
		Event: mutation.EventRecord{
			EventID: "event", EventType: action, SchemaVersion: 1,
			OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor",
			SubjectType: subjectID, SubjectID: subjectID, SubjectVersion: 1,
			Source: "api", CorrelationID: "correlation",
		},
	}
}

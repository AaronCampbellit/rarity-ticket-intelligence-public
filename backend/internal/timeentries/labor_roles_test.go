package timeentries

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type laborRoleRepositoryStub struct {
	created   LaborRoleMutation
	versioned LaborRoleVersionMutation
	snapshot  RateSnapshot
	roles     []LaborRole
	resolve   struct {
		mspID, laborRoleID, technicianID string
		at                               time.Time
	}
	err error
}

func (r *laborRoleRepositoryStub) ListLaborRoles(
	context.Context,
	string,
	time.Time,
) ([]LaborRole, error) {
	return r.roles, r.err
}

func (r *laborRoleRepositoryStub) ListLaborRolesForManagement(
	context.Context,
	string,
) ([]LaborRole, error) {
	return r.roles, r.err
}

func (r *laborRoleRepositoryStub) CreateLaborRoleAtomic(
	_ context.Context,
	mutation LaborRoleMutation,
) error {
	r.created = mutation
	return r.err
}

func (r *laborRoleRepositoryStub) VersionLaborRoleAtomic(
	_ context.Context,
	mutation LaborRoleVersionMutation,
) error {
	r.versioned = mutation
	return r.err
}

func (r *laborRoleRepositoryStub) ResolveLaborRate(
	_ context.Context,
	mspID, laborRoleID, technicianID string,
	at time.Time,
) (RateSnapshot, error) {
	r.resolve.mspID = mspID
	r.resolve.laborRoleID = laborRoleID
	r.resolve.technicianID = technicianID
	r.resolve.at = at
	return r.snapshot, r.err
}

func TestResolveLaborRateUsesEffectiveRoleAndTechnicianOverride(t *testing.T) {
	at := time.Date(2026, time.August, 4, 14, 0, 0, 0, time.FixedZone("CDT", -5*60*60))
	repository := &laborRoleRepositoryStub{snapshot: RateSnapshot{
		LaborRoleVersionID: "role-v2",
		InternalCostMinor:  4500,
		BillRateMinor:      15000,
		Currency:           "USD",
	}}
	service := NewLaborRoleService(repository, time.Now, func() string { return "id" })
	got, err := service.ResolveLaborRate(
		context.Background(),
		authorization.Principal{
			ID: "tech",
			Scope: scope.Principal{
				MSPID: "msp", ClientID: "client",
			},
			Capabilities: authorization.NewCapabilitySet("time_entry.create"),
		},
		"role",
		"tech",
		at,
	)
	if err != nil {
		t.Fatalf("ResolveLaborRate() error = %v", err)
	}
	if got.LaborRoleVersionID != "role-v2" ||
		got.InternalCostMinor != 4500 ||
		got.BillRateMinor != 15000 ||
		got.Currency != "USD" {
		t.Fatalf("ResolveLaborRate() = %+v", got)
	}
	if repository.resolve.mspID != "msp" ||
		repository.resolve.laborRoleID != "role" ||
		repository.resolve.technicianID != "tech" ||
		!repository.resolve.at.Equal(at.UTC()) {
		t.Fatalf("repository resolve = %+v", repository.resolve)
	}
}

func TestListLaborRolesReturnsCurrentSelectableVersions(t *testing.T) {
	repository := &laborRoleRepositoryStub{roles: []LaborRole{{
		ID: "role", MSPID: "msp", Key: "senior_engineer", Version: 2,
		CurrentVersion: LaborRoleVersion{
			ID: "role-v2", Name: "Senior Engineer", Enabled: true,
		},
	}}}
	found, err := NewLaborRoleService(repository, time.Now, idSequence()).
		List(context.Background(), authorization.Principal{
			ID: "tech", Scope: scope.Principal{
				MSPID: "msp", ClientID: "client",
			},
			Capabilities: authorization.NewCapabilitySet("time_entry.create"),
		})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(found) != 1 || found[0].CurrentVersion.ID != "role-v2" {
		t.Fatalf("List() = %+v", found)
	}
}

func TestListLaborRolesForManagementIncludesDisabledVersions(t *testing.T) {
	repository := &laborRoleRepositoryStub{roles: []LaborRole{{
		ID: "role", MSPID: "msp", Key: "legacy", Version: 3,
		CurrentVersion: LaborRoleVersion{
			ID: "role-v3", Name: "Legacy", Enabled: false,
		},
	}}}
	found, err := NewLaborRoleService(repository, time.Now, idSequence()).
		ListForManagement(
			context.Background(),
			authorization.Principal{
				ID: "admin", Scope: scope.Principal{MSPID: "msp"},
				Capabilities: authorization.NewCapabilitySet(
					"organization.manage",
				),
			},
		)
	if err != nil || len(found) != 1 ||
		found[0].CurrentVersion.Enabled {
		t.Fatalf("ListForManagement()=%+v err=%v", found, err)
	}
}

func TestCreateLaborRoleNormalizesValuesAndWritesMutationFacts(t *testing.T) {
	now := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	next := 0
	repository := &laborRoleRepositoryStub{}
	service := NewLaborRoleService(repository, func() time.Time { return now }, func() string {
		next++
		return []string{"role", "version", "audit", "event", "correlation"}[next-1]
	})
	found, err := service.Create(
		context.Background(),
		CreateLaborRoleCommand{
			Principal: authorization.Principal{
				ID:           "admin",
				Scope:        scope.Principal{MSPID: "msp"},
				Capabilities: authorization.NewCapabilitySet("organization.manage"),
			},
			Key:               "  Senior Engineer ",
			Name:              " Senior Engineer ",
			InternalCostMinor: 7000,
			BillRateMinor:     18000,
			Currency:          " usd ",
			EffectiveFrom:     now.Add(time.Hour),
			ActorID:           "admin",
			Source:            "api",
		},
	)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if found.Key != "senior_engineer" ||
		found.CurrentVersion.Name != "Senior Engineer" ||
		found.CurrentVersion.Currency != "USD" {
		t.Fatalf("Create() = %+v", found)
	}
	if repository.created.Role.ID != "role" ||
		repository.created.Version.ID != "version" ||
		repository.created.Audit.Action != "labor_role.created" ||
		repository.created.Event.EventType != "labor_role.created" ||
		repository.created.Audit.CorrelationID != repository.created.Event.CorrelationID {
		t.Fatalf("mutation = %+v", repository.created)
	}
}

func TestVersionLaborRoleRequiresReasonAndAdvancesOptimisticVersion(t *testing.T) {
	now := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	next := 0
	repository := &laborRoleRepositoryStub{roles: []LaborRole{
		{ID: "role", MSPID: "msp", Key: "principal_engineer", Version: 3},
	}}
	service := NewLaborRoleService(repository, func() time.Time { return now }, func() string {
		next++
		return []string{"version", "audit", "event", "correlation"}[next-1]
	})
	found, err := service.Version(
		context.Background(),
		VersionLaborRoleCommand{
			Principal: authorization.Principal{
				ID:           "admin",
				Scope:        scope.Principal{MSPID: "msp"},
				Capabilities: authorization.NewCapabilitySet("organization.manage"),
			},
			LaborRoleID:       "role",
			ExpectedVersion:   3,
			Name:              "Principal Engineer",
			InternalCostMinor: 8000,
			BillRateMinor:     20000,
			Currency:          "usd",
			EffectiveFrom:     now.Add(24 * time.Hour),
			Enabled:           true,
			Reason:            "annual rate review",
			ActorID:           "admin",
			Source:            "api",
		},
	)
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if found.Key != "principal_engineer" ||
		found.Version != 4 ||
		found.CurrentVersion.ID != "version" ||
		repository.versioned.Audit.Reason != "annual rate review" ||
		repository.versioned.Audit.SubjectVersion != 4 {
		t.Fatalf("Version() = %+v mutation=%+v", found, repository.versioned)
	}
}

func TestResolveLaborRateRejectsMissingPermissionBeforeRepository(t *testing.T) {
	repository := &laborRoleRepositoryStub{err: errors.New("repository called")}
	_, err := NewLaborRoleService(repository, time.Now, func() string { return "id" }).
		ResolveLaborRate(
			context.Background(),
			authorization.Principal{
				ID:    "tech",
				Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
			},
			"role",
			"tech",
			time.Now(),
		)
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("ResolveLaborRate() error = %v", err)
	}
	if repository.resolve.mspID != "" {
		t.Fatalf("repository called: %+v", repository.resolve)
	}
}

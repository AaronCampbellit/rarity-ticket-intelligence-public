package projects

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

const (
	milestoneActorID   = "00000000-0000-4000-8000-000000000101"
	milestoneMSPID     = "00000000-0000-4000-8000-000000000102"
	milestoneClientID  = "00000000-0000-4000-8000-000000000103"
	milestoneProjectID = "00000000-0000-4000-8000-000000000104"
	milestonePhaseID   = "00000000-0000-4000-8000-000000000105"
	milestoneOwnerID   = "00000000-0000-4000-8000-000000000106"
)

type milestoneRepositoryStub struct {
	project  Project
	phase    Phase
	mutation MilestoneMutation
	current  Milestone
	err      error
}

func (r *milestoneRepositoryStub) FindMilestone(context.Context, scope.Target, string) (Milestone, error) {
	return r.current, r.err
}

func (r *milestoneRepositoryStub) FindMilestoneProject(context.Context, scope.Target, ProjectID) (Project, error) {
	return r.project, r.err
}
func (r *milestoneRepositoryStub) FindMilestonePhase(context.Context, scope.Target, ProjectID, PhaseID) (Phase, error) {
	return r.phase, r.err
}
func (r *milestoneRepositoryStub) CreateMilestoneAtomic(_ context.Context, m MilestoneMutation) error {
	r.mutation = m
	return r.err
}
func (r *milestoneRepositoryStub) UpdateMilestoneAtomic(_ context.Context, m MilestoneMutation, _ int64) error {
	r.mutation = m
	return r.err
}

func milestonePrincipal(capability, msp, client string) authorization.Principal {
	return authorization.Principal{ID: milestoneActorID, Scope: scope.Principal{MSPID: msp, ClientID: client}, Capabilities: authorization.NewCapabilitySet(capability)}
}

func TestCreateMilestoneRequiresProjectClientAndWritesMutation(t *testing.T) {
	repository := &milestoneRepositoryStub{project: Project{ID: milestoneProjectID, MSPID: milestoneMSPID, ClientID: milestoneClientID}}
	service := NewMilestoneService(repository, func() time.Time { return time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC) }, sequenceIDs())
	found, err := service.Create(context.Background(), CreateMilestoneCommand{
		Principal: milestonePrincipal("project.edit", milestoneMSPID, milestoneClientID), ProjectID: milestoneProjectID, Name: "Cutover",
		DueOn: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC), OwnerID: milestoneOwnerID, ActorID: milestoneActorID, Source: "api", IdempotencyKey: "create",
	})
	if err != nil {
		t.Fatal(err)
	}
	if found.ClientID != milestoneClientID || repository.mutation.Event.EventType != "project.milestone.created" {
		t.Fatalf("unexpected mutation: %+v", repository.mutation)
	}
	if repository.mutation.Audit.CorrelationID == "" || repository.mutation.Audit.CorrelationID != repository.mutation.Event.CorrelationID {
		t.Fatal("mutation facts are not correlated")
	}
}

func TestCreateMilestoneRejectsPhaseOutsideProject(t *testing.T) {
	repository := &milestoneRepositoryStub{project: Project{ID: milestoneProjectID, MSPID: milestoneMSPID, ClientID: milestoneClientID}, phase: Phase{ID: milestonePhaseID, ProjectID: ProjectID("00000000-0000-4000-8000-000000000107"), MSPID: milestoneMSPID, ClientID: milestoneClientID}}
	service := NewMilestoneService(repository, time.Now, sequenceIDs())
	_, err := service.Create(context.Background(), CreateMilestoneCommand{Principal: milestonePrincipal("project.edit", milestoneMSPID, milestoneClientID), ProjectID: milestoneProjectID, PhaseID: milestonePhaseID, Name: "Cutover", DueOn: time.Now(), ActorID: milestoneActorID, Source: "api", IdempotencyKey: "phase"})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("error = %v", err)
	}
}

func sequenceIDs() func() string {
	n := 0
	return func() string { n++; return fmt.Sprintf("00000000-0000-4000-8002-%012d", n) }
}

package projects

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidResourcePlan = errors.New("invalid resource plan")

type ResourcePlanMutation struct {
	Plan  ResourcePlan
	Audit mutation.AuditRecord
	Event mutation.EventRecord
}

type ResourceRepository interface {
	CreateResourcePlanAtomic(context.Context, ResourcePlanMutation) error
}

type PlanResourceCommand struct {
	Principal      authorization.Principal
	Target         scope.Target
	ProjectID      ProjectID
	PhaseID        PhaseID
	RoleID         string
	TeamID         string
	StartsOn       time.Time
	EndsOn         time.Time
	PlannedMinutes int64
	ActorID        string
	Source         string
}

type ResourceService struct {
	repository ResourceRepository
	now        func() time.Time
	newID      func() string
}

func NewResourceService(repository ResourceRepository, now func() time.Time, newID func() string) *ResourceService {
	return &ResourceService{repository: repository, now: now, newID: newID}
}

func (s *ResourceService) Plan(ctx context.Context, command PlanResourceCommand) (ResourcePlan, error) {
	target := projectTarget(command.Principal, command.Target)
	resourceCount := nonemptyCount(command.RoleID, command.TeamID)
	if target.ClientID == "" || command.ProjectID == "" || command.PhaseID == "" ||
		command.StartsOn.IsZero() || command.EndsOn.Before(command.StartsOn) ||
		command.PlannedMinutes < 0 || resourceCount != 1 ||
		strings.TrimSpace(command.ActorID) == "" || strings.TrimSpace(command.Source) == "" {
		return ResourcePlan{}, ErrInvalidResourcePlan
	}
	if err := authorization.Authorize(command.Principal, "project.resource.plan", target); err != nil {
		return ResourcePlan{}, err
	}

	plan := ResourcePlan{
		ID: s.newID(), ProjectID: command.ProjectID, PhaseID: command.PhaseID,
		MSPID: target.MSPID, ClientID: target.ClientID,
		RoleID: strings.TrimSpace(command.RoleID), TeamID: strings.TrimSpace(command.TeamID),
		StartsOn: command.StartsOn, EndsOn: command.EndsOn,
		PlannedMinutes: command.PlannedMinutes, Version: 1,
	}
	now := s.now().UTC()
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := ResourcePlanMutation{
		Plan: plan,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "resource_plan.created", SubjectType: "resource_plan", SubjectID: plan.ID,
			SubjectVersion: plan.Version, Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "resource_plan.created", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "resource_plan", SubjectID: plan.ID,
			SubjectVersion: plan.Version, Source: command.Source, CorrelationID: correlationID,
		},
	}
	if err := s.repository.CreateResourcePlanAtomic(ctx, accepted); err != nil {
		return ResourcePlan{}, err
	}
	return plan, nil
}

func nonemptyCount(values ...string) int {
	count := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}

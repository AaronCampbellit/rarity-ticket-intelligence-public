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

var ErrInvalidCostActual = errors.New("invalid project cost actual")

type CostActual struct {
	ID          string    `json:"id"`
	ProjectID   ProjectID `json:"project_id"`
	PhaseID     PhaseID   `json:"phase_id,omitempty"`
	MSPID       string    `json:"msp_id"`
	ClientID    string    `json:"client_id"`
	CostType    string    `json:"cost_type"`
	Description string    `json:"description"`
	Amount      Money     `json:"amount"`
	Committed   bool      `json:"committed"`
	IncurredAt  time.Time `json:"incurred_at"`
	Version     int64     `json:"version"`
}

type CreateCostActualCommand struct {
	Principal   authorization.Principal
	Target      scope.Target
	ProjectID   ProjectID
	PhaseID     PhaseID
	CostType    string
	Description string
	Amount      Money
	Committed   bool
	IncurredAt  time.Time
	ActorID     string
	Source      string
}

type CostActualMutation struct {
	Cost  CostActual
	Audit mutation.AuditRecord
	Event mutation.EventRecord
}

type CostActualRepository interface {
	FindProject(context.Context, scope.Target, ProjectID) (Project, error)
	FindPhase(context.Context, scope.Target, PhaseID) (Phase, error)
	CreateCostActualAtomic(context.Context, CostActualMutation) error
}

type CostActualService struct {
	repository CostActualRepository
	now        func() time.Time
	newID      func() string
}

func NewCostActualService(
	repository CostActualRepository,
	now func() time.Time,
	newID func() string,
) *CostActualService {
	return &CostActualService{repository: repository, now: now, newID: newID}
}

func (s *CostActualService) Create(
	ctx context.Context,
	command CreateCostActualCommand,
) (CostActual, error) {
	target := projectTarget(command.Principal, command.Target)
	costType := strings.TrimSpace(command.CostType)
	description := strings.TrimSpace(command.Description)
	currency := strings.ToUpper(strings.TrimSpace(command.Amount.Currency))
	if target.ClientID == "" || command.ProjectID == "" || costType == "" ||
		description == "" || len(currency) != 3 || command.Amount.Minor < 0 ||
		command.IncurredAt.IsZero() || command.ActorID == "" || command.Source == "" {
		return CostActual{}, ErrInvalidCostActual
	}
	if err := authorization.Authorize(command.Principal, "project.edit", target); err != nil {
		return CostActual{}, err
	}
	project, err := s.repository.FindProject(ctx, target, command.ProjectID)
	if err != nil {
		return CostActual{}, err
	}
	if project.MSPID != target.MSPID || project.ClientID != target.ClientID {
		return CostActual{}, scope.ErrNotFound
	}
	if command.PhaseID != "" {
		phase, err := s.repository.FindPhase(ctx, target, command.PhaseID)
		if err != nil {
			return CostActual{}, err
		}
		if phase.ProjectID != project.ID || phase.MSPID != target.MSPID ||
			phase.ClientID != target.ClientID {
			return CostActual{}, scope.ErrNotFound
		}
	}
	now := s.now().UTC()
	cost := CostActual{
		ID: s.newID(), ProjectID: project.ID, PhaseID: command.PhaseID,
		MSPID: target.MSPID, ClientID: target.ClientID,
		CostType: costType, Description: description,
		Amount:    Money{Minor: command.Amount.Minor, Currency: currency},
		Committed: command.Committed, IncurredAt: command.IncurredAt.UTC(), Version: 1,
	}
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := CostActualMutation{
		Cost: cost,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "project.cost_actual.created", SubjectType: "cost_actual",
			SubjectID: cost.ID, SubjectVersion: cost.Version,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "project.cost_actual.created", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "cost_actual", SubjectID: cost.ID,
			SubjectVersion: cost.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.CreateCostActualAtomic(ctx, accepted); err != nil {
		return CostActual{}, err
	}
	return cost, nil
}

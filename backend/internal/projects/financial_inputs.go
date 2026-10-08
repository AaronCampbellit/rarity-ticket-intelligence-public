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

var ErrInvalidFinancialInput = errors.New("invalid project financial input")

type TechnicianLaborCostRate struct {
	ID           string    `json:"id"`
	MSPID        string    `json:"msp_id"`
	TechnicianID string    `json:"technician_id"`
	HourlyRate   Money     `json:"hourly_rate"`
	EffectiveAt  time.Time `json:"effective_at"`
	Version      int64     `json:"version"`
}

type BillableWorkRecognition struct {
	ID           string    `json:"id"`
	ProjectID    ProjectID `json:"project_id"`
	PhaseID      PhaseID   `json:"phase_id,omitempty"`
	MSPID        string    `json:"msp_id"`
	ClientID     string    `json:"client_id"`
	Description  string    `json:"description"`
	Amount       Money     `json:"amount"`
	RecognizedAt time.Time `json:"recognized_at"`
	Version      int64     `json:"version"`
}

type CreateLaborCostRateCommand struct {
	Principal    authorization.Principal
	TechnicianID string
	HourlyRate   Money
	EffectiveAt  time.Time
	ActorID      string
	Source       string
}

type RecognizeBillableWorkCommand struct {
	Principal    authorization.Principal
	Target       scope.Target
	ProjectID    ProjectID
	PhaseID      PhaseID
	Description  string
	Amount       Money
	RecognizedAt time.Time
	ActorID      string
	Source       string
}

type LaborCostRateMutation struct {
	Rate  TechnicianLaborCostRate
	Audit mutation.AuditRecord
	Event mutation.EventRecord
}

type BillableWorkMutation struct {
	Recognition BillableWorkRecognition
	Audit       mutation.AuditRecord
	Event       mutation.EventRecord
}

type FinancialInputRepository interface {
	FindProject(context.Context, scope.Target, ProjectID) (Project, error)
	FindPhase(context.Context, scope.Target, PhaseID) (Phase, error)
	CreateLaborCostRateAtomic(context.Context, LaborCostRateMutation) error
	RecognizeBillableWorkAtomic(context.Context, BillableWorkMutation) error
}

type FinancialInputService struct {
	repository FinancialInputRepository
	now        func() time.Time
	newID      func() string
}

func NewFinancialInputService(
	repository FinancialInputRepository,
	now func() time.Time,
	newID func() string,
) *FinancialInputService {
	return &FinancialInputService{
		repository: repository, now: now, newID: newID,
	}
}

func (s *FinancialInputService) CreateLaborCostRate(
	ctx context.Context,
	command CreateLaborCostRateCommand,
) (TechnicianLaborCostRate, error) {
	target := scope.Target{MSPID: command.Principal.Scope.MSPID}
	technicianID := strings.TrimSpace(command.TechnicianID)
	currency := strings.ToUpper(strings.TrimSpace(command.HourlyRate.Currency))
	actorID := strings.TrimSpace(command.ActorID)
	source := strings.TrimSpace(command.Source)
	if target.MSPID == "" || technicianID == "" || len(currency) != 3 ||
		command.HourlyRate.Minor < 0 || command.EffectiveAt.IsZero() ||
		actorID == "" || source == "" {
		return TechnicianLaborCostRate{}, ErrInvalidFinancialInput
	}
	if err := authorization.Authorize(
		command.Principal, "organization.manage", target,
	); err != nil {
		return TechnicianLaborCostRate{}, err
	}
	rate := TechnicianLaborCostRate{
		ID: s.newID(), MSPID: target.MSPID, TechnicianID: technicianID,
		HourlyRate:  Money{Minor: command.HourlyRate.Minor, Currency: currency},
		EffectiveAt: command.EffectiveAt.UTC(), Version: 1,
	}
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	now := s.now().UTC()
	accepted := LaborCostRateMutation{
		Rate: rate,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID,
			ActorType: "technician", ActorID: actorID,
			Action:      "technician.labor_cost_rate.created",
			SubjectType: "technician_labor_cost_rate",
			SubjectID:   rate.ID, SubjectVersion: rate.Version,
			Source: source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "technician.labor_cost_rate.created",
			SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID,
			ActorType: "technician", ActorID: actorID,
			SubjectType: "technician_labor_cost_rate",
			SubjectID:   rate.ID, SubjectVersion: rate.Version,
			Source: source, CorrelationID: correlationID,
		},
	}
	if err := s.repository.CreateLaborCostRateAtomic(ctx, accepted); err != nil {
		return TechnicianLaborCostRate{}, err
	}
	return rate, nil
}

func (s *FinancialInputService) RecognizeBillableWork(
	ctx context.Context,
	command RecognizeBillableWorkCommand,
) (BillableWorkRecognition, error) {
	target := projectTarget(command.Principal, command.Target)
	description := strings.TrimSpace(command.Description)
	currency := strings.ToUpper(strings.TrimSpace(command.Amount.Currency))
	actorID := strings.TrimSpace(command.ActorID)
	source := strings.TrimSpace(command.Source)
	if target.ClientID == "" || command.ProjectID == "" ||
		description == "" || len(currency) != 3 || command.Amount.Minor < 0 ||
		command.RecognizedAt.IsZero() || actorID == "" || source == "" {
		return BillableWorkRecognition{}, ErrInvalidFinancialInput
	}
	if err := authorization.Authorize(
		command.Principal, "project.edit", target,
	); err != nil {
		return BillableWorkRecognition{}, err
	}
	project, err := s.repository.FindProject(ctx, target, command.ProjectID)
	if err != nil {
		return BillableWorkRecognition{}, err
	}
	if project.MSPID != target.MSPID || project.ClientID != target.ClientID {
		return BillableWorkRecognition{}, scope.ErrNotFound
	}
	if command.PhaseID != "" {
		phase, err := s.repository.FindPhase(ctx, target, command.PhaseID)
		if err != nil {
			return BillableWorkRecognition{}, err
		}
		if phase.ProjectID != project.ID || phase.MSPID != target.MSPID ||
			phase.ClientID != target.ClientID {
			return BillableWorkRecognition{}, scope.ErrNotFound
		}
	}
	recognition := BillableWorkRecognition{
		ID: s.newID(), ProjectID: project.ID, PhaseID: command.PhaseID,
		MSPID: target.MSPID, ClientID: target.ClientID,
		Description:  description,
		Amount:       Money{Minor: command.Amount.Minor, Currency: currency},
		RecognizedAt: command.RecognizedAt.UTC(), Version: 1,
	}
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	now := s.now().UTC()
	accepted := BillableWorkMutation{
		Recognition: recognition,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician", ActorID: actorID,
			Action:      "project.billable_work.recognized",
			SubjectType: "recognized_billable_work",
			SubjectID:   recognition.ID, SubjectVersion: recognition.Version,
			Source: source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "project.billable_work.recognized",
			SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician", ActorID: actorID,
			SubjectType: "recognized_billable_work",
			SubjectID:   recognition.ID, SubjectVersion: recognition.Version,
			Source: source, CorrelationID: correlationID,
		},
	}
	if err := s.repository.RecognizeBillableWorkAtomic(ctx, accepted); err != nil {
		return BillableWorkRecognition{}, err
	}
	return recognition, nil
}

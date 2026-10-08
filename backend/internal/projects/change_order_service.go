package projects

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrInvalidChangeOrder        = errors.New("invalid change order")
	ErrOverrideReasonRequired    = errors.New("change order override reason required")
	ErrChangeOrderNotApproved    = errors.New("change order is not approved")
	ErrChangeOrderAlreadyApplied = errors.New("change order version already applied")
)

type ChangeOrderRepository interface {
	FindChangeOrder(context.Context, scope.Target, string) (ChangeOrder, error)
	FindChangeOrderVersion(context.Context, scope.Target, string) (ChangeOrderVersion, error)
	FindProjectForChange(context.Context, scope.Target, ProjectID) (Project, error)
	CreateChangeOrderAtomic(context.Context, CreateChangeOrderMutation) error
	IssueChangeOrderAtomic(context.Context, IssueChangeOrderMutation) error
	DecideChangeOrderAtomic(context.Context, DecideChangeOrderMutation) error
	ApplyChangeOrderAtomic(context.Context, ApplyChangeOrderMutation) error
}

type ChangeOrderService struct {
	repository ChangeOrderRepository
	now        func() time.Time
	newID      func() string
}

func NewChangeOrderService(
	repository ChangeOrderRepository,
	now func() time.Time,
	newID func() string,
) *ChangeOrderService {
	return &ChangeOrderService{repository: repository, now: now, newID: newID}
}

type CreateChangeOrderCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	ProjectID ProjectID
	DisplayID string
	ActorID   string
	Source    string
}

func (s *ChangeOrderService) Create(
	ctx context.Context,
	command CreateChangeOrderCommand,
) (ChangeOrder, error) {
	target := projectTarget(command.Principal, command.Target)
	displayID := strings.TrimSpace(command.DisplayID)
	if target.ClientID == "" || command.ProjectID == "" || displayID == "" ||
		command.ActorID == "" || command.Source == "" {
		return ChangeOrder{}, ErrInvalidChangeOrder
	}
	if err := authorization.Authorize(command.Principal, "change_order.update", target); err != nil {
		return ChangeOrder{}, err
	}
	project, err := s.repository.FindProjectForChange(ctx, target, command.ProjectID)
	if err != nil {
		return ChangeOrder{}, err
	}
	if project.MSPID != target.MSPID || project.ClientID != target.ClientID {
		return ChangeOrder{}, scope.ErrNotFound
	}
	now := s.now().UTC()
	order := ChangeOrder{
		ID: s.newID(), ProjectID: project.ID, MSPID: target.MSPID, ClientID: target.ClientID,
		DisplayID: displayID, State: ChangeOrderDraft, Version: 1,
		CreatedAt: now, CreatedBy: command.ActorID, UpdatedAt: now, UpdatedBy: command.ActorID,
	}
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := CreateChangeOrderMutation{
		Order: order,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "change_order.created", SubjectType: "change_order",
			SubjectID: order.ID, SubjectVersion: order.Version,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "change_order.created", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "change_order", SubjectID: order.ID,
			SubjectVersion: order.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.CreateChangeOrderAtomic(ctx, accepted); err != nil {
		return ChangeOrder{}, err
	}
	return order, nil
}

type IssueChangeOrderCommand struct {
	Principal            authorization.Principal
	Target               scope.Target
	ChangeOrderID        string
	ExpectedOrderVersion int64
	Description          string
	Currency             string
	RevenueDeltaMinor    int64
	CostDeltaMinor       int64
	LaborDeltaMinutes    int64
	ActorID              string
	Source               string
}

func (s *ChangeOrderService) IssueVersion(
	ctx context.Context,
	command IssueChangeOrderCommand,
) (ChangeOrderVersion, error) {
	target := projectTarget(command.Principal, command.Target)
	if target.ClientID == "" || command.ChangeOrderID == "" ||
		command.ExpectedOrderVersion < 1 || strings.TrimSpace(command.Description) == "" ||
		len(command.Currency) != 3 || command.ActorID == "" || command.Source == "" {
		return ChangeOrderVersion{}, ErrInvalidChangeOrder
	}
	if err := authorization.Authorize(command.Principal, "change_order.update", target); err != nil {
		return ChangeOrderVersion{}, err
	}
	order, err := s.repository.FindChangeOrder(ctx, target, command.ChangeOrderID)
	if err != nil {
		return ChangeOrderVersion{}, err
	}
	if !changeOrderInScope(order, target) {
		return ChangeOrderVersion{}, scope.ErrNotFound
	}
	if order.State != ChangeOrderDraft && order.State != ChangeOrderRejected {
		return ChangeOrderVersion{}, ErrInvalidChangeOrder
	}
	if err := object.RequireVersion(order.Version, command.ExpectedOrderVersion); err != nil {
		return ChangeOrderVersion{}, err
	}
	now := s.now().UTC()
	version := ChangeOrderVersion{
		ID: s.newID(), ChangeOrderID: order.ID, MSPID: target.MSPID, ClientID: target.ClientID,
		Version: order.CurrentVersion + 1, Description: strings.TrimSpace(command.Description),
		Currency: command.Currency, RevenueDeltaMinor: command.RevenueDeltaMinor,
		CostDeltaMinor: command.CostDeltaMinor, LaborDeltaMinutes: command.LaborDeltaMinutes,
		IssuedAt: now, IssuedBy: command.ActorID,
	}
	order.State, order.CurrentVersion = ChangeOrderIssued, version.Version
	order.Version++
	order.UpdatedAt, order.UpdatedBy = now, command.ActorID
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := IssueChangeOrderMutation{
		Order: order, Version: version,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "change_order.version.issued", SubjectType: "change_order_version",
			SubjectID: version.ID, SubjectVersion: version.Version,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "change_order.version.issued", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "change_order_version", SubjectID: version.ID,
			SubjectVersion: version.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.IssueChangeOrderAtomic(ctx, accepted); err != nil {
		return ChangeOrderVersion{}, err
	}
	return version, nil
}

type ApprovalCommand struct {
	Principal            authorization.Principal
	Target               scope.Target
	VersionID            string
	ExpectedOrderVersion int64
	ActorID              string
	Source               string
}

func (s *ChangeOrderService) Approve(
	ctx context.Context,
	command ApprovalCommand,
) (ChangeOrderVersion, error) {
	return s.decide(ctx, decisionCommand{
		Principal: command.Principal, Target: command.Target,
		VersionID: command.VersionID, ExpectedOrderVersion: command.ExpectedOrderVersion,
		Decision: ChangeOrderApproved, Capability: "change_order.approve",
		ActorID: command.ActorID, Source: command.Source,
	})
}

type OverrideCommand struct {
	Principal            authorization.Principal
	Target               scope.Target
	VersionID            string
	ExpectedOrderVersion int64
	Reason               string
	ActorID              string
	Source               string
}

func (s *ChangeOrderService) OverrideApproval(
	ctx context.Context,
	command OverrideCommand,
) (ChangeOrderVersion, error) {
	if strings.TrimSpace(command.Reason) == "" {
		return ChangeOrderVersion{}, ErrOverrideReasonRequired
	}
	return s.decide(ctx, decisionCommand{
		Principal: command.Principal, Target: command.Target,
		VersionID: command.VersionID, ExpectedOrderVersion: command.ExpectedOrderVersion,
		Decision: ChangeOrderApproved, Capability: "change_order.update",
		Override: true, Reason: strings.TrimSpace(command.Reason),
		ActorID: command.ActorID, Source: command.Source,
	})
}

type decisionCommand struct {
	Principal            authorization.Principal
	Target               scope.Target
	VersionID            string
	ExpectedOrderVersion int64
	Decision             ChangeOrderState
	Capability           string
	Override             bool
	Reason               string
	ActorID              string
	Source               string
}

func (s *ChangeOrderService) decide(
	ctx context.Context,
	command decisionCommand,
) (ChangeOrderVersion, error) {
	target := projectTarget(command.Principal, command.Target)
	if target.ClientID == "" || command.VersionID == "" ||
		command.ExpectedOrderVersion < 1 || command.ActorID == "" || command.Source == "" {
		return ChangeOrderVersion{}, ErrInvalidChangeOrder
	}
	if err := authorization.Authorize(command.Principal, command.Capability, target); err != nil {
		return ChangeOrderVersion{}, err
	}
	version, err := s.repository.FindChangeOrderVersion(ctx, target, command.VersionID)
	if err != nil {
		return ChangeOrderVersion{}, err
	}
	if version.MSPID != target.MSPID || version.ClientID != target.ClientID {
		return ChangeOrderVersion{}, scope.ErrNotFound
	}
	order, err := s.repository.FindChangeOrder(ctx, target, version.ChangeOrderID)
	if err != nil {
		return ChangeOrderVersion{}, err
	}
	if !changeOrderInScope(order, target) || order.CurrentVersion != version.Version ||
		order.State != ChangeOrderIssued {
		return ChangeOrderVersion{}, ErrInvalidChangeOrder
	}
	if err := object.RequireVersion(order.Version, command.ExpectedOrderVersion); err != nil {
		return ChangeOrderVersion{}, err
	}
	now := s.now().UTC()
	previous := order.State
	order.State = command.Decision
	order.Version++
	order.UpdatedAt, order.UpdatedBy = now, command.ActorID
	decisionID, auditID, eventID, correlationID := s.newID(), s.newID(), s.newID(), s.newID()
	accepted := DecideChangeOrderMutation{
		Order: order,
		Decision: ChangeOrderDecision{
			ID: decisionID, ChangeOrderID: order.ID, ChangeOrderVersionID: version.ID,
			MSPID: target.MSPID, ClientID: target.ClientID,
			PreviousState: previous, Decision: command.Decision,
			Override: command.Override, Reason: command.Reason,
			DecidedAt: now, DecidedBy: command.ActorID,
		},
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "change_order.approval.decided", SubjectType: "change_order_version",
			SubjectID: version.ID, SubjectVersion: version.Version,
			Reason: command.Reason, Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "change_order.approval.decided", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "change_order_version", SubjectID: version.ID,
			SubjectVersion: version.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.DecideChangeOrderAtomic(ctx, accepted); err != nil {
		return ChangeOrderVersion{}, err
	}
	return version, nil
}

type ApplyChangeOrderCommand struct {
	Principal            authorization.Principal
	Target               scope.Target
	VersionID            string
	ExpectedOrderVersion int64
	ActorID              string
	Source               string
}

func (s *ChangeOrderService) Apply(
	ctx context.Context,
	command ApplyChangeOrderCommand,
) (Project, error) {
	target := projectTarget(command.Principal, command.Target)
	if target.ClientID == "" || command.VersionID == "" ||
		command.ExpectedOrderVersion < 1 || command.ActorID == "" || command.Source == "" {
		return Project{}, ErrInvalidChangeOrder
	}
	if err := authorization.Authorize(command.Principal, "change_order.update", target); err != nil {
		return Project{}, err
	}
	version, err := s.repository.FindChangeOrderVersion(ctx, target, command.VersionID)
	if err != nil {
		return Project{}, err
	}
	order, err := s.repository.FindChangeOrder(ctx, target, version.ChangeOrderID)
	if err != nil {
		return Project{}, err
	}
	if !changeOrderInScope(order, target) ||
		version.MSPID != target.MSPID || version.ClientID != target.ClientID {
		return Project{}, scope.ErrNotFound
	}
	if order.State == ChangeOrderApplied {
		return Project{}, ErrChangeOrderAlreadyApplied
	}
	if order.State != ChangeOrderApproved || order.CurrentVersion != version.Version {
		return Project{}, ErrChangeOrderNotApproved
	}
	if err := object.RequireVersion(order.Version, command.ExpectedOrderVersion); err != nil {
		return Project{}, err
	}
	project, err := s.repository.FindProjectForChange(ctx, target, order.ProjectID)
	if err != nil {
		return Project{}, err
	}
	if project.MSPID != target.MSPID || project.ClientID != target.ClientID ||
		project.CurrentBaseline.Currency != version.Currency {
		return Project{}, ErrInvalidChangeOrder
	}
	current := project.CurrentBaseline
	current.RevenueMinor += version.RevenueDeltaMinor
	current.CostMinor += version.CostDeltaMinor
	current.PlannedMinutes += version.LaborDeltaMinutes
	if current.RevenueMinor < 0 || current.CostMinor < 0 || current.PlannedMinutes < 0 {
		return Project{}, ErrInvalidChangeOrder
	}
	project.CurrentBaseline = current
	project.Version++
	now := s.now().UTC()
	order.State = ChangeOrderApplied
	order.Version++
	order.UpdatedAt, order.UpdatedBy = now, command.ActorID
	applicationID, auditID, eventID, correlationID := s.newID(), s.newID(), s.newID(), s.newID()
	accepted := ApplyChangeOrderMutation{
		Order: order, Project: project,
		Application: ChangeOrderApplication{
			ID: applicationID, ChangeOrderID: order.ID,
			ChangeOrderVersionID: version.ID, ProjectID: project.ID,
			MSPID: target.MSPID, ClientID: target.ClientID,
			AppliedAt: now, AppliedBy: command.ActorID,
		},
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "change_order.applied", SubjectType: "change_order_version",
			SubjectID: version.ID, SubjectVersion: version.Version,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "change_order.applied", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "change_order_version", SubjectID: version.ID,
			SubjectVersion: version.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.ApplyChangeOrderAtomic(ctx, accepted); err != nil {
		return Project{}, err
	}
	return project, nil
}

func changeOrderInScope(order ChangeOrder, target scope.Target) bool {
	return order.MSPID == target.MSPID && order.ClientID == target.ClientID
}

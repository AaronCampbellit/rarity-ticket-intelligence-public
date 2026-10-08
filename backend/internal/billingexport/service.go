package billingexport

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

var (
	ErrInvalid  = errors.New("invalid billing export command")
	ErrTooLarge = errors.New("billing export exceeds entry limit")
)

// ClassificationRequiredError retains the blocking entry for a caller to
// recover by classifying that exact object and retrying unchanged export input.
type ClassificationRequiredError struct {
	EntryID string
	Err     error
}

func (e *ClassificationRequiredError) Error() string { return e.Err.Error() }
func (e *ClassificationRequiredError) Unwrap() error { return e.Err }

type ApprovalState string

const (
	PendingApproval ApprovalState = "pending"
	Approved        ApprovalState = "approved"
	Rejected        ApprovalState = "rejected"
)

type ApprovalEntry struct {
	Entry
	MSPID         string        `json:"msp_id"`
	ClientID      string        `json:"client_id"`
	ApprovalState ApprovalState `json:"approval_state"`
	ApprovedAt    *time.Time    `json:"approved_at,omitempty"`
	ApprovedBy    string        `json:"approved_by,omitempty"`
	StartedAt     time.Time     `json:"started_at"`
	EndedAt       time.Time     `json:"ended_at"`
	Note          string        `json:"note"`
}

type ApprovalDecision struct {
	ID        string
	EntryID   string
	MSPID     string
	ClientID  string
	Version   int64
	Decision  ApprovalState
	Reason    string
	DecidedAt time.Time
	DecidedBy string
}

type ApprovalCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	EntryID         string
	ExpectedVersion int64
	Decision        ApprovalState
	Reason          string
	ActorID         string
	Source          string
}

type ListApprovalsCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	State     ApprovalState
	Limit     int
}

type ApprovalMutation struct {
	Entry           ApprovalEntry
	ExpectedVersion int64
	Decision        ApprovalDecision
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type ExportCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	From      time.Time
	Through   time.Time
	ActorID   string
	Source    string
}

type ExportEvidence struct {
	ID            string
	MSPID         string
	ClientID      string
	From          time.Time
	Through       time.Time
	EntryVersions map[string]int64
	EntryCount    int
	SHA256        [sha256.Size]byte
	CreatedAt     time.Time
	CreatedBy     string
}

type ExportMutation struct {
	Export  ExportEvidence
	Entries []Entry
	Audit   mutation.AuditRecord
	Event   mutation.EventRecord
}

type ExportResult struct {
	ID         string    `json:"id"`
	EntryCount int       `json:"entry_count"`
	SHA256     [32]byte  `json:"sha256"`
	CreatedAt  time.Time `json:"created_at"`
	CSV        []byte    `json:"-"`
}

type Repository interface {
	ListApprovalEntries(context.Context, scope.Target, ApprovalState, int) ([]ApprovalEntry, error)
	LoadApprovalEntry(context.Context, scope.Target, string) (ApprovalEntry, error)
	DecideApprovalAtomic(context.Context, ApprovalMutation) error
	ListEntries(context.Context, scope.Target, time.Time, time.Time, int) ([]Entry, error)
	RecordExportAtomic(context.Context, ExportMutation) error
}

type Service struct {
	repository Repository
	now        func() time.Time
	newID      func() string
	guard      *tagging.TerminalGuard
}

func NewService(repository Repository, now func() time.Time, newID func() string, guard *tagging.TerminalGuard) *Service {
	return &Service{repository: repository, now: now, newID: newID, guard: guard}
}

func (s *Service) ListApprovals(
	ctx context.Context,
	command ListApprovalsCommand,
) ([]ApprovalEntry, error) {
	target := billingTarget(command.Target, command.Principal)
	if target.ClientID == "" || command.Limit < 1 || command.Limit > 100 ||
		(command.State != PendingApproval && command.State != Approved &&
			command.State != Rejected) {
		return nil, ErrInvalid
	}
	capability := "time_entry.approve"
	if !command.Principal.Capabilities.Has(capability) {
		capability = "time_entry.export"
	}
	if err := authorization.Authorize(command.Principal, capability, target); err != nil {
		return nil, err
	}
	return s.repository.ListApprovalEntries(
		ctx, target, command.State, command.Limit,
	)
}

func (s *Service) DecideApproval(
	ctx context.Context,
	command ApprovalCommand,
) (ApprovalEntry, error) {
	target := billingTarget(command.Target, command.Principal)
	reason := strings.TrimSpace(command.Reason)
	if target.ClientID == "" || command.EntryID == "" ||
		command.ExpectedVersion < 1 ||
		(command.Decision != Approved && command.Decision != Rejected) ||
		reason == "" || command.ActorID == "" || command.Source == "" {
		return ApprovalEntry{}, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, "time_entry.approve", target); err != nil {
		return ApprovalEntry{}, err
	}
	entry, err := s.repository.LoadApprovalEntry(ctx, target, command.EntryID)
	if err != nil {
		return ApprovalEntry{}, err
	}
	if entry.MSPID != target.MSPID || entry.ClientID != target.ClientID {
		return ApprovalEntry{}, scope.ErrNotFound
	}
	if err := object.RequireVersion(entry.Version, command.ExpectedVersion); err != nil {
		return ApprovalEntry{}, err
	}
	if command.Decision == Approved && (s == nil || s.guard == nil) {
		return ApprovalEntry{}, ErrInvalid
	}
	if command.Decision == Approved {
		if err := s.guard.RequireMeaningful(ctx, tagging.GuardCommand{Principal: command.Principal, Target: tagging.TargetRef{MSPID: target.MSPID, ClientID: target.ClientID, ObjectType: tagging.ObjectTimeEntry, ObjectID: entry.ID}}); err != nil {
			return ApprovalEntry{}, err
		}
	}
	now := s.now().UTC()
	entry.Version++
	entry.ApprovalState = command.Decision
	entry.ApprovedAt = nil
	entry.ApprovedBy = ""
	if command.Decision == Approved {
		entry.ApprovedAt = &now
		entry.ApprovedBy = command.ActorID
		entry.Approved = true
	} else {
		entry.Approved = false
	}
	action := "time_entry." + string(command.Decision)
	decisionID, auditID, eventID, correlationID := s.newID(), s.newID(), s.newID(), s.newID()
	accepted := ApprovalMutation{
		Entry: entry, ExpectedVersion: command.ExpectedVersion,
		Decision: ApprovalDecision{
			ID: decisionID, EntryID: entry.ID, MSPID: target.MSPID,
			ClientID: target.ClientID, Version: entry.Version,
			Decision: command.Decision, Reason: reason,
			DecidedAt: now, DecidedBy: command.ActorID,
		},
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician",
			ActorID: command.ActorID, Action: action, SubjectType: "time_entry",
			SubjectID: entry.ID, SubjectVersion: entry.Version,
			Source: command.Source, Reason: reason, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: action, SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "time_entry", SubjectID: entry.ID,
			SubjectVersion: entry.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.DecideApprovalAtomic(ctx, accepted); err != nil {
		return ApprovalEntry{}, err
	}
	return entry, nil
}

func (s *Service) Export(
	ctx context.Context,
	command ExportCommand,
) (ExportResult, error) {
	target := billingTarget(command.Target, command.Principal)
	from, through := command.From.UTC(), command.Through.UTC()
	if target.ClientID == "" || from.IsZero() || through.IsZero() ||
		!through.After(from) || through.Sub(from) > 366*24*time.Hour ||
		command.ActorID == "" || command.Source == "" {
		return ExportResult{}, ErrInvalid
	}
	if err := authorization.Authorize(
		command.Principal, "time_entry.export", target,
	); err != nil {
		return ExportResult{}, err
	}
	entries, err := s.repository.ListEntries(ctx, target, from, through, 10001)
	if err != nil {
		return ExportResult{}, err
	}
	if len(entries) > 10000 {
		return ExportResult{}, ErrTooLarge
	}
	if s == nil || s.guard == nil {
		return ExportResult{}, ErrInvalid
	}
	for _, entry := range entries {
		if err := s.guard.RequireMeaningful(ctx, tagging.GuardCommand{Principal: command.Principal, Target: tagging.TargetRef{MSPID: target.MSPID, ClientID: target.ClientID, ObjectType: tagging.ObjectTimeEntry, ObjectID: entry.ID}}); err != nil {
			return ExportResult{}, &ClassificationRequiredError{EntryID: entry.ID, Err: err}
		}
	}
	csv, err := BuildCSV(command.Principal, target, Policy{RequireApproval: true}, entries)
	if err != nil {
		return ExportResult{}, err
	}
	now := s.now().UTC()
	exportID := s.newID()
	hash := sha256.Sum256(csv)
	versions := make(map[string]int64, len(entries))
	for _, entry := range entries {
		if entry.ID == "" || entry.Version < 1 {
			return ExportResult{}, ErrInvalid
		}
		versions[entry.ID] = entry.Version
	}
	correlationID := s.newID()
	accepted := ExportMutation{
		Export: ExportEvidence{
			ID: exportID, MSPID: target.MSPID, ClientID: target.ClientID,
			From: from, Through: through, EntryVersions: versions,
			EntryCount: len(entries), SHA256: hash,
			CreatedAt: now, CreatedBy: command.ActorID,
		},
		Entries: append([]Entry(nil), entries...),
		Audit: mutation.AuditRecord{
			ID: s.newID(), OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician",
			ActorID: command.ActorID, Action: "billing_export.created",
			SubjectType: "billing_export", SubjectID: exportID,
			SubjectVersion: 1, Source: command.Source,
			CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: s.newID(), EventType: "billing_export.created",
			SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician",
			ActorID: command.ActorID, SubjectType: "billing_export",
			SubjectID: exportID, SubjectVersion: 1, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.RecordExportAtomic(ctx, accepted); err != nil {
		return ExportResult{}, err
	}
	return ExportResult{
		ID: exportID, EntryCount: len(entries), SHA256: hash,
		CreatedAt: now, CSV: csv,
	}, nil
}

func billingTarget(
	target scope.Target,
	principal authorization.Principal,
) scope.Target {
	if target.MSPID == "" {
		return scope.Target{
			MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
		}
	}
	return target
}

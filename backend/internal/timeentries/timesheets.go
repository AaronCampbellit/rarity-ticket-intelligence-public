package timeentries

import (
	"context"
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
	ErrInvalidTimesheet       = errors.New("invalid timesheet command")
	ErrApprovedEntryImmutable = errors.New("approved time entry is immutable")
	ErrEntryNotPending        = errors.New("time entry is not pending")
)

type Week struct {
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	Timezone string    `json:"timezone"`
}

type TimesheetRow struct {
	Entry                  Entry              `json:"entry"`
	ClientName             string             `json:"client_name"`
	WorkItemTitle          string             `json:"work_item_title"`
	LaborRoleName          string             `json:"labor_role_name"`
	ApprovalState          string             `json:"approval_state"`
	ApprovedAt             *time.Time         `json:"approved_at,omitempty"`
	ApprovedBy             string             `json:"approved_by,omitempty"`
	ReversedAt             *time.Time         `json:"reversed_at,omitempty"`
	ReversedBy             string             `json:"reversed_by,omitempty"`
	ReversalReason         string             `json:"reversal_reason,omitempty"`
	ReplacementTimeEntryID string             `json:"replacement_time_entry_id,omitempty"`
	LastAmendment          *AmendmentEvidence `json:"last_amendment,omitempty"`
}

type Timesheet struct {
	Week               Week           `json:"week"`
	TechnicianID       string         `json:"technician_id"`
	Rows               []TimesheetRow `json:"rows"`
	TotalSeconds       int64          `json:"total_seconds"`
	BillableSeconds    int64          `json:"billable_seconds"`
	NonbillableSeconds int64          `json:"nonbillable_seconds"`
}

type ListWeekCommand struct {
	Principal    authorization.Principal
	Target       scope.Target
	TechnicianID string
	Anchor       time.Time
}

type GetCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	EntryID   string
}

type AmendCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	EntryID         string
	ExpectedVersion int64
	StartedAt       time.Time
	EndedAt         time.Time
	Billable        bool
	Note            string
	Reason          string
	ActorID         string
	Source          string
}

type Replacement struct {
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	Billable  bool      `json:"billable"`
	Note      string    `json:"note"`
	TagIDs    []string  `json:"tag_ids"`
}

type ReverseCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	EntryID         string
	ExpectedVersion int64
	Reason          string
	ActorID         string
	Source          string
	Replacement     Replacement
}

type AmendmentEvidence struct {
	ID               string         `json:"id"`
	TimeEntryID      string         `json:"time_entry_id"`
	MSPID            string         `json:"msp_id"`
	ClientID         string         `json:"client_id"`
	PriorVersion     int64          `json:"prior_version"`
	ResultingVersion int64          `json:"resulting_version"`
	BeforeValues     map[string]any `json:"before_values"`
	AfterValues      map[string]any `json:"after_values"`
	Reason           string         `json:"reason"`
	AmendedAt        time.Time      `json:"amended_at"`
	AmendedBy        string         `json:"amended_by"`
}

type AmendmentMutation struct {
	ExpectedVersion int64
	Result          TimesheetRow
	Amendment       AmendmentEvidence
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type ReversalMutation struct {
	ExpectedVersion  int64
	Original         TimesheetRow
	Replacement      TimesheetRow
	Reason           string
	ReversedAt       time.Time
	ReversedBy       string
	OriginalAudit    mutation.AuditRecord
	OriginalEvent    mutation.EventRecord
	ReplacementAudit mutation.AuditRecord
	ReplacementEvent mutation.EventRecord
	InitialTags      tagging.InitialAssignmentSet
}

type ReversalResult struct {
	Original    TimesheetRow `json:"original"`
	Replacement TimesheetRow `json:"replacement"`
}

type TimesheetRepository interface {
	LoadMSPTimezone(context.Context, string) (string, error)
	ListTimesheetRows(
		context.Context,
		scope.Target,
		string,
		time.Time,
		time.Time,
	) ([]TimesheetRow, error)
	LoadTimesheetRow(context.Context, scope.Target, string) (TimesheetRow, error)
	AmendTimeEntryAtomic(
		context.Context,
		AmendmentMutation,
	) (TimesheetRow, error)
	ReverseTimeEntryAtomic(
		context.Context,
		ReversalMutation,
	) (ReversalResult, error)
}

type TimesheetService struct {
	repository TimesheetRepository
	now        func() time.Time
	newID      func() string
	creation   *tagging.CreationPreparer
}

func NewTimesheetService(
	repository TimesheetRepository,
	now func() time.Time,
	newID func() string,
	creation *tagging.CreationPreparer,
) *TimesheetService {
	return &TimesheetService{repository: repository, now: now, newID: newID, creation: creation}
}

func (s *TimesheetService) ListWeek(
	ctx context.Context,
	command ListWeekCommand,
) (Timesheet, error) {
	target := timesheetTarget(command.Target, command.Principal)
	technicianID := strings.TrimSpace(command.TechnicianID)
	capability := "timesheet.review"
	if technicianID == "" || technicianID == command.Principal.ID {
		technicianID = command.Principal.ID
		capability = "timesheet.read_own"
	}
	if target.ClientID == "" || technicianID == "" {
		return Timesheet{}, ErrInvalidTimesheet
	}
	if err := authorization.Authorize(
		command.Principal,
		capability,
		target,
	); err != nil {
		return Timesheet{}, err
	}
	timezone, err := s.repository.LoadMSPTimezone(ctx, target.MSPID)
	if err != nil {
		return Timesheet{}, err
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return Timesheet{}, ErrInvalidTimesheet
	}
	anchor := command.Anchor
	if anchor.IsZero() {
		anchor = s.now()
	}
	local := anchor.In(location)
	mondayOffset := (int(local.Weekday()) + 6) % 7
	startLocal := time.Date(
		local.Year(),
		local.Month(),
		local.Day()-mondayOffset,
		0,
		0,
		0,
		0,
		location,
	)
	week := Week{
		StartsAt: startLocal.UTC(),
		EndsAt:   startLocal.AddDate(0, 0, 7).UTC(),
		Timezone: timezone,
	}
	rows, err := s.repository.ListTimesheetRows(
		ctx,
		target,
		technicianID,
		week.StartsAt,
		week.EndsAt,
	)
	if err != nil {
		return Timesheet{}, err
	}
	found := Timesheet{
		Week: week, TechnicianID: technicianID, Rows: rows,
	}
	for _, row := range rows {
		if row.ReversedAt != nil {
			continue
		}
		found.TotalSeconds += row.Entry.DurationSeconds
		if row.Entry.Billable {
			found.BillableSeconds += row.Entry.DurationSeconds
		} else {
			found.NonbillableSeconds += row.Entry.DurationSeconds
		}
	}
	return found, nil
}

func (s *TimesheetService) Get(ctx context.Context, command GetCommand) (TimesheetRow, error) {
	target := timesheetTarget(command.Target, command.Principal)
	if s == nil || s.repository == nil || target.MSPID == "" || target.ClientID == "" || strings.TrimSpace(command.EntryID) == "" {
		return TimesheetRow{}, ErrInvalidTimesheet
	}
	found, err := s.repository.LoadTimesheetRow(ctx, target, strings.TrimSpace(command.EntryID))
	if err != nil {
		return TimesheetRow{}, err
	}
	capability := "timesheet.review"
	if found.Entry.TechnicianID == command.Principal.ID {
		capability = "timesheet.read_own"
	}
	if err := authorization.Authorize(command.Principal, capability, target); err != nil {
		return TimesheetRow{}, err
	}
	return found, nil
}

func (s *TimesheetService) Amend(
	ctx context.Context,
	command AmendCommand,
) (TimesheetRow, error) {
	target := timesheetTarget(command.Target, command.Principal)
	reason := strings.TrimSpace(command.Reason)
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || s.creation == nil ||
		target.ClientID == "" || command.EntryID == "" ||
		command.ExpectedVersion < 1 || command.ActorID == "" ||
		command.Source == "" || reason == "" ||
		command.StartedAt.IsZero() || !command.EndedAt.After(command.StartedAt) {
		return TimesheetRow{}, ErrInvalidTimesheet
	}
	current, err := s.repository.LoadTimesheetRow(
		ctx,
		target,
		command.EntryID,
	)
	if err != nil {
		return TimesheetRow{}, err
	}
	capability := "time_entry.amend"
	if current.Entry.TechnicianID == command.Principal.ID &&
		command.Principal.Capabilities.Has("time_entry.update_own") {
		capability = "time_entry.update_own"
	}
	if err := authorization.Authorize(
		command.Principal,
		capability,
		target,
	); err != nil {
		return TimesheetRow{}, err
	}
	if err := object.RequireVersion(
		current.Entry.Version,
		command.ExpectedVersion,
	); err != nil {
		return TimesheetRow{}, err
	}
	if current.ApprovalState == "approved" {
		return TimesheetRow{}, ErrApprovedEntryImmutable
	}
	if current.ApprovalState != "pending" {
		return TimesheetRow{}, ErrEntryNotPending
	}
	now := s.now().UTC()
	result := current
	result.Entry.StartedAt = command.StartedAt.UTC()
	result.Entry.EndedAt = command.EndedAt.UTC()
	result.Entry.DurationSeconds = int64(
		command.EndedAt.Sub(command.StartedAt) / time.Second,
	)
	result.Entry.Billable = command.Billable
	result.Entry.Note = strings.TrimSpace(command.Note)
	result.Entry.Version++
	before := editableValues(current.Entry)
	after := editableValues(result.Entry)
	amendmentID, auditID, eventID, correlationID :=
		s.newID(), s.newID(), s.newID(), s.newID()
	accepted := AmendmentMutation{
		ExpectedVersion: command.ExpectedVersion,
		Result:          result,
		Amendment: AmendmentEvidence{
			ID: amendmentID, TimeEntryID: current.Entry.ID,
			MSPID: target.MSPID, ClientID: target.ClientID,
			PriorVersion:     current.Entry.Version,
			ResultingVersion: result.Entry.Version,
			BeforeValues:     before, AfterValues: after,
			Reason: reason, AmendedAt: now, AmendedBy: command.ActorID,
		},
		Audit: timesheetAudit(
			auditID,
			now,
			target,
			command.ActorID,
			"time_entry.amended",
			current.Entry.ID,
			result.Entry.Version,
			command.Source,
			reason,
			correlationID,
		),
		Event: timesheetEvent(
			eventID,
			now,
			target,
			command.ActorID,
			"time_entry.amended",
			current.Entry.ID,
			result.Entry.Version,
			command.Source,
			correlationID,
		),
	}
	return s.repository.AmendTimeEntryAtomic(ctx, accepted)
}

func (s *TimesheetService) ReverseAndReplace(
	ctx context.Context,
	command ReverseCommand,
) (ReversalResult, error) {
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || s.creation == nil {
		return ReversalResult{}, ErrInvalidTimesheet
	}
	target := timesheetTarget(command.Target, command.Principal)
	reason := strings.TrimSpace(command.Reason)
	replacement := command.Replacement
	if target.ClientID == "" || command.EntryID == "" ||
		command.ExpectedVersion < 1 || command.ActorID == "" ||
		command.Source == "" || reason == "" ||
		replacement.StartedAt.IsZero() ||
		!replacement.EndedAt.After(replacement.StartedAt) {
		return ReversalResult{}, ErrInvalidTimesheet
	}
	if err := authorization.Authorize(
		command.Principal,
		"time_entry.amend",
		target,
	); err != nil {
		return ReversalResult{}, err
	}
	current, err := s.repository.LoadTimesheetRow(
		ctx,
		target,
		command.EntryID,
	)
	if err != nil {
		return ReversalResult{}, err
	}
	if err := object.RequireVersion(
		current.Entry.Version,
		command.ExpectedVersion,
	); err != nil {
		return ReversalResult{}, err
	}
	if current.ApprovalState != "approved" || current.ReversedAt != nil {
		return ReversalResult{}, ErrEntryNotPending
	}
	initial, err := s.creation.Prepare(ctx, tagging.PrepareCreationCommand{
		MSPID: target.MSPID, ClientID: target.ClientID, ObjectType: tagging.ObjectTimeEntry,
		TagIDs: replacement.TagIDs, Source: tagging.SourceHuman, Policy: tagging.CreationRequireMeaningful,
	})
	if err != nil {
		return ReversalResult{}, err
	}
	now := s.now().UTC()
	replacementID := s.newID()
	replacementRow := current
	replacementRow.Entry.ID = replacementID
	replacementRow.Entry.StartedAt = replacement.StartedAt.UTC()
	replacementRow.Entry.EndedAt = replacement.EndedAt.UTC()
	replacementRow.Entry.DurationSeconds = int64(
		replacement.EndedAt.Sub(replacement.StartedAt) / time.Second,
	)
	replacementRow.Entry.Billable = replacement.Billable
	replacementRow.Entry.Note = strings.TrimSpace(replacement.Note)
	replacementRow.Entry.Version = 1
	replacementRow.Entry.CreatedAt = now
	replacementRow.Entry.CreatedBy = command.ActorID
	replacementRow.ApprovalState = "pending"
	replacementRow.ApprovedAt = nil
	replacementRow.ApprovedBy = ""
	replacementRow.ReversedAt = nil
	replacementRow.ReversedBy = ""
	replacementRow.ReversalReason = ""
	replacementRow.ReplacementTimeEntryID = ""

	original := current
	original.Entry.Version++
	original.ReversedAt = &now
	original.ReversedBy = command.ActorID
	original.ReversalReason = reason
	original.ReplacementTimeEntryID = replacementID
	originalAuditID, originalEventID, replacementAuditID,
		replacementEventID, correlationID :=
		s.newID(), s.newID(), s.newID(), s.newID(), s.newID()
	accepted := ReversalMutation{
		ExpectedVersion: command.ExpectedVersion,
		Original:        original,
		Replacement:     replacementRow,
		Reason:          reason,
		ReversedAt:      now,
		ReversedBy:      command.ActorID,
		InitialTags: initial.WithProvenance(tagging.InitialAssignmentProvenance{
			ActorType: "technician", ActorID: command.ActorID, OccurredAt: now,
			CorrelationID: correlationID,
		}),
		OriginalAudit: timesheetAudit(
			originalAuditID,
			now,
			target,
			command.ActorID,
			"time_entry.reversed",
			original.Entry.ID,
			original.Entry.Version,
			command.Source,
			reason,
			correlationID,
		),
		OriginalEvent: timesheetEvent(
			originalEventID,
			now,
			target,
			command.ActorID,
			"time_entry.reversed",
			original.Entry.ID,
			original.Entry.Version,
			command.Source,
			correlationID,
		),
		ReplacementAudit: timesheetAudit(
			replacementAuditID,
			now,
			target,
			command.ActorID,
			"time_entry.created",
			replacementRow.Entry.ID,
			replacementRow.Entry.Version,
			command.Source,
			reason,
			correlationID,
		),
		ReplacementEvent: timesheetEvent(
			replacementEventID,
			now,
			target,
			command.ActorID,
			"time_entry.created",
			replacementRow.Entry.ID,
			replacementRow.Entry.Version,
			command.Source,
			correlationID,
		),
	}
	return s.repository.ReverseTimeEntryAtomic(ctx, accepted)
}

func timesheetTarget(
	target scope.Target,
	principal authorization.Principal,
) scope.Target {
	if target.MSPID == "" {
		return scope.Target{
			MSPID:    principal.Scope.MSPID,
			ClientID: principal.Scope.ClientID,
		}
	}
	return target
}

func editableValues(entry Entry) map[string]any {
	return map[string]any{
		"started_at":       entry.StartedAt,
		"ended_at":         entry.EndedAt,
		"duration_seconds": entry.DurationSeconds,
		"billable":         entry.Billable,
		"note":             entry.Note,
	}
}

func timesheetAudit(
	id string,
	at time.Time,
	target scope.Target,
	actorID string,
	action string,
	subjectID string,
	version int64,
	source string,
	reason string,
	correlationID string,
) mutation.AuditRecord {
	return mutation.AuditRecord{
		ID: id, OccurredAt: at, MSPID: target.MSPID,
		ClientID: target.ClientID, ActorType: "technician",
		ActorID: actorID, Action: action, SubjectType: "time_entry",
		SubjectID: subjectID, SubjectVersion: version,
		Source: source, Reason: reason, CorrelationID: correlationID,
	}
}

func timesheetEvent(
	id string,
	at time.Time,
	target scope.Target,
	actorID string,
	action string,
	subjectID string,
	version int64,
	source string,
	correlationID string,
) mutation.EventRecord {
	return mutation.EventRecord{
		EventID: id, EventType: action, SchemaVersion: 1,
		OccurredAt: at, MSPID: target.MSPID, ClientID: target.ClientID,
		ActorType: "technician", ActorID: actorID,
		SubjectType: "time_entry", SubjectID: subjectID,
		SubjectVersion: version, Source: source,
		CorrelationID: correlationID,
	}
}

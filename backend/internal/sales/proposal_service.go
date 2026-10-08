package sales

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrProposalVersionImmutable = errors.New("issued proposal version is immutable")
	ErrInvalidProposal          = errors.New("invalid proposal")
	ErrInternalApprovalRequired = errors.New("internal approval required")
	ErrSignerEvidenceRequired   = errors.New("signer evidence required")
	ErrInvalidAcceptanceGrant   = errors.New("invalid electronic acceptance grant")
)

type IssueMutation struct {
	Proposal Proposal
	Version  ProposalVersion
	Snapshot PDFSnapshot
	Audit    mutation.AuditRecord
	Event    mutation.EventRecord
}

type CreateProposalMutation struct {
	Proposal                   Proposal
	ExpectedClientVersion      int64
	ExpectedOpportunityVersion int64
	Audit                      mutation.AuditRecord
	Event                      mutation.EventRecord
}

type AcceptanceMutation struct {
	Proposal   Proposal
	Version    ProposalVersion
	Acceptance Acceptance
	Audit      mutation.AuditRecord
	Event      mutation.EventRecord
}

type InternalApprovalMutation struct {
	Approval InternalApproval
	Audit    mutation.AuditRecord
	Event    mutation.EventRecord
}

type AcceptanceGrantMutation struct {
	Grant AcceptanceGrant
	Audit mutation.AuditRecord
	Event mutation.EventRecord
}

type ProposalRepository interface {
	FindProposal(context.Context, scope.Target, string) (Proposal, error)
	ListProposals(context.Context, scope.Target, ProposalListFilter) ([]Proposal, error)
	FindProposalsByReference(context.Context, scope.Target, string, int) ([]Proposal, error)
	FindOpportunityForProposal(context.Context, scope.Target, string) (Opportunity, error)
	ProposalDisplayIDAvailable(context.Context, scope.Target, string) (bool, error)
	FindProposalVersion(context.Context, scope.Target, string) (ProposalVersion, error)
	CreateProposalAtomic(context.Context, CreateProposalMutation) error
	IssueVersionAtomic(context.Context, IssueMutation) error
	RecordAcceptanceAtomic(context.Context, AcceptanceMutation) error
	InternalApprovalSatisfied(context.Context, scope.Target, string) (bool, error)
	FindInternalApproval(context.Context, scope.Target, string) (InternalApproval, error)
	DecideInternalApprovalAtomic(context.Context, InternalApprovalMutation) error
	IssueAcceptanceGrantAtomic(context.Context, AcceptanceGrantMutation) error
	FindAcceptedSnapshot(context.Context, scope.Target, string) (PDFSnapshot, error)
}

func (s *ProposalService) GetProposal(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	id string,
) (Proposal, error) {
	target = proposalTarget(principal, target)
	if target.MSPID == "" || id == "" {
		return Proposal{}, ErrInvalidProposal
	}
	if err := authorization.Authorize(principal, "proposal.read", target); err != nil {
		return Proposal{}, err
	}
	return s.repository.FindProposal(ctx, target, id)
}

func (s *ProposalService) GetProposalVersion(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	id string,
) (ProposalVersion, error) {
	target = proposalTarget(principal, target)
	if target.MSPID == "" || id == "" {
		return ProposalVersion{}, ErrInvalidProposal
	}
	if err := authorization.Authorize(principal, "proposal.read", target); err != nil {
		return ProposalVersion{}, err
	}
	return s.repository.FindProposalVersion(ctx, target, id)
}

func (s *ProposalService) ListProposals(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	filter ProposalListFilter,
) ([]Proposal, error) {
	target = proposalTarget(principal, target)
	if target.MSPID == "" ||
		filter.BeforeUpdatedAt.IsZero() != (filter.BeforeID == "") {
		return nil, ErrInvalidProposal
	}
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	if filter.State != "" && filter.State != ProposalDraft &&
		filter.State != ProposalIssued && filter.State != ProposalAccepted {
		return nil, ErrInvalidProposal
	}
	if err := authorization.Authorize(principal, "proposal.read", target); err != nil {
		return nil, err
	}
	return s.repository.ListProposals(ctx, target, filter)
}

func (s *ProposalService) ResolveProposalReference(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	reference string,
	limit int,
) ([]Proposal, error) {
	reference = strings.TrimSpace(reference)
	if target.MSPID == "" || target.ClientID == "" || reference == "" ||
		limit < 1 || limit > 2 || s.repository == nil {
		return nil, ErrInvalidProposal
	}
	if err := authorization.Authorize(principal, "proposal.read", target); err != nil {
		return nil, err
	}
	return s.repository.FindProposalsByReference(ctx, target, reference, limit)
}

type CreateProposalCommand struct {
	Principal                  authorization.Principal
	Target                     scope.Target
	OpportunityID              string
	ExpectedClientVersion      int64
	ExpectedOpportunityVersion int64
	DisplayID                  string
	ActorID                    string
	Source                     string
}

// ProposalDraftPreflight is the read-only eligibility result for creating an
// empty draft proposal. It deliberately carries only the eligible source
// Opportunity and the caller-supplied display ID; no proposal is allocated or
// persisted until CreateProposal succeeds.
type ProposalDraftPreflight struct {
	Opportunity Opportunity
	DisplayID   string
}

func (s *ProposalService) PreflightCreateProposal(
	ctx context.Context,
	command CreateProposalCommand,
) (ProposalDraftPreflight, error) {
	target := proposalTarget(command.Principal, command.Target)
	displayID := strings.TrimSpace(command.DisplayID)
	if s == nil || s.repository == nil || target.MSPID == "" || target.ClientID == "" ||
		command.OpportunityID == "" || command.ExpectedOpportunityVersion < 0 ||
		displayID == "" || command.ActorID == "" || command.Source == "" {
		return ProposalDraftPreflight{}, ErrInvalidProposal
	}
	if err := authorization.Authorize(command.Principal, "proposal.create", target); err != nil {
		return ProposalDraftPreflight{}, err
	}
	opportunity, err := s.repository.FindOpportunityForProposal(ctx, target, command.OpportunityID)
	if err != nil {
		return ProposalDraftPreflight{}, err
	}
	if opportunity.ID != OpportunityID(command.OpportunityID) || opportunity.MSPID != target.MSPID ||
		opportunity.ClientID != target.ClientID || opportunity.ProspectID != "" ||
		strings.TrimSpace(opportunity.DisplayID) == "" || strings.TrimSpace(opportunity.Name) == "" ||
		strings.TrimSpace(opportunity.PipelineID) == "" || opportunity.StageID == "" || opportunity.Version < 1 {
		return ProposalDraftPreflight{}, scope.ErrNotFound
	}
	if command.ExpectedOpportunityVersion > 0 {
		if err := object.RequireVersion(opportunity.Version, command.ExpectedOpportunityVersion); err != nil {
			return ProposalDraftPreflight{}, err
		}
	}
	available, err := s.repository.ProposalDisplayIDAvailable(ctx, target, displayID)
	if err != nil {
		return ProposalDraftPreflight{}, err
	}
	if !available {
		return ProposalDraftPreflight{}, ErrInvalidProposal
	}
	return ProposalDraftPreflight{Opportunity: opportunity, DisplayID: displayID}, nil
}

func (s *ProposalService) CreateProposal(
	ctx context.Context,
	command CreateProposalCommand,
) (Proposal, error) {
	target := proposalTarget(command.Principal, command.Target)
	displayID := strings.TrimSpace(command.DisplayID)
	if s == nil || s.repository == nil || target.MSPID == "" || command.OpportunityID == "" ||
		command.ExpectedClientVersion < 0 || command.ExpectedOpportunityVersion < 0 ||
		displayID == "" || command.ActorID == "" || command.Source == "" {
		return Proposal{}, ErrInvalidProposal
	}
	if err := authorization.Authorize(command.Principal, "proposal.create", target); err != nil {
		return Proposal{}, err
	}
	opportunity, err := s.repository.FindOpportunityForProposal(ctx, target, command.OpportunityID)
	if err != nil {
		return Proposal{}, err
	}
	if opportunity.ID != OpportunityID(command.OpportunityID) || opportunity.MSPID != target.MSPID ||
		(opportunity.ClientID != target.ClientID && !(target.ClientID == "" && opportunity.ProspectID != "")) {
		return Proposal{}, scope.ErrNotFound
	}
	if command.ExpectedOpportunityVersion > 0 {
		if err := object.RequireVersion(opportunity.Version, command.ExpectedOpportunityVersion); err != nil {
			return Proposal{}, err
		}
	}
	available, err := s.repository.ProposalDisplayIDAvailable(ctx, target, displayID)
	if err != nil {
		return Proposal{}, err
	}
	if !available {
		return Proposal{}, ErrInvalidProposal
	}
	now := s.now().UTC()
	proposalID, auditID, eventID, correlationID :=
		s.newID(), s.newID(), s.newID(), s.newID()
	proposal := Proposal{
		ID: proposalID, MSPID: target.MSPID, ClientID: opportunity.ClientID,
		ProspectID:    opportunity.ProspectID,
		OpportunityID: command.OpportunityID,
		DisplayID:     displayID,
		State:         ProposalDraft, Version: 1,
	}
	accepted := CreateProposalMutation{
		Proposal: proposal, ExpectedClientVersion: command.ExpectedClientVersion,
		ExpectedOpportunityVersion: opportunity.Version,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "proposal.created", SubjectType: "proposal",
			SubjectID: proposalID, SubjectVersion: 1,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "proposal.created", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "proposal", SubjectID: proposalID,
			SubjectVersion: 1, Source: command.Source, CorrelationID: correlationID,
		},
	}
	if err := s.repository.CreateProposalAtomic(ctx, accepted); err != nil {
		return Proposal{}, err
	}
	return proposal, nil
}

type IssueAcceptanceGrantCommand struct {
	Principal         authorization.Principal
	Target            scope.Target
	ProposalVersionID string
	SignerName        string
	SignerEmail       string
	Evidence          map[string]string
	ExpiresAt         time.Time
	ActorID           string
	Source            string
}

func (s *ProposalService) IssueAcceptanceGrant(
	ctx context.Context,
	command IssueAcceptanceGrantCommand,
) (IssuedAcceptanceGrant, error) {
	target := proposalTarget(command.Principal, command.Target)
	now := s.now().UTC()
	if target.MSPID == "" || command.ProposalVersionID == "" ||
		strings.TrimSpace(command.SignerName) == "" ||
		strings.TrimSpace(command.SignerEmail) == "" ||
		len(command.Evidence) == 0 || !command.ExpiresAt.After(now) ||
		command.ExpiresAt.After(now.Add(30*24*time.Hour)) ||
		command.ActorID == "" || command.Source == "" {
		return IssuedAcceptanceGrant{}, ErrInvalidAcceptanceGrant
	}
	if err := authorization.Authorize(
		command.Principal, "proposal.acceptance_grant.issue", target,
	); err != nil {
		return IssuedAcceptanceGrant{}, err
	}
	version, err := s.repository.FindProposalVersion(ctx, target, command.ProposalVersionID)
	if err != nil {
		return IssuedAcceptanceGrant{}, err
	}
	if version.State != ProposalIssued || version.PDFSnapshotID == "" {
		return IssuedAcceptanceGrant{}, ErrInvalidProposal
	}
	if version.RequiresInternalApproval {
		approved, err := s.repository.InternalApprovalSatisfied(ctx, target, version.ID)
		if err != nil {
			return IssuedAcceptanceGrant{}, err
		}
		if !approved {
			return IssuedAcceptanceGrant{}, ErrInternalApprovalRequired
		}
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return IssuedAcceptanceGrant{}, err
	}
	token := "rag_" + base64.RawURLEncoding.EncodeToString(secret)
	sum := sha256.Sum256([]byte(token))
	grantID, auditID, eventID, correlationID :=
		s.newID(), s.newID(), s.newID(), s.newID()
	grant := AcceptanceGrant{
		ID: grantID, MSPID: target.MSPID, ClientID: target.ClientID,
		ProposalVersionID: version.ID,
		SignerName:        strings.TrimSpace(command.SignerName),
		SignerEmail:       strings.TrimSpace(command.SignerEmail),
		Evidence:          command.Evidence, ExpiresAt: command.ExpiresAt.UTC(),
		IssuedAt: now, IssuedBy: command.ActorID,
		TokenSHA256: hex.EncodeToString(sum[:]),
	}
	accepted := AcceptanceGrantMutation{
		Grant: grant,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action:      "proposal.acceptance_grant.issued",
			SubjectType: "proposal_acceptance_grant", SubjectID: grantID,
			SubjectVersion: 1, Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "proposal.acceptance_grant.issued",
			SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "proposal_acceptance_grant", SubjectID: grantID,
			SubjectVersion: 1, Source: command.Source, CorrelationID: correlationID,
		},
	}
	if err := s.repository.IssueAcceptanceGrantAtomic(ctx, accepted); err != nil {
		return IssuedAcceptanceGrant{}, err
	}
	return IssuedAcceptanceGrant{Grant: grant, Token: token}, nil
}

type DecideInternalApprovalCommand struct {
	Principal         authorization.Principal
	Target            scope.Target
	ProposalVersionID string
	ExpectedVersion   int64
	Decision          string
	Reason            string
	ActorID           string
	Source            string
}

func (s *ProposalService) GetInternalApproval(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	proposalVersionID string,
) (InternalApproval, error) {
	target = proposalTarget(principal, target)
	if target.MSPID == "" || proposalVersionID == "" {
		return InternalApproval{}, ErrInvalidProposal
	}
	if err := authorization.Authorize(principal, "proposal.read", target); err != nil {
		return InternalApproval{}, err
	}
	return s.repository.FindInternalApproval(ctx, target, proposalVersionID)
}

func (s *ProposalService) DecideInternalApproval(
	ctx context.Context,
	command DecideInternalApprovalCommand,
) (InternalApproval, error) {
	target := proposalTarget(command.Principal, command.Target)
	decision := strings.ToLower(strings.TrimSpace(command.Decision))
	if target.MSPID == "" || command.ProposalVersionID == "" ||
		command.ExpectedVersion < 1 ||
		(decision != "approved" && decision != "rejected") ||
		strings.TrimSpace(command.Reason) == "" ||
		command.ActorID == "" || command.Source == "" {
		return InternalApproval{}, ErrInvalidProposal
	}
	if err := authorization.Authorize(command.Principal, "proposal.approve", target); err != nil {
		return InternalApproval{}, err
	}
	approval, err := s.repository.FindInternalApproval(ctx, target, command.ProposalVersionID)
	if err != nil {
		return InternalApproval{}, err
	}
	if err := object.RequireVersion(approval.Version, command.ExpectedVersion); err != nil {
		return InternalApproval{}, err
	}
	if approval.State != "pending" {
		return InternalApproval{}, object.ErrVersionConflict
	}
	now := s.now().UTC()
	approval.State, approval.ApproverID = decision, command.ActorID
	approval.Reason, approval.DecisionAt = strings.TrimSpace(command.Reason), now
	approval.Version++
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := InternalApprovalMutation{
		Approval: approval,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "proposal.internal_approval.decided", SubjectType: "approval",
			SubjectID: approval.ID, SubjectVersion: approval.Version,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "proposal.internal_approval.decided", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "approval", SubjectID: approval.ID,
			SubjectVersion: approval.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.DecideInternalApprovalAtomic(ctx, accepted); err != nil {
		return InternalApproval{}, err
	}
	return approval, nil
}

type SnapshotStore interface {
	PutImmutable(context.Context, SnapshotInput) (PDFSnapshot, error)
}

type VerifiedElectronicAcceptance struct {
	GrantID     string
	SignerName  string
	SignerEmail string
	Evidence    map[string]string
	AcceptedAt  time.Time
}

type ElectronicAcceptanceVerifier interface {
	VerifyAndConsume(context.Context, scope.Target, string, string) (VerifiedElectronicAcceptance, error)
}

type ProposalService struct {
	repository         ProposalRepository
	snapshots          SnapshotStore
	acceptanceVerifier ElectronicAcceptanceVerifier
	now                func() time.Time
	newID              func() string
}

func NewProposalService(
	repository ProposalRepository,
	snapshots SnapshotStore,
	acceptanceVerifier ElectronicAcceptanceVerifier,
	now func() time.Time,
	newID func() string,
) *ProposalService {
	return &ProposalService{
		repository: repository, snapshots: snapshots,
		acceptanceVerifier: acceptanceVerifier, now: now, newID: newID,
	}
}

type IssueVersionCommand struct {
	Principal               authorization.Principal
	Target                  scope.Target
	ProposalID              string
	ExpectedProposalVersion int64
	Currency                string
	Lines                   []ProposalLine
	ApprovalRule            ApprovalRule
	ExpiresAt               *time.Time
	ActorID                 string
	Source                  string
}

func (s *ProposalService) IssueVersion(ctx context.Context, command IssueVersionCommand) (ProposalVersion, error) {
	target := proposalTarget(command.Principal, command.Target)
	if target.MSPID == "" || command.ProposalID == "" ||
		command.ExpectedProposalVersion < 0 || len(command.Lines) == 0 ||
		len(command.Currency) != 3 || command.ActorID == "" || command.Source == "" {
		return ProposalVersion{}, ErrInvalidProposal
	}
	if err := authorization.Authorize(command.Principal, "proposal.issue", target); err != nil {
		return ProposalVersion{}, err
	}
	proposal, err := s.repository.FindProposal(ctx, target, command.ProposalID)
	if err != nil {
		return ProposalVersion{}, err
	}
	if proposal.MSPID != target.MSPID || proposal.ClientID != target.ClientID {
		return ProposalVersion{}, scope.ErrNotFound
	}
	if err := object.RequireVersion(proposal.CurrentVersion, command.ExpectedProposalVersion); err != nil {
		return ProposalVersion{}, err
	}
	calculated, err := calculateProposal(command.Currency, command.Lines)
	if err != nil {
		return ProposalVersion{}, err
	}
	now := s.now().UTC()
	versionID := s.newID()
	for index := range calculated.lines {
		calculated.lines[index].ID = s.newID()
	}
	version := ProposalVersion{
		ID: versionID, ProposalID: proposal.ID, MSPID: target.MSPID, ClientID: target.ClientID,
		Version: proposal.CurrentVersion + 1, State: ProposalIssued, Currency: command.Currency,
		Lines: calculated.lines, Subtotal: calculated.subtotal, TaxTotal: calculated.tax,
		Total: calculated.total, Cost: calculated.cost, Margin: calculated.margin,
		RequiresInternalApproval: requiresApproval(calculated, command.ApprovalRule),
		IssuedAt:                 now, IssuedBy: command.ActorID, ExpiresAt: command.ExpiresAt,
	}
	snapshotData, err := renderProposalPDF(version)
	if err != nil {
		return ProposalVersion{}, err
	}
	snapshot, err := s.snapshots.PutImmutable(ctx, SnapshotInput{
		ProposalVersionID: version.ID, Data: snapshotData,
	})
	if err != nil {
		return ProposalVersion{}, err
	}
	version.PDFSnapshotID = snapshot.ID
	proposal.CurrentVersion = version.Version
	proposal.State = ProposalIssued
	proposal.Version++
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := IssueMutation{
		Proposal: proposal, Version: version, Snapshot: snapshot,
		Audit: mutation.AuditRecord{ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID, ActorType: "technician", ActorID: command.ActorID, Action: "proposal_version.issued", SubjectType: "proposal_version", SubjectID: version.ID, SubjectVersion: version.Version, Source: command.Source, CorrelationID: correlationID},
		Event: mutation.EventRecord{EventID: eventID, EventType: "proposal_version.issued", SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID, ActorType: "technician", ActorID: command.ActorID, SubjectType: "proposal_version", SubjectID: version.ID, SubjectVersion: version.Version, Source: command.Source, CorrelationID: correlationID},
	}
	if err := s.repository.IssueVersionAtomic(ctx, accepted); err != nil {
		return ProposalVersion{}, err
	}
	return version, nil
}

func (s *ProposalService) UpdateIssuedVersion(context.Context, string, int64, []ProposalLine) error {
	return ErrProposalVersionImmutable
}

type ElectronicAcceptanceCommand struct {
	Principal         authorization.Principal
	Target            scope.Target
	ProposalVersionID string
	AcceptanceGrant   string
	Source            string
}

func (s *ProposalService) AcceptElectronically(ctx context.Context, command ElectronicAcceptanceCommand) (Acceptance, error) {
	target := proposalTarget(command.Principal, command.Target)
	if target.MSPID == "" || strings.TrimSpace(command.ProposalVersionID) == "" ||
		strings.TrimSpace(command.AcceptanceGrant) == "" || strings.TrimSpace(command.Source) == "" ||
		s.acceptanceVerifier == nil {
		return Acceptance{}, ErrInvalidAcceptanceGrant
	}
	if err := authorization.Authorize(command.Principal, "proposal.accept", target); err != nil {
		return Acceptance{}, err
	}
	verified, err := s.acceptanceVerifier.VerifyAndConsume(
		ctx, target, command.ProposalVersionID, command.AcceptanceGrant,
	)
	if err != nil {
		return Acceptance{}, ErrInvalidAcceptanceGrant
	}
	if strings.TrimSpace(verified.GrantID) == "" ||
		strings.TrimSpace(verified.SignerName) == "" ||
		strings.TrimSpace(verified.SignerEmail) == "" ||
		verified.AcceptedAt.IsZero() || len(verified.Evidence) == 0 {
		return Acceptance{}, ErrInvalidAcceptanceGrant
	}
	evidence := make(map[string]string, len(verified.Evidence)+1)
	for key, value := range verified.Evidence {
		evidence[key] = value
	}
	evidence["acceptance_grant_id"] = verified.GrantID
	return s.accept(ctx, acceptanceCommand{
		principal: command.Principal, target: command.Target,
		versionID: command.ProposalVersionID, signerName: verified.SignerName,
		signerEmail: verified.SignerEmail, evidence: evidence,
		acceptedAt: verified.AcceptedAt, source: command.Source,
		method: AcceptanceElectronic, capability: "proposal.accept",
	})
}

type OfflineAcceptanceCommand struct {
	Principal         authorization.Principal
	Target            scope.Target
	ProposalVersionID string
	SignerName        string
	SignerEmail       string
	RecordedBy        string
	AcceptedAt        time.Time
	Source            string
}

func (s *ProposalService) RecordOfflineAcceptance(ctx context.Context, command OfflineAcceptanceCommand) (Acceptance, error) {
	if command.RecordedBy == "" {
		return Acceptance{}, ErrSignerEvidenceRequired
	}
	return s.accept(ctx, acceptanceCommand{
		principal: command.Principal, target: command.Target,
		versionID: command.ProposalVersionID, signerName: command.SignerName,
		signerEmail: command.SignerEmail, recordedBy: command.RecordedBy,
		acceptedAt: command.AcceptedAt, source: command.Source,
		method: AcceptanceOffline, capability: "proposal.acceptance.record",
	})
}

type acceptanceCommand struct {
	principal                                              authorization.Principal
	target                                                 scope.Target
	versionID, signerName, signerEmail, recordedBy, source string
	evidence                                               map[string]string
	acceptedAt                                             time.Time
	method                                                 AcceptanceMethod
	capability                                             string
}

func (s *ProposalService) accept(ctx context.Context, command acceptanceCommand) (Acceptance, error) {
	target := proposalTarget(command.principal, command.target)
	if target.MSPID == "" || command.versionID == "" ||
		strings.TrimSpace(command.signerName) == "" || command.acceptedAt.IsZero() || command.source == "" {
		return Acceptance{}, ErrSignerEvidenceRequired
	}
	if err := authorization.Authorize(command.principal, command.capability, target); err != nil {
		return Acceptance{}, err
	}
	version, err := s.repository.FindProposalVersion(ctx, target, command.versionID)
	if err != nil {
		return Acceptance{}, err
	}
	if version.MSPID != target.MSPID || version.ClientID != target.ClientID {
		return Acceptance{}, scope.ErrNotFound
	}
	if version.State != ProposalIssued || version.PDFSnapshotID == "" {
		return Acceptance{}, ErrInvalidProposal
	}
	if version.RequiresInternalApproval {
		approved, err := s.repository.InternalApprovalSatisfied(ctx, target, version.ID)
		if err != nil {
			return Acceptance{}, err
		}
		if !approved {
			return Acceptance{}, ErrInternalApprovalRequired
		}
	}
	now := s.now().UTC()
	acceptanceID := s.newID()
	acceptance := Acceptance{
		ID: acceptanceID, ProposalID: version.ProposalID, ProposalVersionID: version.ID,
		MSPID: target.MSPID, ClientID: target.ClientID, Method: command.method,
		SignerName: strings.TrimSpace(command.signerName), SignerEmail: strings.TrimSpace(command.signerEmail),
		AcceptedAt: command.acceptedAt.UTC(), RecordedBy: command.recordedBy,
		Evidence: command.evidence, PDFSnapshotID: version.PDFSnapshotID,
	}
	proposal, err := s.repository.FindProposal(ctx, target, version.ProposalID)
	if err != nil {
		return Acceptance{}, err
	}
	proposal.State = ProposalAccepted
	proposal.Version++
	version.State = ProposalAccepted
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	actorType, actorID := "customer", command.signerEmail
	if command.method == AcceptanceOffline {
		actorType, actorID = "technician", command.recordedBy
	}
	mutation := AcceptanceMutation{
		Proposal: proposal, Version: version, Acceptance: acceptance,
		Audit: mutation.AuditRecord{ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID, ActorType: actorType, ActorID: actorID, Action: "proposal.accepted", SubjectType: "proposal_version", SubjectID: version.ID, SubjectVersion: version.Version, Source: command.source, CorrelationID: correlationID},
		Event: mutation.EventRecord{EventID: eventID, EventType: "proposal.accepted", SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID, ActorType: actorType, ActorID: actorID, SubjectType: "proposal_version", SubjectID: version.ID, SubjectVersion: version.Version, Source: command.source, CorrelationID: correlationID},
	}
	if err := s.repository.RecordAcceptanceAtomic(ctx, mutation); err != nil {
		return Acceptance{}, err
	}
	return acceptance, nil
}

func (s *ProposalService) GetAcceptedSnapshot(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	proposalID string,
) (PDFSnapshot, error) {
	target = proposalTarget(principal, target)
	if err := authorization.Authorize(principal, "proposal.read", target); err != nil {
		return PDFSnapshot{}, err
	}
	return s.repository.FindAcceptedSnapshot(ctx, target, proposalID)
}

type proposalCalculation struct {
	lines                              []ProposalLine
	subtotal, tax, total, cost, margin Money
}

func calculateProposal(currency string, lines []ProposalLine) (proposalCalculation, error) {
	result := proposalCalculation{
		lines:    append([]ProposalLine(nil), lines...),
		subtotal: Money{Currency: currency}, tax: Money{Currency: currency},
		total: Money{Currency: currency}, cost: Money{Currency: currency},
		margin: Money{Currency: currency},
	}
	for index := range result.lines {
		line := &result.lines[index]
		if !validLineType(line.Type) || strings.TrimSpace(line.Description) == "" ||
			line.Quantity < 0 || strings.TrimSpace(line.TaxTreatment) == "" ||
			line.UnitPrice.Currency != currency || line.UnitCost.Currency != currency ||
			line.Discount.Currency != "" && line.Discount.Currency != currency ||
			line.Tax.Currency != "" && line.Tax.Currency != currency ||
			line.UnitPrice.Minor < 0 || line.UnitCost.Minor < 0 ||
			line.Discount.Minor < 0 || line.Tax.Minor < 0 ||
			(line.Type == RecurringService && strings.TrimSpace(line.Recurrence) == "") {
			return proposalCalculation{}, ErrInvalidProposal
		}
		gross := line.Quantity * line.UnitPrice.Minor
		if line.Discount.Minor > gross {
			return proposalCalculation{}, ErrInvalidProposal
		}
		result.subtotal.Minor += gross - line.Discount.Minor
		result.cost.Minor += line.Quantity * line.UnitCost.Minor
		result.tax.Minor += line.Tax.Minor
	}
	result.total.Minor = result.subtotal.Minor + result.tax.Minor
	result.margin.Minor = result.subtotal.Minor - result.cost.Minor
	return result, nil
}

func requiresApproval(calculation proposalCalculation, rule ApprovalRule) bool {
	if rule.MaximumWithoutApprovalMinor > 0 &&
		calculation.total.Minor > rule.MaximumWithoutApprovalMinor {
		return true
	}
	if rule.MinimumMarginBasisPoints > 0 && calculation.subtotal.Minor > 0 {
		marginBPS := calculation.margin.Minor * 10000 / calculation.subtotal.Minor
		return marginBPS < rule.MinimumMarginBasisPoints
	}
	return false
}

func validLineType(lineType ProposalLineType) bool {
	switch lineType {
	case FixedFee, TimeAndMaterials, ProductLicense, RecurringService:
		return true
	default:
		return false
	}
}

func proposalTarget(principal authorization.Principal, target scope.Target) scope.Target {
	if target.MSPID == "" {
		return scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID}
	}
	return target
}

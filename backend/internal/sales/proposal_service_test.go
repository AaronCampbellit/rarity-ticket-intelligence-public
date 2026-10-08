package sales

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type fakeProposalRepository struct {
	proposal           Proposal
	opportunity        Opportunity
	version            ProposalVersion
	approvalOK         bool
	approval           InternalApproval
	decision           InternalApprovalMutation
	grant              AcceptanceGrantMutation
	created            CreateProposalMutation
	listed             []Proposal
	issued             IssueMutation
	accepted           AcceptanceMutation
	resolved           []Proposal
	target             scope.Target
	reference          string
	limit              int
	available          bool
	availableTarget    scope.Target
	availableDisplayID string
	availableCalls     int
	availabilityErr    error
	createErr          error
}

func (r *fakeProposalRepository) FindProposal(_ context.Context, _ scope.Target, _ string) (Proposal, error) {
	return r.proposal, nil
}
func (r *fakeProposalRepository) ListProposals(_ context.Context, _ scope.Target, _ ProposalListFilter) ([]Proposal, error) {
	return r.listed, nil
}
func (r *fakeProposalRepository) FindProposalsByReference(_ context.Context, target scope.Target, reference string, limit int) ([]Proposal, error) {
	r.target, r.reference, r.limit = target, reference, limit
	return r.resolved, nil
}
func (r *fakeProposalRepository) FindOpportunityForProposal(_ context.Context, _ scope.Target, _ string) (Opportunity, error) {
	if r.opportunity.ID != "" {
		return r.opportunity, nil
	}
	return Opportunity{
		ID: "opportunity-id", MSPID: "msp-id",
		ClientID: r.proposal.ClientID, ProspectID: r.proposal.ProspectID,
	}, nil
}
func (r *fakeProposalRepository) ProposalDisplayIDAvailable(_ context.Context, target scope.Target, displayID string) (bool, error) {
	r.availableTarget, r.availableDisplayID, r.availableCalls = target, displayID, r.availableCalls+1
	return r.available, r.availabilityErr
}
func (r *fakeProposalRepository) CreateProposalAtomic(_ context.Context, mutation CreateProposalMutation) error {
	r.created = mutation
	return r.createErr
}
func (r *fakeProposalRepository) FindProposalVersion(_ context.Context, _ scope.Target, _ string) (ProposalVersion, error) {
	return r.version, nil
}

func TestCreateProposalBindsActiveOpportunitySourceAndWritesFacts(t *testing.T) {
	repository := &fakeProposalRepository{available: true, proposal: Proposal{ClientID: "client-id"}, opportunity: Opportunity{
		ID: "opportunity-id", MSPID: "msp-id", ClientID: "client-id", DisplayID: "OPP-100", Name: "Northwind onboarding", PipelineID: "pipeline-id", StageID: "stage-id", Version: 1,
	}}
	ids := []string{"proposal-id", "audit-id", "event-id", "correlation-id"}
	service := NewProposalService(repository, &fakeSnapshotStore{}, nil, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	principal := authorization.Principal{
		ID:           "actor-id",
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("proposal.create"),
	}
	proposal, err := service.CreateProposal(context.Background(), CreateProposalCommand{
		Principal: principal, OpportunityID: "opportunity-id",
		ExpectedClientVersion: 7, DisplayID: "PROP-100", ActorID: "actor-id", Source: "api",
	})
	if err != nil || proposal.ID != "proposal-id" ||
		proposal.OpportunityID != "opportunity-id" || proposal.State != ProposalDraft {
		t.Fatalf("CreateProposal() proposal=%+v error=%v", proposal, err)
	}
	if repository.created.ExpectedClientVersion != 7 || repository.created.Audit.Action != "proposal.created" ||
		repository.created.Event.EventType != "proposal.created" {
		t.Fatalf("unexpected create mutation: %+v", repository.created)
	}
}

func TestCreateProposalPreservesMSPScopedProspectSupport(t *testing.T) {
	repository := &fakeProposalRepository{available: true, opportunity: Opportunity{
		ID: "opportunity-id", MSPID: "msp-id", ProspectID: "prospect-id", Version: 2,
	}}
	ids := []string{"proposal-id", "audit-id", "event-id", "correlation-id"}
	service := NewProposalService(repository, &fakeSnapshotStore{}, nil, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	principal := proposalPrincipal("proposal.create")
	principal.Scope.ClientID = ""
	proposal, err := service.CreateProposal(context.Background(), CreateProposalCommand{
		Principal: principal, Target: scope.Target{MSPID: "msp-id"}, OpportunityID: "opportunity-id",
		DisplayID: "PROP-2042", ActorID: "actor-id", Source: "api",
	})
	if err != nil || proposal.ProspectID != "prospect-id" || proposal.ClientID != "" {
		t.Fatalf("CreateProposal() proposal=%+v error=%v, want MSP-scoped prospect draft", proposal, err)
	}
}

func TestProposalDraftPreflightChecksAuthorizedActiveOpportunityAndDisplayAvailabilityWithoutWrites(t *testing.T) {
	repository := &fakeProposalRepository{
		available: true,
		opportunity: Opportunity{
			ID: "opportunity-id", MSPID: "msp-id", ClientID: "client-id",
			DisplayID: "OPP-2042", Name: "Northwind onboarding",
			PipelineID: "pipeline-id", StageID: "stage-id", Version: 4,
		},
	}
	service := testProposalService(repository)
	principal := proposalPrincipal("proposal.create")

	preflight, err := service.PreflightCreateProposal(context.Background(), CreateProposalCommand{
		Principal: principal, Target: scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		OpportunityID: "opportunity-id", DisplayID: "PROP-2042",
		ActorID: "actor-id", Source: "ai_workspace",
	})
	if err != nil {
		t.Fatalf("PreflightCreateProposal() error=%v", err)
	}
	if preflight.Opportunity.ID != "opportunity-id" || preflight.Opportunity.Version != 4 ||
		preflight.DisplayID != "PROP-2042" {
		t.Fatalf("PreflightCreateProposal()=%+v, want canonical opportunity and display ID", preflight)
	}
	if repository.availableCalls != 1 || repository.availableTarget != (scope.Target{MSPID: "msp-id", ClientID: "client-id"}) ||
		repository.availableDisplayID != "PROP-2042" {
		t.Fatalf("availability check=%d target=%+v display_id=%q", repository.availableCalls, repository.availableTarget, repository.availableDisplayID)
	}
	if repository.created.Proposal.ID != "" || repository.created.Audit.ID != "" || repository.created.Event.EventID != "" {
		t.Fatalf("preflight wrote proposal facts: %+v", repository.created)
	}
}

func TestProposalDraftPreflightRejectsUnavailableOrIneligibleSourceWithoutWrites(t *testing.T) {
	repositoryUnavailable := errors.New("repository unavailable")
	for _, test := range []struct {
		name       string
		repository *fakeProposalRepository
		errWant    error
	}{
		{
			name: "duplicate display ID",
			repository: &fakeProposalRepository{available: false, opportunity: Opportunity{
				ID: "opportunity-id", MSPID: "msp-id", ClientID: "client-id", DisplayID: "OPP-2042", Name: "Northwind onboarding", PipelineID: "pipeline-id", StageID: "stage-id", Version: 4,
			}},
			errWant: ErrInvalidProposal,
		},
		{
			name: "repository availability failure",
			repository: &fakeProposalRepository{available: true, availabilityErr: repositoryUnavailable, opportunity: Opportunity{
				ID: "opportunity-id", MSPID: "msp-id", ClientID: "client-id", DisplayID: "OPP-2042", Name: "Northwind onboarding", PipelineID: "pipeline-id", StageID: "stage-id", Version: 4,
			}},
			errWant: repositoryUnavailable,
		},
		{
			name: "source identity mismatch",
			repository: &fakeProposalRepository{available: true, opportunity: Opportunity{
				ID: "different-opportunity", MSPID: "msp-id", ClientID: "client-id", DisplayID: "OPP-2042", Name: "Northwind onboarding", PipelineID: "pipeline-id", StageID: "stage-id", Version: 4,
			}},
			errWant: scope.ErrNotFound,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := testProposalService(test.repository).PreflightCreateProposal(context.Background(), CreateProposalCommand{
				Principal: proposalPrincipal("proposal.create"), Target: scope.Target{MSPID: "msp-id", ClientID: "client-id"},
				OpportunityID: "opportunity-id", DisplayID: "PROP-2042", ActorID: "actor-id", Source: "ai_workspace",
			})
			if !errors.Is(err, test.errWant) {
				t.Fatalf("PreflightCreateProposal() error=%v, want %v", err, test.errWant)
			}
			if test.repository.created.Proposal.ID != "" || test.repository.created.Audit.ID != "" || test.repository.created.Event.EventID != "" {
				t.Fatalf("failed preflight wrote proposal facts: %+v", test.repository.created)
			}
		})
	}
}

func TestProposalDraftPreflightRejectsOpportunityVersionDriftWithoutWrites(t *testing.T) {
	repository := &fakeProposalRepository{available: true, opportunity: Opportunity{
		ID: "opportunity-id", MSPID: "msp-id", ClientID: "client-id", DisplayID: "OPP-2042", Name: "Northwind onboarding", PipelineID: "pipeline-id", StageID: "stage-id", Version: 4,
	}}
	_, err := testProposalService(repository).PreflightCreateProposal(context.Background(), CreateProposalCommand{
		Principal: proposalPrincipal("proposal.create"), Target: scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		OpportunityID: "opportunity-id", ExpectedOpportunityVersion: 3, DisplayID: "PROP-2042", ActorID: "actor-id", Source: "ai_workspace",
	})
	if !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("PreflightCreateProposal() error=%v, want version conflict", err)
	}
	if repository.created.Proposal.ID != "" || repository.created.Audit.ID != "" || repository.created.Event.EventID != "" {
		t.Fatalf("stale preflight wrote proposal facts: %+v", repository.created)
	}
}

func TestListProposalsValidatesStateAndPairedCursor(t *testing.T) {
	repository := &fakeProposalRepository{listed: []Proposal{{ID: "proposal-id"}}}
	service := testProposalService(repository)
	principal := proposalPrincipal("proposal.read")
	found, err := service.ListProposals(
		context.Background(), principal, scope.Target{},
		ProposalListFilter{State: ProposalIssued, Limit: 200},
	)
	if err != nil || len(found) != 1 {
		t.Fatalf("ListProposals() found=%+v error=%v", found, err)
	}
	_, err = service.ListProposals(
		context.Background(), principal, scope.Target{},
		ProposalListFilter{BeforeID: "proposal-id"},
	)
	if !errors.Is(err, ErrInvalidProposal) {
		t.Fatalf("unpaired cursor error=%v", err)
	}
}

func TestResolveProposalReferenceUsesExplicitAuthorizedClientAndBoundedExactLookup(t *testing.T) {
	repository := &fakeProposalRepository{
		resolved: []Proposal{{ID: "proposal-id", DisplayID: "PROP-100"}},
	}
	service := testProposalService(repository)
	principal := proposalPrincipal("proposal.read")
	principal.Scope.ClientID = ""
	target := scope.Target{MSPID: "msp-id", ClientID: "client-id"}

	found, err := service.ResolveProposalReference(
		context.Background(), principal, target, "  PROP-100  ", 2,
	)
	if err != nil || len(found) != 1 {
		t.Fatalf("ResolveProposalReference() found=%+v error=%v", found, err)
	}
	if repository.target != target || repository.reference != "PROP-100" ||
		repository.limit != 2 || principal.Scope.ClientID != "" {
		t.Fatalf("target=%+v reference=%q limit=%d principal=%+v",
			repository.target, repository.reference, repository.limit, principal.Scope)
	}

	clientPrincipal := proposalPrincipal("proposal.read")
	if _, err := service.ResolveProposalReference(
		context.Background(), clientPrincipal,
		scope.Target{MSPID: "msp-id", ClientID: "other-client"}, "PROP-100", 2,
	); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-client resolver error=%v, want scope.ErrNotFound", err)
	}
	for _, limit := range []int{0, 3} {
		if _, err := service.ResolveProposalReference(
			context.Background(), principal, target, "PROP-100", limit,
		); !errors.Is(err, ErrInvalidProposal) {
			t.Fatalf("limit %d error=%v, want ErrInvalidProposal", limit, err)
		}
	}
}

func TestGetInternalApprovalRequiresProposalReadAndReturnsVersion(t *testing.T) {
	repository := &fakeProposalRepository{approval: InternalApproval{
		ID: "approval-id", ProposalVersionID: "version-id",
		State: "pending", Version: 2,
	}}
	service := testProposalService(repository)
	found, err := service.GetInternalApproval(
		context.Background(), proposalPrincipal("proposal.read"),
		scope.Target{}, "version-id",
	)
	if err != nil || found.Version != 2 {
		t.Fatalf("GetInternalApproval() found=%+v error=%v", found, err)
	}
	if _, err := service.GetInternalApproval(
		context.Background(), proposalPrincipal(),
		scope.Target{}, "version-id",
	); err == nil {
		t.Fatal("GetInternalApproval() without proposal.read unexpectedly passed")
	}
}
func (r *fakeProposalRepository) IssueVersionAtomic(_ context.Context, mutation IssueMutation) error {
	r.issued = mutation
	r.version = mutation.Version
	return nil
}
func (r *fakeProposalRepository) RecordAcceptanceAtomic(_ context.Context, mutation AcceptanceMutation) error {
	r.accepted = mutation
	return nil
}
func (r *fakeProposalRepository) InternalApprovalSatisfied(context.Context, scope.Target, string) (bool, error) {
	return r.approvalOK, nil
}
func (r *fakeProposalRepository) FindInternalApproval(context.Context, scope.Target, string) (InternalApproval, error) {
	return r.approval, nil
}
func (r *fakeProposalRepository) DecideInternalApprovalAtomic(_ context.Context, mutation InternalApprovalMutation) error {
	r.decision = mutation
	return nil
}
func (r *fakeProposalRepository) IssueAcceptanceGrantAtomic(_ context.Context, mutation AcceptanceGrantMutation) error {
	r.grant = mutation
	return nil
}
func (r *fakeProposalRepository) FindAcceptedSnapshot(context.Context, scope.Target, string) (PDFSnapshot, error) {
	return PDFSnapshot{ID: "snapshot-id", ProposalVersionID: r.version.ID}, nil
}

func TestIssueAcceptanceGrantReturnsOpaqueTokenAndStoresOnlyDigest(t *testing.T) {
	repository := &fakeProposalRepository{
		version: ProposalVersion{
			ID: "version-id", MSPID: "msp-id", ClientID: "client-id",
			State: ProposalIssued, PDFSnapshotID: "snapshot-id",
		},
	}
	at := time.Date(2026, time.July, 30, 20, 0, 0, 0, time.UTC)
	ids := []string{"grant-id", "audit-id", "event-id", "correlation-id"}
	service := NewProposalService(repository, &fakeSnapshotStore{}, nil,
		func() time.Time { return at }, func() string {
			id := ids[0]
			ids = ids[1:]
			return id
		})
	principal := authorization.Principal{
		ID:           "actor-id",
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("proposal.acceptance_grant.issue"),
	}
	issued, err := service.IssueAcceptanceGrant(context.Background(), IssueAcceptanceGrantCommand{
		Principal: principal, ProposalVersionID: "version-id",
		SignerName: "Alex Client", SignerEmail: "alex@example.com",
		Evidence:  map[string]string{"delivery": "email"},
		ExpiresAt: at.Add(48 * time.Hour), ActorID: "actor-id", Source: "api",
	})
	if err != nil || !strings.HasPrefix(issued.Token, "rag_") {
		t.Fatalf("IssueAcceptanceGrant() issued=%+v error=%v", issued, err)
	}
	sum := sha256.Sum256([]byte(issued.Token))
	if repository.grant.Grant.TokenSHA256 != hex.EncodeToString(sum[:]) ||
		strings.Contains(repository.grant.Grant.TokenSHA256, issued.Token) {
		t.Fatal("grant did not persist the token digest only")
	}
}

func TestDecideInternalApprovalRecordsReasonAndOptimisticVersion(t *testing.T) {
	repository := &fakeProposalRepository{approval: InternalApproval{
		ID: "approval-id", MSPID: "msp-id", ClientID: "client-id",
		ProposalVersionID: "version-id", State: "pending", Version: 1,
	}}
	at := time.Date(2026, time.July, 30, 19, 0, 0, 0, time.UTC)
	ids := []string{"audit-id", "event-id", "correlation-id"}
	service := NewProposalService(repository, &fakeSnapshotStore{}, nil,
		func() time.Time { return at }, func() string {
			id := ids[0]
			ids = ids[1:]
			return id
		})
	principal := authorization.Principal{
		ID:           "actor-id",
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("proposal.approve"),
	}
	approval, err := service.DecideInternalApproval(context.Background(), DecideInternalApprovalCommand{
		Principal: principal, ProposalVersionID: "version-id",
		ExpectedVersion: 1, Decision: "approved", Reason: "Margin exception accepted",
		ActorID: "actor-id", Source: "api",
	})
	if err != nil || approval.State != "approved" || approval.Version != 2 {
		t.Fatalf("DecideInternalApproval() approval=%+v error=%v", approval, err)
	}
	if repository.decision.Approval.Reason != "Margin exception accepted" ||
		repository.decision.Audit.Action != "proposal.internal_approval.decided" {
		t.Fatalf("unexpected decision mutation: %+v", repository.decision)
	}
}

type fakeSnapshotStore struct {
	snapshot PDFSnapshot
	data     []byte
}

type fakeElectronicAcceptanceVerifier struct {
	verified VerifiedElectronicAcceptance
	err      error
	target   scope.Target
	version  string
	grant    string
}

func (v *fakeElectronicAcceptanceVerifier) VerifyAndConsume(
	_ context.Context,
	target scope.Target,
	version string,
	grant string,
) (VerifiedElectronicAcceptance, error) {
	v.target, v.version, v.grant = target, version, grant
	return v.verified, v.err
}

func (s *fakeSnapshotStore) PutImmutable(_ context.Context, input SnapshotInput) (PDFSnapshot, error) {
	s.data = append([]byte(nil), input.Data...)
	s.snapshot = PDFSnapshot{
		ID: "snapshot-id", ProposalVersionID: input.ProposalVersionID,
		SHA256: "hash", StoredAt: time.Now(),
	}
	return s.snapshot, nil
}

func TestIssueProposalCalculatesFourLineTypesAndApprovalRequirement(t *testing.T) {
	repository := &fakeProposalRepository{proposal: Proposal{
		ID: "proposal-id", MSPID: "msp-id", ClientID: "client-id", CurrentVersion: 1,
	}}
	service := testProposalService(repository)
	version, err := service.IssueVersion(context.Background(), IssueVersionCommand{
		Principal:  proposalPrincipal("proposal.issue"),
		ProposalID: "proposal-id", ExpectedProposalVersion: 1, Currency: "USD",
		Lines:        validProposalLines(),
		ApprovalRule: ApprovalRule{MaximumWithoutApprovalMinor: 5000},
		ActorID:      "actor-id", Source: "web",
	})
	if err != nil {
		t.Fatalf("IssueVersion() error = %v", err)
	}
	if len(version.Lines) != 4 || version.Total.Minor != 11100 ||
		version.Margin.Minor != 5300 || !version.RequiresInternalApproval {
		t.Fatalf("unexpected issued version: %+v", version)
	}
	for index, line := range version.Lines {
		if line.ID == "" {
			t.Fatalf("line %d lacks durable identity", index)
		}
	}
	if repository.issued.Event.EventType != "proposal_version.issued" ||
		repository.issued.Snapshot.ID != "snapshot-id" {
		t.Fatal("issued version lacks event or immutable snapshot")
	}
	if !bytes.HasPrefix(service.snapshots.(*fakeSnapshotStore).data, []byte("%PDF-")) {
		t.Fatal("issued snapshot is not a PDF")
	}
}

func TestIssuedProposalVersionCannotBeMutated(t *testing.T) {
	service := testProposalService(&fakeProposalRepository{})
	err := service.UpdateIssuedVersion(context.Background(), "version-id", 1, validProposalLines())
	if !errors.Is(err, ErrProposalVersionImmutable) {
		t.Fatalf("UpdateIssuedVersion() error = %v", err)
	}
}

func TestIssueProposalSupportsMSPScopedProspectBeforeClientConversion(t *testing.T) {
	repository := &fakeProposalRepository{proposal: Proposal{
		ID: "proposal-id", MSPID: "msp-id", ProspectID: "prospect-id",
		CurrentVersion: 0,
	}}
	service := testProposalService(repository)
	principal := proposalPrincipal("proposal.issue")
	principal.Scope.ClientID = ""

	version, err := service.IssueVersion(context.Background(), IssueVersionCommand{
		Principal: principal, ProposalID: "proposal-id",
		ExpectedProposalVersion: 0, Currency: "USD",
		Lines: validProposalLines(), ActorID: "actor-id", Source: "web",
	})
	if err != nil {
		t.Fatalf("IssueVersion() prospect error = %v", err)
	}
	if version.ClientID != "" || repository.issued.Proposal.ProspectID != "prospect-id" {
		t.Fatalf("prospect proposal lost its pre-client scope: %+v", repository.issued)
	}
}

func TestOfflineAcceptanceRecordsExactVersionRecorderAndSnapshot(t *testing.T) {
	acceptedAt := time.Date(2026, time.July, 30, 4, 0, 0, 0, time.UTC)
	repository := &fakeProposalRepository{
		proposal: Proposal{
			ID: "proposal-id", MSPID: "msp-id", ClientID: "client-id",
			CurrentVersion: 2, State: ProposalIssued, Version: 1,
		},
		version: ProposalVersion{
			ID: "version-id", ProposalID: "proposal-id", MSPID: "msp-id",
			ClientID: "client-id", State: ProposalIssued, PDFSnapshotID: "snapshot-id",
		},
		approvalOK: true,
	}
	service := testProposalService(repository)
	accepted, err := service.RecordOfflineAcceptance(context.Background(), OfflineAcceptanceCommand{
		Principal:         proposalPrincipal("proposal.acceptance.record"),
		ProposalVersionID: "version-id", SignerName: "Alex Client",
		RecordedBy: "technician-id", AcceptedAt: acceptedAt, Source: "web",
	})
	if err != nil {
		t.Fatalf("RecordOfflineAcceptance() error = %v", err)
	}
	if accepted.ProposalVersionID != "version-id" ||
		accepted.RecordedBy != "technician-id" ||
		accepted.PDFSnapshotID != "snapshot-id" ||
		accepted.Method != AcceptanceOffline {
		t.Fatalf("unexpected acceptance: %+v", accepted)
	}
}

func TestElectronicAcceptanceRequiresSignerEvidenceAndInternalApproval(t *testing.T) {
	repository := &fakeProposalRepository{
		version: ProposalVersion{
			ID: "version-id", ProposalID: "proposal-id", MSPID: "msp-id",
			ClientID: "client-id", State: ProposalIssued, PDFSnapshotID: "snapshot-id",
			RequiresInternalApproval: true,
		},
		approvalOK: false,
	}
	service := testProposalService(repository)
	_, err := service.AcceptElectronically(context.Background(), ElectronicAcceptanceCommand{
		Principal:         proposalPrincipal("proposal.accept"),
		ProposalVersionID: "version-id", AcceptanceGrant: "signed-grant",
		Source: "public_acceptance",
	})
	if !errors.Is(err, ErrInternalApprovalRequired) {
		t.Fatalf("AcceptElectronically() error = %v", err)
	}
}

func TestElectronicAcceptanceUsesIdentityFromVerifiedGrant(t *testing.T) {
	acceptedAt := time.Date(2026, time.July, 30, 4, 0, 0, 0, time.UTC)
	repository := &fakeProposalRepository{
		proposal: Proposal{
			ID: "proposal-id", MSPID: "msp-id", ClientID: "client-id",
			CurrentVersion: 2, State: ProposalIssued, Version: 1,
		},
		version: ProposalVersion{
			ID: "version-id", ProposalID: "proposal-id", MSPID: "msp-id",
			ClientID: "client-id", State: ProposalIssued, PDFSnapshotID: "snapshot-id",
		},
		approvalOK: true,
	}
	verifier := &fakeElectronicAcceptanceVerifier{verified: VerifiedElectronicAcceptance{
		GrantID: "grant-id", SignerName: "Alex Client", SignerEmail: "alex@example.com",
		AcceptedAt: acceptedAt, Evidence: map[string]string{"challenge": "verified"},
	}}
	service := testProposalServiceWithVerifier(repository, verifier)

	accepted, err := service.AcceptElectronically(context.Background(), ElectronicAcceptanceCommand{
		Principal:         proposalPrincipal("proposal.accept"),
		ProposalVersionID: "version-id",
		AcceptanceGrant:   "signed-grant",
		Source:            "public_acceptance",
	})

	if err != nil {
		t.Fatalf("AcceptElectronically() error = %v", err)
	}
	if accepted.SignerEmail != "alex@example.com" ||
		accepted.AcceptedAt != acceptedAt ||
		accepted.Evidence["challenge"] != "verified" {
		t.Fatalf("acceptance did not use verified identity: %+v", accepted)
	}
	if repository.accepted.Audit.ActorType != "customer" ||
		repository.accepted.Audit.ActorID != "alex@example.com" {
		t.Fatalf("unexpected acceptance actor: %+v", repository.accepted.Audit)
	}
	if verifier.target != (scope.Target{MSPID: "msp-id", ClientID: "client-id"}) ||
		verifier.version != "version-id" || verifier.grant != "signed-grant" {
		t.Fatalf("verifier received wrong binding: target=%+v version=%q grant=%q", verifier.target, verifier.version, verifier.grant)
	}
}

func TestElectronicAcceptanceRejectsUnverifiedGrant(t *testing.T) {
	repository := &fakeProposalRepository{}
	service := testProposalServiceWithVerifier(
		repository,
		&fakeElectronicAcceptanceVerifier{err: ErrInvalidAcceptanceGrant},
	)

	_, err := service.AcceptElectronically(context.Background(), ElectronicAcceptanceCommand{
		Principal:         proposalPrincipal("proposal.accept"),
		ProposalVersionID: "version-id",
		AcceptanceGrant:   "forged-grant",
		Source:            "public_acceptance",
	})

	if !errors.Is(err, ErrInvalidAcceptanceGrant) {
		t.Fatalf("AcceptElectronically() error = %v", err)
	}
	if repository.accepted.Acceptance.ID != "" {
		t.Fatal("unverified grant recorded an acceptance")
	}
}

func validProposalLines() []ProposalLine {
	return []ProposalLine{
		{Type: FixedFee, Description: "Discovery", Quantity: 1, UnitPrice: Money{Minor: 4000, Currency: "USD"}, UnitCost: Money{Minor: 2000, Currency: "USD"}, TaxTreatment: "taxable", Tax: Money{Minor: 400, Currency: "USD"}},
		{Type: TimeAndMaterials, Description: "Engineering", Quantity: 2, UnitPrice: Money{Minor: 2000, Currency: "USD"}, UnitCost: Money{Minor: 1000, Currency: "USD"}, TaxTreatment: "non_taxable", Tax: Money{Currency: "USD"}},
		{Type: ProductLicense, Description: "License", Quantity: 1, UnitPrice: Money{Minor: 1000, Currency: "USD"}, UnitCost: Money{Minor: 700, Currency: "USD"}, TaxTreatment: "taxable", Tax: Money{Minor: 100, Currency: "USD"}},
		{Type: RecurringService, Description: "Managed service", Quantity: 1, UnitPrice: Money{Minor: 1500, Currency: "USD"}, UnitCost: Money{Minor: 500, Currency: "USD"}, TaxTreatment: "taxable", Tax: Money{Minor: 100, Currency: "USD"}, Recurrence: "monthly"},
	}
}

func proposalPrincipal(capabilities ...string) authorization.Principal {
	return authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet(capabilities...),
	}
}

func testProposalService(repository ProposalRepository) *ProposalService {
	return testProposalServiceWithVerifier(repository, &fakeElectronicAcceptanceVerifier{
		verified: VerifiedElectronicAcceptance{
			GrantID: "grant-id", SignerName: "Alex Client", SignerEmail: "alex@example.com",
			AcceptedAt: time.Now(), Evidence: map[string]string{"challenge": "verified"},
		},
	})
}

func testProposalServiceWithVerifier(
	repository ProposalRepository,
	verifier ElectronicAcceptanceVerifier,
) *ProposalService {
	ids := []string{"version-id", "audit-id", "event-id", "correlation-id", "acceptance-id", "audit-2", "event-2", "correlation-2"}
	return NewProposalService(repository, &fakeSnapshotStore{}, verifier, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
}

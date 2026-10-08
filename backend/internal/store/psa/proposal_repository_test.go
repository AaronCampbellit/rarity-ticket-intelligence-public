package psa

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestIssueProposalVersionAtomicWritesVersionLinesApprovalAndFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewProposalRepository(&fakeSalesDB{tx: tx}, func() string { return "approval-id" })
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	err := repository.IssueVersionAtomic(context.Background(), sales.IssueMutation{
		Proposal: sales.Proposal{
			ID: "proposal", MSPID: "msp", ClientID: "client",
			CurrentVersion: 2, State: sales.ProposalIssued, Version: 3,
		},
		Version: sales.ProposalVersion{
			ID: "version", ProposalID: "proposal", MSPID: "msp", ClientID: "client",
			Version: 2, Currency: "USD", RequiresInternalApproval: true,
			Lines: []sales.ProposalLine{{
				ID: "line", Type: sales.FixedFee, Description: "Discovery", Quantity: 1,
				UnitPrice: sales.Money{Minor: 1000}, UnitCost: sales.Money{Minor: 500},
				TaxTreatment: "taxable",
			}},
			PDFSnapshotID: "snapshot", IssuedAt: at, IssuedBy: "actor",
		},
		Audit: validAudit(at, "proposal_version.issued", "proposal_version", "version"),
		Event: validEvent(at, "proposal_version.issued", "proposal_version", "version"),
	})
	if err != nil {
		t.Fatalf("IssueVersionAtomic() error = %v", err)
	}
	assertQueryOrder(t, tx.queries,
		"UPDATE proposals", "INSERT INTO proposal_versions", "INSERT INTO proposal_lines",
		"INSERT INTO approvals", "INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestCreateProposalValidatesOpportunitySourceAndWritesFacts(t *testing.T) {
	tx := &fakeSalesTx{queryRow: proposalOpportunityVersionRow(1)}
	at := time.Date(2026, time.July, 30, 21, 0, 0, 0, time.UTC)
	err := NewProposalRepository(&fakeSalesDB{tx: tx}, func() string { return "id" }).
		CreateProposalAtomic(context.Background(), sales.CreateProposalMutation{
			Proposal: sales.Proposal{
				ID: "proposal", MSPID: "msp", ClientID: "client",
				OpportunityID: "opportunity", DisplayID: "PROP-100",
				State: sales.ProposalDraft, Version: 1,
			},
			ExpectedOpportunityVersion: 1,
			ExpectedClientVersion:      2,
			Audit:                      validAudit(at, "proposal.created", "proposal", "proposal"),
			Event:                      validEvent(at, "proposal.created", "proposal", "proposal"),
		})
	if err != nil {
		t.Fatalf("CreateProposalAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.calls,
		"FROM client_organizations", "FROM opportunities", "INSERT INTO proposals",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestCreateProposalAtomicRejectsInactiveOrStaleClientBeforeProposalFacts(t *testing.T) {
	for _, test := range []struct {
		name string
	}{
		{name: "inactive Client"},
		{name: "Client version drift"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeSalesTx{zeroRowsAt: 1}
			err := NewProposalRepository(&fakeSalesDB{tx: tx}, func() string { return "id" }).
				CreateProposalAtomic(context.Background(), sales.CreateProposalMutation{
					Proposal:                   sales.Proposal{ID: "proposal", MSPID: "msp", ClientID: "client", OpportunityID: "opportunity", DisplayID: "PROP-2042", State: sales.ProposalDraft, Version: 1},
					ExpectedClientVersion:      7,
					ExpectedOpportunityVersion: 4,
					Audit:                      validAudit(time.Now().UTC(), "proposal.created", "proposal", "proposal"),
					Event:                      validEvent(time.Now().UTC(), "proposal.created", "proposal", "proposal"),
				})
			if !errors.Is(err, scope.ErrNotFound) || len(tx.queries) != 1 || tx.committed || !tx.rolledBack {
				t.Fatalf("CreateProposalAtomic() error=%v queries=%d committed=%t rolled_back=%t", err, len(tx.queries), tx.committed, tx.rolledBack)
			}
			if !strings.Contains(tx.queries[0], "FROM client_organizations") ||
				!strings.Contains(tx.queries[0], "lifecycle_state = 'active'") ||
				!strings.Contains(tx.queries[0], "($3 = 0 OR version = $3)") ||
				len(tx.args[0]) != 3 || tx.args[0][2] != int64(7) {
				t.Fatalf("Client fence query=%q args=%+v", tx.queries[0], tx.args)
			}
		})
	}
}

func TestCreateProposalAtomicPreservesMSPScopedProspectPathWithoutClientLock(t *testing.T) {
	tx := &fakeSalesTx{queryRow: proposalOpportunityVersionRow(4)}
	err := NewProposalRepository(&fakeSalesDB{tx: tx}, func() string { return "id" }).
		CreateProposalAtomic(context.Background(), sales.CreateProposalMutation{
			Proposal:                   sales.Proposal{ID: "proposal", MSPID: "msp", ProspectID: "prospect", OpportunityID: "opportunity", DisplayID: "PROP-2042", State: sales.ProposalDraft, Version: 1},
			ExpectedOpportunityVersion: 4,
			Audit:                      validAudit(time.Now().UTC(), "proposal.created", "proposal", "proposal"),
			Event:                      validEvent(time.Now().UTC(), "proposal.created", "proposal", "proposal"),
		})
	if err != nil || len(tx.calls) != 4 ||
		strings.Contains(tx.calls[0], "client_organizations") ||
		!strings.Contains(tx.calls[0], "FROM opportunities") {
		t.Fatalf("prospect proposal error=%v calls=%+v", err, tx.calls)
	}
}

func TestProposalDisplayIDAvailabilityIsReadOnlyAndMSPScoped(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*bool) = true
	}}}
	available, err := NewProposalRepository(db, func() string { return "id" }).
		ProposalDisplayIDAvailable(context.Background(), scope.Target{MSPID: "msp", ClientID: "client"}, "PROP-2042")
	if err != nil || !available {
		t.Fatalf("ProposalDisplayIDAvailable() available=%t error=%v", available, err)
	}
	if !strings.Contains(db.query, "NOT EXISTS") || !strings.Contains(db.query, "msp_id = $1") ||
		strings.Contains(db.query, "INSERT INTO") {
		t.Fatalf("availability check must be scoped read-only: %s", db.query)
	}
	if len(db.args) != 2 || db.args[0] != "msp" || db.args[1] != "PROP-2042" {
		t.Fatalf("availability args=%+v", db.args)
	}
}

func TestCreateProposalAtomicRollsBackProposalAndFactsOnDownstreamRepositoryFailure(t *testing.T) {
	for _, test := range []struct {
		name      string
		failAt    int
		failedSQL string
	}{
		{name: "audit", failAt: 3, failedSQL: "INSERT INTO audit_ledger"},
		{name: "outbox", failAt: 4, failedSQL: "INSERT INTO event_outbox"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeSalesTx{
				failAt: test.failAt, queryRow: proposalOpportunityVersionRow(4),
			}
			err := NewProposalRepository(&fakeSalesDB{tx: tx}, func() string { return "id" }).
				CreateProposalAtomic(context.Background(), sales.CreateProposalMutation{
					Proposal:                   sales.Proposal{ID: "proposal", MSPID: "msp", ClientID: "client", OpportunityID: "opportunity", DisplayID: "PROP-2042", State: sales.ProposalDraft, Version: 1},
					ExpectedOpportunityVersion: 4,
					Audit:                      validAudit(time.Now().UTC(), "proposal.created", "proposal", "proposal"),
					Event:                      validEvent(time.Now().UTC(), "proposal.created", "proposal", "proposal"),
				})
			if err == nil || !tx.rolledBack || tx.committed {
				t.Fatalf("CreateProposalAtomic() error=%v committed=%t rolled_back=%t", err, tx.committed, tx.rolledBack)
			}
			if len(tx.queries) != test.failAt || !strings.Contains(tx.queries[1], "INSERT INTO proposals") ||
				!strings.Contains(tx.queries[test.failAt-1], test.failedSQL) {
				t.Fatalf("downstream failure queries=%+v, want Proposal before %s", tx.queries, test.failedSQL)
			}
		})
	}
}

func TestCreateProposalAtomicReturnsVersionConflictForOpportunityDriftBeforeFacts(t *testing.T) {
	tx := &fakeSalesTx{queryRow: proposalOpportunityVersionRow(5)}
	err := NewProposalRepository(&fakeSalesDB{tx: tx}, func() string { return "id" }).
		CreateProposalAtomic(context.Background(), sales.CreateProposalMutation{
			Proposal:                   sales.Proposal{ID: "proposal", MSPID: "msp", ProspectID: "prospect", OpportunityID: "opportunity", DisplayID: "PROP-2042", State: sales.ProposalDraft, Version: 1},
			ExpectedOpportunityVersion: 4,
			Audit:                      validAudit(time.Now().UTC(), "proposal.created", "proposal", "proposal"),
			Event:                      validEvent(time.Now().UTC(), "proposal.created", "proposal", "proposal"),
		})
	if !errors.Is(err, object.ErrVersionConflict) || len(tx.calls) != 1 || !tx.rolledBack {
		t.Fatalf("CreateProposalAtomic() error=%v calls=%d rollback=%t", err, len(tx.calls), tx.rolledBack)
	}
	if strings.Contains(tx.query, "version = $5") ||
		!strings.Contains(tx.query, "SELECT version") ||
		!strings.Contains(tx.query, "FOR SHARE") ||
		len(tx.queryArgs) != 4 {
		t.Fatalf("proposal create does not read the in-scope Opportunity version: query=%s args=%+v", tx.query, tx.queryArgs)
	}
}

func TestCreateProposalAtomicReturnsNotFoundForMissingOrOutOfScopeOpportunity(t *testing.T) {
	tx := &fakeSalesTx{queryRow: fakeRow{err: pgx.ErrNoRows}}
	err := NewProposalRepository(&fakeSalesDB{tx: tx}, func() string { return "id" }).
		CreateProposalAtomic(context.Background(), sales.CreateProposalMutation{
			Proposal:                   sales.Proposal{ID: "proposal", MSPID: "msp", ProspectID: "prospect", OpportunityID: "opportunity", DisplayID: "PROP-2042", State: sales.ProposalDraft, Version: 1},
			ExpectedOpportunityVersion: 4,
			Audit:                      validAudit(time.Now().UTC(), "proposal.created", "proposal", "proposal"),
			Event:                      validEvent(time.Now().UTC(), "proposal.created", "proposal", "proposal"),
		})
	if !errors.Is(err, scope.ErrNotFound) || len(tx.calls) != 1 || !tx.rolledBack {
		t.Fatalf("CreateProposalAtomic() error=%v calls=%d rollback=%t", err, len(tx.calls), tx.rolledBack)
	}
}

func proposalOpportunityVersionRow(version int64) row {
	return fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*int64) = version
	}}
}

func TestListProposalsUsesStableScopedCursor(t *testing.T) {
	at := time.Date(2026, time.July, 30, 21, 30, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*destinations[0].(*string) = "proposal"
			*destinations[1].(*string) = "msp"
			*destinations[2].(*string) = "client"
			*destinations[3].(*string) = ""
			*destinations[4].(*string) = "opportunity"
			*destinations[5].(*string) = "PROP-100"
			*destinations[6].(*int64) = 2
			*destinations[7].(*string) = "proposal-version"
			*destinations[8].(*sales.ProposalState) = sales.ProposalIssued
			*destinations[9].(*int64) = 3
			*destinations[10].(*time.Time) = at
		},
	}}}
	found, err := NewProposalRepository(db, func() string { return "id" }).
		ListProposals(
			context.Background(), scope.Target{MSPID: "msp", ClientID: "client"},
			sales.ProposalListFilter{State: sales.ProposalIssued, Limit: 25},
		)
	if err != nil || len(found) != 1 || found[0].DisplayID != "PROP-100" ||
		found[0].CurrentVersionID != "proposal-version" {
		t.Fatalf("ListProposals() found=%+v error=%v", found, err)
	}
	if !strings.Contains(db.query, "(updated_at, id) <") ||
		!strings.Contains(db.query, "client_id IS NOT DISTINCT") {
		t.Fatalf("proposal list lacks stable scoped cursor: %s", db.query)
	}
}

func TestResolveProposalReferenceIsExactDisplayIDClientScopedAndBounded(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	_, err := NewProposalRepository(db, func() string { return "id" }).
		FindProposalsByReference(
			context.Background(),
			scope.Target{MSPID: "msp", ClientID: "client"},
			"  PROP-100  ",
			8,
		)
	if err != nil {
		t.Fatalf("FindProposalsByReference() error=%v", err)
	}
	for _, fragment := range []string{
		"msp_id = $1", "client_id = $2::uuid",
		"lower(btrim(regexp_replace(", "translate(display_id, $4",
		"ORDER BY id", "LIMIT $5",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("proposal resolver missing %q: %s", fragment, db.query)
		}
	}
	if strings.Contains(db.query, "translate(name") ||
		strings.Contains(db.query, "OR lower") {
		t.Fatalf("proposal resolver accepts non-display identity: %s", db.query)
	}
	if len(db.args) != 5 || db.args[0] != "msp" || db.args[1] != "client" ||
		db.args[2] != "prop-100" || db.args[3] != unicodeReferenceWhitespace ||
		db.args[4] != 2 {
		t.Fatalf("proposal resolver args=%+v", db.args)
	}
}

func TestDecideInternalApprovalUpdatesPendingVersionAndWritesFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 30, 19, 0, 0, 0, time.UTC)
	err := NewProposalRepository(&fakeSalesDB{tx: tx}, func() string { return "id" }).
		DecideInternalApprovalAtomic(context.Background(), sales.InternalApprovalMutation{
			Approval: sales.InternalApproval{
				ID: "approval", MSPID: "msp", ClientID: "client",
				ProposalVersionID: "version", State: "approved",
				ApproverID: "actor", Reason: "Margin accepted",
				DecisionAt: at, Version: 2,
			},
			Audit: validAudit(at, "proposal.internal_approval.decided", "approval", "approval"),
			Event: validEvent(at, "proposal.internal_approval.decided", "approval", "approval"),
		})
	if err != nil {
		t.Fatalf("DecideInternalApprovalAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"UPDATE approvals", "INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestIssueAcceptanceGrantStoresDigestWithScopedVersionAndFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 30, 20, 0, 0, 0, time.UTC)
	err := NewProposalRepository(&fakeSalesDB{tx: tx}, func() string { return "id" }).
		IssueAcceptanceGrantAtomic(context.Background(), sales.AcceptanceGrantMutation{
			Grant: sales.AcceptanceGrant{
				ID: "grant", MSPID: "msp", ClientID: "client",
				ProposalVersionID: "version", SignerName: "Alex",
				SignerEmail: "alex@example.com",
				Evidence:    map[string]string{"delivery": "email"},
				ExpiresAt:   at.Add(48 * time.Hour), IssuedAt: at,
				IssuedBy: "actor", TokenSHA256: strings.Repeat("a", 64),
			},
			Audit: validAudit(at, "proposal.acceptance_grant.issued", "proposal_acceptance_grant", "grant"),
			Event: validEvent(at, "proposal.acceptance_grant.issued", "proposal_acceptance_grant", "grant"),
		})
	if err != nil {
		t.Fatalf("IssueAcceptanceGrantAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO proposal_acceptance_grants",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestIssueProposalVersionAtomicRejectsStaleProposal(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 1}
	repository := NewProposalRepository(&fakeSalesDB{tx: tx}, func() string { return "approval" })
	err := repository.IssueVersionAtomic(context.Background(), sales.IssueMutation{
		Proposal: sales.Proposal{ID: "proposal", MSPID: "msp", ClientID: "client", Version: 2},
	})
	if !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("IssueVersionAtomic() error = %v", err)
	}
	if len(tx.queries) != 1 || !tx.rolledBack {
		t.Fatalf("stale issue wrote dependent rows: %+v", tx)
	}
}

func TestRecordAcceptanceAtomicUpdatesProposalAndWritesEvidence(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewProposalRepository(&fakeSalesDB{tx: tx}, func() string { return "approval" })
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	err := repository.RecordAcceptanceAtomic(context.Background(), sales.AcceptanceMutation{
		Proposal: sales.Proposal{
			ID: "proposal", MSPID: "msp", ClientID: "client",
			CurrentVersion: 2, State: sales.ProposalAccepted, Version: 4,
		},
		Acceptance: sales.Acceptance{
			ID: "acceptance", ProposalID: "proposal", ProposalVersionID: "version",
			MSPID: "msp", ClientID: "client", Method: sales.AcceptanceElectronic,
			SignerName: "Alex", SignerEmail: "alex@example.com", AcceptedAt: at,
			Evidence: map[string]string{"grant": "verified"}, PDFSnapshotID: "snapshot",
		},
		Audit: validAudit(at, "proposal.accepted", "proposal_version", "version"),
		Event: validEvent(at, "proposal.accepted", "proposal_version", "version"),
	})
	if err != nil {
		t.Fatalf("RecordAcceptanceAtomic() error = %v", err)
	}
	assertQueryOrder(t, tx.queries,
		"UPDATE proposals", "INSERT INTO approvals",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	if !strings.Contains(tx.queries[0], "COALESCE($8::uuid, updated_by)") {
		t.Fatalf("electronic acceptance tries to persist a customer email as technician UUID: %s", tx.queries[0])
	}
}

func TestFindProposalIsClientScoped(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: errors.New("capture")}}
	_, _ = NewProposalRepository(db, func() string { return "id" }).FindProposal(
		context.Background(), scope.Target{MSPID: "msp", ClientID: "client"}, "proposal",
	)
	if !strings.Contains(db.query, "client_id = NULLIF($3, '')::uuid") {
		t.Fatalf("proposal lookup is not client scoped: %s", db.query)
	}
}

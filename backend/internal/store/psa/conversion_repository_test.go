package psa

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
)

func TestConvertAtomicPersistsEntireAcceptedMutation(t *testing.T) {
	tx := &fakeSalesTx{}
	next := 0
	repository := NewConversionRepository(&fakeSalesDB{tx: tx}, func() string {
		next++
		return "generated-id"
	})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	err := repository.ConvertAtomic(context.Background(), projects.ConversionMutation{
		Project: projects.Project{
			ID: "project", MSPID: "msp", ClientID: "client", DisplayID: "PRJ-1",
			Name: "Migration", OriginalProposalVersionID: "proposal-version",
			LifecycleState: "planned", Version: 1, CreatedAt: at, CreatedBy: "actor",
			Phases: []projects.Phase{{
				ID: "phase", ProjectID: "project", MSPID: "msp", ClientID: "client",
				Name: "Deliver", Position: 1, State: "planned", Version: 1,
			}},
		},
		OriginalBudget: projects.BudgetBaseline{Currency: "USD", RevenueMinor: 1000},
		CurrentBudget:  projects.BudgetBaseline{Currency: "USD", RevenueMinor: 1000},
		Opportunity: sales.Opportunity{
			ID: "opportunity", MSPID: "msp", ClientID: "client",
			StageID: "won", Version: 3, UpdatedAt: at, UpdatedBy: "actor",
		},
		TaskMoves: []projects.TaskMove{{
			Task: tasks.Task{
				ID: "task", MSPID: "msp", ClientID: "client",
				Parent:  tasks.Ref{Type: tasks.ParentProject, ID: "project"},
				Version: 2,
			},
			PreviousVersion: 1,
			History: tasks.MovementHistory{
				TaskID:          "task",
				From:            tasks.Ref{Type: tasks.ParentOpportunity, ID: "opportunity"},
				To:              tasks.Ref{Type: tasks.ParentProject, ID: "project"},
				PreviousVersion: 1, AcceptedVersion: 2, MovedAt: at, MovedBy: "actor",
			},
		}},
		Conversion: projects.ConversionRecord{
			ID: "conversion", OpportunityID: "opportunity",
			ProposalVersionID: "proposal-version", ProjectID: "project",
			MSPID: "msp", ClientID: "client", RequestKey: "request",
			PreviewHash: strings.Repeat("a", 64), ConvertedAt: at, ConvertedBy: "actor",
		},
		Audit:       validAudit(at, "opportunity.converted", "project", "project"),
		Event:       validEvent(at, "opportunity.converted", "project", "project"),
		InitialTags: testInitialTags(at),
	})
	if err != nil {
		t.Fatalf("ConvertAtomic() error = %v", err)
	}
	assertQueryOrder(t, tx.queries,
		"INSERT INTO projects", "INSERT INTO object_tag_assignments",
		"INSERT INTO tag_assignment_events", "INSERT INTO phases",
		"INSERT INTO project_budgets", "INSERT INTO project_budgets",
		"UPDATE opportunities", "UPDATE proposals", "UPDATE approvals",
		"UPDATE tasks", "INSERT INTO task_movement_history",
		"INSERT INTO opportunity_conversions", "INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
	for _, query := range tx.queries {
		if strings.Contains(query, "pg_advisory_xact_lock") ||
			strings.Contains(query, "SELECT name, display_id") {
			t.Fatalf("conversion without a new Client acquired the identity boundary: %s", query)
		}
	}
}

func TestConvertAtomicWithProspectClientLocksAndRechecksIdentityBeforeWrites(t *testing.T) {
	tx := &fakeSalesTx{queryResult: &fakeRows{}}
	repository := NewConversionRepository(&fakeSalesDB{tx: tx}, func() string {
		return "generated-id"
	})
	at := time.Date(2026, time.August, 5, 8, 0, 0, 0, time.UTC)
	mutation := conversionIdentityMutation(at)

	err := repository.ConvertAtomic(context.Background(), mutation)
	if err != nil {
		t.Fatalf("ConvertAtomic() error = %v", err)
	}
	if !tx.committed {
		t.Fatal("conversion transaction did not commit")
	}
	assertQueryOrder(t, tx.queries,
		"pg_advisory_xact_lock",
		"SELECT name, display_id",
		"INSERT INTO client_organizations",
		"INSERT INTO contacts",
		"INSERT INTO projects",
		"INSERT INTO project_budgets",
		"INSERT INTO project_budgets",
		"UPDATE opportunities",
		"UPDATE proposals",
		"UPDATE approvals",
		"INSERT INTO opportunity_conversions",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
	if len(tx.args[0]) != 1 || tx.args[0][0] != "msp" {
		t.Fatalf("identity lock args = %#v", tx.args[0])
	}
	if len(tx.args[1]) != 1 || tx.args[1][0] != "msp" {
		t.Fatalf("identity recheck args = %#v", tx.args[1])
	}
	if strings.Contains(tx.queries[1], "lifecycle_state") {
		t.Fatalf("identity recheck filtered a Client lifecycle: %s", tx.queries[1])
	}
	for _, fragment := range []string{"lower(", "regexp_replace", "[[:space:]]"} {
		if strings.Contains(tx.queries[1], fragment) {
			t.Fatalf("identity recheck used SQL normalization %q: %s", fragment, tx.queries[1])
		}
	}
}

func TestConvertAtomicWithProspectClientReturnsSharedNormalizedConflictBeforeAnyWrite(t *testing.T) {
	tests := []struct {
		name            string
		storedName      string
		storedDisplayID string
		proposedName    string
		proposedID      string
	}{
		{
			name:            "inactive Unicode name",
			storedName:      "\u00a0CAFÉ\u2003Managed\u00a0Services\u00a0",
			storedDisplayID: "CLIENT-OLD",
			proposedName:    " café managed services ",
			proposedID:      "CLIENT-NEW",
		},
		{
			name:            "archived display ID matched by proposed name",
			storedName:      "Archived Client",
			storedDisplayID: "\u00a0CLIENT\u2003ÉLITE\u00a0",
			proposedName:    "client élite",
			proposedID:      "CLIENT-NEW",
		},
		{
			name:            "deleted name matched by proposed display ID",
			storedName:      "\u2003ÅCME\u00a0North\u2003",
			storedDisplayID: "DELETED-100",
			proposedName:    "New Client",
			proposedID:      "åcme north",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeSalesTx{queryResult: &fakeRows{scans: []func(...any){
				func(destinations ...any) {
					*destinations[0].(*string) = tt.storedName
					*destinations[1].(*string) = tt.storedDisplayID
				},
			}}}
			repository := NewConversionRepository(&fakeSalesDB{tx: tx}, func() string {
				return "generated-id"
			})
			mutation := conversionIdentityMutation(
				time.Date(2026, time.August, 5, 8, 0, 0, 0, time.UTC),
			)
			mutation.Client.Name = tt.proposedName
			mutation.Client.DisplayID = tt.proposedID

			err := repository.ConvertAtomic(context.Background(), mutation)
			if !errors.Is(err, organizations.ErrClientIdentityConflict) {
				t.Fatalf(
					"ConvertAtomic() error = %v, want ErrClientIdentityConflict",
					err,
				)
			}
			if tx.committed || !tx.rolledBack {
				t.Fatalf(
					"conversion transaction committed=%t rolledBack=%t",
					tx.committed,
					tx.rolledBack,
				)
			}
			if len(tx.queries) != 2 ||
				!strings.Contains(tx.queries[0], "pg_advisory_xact_lock") ||
				!strings.Contains(tx.queries[1], "SELECT name, display_id") {
				t.Fatalf("conflict operations = %#v", tx.queries)
			}
			for _, query := range tx.queries {
				if strings.Contains(query, "INSERT INTO") ||
					strings.Contains(query, "UPDATE ") {
					t.Fatalf("conflicting identity reached a write: %s", query)
				}
			}
		})
	}
}

func TestFindOpportunityConversionIsTenantScoped(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: errors.New("capture")}}
	_, _, _ = NewConversionRepository(db, func() string { return "id" }).
		FindOpportunityConversion(
			context.Background(),
			scope.Target{MSPID: "msp", ClientID: "client"},
			"opportunity",
		)
	if !strings.Contains(db.query, "msp_id = $2") ||
		!strings.Contains(db.query, "client_id = NULLIF($3, '')::uuid") {
		t.Fatalf("conversion lookup is not tenant scoped: %s", db.query)
	}
}

func conversionIdentityMutation(at time.Time) projects.ConversionMutation {
	return projects.ConversionMutation{
		Client: &projects.ClientSeed{
			ID: "client", MSPID: "msp", ProspectID: "prospect",
			DisplayID: "CLIENT-NEW", Name: "New Client",
			Email: "client@example.test", Phone: "555-0100",
			CreatedAt: at, CreatedBy: "actor",
		},
		Project: projects.Project{
			ID: "project", MSPID: "msp", ClientID: "client", DisplayID: "PRJ-1",
			Name: "Migration", OriginalProposalVersionID: "proposal-version",
			LifecycleState: "planned", Version: 1, CreatedAt: at, CreatedBy: "actor",
		},
		OriginalBudget: projects.BudgetBaseline{Currency: "USD", RevenueMinor: 1000},
		CurrentBudget:  projects.BudgetBaseline{Currency: "USD", RevenueMinor: 1000},
		Opportunity: sales.Opportunity{
			ID: "opportunity", MSPID: "msp", ClientID: "client",
			StageID: "won", Version: 3, UpdatedAt: at, UpdatedBy: "actor",
		},
		Conversion: projects.ConversionRecord{
			ID: "conversion", OpportunityID: "opportunity",
			ProposalVersionID: "proposal-version", ProjectID: "project",
			MSPID: "msp", ClientID: "client", RequestKey: "request",
			PreviewHash: strings.Repeat("a", 64), ConvertedAt: at, ConvertedBy: "actor",
		},
		Audit: validAudit(at, "opportunity.converted", "project", "project"),
		Event: validEvent(at, "opportunity.converted", "project", "project"),
	}
}

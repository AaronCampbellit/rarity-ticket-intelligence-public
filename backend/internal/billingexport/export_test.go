package billingexport

import (
	"errors"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestBuildCSVRequiresApprovalWhenPolicyEnabled(t *testing.T) {
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("time_entry.export"),
	}
	entries := []Entry{
		{ID: "approved", WorkRecordID: "work-1", DurationSeconds: 3600, Billable: true, Approved: true},
		{ID: "pending", WorkRecordID: "work-2", DurationSeconds: 1800, Billable: true, Approved: false},
	}
	_, err := BuildCSV(principal, scope.Target{MSPID: "msp-id", ClientID: "client-id"}, Policy{
		RequireApproval: true,
	}, entries)
	if !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("BuildCSV() error = %v, want ErrApprovalRequired", err)
	}
}

func TestBuildCSVExportsCleanApprovedBillingDataWithoutInvoiceFields(t *testing.T) {
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("time_entry.export"),
	}
	output, err := BuildCSV(principal, scope.Target{MSPID: "msp-id", ClientID: "client-id"}, Policy{
		RequireApproval: true,
	}, []Entry{{
		ID: "entry-1", WorkRecordID: "work-1", TechnicianID: "tech-1",
		DurationSeconds: 3600, Billable: true, Approved: true,
	}})
	if err != nil {
		t.Fatalf("BuildCSV() error = %v", err)
	}
	csv := string(output)
	for _, required := range []string{"time_entry_id", "duration_seconds", "entry-1"} {
		if !strings.Contains(csv, required) {
			t.Fatalf("CSV missing %q: %s", required, csv)
		}
	}
	for _, forbidden := range []string{"invoice", "payment", "accounting"} {
		if strings.Contains(strings.ToLower(csv), forbidden) {
			t.Fatalf("CSV contains deferred field %q: %s", forbidden, csv)
		}
	}
}

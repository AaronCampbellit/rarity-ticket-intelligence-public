package datto

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type dattoManagementRepository struct {
	request    ManualSyncRequest
	mapping    SiteMapping
	candidates []ReconciliationCandidate
	candidate  ReconciliationCandidate
	decision   CandidateDecision
}

func (r *dattoManagementRepository) MapSite(
	_ context.Context,
	mapping SiteMapping,
) error {
	r.mapping = mapping
	return nil
}

func (r *dattoManagementRepository) QueueManualSync(
	_ context.Context,
	request ManualSyncRequest,
) error {
	r.request = request
	return nil
}
func (r *dattoManagementRepository) Progress(
	context.Context,
	scope.Target,
	string,
) (SyncProgress, error) {
	return SyncProgress{ConnectionID: "connection-id", State: "running"}, nil
}
func (r *dattoManagementRepository) FindCandidate(
	context.Context,
	string,
	string,
) (ReconciliationCandidate, error) {
	return r.candidate, nil
}
func (r *dattoManagementRepository) ListCandidates(
	context.Context,
	scope.Target,
	int,
) ([]ReconciliationCandidate, error) {
	return r.candidates, nil
}
func (r *dattoManagementRepository) DecideCandidate(
	_ context.Context,
	decision CandidateDecision,
) error {
	r.decision = decision
	return nil
}

func dattoPrincipal(capabilities ...string) authorization.Principal {
	return authorization.Principal{
		ID: "technician-id",
		Scope: scope.Principal{
			MSPID: "msp-id", ClientID: "client-id",
		},
		Capabilities: authorization.NewCapabilitySet(capabilities...),
	}
}

func dattoMSPPrincipal(capabilities ...string) authorization.Principal {
	principal := dattoPrincipal(capabilities...)
	principal.Scope.ClientID = ""
	return principal
}

func TestManagementServiceQueuesAuthorizedManualSync(t *testing.T) {
	now := time.Date(2026, time.July, 29, 23, 30, 0, 0, time.UTC)
	repository := &dattoManagementRepository{}
	service := NewManagementService(
		repository, func() time.Time { return now },
		func() string { return "request-id" },
	)
	request, err := service.QueueManualSync(
		context.Background(),
		ManualSyncCommand{
			Principal:    dattoMSPPrincipal("integration.manage"),
			Target:       scope.Target{MSPID: "msp-id"},
			ConnectionID: "connection-id",
		},
	)
	if err != nil {
		t.Fatalf("QueueManualSync() error = %v", err)
	}
	if request.ID != "request-id" ||
		repository.request.RequestedBy != "technician-id" ||
		repository.request.RequestedAt != now {
		t.Fatalf("request=%+v repository=%+v", request, repository.request)
	}
}

func TestManagementServiceMapsDattoSiteToAuthorizedClient(t *testing.T) {
	now := time.Date(2026, time.July, 29, 23, 30, 0, 0, time.UTC)
	repository := &dattoManagementRepository{}
	service := NewManagementService(
		repository, func() time.Time { return now },
		func() string { return "mapping-id" },
	)

	mapping, err := service.MapSite(
		context.Background(),
		MapSiteCommand{
			Principal:    dattoMSPPrincipal("integration.manage"),
			ConnectionID: "connection-id", SiteID: "site-id",
			ClientID: "client-id", Reason: "verified Datto site",
		},
	)

	if err != nil || mapping.ID != "mapping-id" ||
		mapping.MSPID != "msp-id" ||
		repository.mapping.ClientID != "client-id" ||
		repository.mapping.MappedBy != "technician-id" ||
		repository.mapping.MappedAt != now {
		t.Fatalf(
			"MapSite() mapping=%+v repository=%+v error=%v",
			mapping, repository.mapping, err,
		)
	}
}

func TestManagementServiceRequiresClientAuthorizedReconciliationDecision(t *testing.T) {
	now := time.Date(2026, time.July, 29, 23, 30, 0, 0, time.UTC)
	repository := &dattoManagementRepository{
		candidate: ReconciliationCandidate{
			ID: "candidate-id", MSPID: "msp-id", ClientID: "client-id",
			SnapshotID: "snapshot-id", AssetID: "asset-id",
			State: "pending",
		},
	}
	service := NewManagementService(
		repository, func() time.Time { return now },
		func() string { return "decision-id" },
	)
	decision, err := service.Decide(
		context.Background(),
		DecisionCommand{
			Principal:   dattoPrincipal("integration.datto.reconcile"),
			CandidateID: "candidate-id",
			Decision:    DecisionChooseDatto,
			Reason:      "verified inventory authority",
		},
	)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.ID != "decision-id" ||
		decision.CandidateID != "candidate-id" ||
		repository.decision.DecidedBy != "technician-id" {
		t.Fatalf("decision=%+v repository=%+v", decision, repository.decision)
	}
}

func TestManagementServiceListsAuthorizedPendingCandidates(t *testing.T) {
	repository := &dattoManagementRepository{
		candidates: []ReconciliationCandidate{{
			ID: "candidate-id", MSPID: "msp-id", ClientID: "client-id",
			State: "pending",
		}},
	}
	service := NewManagementService(
		repository, time.Now, func() string { return "id" },
	)

	result, err := service.ListCandidates(
		context.Background(),
		ListCandidatesCommand{
			Principal: dattoPrincipal("integration.datto.reconcile"),
			Target: scope.Target{
				MSPID: "msp-id", ClientID: "client-id",
			},
			Limit: 25,
		},
	)

	if err != nil || len(result) != 1 || result[0].ID != "candidate-id" {
		t.Fatalf("ListCandidates() result=%+v error=%v", result, err)
	}
}

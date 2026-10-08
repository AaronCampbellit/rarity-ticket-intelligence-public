package calendar

import (
	"context"
	"testing"
	"time"
)

type reconcileCatalogStub struct{ sources []ReconcileSource }

func (s reconcileCatalogStub) ScanProjectionSources(context.Context, ReconcileRequest) ([]ReconcileSource, error) {
	return s.sources, nil
}
func TestReconciliationReportsAndRepairsMissingStaleAndOrphaned(t *testing.T) {
	date := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	registry := NewAdapterRegistry()
	task := &projectingAdapterStub{sourceType: "task", revisions: map[string]int64{"missing": 3, "stale": 4}, projected: []Projection{{ID: "p", Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "missing"}, EventRole: "due", SourceRevision: 3, Title: "Due", AllDay: true, StartsOn: &date, SchedulingMode: Informational, TerminalState: Active}}}
	_ = registry.Register(task)
	repo := &projectionRepositoryStub{}
	roles, _ := NewProductionRoleRegistry(nil)
	service := NewProjectionService(repo, roles)
	catalog := reconcileCatalogStub{sources: []ReconcileSource{{Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "missing"}, SourceRevision: 3, SourceExists: true}, {Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "stale"}, SourceRevision: 4, AppliedRevision: 2, ProjectionRoles: []string{"due"}, SourceExists: true}, {Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "orphan"}, AppliedRevision: 5, ProjectionRoles: []string{"due"}, SourceExists: false}}}
	reconciler := NewReconciliationService(catalog, registry, service)
	report, err := reconciler.Scan(context.Background(), ReconcileRequest{MSPID: "msp", SourceTypes: []string{"task"}, Limit: 10, Repair: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Scanned != 3 || report.Missing != 1 || report.Stale != 1 || report.Orphaned != 1 || report.Repaired != 3 {
		t.Fatalf("report=%+v", report)
	}
	if len(repo.applied) != 3 {
		t.Fatalf("repairs=%d", len(repo.applied))
	}
	if len(repo.applied[2].Projections) != 0 || repo.applied[2].SourceRevision != 6 {
		t.Fatalf("orphan repair=%+v", repo.applied[2])
	}
}

func TestReconciliationDoesNotRepairAuthoritativelyAppliedZeroRoleSource(t *testing.T) {
	registry := NewAdapterRegistry()
	task := &projectingAdapterStub{sourceType: "task", projected: nil}
	_ = registry.Register(task)
	repo := &projectionRepositoryStub{}
	roles, _ := NewProductionRoleRegistry(nil)
	service := NewProjectionService(repo, roles)
	catalog := reconcileCatalogStub{sources: []ReconcileSource{{Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "task"}, SourceRevision: 4, AppliedRevision: 4, ProjectionRoles: []string{}, SourceExists: true}}}
	report, err := NewReconciliationService(catalog, registry, service).Scan(context.Background(), ReconcileRequest{MSPID: "msp", SourceTypes: []string{"task"}, Limit: 10, Repair: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Missing != 0 || report.Stale != 0 || report.Repaired != 0 || len(repo.applied) != 0 {
		t.Fatalf("report=%+v applies=%d", report, len(repo.applied))
	}
}

func TestReconciliationDoesNotRepeatedlyRepairTombstonedOrphan(t *testing.T) {
	registry := NewAdapterRegistry()
	repo := &projectionRepositoryStub{}
	roles, _ := NewProductionRoleRegistry(nil)
	catalog := reconcileCatalogStub{sources: []ReconcileSource{{Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "work_record", ID: "deleted"}, AppliedRevision: 9, ProjectionRoles: []string{}, SourceExists: false}}}
	report, err := NewReconciliationService(catalog, registry, NewProjectionService(repo, roles)).Scan(context.Background(), ReconcileRequest{MSPID: "msp", Limit: 10, Repair: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Scanned != 1 || report.Orphaned != 0 || report.Repaired != 0 || len(repo.applied) != 0 {
		t.Fatalf("report=%+v applies=%d", report, len(repo.applied))
	}
}

func TestReconciliationCountsRepairOnlyWhenRepositoryApplies(t *testing.T) {
	date := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	registry := NewAdapterRegistry()
	task := &projectingAdapterStub{sourceType: "task", projected: []Projection{{ID: "p", EventRole: "due", SourceRevision: 3, Title: "Due", AllDay: true, StartsOn: &date, SchedulingMode: Informational, TerminalState: Active}}}
	_ = registry.Register(task)
	repo := &projectionRepositoryStub{revision: 7}
	roles, _ := NewProductionRoleRegistry(nil)
	service := NewProjectionService(repo, roles)
	catalog := reconcileCatalogStub{sources: []ReconcileSource{{Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "task"}, SourceRevision: 3, AppliedRevision: 0, SourceExists: true}}}
	report, err := NewReconciliationService(catalog, registry, service).Scan(context.Background(), ReconcileRequest{MSPID: "msp", Limit: 10, Repair: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Missing != 1 || report.Repaired != 0 {
		t.Fatalf("report=%+v", report)
	}
}

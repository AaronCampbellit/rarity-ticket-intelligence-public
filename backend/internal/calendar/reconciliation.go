package calendar

import (
	"context"
	"sort"
	"strings"
)

type ReconcileRequest struct {
	MSPID       string
	SourceTypes []string
	Limit       int
	Repair      bool
}
type ReconcileReport struct{ Scanned, Missing, Stale, Orphaned, Repaired int }
type ReconcileSource struct {
	Source                          SourceRef
	SourceRevision, AppliedRevision int64
	ProjectionRoles                 []string
	SourceExists                    bool
}
type ReconciliationCatalog interface {
	ScanProjectionSources(context.Context, ReconcileRequest) ([]ReconcileSource, error)
}
type ReconciliationService struct {
	catalog     ReconciliationCatalog
	adapters    *AdapterRegistry
	projections *ProjectionService
}

func NewReconciliationService(c ReconciliationCatalog, a *AdapterRegistry, p *ProjectionService) *ReconciliationService {
	return &ReconciliationService{catalog: c, adapters: a, projections: p}
}
func (s *ReconciliationService) Scan(ctx context.Context, request ReconcileRequest) (ReconcileReport, error) {
	report := ReconcileReport{}
	if s == nil || s.catalog == nil || s.adapters == nil || s.projections == nil || strings.TrimSpace(request.MSPID) == "" || request.Limit < 1 {
		return report, ErrInvalidProjectionBatch
	}
	values, err := s.catalog.ScanProjectionSources(ctx, request)
	if err != nil {
		return report, err
	}
	if len(values) > request.Limit {
		values = values[:request.Limit]
	}
	for _, value := range values {
		report.Scanned++
		repair := false
		var batch ProjectionBatch
		if !value.SourceExists {
			if len(value.ProjectionRoles) == 0 {
				continue
			}
			report.Orphaned++
			repair = true
			batch = ProjectionBatch{Source: value.Source, SourceRevision: value.AppliedRevision + 1, TrustedSourceReload: true}
		} else {
			adapter, ok := s.adapters.ForSource(value.Source.Type)
			if !ok {
				return report, ErrInvalidAdapter
			}
			projected, projectErr := adapter.Project(ctx, value.Source)
			if projectErr != nil {
				return report, projectErr
			}
			revision := value.SourceRevision
			if len(projected) > 0 {
				revision = projected[0].SourceRevision
			}
			batch = ProjectionBatch{Source: value.Source, SourceRevision: revision, Projections: projected, TrustedSourceReload: true}
			if value.AppliedRevision == 0 {
				report.Missing++
				repair = true
			} else if value.AppliedRevision < revision || !sameRoles(projected, value.ProjectionRoles) {
				report.Stale++
				repair = true
			}
		}
		if repair && request.Repair {
			var applied bool
			if applied, err = s.projections.Apply(ctx, batch); err != nil {
				return report, err
			}
			if applied {
				report.Repaired++
			}
		}
	}
	return report, nil
}
func sameRoles(projections []Projection, roles []string) bool {
	a := make([]string, len(projections))
	for i, p := range projections {
		a[i] = p.EventRole + ":" + p.SourceRoleKey
	}
	b := append([]string(nil), roles...)
	sort.Strings(a)
	sort.Strings(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

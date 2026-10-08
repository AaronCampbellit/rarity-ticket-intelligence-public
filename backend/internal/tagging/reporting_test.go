package tagging

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestValidateReportFilterBoundsDatesTagsAndPagination(t *testing.T) {
	valid := ReportFilter{ClientID: "client", From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), TagIDs: []string{"tag"}, Limit: 50}
	if err := ValidateReportFilter(valid); err != nil {
		t.Fatalf("valid filter: %v", err)
	}
	invalid := valid
	invalid.TagIDs = make([]string, 51)
	if err := ValidateReportFilter(invalid); !errors.Is(err, ErrInvalidReportFilter) {
		t.Fatalf("tag bound error=%v", err)
	}
	invalid = valid
	invalid.To = invalid.From.AddDate(1, 0, 1)
	if err := ValidateReportFilter(invalid); !errors.Is(err, ErrInvalidReportFilter) {
		t.Fatalf("date bound error=%v", err)
	}
}

type reportRepositoryStub struct {
	mspID  string
	filter ReportFilter
	calls  int
}

func (s *reportRepositoryStub) ClassificationReport(_ context.Context, mspID string, _ ReportKind, filter ReportFilter) (Report, error) {
	s.calls++
	s.mspID, s.filter = mspID, filter
	return Report{Kind: ReportUsage, Rows: []ReportRow{}}, nil
}
func (s *reportRepositoryStub) ClassificationEvidence(context.Context, string, string, ReportFilter) (EvidencePage, error) {
	return EvidencePage{Items: []EvidenceRef{}}, nil
}
func (s *reportRepositoryStub) ClassificationTechnicians(context.Context, string, string) ([]TechnicianOption, error) {
	return []TechnicianOption{}, nil
}

func TestReportServiceDerivesClientScopeAndRequiresReportCapability(t *testing.T) {
	repository := &reportRepositoryStub{}
	service := NewReportService(repository)
	filter := ReportFilter{ClientID: "forged", From: time.Now().Add(-time.Hour), To: time.Now(), Limit: 50}
	principal := authorization.Principal{ID: "actor", Scope: scope.Principal{MSPID: "msp", ClientID: "authorized"}}
	if _, err := service.Query(context.Background(), principal, ReportUsage, filter); !errors.Is(err, authorization.ErrForbidden) || repository.calls != 0 {
		t.Fatalf("without capability error=%v calls=%d", err, repository.calls)
	}
	principal.Capabilities = authorization.NewCapabilitySet("classification.report")
	if _, err := service.Query(context.Background(), principal, ReportUsage, filter); err != nil {
		t.Fatalf("authorized query: %v", err)
	}
	if repository.calls != 1 || repository.mspID != "msp" || repository.filter.ClientID != "authorized" {
		t.Fatalf("repository calls=%d msp=%q filter=%+v", repository.calls, repository.mspID, repository.filter)
	}
}

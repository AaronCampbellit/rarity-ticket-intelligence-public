package tagging

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrInvalidReportFilter    = errors.New("invalid classification report filter")
	ErrReportCapacityExceeded = errors.New("classification report snapshot capacity exceeded")
)

type ReportKind string

const (
	ReportUsage        ReportKind = "usage"
	ReportCombinations ReportKind = "combinations"
	ReportTrends       ReportKind = "trends"
	ReportHealth       ReportKind = "classification-health"
	ReportRecurring    ReportKind = "recurring-issues"
)

type ReportFilter struct {
	ClientID     string     `json:"client_id"`
	ObjectType   ObjectType `json:"object_type,omitempty"`
	GroupID      string     `json:"group_id,omitempty"`
	TagIDs       []string   `json:"tag_ids,omitempty"`
	Match        string     `json:"match,omitempty"`
	Source       Source     `json:"source,omitempty"`
	Inheritance  string     `json:"inheritance,omitempty"`
	TechnicianID string     `json:"technician_id,omitempty"`
	TeamID       string     `json:"team_id,omitempty"`
	Priority     string     `json:"priority,omitempty"`
	Status       string     `json:"status,omitempty"`
	From         time.Time  `json:"from"`
	To           time.Time  `json:"to"`
	Cursor       string     `json:"cursor,omitempty"`
	Limit        int        `json:"limit"`
}

func ValidateReportFilter(filter ReportFilter) error {
	if strings.TrimSpace(filter.ClientID) == "" || filter.From.IsZero() || filter.To.IsZero() || filter.To.Before(filter.From) || filter.To.Sub(filter.From) >= 366*24*time.Hour || len(filter.TagIDs) > 50 || filter.Limit < 1 || filter.Limit > 200 {
		return ErrInvalidReportFilter
	}
	if filter.ObjectType != "" && !validObjectType(filter.ObjectType) || filter.Source != "" && !validSource(filter.Source) {
		return ErrInvalidReportFilter
	}
	if filter.Match != "" && filter.Match != "any" && filter.Match != "all" && filter.Match != "none" {
		return ErrInvalidReportFilter
	}
	if filter.Inheritance != "" && filter.Inheritance != "all" && filter.Inheritance != "direct" && filter.Inheritance != "inherited" {
		return ErrInvalidReportFilter
	}
	return nil
}

type ReportRow struct {
	TagID          string     `json:"tag_id,omitempty"`
	LeftTagID      string     `json:"left_tag_id,omitempty"`
	RightTagID     string     `json:"right_tag_id,omitempty"`
	ObjectType     ObjectType `json:"object_type,omitempty"`
	Date           string     `json:"date,omitempty"`
	Count          int64      `json:"count"`
	AddedCount     int64      `json:"added_count,omitempty"`
	RemovedCount   int64      `json:"removed_count,omitempty"`
	ActiveCount    int64      `json:"active_count,omitempty"`
	AgeSeconds     int64      `json:"age_seconds,omitempty"`
	Source         string     `json:"source,omitempty"`
	ObjectIDs      []string   `json:"object_ids,omitempty"`
	EvidenceCursor string     `json:"evidence_cursor,omitempty"`
}
type Report struct {
	Kind           ReportKind  `json:"kind"`
	Rows           []ReportRow `json:"rows"`
	NextCursor     string      `json:"next_cursor,omitempty"`
	ProjectionAsOf time.Time   `json:"projection_as_of"`
}
type EvidenceRef struct {
	ObjectType     ObjectType `json:"object_type"`
	ObjectID       string     `json:"object_id"`
	Label          string     `json:"label"`
	ClientID       string     `json:"client_id,omitempty"`
	ParentObjectID string     `json:"parent_object_id,omitempty"`
}
type EvidencePage struct {
	Items          []EvidenceRef `json:"items"`
	NextCursor     string        `json:"next_cursor,omitempty"`
	ProjectionAsOf time.Time     `json:"projection_as_of"`
}
type TechnicianOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type ReportRepository interface {
	ClassificationReport(context.Context, string, ReportKind, ReportFilter) (Report, error)
	ClassificationEvidence(context.Context, string, string, ReportFilter) (EvidencePage, error)
	ClassificationTechnicians(context.Context, string, string) ([]TechnicianOption, error)
}

func (s *ReportService) Technicians(ctx context.Context, principal authorization.Principal) ([]TechnicianOption, error) {
	if s == nil || s.repository == nil {
		return nil, ErrInvalidReportFilter
	}
	if err := authorization.Authorize(principal, "classification.report", scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID}); err != nil {
		return nil, err
	}
	return s.repository.ClassificationTechnicians(ctx, principal.Scope.MSPID, principal.Scope.ClientID)
}

func (s *ReportService) Evidence(ctx context.Context, principal authorization.Principal, tagID string, filter ReportFilter) (EvidencePage, error) {
	filter.ClientID = principal.Scope.ClientID
	if s == nil || s.repository == nil || strings.TrimSpace(tagID) == "" || filter.ObjectType == "" || ValidateReportFilter(filter) != nil {
		return EvidencePage{}, ErrInvalidReportFilter
	}
	if err := authorization.Authorize(principal, "classification.report", scope.Target{MSPID: principal.Scope.MSPID, ClientID: filter.ClientID}); err != nil {
		return EvidencePage{}, err
	}
	return s.repository.ClassificationEvidence(ctx, principal.Scope.MSPID, tagID, filter)
}

type ReportService struct{ repository ReportRepository }

func NewReportService(repository ReportRepository) *ReportService {
	return &ReportService{repository: repository}
}
func (s *ReportService) Query(ctx context.Context, principal authorization.Principal, kind ReportKind, filter ReportFilter) (Report, error) {
	filter.ClientID = principal.Scope.ClientID
	if s == nil || s.repository == nil || !validReportKind(kind) || ValidateReportFilter(filter) != nil {
		return Report{}, ErrInvalidReportFilter
	}
	if err := authorization.Authorize(principal, "classification.report", scope.Target{MSPID: principal.Scope.MSPID, ClientID: filter.ClientID}); err != nil {
		return Report{}, err
	}
	return s.repository.ClassificationReport(ctx, principal.Scope.MSPID, kind, filter)
}
func validReportKind(kind ReportKind) bool {
	return kind == ReportUsage || kind == ReportCombinations || kind == ReportTrends || kind == ReportHealth || kind == ReportRecurring
}

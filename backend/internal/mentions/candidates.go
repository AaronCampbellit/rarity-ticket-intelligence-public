package mentions

import (
	"context"
	"errors"
	"sort"
	"strings"
)

const maximumCandidates = 50

var (
	ErrInvalidCandidateQuery = errors.New("invalid mention candidate query")
	ErrInvalidAccessData     = errors.New("invalid mention access data")
)

// SourceRef identifies an internal source and the parent whose effective
// visibility applies. Repositories resolve task visibility through its current
// work-record or project/phase parent; team membership is never an access grant.
type SourceRef struct {
	MSPID, ClientID string
	ParentType      ParentType
	ParentID        string
	SourceKind      SourceKind
}

// CandidateQuery is always scoped to an author and an exact internal source.
// AuthorizationOnly is reserved for Resolver: repositories must verify an
// active internal author with mention.create and source-edit authorization,
// return no candidate rows, and fail before loading target access.
type CandidateQuery struct {
	AuthorID          string
	Source            SourceRef
	Search            string
	ExactTargetType   TargetType
	ExactTargetID     string
	Limit             int
	AuthorizationOnly bool
}

// Candidate is safe picker data. The fields excluded from JSON let the service
// defensively enforce repository filtering without exposing authorization
// inputs to clients.
type Candidate struct {
	TargetType        TargetType `json:"target_type"`
	ID                string     `json:"id"`
	Label             string     `json:"label"`
	EligibleCount     int        `json:"eligible_count,omitempty"`
	ExcludedCount     int        `json:"excluded_count,omitempty"`
	EligibleMemberIDs []string   `json:"eligible_member_ids,omitempty"`
	Version           int64      `json:"version"`

	MSPID          string `json:"-"`
	Active         bool   `json:"-"`
	Internal       bool   `json:"-"`
	HasMentionRead bool   `json:"-"`
	CanRead        bool   `json:"-"`
}

// AccessRepository must compute all access from current persisted role,
// lifecycle, team-membership, parent, project, and source visibility state.
// Its methods are intended to be implemented by the source write transaction.
// LoadTeamAccess returns each requested team's exact current version and every
// current member, including members whose authorization makes them ineligible.
type AccessRepository interface {
	ListCandidates(context.Context, CandidateQuery) ([]Candidate, error)
	LoadDirectAccess(context.Context, SourceRef, []string) ([]MemberAccess, error)
	LoadTeamAccess(context.Context, SourceRef, []string) ([]TeamAccess, error)
}

type CandidateService struct {
	repository AccessRepository
}

func NewCandidateService(repository AccessRepository) *CandidateService {
	return &CandidateService{repository: repository}
}

// List returns only permission-safe matches. Repositories enforce author
// authorization and should apply Query.Limit; the service independently
// filters, orders, deduplicates, and caps results as a fail-safe.
func (s *CandidateService) List(ctx context.Context, query CandidateQuery) ([]Candidate, error) {
	if s == nil || s.repository == nil || !validStableID(query.AuthorID) ||
		!validSourceRef(query.Source) || query.AuthorizationOnly {
		return nil, ErrInvalidCandidateQuery
	}
	query.Search = strings.TrimSpace(query.Search)
	query.ExactTargetID = strings.TrimSpace(query.ExactTargetID)
	exactTypeSet := query.ExactTargetType != ""
	exactIDSet := query.ExactTargetID != ""
	if exactTypeSet != exactIDSet ||
		(exactTypeSet && (query.ExactTargetType != TargetTeam ||
			!validStableID(query.ExactTargetID) || query.Search != "")) {
		return nil, ErrInvalidCandidateQuery
	}
	query.Limit = maximumCandidates
	rows, err := s.repository.ListCandidates(ctx, query)
	if err != nil {
		return nil, err
	}

	search := strings.ToLower(query.Search)
	filtered := make([]Candidate, 0, min(len(rows), maximumCandidates))
	repositoryRows := make(map[string]Candidate, len(rows))
	for _, row := range rows {
		if exactTypeSet &&
			(row.TargetType != query.ExactTargetType || row.ID != query.ExactTargetID) {
			continue
		}
		if row.MSPID != query.Source.MSPID {
			continue
		}
		row, ok := normalizeCandidateSnapshot(row)
		if !ok {
			return nil, ErrInvalidAccessData
		}
		if !validCandidate(row) {
			return nil, ErrInvalidAccessData
		}
		key := string(row.TargetType) + "\x00" + row.ID
		if previous, exists := repositoryRows[key]; exists {
			if !sameCandidate(previous, row) {
				return nil, ErrInvalidAccessData
			}
			continue
		}
		repositoryRows[key] = row
		if row.TargetType == TargetStaff {
			if row.ID == query.AuthorID || !row.Active || !row.Internal ||
				!row.HasMentionRead || !row.CanRead {
				continue
			}
		} else if row.EligibleCount == 0 {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(row.Label), search) {
			continue
		}
		filtered = append(filtered, row)
	}

	sort.Slice(filtered, func(left, right int) bool {
		leftLabel := strings.ToLower(filtered[left].Label)
		rightLabel := strings.ToLower(filtered[right].Label)
		leftPrefix := search != "" && strings.HasPrefix(leftLabel, search)
		rightPrefix := search != "" && strings.HasPrefix(rightLabel, search)
		if leftPrefix != rightPrefix {
			return leftPrefix
		}
		if leftLabel != rightLabel {
			return leftLabel < rightLabel
		}
		if filtered[left].TargetType != filtered[right].TargetType {
			return filtered[left].TargetType < filtered[right].TargetType
		}
		return filtered[left].ID < filtered[right].ID
	})

	result := make([]Candidate, 0, min(len(filtered), maximumCandidates))
	for _, row := range filtered {
		result = append(result, row)
		if len(result) == maximumCandidates {
			break
		}
	}
	return result, nil
}

func validCandidate(candidate Candidate) bool {
	if (candidate.TargetType != TargetStaff && candidate.TargetType != TargetTeam) ||
		!validStableID(candidate.ID) || !validStableID(candidate.MSPID) ||
		!validLabel(candidate.Label) || candidate.Version < 1 ||
		candidate.EligibleCount < 0 || candidate.ExcludedCount < 0 {
		return false
	}
	return true
}

func normalizeCandidateSnapshot(candidate Candidate) (Candidate, bool) {
	if candidate.TargetType == TargetStaff {
		if candidate.EligibleCount != 0 || candidate.ExcludedCount != 0 || len(candidate.EligibleMemberIDs) != 0 {
			return Candidate{}, false
		}
		candidate.EligibleMemberIDs = nil
		return candidate, true
	}
	ids := append([]string(nil), candidate.EligibleMemberIDs...)
	for _, id := range ids {
		if !validStableID(id) {
			return Candidate{}, false
		}
	}
	sort.Strings(ids)
	result := ids[:0]
	for _, id := range ids {
		if len(result) == 0 || result[len(result)-1] != id {
			result = append(result, id)
		}
	}
	if candidate.EligibleCount != len(result) {
		return Candidate{}, false
	}
	candidate.EligibleMemberIDs = result
	return candidate, true
}

func sameCandidate(left, right Candidate) bool {
	if left.TargetType != right.TargetType || left.ID != right.ID || left.Label != right.Label ||
		left.EligibleCount != right.EligibleCount || left.ExcludedCount != right.ExcludedCount ||
		left.Version != right.Version || left.MSPID != right.MSPID || left.Active != right.Active ||
		left.Internal != right.Internal || left.HasMentionRead != right.HasMentionRead || left.CanRead != right.CanRead ||
		len(left.EligibleMemberIDs) != len(right.EligibleMemberIDs) {
		return false
	}
	for index := range left.EligibleMemberIDs {
		if left.EligibleMemberIDs[index] != right.EligibleMemberIDs[index] {
			return false
		}
	}
	return true
}

func validSourceRef(source SourceRef) bool {
	return validStableID(source.MSPID) && validStableID(source.ClientID) &&
		validStableID(source.ParentID) && validParentType(source.ParentType) &&
		validSourceKind(source.SourceKind)
}

func validParentType(parentType ParentType) bool {
	return parentType == ParentWorkRecord || parentType == ParentTask || parentType == ParentProject
}

func validSourceKind(sourceKind SourceKind) bool {
	return sourceKind == SourceDetails || sourceKind == SourceComment || sourceKind == SourceNote
}

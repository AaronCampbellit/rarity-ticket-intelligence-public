package calendar

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
)

var ErrInvalidConflictInput = errors.New("invalid calendar conflict input")

type ConflictSeverity string

const (
	ConflictInfo         ConflictSeverity = "informational"
	ConflictWarning      ConflictSeverity = "warning"
	ConflictOverrideable ConflictSeverity = "overrideable_block"
	ConflictHard         ConflictSeverity = "hard_block"
)

type ConflictKind string

const (
	ConflictApprovedPTO          ConflictKind = "approved_pto"
	ConflictNonWorkingTime       ConflictKind = "non_working_time"
	ConflictProtectedMaintenance ConflictKind = "protected_maintenance"
	ConflictOrdinaryOverbooking  ConflictKind = "ordinary_overbooking"
)

var ConflictKindList = []ConflictKind{
	ConflictApprovedPTO, ConflictNonWorkingTime,
	ConflictProtectedMaintenance, ConflictOrdinaryOverbooking,
}

type SafeSourceRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type ConflictPolicy struct {
	ID           string                  `json:"id"`
	MSPID        string                  `json:"-"`
	TeamID       string                  `json:"team_id,omitempty"`
	TechnicianID string                  `json:"technician_id,omitempty"`
	ScopeType    ConflictPolicyScopeType `json:"scope_type"`
	Kind         ConflictKind            `json:"kind"`
	Severity     ConflictSeverity        `json:"severity"`
	Version      int64                   `json:"version"`
}

type ProposedSchedule struct {
	Interval     TimeInterval
	Source       SafeSourceRef
	ProjectionID string
	ClientID     string
	ServiceID    string
	AssetID      string
	TeamID       string
	TechnicianID string
}

type ConflictConstraint struct {
	Kind     ConflictKind
	Interval TimeInterval
	Related  SafeSourceRef
	Policy   *ConflictPolicyEvidence
}

type ConflictPolicyEvidence struct {
	ID       string
	Version  int64
	Severity ConflictSeverity
}

type ConflictInput struct {
	Policies     []ConflictPolicy
	Proposed     ProposedSchedule
	Constraints  []ConflictConstraint
	Dependencies []DependencyConstraint
}

type Conflict struct {
	Severity       ConflictSeverity `json:"severity"`
	ReasonRequired bool             `json:"reason_required"`
	ReasonCode     string           `json:"reason_code"`
	PolicyID       string           `json:"policy_id"`
	PolicyVersion  int64            `json:"policy_version"`
	Interval       TimeInterval     `json:"interval"`
	Related        SafeSourceRef    `json:"related"`
}

type ConflictRepository interface {
	LoadConflictInput(context.Context, authorization.Principal, ProposedSchedule) (ConflictInput, error)
}

type ConflictService struct{ repository ConflictRepository }

func NewConflictService(repository ConflictRepository) *ConflictService {
	return &ConflictService{repository: repository}
}

func (s *ConflictService) Evaluate(ctx context.Context, principal authorization.Principal, proposed ProposedSchedule) ([]Conflict, error) {
	if s == nil || s.repository == nil || strings.TrimSpace(principal.Scope.MSPID) == "" || !proposed.Interval.valid() {
		return nil, ErrInvalidConflictInput
	}
	input, err := s.repository.LoadConflictInput(ctx, principal, proposed)
	if err != nil {
		return nil, err
	}
	input.Proposed.Interval = proposed.Interval
	return EvaluateConflicts(input), nil
}

func EvaluateConflicts(input ConflictInput) []Conflict {
	if !input.Proposed.Interval.valid() {
		return nil
	}
	byKind := make(map[ConflictKind]ConflictPolicy, len(input.Policies))
	for _, policy := range input.Policies {
		if !knownConflictKind(policy.Kind) || !validConflictSeverity(policy.Severity) || policy.ID == "" || policy.Version < 1 {
			continue
		}
		if current, exists := byKind[policy.Kind]; !exists || policy.Version > current.Version {
			byKind[policy.Kind] = policy
		}
	}
	result := make([]Conflict, 0, len(input.Constraints)+len(input.Dependencies))
	for _, constraint := range input.Constraints {
		if !knownConflictKind(constraint.Kind) {
			continue
		}
		affected := intersectInterval(input.Proposed.Interval, constraint.Interval)
		if !affected.valid() {
			continue
		}
		policy, configured := byKind[constraint.Kind]
		if constraint.Policy != nil && strings.TrimSpace(constraint.Policy.ID) != "" && constraint.Policy.Version > 0 && validConflictSeverity(constraint.Policy.Severity) {
			specific := ConflictPolicy{ID: strings.TrimSpace(constraint.Policy.ID), Kind: constraint.Kind, Severity: constraint.Policy.Severity, Version: constraint.Policy.Version}
			if !configured || conflictSeverityStrength(specific.Severity) >= conflictSeverityStrength(policy.Severity) {
				policy = specific
			}
			configured = true
		}
		if !configured {
			policy = defaultConflictPolicy(constraint.Kind)
		}
		result = append(result, Conflict{
			Severity: policy.Severity, ReasonRequired: policy.Severity == ConflictOverrideable,
			ReasonCode: string(constraint.Kind), PolicyID: policy.ID, PolicyVersion: policy.Version,
			Interval: affected, Related: safeSourceReference(constraint.Related),
		})
	}
	for _, dependency := range input.Dependencies {
		if !dependency.Unmet {
			continue
		}
		affected := intersectInterval(input.Proposed.Interval, dependency.Interval)
		if !affected.valid() {
			continue
		}
		result = append(result, Conflict{
			Severity: ConflictHard, ReasonCode: "dependency_constraint",
			PolicyID: "default:dependency_constraint", PolicyVersion: 1,
			Interval: affected, Related: safeSourceReference(dependency.Related),
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Interval.Start.Equal(result[j].Interval.Start) {
			return result[i].ReasonCode < result[j].ReasonCode
		}
		return result[i].Interval.Start.Before(result[j].Interval.Start)
	})
	return result
}

func defaultConflictPolicy(kind ConflictKind) ConflictPolicy {
	severity := ConflictHard
	if kind == ConflictOrdinaryOverbooking {
		severity = ConflictOverrideable
	}
	return ConflictPolicy{ID: "default:" + string(kind), Kind: kind, Severity: severity, Version: 1, ScopeType: ConflictScopeMSP}
}

func safeSourceReference(input SafeSourceRef) SafeSourceRef {
	return SafeSourceRef{Type: strings.TrimSpace(input.Type), ID: strings.TrimSpace(input.ID)}
}
func knownConflictKind(kind ConflictKind) bool {
	for _, known := range ConflictKindList {
		if kind == known {
			return true
		}
	}
	return false
}
func validConflictSeverity(severity ConflictSeverity) bool {
	return severity == ConflictInfo || severity == ConflictWarning || severity == ConflictOverrideable || severity == ConflictHard
}

func conflictSeverityStrength(severity ConflictSeverity) int {
	switch severity {
	case ConflictInfo:
		return 1
	case ConflictWarning:
		return 2
	case ConflictOverrideable:
		return 3
	case ConflictHard:
		return 4
	default:
		return 0
	}
}

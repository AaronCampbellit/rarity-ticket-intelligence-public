package datto

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidManagementAction = errors.New("invalid Datto management action")

type ManualSyncRequest struct {
	ID           string    `json:"id"`
	ConnectionID string    `json:"connection_id"`
	MSPID        string    `json:"-"`
	RequestedAt  time.Time `json:"requested_at"`
	RequestedBy  string    `json:"-"`
}

type SiteMapping struct {
	ID           string    `json:"id"`
	ConnectionID string    `json:"connection_id"`
	MSPID        string    `json:"-"`
	SiteID       string    `json:"site_id"`
	ClientID     string    `json:"client_id"`
	Reason       string    `json:"reason"`
	MappedAt     time.Time `json:"mapped_at"`
	MappedBy     string    `json:"-"`
}

type SyncProgress struct {
	ConnectionID      string         `json:"connection_id"`
	State             string         `json:"state"`
	RunID             string         `json:"run_id,omitempty"`
	Kind              string         `json:"kind,omitempty"`
	StartedAt         *time.Time     `json:"started_at,omitempty"`
	CompletedAt       *time.Time     `json:"completed_at,omitempty"`
	AssetsSeen        int            `json:"assets_seen"`
	AssetsChanged     int            `json:"assets_changed"`
	RateLimit         RateLimitState `json:"rate_limit"`
	LastErrorCode     string         `json:"last_error_code,omitempty"`
	HealthState       string         `json:"health_state"`
	PendingCandidates int            `json:"pending_candidates"`
	PendingAlerts     int            `json:"pending_alerts"`
	BlockedAlerts     int            `json:"blocked_alerts"`
}

type ReconciliationCandidate struct {
	ID         string         `json:"id"`
	SnapshotID string         `json:"snapshot_id"`
	MSPID      string         `json:"-"`
	ClientID   string         `json:"client_id"`
	AssetID    string         `json:"asset_id"`
	Evidence   map[string]any `json:"evidence"`
	State      string         `json:"state"`
}

type CandidateDecision struct {
	ID          string                 `json:"id"`
	CandidateID string                 `json:"candidate_id"`
	SnapshotID  string                 `json:"snapshot_id"`
	MSPID       string                 `json:"-"`
	ClientID    string                 `json:"client_id"`
	AssetID     string                 `json:"asset_id"`
	Decision    ReconciliationDecision `json:"decision"`
	Reason      string                 `json:"reason"`
	DecidedAt   time.Time              `json:"decided_at"`
	DecidedBy   string                 `json:"-"`
}

type ManagementRepository interface {
	QueueManualSync(context.Context, ManualSyncRequest) error
	MapSite(context.Context, SiteMapping) error
	Progress(context.Context, scope.Target, string) (SyncProgress, error)
	ListCandidates(
		context.Context,
		scope.Target,
		int,
	) ([]ReconciliationCandidate, error)
	FindCandidate(context.Context, string, string) (ReconciliationCandidate, error)
	DecideCandidate(context.Context, CandidateDecision) error
}

type ManagementService struct {
	repository ManagementRepository
	now        func() time.Time
	newID      func() string
}

func NewManagementService(
	repository ManagementRepository,
	now func() time.Time,
	newID func() string,
) *ManagementService {
	return &ManagementService{
		repository: repository, now: now, newID: newID,
	}
}

type ManualSyncCommand struct {
	Principal    authorization.Principal
	Target       scope.Target
	ConnectionID string
}

type MapSiteCommand struct {
	Principal    authorization.Principal
	ConnectionID string
	SiteID       string
	ClientID     string
	Reason       string
}

func (s *ManagementService) MapSite(
	ctx context.Context,
	command MapSiteCommand,
) (SiteMapping, error) {
	if s.repository == nil || s.now == nil || s.newID == nil ||
		strings.TrimSpace(command.ConnectionID) == "" ||
		strings.TrimSpace(command.SiteID) == "" ||
		strings.TrimSpace(command.ClientID) == "" ||
		strings.TrimSpace(command.Reason) == "" ||
		command.Principal.Scope.MSPID == "" {
		return SiteMapping{}, ErrInvalidManagementAction
	}
	target := scope.Target{
		MSPID:    command.Principal.Scope.MSPID,
		ClientID: strings.TrimSpace(command.ClientID),
	}
	if err := authorization.Authorize(
		command.Principal, "integration.manage", target,
	); err != nil {
		return SiteMapping{}, err
	}
	mapping := SiteMapping{
		ID: s.newID(), ConnectionID: strings.TrimSpace(command.ConnectionID),
		MSPID: target.MSPID, SiteID: strings.TrimSpace(command.SiteID),
		ClientID: target.ClientID, Reason: strings.TrimSpace(command.Reason),
		MappedAt: s.now().UTC(), MappedBy: command.Principal.ID,
	}
	if mapping.ID == "" || mapping.MappedBy == "" {
		return SiteMapping{}, ErrInvalidManagementAction
	}
	if err := s.repository.MapSite(ctx, mapping); err != nil {
		return SiteMapping{}, err
	}
	return mapping, nil
}

func (s *ManagementService) QueueManualSync(
	ctx context.Context,
	command ManualSyncCommand,
) (ManualSyncRequest, error) {
	if s.repository == nil || s.now == nil || s.newID == nil ||
		strings.TrimSpace(command.ConnectionID) == "" ||
		command.Target.MSPID == "" || command.Target.ClientID != "" {
		return ManualSyncRequest{}, ErrInvalidManagementAction
	}
	if err := authorization.Authorize(
		command.Principal, "integration.manage", command.Target,
	); err != nil {
		return ManualSyncRequest{}, err
	}
	request := ManualSyncRequest{
		ID: s.newID(), ConnectionID: strings.TrimSpace(command.ConnectionID),
		MSPID: command.Target.MSPID, RequestedAt: s.now().UTC(),
		RequestedBy: command.Principal.ID,
	}
	if request.ID == "" || request.RequestedBy == "" {
		return ManualSyncRequest{}, ErrInvalidManagementAction
	}
	if err := s.repository.QueueManualSync(ctx, request); err != nil {
		return ManualSyncRequest{}, err
	}
	return request, nil
}

type ProgressCommand struct {
	Principal    authorization.Principal
	Target       scope.Target
	ConnectionID string
}

func (s *ManagementService) Progress(
	ctx context.Context,
	command ProgressCommand,
) (SyncProgress, error) {
	if strings.TrimSpace(command.ConnectionID) == "" ||
		command.Target.MSPID == "" || command.Target.ClientID != "" {
		return SyncProgress{}, ErrInvalidManagementAction
	}
	if err := authorization.Authorize(
		command.Principal, "integration.read", command.Target,
	); err != nil {
		return SyncProgress{}, err
	}
	return s.repository.Progress(
		ctx, command.Target, strings.TrimSpace(command.ConnectionID),
	)
}

type DecisionCommand struct {
	Principal   authorization.Principal
	CandidateID string
	Decision    ReconciliationDecision
	Reason      string
}

type ListCandidatesCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	Limit     int
}

func (s *ManagementService) ListCandidates(
	ctx context.Context,
	command ListCandidatesCommand,
) ([]ReconciliationCandidate, error) {
	if s.repository == nil ||
		command.Target.MSPID == "" ||
		command.Target.MSPID != command.Principal.Scope.MSPID ||
		command.Target.ClientID != command.Principal.Scope.ClientID ||
		command.Limit < 1 || command.Limit > 100 {
		return nil, ErrInvalidManagementAction
	}
	if err := authorization.Authorize(
		command.Principal, "integration.datto.reconcile", command.Target,
	); err != nil {
		return nil, err
	}
	return s.repository.ListCandidates(
		ctx, command.Target, command.Limit,
	)
}

func (s *ManagementService) Decide(
	ctx context.Context,
	command DecisionCommand,
) (CandidateDecision, error) {
	if s.repository == nil || s.now == nil || s.newID == nil ||
		strings.TrimSpace(command.CandidateID) == "" ||
		strings.TrimSpace(command.Reason) == "" ||
		!supportedDecision(command.Decision) ||
		command.Principal.Scope.MSPID == "" {
		return CandidateDecision{}, ErrInvalidManagementAction
	}
	candidate, err := s.repository.FindCandidate(
		ctx, command.Principal.Scope.MSPID,
		strings.TrimSpace(command.CandidateID),
	)
	if err != nil {
		return CandidateDecision{}, err
	}
	target := scope.Target{
		MSPID: candidate.MSPID, ClientID: candidate.ClientID,
	}
	if err := authorization.Authorize(
		command.Principal, "integration.datto.reconcile", target,
	); err != nil {
		return CandidateDecision{}, err
	}
	decision := CandidateDecision{
		ID: s.newID(), CandidateID: candidate.ID,
		SnapshotID: candidate.SnapshotID, MSPID: candidate.MSPID,
		ClientID: candidate.ClientID, AssetID: candidate.AssetID,
		Decision: command.Decision, Reason: strings.TrimSpace(command.Reason),
		DecidedAt: s.now().UTC(), DecidedBy: command.Principal.ID,
	}
	if decision.ID == "" || decision.DecidedBy == "" {
		return CandidateDecision{}, ErrInvalidManagementAction
	}
	if err := s.repository.DecideCandidate(ctx, decision); err != nil {
		return CandidateDecision{}, err
	}
	return decision, nil
}

func supportedDecision(decision ReconciliationDecision) bool {
	switch decision {
	case DecisionLink, DecisionChooseRarity,
		DecisionChooseDatto, DecisionKeepSeparate:
		return true
	default:
		return false
	}
}

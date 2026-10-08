// Package datto defines the read-only Datto RMM ingestion and reconciliation
// boundary. It intentionally contains no command capable of writing to Datto.
package datto

import (
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
)

var ErrInvalidDecision = errors.New("invalid Datto reconciliation decision")

type Connection struct {
	ID                  string
	MSPID               string
	CredentialSecretRef string
	Cursor              string
	Manual              bool
	ManualRequestID     string
	SyncInterval        time.Duration
	LastCompletedAt     time.Time
}

type SyncKind string

const (
	SyncFull        SyncKind = "full"
	SyncIncremental SyncKind = "incremental"
)

type SyncPlan struct {
	Kind     SyncKind
	Due      bool
	Interval time.Duration
}

func PlanSync(connection Connection, now time.Time) SyncPlan {
	interval := connection.SyncInterval
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	if strings.TrimSpace(connection.Cursor) == "" {
		return SyncPlan{Kind: SyncFull, Due: true, Interval: interval}
	}
	return SyncPlan{
		Kind: SyncIncremental, Interval: interval,
		Due: !connection.LastCompletedAt.After(now.Add(-interval)),
	}
}

type AssetState string

const (
	AssetActive AssetState = "active"
	AssetStale  AssetState = "stale"
)

type Asset struct {
	ID              string
	DattoExternalID string
	Hostname        string
	SerialNumber    string
	MACAddresses    []string
	State           AssetState
	Deleted         bool
	StaleSince      *time.Time
}

type RemoteAsset struct {
	ExternalID       string
	SiteID           string
	Hostname         string
	SerialNumber     string
	MACAddresses     []string
	SourcePayloadRef string
	SourceUpdatedAt  time.Time
}

type MatchKind string

const (
	MatchNone   MatchKind = "none"
	MatchExact  MatchKind = "exact"
	MatchReview MatchKind = "review"
)

type AssetMatch struct {
	Kind              MatchKind
	AssetID           string
	CandidateAssetIDs []string
}

func MatchAsset(remote RemoteAsset, existing []Asset) AssetMatch {
	for _, asset := range existing {
		if remote.ExternalID != "" && asset.DattoExternalID == remote.ExternalID {
			return AssetMatch{Kind: MatchExact, AssetID: asset.ID}
		}
	}
	candidates := make([]string, 0)
	for _, asset := range existing {
		if candidateEvidence(remote, asset) {
			candidates = append(candidates, asset.ID)
		}
	}
	if len(candidates) > 0 {
		return AssetMatch{Kind: MatchReview, CandidateAssetIDs: candidates}
	}
	return AssetMatch{Kind: MatchNone}
}

func candidateEvidence(remote RemoteAsset, asset Asset) bool {
	if equalNonblank(remote.SerialNumber, asset.SerialNumber) ||
		equalNonblank(remote.Hostname, asset.Hostname) {
		return true
	}
	for _, remoteMAC := range remote.MACAddresses {
		for _, localMAC := range asset.MACAddresses {
			if equalNonblank(remoteMAC, localMAC) {
				return true
			}
		}
	}
	return false
}

func equalNonblank(left, right string) bool {
	return strings.TrimSpace(left) != "" &&
		strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

type ReconciliationDecision string

const (
	DecisionLink         ReconciliationDecision = "link"
	DecisionChooseRarity ReconciliationDecision = "choose_rarity"
	DecisionChooseDatto  ReconciliationDecision = "choose_datto"
	DecisionKeepSeparate ReconciliationDecision = "keep_separate"
)

type ReconciliationResult struct {
	Asset Asset
	Audit mutation.AuditRecord
}

func ApplyDecision(
	local Asset,
	remote RemoteAsset,
	decision ReconciliationDecision,
	actorID string,
	reason string,
) (ReconciliationResult, error) {
	if strings.TrimSpace(local.ID) == "" || strings.TrimSpace(remote.ExternalID) == "" ||
		strings.TrimSpace(actorID) == "" || strings.TrimSpace(reason) == "" {
		return ReconciliationResult{}, ErrInvalidDecision
	}
	result := local
	switch decision {
	case DecisionLink, DecisionChooseRarity:
		result.DattoExternalID = remote.ExternalID
	case DecisionChooseDatto:
		result.DattoExternalID = remote.ExternalID
		result.Hostname = remote.Hostname
		result.SerialNumber = remote.SerialNumber
		result.MACAddresses = append([]string(nil), remote.MACAddresses...)
	case DecisionKeepSeparate:
	default:
		return ReconciliationResult{}, ErrInvalidDecision
	}
	return ReconciliationResult{
		Asset: result,
		Audit: mutation.AuditRecord{
			ActorType: "technician", ActorID: actorID,
			Action: "datto.asset.reconciled", SubjectType: "asset",
			SubjectID: local.ID, Source: "datto", Reason: reason,
		},
	}, nil
}

func MarkMissingFromDatto(asset Asset, detectedAt time.Time) Asset {
	result := asset
	result.State = AssetStale
	result.Deleted = false
	value := detectedAt.UTC()
	result.StaleSince = &value
	return result
}

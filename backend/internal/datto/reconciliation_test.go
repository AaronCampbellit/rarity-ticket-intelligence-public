package datto

import (
	"testing"
	"time"
)

func TestPlanSyncUsesFullSnapshotThenFifteenMinuteIncrementalDefault(t *testing.T) {
	now := time.Date(2026, time.July, 29, 21, 0, 0, 0, time.UTC)
	full := PlanSync(Connection{ID: "connection-id"}, now)
	if full.Kind != SyncFull || !full.Due {
		t.Fatalf("new connection did not request full sync: %+v", full)
	}
	incremental := PlanSync(Connection{
		ID: "connection-id", Cursor: "cursor-1", LastCompletedAt: now.Add(-15 * time.Minute),
	}, now)
	if incremental.Kind != SyncIncremental || !incremental.Due ||
		incremental.Interval != 15*time.Minute {
		t.Fatalf("incremental default invalid: %+v", incremental)
	}
	notDue := PlanSync(Connection{
		ID: "connection-id", Cursor: "cursor-1", LastCompletedAt: now.Add(-14 * time.Minute),
	}, now)
	if notDue.Due {
		t.Fatalf("incremental sync scheduled early: %+v", notDue)
	}
}

func TestMatchAssetUsesStableDattoIdentityAsOnlyAutomaticMatch(t *testing.T) {
	remote := RemoteAsset{
		ExternalID: "datto-device-id", SerialNumber: "SERIAL-1",
		Hostname: "WORKSTATION-1", MACAddresses: []string{"00:11:22:33:44:55"},
	}
	existing := []Asset{
		{ID: "asset-candidate", SerialNumber: "SERIAL-1", Hostname: "WORKSTATION-1"},
		{ID: "asset-linked", DattoExternalID: "datto-device-id"},
	}
	match := MatchAsset(remote, existing)
	if match.Kind != MatchExact || match.AssetID != "asset-linked" {
		t.Fatalf("stable external identity was not authoritative: %+v", match)
	}

	existing[1].DattoExternalID = ""
	match = MatchAsset(remote, existing)
	if match.Kind != MatchReview || len(match.CandidateAssetIDs) != 1 ||
		match.CandidateAssetIDs[0] != "asset-candidate" {
		t.Fatalf("human attributes bypassed reconciliation: %+v", match)
	}
}

func TestReconciliationDecisionPreservesAuthorityAndNeverDeletes(t *testing.T) {
	remote := RemoteAsset{ExternalID: "datto-id", Hostname: "DATTO-NAME"}
	local := Asset{ID: "asset-id", Hostname: "RARITY-NAME"}
	dattoResult, err := ApplyDecision(local, remote, DecisionChooseDatto, "technician-id", "verified source")
	if err != nil {
		t.Fatalf("ApplyDecision() error = %v", err)
	}
	if dattoResult.Asset.Hostname != "DATTO-NAME" ||
		dattoResult.Asset.DattoExternalID != "datto-id" ||
		dattoResult.Audit.Action != "datto.asset.reconciled" {
		t.Fatalf("Datto decision invalid: %+v", dattoResult)
	}
	rarityResult, err := ApplyDecision(local, remote, DecisionChooseRarity, "technician-id", "local exception")
	if err != nil || rarityResult.Asset.Hostname != "RARITY-NAME" {
		t.Fatalf("Rarity authority was not preserved: result=%+v err=%v", rarityResult, err)
	}
	stale := MarkMissingFromDatto(local, time.Now())
	if stale.Deleted || stale.State != AssetStale {
		t.Fatalf("missing Datto asset was deleted: %+v", stale)
	}
}

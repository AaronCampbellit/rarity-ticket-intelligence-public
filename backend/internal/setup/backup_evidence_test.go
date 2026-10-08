package setup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
	"time"
)

func TestBackupEvidenceSignatureIsInstallationBoundAndExpires(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	at := time.Date(2026, 8, 4, 20, 0, 0, 0, time.UTC)
	payload := BackupEvidencePayload{
		MSPID: "msp-id", Nonce: "nonce", IssuedAt: at,
		Stanza: "rarity", Repository: "remote-s3",
		LatestBackupAt:    at.Add(-time.Hour),
		LatestWALAt:       at.Add(-time.Minute),
		RestoreVerifiedAt: at.Add(-24 * time.Hour),
	}
	signature, err := SignBackupEvidence(payload, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyBackupEvidence(payload, signature, key, "msp-id",
		at.Add(10*time.Minute)); err != nil {
		t.Fatalf("valid evidence rejected: %v", err)
	}
	changed := payload
	changed.MSPID = "other-msp"
	if VerifyBackupEvidence(changed, signature, key, "other-msp", at) == nil {
		t.Fatal("changed installation accepted")
	}
	if VerifyBackupEvidence(payload, signature, key, "msp-id",
		at.Add(16*time.Minute)) == nil {
		t.Fatal("expired evidence accepted")
	}
}

func TestAcceptBackupEvidenceRecordsSafeReplayProtectedResult(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	at := time.Date(2026, 8, 4, 20, 0, 0, 0, time.UTC)
	payload := BackupEvidencePayload{
		MSPID: "msp-id", Nonce: "one-time-nonce", IssuedAt: at,
		Stanza: "rarity", Repository: "remote-s3",
		LatestBackupAt: at.Add(-time.Hour), LatestWALAt: at.Add(-time.Minute),
		RestoreVerifiedAt: at.Add(-24 * time.Hour),
	}
	signature, _ := SignBackupEvidence(payload, key)
	repository := &fakeRepository{status: CenterStatus{ConfigurationVersion: 3}}
	service := NewService(repository, func() time.Time { return at },
		func() string { return "00000000-0000-4000-8000-000000000010" },
		WithExpectedMSPID("msp-id"), WithBackupEvidenceKey(key))
	_, err := service.AcceptBackupEvidence(context.Background(),
		AcceptBackupEvidenceCommand{Payload: payload, Signature: signature,
			ExpectedVersion: 3, Source: "backup-cli"})
	if err != nil {
		t.Fatal(err)
	}
	if repository.verification.SafeCode != "backup_verified" ||
		repository.verification.ConsumedNonce != "one-time-nonce" ||
		len(repository.verification.EvidenceHash) != sha256.Size {
		t.Fatalf("verification=%+v", repository.verification)
	}
}

func TestBackupEvidencePolicyDistinguishesBackupWALAndRestoreFreshness(t *testing.T) {
	now := time.Date(2026, 8, 4, 20, 0, 0, 0, time.UTC)
	base := BackupEvidencePayload{
		LatestBackupAt:    now.Add(-time.Hour),
		LatestWALAt:       now.Add(-time.Minute),
		RestoreVerifiedAt: now.Add(-24 * time.Hour),
	}
	if state, code := BackupEvidenceState(base, now); state != ReadinessVerified ||
		code != "backup_verified" {
		t.Fatalf("state=%s code=%s", state, code)
	}
	base.LatestWALAt = now.Add(-16 * time.Minute)
	if _, code := BackupEvidenceState(base, now); code != "wal_stale" {
		t.Fatalf("code=%s", code)
	}
}

package setup

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
)

var ErrInvalidBackupEvidence = errors.New("invalid backup evidence")

type BackupEvidencePayload struct {
	MSPID             string    `json:"msp_id"`
	Nonce             string    `json:"nonce"`
	IssuedAt          time.Time `json:"issued_at"`
	Stanza            string    `json:"stanza"`
	Repository        string    `json:"repository"`
	LatestBackupAt    time.Time `json:"latest_backup_at"`
	LatestWALAt       time.Time `json:"latest_wal_at"`
	RestoreVerifiedAt time.Time `json:"restore_verified_at"`
}

type AcceptBackupEvidenceCommand struct {
	Payload         BackupEvidencePayload
	Signature       string
	ExpectedVersion int64
	Source          string
}

func SignBackupEvidence(
	payload BackupEvidencePayload,
	key []byte,
) (string, error) {
	if len(key) < 32 {
		return "", ErrInvalidBackupEvidence
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", ErrInvalidBackupEvidence
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(encoded)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func VerifyBackupEvidence(
	payload BackupEvidencePayload,
	signature string,
	key []byte,
	expectedMSPID string,
	now time.Time,
) error {
	if strings.TrimSpace(payload.MSPID) != strings.TrimSpace(expectedMSPID) ||
		strings.TrimSpace(payload.Nonce) == "" ||
		now.Before(payload.IssuedAt.Add(-time.Minute)) ||
		now.After(payload.IssuedAt.Add(15*time.Minute)) {
		return ErrInvalidBackupEvidence
	}
	expected, err := SignBackupEvidence(payload, key)
	if err != nil {
		return err
	}
	expectedBytes, _ := base64.RawURLEncoding.DecodeString(expected)
	actualBytes, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(expectedBytes, actualBytes) {
		return ErrInvalidBackupEvidence
	}
	return nil
}

func BackupEvidenceState(
	payload BackupEvidencePayload,
	now time.Time,
) (ReadinessState, string) {
	switch {
	case payload.LatestBackupAt.IsZero() ||
		now.Sub(payload.LatestBackupAt) > 24*time.Hour:
		return ReadinessAttention, "backup_stale"
	case payload.LatestWALAt.IsZero() ||
		now.Sub(payload.LatestWALAt) > 15*time.Minute:
		return ReadinessAttention, "wal_stale"
	case payload.RestoreVerifiedAt.IsZero() ||
		now.Sub(payload.RestoreVerifiedAt) > 31*24*time.Hour:
		return ReadinessAttention, "restore_proof_stale"
	default:
		return ReadinessVerified, "backup_verified"
	}
}

func (s *Service) AcceptBackupEvidence(
	ctx context.Context,
	command AcceptBackupEvidenceCommand,
) (CenterStatus, error) {
	now := s.now().UTC()
	if s.repository == nil || s.newID == nil || command.ExpectedVersion < 1 ||
		strings.TrimSpace(command.Source) == "" ||
		VerifyBackupEvidence(command.Payload, command.Signature,
			s.backupEvidenceKey, s.expectedMSPID, now) != nil {
		return CenterStatus{}, ErrInvalidBackupEvidence
	}
	state, safeCode := BackupEvidenceState(command.Payload, now)
	encoded, _ := json.Marshal(command.Payload)
	hash := sha256.Sum256(encoded)
	validUntil := command.Payload.LatestBackupAt.Add(24 * time.Hour)
	for _, candidate := range []time.Time{
		command.Payload.LatestWALAt.Add(15 * time.Minute),
		command.Payload.RestoreVerifiedAt.Add(31 * 24 * time.Hour),
	} {
		if candidate.Before(validUntil) {
			validUntil = candidate
		}
	}
	correlationID := s.newID()
	action := "installation.setup.backups.verified"
	value := VerificationMutation{
		ExpectedVersion: command.ExpectedVersion, Section: "backups",
		State: state, SafeCode: safeCode, CheckedAt: now, ValidUntil: validUntil,
		Details: map[string]string{
			"stanza":     command.Payload.Stanza,
			"repository": command.Payload.Repository,
		},
		EvidenceHash: hash[:], ConsumedNonce: command.Payload.Nonce,
		Audit: mutation.AuditRecord{
			ID: s.newID(), OccurredAt: now, MSPID: command.Payload.MSPID,
			ActorType: "service", ActorID: command.Payload.MSPID,
			Action: action, SubjectType: "installation",
			SubjectID:      command.Payload.MSPID,
			SubjectVersion: command.ExpectedVersion + 1,
			Source:         command.Source, Reason: "Signed pgBackRest verification evidence",
			CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: s.newID(), EventType: action, SchemaVersion: 1,
			OccurredAt: now, MSPID: command.Payload.MSPID,
			ActorType: "service", ActorID: command.Payload.MSPID,
			SubjectType: "installation", SubjectID: command.Payload.MSPID,
			SubjectVersion: command.ExpectedVersion + 1,
			Source:         command.Source, CorrelationID: correlationID,
		},
	}
	if err := s.repository.RecordVerification(ctx, value); err != nil {
		return CenterStatus{}, err
	}
	return s.composeCenterStatus(ctx, command.Payload.MSPID)
}

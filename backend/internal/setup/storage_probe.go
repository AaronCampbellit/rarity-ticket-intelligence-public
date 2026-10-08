package setup

import (
	"bytes"
	"context"
	"io"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type StorageProbe interface {
	Put(context.Context, string, io.Reader, int64, string) error
	Delete(context.Context, string) error
}

type VerifyObjectStorageCommand struct {
	Principal       authorization.Principal
	ExpectedVersion int64
	Reason          string
	Source          string
}

type VerificationMutation struct {
	ExpectedVersion int64
	Section         string
	State           ReadinessState
	SafeCode        string
	CheckedAt       time.Time
	ValidUntil      time.Time
	Details         map[string]string
	EvidenceHash    []byte
	ConsumedNonce   string
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

func (s *Service) VerifyObjectStorage(
	ctx context.Context,
	command VerifyObjectStorageCommand,
) (CenterStatus, error) {
	target := scope.Target{MSPID: command.Principal.Scope.MSPID}
	if err := authorization.Authorize(command.Principal, "organization.manage", target); err != nil {
		return CenterStatus{}, err
	}
	reason, source := strings.TrimSpace(command.Reason), strings.TrimSpace(command.Source)
	if s.repository == nil || s.storageProbe == nil || s.newID == nil ||
		command.ExpectedVersion < 1 || reason == "" || source == "" {
		return CenterStatus{}, ErrInvalidCenterConfiguration
	}
	at, correlationID := s.now().UTC(), s.newID()
	key := "setup-probes/" + target.MSPID + "/" + s.newID()
	body := []byte("rarity-storage-probe")
	state, safeCode := ReadinessVerified, "storage_verified"
	if err := s.storageProbe.Put(ctx, key, bytes.NewReader(body), int64(len(body)),
		"application/octet-stream"); err != nil {
		state, safeCode = ReadinessAttention, "storage_write_failed"
	} else if err := s.storageProbe.Delete(ctx, key); err != nil {
		state, safeCode = ReadinessAttention, "storage_cleanup_failed"
	}
	action := "installation.setup.object_storage.verified"
	value := VerificationMutation{
		ExpectedVersion: command.ExpectedVersion, Section: "object_storage",
		State: state, SafeCode: safeCode, CheckedAt: at,
		ValidUntil: at.Add(30 * time.Minute), Details: map[string]string{},
		Audit: mutation.AuditRecord{
			ID: s.newID(), OccurredAt: at, MSPID: target.MSPID,
			ActorType: "technician", ActorID: command.Principal.ID,
			Action: action, SubjectType: "installation", SubjectID: target.MSPID,
			SubjectVersion: command.ExpectedVersion + 1,
			Source:         source, Reason: reason, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: s.newID(), EventType: action, SchemaVersion: 1,
			OccurredAt: at, MSPID: target.MSPID,
			ActorType: "technician", ActorID: command.Principal.ID,
			SubjectType: "installation", SubjectID: target.MSPID,
			SubjectVersion: command.ExpectedVersion + 1,
			Source:         source, CorrelationID: correlationID,
		},
	}
	if err := s.repository.RecordVerification(ctx, value); err != nil {
		return CenterStatus{}, err
	}
	return s.composeCenterStatus(ctx, target.MSPID)
}

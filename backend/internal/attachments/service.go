// Package attachments streams authorized Work Record files to object storage
// and persists only scoped metadata and checksums.
package attachments

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrRejected = errors.New("attachment rejected")

type Policy struct {
	MaxBytes            int64
	AllowedContentTypes map[string]struct{}
}

type Attachment struct {
	ID            string
	MSPID         string
	ClientID      string
	WorkRecordID  string
	OpportunityID string
	Filename      string
	ContentType   string
	SizeBytes     int64
	SHA256        [32]byte
	StorageKey    string
	Version       int64
	UploadedBy    string
	CreatedAt     time.Time
}

type UploadCommand struct {
	Principal     authorization.Principal
	Target        scope.Target
	WorkRecordID  string
	OpportunityID string
	Filename      string
	ContentType   string
	SizeBytes     int64
	ActorID       string
	Source        string
}

type UploadMutation struct {
	Attachment Attachment
	Audit      mutation.AuditRecord
	Event      mutation.EventRecord
}

type Repository interface {
	CreateAtomic(context.Context, UploadMutation) error
	ListOpportunity(context.Context, scope.Target, string, int) ([]Attachment, error)
}

type ObjectStore interface {
	Put(context.Context, string, io.Reader, int64, string) error
	Delete(context.Context, string) error
}

type Service struct {
	repository Repository
	store      ObjectStore
	policy     Policy
	now        func() time.Time
	newID      func() string
}

func NewService(
	repository Repository,
	store ObjectStore,
	policy Policy,
	now func() time.Time,
	newID func() string,
) *Service {
	return &Service{
		repository: repository, store: store, policy: policy,
		now: now, newID: newID,
	}
}

func (s *Service) Upload(
	ctx context.Context,
	command UploadCommand,
	body io.Reader,
) (Attachment, error) {
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID:    command.Principal.Scope.MSPID,
			ClientID: command.Principal.Scope.ClientID,
		}
	}
	filename := safeFilename(command.Filename)
	contentType := strings.ToLower(strings.TrimSpace(command.ContentType))
	_, contentAllowed := s.policy.AllowedContentTypes[contentType]
	if s.repository == nil || s.store == nil || s.now == nil || s.newID == nil ||
		target.ClientID == "" ||
		(command.WorkRecordID == "") == (command.OpportunityID == "") ||
		command.ActorID == "" ||
		command.Source == "" ||
		filename == "" ||
		command.SizeBytes < 0 ||
		command.SizeBytes > s.policy.MaxBytes ||
		!contentAllowed ||
		body == nil {
		return Attachment{}, ErrRejected
	}
	if err := authorization.Authorize(command.Principal, "attachment.create", target); err != nil {
		return Attachment{}, err
	}
	attachmentID := s.newID()
	storageKey := fmt.Sprintf("%s/%s/%s", target.MSPID, target.ClientID, attachmentID)
	hasher := sha256.New()
	counter := &countingReader{reader: io.TeeReader(body, hasher)}
	if err := s.store.Put(
		ctx, storageKey, counter, command.SizeBytes, contentType,
	); err != nil {
		return Attachment{}, err
	}
	if counter.count != command.SizeBytes {
		_ = s.store.Delete(ctx, storageKey)
		return Attachment{}, ErrRejected
	}
	now := s.now().UTC()
	var digest [32]byte
	copy(digest[:], hasher.Sum(nil))
	attachment := Attachment{
		ID: attachmentID, MSPID: target.MSPID, ClientID: target.ClientID,
		WorkRecordID: command.WorkRecordID, OpportunityID: command.OpportunityID,
		Filename:    filename,
		ContentType: contentType, SizeBytes: command.SizeBytes,
		SHA256: digest, StorageKey: storageKey, Version: 1,
		UploadedBy: command.ActorID, CreatedAt: now,
	}
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := UploadMutation{
		Attachment: attachment,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "attachment.created", SubjectType: "attachment",
			SubjectID: attachment.ID, SubjectVersion: attachment.Version,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "attachment.created", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "attachment", SubjectID: attachment.ID,
			SubjectVersion: attachment.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.CreateAtomic(ctx, accepted); err != nil {
		_ = s.store.Delete(ctx, storageKey)
		return Attachment{}, err
	}
	return attachment, nil
}

func (s *Service) ListOpportunity(
	ctx context.Context,
	principal authorization.Principal,
	opportunityID string,
	limit int,
) ([]Attachment, error) {
	target := scope.Target{
		MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
	}
	if s.repository == nil || target.MSPID == "" || target.ClientID == "" ||
		strings.TrimSpace(opportunityID) == "" || limit < 0 {
		return nil, ErrRejected
	}
	if limit == 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}
	if err := authorization.Authorize(principal, "opportunity.read", target); err != nil {
		return nil, err
	}
	return s.repository.ListOpportunity(
		ctx, target, strings.TrimSpace(opportunityID), limit,
	)
}

func safeFilename(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	filename := path.Base(value)
	if filename == "." || filename == "/" || filename == "" ||
		strings.ContainsRune(filename, '\x00') {
		return ""
	}
	return filename
}

type countingReader struct {
	reader io.Reader
	count  int64
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	read, err := r.reader.Read(buffer)
	r.count += int64(read)
	return read, err
}

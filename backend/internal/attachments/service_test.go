package attachments

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type captureRepository struct {
	mutation UploadMutation
	listed   []Attachment
	err      error
}

func (r *captureRepository) CreateAtomic(
	_ context.Context,
	mutation UploadMutation,
) error {
	r.mutation = mutation
	return r.err
}

func (r *captureRepository) ListOpportunity(
	_ context.Context,
	_ scope.Target,
	_ string,
	_ int,
) ([]Attachment, error) {
	return r.listed, r.err
}

type memoryObjectStore struct {
	key     string
	body    []byte
	deleted string
	err     error
}

func (s *memoryObjectStore) Put(
	_ context.Context,
	key string,
	body io.Reader,
	_ int64,
	_ string,
) error {
	s.key = key
	if s.err != nil {
		return s.err
	}
	s.body, _ = io.ReadAll(body)
	return nil
}

func (s *memoryObjectStore) Delete(_ context.Context, key string) error {
	s.deleted = key
	return nil
}

func TestUploadStreamsChecksumAndWritesMetadataFacts(t *testing.T) {
	repository := &captureRepository{}
	store := &memoryObjectStore{}
	now := time.Date(2026, time.July, 30, 19, 0, 0, 0, time.UTC)
	ids := []string{"attachment-id", "audit-id", "event-id", "correlation-id"}
	service := NewService(repository, store, Policy{
		MaxBytes:            10 << 20,
		AllowedContentTypes: map[string]struct{}{"image/png": {}},
	}, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	principal := attachmentPrincipal()
	body := []byte("synthetic png")
	attachment, err := service.Upload(context.Background(), UploadCommand{
		Principal: principal, WorkRecordID: "work-id", Filename: `..\..\screen.png`,
		ContentType: "image/png", SizeBytes: int64(len(body)),
		ActorID: "actor-id", Source: "api",
	}, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if attachment.Filename != "screen.png" ||
		attachment.StorageKey != "msp-id/client-id/attachment-id" ||
		!bytes.Equal(store.body, body) ||
		repository.mutation.Attachment.SHA256 == ([32]byte{}) ||
		repository.mutation.Audit.Action != "attachment.created" ||
		repository.mutation.Event.EventType != "attachment.created" {
		t.Fatalf("unexpected attachment/store/mutation: %+v %+v %+v", attachment, store, repository.mutation)
	}
}

func TestOpportunityUploadAndListingUseScopedParent(t *testing.T) {
	repository := &captureRepository{listed: []Attachment{{
		ID: "attachment-id", OpportunityID: "opportunity-id",
	}}}
	store := &memoryObjectStore{}
	body := []byte("scope")
	service := NewService(repository, store, Policy{
		MaxBytes: 1024, AllowedContentTypes: map[string]struct{}{"text/plain": {}},
	}, time.Now, func() string { return "attachment-id" })
	principal := attachmentPrincipal()
	principal.Capabilities = authorization.NewCapabilitySet(
		"attachment.create", "opportunity.read",
	)
	attachment, err := service.Upload(context.Background(), UploadCommand{
		Principal: principal, OpportunityID: "opportunity-id",
		Filename: "scope.txt", ContentType: "text/plain", SizeBytes: int64(len(body)),
		ActorID: "actor-id", Source: "api",
	}, bytes.NewReader(body))
	if err != nil || attachment.OpportunityID != "opportunity-id" ||
		repository.mutation.Attachment.OpportunityID != "opportunity-id" {
		t.Fatalf("opportunity Upload() attachment=%+v error=%v", attachment, err)
	}
	found, err := service.ListOpportunity(
		context.Background(), principal, "opportunity-id", 500,
	)
	if err != nil || len(found) != 1 || found[0].OpportunityID != "opportunity-id" {
		t.Fatalf("ListOpportunity() found=%+v error=%v", found, err)
	}
}

func TestUploadRejectsOversizeAndUnapprovedContentBeforeStorage(t *testing.T) {
	policy := Policy{
		MaxBytes:            1024,
		AllowedContentTypes: map[string]struct{}{"image/png": {}},
	}
	tests := []UploadCommand{
		{Principal: attachmentPrincipal(), WorkRecordID: "work-id", Filename: "large.png", ContentType: "image/png", SizeBytes: 1025, ActorID: "actor-id", Source: "api"},
		{Principal: attachmentPrincipal(), WorkRecordID: "work-id", Filename: "payload.exe", ContentType: "application/x-msdownload", SizeBytes: 100, ActorID: "actor-id", Source: "api"},
	}
	for _, command := range tests {
		store := &memoryObjectStore{}
		service := NewService(&captureRepository{}, store, policy, time.Now, func() string { return "attachment-id" })
		if _, err := service.Upload(
			context.Background(), command, bytes.NewReader(nil),
		); !errors.Is(err, ErrRejected) {
			t.Fatalf("Upload(%q) error = %v, want ErrRejected", command.Filename, err)
		}
		if store.key != "" {
			t.Fatal("rejected upload reached object storage")
		}
	}
}

func TestUploadDeletesObjectWhenMetadataCommitFails(t *testing.T) {
	repository := &captureRepository{err: errors.New("database unavailable")}
	store := &memoryObjectStore{}
	body := []byte("content")
	service := NewService(repository, store, Policy{
		MaxBytes:            1024,
		AllowedContentTypes: map[string]struct{}{"text/plain": {}},
	}, time.Now, func() string { return "attachment-id" })
	_, err := service.Upload(context.Background(), UploadCommand{
		Principal: attachmentPrincipal(), WorkRecordID: "work-id",
		Filename: "notes.txt", ContentType: "text/plain",
		SizeBytes: int64(len(body)), ActorID: "actor-id", Source: "api",
	}, bytes.NewReader(body))
	if err == nil || store.deleted != "msp-id/client-id/attachment-id" {
		t.Fatalf("orphan cleanup failed: err=%v store=%+v", err, store)
	}
}

func attachmentPrincipal() authorization.Principal {
	return authorization.Principal{
		ID: "actor-id",
		Scope: scope.Principal{
			MSPID: "msp-id", ClientID: "client-id",
		},
		Capabilities: authorization.NewCapabilitySet("attachment.create"),
	}
}

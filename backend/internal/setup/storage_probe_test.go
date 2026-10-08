package setup

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

type recordingStorageProbe struct {
	key       string
	body      []byte
	deleted   string
	putErr    error
	deleteErr error
}

func (p *recordingStorageProbe) Put(
	_ context.Context, key string, body io.Reader, _ int64, _ string,
) error {
	p.key = key
	p.body, _ = io.ReadAll(body)
	return p.putErr
}

func (p *recordingStorageProbe) Delete(_ context.Context, key string) error {
	p.deleted = key
	return p.deleteErr
}

func TestVerifyObjectStorageWritesAndCleansUpBoundedProbe(t *testing.T) {
	at := time.Date(2026, 8, 4, 20, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		status: CenterStatus{ConfigurationVersion: 3},
	}
	probe := &recordingStorageProbe{}
	service := NewService(repository, func() time.Time { return at },
		func() string { return "00000000-0000-4000-8000-000000000010" },
		WithStorageProbe(probe))
	_, err := service.VerifyObjectStorage(context.Background(),
		VerifyObjectStorageCommand{
			Principal: authorization.Principal{
				ID:           "00000000-0000-4000-8000-000000000002",
				Scope:        scope.Principal{MSPID: "00000000-0000-4000-8000-000000000001"},
				Capabilities: authorization.NewCapabilitySet("organization.manage"),
			},
			ExpectedVersion: 3, Reason: "Verify attachment bucket", Source: "browser",
		})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(probe.body, []byte("rarity-storage-probe")) ||
		probe.deleted != probe.key {
		t.Fatalf("probe=%+v", probe)
	}
	if repository.verification.SafeCode != "storage_verified" ||
		repository.verification.ValidUntil != at.Add(30*time.Minute) {
		t.Fatalf("verification=%+v", repository.verification)
	}
}

func TestVerifyObjectStorageReportsCleanupFailure(t *testing.T) {
	repository := &fakeRepository{status: CenterStatus{ConfigurationVersion: 3}}
	probe := &recordingStorageProbe{deleteErr: errors.New("provider detail")}
	service := NewService(repository, time.Now, func() string { return "id" },
		WithStorageProbe(probe))
	_, err := service.VerifyObjectStorage(context.Background(),
		VerifyObjectStorageCommand{
			Principal: authorization.Principal{
				ID: "admin", Scope: scope.Principal{MSPID: "msp"},
				Capabilities: authorization.NewCapabilitySet("organization.manage"),
			},
			ExpectedVersion: 3, Reason: "Verify", Source: "browser",
		})
	if err != nil {
		t.Fatal(err)
	}
	if repository.verification.SafeCode != "storage_cleanup_failed" ||
		repository.verification.State != ReadinessAttention {
		t.Fatalf("verification=%+v", repository.verification)
	}
}

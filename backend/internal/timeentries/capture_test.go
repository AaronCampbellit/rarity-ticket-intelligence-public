package timeentries

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

type captureRepositoryStub struct {
	mutation CaptureMutation
	entry    Entry
}

type captureClassificationRepository struct{}

func (captureClassificationRepository) ResolveTags(_ context.Context, mspID string, ids []string) ([]tagging.Tag, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return []tagging.Tag{{ID: ids[0], MSPID: mspID, InternalKey: "taxonomy.support", State: tagging.StateActive}}, nil
}
func (captureClassificationRepository) FindUnclassified(context.Context, string) (tagging.Tag, error) {
	return tagging.Tag{}, scope.ErrNotFound
}

func (r *captureRepositoryStub) CreateFromCaptureAtomic(
	_ context.Context,
	mutation CaptureMutation,
) (Entry, error) {
	r.mutation = mutation
	return r.entry, nil
}

func TestCreateFromCapturePassesOnlyTrustedReferencesToAtomicRepository(t *testing.T) {
	now := time.Date(2026, time.August, 4, 18, 30, 0, 0, time.UTC)
	repository := &captureRepositoryStub{entry: Entry{
		ID: "entry", MSPID: "msp", ClientID: "client",
		WorkRecordID: "ticket", TechnicianID: "tech",
		StartedAt: now.Add(-15 * time.Minute), EndedAt: now,
		DurationSeconds: 900, Billable: true, Note: "Investigated",
		LaborRoleVersionID: "role-version", InternalCostMinor: 6500,
		BillRateMinor: 18000, RateCurrency: "USD",
		Version: 1, CreatedAt: now, CreatedBy: "tech",
	}}
	next := 0
	service := NewCaptureService(
		repository,
		func() time.Time { return now },
		func() string {
			next++
			return []string{
				"entry", "entry-audit", "entry-event",
				"timer-audit", "timer-event", "correlation",
			}[next-1]
		},
		tagging.NewCreationPreparer(captureClassificationRepository{}),
	)
	found, err := service.Create(
		context.Background(),
		CaptureCommand{
			Principal: authorization.Principal{
				ID: "tech", Scope: scope.Principal{
					MSPID: "msp", ClientID: "client",
				},
				Capabilities: authorization.NewCapabilitySet("time_entry.create"),
			},
			WorkRecordID: "ticket", CaptureID: "capture",
			ExpectedCaptureVersion: 2, LaborRoleID: "role",
			Billable: true, Note: " Investigated ",
			ActorID: "tech", Source: "api",
			TagIDs: []string{"tag-id"}, ClassificationPolicy: tagging.CreationRequireMeaningful,
		},
	)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if found.DurationSeconds != 900 ||
		found.LaborRoleVersionID != "role-version" {
		t.Fatalf("Create() = %+v", found)
	}
	mutation := repository.mutation
	if mutation.EntryID != "entry" ||
		mutation.CaptureID != "capture" ||
		mutation.ExpectedCaptureVersion != 2 ||
		mutation.LaborRoleID != "role" ||
		mutation.Note != "Investigated" ||
		mutation.EntryAudit.Action != "time_entry.created_from_capture" ||
		mutation.TimerAudit.Action != "ticket_timer.consumed" ||
		mutation.EntryAudit.CorrelationID != mutation.TimerAudit.CorrelationID {
		t.Fatalf("mutation = %+v", mutation)
	}
}

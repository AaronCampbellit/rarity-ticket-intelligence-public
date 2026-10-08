package timeentries

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

type captureRepository struct {
	mutation CreateMutation
	calls    int
}

type classificationRepository struct{}

func (classificationRepository) ResolveTags(_ context.Context, mspID string, ids []string) ([]tagging.Tag, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return []tagging.Tag{{ID: ids[0], MSPID: mspID, InternalKey: "taxonomy.support", State: tagging.StateActive}}, nil
}
func (classificationRepository) FindUnclassified(_ context.Context, mspID string) (tagging.Tag, error) {
	return tagging.Tag{ID: "unclassified", MSPID: mspID, InternalKey: "unclassified", State: tagging.StateActive}, nil
}

func TestCreateClassificationPolicyRejectsInteractiveMissingTags(t *testing.T) {
	repository := &captureRepository{}
	service := NewService(repository, time.Now, func() string { return "id" }, tagging.NewCreationPreparer(classificationRepository{}))
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("time_entry.create"),
	}
	started := time.Date(2026, time.July, 30, 0, 0, 0, 0, time.UTC)
	_, err := service.Create(context.Background(), CreateCommand{
		Principal: principal, WorkRecordID: "work-id", TechnicianID: "tech-id",
		StartedAt: started, EndedAt: started.Add(time.Minute), ActorID: "tech-id", Source: "web",
		ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if !errors.Is(err, tagging.ErrMeaningfulTagRequired) {
		t.Fatalf("Create() error = %v, want meaningful-tag rejection", err)
	}
	if repository.calls != 0 {
		t.Fatal("missing-tag time entry reached repository")
	}
	_, err = service.initialTags(context.Background(), scope.Target{MSPID: "msp-id", ClientID: "client-id"}, CreateCommand{Source: "automation", ClassificationPolicy: tagging.CreationAllowFallback})
	if !errors.Is(err, tagging.ErrInvalidAssociation) {
		t.Fatalf("untrusted fallback error=%v", err)
	}
}

func (r *captureRepository) CreateAtomic(_ context.Context, mutation CreateMutation) error {
	r.calls++
	r.mutation = mutation
	return nil
}

func TestCreatePreservesExactDurationAndBillingClassification(t *testing.T) {
	repository := &captureRepository{}
	ids := []string{"entry-id", "audit-id", "event-id", "correlation-id"}
	service := NewService(repository, func() time.Time {
		return time.Date(2026, time.July, 30, 1, 0, 0, 0, time.UTC)
	}, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}, tagging.NewCreationPreparer(classificationRepository{}))
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("time_entry.create"),
	}
	started := time.Date(2026, time.July, 30, 0, 0, 0, 0, time.UTC)
	ended := started.Add(90*time.Second + 250*time.Millisecond)

	entry, err := service.Create(context.Background(), CreateCommand{
		Principal: principal, WorkRecordID: "work-id", TechnicianID: "tech-id",
		StartedAt: started, EndedAt: ended, Billable: true,
		Note: "Investigated authentication failure", ActorID: "tech-id", Source: "web", TagIDs: []string{"tag-id"}, ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if entry.DurationSeconds != 90 || !entry.Billable {
		t.Fatalf("unexpected time entry: %+v", entry)
	}
	if repository.mutation.Event.EventType != "time_entry.created" ||
		repository.mutation.Audit.Action != "time_entry.created" {
		t.Fatal("time entry did not produce audit and event facts")
	}
}

func TestCreateSupportsTaskOnlyProjectActualTime(t *testing.T) {
	repository := &captureRepository{}
	ids := []string{"entry-id", "audit-id", "event-id", "correlation-id"}
	service := NewService(repository, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}, tagging.NewCreationPreparer(classificationRepository{}))
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("time_entry.create"),
	}
	started := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	entry, err := service.Create(context.Background(), CreateCommand{
		Principal: principal, TaskID: "project-task-id", TechnicianID: "tech-id",
		StartedAt: started, EndedAt: started.Add(45 * time.Minute),
		ActorID: "tech-id", Source: "web", TagIDs: []string{"tag-id"}, ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if entry.WorkRecordID != "" || entry.TaskID != "project-task-id" ||
		entry.DurationSeconds != 2700 {
		t.Fatalf("unexpected Project task time entry: %+v", entry)
	}
}

func TestCreateRejectsNonPositiveWindowAndCrossClientScope(t *testing.T) {
	repository := &captureRepository{}
	service := NewService(repository, time.Now, func() string { return "unused" }, tagging.NewCreationPreparer(classificationRepository{}))
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-a"},
		Capabilities: authorization.NewCapabilitySet("time_entry.create"),
	}
	now := time.Now()
	_, err := service.Create(context.Background(), CreateCommand{
		Principal: principal, WorkRecordID: "work-id", TechnicianID: "tech-id",
		StartedAt: now, EndedAt: now, ActorID: "tech-id", Source: "web",
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero-duration error = %v", err)
	}
	_, err = service.Create(context.Background(), CreateCommand{
		Principal:    principal,
		Target:       scope.Target{MSPID: "msp-id", ClientID: "client-b"},
		WorkRecordID: "work-id", TechnicianID: "tech-id",
		StartedAt: now, EndedAt: now.Add(time.Minute), ActorID: "tech-id", Source: "web",
	})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-client error = %v", err)
	}
	if repository.calls != 0 {
		t.Fatal("invalid time entry reached repository")
	}
}

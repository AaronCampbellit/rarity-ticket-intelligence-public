package mentions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/observability"
)

type invalidationRepository struct {
	affected  []AffectedItem
	decisions map[string]InvalidationDecision
	claimed   int
	processed []string
}

func TestInvalidationEmitsSuppressionCounterWithoutContent(t *testing.T) {
	telemetry := observability.NewMentionTelemetry(nil)
	repository := &invalidationRepository{
		affected:  []AffectedItem{{ID: "item-1", MSPID: "msp", ClientID: "client", ParentType: ParentTask, ParentID: "task"}},
		decisions: map[string]InvalidationDecision{"item-1": {ItemSuppressed: true}},
	}
	_, err := NewInvalidationWorker(repository, fixedNow).WithTelemetry(telemetry).RunOnce(context.Background(), 1)
	if err != nil || telemetry.Value("suppression", "task", "access_revoked") != 1 {
		t.Fatalf("error=%v suppression counter missing", err)
	}
}

func TestInvalidationCountsEveryAccessRevokedNotificationCancellation(t *testing.T) {
	telemetry := observability.NewMentionTelemetry(nil)
	repository := &invalidationRepository{
		affected: []AffectedItem{{ID: "item-1", MSPID: "msp", ClientID: "client", ParentType: ParentTask, ParentID: "task"}},
		decisions: map[string]InvalidationDecision{
			"item-1": {ItemSuppressed: true, DeliveriesCanceled: 2},
		},
	}
	_, err := NewInvalidationWorker(repository, fixedNow).WithTelemetry(telemetry).RunOnce(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := telemetry.Value("notification", "task", "access_revoked"); got != 2 {
		t.Fatalf("access-revoked notification counter=%d want 2", got)
	}
}

func (r *invalidationRepository) ClaimAccessInvalidations(
	_ context.Context,
	limit int,
	_ time.Time,
	_ time.Duration,
) ([]AffectedItem, error) {
	r.claimed = limit
	result := append([]AffectedItem(nil), r.affected...)
	r.affected = nil
	return result, nil
}

func (r *invalidationRepository) ProcessAccessInvalidation(
	_ context.Context,
	item AffectedItem,
	_ time.Time,
) (InvalidationDecision, error) {
	r.processed = append(r.processed, item.ID)
	return r.decisions[item.ID], nil
}

func TestInvalidationSuppressesItemsAndCancelsPendingDelivery(t *testing.T) {
	repository := &invalidationRepository{
		affected: []AffectedItem{{ID: "item-1", ReasonCode: "role.removed"}},
		decisions: map[string]InvalidationDecision{
			"item-1": {AccessAllowed: false, ItemSuppressed: true, DeliveriesCanceled: 1},
		},
	}
	result, err := NewInvalidationWorker(repository, fixedNow).RunOnce(context.Background(), 50)
	if err != nil || result.Claimed != 1 || result.Suppressed != 1 || result.DeliveriesCanceled != 1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if repository.claimed != 50 || len(repository.processed) != 1 {
		t.Fatalf("repository=%+v", repository)
	}
}

func TestInvalidationRecomputesEveryAccessAffectingEvent(t *testing.T) {
	tests := []struct {
		name       string
		reason     string
		authorized bool
		suppressed int
	}{
		{name: "role removal", reason: "role.removed", suppressed: 1},
		{name: "client access removal", reason: "client.access.removed", suppressed: 1},
		{name: "project visibility removal", reason: "project.visibility.changed", suppressed: 1},
		{name: "technician disablement", reason: "technician.disabled", suppressed: 1},
		{name: "team membership without access change", reason: "team.members.replaced", authorized: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &invalidationRepository{
				affected: []AffectedItem{{ID: "item-1", ReasonCode: test.reason}},
				decisions: map[string]InvalidationDecision{
					"item-1": {AccessAllowed: test.authorized, ItemSuppressed: test.suppressed == 1},
				},
			}
			result, err := NewInvalidationWorker(repository, fixedNow).RunOnce(context.Background(), 1)
			if err != nil || result.Suppressed != test.suppressed || result.Completed != 1 {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestInvalidationReplayIsIdempotentAndAccessReturnDoesNotRestore(t *testing.T) {
	repository := &invalidationRepository{
		affected: []AffectedItem{{ID: "item-1"}},
		decisions: map[string]InvalidationDecision{
			"item-1": {AccessAllowed: false, ItemSuppressed: true},
		},
	}
	worker := NewInvalidationWorker(repository, fixedNow)
	first, err := worker.RunOnce(context.Background(), 10)
	if err != nil || first.Suppressed != 1 {
		t.Fatalf("first=%+v error=%v", first, err)
	}
	repository.decisions["item-1"] = InvalidationDecision{AccessAllowed: true}
	second, err := worker.RunOnce(context.Background(), 10)
	if err != nil || second.Claimed != 0 || second.Suppressed != 0 || len(repository.processed) != 1 {
		t.Fatalf("replay restored or reprocessed item: second=%+v repository=%+v error=%v", second, repository, err)
	}
}

func TestInvalidationRejectsUnboundedRunsAndStopsOnPersistenceFailure(t *testing.T) {
	if _, err := NewInvalidationWorker(&invalidationRepository{}, fixedNow).RunOnce(context.Background(), 0); !errors.Is(err, ErrInvalidInvalidationRun) {
		t.Fatalf("limit error=%v", err)
	}
	if _, err := NewInvalidationWorker(&invalidationRepository{}, fixedNow).RunOnce(context.Background(), 501); !errors.Is(err, ErrInvalidInvalidationRun) {
		t.Fatalf("limit error=%v", err)
	}
}

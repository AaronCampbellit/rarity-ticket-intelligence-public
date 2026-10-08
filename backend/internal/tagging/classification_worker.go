package tagging

import (
	"context"
	"sort"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

// BuildAutomaticReplacement produces an additive replacement set. It never
// drops existing direct (including human-selected) tags; the worker must call
// this only after reloading the current policy, target version, visibility,
// provider evidence, and active catalog state in its transaction.
func BuildAutomaticReplacement(existing []string, threshold float64, suggestions []ClassificationSuggestion) ([]string, bool) {
	if threshold < .5 || threshold > 1 {
		return []string{}, false
	}
	set := make(map[string]struct{}, len(existing)+len(suggestions))
	for _, id := range existing {
		if id != "" {
			set[id] = struct{}{}
		}
	}
	before := len(set)
	for _, id := range EligibleAutomaticTagIDs(threshold, suggestions) {
		set[id] = struct{}{}
	}
	if len(set) == before {
		return []string{}, false
	}
	result := make([]string, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	sort.Strings(result)
	return result, true
}

// ClassificationApplicationQueue owns a short lease around each durable
// suggestion application. Marking terminal state is idempotent by suggestion
// and lease token, so worker retries cannot double-apply an association.
type ClassificationApplicationQueue interface {
	ClaimClassificationApplications(context.Context, int, time.Duration) ([]ClassificationSuggestionRecord, error)
	FinishClassificationApplication(context.Context, ClassificationSuggestionRecord, string) error
}

type atomicClassificationApplicationQueue interface {
	ApplyClassificationApplication(context.Context, ClassificationSuggestionRecord) error
}

type ClassificationApplicationWorker struct {
	queue        ClassificationApplicationQueue
	associations *AssociationService
	now          func() time.Time
	newID        func() string
}

func NewClassificationApplicationWorker(queue ClassificationApplicationQueue, associations *AssociationService, now func() time.Time, newID func() string) *ClassificationApplicationWorker {
	return &ClassificationApplicationWorker{queue: queue, associations: associations, now: now, newID: newID}
}

func (w *ClassificationApplicationWorker) RunOnce(ctx context.Context, limit int) error {
	if w == nil || w.queue == nil || w.associations == nil || w.now == nil || w.newID == nil || limit < 1 {
		return ErrInvalidAssociation
	}
	items, err := w.queue.ClaimClassificationApplications(ctx, limit, time.Minute)
	if err != nil {
		return err
	}
	for _, item := range items {
		if atomic, ok := w.queue.(atomicClassificationApplicationQueue); ok {
			if err := atomic.ApplyClassificationApplication(ctx, item); err != nil {
				return err
			}
			continue
		}
		state := "skipped"
		if item.Automatic && item.Threshold >= .5 && item.Threshold <= 1 {
			principal := authorization.Principal{ID: item.RequestedBy, Scope: scope.Principal{MSPID: item.Target.MSPID, ClientID: item.Target.ClientID}, Capabilities: authorization.NewCapabilitySet("classification.apply")}
			object, getErr := w.associations.Get(ctx, GetCommand{Principal: principal, Target: item.Target})
			if getErr != nil {
				state = "failed"
			} else if object.ObjectVersion == item.ObjectVersion {
				direct := make([]string, 0, len(object.Direct))
				for _, assignment := range object.Direct {
					direct = append(direct, assignment.Tag.ID)
				}
				if tags, apply := BuildAutomaticReplacement(direct, item.Threshold, item.Items); apply {
					_, applyErr := w.associations.ReplaceDirect(ctx, ReplaceCommand{Principal: principal, Target: item.Target, ExpectedObjectVersion: object.ObjectVersion, TagIDs: tags, Source: SourceAIAutomatic, Reason: "High-confidence AI classification", IdempotencyKey: "classification-suggestion:" + item.ID, CorrelationID: w.newID()})
					if applyErr == nil {
						state = "applied"
					} else {
						state = "failed"
					}
				}
			}
		}
		if err := w.queue.FinishClassificationApplication(ctx, item, state); err != nil {
			return err
		}
	}
	return nil
}

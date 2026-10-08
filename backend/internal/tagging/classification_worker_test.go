package tagging

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestClassificationWorkerBuildsAdditiveAutomaticReplacement(t *testing.T) {
	result, ok := BuildAutomaticReplacement(
		[]string{"human", "already"},
		.95,
		[]ClassificationSuggestion{
			{TagID: "already", Confidence: 1, Active: true},
			{TagID: "suggested", Confidence: .95, Active: true},
			{TagID: "archived", Confidence: 1, Active: false},
		},
	)
	if !ok || len(result) != 3 || result[0] != "already" || result[1] != "human" || result[2] != "suggested" {
		t.Fatalf("replacement=%v applied=%t", result, ok)
	}
}

type applicationQueueStub struct {
	items     []ClassificationSuggestionRecord
	state     string
	finished  ClassificationSuggestionRecord
	claimErr  error
	finishErr error
}

func (s *applicationQueueStub) ClaimClassificationApplications(context.Context, int, time.Duration) ([]ClassificationSuggestionRecord, error) {
	return s.items, s.claimErr
}
func (s *applicationQueueStub) FinishClassificationApplication(_ context.Context, record ClassificationSuggestionRecord, state string) error {
	s.state = state
	s.finished = record
	return s.finishErr
}

type applicationAssociationStub struct {
	object     TaggedObject
	replace    ReplaceCommand
	getErr     error
	replaceErr error
}

func (s *applicationAssociationStub) Get(context.Context, TargetRef) (TaggedObject, error) {
	return s.object, s.getErr
}
func (s *applicationAssociationStub) ResolveTags(_ context.Context, _ string, ids []string) ([]Tag, error) {
	values := make([]Tag, 0, len(ids))
	for _, id := range ids {
		values = append(values, Tag{ID: id, State: StateActive})
	}
	return values, nil
}
func (s *applicationAssociationStub) Accepted(context.Context, TargetRef, string) (TaggedObject, bool, error) {
	return TaggedObject{}, false, nil
}
func (s *applicationAssociationStub) ReplaceDirect(_ context.Context, mutation AssociationMutation) (TaggedObject, error) {
	s.replace = ReplaceCommand{TagIDs: assignmentTagIDs(mutation.Direct), Source: mutation.Source}
	return s.object, s.replaceErr
}
func (s *applicationAssociationStub) History(context.Context, TargetRef) ([]HistoryEntry, error) {
	return nil, nil
}
func assignmentTagIDs(items []Assignment) []string {
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, item.Tag.ID)
	}
	return values
}

func TestClassificationApplicationWorkerPreservesExistingDirectTags(t *testing.T) {
	target := TargetRef{MSPID: "msp", ClientID: "client", ObjectType: ObjectTask, ObjectID: "task"}
	queue := &applicationQueueStub{items: []ClassificationSuggestionRecord{{ID: "suggestion", LeaseToken: "lease", Target: target, ObjectVersion: 1, RequestedBy: "tech", Automatic: true, Threshold: .95, Items: []ClassificationSuggestion{{TagID: "ai", Confidence: .99, Active: true}}}}}
	repository := &applicationAssociationStub{object: TaggedObject{Target: target, ObjectVersion: 1, Direct: []Assignment{{Tag: Tag{ID: "human", State: StateActive}, Source: SourceHuman}}}}
	worker := NewClassificationApplicationWorker(queue, NewAssociationServiceWithClock(repository, time.Now, func() string { return "id" }), time.Now, func() string { return "id" })
	if err := worker.RunOnce(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if queue.state != "applied" || queue.finished.LeaseToken != "lease" || len(repository.replace.TagIDs) != 2 || repository.replace.Source != SourceAIAutomatic {
		t.Fatalf("state=%s replace=%+v", queue.state, repository.replace)
	}
}

func TestClassificationApplicationWorkerTerminalizesDisabledBelowThresholdStaleAndFailure(t *testing.T) {
	target := TargetRef{MSPID: "msp", ClientID: "client", ObjectType: ObjectTask, ObjectID: "task"}
	for _, test := range []struct {
		name       string
		item       ClassificationSuggestionRecord
		object     TaggedObject
		replaceErr error
		want       string
	}{
		{name: "disabled", item: ClassificationSuggestionRecord{Automatic: false, Threshold: .95}, object: TaggedObject{ObjectVersion: 1}, want: "skipped"},
		{name: "below threshold", item: ClassificationSuggestionRecord{Automatic: true, Threshold: .95, Items: []ClassificationSuggestion{{TagID: "ai", Confidence: .949, Active: true}}}, object: TaggedObject{ObjectVersion: 1}, want: "skipped"},
		{name: "stale object", item: ClassificationSuggestionRecord{Automatic: true, Threshold: .95, Items: []ClassificationSuggestion{{TagID: "ai", Confidence: .99, Active: true}}}, object: TaggedObject{ObjectVersion: 2}, want: "skipped"},
		{name: "association failure", item: ClassificationSuggestionRecord{Automatic: true, Threshold: .95, Items: []ClassificationSuggestion{{TagID: "ai", Confidence: .99, Active: true}}}, object: TaggedObject{ObjectVersion: 1}, replaceErr: errors.New("write failed"), want: "failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.item.ID, test.item.LeaseToken, test.item.Target, test.item.ObjectVersion, test.item.RequestedBy = "suggestion", "lease", target, 1, "tech"
			queue := &applicationQueueStub{items: []ClassificationSuggestionRecord{test.item}}
			repository := &applicationAssociationStub{object: test.object, replaceErr: test.replaceErr}
			worker := NewClassificationApplicationWorker(queue, NewAssociationServiceWithClock(repository, time.Now, func() string { return "id" }), time.Now, func() string { return "id" })
			if err := worker.RunOnce(context.Background(), 1); err != nil {
				t.Fatal(err)
			}
			if queue.state != test.want {
				t.Fatalf("state=%s, want %s", queue.state, test.want)
			}
		})
	}
}

func TestClassificationApplicationWorkerPropagatesQueueFailures(t *testing.T) {
	want := errors.New("queue failed")
	worker := NewClassificationApplicationWorker(&applicationQueueStub{claimErr: want}, NewAssociationServiceWithClock(&applicationAssociationStub{}, time.Now, func() string { return "id" }), time.Now, func() string { return "id" })
	if err := worker.RunOnce(context.Background(), 1); !errors.Is(err, want) {
		t.Fatalf("error=%v, want %v", err, want)
	}
}

func TestClassificationWorkerDoesNotApplyWhenPolicyIsOffOrNoSuggestionQualifies(t *testing.T) {
	if result, ok := BuildAutomaticReplacement(nil, 0, []ClassificationSuggestion{{TagID: "tag", Confidence: 1, Active: true}}); ok || len(result) != 0 {
		t.Fatalf("off policy result=%v applied=%t", result, ok)
	}
}

package tagging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestAssociationGetSerializesEmptyAssignmentCollectionsAsArrays(t *testing.T) {
	result, err := NewAssociationService(&associationRepositoryStub{loaded: TaggedObject{Target: associationTarget(), ObjectVersion: 1}}).Get(context.Background(), GetCommand{Principal: associationPrincipal("client-id", "classification.apply"), Target: associationTarget()})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"direct":[]`, `"inherited":[]`, `"effective":[]`} {
		if !bytes.Contains(payload, []byte(field)) {
			t.Fatalf("empty collection %s encoded as null: %s", field, payload)
		}
	}
}

type associationRepositoryStub struct {
	loaded   TaggedObject
	resolved []Tag
	existing TaggedObject
	found    bool
	accepted *AssociationMutation
	history  []HistoryEntry
}

func (r *associationRepositoryStub) Get(
	context.Context,
	TargetRef,
) (TaggedObject, error) {
	return r.loaded, nil
}

func (r *associationRepositoryStub) ResolveTags(
	context.Context,
	string,
	[]string,
) ([]Tag, error) {
	return r.resolved, nil
}

func (r *associationRepositoryStub) Accepted(
	context.Context,
	TargetRef,
	string,
) (TaggedObject, bool, error) {
	return r.existing, r.found, nil
}

func (r *associationRepositoryStub) ReplaceDirect(
	_ context.Context,
	accepted AssociationMutation,
) (TaggedObject, error) {
	r.accepted = &accepted
	result := accepted.Before
	result.ObjectVersion++
	result.Direct = append([]Assignment(nil), accepted.Direct...)
	result.Effective = effectiveAssignments(result.Direct, result.Inherited)
	result.ClassificationState = classificationState(result.Effective)
	return result, nil
}

func (r *associationRepositoryStub) History(
	context.Context,
	TargetRef,
) ([]HistoryEntry, error) {
	return r.history, nil
}

func associationPrincipal(
	clientID string,
	capabilities ...string,
) authorization.Principal {
	return authorization.Principal{
		ID: "technician-id",
		Scope: scope.Principal{
			MSPID: "msp-id", ClientID: clientID,
		},
		Capabilities: authorization.NewCapabilitySet(capabilities...),
	}
}

func associationTarget() TargetRef {
	return TargetRef{
		ObjectType: ObjectWorkRecord, ObjectID: "work-id",
		MSPID: "msp-id", ClientID: "client-id",
	}
}

func TestAssociationRejectsCrossClientAndRequiresApply(t *testing.T) {
	for _, test := range []struct {
		name      string
		principal authorization.Principal
		want      error
	}{
		{
			name: "cross client",
			principal: associationPrincipal(
				"other-client", "classification.apply",
			),
			want: scope.ErrNotFound,
		},
		{
			name:      "missing capability",
			principal: associationPrincipal("client-id"),
			want:      authorization.ErrForbidden,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &associationRepositoryStub{}
			_, err := NewAssociationService(repository).Get(
				context.Background(),
				GetCommand{
					Principal: test.principal,
					Target:    associationTarget(),
				},
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("Get() error=%v, want %v", err, test.want)
			}
		})
	}
}

func TestAssociationResolvesMergedTagsAndRemovesUnclassified(t *testing.T) {
	repository := &associationRepositoryStub{
		loaded: TaggedObject{
			Target: associationTarget(), ObjectVersion: 4,
			Direct: []Assignment{{
				ID: "fallback-assignment",
				Tag: Tag{
					ID: "unclassified", InternalKey: "taxonomy.system.unclassified",
					State: StateActive, SystemManaged: true,
				},
				Source: SourceSystemFallback,
			}},
		},
		resolved: []Tag{{
			ID: "survivor", Label: "VPN", State: StateActive,
		}},
	}
	result, err := NewAssociationService(repository).ReplaceDirect(
		context.Background(),
		ReplaceCommand{
			Principal: associationPrincipal(
				"client-id", "classification.apply",
			),
			Target: associationTarget(), ExpectedObjectVersion: 4,
			TagIDs: []string{"merged-tag"}, Source: SourceHuman,
			Reason: "Classify work", IdempotencyKey: "replace-1",
			CorrelationID: "correlation-id",
		},
	)
	if err != nil {
		t.Fatalf("ReplaceDirect() error=%v", err)
	}
	if len(result.Direct) != 1 || result.Direct[0].Tag.ID != "survivor" {
		t.Fatalf("ReplaceDirect() direct=%+v", result.Direct)
	}
	if repository.accepted == nil ||
		repository.accepted.Direct[0].Tag.ID != "survivor" {
		t.Fatalf("accepted mutation=%+v", repository.accepted)
	}
}

func TestAssociationRejectsArchivedAndEmptyMeaningfulClassification(t *testing.T) {
	t.Run("archived requested tag", func(t *testing.T) {
		repository := &associationRepositoryStub{
			loaded: TaggedObject{
				Target: associationTarget(), ObjectVersion: 1,
			},
			resolved: []Tag{{
				ID: "archived", State: StateArchived,
			}},
		}
		_, err := NewAssociationService(repository).ReplaceDirect(
			context.Background(),
			ReplaceCommand{
				Principal: associationPrincipal(
					"client-id", "classification.apply",
				),
				Target: associationTarget(), ExpectedObjectVersion: 1,
				TagIDs: []string{"archived"}, Source: SourceHuman,
				Reason: "Classify", IdempotencyKey: "replace-2",
				CorrelationID: "correlation-id",
			},
		)
		if !errors.Is(err, ErrInactiveTarget) {
			t.Fatalf("ReplaceDirect() error=%v, want inactive", err)
		}
	})

	t.Run("empty without inherited tags", func(t *testing.T) {
		repository := &associationRepositoryStub{
			loaded: TaggedObject{
				Target: associationTarget(), ObjectVersion: 1,
			},
		}
		_, err := NewAssociationService(repository).ReplaceDirect(
			context.Background(),
			ReplaceCommand{
				Principal: associationPrincipal(
					"client-id", "classification.apply",
				),
				Target: associationTarget(), ExpectedObjectVersion: 1,
				Source: SourceHuman, Reason: "Clear",
				IdempotencyKey: "replace-3",
				CorrelationID:  "correlation-id",
			},
		)
		if !errors.Is(err, ErrMeaningfulTagRequired) {
			t.Fatalf("ReplaceDirect() error=%v, want meaningful tag", err)
		}
	})
}

func TestAssociationProjectInheritanceSatisfiesTaskClassification(t *testing.T) {
	target := associationTarget()
	target.ObjectType = ObjectTask
	repository := &associationRepositoryStub{
		loaded: TaggedObject{
			Target: target, ObjectVersion: 2,
			Inherited: []Assignment{{
				ID: "project-assignment",
				Tag: Tag{
					ID: "project-tag", Label: "Cisco", State: StateActive,
				},
				Source: SourceHuman, Inherited: true,
				SourceObjectType: ObjectProject,
				SourceObjectID:   "project-id",
			}},
		},
	}
	result, err := NewAssociationService(repository).ReplaceDirect(
		context.Background(),
		ReplaceCommand{
			Principal: associationPrincipal(
				"client-id", "classification.apply",
			),
			Target: target, ExpectedObjectVersion: 2,
			Source: SourceHuman, Reason: "Use project classification",
			IdempotencyKey: "replace-4", CorrelationID: "correlation-id",
		},
	)
	if err != nil {
		t.Fatalf("ReplaceDirect() error=%v", err)
	}
	if len(result.Direct) != 0 || len(result.Inherited) != 1 ||
		result.ClassificationState != "classified" {
		t.Fatalf("ReplaceDirect()=%+v", result)
	}
}

func TestAssociationRepeatedIdempotencyKeyReturnsAcceptedResult(t *testing.T) {
	expected := TaggedObject{
		Target: associationTarget(), ObjectVersion: 8,
		ClassificationState: "classified",
	}
	repository := &associationRepositoryStub{
		existing: expected,
		found:    true,
	}
	result, err := NewAssociationService(repository).ReplaceDirect(
		context.Background(),
		ReplaceCommand{
			Principal: associationPrincipal(
				"client-id", "classification.apply",
			),
			Target: associationTarget(), ExpectedObjectVersion: 7,
			TagIDs: []string{"tag-id"}, Source: SourceHuman,
			Reason: "Classify", IdempotencyKey: "repeat",
			CorrelationID: "correlation-id",
		},
	)
	if err != nil || result.ObjectVersion != expected.ObjectVersion {
		t.Fatalf("ReplaceDirect()=%+v error=%v", result, err)
	}
	if repository.accepted != nil {
		t.Fatal("idempotent replay wrote another mutation")
	}
}

func TestAssociationAutomaticSourceCannotRemoveExistingDirectTag(t *testing.T) {
	repository := &associationRepositoryStub{
		loaded: TaggedObject{Target: associationTarget(), ObjectVersion: 3,
			Direct: []Assignment{{Tag: Tag{ID: "human-tag", State: StateActive}}}},
		resolved: []Tag{{ID: "ai-tag", State: StateActive}},
	}
	_, err := NewAssociationService(repository).ReplaceDirect(
		context.Background(), ReplaceCommand{
			Principal: associationPrincipal("client-id", "classification.apply"),
			Target:    associationTarget(), ExpectedObjectVersion: 3,
			TagIDs: []string{"ai-tag"}, Source: SourceAIAutomatic,
			Reason: "Automatic classification", IdempotencyKey: "ai-1",
			CorrelationID: "correlation-id",
		},
	)
	if !errors.Is(err, ErrAutomaticReplacement) {
		t.Fatalf("ReplaceDirect() error=%v, want automatic replacement error", err)
	}
}

package tagging

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInitialAssignmentsRetainAcceptedMutationProvenance(t *testing.T) {
	at := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	initial := InitialAssignmentSet{Direct: []Assignment{{
		Tag: Tag{ID: "meaningful", State: StateActive}, Source: SourceHuman,
	}}}.WithProvenance(InitialAssignmentProvenance{
		ActorType: "technician", ActorID: "technician-id", OccurredAt: at,
		CorrelationID: "correlation-id", Evidence: map[string]any{"request": "create"},
	})
	if initial.ActorType != "technician" || initial.ActorID != "technician-id" ||
		!initial.OccurredAt.Equal(at) || initial.CorrelationID != "correlation-id" ||
		initial.Evidence["request"] != "create" {
		t.Fatalf("initial provenance = %+v", initial)
	}
}

type creationRepositoryStub struct {
	resolved     []Tag
	unclassified Tag
	parent       TaggedObject
}

func (r creationRepositoryStub) ResolveTags(
	context.Context,
	string,
	[]string,
) ([]Tag, error) {
	return r.resolved, nil
}

func (r creationRepositoryStub) FindUnclassified(
	context.Context,
	string,
) (Tag, error) {
	return r.unclassified, nil
}

func (r creationRepositoryStub) Get(
	context.Context,
	TargetRef,
) (TaggedObject, error) {
	return r.parent, nil
}

func (r creationRepositoryStub) ResolveTaskProject(_ context.Context, mspID, clientID, parentType, parentID string) (TargetRef, error) {
	if parentType != "project" && parentType != "phase" {
		return TargetRef{}, ErrInvalidAssociation
	}
	return TargetRef{MSPID: mspID, ClientID: clientID, ObjectType: ObjectProject, ObjectID: parentID}, nil
}

func TestCreationDerivesProjectClassificationFromParentReference(t *testing.T) {
	result, err := NewCreationPreparer(creationRepositoryStub{
		parent: TaggedObject{Effective: []Assignment{{
			Tag: Tag{ID: "project-tag", State: StateActive},
		}}},
	}).Prepare(context.Background(), PrepareCreationCommand{
		MSPID: "msp-id", ClientID: "client-id", ObjectType: ObjectTask,
		Policy: CreationRequireMeaningful, Source: SourceHuman,
		ParentType: "project", ParentID: "project-id",
	})
	if err != nil || len(result.Direct) != 0 || len(result.Inherited) != 1 ||
		result.Inherited[0].SourceObjectID != "project-id" {
		t.Fatalf("Prepare() result=%+v error=%v", result, err)
	}
}

func TestCreationRequiresMeaningfulInteractiveClassification(t *testing.T) {
	_, err := NewCreationPreparer(creationRepositoryStub{}).Prepare(
		context.Background(),
		PrepareCreationCommand{
			MSPID: "msp-id", ClientID: "client-id",
			ObjectType: ObjectWorkRecord,
			Policy:     CreationRequireMeaningful,
			Source:     SourceHuman,
		},
	)
	if !errors.Is(err, ErrMeaningfulTagRequired) {
		t.Fatalf("Prepare() error=%v, want meaningful tag", err)
	}
}

func TestCreationAllowsSystemFallbackForTrustedAutomation(t *testing.T) {
	fallback := Tag{
		ID: "unclassified", MSPID: "msp-id",
		InternalKey: "taxonomy.system.unclassified",
		State:       StateActive, SystemManaged: true,
	}
	result, err := NewCreationPreparer(creationRepositoryStub{
		unclassified: fallback,
	}).Prepare(
		context.Background(),
		PrepareCreationCommand{
			MSPID: "msp-id", ClientID: "client-id",
			ObjectType: ObjectAsset,
			Policy:     CreationAllowFallback,
			Source:     SourceIntegration,
		},
	)
	if err != nil {
		t.Fatalf("Prepare() error=%v", err)
	}
	if len(result.Direct) != 1 ||
		result.Direct[0].Tag.ID != fallback.ID ||
		result.Direct[0].Source != SourceSystemFallback {
		t.Fatalf("Prepare()=%+v", result)
	}
}

func TestCreationRejectsHumanFallbackPolicy(t *testing.T) {
	_, err := NewCreationPreparer(creationRepositoryStub{}).Prepare(
		context.Background(),
		PrepareCreationCommand{
			MSPID: "msp-id", ClientID: "client-id", ObjectType: ObjectAsset,
			Policy: CreationAllowFallback, Source: SourceHuman,
		},
	)
	if !errors.Is(err, ErrInvalidAssociation) {
		t.Fatalf("Prepare() error=%v, want invalid association", err)
	}
}

func TestCreationDoesNotTrustCallerSuppliedInheritance(t *testing.T) {
	_, err := NewCreationPreparer(creationRepositoryStub{}).Prepare(
		context.Background(),
		PrepareCreationCommand{
			MSPID: "msp-id", ClientID: "client-id", ObjectType: ObjectWorkRecord,
			Policy: CreationRequireMeaningful, Source: SourceHuman,
			Inherited: []Assignment{{Tag: Tag{ID: "forged", State: StateActive}}},
		},
	)
	if !errors.Is(err, ErrMeaningfulTagRequired) {
		t.Fatalf("Prepare() error=%v, want meaningful tag required", err)
	}
}

func TestCreationAcceptsProjectInheritanceWithoutCopyingTaskTags(t *testing.T) {
	result, err := NewCreationPreparer(creationRepositoryStub{
		parent: TaggedObject{Effective: []Assignment{{
			Tag: Tag{ID: "project-tag", InternalKey: "taxonomy.custom.cisco", State: StateActive},
		}}},
	}).Prepare(
		context.Background(),
		PrepareCreationCommand{
			MSPID: "msp-id", ClientID: "client-id",
			ObjectType: ObjectTask,
			Policy:     CreationRequireMeaningful,
			Source:     SourceHuman,
			ParentType: "project", ParentID: "project-id",
		},
	)
	if err != nil {
		t.Fatalf("Prepare() error=%v", err)
	}
	if len(result.Direct) != 0 || len(result.Effective) != 1 {
		t.Fatalf("Prepare()=%+v", result)
	}
}

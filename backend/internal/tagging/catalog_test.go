package tagging

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type catalogRepositoryStub struct {
	catalog      Catalog
	tags         map[string]Tag
	groups       map[string]Group
	unclassified Tag
	impact       Impact
	termErr      error
	mutations    []CatalogMutation
	merge        *MergeMutation
	archive      *ArchiveMutation
}

func (r *catalogRepositoryStub) EnsureSystemCatalog(
	_ context.Context,
	accepted SystemCatalogMutation,
) (Tag, error) {
	r.mutations = append(r.mutations, accepted.CatalogMutation)
	r.unclassified = accepted.Tag
	return accepted.Tag, nil
}

func (r *catalogRepositoryStub) List(
	context.Context,
	string,
) (Catalog, error) {
	return r.catalog, nil
}

func (r *catalogRepositoryStub) Health(
	context.Context,
	scope.Target,
) (Health, error) {
	return Health{}, nil
}

func (r *catalogRepositoryStub) MigrationHistory(
	context.Context,
	string,
) ([]MigrationRun, error) {
	return nil, nil
}

func (r *catalogRepositoryStub) FindGroup(
	_ context.Context,
	_ string,
	id string,
) (Group, error) {
	group, ok := r.groups[id]
	if !ok {
		return Group{}, scope.ErrNotFound
	}
	return group, nil
}

func (r *catalogRepositoryStub) FindTag(
	_ context.Context,
	_ string,
	id string,
) (Tag, error) {
	tag, ok := r.tags[id]
	if !ok {
		return Tag{}, scope.ErrNotFound
	}
	return tag, nil
}

func (r *catalogRepositoryStub) FindUnclassified(
	context.Context,
	string,
) (Tag, error) {
	if r.unclassified.ID == "" {
		return Tag{}, scope.ErrNotFound
	}
	return r.unclassified, nil
}

func (r *catalogRepositoryStub) TermsAvailable(
	context.Context,
	string,
	string,
	[]string,
) error {
	return r.termErr
}

func (r *catalogRepositoryStub) CreateGroup(
	_ context.Context,
	accepted CatalogMutation,
) error {
	r.mutations = append(r.mutations, accepted)
	return nil
}

func (r *catalogRepositoryStub) UpdateGroup(
	_ context.Context,
	accepted CatalogMutation,
) error {
	r.mutations = append(r.mutations, accepted)
	return nil
}

func (r *catalogRepositoryStub) CreateTag(
	_ context.Context,
	accepted CatalogMutation,
) error {
	r.mutations = append(r.mutations, accepted)
	return nil
}

func (r *catalogRepositoryStub) UpdateTag(
	_ context.Context,
	accepted CatalogMutation,
) error {
	r.mutations = append(r.mutations, accepted)
	return nil
}

func (r *catalogRepositoryStub) PreviewImpact(
	context.Context,
	string,
	string,
	ImpactOperation,
	string,
) (Impact, error) {
	return r.impact, nil
}

func (r *catalogRepositoryStub) Merge(
	_ context.Context,
	accepted MergeMutation,
) error {
	r.merge = &accepted
	return nil
}

func (r *catalogRepositoryStub) Archive(
	_ context.Context,
	accepted ArchiveMutation,
) error {
	r.archive = &accepted
	return nil
}

func catalogPrincipal(capabilities ...string) authorization.Principal {
	return authorization.Principal{
		ID: "technician-id",
		Scope: scope.Principal{
			MSPID: "msp-id",
		},
		Capabilities: authorization.NewCapabilitySet(capabilities...),
	}
}

func catalogService(repository CatalogRepository) *CatalogService {
	next := 0
	return NewCatalogService(
		repository,
		func() time.Time {
			return time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
		},
		func() string {
			next++
			return "generated-" + string(rune('0'+next))
		},
	)
}

func TestCatalogRejectsDuplicateNormalizedLabelAndSynonym(t *testing.T) {
	t.Run("inside the proposed tag", func(t *testing.T) {
		repository := &catalogRepositoryStub{
			groups: map[string]Group{
				"group-id": {ID: "group-id", MSPID: "msp-id", State: StateActive},
			},
		}
		_, err := catalogService(repository).CreateTag(
			context.Background(),
			CreateTagCommand{
				Principal: catalogPrincipal("classification.manage"),
				GroupID:   "group-id",
				Label:     "VPN",
				Synonyms:  []string{" vpn "},
			},
		)
		if !errors.Is(err, ErrDuplicateTerm) {
			t.Fatalf("CreateTag() error=%v, want ErrDuplicateTerm", err)
		}
	})

	t.Run("against the global catalog", func(t *testing.T) {
		repository := &catalogRepositoryStub{
			groups: map[string]Group{
				"group-id": {ID: "group-id", MSPID: "msp-id", State: StateActive},
			},
			termErr: ErrDuplicateTerm,
		}
		_, err := catalogService(repository).CreateTag(
			context.Background(),
			CreateTagCommand{
				Principal: catalogPrincipal("classification.manage"),
				GroupID:   "group-id",
				Label:     "Microsoft 365",
				Synonyms:  []string{"M365"},
			},
		)
		if !errors.Is(err, ErrDuplicateTerm) {
			t.Fatalf("CreateTag() error=%v, want ErrDuplicateTerm", err)
		}
	})
}

func TestCatalogRenamePreservesIDAndInternalKey(t *testing.T) {
	repository := &catalogRepositoryStub{
		tags: map[string]Tag{
			"tag-id": {
				ID: "tag-id", MSPID: "msp-id", GroupID: "group-id",
				InternalKey: "taxonomy.custom.tag-id", Label: "M365",
				State: StateActive, Version: 4,
			},
		},
		groups: map[string]Group{
			"group-id": {ID: "group-id", MSPID: "msp-id", State: StateActive},
		},
	}
	updated, err := catalogService(repository).UpdateTag(
		context.Background(),
		UpdateTagCommand{
			Principal: catalogPrincipal("classification.manage"),
			ID:        "tag-id", GroupID: "group-id", Label: "Microsoft 365",
			ExpectedVersion: 4,
		},
	)
	if err != nil {
		t.Fatalf("UpdateTag() error=%v", err)
	}
	if updated.ID != "tag-id" ||
		updated.InternalKey != "taxonomy.custom.tag-id" ||
		updated.Version != 5 {
		t.Fatalf("UpdateTag()=%+v, identity or version changed incorrectly", updated)
	}
	accepted := repository.mutations[0]
	if accepted.Audit.Action != "classification.tag.renamed" ||
		accepted.Event.EventType != "classification.tag.renamed" {
		t.Fatalf("rename facts=%+v %+v", accepted.Audit, accepted.Event)
	}
}

func TestCatalogMergeRejectsSystemTagAndUsesActiveSurvivor(t *testing.T) {
	t.Run("system tag", func(t *testing.T) {
		repository := &catalogRepositoryStub{
			tags: map[string]Tag{
				"system": {
					ID: "system", MSPID: "msp-id", State: StateActive,
					SystemManaged: true, Version: 1,
				},
				"survivor": {
					ID: "survivor", MSPID: "msp-id", State: StateActive,
					Version: 1,
				},
			},
		}
		_, err := catalogService(repository).Merge(
			context.Background(),
			MergeCommand{
				Principal: catalogPrincipal("classification.manage"),
				TagID:     "system", SurvivorTagID: "survivor",
				ExpectedVersion: 1, Reason: "Duplicate",
			},
		)
		if !errors.Is(err, ErrSystemManaged) {
			t.Fatalf("Merge() error=%v, want ErrSystemManaged", err)
		}
	})

	t.Run("active survivor", func(t *testing.T) {
		repository := &catalogRepositoryStub{
			tags: map[string]Tag{
				"retired": {
					ID: "retired", MSPID: "msp-id", State: StateActive,
					Version: 2,
				},
				"survivor": {
					ID: "survivor", MSPID: "msp-id", State: StateActive,
					Version: 7,
				},
			},
			impact: Impact{AffectedObjects: 12},
		}
		merged, err := catalogService(repository).Merge(
			context.Background(),
			MergeCommand{
				Principal: catalogPrincipal("classification.manage"),
				TagID:     "retired", SurvivorTagID: "survivor",
				ExpectedVersion: 2, Reason: "Same classification",
			},
		)
		if err != nil {
			t.Fatalf("Merge() error=%v", err)
		}
		if merged.State != StateMerged ||
			merged.MergedIntoTagID != "survivor" ||
			repository.merge == nil ||
			repository.merge.Survivor.State != StateActive {
			t.Fatalf("merged=%+v mutation=%+v", merged, repository.merge)
		}
		if repository.merge.Audit.Action != "classification.tag.merged" {
			t.Fatalf("merge audit=%+v", repository.merge.Audit)
		}
	})
}

func TestCatalogArchiveAppliesUnclassifiedToImpactedObjects(t *testing.T) {
	repository := &catalogRepositoryStub{
		tags: map[string]Tag{
			"tag-id": {
				ID: "tag-id", MSPID: "msp-id", State: StateActive,
				Version: 3,
			},
		},
		unclassified: Tag{
			ID: "unclassified-id", MSPID: "msp-id", State: StateActive,
			SystemManaged: true,
		},
		impact: Impact{
			AffectedObjects: 5,
			FallbackByObjectType: map[ObjectType]int64{
				ObjectWorkRecord: 3,
				ObjectAsset:      2,
			},
		},
	}
	archived, err := catalogService(repository).Archive(
		context.Background(),
		ArchiveCommand{
			Principal: catalogPrincipal("classification.manage"),
			TagID:     "tag-id", ExpectedVersion: 3,
			Reason: "Retired technology",
		},
	)
	if err != nil {
		t.Fatalf("Archive() error=%v", err)
	}
	if archived.State != StateArchived ||
		repository.archive == nil ||
		repository.archive.UnclassifiedTag.ID != "unclassified-id" ||
		repository.archive.Impact.AffectedObjects != 5 {
		t.Fatalf("archived=%+v mutation=%+v", archived, repository.archive)
	}
	if repository.archive.Audit.Action != "classification.tag.archived" {
		t.Fatalf("archive audit=%+v", repository.archive.Audit)
	}
}

func TestCatalogArchiveRejectsTheRetiringTagAsItsOwnReplacement(t *testing.T) {
	repository := &catalogRepositoryStub{
		tags: map[string]Tag{
			"tag-id": {
				ID: "tag-id", MSPID: "msp-id", State: StateActive,
				Version: 3,
			},
		},
		unclassified: Tag{
			ID: "unclassified-id", MSPID: "msp-id", State: StateActive,
			SystemManaged: true,
		},
	}
	_, err := catalogService(repository).Archive(
		context.Background(),
		ArchiveCommand{
			Principal: catalogPrincipal("classification.manage"),
			TagID:     "tag-id", ReplacementTagID: "tag-id",
			ExpectedVersion: 3, Reason: "Retired technology",
		},
	)
	if !errors.Is(err, ErrInvalidCatalog) {
		t.Fatalf("Archive() error=%v, want invalid catalog", err)
	}
	if repository.archive != nil {
		t.Fatal("self-replacement reached archive repository")
	}
}

func TestCatalogRequiresClassificationManage(t *testing.T) {
	repository := &catalogRepositoryStub{}
	_, err := catalogService(repository).CreateGroup(
		context.Background(),
		CreateGroupCommand{
			Principal: catalogPrincipal("classification.apply"),
			Label:     "Technology",
			Position:  1,
		},
	)
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("CreateGroup() error=%v, want forbidden", err)
	}
	if len(repository.mutations) != 0 {
		t.Fatal("unauthorized catalog mutation reached repository")
	}
}

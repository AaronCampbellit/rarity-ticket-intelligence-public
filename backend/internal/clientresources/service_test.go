package clientresources

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

const (
	preparedResourceID    = "019fb3c2-0000-7000-8000-000000000301"
	preparedCorrelationID = "019fb3c2-0000-7000-8000-000000000302"
)

type captureRepository struct {
	mutations []CreateMutation
}

type assetClassificationRepository struct{}

func (assetClassificationRepository) ResolveTags(_ context.Context, mspID string, ids []string) ([]tagging.Tag, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return []tagging.Tag{{ID: ids[0], MSPID: mspID, InternalKey: "taxonomy.support", State: tagging.StateActive}}, nil
}
func (assetClassificationRepository) FindUnclassified(_ context.Context, msp string) (tagging.Tag, error) {
	return tagging.Tag{ID: "unclassified", MSPID: msp, InternalKey: "unclassified", State: tagging.StateActive}, nil
}
func TestInitialTagsClassificationPolicyForAsset(t *testing.T) {
	s := &Service{creation: tagging.NewCreationPreparer(assetClassificationRepository{})}
	envelope := object.Envelope{MSPID: "msp", ClientID: "client"}
	if _, err := s.initialAssetTags(context.Background(), envelope, CreateAssetCommand{Source: "api", ClassificationPolicy: tagging.CreationRequireMeaningful}); !errors.Is(err, tagging.ErrMeaningfulTagRequired) {
		t.Fatalf("interactive error=%v", err)
	}
	_, err := s.initialAssetTags(context.Background(), envelope, CreateAssetCommand{Source: "integration", ClassificationPolicy: tagging.CreationAllowFallback})
	if !errors.Is(err, tagging.ErrInvalidAssociation) {
		t.Fatalf("untrusted fallback error=%v", err)
	}
}

func (r *captureRepository) CreateAtomic(_ context.Context, mutation CreateMutation) error {
	r.mutations = append(r.mutations, mutation)
	return nil
}

func TestCreateLocationAndContactPreserveExplicitClientScope(t *testing.T) {
	repository := &captureRepository{}
	service := testService(repository)
	principal := resourcePrincipal("location.create", "contact.create")

	location, err := service.CreateLocation(context.Background(), CreateLocationCommand{
		Principal: principal, DisplayID: "LOC-1", Name: "Headquarters",
		ActorID: "actor-id", Source: "web",
	})
	if err != nil {
		t.Fatalf("CreateLocation() error = %v", err)
	}
	contact, err := service.CreateContact(context.Background(), CreateContactCommand{
		Principal: principal, Location: Ref{
			ID: location.ID, MSPID: location.MSPID, ClientID: location.ClientID,
		},
		DisplayID: "CONTACT-1", DisplayName: "Alex Client",
		Email: "alex@example.com", Phone: "555-0100", ActorID: "actor-id", Source: "web",
	})
	if err != nil {
		t.Fatalf("CreateContact() error = %v", err)
	}
	if contact.ClientID != "client-id" || contact.LocationID != location.ID || contact.Phone != "555-0100" {
		t.Fatalf("unexpected scoped contact: %+v", contact)
	}
	if len(repository.mutations) != 2 ||
		repository.mutations[1].Event.EventType != "contact.created" {
		t.Fatalf("unexpected atomic mutations: %+v", repository.mutations)
	}
}

func TestCreateContactRejectsCrossClientLocation(t *testing.T) {
	repository := &captureRepository{}
	service := testService(repository)
	_, err := service.CreateContact(context.Background(), CreateContactCommand{
		Principal: resourcePrincipal("contact.create"),
		Location:  Ref{ID: "location-id", MSPID: "msp-id", ClientID: "client-bravo"},
		DisplayID: "CONTACT-2", DisplayName: "Wrong Client",
		ActorID: "actor-id", Source: "api",
	})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("CreateContact() error = %v, want ErrNotFound", err)
	}
	if len(repository.mutations) != 0 {
		t.Fatal("cross-client contact reached repository")
	}
}

func TestCreateAssetRecordsSourceProvenanceWithoutOverwritingAuthority(t *testing.T) {
	repository := &captureRepository{}
	service := testService(repository)
	asset, err := service.CreateAsset(context.Background(), CreateAssetCommand{
		Principal: resourcePrincipal("asset.create"),
		DisplayID: "ASSET-1", Name: "MAIL01", AssetType: "server",
		SourceSystem: "datto", ExternalID: "device-100",
		Authority: Discovered, ActorID: "integration-id", Source: "integration",
		TagIDs: []string{"tag-id"}, ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if err != nil {
		t.Fatalf("CreateAsset() error = %v", err)
	}
	if asset.Provenance.SourceSystem != "datto" ||
		asset.Provenance.Authority != Discovered {
		t.Fatalf("asset provenance lost: %+v", asset)
	}
}

func TestCreateAssetRejectsInteractiveMissingTagsBeforePersistence(t *testing.T) {
	repository := &captureRepository{}
	service := testService(repository)
	_, err := service.CreateAsset(context.Background(), CreateAssetCommand{
		Principal: resourcePrincipal("asset.create"), DisplayID: "ASSET-1", Name: "MAIL01", AssetType: "server", Authority: TechnicianConfirmed, ActorID: "actor-id", Source: "api", ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if !errors.Is(err, tagging.ErrMeaningfulTagRequired) || len(repository.mutations) != 0 {
		t.Fatalf("CreateAsset() error=%v mutations=%d", err, len(repository.mutations))
	}
}

func TestCreateAssetAndServiceValidateClosedBusinessValues(t *testing.T) {
	service := testService(&captureRepository{})
	principal := resourcePrincipal("asset.create", "service.create")
	for _, command := range []CreateAssetCommand{
		{Principal: principal, DisplayID: "ASSET-ONLY-SOURCE", Name: "Router", AssetType: "router", SourceSystem: "rmm", Authority: TechnicianConfirmed, ActorID: "actor-id", Source: "api"},
		{Principal: principal, DisplayID: "ASSET-ONLY-EXTERNAL", Name: "Router", AssetType: "router", ExternalID: "device-1", Authority: TechnicianConfirmed, ActorID: "actor-id", Source: "api"},
	} {
		if _, err := service.CreateAsset(context.Background(), command); !errors.Is(err, ErrInvalid) {
			t.Fatalf("CreateAsset(%+v) error=%v, want ErrInvalid", command, err)
		}
	}
	for _, criticality := range []string{"urgent", "highest"} {
		if _, err := service.CreateService(context.Background(), CreateServiceCommand{
			Principal: principal, DisplayID: "SERVICE-" + criticality, Name: "Managed Service", Criticality: criticality,
			ActorID: "actor-id", Source: "api",
		}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("CreateService(%q) error=%v, want ErrInvalid", criticality, err)
		}
	}
}

func TestCreateContractValidatesEffectiveWindow(t *testing.T) {
	repository := &captureRepository{}
	service := testService(repository)
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(-24 * time.Hour)
	_, err := service.CreateContract(context.Background(), CreateContractCommand{
		Principal: resourcePrincipal("contract.create"),
		DisplayID: "CONTRACT-1", Name: "Managed Services",
		StartsOn: start, EndsOn: &end, ActorID: "actor-id", Source: "web",
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("CreateContract() error = %v, want ErrInvalid", err)
	}
}

func TestCreateServiceUsesClientResourceContract(t *testing.T) {
	repository := &captureRepository{}
	service := testService(repository)
	created, err := service.CreateService(context.Background(), CreateServiceCommand{
		Principal: resourcePrincipal("service.create"),
		DisplayID: "SERVICE-1", Name: "Email", Criticality: "high",
		ActorID: "actor-id", Source: "web",
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	if created.ClientID != "client-id" || created.Criticality != "high" {
		t.Fatalf("unexpected service: %+v", created)
	}
}

func TestCreateResourcesUsePreparedResourceAndCorrelationIdentities(t *testing.T) {
	tests := []struct {
		name string
		call func(*Service) error
	}{
		{
			name: "location",
			call: func(service *Service) error {
				_, err := service.CreateLocation(context.Background(), CreateLocationCommand{
					Principal: resourcePrincipal("location.create"), DisplayID: "LOC-PREPARED", Name: "Prepared Location",
					ActorID: "actor-id", Source: "ai", Prepared: PreparedIdentity{ResourceID: preparedResourceID, CorrelationID: preparedCorrelationID},
				})
				return err
			},
		},
		{
			name: "contact",
			call: func(service *Service) error {
				_, err := service.CreateContact(context.Background(), CreateContactCommand{
					Principal: resourcePrincipal("contact.create"), DisplayID: "CONTACT-PREPARED", DisplayName: "Prepared Contact",
					ActorID: "actor-id", Source: "ai", Prepared: PreparedIdentity{ResourceID: preparedResourceID, CorrelationID: preparedCorrelationID},
				})
				return err
			},
		},
		{
			name: "asset",
			call: func(service *Service) error {
				_, err := service.CreateAsset(context.Background(), CreateAssetCommand{
					Principal: resourcePrincipal("asset.create"), DisplayID: "ASSET-PREPARED", Name: "Prepared Asset", AssetType: "server", Authority: Discovered,
					ActorID: "actor-id", Source: "ai", Prepared: PreparedIdentity{ResourceID: preparedResourceID, CorrelationID: preparedCorrelationID},
				})
				return err
			},
		},
		{
			name: "service",
			call: func(service *Service) error {
				_, err := service.CreateService(context.Background(), CreateServiceCommand{
					Principal: resourcePrincipal("service.create"), DisplayID: "SERVICE-PREPARED", Name: "Prepared Service",
					ActorID: "actor-id", Source: "ai", Prepared: PreparedIdentity{ResourceID: preparedResourceID, CorrelationID: preparedCorrelationID},
				})
				return err
			},
		},
		{
			name: "contract",
			call: func(service *Service) error {
				_, err := service.CreateContract(context.Background(), CreateContractCommand{
					Principal: resourcePrincipal("contract.create"), DisplayID: "CONTRACT-PREPARED", Name: "Prepared Contract", StartsOn: time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC),
					ActorID: "actor-id", Source: "ai", Prepared: PreparedIdentity{ResourceID: preparedResourceID, CorrelationID: preparedCorrelationID},
				})
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &captureRepository{}
			if err := test.call(testService(repository)); err != nil {
				t.Fatalf("create error = %v", err)
			}
			mutation := repository.mutations[0]
			if mutation.Object.ID != preparedResourceID ||
				mutation.Audit.SubjectID != preparedResourceID ||
				mutation.Event.SubjectID != preparedResourceID ||
				mutation.Audit.CorrelationID != preparedCorrelationID ||
				mutation.Event.CorrelationID != preparedCorrelationID {
				t.Fatalf("prepared identity was not used throughout atomic mutation: %+v", mutation)
			}
		})
	}
}

func TestCreateResourceRejectsMalformedPreparedIdentityBeforeGeneratingIDs(t *testing.T) {
	repository := &captureRepository{}
	generated := 0
	service := NewService(repository, time.Now, func() string {
		generated++
		return "generated"
	})
	_, err := service.CreateLocation(context.Background(), CreateLocationCommand{
		Principal: resourcePrincipal("location.create"), DisplayID: "LOC-INVALID", Name: "Invalid",
		ActorID: "actor-id", Source: "ai", Prepared: PreparedIdentity{ResourceID: "not-a-uuid"},
	})
	if !errors.Is(err, ErrInvalid) || len(repository.mutations) != 0 || generated != 0 {
		t.Fatalf("malformed prepared identity escaped validation: error=%v mutations=%v generated=%d", err, repository.mutations, generated)
	}
}

func resourcePrincipal(capabilities ...string) authorization.Principal {
	return authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet(capabilities...),
	}
}

func testService(repository Repository) *Service {
	next := 0
	return NewService(repository, time.Now, func() string {
		next++
		return "id-" + string(rune('0'+next))
	}, tagging.NewCreationPreparer(assetClassificationRepository{}))
}

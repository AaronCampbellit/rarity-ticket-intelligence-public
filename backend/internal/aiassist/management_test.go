package aiassist

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type managementRepositoryStub struct {
	connection            ProviderConnection
	connections           []ProviderConnection
	models                []ModelProfile
	managedModels         []ModelProfile
	returnedModels        []ModelProfile
	policy                Policy
	policyErr             error
	created               ConnectionMutation
	createdCredentialText string
	createdErr            error
	updated               ConnectionMutation
	credential            CredentialMutation
	credentialText        string
	credentialErr         error
	enabled               ConnectionMutation
	reconciled            ModelReconciliationMutation
	reconciledModels      []ModelProfile
	model                 ModelMutation
	modelsUpdated         []ModelMutation
	lastBatch             ModelBatchMutation
	updateModelsErr       error
	failModelID           string
	policyMutation        PolicyMutation
	health                ConnectionHealthMutation
}

func (r *managementRepositoryStub) CreateConnection(_ context.Context, mutation ConnectionMutation) error {
	r.createdCredentialText = string(mutation.PlaintextCredential)
	r.created = mutation
	return r.createdErr
}
func (r *managementRepositoryStub) ListConnections(_ context.Context, _ scope.Target) ([]ProviderConnection, error) {
	return append([]ProviderConnection(nil), r.connections...), nil
}
func (r *managementRepositoryStub) GetConnection(_ context.Context, _ scope.Target, _ string) (ProviderConnection, error) {
	return r.connection, nil
}
func (r *managementRepositoryStub) ListModels(_ context.Context, _ scope.Target, _ string) ([]ModelProfile, error) {
	return append([]ModelProfile(nil), r.models...), nil
}
func (r *managementRepositoryStub) FindModels(_ context.Context, _ scope.Target, ids []string) ([]ModelProfile, error) {
	models := make([]ModelProfile, 0, len(ids))
	for _, id := range ids {
		for _, model := range r.managedModels {
			if model.ID == id {
				models = append(models, model)
				break
			}
		}
	}
	return models, nil
}
func (r *managementRepositoryStub) UpdateConnection(_ context.Context, mutation ConnectionMutation) error {
	r.updated = mutation
	return nil
}
func (r *managementRepositoryStub) ReplaceCredential(_ context.Context, mutation CredentialMutation) error {
	r.credentialText = string(mutation.PlaintextCredential)
	r.credential = mutation
	return r.credentialErr
}
func (r *managementRepositoryStub) SetConnectionEnabled(_ context.Context, mutation ConnectionMutation) error {
	r.enabled = mutation
	return nil
}
func (r *managementRepositoryStub) ReconcileModels(_ context.Context, mutation ModelReconciliationMutation) ([]ModelProfile, error) {
	r.reconciled = mutation
	if r.reconciledModels != nil {
		return append([]ModelProfile(nil), r.reconciledModels...), nil
	}
	return append([]ModelProfile(nil), mutation.Models...), nil
}
func (r *managementRepositoryStub) UpdateModels(_ context.Context, batch ModelBatchMutation) ([]ModelProfile, error) {
	if r.updateModelsErr != nil {
		return nil, r.updateModelsErr
	}
	for _, mutation := range batch.Mutations {
		if mutation.Model.ID == r.failModelID {
			return nil, errors.New("stale model version")
		}
	}
	r.modelsUpdated = append([]ModelMutation(nil), batch.Mutations...)
	r.lastBatch = batch
	if len(batch.Mutations) > 0 {
		r.model = batch.Mutations[len(batch.Mutations)-1]
	}
	if r.returnedModels != nil {
		return append([]ModelProfile(nil), r.returnedModels...), nil
	}
	models := make([]ModelProfile, 0, len(batch.Mutations))
	for _, mutation := range batch.Mutations {
		models = append(models, mutation.Model)
	}
	return models, nil
}
func (r *managementRepositoryStub) GetPolicy(_ context.Context, _ scope.Target) (Policy, error) {
	return r.policy, r.policyErr
}
func (r *managementRepositoryStub) UpdatePolicy(_ context.Context, mutation PolicyMutation) error {
	r.policyMutation = mutation
	return nil
}
func (r *managementRepositoryStub) RecordConnectionHealth(_ context.Context, mutation ConnectionHealthMutation) error {
	r.health = mutation
	return nil
}

type providerOperationsStub struct {
	tested     ProviderConnection
	discovered ProviderConnection
	models     []DiscoveredModel
	testErr    error
}

func (o *providerOperationsStub) Test(_ context.Context, connection ProviderConnection) error {
	o.tested = connection
	return o.testErr
}
func (o *providerOperationsStub) Discover(_ context.Context, connection ProviderConnection) ([]DiscoveredModel, error) {
	o.discovered = connection
	return append([]DiscoveredModel(nil), o.models...), nil
}

func managementPrincipal() authorization.Principal {
	return authorization.Principal{
		ID: "technician-id", Scope: scope.Principal{MSPID: "msp-id"},
		Capabilities: authorization.NewCapabilitySet("ai.manage"),
	}
}

func allZero(value []byte) bool {
	for _, byteValue := range value {
		if byteValue != 0 {
			return false
		}
	}
	return true
}

func TestManagementServiceCreatesCredentialFreeConnectionWithAuditAndOutbox(t *testing.T) {
	now := time.Date(2026, time.July, 29, 13, 0, 0, 0, time.UTC)
	repository := &managementRepositoryStub{}
	service := NewManagementService(repository, &providerOperationsStub{}, func() time.Time { return now }, sequenceIDs("connection-id", "audit-id", "event-id", "correlation-id"))
	created, err := service.CreateConnection(context.Background(), CreateConnectionCommand{
		Principal: managementPrincipal(), Connection: ProviderConnection{
			Name: "Local Ollama", Adapter: AdapterOllama, Network: NetworkLocal,
			BaseURL: "http://127.0.0.1:11434",
		}, PlaintextCredential: []byte("synthetic-api-key"),
		AcknowledgeLocalNetwork: true, Reason: "initial provider setup",
	})
	if err != nil {
		t.Fatalf("CreateConnection() error=%v", err)
	}
	if created.ID != "connection-id" || created.MSPID != "msp-id" ||
		!created.CredentialConfigured || created.Timeout != 15*time.Minute ||
		repository.created.Audit.Action != "ai.provider_connection.created" ||
		repository.created.Event.EventType != "ai.provider_connection.created" ||
		repository.created.Audit.Reason != "initial provider setup" {
		t.Fatalf("created=%+v mutation=%+v", created, repository.created)
	}
	if repository.createdCredentialText != "synthetic-api-key" || !allZero(repository.created.PlaintextCredential) {
		t.Fatalf("credential ownership not cleared after repository call: %+v", repository.created)
	}
}

func TestManagementServiceCreateIgnoresBrowserControlledOperationalAndAcknowledgementState(t *testing.T) {
	now := time.Date(2026, time.July, 29, 13, 15, 0, 0, time.UTC)
	forged := now.Add(-time.Hour)
	repository := &managementRepositoryStub{}
	service := NewManagementService(repository, &providerOperationsStub{}, func() time.Time { return now }, sequenceIDs("connection-id", "audit", "event", "correlation"))
	created, err := service.CreateConnection(context.Background(), CreateConnectionCommand{
		Principal: managementPrincipal(), AcknowledgeLocalNetwork: true, Reason: "initial provider setup",
		Connection: ProviderConnection{
			Name: "Local Ollama", Adapter: AdapterOllama, Network: NetworkLocal, BaseURL: "http://127.0.0.1:11434",
			Enabled: true, Health: HealthHealthy, LastTestedAt: &forged, LastSucceededAt: &forged,
			LastErrorCode: "browser-controlled", LocalNetworkAcknowledgedAt: &forged,
		},
	})
	if err != nil {
		t.Fatalf("CreateConnection() error=%v", err)
	}
	if created.Enabled || created.Health != HealthPending || created.LastTestedAt != nil ||
		created.LastSucceededAt != nil || created.LastErrorCode != "" ||
		created.LocalNetworkAcknowledgedAt == nil || !created.LocalNetworkAcknowledgedAt.Equal(now) {
		t.Fatalf("browser-controlled create state accepted: %+v", created)
	}
}

func TestManagementServiceCreateClearsForgedAcknowledgementOutsideAcknowledgedLocalMode(t *testing.T) {
	now := time.Date(2026, time.July, 29, 13, 20, 0, 0, time.UTC)
	forged := now.Add(-time.Hour)
	service := NewManagementService(&managementRepositoryStub{}, &providerOperationsStub{}, func() time.Time { return now }, sequenceIDs("connection-id", "audit", "event", "correlation"))
	created, err := service.CreateConnection(context.Background(), CreateConnectionCommand{
		Principal: managementPrincipal(), Reason: "initial provider setup",
		Connection: ProviderConnection{Name: "Remote", Adapter: AdapterOpenAICompatible, Network: NetworkRemote, BaseURL: "https://models.example.test", LocalNetworkAcknowledgedAt: &forged},
	})
	if err != nil || created.LocalNetworkAcknowledgedAt != nil {
		t.Fatalf("CreateConnection() connection=%+v error=%v", created, err)
	}
}

func TestManagementServiceRequiresMSPScopedAIManageAndVersionedReasons(t *testing.T) {
	repository := &managementRepositoryStub{connection: validProviderConnection()}
	service := NewManagementService(repository, &providerOperationsStub{}, time.Now, sequenceIDs("id"))
	denied := managementPrincipal()
	denied.Capabilities = authorization.NewCapabilitySet("ai.assist")
	if _, err := service.ListConnections(context.Background(), ListConnectionsCommand{Principal: denied}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("missing ai.manage error=%v", err)
	}
	clientScoped := managementPrincipal()
	clientScoped.Scope.ClientID = "client-id"
	if _, err := service.ListConnections(context.Background(), ListConnectionsCommand{Principal: clientScoped}); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("client scoped management error=%v", err)
	}
	if _, err := service.SetConnectionEnabled(context.Background(), SetConnectionEnabledCommand{
		Principal: managementPrincipal(), ID: "connection-id", ExpectedVersion: 1, Enabled: true,
	}); !errors.Is(err, ErrInvalidProviderManagement) {
		t.Fatalf("missing reason error=%v", err)
	}
	if _, err := service.CreateConnection(context.Background(), CreateConnectionCommand{
		Principal: managementPrincipal(), Connection: ProviderConnection{Name: "Remote", Adapter: AdapterOpenAICompatible, Network: NetworkRemote, BaseURL: "https://models.example.test"},
	}); !errors.Is(err, ErrInvalidProviderManagement) {
		t.Fatalf("missing create reason error=%v", err)
	}
}

func TestManagementServiceWipesCredentialClonesAfterRepositoryReturns(t *testing.T) {
	for _, test := range []struct {
		name  string
		run   func(*ManagementService) error
		check func(*managementRepositoryStub) ([]byte, string)
	}{
		{name: "create error", run: func(service *ManagementService) error {
			_, err := service.CreateConnection(context.Background(), CreateConnectionCommand{Principal: managementPrincipal(), Connection: ProviderConnection{Name: "Remote", Adapter: AdapterOpenAICompatible, Network: NetworkRemote, BaseURL: "https://models.example.test"}, PlaintextCredential: []byte("create-secret"), Reason: "initial provider setup"})
			return err
		}, check: func(repository *managementRepositoryStub) ([]byte, string) {
			return repository.created.PlaintextCredential, repository.createdCredentialText
		}},
		{name: "replace error", run: func(service *ManagementService) error {
			_, err := service.ReplaceCredential(context.Background(), ReplaceCredentialCommand{Principal: managementPrincipal(), ID: "connection-id", ExpectedVersion: 1, PlaintextCredential: []byte("replace-secret"), Reason: "rotate credential"})
			return err
		}, check: func(repository *managementRepositoryStub) ([]byte, string) {
			return repository.credential.PlaintextCredential, repository.credentialText
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &managementRepositoryStub{connection: validProviderConnection(), createdErr: errors.New("repository failure"), credentialErr: errors.New("repository failure")}
			service := NewManagementService(repository, &providerOperationsStub{}, time.Now, sequenceIDs("connection", "audit", "event", "correlation"))
			if err := test.run(service); err == nil {
				t.Fatal("expected repository error")
			}
			credential, seen := test.check(repository)
			if !allZero(credential) || !strings.HasSuffix(seen, "secret") {
				t.Fatalf("credential clone not cleared after repository return: seen=%q stored=%v", seen, credential)
			}
		})
	}
}

func TestManagementServiceRetainsAcknowledgedLocalModeOnConnectionUpdate(t *testing.T) {
	now := time.Date(2026, time.July, 29, 13, 30, 0, 0, time.UTC)
	connection := validProviderConnection()
	connection.Adapter, connection.Network, connection.BaseURL = AdapterOllama, NetworkLocal, "http://127.0.0.1:11434"
	connection.Timeout = 15 * time.Minute
	connection.LocalNetworkAcknowledgedAt = &now
	repository := &managementRepositoryStub{connection: connection}
	service := NewManagementService(repository, &providerOperationsStub{}, func() time.Time { return now }, sequenceIDs("audit", "event", "correlation"))
	update := connection
	update.Name = "Renamed local Ollama"
	update.LocalNetworkAcknowledgedAt = nil // Browsers need not round-trip a historical acknowledgement.
	updated, err := service.UpdateConnection(context.Background(), UpdateConnectionCommand{
		Principal: managementPrincipal(), Connection: update, ExpectedVersion: 1, Reason: "clarify provider name",
	})
	if err != nil || updated.LocalNetworkAcknowledgedAt == nil {
		t.Fatalf("UpdateConnection() connection=%+v error=%v", updated, err)
	}
}

func TestManagementServiceUpdateIgnoresBrowserOperationalStateAndForgedAcknowledgement(t *testing.T) {
	now := time.Date(2026, time.July, 29, 13, 45, 0, 0, time.UTC)
	acknowledgedAt := now.Add(-time.Hour)
	testedAt := now.Add(-30 * time.Minute)
	connection := validProviderConnection()
	connection.Adapter, connection.Network, connection.BaseURL = AdapterOllama, NetworkLocal, "http://127.0.0.1:11434"
	connection.Timeout, connection.Enabled, connection.Health = 15*time.Minute, true, HealthHealthy
	connection.LocalNetworkAcknowledgedAt, connection.LastTestedAt, connection.LastSucceededAt = &acknowledgedAt, &testedAt, &testedAt
	connection.DisclosureAcceptedAt = &acknowledgedAt
	connection.LastErrorCode = "previous-safe-code"
	repository := &managementRepositoryStub{connection: connection}
	service := NewManagementService(repository, &providerOperationsStub{}, func() time.Time { return now }, sequenceIDs("audit", "event", "correlation"))
	update := connection
	update.Name, update.Enabled, update.Health = "Renamed local Ollama", false, HealthFailed
	update.LastTestedAt, update.LastSucceededAt, update.LastErrorCode = &now, &now, "browser-controlled"
	update.LocalNetworkAcknowledgedAt = &now
	updated, err := service.UpdateConnection(context.Background(), UpdateConnectionCommand{
		Principal: managementPrincipal(), Connection: update, ExpectedVersion: 1, Reason: "rename provider",
	})
	if err != nil || !updated.Enabled || updated.Health != HealthHealthy ||
		updated.LastTestedAt != connection.LastTestedAt || updated.LastSucceededAt != connection.LastSucceededAt ||
		updated.LastErrorCode != "previous-safe-code" ||
		updated.LocalNetworkAcknowledgedAt != connection.LocalNetworkAcknowledgedAt ||
		updated.DisclosureAcceptedAt != connection.DisclosureAcceptedAt {
		t.Fatalf("browser-controlled update state accepted: connection=%+v error=%v", updated, err)
	}
}

func TestManagementServiceRequiresServerAcknowledgementForChangedLocalEndpoint(t *testing.T) {
	now := time.Date(2026, time.July, 29, 14, 15, 0, 0, time.UTC)
	oldAck, forged := now.Add(-time.Hour), now.Add(-2*time.Hour)
	connection := validProviderConnection()
	connection.Adapter, connection.Network, connection.BaseURL = AdapterOllama, NetworkLocal, "http://127.0.0.1:11434"
	connection.Timeout, connection.LocalNetworkAcknowledgedAt = 15*time.Minute, &oldAck
	connection.DisclosureAcceptedAt = &oldAck
	repository := &managementRepositoryStub{connection: connection}
	service := NewManagementService(repository, &providerOperationsStub{}, func() time.Time { return now }, sequenceIDs("audit", "event", "correlation"))
	update := connection
	update.BaseURL, update.LocalNetworkAcknowledgedAt = "http://127.0.0.1:11435", &forged
	if _, err := service.UpdateConnection(context.Background(), UpdateConnectionCommand{
		Principal: managementPrincipal(), Connection: update, ExpectedVersion: 1, Reason: "move provider endpoint",
	}); !errors.Is(err, ErrLocalAcknowledgementRequired) {
		t.Fatalf("changed local endpoint error=%v", err)
	}
	updated, err := service.UpdateConnection(context.Background(), UpdateConnectionCommand{
		Principal: managementPrincipal(), Connection: update, ExpectedVersion: 1, Reason: "move provider endpoint", AcknowledgeLocalNetwork: true,
	})
	if err != nil || updated.LocalNetworkAcknowledgedAt == nil ||
		!updated.LocalNetworkAcknowledgedAt.Equal(now) ||
		updated.DisclosureAcceptedAt != nil {
		t.Fatalf("server acknowledgement connection=%+v error=%v", updated, err)
	}
}

func TestManagementServiceRequiresServerAcknowledgementWhenChangingIntoLocalMode(t *testing.T) {
	now := time.Date(2026, time.July, 29, 14, 20, 0, 0, time.UTC)
	forged := now.Add(-time.Hour)
	connection := validProviderConnection()
	repository := &managementRepositoryStub{connection: connection}
	service := NewManagementService(repository, &providerOperationsStub{}, func() time.Time { return now }, sequenceIDs("audit", "event", "correlation"))
	update := connection
	update.Adapter, update.Network, update.BaseURL, update.LocalNetworkAcknowledgedAt = AdapterOllama, NetworkLocal, "http://127.0.0.1:11434", &forged
	if _, err := service.UpdateConnection(context.Background(), UpdateConnectionCommand{
		Principal: managementPrincipal(), Connection: update, ExpectedVersion: 1, Reason: "move to local provider",
	}); !errors.Is(err, ErrLocalAcknowledgementRequired) {
		t.Fatalf("local mode transition error=%v", err)
	}
	updated, err := service.UpdateConnection(context.Background(), UpdateConnectionCommand{
		Principal: managementPrincipal(), Connection: update, ExpectedVersion: 1, Reason: "move to local provider", AcknowledgeLocalNetwork: true,
	})
	if err != nil || updated.LocalNetworkAcknowledgedAt == nil || !updated.LocalNetworkAcknowledgedAt.Equal(now) {
		t.Fatalf("UpdateConnection() connection=%+v error=%v", updated, err)
	}
}

func TestManagementCommandsNeverSerializePlaintextCredentials(t *testing.T) {
	for _, command := range []any{
		CreateConnectionCommand{PlaintextCredential: []byte("synthetic-api-key")},
		ReplaceCredentialCommand{PlaintextCredential: []byte("synthetic-api-key")},
	} {
		body, err := json.Marshal(command)
		if err != nil || strings.Contains(string(body), "synthetic-api-key") || strings.Contains(string(body), "PlaintextCredential") {
			t.Fatalf("command=%T JSON=%s error=%v", command, body, err)
		}
	}
}

func TestManagementServiceListsModelsAtMSPScope(t *testing.T) {
	repository := &managementRepositoryStub{models: []ModelProfile{{ID: "model-id", MSPID: "msp-id"}}}
	service := NewManagementService(repository, &providerOperationsStub{}, time.Now, sequenceIDs("unused"))
	models, err := service.ListModels(context.Background(), ListModelsCommand{Principal: managementPrincipal(), ConnectionID: "connection-id"})
	if err != nil || len(models) != 1 || models[0].ID != "model-id" {
		t.Fatalf("ListModels() models=%+v error=%v", models, err)
	}
}

func TestManagementServiceTestsOptionalCredentialsAndReconcilesDisabledModels(t *testing.T) {
	now := time.Date(2026, time.July, 29, 14, 0, 0, 0, time.UTC)
	connection := validProviderConnection()
	connection.CredentialConfigured = false
	repository := &managementRepositoryStub{connection: connection}
	operations := &providerOperationsStub{models: []DiscoveredModel{{
		ProviderModelID: "gpt-compatible", DisplayName: "Compatible model", ContextLimit: 32_000,
	}}}
	service := NewManagementService(repository, operations, func() time.Time { return now }, sequenceIDs("health-audit", "health-event", "health-correlation", "model-id", "discover-audit", "discover-event", "discover-correlation"))
	tested, err := service.TestConnection(context.Background(), TestConnectionCommand{Principal: managementPrincipal(), ID: connection.ID})
	if err != nil {
		t.Fatalf("optional-credential connection test error=%v", err)
	}
	if operations.tested.CredentialConfigured || tested.Health != HealthHealthy || tested.LastSucceededAt == nil || repository.health.Health != HealthHealthy || repository.health.Audit.Action != "ai.provider_connection.tested" {
		t.Fatalf("test did not record safe health result: operations=%+v mutation=%+v", operations, repository.health)
	}
	models, err := service.DiscoverModels(context.Background(), DiscoverModelsCommand{Principal: managementPrincipal(), ID: connection.ID, ExpectedVersion: 1, Reason: "refresh available models"})
	if err != nil {
		t.Fatalf("DiscoverModels() error=%v", err)
	}
	if len(models) != 1 || models[0].Enabled || models[0].ID != "model-id" ||
		repository.reconciled.Audit.Reason != "refresh available models" ||
		repository.reconciled.ConnectionVersion != 1 {
		t.Fatalf("discovery reconciliation=%+v mutation=%+v", models, repository.reconciled)
	}
}

func TestManagementServiceDiscoveryReturnsCanonicalPersistedModelsAfterRepeatDiscovery(t *testing.T) {
	now := time.Date(2026, time.July, 29, 16, 0, 0, 0, time.UTC)
	connection := validProviderConnection()
	canonical := ModelProfile{
		ID: "persisted-model-id", MSPID: connection.MSPID, ConnectionID: connection.ID,
		ProviderModelID: "provider-model", DisplayName: "Refreshed provider model",
		SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 16384, OutputLimit: 2048,
		ZeroCost: true, Enabled: true, Version: 7,
	}
	repository := &managementRepositoryStub{connection: connection, reconciledModels: []ModelProfile{canonical}}
	operations := &providerOperationsStub{models: []DiscoveredModel{{
		ProviderModelID: "provider-model", DisplayName: "Refreshed provider model", ContextLimit: 16384,
	}}}
	service := NewManagementService(repository, operations, func() time.Time { return now }, sequenceIDs(
		"fresh-browser-id", "audit", "event", "correlation",
	))

	models, err := service.DiscoverModels(context.Background(), DiscoverModelsCommand{
		Principal: managementPrincipal(), ID: connection.ID, ExpectedVersion: connection.Version, Reason: "refresh models",
	})

	if err != nil {
		t.Fatalf("DiscoverModels() error=%v", err)
	}
	if len(models) != 1 || models[0].ID != canonical.ID || models[0].ID == repository.reconciled.Models[0].ID {
		t.Fatalf("DiscoverModels() models=%+v mutation=%+v", models, repository.reconciled.Models)
	}
	if !models[0].Enabled || models[0].Version != 7 || len(models[0].SupportedFeatures) != 1 || models[0].SupportedFeatures[0] != FeatureSummary {
		t.Fatalf("repeat discovery lost persisted configuration: %+v", models[0])
	}
}

func TestManagementServiceRecordsFailedConnectionHealthWithoutLeakingProviderError(t *testing.T) {
	connection := validProviderConnection()
	repository := &managementRepositoryStub{connection: connection}
	operations := &providerOperationsStub{testErr: errors.New("provider replied Authorization: synthetic-api-key")}
	service := NewManagementService(repository, operations, time.Now, sequenceIDs("audit", "event", "correlation"))
	tested, err := service.TestConnection(context.Background(), TestConnectionCommand{Principal: managementPrincipal(), ID: connection.ID})
	if err == nil || tested.Health != HealthFailed || tested.LastErrorCode != ErrProviderConnectionTestFailed.Error() || repository.health.Health != HealthFailed || repository.health.LastErrorCode != ErrProviderConnectionTestFailed.Error() {
		t.Fatalf("tested=%+v error=%v health=%+v", tested, err, repository.health)
	}
}

func TestManagementServiceUpdatesModelsWithPlannedBulkSurface(t *testing.T) {
	repository := &managementRepositoryStub{managedModels: []ModelProfile{
		{ID: "model-id", MSPID: "msp-id", ConnectionID: "connection-id", ProviderModelID: "model", DisplayName: "Existing", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 32_000, OutputLimit: 4_000, Version: 4},
		{ID: "model-two-id", MSPID: "msp-id", ConnectionID: "connection-id", ProviderModelID: "model-two", DisplayName: "Existing two", SupportedFeatures: []Feature{FeatureReplyDraft}, ContextLimit: 32_000, OutputLimit: 4_000, Version: 9},
	}}
	service := NewManagementService(repository, &providerOperationsStub{}, time.Now, sequenceIDs("first-audit", "first-event", "first-correlation", "second-audit", "second-event", "second-correlation"))
	models, err := service.UpdateModels(context.Background(), UpdateModelsCommand{
		Principal: managementPrincipal(), Reason: "enable approved summary model",
		Updates: []ModelUpdate{
			{ExpectedVersion: 4, Model: ModelProfile{ID: "model-id", ConnectionID: "connection-id", ProviderModelID: "model", DisplayName: "Model", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 32_000, OutputLimit: 4_000}},
			{ExpectedVersion: 9, Model: ModelProfile{ID: "model-two-id", ConnectionID: "connection-id", ProviderModelID: "model-two", DisplayName: "Model two", SupportedFeatures: []Feature{FeatureReplyDraft}, ContextLimit: 32_000, OutputLimit: 4_000}},
		},
	})
	if err != nil || len(models) != 2 || models[0].Version != 5 || models[1].Version != 10 || len(repository.modelsUpdated) != 2 ||
		repository.modelsUpdated[0].ExpectedVersion != 4 || repository.modelsUpdated[1].ExpectedVersion != 9 ||
		repository.modelsUpdated[0].Audit.Reason != "enable approved summary model" ||
		repository.lastBatch.MSPID != "msp-id" || repository.modelsUpdated[0].Audit.CorrelationID == "" || repository.modelsUpdated[1].Event.EventType != "ai.model_profile.updated" {
		t.Fatalf("UpdateModels() models=%+v error=%v mutations=%+v", models, err, repository.modelsUpdated)
	}
}

func TestManagementServiceRejectsDuplicateModelUpdatesBeforeRepositoryMutation(t *testing.T) {
	repository := &managementRepositoryStub{}
	service := NewManagementService(repository, &providerOperationsStub{}, time.Now, sequenceIDs("first-audit", "first-event", "first-correlation", "second-audit", "second-event", "second-correlation"))
	model := ModelProfile{ID: "model-id", ConnectionID: "connection-id", ProviderModelID: "model", DisplayName: "Model", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 32_000, OutputLimit: 4_000}
	_, err := service.UpdateModels(context.Background(), UpdateModelsCommand{
		Principal: managementPrincipal(), Reason: "duplicate request", Updates: []ModelUpdate{{Model: model, ExpectedVersion: 1}, {Model: model, ExpectedVersion: 1}},
	})
	if !errors.Is(err, ErrInvalidProviderManagement) || len(repository.modelsUpdated) != 0 {
		t.Fatalf("duplicate update error=%v mutations=%+v", err, repository.modelsUpdated)
	}
}

func TestManagementServiceDoesNotExposePartialModelsWhenAtomicBatchFails(t *testing.T) {
	repository := &managementRepositoryStub{failModelID: "model-two-id", managedModels: []ModelProfile{
		{ID: "model-one-id", MSPID: "msp-id", ConnectionID: "connection-id", ProviderModelID: "one", DisplayName: "Existing one", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 32_000, OutputLimit: 4_000, Version: 1},
		{ID: "model-two-id", MSPID: "msp-id", ConnectionID: "connection-id", ProviderModelID: "two", DisplayName: "Existing two", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 32_000, OutputLimit: 4_000, Version: 1},
	}}
	service := NewManagementService(repository, &providerOperationsStub{}, time.Now, sequenceIDs("first-audit", "first-event", "first-correlation", "second-audit", "second-event", "second-correlation"))
	_, err := service.UpdateModels(context.Background(), UpdateModelsCommand{
		Principal: managementPrincipal(), Reason: "atomic change", Updates: []ModelUpdate{
			{ExpectedVersion: 1, Model: ModelProfile{ID: "model-one-id", ConnectionID: "connection-id", ProviderModelID: "one", DisplayName: "One", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 32_000, OutputLimit: 4_000}},
			{ExpectedVersion: 1, Model: ModelProfile{ID: "model-two-id", ConnectionID: "connection-id", ProviderModelID: "two", DisplayName: "Two", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 32_000, OutputLimit: 4_000}},
		},
	})
	if err == nil || len(repository.modelsUpdated) != 0 {
		t.Fatalf("partial model update error=%v mutations=%+v", err, repository.modelsUpdated)
	}
}

func TestManagementServiceUpdatesModelAndPolicyWithScopedVersionedMutations(t *testing.T) {
	now := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	connection := validProviderConnection()
	connection.Enabled = true
	repository := &managementRepositoryStub{connection: connection, policy: Policy{MSPID: "msp-id", Enabled: false, PromptVersion: "v1", Version: 2}, managedModels: []ModelProfile{{ID: "model-id", MSPID: "msp-id", ConnectionID: "connection-id", ProviderModelID: "model", DisplayName: "Existing model", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 32_000, OutputLimit: 4_000, Enabled: true, Version: 4}}}
	service := NewManagementService(repository, &providerOperationsStub{}, func() time.Time { return now }, sequenceIDs("audit", "event", "correlation", "policy-audit", "policy-event", "policy-correlation"))
	model, err := service.UpdateModel(context.Background(), UpdateModelCommand{
		Principal: managementPrincipal(), Model: ModelProfile{ID: "model-id", ConnectionID: "connection-id", ProviderModelID: "model", DisplayName: "Model", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 32_000, OutputLimit: 4_000},
		ExpectedVersion: 4, Reason: "enable approved summary model",
	})
	if err != nil || model.MSPID != "msp-id" || model.Version != 5 || repository.model.Audit.Reason == "" {
		t.Fatalf("UpdateModel() model=%+v error=%v mutation=%+v", model, err, repository.model)
	}
	policy := Policy{MSPID: "msp-id", Enabled: true, ProviderDisclosureAccepted: true, PromptVersion: "summary-v1", AllowedFeatures: []Feature{FeatureSummary}, SummaryModelProfileID: "model-id"}
	updated, err := service.UpdatePolicy(context.Background(), UpdatePolicyCommand{Principal: managementPrincipal(), Policy: policy, ExpectedVersion: 2, Reason: "enable reviewed summaries"})
	if err != nil || updated.Version != 3 || repository.policyMutation.Audit.Action != "ai.policy.updated" || repository.policyMutation.Event.EventType != "ai.policy.updated" {
		t.Fatalf("UpdatePolicy() policy=%+v error=%v mutation=%+v", updated, err, repository.policyMutation)
	}
}

func TestManagementServiceReturnsEditableDefaultWhenPolicyHasNotBeenCreated(t *testing.T) {
	repository := &managementRepositoryStub{policyErr: scope.ErrNotFound}
	service := NewManagementService(
		repository,
		&providerOperationsStub{},
		time.Now,
		sequenceIDs("unused"),
	)

	policy, err := service.GetPolicy(context.Background(), GetPolicyCommand{
		Principal: managementPrincipal(),
	})
	if err != nil {
		t.Fatalf("GetPolicy() error=%v", err)
	}
	if policy.MSPID != "msp-id" || policy.Enabled || policy.Version != 0 ||
		policy.PromptVersion != "ai-v1" || len(policy.AllowedFeatures) != 0 {
		t.Fatalf("GetPolicy() policy=%+v", policy)
	}
}

func TestManagementServiceRejectsEnablingPolicyWithoutProviderDisclosure(t *testing.T) {
	repository := &managementRepositoryStub{policy: Policy{MSPID: "msp-id", Version: 1}}
	service := NewManagementService(
		repository,
		&providerOperationsStub{},
		time.Now,
		sequenceIDs("audit", "event", "correlation"),
	)
	_, err := service.UpdatePolicy(context.Background(), UpdatePolicyCommand{
		Principal: managementPrincipal(),
		Policy: Policy{
			Enabled: true, AllowedFeatures: []Feature{FeatureSummary},
			SummaryModelProfileID: "model-id",
		},
		ExpectedVersion: 1,
		Reason:          "attempt enablement without disclosure",
	})
	if !errors.Is(err, ErrInvalidProviderConfiguration) ||
		repository.policyMutation.Policy.Version != 0 {
		t.Fatalf("UpdatePolicy() error=%v mutation=%+v", err, repository.policyMutation)
	}
}

func TestManagementServiceRejectsPolicyMappingToFeatureIncompatibleModelBeforeWrite(t *testing.T) {
	connection := validProviderConnection()
	connection.Enabled = true
	repository := &managementRepositoryStub{
		connection: connection,
		policy:     Policy{MSPID: "msp-id", Version: 1},
		managedModels: []ModelProfile{{
			ID: "reply-only", MSPID: "msp-id", ConnectionID: "connection-id", ProviderModelID: "reply",
			DisplayName: "Reply only", SupportedFeatures: []Feature{FeatureReplyDraft}, ContextLimit: 32_000,
			OutputLimit: 4_000, Enabled: true, Version: 1,
		}},
	}
	service := NewManagementService(repository, &providerOperationsStub{}, time.Now, sequenceIDs("audit", "event", "correlation"))
	_, err := service.UpdatePolicy(context.Background(), UpdatePolicyCommand{
		Principal: managementPrincipal(), ExpectedVersion: 1, Reason: "invalid summary routing",
		Policy: Policy{Enabled: true, ProviderDisclosureAccepted: true, PromptVersion: "v1", AllowedFeatures: []Feature{FeatureSummary}, SummaryModelProfileID: "reply-only"},
	})
	if !errors.Is(err, ErrInvalidProviderConfiguration) || repository.policyMutation.Policy.Version != 0 {
		t.Fatalf("UpdatePolicy() error=%v mutation=%+v", err, repository.policyMutation)
	}
}

func TestManagementServicePreservesTrustedModelIdentityDuringBatchUpdate(t *testing.T) {
	stored := ModelProfile{ID: "model-id", MSPID: "msp-id", ConnectionID: "trusted-connection", ProviderModelID: "trusted-model", DisplayName: "Discovered", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 32_000, OutputLimit: 4_000, Version: 3}
	repository := &managementRepositoryStub{managedModels: []ModelProfile{stored}}
	service := NewManagementService(repository, &providerOperationsStub{}, time.Now, sequenceIDs("audit", "event", "correlation"))
	requested := stored
	requested.ConnectionID, requested.ProviderModelID, requested.DisplayName = "browser-reassigned", "browser-model", "Operator label"
	updated, err := service.UpdateModels(context.Background(), UpdateModelsCommand{Principal: managementPrincipal(), Reason: "rename model", Updates: []ModelUpdate{{Model: requested, ExpectedVersion: 3}}})
	if err != nil || len(updated) != 1 || updated[0].ConnectionID != "trusted-connection" || updated[0].ProviderModelID != "trusted-model" || updated[0].DisplayName != "Operator label" || repository.modelsUpdated[0].Model.MSPID != "msp-id" {
		t.Fatalf("UpdateModels() models=%+v error=%v mutations=%+v", updated, err, repository.modelsUpdated)
	}
}

func TestManagementServiceRejectsMissingOrStaleTrustedModelsBeforeBatchWrite(t *testing.T) {
	stored := ModelProfile{ID: "model-id", MSPID: "msp-id", ConnectionID: "connection-id", ProviderModelID: "model", DisplayName: "Existing", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 32_000, OutputLimit: 4_000, Version: 3}
	for _, test := range []struct {
		name    string
		models  []ModelProfile
		updates []ModelUpdate
	}{
		{"missing", []ModelProfile{stored}, []ModelUpdate{{Model: ModelProfile{ID: "missing-id", ConnectionID: "connection-id", ProviderModelID: "model", DisplayName: "Missing", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 32_000, OutputLimit: 4_000}, ExpectedVersion: 1}}},
		{"cross MSP", []ModelProfile{{ID: "model-id", MSPID: "other-msp", ConnectionID: "connection-id", ProviderModelID: "model", DisplayName: "Cross", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 32_000, OutputLimit: 4_000, Version: 3}}, []ModelUpdate{{Model: stored, ExpectedVersion: 3}}},
		{"stale", []ModelProfile{stored}, []ModelUpdate{{Model: stored, ExpectedVersion: 2}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &managementRepositoryStub{managedModels: test.models}
			service := NewManagementService(repository, &providerOperationsStub{}, time.Now, sequenceIDs("unused"))
			_, err := service.UpdateModels(context.Background(), UpdateModelsCommand{Principal: managementPrincipal(), Reason: "validate trusted model", Updates: test.updates})
			if !errors.Is(err, ErrProviderRepositoryContract) || len(repository.modelsUpdated) != 0 {
				t.Fatalf("UpdateModels() error=%v mutations=%+v", err, repository.modelsUpdated)
			}
		})
	}
}

func TestManagementServiceRejectsInvalidModelBatchVersionBeforeRepositoryWrite(t *testing.T) {
	repository := &managementRepositoryStub{}
	service := NewManagementService(repository, &providerOperationsStub{}, time.Now, sequenceIDs("unused"))
	_, err := service.UpdateModels(context.Background(), UpdateModelsCommand{Principal: managementPrincipal(), Reason: "invalid version", Updates: []ModelUpdate{{ExpectedVersion: 0, Model: ModelProfile{ID: "model-id"}}}})
	if !errors.Is(err, ErrInvalidProviderManagement) || len(repository.modelsUpdated) != 0 {
		t.Fatalf("UpdateModels() error=%v mutations=%+v", err, repository.modelsUpdated)
	}
}

func TestManagementServiceRejectsEmptyModelBatchBeforeRepositoryWrite(t *testing.T) {
	repository := &managementRepositoryStub{}
	service := NewManagementService(repository, &providerOperationsStub{}, time.Now, sequenceIDs("unused"))
	_, err := service.UpdateModels(context.Background(), UpdateModelsCommand{Principal: managementPrincipal(), Reason: "empty batch"})
	if !errors.Is(err, ErrInvalidProviderManagement) || len(repository.modelsUpdated) != 0 {
		t.Fatalf("UpdateModels() error=%v mutations=%+v", err, repository.modelsUpdated)
	}
}

func TestManagementServiceRejectsInvalidOrIncompleteRepositoryBatchResults(t *testing.T) {
	stored := ModelProfile{ID: "model-id", MSPID: "msp-id", ConnectionID: "connection-id", ProviderModelID: "model", DisplayName: "Existing", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 32_000, OutputLimit: 4_000, Version: 3}
	cases := []struct {
		name     string
		returned []ModelProfile
	}{
		{"empty", []ModelProfile{}},
		{"duplicate", []ModelProfile{{ID: "model-id", MSPID: "msp-id", ConnectionID: "connection-id", ProviderModelID: "model", Version: 4}, {ID: "model-id", MSPID: "msp-id", ConnectionID: "connection-id", ProviderModelID: "model", Version: 4}}},
		{"wrong identity", []ModelProfile{{ID: "model-id", MSPID: "msp-id", ConnectionID: "other-connection", ProviderModelID: "model", Version: 4}}},
		{"wrong version", []ModelProfile{{ID: "model-id", MSPID: "msp-id", ConnectionID: "connection-id", ProviderModelID: "model", Version: 3}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			repository := &managementRepositoryStub{managedModels: []ModelProfile{stored}, returnedModels: test.returned}
			service := NewManagementService(repository, &providerOperationsStub{}, time.Now, sequenceIDs("audit", "event", "correlation"))
			_, err := service.UpdateModel(context.Background(), UpdateModelCommand{Principal: managementPrincipal(), Model: stored, ExpectedVersion: 3, Reason: "validate result"})
			if !errors.Is(err, ErrProviderRepositoryContract) {
				t.Fatalf("UpdateModel() error=%v", err)
			}
		})
	}
}

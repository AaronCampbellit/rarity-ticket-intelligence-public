package aiassist

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrInvalidProviderManagement    = errors.New("invalid AI provider management request")
	ErrProviderConnectionTestFailed = errors.New("AI provider connection test failed")
	ErrProviderRepositoryContract   = errors.New("invalid AI provider repository result")
	ErrProviderInUse                = errors.New("AI provider is referenced by an enabled policy or active job")
)

// ProviderOperations is the sole provider-I/O boundary. Repository
// implementations persist control-plane facts only and never call providers.
type ProviderOperations interface {
	Test(context.Context, ProviderConnection) error
	Discover(context.Context, ProviderConnection) ([]DiscoveredModel, error)
}

type ConnectionMutation struct {
	Connection      ProviderConnection
	ExpectedVersion int64
	// PlaintextCredential is owned by the repository for the synchronous call.
	// It must be overwritten before CreateConnection returns, including errors.
	PlaintextCredential []byte `json:"-"`
	Audit               mutation.AuditRecord
	Event               mutation.EventRecord
}

type CredentialMutation struct {
	ConnectionID    string
	MSPID           string
	ExpectedVersion int64
	// PlaintextCredential is owned by the repository for the synchronous call.
	// It must be overwritten before ReplaceCredential returns, including errors.
	PlaintextCredential []byte `json:"-"`
	Audit               mutation.AuditRecord
	Event               mutation.EventRecord
}

type ModelReconciliationMutation struct {
	ConnectionID      string
	MSPID             string
	ConnectionVersion int64
	Models            []ModelProfile
	Audit             mutation.AuditRecord
	Event             mutation.EventRecord
}

type ModelMutation struct {
	Model           ModelProfile
	ExpectedVersion int64
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

// ModelBatchMutation is the trusted MSP-scoped atomic update contract.
// Implementations MUST update every model row by (id, msp_id, version) and
// insert its audit/outbox facts in one transaction, rolling back the entire
// batch if any optimistic match or write fails.
type ModelBatchMutation struct {
	MSPID     string
	Mutations []ModelMutation
}

type PolicyMutation struct {
	Policy          Policy
	ExpectedVersion int64
	Created         bool
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type ConnectionHealthMutation struct {
	ConnectionID    string
	MSPID           string
	ExpectedVersion int64
	Health          HealthState
	TestedAt        time.Time
	SucceededAt     *time.Time
	LastErrorCode   string
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

// ManagementRepository receives complete accepted mutations so storage can
// atomically apply the state change, audit record, and outbox event.
type ManagementRepository interface {
	CreateConnection(context.Context, ConnectionMutation) error
	ListConnections(context.Context, scope.Target) ([]ProviderConnection, error)
	GetConnection(context.Context, scope.Target, string) (ProviderConnection, error)
	ListModels(context.Context, scope.Target, string) ([]ModelProfile, error)
	FindModels(context.Context, scope.Target, []string) ([]ModelProfile, error)
	UpdateConnection(context.Context, ConnectionMutation) error
	ReplaceCredential(context.Context, CredentialMutation) error
	SetConnectionEnabled(context.Context, ConnectionMutation) error
	// ReconcileModels returns canonical persisted profiles. Existing provider
	// model identities and technician configuration must win over fresh
	// discovery input IDs.
	ReconcileModels(context.Context, ModelReconciliationMutation) ([]ModelProfile, error)
	// UpdateModels MUST atomically apply every row/version mutation and its
	// audit/outbox facts, or roll back the whole MSP-scoped batch.
	UpdateModels(context.Context, ModelBatchMutation) ([]ModelProfile, error)
	GetPolicy(context.Context, scope.Target) (Policy, error)
	UpdatePolicy(context.Context, PolicyMutation) error
	RecordConnectionHealth(context.Context, ConnectionHealthMutation) error
}

type ManagementService struct {
	repository ManagementRepository
	operations ProviderOperations
	now        func() time.Time
	newID      func() string
}

func NewManagementService(
	repository ManagementRepository,
	operations ProviderOperations,
	now func() time.Time,
	newID func() string,
) *ManagementService {
	return &ManagementService{
		repository: repository, operations: operations, now: now, newID: newID,
	}
}

type CreateConnectionCommand struct {
	Principal               authorization.Principal
	Connection              ProviderConnection
	PlaintextCredential     []byte `json:"-"`
	AcknowledgeLocalNetwork bool
	Reason                  string
}

type ListConnectionsCommand struct {
	Principal authorization.Principal
}

type UpdateConnectionCommand struct {
	Principal               authorization.Principal
	Connection              ProviderConnection
	ExpectedVersion         int64
	Reason                  string
	AcknowledgeLocalNetwork bool
}

type ReplaceCredentialCommand struct {
	Principal           authorization.Principal
	ID                  string
	ExpectedVersion     int64
	PlaintextCredential []byte `json:"-"`
	Reason              string
}

type SetConnectionEnabledCommand struct {
	Principal       authorization.Principal
	ID              string
	ExpectedVersion int64
	Enabled         bool
	Reason          string
}

type TestConnectionCommand struct {
	Principal authorization.Principal
	ID        string
}

type DiscoverModelsCommand struct {
	Principal       authorization.Principal
	ID              string
	ExpectedVersion int64
	Reason          string
}

type ListModelsCommand struct {
	Principal    authorization.Principal
	ConnectionID string
}

type UpdateModelCommand struct {
	Principal       authorization.Principal
	Model           ModelProfile
	ExpectedVersion int64
	Reason          string
}

type ModelUpdate struct {
	Model           ModelProfile
	ExpectedVersion int64
}

type UpdateModelsCommand struct {
	Principal authorization.Principal
	Updates   []ModelUpdate
	Reason    string
}

type GetPolicyCommand struct {
	Principal authorization.Principal
}

type UpdatePolicyCommand struct {
	Principal       authorization.Principal
	Policy          Policy
	ExpectedVersion int64
	Reason          string
}

func (s *ManagementService) CreateConnection(
	ctx context.Context, command CreateConnectionCommand,
) (ProviderConnection, error) {
	target, err := s.authorize(command.Principal)
	if err != nil {
		return ProviderConnection{}, err
	}
	connection := command.Connection
	connection.ID, connection.MSPID, connection.Version = s.newID(), target.MSPID, 1
	connection.Enabled, connection.Health = false, HealthPending
	connection.LocalNetworkAcknowledgedAt = nil
	connection.LastTestedAt, connection.LastSucceededAt, connection.LastErrorCode = nil, nil, ""
	connection = ConnectionWithDefaults(connection)
	if connection.Network == NetworkLocal && command.AcknowledgeLocalNetwork {
		now := s.now().UTC()
		connection.LocalNetworkAcknowledgedAt = &now
	}
	if err := ValidateConnection(connection); err != nil {
		return ProviderConnection{}, err
	}
	connection.CredentialConfigured = len(command.PlaintextCredential) > 0
	if strings.TrimSpace(command.Reason) == "" {
		return ProviderConnection{}, ErrInvalidProviderManagement
	}
	now := s.now().UTC()
	audit, event := s.facts(command.Principal, target, now, "ai.provider_connection.created", "ai_provider_connection", connection.ID, connection.Version, command.Reason)
	accepted := ConnectionMutation{
		Connection: connection, PlaintextCredential: cloneBytes(command.PlaintextCredential),
		Audit: audit, Event: event,
	}
	defer wipePlaintext(accepted.PlaintextCredential)
	if err := s.repository.CreateConnection(ctx, accepted); err != nil {
		return ProviderConnection{}, err
	}
	return connection, nil
}

func (s *ManagementService) ListConnections(
	ctx context.Context, command ListConnectionsCommand,
) ([]ProviderConnection, error) {
	target, err := s.authorize(command.Principal)
	if err != nil {
		return nil, err
	}
	connections, err := s.repository.ListConnections(ctx, target)
	if err != nil {
		return nil, err
	}
	return append([]ProviderConnection(nil), connections...), nil
}

func (s *ManagementService) ListModels(
	ctx context.Context, command ListModelsCommand,
) ([]ModelProfile, error) {
	target, err := s.authorize(command.Principal)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(command.ConnectionID) == "" {
		return nil, ErrInvalidProviderManagement
	}
	models, err := s.repository.ListModels(ctx, target, strings.TrimSpace(command.ConnectionID))
	if err != nil {
		return nil, err
	}
	return append([]ModelProfile(nil), models...), nil
}

func (s *ManagementService) UpdateConnection(
	ctx context.Context, command UpdateConnectionCommand,
) (ProviderConnection, error) {
	target, err := s.authorize(command.Principal)
	if err != nil {
		return ProviderConnection{}, err
	}
	if strings.TrimSpace(command.Connection.ID) == "" || command.ExpectedVersion < 1 || strings.TrimSpace(command.Reason) == "" {
		return ProviderConnection{}, ErrInvalidProviderManagement
	}
	current, err := s.repository.GetConnection(ctx, target, command.Connection.ID)
	if err != nil {
		return ProviderConnection{}, err
	}
	if err := object.RequireVersion(current.Version, command.ExpectedVersion); err != nil {
		return ProviderConnection{}, err
	}
	connection := command.Connection
	unchangedAcknowledgedLocalEndpoint := connection.Network == NetworkLocal &&
		current.Network == NetworkLocal &&
		strings.TrimSpace(connection.BaseURL) == strings.TrimSpace(current.BaseURL)
	unchangedProviderIdentity := connection.Adapter == current.Adapter &&
		connection.Network == current.Network &&
		strings.TrimSpace(connection.BaseURL) == strings.TrimSpace(current.BaseURL)
	connection.ID, connection.MSPID = current.ID, target.MSPID
	connection.CredentialConfigured = current.CredentialConfigured
	connection.Enabled, connection.Health = current.Enabled, current.Health
	connection.LastTestedAt, connection.LastSucceededAt = current.LastTestedAt, current.LastSucceededAt
	connection.LastErrorCode = current.LastErrorCode
	connection.DisclosureAcceptedAt = nil
	if unchangedProviderIdentity {
		connection.DisclosureAcceptedAt = current.DisclosureAcceptedAt
	}
	connection.LocalNetworkAcknowledgedAt = nil
	connection.Version = current.Version + 1
	connection = ConnectionWithDefaults(connection)
	if connection.Network == NetworkLocal {
		if unchangedAcknowledgedLocalEndpoint {
			connection.LocalNetworkAcknowledgedAt = current.LocalNetworkAcknowledgedAt
		}
		if command.AcknowledgeLocalNetwork {
			now := s.now().UTC()
			connection.LocalNetworkAcknowledgedAt = &now
		}
	}
	if err := ValidateConnection(connection); err != nil {
		return ProviderConnection{}, err
	}
	now := s.now().UTC()
	audit, event := s.facts(command.Principal, target, now, "ai.provider_connection.updated", "ai_provider_connection", connection.ID, connection.Version, command.Reason)
	accepted := ConnectionMutation{
		Connection: connection, ExpectedVersion: command.ExpectedVersion,
		Audit: audit, Event: event,
	}
	if err := s.repository.UpdateConnection(ctx, accepted); err != nil {
		return ProviderConnection{}, err
	}
	return connection, nil
}

func (s *ManagementService) ReplaceCredential(
	ctx context.Context, command ReplaceCredentialCommand,
) (ProviderConnection, error) {
	target, err := s.authorize(command.Principal)
	if err != nil {
		return ProviderConnection{}, err
	}
	if strings.TrimSpace(command.ID) == "" || command.ExpectedVersion < 1 || strings.TrimSpace(command.Reason) == "" {
		return ProviderConnection{}, ErrInvalidProviderManagement
	}
	connection, err := s.repository.GetConnection(ctx, target, command.ID)
	if err != nil {
		return ProviderConnection{}, err
	}
	if err := object.RequireVersion(connection.Version, command.ExpectedVersion); err != nil {
		return ProviderConnection{}, err
	}
	now := s.now().UTC()
	audit, event := s.facts(command.Principal, target, now, "ai.provider_connection.credential_replaced", "ai_provider_connection", connection.ID, connection.Version+1, command.Reason)
	accepted := CredentialMutation{
		ConnectionID: connection.ID, MSPID: target.MSPID, ExpectedVersion: command.ExpectedVersion,
		PlaintextCredential: cloneBytes(command.PlaintextCredential),
		Audit:               audit, Event: event,
	}
	defer wipePlaintext(accepted.PlaintextCredential)
	if err := s.repository.ReplaceCredential(ctx, accepted); err != nil {
		return ProviderConnection{}, err
	}
	connection.Version++
	connection.CredentialConfigured = len(command.PlaintextCredential) > 0
	return connection, nil
}

func (s *ManagementService) SetConnectionEnabled(
	ctx context.Context, command SetConnectionEnabledCommand,
) (ProviderConnection, error) {
	target, err := s.authorize(command.Principal)
	if err != nil {
		return ProviderConnection{}, err
	}
	if strings.TrimSpace(command.ID) == "" || command.ExpectedVersion < 1 || strings.TrimSpace(command.Reason) == "" {
		return ProviderConnection{}, ErrInvalidProviderManagement
	}
	connection, err := s.repository.GetConnection(ctx, target, command.ID)
	if err != nil {
		return ProviderConnection{}, err
	}
	if err := object.RequireVersion(connection.Version, command.ExpectedVersion); err != nil {
		return ProviderConnection{}, err
	}
	connection.Enabled = command.Enabled
	connection.Version++
	if !connection.Enabled {
		connection.Health = HealthDisabled
	} else if connection.Health == HealthDisabled {
		connection.Health = HealthPending
	}
	now := s.now().UTC()
	action := "ai.provider_connection.disabled"
	if connection.Enabled {
		action = "ai.provider_connection.enabled"
	}
	audit, event := s.facts(command.Principal, target, now, action, "ai_provider_connection", connection.ID, connection.Version, command.Reason)
	accepted := ConnectionMutation{
		Connection: connection, ExpectedVersion: command.ExpectedVersion,
		Audit: audit, Event: event,
	}
	if err := s.repository.SetConnectionEnabled(ctx, accepted); err != nil {
		return ProviderConnection{}, err
	}
	return connection, nil
}

func (s *ManagementService) TestConnection(ctx context.Context, command TestConnectionCommand) (ProviderConnection, error) {
	target, err := s.authorize(command.Principal)
	if err != nil {
		return ProviderConnection{}, err
	}
	if strings.TrimSpace(command.ID) == "" || s.operations == nil {
		return ProviderConnection{}, ErrInvalidProviderManagement
	}
	connection, err := s.repository.GetConnection(ctx, target, command.ID)
	if err != nil {
		return ProviderConnection{}, err
	}
	now := s.now().UTC()
	health := ConnectionHealthMutation{
		ConnectionID: connection.ID, MSPID: target.MSPID, ExpectedVersion: connection.Version,
		Health: HealthHealthy, TestedAt: now, SucceededAt: &now,
	}
	if err := s.operations.Test(ctx, connection); err != nil {
		health.Health, health.SucceededAt = HealthFailed, nil
		health.LastErrorCode = ErrProviderConnectionTestFailed.Error()
	}
	connection.Version++
	connection.Health, connection.LastTestedAt, connection.LastSucceededAt = health.Health, &now, health.SucceededAt
	connection.LastErrorCode = health.LastErrorCode
	health.Audit, health.Event = s.facts(command.Principal, target, now, "ai.provider_connection.tested", "ai_provider_connection", connection.ID, connection.Version, "")
	if err := s.repository.RecordConnectionHealth(ctx, health); err != nil {
		return ProviderConnection{}, err
	}
	if health.Health == HealthFailed {
		return connection, ErrProviderConnectionTestFailed
	}
	return connection, nil
}

func (s *ManagementService) DiscoverModels(
	ctx context.Context, command DiscoverModelsCommand,
) ([]ModelProfile, error) {
	target, err := s.authorize(command.Principal)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(command.ID) == "" || command.ExpectedVersion < 1 || strings.TrimSpace(command.Reason) == "" || s.operations == nil {
		return nil, ErrInvalidProviderManagement
	}
	connection, err := s.repository.GetConnection(ctx, target, command.ID)
	if err != nil {
		return nil, err
	}
	if err := object.RequireVersion(connection.Version, command.ExpectedVersion); err != nil {
		return nil, err
	}
	discovered, err := s.operations.Discover(ctx, connection)
	if err != nil {
		return nil, ErrProviderConnectionTestFailed
	}
	models := make([]ModelProfile, 0, len(discovered))
	seenProviderModels := make(map[string]struct{}, len(discovered))
	for _, item := range discovered {
		if strings.TrimSpace(item.ProviderModelID) == "" || strings.TrimSpace(item.DisplayName) == "" || item.ContextLimit < 1 {
			return nil, ErrInvalidProviderConfiguration
		}
		providerModelID := strings.TrimSpace(item.ProviderModelID)
		if _, duplicate := seenProviderModels[providerModelID]; duplicate {
			return nil, ErrInvalidProviderConfiguration
		}
		seenProviderModels[providerModelID] = struct{}{}
		models = append(models, ModelProfile{
			ID: s.newID(), MSPID: target.MSPID, ConnectionID: connection.ID,
			ProviderModelID: providerModelID, DisplayName: strings.TrimSpace(item.DisplayName),
			ContextLimit: item.ContextLimit, OutputLimit: item.ContextLimit, Enabled: false, Version: 1,
		})
	}
	now := s.now().UTC()
	audit, event := s.facts(command.Principal, target, now, "ai.provider_connection.models_reconciled", "ai_provider_connection", connection.ID, connection.Version, command.Reason)
	accepted := ModelReconciliationMutation{
		ConnectionID: connection.ID, MSPID: target.MSPID, ConnectionVersion: command.ExpectedVersion, Models: models,
		Audit: audit, Event: event,
	}
	persisted, err := s.repository.ReconcileModels(ctx, accepted)
	if err != nil {
		return nil, err
	}
	if !matchesReconciledModels(accepted, persisted) {
		return nil, ErrProviderRepositoryContract
	}
	return append([]ModelProfile(nil), persisted...), nil
}

func matchesReconciledModels(accepted ModelReconciliationMutation, persisted []ModelProfile) bool {
	if len(persisted) != len(accepted.Models) {
		return false
	}
	acceptedByProviderModel := make(map[string]ModelProfile, len(accepted.Models))
	for _, model := range accepted.Models {
		if _, duplicate := acceptedByProviderModel[model.ProviderModelID]; duplicate {
			return false
		}
		acceptedByProviderModel[model.ProviderModelID] = model
	}
	seen := make(map[string]struct{}, len(persisted))
	for _, model := range persisted {
		acceptedModel, found := acceptedByProviderModel[model.ProviderModelID]
		if !found || model.MSPID != accepted.MSPID || model.ConnectionID != accepted.ConnectionID ||
			model.DisplayName != acceptedModel.DisplayName || model.ContextLimit != acceptedModel.ContextLimit ||
			ValidateModelProfile(model) != nil {
			return false
		}
		if _, duplicate := seen[model.ProviderModelID]; duplicate {
			return false
		}
		seen[model.ProviderModelID] = struct{}{}
	}
	return true
}

func (s *ManagementService) UpdateModel(
	ctx context.Context, command UpdateModelCommand,
) (ModelProfile, error) {
	models, err := s.UpdateModels(ctx, UpdateModelsCommand{
		Principal: command.Principal, Reason: command.Reason,
		Updates: []ModelUpdate{{Model: command.Model, ExpectedVersion: command.ExpectedVersion}},
	})
	if err != nil {
		return ModelProfile{}, err
	}
	return models[0], nil
}

// UpdateModels is the collection operation consumed by the management HTTP
// seam. The singular method remains the repository-aligned building block.
func (s *ManagementService) UpdateModels(
	ctx context.Context, command UpdateModelsCommand,
) ([]ModelProfile, error) {
	target, err := s.authorize(command.Principal)
	if err != nil {
		return nil, err
	}
	if len(command.Updates) == 0 || strings.TrimSpace(command.Reason) == "" {
		return nil, ErrInvalidProviderManagement
	}
	now := s.now().UTC()
	updatesByID := make(map[string]ModelUpdate, len(command.Updates))
	modelIDs := make([]string, 0, len(command.Updates))
	seenIDs := make(map[string]struct{}, len(command.Updates))
	for _, update := range command.Updates {
		modelID := strings.TrimSpace(update.Model.ID)
		if modelID == "" || update.ExpectedVersion < 1 {
			return nil, ErrInvalidProviderManagement
		}
		if _, duplicate := seenIDs[modelID]; duplicate {
			return nil, ErrInvalidProviderManagement
		}
		seenIDs[modelID] = struct{}{}
		update.Model.ID = modelID
		updatesByID[modelID] = update
		modelIDs = append(modelIDs, modelID)
	}
	stored, err := s.repository.FindModels(ctx, target, modelIDs)
	if err != nil {
		return nil, err
	}
	if len(stored) != len(modelIDs) {
		return nil, ErrProviderRepositoryContract
	}
	storedByID := make(map[string]ModelProfile, len(stored))
	for _, model := range stored {
		if strings.TrimSpace(model.ID) == "" || model.MSPID != target.MSPID {
			return nil, ErrProviderRepositoryContract
		}
		if _, duplicate := storedByID[model.ID]; duplicate {
			return nil, ErrProviderRepositoryContract
		}
		storedByID[model.ID] = model
	}
	mutations := make([]ModelMutation, 0, len(command.Updates))
	for _, modelID := range modelIDs {
		update := updatesByID[modelID]
		storedModel, found := storedByID[modelID]
		if !found || object.RequireVersion(storedModel.Version, update.ExpectedVersion) != nil {
			return nil, ErrProviderRepositoryContract
		}
		model := update.Model
		model.ID, model.MSPID = storedModel.ID, storedModel.MSPID
		model.ConnectionID, model.ProviderModelID = storedModel.ConnectionID, storedModel.ProviderModelID
		model.Version = update.ExpectedVersion + 1
		if err := ValidateModelProfile(model); err != nil {
			return nil, err
		}
		audit, event := s.facts(command.Principal, target, now, "ai.model_profile.updated", "ai_model_profile", model.ID, model.Version, command.Reason)
		mutations = append(mutations, ModelMutation{
			Model: model, ExpectedVersion: update.ExpectedVersion, Audit: audit, Event: event,
		})
	}
	batch := ModelBatchMutation{MSPID: target.MSPID, Mutations: mutations}
	models, err := s.repository.UpdateModels(ctx, batch)
	if err != nil {
		return nil, err
	}
	if !matchesUpdatedModels(batch, models) {
		return nil, ErrProviderRepositoryContract
	}
	return append([]ModelProfile(nil), models...), nil
}

func matchesUpdatedModels(batch ModelBatchMutation, models []ModelProfile) bool {
	if len(models) != len(batch.Mutations) {
		return false
	}
	mutationsByID := make(map[string]ModelProfile, len(batch.Mutations))
	for _, mutation := range batch.Mutations {
		if _, duplicate := mutationsByID[mutation.Model.ID]; duplicate {
			return false
		}
		mutationsByID[mutation.Model.ID] = mutation.Model
	}
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		expected, found := mutationsByID[model.ID]
		if !found {
			return false
		}
		if _, duplicate := seen[model.ID]; duplicate ||
			model.MSPID != batch.MSPID || !reflect.DeepEqual(model, expected) {
			return false
		}
		seen[model.ID] = struct{}{}
	}
	return true
}

func (s *ManagementService) GetPolicy(ctx context.Context, command GetPolicyCommand) (Policy, error) {
	target, err := s.authorize(command.Principal)
	if err != nil {
		return Policy{}, err
	}
	policy, err := s.repository.GetPolicy(ctx, target)
	if errors.Is(err, scope.ErrNotFound) {
		return Policy{
			MSPID: target.MSPID, PromptVersion: "ai-v1", Version: 0,
		}, nil
	}
	return policy, err
}

func (s *ManagementService) UpdatePolicy(
	ctx context.Context, command UpdatePolicyCommand,
) (Policy, error) {
	target, err := s.authorize(command.Principal)
	if err != nil {
		return Policy{}, err
	}
	if command.ExpectedVersion < 0 || strings.TrimSpace(command.Reason) == "" {
		return Policy{}, ErrInvalidProviderManagement
	}
	current, err := s.repository.GetPolicy(ctx, target)
	created := errors.Is(err, scope.ErrNotFound)
	if err != nil && !created {
		return Policy{}, err
	}
	if created {
		if command.ExpectedVersion != 0 {
			return Policy{}, scope.ErrNotFound
		}
		current = Policy{MSPID: target.MSPID}
	} else if err := object.RequireVersion(current.Version, command.ExpectedVersion); err != nil {
		return Policy{}, err
	}
	policy := command.Policy
	policy.MSPID, policy.Version = target.MSPID, current.Version+1
	if err := ValidateManagementPolicy(policy); err != nil {
		return Policy{}, err
	}
	if err := s.validatePolicyModels(ctx, target, policy); err != nil {
		return Policy{}, err
	}
	now := s.now().UTC()
	audit, event := s.facts(command.Principal, target, now, "ai.policy.updated", "ai_policy", target.MSPID, policy.Version, command.Reason)
	accepted := PolicyMutation{
		Policy: policy, ExpectedVersion: command.ExpectedVersion, Created: created,
		Audit: audit, Event: event,
	}
	if err := s.repository.UpdatePolicy(ctx, accepted); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

func (s *ManagementService) validatePolicyModels(ctx context.Context, target scope.Target, policy Policy) error {
	if !policy.Enabled {
		return nil
	}
	if !policy.ProviderDisclosureAccepted {
		return ErrInvalidProviderConfiguration
	}
	required := make(map[string][]Feature, len(policy.AllowedFeatures))
	for _, feature := range policy.AllowedFeatures {
		var id string
		switch feature {
		case FeatureSummary:
			id = policy.SummaryModelProfileID
		case FeatureReplyDraft:
			id = policy.ReplyDraftModelProfileID
		case FeatureSimilar:
			id = policy.SimilarSuggestionsModelProfileID
		case FeatureCalendarRecommendation:
			id = policy.CalendarRecommendationModelProfileID
		}
		required[id] = append(required[id], feature)
	}
	ids := make([]string, 0, len(required))
	for id := range required {
		ids = append(ids, id)
	}
	models, err := s.repository.FindModels(ctx, target, ids)
	if err != nil || len(models) != len(ids) {
		return ErrInvalidProviderConfiguration
	}
	connections := make(map[string]ProviderConnection, len(models))
	for _, model := range models {
		features, found := required[model.ID]
		if !found || !model.Enabled {
			return ErrInvalidProviderConfiguration
		}
		for _, feature := range features {
			if !modelSupportsFeature(model, feature) {
				return ErrInvalidProviderConfiguration
			}
		}
		connection, found := connections[model.ConnectionID]
		if !found {
			connection, err = s.repository.GetConnection(ctx, target, model.ConnectionID)
			if err != nil {
				return ErrInvalidProviderConfiguration
			}
			connections[model.ConnectionID] = connection
		}
		if !connection.Enabled {
			return ErrInvalidProviderConfiguration
		}
	}
	return nil
}

func modelSupportsFeature(model ModelProfile, feature Feature) bool {
	for _, supported := range model.SupportedFeatures {
		if supported == feature {
			return true
		}
	}
	return false
}

func ValidateManagementPolicy(policy Policy) error {
	if strings.TrimSpace(policy.MSPID) == "" || policy.Version < 1 || !validFeatures(policy.AllowedFeatures) {
		return ErrInvalidProviderConfiguration
	}
	if !policy.Enabled {
		return nil
	}
	if len(policy.AllowedFeatures) == 0 {
		return ErrInvalidProviderConfiguration
	}
	if !policy.ProviderDisclosureAccepted {
		return ErrInvalidProviderConfiguration
	}
	for _, feature := range policy.AllowedFeatures {
		if (feature == FeatureSummary && strings.TrimSpace(policy.SummaryModelProfileID) == "") ||
			(feature == FeatureReplyDraft && strings.TrimSpace(policy.ReplyDraftModelProfileID) == "") ||
			(feature == FeatureSimilar && strings.TrimSpace(policy.SimilarSuggestionsModelProfileID) == "") ||
			(feature == FeatureCalendarRecommendation && strings.TrimSpace(policy.CalendarRecommendationModelProfileID) == "") {
			return ErrInvalidProviderConfiguration
		}
	}
	return nil
}

func (s *ManagementService) authorize(principal authorization.Principal) (scope.Target, error) {
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || strings.TrimSpace(principal.ID) == "" {
		return scope.Target{}, ErrInvalidProviderManagement
	}
	target := scope.Target{MSPID: principal.Scope.MSPID}
	if target.MSPID == "" {
		return scope.Target{}, ErrInvalidProviderManagement
	}
	if err := authorization.Authorize(principal, "ai.manage", target); err != nil {
		return scope.Target{}, err
	}
	return target, nil
}

func (s *ManagementService) facts(principal authorization.Principal, target scope.Target, now time.Time, action, subjectType, subjectID string, version int64, reason string) (mutation.AuditRecord, mutation.EventRecord) {
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	return mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ActorType: "technician", ActorID: principal.ID,
			Action: action, SubjectType: subjectType, SubjectID: subjectID, SubjectVersion: version,
			Source: "api", Reason: reason, CorrelationID: correlationID,
		}, mutation.EventRecord{
			EventID: eventID, EventType: action, SchemaVersion: 1, OccurredAt: now,
			MSPID: target.MSPID, ActorType: "technician", ActorID: principal.ID,
			SubjectType: subjectType, SubjectID: subjectID, SubjectVersion: version,
			Source: "api", CorrelationID: correlationID,
		}
}

func cloneBytes(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	return append([]byte(nil), value...)
}

func wipePlaintext(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

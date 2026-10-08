package psa

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
)

func TestNormalizeAIManagementWriteErrorClassifiesReferencedProvider(t *testing.T) {
	err := normalizeAIManagementWriteError(&pgconn.PgError{
		Code:    "P0001",
		Message: "cannot disable or remove disclosure acceptance from an AI provider connection while enabled policies or active jobs reference it",
	})
	if !errors.Is(err, aiassist.ErrProviderInUse) {
		t.Fatalf("error=%v", err)
	}
	other := &pgconn.PgError{Code: "23505", ConstraintName: "ai_provider_connections_pkey"}
	if normalized := normalizeAIManagementWriteError(other); normalized != other {
		t.Fatalf("unexpected normalization: %v", normalized)
	}
	wrappedText := errors.New("transaction commit failed: cannot disable or remove disclosure acceptance from an AI provider connection while enabled policies or active jobs reference it (SQLSTATE P0001)")
	if normalized := normalizeAIManagementWriteError(wrappedText); !errors.Is(normalized, aiassist.ErrProviderInUse) {
		t.Fatalf("wrapped text error=%v", normalized)
	}
}

func TestAIManagementRepositoryClassifiesReferencedProviderCommitFailure(t *testing.T) {
	tx := &fakeSalesTx{commitErr: &pgconn.PgError{
		Code:    "P0001",
		Message: "cannot disable or remove disclosure acceptance from an AI provider connection while enabled policies or active jobs reference it",
	}}
	mutation := validAIConnectionMutation()
	mutation.ExpectedVersion = 1
	err := NewAIManagementRepository(
		&fakeSalesDB{tx: tx},
		&recordingSecretProvider{},
		func() string { return "id" },
	).UpdateConnection(context.Background(), mutation)
	if !errors.Is(err, aiassist.ErrProviderInUse) {
		t.Fatalf("error=%v", err)
	}
}

type recordingSecretProvider struct {
	purpose string
	plain   []byte
	open    []byte
	err     error
}

func (p *recordingSecretProvider) Seal(_ context.Context, purpose string, plain []byte) (secrets.SealedValue, error) {
	p.purpose, p.plain = purpose, append([]byte(nil), plain...)
	if p.err != nil {
		return secrets.SealedValue{}, p.err
	}
	return secrets.SealedValue{Version: 7, Nonce: []byte("nonce"), Ciphertext: []byte("sealed")}, nil
}

func credentialZeroed(value []byte) bool {
	for _, byteValue := range value {
		if byteValue != 0 {
			return false
		}
	}
	return true
}

func (p *recordingSecretProvider) Open(_ context.Context, purpose string, _ secrets.SealedValue) ([]byte, error) {
	if p.open == nil {
		return nil, errors.New("unexpected Open")
	}
	p.purpose = purpose
	return append([]byte(nil), p.open...), nil
}

func validProviderConnection() aiassist.ProviderConnection {
	return aiassist.ProviderConnection{
		ID: "connection-id", MSPID: "msp-id", Name: "Primary model service",
		Adapter: aiassist.AdapterOpenAICompatible, Network: aiassist.NetworkRemote,
		BaseURL: "https://models.example.test", Timeout: 5 * time.Minute,
		RequestLimitBytes: 1 << 20, ResponseLimitBytes: 5 << 20,
		Health: aiassist.HealthPending, Version: 1,
	}
}

func validAIConnectionMutation() aiassist.ConnectionMutation {
	at := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	connection := validProviderConnection()
	return aiassist.ConnectionMutation{
		Connection: connection, PlaintextCredential: []byte("synthetic-api-key"),
		Audit: validAudit(at, "ai.provider_connection.created", "ai_provider_connection", connection.ID),
		Event: validEvent(at, "ai.provider_connection.created", "ai_provider_connection", connection.ID),
	}
}

func TestAIManagementRepositorySealsCredentialAndWritesFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	sealer := &recordingSecretProvider{}
	repository := NewAIManagementRepository(&fakeSalesDB{tx: tx}, sealer, func() string { return "id" })

	err := repository.CreateConnection(context.Background(), validAIConnectionMutation())

	if err != nil {
		t.Fatalf("CreateConnection() error=%v", err)
	}
	if sealer.purpose != "ai.provider.connection-id" {
		t.Fatalf("purpose=%q", sealer.purpose)
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("transaction committed=%v rolledBack=%v", tx.committed, tx.rolledBack)
	}
	assertQueryOrder(t, tx.queries,
		"pg_advisory_xact_lock", "INSERT INTO ai_provider_connections", "INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	for _, args := range tx.args {
		if fmt.Sprint(args) == "[synthetic-api-key]" || fmt.Sprint(args) == "synthetic-api-key" {
			t.Fatal("plaintext credential reached SQL")
		}
	}
}

func TestAIManagementRepositoryCreateLocalConnectionPersistsTrustedAcknowledgementActor(t *testing.T) {
	tx := &fakeSalesTx{}
	mutation := validAIConnectionMutation()
	acknowledged := time.Date(2026, time.July, 29, 12, 15, 0, 0, time.UTC)
	mutation.Connection.Network = aiassist.NetworkLocal
	mutation.Connection.Adapter = aiassist.AdapterOllama
	mutation.Connection.BaseURL = "http://127.0.0.1:11434"
	mutation.Connection.LocalNetworkAcknowledgedAt = &acknowledged

	err := NewAIManagementRepository(&fakeSalesDB{tx: tx}, &recordingSecretProvider{}, func() string { return "id" }).CreateConnection(context.Background(), mutation)

	if err != nil {
		t.Fatalf("CreateConnection() error=%v", err)
	}
	if !strings.Contains(tx.queries[0], "pg_advisory_xact_lock") || !strings.Contains(tx.queries[1], "local_network_acknowledged_at, local_network_acknowledged_by") {
		t.Fatalf("create does not take lock before acknowledgement pair: %s", tx.queries)
	}
	if !strings.Contains(
		tx.queries[1],
		"CASE WHEN $5 = 'local' THEN $14::timestamptz ELSE NULL END",
	) {
		t.Fatal("local acknowledgement timestamp parameter is not typed")
	}
	if got := tx.args[1][14]; got != mutation.Audit.ActorID {
		t.Fatalf("acknowledged_by=%v, want trusted audit actor %q", got, mutation.Audit.ActorID)
	}
}

func TestAIManagementRepositoryGetConnectionIsMSPScopedAndEnumerationSafe(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: errors.New("no row")}}
	_, _ = NewAIManagementRepository(db, &recordingSecretProvider{}, func() string { return "id" }).GetConnection(
		context.Background(), scope.Target{MSPID: "msp-id"}, "connection-id",
	)
	if !strings.Contains(db.query, "WHERE id = $1 AND msp_id = $2") {
		t.Fatalf("connection lookup is not MSP scoped: %s", db.query)
	}

	db.queryRow = fakeRow{err: pgxNoRows()}
	_, err := NewAIManagementRepository(db, &recordingSecretProvider{}, func() string { return "id" }).GetConnection(
		context.Background(), scope.Target{MSPID: "msp-id"}, "connection-id",
	)
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("GetConnection() error=%v", err)
	}
}

type repositoryCalendarControlPlane struct {
	repository *AIManagementRepository
	model      aiassist.ModelProfile
	connection aiassist.ProviderConnection
}

func (c repositoryCalendarControlPlane) GetPolicy(ctx context.Context, target scope.Target) (aiassist.Policy, error) {
	return c.repository.GetPolicy(ctx, target)
}

func (c repositoryCalendarControlPlane) FindModels(context.Context, scope.Target, []string) ([]aiassist.ModelProfile, error) {
	return []aiassist.ModelProfile{c.model}, nil
}

func (c repositoryCalendarControlPlane) GetConnection(context.Context, scope.Target, string) (aiassist.ProviderConnection, error) {
	return c.connection, nil
}

type repositoryCalendarAdapter struct {
	calls         int
	promptVersion string
}

func (*repositoryCalendarAdapter) Type() aiassist.AdapterType {
	return aiassist.AdapterOpenAICompatible
}
func (*repositoryCalendarAdapter) Discover(context.Context, aiassist.ProviderConnection, []byte) ([]aiassist.DiscoveredModel, error) {
	return nil, nil
}
func (a *repositoryCalendarAdapter) Generate(_ context.Context, _ aiassist.ProviderConnection, _ aiassist.ModelProfile, request aiassist.ProviderRequest, _ []byte) (aiassist.GenerationResult, error) {
	a.calls++
	a.promptVersion = request.PromptVersion
	return aiassist.GenerationResult{CalendarCandidates: []aiassist.CalendarProviderCandidate{}}, nil
}

func TestAIManagementRepositoryPolicyComposesWithGovernedCalendarProviderPrompt(t *testing.T) {
	disclosureAt := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(values ...any) {
		*(values[0].(*string)) = "msp"
		*(values[1].(*bool)) = true
		*(values[2].(*[]string)) = []string{string(aiassist.FeatureCalendarRecommendation)}
		for index := 3; index <= 6; index++ {
			*(values[index].(*string)) = ""
		}
		*(values[7].(*string)) = "calendar-model"
		*(values[8].(*bool)) = false
		*(values[9].(*bool)) = false
		*(values[10].(*int64)) = 0
		*(values[11].(*int64)) = 1
		*(values[12].(*bool)) = true
	}}}
	repository := NewAIManagementRepository(db, &recordingSecretProvider{}, func() string { return "id" })
	adapter := &repositoryCalendarAdapter{}
	registry, err := aiassist.NewAdapterRegistry(adapter)
	if err != nil {
		t.Fatal(err)
	}
	control := repositoryCalendarControlPlane{
		repository: repository,
		model: aiassist.ModelProfile{
			ID: "calendar-model", MSPID: "msp", ConnectionID: "connection", ProviderModelID: "provider-model",
			DisplayName: "Calendar", SupportedFeatures: []aiassist.Feature{aiassist.FeatureCalendarRecommendation},
			ContextLimit: 4096, OutputLimit: 512, ZeroCost: true, Enabled: true, Version: 1,
		},
		connection: aiassist.ProviderConnection{
			ID: "connection", MSPID: "msp", Name: "Provider", Adapter: aiassist.AdapterOpenAICompatible,
			Network: aiassist.NetworkRemote, BaseURL: "https://ai.example.test", Enabled: true,
			Timeout: time.Minute, RequestLimitBytes: 1 << 20, ResponseLimitBytes: 1 << 20,
			DisclosureAcceptedAt: &disclosureAt, Health: aiassist.HealthHealthy, Version: 1,
		},
	}
	provider := aiassist.NewGovernedCalendarProvider(control, registry, nil)
	_, err = provider.RecommendCalendar(context.Background(), aiassist.CalendarRecommendationContext{
		MSPID: "msp", ProjectionID: "projection", AuthorizedTechnicianIDs: []string{"tech"},
	})
	if err != nil || adapter.calls != 1 || adapter.promptVersion != "ai-v1" {
		t.Fatalf("error=%v calls=%d prompt version=%q", err, adapter.calls, adapter.promptVersion)
	}
}

func TestAIManagementRepositoryOpensManagementCredentialOnlyForExactConnectionSnapshot(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(*int)) = 1
		*(destinations[1].(*[]byte)) = []byte("nonce")
		*(destinations[2].(*[]byte)) = []byte("ciphertext")
	}}}
	provider := &recordingSecretProvider{open: []byte("management-secret")}
	connection := validProviderConnection()
	connection.CredentialConfigured = true
	var observed []byte
	err := NewAIManagementRepository(db, provider, func() string { return "id" }).UseCredential(
		context.Background(), connection, func(credential []byte) error {
			observed = append([]byte(nil), credential...)
			return nil
		},
	)
	if err != nil || string(observed) != "management-secret" ||
		provider.purpose != "ai.provider.connection-id" {
		t.Fatalf("UseCredential() observed=%q purpose=%q error=%v", observed, provider.purpose, err)
	}
	for _, required := range []string{
		"connection.version = $3", "connection.base_url = $4", "connection.network_mode = $5",
	} {
		if !strings.Contains(db.query, required) {
			t.Fatalf("credential lookup omitted snapshot fence %q: %s", required, db.query)
		}
	}
}

func TestAIManagementRepositoryReplaceCredentialSealsOrClearsWithoutPlaintextSQL(t *testing.T) {
	for _, test := range []struct {
		name       string
		credential []byte
		wantNulls  bool
	}{
		{name: "replacement", credential: []byte("next-key")},
		{name: "clearing", credential: nil, wantNulls: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeSalesTx{}
			sealer := &recordingSecretProvider{}
			repository := NewAIManagementRepository(&fakeSalesDB{tx: tx}, sealer, func() string { return "id" })
			mutation := validAIConnectionMutation()
			err := repository.ReplaceCredential(context.Background(), aiassist.CredentialMutation{
				ConnectionID: "connection-id", MSPID: "msp-id", ExpectedVersion: 4,
				PlaintextCredential: test.credential, Audit: mutation.Audit, Event: mutation.Event,
			})
			if err != nil {
				t.Fatalf("ReplaceCredential() error=%v", err)
			}
			if !test.wantNulls && sealer.purpose != "ai.provider.connection-id" {
				t.Fatalf("purpose=%q", sealer.purpose)
			}
			if !strings.Contains(tx.queries[0], "pg_advisory_xact_lock") || !strings.Contains(tx.queries[1], "id = $1 AND msp_id = $2 AND version = $3") {
				t.Fatalf("missing advisory lock or optimistic MSP predicate: %s", tx.queries)
			}
			if test.wantNulls && (tx.args[1][3] != nil || tx.args[1][4] != nil || tx.args[1][5] != nil) {
				t.Fatalf("clear credential args=%#v", tx.args[1])
			}
			for _, args := range tx.args {
				if fmt.Sprint(args) == "[next-key]" {
					t.Fatal("plaintext credential reached SQL")
				}
			}
		})
	}
}

func TestAIManagementRepositoryWipesOwnedCredentialOnSuccessAndErrors(t *testing.T) {
	for _, test := range []struct {
		name      string
		invoke    func(*AIManagementRepository, []byte) error
		sealerErr error
		tx        *fakeSalesTx
	}{
		{name: "create success", invoke: func(repository *AIManagementRepository, credential []byte) error {
			mutation := validAIConnectionMutation()
			mutation.PlaintextCredential = credential
			return repository.CreateConnection(context.Background(), mutation)
		}, tx: &fakeSalesTx{}},
		{name: "replace SQL error", invoke: func(repository *AIManagementRepository, credential []byte) error {
			mutation := validAIConnectionMutation()
			return repository.ReplaceCredential(context.Background(), aiassist.CredentialMutation{ConnectionID: mutation.Connection.ID, MSPID: mutation.Connection.MSPID, ExpectedVersion: 1, PlaintextCredential: credential, Audit: mutation.Audit, Event: mutation.Event})
		}, tx: &fakeSalesTx{failAt: 2}},
		{name: "seal error", invoke: func(repository *AIManagementRepository, credential []byte) error {
			mutation := validAIConnectionMutation()
			mutation.PlaintextCredential = credential
			return repository.CreateConnection(context.Background(), mutation)
		}, sealerErr: errors.New("seal failed"), tx: &fakeSalesTx{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			credential := []byte("owned-credential")
			sealer := &recordingSecretProvider{err: test.sealerErr}
			repository := NewAIManagementRepository(&fakeSalesDB{tx: test.tx}, sealer, func() string { return "id" })
			_ = test.invoke(repository, credential)
			if !credentialZeroed(credential) {
				t.Fatalf("repository retained credential backing bytes: %q", credential)
			}
			if sealer.purpose != "ai.provider.connection-id" || string(sealer.plain) != "owned-credential" {
				t.Fatalf("Seal did not receive the source before wipe: provider=%+v", sealer)
			}
		})
	}
}

func TestAIManagementRepositoryVersionConflictRollsBackBeforeFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 2}
	mutation := validAIConnectionMutation()
	mutation.ExpectedVersion = 1
	acknowledged := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	mutation.Connection.Network = aiassist.NetworkLocal
	mutation.Connection.BaseURL = "http://127.0.0.1:11434"
	mutation.Connection.LocalNetworkAcknowledgedAt = &acknowledged

	err := NewAIManagementRepository(&fakeSalesDB{tx: tx}, &recordingSecretProvider{}, func() string { return "id" }).UpdateConnection(context.Background(), mutation)

	if !errors.Is(err, object.ErrVersionConflict) || len(tx.queries) != 2 || tx.committed || !tx.rolledBack {
		t.Fatalf("UpdateConnection() error=%v tx=%+v", err, tx)
	}
	if !strings.Contains(tx.queries[0], "pg_advisory_xact_lock") ||
		!strings.Contains(tx.queries[1], "local_network_acknowledged_at") ||
		!strings.Contains(tx.queries[1], "local_network_acknowledged_by") ||
		!strings.Contains(tx.queries[1], "id = $1 AND msp_id = $2 AND version = $3") {
		t.Fatalf("update did not take lock before acknowledgement/version mutation: %s", tx.queries)
	}
	if got := tx.args[1][11]; got != mutation.Audit.ActorID {
		t.Fatalf("acknowledged_by=%v, want trusted audit actor %q", got, mutation.Audit.ActorID)
	}
}

func TestAIManagementRepositoryPreservesAcknowledgementActorUntilFreshAcknowledgement(t *testing.T) {
	acknowledgedAt := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	adminA := "33333333-3333-4333-8333-333333333333"
	adminB := "44444444-4444-4444-8444-444444444444"
	create := validAIConnectionMutation()
	create.Audit.ActorID = adminA
	create.Connection.Network, create.Connection.Adapter = aiassist.NetworkLocal, aiassist.AdapterOllama
	create.Connection.BaseURL, create.Connection.LocalNetworkAcknowledgedAt = "http://127.0.0.1:11434", &acknowledgedAt

	createTx := &fakeSalesTx{}
	if err := NewAIManagementRepository(&fakeSalesDB{tx: createTx}, &recordingSecretProvider{}, func() string { return "id" }).CreateConnection(context.Background(), create); err != nil {
		t.Fatalf("CreateConnection() error=%v", err)
	}
	if got := createTx.args[1][14]; got != adminA {
		t.Fatalf("create acknowledged_by=%v, want %q", got, adminA)
	}

	rename := create
	rename.ExpectedVersion, rename.Connection.Version = 1, 2
	rename.Connection.Name, rename.Audit.ActorID = "Renamed local provider", adminB
	renameTx := &fakeSalesTx{}
	if err := NewAIManagementRepository(&fakeSalesDB{tx: renameTx}, &recordingSecretProvider{}, func() string { return "id" }).UpdateConnection(context.Background(), rename); err != nil {
		t.Fatalf("UpdateConnection() error=%v", err)
	}
	if !strings.Contains(renameTx.queries[1], "local_network_acknowledged_at IS NOT DISTINCT FROM $11::timestamptz") ||
		!strings.Contains(renameTx.queries[1], "THEN local_network_acknowledged_by") ||
		!strings.Contains(renameTx.queries[1], "ELSE $12::uuid") ||
		!strings.Contains(renameTx.queries[1], "disclosure_accepted_at = CASE") ||
		!strings.Contains(renameTx.queries[1], "adapter_type IS NOT DISTINCT FROM $5") ||
		renameTx.args[1][11] != adminB {
		t.Fatalf("ordinary local edit can replace Admin A acknowledgement: query=%s args=%#v", renameTx.queries[1], renameTx.args[1])
	}

	freshAcknowledgement := rename
	freshAt := acknowledgedAt.Add(time.Minute)
	freshAcknowledgement.Connection.BaseURL = "http://127.0.0.1:11435"
	freshAcknowledgement.Connection.LocalNetworkAcknowledgedAt = &freshAt
	freshTx := &fakeSalesTx{}
	if err := NewAIManagementRepository(&fakeSalesDB{tx: freshTx}, &recordingSecretProvider{}, func() string { return "id" }).UpdateConnection(context.Background(), freshAcknowledgement); err != nil {
		t.Fatalf("UpdateConnection() fresh acknowledgement error=%v", err)
	}
	if freshTx.args[1][10] != &freshAt || freshTx.args[1][11] != adminB || !strings.Contains(freshTx.queries[1], "ELSE $12::uuid") {
		t.Fatalf("fresh acknowledgement does not record Admin B: query=%s args=%#v", freshTx.queries[1], freshTx.args[1])
	}

	remote := rename
	remote.Connection.Network, remote.Connection.BaseURL = aiassist.NetworkRemote, "https://models.example.test"
	remote.Connection.LocalNetworkAcknowledgedAt = nil
	remoteTx := &fakeSalesTx{}
	if err := NewAIManagementRepository(&fakeSalesDB{tx: remoteTx}, &recordingSecretProvider{}, func() string { return "id" }).UpdateConnection(context.Background(), remote); err != nil {
		t.Fatalf("UpdateConnection() remote error=%v", err)
	}
	if !strings.Contains(remoteTx.queries[1], "CASE WHEN $6 = 'local' THEN $11::timestamptz ELSE NULL END") ||
		!strings.Contains(remoteTx.queries[1], "WHEN $6 <> 'local' OR $11::timestamptz IS NULL THEN NULL") {
		t.Fatalf("remote transition does not clear acknowledgement pair: %s", remoteTx.queries[1])
	}
}

func TestAIManagementRepositoryReconcilesModelsDisabledAndWritesFactsAtomically(t *testing.T) {
	persisted := validModel("persisted-model-id", 7)
	persisted.Enabled = false
	tx := &aiReconcileTx{returned: []aiassist.ModelProfile{persisted}}
	model := aiassist.ModelProfile{
		ID: "fresh-model-id", MSPID: "msp-id", ConnectionID: "connection-id", ProviderModelID: "persisted-model-id",
		DisplayName: "Provider model", ContextLimit: 8192, OutputLimit: 8192, Enabled: true, Version: 1,
	}
	mutation := validAIConnectionMutation()
	models, err := NewAIManagementRepository(&aiReconcileDB{tx: tx}, &recordingSecretProvider{}, func() string { return "id" }).ReconcileModels(context.Background(), aiassist.ModelReconciliationMutation{
		ConnectionID: "connection-id", MSPID: "msp-id", ConnectionVersion: 2, Models: []aiassist.ModelProfile{model},
		Audit: mutation.Audit, Event: mutation.Event,
	})
	if err != nil {
		t.Fatalf("ReconcileModels() error=%v", err)
	}
	if len(models) != 1 || models[0].ID != persisted.ID || models[0].Version != persisted.Version || models[0].Enabled {
		t.Fatalf("reconciliation returned non-canonical model: %+v", models)
	}
	assertQueryOrder(t, tx.queries, "pg_advisory_xact_lock", "INSERT INTO ai_model_profiles", "INSERT INTO audit_ledger", "INSERT INTO event_outbox")
	if !strings.Contains(tx.queries[1], "zero_cost, enabled") ||
		!strings.Contains(tx.queries[1], "$10, false,") ||
		strings.Contains(tx.queries[1], "enabled = EXCLUDED.enabled") ||
		!strings.Contains(tx.queries[1], "version = $3") {
		t.Fatalf("reconciliation can auto-enable or lacks connection version: %s", tx.queries[1])
	}
}

func TestAIManagementRepositoryReconciliationReturnsNewPersistedDisabledModel(t *testing.T) {
	model := validModel("fresh-model-id", 1)
	model.Enabled = false
	tx := &aiReconcileTx{returned: []aiassist.ModelProfile{model}}
	mutation := validAIConnectionMutation()
	models, err := NewAIManagementRepository(&aiReconcileDB{tx: tx}, &recordingSecretProvider{}, func() string { return "id" }).ReconcileModels(context.Background(), aiassist.ModelReconciliationMutation{
		ConnectionID: model.ConnectionID, MSPID: model.MSPID, ConnectionVersion: 1, Models: []aiassist.ModelProfile{model},
		Audit: mutation.Audit, Event: mutation.Event,
	})
	if err != nil {
		t.Fatalf("ReconcileModels() error=%v", err)
	}
	if len(models) != 1 || models[0].ID != "fresh-model-id" || models[0].Enabled || models[0].Version != 1 {
		t.Fatalf("first discovery did not return persisted disabled model: %+v", models)
	}
}

func TestAIManagementRepositoryReconciliationBumpsVersionOnlyWhenTrustedDiscoveryChanges(t *testing.T) {
	model := validModel("fresh-model-id", 1)
	tx := &aiReconcileTx{returned: []aiassist.ModelProfile{model}}
	mutation := validAIConnectionMutation()
	_, err := NewAIManagementRepository(&aiReconcileDB{tx: tx}, &recordingSecretProvider{}, func() string { return "id" }).ReconcileModels(context.Background(), aiassist.ModelReconciliationMutation{
		ConnectionID: model.ConnectionID, MSPID: model.MSPID, ConnectionVersion: 1, Models: []aiassist.ModelProfile{model},
		Audit: mutation.Audit, Event: mutation.Event,
	})
	if err != nil {
		t.Fatalf("ReconcileModels() error=%v", err)
	}
	query := tx.queries[1]
	for _, required := range []string{
		"version = CASE", "display_name IS DISTINCT FROM EXCLUDED.display_name",
		"context_limit IS DISTINCT FROM EXCLUDED.context_limit", "version + 1", "ELSE ai_model_profiles.version",
	} {
		if !strings.Contains(query, required) {
			t.Fatalf("reconciliation does not make trusted discovery version-sensitive (%q): %s", required, query)
		}
	}
}

type aiReconcileTx struct {
	fakeSalesTx
	returned []aiassist.ModelProfile
	index    int
}

func (tx *aiReconcileTx) QueryRow(_ context.Context, query string, args ...any) row {
	tx.queries = append(tx.queries, query)
	tx.args = append(tx.args, args)
	if tx.index >= len(tx.returned) {
		return fakeRow{err: errors.New("unexpected reconciliation row")}
	}
	model := tx.returned[tx.index]
	tx.index++
	return fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*string) = model.ID
		*destinations[1].(*string) = model.MSPID
		*destinations[2].(*string) = model.ConnectionID
		*destinations[3].(*string) = model.ProviderModelID
		*destinations[4].(*string) = model.DisplayName
		features := make([]string, len(model.SupportedFeatures))
		for index, feature := range model.SupportedFeatures {
			features[index] = string(feature)
		}
		*destinations[5].(*[]string) = features
		*destinations[6].(*int64) = model.ContextLimit
		*destinations[7].(*int64) = model.OutputLimit
		*destinations[8].(*bool) = model.ZeroCost
		*destinations[9].(**int64) = model.InputCostPerMillionMinor
		*destinations[10].(**int64) = model.OutputCostPerMillionMinor
		*destinations[11].(*bool) = model.Enabled
		*destinations[12].(*int64) = model.Version
	}}
}

type aiReconcileDB struct{ tx *aiReconcileTx }

func (db *aiReconcileDB) Begin(context.Context) (transaction, error) { return db.tx, nil }
func (db *aiReconcileDB) Query(context.Context, string, ...any) (rows, error) {
	return nil, errors.New("unexpected Query")
}
func (db *aiReconcileDB) QueryRow(context.Context, string, ...any) row {
	return fakeRow{err: errors.New("unexpected QueryRow")}
}

func TestAIManagementRepositoryUpdateModelsLocksTrustedConnectionsInSortedOrder(t *testing.T) {
	tx := &aiLockTx{lockRows: []row{stringSliceRow("connection-z", "connection-a", "connection-z")}}
	mutation := validAIConnectionMutation()
	models := []aiassist.ModelMutation{
		{Model: validModel("model-one", 2), ExpectedVersion: 1, Audit: mutation.Audit, Event: mutation.Event},
		{Model: validModel("model-two", 2), ExpectedVersion: 1, Audit: mutation.Audit, Event: mutation.Event},
	}
	models[0].Model.ConnectionID = "caller-supplied-connection"

	_, err := NewAIManagementRepository(&aiLockDB{tx: tx}, &recordingSecretProvider{}, func() string { return "id" }).UpdateModels(
		context.Background(), aiassist.ModelBatchMutation{MSPID: "msp-id", Mutations: models},
	)
	if err != nil {
		t.Fatalf("UpdateModels() error=%v", err)
	}
	assertQueryOrder(t, tx.queries,
		"SELECT array_agg", "pg_advisory_xact_lock", "pg_advisory_xact_lock", "UPDATE ai_model_profiles",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox", "UPDATE ai_model_profiles",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	if got := []any{tx.args[1][1], tx.args[2][1]}; fmt.Sprint(got) != "[connection-a connection-z]" {
		t.Fatalf("lock order=%v, want trusted sorted connection IDs", got)
	}
}

func TestAIManagementRepositoryUpdateModelsRollsBackWholeBatchOnAnyMismatch(t *testing.T) {
	tx := &aiLockTx{fakeSalesTx: fakeSalesTx{zeroRowsAt: 3}, lockRows: []row{stringSliceRow("connection-id")}}
	mutation := validAIConnectionMutation()
	models := []aiassist.ModelMutation{
		{Model: validModel("model-one", 2), ExpectedVersion: 1, Audit: mutation.Audit, Event: mutation.Event},
		{Model: validModel("model-two", 2), ExpectedVersion: 1, Audit: mutation.Audit, Event: mutation.Event},
	}
	_, err := NewAIManagementRepository(&aiLockDB{tx: tx}, &recordingSecretProvider{}, func() string { return "id" }).UpdateModels(
		context.Background(), aiassist.ModelBatchMutation{MSPID: "msp-id", Mutations: models},
	)
	if !errors.Is(err, object.ErrVersionConflict) || tx.committed || !tx.rolledBack || len(tx.queries) != 3 {
		t.Fatalf("UpdateModels() error=%v tx=%+v", err, tx)
	}
	for _, query := range []string{tx.queries[2]} {
		if !strings.Contains(query, "id = $1 AND msp_id = $2 AND version = $3") {
			t.Fatalf("model mutation lacks exact optimistic predicate: %s", query)
		}
	}
}

func TestAIManagementRepositoryUpdatePolicyLocksCurrentAndRequestedConnectionsInSortedOrder(t *testing.T) {
	tx := &aiLockTx{lockRows: []row{
		policyModelIDsRow("current-model", "", ""),
		stringSliceRow("connection-z", "connection-a", "connection-a"),
		int64Row(1),
		int64Row(1),
	}}
	mutation := validAIConnectionMutation()
	policy := aiassist.Policy{
		MSPID: "msp-id", Enabled: true, AllowedFeatures: []aiassist.Feature{aiassist.FeatureSummary},
		SummaryModelProfileID: "requested-model", ClassificationModelProfileID: "must-not-be-written", ProviderDisclosureAccepted: true, Version: 2,
	}
	err := NewAIManagementRepository(&aiLockDB{tx: tx}, &recordingSecretProvider{}, func() string { return "id" }).UpdatePolicy(context.Background(), aiassist.PolicyMutation{
		Policy: policy, ExpectedVersion: 1, Audit: mutation.Audit, Event: mutation.Event,
	})
	if err != nil {
		t.Fatalf("UpdatePolicy() error=%v", err)
	}
	assertQueryOrder(t, tx.queries,
		"SELECT COALESCE(summary_model_profile_id", "SELECT array_agg", "pg_advisory_xact_lock", "pg_advisory_xact_lock", "SELECT version", "UPDATE ai_provider_connections connection", "JOIN ai_provider_connections", "UPDATE ai_policies", "INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	if got := []any{tx.args[2][1], tx.args[3][1]}; fmt.Sprint(got) != "[connection-a connection-z]" {
		t.Fatalf("lock order=%v, want current and requested trusted sorted connection IDs", got)
	}
	if !strings.Contains(tx.queries[5], "disclosure_accepted_at = $6") ||
		tx.args[5][6] != mutation.Audit.ActorID {
		t.Fatalf("selected provider disclosure was not stamped by the trusted actor: query=%s args=%v", tx.queries[5], tx.args[5])
	}
	if strings.Contains(tx.queries[7], "classification_model_profile_id") {
		t.Fatalf("generic policy mutation can write the canonical classification mapping: %s", tx.queries[7])
	}
}

func TestAIManagementRepositoryRecordConnectionHealthLocksBeforeVersionedUpdate(t *testing.T) {
	tx := &fakeSalesTx{}
	mutation := validAIConnectionMutation()
	err := NewAIManagementRepository(&fakeSalesDB{tx: tx}, &recordingSecretProvider{}, func() string { return "id" }).RecordConnectionHealth(context.Background(), aiassist.ConnectionHealthMutation{
		ConnectionID: "connection-id", MSPID: "msp-id", ExpectedVersion: 1, Health: aiassist.HealthHealthy,
		TestedAt: mutation.Audit.OccurredAt, SucceededAt: &mutation.Audit.OccurredAt, Audit: mutation.Audit, Event: mutation.Event,
	})
	if err != nil {
		t.Fatalf("RecordConnectionHealth() error=%v", err)
	}
	assertQueryOrder(t, tx.queries, "pg_advisory_xact_lock", "UPDATE ai_provider_connections", "INSERT INTO audit_ledger", "INSERT INTO event_outbox")
}

func TestAIManagementRepositoryUpdatePolicyChecksEnabledModelOwnershipBeforeFacts(t *testing.T) {
	tx := &aiLockTx{lockRows: []row{
		fakeRow{err: pgxNoRows()},
		stringSliceRow("connection-id"),
		int64Row(1),
		int64Row(1),
	}}
	mutation := validAIConnectionMutation()
	policy := aiassist.Policy{
		MSPID: "msp-id", Enabled: true, AllowedFeatures: []aiassist.Feature{aiassist.FeatureSummary},
		SummaryModelProfileID: "model-id", ProviderDisclosureAccepted: true, Version: 2,
	}
	err := NewAIManagementRepository(&aiLockDB{tx: tx}, &recordingSecretProvider{}, func() string { return "id" }).UpdatePolicy(context.Background(), aiassist.PolicyMutation{
		Policy: policy, ExpectedVersion: 1, Audit: mutation.Audit, Event: mutation.Event,
	})
	if err != nil {
		t.Fatalf("UpdatePolicy() error=%v", err)
	}
	if len(tx.queries) != 9 || !strings.Contains(tx.queries[5], "JOIN ai_provider_connections") ||
		!strings.Contains(tx.queries[5], "profile.enabled") ||
		!strings.Contains(tx.queries[5], "connection.enabled") ||
		!strings.Contains(tx.queries[5], "connection.disclosure_accepted_at IS NOT NULL") ||
		!strings.Contains(tx.queries[5], "selected.feature <> 'calendar_recommendation' OR profile.zero_cost") {
		t.Fatalf("policy did not validate enabled same-MSP model ownership: %#v", tx.queries)
	}
	assertQueryOrder(t, tx.queries[6:], "UPDATE ai_policies", "INSERT INTO audit_ledger", "INSERT INTO event_outbox")
}

func validModel(id string, version int64) aiassist.ModelProfile {
	return aiassist.ModelProfile{ID: id, MSPID: "msp-id", ConnectionID: "connection-id", ProviderModelID: id,
		DisplayName: id, SupportedFeatures: []aiassist.Feature{aiassist.FeatureSummary}, ContextLimit: 8192,
		OutputLimit: 2048, Enabled: true, Version: version}
}

type aiLockTx struct {
	fakeSalesTx
	lockRows []row
}

func (tx *aiLockTx) QueryRow(_ context.Context, query string, args ...any) row {
	tx.queries = append(tx.queries, query)
	tx.args = append(tx.args, args)
	if len(tx.lockRows) == 0 {
		return fakeRow{err: errors.New("unexpected lock query")}
	}
	result := tx.lockRows[0]
	tx.lockRows = tx.lockRows[1:]
	return result
}

type aiLockDB struct{ tx *aiLockTx }

func (db *aiLockDB) Begin(context.Context) (transaction, error) { return db.tx, nil }
func (db *aiLockDB) Query(context.Context, string, ...any) (rows, error) {
	return nil, errors.New("unexpected Query")
}
func (db *aiLockDB) QueryRow(context.Context, string, ...any) row {
	return fakeRow{err: errors.New("unexpected QueryRow")}
}

func stringSliceRow(values ...string) row {
	return fakeRow{scan: func(destinations ...any) { *destinations[0].(*[]string) = values }}
}

func policyModelIDsRow(summary, replyDraft, similar string) row {
	return fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*string) = summary
		*destinations[1].(*string) = replyDraft
		*destinations[2].(*string) = similar
	}}
}

func int64Row(value int64) row {
	return fakeRow{scan: func(destinations ...any) { *destinations[0].(*int64) = value }}
}

// pgx.ErrNoRows is kept behind this helper so the enumeration test reads as a
// repository behavior rather than a dependency detail.
func pgxNoRows() error { return pgx.ErrNoRows }

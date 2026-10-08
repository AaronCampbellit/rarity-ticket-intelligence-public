package aiassist

import (
	"context"
	"errors"
	"math"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestEstimateReservedCostMinorUsesCeilingAndRejectsUnknownPaidPricing(t *testing.T) {
	inputRate, outputRate := int64(3), int64(7)
	model := ModelProfile{InputCostPerMillionMinor: &inputRate, OutputCostPerMillionMinor: &outputRate}
	got, err := EstimateReservedCostMinor(model, 1, 1)
	if err != nil || got != 1 { // (3 + 7) / 1,000,000 rounds up.
		t.Fatalf("EstimateReservedCostMinor() = %d, %v", got, err)
	}
	if _, err := EstimateReservedCostMinor(ModelProfile{}, 1, 1); !errors.Is(err, ErrUnknownCost) {
		t.Fatalf("unknown paid pricing error = %v, want ErrUnknownCost", err)
	}
	if got, err := EstimateReservedCostMinor(ModelProfile{ZeroCost: true}, math.MaxInt64, math.MaxInt64); err != nil || got != 0 {
		t.Fatalf("zero-cost reservation = %d, %v", got, err)
	}
}

func TestExecutionFingerprintChangesForSameLengthContextAndTrustedSnapshotMutation(t *testing.T) {
	execution := JobExecution{
		Job:        GenerationJob{ID: "job", MSPID: "msp", ClientID: "client", WorkRecordID: "work", Feature: FeatureSummary},
		Policy:     Policy{Version: 7, PromptVersion: "prompt-v2"},
		Model:      ModelProfile{ID: "model", Version: 3, ProviderModelID: "model-v3", ContextLimit: 8192, OutputLimit: 512},
		Connection: ProviderConnection{ID: "connection", Version: 4, BaseURL: "http://ollama.example", Network: NetworkLocal},
		Context:    GenerationContext{Fields: []ContextField{{Name: "title", Value: "AAAA", Classification: ContextStandard}}, AuthorizedCandidateIDs: []string{"candidate-a"}},
		Snapshot:   ExecutionSnapshot{WorkUpdatedAt: time.Date(2026, time.July, 30, 1, 2, 3, 0, time.UTC), CandidateFingerprint: "candidate-snapshot"},
	}
	first := ExecutionFingerprint([]byte(`{"same":"bytes"}`), execution)
	execution.Context.Fields[0].Value = "BBBB" // same byte length, distinct disclosure.
	if second := ExecutionFingerprint([]byte(`{"same":"bytes"}`), execution); second == first {
		t.Fatal("same-length context mutation retained execution fingerprint")
	}
	execution.Context.Fields[0].Value = "AAAA"
	execution.Model.Version++
	if second := ExecutionFingerprint([]byte(`{"same":"bytes"}`), execution); second == first {
		t.Fatal("model version mutation retained execution fingerprint")
	}
	execution.Model.Version--
	firstRate, secondRate := int64(9), int64(9)
	execution.Model.InputCostPerMillionMinor = &firstRate
	withFirstPointer := ExecutionFingerprint([]byte(`{"same":"bytes"}`), execution)
	execution.Model.InputCostPerMillionMinor = &secondRate
	if withSecondPointer := ExecutionFingerprint([]byte(`{"same":"bytes"}`), execution); withSecondPointer != withFirstPointer {
		t.Fatal("equivalent model price values produced pointer-dependent fingerprints")
	}
}

func TestBuildRecommendationRecordTreatsUnknownActualCostAsCostLimitRejection(t *testing.T) {
	now := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	confidence := 0.8
	_, err := BuildRecommendationRecord(RecommendationCommand{
		Job:        GenerationJob{ID: "job", MSPID: "msp", ClientID: "client", WorkRecordID: "work", RequestedBy: "tech", Feature: FeatureSummary},
		Policy:     Policy{MSPID: "msp", Enabled: true, PromptVersion: "v1", AllowedFeatures: []Feature{FeatureSummary}, CostLimitEnabled: true},
		Model:      ModelProfile{ID: "model", MSPID: "msp", ConnectionID: "connection", ProviderModelID: "model", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 10_000, OutputLimit: 100, Enabled: true},
		Connection: ProviderConnection{ID: "connection", MSPID: "msp", Enabled: true, Adapter: AdapterOpenAICompatible},
		Context:    GenerationContext{Fields: []ContextField{{Name: "title", Value: "VPN", Classification: ContextStandard}}},
	}, GenerationResult{Text: "Draft", Confidence: &confidence}, now, RecommendationIDs{RecommendationID: "rec", UsageID: "use", AuditID: "audit", EventID: "event", CorrelationID: "correlation"})
	if !errors.Is(err, ErrCostLimitExceeded) {
		t.Fatalf("BuildRecommendationRecord() error=%v, want ErrCostLimitExceeded", err)
	}
}

func TestRetryableGenerationErrorOnlyRetriesTemporaryTransportFailures(t *testing.T) {
	for _, status := range []int{401, 403, 400, 404} {
		if retryableGenerationError(&ProviderHTTPError{StatusCode: status}) {
			t.Fatalf("status %d unexpectedly retryable", status)
		}
	}
	for _, status := range []int{429, 500, 503} {
		if !retryableGenerationError(&ProviderHTTPError{StatusCode: status}) {
			t.Fatalf("status %d unexpectedly terminal", status)
		}
	}
	if !retryableGenerationError(temporaryJobNetError{}) || retryableGenerationError(ErrUnsafeProviderEndpoint) {
		t.Fatal("retry classification did not preserve the narrow transport whitelist")
	}
	if !retryableGenerationError(&ProviderTransportError{Temporary: true}) || retryableGenerationError(&ProviderTransportError{}) {
		t.Fatal("body-safe transport transient state was not classified exactly")
	}
}

type temporaryJobNetError struct{}

func (temporaryJobNetError) Error() string   { return "dial failure" }
func (temporaryJobNetError) Timeout() bool   { return false }
func (temporaryJobNetError) Temporary() bool { return true }

var _ net.Error = temporaryJobNetError{}

type jobStoreStub struct {
	job GenerationJob
	err error
}

type jobQueueStub struct {
	jobs              []ClaimedGenerationJob
	execution         JobExecution
	completed         []JobCompletion
	purpose           string
	cancelled         bool
	failed            []JobFailure
	maintain          func() LeaseMaintenance
	reserveErr        error
	reserved          bool
	reservedExecution *JobExecution
	swept             int
	credential        []byte
}

func (q *jobQueueStub) MaintainLease(context.Context, string, string, time.Duration) (LeaseMaintenance, error) {
	if q.maintain != nil {
		return q.maintain(), nil
	}
	return LeaseMaintenance{Owned: true}, nil
}

func (q *jobQueueStub) Claim(context.Context, int, time.Duration, []JobTransitionIDs) ([]ClaimedGenerationJob, error) {
	return q.jobs, nil
}

func (q *jobQueueStub) SweepStranded(context.Context) (int, error) { return q.swept, nil }

func (q *jobQueueStub) LoadExecution(context.Context, string, string) (JobExecution, error) {
	return q.execution, nil
}

func (q *jobQueueStub) AuthorizeAndReserve(_ context.Context, _ string, _ string, _ string, _ int64, _ int64) (JobExecution, error) {
	q.reserved = true
	if q.reserveErr != nil {
		return JobExecution{}, q.reserveErr
	}
	if q.reservedExecution != nil {
		return *q.reservedExecution, nil
	}
	return q.execution, nil
}

func (q *jobQueueStub) CancellationRequested(context.Context, string, string) (bool, error) {
	return q.cancelled, nil
}

func (q *jobQueueStub) ExecuteWithCredential(_ context.Context, reference CredentialReference, use func([]byte) error) error {
	q.purpose = "ai.provider." + reference.ConnectionID
	return use(q.credential)
}

func (q *jobQueueStub) Complete(_ context.Context, completion JobCompletion) error {
	q.completed = append(q.completed, completion)
	return nil
}

func (q *jobQueueStub) Fail(_ context.Context, failure JobFailure) error {
	q.failed = append(q.failed, failure)
	return nil
}

type jobAdapterStub struct{}

func (jobAdapterStub) Type() AdapterType { return AdapterOpenAICompatible }
func (jobAdapterStub) Discover(context.Context, ProviderConnection, []byte) ([]DiscoveredModel, error) {
	return nil, nil
}
func (jobAdapterStub) Generate(context.Context, ProviderConnection, ModelProfile, ProviderRequest, []byte) (GenerationResult, error) {
	confidence := 0.8
	return GenerationResult{Text: "A safe draft", Confidence: &confidence, CandidateIDs: []string{"candidate-a"}}, nil
}

type credentialRecordingAdapter struct {
	credential []byte
	calls      int
}

func (a *credentialRecordingAdapter) Type() AdapterType { return AdapterOllama }
func (a *credentialRecordingAdapter) Discover(context.Context, ProviderConnection, []byte) ([]DiscoveredModel, error) {
	return nil, nil
}
func (a *credentialRecordingAdapter) Generate(_ context.Context, _ ProviderConnection, _ ModelProfile, _ ProviderRequest, credential []byte) (GenerationResult, error) {
	a.calls++
	a.credential = credential
	confidence := 0.8
	return GenerationResult{Text: "draft", Confidence: &confidence}, nil
}

func TestJobWorkerFailsClosedWhenModelChangesAfterReservation(t *testing.T) {
	now := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	execution := JobExecution{Job: GenerationJob{ID: "job", MSPID: "msp", ClientID: "client", WorkRecordID: "work", RequestedBy: "tech", Feature: FeatureSummary, State: JobRunning}, Policy: Policy{MSPID: "msp", Enabled: true, PromptVersion: "v1", AllowedFeatures: []Feature{FeatureSummary}, Version: 1}, Model: ModelProfile{ID: "model", MSPID: "msp", ConnectionID: "connection", ProviderModelID: "model", ContextLimit: 10000, OutputLimit: 10, Enabled: true, SupportedFeatures: []Feature{FeatureSummary}, Version: 1}, Connection: ProviderConnection{ID: "connection", MSPID: "msp", Adapter: AdapterOllama, Enabled: true, Version: 1}, Context: GenerationContext{Fields: []ContextField{{Name: "title", Value: "value", Classification: ContextStandard}}}}
	changed := execution
	changed.Model.Version = 2
	queue := &jobQueueStub{jobs: []ClaimedGenerationJob{{Job: execution.Job, LeaseToken: "lease"}}, execution: execution, reservedExecution: &changed}
	adapter := &credentialRecordingAdapter{}
	registry, _ := NewAdapterRegistry(adapter)
	worker := NewJobWorker(queue, registry, func() time.Time { return now }, sequenceIDs("audit", "event", "correlation", "failure-audit", "failure-event", "failure-correlation"))
	result, err := worker.RunOnce(context.Background(), 1)
	if err != nil || adapter.calls != 0 || queue.purpose != "" || len(queue.failed) != 1 || queue.failed[0].SafeErrorCode != "execution_changed" || result.Failed != 1 {
		t.Fatalf("result=%+v err=%v adapter=%d purpose=%q failures=%+v", result, err, adapter.calls, queue.purpose, queue.failed)
	}
}

func TestJobWorkerFencesCredentiallessProviderThroughExecutionCallback(t *testing.T) {
	now := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	queue := &jobQueueStub{jobs: []ClaimedGenerationJob{{Job: GenerationJob{ID: "job", MSPID: "msp", ClientID: "client", WorkRecordID: "work", RequestedBy: "tech", Feature: FeatureSummary, State: JobRunning}, LeaseToken: "lease"}}, execution: JobExecution{Job: GenerationJob{ID: "job", MSPID: "msp", ClientID: "client", WorkRecordID: "work", RequestedBy: "tech", Feature: FeatureSummary, State: JobRunning}, Policy: Policy{MSPID: "msp", Enabled: true, PromptVersion: "v1", AllowedFeatures: []Feature{FeatureSummary}}, Model: ModelProfile{ID: "model", MSPID: "msp", ConnectionID: "connection", ProviderModelID: "model", ContextLimit: 10000, OutputLimit: 10, Enabled: true, SupportedFeatures: []Feature{FeatureSummary}}, Connection: ProviderConnection{ID: "connection", MSPID: "msp", Adapter: AdapterOllama, Enabled: true, CredentialConfigured: false, Version: 1}, Context: GenerationContext{Fields: []ContextField{{Name: "title", Value: "value", Classification: ContextStandard}}}}}
	adapter := &credentialRecordingAdapter{}
	registry, _ := NewAdapterRegistry(adapter)
	worker := NewJobWorker(queue, registry, func() time.Time { return now }, sequenceIDs("audit", "event", "correlation", "rec", "usage", "audit-2", "event-2", "correlation-2"))
	result, err := worker.RunOnce(context.Background(), 1)
	if err != nil || result.Completed != 1 || queue.purpose != "ai.provider.connection" || adapter.credential != nil {
		t.Fatalf("result=%+v err=%v purpose=%q credential=%q", result, err, queue.purpose, adapter.credential)
	}
}

func (s *jobStoreStub) Submit(_ context.Context, _ SubmitCommand, _ time.Time, _ JobIDs) (GenerationJob, error) {
	return s.job, s.err
}

func (s *jobStoreStub) Get(context.Context, scope.Target, string) (GenerationJob, error) {
	return GenerationJob{}, ErrAIDenied
}

func (s *jobStoreStub) Cancel(context.Context, scope.Target, string, string, string, int64, time.Time, JobTransitionIDs) (GenerationJob, error) {
	return GenerationJob{}, ErrAIDenied
}

func (s *jobStoreStub) Retry(context.Context, scope.Target, string, string, int64, time.Time, JobTransitionIDs) (GenerationJob, error) {
	return GenerationJob{}, ErrAIDenied
}

func TestJobSubmissionReturnsOnlyServerOwnedRelevantInputNames(t *testing.T) {
	now := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	store := &jobStoreStub{job: GenerationJob{
		ID: "job-id", State: JobQueued, RelevantInputs: []string{"title"},
	}}
	service := NewJobService(store, func() time.Time { return now }, sequenceIDs("job-id", "audit-id", "event-id", "correlation-id"))
	job, err := service.Submit(context.Background(), SubmitCommand{
		Principal: aiJobPrincipal(), WorkRecordID: "work-id",
		Feature: FeatureSummary, IdempotencyKey: "request-1",
	})
	if err != nil || job.State != JobQueued || !reflect.DeepEqual(job.RelevantInputs, []string{"title"}) {
		t.Fatalf("Submit() job=%+v error=%v", job, err)
	}
}

func TestBuildRecommendationRecordRejectsUnknownCandidatesAndKeepsUsageNullable(t *testing.T) {
	now := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	confidence := 0.75
	_, err := BuildRecommendationRecord(RecommendationCommand{
		Job:        GenerationJob{ID: "job", MSPID: "msp", ClientID: "client", WorkRecordID: "work", RequestedBy: "tech", Feature: FeatureSummary},
		Policy:     Policy{MSPID: "msp", Enabled: true, PromptVersion: "summary-v1", AllowedFeatures: []Feature{FeatureSummary}},
		Model:      ModelProfile{ID: "model", MSPID: "msp", ConnectionID: "connection", ProviderModelID: "model-a", DisplayName: "Model A", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 100000, OutputLimit: 1000, Enabled: true, Version: 1},
		Connection: ProviderConnection{ID: "connection", MSPID: "msp", Name: "Provider", BaseURL: "https://provider.example", Adapter: AdapterOpenAICompatible, Network: NetworkRemote, Enabled: true, Timeout: time.Minute, RequestLimitBytes: 1024, ResponseLimitBytes: 1024, Health: HealthHealthy, Version: 1},
		Context:    GenerationContext{Fields: []ContextField{{Name: "title", Value: "VPN down", Classification: ContextStandard}}, AuthorizedCandidateIDs: []string{"candidate-a"}},
	}, GenerationResult{Text: "Draft", Confidence: &confidence, CandidateIDs: []string{"candidate-a", "candidate-a"}}, now, RecommendationIDs{RecommendationID: "recommendation", AuditID: "audit", EventID: "event", CorrelationID: "correlation", UsageID: "usage"})
	if err != nil {
		t.Fatalf("BuildRecommendationRecord() error = %v", err)
	}

	_, err = BuildRecommendationRecord(RecommendationCommand{
		Job:        GenerationJob{ID: "job", MSPID: "msp", ClientID: "client", WorkRecordID: "work", RequestedBy: "tech", Feature: FeatureSummary},
		Policy:     Policy{MSPID: "msp", Enabled: true, PromptVersion: "summary-v1", AllowedFeatures: []Feature{FeatureSummary}},
		Model:      ModelProfile{ID: "model", MSPID: "msp", ConnectionID: "connection", ProviderModelID: "model-a", DisplayName: "Model A", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 100000, OutputLimit: 1000, Enabled: true, Version: 1},
		Connection: ProviderConnection{ID: "connection", MSPID: "msp", Name: "Provider", BaseURL: "https://provider.example", Adapter: AdapterOpenAICompatible, Network: NetworkRemote, Enabled: true, Timeout: time.Minute, RequestLimitBytes: 1024, ResponseLimitBytes: 1024, Health: HealthHealthy, Version: 1},
		Context:    GenerationContext{Fields: []ContextField{{Name: "title", Value: "VPN down", Classification: ContextStandard}}, AuthorizedCandidateIDs: []string{"candidate-a"}},
	}, GenerationResult{Text: "Draft", Confidence: &confidence, CandidateIDs: []string{"candidate-b"}}, now, RecommendationIDs{RecommendationID: "recommendation", AuditID: "audit", EventID: "event", CorrelationID: "correlation", UsageID: "usage"})
	if err == nil {
		t.Fatal("BuildRecommendationRecord() accepted an unauthorized candidate")
	}
}

func TestBuildRecommendationRecordAllowsGlobalKnowledgeClassification(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	result, err := BuildRecommendationRecord(RecommendationCommand{
		Job:        GenerationJob{ID: "job", MSPID: "msp", Feature: FeatureClassification, RequestedBy: "tech", Subject: SubjectRef{Type: "knowledge_article", ID: "article", MSPID: "msp"}},
		Policy:     Policy{MSPID: "msp", Enabled: true, PromptVersion: "classification-v1", AllowedFeatures: []Feature{FeatureClassification}},
		Model:      ModelProfile{ID: "model", MSPID: "msp", ConnectionID: "connection", Enabled: true, SupportedFeatures: []Feature{FeatureClassification}},
		Connection: ProviderConnection{ID: "connection", MSPID: "msp", Enabled: true},
		Context:    GenerationContext{Fields: []ContextField{{Name: "title", Value: "Runbook", Classification: ContextStandard}}, AuthorizedCandidateIDs: []string{"tag"}},
	}, GenerationResult{ClassificationCandidates: []ClassificationCandidate{}}, now, RecommendationIDs{RecommendationID: "recommendation", UsageID: "usage", AuditID: "audit", EventID: "event", CorrelationID: "correlation"})
	if err != nil {
		t.Fatalf("BuildRecommendationRecord() error = %v", err)
	}
	if result.ClassificationCandidates == nil {
		t.Fatal("ClassificationCandidates is nil; an explicit empty provider result must remain non-nil")
	}
}

func TestBuildClassificationRecordKeepsEmptyTypedResultDistinctFromRecommendation(t *testing.T) {
	now := time.Date(2026, time.August, 7, 1, 0, 0, 0, time.UTC)
	record, err := BuildRecommendationRecord(RecommendationCommand{
		Job: GenerationJob{ID: "job", MSPID: "msp", ClientID: "client", RequestedBy: "tech", Feature: FeatureClassification,
			Subject: SubjectRef{Type: "task", ID: "task", MSPID: "msp", ClientID: "client"}},
		Policy:     Policy{MSPID: "msp", Enabled: true, PromptVersion: "classification-v1", AllowedFeatures: []Feature{FeatureClassification}},
		Model:      ModelProfile{ID: "model", MSPID: "msp", ConnectionID: "connection", ProviderModelID: "classifier", SupportedFeatures: []Feature{FeatureClassification}, ContextLimit: 10000, OutputLimit: 100, Enabled: true},
		Connection: ProviderConnection{ID: "connection", MSPID: "msp", Adapter: AdapterOllama, Enabled: true},
		Context:    GenerationContext{Fields: []ContextField{{Name: "title", Value: "Nothing matches", Classification: ContextStandard}}},
	}, GenerationResult{ClassificationCandidates: []ClassificationCandidate{}}, now,
		RecommendationIDs{RecommendationID: "recommendation", UsageID: "usage", AuditID: "audit", EventID: "event", CorrelationID: "correlation"})
	if err != nil {
		t.Fatal(err)
	}
	if record.ClassificationCandidates == nil || len(record.ClassificationCandidates) != 0 || record.Audit.ID != "audit" || record.Event.EventID != "event" {
		t.Fatalf("classification record=%+v", record)
	}

	bad := RecommendationCommand{Job: GenerationJob{ID: "job", MSPID: "msp", ClientID: "client", RequestedBy: "tech", Feature: FeatureClassification}, Policy: Policy{MSPID: "msp", Enabled: true, AllowedFeatures: []Feature{FeatureClassification}}, Model: ModelProfile{ID: "model", MSPID: "msp", ConnectionID: "connection", SupportedFeatures: []Feature{FeatureClassification}, Enabled: true}, Connection: ProviderConnection{ID: "connection", MSPID: "msp", Enabled: true}, Context: GenerationContext{Fields: []ContextField{{Name: "title", Value: "value", Classification: ContextStandard}}}}
	if _, err := BuildRecommendationRecord(bad, GenerationResult{ClassificationCandidates: []ClassificationCandidate{}}, now, RecommendationIDs{RecommendationID: "recommendation", UsageID: "usage", AuditID: "audit", EventID: "event", CorrelationID: "correlation"}); !errors.Is(err, ErrAIDenied) {
		t.Fatalf("missing typed subject error=%v, want denied", err)
	}
}

func TestBuildClassificationRecordEnforcesPaidActualCost(t *testing.T) {
	now := time.Date(2026, time.August, 8, 1, 0, 0, 0, time.UTC)
	inputUnits, outputUnits := int64(1_000_000), int64(2_000_000)
	inputRate, outputRate := int64(2), int64(3)
	command := RecommendationCommand{
		Job: GenerationJob{ID: "job", MSPID: "msp", ClientID: "client", RequestedBy: "tech", Feature: FeatureClassification,
			Subject: SubjectRef{Type: "task", ID: "task", MSPID: "msp", ClientID: "client"}},
		Policy: Policy{MSPID: "msp", Enabled: true, PromptVersion: "classification-v1", AllowedFeatures: []Feature{FeatureClassification}, CostLimitEnabled: true},
		Model: ModelProfile{ID: "model", MSPID: "msp", ConnectionID: "connection", ProviderModelID: "classifier", SupportedFeatures: []Feature{FeatureClassification}, ContextLimit: 10_000, OutputLimit: 100, Enabled: true,
			InputCostPerMillionMinor: &inputRate, OutputCostPerMillionMinor: &outputRate},
		Connection: ProviderConnection{ID: "connection", MSPID: "msp", Adapter: AdapterOpenAICompatible, Enabled: true},
		Context:    GenerationContext{Fields: []ContextField{{Name: "title", Value: "Classify", Classification: ContextStandard}}},
	}
	ids := RecommendationIDs{RecommendationID: "recommendation", UsageID: "usage", AuditID: "audit", EventID: "event", CorrelationID: "correlation"}
	record, err := BuildRecommendationRecord(command, GenerationResult{ClassificationCandidates: []ClassificationCandidate{}, Usage: Usage{InputUnits: &inputUnits, OutputUnits: &outputUnits}}, now, ids)
	if err != nil || record.Usage.CostMinor == nil || *record.Usage.CostMinor != 8 {
		t.Fatalf("metered classification record=%+v error=%v", record, err)
	}
	command.Model.InputCostPerMillionMinor = nil
	command.Model.OutputCostPerMillionMinor = nil
	if _, err := BuildRecommendationRecord(command, GenerationResult{ClassificationCandidates: []ClassificationCandidate{}}, now, ids); !errors.Is(err, ErrCostLimitExceeded) {
		t.Fatalf("unknown paid classification cost error=%v, want cost limit exceeded", err)
	}
}

func TestBuildRecommendationRecordDerivesActualCostOnceFromConfiguredRates(t *testing.T) {
	now := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	in, out, inputRate, outputRate := int64(1_000_000), int64(1_000_000), int64(2), int64(3)
	confidence := 0.5
	record, err := BuildRecommendationRecord(RecommendationCommand{
		Job:        GenerationJob{ID: "job", MSPID: "msp", ClientID: "client", WorkRecordID: "work", RequestedBy: "tech", Feature: FeatureSummary},
		Policy:     Policy{MSPID: "msp", Enabled: true, PromptVersion: "v1", AllowedFeatures: []Feature{FeatureSummary}, CostLimitEnabled: true},
		Model:      ModelProfile{ID: "model", MSPID: "msp", ConnectionID: "connection", ProviderModelID: "model", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 100, OutputLimit: 10, Enabled: true, InputCostPerMillionMinor: &inputRate, OutputCostPerMillionMinor: &outputRate},
		Connection: ProviderConnection{ID: "connection", MSPID: "msp", Enabled: true},
		Context:    GenerationContext{Fields: []ContextField{{Name: "title", Value: "value", Classification: ContextStandard}}, AuthorizedCandidateIDs: []string{"candidate"}},
	}, GenerationResult{Text: "draft", Confidence: &confidence, CandidateIDs: []string{"candidate"}, Usage: Usage{InputUnits: &in, OutputUnits: &out}}, now,
		RecommendationIDs{RecommendationID: "recommendation", UsageID: "usage", AuditID: "audit", EventID: "event", CorrelationID: "correlation"})
	if err != nil || record.Usage.CostMinor == nil || *record.Usage.CostMinor != 5 {
		t.Fatalf("record=%+v err=%v", record, err)
	}
}

func TestJobWorkerReauthorizesContextUsesConnectionScopedCredentialAndCompletesOnce(t *testing.T) {
	startedAt := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(2 * time.Minute)
	times := []time.Time{startedAt, completedAt}
	queue := &jobQueueStub{
		jobs: []ClaimedGenerationJob{{Job: GenerationJob{ID: "job", MSPID: "msp", ClientID: "client", WorkRecordID: "work", RequestedBy: "tech", Feature: FeatureSummary, State: JobRunning}, LeaseToken: "lease"}},
		execution: JobExecution{
			Job:        GenerationJob{ID: "job", MSPID: "msp", ClientID: "client", WorkRecordID: "work", RequestedBy: "tech", Feature: FeatureSummary, State: JobRunning},
			Policy:     Policy{MSPID: "msp", Enabled: true, PromptVersion: "summary-v1", AllowedFeatures: []Feature{FeatureSummary}},
			Model:      ModelProfile{ID: "model", MSPID: "msp", ConnectionID: "connection", ProviderModelID: "model-a", DisplayName: "Model A", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 100000, OutputLimit: 1000, Enabled: true, Version: 1},
			Connection: ProviderConnection{ID: "connection", MSPID: "msp", Name: "Provider", BaseURL: "https://provider.example", Adapter: AdapterOpenAICompatible, Network: NetworkRemote, CredentialConfigured: true, Enabled: true, Timeout: time.Minute, RequestLimitBytes: 1024, ResponseLimitBytes: 1024, Health: HealthHealthy, Version: 1},
			Context:    GenerationContext{Fields: []ContextField{{Name: "title", Value: "VPN down", Classification: ContextStandard}}, AuthorizedCandidateIDs: []string{"candidate-a"}},
		},
	}
	registry, err := NewAdapterRegistry(jobAdapterStub{})
	if err != nil {
		t.Fatal(err)
	}
	worker := NewJobWorker(queue, registry, func() time.Time {
		next := times[0]
		times = times[1:]
		return next
	}, sequenceIDs("claim-audit", "claim-event", "claim-correlation", "rec", "usage", "audit", "event", "correlation"))
	result, err := worker.RunOnce(context.Background(), 1)
	if err != nil || result.Completed != 1 || len(queue.completed) != 1 || queue.purpose != "ai.provider.connection" {
		t.Fatalf("RunOnce() result=%+v completed=%+v purpose=%q error=%v", result, queue.completed, queue.purpose, err)
	}
	if got := queue.completed[0].Record.Recommendation.GeneratedAt; !got.Equal(completedAt) {
		t.Fatalf("recommendation generated_at=%s, want actual completion %s", got, completedAt)
	}
}

func TestJobWorkerStopsBeforeCredentialWhenBoundaryReservationRejects(t *testing.T) {
	now := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	queue := &jobQueueStub{reserveErr: ErrCostLimitExceeded,
		jobs: []ClaimedGenerationJob{{Job: GenerationJob{ID: "job", Feature: FeatureSummary, State: JobRunning}, LeaseToken: "lease"}},
		execution: JobExecution{Job: GenerationJob{ID: "job", Feature: FeatureSummary, State: JobRunning},
			Policy:     Policy{Enabled: true, PromptVersion: "v1", AllowedFeatures: []Feature{FeatureSummary}},
			Model:      ModelProfile{ProviderModelID: "model", ContextLimit: 10000, OutputLimit: 10, Enabled: true, SupportedFeatures: []Feature{FeatureSummary}},
			Connection: ProviderConnection{Adapter: AdapterOpenAICompatible, Enabled: true, CredentialConfigured: true},
			Context:    GenerationContext{Fields: []ContextField{{Name: "title", Value: "value", Classification: ContextStandard}}},
		},
	}
	registry, _ := NewAdapterRegistry(jobAdapterStub{})
	worker := NewJobWorker(queue, registry, func() time.Time { return now }, sequenceIDs("claim-audit", "claim-event", "claim-correlation", "failure-audit", "failure-event", "failure-correlation"))
	result, err := worker.RunOnce(context.Background(), 1)
	if err != nil || !queue.reserved || queue.purpose != "" || result.Failed != 1 || len(queue.failed) != 1 || queue.failed[0].SafeErrorCode != "cost_limit_exceeded" {
		t.Fatalf("result=%+v purpose=%q reserve=%v failures=%+v err=%v", result, queue.purpose, queue.reserved, queue.failed, err)
	}
}

func TestJobWorkerSweepsStrandedLeasesBeforeClaiming(t *testing.T) {
	queue := &jobQueueStub{swept: 2}
	registry, _ := NewAdapterRegistry(jobAdapterStub{})
	worker := NewJobWorker(queue, registry, time.Now, sequenceIDs("claim-audit", "claim-event", "claim-correlation"))
	result, err := worker.RunOnce(context.Background(), 1)
	if err != nil || result.Stranded != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestJobWorkerDoesNotPublishWhenCancellationWasRequested(t *testing.T) {
	now := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	queue := &jobQueueStub{cancelled: true,
		jobs:      []ClaimedGenerationJob{{Job: GenerationJob{ID: "job", State: JobRunning}, LeaseToken: "lease"}},
		execution: JobExecution{Job: GenerationJob{ID: "job", State: JobRunning}},
	}
	registry, err := NewAdapterRegistry(jobAdapterStub{})
	if err != nil {
		t.Fatal(err)
	}
	worker := NewJobWorker(queue, registry, func() time.Time { return now }, sequenceIDs("claim-audit", "claim-event", "claim-correlation", "failure-audit", "failure-event", "failure-correlation"))
	result, err := worker.RunOnce(context.Background(), 1)
	if err != nil || result.Cancelled != 1 || len(queue.completed) != 0 || len(queue.failed) != 1 || queue.failed[0].SafeErrorCode != "cancelled" {
		t.Fatalf("RunOnce() result=%+v completed=%+v failed=%+v error=%v", result, queue.completed, queue.failed, err)
	}
}

type blockingJobAdapter struct{ started chan struct{} }

func (a blockingJobAdapter) Type() AdapterType { return AdapterOpenAICompatible }
func (a blockingJobAdapter) Discover(context.Context, ProviderConnection, []byte) ([]DiscoveredModel, error) {
	return nil, nil
}
func (a blockingJobAdapter) Generate(ctx context.Context, _ ProviderConnection, _ ModelProfile, _ ProviderRequest, _ []byte) (GenerationResult, error) {
	close(a.started)
	<-ctx.Done()
	return GenerationResult{}, ctx.Err()
}

func TestJobWorkerCancelsBlockingProviderOnLeaseMaintenanceCancellation(t *testing.T) {
	now := time.Now().UTC()
	started := make(chan struct{})
	maintenanceCalls := 0
	queue := &jobQueueStub{jobs: []ClaimedGenerationJob{{Job: GenerationJob{ID: "job", Feature: FeatureSummary, State: JobRunning}, LeaseToken: "lease"}}, execution: JobExecution{Job: GenerationJob{ID: "job", Feature: FeatureSummary, State: JobRunning}, Policy: Policy{Enabled: true, PromptVersion: "v1", AllowedFeatures: []Feature{FeatureSummary}}, Model: ModelProfile{ProviderModelID: "model", ContextLimit: 10000, OutputLimit: 10, Enabled: true, SupportedFeatures: []Feature{FeatureSummary}}, Connection: ProviderConnection{Adapter: AdapterOpenAICompatible, Enabled: true}, Context: GenerationContext{Fields: []ContextField{{Name: "title", Value: "value", Classification: ContextStandard}}}}, maintain: func() LeaseMaintenance {
		maintenanceCalls++
		return LeaseMaintenance{Owned: true, Cancelled: maintenanceCalls > 1}
	}}
	registry, _ := NewAdapterRegistry(blockingJobAdapter{started: started})
	worker := NewJobWorker(queue, registry, func() time.Time { return now }, sequenceIDs("claim-audit", "claim-event", "claim-correlation", "failure-audit", "failure-event", "failure-correlation"))
	worker.heartbeat = time.Millisecond
	result, err := worker.RunOnce(context.Background(), 1)
	select {
	case <-started:
	default:
		t.Fatal("provider was never started")
	}
	if err != nil || result.Cancelled != 1 || len(queue.completed) != 0 || len(queue.failed) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func aiJobPrincipal() authorization.Principal {
	return authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp", ClientID: "client"}, Capabilities: authorization.NewCapabilitySet("ai.assist")}
}

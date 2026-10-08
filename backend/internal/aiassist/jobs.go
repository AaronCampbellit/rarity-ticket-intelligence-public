package aiassist

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrJobNotRetryable    = errors.New("AI generation job is not retryable")
	ErrJobLeaseLost       = errors.New("AI generation job lease lost")
	ErrJobCancelled       = errors.New("AI generation job cancelled")
	ErrExecutionFenceLost = errors.New("AI generation execution snapshot changed")
	ErrCostLimitExceeded  = errors.New("AI generation cost limit exceeded")
	ErrUnknownCost        = errors.New("AI generation cost is unknown")
)

type JobState string

const (
	JobQueued    JobState = "queued"
	JobRunning   JobState = "running"
	JobCompleted JobState = "completed"
	JobFailed    JobState = "failed"
	JobCancelled JobState = "cancelled"
)

// GenerationJob is deliberately free of browser-provided generation data.
// RelevantInputs contains field names only; values are reloaded at execution.
type GenerationJob struct {
	ID, MSPID, ClientID, WorkRecordID, RequestedBy string
	Subject                                        SubjectRef
	Feature                                        Feature
	ModelProfileID                                 string
	RelevantInputs                                 []string
	State                                          JobState
	Attempt, MaxAttempts                           int
	LeaseToken                                     string
	LeaseUntil, CancellationRequestedAt            *time.Time
	CancellationRequestedBy                        string
	CancelledAt                                    *time.Time
	SafeErrorCode, RecommendationID                string
	IdempotencyKey                                 string
	CreatedAt, UpdatedAt                           time.Time
	CompletedAt                                    *time.Time
	Version                                        int64
}

type SubmitCommand struct {
	Principal      authorization.Principal
	WorkRecordID   string
	Feature        Feature
	IdempotencyKey string
}

type CancelCommand struct {
	Principal       authorization.Principal
	JobID           string
	Reason          string
	ExpectedVersion int64
}

type RetryCommand struct {
	Principal       authorization.Principal
	JobID           string
	Reason          string
	ExpectedVersion int64
}

type JobIDs struct {
	JobID, AuditID, EventID, CorrelationID string
}

type JobTransitionIDs struct{ AuditID, EventID, CorrelationID string }

// JobStore owns the submission transaction. It must resolve the work record,
// policy, model and connection within the Principal's trusted scope before it
// inserts (or replays) the job.
type JobStore interface {
	Submit(context.Context, SubmitCommand, time.Time, JobIDs) (GenerationJob, error)
	Get(context.Context, scope.Target, string) (GenerationJob, error)
	Cancel(context.Context, scope.Target, string, string, string, int64, time.Time, JobTransitionIDs) (GenerationJob, error)
	Retry(context.Context, scope.Target, string, string, int64, time.Time, JobTransitionIDs) (GenerationJob, error)
}

type JobService struct {
	store JobStore
	now   func() time.Time
	newID func() string
}

func NewJobService(store JobStore, now func() time.Time, newID func() string) *JobService {
	return &JobService{store: store, now: now, newID: newID}
}

func (s *JobService) Submit(ctx context.Context, command SubmitCommand) (GenerationJob, error) {
	if s == nil || s.store == nil || s.now == nil || s.newID == nil ||
		strings.TrimSpace(command.WorkRecordID) == "" || strings.TrimSpace(command.IdempotencyKey) == "" ||
		!validFeature(command.Feature) || strings.TrimSpace(command.Principal.ID) == "" {
		return GenerationJob{}, ErrAIDenied
	}
	target := scope.Target{MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID}
	if err := authorization.Authorize(command.Principal, "ai.assist", target); err != nil {
		return GenerationJob{}, ErrAIDenied
	}
	now := s.now().UTC()
	return s.store.Submit(ctx, command, now, JobIDs{JobID: s.newID(), AuditID: s.newID(), EventID: s.newID(), CorrelationID: s.newID()})
}

func (s *JobService) Get(ctx context.Context, principal authorization.Principal, id string) (GenerationJob, error) {
	if s == nil || s.store == nil || strings.TrimSpace(id) == "" {
		return GenerationJob{}, scope.ErrNotFound
	}
	target := scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID}
	if err := authorization.Authorize(principal, "ai.assist", target); err != nil {
		return GenerationJob{}, ErrAIDenied
	}
	return s.store.Get(ctx, target, id)
}

func (s *JobService) Cancel(ctx context.Context, command CancelCommand) (GenerationJob, error) {
	if s == nil || s.store == nil || s.now == nil || s.newID == nil || strings.TrimSpace(command.JobID) == "" || strings.TrimSpace(command.Reason) == "" || command.ExpectedVersion < 1 {
		return GenerationJob{}, scope.ErrNotFound
	}
	target := scope.Target{MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID}
	if err := authorization.Authorize(command.Principal, "ai.assist", target); err != nil {
		return GenerationJob{}, ErrAIDenied
	}
	return s.store.Cancel(ctx, target, command.JobID, command.Principal.ID, command.Reason, command.ExpectedVersion, s.now().UTC(), JobTransitionIDs{AuditID: s.newID(), EventID: s.newID(), CorrelationID: s.newID()})
}

func (s *JobService) Retry(ctx context.Context, command RetryCommand) (GenerationJob, error) {
	if s == nil || s.store == nil || s.now == nil || s.newID == nil || strings.TrimSpace(command.JobID) == "" || strings.TrimSpace(command.Reason) == "" || command.ExpectedVersion < 1 {
		return GenerationJob{}, scope.ErrNotFound
	}
	target := scope.Target{MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID}
	if err := authorization.Authorize(command.Principal, "ai.assist", target); err != nil {
		return GenerationJob{}, ErrAIDenied
	}
	return s.store.Retry(ctx, target, command.JobID, command.Reason, command.ExpectedVersion, s.now().UTC(), JobTransitionIDs{AuditID: s.newID(), EventID: s.newID(), CorrelationID: s.newID()})
}

type GenerationContext struct {
	Fields                 []ContextField
	AuthorizedCandidateIDs []string
}

type RecommendationCommand struct {
	Job        GenerationJob
	Policy     Policy
	Model      ModelProfile
	Connection ProviderConnection
	Context    GenerationContext
}

type RecommendationIDs struct {
	RecommendationID, UsageID, AuditID, EventID, CorrelationID string
}

// UsageRecord preserves the fact that a provider did not report metering.
// Nil values are persisted as SQL NULL rather than guessed as zero.
type UsageRecord struct {
	ID, RecommendationID, MSPID, Provider, Model string
	InputUnits, OutputUnits, CostMinor           *int64
	RecordedAt                                   time.Time
}

type ClaimedGenerationJob struct {
	Job        GenerationJob
	LeaseToken string
}

// JobExecution is reloaded after leasing, directly before provider I/O. Its
// context and candidate IDs are repository-owned data, never job payloads.
type JobExecution struct {
	Job        GenerationJob
	Policy     Policy
	Model      ModelProfile
	Connection ProviderConnection
	Context    GenerationContext
	Snapshot   ExecutionSnapshot
}

// ExecutionSnapshot is repository-owned identity for every input that makes a
// provider request authorized. It is persisted with the reservation and
// checked again immediately before any provider call.
type ExecutionSnapshot struct {
	WorkUpdatedAt        time.Time
	CandidateFingerprint string
}

// ExecutionFingerprint binds the exact encoded provider body to its trusted
// authorization/configuration snapshot. Delimiter-safe field framing avoids
// ambiguous concatenation; the request bytes are hashed verbatim, never by
// length.
func ExecutionFingerprint(encodedRequest []byte, execution JobExecution) string {
	hash := sha256.New()
	write := func(value string) { _, _ = fmt.Fprintf(hash, "%d:%s|", len(value), value) }
	write(string(encodedRequest))
	write(execution.Job.ID)
	write(execution.Job.MSPID)
	write(execution.Job.ClientID)
	write(execution.Job.WorkRecordID)
	write(execution.Job.Subject.Type)
	write(execution.Job.Subject.ID)
	write(string(execution.Job.Feature))
	write(fmt.Sprintf("%d", execution.Policy.Version))
	write(execution.Policy.PromptVersion)
	write(execution.Model.ID)
	price := func(value *int64) string {
		if value == nil {
			return "null"
		}
		return fmt.Sprintf("%d", *value)
	}
	write(fmt.Sprintf("%d:%d:%d:%t:%s:%s", execution.Model.Version, execution.Model.ContextLimit, execution.Model.OutputLimit, execution.Model.ZeroCost, price(execution.Model.InputCostPerMillionMinor), price(execution.Model.OutputCostPerMillionMinor)))
	write(execution.Connection.ID)
	write(fmt.Sprintf("%d:%s:%s", execution.Connection.Version, execution.Connection.BaseURL, execution.Connection.Network))
	write(execution.Snapshot.WorkUpdatedAt.UTC().Format(time.RFC3339Nano))
	write(execution.Snapshot.CandidateFingerprint)
	for _, field := range execution.Context.Fields {
		write(field.Name)
		write(field.Value)
		write(string(field.Classification))
	}
	for _, candidateID := range execution.Context.AuthorizedCandidateIDs {
		write(candidateID)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

type JobCompletion struct {
	JobID, LeaseToken string
	Record            RecommendationRecord
}

// CredentialReference is a fenced, MSP-scoped reference to the exact
// connection snapshot authorized for one execution. It intentionally carries
// no secret material and prevents an endpoint snapshot from being paired with
// a credential rotated after authorization.
type CredentialReference struct {
	ConnectionID, MSPID  string
	ConnectionVersion    int64
	ConnectionBaseURL    string
	ConnectionNetwork    NetworkMode
	JobID, LeaseToken    string
	ExecutionFingerprint string
}

type JobFailure struct {
	JobID, LeaseToken, SafeErrorCode string
	FailedAt                         time.Time
	Retryable                        bool
	IDs                              JobTransitionIDs
}

type JobQueue interface {
	Claim(context.Context, int, time.Duration, []JobTransitionIDs) ([]ClaimedGenerationJob, error)
	SweepStranded(context.Context) (int, error)
	LoadExecution(context.Context, string, string) (JobExecution, error)
	// AuthorizeAndReserve repeats every disclosure boundary check while holding
	// the MSP cost-governance serialization lock, then durably reserves a
	// conservative maximum before credential access.
	AuthorizeAndReserve(context.Context, string, string, string, int64, int64) (JobExecution, error)
	CancellationRequested(context.Context, string, string) (bool, error)
	MaintainLease(context.Context, string, string, time.Duration) (LeaseMaintenance, error)
	// ExecuteWithCredential validates the complete reserved connection snapshot
	// for every provider call. Optional-credential adapters receive nil only
	// through this callback, never through an unfenced direct invocation.
	ExecuteWithCredential(context.Context, CredentialReference, func([]byte) error) error
	Complete(context.Context, JobCompletion) error
	Fail(context.Context, JobFailure) error
}
type LeaseMaintenance struct{ Owned, Cancelled bool }

type WorkerResult struct {
	Claimed, Completed, Failed, Cancelled, Stranded int
}

type JobWorker struct {
	queue     JobQueue
	registry  *AdapterRegistry
	now       func() time.Time
	newID     func() string
	lease     time.Duration
	heartbeat time.Duration
}

func NewJobWorker(queue JobQueue, registry *AdapterRegistry, now func() time.Time, ids ...func() string) *JobWorker {
	newID := func() string { return "" }
	if len(ids) > 0 && ids[0] != nil {
		newID = ids[0]
	}
	return &JobWorker{queue: queue, registry: registry, now: now, newID: newID, lease: 5 * time.Minute, heartbeat: time.Minute}
}

func (w *JobWorker) RunOnce(ctx context.Context, limit int) (WorkerResult, error) {
	if w == nil || w.queue == nil || w.registry == nil || w.now == nil || w.newID == nil || limit < 1 {
		return WorkerResult{}, ErrAIDenied
	}
	if limit > 25 {
		limit = 25
	}
	result := WorkerResult{}
	stranded, err := w.queue.SweepStranded(ctx)
	if err != nil {
		return result, err
	}
	result.Stranded = stranded
	for i := 0; i < limit; i++ {
		now := w.now().UTC()
		jobs, err := w.queue.Claim(ctx, 1, w.lease, []JobTransitionIDs{{
			AuditID: w.newID(), EventID: w.newID(), CorrelationID: w.newID(),
		}})
		if err != nil {
			return result, err
		}
		if len(jobs) == 0 {
			break
		}
		result.Claimed++
		claimed := jobs[0]
		if err := w.runClaimed(ctx, claimed, now, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (w *JobWorker) runClaimed(ctx context.Context, claimed ClaimedGenerationJob, now time.Time, result *WorkerResult) error {
	execution, err := w.queue.LoadExecution(ctx, claimed.Job.ID, claimed.LeaseToken)
	if err != nil {
		return w.fail(ctx, claimed, now, "context_unavailable", false, result)
	}
	if execution.Job.ID != claimed.Job.ID || execution.Job.State != JobRunning {
		return w.fail(ctx, claimed, now, "lease_lost", false, result)
	}
	maintenance, err := w.queue.MaintainLease(ctx, claimed.Job.ID, claimed.LeaseToken, w.lease)
	if err != nil || !maintenance.Owned {
		return w.fail(ctx, claimed, now, "lease_lost", false, result)
	}
	if maintenance.Cancelled {
		return w.fail(ctx, claimed, now, "cancelled", false, result)
	}
	cancelled, err := w.queue.CancellationRequested(ctx, claimed.Job.ID, claimed.LeaseToken)
	if err != nil {
		return err
	}
	if cancelled {
		return w.fail(ctx, claimed, now, "cancelled", false, result)
	}
	adapter, ok := w.registry.Lookup(execution.Connection.Adapter)
	if !ok {
		return w.fail(ctx, claimed, now, "invalid_provider_configuration", false, result)
	}
	fields := minimizedFields(execution.Context.Fields)
	request := ProviderRequest{Feature: execution.Job.Feature, Provider: string(execution.Connection.Adapter),
		Model: execution.Model.ProviderModelID, PromptVersion: execution.Policy.PromptVersion,
		MSPID: execution.Job.MSPID, ClientID: execution.Job.ClientID, WorkRecordID: execution.Job.WorkRecordID,
		Fields: fields, AuthorizedCandidateIDs: append([]string(nil), execution.Context.AuthorizedCandidateIDs...), MaxOutputUnits: execution.Model.OutputLimit}
	encodedRequest, err := PrepareProviderRequest(adapter.Type(), execution.Model, request)
	if err != nil {
		return w.fail(ctx, claimed, now, safeGenerationError(err), false, result)
	}
	inputBytes := int64(len(encodedRequest))
	if execution.Model.ContextLimit <= 0 || execution.Model.OutputLimit <= 0 || execution.Model.OutputLimit > execution.Model.ContextLimit || inputBytes+execution.Model.OutputLimit > execution.Model.ContextLimit {
		return w.fail(ctx, claimed, now, "context_limit_exceeded", false, result)
	}
	// This repeats authorization, lifecycle, policy/model/connection, and lease
	// checks in a single locked transaction and stores a durable reservation
	// before any credential can be opened or provider request can be sent.
	fingerprint := ExecutionFingerprint(encodedRequest, execution)
	execution, err = w.queue.AuthorizeAndReserve(ctx, claimed.Job.ID, claimed.LeaseToken, fingerprint, inputBytes, execution.Model.OutputLimit)
	if err != nil {
		return w.fail(ctx, claimed, now, safeGenerationError(err), false, result)
	}
	adapter, ok = w.registry.Lookup(execution.Connection.Adapter)
	if !ok {
		return w.fail(ctx, claimed, now, "invalid_provider_configuration", false, result)
	}
	// Build the actual transport body only from the final authorized execution.
	// A changed final snapshot is never allowed to send the preliminary body.
	fields = minimizedFields(execution.Context.Fields)
	request = ProviderRequest{Feature: execution.Job.Feature, Provider: string(execution.Connection.Adapter),
		Model: execution.Model.ProviderModelID, PromptVersion: execution.Policy.PromptVersion,
		MSPID: execution.Job.MSPID, ClientID: execution.Job.ClientID, WorkRecordID: execution.Job.WorkRecordID,
		Fields: fields, AuthorizedCandidateIDs: append([]string(nil), execution.Context.AuthorizedCandidateIDs...), MaxOutputUnits: execution.Model.OutputLimit}
	confirmedRequest, err := PrepareProviderRequest(adapter.Type(), execution.Model, request)
	if err != nil {
		return w.fail(ctx, claimed, now, safeGenerationError(err), false, result)
	}
	// The reservation is bound to the exact bytes plus every trusted snapshot
	// input. Same-size changes are not equivalent and fail before I/O.
	if ExecutionFingerprint(confirmedRequest, execution) != fingerprint || execution.Model.OutputLimit <= 0 ||
		execution.Model.OutputLimit > execution.Model.ContextLimit || int64(len(confirmedRequest))+execution.Model.OutputLimit > execution.Model.ContextLimit {
		return w.fail(ctx, claimed, now, "execution_changed", true, result)
	}
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := make(chan struct{})
	maintained := make(chan LeaseMaintenance, 1)
	go w.monitorLease(callCtx, stop, claimed, maintained, cancel)
	var generated GenerationResult
	generate := func(credential []byte) error {
		if cancelled, err := w.queue.CancellationRequested(ctx, claimed.Job.ID, claimed.LeaseToken); err != nil || cancelled {
			if err != nil {
				return err
			}
			return ErrJobCancelled
		}
		result, err := adapter.Generate(callCtx, execution.Connection, execution.Model, request, credential)
		if err != nil {
			return err
		}
		generated = result
		return nil
	}
	err = w.queue.ExecuteWithCredential(ctx, CredentialReference{ConnectionID: execution.Connection.ID,
		MSPID: execution.Connection.MSPID, ConnectionVersion: execution.Connection.Version,
		ConnectionBaseURL: execution.Connection.BaseURL, ConnectionNetwork: execution.Connection.Network,
		JobID: claimed.Job.ID, LeaseToken: claimed.LeaseToken, ExecutionFingerprint: fingerprint}, generate)
	close(stop)
	state := <-maintained
	if !state.Owned {
		return w.fail(ctx, claimed, now, "lease_lost", false, result)
	}
	if state.Cancelled {
		return w.fail(ctx, claimed, now, "cancelled", false, result)
	}
	if err != nil {
		if errors.Is(err, ErrJobCancelled) || errors.Is(err, context.Canceled) {
			return w.fail(ctx, claimed, now, "cancelled", false, result)
		}
		return w.fail(ctx, claimed, now, safeGenerationError(err), retryableGenerationError(err), result)
	}
	if cancelled, err := w.queue.CancellationRequested(ctx, claimed.Job.ID, claimed.LeaseToken); err != nil {
		return err
	} else if cancelled {
		return w.fail(ctx, claimed, now, "cancelled", false, result)
	}
	completedAt := w.now().UTC()
	record, err := BuildRecommendationRecord(RecommendationCommand{Job: execution.Job, Policy: execution.Policy,
		Model: execution.Model, Connection: execution.Connection, Context: execution.Context}, generated, completedAt,
		RecommendationIDs{RecommendationID: w.newID(), UsageID: w.newID(), AuditID: w.newID(), EventID: w.newID(), CorrelationID: w.newID()})
	if err != nil {
		return w.fail(ctx, claimed, now, safeGenerationError(err), false, result)
	}
	if err := w.queue.Complete(ctx, JobCompletion{JobID: claimed.Job.ID, LeaseToken: claimed.LeaseToken, Record: record}); err != nil {
		if errors.Is(err, ErrJobLeaseLost) {
			return nil
		}
		if errors.Is(err, ErrJobCancelled) {
			return w.fail(ctx, claimed, now, "cancelled", false, result)
		}
		if errors.Is(err, ErrCostLimitExceeded) {
			return w.fail(ctx, claimed, now, "cost_limit_exceeded", false, result)
		}
		return err
	}
	result.Completed++
	return nil
}

// EstimateProviderRequestUnits is a conservative byte-based upper bound when
// a provider tokenizer is unavailable. It includes fixed JSON/system framing.
func EstimateProviderRequestUnits(fields []ContextField, candidates []string) int64 {
	units := int64(len(recommendationInstruction) + 512)
	for _, field := range fields {
		units += int64(len(field.Name) + len(field.Value) + 32)
	}
	for _, candidate := range candidates {
		units += int64(len(candidate) + 8)
	}
	return units
}

func (w *JobWorker) monitorLease(ctx context.Context, stop <-chan struct{}, claimed ClaimedGenerationJob, result chan<- LeaseMaintenance, cancel context.CancelFunc) {
	interval := w.heartbeat
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	state := LeaseMaintenance{Owned: true}
	defer func() { result <- state }()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			next, err := w.queue.MaintainLease(ctx, claimed.Job.ID, claimed.LeaseToken, w.lease)
			if err != nil || !next.Owned || next.Cancelled {
				state = next
				cancel()
				return
			}
		}
	}
}

func (w *JobWorker) fail(ctx context.Context, claimed ClaimedGenerationJob, now time.Time, code string, retryable bool, result *WorkerResult) error {
	if err := w.queue.Fail(ctx, JobFailure{
		JobID: claimed.Job.ID, LeaseToken: claimed.LeaseToken,
		SafeErrorCode: code, FailedAt: now, Retryable: retryable,
		IDs: JobTransitionIDs{AuditID: w.newID(), EventID: w.newID(), CorrelationID: w.newID()},
	}); err != nil {
		if errors.Is(err, ErrJobLeaseLost) {
			return nil
		}
		return err
	}
	if code == "cancelled" {
		result.Cancelled++
	} else {
		result.Failed++
	}
	return nil
}

func retryableGenerationError(err error) bool {
	var providerStatus *ProviderHTTPError
	if errors.As(err, &providerStatus) {
		return providerStatus.StatusCode == 429 || providerStatus.StatusCode >= 500
	}
	var providerTransport *ProviderTransportError
	if errors.As(err, &providerTransport) {
		return providerTransport.Temporary
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())
}

// EstimateReservedCostMinor calculates a conservative upper bound in minor
// units. Inputs are deliberately bounded by the exact encoded request and the
// configured output cap before this is called. Paid models require both rates.
func EstimateReservedCostMinor(model ModelProfile, inputUnits, outputUnits int64) (int64, error) {
	if model.ZeroCost {
		return 0, nil
	}
	if inputUnits < 0 || outputUnits < 0 || model.InputCostPerMillionMinor == nil || model.OutputCostPerMillionMinor == nil ||
		*model.InputCostPerMillionMinor < 0 || *model.OutputCostPerMillionMinor < 0 {
		return 0, ErrUnknownCost
	}
	const million = int64(1_000_000)
	inputRaw, ok := mulAddCeiling(inputUnits, *model.InputCostPerMillionMinor, 0, 1)
	if !ok {
		return 0, ErrCostLimitExceeded
	}
	outputRaw, ok := mulAddCeiling(outputUnits, *model.OutputCostPerMillionMinor, 0, 1)
	if !ok || inputRaw > math.MaxInt64-outputRaw {
		return 0, ErrCostLimitExceeded
	}
	reserved, ok := mulAddCeiling(1, inputRaw+outputRaw, 0, million)
	if !ok {
		return 0, ErrCostLimitExceeded
	}
	return reserved, nil
}

func mulAddCeiling(units, rate, add, divisor int64) (int64, bool) {
	if units < 0 || rate < 0 || add < 0 || divisor <= 0 || (units != 0 && rate > (math.MaxInt64-add)/units) {
		return 0, false
	}
	value := units*rate + add
	if value > math.MaxInt64-(divisor-1) {
		return 0, false
	}
	return (value + divisor - 1) / divisor, true
}

func safeGenerationError(err error) string {
	switch {
	case errors.Is(err, ErrInvalidAIOutput):
		return "invalid_output"
	case errors.Is(err, ErrInvalidProviderResponse):
		return "invalid_provider_response"
	case errors.Is(err, ErrProviderCredentialUnavailable):
		return "credential_unavailable"
	case errors.Is(err, ErrExecutionFenceLost):
		return "execution_changed"
	case errors.Is(err, ErrCostLimitExceeded):
		return "cost_limit_exceeded"
	case errors.Is(err, ErrUnknownCost):
		return "unknown_cost_disallowed"
	case errors.Is(err, context.DeadlineExceeded):
		return "provider_timeout"
	default:
		return "provider_unavailable"
	}
}

type RecommendationRecord struct {
	Recommendation           Recommendation
	Usage                    UsageRecord
	Audit                    mutation.AuditRecord
	Event                    mutation.EventRecord
	ClassificationCandidates []ClassificationCandidate
}

func BuildRecommendationRecord(command RecommendationCommand, result GenerationResult, now time.Time, ids RecommendationIDs) (RecommendationRecord, error) {
	if strings.TrimSpace(ids.RecommendationID) == "" || strings.TrimSpace(ids.UsageID) == "" || strings.TrimSpace(ids.AuditID) == "" ||
		strings.TrimSpace(ids.EventID) == "" || strings.TrimSpace(ids.CorrelationID) == "" ||
		strings.TrimSpace(command.Job.ID) == "" || strings.TrimSpace(command.Job.MSPID) == "" ||
		(command.Job.Feature != FeatureClassification && strings.TrimSpace(command.Job.ClientID) == "") ||
		(command.Job.Feature == FeatureClassification && command.Job.Subject.Type != "knowledge_article" && strings.TrimSpace(command.Job.ClientID) == "") ||
		(command.Job.Feature != FeatureClassification && strings.TrimSpace(command.Job.WorkRecordID) == "") ||
		!validFeature(command.Job.Feature) || command.Policy.MSPID != command.Job.MSPID ||
		command.Model.MSPID != command.Job.MSPID || command.Connection.MSPID != command.Job.MSPID ||
		command.Model.ConnectionID != command.Connection.ID || !command.Connection.Enabled || !command.Model.Enabled ||
		!modelSupports(command.Model, command.Job.Feature) || !validJobPolicy(command.Policy, command.Job.Feature) {
		return RecommendationRecord{}, ErrAIDenied
	}
	if command.Job.Feature == FeatureClassification && (!validClassificationSubject(command.Job.Subject) ||
		command.Job.Subject.MSPID != command.Job.MSPID || command.Job.Subject.ClientID != command.Job.ClientID) {
		return RecommendationRecord{}, ErrAIDenied
	}
	// Providers commonly report units but not currency. Derive one actual cost
	// consistently for every feature before typed result handling diverges.
	if result.Usage.CostMinor == nil && result.Usage.InputUnits != nil && result.Usage.OutputUnits != nil {
		if cost, err := EstimateReservedCostMinor(command.Model, *result.Usage.InputUnits, *result.Usage.OutputUnits); err == nil {
			result.Usage.CostMinor = &cost
		}
	}
	if command.Policy.CostLimitEnabled && !command.Model.ZeroCost && result.Usage.CostMinor == nil && !command.Policy.AllowUnmeteredUnknown {
		return RecommendationRecord{}, ErrCostLimitExceeded
	}
	if command.Job.Feature == FeatureClassification {
		for _, value := range []*int64{result.Usage.InputUnits, result.Usage.OutputUnits, result.Usage.CostMinor} {
			if value != nil && *value < 0 {
				return RecommendationRecord{}, ErrInvalidAIOutput
			}
		}
		fields := minimizedFields(command.Context.Fields)
		if len(fields) == 0 {
			return RecommendationRecord{}, ErrAIDenied
		}
		candidates, ok := validatedClassificationCandidates(result.ClassificationCandidates, command.Context.AuthorizedCandidateIDs)
		if !ok {
			return RecommendationRecord{}, ErrInvalidAIOutput
		}
		return RecommendationRecord{
			ClassificationCandidates: candidates,
			Usage: UsageRecord{ID: ids.UsageID, MSPID: command.Job.MSPID,
				Provider: string(command.Connection.Adapter), Model: command.Model.ProviderModelID,
				InputUnits: result.Usage.InputUnits, OutputUnits: result.Usage.OutputUnits,
				CostMinor: result.Usage.CostMinor, RecordedAt: now},
			Audit: mutation.AuditRecord{ID: ids.AuditID, CorrelationID: ids.CorrelationID},
			Event: mutation.EventRecord{EventID: ids.EventID, CorrelationID: ids.CorrelationID},
		}, nil
	}
	text := strings.TrimSpace(result.Text)
	if text == "" || len(text) > 32_000 || result.Confidence == nil || math.IsNaN(*result.Confidence) ||
		math.IsInf(*result.Confidence, 0) || *result.Confidence < 0 || *result.Confidence > 1 {
		return RecommendationRecord{}, ErrInvalidAIOutput
	}
	for _, value := range []*int64{result.Usage.InputUnits, result.Usage.OutputUnits, result.Usage.CostMinor} {
		if value != nil && *value < 0 {
			return RecommendationRecord{}, ErrInvalidAIOutput
		}
	}
	candidates, ok := dedupeCandidates(result.CandidateIDs, command.Context.AuthorizedCandidateIDs)
	if !ok {
		return RecommendationRecord{}, ErrInvalidAIOutput
	}
	fields := minimizedFields(command.Context.Fields)
	if len(fields) == 0 {
		return RecommendationRecord{}, ErrAIDenied
	}
	inputNames := make([]string, 0, len(fields))
	for _, field := range fields {
		inputNames = append(inputNames, field.Name)
	}
	now = now.UTC()
	recommendation := Recommendation{
		ID: ids.RecommendationID, Feature: command.Job.Feature, MSPID: command.Job.MSPID,
		ClientID: command.Job.ClientID, WorkRecordID: command.Job.WorkRecordID, Text: text,
		CandidateIDs: candidates, Confidence: result.Confidence, Provider: string(command.Connection.Adapter),
		Model: command.Model.ProviderModelID, PromptVersion: command.Policy.PromptVersion,
		State: RecommendationPendingHuman, GeneratedAt: now, RelevantInputs: inputNames,
	}
	return RecommendationRecord{
		Recommendation: recommendation,
		Usage: UsageRecord{ID: ids.UsageID, RecommendationID: recommendation.ID, MSPID: recommendation.MSPID,
			Provider: recommendation.Provider, Model: recommendation.Model, InputUnits: result.Usage.InputUnits,
			OutputUnits: result.Usage.OutputUnits, CostMinor: result.Usage.CostMinor, RecordedAt: now},
		Audit: mutation.AuditRecord{ID: ids.AuditID, OccurredAt: now, MSPID: recommendation.MSPID,
			ClientID: recommendation.ClientID, ActorType: "technician", ActorID: command.Job.RequestedBy,
			Action: "ai.recommendation.generated", SubjectType: "ai_recommendation", SubjectID: recommendation.ID,
			SubjectVersion: 1, Source: "ai", CorrelationID: ids.CorrelationID},
		Event: mutation.EventRecord{EventID: ids.EventID, EventType: "ai.recommendation.generated", SchemaVersion: 1,
			OccurredAt: now, MSPID: recommendation.MSPID, ClientID: recommendation.ClientID,
			ActorType: "technician", ActorID: command.Job.RequestedBy, SubjectType: "ai_recommendation",
			SubjectID: recommendation.ID, SubjectVersion: 1, Source: "ai", CorrelationID: ids.CorrelationID},
	}, nil
}

func validClassificationSubject(subject SubjectRef) bool {
	if strings.TrimSpace(subject.ID) == "" || strings.TrimSpace(subject.MSPID) == "" {
		return false
	}
	switch subject.Type {
	case "work_record", "task", "project", "asset", "knowledge_article", "time_entry":
		return subject.Type == "knowledge_article" || strings.TrimSpace(subject.ClientID) != ""
	default:
		return false
	}
}

func validatedClassificationCandidates(values []ClassificationCandidate, authorized []string) ([]ClassificationCandidate, bool) {
	if values == nil || len(values) > maximumClassificationSuggestions {
		return nil, false
	}
	allowed := make(map[string]struct{}, len(authorized))
	for _, id := range authorized {
		if strings.TrimSpace(id) == "" {
			return nil, false
		}
		allowed[id] = struct{}{}
	}
	result := make([]ClassificationCandidate, len(values))
	seen := make(map[string]struct{}, len(values))
	for index, candidate := range values {
		if _, ok := allowed[candidate.TagID]; !ok || strings.TrimSpace(candidate.Rationale) == "" ||
			len(candidate.Rationale) > maximumClassificationRationale || candidate.Confidence < 0 || candidate.Confidence > 1 ||
			math.IsNaN(candidate.Confidence) || math.IsInf(candidate.Confidence, 0) {
			return nil, false
		}
		if _, duplicate := seen[candidate.TagID]; duplicate {
			return nil, false
		}
		seen[candidate.TagID] = struct{}{}
		result[index] = candidate
	}
	return result, true
}

func validFeature(feature Feature) bool {
	return feature == FeatureSummary || feature == FeatureReplyDraft || feature == FeatureSimilar || feature == FeatureClassification
}

func validJobPolicy(policy Policy, feature Feature) bool {
	if !policy.Enabled || strings.TrimSpace(policy.MSPID) == "" || strings.TrimSpace(policy.PromptVersion) == "" {
		return false
	}
	for _, allowed := range policy.AllowedFeatures {
		if allowed == feature && validFeature(feature) {
			return true
		}
	}
	return false
}

func modelSupports(model ModelProfile, feature Feature) bool {
	for _, supported := range model.SupportedFeatures {
		if supported == feature {
			return true
		}
	}
	return false
}

func dedupeCandidates(returned, authorized []string) ([]string, bool) {
	allowed := make(map[string]struct{}, len(authorized))
	for _, id := range authorized {
		allowed[id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(returned))
	result := make([]string, 0, len(returned))
	for _, id := range returned {
		if _, ok := allowed[id]; !ok {
			return nil, false
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result, true
}

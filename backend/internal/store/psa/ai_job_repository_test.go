package psa

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestAIJobRepositorySubmissionDerivesClientContextInsideInsertTransaction(t *testing.T) {
	at := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	tx := &fakeSalesTx{queryRow: fakeRow{scan: scanAIJob("job", "msp", "client", "work", "tech", at)}}
	repository := NewAIJobRepository(&fakeSalesDB{tx: tx}, nil)
	job, err := repository.Submit(context.Background(), aiassist.SubmitCommand{
		Principal:    authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp", ClientID: "client"}},
		WorkRecordID: "work", Feature: aiassist.FeatureSummary, IdempotencyKey: "browser-key",
	}, at, aiassist.JobIDs{JobID: "job", AuditID: "audit", EventID: "event", CorrelationID: "correlation"})
	if err != nil || job.ClientID != "client" || !tx.committed || tx.rolledBack {
		t.Fatalf("Submit() job=%+v tx=%+v error=%v", job, tx, err)
	}
	for _, required := range []string{
		"FROM work_records work", "work.client_id = $4", "policy.enabled",
		"model.enabled", "connection.enabled", "connection.disclosure_accepted_at IS NOT NULL",
		"connection.disclosure_accepted_by IS NOT NULL", "relevant_input_names", "ON CONFLICT (idempotency_key)",
		"DO UPDATE SET id = ai_generation_jobs.id", "(xmax = 0) AS inserted",
	} {
		if !strings.Contains(tx.query, required) {
			t.Fatalf("Submit() omitted %q: %s", required, tx.query)
		}
	}
	if got := tx.queryArgs[7]; got != "msp|client|tech|work|summary|browser-key" {
		t.Fatalf("idempotency key is not requester/client/feature scoped: %v", got)
	}
	assertQueryOrder(t, tx.calls, "INSERT INTO ai_generation_jobs", "INSERT INTO audit_ledger", "INSERT INTO event_outbox")
	if tx.args[0][0] != "audit" || tx.args[0][6] != "ai.generation_job.submitted" || tx.args[0][11] != "queued" || tx.args[0][12] != "correlation" ||
		tx.args[1][0] != "event" || tx.args[1][1] != "ai.generation_job.submitted" || tx.args[1][11] != "correlation" {
		t.Fatalf("Submit() did not persist supplied transactional evidence: args=%v", tx.args)
	}
}

func TestAIJobRepositorySubmissionRollsBackJobWhenEvidenceWriteFails(t *testing.T) {
	at := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	for _, failAt := range []int{1, 2} {
		t.Run("fact write "+string(rune('0'+failAt)), func(t *testing.T) {
			tx := &fakeSalesTx{queryRow: fakeRow{scan: scanAIJob("job", "msp", "client", "work", "tech", at)}, failAt: failAt}
			_, err := NewAIJobRepository(&fakeSalesDB{tx: tx}, nil).Submit(context.Background(), aiassist.SubmitCommand{
				Principal: authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp", ClientID: "client"}}, WorkRecordID: "work", Feature: aiassist.FeatureSummary, IdempotencyKey: "key",
			}, at, aiassist.JobIDs{JobID: "job", AuditID: "audit", EventID: "event", CorrelationID: "correlation"})
			if err == nil || !tx.rolledBack || tx.committed {
				t.Fatalf("Submit() err=%v tx=%+v", err, tx)
			}
		})
	}
}

func TestAIJobRepositorySubmissionReplayReturnsCanonicalJobWithoutFacts(t *testing.T) {
	at := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	tx := &fakeSalesTx{queryRow: fakeRow{scan: func(destinations ...any) {
		scanAIJob("canonical-job", "msp", "client", "work", "tech", at)(destinations...)
		*(destinations[25].(*bool)) = false
	}}}
	job, err := NewAIJobRepository(&fakeSalesDB{tx: tx}, nil).Submit(context.Background(), aiassist.SubmitCommand{
		Principal: authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp", ClientID: "client"}}, WorkRecordID: "work", Feature: aiassist.FeatureSummary, IdempotencyKey: "key",
	}, at, aiassist.JobIDs{JobID: "new-id", AuditID: "audit", EventID: "event", CorrelationID: "correlation"})
	if err != nil || job.ID != "canonical-job" || len(tx.calls) != 1 || strings.Contains(tx.calls[0], "INSERT INTO audit_ledger") || !tx.committed || tx.rolledBack {
		t.Fatalf("Submit() job=%+v err=%v calls=%v tx=%+v", job, err, tx.calls, tx)
	}
}

func scanAIJob(id, mspID, clientID, workID, actorID string, at time.Time) func(...any) {
	return func(destinations ...any) {
		*(destinations[0].(*string)) = id
		*(destinations[1].(*string)) = mspID
		*(destinations[2].(*string)) = clientID
		*(destinations[3].(*string)) = workID
		*(destinations[4].(*string)) = "work_record"
		*(destinations[5].(*string)) = workID
		*(destinations[6].(*string)) = actorID
		*(destinations[7].(*aiassist.Feature)) = aiassist.FeatureSummary
		*(destinations[8].(*string)) = "model"
		*(destinations[9].(*[]string)) = []string{"title"}
		*(destinations[10].(*aiassist.JobState)) = aiassist.JobQueued
		*(destinations[11].(*int)) = 0
		*(destinations[12].(*int)) = 3
		*(destinations[13].(*string)) = ""
		*(destinations[16].(*string)) = ""
		*(destinations[18].(*string)) = ""
		*(destinations[19].(*string)) = ""
		*(destinations[20].(*string)) = "msp|client|tech|work|summary|browser-key"
		*(destinations[21].(*time.Time)) = at
		*(destinations[22].(*time.Time)) = at
		if len(destinations) > 25 {
			*(destinations[25].(*bool)) = true
		}
	}
}

func TestAIJobRepositoryClaimsQueuedOrExpiredJobsWithFencedLease(t *testing.T) {
	at := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	tx := &fakeSalesTx{queryRow: fakeRow{scan: scanAIJobTransition("job", aiassist.JobRunning, "lease", "", at)}}
	db := &fakeSalesDB{tx: tx}
	ids := aiassist.JobTransitionIDs{AuditID: "claim-audit", EventID: "claim-event", CorrelationID: "claim-correlation"}
	jobs, err := NewAIJobRepository(db, nil).Claim(context.Background(), 1, 1500*time.Millisecond, []aiassist.JobTransitionIDs{ids})
	if err != nil || len(jobs) != 1 || jobs[0].LeaseToken != "lease" {
		t.Fatalf("Claim() jobs=%+v error=%v", jobs, err)
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("Claim() must commit its lease update and evidence together: tx=%+v", tx)
	}
	assertQueryOrder(t, tx.calls, "UPDATE ai_generation_jobs", "INSERT INTO audit_ledger", "INSERT INTO event_outbox")
	if got := tx.args[0][0]; got != ids.AuditID || tx.args[0][6] != "ai.generation_job.claimed" || tx.args[0][10] != "worker" || tx.args[0][11] != "lease_acquired" || tx.args[0][12] != ids.CorrelationID {
		t.Fatalf("claim audit lost its durable worker fact: args=%v", tx.args[0])
	}
	if got := tx.args[1][0]; got != ids.EventID || tx.args[1][1] != "ai.generation_job.claimed" || tx.args[1][11] != ids.CorrelationID || tx.args[1][13] != "worker" || !strings.Contains(string(tx.args[1][14].([]byte)), `"reason":"lease_acquired"`) {
		t.Fatalf("claim event lost its durable worker fact: args=%v", tx.args[1])
	}
	for _, required := range []string{
		"state = 'queued'", "state = 'running' AND job.lease_until < now()",
		"ORDER BY job.created_at, job.id", "FOR UPDATE SKIP LOCKED",
		"attempt = job.attempt + 1", "lease_token = md5(random()::text || clock_timestamp()::text)::uuid",
		"lease_until = now() + ($1 * interval '1 microsecond')", "updated_at = now()",
	} {
		if !strings.Contains(tx.query, required) {
			t.Fatalf("Claim() omitted %q: %s", required, tx.query)
		}
	}
	if got := tx.queryArgs[0]; got != int64(1500*time.Millisecond/time.Microsecond) {
		t.Fatalf("lease microseconds=%v, want %d", got, int64(1500*time.Millisecond/time.Microsecond))
	}
}

func TestAIJobRepositoryClaimRollsBackLeaseWhenFactWriteFails(t *testing.T) {
	at := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	for _, failAt := range []int{1, 2} {
		t.Run("fact write "+string(rune('0'+failAt)), func(t *testing.T) {
			tx := &fakeSalesTx{queryRow: fakeRow{scan: scanAIJobTransition("job", aiassist.JobRunning, "lease", "", at)}, failAt: failAt}
			_, err := NewAIJobRepository(&fakeSalesDB{tx: tx}, nil).Claim(context.Background(), 1, time.Minute, []aiassist.JobTransitionIDs{{AuditID: "audit", EventID: "event", CorrelationID: "correlation"}})
			if err == nil || !tx.rolledBack || tx.committed {
				t.Fatalf("Claim() err=%v tx=%+v", err, tx)
			}
		})
	}
}

func TestAIJobRepositoryCompletesRecommendationAndFactsAtomicallyUnderLease(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewAIJobRepository(&fakeSalesDB{tx: tx}, nil)
	at := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	confidence := 0.8
	err := repository.Complete(context.Background(), aiassist.JobCompletion{
		JobID: "job", LeaseToken: "lease",
		Record: aiassist.RecommendationRecord{
			Recommendation: aiassist.Recommendation{ID: "recommendation", MSPID: "msp", ClientID: "client", WorkRecordID: "work", Feature: aiassist.FeatureSummary, Text: "Draft", Provider: "ollama", Model: "model", PromptVersion: "v1", Confidence: &confidence, State: aiassist.RecommendationPendingHuman, GeneratedAt: at, RelevantInputs: []string{"title"}},
			Usage:          aiassist.UsageRecord{ID: "usage", RecommendationID: "recommendation", MSPID: "msp", Provider: "ollama", Model: "model", RecordedAt: at},
			Audit:          mutation.AuditRecord{ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client", ActorType: "technician", ActorID: "tech", Action: "ai.recommendation.generated", SubjectType: "ai_recommendation", SubjectID: "recommendation", SubjectVersion: 1, Source: "ai", CorrelationID: "correlation"},
			Event:          mutation.EventRecord{EventID: "event", EventType: "ai.recommendation.generated", SchemaVersion: 1, OccurredAt: at, MSPID: "msp", ClientID: "client", ActorType: "technician", ActorID: "tech", SubjectType: "ai_recommendation", SubjectID: "recommendation", SubjectVersion: 1, Source: "ai", CorrelationID: "correlation"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	assertQueryOrder(t, tx.queries, "pg_advisory_xact_lock", "INSERT INTO ai_recommendations", "INSERT INTO ai_usage_records", "UPDATE ai_generation_jobs", "INSERT INTO audit_ledger", "INSERT INTO event_outbox")
	if !strings.Contains(tx.queries[3], "state = 'running' AND lease_token = $2") ||
		!strings.Contains(tx.queries[3], "reserved_cost_minor = 0") ||
		!strings.Contains(tx.queries[3], "connection.disclosure_accepted_at IS NOT NULL") ||
		!tx.committed || tx.rolledBack {
		t.Fatalf("completion is not atomically lease fenced: queries=%v tx=%+v", tx.queries, tx)
	}
}

func TestAIJobRepositoryCompletesClassificationUsageAndBudgetAtomically(t *testing.T) {
	at := time.Date(2026, time.August, 8, 2, 0, 0, 0, time.UTC)
	cost, input, output := int64(17), int64(120), int64(30)
	tx := &fakeSalesTx{queryRows: []row{
		fakeRow{scan: func(dest ...any) {
			*(dest[0].(*string)) = "11111111-1111-1111-1111-111111111111"
			*(dest[1].(*string)) = "22222222-2222-2222-2222-222222222222"
			*(dest[2].(*string)) = "33333333-3333-3333-3333-333333333333"
			*(dest[3].(*string)) = "44444444-4444-4444-4444-444444444444"
			*(dest[4].(*bool)) = false
			*(dest[5].(*float64)) = 0.9
			*(dest[6].(*int64)) = 3
		}},
		fakeRow{scan: func(dest ...any) { *(dest[0].(*int64)) = 2 }},
	}}
	err := NewAIJobRepository(&fakeSalesDB{tx: tx}, nil).Complete(context.Background(), aiassist.JobCompletion{
		JobID: "55555555-5555-5555-5555-555555555555", LeaseToken: "66666666-6666-6666-6666-666666666666",
		Record: aiassist.RecommendationRecord{
			ClassificationCandidates: []aiassist.ClassificationCandidate{},
			Usage:                    aiassist.UsageRecord{ID: "77777777-7777-7777-7777-777777777777", Provider: "openai", Model: "paid", InputUnits: &input, OutputUnits: &output, CostMinor: &cost, RecordedAt: at},
			Audit:                    mutation.AuditRecord{ID: "88888888-8888-8888-8888-888888888888", CorrelationID: "99999999-9999-9999-9999-999999999999"},
			Event:                    mutation.EventRecord{EventID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", CorrelationID: "99999999-9999-9999-9999-999999999999"},
		},
	})
	if err != nil {
		t.Fatalf("Complete(classification) error = %v", err)
	}
	assertQueryOrder(t, tx.calls, "pg_advisory_xact_lock", "SELECT job.msp_id", "UPDATE ai_generation_jobs job", "INSERT INTO ai_usage_records", "UPDATE tag_ai_suggestions", "INSERT INTO audit_ledger", "INSERT INTO event_outbox")
	jobUpdate := tx.calls[2]
	for _, required := range []string{"ai_policy.classification_model_profile_id", "sum(usage.cost_minor)", "sum(other.reserved_cost_minor)", "+ $4 > ai_policy.monthly_cost_limit_minor", "reserved_cost_minor = 0"} {
		if !strings.Contains(jobUpdate, required) {
			t.Fatalf("classification completion omitted %q: %s", required, jobUpdate)
		}
	}
	if !strings.Contains(tx.calls[3], "generation_job_id") || !tx.committed || tx.rolledBack {
		t.Fatalf("classification usage settlement is not atomic: calls=%v tx=%+v", tx.calls, tx)
	}
}

func TestAIJobRepositoryReservationSerializesSpendAndStoresReservationBeforeCredential(t *testing.T) {
	tx := &fakeSalesTx{queryRow: fakeRow{err: pgx.ErrNoRows}}
	repository := NewAIJobRepository(&fakeSalesDB{tx: tx}, nil)
	_, err := repository.AuthorizeAndReserve(context.Background(), "job", "lease", "fingerprint", 100, 50)
	if !errors.Is(err, aiassist.ErrJobLeaseLost) {
		t.Fatalf("AuthorizeAndReserve() error=%v", err)
	}
	if len(tx.queries) < 1 || !strings.Contains(tx.queries[0], "pg_advisory_xact_lock") {
		t.Fatalf("reservation did not take MSP transaction advisory lock: %v", tx.queries)
	}
}

func TestAIJobRepositoryLoadsLocalNetworkAcknowledgementForProviderExecution(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: pgx.ErrNoRows}}
	_, err := NewAIJobRepository(db, nil).LoadExecution(context.Background(), "job", "lease")
	if !errors.Is(err, aiassist.ErrJobLeaseLost) {
		t.Fatalf("LoadExecution() error=%v", err)
	}
	if !strings.Contains(db.query, "connection.local_network_acknowledged_at") {
		t.Fatalf("LoadExecution() omitted the local network acknowledgement required by the worker transport: %s", db.query)
	}
}

func TestAIJobRepositoryFailureReleasesReservation(t *testing.T) {
	at := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	tx := &fakeSalesTx{queryRow: fakeRow{scan: scanAIJobTransition("job", aiassist.JobQueued, "", "provider_unavailable", at)}}
	failure := aiassist.JobFailure{JobID: "job", LeaseToken: "lease", SafeErrorCode: "provider_unavailable", FailedAt: at, Retryable: true, IDs: aiassist.JobTransitionIDs{AuditID: "failure-audit", EventID: "failure-event", CorrelationID: "failure-correlation"}}
	err := NewAIJobRepository(&fakeSalesDB{tx: tx}, nil).Fail(context.Background(), failure)
	if err != nil || len(tx.calls) != 3 || !strings.Contains(tx.calls[0], "reserved_cost_minor = 0") ||
		!strings.Contains(tx.calls[1], "INSERT INTO audit_ledger") || !strings.Contains(tx.calls[2], "INSERT INTO event_outbox") {
		t.Fatalf("Fail() err=%v calls=%v", err, tx.calls)
	}
	if !strings.Contains(tx.calls[0], "NOT ($6 AND attempt < max_attempts) AND $3 = 'failed'") {
		t.Fatalf("retryable failure must not set completed_at while it requeues: %s", tx.calls[0])
	}
	if !strings.Contains(tx.calls[0], "UPDATE ai_generation_jobs AS job") {
		t.Fatalf("failure transition RETURNING clause requires the declared job alias: %s", tx.calls[0])
	}
	if !strings.Contains(tx.calls[0], "$5::timestamptz + interval '30 seconds'") {
		t.Fatalf("failure transition must type its timestamp parameter before interval arithmetic: %s", tx.calls[0])
	}
	if tx.args[0][0] != failure.IDs.AuditID || tx.args[0][6] != "ai.generation_job.requeued" || tx.args[0][11] != "provider_unavailable" || tx.args[0][12] != failure.IDs.CorrelationID ||
		tx.args[1][0] != failure.IDs.EventID || tx.args[1][1] != "ai.generation_job.requeued" || tx.args[1][11] != failure.IDs.CorrelationID {
		t.Fatalf("retryable failure did not emit the requeue fact with supplied IDs: args=%v", tx.args)
	}
}

func TestAIJobRepositoryTerminalFailureRollsBackWhenEvidenceFails(t *testing.T) {
	at := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	for _, failAt := range []int{1, 2} {
		t.Run("fact write "+string(rune('0'+failAt)), func(t *testing.T) {
			tx := &fakeSalesTx{queryRow: fakeRow{scan: scanAIJobTransition("job", aiassist.JobFailed, "", "provider_rejected", at)}, failAt: failAt}
			err := NewAIJobRepository(&fakeSalesDB{tx: tx}, nil).Fail(context.Background(), aiassist.JobFailure{JobID: "job", LeaseToken: "lease", SafeErrorCode: "provider_rejected", FailedAt: at, IDs: aiassist.JobTransitionIDs{AuditID: "audit", EventID: "event", CorrelationID: "correlation"}})
			if err == nil || !tx.rolledBack || tx.committed {
				t.Fatalf("Fail() err=%v tx=%+v", err, tx)
			}
		})
	}
}

func TestAIJobRepositoryTerminalTransitionsEmitSafeWorkerFacts(t *testing.T) {
	at := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name, code, action string
		state              aiassist.JobState
	}{
		{name: "failed", state: aiassist.JobFailed, code: "provider_rejected", action: "ai.generation_job.failed"},
		{name: "cancelled", state: aiassist.JobCancelled, code: "cancelled", action: "ai.generation_job.cancelled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeSalesTx{queryRow: fakeRow{scan: scanAIJobTransition("job", test.state, "", test.code, at)}}
			ids := aiassist.JobTransitionIDs{AuditID: "audit-" + test.name, EventID: "event-" + test.name, CorrelationID: "correlation-" + test.name}
			err := NewAIJobRepository(&fakeSalesDB{tx: tx}, nil).Fail(context.Background(), aiassist.JobFailure{JobID: "job", LeaseToken: "lease", SafeErrorCode: test.code, FailedAt: at, IDs: ids})
			if err != nil || !tx.committed || tx.rolledBack {
				t.Fatalf("Fail() err=%v tx=%+v", err, tx)
			}
			if tx.args[0][0] != ids.AuditID || tx.args[0][5] != "tech" || tx.args[0][6] != test.action || tx.args[0][10] != "worker" || tx.args[0][11] != test.code || tx.args[0][12] != ids.CorrelationID ||
				tx.args[1][0] != ids.EventID || tx.args[1][1] != test.action || tx.args[1][7] != "tech" || tx.args[1][11] != ids.CorrelationID || tx.args[1][13] != "worker" || !strings.Contains(string(tx.args[1][14].([]byte)), `"reason":"`+test.code+`"`) {
				t.Fatalf("terminal transition lost its safe worker facts: args=%v", tx.args)
			}
		})
	}
}

func TestAIJobRepositoryMaintainLeaseOnlyRenewsLeaseMetadata(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(*bool)) = true
		*(destinations[1].(*bool)) = false
	}}}
	maintenance, err := NewAIJobRepository(db, nil).MaintainLease(context.Background(), "job", "lease", 500*time.Millisecond)
	if err != nil || !maintenance.Owned || !strings.Contains(db.query, "lease_until = now() + ($3 * interval '1 microsecond')") || strings.Contains(db.query, "version = version + 1") || strings.Contains(db.query, "audit_ledger") || strings.Contains(db.query, "event_outbox") {
		t.Fatalf("MaintainLease() maintenance=%+v err=%v query=%s", maintenance, err, db.query)
	}
	if got := db.args[2]; got != int64(500*time.Millisecond/time.Microsecond) {
		t.Fatalf("lease microseconds=%v, want %d", got, int64(500*time.Millisecond/time.Microsecond))
	}
}

func TestLeaseMicrosecondsRoundsPositiveSubMicrosecondDurationUp(t *testing.T) {
	if got := leaseMicroseconds(500 * time.Nanosecond); got != 1 {
		t.Fatalf("leaseMicroseconds(500ns)=%d, want 1", got)
	}
}

func TestLeaseMicrosecondsPreservesPositiveDurationsWithoutOverflow(t *testing.T) {
	for _, test := range []struct {
		name  string
		lease time.Duration
		want  int64
	}{
		{name: "one nanosecond", lease: time.Nanosecond, want: 1},
		{name: "exact microsecond", lease: time.Microsecond, want: 1},
		{name: "maximum duration", lease: time.Duration(1<<63 - 1), want: int64((1<<63-1)/int64(time.Microsecond)) + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := leaseMicroseconds(test.lease); got != test.want {
				t.Fatalf("leaseMicroseconds(%s)=%d, want %d", test.lease, got, test.want)
			}
		})
	}
}

func TestAIJobRepositoryFencesCredentiallessConnectionBeforeNilCallback(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(*int)) = 0
		*(destinations[1].(*[]byte)) = nil
		*(destinations[2].(*[]byte)) = nil
	}}}
	session := &fakeSession{}
	db.session = session
	called := false
	err := NewAIJobRepository(db, nil).ExecuteWithCredential(context.Background(), aiassist.CredentialReference{
		ConnectionID: "connection", MSPID: "msp", ConnectionVersion: 2, ConnectionBaseURL: "http://ollama.test", ConnectionNetwork: aiassist.NetworkLocal, JobID: "job", LeaseToken: "lease", ExecutionFingerprint: "fingerprint",
	}, func(credential []byte) error {
		called = true
		if credential != nil {
			t.Fatalf("credentialless callback received %q", credential)
		}
		return nil
	})
	if err != nil || !called || !session.released || len(session.calls) != 3 ||
		!strings.Contains(session.calls[0], "pg_advisory_lock_shared") ||
		!strings.Contains(session.calls[1], "job.execution_fingerprint = $8") ||
		!strings.Contains(session.calls[1], "connection.base_url = $6") ||
		!strings.Contains(session.calls[1], "connection.network_mode = $7") ||
		!strings.Contains(session.calls[1], "connection.disclosure_accepted_at IS NOT NULL") ||
		!strings.Contains(session.calls[1], "execution_candidate_fingerprint") ||
		!strings.Contains(session.calls[1], "connection.version = $3") ||
		!strings.Contains(session.calls[2], "pg_advisory_unlock_shared") {
		t.Fatalf("ExecuteWithCredential() err=%v called=%v calls=%v released=%v", err, called, session.calls, session.released)
	}
}

func TestAIJobRepositoryRejectsRotatedCredentiallessEndpointBeforeCallback(t *testing.T) {
	called := false
	db := &fakeSalesDB{queryRow: fakeRow{err: pgx.ErrNoRows}}
	db.session = &fakeSession{}
	err := NewAIJobRepository(db, nil).ExecuteWithCredential(context.Background(), aiassist.CredentialReference{
		ConnectionID: "connection", MSPID: "msp", ConnectionVersion: 2, ConnectionBaseURL: "http://old-ollama.test", ConnectionNetwork: aiassist.NetworkLocal, JobID: "job", LeaseToken: "lease", ExecutionFingerprint: "fingerprint",
	}, func([]byte) error { called = true; return nil })
	if !errors.Is(err, aiassist.ErrExecutionFenceLost) || called {
		t.Fatalf("ExecuteWithCredential() err=%v called=%v", err, called)
	}
}

func TestAIJobRepositoryReleasesSessionLockAfterProviderPanic(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(*int)) = 0
		*(destinations[1].(*[]byte)) = nil
		*(destinations[2].(*[]byte)) = nil
	}}}
	session := &fakeSession{}
	db.session = session
	defer func() {
		if recovered := recover(); recovered != "provider panic" {
			t.Fatalf("panic=%v, want provider panic", recovered)
		}
		if !session.released || len(session.calls) != 3 || !strings.Contains(session.calls[2], "pg_advisory_unlock_shared") {
			t.Fatalf("panic cleanup calls=%v released=%v", session.calls, session.released)
		}
	}()
	_ = NewAIJobRepository(db, nil).ExecuteWithCredential(context.Background(), aiassist.CredentialReference{
		ConnectionID: "connection", MSPID: "msp", ConnectionVersion: 2, ConnectionBaseURL: "http://ollama.test", ConnectionNetwork: aiassist.NetworkLocal, JobID: "job", LeaseToken: "lease", ExecutionFingerprint: "fingerprint",
	}, func([]byte) error { panic("provider panic") })
}

func TestProviderConnectionAdvisoryLockBlocksManagementMutation(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL advisory-lock integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)
	db := &poolDatabase{pool: pool}
	session, err := db.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire execution session: %v", err)
	}
	defer session.Release()
	if _, err := session.Exec(ctx, providerConnectionAdvisoryLockSQL, "lock-msp", "lock-connection"); err != nil {
		t.Fatalf("take shared session lock: %v", err)
	}
	defer func() {
		_, _ = session.Exec(context.Background(), providerConnectionAdvisoryUnlockSQL, "lock-msp", "lock-connection")
	}()

	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		tx, err := db.Begin(ctx)
		if err == nil {
			err = lockProviderConnection(ctx, tx, "lock-msp", "lock-connection")
		}
		if err == nil {
			close(entered)
			err = tx.Commit(ctx)
		} else if tx != nil {
			_ = tx.Rollback(ctx)
		}
		done <- err
	}()
	select {
	case <-entered:
		t.Fatal("management mutation lock acquired while provider callback lock was held")
	case err := <-done:
		t.Fatalf("management mutation returned before release: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := session.Exec(ctx, providerConnectionAdvisoryUnlockSQL, "lock-msp", "lock-connection"); err != nil {
		t.Fatalf("release shared session lock: %v", err)
	}
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("management mutation failed after release: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("management mutation remained blocked after provider release")
	}
	if err := <-done; err != nil {
		t.Fatalf("management mutation completion: %v", err)
	}
}

func TestAIJobRepositorySweepStrandedKeepsOneAtomicFactPairPerTransition(t *testing.T) {
	tx := &fakeSalesTx{queryRow: fakeRow{scan: func(destinations ...any) { *(destinations[0].(*int)) = 1 }}}
	swept, err := NewAIJobRepository(&fakeSalesDB{tx: tx}, nil).SweepStranded(context.Background())
	if err != nil || swept != 1 || !tx.committed || tx.rolledBack || len(tx.calls) != 1 ||
		strings.Count(tx.calls[0], "INSERT INTO audit_ledger") != 1 || strings.Count(tx.calls[0], "INSERT INTO event_outbox") != 1 ||
		!strings.Contains(tx.calls[0], "'ai.generation_job.' || state") || !strings.Contains(tx.calls[0], "jsonb_build_object('reason', safe_error_code)") {
		t.Fatalf("SweepStranded() swept=%d err=%v tx=%+v", swept, err, tx)
	}
}

func scanAIJobTransition(id string, state aiassist.JobState, leaseToken, safeError string, at time.Time) func(...any) {
	return func(destinations ...any) {
		scanAIJob(id, "msp", "client", "work", "tech", at)(destinations...)
		*(destinations[10].(*aiassist.JobState)) = state
		*(destinations[13].(*string)) = leaseToken
		*(destinations[18].(*string)) = safeError
		*(destinations[24].(*int64)) = 2
	}
}

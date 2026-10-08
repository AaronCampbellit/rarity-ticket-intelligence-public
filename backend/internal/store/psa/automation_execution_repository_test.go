package psa

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/automation"
)

func TestAuthoritativeTagSnapshotKeepsOnlyImmutableIdentifiersAndProvenance(t *testing.T) {
	snapshot, err := automationEventSnapshot([]byte(`{
	  "tag_id":"11111111-1111-4111-8111-111111111111",
	  "direct_tag_ids":["22222222-2222-4222-8222-222222222222"],
	  "effective_tag_ids":["33333333-3333-4333-8333-333333333333"],
	  "group_ids":["44444444-4444-4444-8444-444444444444"],
	  "assignment_source":"automation",
	  "tag_label":"Renamed later",
	  "object_title":"Sensitive customer content",
	  "nested":{"body":"must not escape"}
	}`))
	if err != nil {
		t.Fatalf("automationEventSnapshot() error=%v", err)
	}
	want := map[string]string{
		"tag_id":            "11111111-1111-4111-8111-111111111111",
		"direct_tag_ids":    `["22222222-2222-4222-8222-222222222222"]`,
		"effective_tag_ids": `["33333333-3333-4333-8333-333333333333"]`,
		"group_ids":         `["44444444-4444-4444-8444-444444444444"]`,
		"assignment_source": "automation",
	}
	if got := authoritativeTagSnapshot(snapshot); !reflect.DeepEqual(got, want) {
		t.Fatalf("authoritativeTagSnapshot()=%v want=%v", got, want)
	}
}

func TestAutomationExecutionRepositoryPlansEveryEligibleEventOnce(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(*int)) = 2
	}}}
	repository := NewAutomationExecutionRepository(db)

	planned, err := repository.Plan(
		context.Background(), 25, time.Now().UTC(),
	)

	if err != nil || planned != 2 ||
		!strings.Contains(db.query, "INSERT INTO automation_execution_jobs") ||
		!strings.Contains(db.query, "INSERT INTO automation_event_plans") ||
		!strings.Contains(db.query, "version.version = definition.current_version") ||
		!strings.Contains(db.query, "event.event_type = version.trigger_event_type") ||
		!strings.Contains(db.query, "event.client_id = ANY(version.client_scopes)") ||
		!strings.Contains(
			db.query,
			"ON CONFLICT (event_id, automation_version_id, replay_generation)",
		) {
		t.Fatalf("Plan() planned=%d error=%v query=%s", planned, err, db.query)
	}
}

func TestAutomationExecutionRepositoryClaimsTypedPublishedDefinitions(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	definitionJSON := []byte(`{
	  "steps":[{
	    "id":"assign",
	    "kind":"action",
	    "action":{"kind":"assign","parameters":{"team_id":"team-id"}}
	  }]
	}`)
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*(destinations[0].(*string)) = "job-id"
			*(destinations[1].(*string)) = "automation-id"
			*(destinations[2].(*string)) = "msp-id"
			*(destinations[3].(*int64)) = 2
			*(destinations[4].(*[]string)) = []string{"client-id"}
			*(destinations[5].(*[]string)) = []string{"work_record.edit"}
			*(destinations[6].(*string)) = "work_record.created"
			*(destinations[7].(*[]byte)) = definitionJSON
			*(destinations[8].(*string)) = "event-id"
			*(destinations[9].(*string)) = "client-id"
			*(destinations[10].(*string)) = "cause-id"
			*(destinations[11].(*int)) = 0
			*(destinations[12].(*[]byte)) = []byte(
				`{"priority":"critical","count":2,"nested":{"unsafe":"ignored"}}`,
			)
		},
	}}}
	repository := NewAutomationExecutionRepository(db)

	jobs, err := repository.Claim(
		context.Background(), 25, now, 5*time.Minute,
	)

	if err != nil || len(jobs) != 1 ||
		jobs[0].ID != "job-id" ||
		jobs[0].Command.Definition.State != automation.Published ||
		jobs[0].Command.Definition.Steps[0].Action.Kind != automation.ActionAssign ||
		jobs[0].Command.Event.InputSnapshot["priority"] != "critical" ||
		jobs[0].Command.Event.InputSnapshot["count"] != "2" ||
		jobs[0].Command.Event.InputSnapshot["nested"] != "" ||
		jobs[0].Command.IdempotencyKey != "event-id|automation-id|2" ||
		jobs[0].Command.MaxAttempts != 3 ||
		!strings.Contains(db.query, "FOR UPDATE OF job SKIP LOCKED") {
		t.Fatalf("Claim() jobs=%+v error=%v query=%s", jobs, err, db.query)
	}
}

func TestAutomationExecutionRepositoryCompletesAndRetriesOnlyLeasedJob(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(*AutomationExecutionRepository) error
		want string
	}{
		{
			name: "complete",
			run: func(repository *AutomationExecutionRepository) error {
				return repository.Complete(
					context.Background(),
					automation.ExecutionCompletion{
						JobID: "job-id", RunID: "run-id",
						CompletedAt: time.Now().UTC(),
					},
				)
			},
			want: "state = 'processed'",
		},
		{
			name: "retry",
			run: func(repository *AutomationExecutionRepository) error {
				return repository.Fail(
					context.Background(),
					automation.ExecutionFailure{
						JobID: "job-id", FailedAt: time.Now().UTC(),
						NextAttemptAt: time.Now().UTC().Add(time.Minute),
						ErrorCode:     "execution_failed", Retry: true,
					},
				)
			},
			want: "state = 'pending'",
		},
		{
			name: "terminal",
			run: func(repository *AutomationExecutionRepository) error {
				return repository.Fail(
					context.Background(),
					automation.ExecutionFailure{
						JobID: "job-id", FailedAt: time.Now().UTC(),
						ErrorCode: "execution_denied",
					},
				)
			},
			want: "state = 'failed'",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeSalesTx{}
			repository := NewAutomationExecutionRepository(
				&fakeSalesDB{tx: tx},
			)
			if err := test.run(repository); err != nil {
				t.Fatalf("mutation error=%v", err)
			}
			if len(tx.queries) != 1 ||
				!strings.Contains(tx.queries[0], test.want) ||
				!strings.Contains(
					tx.queries[0],
					"state = 'processing' AND lease_until IS NOT NULL",
				) ||
				!tx.committed || tx.rolledBack {
				t.Fatalf(
					"queries=%v committed=%v rolledBack=%v",
					tx.queries, tx.committed, tx.rolledBack,
				)
			}
		})
	}
}

func TestAutomationExecutionRepositoryLoadsOnlyApprovedScopedHTTPConnection(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(*string)) = "connection-id"
		*(destinations[1].(*string)) = "msp-id"
		*(destinations[2].(*string)) = "client-id"
		*(destinations[3].(*string)) = "https://automation.example/hook"
		*(destinations[4].(*string)) =
			"env://RARITY_AUTOMATION_HTTP_SECRET_PRIMARY"
	}}}
	connection, err := NewAutomationExecutionRepository(db).Load(
		context.Background(), "connection-id", "msp-id", "client-id",
	)
	if err != nil || connection.ClientID != "client-id" ||
		!strings.Contains(db.query, "kind = 'external_http'") ||
		!strings.Contains(db.query, "$3::uuid = ANY(client_scopes)") ||
		!strings.Contains(db.query, "'automation.call_http' = ANY(capabilities)") ||
		!strings.Contains(db.query, "'automation.input.safe' = ANY(data_scopes)") ||
		!strings.Contains(db.query, "secret_refs->>'signing_secret'") {
		t.Fatalf("Load() connection=%+v error=%v query=%s", connection, err, db.query)
	}
}

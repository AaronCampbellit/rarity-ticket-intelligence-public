package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/config"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/graphintake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/setup"
)

type calendarProjectionEventSourceStub struct{ events []mutation.EventRecord }

func (s calendarProjectionEventSourceStub) ListCalendarProjectionEvents(context.Context, string, string, int) ([]mutation.EventRecord, error) {
	return s.events, nil
}

type calendarProjectionConsumerStub struct{ seen []string }

func (s *calendarProjectionConsumerStub) Handle(_ context.Context, event mutation.EventRecord) error {
	s.seen = append(s.seen, event.EventID)
	return nil
}

func TestCalendarProjectionRunnerDispatchesBoundedOrderedBatch(t *testing.T) {
	consumer := &calendarProjectionConsumerStub{}
	runner := calendarProjectionRunner{source: calendarProjectionEventSourceStub{events: []mutation.EventRecord{{EventID: "one"}, {EventID: "two"}}}, consumer: consumer, mspID: "msp", consumerKey: "calendar"}
	processed, err := runner.RunOnce(context.Background(), 100)
	if err != nil || processed != 2 || !reflect.DeepEqual(consumer.seen, []string{"one", "two"}) {
		t.Fatalf("processed=%d seen=%v err=%v", processed, consumer.seen, err)
	}
}

type testAttachmentStore struct{}

type aiJobRunnerFunc func(context.Context, int) (aiassist.WorkerResult, error)

type graphNotificationActionsStub struct{}

func (graphNotificationActionsStub) Accept(
	context.Context,
	[]byte,
) (graphintake.NotificationAcceptance, error) {
	return graphintake.NotificationAcceptance{}, nil
}

func (f aiJobRunnerFunc) RunOnce(ctx context.Context, limit int) (aiassist.WorkerResult, error) {
	return f(ctx, limit)
}

func (testAttachmentStore) Put(
	context.Context,
	string,
	io.Reader,
	int64,
	string,
) error {
	return nil
}

func (testAttachmentStore) Delete(context.Context, string) error {
	return nil
}

func TestRuntimeDependenciesComposePSAPersistence(t *testing.T) {
	provider, err := secrets.NewLocalProvider(bytes.Repeat([]byte{1}, 32), nil)
	if err != nil {
		t.Fatalf("NewLocalProvider() error=%v", err)
	}
	_, management, jobs, calendarAIProvider, err := buildAIRuntime(nil, provider)
	if err != nil {
		t.Fatalf("buildAIRuntime() error=%v", err)
	}
	fallback := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})
	dependencies := buildDependencies(
		nil, fallback, testAttachmentStore{}, provider, management, jobs, calendarAIProvider,
		"00000000-0000-4000-8000-000000000001",
		setup.SetupRuntimeConfiguration{},
		nil,
		bytes.Repeat([]byte{2}, 32),
		nil,
		graphNotificationActionsStub{},
	)

	value := reflect.ValueOf(dependencies)
	valueType := value.Type()
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		switch field.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
			reflect.Pointer, reflect.Slice:
			if field.IsNil() {
				t.Errorf("%s runtime dependency is not composed", valueType.Field(index).Name)
			}
		default:
			t.Fatalf(
				"%s has non-nilable kind %s; update the composition assertion",
				valueType.Field(index).Name,
				field.Kind(),
			)
		}
	}

	if dependencies.Principal == nil {
		t.Fatal("session principal resolver is not composed")
	}
	if dependencies.Sales == nil {
		t.Fatal("sales service is not composed")
	}
	if dependencies.Projects == nil {
		t.Fatal("projects service is not composed")
	}
	if dependencies.Proposals == nil {
		t.Fatal("proposals service is not composed")
	}
	if dependencies.Conversions == nil {
		t.Fatal("conversions service is not composed")
	}
	if dependencies.ChangeOrders == nil {
		t.Fatal("change order service is not composed")
	}
	if dependencies.AIDecisions == nil {
		t.Fatal("AI decision service is not composed")
	}
	if dependencies.AIManagement == nil {
		t.Fatal("AI management service is not composed")
	}
	if dependencies.AIJobs == nil {
		t.Fatal("AI job service is not composed")
	}
	if dependencies.AutomationDeadLetters == nil {
		t.Fatal("automation dead-letter service is not composed")
	}
	if dependencies.AutomationManagement == nil {
		t.Fatal("automation management service is not composed")
	}
	if dependencies.Locations == nil {
		t.Fatal("location service is not composed")
	}
	if dependencies.Contracts == nil {
		t.Fatal("contract service is not composed")
	}
	if dependencies.Contacts == nil {
		t.Fatal("contact service is not composed")
	}
	if dependencies.Services == nil {
		t.Fatal("service resource service is not composed")
	}
	if dependencies.Assets == nil {
		t.Fatal("asset service is not composed")
	}
	if dependencies.ClientResourceCatalog == nil {
		t.Fatal("client resource catalog service is not composed")
	}
	if dependencies.TagCatalog == nil || dependencies.TagAssociations == nil || dependencies.TagCreation == nil {
		t.Fatal("tagging services are not composed")
	}
	if dependencies.Datto == nil {
		t.Fatal("Datto management service is not composed")
	}
	if dependencies.DattoManagement == nil {
		t.Fatal("Datto connection management service is not composed")
	}
	if dependencies.WorkRecords == nil {
		t.Fatal("work-record service is not composed")
	}
	if dependencies.WorkAssignments == nil {
		t.Fatal("work assignment service is not composed")
	}
	if dependencies.WorkQueues == nil {
		t.Fatal("work queue service is not composed")
	}
	if dependencies.WorkMerges == nil {
		t.Fatal("work merge service is not composed")
	}
	if dependencies.WorkParticipants == nil {
		t.Fatal("work participant service is not composed")
	}
	if dependencies.Comments == nil {
		t.Fatal("comment service is not composed")
	}
	if dependencies.TimeEntries == nil {
		t.Fatal("time-entry service is not composed")
	}
	if dependencies.Attachments == nil {
		t.Fatal("attachment service is not composed")
	}
	if dependencies.Relationships == nil {
		t.Fatal("relationship service is not composed")
	}
	if dependencies.Tasks == nil {
		t.Fatal("task service is not composed")
	}
	if dependencies.Search == nil {
		t.Fatal("search service is not composed")
	}
	if dependencies.ServiceKeys == nil {
		t.Fatal("service-key lifecycle service is not composed")
	}
	if dependencies.Workflows == nil {
		t.Fatal("workflow management service is not composed")
	}
	if dependencies.NotificationPolicies == nil {
		t.Fatal("notification policy management service is not composed")
	}
	if dependencies.Knowledge == nil {
		t.Fatal("knowledge service is not composed")
	}
	if dependencies.BillingExports == nil {
		t.Fatal("billing export service is not composed")
	}
	if dependencies.IntegrationHealth == nil {
		t.Fatal("integration health service is not composed")
	}
	if dependencies.DirectIntake == nil {
		t.Fatal("direct intake service is not composed")
	}
	if dependencies.InboundWebhooks == nil {
		t.Fatal("inbound webhook service is not composed")
	}
	if dependencies.ForwardingIntake == nil {
		t.Fatal("forwarding intake service is not composed")
	}
	response := httptest.NewRecorder()
	dependencies.Fallback.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/readyz", nil),
	)
	if response.Code != http.StatusNoContent {
		t.Fatalf("fallback status = %d", response.Code)
	}
}

func TestRuntimeDependenciesFailClosedWithoutMentionCursorSecret(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("buildDependencies did not fail closed for a missing mention cursor secret")
		}
	}()
	buildDependencies(nil, http.NotFoundHandler(), testAttachmentStore{}, nil, nil, nil, nil,
		"00000000-0000-4000-8000-000000000001", setup.SetupRuntimeConfiguration{}, nil, nil, nil)
}

func TestBuildAIRuntimeComposesProviderNeutralProtectedRuntime(t *testing.T) {
	provider, err := secrets.NewLocalProvider(bytes.Repeat([]byte{1}, 32), nil)
	if err != nil {
		t.Fatalf("NewLocalProvider() error=%v", err)
	}
	worker, management, jobs, calendarAIProvider, err := buildAIRuntime(nil, provider)
	if err != nil || worker == nil || management == nil || jobs == nil || calendarAIProvider == nil {
		t.Fatalf("runtime worker=%v management=%v jobs=%v calendar=%v error=%v", worker, management, jobs, calendarAIProvider, err)
	}
}

func TestBuildAIRuntimeRejectsMissingProtectedSecretProvider(t *testing.T) {
	worker, management, jobs, calendarAIProvider, err := buildAIRuntime(nil, nil)
	if err == nil || worker != nil || management != nil || jobs != nil || calendarAIProvider != nil {
		t.Fatalf("runtime worker=%v management=%v jobs=%v calendar=%v error=%v", worker, management, jobs, calendarAIProvider, err)
	}
}

func TestBuildAIWorkspaceToolsRegistersOnlyClosedPermissionAwareTools(t *testing.T) {
	tools := buildAIWorkspaceTools(
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		func() string { return "system-id" },
	)
	got := make(map[string]string, len(tools))
	for _, tool := range tools {
		got[tool.Name()] = tool.RequiredCapability()
	}
	for name, capability := range map[string]string{
		"product.help":                "ai.assist",
		"navigation.find":             "ai.assist",
		"system.health":               "integration.read",
		"ticket.get":                  "work_record.read",
		"ticket.search":               "search.read",
		"ticket.transition":           "work_record.transition",
		"ticket.priority":             "work_record.edit",
		"ticket.note":                 "comment.internal.create",
		"ticket.reply":                "comment.public.create",
		"project.create":              "project.create",
		"task.create":                 "task.create",
		"project.search":              "project.read",
		"project.get":                 "project.read",
		"knowledge.search":            "knowledge.read",
		"knowledge.get":               "knowledge.read",
		"knowledge.draft.create":      "knowledge.edit",
		"knowledge.draft.revise":      "knowledge.edit",
		"prospect.list":               "opportunity.read",
		"prospect.create":             "prospect.create",
		"ticket.route":                "work_record.route",
		"client.create":               "client.create",
		"client_resource.list":        "search.read",
		"location.create":             "location.create",
		"contact.create":              "contact.create",
		"asset.create":                "asset.create",
		"service.create":              "service.create",
		"contract.create":             "contract.create",
		"location.update":             "location.update",
		"location.deactivate":         "location.lifecycle",
		"location.reactivate":         "location.lifecycle",
		"contact.update":              "contact.update",
		"contact.deactivate":          "contact.lifecycle",
		"contact.reactivate":          "contact.lifecycle",
		"asset.update":                "asset.update",
		"asset.deactivate":            "asset.lifecycle",
		"asset.reactivate":            "asset.lifecycle",
		"service.update":              "service.update",
		"service.deactivate":          "service.lifecycle",
		"service.reactivate":          "service.lifecycle",
		"contract.update":             "contract.update",
		"contract.deactivate":         "contract.lifecycle",
		"contract.reactivate":         "contract.lifecycle",
		"ticket.create":               "work_record.create",
		"ticket.assign":               "work_record.assign",
		"opportunity.list":            "opportunity.read",
		"opportunity.get":             "opportunity.read",
		"opportunity.transition":      "opportunity.transition",
		"opportunity.activity.create": "opportunity.activity.create",
		"proposal.list":               "proposal.read",
		"proposal.get":                "proposal.read",
		"proposal.create":             "proposal.create",
		"knowledge.publish":           "knowledge.publish",
	} {
		if got[name] != capability {
			t.Fatalf("tool %q capability=%q, want %q", name, got[name], capability)
		}
	}
	for _, name := range []string{
		"proposal.issue",
		"proposal.approve",
		"proposal.acceptance_grant.issue",
		"proposal.accept",
		"proposal.acceptance.record",
		"opportunity.convert",
		"proposal.version.create",
		"proposal.line.create",
		"project.financial.create",
		"routing.configure",
		"workflow.configure",
		"sla.configure",
		"integration.configure",
	} {
		if _, exposed := got[name]; exposed {
			t.Fatalf("high-impact tool %q must remain absent", name)
		}
	}
	if len(got) != 52 {
		t.Fatalf("registered tools=%d, want 52", len(got))
	}
}

func TestRunAIJobWorkerStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runs := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runAIJobWorker(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)),
			aiJobRunnerFunc(func(context.Context, int) (aiassist.WorkerResult, error) {
				runs <- struct{}{}
				return aiassist.WorkerResult{}, nil
			}),
		)
	}()
	<-runs
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("AI job worker did not stop after cancellation")
	}
}

func TestStopAIWorkerJoinsInFlightRunBeforePoolClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	finished := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		runAIJobWorker(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)),
			aiJobRunnerFunc(func(ctx context.Context, _ int) (aiassist.WorkerResult, error) {
				close(started)
				<-ctx.Done()
				close(finished)
				return aiassist.WorkerResult{}, ctx.Err()
			}),
		)
	}()
	<-started
	if !stopAIWorker(cancel, done, time.Second) {
		t.Fatal("stopAIWorker() timed out while an in-flight run honored cancellation")
	}
	select {
	case <-finished:
	default:
		t.Fatal("AI worker joined before its in-flight run completed")
	}
	closePool := func() {
		select {
		case <-finished:
		default:
			t.Fatal("pool would close underneath a live AI worker")
		}
	}
	closePool()
}

func TestFinalizeAPIShutdownClosesPoolOnlyAfterHTTPAndAIStop(t *testing.T) {
	for _, test := range []struct {
		name            string
		httpShutdownErr error
		aiDone          bool
		timeout         time.Duration
		want            shutdownResult
		wantCloseCalls  int
	}{
		{
			name: "normal shutdown", aiDone: true, timeout: time.Second,
			want:           shutdownResult{httpStopped: true, aiStopped: true, canClosePool: true},
			wantCloseCalls: 1,
		},
		{
			name: "AI join timeout", timeout: time.Millisecond,
			want: shutdownResult{httpStopped: true, aiStopped: false, canClosePool: false},
		},
		{
			name: "HTTP shutdown error", httpShutdownErr: errors.New("handler still active"), aiDone: true, timeout: time.Second,
			want: shutdownResult{httpStopped: false, aiStopped: true, canClosePool: false},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			if test.aiDone {
				close(done)
			}
			closeCalls := 0
			result := finalizeAPIShutdown(
				func() error {
					select {
					case <-ctx.Done():
					default:
						t.Fatal("AI worker was not cancelled before HTTP shutdown")
					}
					return test.httpShutdownErr
				},
				cancel, done, test.timeout,
				func() { closeCalls++ },
			)
			if result != test.want || closeCalls != test.wantCloseCalls {
				t.Fatalf("result=%+v closeCalls=%d, want result=%+v closeCalls=%d", result, closeCalls, test.want, test.wantCloseCalls)
			}
		})
	}
}

func TestBuildDattoAlertWorkerComposesIncidentRuntime(t *testing.T) {
	worker := buildDattoAlertWorker(nil)
	if worker == nil {
		t.Fatal("Datto alert worker is not composed")
	}
}

func TestBuildGraphSubscriptionWorkerRequiresConfiguredPublicCallback(t *testing.T) {
	secretProvider, err := secrets.NewLocalProvider(make([]byte, 32), nil)
	if err != nil {
		t.Fatalf("NewLocalProvider() error=%v", err)
	}
	worker, err := buildGraphSubscriptionWorker(
		nil, secretProvider,
		"https://rarity.example/api/v1/graph/notifications",
	)
	if err != nil || worker == nil {
		t.Fatalf("configured worker=%v error=%v", worker, err)
	}
	worker, err = buildGraphSubscriptionWorker(nil, secretProvider, "")
	if err != nil || worker != nil {
		t.Fatalf("unconfigured worker=%v error=%v", worker, err)
	}
}

func TestBuildAutomationExecutionWorkerComposesGovernedRuntime(t *testing.T) {
	executionWorker, continuationWorker := buildAutomationRuntime(nil)
	if executionWorker == nil || continuationWorker == nil {
		t.Fatal("automation execution worker is not composed")
	}
}

func TestNotificationDeliveryWorkerIsComposedWithRuntimeAdapters(t *testing.T) {
	if worker := buildNotificationDeliveryWorker(config.Config{}, nil, nil); worker == nil {
		t.Fatal("notification delivery worker is not composed")
	}
}

func TestCalendarNotificationRuntimeComposesInboxOnExistingPipeline(t *testing.T) {
	provider, err := secrets.NewLocalProvider(bytes.Repeat([]byte{1}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, management, jobs, calendarAIProvider, err := buildAIRuntime(nil, provider)
	if err != nil {
		t.Fatal(err)
	}
	dependencies := buildDependencies(
		nil, http.NotFoundHandler(), testAttachmentStore{}, provider,
		management, jobs, calendarAIProvider,
		"00000000-0000-4000-8000-000000000001",
		setup.SetupRuntimeConfiguration{}, nil, bytes.Repeat([]byte{2}, 32), nil,
		graphNotificationActionsStub{},
	)
	if _, ok := dependencies.NotificationInbox.(*notifications.InboxService); !ok {
		t.Fatalf("notification inbox dependency=%T", dependencies.NotificationInbox)
	}
	if worker := buildNotificationDeliveryWorker(config.Config{}, nil, nil); worker == nil {
		t.Fatal("calendar in-app delivery is not handled by the existing notification worker")
	}
}

func TestOutboundWebhookWorkerIsComposedWithRuntimeAdapters(t *testing.T) {
	if worker := buildOutboundWebhookWorker(nil); worker == nil {
		t.Fatal("outbound webhook worker is not composed")
	}
}

func TestApplyRuntimeSetupMakesStoredEntraConfigurationAuthoritative(t *testing.T) {
	base := config.Config{MSPID: "00000000-0000-4000-8000-000000000001"}
	runtime := setup.RuntimeConfiguration{
		MSPID: base.MSPID, EntraConfigured: true,
		EntraTenantID: "tenant", EntraClientID: "client",
		EntraClientSecret: "protected-secret",
		EntraRedirectURL:  "https://rarity.example/auth/entra/callback",
	}
	applied, err := applyRuntimeSetup(base, runtime, true)
	if err != nil {
		t.Fatal(err)
	}
	if applied.EntraMSPID != base.MSPID ||
		applied.EntraClientSecret != "protected-secret" ||
		applied.EntraDefaultRoleKey != "global-admin" {
		t.Fatalf("runtime setup not applied: %+v", applied)
	}
}

func TestApplyRuntimeSetupSupportsLocalOnlyInstallation(t *testing.T) {
	base := config.Config{
		MSPID:         "00000000-0000-4000-8000-000000000001",
		EntraTenantID: "stale-environment-tenant",
		EntraClientID: "stale-environment-client",
	}
	applied, err := applyRuntimeSetup(
		base,
		setup.RuntimeConfiguration{MSPID: base.MSPID, EntraState: "not_connected"},
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if applied.MSPID != base.MSPID || applied.EntraTenantID != "" ||
		applied.EntraClientID != "" {
		t.Fatalf("local-only runtime retained Entra configuration: %+v", applied)
	}
}

func TestApplyRuntimeSetupRejectsDeploymentMSPMismatch(t *testing.T) {
	_, err := applyRuntimeSetup(
		config.Config{MSPID: "00000000-0000-4000-8000-000000000001"},
		setup.RuntimeConfiguration{
			MSPID: "00000000-0000-4000-8000-000000000002",
		},
		true,
	)
	if err == nil {
		t.Fatal("expected MSP mismatch")
	}
}

func TestDynamicEntraHandlerLoadsConnectedConfigurationAfterStartup(t *testing.T) {
	available := false
	loads := 0
	handler := newDynamicEntraHandler(
		func(context.Context) (http.Handler, bool, error) {
			loads++
			if !available {
				return nil, false, nil
			}
			return http.HandlerFunc(func(
				writer http.ResponseWriter,
				request *http.Request,
			) {
				http.Redirect(
					writer,
					request,
					"https://login.microsoftonline.com/tenant/oauth2/v2.0/authorize",
					http.StatusFound,
				)
			}), true, nil
		},
		http.NotFoundHandler(),
	)

	before := httptest.NewRecorder()
	handler.ServeHTTP(
		before,
		httptest.NewRequest(http.MethodGet, "/auth/entra/login", nil),
	)
	available = true
	after := httptest.NewRecorder()
	handler.ServeHTTP(
		after,
		httptest.NewRequest(http.MethodGet, "/auth/entra/login", nil),
	)

	if before.Code != http.StatusNotFound ||
		after.Code != http.StatusFound ||
		loads != 2 {
		t.Fatalf(
			"before=%d after=%d loads=%d",
			before.Code,
			after.Code,
			loads,
		)
	}
}

func TestSecureBrowserCookiesFollowHTTPSPublicURL(t *testing.T) {
	if !secureBrowserCookies(config.Config{
		Environment: "demo",
		PublicURL:   "https://rarity.example",
	}) {
		t.Fatal("HTTPS demo deployment must use secure browser cookies")
	}
	if secureBrowserCookies(config.Config{
		Environment: "demo",
		PublicURL:   "http://localhost:8080",
	}) {
		t.Fatal("loopback HTTP demo must retain development cookies")
	}
}

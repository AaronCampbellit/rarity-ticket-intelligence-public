package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist/rtitools"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/attachments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/auditlog"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/automation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/billingexport"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/browserauth"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	calendaradapters "github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar/adapters"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/collaboration"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/comments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/commitments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/config"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/customfields"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/datto"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/graphintake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/health"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/httpapi"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/httpauth"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/identity"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/intake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/integrationhealth"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/links"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mentions"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/objectstorage"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/observability"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/routing"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/search"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/servicekeys"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sessions"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/setup"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	authstore "github.com/rarity-ticket-intelligence/rarity/backend/internal/store/authn"
	psastore "github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/timeentries"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/views"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

func main() {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		slog.Error("invalid runtime configuration", "error", err)
		os.Exit(1)
	}

	logger := observability.NewLogger(os.Stdout, slog.LevelInfo)
	metrics := observability.NewHTTPMetrics()
	mentionTelemetry := observability.NewMentionTelemetry(logger)
	logger.Info("starting rarity api", "environment", cfg.Environment, "revision", cfg.BuildRevision)
	if err := store.Migrate(context.Background(), cfg.DatabaseURL); err != nil {
		logger.Error("apply database migrations", "error", err)
		os.Exit(1)
	}
	pool, err := store.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("open database pool", "error", err)
		os.Exit(1)
	}
	attachmentStore, err := objectstorage.NewMinIOStore(objectstorage.Config{
		Endpoint: cfg.S3Endpoint, Bucket: cfg.S3Bucket, Region: cfg.S3Region,
		AccessKey: cfg.S3AccessKey, SecretKey: cfg.S3SecretKey,
	})
	if err != nil {
		logger.Error("configure attachment object storage", "error", err)
		os.Exit(1)
	}
	createAttachmentBucket := cfg.Environment == "local" ||
		cfg.Environment == "demo" || cfg.Environment == "test"
	if err := attachmentStore.EnsureBucket(
		context.Background(), createAttachmentBucket, cfg.S3Region,
	); err != nil {
		logger.Error("validate attachment object storage", "error", err)
		os.Exit(1)
	}
	secretProvider, err := secrets.NewLocalProvider(cfg.SecretKey, nil)
	if err != nil {
		logger.Error("configure protected secret provider", "error", err)
		os.Exit(1)
	}
	setupRepository := setup.NewPostgresRepository(pool)
	runtimeSetup, setupCompleted, err := setupRepository.RuntimeConfiguration(
		context.Background(), secretProvider,
	)
	if err != nil {
		logger.Error("load installation setup", "error", err)
		os.Exit(1)
	}
	cfg, err = applyRuntimeSetup(cfg, runtimeSetup, setupCompleted)
	if err != nil {
		logger.Error(
			"installation MSP does not match runtime configuration",
			"code", "installation_msp_mismatch",
		)
		os.Exit(1)
	}
	if err := issueFirstRunSetup(
		context.Background(),
		setupCompleted,
		cfg.PublicURL,
		setup.NewService(setupRepository, time.Now, nil),
		os.Stdout,
	); err != nil {
		logger.Error(
			"first-run setup authority unavailable",
			"code", "first_run_setup_unavailable",
		)
		os.Exit(1)
	}
	aiWorker, aiManagement, aiJobs, calendarAIProvider, err := buildAIRuntime(pool, secretProvider)
	if err != nil {
		logger.Error("configure AI provider runtime", "error", err)
		os.Exit(1)
	}
	taggingRepository := psastore.NewTaggingRepositoryFromPool(pool)
	taggingApplications := tagging.NewClassificationApplicationWorker(
		taggingRepository, tagging.NewAssociationService(taggingRepository), time.Now, id.New,
	)
	taggingProjectionWorker := tagging.NewProjectionWorker(
		psastore.NewTaggingProjectionRepositoryFromPool(pool),
	)
	graphWorker, err := buildGraphReconciliationWorker(
		pool, attachmentStore, secretProvider,
	)
	if err != nil {
		logger.Error("configure Graph reconciliation", "error", err)
		os.Exit(1)
	}
	graphNotificationWorker, err := buildGraphNotificationWorker(
		pool, attachmentStore, secretProvider,
	)
	if err != nil {
		logger.Error("configure Graph notification retrieval", "error", err)
		os.Exit(1)
	}
	graphSubscriptionWorker, err := buildGraphSubscriptionWorker(
		pool, secretProvider, cfg.GraphNotificationURL,
	)
	if err != nil {
		logger.Error("configure Graph subscriptions", "error", err)
		os.Exit(1)
	}
	dattoWorker, err := buildDattoSyncWorker(
		pool, attachmentStore, secretProvider,
	)
	if err != nil {
		logger.Error("configure Datto synchronization", "error", err)
		os.Exit(1)
	}
	dattoAlertWorker := buildDattoAlertWorker(pool)
	automationExecutionWorker, automationContinuationWorker :=
		buildAutomationRuntime(pool)

	fallback := health.NewHandlerWithMetrics(
		health.PostgresCheck{Pool: pool},
		cfg.BuildRevision,
		metrics,
	)
	graphNotificationRepository := psastore.NewGraphRepositoryFromPool(
		pool, secretProvider, id.New,
	).WithLegacyResolvers(
		graphintake.NewEnvironmentCredentialResolver(os.LookupEnv),
		graphintake.NewEnvironmentClientStateResolver(os.LookupEnv),
	)
	graphNotifications := graphintake.NewNotificationService(
		graphNotificationRepository,
		graphNotificationRepository,
		time.Now, id.New,
	)
	dependencies := buildDependencies(
		pool, fallback, attachmentStore, secretProvider, aiManagement, aiJobs, calendarAIProvider,
		cfg.MSPID,
		setup.SetupRuntimeConfiguration{
			PublicURL:  cfg.PublicURL,
			S3Endpoint: cfg.S3Endpoint, S3Bucket: cfg.S3Bucket,
			S3Region:                    cfg.S3Region,
			S3AccessKeyConfigured:       strings.TrimSpace(cfg.S3AccessKey) != "",
			S3CredentialConfigured:      strings.TrimSpace(cfg.S3SecretKey) != "",
			BackupEvidenceKeyConfigured: len(cfg.BackupEvidenceKey) >= 32,
		},
		cfg.BackupEvidenceKey,
		cfg.SecretKey,
		mentionTelemetry,
		graphNotifications,
	)
	dependencies.NotificationPreferences = notifications.NewPreferenceService(
		psastore.NewNotificationRepositoryFromPool(pool).WithExternalDelivery(
			cfg.PublicURL, cfg.SMTPHost != "",
		),
	)
	apiHandler := httpapi.NewRouter(dependencies)
	handler, err := buildBrowserAuthHandler(
		cfg, pool, setupRepository, secretProvider, apiHandler,
	)
	if err != nil {
		logger.Error("configure Entra browser authentication", "error", err)
		os.Exit(1)
	}
	server := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: observability.HTTPMiddleware(logger, metrics)(
			sessions.BrowserSecurityMiddleware(
				sessions.CSRFMiddleware(handler),
			),
		),
		ReadHeaderTimeout: 5 * time.Second,
	}
	workerContext, stopWorkers := context.WithCancel(context.Background())
	defer stopWorkers()
	aiWorkerContext, stopAIWorkerContext := context.WithCancel(workerContext)
	aiWorkerDone := make(chan struct{})
	go runSLAEvaluator(
		workerContext,
		logger,
		sla.NewEvaluator(
			psastore.NewSLARepositoryFromPool(pool), time.Now, id.New,
		),
	)
	go runNotificationPlanner(
		workerContext,
		logger,
		notifications.NewPlanner(
			psastore.NewNotificationRepositoryFromPool(pool).WithExternalDelivery(
				cfg.PublicURL, cfg.SMTPHost != "",
			), time.Now, id.New,
		).WithTelemetry(mentionTelemetry),
	)
	go runNotificationDeliveryWorker(
		workerContext, logger, buildNotificationDeliveryWorker(cfg, pool, secretProvider, mentionTelemetry),
	)
	go runMentionInvalidationWorker(
		workerContext, logger,
		mentions.NewInvalidationWorker(
			psastore.NewMentionRepositoryFromPool(pool), time.Now,
		).WithTelemetry(mentionTelemetry),
	)
	go runOutboundWebhookWorker(
		workerContext, logger, buildOutboundWebhookWorker(pool, secretProvider),
	)
	go runAutomationExecutionWorker(
		workerContext, logger, automationExecutionWorker,
	)
	go runAutomationContinuationWorker(
		workerContext, logger, automationContinuationWorker,
	)
	go runGraphReconciliationWorker(workerContext, logger, graphWorker)
	go runGraphNotificationWorker(
		workerContext, logger, graphNotificationWorker,
	)
	if graphSubscriptionWorker != nil {
		go runGraphSubscriptionWorker(
			workerContext, logger, graphSubscriptionWorker,
		)
	}
	go runDattoSyncWorker(workerContext, logger, dattoWorker)
	go runDattoAlertWorker(workerContext, logger, dattoAlertWorker)
	go func() {
		defer close(aiWorkerDone)
		runAIJobWorker(aiWorkerContext, logger, aiWorker)
	}()
	go runClassificationApplicationWorker(workerContext, logger, taggingApplications)
	go runTaggingProjectionWorker(workerContext, logger, taggingProjectionWorker)
	if strings.TrimSpace(cfg.MSPID) != "" {
		go runCalendarProjectionWorker(workerContext, logger, calendarProjectionRunner{
			source: dependencies.CalendarProjectionEvents, consumer: dependencies.CalendarProjectionWorker,
			mspID: cfg.MSPID, consumerKey: "calendar",
		})
		go runCalendarReminderWorker(workerContext, logger, dependencies.CalendarReminders, cfg.MSPID)
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("api server stopped", "error", err)
			os.Exit(1)
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	stopWorkers()

	// HTTP gets 10 seconds to drain active handlers. The AI worker then gets a
	// separate 20-second join budget, so shutdown can take up to 30 seconds.
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutdown := finalizeAPIShutdown(
		func() error { return server.Shutdown(shutdownContext) },
		stopAIWorkerContext, aiWorkerDone, 20*time.Second, pool.Close,
	)
	if !shutdown.httpStopped {
		logger.Error("API shutdown did not complete", "code", "api_shutdown_incomplete")
	}
	if !shutdown.aiStopped {
		logger.Error("AI worker shutdown timed out", "code", "ai_worker_shutdown_timeout")
	}
}

func applyRuntimeSetup(
	cfg config.Config,
	runtime setup.RuntimeConfiguration,
	completed bool,
) (config.Config, error) {
	if !completed {
		return cfg, nil
	}
	if cfg.MSPID != "" && cfg.MSPID != runtime.MSPID {
		return config.Config{}, errors.New("installation MSP mismatch")
	}
	cfg.MSPID = runtime.MSPID
	cfg.EntraMSPID = runtime.MSPID
	cfg.EntraTenantID = runtime.EntraTenantID
	cfg.EntraClientID = runtime.EntraClientID
	cfg.EntraClientSecret = runtime.EntraClientSecret
	cfg.EntraRedirectURL = runtime.EntraRedirectURL
	cfg.EntraDefaultRoleKey = "global-admin"
	return cfg, nil
}

func buildBrowserAuthHandler(
	cfg config.Config,
	pool *pgxpool.Pool,
	setupRepository *setup.PostgresRepository,
	secretProvider secrets.Provider,
	next http.Handler,
) (http.Handler, error) {
	sessionService := sessions.NewService(
		authstore.NewSessionStore(pool), time.Now, nil, id.New,
	)
	handler := next
	if cfg.MSPID != "" {
		breakGlass := identity.NewBreakGlassAuthenticator(
			cfg.MSPID, authstore.NewBreakGlassStore(pool, id.New), time.Now,
		)
		var err error
		handler, err = browserauth.NewBreakGlassHandler(browserauth.BreakGlassConfig{
			MSPID: cfg.MSPID, SessionTTL: 30 * time.Minute,
			Secure: secureBrowserCookies(cfg),
		}, breakGlass, sessionService, nil, handler)
		if err != nil {
			return nil, err
		}
	}
	fallback := handler
	return newDynamicEntraHandler(
		func(ctx context.Context) (http.Handler, bool, error) {
			resolved := cfg
			if setupRepository != nil {
				runtime, completed, err := setupRepository.RuntimeConfiguration(
					ctx,
					secretProvider,
				)
				if err != nil {
					return nil, false, err
				}
				resolved, err = applyRuntimeSetup(resolved, runtime, completed)
				if err != nil {
					return nil, false, err
				}
			}
			if resolved.EntraTenantID == "" {
				return nil, false, nil
			}
			entraHandler, err := buildEntraBrowserAuthHandler(
				resolved,
				pool,
				sessionService,
				fallback,
			)
			return entraHandler, err == nil, err
		},
		fallback,
	), nil
}

type entraHandlerLoader func(context.Context) (http.Handler, bool, error)

type dynamicEntraHandler struct {
	load entraHandlerLoader
	next http.Handler
}

func newDynamicEntraHandler(
	load entraHandlerLoader,
	next http.Handler,
) *dynamicEntraHandler {
	return &dynamicEntraHandler{load: load, next: next}
}

func (h *dynamicEntraHandler) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	if request.URL.Path != "/auth/entra/login" &&
		request.URL.Path != "/auth/entra/callback" {
		h.next.ServeHTTP(writer, request)
		return
	}
	handler, available, err := h.load(request.Context())
	if err != nil {
		http.Error(
			writer,
			"authentication unavailable",
			http.StatusServiceUnavailable,
		)
		return
	}
	if !available {
		h.next.ServeHTTP(writer, request)
		return
	}
	handler.ServeHTTP(writer, request)
}

func buildEntraBrowserAuthHandler(
	cfg config.Config,
	pool *pgxpool.Pool,
	sessionService *sessions.Service,
	next http.Handler,
) (http.Handler, error) {
	issuer := "https://login.microsoftonline.com/" +
		url.PathEscape(cfg.EntraTenantID) + "/v2.0"
	jwksURL := "https://login.microsoftonline.com/" +
		url.PathEscape(cfg.EntraTenantID) + "/discovery/v2.0/keys"
	verifier := identity.NewEntraVerifier(identity.EntraVerifierConfig{
		TenantID: cfg.EntraTenantID, ClientID: cfg.EntraClientID,
		Issuer: issuer, JWKSURL: jwksURL,
	}, nil, time.Now)
	authenticator := identity.NewAuthenticator(identity.Config{
		MSPID: cfg.EntraMSPID, TenantID: cfg.EntraTenantID,
		Issuer: issuer, Audience: cfg.EntraClientID,
		DefaultRoleKey: cfg.EntraDefaultRoleKey, JITEnabled: true,
	}, verifier, authstore.NewIdentityStore(pool), id.New, time.Now)
	return browserauth.NewEntraHandler(browserauth.Config{
		MSPID: cfg.EntraMSPID, TenantID: cfg.EntraTenantID,
		ClientID: cfg.EntraClientID, ClientSecret: cfg.EntraClientSecret,
		RedirectURL: cfg.EntraRedirectURL, SessionKey: cfg.SessionKey,
		SessionTTL: 8 * time.Hour,
	}, authenticator, sessionService, nil, time.Now, nil, next)
}

func secureBrowserCookies(cfg config.Config) bool {
	if cfg.Environment == "pilot" || cfg.Environment == "production" {
		return true
	}
	publicURL, err := url.Parse(cfg.PublicURL)
	return err == nil && strings.EqualFold(publicURL.Scheme, "https")
}

func buildGraphReconciliationWorker(
	pool *pgxpool.Pool,
	mimeStore graphintake.MIMEObjectStore,
	secretProvider secrets.Provider,
) (*graphintake.ReconciliationWorker, error) {
	repository := psastore.NewGraphRepositoryFromPool(
		pool, secretProvider, id.New,
	).WithLegacyResolvers(
		graphintake.NewEnvironmentCredentialResolver(os.LookupEnv),
		graphintake.NewEnvironmentClientStateResolver(os.LookupEnv),
	)
	sources, err := graphintake.NewRuntimeSourceFactory(
		graphintake.RuntimeSourceFactoryConfig{
			Client:       graphintake.NewProviderHTTPClient(),
			GraphBaseURL: "https://graph.microsoft.com/v1.0",
			TokenBaseURL: "https://login.microsoftonline.com",
			Credentials:  repository,
			MIMEStore:    mimeStore,
		},
	)
	if err != nil {
		return nil, err
	}
	return graphintake.NewReconciliationWorker(
		repository, sources, time.Now,
	), nil
}

func buildGraphNotificationWorker(
	pool *pgxpool.Pool,
	mimeStore graphintake.MIMEObjectStore,
	secretProvider secrets.Provider,
) (*graphintake.NotificationWorker, error) {
	repository := psastore.NewGraphRepositoryFromPool(
		pool, secretProvider, id.New,
	).WithLegacyResolvers(
		graphintake.NewEnvironmentCredentialResolver(os.LookupEnv),
		graphintake.NewEnvironmentClientStateResolver(os.LookupEnv),
	)
	sources, err := graphintake.NewRuntimeSourceFactory(
		graphintake.RuntimeSourceFactoryConfig{
			Client:       graphintake.NewProviderHTTPClient(),
			GraphBaseURL: "https://graph.microsoft.com/v1.0",
			TokenBaseURL: "https://login.microsoftonline.com",
			Credentials:  repository,
			MIMEStore:    mimeStore,
		},
	)
	if err != nil {
		return nil, err
	}
	return graphintake.NewNotificationWorker(
		repository, sources, time.Now,
	), nil
}

func buildGraphSubscriptionWorker(
	pool *pgxpool.Pool,
	secretProvider secrets.Provider,
	notificationURL string,
) (*graphintake.SubscriptionWorker, error) {
	if strings.TrimSpace(notificationURL) == "" {
		return nil, nil
	}
	repository := psastore.NewGraphRepositoryFromPool(
		pool, secretProvider, id.New,
	).WithLegacyResolvers(
		graphintake.NewEnvironmentCredentialResolver(os.LookupEnv),
		graphintake.NewEnvironmentClientStateResolver(os.LookupEnv),
	)
	providers, err := graphintake.NewRuntimeSubscriptionProviderFactory(
		graphintake.RuntimeSubscriptionProviderFactoryConfig{
			Client:       graphintake.NewProviderHTTPClient(),
			GraphBaseURL: "https://graph.microsoft.com/v1.0",
			TokenBaseURL: "https://login.microsoftonline.com",
			Credentials:  repository,
		},
	)
	if err != nil {
		return nil, err
	}
	return graphintake.NewSubscriptionWorker(
		repository, providers,
		repository,
		notificationURL, time.Now,
	), nil
}

func buildDattoSyncWorker(
	pool *pgxpool.Pool,
	payloadStore datto.PayloadObjectStore,
	secretProvider secrets.Provider,
) (*datto.SyncWorker, error) {
	repository := psastore.NewDattoRepositoryFromPool(
		pool, secretProvider, id.New,
	).WithLegacyCredentialResolver(
		datto.NewEnvironmentCredentialResolver(os.LookupEnv),
	)
	sources, err := datto.NewRuntimeSourceFactory(
		datto.RuntimeSourceFactoryConfig{
			Client:       datto.NewProviderHTTPClient(),
			Credentials:  repository,
			PayloadStore: payloadStore,
		},
	)
	if err != nil {
		return nil, err
	}
	return datto.NewSyncWorker(
		repository, sources, time.Now, id.New,
	), nil
}

func buildDattoAlertWorker(pool *pgxpool.Pool) *datto.AlertWorker {
	dattoRepository := psastore.NewDattoRepositoryFromPool(
		pool, nil, id.New,
	)
	workRecordRepository := psastore.NewWorkRecordRepositoryFromPool(pool)
	workRecordService := workrecords.NewService(
		workRecordRepository,
		psastore.NewRoutingRepositoryFromPool(pool),
		psastore.NewWorkflowRepositoryFromPool(pool),
		psastore.NewSLARepositoryFromPool(pool),
		time.Now, id.New,
		tagging.NewCreationPreparer(psastore.NewTaggingRepositoryFromPool(pool)),
	)
	return datto.NewAlertWorker(
		dattoRepository,
		datto.NewWorkRecordAlertIncidentWriter(
			workRecordRepository, workRecordService,
		),
		time.Now,
	)
}

func buildAutomationRuntime(
	pool *pgxpool.Pool,
) (*automation.ExecutionWorker, *automation.ContinuationWorker) {
	executionRepository :=
		psastore.NewAutomationExecutionRepositoryFromPool(pool)
	runRepository := psastore.NewAutomationRuntimeRepositoryFromPool(
		pool, id.New,
	)
	connections := automation.NewRuntimeConnections(
		executionRepository,
		automation.NewEnvironmentExternalSecretResolver(os.LookupEnv),
		webhooks.NewHTTPSender(nil),
		time.Now,
	)
	executor := buildAutomationActionExecutorWithExternal(pool, connections)
	engine := automation.NewEngine(
		runRepository, executor, connections, time.Now, id.New,
	)
	return automation.NewExecutionWorker(
			executionRepository, engine, time.Now,
		),
		automation.NewContinuationWorker(
			runRepository, engine, time.Now,
		)
}

func buildAutomationActionExecutor(pool *pgxpool.Pool) *automation.RuntimeActionExecutor {
	executionRepository := psastore.NewAutomationExecutionRepositoryFromPool(pool)
	connections := automation.NewRuntimeConnections(
		executionRepository,
		automation.NewEnvironmentExternalSecretResolver(os.LookupEnv),
		webhooks.NewHTTPSender(nil),
		time.Now,
	)
	return buildAutomationActionExecutorWithExternal(pool, connections)
}

func buildAutomationActionExecutorWithExternal(
	pool *pgxpool.Pool,
	external automation.ExternalActions,
) *automation.RuntimeActionExecutor {
	workRecordRepository :=
		psastore.NewWorkRecordRepositoryFromPool(pool)
	tagAssociationActions := tagging.NewAssociationService(
		psastore.NewTaggingRepositoryFromPool(pool),
	)
	tagTerminalGuard := tagging.NewTerminalGuard(tagAssociationActions)
	commentActions := automation.NewRuntimeCommentActions(
		comments.NewService(
			psastore.NewCommentRepositoryFromPool(pool), time.Now, id.New,
		),
		collaboration.NewService(
			psastore.NewCollaborationRepositoryFromPool(pool),
			mentions.NewService(time.Now, id.New), time.Now, id.New,
		),
		psastore.NewAutomationCommentActorRepositoryFromPool(pool),
	)
	return automation.NewRuntimeActionExecutor(
		workrecords.NewPriorityService(
			workRecordRepository, time.Now, id.New,
		),
		workrecords.NewAssignmentService(
			workRecordRepository, time.Now, id.New,
		),
		workrecords.NewTransitionService(
			workRecordRepository, time.Now, id.New, tagTerminalGuard,
		),
		commentActions,
		external,
		tagAssociationActions,
	)
}

// buildAIRuntime constructs the protected management and execution paths from
// one runtime secret provider. Management credential opening is snapshot-bound
// and job execution remains fenced by the same PostgreSQL-backed repository.
func buildAIRuntime(
	pool *pgxpool.Pool,
	secretProvider secrets.Provider,
) (*aiassist.JobWorker, *aiassist.ManagementService, *aiassist.JobService, aiassist.CalendarRecommendationProvider, error) {
	if secretProvider == nil {
		return nil, nil, nil, nil, aiassist.ErrProviderCredentialUnavailable
	}
	transport := aiassist.NewModeTransport(
		aiassist.NewRemoteTransport(nil),
		aiassist.NewLocalTransport(),
	)
	registry, err := aiassist.NewAdapterRegistry(
		aiassist.NewOllamaAdapter(transport),
		aiassist.NewOpenAICompatibleAdapter(transport),
	)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	managementRepository := psastore.NewAIManagementRepositoryFromPool(
		pool, secretProvider, id.New,
	)
	jobRepository := psastore.NewAIJobRepositoryFromPool(pool, secretProvider)
	return aiassist.NewJobWorker(jobRepository, registry, time.Now, id.New),
		aiassist.NewManagementService(
			managementRepository,
			aiassist.NewAdapterOperations(registry, managementRepository),
			time.Now, id.New,
		),
		aiassist.NewJobService(jobRepository, time.Now, id.New),
		aiassist.NewGovernedCalendarProvider(managementRepository, registry, managementRepository), nil
}

type proposalAIWorkspaceServices struct {
	opportunities *sales.Service
	proposals     *sales.ProposalService
}

func (s proposalAIWorkspaceServices) ResolveOpportunityReference(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	reference string,
	limit int,
) ([]sales.Opportunity, error) {
	return s.opportunities.ResolveOpportunityReference(
		ctx, principal, target, reference, limit,
	)
}

func (s proposalAIWorkspaceServices) GetOpportunityInTarget(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	opportunityID sales.OpportunityID,
) (sales.Opportunity, error) {
	return s.opportunities.GetOpportunityInTarget(
		ctx, principal, target, opportunityID,
	)
}

func (s proposalAIWorkspaceServices) PreflightCreateProposal(
	ctx context.Context,
	command sales.CreateProposalCommand,
) (sales.ProposalDraftPreflight, error) {
	return s.proposals.PreflightCreateProposal(ctx, command)
}

func (s proposalAIWorkspaceServices) CreateProposal(
	ctx context.Context,
	command sales.CreateProposalCommand,
) (sales.Proposal, error) {
	return s.proposals.CreateProposal(ctx, command)
}

func buildAIWorkspaceTools(
	knowledgeActions rtitools.ProductKnowledgeActions,
	workQueries rtitools.WorkRecordGetter,
	workTransitions rtitools.WorkRecordTransitioner,
	workPriorities rtitools.WorkRecordPrioritizer,
	commentActions rtitools.CommentCreator,
	searchActions rtitools.WorkRecordSearcher,
	healthActions rtitools.IntegrationHealthActions,
	projectActions rtitools.ProjectWorkspaceCreator,
	projectQueries rtitools.ProjectWorkspaceGetter,
	projectOperationalQueries rtitools.ProjectOperationalQueries,
	knowledgeOperationalQueries rtitools.KnowledgeOperationalQueries,
	prospectOperationalQueries rtitools.ProspectOperationalQueries,
	knowledgeDraftActions rtitools.KnowledgeDraftActions,
	prospectCreateActions rtitools.ProspectCreateActions,
	ticketRouteActions rtitools.TicketRouteActions,
	taskActions rtitools.TaskCreator,
	clientConflicts rtitools.ClientIdentityConflictChecker,
	clientCreator rtitools.ClientCreator,
	directory rtitools.ActiveClientResolver,
	clientResourceCatalog rtitools.ClientResourceCatalog,
	locationCreator rtitools.LocationCreator,
	contactCreator rtitools.ContactCreator,
	assetCreator rtitools.AssetCreator,
	serviceCreator rtitools.ServiceCreator,
	contractCreator rtitools.ContractCreator,
	locationLifecycle rtitools.ClientResourceLifecycle,
	contactLifecycle rtitools.ClientResourceLifecycle,
	assetLifecycle rtitools.ClientResourceLifecycle,
	serviceLifecycle rtitools.ClientResourceLifecycle,
	contractLifecycle rtitools.ClientResourceLifecycle,
	workRecordCreateActions rtitools.WorkRecordCreateActions,
	workRecordAssignmentResolver rtitools.WorkRecordAssignmentResolver,
	assignmentDirectory workrecords.AssignmentDirectory,
	workRecordAssigner rtitools.WorkRecordAssigner,
	opportunityQueries rtitools.OpportunityOperationalQueries,
	opportunityActions rtitools.OpportunityOperationalActions,
	proposalQueries rtitools.ProposalOperationalQueries,
	proposalActions rtitools.ProposalOperationalActions,
	knowledgePublishActions rtitools.KnowledgePublishActions,
	newID func() string,
) []aiassist.Tool {
	tools := []aiassist.Tool{
		rtitools.NewProductHelpTool(knowledgeActions),
		rtitools.NewNavigationTool(),
		rtitools.NewHealthTool(healthActions),
		rtitools.NewTicketGetTool(workQueries),
		rtitools.NewTicketSearchTool(searchActions),
		rtitools.NewTicketTransitionTool(workQueries, workTransitions),
		rtitools.NewTicketPriorityTool(workQueries, workPriorities),
		rtitools.NewTicketNoteTool(workQueries, commentActions),
		rtitools.NewTicketReplyTool(workQueries, commentActions),
		rtitools.NewProjectCreateTool(projectActions, newID),
		rtitools.NewProjectTaskCreateTool(projectQueries, taskActions),
		rtitools.NewProjectSearchTool(directory, projectOperationalQueries),
		rtitools.NewProjectGetTool(directory, projectOperationalQueries),
		rtitools.NewKnowledgeSearchTool(directory, knowledgeOperationalQueries),
		rtitools.NewKnowledgeGetTool(directory, knowledgeOperationalQueries),
		rtitools.NewProspectListTool(prospectOperationalQueries),
		rtitools.NewKnowledgeDraftCreateTool(directory, knowledgeDraftActions, newID),
		rtitools.NewKnowledgeDraftReviseTool(directory, knowledgeDraftActions),
		rtitools.NewProspectCreateTool(prospectCreateActions, newID),
		rtitools.NewTicketRouteTool(directory, ticketRouteActions),
		rtitools.NewClientCreateTool(clientConflicts, clientCreator, newID),
		rtitools.NewClientResourceListTool(directory, clientResourceCatalog),
		rtitools.NewLocationCreateTool(directory, locationCreator, newID),
		rtitools.NewContactCreateTool(directory, clientResourceCatalog, contactCreator, newID),
		rtitools.NewAssetCreateTool(directory, clientResourceCatalog, assetCreator, newID),
		rtitools.NewServiceCreateTool(directory, serviceCreator, newID),
		rtitools.NewContractCreateTool(directory, contractCreator, newID),
	}
	tools = append(tools,
		rtitools.NewLocationUpdateTool(directory, clientResourceCatalog, locationLifecycle),
		rtitools.NewLocationDeactivateTool(directory, clientResourceCatalog, locationLifecycle),
		rtitools.NewLocationReactivateTool(directory, clientResourceCatalog, locationLifecycle),
		rtitools.NewContactUpdateTool(directory, clientResourceCatalog, contactLifecycle),
		rtitools.NewContactDeactivateTool(directory, clientResourceCatalog, contactLifecycle),
		rtitools.NewContactReactivateTool(directory, clientResourceCatalog, contactLifecycle),
		rtitools.NewAssetUpdateTool(directory, clientResourceCatalog, assetLifecycle),
		rtitools.NewAssetDeactivateTool(directory, clientResourceCatalog, assetLifecycle),
		rtitools.NewAssetReactivateTool(directory, clientResourceCatalog, assetLifecycle),
		rtitools.NewServiceUpdateTool(directory, clientResourceCatalog, serviceLifecycle),
		rtitools.NewServiceDeactivateTool(directory, clientResourceCatalog, serviceLifecycle),
		rtitools.NewServiceReactivateTool(directory, clientResourceCatalog, serviceLifecycle),
		rtitools.NewContractUpdateTool(directory, clientResourceCatalog, contractLifecycle),
		rtitools.NewContractDeactivateTool(directory, clientResourceCatalog, contractLifecycle),
		rtitools.NewContractReactivateTool(directory, clientResourceCatalog, contractLifecycle),
		rtitools.NewTicketCreateTool(directory, clientResourceCatalog, workRecordCreateActions, newID),
		rtitools.NewTicketAssignTool(directory, workRecordAssignmentResolver, assignmentDirectory, workRecordAssigner),
		rtitools.NewOpportunityListTool(directory, opportunityQueries),
		rtitools.NewOpportunityGetTool(directory, opportunityQueries),
		rtitools.NewOpportunityTransitionTool(directory, opportunityActions),
		rtitools.NewOpportunityActivityCreateTool(directory, opportunityActions),
		rtitools.NewProposalListTool(directory, proposalQueries),
		rtitools.NewProposalGetTool(directory, proposalQueries),
		rtitools.NewProposalCreateTool(directory, proposalActions),
		rtitools.NewKnowledgePublishTool(directory, knowledgePublishActions),
	)
	return tools
}

type aiJobRunner interface {
	RunOnce(context.Context, int) (aiassist.WorkerResult, error)
}

type shutdownResult struct {
	httpStopped, aiStopped, canClosePool bool
}

// finalizeAPIShutdown cancels provider work before draining HTTP handlers.
// The pool remains open unless both drains finish: HTTP handlers and an
// in-flight worker can each still depend on it. Process teardown is safer than
// closing the pool under either live operation.
func finalizeAPIShutdown(
	httpShutdown func() error,
	cancelAI context.CancelFunc,
	aiDone <-chan struct{},
	aiTimeout time.Duration,
	closePool func(),
) shutdownResult {
	if cancelAI != nil {
		cancelAI()
	}
	result := shutdownResult{}
	if httpShutdown != nil && httpShutdown() == nil {
		result.httpStopped = true
	}
	result.aiStopped = stopAIWorker(cancelAI, aiDone, aiTimeout)
	result.canClosePool = result.httpStopped && result.aiStopped
	if result.canClosePool && closePool != nil {
		closePool()
	}
	return result
}

func stopAIWorker(cancel context.CancelFunc, done <-chan struct{}, timeout time.Duration) bool {
	if cancel != nil {
		cancel()
	}
	if done == nil || timeout <= 0 {
		return false
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func runAIJobWorker(ctx context.Context, logger *slog.Logger, worker aiJobRunner) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		result, code := runAIJobWorkerOnce(ctx, worker)
		if code != "" && ctx.Err() == nil {
			logger.Error("process AI generation jobs", "code", code)
		} else if result.Claimed > 0 || result.Completed > 0 || result.Failed > 0 ||
			result.Cancelled > 0 || result.Stranded > 0 {
			logger.Info(
				"processed AI generation jobs",
				"claimed", result.Claimed,
				"completed", result.Completed,
				"failed", result.Failed,
				"cancelled", result.Cancelled,
				"stranded", result.Stranded,
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type classificationApplicationRunner interface {
	RunOnce(context.Context, int) error
}

func runClassificationApplicationWorker(ctx context.Context, logger *slog.Logger, worker classificationApplicationRunner) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := worker.RunOnce(ctx, 10); err != nil && ctx.Err() == nil {
			logger.Error("apply AI classification suggestions", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runAIJobWorkerOnce(ctx context.Context, worker aiJobRunner) (result aiassist.WorkerResult, code string) {
	defer func() {
		if recover() != nil {
			result, code = aiassist.WorkerResult{}, "worker_panic"
		}
	}()
	result, err := worker.RunOnce(ctx, 25)
	if err != nil {
		return result, "worker_failed"
	}
	return result, ""
}

func runGraphReconciliationWorker(
	ctx context.Context,
	logger *slog.Logger,
	worker *graphintake.ReconciliationWorker,
) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		result, err := worker.RunOnce(ctx, 100)
		if err != nil && ctx.Err() == nil {
			logger.Error("reconcile Graph mailbox intake", "error", err)
		} else if result.Planned > 0 || result.Claimed > 0 {
			logger.Info(
				"reconciled Graph mailbox intake",
				"planned", result.Planned,
				"claimed", result.Claimed,
				"processed", result.Processed,
				"failed", result.Failed,
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runGraphNotificationWorker(
	ctx context.Context,
	logger *slog.Logger,
	worker *graphintake.NotificationWorker,
) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		result, err := worker.RunOnce(ctx, 100)
		if err != nil && ctx.Err() == nil {
			logger.Error("retrieve Graph notification messages", "error", err)
		} else if result.Claimed > 0 {
			logger.Info(
				"retrieved Graph notification messages",
				"claimed", result.Claimed,
				"processed", result.Processed,
				"failed", result.Failed,
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runGraphSubscriptionWorker(
	ctx context.Context,
	logger *slog.Logger,
	worker *graphintake.SubscriptionWorker,
) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		result, err := worker.RunOnce(ctx, 25)
		if err != nil && ctx.Err() == nil {
			logger.Error("provision Graph subscriptions", "error", err)
		} else if result.Claimed > 0 {
			logger.Info(
				"provisioned Graph subscriptions",
				"claimed", result.Claimed,
				"created", result.Created,
				"renewed", result.Renewed,
				"failed", result.Failed,
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runDattoSyncWorker(
	ctx context.Context,
	logger *slog.Logger,
	worker *datto.SyncWorker,
) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		result, err := worker.RunOnce(ctx, 25)
		if err != nil && ctx.Err() == nil {
			logger.Error("synchronize Datto RMM", "error", err)
		} else if result.Claimed > 0 {
			logger.Info(
				"synchronized Datto RMM",
				"claimed", result.Claimed,
				"succeeded", result.Succeeded,
				"failed", result.Failed,
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runDattoAlertWorker(
	ctx context.Context,
	logger *slog.Logger,
	worker *datto.AlertWorker,
) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		result, err := worker.RunOnce(ctx, 100)
		if err != nil && ctx.Err() == nil {
			logger.Error("process Datto RMM alerts", "error", err)
		} else if result.Claimed > 0 {
			logger.Info(
				"processed Datto RMM alerts",
				"claimed", result.Claimed,
				"created", result.Created,
				"updated", result.Updated,
				"recovered", result.Recovered,
				"ignored", result.Ignored,
				"failed", result.Failed,
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runAutomationExecutionWorker(
	ctx context.Context,
	logger *slog.Logger,
	worker *automation.ExecutionWorker,
) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		result, err := worker.RunOnce(ctx, 100)
		if err != nil && ctx.Err() == nil {
			logger.Error("execute automation", "error", err)
		} else if result.Planned > 0 || result.Claimed > 0 {
			logger.Info(
				"executed automation",
				"planned", result.Planned,
				"claimed", result.Claimed,
				"processed", result.Processed,
				"failed", result.Failed,
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runAutomationContinuationWorker(
	ctx context.Context,
	logger *slog.Logger,
	worker *automation.ContinuationWorker,
) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		result, err := worker.RunOnce(ctx, 100)
		if err != nil && ctx.Err() == nil {
			logger.Error("resume automation", "error", err)
		} else if result.Claimed > 0 {
			logger.Info(
				"resumed automation",
				"claimed", result.Claimed,
				"resumed", result.Resumed,
				"failed", result.Failed,
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runSLAEvaluator(
	ctx context.Context,
	logger *slog.Logger,
	evaluator *sla.Evaluator,
) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		result, err := evaluator.RunOnce(ctx, 500)
		if err != nil && ctx.Err() == nil {
			logger.Error("evaluate SLA timers", "error", err)
		} else if result.Updated > 0 || result.Conflicts > 0 {
			logger.Info(
				"evaluated SLA timers",
				"evaluated", result.Evaluated,
				"updated", result.Updated,
				"conflicts", result.Conflicts,
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runNotificationPlanner(
	ctx context.Context,
	logger *slog.Logger,
	planner *notifications.Planner,
) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		result, err := planner.RunOnce(ctx, 500)
		if err != nil && ctx.Err() == nil {
			logger.Error("plan notifications", "error", err)
		} else if result.Pending > 0 || result.Suppressed > 0 {
			logger.Info(
				"planned notifications",
				"events", result.Events,
				"pending", result.Pending,
				"suppressed", result.Suppressed,
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func buildNotificationDeliveryWorker(
	cfg config.Config,
	pool *pgxpool.Pool,
	secretProvider secrets.Provider,
	telemetry ...*observability.MentionTelemetry,
) *notifications.DeliveryWorker {
	repository := psastore.NewNotificationRepositoryFromPool(pool).WithExternalDelivery(
		cfg.PublicURL, cfg.SMTPHost != "",
	)
	secretResolver := psastore.NewTeamsConnectionRepositoryFromPool(
		pool,
		secretProvider,
		notifications.NewEnvironmentSecretResolver(os.LookupEnv),
	)
	teams := notifications.NewTeamsService(
		secretResolver,
		notifications.NewHTTPTeamsTransport(),
		repository,
		time.Now,
	)
	var email *notifications.EmailService
	if cfg.SMTPHost != "" {
		transport, err := notifications.NewSMTPTransport(notifications.SMTPConfig{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword, From: cfg.SMTPFrom,
		})
		if err == nil {
			email = notifications.NewEmailService(transport)
		}
	}
	worker := notifications.NewDeliveryWorkerWithEmail(repository, teams, email, time.Now)
	if len(telemetry) != 0 {
		worker.WithTelemetry(telemetry[0])
	}
	return worker
}

func runMentionInvalidationWorker(
	ctx context.Context,
	logger *slog.Logger,
	worker *mentions.InvalidationWorker,
) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		result, err := worker.RunOnce(ctx, 100)
		if err != nil && ctx.Err() == nil {
			logger.Error("process mention access invalidations", "code", "mention_invalidation_failed")
		} else if result.Claimed > 0 {
			logger.Info(
				"processed mention access invalidations",
				"claimed", result.Claimed,
				"completed", result.Completed,
				"suppressed", result.Suppressed,
				"deliveries_canceled", result.DeliveriesCanceled,
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runTaggingProjectionWorker(ctx context.Context, logger *slog.Logger, worker *tagging.ProjectionWorker) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		result, err := worker.RunOnce(ctx, 500)
		if err != nil && ctx.Err() == nil {
			logger.Error("project classification reports", "error", err)
		} else if result.Processed > 0 {
			logger.Info("projected classification report batch",
				"processed", result.Processed,
				"inherited_effects", result.InheritedEffects,
				"projection_as_of", result.ProjectionAsOf,
				"lag_seconds", result.LagSeconds,
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type calendarProjectionEventSource interface {
	ListCalendarProjectionEvents(context.Context, string, string, int) ([]mutation.EventRecord, error)
}
type calendarProjectionConsumer interface {
	Handle(context.Context, mutation.EventRecord) error
}
type calendarProjectionRunner struct {
	source      calendarProjectionEventSource
	consumer    calendarProjectionConsumer
	mspID       string
	consumerKey string
}

func (r calendarProjectionRunner) RunOnce(ctx context.Context, limit int) (int, error) {
	if r.source == nil || r.consumer == nil || strings.TrimSpace(r.mspID) == "" || strings.TrimSpace(r.consumerKey) == "" || limit < 1 || limit > 500 {
		return 0, calendar.ErrInvalidProjectionBatch
	}
	events, err := r.source.ListCalendarProjectionEvents(ctx, r.mspID, r.consumerKey, limit)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, event := range events {
		if err = r.consumer.Handle(ctx, event); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func runCalendarProjectionWorker(ctx context.Context, logger *slog.Logger, runner calendarProjectionRunner) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		processed, err := runner.RunOnce(ctx, 100)
		if err != nil && ctx.Err() == nil {
			logger.Error("project calendar events", "error", err)
		} else if processed > 0 {
			logger.Info("projected calendar events", "processed", processed)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runCalendarReminderWorker(ctx context.Context, logger *slog.Logger, service *calendar.ReminderService, mspID string) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		facts, err := service.EvaluateDue(ctx, mspID, 500)
		if err != nil && ctx.Err() == nil {
			logger.Error("evaluate calendar reminders", "error", err)
		} else if len(facts) > 0 {
			logger.Info("emitted calendar reminders", "count", len(facts))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runNotificationDeliveryWorker(
	ctx context.Context,
	logger *slog.Logger,
	worker *notifications.DeliveryWorker,
) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		result, err := worker.RunOnce(ctx, 100)
		if err != nil && ctx.Err() == nil {
			logger.Error("deliver notifications", "error", err)
		} else if result.Claimed > 0 {
			logger.Info(
				"delivered notification batch",
				"claimed", result.Claimed,
				"delivered", result.Delivered,
				"retrying", result.Retrying,
				"failed", result.Failed,
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func buildOutboundWebhookWorker(
	pool *pgxpool.Pool,
	secretProvider ...secrets.Provider,
) *webhooks.OutboundWorker {
	repository := psastore.NewWebhookOutboundRepositoryFromPool(pool, id.New)
	var resolver webhooks.InboundSecretResolver = webhooks.NewEnvironmentInboundSecretResolver(os.LookupEnv)
	if len(secretProvider) > 0 && secretProvider[0] != nil {
		resolver = psastore.NewWebhookManagementRepositoryFromPool(
			pool, secretProvider[0], resolver, id.New,
		)
	}
	publisher := webhooks.NewResolvingPublisher(
		resolver,
		webhooks.NewPublisher(
			webhooks.NewHTTPSender(nil), repository, time.Now,
		),
	)
	return webhooks.NewOutboundWorker(repository, publisher, time.Now)
}

func runOutboundWebhookWorker(
	ctx context.Context,
	logger *slog.Logger,
	worker *webhooks.OutboundWorker,
) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		result, err := worker.RunOnce(ctx, 100)
		if err != nil && ctx.Err() == nil {
			logger.Error("deliver outbound webhooks", "error", err)
		} else if result.Planned > 0 || result.Claimed > 0 {
			logger.Info(
				"delivered outbound webhook batch",
				"planned", result.Planned,
				"claimed", result.Claimed,
				"delivered", result.Delivered,
				"retrying", result.Retrying,
				"failed", result.Failed,
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func buildDependencies(
	pool *pgxpool.Pool,
	fallback http.Handler,
	attachmentStore attachments.ObjectStore,
	secretProvider secrets.Provider,
	aiManagement httpapi.AIManagementActions,
	aiJobs httpapi.AIJobActions,
	calendarAIProvider aiassist.CalendarRecommendationProvider,
	expectedMSPID string,
	setupRuntime setup.SetupRuntimeConfiguration,
	backupEvidenceKey []byte,
	mentionCursorKey []byte,
	mentionTelemetry *observability.MentionTelemetry,
	graphNotifications ...httpapi.GraphNotificationActions,
) httpapi.Dependencies {
	if len(mentionCursorKey) < 32 {
		panic("compose mention query service: cursor signing key must be at least 32 bytes")
	}
	salesRepository := psastore.NewSalesRepositoryFromPool(pool)
	projectRepository := psastore.NewProjectRepositoryFromPool(pool)
	proposalRepository := psastore.NewProposalRepositoryFromPool(pool, id.New)
	snapshotStore := psastore.NewSnapshotStoreFromPool(pool, time.Now, id.New)
	acceptanceVerifier := psastore.NewAcceptanceVerifierFromPool(pool, time.Now)
	conversionRepository := psastore.NewConversionRepositoryFromPool(pool, id.New)
	changeOrderRepository := psastore.NewChangeOrderRepositoryFromPool(pool)
	aiRepository := psastore.NewAIRepositoryFromPool(pool)
	aiWorkspaceRepository := psastore.NewAIWorkspaceRepositoryFromPool(pool)
	automationRepository := psastore.NewAutomationRepositoryFromPool(pool)
	locationRepository := psastore.NewLocationRepositoryFromPool(pool)
	contractRepository := psastore.NewContractRepositoryFromPool(pool)
	contactRepository := psastore.NewContactRepositoryFromPool(pool)
	serviceResourceRepository := psastore.NewServiceResourceRepositoryFromPool(pool)
	assetRepository := psastore.NewAssetRepositoryFromPool(pool)
	workRecordRepository := psastore.NewWorkRecordRepositoryFromPool(pool)
	commentRepository := psastore.NewCommentRepositoryFromPool(pool)
	timeEntryRepository := psastore.NewTimeEntryRepositoryFromPool(pool)
	timeWorkforceRepository := psastore.NewTimeWorkforceRepositoryFromPool(pool)
	taggingRepository := psastore.NewTaggingRepositoryFromPool(pool)
	taggingProjectionRepository := psastore.NewTaggingProjectionRepositoryFromPool(pool)
	collaborationRepository := psastore.NewCollaborationRepositoryFromPool(pool).WithTelemetry(mentionTelemetry)
	mentionRepository := psastore.NewMentionRepositoryFromPool(pool)
	timeCaptureService := timeentries.NewCaptureService(
		timeWorkforceRepository,
		time.Now,
		id.New,
		tagging.NewCreationPreparer(taggingRepository),
	)
	attachmentRepository := psastore.NewAttachmentRepositoryFromPool(pool)
	linkRepository := psastore.NewLinkRepositoryFromPool(pool)
	taskRepository := psastore.NewTaskRepositoryFromPool(pool)
	searchRepository := psastore.NewSearchRepositoryFromPool(pool)
	clientResourceCatalogRepository :=
		psastore.NewClientResourceCatalogRepositoryFromPool(pool)
	workflowRepository := psastore.NewWorkflowRepositoryFromPool(pool)
	routingRepository := psastore.NewRoutingRepositoryFromPool(pool)
	slaRepository := psastore.NewSLARepositoryFromPool(pool)
	notificationRepository := psastore.NewNotificationRepositoryFromPool(pool)
	teamsConnectionRepository := psastore.NewTeamsConnectionRepositoryFromPool(
		pool,
		secretProvider,
		notifications.NewEnvironmentSecretResolver(os.LookupEnv),
	)
	knowledgeRepository := psastore.NewKnowledgeRepositoryFromPool(pool)
	billingExportRepository := psastore.NewBillingExportRepositoryFromPool(pool)
	integrationHealthRepository := psastore.NewIntegrationHealthRepositoryFromPool(pool)
	dattoRepository := psastore.NewDattoRepositoryFromPool(
		pool, secretProvider, id.New,
	).WithLegacyCredentialResolver(
		datto.NewEnvironmentCredentialResolver(os.LookupEnv),
	)
	systemIntakeRepository := psastore.NewSystemIntakeRepositoryFromPool(pool)
	webhookInboundRepository := psastore.NewWebhookInboundRepositoryFromPool(pool)
	webhookManagementRepository := psastore.NewWebhookManagementRepositoryFromPool(
		pool, secretProvider,
		webhooks.NewEnvironmentInboundSecretResolver(os.LookupEnv), id.New,
	)
	graphManagementRepository := psastore.NewGraphRepositoryFromPool(
		pool, secretProvider, id.New,
	).WithLegacyResolvers(
		graphintake.NewEnvironmentCredentialResolver(os.LookupEnv),
		graphintake.NewEnvironmentClientStateResolver(os.LookupEnv),
	)
	forwardingRepository := psastore.NewForwardingRepositoryFromPool(pool)
	viewRepository := psastore.NewViewRepositoryFromPool(pool)
	calendarRepository := psastore.NewCalendarRepositoryFromPool(pool)
	calendarNotificationRepository := psastore.NewCalendarNotificationRepositoryFromPool(pool, id.New)
	workforceRepository := psastore.NewWorkforceRepositoryFromPool(pool)
	workforceScheduleService := workforce.NewScheduleService(workforceRepository, time.Now, id.New)
	commitmentRepository := psastore.NewCommitmentRepositoryFromPool(pool)
	customDateRepository := psastore.NewCustomDateRepositoryFromPool(pool)
	customRoles := map[string]calendar.EventRoleDefinition{}
	if pool != nil && strings.TrimSpace(expectedMSPID) != "" {
		var err error
		customRoles, err = calendarRepository.LoadCustomRoleDefinitions(context.Background(), expectedMSPID)
		if err != nil {
			panic("compose calendar role registry: " + err.Error())
		}
	}
	roleDefinitions := make([]calendar.EventRoleDefinition, 0, len(customRoles))
	for _, definition := range customRoles {
		roleDefinitions = append(roleDefinitions, definition)
	}
	calendarRoles, err := calendar.NewProductionRoleRegistry(roleDefinitions)
	if err != nil {
		panic("compose calendar role registry: " + err.Error())
	}
	calendarAdapters, err := calendaradapters.NewProductionAdapterRegistry(calendaradapters.Dependencies{
		Work: calendarRepository, Projects: calendarRepository,
		Workforce: calendarRepository, Commitments: calendarRepository,
		CustomDates: calendarRepository, CustomRoles: customRoles,
	})
	if err != nil {
		panic("compose calendar projection adapters: " + err.Error())
	}
	calendarWriteAdapters := calendar.NewWriteAdapterRegistry()
	for _, adapter := range []calendar.WriteAdapter{
		calendaradapters.NewWorkRecordAdapter(calendarRepository),
		calendaradapters.NewTaskAdapter(calendarRepository),
		calendaradapters.NewProjectAdapter(calendarRepository),
		calendaradapters.NewPhaseAdapter(calendarRepository),
		calendaradapters.NewMilestoneAdapter(calendarRepository),
		calendaradapters.NewResourcePlanAdapter(calendarRepository),
		calendaradapters.NewScheduleAdapter(calendarRepository),
		calendaradapters.NewPTOAdapter(calendarRepository),
		calendaradapters.NewMaintenanceAdapter(calendarRepository),
		calendaradapters.NewCommercialAdapter(calendarRepository),
	} {
		if err = calendarWriteAdapters.Register(adapter); err != nil {
			panic("compose calendar write adapters: " + err.Error())
		}
	}
	calendarProjectionService := calendar.NewProjectionService(calendarRepository, calendarRoles)
	calendarProjectionWorker := calendar.NewProjectionWorker(calendarRepository, calendarAdapters, calendarProjectionService, "calendar")
	calendarImpactService := calendar.NewSchedulingImpactService(calendarRepository)
	calendarUnitOfWork := calendar.NewScheduleUnitOfWork(calendarRepository, calendarWriteAdapters, time.Now, id.New)
	calendarProposalService := calendar.NewProposalService(calendar.ProposalServiceDependencies{
		Projections: calendarRepository, Workforce: calendarRepository,
		Impacts: calendarImpactService, Store: calendarRepository,
		UnitOfWork: calendarUnitOfWork, RevisionValidator: calendarRepository,
		Adapters: calendarWriteAdapters, Roles: calendarRoles,
		Now: time.Now, NewID: id.New, TTL: 5 * time.Minute,
	})
	sessionService := sessions.NewService(
		authstore.NewSessionStore(pool), time.Now, nil, id.New,
	)
	principalStore := authstore.NewPrincipalStore(pool, time.Now)
	serviceKeyService := servicekeys.NewService(
		authstore.NewServiceKeyRepository(pool), time.Now, nil, id.New,
	)
	sessionResolver := sessions.CookieOrBearerPrincipalResolver(sessionService, principalStore)
	serviceKeyResolver := servicekeys.BearerPrincipalResolver(serviceKeyService)
	tagCatalogActions := tagging.NewCatalogService(taggingRepository, time.Now, id.New)
	tagAssociationActions := tagging.NewAssociationService(taggingRepository)
	tagClassificationActions := tagging.NewClassificationService(taggingRepository, tagAssociationActions, id.New)
	tagClassificationSuggestionActions := tagging.NewClassificationSuggestionService(taggingRepository, tagAssociationActions, id.New)
	tagCreationPreparer := tagging.NewCreationPreparer(taggingRepository)
	tagTerminalGuard := tagging.NewTerminalGuard(tagAssociationActions)
	workRecordActions := workrecords.NewService(
		workRecordRepository, routingRepository, workflowRepository,
		slaRepository, time.Now, id.New, tagCreationPreparer,
	)
	workAssignmentActions := workrecords.NewAssignmentService(
		workRecordRepository, time.Now, id.New,
	)
	technicianDirectory := psastore.NewTechnicianDirectoryRepositoryFromPool(pool)
	workRecordQueries := workrecords.NewQueryService(workRecordRepository)
	workTransitionActions := workrecords.NewTransitionService(
		workRecordRepository, time.Now, id.New, tagTerminalGuard,
	)
	workPriorityActions := workrecords.NewPriorityService(
		workRecordRepository, time.Now, id.New,
	)
	workQueueActions := workrecords.NewQueueService(
		workRecordRepository, time.Now, id.New,
	)
	commentActions := comments.NewServiceWithCapture(
		commentRepository, timeCaptureService, time.Now, id.New,
	)
	mentionPreparer := mentions.NewService(time.Now, id.New).WithTelemetry(mentionTelemetry)
	collaborationActions := collaboration.NewService(
		collaborationRepository, mentionPreparer, time.Now, id.New,
	)
	mentionActions := mentions.NewQueryService(
		mentionRepository, mentionCursorKey, time.Now,
	).WithTelemetry(mentionTelemetry)
	internalContentActions := collaboration.NewListService(mentionRepository)
	searchActions := search.NewService(searchRepository)
	integrationHealthActions := integrationhealth.NewService(
		integrationHealthRepository, time.Now,
	)
	productKnowledgeActions := aiassist.NewProductKnowledgeService(aiWorkspaceRepository)
	projectQueryActions := projects.NewQueryService(
		projectRepository,
		projects.NewFinancialService(projectRepository),
	).WithCapacity(projects.NewCapacityService(projectRepository))
	salesActions := sales.NewService(salesRepository, time.Now, id.New)
	proposalActions := sales.NewProposalService(
		proposalRepository, snapshotStore, acceptanceVerifier, time.Now, id.New,
	)
	knowledgeActions := knowledge.NewService(
		knowledgeRepository, time.Now, id.New, tagCreationPreparer, tagTerminalGuard,
	)
	taskActions := tasks.NewService(taskRepository, time.Now, id.New, tagCreationPreparer)
	organizationActions := organizations.NewService(
		organizations.NewPostgresRepository(pool), time.Now, id.New,
	)
	directoryActions := organizations.NewDirectoryService(
		psastore.NewDirectoryRepositoryFromPool(pool), time.Now, id.New,
	)
	locationActions := clientresources.NewService(locationRepository, time.Now, id.New, tagCreationPreparer)
	contractActions := clientresources.NewService(contractRepository, time.Now, id.New, tagCreationPreparer)
	contactResourceActions := clientresources.NewService(contactRepository, time.Now, id.New, tagCreationPreparer)
	serviceResourceActions := clientresources.NewService(serviceResourceRepository, time.Now, id.New, tagCreationPreparer)
	assetActions := clientresources.NewService(assetRepository, time.Now, id.New, tagCreationPreparer)
	clientResourceCatalogActions := clientresources.NewCatalogService(
		clientResourceCatalogRepository,
	).WithActiveTargetAuthorizer(directoryActions)
	aiWorkspaceTools, err := aiassist.NewRegistry(
		buildAIWorkspaceTools(
			productKnowledgeActions, workRecordQueries,
			workTransitionActions, workPriorityActions, commentActions,
			searchActions, integrationHealthActions,
			projects.NewAIWorkspaceService(projectRepository, time.Now, id.New, tagCreationPreparer),
			projectQueryActions, projectQueryActions, knowledgeActions, salesActions,
			knowledgeActions, salesActions, workQueueActions,
			taskActions,
			organizationActions, organizationActions,
			directoryActions, clientResourceCatalogActions,
			locationActions, contactResourceActions, assetActions,
			serviceResourceActions, contractActions,
			locationActions, contactResourceActions, assetActions,
			serviceResourceActions, contractActions,
			workRecordActions, workRecordRepository, technicianDirectory,
			workAssignmentActions, salesActions, salesActions, proposalActions,
			proposalAIWorkspaceServices{
				opportunities: salesActions,
				proposals:     proposalActions,
			},
			knowledgeActions,
			id.New,
		),
		aiWorkspaceRepository,
		func(ctx context.Context, current authorization.Principal) (authorization.Principal, error) {
			return principalStore.LoadPrincipal(ctx, sessions.Authenticated{
				MSPID: current.Scope.MSPID, TechnicianID: current.ID,
			}, current.Scope.ClientID)
		},
		time.Now,
		id.New,
		directoryActions,
	)
	if err != nil {
		panic("compose AI workspace tools: " + err.Error())
	}
	var graphNotificationActions httpapi.GraphNotificationActions
	if len(graphNotifications) > 0 {
		graphNotificationActions = graphNotifications[0]
	}
	return httpapi.Dependencies{
		Principal: httpapi.PrincipalResolver(
			httpauth.MultiplexBearer(sessionResolver, serviceKeyResolver),
		),
		Sales:                  salesActions,
		Proposals:              proposalActions,
		ProposalVersionQueries: proposalActions,
		Projects:               projects.NewService(projectRepository, time.Now, id.New, tagCreationPreparer),
		ProjectQueries:         projectQueryActions,
		ResourcePlans: projects.NewResourceService(
			projectRepository, time.Now, id.New,
		),
		CostActuals: projects.NewCostActualService(
			projectRepository, time.Now, id.New,
		),
		Availability: projects.NewAvailabilityService(
			projectRepository, time.Now, id.New,
		),
		FinancialInputs: projects.NewFinancialInputService(
			projectRepository, time.Now, id.New,
		),
		Conversions: projects.NewConversionService(
			conversionRepository, time.Now, id.New, tagCreationPreparer,
		),
		ChangeOrders: projects.NewChangeOrderService(
			changeOrderRepository, time.Now, id.New,
		),
		AIDecisions:       aiassist.NewDecisionService(aiRepository, time.Now, id.New),
		AIRecommendations: aiassist.NewRecommendationReviewService(aiRepository),
		AIManagement:      aiManagement,
		AIJobs:            aiJobs,
		AIWorkspaceConversations: aiassist.NewConversationService(
			aiWorkspaceRepository, time.Now, id.New,
		),
		AIWorkspaceTools: aiWorkspaceTools,
		TeamsConnections: notifications.NewTeamsConnectionManagementService(
			teamsConnectionRepository,
			notifications.NewHTTPTeamsConnectionTester(notifications.NewHTTPTeamsTransport()),
			time.Now,
			id.New,
		),
		BreakGlassManagement: identity.NewBreakGlassManagementService(
			authstore.NewBreakGlassStore(pool, id.New), time.Now, id.New,
		),
		EntraSettings: identity.NewEntraSettingsService(
			authstore.NewEntraSettingsStore(pool), secretProvider,
			identity.NewHTTPEntraDiscovery(nil), time.Now, id.New,
		),
		RoleManagement: authorization.NewRoleManagementService(
			authstore.NewRoleRepository(pool), time.Now, id.New,
		),
		Audit: auditlog.NewService(authstore.NewAuditRepository(pool)),
		Setup: setup.NewService(
			setup.NewPostgresRepository(pool), time.Now, id.New,
			setup.WithExpectedMSPID(expectedMSPID),
			setup.WithSecretProvider(secretProvider),
			setup.WithSetupRuntimeConfiguration(setupRuntime),
			setup.WithStorageProbe(attachmentStore),
			setup.WithBackupEvidenceKey(backupEvidenceKey),
		),
		AutomationDeadLetters: automation.NewDeadLetterService(
			automationRepository, time.Now, id.New,
		),
		AutomationManagement: automation.NewManagementService(
			automationRepository, time.Now, id.New,
		),
		Locations:                    locationActions,
		Contracts:                    contractActions,
		Contacts:                     contactResourceActions,
		Services:                     serviceResourceActions,
		Assets:                       assetActions,
		ClientResourceCatalog:        clientResourceCatalogActions,
		ClientResourceQueries:        clientResourceCatalogActions,
		LocationLifecycle:            locationActions,
		ContactLifecycle:             contactResourceActions,
		AssetLifecycle:               assetActions,
		ServiceLifecycle:             serviceResourceActions,
		ContractLifecycle:            contractActions,
		TagCatalog:                   tagCatalogActions,
		TagAssociations:              tagAssociationActions,
		TagClassification:            tagClassificationActions,
		TagClassificationSuggestions: tagClassificationSuggestionActions,
		TagReports:                   tagging.NewReportService(taggingProjectionRepository),
		TagCreation:                  tagCreationPreparer,
		CalendarQueries:              calendar.NewQueryService(calendarRepository, calendarRepository, nil),
		CalendarLive:                 calendar.NewLiveService(calendarRepository, calendarRepository, nil),
		CalendarProposals:            calendarProposalService,
		CalendarDependencies:         calendar.NewDependencyService(calendarRepository, time.Now, id.New, calendar.WithDependencyRoleRegistry(calendarRoles)),
		CalendarConfiguration:        calendar.NewConfigurationService(calendarRepository, time.Now, id.New),
		CalendarConfigurationQueries: calendarRepository,
		CalendarPreferences:          notifications.NewCalendarPreferenceService(calendarNotificationRepository, time.Now, id.New),
		CalendarCustomDateValues:     customfields.NewDateService(customDateRepository, time.Now, id.New),
		CalendarCustomDateQueries:    calendarRepository,
		WorkforceSchedules:           workforceScheduleService,
		WorkforceScheduleExceptions:  workforceScheduleService,
		WorkforcePTO:                 workforce.NewPTOService(workforceRepository, time.Now, id.New),
		WorkforceQueries:             calendarRepository,
		ProjectMilestones:            projects.NewMilestoneService(projectRepository, time.Now, id.New),
		ProjectMilestoneQueries:      calendarRepository,
		MaintenanceWindows:           commitments.NewMaintenanceService(commitmentRepository, time.Now, id.New),
		CommercialCommitments:        commitments.NewCommercialService(commitmentRepository, time.Now, id.New),
		CommitmentQueries:            calendarRepository,
		CalendarProjectionWorker:     calendarProjectionWorker,
		CalendarProjectionEvents:     calendarRepository,
		CalendarReminders:            calendar.NewReminderService(calendarNotificationRepository, time.Now),
		WorkRecords:                  workRecordActions,
		WorkRecordQueries:            workRecordQueries,
		Organizations:                organizationActions,
		Directory:                    directoryActions,
		WorkAssignments:              workAssignmentActions,
		WorkTransitions:              workTransitionActions,
		WorkPriorities:               workPriorityActions,
		SLAOverrides: workrecords.NewSLAOverrideService(
			workRecordRepository, time.Now, id.New,
		),
		WorkQueues: workrecords.NewQueueService(
			workRecordRepository, time.Now, id.New,
		),
		WorkMerges: workrecords.NewMergeService(
			workRecordRepository, time.Now, id.New,
		),
		WorkParticipants: workrecords.NewParticipationService(
			workRecordRepository, time.Now, id.New,
		),
		Comments:        commentActions,
		Collaboration:   collaborationActions,
		InternalContent: internalContentActions,
		Mentions:        mentionActions,
		NewID:           id.New,
		TimeEntries: timeentries.NewService(
			timeEntryRepository, time.Now, id.New, tagCreationPreparer,
		),
		TimeCaptureEntries: timeCaptureService,
		LaborRoles: timeentries.NewLaborRoleService(
			timeWorkforceRepository, time.Now, id.New,
		),
		Timesheets: timeentries.NewTimesheetService(
			timeWorkforceRepository, time.Now, id.New, tagCreationPreparer,
		),
		Timers: timeentries.NewTimerService(
			timeWorkforceRepository, time.Now, id.New,
		),
		Attachments: attachments.NewService(
			attachmentRepository, attachmentStore, defaultAttachmentPolicy(),
			time.Now, id.New,
		),
		Relationships: links.NewService(linkRepository, time.Now, id.New),
		Tasks:         taskActions,
		Search:        searchActions,
		ServiceKeys:   serviceKeyService,
		Workflows: workflow.NewManagementService(
			workflowRepository, time.Now, id.New,
		),
		Views: views.NewService(viewRepository, id.New),
		Routing: routing.NewManagementService(
			routingRepository, time.Now, id.New,
		),
		SLACalendars: sla.NewCalendarManagementService(
			slaRepository, time.Now, id.New,
		),
		SLAPolicies: sla.NewPolicyManagementService(
			slaRepository, time.Now, id.New,
		),
		NotificationPolicies: notifications.NewPolicyManagementService(
			notificationRepository, time.Now, id.New,
		),
		NotificationPreferences: notifications.NewPreferenceService(notificationRepository),
		NotificationInbox:       notifications.NewInboxService(notificationRepository, time.Now),
		Knowledge:               knowledgeActions,
		BillingExports: billingexport.NewService(
			billingExportRepository, time.Now, id.New, tagTerminalGuard,
		),
		IntegrationHealth: integrationHealthActions,
		DirectIntake: intake.NewSystemService(
			systemIntakeRepository, rawPayloadObjectStore{store: attachmentStore},
			time.Now, id.New,
		),
		InboundWebhooks: webhooks.NewInboundService(
			webhookInboundRepository,
			webhookManagementRepository,
			rawPayloadObjectStore{store: attachmentStore}, time.Now, id.New,
		),
		WebhookManagement: webhooks.NewManagementService(
			webhookManagementRepository, time.Now,
		).WithIDGenerator(id.New),
		ForwardingIntake: intake.NewForwardingService(
			forwardingRepository,
			rawPayloadObjectStore{store: attachmentStore}, time.Now, id.New,
		),
		ForwardingManagement: intake.NewForwardingManagementService(
			forwardingRepository, time.Now, id.New,
		),
		GraphNotifications: graphNotificationActions,
		GraphManagement: graphintake.NewManagementService(
			graphManagementRepository, time.Now, id.New, nil,
		),
		Datto: datto.NewManagementService(
			dattoRepository, time.Now, id.New,
		),
		DattoManagement: datto.NewConnectionManagementService(
			dattoRepository, time.Now, id.New,
		),
		CalendarAIRecommendations: aiassist.NewCalendarRecommendationService(
			psastore.NewCalendarAIContextRepositoryFromPool(pool, time.Now),
			calendarAIProvider,
			calendarProposalService,
		),
		Fallback: fallback,
	}
}

type rawPayloadObjectStore struct {
	store attachments.ObjectStore
}

func (s rawPayloadObjectStore) Put(
	ctx context.Context,
	key string,
	payload []byte,
) error {
	return s.store.Put(
		ctx, key, bytes.NewReader(payload), int64(len(payload)), "application/json",
	)
}

func (s rawPayloadObjectStore) Delete(ctx context.Context, key string) error {
	return s.store.Delete(ctx, key)
}

func defaultAttachmentPolicy() attachments.Policy {
	return attachments.Policy{
		MaxBytes: int64(250 << 20),
		AllowedContentTypes: map[string]struct{}{
			"application/json":         {},
			"application/pdf":          {},
			"application/vnd.ms-excel": {},
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":       {},
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document": {},
			"application/zip": {},
			"image/gif":       {},
			"image/jpeg":      {},
			"image/png":       {},
			"image/webp":      {},
			"text/csv":        {},
			"text/plain":      {},
		},
	}
}

package psa

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
)

type poolDatabase struct {
	pool *pgxpool.Pool
}

func NewSalesRepositoryFromPool(pool *pgxpool.Pool) *SalesRepository {
	return NewSalesRepository(&poolDatabase{pool: pool})
}

func NewProjectRepositoryFromPool(pool *pgxpool.Pool) *ProjectRepository {
	return NewProjectRepository(&poolDatabase{pool: pool})
}

func NewWorkforceRepositoryFromPool(pool *pgxpool.Pool) *WorkforceRepository {
	return NewWorkforceRepository(&poolDatabase{pool: pool})
}

func NewCommitmentRepositoryFromPool(pool *pgxpool.Pool) *CommitmentRepository {
	return NewCommitmentRepository(&poolDatabase{pool: pool})
}

func NewCustomDateRepositoryFromPool(pool *pgxpool.Pool) *CustomDateRepository {
	return NewCustomDateRepository(&poolDatabase{pool: pool})
}

func NewCalendarRepositoryFromPool(pool *pgxpool.Pool) *CalendarRepository {
	return NewCalendarRepository(&poolDatabase{pool: pool})
}

func NewCalendarAIContextRepositoryFromPool(pool *pgxpool.Pool, now func() time.Time) *CalendarAIContextRepository {
	return NewCalendarAIContextRepository(&poolDatabase{pool: pool}, now)
}

func NewCalendarNotificationRepositoryFromPool(pool *pgxpool.Pool, newID func() string) *CalendarNotificationRepository {
	return NewCalendarNotificationRepository(&poolDatabase{pool: pool}, newID)
}

func NewProposalRepositoryFromPool(
	pool *pgxpool.Pool,
	newID func() string,
) *ProposalRepository {
	return NewProposalRepository(&poolDatabase{pool: pool}, newID)
}

func NewConversionRepositoryFromPool(
	pool *pgxpool.Pool,
	newID func() string,
) *ConversionRepository {
	return NewConversionRepository(&poolDatabase{pool: pool}, newID)
}

func NewChangeOrderRepositoryFromPool(pool *pgxpool.Pool) *ChangeOrderRepository {
	return NewChangeOrderRepository(&poolDatabase{pool: pool})
}

func NewAIRepositoryFromPool(pool *pgxpool.Pool) *AIRepository {
	return NewAIRepository(&poolDatabase{pool: pool})
}

func NewAIManagementRepositoryFromPool(
	pool *pgxpool.Pool,
	provider secrets.Provider,
	newID func() string,
) *AIManagementRepository {
	return NewAIManagementRepository(&poolDatabase{pool: pool}, provider, newID)
}

func NewAIJobRepositoryFromPool(
	pool *pgxpool.Pool,
	provider secrets.Provider,
) *AIJobRepository {
	return NewAIJobRepository(&poolDatabase{pool: pool}, provider)
}

func NewAIWorkspaceRepositoryFromPool(pool *pgxpool.Pool) *AIWorkspaceRepository {
	return NewAIWorkspaceRepository(&poolDatabase{pool: pool})
}

func NewAutomationRepositoryFromPool(pool *pgxpool.Pool) *AutomationRepository {
	return NewAutomationRepository(&poolDatabase{pool: pool})
}

func NewAutomationCommentActorRepositoryFromPool(
	pool *pgxpool.Pool,
) *AutomationCommentActorRepository {
	return NewAutomationCommentActorRepository(&poolDatabase{pool: pool})
}

func NewTaggingRepositoryFromPool(pool *pgxpool.Pool) *TaggingRepository {
	return NewTaggingRepository(&poolDatabase{pool: pool})
}

func NewCollaborationRepositoryFromPool(pool *pgxpool.Pool) *CollaborationRepository {
	return NewCollaborationRepository(&poolDatabase{pool: pool})
}

func NewMentionRepositoryFromPool(pool *pgxpool.Pool) *MentionRepository {
	return NewMentionRepository(&poolDatabase{pool: pool})
}

func NewClassificationMigrationRepositoryFromPool(pool *pgxpool.Pool) *ClassificationMigrationRepository {
	return NewClassificationMigrationRepository(&poolDatabase{pool: pool})
}

func NewTaggingProjectionRepositoryFromPool(pool *pgxpool.Pool) *TaggingProjectionRepository {
	return NewTaggingProjectionRepository(&poolDatabase{pool: pool})
}

func NewAutomationRuntimeRepositoryFromPool(
	pool *pgxpool.Pool,
	newID func() string,
) *AutomationRuntimeRepository {
	return NewAutomationRuntimeRepository(&poolDatabase{pool: pool}, newID)
}

func NewAutomationExecutionRepositoryFromPool(
	pool *pgxpool.Pool,
) *AutomationExecutionRepository {
	return NewAutomationExecutionRepository(&poolDatabase{pool: pool})
}

func NewLocationRepositoryFromPool(pool *pgxpool.Pool) *LocationRepository {
	return NewLocationRepository(&poolDatabase{pool: pool})
}

func NewContractRepositoryFromPool(pool *pgxpool.Pool) *ContractRepository {
	return NewContractRepository(&poolDatabase{pool: pool})
}

func NewContactRepositoryFromPool(pool *pgxpool.Pool) *ContactRepository {
	return NewContactRepository(&poolDatabase{pool: pool})
}

func NewServiceResourceRepositoryFromPool(
	pool *pgxpool.Pool,
) *ServiceResourceRepository {
	return NewServiceResourceRepository(&poolDatabase{pool: pool})
}

func NewAssetRepositoryFromPool(pool *pgxpool.Pool) *AssetRepository {
	return NewAssetRepository(&poolDatabase{pool: pool})
}

func NewWorkRecordRepositoryFromPool(pool *pgxpool.Pool) *WorkRecordRepository {
	return NewWorkRecordRepository(&poolDatabase{pool: pool})
}

func NewTechnicianDirectoryRepositoryFromPool(
	pool *pgxpool.Pool,
) *TechnicianDirectoryRepository {
	return NewTechnicianDirectoryRepository(&poolDatabase{pool: pool})
}

func NewDirectoryRepositoryFromPool(pool *pgxpool.Pool) *DirectoryRepository {
	return NewDirectoryRepository(&poolDatabase{pool: pool})
}

func NewCommentRepositoryFromPool(pool *pgxpool.Pool) *CommentRepository {
	return NewCommentRepository(&poolDatabase{pool: pool})
}

func NewTimeEntryRepositoryFromPool(pool *pgxpool.Pool) *TimeEntryRepository {
	return NewTimeEntryRepository(&poolDatabase{pool: pool})
}

func NewTimeWorkforceRepositoryFromPool(
	pool *pgxpool.Pool,
) *TimeWorkforceRepository {
	return NewTimeWorkforceRepository(&poolDatabase{pool: pool})
}

func NewAttachmentRepositoryFromPool(pool *pgxpool.Pool) *AttachmentRepository {
	return NewAttachmentRepository(&poolDatabase{pool: pool})
}

func NewLinkRepositoryFromPool(pool *pgxpool.Pool) *LinkRepository {
	return NewLinkRepository(&poolDatabase{pool: pool})
}

func NewTaskRepositoryFromPool(pool *pgxpool.Pool) *TaskRepository {
	return NewTaskRepository(&poolDatabase{pool: pool})
}

func NewSearchRepositoryFromPool(pool *pgxpool.Pool) *SearchRepository {
	return NewSearchRepository(&poolDatabase{pool: pool})
}

func NewClientResourceCatalogRepositoryFromPool(
	pool *pgxpool.Pool,
) *ClientResourceCatalogRepository {
	return NewClientResourceCatalogRepository(&poolDatabase{pool: pool})
}

func NewWorkflowRepositoryFromPool(pool *pgxpool.Pool) *WorkflowRepository {
	return NewWorkflowRepository(&poolDatabase{pool: pool})
}

func NewRoutingRepositoryFromPool(pool *pgxpool.Pool) *RoutingRepository {
	return NewRoutingRepository(&poolDatabase{pool: pool})
}

func NewSLARepositoryFromPool(pool *pgxpool.Pool) *SLARepository {
	return NewSLARepository(&poolDatabase{pool: pool})
}

func NewNotificationRepositoryFromPool(pool *pgxpool.Pool) *NotificationRepository {
	return NewNotificationRepository(&poolDatabase{pool: pool})
}

func NewTeamsConnectionRepositoryFromPool(
	pool *pgxpool.Pool,
	provider secrets.Provider,
	legacy notifications.TeamsSecretResolver,
) *TeamsConnectionRepository {
	return NewTeamsConnectionRepository(&poolDatabase{pool: pool}, provider, legacy)
}

func NewKnowledgeRepositoryFromPool(pool *pgxpool.Pool) *KnowledgeRepository {
	return NewKnowledgeRepository(&poolDatabase{pool: pool})
}

func NewBillingExportRepositoryFromPool(pool *pgxpool.Pool) *BillingExportRepository {
	return NewBillingExportRepository(&poolDatabase{pool: pool})
}

func NewIntegrationHealthRepositoryFromPool(pool *pgxpool.Pool) *IntegrationHealthRepository {
	return NewIntegrationHealthRepository(&poolDatabase{pool: pool})
}

func NewSystemIntakeRepositoryFromPool(pool *pgxpool.Pool) *SystemIntakeRepository {
	return NewSystemIntakeRepository(&poolDatabase{pool: pool})
}

func NewWebhookInboundRepositoryFromPool(pool *pgxpool.Pool) *WebhookInboundRepository {
	return NewWebhookInboundRepository(&poolDatabase{pool: pool})
}

func NewWebhookOutboundRepositoryFromPool(
	pool *pgxpool.Pool,
	newID func() string,
) *WebhookOutboundRepository {
	return NewWebhookOutboundRepository(&poolDatabase{pool: pool}, newID)
}

func NewWebhookManagementRepositoryFromPool(
	pool *pgxpool.Pool,
	provider secrets.Provider,
	legacy webhooks.InboundSecretResolver,
	newID func() string,
) *WebhookOutboundRepository {
	return NewWebhookManagementRepository(
		&poolDatabase{pool: pool}, provider, legacy, newID,
	)
}

func NewForwardingRepositoryFromPool(pool *pgxpool.Pool) *ForwardingRepository {
	return NewForwardingRepository(&poolDatabase{pool: pool})
}

func NewGraphRepositoryFromPool(
	pool *pgxpool.Pool,
	provider secrets.Provider,
	newID func() string,
) *GraphRepository {
	return NewGraphRepository(&poolDatabase{pool: pool}, provider, newID)
}

func NewDattoRepositoryFromPool(
	pool *pgxpool.Pool,
	provider secrets.Provider,
	newID func() string,
) *DattoRepository {
	return NewDattoRepository(&poolDatabase{pool: pool}, provider, newID)
}

func NewViewRepositoryFromPool(pool *pgxpool.Pool) *ViewRepository {
	return NewViewRepository(&poolDatabase{pool: pool})
}

func NewSnapshotStoreFromPool(
	pool *pgxpool.Pool,
	now func() time.Time,
	newID func() string,
) *SnapshotStore {
	return NewSnapshotStore(&poolDatabase{pool: pool}, now, newID)
}

func NewAcceptanceVerifierFromPool(
	pool *pgxpool.Pool,
	now func() time.Time,
) *AcceptanceVerifier {
	return NewAcceptanceVerifier(&poolDatabase{pool: pool}, now)
}

func (db *poolDatabase) Begin(ctx context.Context) (transaction, error) {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return pgxTransaction{Tx: tx}, nil
}

func (db *poolDatabase) QueryRow(ctx context.Context, query string, args ...any) row {
	return db.pool.QueryRow(ctx, query, args...)
}

func (db *poolDatabase) Query(ctx context.Context, query string, args ...any) (rows, error) {
	return db.pool.Query(ctx, query, args...)
}

func (db *poolDatabase) Acquire(ctx context.Context) (executionSession, error) {
	connection, err := db.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return pgxExecutionSession{Conn: connection}, nil
}

type pgxExecutionSession struct{ *pgxpool.Conn }

func (session pgxExecutionSession) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	return session.Conn.Exec(ctx, query, args...)
}

func (session pgxExecutionSession) QueryRow(ctx context.Context, query string, args ...any) row {
	return session.Conn.QueryRow(ctx, query, args...)
}

func (session pgxExecutionSession) Release() { session.Conn.Release() }

type pgxTransaction struct {
	pgx.Tx
}

func (tx pgxTransaction) Exec(
	ctx context.Context,
	query string,
	args ...any,
) (pgconn.CommandTag, error) {
	return tx.Tx.Exec(ctx, query, args...)
}

func (tx pgxTransaction) Query(
	ctx context.Context,
	query string,
	args ...any,
) (rows, error) {
	return tx.Tx.Query(ctx, query, args...)
}

func (tx pgxTransaction) QueryRow(
	ctx context.Context,
	query string,
	args ...any,
) row {
	return tx.Tx.QueryRow(ctx, query, args...)
}

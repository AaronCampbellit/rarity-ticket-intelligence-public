package organizations

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientidentity"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type PostgresRepository struct {
	identityConflictQuery clientidentity.Query
	beginClientCreation   func(context.Context) (clientCreationTransaction, error)
}

type clientCreationTransaction interface {
	Exec(context.Context, string, ...any) error
	Query(context.Context, string, ...any) (clientidentity.Rows, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

type postgresClientCreationTransaction struct {
	value pgx.Tx
}

func (t postgresClientCreationTransaction) Exec(
	ctx context.Context,
	query string,
	args ...any,
) error {
	_, err := t.value.Exec(ctx, query, args...)
	return err
}

func (t postgresClientCreationTransaction) Query(
	ctx context.Context,
	query string,
	args ...any,
) (clientidentity.Rows, error) {
	return t.value.Query(ctx, query, args...)
}

func (t postgresClientCreationTransaction) Commit(ctx context.Context) error {
	return t.value.Commit(ctx)
}

func (t postgresClientCreationTransaction) Rollback(ctx context.Context) error {
	return t.value.Rollback(ctx)
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{
		identityConflictQuery: func(
			ctx context.Context,
			query string,
			args ...any,
		) (clientidentity.Rows, error) {
			return pool.Query(ctx, query, args...)
		},
		beginClientCreation: func(ctx context.Context) (clientCreationTransaction, error) {
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
			if err != nil {
				return nil, err
			}
			return postgresClientCreationTransaction{value: tx}, nil
		},
	}
}

func (r *PostgresRepository) ClientIdentityConflict(
	ctx context.Context,
	target scope.Target,
	normalizedName string,
	normalizedDisplayID string,
) (bool, error) {
	if r == nil || r.identityConflictQuery == nil {
		return false, ErrInvalid
	}
	return clientidentity.HasConflict(
		ctx,
		target.MSPID,
		normalizedName,
		normalizedDisplayID,
		r.identityConflictQuery,
	)
}

func (r *PostgresRepository) CreateClientAtomic(ctx context.Context, mutation CreateClientMutation) error {
	if r == nil || r.beginClientCreation == nil {
		return ErrInvalid
	}
	client := mutation.Client
	normalizedName := clientidentity.Normalize(client.Name)
	normalizedDisplayID := clientidentity.Normalize(client.DisplayID)
	if strings.TrimSpace(client.MSPID) == "" || normalizedName == "" || normalizedDisplayID == "" {
		return ErrInvalid
	}
	tx, err := r.beginClientCreation(ctx)
	if err != nil {
		return fmt.Errorf("begin client creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := clientidentity.Enforce(
		ctx,
		client.MSPID,
		normalizedName,
		normalizedDisplayID,
		tx.Exec,
		tx.Query,
	); err != nil {
		return err
	}

	if err := tx.Exec(ctx, `
		INSERT INTO client_organizations (
			id, msp_id, display_id, name, lifecycle_state, version,
			created_at, created_by, updated_at, updated_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		client.ID, client.MSPID, client.DisplayID, client.Name,
		client.LifecycleState, client.Version, client.CreatedAt,
		client.CreatedBy, client.UpdatedAt, client.UpdatedBy,
	); err != nil {
		return fmt.Errorf("insert client organization: %w", err)
	}

	audit := mutation.Audit
	if err := tx.Exec(ctx, `
		INSERT INTO audit_ledger (
			id, occurred_at, msp_id, client_id, actor_type, actor_id, action,
			subject_type, subject_id, subject_version, source, correlation_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		audit.ID, audit.OccurredAt, audit.MSPID, audit.ClientID,
		audit.ActorType, audit.ActorID, audit.Action, audit.SubjectType,
		audit.SubjectID, audit.SubjectVersion, audit.Source, audit.CorrelationID,
	); err != nil {
		return fmt.Errorf("insert client audit record: %w", err)
	}

	event := mutation.Event
	if err := tx.Exec(ctx, `
		INSERT INTO event_outbox (
			event_id, event_type, schema_version, occurred_at, msp_id, client_id,
			actor_type, actor_id, subject_type, subject_id, subject_version,
			correlation_id, source
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		event.EventID, event.EventType, event.SchemaVersion, event.OccurredAt,
		event.MSPID, event.ClientID, event.ActorType, event.ActorID,
		event.SubjectType, event.SubjectID, event.SubjectVersion,
		event.CorrelationID, event.Source,
	); err != nil {
		return fmt.Errorf("insert client outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit client creation: %w", err)
	}
	return nil
}

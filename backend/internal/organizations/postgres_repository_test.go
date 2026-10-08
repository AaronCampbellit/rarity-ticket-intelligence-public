package organizations

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientidentity"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type clientIdentityRowsStub struct {
	index  int
	values [][2]string
	err    error
	closed bool
}

func (r *clientIdentityRowsStub) Next() bool {
	return r.index < len(r.values)
}

func (r *clientIdentityRowsStub) Scan(destinations ...any) error {
	if r.err != nil {
		return r.err
	}
	values := r.values[r.index]
	r.index++
	*destinations[0].(*string) = values[0]
	*destinations[1].(*string) = values[1]
	return nil
}

func (r *clientIdentityRowsStub) Err() error {
	return r.err
}

func (r *clientIdentityRowsStub) Close() {
	r.closed = true
}

func TestPostgresRepositoryNormalizesStoredIdentityAcrossEveryLifecycleState(t *testing.T) {
	tests := []struct {
		name                string
		storedName          string
		storedDisplayID     string
		normalizedName      string
		normalizedDisplayID string
	}{
		{
			name:                "inactive name",
			storedName:          "\u00a0CAFÉ\u2003Managed\u00a0Services\u00a0",
			storedDisplayID:     "LEGACY-100",
			normalizedName:      "café managed services",
			normalizedDisplayID: "new-100",
		},
		{
			name:                "archived display ID",
			storedName:          "Archived Client",
			storedDisplayID:     "\u00a0CLIENT\u2003ÉLITE\u00a0",
			normalizedName:      "new client",
			normalizedDisplayID: "client élite",
		},
		{
			name:                "deleted name",
			storedName:          "\u2003ÅCME\u00a0North\u2003",
			storedDisplayID:     "DELETED-100",
			normalizedName:      "åcme north",
			normalizedDisplayID: "new-200",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var query string
			var args []any
			rows := &clientIdentityRowsStub{
				values: [][2]string{{tt.storedName, tt.storedDisplayID}},
			}
			repository := &PostgresRepository{
				identityConflictQuery: func(
					_ context.Context,
					statement string,
					values ...any,
				) (clientidentity.Rows, error) {
					query, args = statement, values
					return rows, nil
				},
			}

			conflict, err := repository.ClientIdentityConflict(
				context.Background(),
				scope.Target{MSPID: "msp-id"},
				tt.normalizedName,
				tt.normalizedDisplayID,
			)
			if err != nil {
				t.Fatalf("ClientIdentityConflict() error = %v", err)
			}
			if !conflict {
				t.Fatal("ClientIdentityConflict() = false, want true")
			}
			if !rows.closed {
				t.Fatal("identity rows were not closed")
			}
			if strings.Contains(query, "lifecycle_state") {
				t.Fatalf("identity conflict query filtered lifecycle: %s", query)
			}
			if !strings.Contains(query, "SELECT name, display_id") ||
				!strings.Contains(query, "FROM client_organizations") ||
				!strings.Contains(query, "WHERE msp_id = $1") {
				t.Fatalf("identity conflict query is not the narrow MSP lookup: %s", query)
			}
			for _, fragment := range []string{
				"lower(", "regexp_replace", "[[:space:]]",
			} {
				if strings.Contains(query, fragment) {
					t.Fatalf("identity conflict query retains SQL normalization %q: %s", fragment, query)
				}
			}
			if len(args) != 1 || args[0] != "msp-id" {
				t.Fatalf("identity conflict args = %#v", args)
			}
		})
	}
}

type clientCreationOperation struct {
	kind  string
	query string
	args  []any
}

type clientCreationTransactionStub struct {
	operations []clientCreationOperation
	rows       *clientIdentityRowsStub
	committed  bool
	rolledBack bool
}

func (t *clientCreationTransactionStub) Exec(
	_ context.Context,
	query string,
	args ...any,
) error {
	t.operations = append(t.operations, clientCreationOperation{
		kind: "exec", query: query, args: append([]any(nil), args...),
	})
	return nil
}

func (t *clientCreationTransactionStub) Query(
	_ context.Context,
	query string,
	args ...any,
) (clientidentity.Rows, error) {
	t.operations = append(t.operations, clientCreationOperation{
		kind: "query", query: query, args: append([]any(nil), args...),
	})
	if t.rows == nil {
		t.rows = &clientIdentityRowsStub{}
	}
	return t.rows, nil
}

func (t *clientCreationTransactionStub) Commit(context.Context) error {
	t.committed = true
	return nil
}

func (t *clientCreationTransactionStub) Rollback(context.Context) error {
	t.rolledBack = true
	return nil
}

func TestPostgresRepositoryCreateClientLocksAndRechecksIdentityBeforeAtomicWrites(t *testing.T) {
	tx := &clientCreationTransactionStub{}
	repository := &PostgresRepository{
		beginClientCreation: func(context.Context) (clientCreationTransaction, error) {
			return tx, nil
		},
	}

	err := repository.CreateClientAtomic(
		context.Background(),
		clientCreationMutation("client-new", "New Client", "CLIENT-NEW"),
	)
	if err != nil {
		t.Fatalf("CreateClientAtomic() error = %v", err)
	}
	if !tx.committed {
		t.Fatal("Client creation transaction did not commit")
	}
	if len(tx.operations) != 5 {
		t.Fatalf("operations = %#v", tx.operations)
	}
	wantFragments := []string{
		"pg_advisory_xact_lock",
		"SELECT name, display_id",
		"INSERT INTO client_organizations",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	}
	for index, fragment := range wantFragments {
		if !strings.Contains(tx.operations[index].query, fragment) {
			t.Fatalf("operation %d query = %q, want %q", index, tx.operations[index].query, fragment)
		}
	}
	if tx.operations[0].kind != "exec" ||
		len(tx.operations[0].args) != 1 ||
		tx.operations[0].args[0] != "msp-id" {
		t.Fatalf("advisory lock operation = %#v", tx.operations[0])
	}
	if tx.operations[1].kind != "query" ||
		len(tx.operations[1].args) != 1 ||
		tx.operations[1].args[0] != "msp-id" ||
		strings.Contains(tx.operations[1].query, "lifecycle_state") {
		t.Fatalf("identity recheck operation = %#v", tx.operations[1])
	}
}

func TestPostgresRepositoryCreateClientReturnsTypedNormalizedIdentityConflictBeforeWrites(t *testing.T) {
	tx := &clientCreationTransactionStub{rows: &clientIdentityRowsStub{
		values: [][2]string{{"\u00a0CAFÉ\u2003Managed\u00a0Services\u00a0", "CLIENT-OLD"}},
	}}
	repository := &PostgresRepository{
		beginClientCreation: func(context.Context) (clientCreationTransaction, error) {
			return tx, nil
		},
	}

	err := repository.CreateClientAtomic(
		context.Background(),
		clientCreationMutation("client-new", " café managed services ", "CLIENT-NEW"),
	)
	if !errors.Is(err, ErrClientIdentityConflict) {
		t.Fatalf("CreateClientAtomic() error = %v, want ErrClientIdentityConflict", err)
	}
	if tx.committed || !tx.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t", tx.committed, tx.rolledBack)
	}
	if len(tx.operations) != 2 ||
		!strings.Contains(tx.operations[0].query, "pg_advisory_xact_lock") ||
		!strings.Contains(tx.operations[1].query, "SELECT name, display_id") {
		t.Fatalf("conflict operations = %#v", tx.operations)
	}
	for _, operation := range tx.operations {
		if strings.Contains(operation.query, "INSERT INTO") {
			t.Fatalf("conflicting identity reached insert: %q", operation.query)
		}
	}
}

func clientCreationMutation(id, name, displayID string) CreateClientMutation {
	now := time.Date(2026, time.August, 5, 9, 0, 0, 0, time.UTC)
	return CreateClientMutation{
		Client: Client{
			Envelope: object.Envelope{
				ID: id, ObjectType: "client_organization",
				MSPID: "msp-id", ClientID: id, DisplayID: displayID,
				LifecycleState: "active", Version: 1,
				CreatedAt: now, CreatedBy: "actor-id",
				UpdatedAt: now, UpdatedBy: "actor-id",
			},
			Name: name,
		},
		Audit: mutation.AuditRecord{
			ID: "audit-" + id, OccurredAt: now, MSPID: "msp-id", ClientID: id,
			ActorType: "technician", ActorID: "actor-id", Action: "client.created",
			SubjectType: "client_organization", SubjectID: id, SubjectVersion: 1,
			Source: "test", CorrelationID: "correlation-" + id,
		},
		Event: mutation.EventRecord{
			EventID: "event-" + id, EventType: "client.created", SchemaVersion: 1,
			OccurredAt: now, MSPID: "msp-id", ClientID: id,
			ActorType: "technician", ActorID: "actor-id",
			SubjectType: "client_organization", SubjectID: id, SubjectVersion: 1,
			Source: "test", CorrelationID: "correlation-" + id,
		},
	}
}

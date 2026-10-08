package psa

import (
	"context"
	"errors"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/comments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

func TestAIWriteTransactionsRejectInactiveClientBeforeMutation(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(context.Context, *fakeSalesDB) error
	}{
		{
			name: "project and setup tasks",
			run: func(ctx context.Context, db *fakeSalesDB) error {
				return NewProjectRepository(db).CreateAIWorkspaceProjectAtomic(
					ctx,
					projects.AIWorkspaceCreateMutation{
						Project: projects.Project{MSPID: "msp", ClientID: "client"},
						Audits:  make([]mutation.AuditRecord, 1),
						Events:  make([]mutation.EventRecord, 1),
					},
				)
			},
		},
		{
			name: "task",
			run: func(ctx context.Context, db *fakeSalesDB) error {
				return NewTaskRepository(db).CreateAtomic(
					ctx,
					tasks.CreateMutation{Task: tasks.Task{
						MSPID: "msp", ClientID: "client",
					}},
				)
			},
		},
		{
			name: "ticket creation",
			run: func(ctx context.Context, db *fakeSalesDB) error {
				return NewWorkRecordRepository(db).CreateAtomic(
					ctx,
					workrecords.CreateMutation{Record: workrecords.Record{
						Envelope: object.Envelope{MSPID: "msp", ClientID: "client"},
					}},
				)
			},
		},
		{
			name: "ticket transition",
			run: func(ctx context.Context, db *fakeSalesDB) error {
				return NewWorkRecordRepository(db).TransitionAtomic(
					ctx,
					workrecords.TransitionMutation{Record: workrecords.Record{
						Envelope: object.Envelope{MSPID: "msp", ClientID: "client"},
					}},
				)
			},
		},
		{
			name: "ticket priority",
			run: func(ctx context.Context, db *fakeSalesDB) error {
				return NewWorkRecordRepository(db).ChangePriorityAtomic(
					ctx,
					workrecords.PriorityMutation{Record: workrecords.Record{
						Envelope: object.Envelope{MSPID: "msp", ClientID: "client"},
					}},
				)
			},
		},
		{
			name: "ticket note or reply",
			run: func(ctx context.Context, db *fakeSalesDB) error {
				return NewCommentRepository(db).CreateAtomic(
					ctx,
					comments.CreateMutation{Comment: comments.Comment{
						MSPID: "msp", ClientID: "client",
					}},
				)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeSalesTx{zeroRowsAt: 1}
			err := test.run(context.Background(), &fakeSalesDB{tx: tx})
			if !errors.Is(err, scope.ErrNotFound) {
				t.Fatalf("mutation error=%v, want ErrNotFound", err)
			}
			if len(tx.queries) != 1 || tx.committed || !tx.rolledBack {
				t.Fatalf(
					"inactive Client mutation queries=%d committed=%t rolled back=%t",
					len(tx.queries), tx.committed, tx.rolledBack,
				)
			}
		})
	}
}

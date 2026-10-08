package clientidentity

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type identityRowsStub struct {
	index  int
	values [][2]string
	closed bool
}

func (r *identityRowsStub) Next() bool {
	return r.index < len(r.values)
}

func (r *identityRowsStub) Scan(destinations ...any) error {
	values := r.values[r.index]
	r.index++
	*destinations[0].(*string) = values[0]
	*destinations[1].(*string) = values[1]
	return nil
}

func (r *identityRowsStub) Err() error {
	return nil
}

func (r *identityRowsStub) Close() {
	r.closed = true
}

func TestNormalizeUsesDeterministicUnicodeWhitespaceAndCaseSemantics(t *testing.T) {
	got := Normalize("\u00a0CAFÉ\u2003Managed\tServices\u00a0")
	if got != "café managed services" {
		t.Fatalf("Normalize() = %q, want %q", got, "café managed services")
	}
}

func TestEnforceLocksThenRechecksEveryLifecycleBeforeReturningConflict(t *testing.T) {
	var operations []string
	var arguments [][]any
	rows := &identityRowsStub{
		values: [][2]string{{"Archived Client", "\u00a0CLIENT\u2003ÉLITE\u00a0"}},
	}

	err := Enforce(
		context.Background(),
		"msp-id",
		"client élite",
		"CLIENT-NEW",
		func(_ context.Context, query string, args ...any) error {
			operations = append(operations, query)
			arguments = append(arguments, args)
			return nil
		},
		func(_ context.Context, query string, args ...any) (Rows, error) {
			operations = append(operations, query)
			arguments = append(arguments, args)
			return rows, nil
		},
	)

	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Enforce() error = %v, want ErrConflict", err)
	}
	if !rows.closed {
		t.Fatal("identity rows were not closed")
	}
	if len(operations) != 2 ||
		!strings.Contains(operations[0], "pg_advisory_xact_lock") ||
		!strings.Contains(operations[1], "SELECT name, display_id") {
		t.Fatalf("identity operations = %#v", operations)
	}
	if strings.Contains(operations[1], "lifecycle_state") {
		t.Fatalf("identity recheck filtered lifecycle: %s", operations[1])
	}
	for _, fragment := range []string{"lower(", "regexp_replace", "[[:space:]]"} {
		if strings.Contains(operations[1], fragment) {
			t.Fatalf("identity recheck used SQL normalization %q: %s", fragment, operations[1])
		}
	}
	for index, args := range arguments {
		if len(args) != 1 || args[0] != "msp-id" {
			t.Fatalf("identity operation %d args = %#v", index, args)
		}
	}
}

func TestHasConflictMatchesBothProposedFieldsAgainstBothStoredFields(t *testing.T) {
	tests := []struct {
		name            string
		storedName      string
		storedDisplayID string
		proposedName    string
		proposedID      string
	}{
		{
			name:            "proposed name matches stored name",
			storedName:      "Café Managed Services",
			storedDisplayID: "CLIENT-OLD",
			proposedName:    "\u00a0CAFÉ\u2003Managed Services\u00a0",
			proposedID:      "CLIENT-NEW",
		},
		{
			name:            "proposed name matches stored display ID",
			storedName:      "Existing Client",
			storedDisplayID: "CLIENT-EXISTING",
			proposedName:    "client-existing",
			proposedID:      "CLIENT-NEW",
		},
		{
			name:            "proposed display ID matches stored name",
			storedName:      "Shared Identity",
			storedDisplayID: "CLIENT-OLD",
			proposedName:    "New Client",
			proposedID:      "shared identity",
		},
		{
			name:            "proposed display ID matches stored display ID",
			storedName:      "Existing Client",
			storedDisplayID: "CLIENT-EXISTING",
			proposedName:    "New Client",
			proposedID:      "client-existing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := &identityRowsStub{
				values: [][2]string{{tt.storedName, tt.storedDisplayID}},
			}
			conflict, err := HasConflict(
				context.Background(),
				"msp-id",
				tt.proposedName,
				tt.proposedID,
				func(context.Context, string, ...any) (Rows, error) {
					return rows, nil
				},
			)
			if err != nil {
				t.Fatalf("HasConflict() error = %v", err)
			}
			if !conflict {
				t.Fatal("HasConflict() = false, want true")
			}
		})
	}
}

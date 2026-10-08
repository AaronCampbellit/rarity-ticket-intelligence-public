// Package objectidentity serializes normalized two-field business identities
// inside caller-owned PostgreSQL transactions.
package objectidentity

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrConflict = errors.New("object identity conflict")

type Rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

type Execute func(context.Context, string, ...any) error
type Query func(context.Context, string, ...any) (Rows, error)

const identityLockSQL = `
SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`

func Normalize(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

// Enforce locks one identity namespace, then compares both proposed fields
// against both stored fields returned by the caller's unfiltered query.
func Enforce(
	ctx context.Context,
	lockKey string,
	first string,
	second string,
	conflictSQL string,
	conflictArgs []any,
	execute Execute,
	query Query,
) error {
	if strings.TrimSpace(lockKey) == "" || Normalize(first) == "" ||
		Normalize(second) == "" || strings.TrimSpace(conflictSQL) == "" ||
		execute == nil || query == nil {
		return errors.New("invalid object identity boundary")
	}
	if err := execute(ctx, identityLockSQL, lockKey); err != nil {
		return fmt.Errorf("lock object identity: %w", err)
	}
	rows, err := query(ctx, conflictSQL, conflictArgs...)
	if err != nil {
		return fmt.Errorf("recheck object identity: %w", err)
	}
	defer rows.Close()
	proposedFirst, proposedSecond := Normalize(first), Normalize(second)
	for rows.Next() {
		var storedFirst, storedSecond string
		if err := rows.Scan(&storedFirst, &storedSecond); err != nil {
			return fmt.Errorf("scan object identity: %w", err)
		}
		normalizedFirst, normalizedSecond := Normalize(storedFirst), Normalize(storedSecond)
		if proposedFirst == normalizedFirst || proposedFirst == normalizedSecond ||
			proposedSecond == normalizedFirst || proposedSecond == normalizedSecond {
			return ErrConflict
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read object identity: %w", err)
	}
	return nil
}

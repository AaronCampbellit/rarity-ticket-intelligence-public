// Package clientidentity owns the shared PostgreSQL serialization and
// normalized conflict boundary for Client organization creation.
package clientidentity

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrConflict is returned when either proposed identity field matches an
// existing Client name or display ID.
var ErrConflict = errors.New("client identity conflict")

// Rows is the minimal identity-query result contract.
type Rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

// Execute runs one statement inside the caller-owned transaction.
type Execute func(context.Context, string, ...any) error

// Query reads Client identities inside the caller-owned transaction.
type Query func(context.Context, string, ...any) (Rows, error)

const identityLockSQL = `
SELECT pg_advisory_xact_lock(
	hashtextextended('client_organization_identity:' || $1::text, 0)
)`

const identityConflictSQL = `
SELECT name, display_id
FROM client_organizations
WHERE msp_id = $1
`

// Normalize applies the one deterministic Client identity comparison form.
func Normalize(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

// Enforce serializes and rechecks Client identity inside an existing
// transaction.
func Enforce(
	ctx context.Context,
	mspID string,
	name string,
	displayID string,
	execute Execute,
	query Query,
) error {
	if execute == nil {
		return errors.New("client identity executor is required")
	}
	if err := execute(ctx, identityLockSQL, mspID); err != nil {
		return fmt.Errorf("lock client identities: %w", err)
	}
	conflict, err := HasConflict(ctx, mspID, name, displayID, query)
	if err != nil {
		return fmt.Errorf("recheck client identity: %w", err)
	}
	if conflict {
		return ErrConflict
	}
	return nil
}

// HasConflict reports whether name or displayID matches either stored identity
// field for any Client lifecycle in the MSP.
func HasConflict(
	ctx context.Context,
	mspID string,
	name string,
	displayID string,
	query Query,
) (bool, error) {
	if query == nil {
		return false, errors.New("client identity query is required")
	}
	rows, err := query(ctx, identityConflictSQL, mspID)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	normalizedName := Normalize(name)
	normalizedDisplayID := Normalize(displayID)
	for rows.Next() {
		var storedName, storedDisplayID string
		if err := rows.Scan(&storedName, &storedDisplayID); err != nil {
			return false, err
		}
		normalizedStoredName := Normalize(storedName)
		normalizedStoredDisplayID := Normalize(storedDisplayID)
		if normalizedName == normalizedStoredName ||
			normalizedName == normalizedStoredDisplayID ||
			normalizedDisplayID == normalizedStoredName ||
			normalizedDisplayID == normalizedStoredDisplayID {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

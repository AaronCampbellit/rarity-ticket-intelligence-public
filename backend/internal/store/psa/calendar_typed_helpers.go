package psa

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func classifyScopedVersion(ctx context.Context, q transaction, table, id, mspID, clientID string, expected int64, conflict error) error {
	if !calendarVersionTable(table) {
		return scope.ErrNotFound
	}
	var current int64
	err := q.QueryRow(ctx, fmt.Sprintf(`SELECT version FROM %s WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid`, table), id, mspID, clientID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return scope.ErrNotFound
	}
	if err != nil {
		return err
	}
	if current != expected {
		return conflict
	}
	return scope.ErrNotFound
}
func classifyMSPVersion(ctx context.Context, q transaction, table, id, mspID string, expected int64, conflict error) error {
	if !calendarVersionTable(table) {
		return scope.ErrNotFound
	}
	var current int64
	err := q.QueryRow(ctx, fmt.Sprintf(`SELECT version FROM %s WHERE id=$1::uuid AND msp_id=$2::uuid`, table), id, mspID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return scope.ErrNotFound
	}
	if err != nil {
		return err
	}
	if current != expected {
		return conflict
	}
	return scope.ErrNotFound
}
func calendarVersionTable(v string) bool {
	switch v {
	case "project_milestones", "commercial_commitments", "object_custom_date_values", "pto_requests", "maintenance_windows":
		return true
	}
	return false
}

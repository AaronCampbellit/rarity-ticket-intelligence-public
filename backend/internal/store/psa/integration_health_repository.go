package psa

import (
	"context"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/integrationhealth"
)

type IntegrationHealthRepository struct {
	db database
}

var _ integrationhealth.Repository = (*IntegrationHealthRepository)(nil)

func NewIntegrationHealthRepository(db database) *IntegrationHealthRepository {
	return &IntegrationHealthRepository{db: db}
}

func (r *IntegrationHealthRepository) List(
	ctx context.Context,
	mspID string,
) ([]integrationhealth.StoredSignal, error) {
	rows, err := r.db.Query(ctx, `
SELECT connection_id::text, integration_kind, enabled, health_state,
       last_success_at, COALESCE(last_error_code, ''), pending_failures
FROM integration_health_signals
WHERE msp_id = $1
ORDER BY integration_kind, connection_id
`, mspID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]integrationhealth.StoredSignal, 0)
	for rows.Next() {
		var signal integrationhealth.StoredSignal
		var lastSuccessAt *time.Time
		if err := rows.Scan(
			&signal.ID, &signal.Kind, &signal.Enabled, &signal.HealthState,
			&lastSuccessAt, &signal.LastErrorCode, &signal.PendingFailures,
		); err != nil {
			return nil, err
		}
		if lastSuccessAt != nil {
			signal.LastSuccessAt = lastSuccessAt.UTC()
		}
		result = append(result, signal)
	}
	return result, rows.Err()
}

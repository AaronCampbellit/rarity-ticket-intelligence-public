package psa

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
)

// AIManagementRepository persists only the protected AI control plane. Provider
// I/O belongs at the aiassist.ProviderOperations boundary, never here.
type AIManagementRepository struct {
	db      database
	secrets secrets.Provider
	newID   func() string
}

var _ aiassist.ManagementRepository = (*AIManagementRepository)(nil)
var _ aiassist.CredentialProvider = (*AIManagementRepository)(nil)

func NewAIManagementRepository(db database, provider secrets.Provider, newID func() string) *AIManagementRepository {
	return &AIManagementRepository{db: db, secrets: provider, newID: newID}
}

// UseCredential opens a credential only for the exact connection snapshot
// being tested or discovered. A concurrent endpoint, network mode, or secret
// update cannot pair a different connection state with this provider request.
func (r *AIManagementRepository) UseCredential(
	ctx context.Context,
	connection aiassist.ProviderConnection,
	use func([]byte) error,
) error {
	if r == nil || r.db == nil || r.secrets == nil || use == nil ||
		!connection.CredentialConfigured || aiassist.ValidateConnection(connection) != nil {
		return aiassist.ErrProviderCredentialUnavailable
	}
	var sealed secrets.SealedValue
	err := r.db.QueryRow(ctx, `
SELECT credential_version, credential_nonce, credential_ciphertext
FROM ai_provider_connections connection
WHERE connection.id = $1 AND connection.msp_id = $2
  AND connection.version = $3 AND connection.base_url = $4
  AND connection.network_mode = $5
  AND credential_version IS NOT NULL AND credential_nonce IS NOT NULL
  AND credential_ciphertext IS NOT NULL
`, connection.ID, connection.MSPID, connection.Version, connection.BaseURL,
		connection.Network).Scan(&sealed.Version, &sealed.Nonce, &sealed.Ciphertext)
	if err != nil {
		return aiassist.ErrProviderCredentialUnavailable
	}
	credential, err := r.secrets.Open(ctx, "ai.provider."+connection.ID, sealed)
	if err != nil || len(credential) == 0 {
		return aiassist.ErrProviderCredentialUnavailable
	}
	defer wipeCredential(credential)
	return use(credential)
}

func (r *AIManagementRepository) CreateConnection(ctx context.Context, accepted aiassist.ConnectionMutation) error {
	// The service hands repository-owned plaintext to this synchronous method.
	// Keep it only through sealing, then overwrite it on every return path.
	defer wipeCredential(accepted.PlaintextCredential)
	sealed, err := r.sealCredential(ctx, accepted.Connection.ID, accepted.PlaintextCredential)
	if err != nil {
		return err
	}
	return r.withTransaction(ctx, func(tx transaction) error {
		connection := accepted.Connection
		if err := lockProviderConnection(ctx, tx, connection.MSPID, connection.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
INSERT INTO ai_provider_connections (
  id, msp_id, name, adapter_type, network_mode, base_url,
  credential_version, credential_nonce, credential_ciphertext, enabled,
  timeout_seconds, request_limit_bytes, response_limit_bytes,
  local_network_acknowledged_at, local_network_acknowledged_by, health_state, last_tested_at,
  last_succeeded_at, last_error_code, version, created_at, created_by,
  updated_at, updated_by
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
  CASE WHEN $5 = 'local' THEN $14::timestamptz ELSE NULL END,
  CASE WHEN $5 = 'local' AND $14 IS NOT NULL THEN $15::uuid ELSE NULL END,
  $16, $17, $18, NULLIF($19, ''), $20, $21, $22, $21, $22
)
`, connection.ID, connection.MSPID, connection.Name, connection.Adapter, connection.Network,
			connection.BaseURL, sealed.version, sealed.nonce, sealed.ciphertext, connection.Enabled,
			int64(connection.Timeout/time.Second), connection.RequestLimitBytes, connection.ResponseLimitBytes,
			connection.LocalNetworkAcknowledgedAt, accepted.Audit.ActorID, connection.Health,
			connection.LastTestedAt, connection.LastSucceededAt, connection.LastErrorCode,
			connection.Version, accepted.Audit.OccurredAt, accepted.Audit.ActorID)
		if err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *AIManagementRepository) ListConnections(ctx context.Context, target scope.Target) ([]aiassist.ProviderConnection, error) {
	rows, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, name, adapter_type, network_mode, base_url,
       (credential_version IS NOT NULL AND credential_nonce IS NOT NULL AND credential_ciphertext IS NOT NULL),
       enabled, timeout_seconds, request_limit_bytes, response_limit_bytes,
       disclosure_accepted_at, local_network_acknowledged_at, health_state, last_tested_at, last_succeeded_at,
       COALESCE(last_error_code, ''), version
FROM ai_provider_connections
WHERE msp_id = $1
ORDER BY name, id
`, target.MSPID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	connections := make([]aiassist.ProviderConnection, 0)
	for rows.Next() {
		connection, err := scanProviderConnection(rows)
		if err != nil {
			return nil, err
		}
		connections = append(connections, connection)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return connections, nil
}

func (r *AIManagementRepository) GetConnection(ctx context.Context, target scope.Target, id string) (aiassist.ProviderConnection, error) {
	connection, err := scanProviderConnection(r.db.QueryRow(ctx, `
SELECT id::text, msp_id::text, name, adapter_type, network_mode, base_url,
       (credential_version IS NOT NULL AND credential_nonce IS NOT NULL AND credential_ciphertext IS NOT NULL),
       enabled, timeout_seconds, request_limit_bytes, response_limit_bytes,
       disclosure_accepted_at, local_network_acknowledged_at, health_state, last_tested_at, last_succeeded_at,
       COALESCE(last_error_code, ''), version
FROM ai_provider_connections
WHERE id = $1 AND msp_id = $2
`, id, target.MSPID))
	if errors.Is(err, pgx.ErrNoRows) {
		return aiassist.ProviderConnection{}, scope.ErrNotFound
	}
	if err != nil {
		return aiassist.ProviderConnection{}, err
	}
	return connection, nil
}

func (r *AIManagementRepository) ListModels(ctx context.Context, target scope.Target, connectionID string) ([]aiassist.ModelProfile, error) {
	rows, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, connection_id::text, provider_model_id,
       display_name, supported_features, context_limit, output_limit,
       zero_cost, input_cost_per_million_minor, output_cost_per_million_minor, enabled, version
FROM ai_model_profiles
WHERE msp_id = $1 AND connection_id = $2
ORDER BY display_name, id
`, target.MSPID, connectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectModels(rows)
}

func (r *AIManagementRepository) FindModels(ctx context.Context, target scope.Target, ids []string) ([]aiassist.ModelProfile, error) {
	if len(ids) == 0 {
		return []aiassist.ModelProfile{}, nil
	}
	rows, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, connection_id::text, provider_model_id,
       display_name, supported_features, context_limit, output_limit,
       zero_cost, input_cost_per_million_minor, output_cost_per_million_minor, enabled, version
FROM ai_model_profiles
WHERE msp_id = $1 AND id = ANY($2::uuid[])
`, target.MSPID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectModels(rows)
}

func (r *AIManagementRepository) UpdateConnection(ctx context.Context, accepted aiassist.ConnectionMutation) error {
	return r.updateConnection(ctx, accepted, false)
}

func (r *AIManagementRepository) SetConnectionEnabled(ctx context.Context, accepted aiassist.ConnectionMutation) error {
	return r.updateConnection(ctx, accepted, true)
}

func (r *AIManagementRepository) updateConnection(ctx context.Context, accepted aiassist.ConnectionMutation, onlyEnabled bool) error {
	err := r.withTransaction(ctx, func(tx transaction) error {
		connection := accepted.Connection
		if err := lockProviderConnection(ctx, tx, connection.MSPID, connection.ID); err != nil {
			return err
		}
		var (
			tag pgconnTag
			err error
		)
		if onlyEnabled {
			tag, err = tx.Exec(ctx, `
UPDATE ai_provider_connections
SET enabled = $4, health_state = $5, updated_at = $6, updated_by = $7,
    version = version + 1
WHERE id = $1 AND msp_id = $2 AND version = $3
`, connection.ID, connection.MSPID, accepted.ExpectedVersion, connection.Enabled,
				connection.Health, accepted.Audit.OccurredAt, accepted.Audit.ActorID)
		} else {
			tag, err = tx.Exec(ctx, `
UPDATE ai_provider_connections
SET name = $4, adapter_type = $5, network_mode = $6, base_url = $7,
    timeout_seconds = $8, request_limit_bytes = $9, response_limit_bytes = $10,
    disclosure_accepted_at = CASE
      WHEN adapter_type IS NOT DISTINCT FROM $5
       AND network_mode IS NOT DISTINCT FROM $6
       AND base_url IS NOT DISTINCT FROM $7
      THEN disclosure_accepted_at
      ELSE NULL
    END,
    disclosure_accepted_by = CASE
      WHEN adapter_type IS NOT DISTINCT FROM $5
       AND network_mode IS NOT DISTINCT FROM $6
       AND base_url IS NOT DISTINCT FROM $7
      THEN disclosure_accepted_by
      ELSE NULL
    END,
    local_network_acknowledged_by = CASE
      WHEN $6 <> 'local' OR $11::timestamptz IS NULL THEN NULL
      WHEN local_network_acknowledged_at IS NOT DISTINCT FROM $11::timestamptz THEN local_network_acknowledged_by
      ELSE $12::uuid
    END,
    local_network_acknowledged_at = CASE WHEN $6 = 'local' THEN $11::timestamptz ELSE NULL END,
    updated_at = $13, updated_by = $14,
    version = version + 1
WHERE id = $1 AND msp_id = $2 AND version = $3
`, connection.ID, connection.MSPID, accepted.ExpectedVersion, connection.Name,
				connection.Adapter, connection.Network, connection.BaseURL,
				int64(connection.Timeout/time.Second), connection.RequestLimitBytes,
				connection.ResponseLimitBytes, connection.LocalNetworkAcknowledgedAt,
				accepted.Audit.ActorID, accepted.Audit.OccurredAt, accepted.Audit.ActorID)
		}
		if err != nil {
			return normalizeAIManagementWriteError(err)
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
	return normalizeAIManagementWriteError(err)
}

func normalizeAIManagementWriteError(err error) error {
	const referencedProviderMessage = "cannot disable or remove disclosure acceptance from an AI provider connection while enabled policies or active jobs reference it"
	var postgres *pgconn.PgError
	if (errors.As(err, &postgres) &&
		postgres.Code == "P0001" &&
		strings.Contains(postgres.Message, referencedProviderMessage)) ||
		(err != nil && strings.Contains(err.Error(), referencedProviderMessage) &&
			strings.Contains(err.Error(), "SQLSTATE P0001")) {
		return aiassist.ErrProviderInUse
	}
	return err
}

func (r *AIManagementRepository) ReplaceCredential(ctx context.Context, accepted aiassist.CredentialMutation) error {
	// See ConnectionMutation ownership contract: this buffer is consumed here.
	defer wipeCredential(accepted.PlaintextCredential)
	sealed, err := r.sealCredential(ctx, accepted.ConnectionID, accepted.PlaintextCredential)
	if err != nil {
		return err
	}
	return r.withTransaction(ctx, func(tx transaction) error {
		if err := lockProviderConnection(ctx, tx, accepted.MSPID, accepted.ConnectionID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
UPDATE ai_provider_connections
SET credential_version = $4, credential_nonce = $5, credential_ciphertext = $6,
    updated_at = $7, updated_by = $8, version = version + 1
WHERE id = $1 AND msp_id = $2 AND version = $3
`, accepted.ConnectionID, accepted.MSPID, accepted.ExpectedVersion,
			sealed.version, sealed.nonce, sealed.ciphertext,
			accepted.Audit.OccurredAt, accepted.Audit.ActorID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *AIManagementRepository) ReconcileModels(ctx context.Context, accepted aiassist.ModelReconciliationMutation) ([]aiassist.ModelProfile, error) {
	persisted := make([]aiassist.ModelProfile, 0, len(accepted.Models))
	err := r.withTransaction(ctx, func(tx transaction) error {
		if err := lockProviderConnection(ctx, tx, accepted.MSPID, accepted.ConnectionID); err != nil {
			return err
		}
		if len(accepted.Models) == 0 {
			tag, err := tx.Exec(ctx, `
UPDATE ai_provider_connections
SET updated_at = updated_at
WHERE id = $1 AND msp_id = $2 AND version = $3
`, accepted.ConnectionID, accepted.MSPID, accepted.ConnectionVersion)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return object.ErrVersionConflict
			}
		}
		for _, model := range accepted.Models {
			if model.MSPID != accepted.MSPID || model.ConnectionID != accepted.ConnectionID {
				return aiassist.ErrInvalidProviderManagement
			}
			profile, err := scanModelProfile(tx.QueryRow(ctx, `
WITH connection AS (
  SELECT id FROM ai_provider_connections
  WHERE id = $1 AND msp_id = $2 AND version = $3
  FOR UPDATE
)
INSERT INTO ai_model_profiles (
  id, msp_id, connection_id, provider_model_id, display_name,
  supported_features, context_limit, output_limit, zero_cost, enabled,
  discovered_at, last_discovered_at, version, created_at, updated_at
)
SELECT $4, $2, connection.id, $5, $6, $7, $8, $9, $10, false,
       $11, $11, $12, $11, $11
FROM connection
ON CONFLICT (connection_id, provider_model_id) DO UPDATE
SET display_name = EXCLUDED.display_name,
    context_limit = EXCLUDED.context_limit,
    last_discovered_at = EXCLUDED.last_discovered_at,
    version = CASE
      WHEN ai_model_profiles.display_name IS DISTINCT FROM EXCLUDED.display_name
        OR ai_model_profiles.context_limit IS DISTINCT FROM EXCLUDED.context_limit
      THEN ai_model_profiles.version + 1
      ELSE ai_model_profiles.version
    END
RETURNING id::text, msp_id::text, connection_id::text, provider_model_id,
          display_name, supported_features, context_limit, output_limit,
          zero_cost, input_cost_per_million_minor, output_cost_per_million_minor, enabled, version
`, accepted.ConnectionID, accepted.MSPID, accepted.ConnectionVersion,
				model.ID, model.ProviderModelID, model.DisplayName,
				featuresToStrings(model.SupportedFeatures), model.ContextLimit,
				model.OutputLimit, model.ZeroCost, accepted.Audit.OccurredAt, model.Version))
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return object.ErrVersionConflict
				}
				return err
			}
			persisted = append(persisted, profile)
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
	if err != nil {
		return nil, err
	}
	return persisted, nil
}

const providerConnectionAdvisoryKeySQL = "hashtextextended('ai.provider.' || $1::text || '.' || $2::text, 0)"
const providerConnectionAdvisoryLockSQL = "SELECT pg_advisory_lock_shared(" + providerConnectionAdvisoryKeySQL + ")"
const providerConnectionAdvisoryUnlockSQL = "SELECT pg_advisory_unlock_shared(" + providerConnectionAdvisoryKeySQL + ")"

func lockProviderConnection(ctx context.Context, tx transaction, mspID, connectionID string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock("+providerConnectionAdvisoryKeySQL+")", mspID, connectionID)
	return err
}

func (r *AIManagementRepository) UpdateModels(ctx context.Context, accepted aiassist.ModelBatchMutation) ([]aiassist.ModelProfile, error) {
	if strings.TrimSpace(accepted.MSPID) == "" || len(accepted.Mutations) == 0 {
		return nil, aiassist.ErrInvalidProviderManagement
	}
	modelIDs, err := modelIDsForBatch(accepted)
	if err != nil {
		return nil, err
	}
	updated := make([]aiassist.ModelProfile, 0, len(accepted.Mutations))
	err = r.withTransaction(ctx, func(tx transaction) error {
		connectionIDs, err := providerConnectionIDsForModels(ctx, tx, accepted.MSPID, modelIDs)
		if err != nil {
			return err
		}
		if err := lockProviderConnections(ctx, tx, accepted.MSPID, connectionIDs); err != nil {
			return err
		}
		for _, mutation := range accepted.Mutations {
			model := mutation.Model
			tag, err := tx.Exec(ctx, `
UPDATE ai_model_profiles
SET display_name = $4, supported_features = $5, context_limit = $6,
    output_limit = $7, zero_cost = $8, input_cost_per_million_minor = $9,
    output_cost_per_million_minor = $10, enabled = $11, updated_at = $12,
    version = version + 1
WHERE id = $1 AND msp_id = $2 AND version = $3
`, model.ID, accepted.MSPID, mutation.ExpectedVersion, model.DisplayName,
				featuresToStrings(model.SupportedFeatures), model.ContextLimit,
				model.OutputLimit, model.ZeroCost, model.InputCostPerMillionMinor, model.OutputCostPerMillionMinor,
				model.Enabled, mutation.Audit.OccurredAt)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return object.ErrVersionConflict
			}
			if err := writeMutationFacts(ctx, tx, mutation.Audit, mutation.Event); err != nil {
				return err
			}
			updated = append(updated, model)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (r *AIManagementRepository) GetPolicy(ctx context.Context, target scope.Target) (aiassist.Policy, error) {
	var (
		policy   aiassist.Policy
		features []string
	)
	err := r.db.QueryRow(ctx, `
SELECT msp_id::text, enabled, allowed_features,
       COALESCE(summary_model_profile_id::text, ''),
       COALESCE(reply_draft_model_profile_id::text, ''),
       COALESCE(similar_suggestions_model_profile_id::text, ''),
       COALESCE(classification_model_profile_id::text, ''),
	   COALESCE(calendar_recommendation_model_profile_id::text, ''),
       cost_limit_enabled, allow_unmetered_unknown, monthly_cost_limit_minor, version,
       CASE
         WHEN cardinality(ARRAY_REMOVE(ARRAY[
           summary_model_profile_id, reply_draft_model_profile_id,
	       similar_suggestions_model_profile_id, classification_model_profile_id,
	       calendar_recommendation_model_profile_id
         ], NULL)) = 0 THEN false
         ELSE NOT EXISTS (
           SELECT 1
           FROM unnest(ARRAY_REMOVE(ARRAY[
             summary_model_profile_id, reply_draft_model_profile_id,
	         similar_suggestions_model_profile_id, classification_model_profile_id,
	         calendar_recommendation_model_profile_id
           ], NULL)) AS selected(model_profile_id)
           JOIN ai_model_profiles selected_model
             ON selected_model.id = selected.model_profile_id
           JOIN ai_provider_connections selected_connection
             ON selected_connection.id = selected_model.connection_id
           WHERE selected_connection.disclosure_accepted_at IS NULL
              OR selected_connection.disclosure_accepted_by IS NULL
         )
       END
FROM ai_policies
WHERE msp_id = $1
`, target.MSPID).Scan(&policy.MSPID, &policy.Enabled, &features,
		&policy.SummaryModelProfileID, &policy.ReplyDraftModelProfileID,
		&policy.SimilarSuggestionsModelProfileID, &policy.ClassificationModelProfileID,
		&policy.CalendarRecommendationModelProfileID,
		&policy.CostLimitEnabled,
		&policy.AllowUnmeteredUnknown, &policy.MonthlyCostLimitMinor, &policy.Version,
		&policy.ProviderDisclosureAccepted)
	if errors.Is(err, pgx.ErrNoRows) {
		return aiassist.Policy{}, scope.ErrNotFound
	}
	if err != nil {
		return aiassist.Policy{}, err
	}
	policy.PromptVersion = "ai-v1"
	policy.AllowedFeatures = featuresFromStrings(features)
	return policy, nil
}

func (r *AIManagementRepository) UpdatePolicy(ctx context.Context, accepted aiassist.PolicyMutation) error {
	if accepted.Policy.Enabled && !accepted.Policy.ProviderDisclosureAccepted {
		return aiassist.ErrInvalidProviderManagement
	}
	return r.withTransaction(ctx, func(tx transaction) error {
		currentModelIDs, err := policyModelIDs(ctx, tx, accepted.Policy.MSPID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		connectionIDs, err := providerConnectionIDsForModels(ctx, tx, accepted.Policy.MSPID,
			append(currentModelIDs, selectedPolicyModelIDs(accepted.Policy)...))
		if err != nil {
			return err
		}
		if err := lockProviderConnections(ctx, tx, accepted.Policy.MSPID, connectionIDs); err != nil {
			return err
		}
		if !accepted.Created {
			if err := validatePolicyVersion(ctx, tx, accepted.Policy.MSPID, accepted.ExpectedVersion); err != nil {
				return err
			}
		}
		if accepted.Policy.ProviderDisclosureAccepted {
			if err := acceptSelectedProviderDisclosures(ctx, tx, accepted); err != nil {
				return err
			}
		}
		if err := validatePolicyModels(ctx, tx, accepted.Policy); err != nil {
			return err
		}
		policy := accepted.Policy
		var tag pgconnTag
		if accepted.Created {
			if r.newID == nil {
				return aiassist.ErrInvalidProviderManagement
			}
			tag, err = tx.Exec(ctx, `
INSERT INTO ai_policies (
  id, msp_id, enabled, allowed_features, summary_model_profile_id,
	reply_draft_model_profile_id, similar_suggestions_model_profile_id,
	calendar_recommendation_model_profile_id,
  cost_limit_enabled, allow_unmetered_unknown, monthly_cost_limit_minor, version, created_at,
  created_by, updated_at, updated_by
) VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid,
	      NULLIF($7, '')::uuid, NULLIF($8, '')::uuid, $9, $10, $11, $12, $13, $14, $13, $14)
`, r.newID(), policy.MSPID, policy.Enabled, featuresToStrings(policy.AllowedFeatures),
				policy.SummaryModelProfileID, policy.ReplyDraftModelProfileID,
				policy.SimilarSuggestionsModelProfileID, policy.CalendarRecommendationModelProfileID, policy.CostLimitEnabled,
				policy.AllowUnmeteredUnknown, policy.MonthlyCostLimitMinor, policy.Version, accepted.Audit.OccurredAt,
				accepted.Audit.ActorID)
		} else {
			tag, err = tx.Exec(ctx, `
UPDATE ai_policies
SET enabled = $3, allowed_features = $4,
    summary_model_profile_id = NULLIF($5, '')::uuid,
    reply_draft_model_profile_id = NULLIF($6, '')::uuid,
    similar_suggestions_model_profile_id = NULLIF($7, '')::uuid,
	calendar_recommendation_model_profile_id = NULLIF($8, '')::uuid,
	cost_limit_enabled = $9, allow_unmetered_unknown = $10, monthly_cost_limit_minor = $11,
	updated_at = $12, updated_by = $13, version = version + 1
WHERE msp_id = $1 AND version = $2
`, policy.MSPID, accepted.ExpectedVersion, policy.Enabled,
				featuresToStrings(policy.AllowedFeatures), policy.SummaryModelProfileID,
				policy.ReplyDraftModelProfileID, policy.SimilarSuggestionsModelProfileID,
				policy.CalendarRecommendationModelProfileID, policy.CostLimitEnabled, policy.AllowUnmeteredUnknown, policy.MonthlyCostLimitMinor,
				accepted.Audit.OccurredAt, accepted.Audit.ActorID)
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *AIManagementRepository) RecordConnectionHealth(ctx context.Context, accepted aiassist.ConnectionHealthMutation) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		if err := lockProviderConnection(ctx, tx, accepted.MSPID, accepted.ConnectionID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
UPDATE ai_provider_connections
SET health_state = $4, last_tested_at = $5, last_succeeded_at = $6,
    last_error_code = NULLIF($7, ''), updated_at = $8, updated_by = $9,
    version = version + 1
WHERE id = $1 AND msp_id = $2 AND version = $3
`, accepted.ConnectionID, accepted.MSPID, accepted.ExpectedVersion,
			accepted.Health, accepted.TestedAt, accepted.SucceededAt,
			accepted.LastErrorCode, accepted.Audit.OccurredAt, accepted.Audit.ActorID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func modelIDsForBatch(accepted aiassist.ModelBatchMutation) ([]string, error) {
	seen := make(map[string]struct{}, len(accepted.Mutations))
	ids := make([]string, 0, len(accepted.Mutations))
	for _, mutation := range accepted.Mutations {
		model := mutation.Model
		if model.MSPID != accepted.MSPID || strings.TrimSpace(model.ID) == "" || mutation.ExpectedVersion < 1 {
			return nil, aiassist.ErrInvalidProviderManagement
		}
		if _, duplicate := seen[model.ID]; duplicate {
			return nil, aiassist.ErrInvalidProviderManagement
		}
		seen[model.ID] = struct{}{}
		ids = append(ids, model.ID)
	}
	return ids, nil
}

func providerConnectionIDsForModels(ctx context.Context, tx transaction, mspID string, modelIDs []string) ([]string, error) {
	if len(modelIDs) == 0 {
		return nil, nil
	}
	var connectionIDs []string
	err := tx.QueryRow(ctx, `
SELECT array_agg(DISTINCT profile.connection_id::text ORDER BY profile.connection_id::text)
FROM ai_model_profiles profile
JOIN ai_provider_connections connection
  ON connection.id = profile.connection_id AND connection.msp_id = profile.msp_id
WHERE profile.msp_id = $1 AND profile.id = ANY($2::uuid[])
`, mspID, modelIDs).Scan(&connectionIDs)
	if err != nil {
		return nil, err
	}
	return sortedUniqueStrings(connectionIDs), nil
}

func lockProviderConnections(ctx context.Context, tx transaction, mspID string, connectionIDs []string) error {
	for _, connectionID := range sortedUniqueStrings(connectionIDs) {
		if err := lockProviderConnection(ctx, tx, mspID, connectionID); err != nil {
			return err
		}
	}
	return nil
}

func sortedUniqueStrings(values []string) []string {
	values = append([]string(nil), values...)
	sort.Strings(values)
	unique := values[:0]
	for _, value := range values {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	return unique
}

func selectedPolicyModelIDs(policy aiassist.Policy) []string {
	return nonBlankStrings([]string{
		policy.SummaryModelProfileID,
		policy.ReplyDraftModelProfileID,
		policy.SimilarSuggestionsModelProfileID,
		policy.CalendarRecommendationModelProfileID,
	})
}

func policyModelIDs(ctx context.Context, tx transaction, mspID string) ([]string, error) {
	var summary, replyDraft, similar, classification, calendarRecommendation string
	err := tx.QueryRow(ctx, `
SELECT COALESCE(summary_model_profile_id::text, ''),
       COALESCE(reply_draft_model_profile_id::text, ''),
       COALESCE(similar_suggestions_model_profile_id::text, ''),
       COALESCE(classification_model_profile_id::text, '')
	   , COALESCE(calendar_recommendation_model_profile_id::text, '')
FROM ai_policies
WHERE msp_id = $1
`, mspID).Scan(&summary, &replyDraft, &similar, &classification, &calendarRecommendation)
	if err != nil {
		return nil, err
	}
	return nonBlankStrings([]string{summary, replyDraft, similar, classification, calendarRecommendation}), nil
}

func validatePolicyVersion(ctx context.Context, tx transaction, mspID string, expectedVersion int64) error {
	var version int64
	err := tx.QueryRow(ctx, `
SELECT version
FROM ai_policies
WHERE msp_id = $1
FOR UPDATE
`, mspID).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) || version != expectedVersion {
		return object.ErrVersionConflict
	}
	return err
}

func nonBlankStrings(values []string) []string {
	nonBlank := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			nonBlank = append(nonBlank, value)
		}
	}
	return nonBlank
}

type sealedCredential struct {
	version    any
	nonce      any
	ciphertext any
}

func (r *AIManagementRepository) sealCredential(ctx context.Context, connectionID string, plaintext []byte) (sealedCredential, error) {
	if len(plaintext) == 0 {
		return sealedCredential{}, nil
	}
	if r.secrets == nil || strings.TrimSpace(connectionID) == "" {
		return sealedCredential{}, aiassist.ErrInvalidProviderManagement
	}
	sealed, err := r.secrets.Seal(ctx, "ai.provider."+connectionID, plaintext)
	if err != nil {
		return sealedCredential{}, err
	}
	return sealedCredential{version: sealed.Version, nonce: sealed.Nonce, ciphertext: sealed.Ciphertext}, nil
}

func wipeCredential(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func (r *AIManagementRepository) withTransaction(ctx context.Context, fn func(transaction) error) error {
	if r == nil || r.db == nil {
		return aiassist.ErrInvalidProviderManagement
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

// pgconnTag keeps SQL mutation code independent from the concrete pgx adapter.
// The project transaction contract already returns this shape.
type pgconnTag interface{ RowsAffected() int64 }

func scanProviderConnection(scanner interface{ Scan(...any) error }) (aiassist.ProviderConnection, error) {
	var (
		connection     aiassist.ProviderConnection
		adapter        string
		network        string
		health         string
		timeoutSeconds int64
	)
	err := scanner.Scan(&connection.ID, &connection.MSPID, &connection.Name,
		&adapter, &network, &connection.BaseURL, &connection.CredentialConfigured,
		&connection.Enabled, &timeoutSeconds, &connection.RequestLimitBytes,
		&connection.ResponseLimitBytes, &connection.DisclosureAcceptedAt,
		&connection.LocalNetworkAcknowledgedAt,
		&health, &connection.LastTestedAt, &connection.LastSucceededAt,
		&connection.LastErrorCode, &connection.Version)
	if err != nil {
		return aiassist.ProviderConnection{}, err
	}
	connection.Adapter, connection.Network, connection.Health = aiassist.AdapterType(adapter), aiassist.NetworkMode(network), aiassist.HealthState(health)
	connection.Timeout = time.Duration(timeoutSeconds) * time.Second
	return connection, nil
}

func collectModels(rows rows) ([]aiassist.ModelProfile, error) {
	models := make([]aiassist.ModelProfile, 0)
	for rows.Next() {
		model, err := scanModelProfile(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return models, nil
}

func scanModelProfile(scanner interface{ Scan(...any) error }) (aiassist.ModelProfile, error) {
	var (
		model    aiassist.ModelProfile
		features []string
	)
	err := scanner.Scan(&model.ID, &model.MSPID, &model.ConnectionID,
		&model.ProviderModelID, &model.DisplayName, &features,
		&model.ContextLimit, &model.OutputLimit, &model.ZeroCost,
		&model.InputCostPerMillionMinor, &model.OutputCostPerMillionMinor,
		&model.Enabled, &model.Version)
	if err != nil {
		return aiassist.ModelProfile{}, err
	}
	model.SupportedFeatures = featuresFromStrings(features)
	return model, nil
}

func validatePolicyModels(ctx context.Context, tx transaction, policy aiassist.Policy) error {
	type selectedModel struct {
		id      string
		feature aiassist.Feature
	}
	selected := []selectedModel{
		{policy.SummaryModelProfileID, aiassist.FeatureSummary},
		{policy.ReplyDraftModelProfileID, aiassist.FeatureReplyDraft},
		{policy.SimilarSuggestionsModelProfileID, aiassist.FeatureSimilar},
		{policy.CalendarRecommendationModelProfileID, aiassist.FeatureCalendarRecommendation},
	}
	selectedCount := 0
	for _, item := range selected {
		if strings.TrimSpace(item.id) != "" {
			selectedCount++
		}
	}
	if selectedCount == 0 {
		return nil
	}
	var matched int64
	err := tx.QueryRow(ctx, `
WITH selected(model_profile_id, feature) AS (
  VALUES (NULLIF($2, '')::uuid, 'summary'::text),
         (NULLIF($3, '')::uuid, 'reply_draft'::text),
	     (NULLIF($4, '')::uuid, 'similar_suggestions'::text),
	     (NULLIF($5, '')::uuid, 'calendar_recommendation'::text)
)
SELECT count(*)
FROM selected
JOIN ai_model_profiles profile
  ON profile.id = selected.model_profile_id AND profile.msp_id = $1
JOIN ai_provider_connections connection
  ON connection.id = profile.connection_id AND connection.msp_id = profile.msp_id
WHERE profile.enabled AND connection.enabled
  AND connection.disclosure_accepted_at IS NOT NULL
  AND connection.disclosure_accepted_by IS NOT NULL
  AND selected.feature = ANY(profile.supported_features)
	AND (selected.feature <> 'calendar_recommendation' OR profile.zero_cost)
	`, policy.MSPID, policy.SummaryModelProfileID, policy.ReplyDraftModelProfileID,
		policy.SimilarSuggestionsModelProfileID, policy.CalendarRecommendationModelProfileID).Scan(&matched)
	if err != nil {
		return err
	}
	if matched != int64(selectedCount) {
		return aiassist.ErrInvalidProviderManagement
	}
	return nil
}

func acceptSelectedProviderDisclosures(
	ctx context.Context,
	tx transaction,
	accepted aiassist.PolicyMutation,
) error {
	_, err := tx.Exec(ctx, `
WITH selected(model_profile_id) AS (
  VALUES (NULLIF($1, '')::uuid),
         (NULLIF($2, '')::uuid),
	     (NULLIF($3, '')::uuid),
	     (NULLIF($4, '')::uuid)
), selected_connections AS (
  SELECT DISTINCT connection.id
  FROM selected
  JOIN ai_model_profiles profile
	ON profile.id = selected.model_profile_id AND profile.msp_id = $5
  JOIN ai_provider_connections connection
    ON connection.id = profile.connection_id AND connection.msp_id = profile.msp_id
)
UPDATE ai_provider_connections connection
	SET disclosure_accepted_at = $6,
	disclosure_accepted_by = $7::uuid,
	updated_at = $6,
	updated_by = $7::uuid,
    version = connection.version + 1
FROM selected_connections selected
WHERE connection.id = selected.id
  AND (connection.disclosure_accepted_at IS NULL
       OR connection.disclosure_accepted_by IS NULL)
`, accepted.Policy.SummaryModelProfileID,
		accepted.Policy.ReplyDraftModelProfileID,
		accepted.Policy.SimilarSuggestionsModelProfileID,
		accepted.Policy.CalendarRecommendationModelProfileID,
		accepted.Policy.MSPID, accepted.Audit.OccurredAt, accepted.Audit.ActorID)
	return err
}

func featuresToStrings(features []aiassist.Feature) []string {
	values := make([]string, len(features))
	for index, feature := range features {
		values[index] = string(feature)
	}
	return values
}

func featuresFromStrings(values []string) []aiassist.Feature {
	features := make([]aiassist.Feature, len(values))
	for index, value := range values {
		features[index] = aiassist.Feature(value)
	}
	return features
}

package setup

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) IssueToken(
	ctx context.Context,
	hash [32]byte,
	issuedAt, expiresAt time.Time,
) error {
	_, err := r.pool.Exec(ctx, `
WITH invalidated AS (
  UPDATE installation_bootstrap_tokens
  SET consumed_at = $2
  WHERE consumed_at IS NULL
  RETURNING token_hash
)
INSERT INTO installation_bootstrap_tokens (token_hash, issued_at, expires_at)
SELECT $1, $2, $3
FROM (SELECT count(*) FROM invalidated) AS invalidation_barrier
`, hash[:], issuedAt, expiresAt)
	return err
}

func (r *PostgresRepository) Status(ctx context.Context) (bool, bool, error) {
	var completed, entraAvailable bool
	err := r.pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM installation_setup),
       EXISTS (
         SELECT 1 FROM installation_setup WHERE entra_state = 'connected'
       )
`).Scan(&completed, &entraAvailable)
	return completed, entraAvailable, err
}

func (r *PostgresRepository) IntakeStatus(
	ctx context.Context,
	mspID string,
) (IntakeStatus, error) {
	var status IntakeStatus
	err := r.pool.QueryRow(ctx, `
SELECT EXISTS (
         SELECT 1 FROM graph_mailbox_connections
         WHERE msp_id = $1 AND enabled
           AND credential_secret_ref <> ''
           AND health_state IN ('pending', 'healthy')
       ),
       EXISTS (
         SELECT 1 FROM service_api_keys
         WHERE msp_id = $1 AND revoked_at IS NULL AND expires_at > now()
       ),
       EXISTS (
         SELECT 1 FROM forwarding_intake_connections
         WHERE msp_id = $1 AND enabled
           AND health_state IN ('pending', 'healthy')
       )
`, mspID).Scan(
		&status.GraphConfigured,
		&status.APIKeyConfigured,
		&status.ForwardingConfigured,
	)
	return status, err
}

func (r *PostgresRepository) Bootstrap(ctx context.Context, value BootstrapMutation) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var tokenAccepted bool
	err = tx.QueryRow(ctx, `
UPDATE installation_bootstrap_tokens
SET consumed_at = $2
WHERE token_hash = $1
  AND consumed_at IS NULL
  AND expires_at > $2
  AND NOT EXISTS (SELECT 1 FROM installation_setup)
RETURNING true
`, value.TokenHash[:], value.OccurredAt).Scan(&tokenAccepted)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBootstrapClosed
	}
	if err != nil || !tokenAccepted {
		return err
	}

	config := value.Configuration
	intake, _ := json.Marshal(config.Intake)
	objectStorage, _ := json.Marshal(config.ObjectStorage)
	backups, _ := json.Marshal(config.Backups)
	type statement struct {
		query string
		args  []any
	}
	var entraTenantID, entraClientID, entraSecretVersion any
	var entraSecretNonce, entraSecretCiphertext, entraRedirectURL any
	entraState := "not_connected"
	if config.EntraConfigured() {
		entraTenantID, entraClientID = config.EntraTenantID, config.EntraClientID
		entraSecretVersion = value.EntraSecret.Version
		entraSecretNonce, entraSecretCiphertext = value.EntraSecret.Nonce, value.EntraSecret.Ciphertext
		entraRedirectURL, entraState = config.EntraRedirectURL, "connected"
	}
	statements := []statement{
		{`INSERT INTO msp_organizations
			(id, display_id, name, created_at, created_by, updated_at, updated_by)
			VALUES ($1,$2,$3,$4,$5,$4,$5)`,
			[]any{value.MSPID, config.OrganizationDisplay, config.OrganizationName, value.OccurredAt, value.TechnicianID}},
		{`INSERT INTO technicians (id,msp_id,email,display_name,created_at,updated_at)
			VALUES ($1,$2,$3,$4,$5,$5)`,
			[]any{value.TechnicianID, value.MSPID, config.AdminEmail, config.AdminDisplayName, value.OccurredAt}},
	}
	if config.EntraConfigured() {
		statements = append(statements, statement{
			`INSERT INTO external_identities
				(id,msp_id,technician_id,provider,issuer,subject,tenant_id,email_at_link,linked_at)
				VALUES ($1,$2,$3,'entra',$4,$5,$6,$7,$8)`,
			[]any{value.IdentityID, value.MSPID, value.TechnicianID,
				"https://login.microsoftonline.com/" + config.EntraTenantID + "/v2.0",
				config.AdminEntraSubject, config.EntraTenantID, config.AdminEmail,
				value.OccurredAt},
		})
	}
	statements = append(statements,
		statement{
			`INSERT INTO roles (id,msp_id,key,name,system_role,created_at,updated_at)
			 VALUES ($1,$2,'global-admin','Global administrator',true,$3,$3)`,
			[]any{value.RoleID, value.MSPID, value.OccurredAt},
		},
		statement{
			`INSERT INTO role_capabilities (role_id,msp_id,capability)
			 SELECT $1,$2,unnest($3::text[])`,
			[]any{value.RoleID, value.MSPID, globalAdminCapabilities},
		},
		statement{
			`INSERT INTO tag_groups
				(id,msp_id,internal_key,label,description,position,lifecycle_state,system_group,created_by,updated_by)
			 VALUES
				(md5($1::uuid::text || ':classification:system-group')::uuid,$1,'taxonomy.system','System',
				 'System-managed classification tags.',1,'active',true,$2,$2)`,
			[]any{value.MSPID, value.TechnicianID},
		},
		statement{
			`INSERT INTO tags
				(id,msp_id,group_id,internal_key,label,description,lifecycle_state,system_tag,created_by,updated_by)
			 VALUES
				(md5($1::uuid::text || ':classification:unclassified')::uuid,$1,
				 md5($1::uuid::text || ':classification:system-group')::uuid,
				 'taxonomy.system.unclassified','Unclassified',
				 'Required fallback until a governed tag is selected.','active',true,$2,$2)`,
			[]any{value.MSPID, value.TechnicianID},
		},
		statement{
			`INSERT INTO roles
			 (id,msp_id,key,name,system_role,created_at,updated_at)
			 VALUES (
			   md5($1::uuid::text || ':time_reviewer')::uuid,
			   $1::uuid,'time_reviewer','Time reviewer',true,$2,$2
			 )`,
			[]any{value.MSPID, value.OccurredAt},
		},
		statement{
			`INSERT INTO role_capabilities (role_id,msp_id,capability)
			 SELECT md5($1::uuid::text || ':time_reviewer')::uuid,$1::uuid,
			   unnest($2::text[])`,
			[]any{value.MSPID, timeReviewerCapabilities},
		},
		statement{
			`INSERT INTO role_assignments
				(id,msp_id,technician_id,role_id,granted_at,granted_by)
			 VALUES ($1,$2,$3,$4,$5,$3)`,
			[]any{value.AssignmentID, value.MSPID, value.TechnicianID, value.RoleID, value.OccurredAt},
		},
		statement{
			`INSERT INTO break_glass_accounts
				(id,msp_id,technician_id,username,password_hash,allowed_cidrs,created_at,created_by,updated_at)
			 VALUES ($1,$2,$3,lower(btrim($4)),$5,$6::cidr[],$7,$3,$7)`,
			[]any{value.RecoveryID, value.MSPID, value.TechnicianID, config.RecoveryUsername, value.PasswordHash, config.RecoveryAllowedCIDR, value.OccurredAt},
		},
		statement{
			`INSERT INTO installation_setup
				(msp_id,entra_tenant_id,entra_client_id,entra_secret_version,
				 entra_secret_nonce,entra_secret_ciphertext,entra_redirect_url,
				 entra_state,intake_config,object_storage_config,backup_config,
				 completed_at,completed_by)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb,$11::jsonb,$12,$13)`,
			[]any{value.MSPID, entraTenantID, entraClientID, entraSecretVersion,
				entraSecretNonce, entraSecretCiphertext, entraRedirectURL,
				entraState, intake, objectStorage, backups, value.OccurredAt,
				value.TechnicianID},
		},
		statement{
			`INSERT INTO audit_ledger
				(id,occurred_at,msp_id,actor_type,actor_id,action,subject_type,subject_id,
				 subject_version,source,reason,correlation_id,safe_diff)
			 VALUES ($1,$2,$3,'deployment_operator',$4,'installation.bootstrap.completed',
				 'installation',$3,1,'setup_wizard','Initial installation',$5,
				 '{"bootstrap_token":"consumed","secrets":"redacted"}')`,
			[]any{value.AuditID, value.OccurredAt, value.MSPID, value.TechnicianID, value.CorrelationID},
		},
		statement{
			`INSERT INTO event_outbox
				(event_id,event_type,schema_version,occurred_at,msp_id,actor_type,actor_id,
				 subject_type,subject_id,subject_version,correlation_id,source)
			 VALUES ($1,'installation.bootstrap.completed',1,$2,$3,'deployment_operator',
				 $4,'installation',$3,1,$5,'setup_wizard')`,
			[]any{value.EventID, value.OccurredAt, value.MSPID, value.TechnicianID, value.CorrelationID},
		},
	)
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement.query, statement.args...); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

type RuntimeConfiguration struct {
	MSPID             string
	EntraConfigured   bool
	EntraState        string
	EntraVersion      int64
	EntraTenantID     string
	EntraClientID     string
	EntraClientSecret string
	EntraRedirectURL  string
}

func (r *PostgresRepository) RuntimeConfiguration(
	ctx context.Context,
	provider secrets.Provider,
) (RuntimeConfiguration, bool, error) {
	var result RuntimeConfiguration
	var tenantID, clientID, redirectURL *string
	var secretVersion *int
	var nonce, ciphertext []byte
	err := r.pool.QueryRow(ctx, `
SELECT msp_id::text, entra_tenant_id, entra_client_id, entra_secret_version,
       entra_secret_nonce, entra_secret_ciphertext, entra_redirect_url,
       entra_state, entra_version
FROM installation_setup
WHERE singleton = true
`).Scan(
		&result.MSPID, &tenantID, &clientID, &secretVersion, &nonce, &ciphertext,
		&redirectURL, &result.EntraState, &result.EntraVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RuntimeConfiguration{}, false, nil
	}
	if err != nil {
		return RuntimeConfiguration{}, false, err
	}
	if tenantID == nil {
		return result, true, nil
	}
	result.EntraConfigured = result.EntraState == "connected"
	result.EntraTenantID, result.EntraClientID = *tenantID, *clientID
	result.EntraRedirectURL = *redirectURL
	sealed := secrets.SealedValue{
		Version: *secretVersion, Nonce: nonce, Ciphertext: ciphertext,
	}
	plaintext, err := provider.Open(
		ctx, "installation.entra.client_secret", sealed,
	)
	if err != nil {
		return RuntimeConfiguration{}, false, err
	}
	result.EntraClientSecret = string(plaintext)
	return result, true, nil
}

func (r *PostgresRepository) CenterStatus(
	ctx context.Context,
	mspID string,
) (CenterStatus, error) {
	var completedAt time.Time
	var configurationVersion int64
	var intakeJSON, objectStorageJSON, backupsJSON []byte
	var entraState string
	err := r.pool.QueryRow(ctx, `
SELECT completed_at,
       setup_version,
       intake_config,
       object_storage_config,
       backup_config,
       entra_state
FROM installation_setup
WHERE singleton = true AND msp_id = $1
`, mspID).Scan(
		&completedAt, &configurationVersion,
		&intakeJSON, &objectStorageJSON, &backupsJSON, &entraState,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CenterStatus{}, ErrBootstrapClosed
	}
	if err != nil {
		return CenterStatus{}, err
	}
	var intake, objectStorage, backups map[string]any
	if json.Unmarshal(intakeJSON, &intake) != nil ||
		json.Unmarshal(objectStorageJSON, &objectStorage) != nil ||
		json.Unmarshal(backupsJSON, &backups) != nil {
		return CenterStatus{}, ErrInvalidCenterConfiguration
	}
	objectVerification, err := r.verification(ctx, mspID, "object_storage")
	if err != nil {
		return CenterStatus{}, err
	}
	backupVerification, err := r.verification(ctx, mspID, "backups")
	if err != nil {
		return CenterStatus{}, err
	}
	return CenterStatus{
		CompletedAt: completedAt.UTC(), ConfigurationVersion: configurationVersion,
		Intake: intake, ObjectStorage: objectStorage, Backups: backups,
		ObjectStorageStatus: ObjectStorageStatus{Verification: objectVerification},
		BackupStatus:        BackupStatus{Verification: backupVerification},
		Sections: []CenterSection{
			identitySection(entraState),
			configSection(
				"intake", "Mailbox and API intake", len(intake) > 0,
				"#/operations",
			),
			configSection(
				"object_storage", "Object storage", len(objectStorage) > 0,
				"#/setup",
			),
			configSection(
				"backups", "Backup and PITR", len(backups) > 0,
				"#/setup",
			),
			{
				Key: "integrations", Label: "Integration health",
				State: "verify", Summary: "Review live freshness and delivery evidence.",
				RemediationHref: "#/operations",
			},
		},
	}, nil
}

func (r *PostgresRepository) verification(
	ctx context.Context,
	mspID, section string,
) (*VerificationResult, error) {
	var result VerificationResult
	var detailsJSON []byte
	err := r.pool.QueryRow(ctx, `
SELECT section,state,safe_code,checked_at,valid_until,details,version
FROM setup_verification_evidence
WHERE msp_id=$1 AND section=$2
`, mspID, section).Scan(
		&result.Section, &result.State, &result.SafeCode, &result.CheckedAt,
		&result.ValidUntil, &detailsJSON, &result.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(detailsJSON, &result.Details); err != nil {
		return nil, ErrInvalidCenterConfiguration
	}
	return &result, nil
}

func (r *PostgresRepository) UpdateCenterConfiguration(
	ctx context.Context,
	value CenterConfigurationMutation,
) (CenterStatus, error) {
	intake, err := json.Marshal(value.Intake)
	if err != nil {
		return CenterStatus{}, ErrInvalidCenterConfiguration
	}
	objectStorage, err := json.Marshal(value.ObjectStorage)
	if err != nil {
		return CenterStatus{}, ErrInvalidCenterConfiguration
	}
	backups, err := json.Marshal(value.Backups)
	if err != nil {
		return CenterStatus{}, ErrInvalidCenterConfiguration
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return CenterStatus{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var version int64
	err = tx.QueryRow(ctx, `
UPDATE installation_setup
SET intake_config = $3::jsonb,
    object_storage_config = $4::jsonb,
    backup_config = $5::jsonb,
    setup_version = setup_version + 1
WHERE singleton = true AND msp_id = $1 AND setup_version = $2
RETURNING setup_version
`, value.Audit.MSPID, value.ExpectedVersion, intake, objectStorage, backups).
		Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return CenterStatus{}, ErrCenterConfigurationConflict
	}
	if err != nil {
		return CenterStatus{}, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_ledger (
  id, occurred_at, msp_id, actor_type, actor_id, action, subject_type,
  subject_id, subject_version, source, reason, correlation_id, safe_diff
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,
  '{"configuration":"references_updated","inline_secrets":"rejected"}'::jsonb)
`, value.Audit.ID, value.Audit.OccurredAt, value.Audit.MSPID,
		value.Audit.ActorType, value.Audit.ActorID, value.Audit.Action,
		value.Audit.SubjectType, value.Audit.SubjectID, version,
		value.Audit.Source, value.Audit.Reason, value.Audit.CorrelationID); err != nil {
		return CenterStatus{}, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO event_outbox (
  event_id, event_type, schema_version, occurred_at, msp_id, actor_type,
  actor_id, subject_type, subject_id, subject_version, correlation_id,
  source, data
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'{}'::jsonb)
`, value.Event.EventID, value.Event.EventType, value.Event.SchemaVersion,
		value.Event.OccurredAt, value.Event.MSPID, value.Event.ActorType,
		value.Event.ActorID, value.Event.SubjectType, value.Event.SubjectID,
		version, value.Event.CorrelationID, value.Event.Source); err != nil {
		return CenterStatus{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CenterStatus{}, err
	}
	return r.CenterStatus(ctx, value.Audit.MSPID)
}

func (r *PostgresRepository) RecordVerification(
	ctx context.Context,
	value VerificationMutation,
) error {
	details, err := json.Marshal(value.Details)
	if err != nil {
		return ErrInvalidCenterConfiguration
	}
	safeDiff, err := verificationSafeDiff(value.Section, value.SafeCode)
	if err != nil {
		return ErrInvalidCenterConfiguration
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var version int64
	err = tx.QueryRow(ctx, `
UPDATE installation_setup
SET setup_version = setup_version + 1
WHERE singleton = true AND msp_id = $1 AND setup_version = $2
RETURNING setup_version
`, value.Audit.MSPID, value.ExpectedVersion).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCenterConfigurationConflict
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO setup_verification_evidence (
  msp_id,section,state,safe_code,checked_at,valid_until,details,
  evidence_hash,consumed_nonce,version,updated_by
) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11)
ON CONFLICT (msp_id,section) DO UPDATE SET
  state=EXCLUDED.state,safe_code=EXCLUDED.safe_code,
  checked_at=EXCLUDED.checked_at,valid_until=EXCLUDED.valid_until,
  details=EXCLUDED.details,evidence_hash=EXCLUDED.evidence_hash,
  consumed_nonce=EXCLUDED.consumed_nonce,version=EXCLUDED.version,
  updated_by=EXCLUDED.updated_by
`, value.Audit.MSPID, value.Section, value.State, value.SafeCode,
		value.CheckedAt, value.ValidUntil, details, value.EvidenceHash,
		nullString(value.ConsumedNonce), version, value.Audit.ActorID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO audit_ledger (
  id,occurred_at,msp_id,actor_type,actor_id,action,subject_type,
  subject_id,subject_version,source,reason,correlation_id,safe_diff
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb)
`, value.Audit.ID, value.Audit.OccurredAt, value.Audit.MSPID,
		value.Audit.ActorType, value.Audit.ActorID, value.Audit.Action,
		value.Audit.SubjectType, value.Audit.SubjectID, version,
		value.Audit.Source, value.Audit.Reason, value.Audit.CorrelationID,
		safeDiff)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO event_outbox (
  event_id,event_type,schema_version,occurred_at,msp_id,actor_type,
  actor_id,subject_type,subject_id,subject_version,correlation_id,source,data
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb)
`, value.Event.EventID, value.Event.EventType, value.Event.SchemaVersion,
		value.Event.OccurredAt, value.Event.MSPID, value.Event.ActorType,
		value.Event.ActorID, value.Event.SubjectType, value.Event.SubjectID,
		version, value.Event.CorrelationID, value.Event.Source, safeDiff)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func verificationSafeDiff(section, safeCode string) ([]byte, error) {
	return json.Marshal(map[string]string{
		"section": section,
		"result":  safeCode,
	})
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func identitySection(entraState string) CenterSection {
	section := CenterSection{
		Key: "identity", Label: "Identity", State: "configured",
		RemediationHref: "#/recovery-access",
	}
	switch entraState {
	case "connected":
		section.Summary = "Local Platform Administrators and Microsoft Entra are configured."
	case "verification_required":
		section.State = "verify"
		section.Summary = "Local administration is ready; Microsoft Entra verification is required."
	default:
		section.Summary = "Local administration is ready; Microsoft Entra is optional and not connected."
	}
	return section
}

func configSection(
	key, label string,
	configured bool,
	href string,
) CenterSection {
	if configured {
		return CenterSection{
			Key: key, Label: label, State: "configured",
			Summary:         "Configuration references are recorded; live health still requires verification.",
			RemediationHref: href,
		}
	}
	return CenterSection{
		Key: key, Label: label, State: "missing",
		Summary:         "Required configuration references are missing.",
		RemediationHref: href,
	}
}

var globalAdminCapabilities = []string{
	"ai.assist", "ai.manage", "asset.create", "asset.lifecycle", "asset.update",
	"attachment.create", "audit.read",
	"automation.manage", "automation.dead_letter.manage", "change_order.update",
	"calendar.ai.recommend", "calendar.commitment.manage", "calendar.policy.manage",
	"calendar.read", "calendar.schedule", "calendar.workforce.manage",
	"client.create", "client.read",
	"client.update", "comment.internal.create", "comment.public.create",
	"classification.manage", "classification.apply", "classification.report", "classification.ai.manage",
	"contact.create", "contact.lifecycle", "contact.update",
	"contract.create", "contract.lifecycle", "contract.update", "integration.datto",
	"integration.datto.reconcile",
	"integration.graph", "integration.manage", "integration.read", "intake.write",
	"knowledge.edit", "knowledge.publish", "knowledge.read", "location.create",
	"location.lifecycle", "location.update",
	"mention.create", "mention.read",
	"notification.manage", "opportunity.activity.create", "opportunity.convert",
	"opportunity.create", "opportunity.read", "opportunity.transition", "opportunity.update",
	"organization.manage", "organization.read", "pipeline.create",
	"project.create", "project.edit", "project.financial.read", "project.read",
	"project.resource.plan", "proposal.accept", "proposal.acceptance.record", "proposal.approve",
	"proposal.create", "proposal.issue", "proposal.read", "prospect.create",
	"relationship.create", "role.manage", "routing.manage", "search.read",
	"service.create", "service.lifecycle", "service.update", "service_key.manage", "sla.manage", "sla.override",
	"task.create", "task.move", "time_entry.approve", "time_entry.create",
	"time_entry.export", "time_entry.read_scoped", "time_entry.update_own",
	"time_entry.amend", "timesheet.read_own", "timesheet.review",
	"view.save", "view.share", "workflow.publish",
	"work_record.assign", "work_record.comment", "work_record.create",
	"work_record.edit", "work_record.merge", "work_record.read",
	"work_record.route", "work_record.transition", "work_record.update",
}

var timeReviewerCapabilities = []string{
	"time_entry.read_scoped",
	"time_entry.amend",
	"time_entry.approve",
	"timesheet.review",
}

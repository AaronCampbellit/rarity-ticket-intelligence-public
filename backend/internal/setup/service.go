package setup

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidBootstrap            = errors.New("invalid bootstrap request")
	ErrBootstrapClosed             = errors.New("bootstrap is unavailable")
	ErrInvalidCenterConfiguration  = errors.New("invalid setup center configuration")
	ErrCenterConfigurationConflict = errors.New(
		"setup center configuration changed",
	)
)

const (
	tokenLifetime        = 15 * time.Minute
	localAdminBcryptCost = 12
)

type Configuration struct {
	OrganizationID      string         `json:"organization_id"`
	OrganizationName    string         `json:"organization_name"`
	OrganizationDisplay string         `json:"organization_display_id"`
	EntraTenantID       string         `json:"entra_tenant_id"`
	EntraClientID       string         `json:"entra_client_id"`
	EntraClientSecret   string         `json:"entra_client_secret"`
	EntraRedirectURL    string         `json:"entra_redirect_url"`
	AdminEmail          string         `json:"admin_email"`
	AdminDisplayName    string         `json:"admin_display_name"`
	AdminEntraSubject   string         `json:"admin_entra_subject"`
	RecoveryUsername    string         `json:"recovery_username"`
	RecoveryPassword    string         `json:"recovery_password"`
	RecoveryAllowedCIDR []string       `json:"recovery_allowed_cidrs"`
	Intake              map[string]any `json:"intake"`
	ObjectStorage       map[string]any `json:"object_storage"`
	Backups             map[string]any `json:"backups"`
}

type BootstrapMutation struct {
	TokenHash     [32]byte
	PasswordHash  string
	EntraSecret   secrets.SealedValue
	Configuration Configuration
	OccurredAt    time.Time
	MSPID         string
	TechnicianID  string
	IdentityID    string
	RoleID        string
	AssignmentID  string
	RecoveryID    string
	AuditID       string
	EventID       string
	CorrelationID string
}

type Repository interface {
	IssueToken(context.Context, [32]byte, time.Time, time.Time) error
	Bootstrap(context.Context, BootstrapMutation) error
	Status(context.Context) (bool, bool, error)
	CenterStatus(context.Context, string) (CenterStatus, error)
	IntakeStatus(context.Context, string) (IntakeStatus, error)
	RecordVerification(context.Context, VerificationMutation) error
	UpdateCenterConfiguration(
		context.Context,
		CenterConfigurationMutation,
	) (CenterStatus, error)
}

type CenterSection struct {
	Key             string         `json:"key"`
	Label           string         `json:"label"`
	State           ReadinessState `json:"state"`
	Summary         string         `json:"summary"`
	RemediationHref string         `json:"remediation_href,omitempty"`
}

type ReadinessState string

const (
	ReadinessNotStarted     ReadinessState = "not_started"
	ReadinessActionRequired ReadinessState = "action_required"
	ReadinessReadyToVerify  ReadinessState = "ready_to_verify"
	ReadinessVerified       ReadinessState = "verified"
	ReadinessAttention      ReadinessState = "attention"
)

type VerificationResult struct {
	Section    string            `json:"section"`
	State      ReadinessState    `json:"state"`
	SafeCode   string            `json:"safe_code"`
	CheckedAt  time.Time         `json:"checked_at"`
	ValidUntil time.Time         `json:"valid_until"`
	Details    map[string]string `json:"details"`
	Version    int64             `json:"version"`
}

type IntakeStatus struct {
	GraphConfigured      bool `json:"graph_configured"`
	APIKeyConfigured     bool `json:"api_key_configured"`
	ForwardingConfigured bool `json:"forwarding_configured"`
}

type ObjectStorageStatus struct {
	Provider             string              `json:"provider"`
	Endpoint             string              `json:"endpoint"`
	Bucket               string              `json:"bucket"`
	Region               string              `json:"region"`
	AccessKeyConfigured  bool                `json:"access_key_configured"`
	CredentialConfigured bool                `json:"credential_configured"`
	Verification         *VerificationResult `json:"verification,omitempty"`
}

type BackupStatus struct {
	EvidenceKeyConfigured bool                `json:"evidence_key_configured"`
	Verification          *VerificationResult `json:"verification,omitempty"`
}

type CenterStatus struct {
	MSPID                string              `json:"msp_id"`
	PublicURL            string              `json:"public_url"`
	CompletedAt          time.Time           `json:"completed_at"`
	ConfigurationVersion int64               `json:"configuration_version"`
	Intake               map[string]any      `json:"intake"`
	ObjectStorage        map[string]any      `json:"object_storage"`
	Backups              map[string]any      `json:"backups"`
	IntakeStatus         IntakeStatus        `json:"intake_status"`
	ObjectStorageStatus  ObjectStorageStatus `json:"object_storage_status"`
	BackupStatus         BackupStatus        `json:"backup_status"`
	Sections             []CenterSection     `json:"sections"`
}

type UpdateCenterConfigurationCommand struct {
	Principal       authorization.Principal
	ExpectedVersion int64
	Intake          map[string]any
	ObjectStorage   map[string]any
	Backups         map[string]any
	Reason          string
	Source          string
}

type CenterConfigurationMutation struct {
	ExpectedVersion int64
	Intake          map[string]any
	ObjectStorage   map[string]any
	Backups         map[string]any
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type Service struct {
	repository        Repository
	now               func() time.Time
	newID             func() string
	expectedMSPID     string
	secrets           secrets.Provider
	runtime           SetupRuntimeConfiguration
	storageProbe      StorageProbe
	backupEvidenceKey []byte
}

type SetupRuntimeConfiguration struct {
	PublicURL                   string
	S3Endpoint                  string
	S3Bucket                    string
	S3Region                    string
	S3AccessKeyConfigured       bool
	S3CredentialConfigured      bool
	BackupEvidenceKeyConfigured bool
}

type Option func(*Service)

func WithExpectedMSPID(value string) Option {
	return func(service *Service) { service.expectedMSPID = strings.TrimSpace(value) }
}

func WithSecretProvider(provider secrets.Provider) Option {
	return func(service *Service) { service.secrets = provider }
}

func WithSetupRuntimeConfiguration(value SetupRuntimeConfiguration) Option {
	return func(service *Service) { service.runtime = value }
}

func WithStorageProbe(probe StorageProbe) Option {
	return func(service *Service) { service.storageProbe = probe }
}

func WithBackupEvidenceKey(key []byte) Option {
	return func(service *Service) {
		service.backupEvidenceKey = append([]byte(nil), key...)
		service.runtime.BackupEvidenceKeyConfigured = len(key) >= 32
	}
}

func NewService(
	repository Repository,
	now func() time.Time,
	newID func() string,
	options ...Option,
) *Service {
	if now == nil {
		now = time.Now
	}
	service := &Service{repository: repository, now: now, newID: newID}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) IssueToken(ctx context.Context) (string, time.Time, error) {
	if s.repository == nil {
		return "", time.Time{}, ErrBootstrapClosed
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	now := s.now().UTC()
	expiresAt := now.Add(tokenLifetime)
	if err := s.repository.IssueToken(ctx, hash, now, expiresAt); err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func (s *Service) Status(ctx context.Context) (bool, bool, error) {
	if s.repository == nil {
		return false, false, ErrBootstrapClosed
	}
	return s.repository.Status(ctx)
}

func (s *Service) CenterStatus(
	ctx context.Context,
	principal authorization.Principal,
) (CenterStatus, error) {
	target := scope.Target{MSPID: principal.Scope.MSPID}
	if err := authorization.Authorize(principal, "organization.read", target); err != nil {
		return CenterStatus{}, err
	}
	if s.repository == nil {
		return CenterStatus{}, ErrBootstrapClosed
	}
	return s.composeCenterStatus(ctx, target.MSPID)
}

func (s *Service) composeCenterStatus(
	ctx context.Context,
	mspID string,
) (CenterStatus, error) {
	status, err := s.repository.CenterStatus(ctx, mspID)
	if err != nil {
		return CenterStatus{}, err
	}
	status.IntakeStatus, err = s.repository.IntakeStatus(ctx, mspID)
	if err != nil {
		return CenterStatus{}, err
	}
	status.MSPID, status.PublicURL = mspID, s.runtime.PublicURL
	objectVerification := status.ObjectStorageStatus.Verification
	backupVerification := status.BackupStatus.Verification
	status.ObjectStorageStatus = ObjectStorageStatus{
		Provider: providerName(s.runtime.S3Endpoint),
		Endpoint: s.runtime.S3Endpoint, Bucket: s.runtime.S3Bucket,
		Region:               s.runtime.S3Region,
		AccessKeyConfigured:  s.runtime.S3AccessKeyConfigured,
		CredentialConfigured: s.runtime.S3CredentialConfigured,
		Verification:         objectVerification,
	}
	status.BackupStatus.EvidenceKeyConfigured =
		s.runtime.BackupEvidenceKeyConfigured
	status.BackupStatus.Verification = backupVerification
	identity := CenterSection{
		Key: "identity", Label: "Identity", State: ReadinessVerified,
		Summary:         "Local administration is configured.",
		RemediationHref: "#/recovery-access",
	}
	for _, section := range status.Sections {
		if section.Key == "identity" {
			identity = section
			identity.State = ReadinessVerified
			break
		}
	}
	intakeReady := status.IntakeStatus.GraphConfigured ||
		status.IntakeStatus.APIKeyConfigured ||
		status.IntakeStatus.ForwardingConfigured
	intakeState, intakeSummary := ReadinessActionRequired,
		"Choose Microsoft 365 mailbox, service API, or forwarding intake."
	if intakeReady {
		intakeState, intakeSummary = ReadinessVerified,
			"At least one active intake path is configured."
	}
	storageReady := status.ObjectStorageStatus.Endpoint != "" &&
		status.ObjectStorageStatus.Bucket != "" &&
		status.ObjectStorageStatus.Region != "" &&
		status.ObjectStorageStatus.AccessKeyConfigured &&
		status.ObjectStorageStatus.CredentialConfigured
	storageState, storageSummary := ReadinessActionRequired,
		"Complete the S3-compatible storage settings in the deployment."
	if storageReady {
		storageState, storageSummary = ReadinessReadyToVerify,
			"Runtime storage settings are present and ready for a live test."
	}
	if verification := status.ObjectStorageStatus.Verification; verification != nil {
		storageState, storageSummary = verification.State,
			"Latest live storage test: "+verification.SafeCode+"."
		if verification.ValidUntil.Before(s.now().UTC()) {
			storageState, storageSummary = ReadinessAttention,
				"The latest storage test has expired; run it again."
		}
	}
	backupState, backupSummary := ReadinessActionRequired,
		"Configure pgBackRest and its verification authority."
	if status.BackupStatus.EvidenceKeyConfigured {
		backupState, backupSummary = ReadinessReadyToVerify,
			"Backup evidence authority is configured; submit a current verification."
	}
	if verification := status.BackupStatus.Verification; verification != nil {
		backupState, backupSummary = verification.State,
			"Latest backup evidence: "+verification.SafeCode+"."
		if verification.ValidUntil.Before(s.now().UTC()) {
			backupState, backupSummary = ReadinessAttention,
				"The latest backup evidence has expired; submit a new verification."
		}
	}
	status.Sections = []CenterSection{
		identity,
		{Key: "intake", Label: "Mailbox and API intake", State: intakeState, Summary: intakeSummary},
		{Key: "object_storage", Label: "Object storage", State: storageState, Summary: storageSummary},
		{Key: "backups", Label: "Backup and PITR", State: backupState, Summary: backupSummary},
		{Key: "integrations", Label: "Integration health", State: ReadinessReadyToVerify,
			Summary: "Review live freshness and delivery evidence.", RemediationHref: "#/operations"},
	}
	return status, nil
}

func providerName(endpoint string) string {
	value := strings.ToLower(strings.TrimSpace(endpoint))
	switch {
	case strings.Contains(value, "amazonaws.com"):
		return "amazon_s3"
	case strings.Contains(value, "minio"):
		return "minio"
	case value != "":
		return "s3_compatible"
	default:
		return ""
	}
}

func (s *Service) UpdateCenterConfiguration(
	ctx context.Context,
	command UpdateCenterConfigurationCommand,
) (CenterStatus, error) {
	target := scope.Target{MSPID: command.Principal.Scope.MSPID}
	if err := authorization.Authorize(
		command.Principal,
		"organization.manage",
		target,
	); err != nil {
		return CenterStatus{}, err
	}
	reason, source := strings.TrimSpace(command.Reason), strings.TrimSpace(command.Source)
	if s.repository == nil || s.newID == nil ||
		command.ExpectedVersion < 1 || reason == "" || source == "" ||
		!validCenterReferences(command.Intake) ||
		!validCenterReferences(command.ObjectStorage) ||
		!validCenterReferences(command.Backups) {
		return CenterStatus{}, ErrInvalidCenterConfiguration
	}
	at, correlationID := s.now().UTC(), s.newID()
	action := "installation.setup.configuration.updated"
	mutation := CenterConfigurationMutation{
		ExpectedVersion: command.ExpectedVersion,
		Intake:          command.Intake,
		ObjectStorage:   command.ObjectStorage,
		Backups:         command.Backups,
		Audit: mutation.AuditRecord{
			ID: s.newID(), OccurredAt: at, MSPID: target.MSPID,
			ActorType: "technician", ActorID: command.Principal.ID,
			Action: action, SubjectType: "installation", SubjectID: target.MSPID,
			SubjectVersion: command.ExpectedVersion + 1,
			Source:         source, Reason: reason, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: s.newID(), EventType: action, SchemaVersion: 1,
			OccurredAt: at, MSPID: target.MSPID,
			ActorType: "technician", ActorID: command.Principal.ID,
			SubjectType: "installation", SubjectID: target.MSPID,
			SubjectVersion: command.ExpectedVersion + 1,
			Source:         source, CorrelationID: correlationID,
		},
	}
	return s.repository.UpdateCenterConfiguration(ctx, mutation)
}

func validCenterReferences(value map[string]any) bool {
	if value == nil {
		return false
	}
	encoded, err := json.Marshal(value)
	return err == nil && len(encoded) <= 64*1024 &&
		validReferenceValue("", value)
}

func validReferenceValue(key string, value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for childKey, childValue := range typed {
			if !validReferenceValue(childKey, childValue) {
				return false
			}
		}
	case []any:
		for _, childValue := range typed {
			if !validReferenceValue(key, childValue) {
				return false
			}
		}
	case string:
		if !sensitiveReferenceKey(key) || typed == "" {
			return true
		}
		for _, prefix := range []string{
			"env://",
			"file://",
			"secret://",
			"vault://",
		} {
			if strings.HasPrefix(typed, prefix) && len(typed) > len(prefix) {
				return true
			}
		}
		return false
	}
	return true
}

func sensitiveReferenceKey(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer(
		"-", "_",
		".", "_",
		" ", "_",
	).Replace(strings.TrimSpace(key)))
	for _, marker := range []string{
		"password",
		"secret",
		"token",
		"credential",
		"private_key",
		"access_key",
		"api_key",
	} {
		if normalized == marker ||
			strings.HasSuffix(normalized, "_"+marker) ||
			strings.Contains(normalized, "_"+marker+"_ref") {
			return true
		}
	}
	return false
}

func (s *Service) Bootstrap(ctx context.Context, token string, value Configuration) error {
	token = strings.TrimSpace(token)
	if s.repository == nil || s.newID == nil ||
		(value.EntraConfigured() && s.secrets == nil) ||
		token == "" || !valid(value) ||
		value.OrganizationID != s.expectedMSPID {
		return ErrInvalidBootstrap
	}
	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(value.RecoveryPassword), localAdminBcryptCost,
	)
	if err != nil {
		return ErrInvalidBootstrap
	}
	var entraSecret secrets.SealedValue
	if value.EntraConfigured() {
		entraSecret, err = s.secrets.Seal(
			ctx, "installation.entra.client_secret", []byte(value.EntraClientSecret),
		)
		if err != nil {
			return err
		}
	}
	value.RecoveryPassword = ""
	value.EntraClientSecret = ""
	err = s.repository.Bootstrap(ctx, BootstrapMutation{
		TokenHash: sha256.Sum256([]byte(token)), PasswordHash: string(passwordHash),
		EntraSecret:   entraSecret,
		Configuration: value, OccurredAt: s.now().UTC(),
		MSPID: value.OrganizationID, TechnicianID: s.newID(), IdentityID: s.newID(),
		RoleID: s.newID(), AssignmentID: s.newID(), RecoveryID: s.newID(),
		AuditID: s.newID(), EventID: s.newID(), CorrelationID: s.newID(),
	})
	if err != nil {
		return err
	}
	return nil
}

func (value Configuration) EntraConfigured() bool {
	return strings.TrimSpace(value.EntraTenantID) != "" ||
		strings.TrimSpace(value.EntraClientID) != "" ||
		strings.TrimSpace(value.EntraClientSecret) != "" ||
		strings.TrimSpace(value.EntraRedirectURL) != "" ||
		strings.TrimSpace(value.AdminEntraSubject) != ""
}

func valid(value Configuration) bool {
	return uuidPattern.MatchString(strings.TrimSpace(value.OrganizationID)) &&
		strings.TrimSpace(value.OrganizationName) != "" &&
		strings.TrimSpace(value.OrganizationDisplay) != "" &&
		validEntra(value) &&
		strings.TrimSpace(value.AdminEmail) != "" &&
		strings.TrimSpace(value.AdminDisplayName) != "" &&
		strings.TrimSpace(value.RecoveryUsername) != "" &&
		len(value.RecoveryPassword) >= 16 && len(value.RecoveryPassword) <= 72 &&
		validCIDRs(value.RecoveryAllowedCIDR) &&
		validCenterReferences(value.Intake) &&
		validCenterReferences(value.ObjectStorage) &&
		validCenterReferences(value.Backups)
}

func validEntra(value Configuration) bool {
	if !value.EntraConfigured() {
		return true
	}
	redirect, err := url.Parse(strings.TrimSpace(value.EntraRedirectURL))
	return strings.TrimSpace(value.EntraTenantID) != "" &&
		strings.TrimSpace(value.EntraClientID) != "" &&
		len(strings.TrimSpace(value.EntraClientSecret)) >= 16 &&
		strings.TrimSpace(value.AdminEntraSubject) != "" &&
		err == nil && redirect.IsAbs() && redirect.Scheme == "https" &&
		redirect.Host != "" && redirect.User == nil
}

var uuidPattern = regexp.MustCompile(
	`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

func validCIDRs(values []string) bool {
	if len(values) == 0 || len(values) > 16 {
		return false
	}
	for _, value := range values {
		if _, _, err := net.ParseCIDR(strings.TrimSpace(value)); err != nil {
			return false
		}
	}
	return true
}

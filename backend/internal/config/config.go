package config

import (
	"encoding/hex"
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
)

type Config struct {
	Environment          string
	HTTPAddr             string
	DatabaseURL          string
	ValkeyURL            string
	S3Endpoint           string
	S3Bucket             string
	S3Region             string
	S3AccessKey          string
	S3SecretKey          string
	SessionKey           string
	SecretKey            []byte
	BackupEvidenceKey    []byte
	BuildRevision        string
	PublicURL            string
	GraphNotificationURL string
	MSPID                string
	EntraMSPID           string
	EntraTenantID        string
	EntraClientID        string
	EntraClientSecret    string
	EntraRedirectURL     string
	EntraDefaultRoleKey  string
	SMTPHost             string
	SMTPPort             int
	SMTPUsername         string
	SMTPPassword         string
	SMTPFrom             string
}

func Load(lookup func(string) string) (Config, error) {
	protectedSecretKey := lookup("RARITY_SECRET_KEY")
	backupEvidenceKey := lookup("RARITY_BACKUP_EVIDENCE_KEY")
	cfg := Config{
		Environment:         lookup("RARITY_ENV"),
		HTTPAddr:            lookup("RARITY_HTTP_ADDR"),
		DatabaseURL:         lookup("DATABASE_URL"),
		ValkeyURL:           lookup("VALKEY_URL"),
		S3Endpoint:          lookup("S3_ENDPOINT"),
		S3Bucket:            lookup("S3_BUCKET"),
		S3Region:            lookup("S3_REGION"),
		S3AccessKey:         lookup("S3_ACCESS_KEY_ID"),
		S3SecretKey:         lookup("S3_SECRET_ACCESS_KEY"),
		SessionKey:          lookup("RARITY_SESSION_KEY"),
		BuildRevision:       lookup("RARITY_BUILD_REVISION"),
		PublicURL:           strings.TrimSpace(lookup("RARITY_PUBLIC_URL")),
		BackupEvidenceKey:   []byte(backupEvidenceKey),
		MSPID:               strings.TrimSpace(lookup("RARITY_MSP_ID")),
		EntraMSPID:          strings.TrimSpace(lookup("RARITY_ENTRA_MSP_ID")),
		EntraTenantID:       strings.TrimSpace(lookup("RARITY_ENTRA_TENANT_ID")),
		EntraClientID:       strings.TrimSpace(lookup("RARITY_ENTRA_CLIENT_ID")),
		EntraClientSecret:   lookup("RARITY_ENTRA_CLIENT_SECRET"),
		EntraRedirectURL:    strings.TrimSpace(lookup("RARITY_ENTRA_REDIRECT_URL")),
		EntraDefaultRoleKey: strings.TrimSpace(lookup("RARITY_ENTRA_DEFAULT_ROLE_KEY")),
		GraphNotificationURL: strings.TrimSpace(
			lookup("RARITY_GRAPH_NOTIFICATION_URL"),
		),
		SMTPHost:     strings.TrimSpace(lookup("RARITY_SMTP_HOST")),
		SMTPUsername: strings.TrimSpace(lookup("RARITY_SMTP_USERNAME")),
		SMTPPassword: lookup("RARITY_SMTP_PASSWORD"),
		SMTPFrom:     strings.TrimSpace(lookup("RARITY_SMTP_FROM")),
	}
	if cfg.MSPID == "" {
		cfg.MSPID = cfg.EntraMSPID
	}

	required := []struct {
		name  string
		value string
	}{
		{"RARITY_ENV", cfg.Environment},
		{"RARITY_HTTP_ADDR", cfg.HTTPAddr},
		{"DATABASE_URL", cfg.DatabaseURL},
		{"VALKEY_URL", cfg.ValkeyURL},
		{"S3_ENDPOINT", cfg.S3Endpoint},
		{"S3_BUCKET", cfg.S3Bucket},
		{"S3_REGION", cfg.S3Region},
		{"S3_ACCESS_KEY_ID", cfg.S3AccessKey},
		{"S3_SECRET_ACCESS_KEY", cfg.S3SecretKey},
		{"RARITY_SESSION_KEY", cfg.SessionKey},
		{"RARITY_SECRET_KEY", protectedSecretKey},
		{"RARITY_BUILD_REVISION", cfg.BuildRevision},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return Config{}, fmt.Errorf("%s is required", field.name)
		}
	}
	secretKey, err := hex.DecodeString(strings.TrimSpace(protectedSecretKey))
	if err != nil || len(secretKey) != 32 {
		return Config{}, fmt.Errorf("RARITY_SECRET_KEY must be a 32-byte hexadecimal key")
	}
	cfg.SecretKey = secretKey
	if len(cfg.BackupEvidenceKey) > 0 && len(cfg.BackupEvidenceKey) < 32 {
		return Config{}, fmt.Errorf(
			"RARITY_BACKUP_EVIDENCE_KEY must contain at least 32 characters",
		)
	}
	if err := validateEntraConfig(cfg); err != nil {
		return Config{}, err
	}
	if cfg.GraphNotificationURL != "" {
		notificationURL, err := url.Parse(cfg.GraphNotificationURL)
		if err != nil || !notificationURL.IsAbs() ||
			notificationURL.Scheme != "https" ||
			notificationURL.Host == "" || notificationURL.User != nil ||
			notificationURL.Fragment != "" {
			return Config{}, fmt.Errorf(
				"RARITY_GRAPH_NOTIFICATION_URL must be an absolute https URL",
			)
		}
	}
	if err := validatePublicURL(cfg.PublicURL, cfg.Environment); err != nil {
		return Config{}, err
	}
	port := strings.TrimSpace(lookup("RARITY_SMTP_PORT"))
	smtpValues := []string{cfg.SMTPHost, port, cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPFrom}
	configured := 0
	for _, value := range smtpValues {
		if strings.TrimSpace(value) != "" {
			configured++
		}
	}
	if configured != 0 && configured != len(smtpValues) {
		return Config{}, fmt.Errorf("SMTP configuration must be complete when enabled")
	}
	if configured == len(smtpValues) {
		parsedPort, err := strconv.Atoi(port)
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			return Config{}, fmt.Errorf("RARITY_SMTP_PORT must be a valid TCP port")
		}
		from, err := mail.ParseAddress(cfg.SMTPFrom)
		if err != nil || from.Address != cfg.SMTPFrom || strings.ContainsAny(cfg.SMTPHost, " \t\r\n") {
			return Config{}, fmt.Errorf("SMTP configuration contains an invalid host or sender")
		}
		cfg.SMTPPort = parsedPort
	}

	switch cfg.Environment {
	case "local", "demo", "test":
		return cfg, nil
	case "pilot", "production":
		if err := validateDeployedConfig(cfg); err != nil {
			return Config{}, err
		}
		return cfg, nil
	default:
		return Config{}, fmt.Errorf("RARITY_ENV must be local, demo, test, pilot, or production")
	}
}

func validatePublicURL(rawURL, environment string) error {
	if rawURL == "" {
		return nil
	}
	publicURL, err := url.Parse(rawURL)
	if err != nil || !publicURL.IsAbs() || publicURL.Host == "" ||
		publicURL.User != nil || publicURL.RawQuery != "" ||
		publicURL.Fragment != "" ||
		(publicURL.Scheme != "https" && publicURL.Scheme != "http") {
		return fmt.Errorf(
			"RARITY_PUBLIC_URL must be an absolute HTTP(S) URL without credentials, query, or fragment",
		)
	}
	if publicURL.Scheme == "https" {
		return nil
	}
	if environment != "local" && environment != "test" && environment != "demo" {
		return fmt.Errorf("RARITY_PUBLIC_URL must use https in deployed environments")
	}
	switch strings.ToLower(publicURL.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
		return nil
	default:
		return fmt.Errorf("RARITY_PUBLIC_URL may use http only on loopback hosts")
	}
}

func validateEntraConfig(cfg Config) error {
	values := []string{
		cfg.EntraMSPID, cfg.EntraTenantID, cfg.EntraClientID,
		cfg.EntraClientSecret, cfg.EntraRedirectURL, cfg.EntraDefaultRoleKey,
	}
	configured := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			configured++
		}
	}
	if configured == 0 {
		if (cfg.Environment == "pilot" || cfg.Environment == "production") &&
			cfg.MSPID == "" {
			return fmt.Errorf("RARITY_MSP_ID is required before first-run setup")
		}
		return nil
	}
	if configured != len(values) {
		return fmt.Errorf("Entra configuration must be complete when enabled")
	}
	redirect, err := url.Parse(cfg.EntraRedirectURL)
	if err != nil || !redirect.IsAbs() || redirect.Host == "" ||
		redirect.User != nil || redirect.RawQuery != "" || redirect.Fragment != "" {
		return fmt.Errorf("RARITY_ENTRA_REDIRECT_URL must be an absolute callback URL")
	}
	if redirect.Scheme != "https" &&
		!(redirect.Scheme == "http" &&
			(redirect.Hostname() == "localhost" || redirect.Hostname() == "127.0.0.1")) {
		return fmt.Errorf("RARITY_ENTRA_REDIRECT_URL must use https except on localhost")
	}
	return nil
}

func validateDeployedConfig(cfg Config) error {
	if len(strings.TrimSpace(cfg.SessionKey)) < 32 {
		return fmt.Errorf("RARITY_SESSION_KEY must contain at least 32 characters in deployed environments")
	}
	if err := requirePostgresTLS(cfg.DatabaseURL); err != nil {
		return fmt.Errorf("DATABASE_URL must require verified TLS in deployed environments")
	}
	if err := requireScheme(cfg.ValkeyURL, "rediss"); err != nil {
		return fmt.Errorf("VALKEY_URL must use rediss in deployed environments")
	}
	if err := requireScheme(cfg.S3Endpoint, "https"); err != nil {
		return fmt.Errorf("S3_ENDPOINT must use https in deployed environments")
	}
	if len(strings.TrimSpace(cfg.S3SecretKey)) < 16 {
		return fmt.Errorf("S3_SECRET_ACCESS_KEY must contain at least 16 characters in deployed environments")
	}
	return nil
}

func requirePostgresTLS(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return fmt.Errorf("invalid postgres URL")
	}
	switch parsed.Query().Get("sslmode") {
	case "verify-ca", "verify-full":
		return nil
	default:
		return fmt.Errorf("verified TLS is required")
	}
}

func requireScheme(rawURL, expected string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != expected || parsed.Host == "" {
		return fmt.Errorf("invalid URL scheme")
	}
	return nil
}

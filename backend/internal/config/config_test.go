package config

import (
	"strings"
	"testing"
)

func TestLoadRejectsMissingDatabaseURLWithoutEchoingValues(t *testing.T) {
	lookup := mapLookup(map[string]string{
		"RARITY_ENV":            "local",
		"RARITY_HTTP_ADDR":      ":8080",
		"RARITY_SESSION_KEY":    "test-session-key",
		"RARITY_SECRET_KEY":     strings.Repeat("1", 64),
		"VALKEY_URL":            "redis://valkey:6379/0",
		"S3_ENDPOINT":           "http://minio:9000",
		"S3_BUCKET":             "rarity-attachments",
		"S3_REGION":             "us-east-1",
		"S3_ACCESS_KEY_ID":      "attachment-writer",
		"S3_SECRET_ACCESS_KEY":  "synthetic-secret",
		"RARITY_BUILD_REVISION": "test-revision",
	})

	_, err := Load(lookup)
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected DATABASE_URL validation error, got %v", err)
	}
	if strings.Contains(err.Error(), "test-session-key") {
		t.Fatalf("validation error leaked a supplied secret: %v", err)
	}
}

func TestLoadReturnsTypedConfiguration(t *testing.T) {
	values := map[string]string{
		"RARITY_ENV":                    "demo",
		"RARITY_HTTP_ADDR":              ":8080",
		"RARITY_SESSION_KEY":            "test-session-key",
		"RARITY_SECRET_KEY":             strings.Repeat("1", 64),
		"DATABASE_URL":                  "postgres://rarity:password@postgres:5432/rarity?sslmode=disable",
		"VALKEY_URL":                    "redis://valkey:6379/0",
		"S3_ENDPOINT":                   "http://minio:9000",
		"S3_BUCKET":                     "rarity-attachments",
		"S3_REGION":                     "us-east-1",
		"S3_ACCESS_KEY_ID":              "attachment-writer",
		"S3_SECRET_ACCESS_KEY":          "synthetic-secret",
		"RARITY_BUILD_REVISION":         "abc123",
		"RARITY_GRAPH_NOTIFICATION_URL": "https://rarity.example/api/v1/graph/notifications",
	}

	cfg, err := Load(mapLookup(values))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Environment != "demo" || cfg.BuildRevision != "abc123" ||
		cfg.GraphNotificationURL !=
			"https://rarity.example/api/v1/graph/notifications" ||
		len(cfg.SecretKey) != 32 {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestLoadRejectsInvalidGraphNotificationURL(t *testing.T) {
	values := validConfigValues()
	values["RARITY_GRAPH_NOTIFICATION_URL"] =
		"http://rarity.example/api/v1/graph/notifications"

	_, err := Load(mapLookup(values))
	if err == nil ||
		!strings.Contains(err.Error(), "RARITY_GRAPH_NOTIFICATION_URL") {
		t.Fatalf("expected Graph notification URL validation error, got %v", err)
	}
}

func TestLoadRejectsWeakBackupEvidenceKey(t *testing.T) {
	values := validConfigValues()
	values["RARITY_BACKUP_EVIDENCE_KEY"] = "too-short"
	_, err := Load(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), "RARITY_BACKUP_EVIDENCE_KEY") {
		t.Fatalf("expected backup evidence key validation error, got %v", err)
	}
}

func TestLoadAcceptsSecurePilotConfiguration(t *testing.T) {
	values := validConfigValues()
	values["RARITY_ENV"] = "pilot"
	values["DATABASE_URL"] = "postgres://rarity:password@postgres:5432/rarity?sslmode=verify-full"
	values["VALKEY_URL"] = "rediss://valkey:6379/0"
	values["S3_ENDPOINT"] = "https://minio:9000"

	cfg, err := Load(mapLookup(values))
	if err != nil {
		t.Fatalf("load secure pilot config: %v", err)
	}
	if cfg.Environment != "pilot" {
		t.Fatalf("expected pilot environment, got %q", cfg.Environment)
	}
}

func TestLoadRejectsInsecureDeployedTransports(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "database TLS disabled", key: "DATABASE_URL", value: "postgres://rarity:password@postgres:5432/rarity?sslmode=disable"},
		{name: "plaintext Valkey", key: "VALKEY_URL", value: "redis://valkey:6379/0"},
		{name: "plaintext object storage", key: "S3_ENDPOINT", value: "http://minio:9000"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := validConfigValues()
			values["RARITY_ENV"] = "production"
			values[test.key] = test.value

			_, err := Load(mapLookup(values))
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("expected %s validation error, got %v", test.key, err)
			}
			if strings.Contains(err.Error(), test.value) {
				t.Fatalf("validation error leaked supplied value: %v", err)
			}
		})
	}
}

func TestLoadRejectsWeakDeployedSessionKey(t *testing.T) {
	values := validConfigValues()
	values["RARITY_ENV"] = "pilot"
	values["RARITY_SESSION_KEY"] = "too-short"

	_, err := Load(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), "RARITY_SESSION_KEY") {
		t.Fatalf("expected session-key validation error, got %v", err)
	}
}

func TestLoadRejectsInvalidProtectedSecretKeyWithoutEchoingIt(t *testing.T) {
	values := validConfigValues()
	values["RARITY_SECRET_KEY"] = "not-a-32-byte-hex-key"

	_, err := Load(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), "RARITY_SECRET_KEY") {
		t.Fatalf("expected protected-secret-key validation error, got %v", err)
	}
	if strings.Contains(err.Error(), values["RARITY_SECRET_KEY"]) {
		t.Fatalf("validation error leaked supplied secret: %v", err)
	}
}

func TestLoadKeepsLocalDevelopmentTransportCompatible(t *testing.T) {
	values := validConfigValues()
	values["RARITY_ENV"] = "local"
	values["DATABASE_URL"] = "postgres://rarity:password@postgres:5432/rarity?sslmode=disable"
	values["VALKEY_URL"] = "redis://valkey:6379/0"
	values["S3_ENDPOINT"] = "http://minio:9000"

	if _, err := Load(mapLookup(values)); err != nil {
		t.Fatalf("load local config: %v", err)
	}
}

func TestLoadRequiresCompleteSMTPConfiguration(t *testing.T) {
	values := validConfigValues()
	values["RARITY_SMTP_HOST"] = "smtp.example.test"
	values["RARITY_SMTP_PASSWORD"] = "super-secret"
	_, err := Load(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), "SMTP configuration") || strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("partial SMTP error=%v", err)
	}
}

func TestLoadAcceptsCompleteSMTPConfiguration(t *testing.T) {
	values := validConfigValues()
	values["RARITY_SMTP_HOST"] = "smtp.example.test"
	values["RARITY_SMTP_PORT"] = "587"
	values["RARITY_SMTP_USERNAME"] = "mailer"
	values["RARITY_SMTP_PASSWORD"] = "super-secret"
	values["RARITY_SMTP_FROM"] = "rarity@example.test"
	cfg, err := Load(mapLookup(values))
	if err != nil || cfg.SMTPHost != "smtp.example.test" || cfg.SMTPPort != 587 || cfg.SMTPPassword != "super-secret" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestLoadUsesProviderAgnosticMSPIdentity(t *testing.T) {
	values := validConfigValues()
	values["RARITY_ENV"] = "local"
	values["RARITY_MSP_ID"] = "22222222-2222-4222-8222-222222222222"

	cfg, err := Load(mapLookup(values))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MSPID != values["RARITY_MSP_ID"] {
		t.Fatalf("MSP identity was not loaded independently of Entra: %+v", cfg)
	}
}

func TestLoadAllowsSecureDeployedBootstrapBeforeEntraExists(t *testing.T) {
	values := validConfigValues()
	values["RARITY_MSP_ID"] = "22222222-2222-4222-8222-222222222222"
	values["RARITY_PUBLIC_URL"] = "https://rarity.example/support/rti"
	for _, key := range []string{
		"RARITY_ENTRA_MSP_ID", "RARITY_ENTRA_TENANT_ID",
		"RARITY_ENTRA_CLIENT_ID", "RARITY_ENTRA_CLIENT_SECRET",
		"RARITY_ENTRA_REDIRECT_URL", "RARITY_ENTRA_DEFAULT_ROLE_KEY",
	} {
		delete(values, key)
	}
	cfg, err := Load(mapLookup(values))
	if err != nil {
		t.Fatalf("Load() bootstrap configuration error=%v", err)
	}
	if cfg.MSPID != values["RARITY_MSP_ID"] || cfg.EntraTenantID != "" {
		t.Fatalf("unexpected bootstrap config: %+v", cfg)
	}
	if cfg.PublicURL != values["RARITY_PUBLIC_URL"] {
		t.Fatalf("public URL=%q want=%q", cfg.PublicURL, values["RARITY_PUBLIC_URL"])
	}
}

func TestLoadRejectsInsecureDeployedPublicURL(t *testing.T) {
	values := validConfigValues()
	values["RARITY_PUBLIC_URL"] = "http://rarity.example"

	_, err := Load(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), "RARITY_PUBLIC_URL") {
		t.Fatalf("expected deployed public URL validation error, got %v", err)
	}
}

func TestLoadAllowsLoopbackHTTPPublicURLOnlyOutsideDeployment(t *testing.T) {
	for _, publicURL := range []string{
		"http://localhost:18080",
		"http://127.0.0.1:18080",
		"http://[::1]:18080",
	} {
		t.Run(publicURL, func(t *testing.T) {
			values := validConfigValues()
			values["RARITY_ENV"] = "local"
			values["RARITY_PUBLIC_URL"] = publicURL

			cfg, err := Load(mapLookup(values))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.PublicURL != publicURL {
				t.Fatalf("public URL=%q want=%q", cfg.PublicURL, publicURL)
			}
		})
	}

	values := validConfigValues()
	values["RARITY_ENV"] = "demo"
	values["RARITY_PUBLIC_URL"] = "http://192.0.2.10:18081"
	_, err := Load(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), "RARITY_PUBLIC_URL") {
		t.Fatalf("expected non-loopback HTTP validation error, got %v", err)
	}
}

func TestLoadRejectsPublicURLWithAmbientRequestData(t *testing.T) {
	for name, publicURL := range map[string]string{
		"user information": "https://operator@rarity.example",
		"query":            "https://rarity.example?source=console",
		"fragment":         "https://rarity.example/#/setup",
	} {
		t.Run(name, func(t *testing.T) {
			values := validConfigValues()
			values["RARITY_PUBLIC_URL"] = publicURL
			_, err := Load(mapLookup(values))
			if err == nil || !strings.Contains(err.Error(), "RARITY_PUBLIC_URL") {
				t.Fatalf("expected public URL validation error, got %v", err)
			}
		})
	}
}

func TestLoadRejectsDeployedBootstrapWithoutMSPAuthority(t *testing.T) {
	values := validConfigValues()
	for _, key := range []string{
		"RARITY_MSP_ID", "RARITY_ENTRA_MSP_ID", "RARITY_ENTRA_TENANT_ID",
		"RARITY_ENTRA_CLIENT_ID", "RARITY_ENTRA_CLIENT_SECRET",
		"RARITY_ENTRA_REDIRECT_URL", "RARITY_ENTRA_DEFAULT_ROLE_KEY",
	} {
		delete(values, key)
	}
	_, err := Load(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), "RARITY_MSP_ID") {
		t.Fatalf("expected MSP authority validation error, got %v", err)
	}
}

func validConfigValues() map[string]string {
	return map[string]string{
		"RARITY_ENV":                    "production",
		"RARITY_HTTP_ADDR":              ":8080",
		"RARITY_SESSION_KEY":            strings.Repeat("a", 64),
		"RARITY_SECRET_KEY":             strings.Repeat("1", 64),
		"DATABASE_URL":                  "postgres://rarity:password@postgres:5432/rarity?sslmode=verify-full",
		"VALKEY_URL":                    "rediss://valkey:6379/0",
		"S3_ENDPOINT":                   "https://minio:9000",
		"S3_BUCKET":                     "rarity-attachments",
		"S3_REGION":                     "us-east-1",
		"S3_ACCESS_KEY_ID":              "attachment-writer",
		"S3_SECRET_ACCESS_KEY":          "synthetic-secret",
		"RARITY_BUILD_REVISION":         "abc123",
		"RARITY_ENTRA_MSP_ID":           "11111111-1111-4111-8111-111111111111",
		"RARITY_ENTRA_TENANT_ID":        "tenant-id",
		"RARITY_ENTRA_CLIENT_ID":        "client-id",
		"RARITY_ENTRA_CLIENT_SECRET":    "synthetic-entra-secret",
		"RARITY_ENTRA_REDIRECT_URL":     "https://rarity.example/auth/entra/callback",
		"RARITY_ENTRA_DEFAULT_ROLE_KEY": "service_desk_technician",
	}
}

func mapLookup(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

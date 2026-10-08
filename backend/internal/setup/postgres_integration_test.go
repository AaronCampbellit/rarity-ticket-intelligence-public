package setup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
)

func TestPostgresBootstrapCreatesSystemCatalogIsAtomicSingleUseAndRuntimeAuthoritative(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL bootstrap verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(pool.Close)
	repository := NewPostgresRepository(pool)
	if completed, _, err := repository.Status(ctx); err != nil {
		t.Fatal(err)
	} else if completed {
		t.Skip("test database already contains an installation bootstrap")
	}
	provider, err := secrets.NewLocalProvider(bytes.Repeat([]byte{7}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	mspID := id.New()
	service := NewService(
		repository, time.Now, id.New,
		WithExpectedMSPID(mspID), WithSecretProvider(provider),
	)
	token, _, err := service.IssueToken(ctx)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	replacementToken, _, err := service.IssueToken(ctx)
	if err != nil {
		t.Fatalf("replace unused token: %v", err)
	}
	if replacementToken == token {
		t.Fatal("replacement token reused prior authority")
	}
	token = replacementToken
	config := validConfiguration()
	config.OrganizationID = mspID
	config.OrganizationDisplay = "TEST-" + mspID[:8]
	config.EntraTenantID = ""
	config.EntraClientID = ""
	config.EntraClientSecret = ""
	config.EntraRedirectURL = ""
	config.AdminEntraSubject = ""
	config.Intake = map[string]any{"mailbox": "support@example.test"}
	config.ObjectStorage = map[string]any{"endpoint_ref": "env://S3_ENDPOINT"}
	config.Backups = map[string]any{"repository_ref": "env://PGBACKREST_REPO"}
	if err := service.Bootstrap(ctx, token, config); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	var systemCatalogCreated bool
	if err := pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM tag_groups
  WHERE msp_id = $1 AND internal_key = 'taxonomy.system' AND system_group
) AND EXISTS (
  SELECT 1 FROM tags
  WHERE msp_id = $1 AND internal_key = 'taxonomy.system.unclassified' AND system_tag
)`, mspID).Scan(&systemCatalogCreated); err != nil || !systemCatalogCreated {
		t.Fatalf("system catalog created=%v error=%v", systemCatalogCreated, err)
	}
	var classificationCapabilityCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM role_capabilities capability
JOIN roles role ON role.id = capability.role_id AND role.msp_id = capability.msp_id
WHERE role.msp_id = $1 AND role.key = 'global-admin'
  AND capability.capability = ANY($2::text[])
`, mspID, []string{
		"classification.manage", "classification.apply",
		"classification.report", "classification.ai.manage",
	}).Scan(&classificationCapabilityCount); err != nil || classificationCapabilityCount != 4 {
		t.Fatalf("fresh bootstrap classification capabilities=%d error=%v", classificationCapabilityCount, err)
	}
	if err := service.Bootstrap(ctx, token, config); !errors.Is(err, ErrBootstrapClosed) {
		t.Fatalf("reused bootstrap token error=%v", err)
	}
	runtime, found, err := repository.RuntimeConfiguration(ctx, provider)
	if err != nil || !found {
		t.Fatalf("runtime config found=%v err=%v", found, err)
	}
	if runtime.MSPID != mspID || runtime.EntraConfigured ||
		runtime.EntraClientSecret != "" {
		t.Fatalf("runtime configuration mismatch: %+v", runtime)
	}
	for _, kind := range []string{"location", "contact", "asset", "service", "contract"} {
		for _, action := range []string{"update", "lifecycle"} {
			capability := kind + "." + action
			var present bool
			if err := pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM roles
  JOIN role_capabilities ON role_capabilities.role_id = roles.id
    AND role_capabilities.msp_id = roles.msp_id
  WHERE roles.msp_id = $1 AND roles.key = 'global-admin'
    AND role_capabilities.capability = $2
)`, mspID, capability).Scan(&present); err != nil {
				t.Fatalf("read fresh bootstrap capability %s: %v", capability, err)
			}
			if !present {
				t.Errorf("fresh bootstrap global administrator missing %s", capability)
			}
		}
	}
}

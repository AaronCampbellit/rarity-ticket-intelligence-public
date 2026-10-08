package localadminrecovery

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func TestPostgresResetPasswordIsAtomicAndAudited(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL recovery verification")
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

	mspID, technicianID, accountID, sessionID := id.New(), id.New(), id.New(), id.New()
	displayID := "RECOVERY-" + mspID[:8]
	username := "recovery-" + accountID[:8]
	oldHash, err := bcrypt.GenerateFromPassword([]byte("old-password-value"), 12)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(statement string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, statement, args...); err != nil {
			t.Fatalf("seed recovery fixture: %v", err)
		}
	}
	exec(`INSERT INTO msp_organizations
		(id, display_id, name, created_by, updated_by)
		VALUES ($1,$2,'Recovery Test',$3,$3)`, mspID, displayID, technicianID)
	exec(`INSERT INTO technicians
		(id, msp_id, email, display_name)
		VALUES ($1,$2,$3,'Recovery Test')`,
		technicianID, mspID, username+"@example.invalid")
	exec(`INSERT INTO break_glass_accounts
		(id, msp_id, technician_id, username, password_hash, allowed_cidrs, created_by)
		VALUES ($1,$2,$3,$4,$5,ARRAY['127.0.0.1/32'::cidr],$3)`,
		accountID, mspID, technicianID, username, string(oldHash))
	tokenHash := sha256.Sum256([]byte(sessionID))
	now := time.Now().UTC()
	exec(`INSERT INTO sessions
		(id, msp_id, technician_id, token_hash, created_at, last_seen_at,
		 idle_expires_at, idle_timeout_seconds, expires_at, rotated_at)
		VALUES ($1,$2,$3,$4,$5,$5,$6,1800,$7,$5)`,
		sessionID, mspID, technicianID, tokenHash[:], now,
		now.Add(30*time.Minute), now.Add(8*time.Hour))

	password := []byte("new recovery password value")
	result, err := NewService(NewPostgresRepository(pool), func() time.Time { return now }, id.New).
		ResetPassword(ctx, Command{
			MSPDisplayID: displayID,
			Username:     username,
			Password:     password,
			Reason:       "integration recovery test",
		})
	if err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if result.Version != 2 || result.SessionsRevoked != 1 {
		t.Fatalf("result = %+v", result)
	}

	var storedHash string
	var version int64
	if err := pool.QueryRow(ctx, `
SELECT password_hash, version FROM break_glass_accounts WHERE id = $1
`, accountID).Scan(&storedHash, &version); err != nil {
		t.Fatal(err)
	}
	if version != 2 ||
		bcrypt.CompareHashAndPassword([]byte(storedHash), []byte("new recovery password value")) != nil {
		t.Fatal("new password was not persisted")
	}
	var revokedAt *time.Time
	var revokedBy string
	if err := pool.QueryRow(ctx, `
SELECT revoked_at, revoked_by::text FROM sessions WHERE id = $1
`, sessionID).Scan(&revokedAt, &revokedBy); err != nil {
		t.Fatal(err)
	}
	if revokedAt == nil || revokedBy != SystemActorID {
		t.Fatalf("session revocation = %v by %q", revokedAt, revokedBy)
	}
	var auditReason, auditActor string
	var safeDiff []byte
	if err := pool.QueryRow(ctx, `
SELECT reason, actor_type, safe_diff FROM audit_ledger
WHERE msp_id = $1 AND subject_id = $2 AND action = $3
`, mspID, accountID, PasswordResetAction).Scan(&auditReason, &auditActor, &safeDiff); err != nil {
		t.Fatal(err)
	}
	var facts map[string]any
	if err := json.Unmarshal(safeDiff, &facts); err != nil {
		t.Fatal(err)
	}
	if auditReason != "integration recovery test" || auditActor != "system" ||
		facts["recovery"] != true || facts["sessions_revoked"] != float64(1) {
		t.Fatalf("audit facts reason=%q actor=%q facts=%v", auditReason, auditActor, facts)
	}
	var outboxCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM event_outbox
WHERE msp_id = $1 AND subject_id = $2 AND event_type = $3
`, mspID, accountID, PasswordResetAction).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox count = %d", outboxCount)
	}
}

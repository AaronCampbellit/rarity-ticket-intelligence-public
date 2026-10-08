package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/setup"
)

type pgBackRestEvidence struct {
	Stanza         string    `json:"stanza"`
	Repository     string    `json:"repository"`
	LatestBackupAt time.Time `json:"latest_backup_at"`
	LatestWALAt    time.Time `json:"latest_wal_at"`
}

type restoreEvidence struct {
	RestoreVerifiedAt time.Time `json:"restore_verified_at"`
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, http.DefaultClient); err != nil {
		fmt.Fprintln(os.Stderr, "backup evidence failed:", err)
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	args []string,
	lookup func(string) string,
	client *http.Client,
) error {
	flags := flag.NewFlagSet("rarity-backup-evidence", flag.ContinueOnError)
	pgPath := flags.String("pgbackrest-evidence", "", "normalized pgBackRest evidence file")
	legacyPGPath := flags.String("pgbackrest-json", "", "deprecated alias for --pgbackrest-evidence")
	restorePath := flags.String("restore-evidence", "", "restore verification JSON")
	mspID := flags.String("msp-id", "", "Rarity MSP UUID")
	apiURL := flags.String("api-url", "", "Rarity public URL")
	version := flags.Int64("expected-version", 0, "Setup Center version")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *pgPath == "" {
		*pgPath = *legacyPGPath
	}
	key := []byte(lookup("RARITY_BACKUP_EVIDENCE_KEY"))
	if len(key) < 32 || *pgPath == "" || *restorePath == "" ||
		*mspID == "" || *apiURL == "" || *version < 1 {
		return errors.New("required evidence inputs are missing")
	}
	target, err := url.Parse(strings.TrimRight(*apiURL, "/"))
	if err != nil || target.Host == "" ||
		(target.Scheme != "https" && target.Hostname() != "localhost") {
		return errors.New("api-url must use HTTPS")
	}
	var pg pgBackRestEvidence
	if err := readJSON(*pgPath, &pg); err != nil {
		return err
	}
	var restore restoreEvidence
	if err := readJSON(*restorePath, &restore); err != nil {
		return err
	}
	nonceBytes := make([]byte, 24)
	if _, err := rand.Read(nonceBytes); err != nil {
		return err
	}
	payload := setup.BackupEvidencePayload{
		MSPID: *mspID, Nonce: base64.RawURLEncoding.EncodeToString(nonceBytes),
		IssuedAt: time.Now().UTC(), Stanza: pg.Stanza,
		Repository: pg.Repository, LatestBackupAt: pg.LatestBackupAt,
		LatestWALAt:       pg.LatestWALAt,
		RestoreVerifiedAt: restore.RestoreVerifiedAt,
	}
	signature, err := setup.SignBackupEvidence(payload, key)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{
		"expected_version": *version,
		"payload":          payload,
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(*apiURL, "/")+"/api/v1/setup/center/backups/evidence",
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "BackupEvidence "+signature)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %s", response.Status)
	}
	fmt.Println("Backup and PITR evidence accepted.")
	return nil
}

func readJSON(path string, destination any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, destination); err != nil {
		return errors.New("evidence JSON is invalid")
	}
	return nil
}

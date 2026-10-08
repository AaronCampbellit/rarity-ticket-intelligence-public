package haconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderWritesCompleteOwnerOnlyConfiguration(t *testing.T) {
	templateDir := t.TempDir()
	outputDir := filepath.Join(t.TempDir(), "rendered")
	for name, content := range map[string]string{
		"patroni.yaml.tmpl": "name: ${RARITY_NODE_NAME}\npassword: ${POSTGRES_ADMIN_PASSWORD}\n",
		"etcd.env.tmpl":     "ETCD_NAME=${RARITY_NODE_NAME}\nTOKEN=${ETCD_INITIAL_CLUSTER_TOKEN}\n",
		"haproxy.cfg.tmpl":  "server node ${RARITY_NODE_IP}:5433\n",
	} {
		if err := os.WriteFile(filepath.Join(templateDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write template: %v", err)
		}
	}

	if err := Render(templateDir, outputDir, validValues()); err != nil {
		t.Fatalf("render HA configuration: %v", err)
	}

	for _, name := range []string{"patroni.yaml", "etcd.env", "haproxy.cfg"} {
		info, err := os.Stat(filepath.Join(outputDir, name))
		if err != nil {
			t.Fatalf("stat rendered file: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s permissions = %o, want 600", name, info.Mode().Perm())
		}
	}
}

func TestRenderRejectsMissingValueWithoutWritingSecrets(t *testing.T) {
	templateDir := t.TempDir()
	outputDir := filepath.Join(t.TempDir(), "rendered")
	if err := os.WriteFile(
		filepath.Join(templateDir, "patroni.yaml.tmpl"),
		[]byte("password: ${POSTGRES_ADMIN_PASSWORD}\nmissing: ${ABSENT}\n"),
		0o600,
	); err != nil {
		t.Fatalf("write template: %v", err)
	}
	for _, name := range []string{"etcd.env.tmpl", "haproxy.cfg.tmpl"} {
		if err := os.WriteFile(filepath.Join(templateDir, name), []byte("ok\n"), 0o600); err != nil {
			t.Fatalf("write template: %v", err)
		}
	}

	values := validValues()
	err := Render(templateDir, outputDir, values)
	if err == nil || !strings.Contains(err.Error(), "ABSENT") {
		t.Fatalf("expected missing-value error, got %v", err)
	}
	if strings.Contains(err.Error(), values["POSTGRES_ADMIN_PASSWORD"]) {
		t.Fatalf("error leaked secret: %v", err)
	}
	if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
		t.Fatalf("renderer must not leave partial output, stat error = %v", statErr)
	}
}

func TestRenderRejectsInvalidNodeIdentity(t *testing.T) {
	values := validValues()
	values["RARITY_NODE_NAME"] = "rti-db-4"

	err := Render(t.TempDir(), filepath.Join(t.TempDir(), "rendered"), values)
	if err == nil || !strings.Contains(err.Error(), "RARITY_NODE_NAME") {
		t.Fatalf("expected node-name validation error, got %v", err)
	}
}

func validValues() map[string]string {
	return map[string]string{
		"RARITY_NODE_NAME":              "rti-db-1",
		"RARITY_NODE_IP":                "10.20.0.11",
		"RARITY_NODE_1_IP":              "10.20.0.11",
		"RARITY_NODE_2_IP":              "10.20.0.12",
		"RARITY_NODE_3_IP":              "10.20.0.13",
		"RARITY_CLUSTER_CIDR":           "10.20.0.0/24",
		"RARITY_CLUSTER_NAME":           "rarity-production",
		"ETCD_INITIAL_CLUSTER_STATE":    "new",
		"ETCD_INITIAL_CLUSTER_TOKEN":    strings.Repeat("t", 32),
		"PGBACKREST_STANZA":             "rarity",
		"POSTGRES_ADMIN_USER":           "rarity_admin",
		"POSTGRES_ADMIN_PASSWORD":       strings.Repeat("a", 32),
		"POSTGRES_REPLICATION_USER":     "rarity_replication",
		"POSTGRES_REPLICATION_PASSWORD": strings.Repeat("r", 32),
		"POSTGRES_REWIND_USER":          "rarity_rewind",
		"POSTGRES_REWIND_PASSWORD":      strings.Repeat("w", 32),
		"POSTGRES_SUPERUSER":            "postgres",
		"POSTGRES_SUPERUSER_PASSWORD":   strings.Repeat("s", 32),
	}
}

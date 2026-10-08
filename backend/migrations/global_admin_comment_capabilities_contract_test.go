package migrations

import (
	"strings"
	"testing"
)

func TestGlobalAdministratorCommentCapabilitiesMigration(t *testing.T) {
	sql, err := FS.ReadFile("000070_global_admin_comment_capabilities.sql")
	if err != nil {
		t.Fatalf("read global administrator comment capabilities migration: %v", err)
	}
	body := string(sql)
	for _, fragment := range []string{
		"INSERT INTO role_capabilities",
		"'global-admin'",
		"'comment.internal.create'",
		"'comment.public.create'",
		"ON CONFLICT DO NOTHING",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("global administrator comment capability migration missing %q", fragment)
		}
	}
}

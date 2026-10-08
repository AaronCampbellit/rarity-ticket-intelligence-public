package ha

import (
	"os"
	"strings"
	"testing"
)

func readContractFile(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(content)
}

func TestPatroniContractProtectsAcknowledgedWrites(t *testing.T) {
	text := readContractFile(t, "patroni.yaml.tmpl")
	for _, required := range []string{
		"synchronous_mode: true",
		"synchronous_mode_strict: true",
		"synchronous_node_count: 1",
		"mode: required",
		"archive_timeout: 60s",
		"ssl: \"on\"",
		"ssl_min_protocol_version: TLSv1.2",
		"etcd3:",
		"protocol: https",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Patroni contract must include %q", required)
		}
	}
}

func TestEtcdContractUsesThreeAuthenticatedTLSPeers(t *testing.T) {
	text := readContractFile(t, "etcd.env.tmpl")
	for _, required := range []string{
		"ETCD_INITIAL_CLUSTER=rti-db-1=https://",
		"rti-db-2=https://",
		"rti-db-3=https://",
		"ETCD_CLIENT_CERT_AUTH=true",
		"ETCD_PEER_CLIENT_CERT_AUTH=true",
		"ETCD_TLS_MIN_VERSION=TLS1.2",
		"ETCD_INITIAL_CLUSTER_TOKEN=",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("etcd contract must include %q", required)
		}
	}
}

func TestHAProxyRoutesWritesOnlyToPatroniLeader(t *testing.T) {
	text := readContractFile(t, "haproxy.cfg.tmpl")
	for _, required := range []string{
		"option httpchk GET /primary",
		"verify required",
		"check-ssl",
		"server rti-db-1 ${RARITY_NODE_1_IP}:5433 ssl check port 8008",
		"server rti-db-2 ${RARITY_NODE_2_IP}:5433 ssl check port 8008",
		"server rti-db-3 ${RARITY_NODE_3_IP}:5433 ssl check port 8008",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("HAProxy contract must include %q", required)
		}
	}
}

func TestNodeComposeRequiresPinnedImagesAndWatchdog(t *testing.T) {
	text := readContractFile(t, "compose.node.yaml")
	for _, required := range []string{
		"PATRONI_IMAGE_DIGEST:?",
		"ETCD_IMAGE_DIGEST:?",
		"HAPROXY_IMAGE_DIGEST:?",
		"/dev/watchdog:/dev/watchdog",
		"network_mode: host",
		"read_only: true",
		"no-new-privileges:true",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("HA node Compose contract must include %q", required)
		}
	}
}

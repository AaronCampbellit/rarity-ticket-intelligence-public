package haconfig

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,62}$`)

var templates = map[string]string{
	"patroni.yaml.tmpl": "patroni.yaml",
	"etcd.env.tmpl":     "etcd.env",
	"haproxy.cfg.tmpl":  "haproxy.cfg",
}

func Render(templateDir, outputDir string, values map[string]string) error {
	if err := validate(values); err != nil {
		return err
	}
	if _, err := os.Stat(outputDir); err == nil {
		return fmt.Errorf("output directory already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect output directory: %w", err)
	}

	rendered := make(map[string][]byte, len(templates))
	for source, destination := range templates {
		content, err := os.ReadFile(filepath.Join(templateDir, source))
		if err != nil {
			return fmt.Errorf("read template %s: %w", source, err)
		}
		missing := make(map[string]struct{})
		expanded := os.Expand(string(content), func(key string) string {
			value, ok := values[key]
			if !ok || strings.TrimSpace(value) == "" {
				missing[key] = struct{}{}
			}
			return value
		})
		if len(missing) > 0 {
			names := make([]string, 0, len(missing))
			for name := range missing {
				names = append(names, name)
			}
			return fmt.Errorf("template %s has missing value(s): %s", source, strings.Join(names, ", "))
		}
		rendered[destination] = []byte(expanded)
	}

	parent := filepath.Dir(outputDir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create output parent: %w", err)
	}
	temporary, err := os.MkdirTemp(parent, ".rarity-ha-render-")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	defer os.RemoveAll(temporary)

	for name, content := range rendered {
		if err := os.WriteFile(filepath.Join(temporary, name), content, 0o600); err != nil {
			return fmt.Errorf("write rendered %s: %w", name, err)
		}
	}
	if err := os.Rename(temporary, outputDir); err != nil {
		return fmt.Errorf("publish rendered configuration: %w", err)
	}
	return nil
}

func validate(values map[string]string) error {
	nodeName := values["RARITY_NODE_NAME"]
	nodeNumber := map[string]int{"rti-db-1": 1, "rti-db-2": 2, "rti-db-3": 3}[nodeName]
	if nodeNumber == 0 {
		return fmt.Errorf("RARITY_NODE_NAME must be rti-db-1, rti-db-2, or rti-db-3")
	}
	for _, key := range []string{
		"RARITY_CLUSTER_NAME",
		"PGBACKREST_STANZA",
		"POSTGRES_ADMIN_USER",
		"POSTGRES_REPLICATION_USER",
		"POSTGRES_REWIND_USER",
		"POSTGRES_SUPERUSER",
	} {
		if !identifierPattern.MatchString(values[key]) {
			return fmt.Errorf("%s must be a safe identifier", key)
		}
	}
	for _, key := range []string{
		"ETCD_INITIAL_CLUSTER_TOKEN",
		"POSTGRES_ADMIN_PASSWORD",
		"POSTGRES_REPLICATION_PASSWORD",
		"POSTGRES_REWIND_PASSWORD",
		"POSTGRES_SUPERUSER_PASSWORD",
	} {
		if len(values[key]) < 32 {
			return fmt.Errorf("%s must contain at least 32 characters", key)
		}
	}
	if state := values["ETCD_INITIAL_CLUSTER_STATE"]; state != "new" && state != "existing" {
		return fmt.Errorf("ETCD_INITIAL_CLUSTER_STATE must be new or existing")
	}

	_, cluster, err := net.ParseCIDR(values["RARITY_CLUSTER_CIDR"])
	if err != nil {
		return fmt.Errorf("RARITY_CLUSTER_CIDR must be a valid CIDR")
	}
	seen := make(map[string]struct{}, 3)
	for index := 1; index <= 3; index++ {
		key := fmt.Sprintf("RARITY_NODE_%d_IP", index)
		ip := net.ParseIP(values[key])
		if ip == nil || !cluster.Contains(ip) {
			return fmt.Errorf("%s must be a valid address inside RARITY_CLUSTER_CIDR", key)
		}
		if _, duplicate := seen[ip.String()]; duplicate {
			return fmt.Errorf("%s must be unique", key)
		}
		seen[ip.String()] = struct{}{}
	}
	if values["RARITY_NODE_IP"] != values[fmt.Sprintf("RARITY_NODE_%d_IP", nodeNumber)] {
		return fmt.Errorf("RARITY_NODE_IP must match RARITY_NODE_NAME")
	}
	return nil
}

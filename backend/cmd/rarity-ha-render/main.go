package main

import (
	"fmt"
	"os"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/haconfig"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: rarity-ha-render <template-directory> <new-output-directory>")
		os.Exit(2)
	}

	values := make(map[string]string)
	for _, key := range []string{
		"RARITY_NODE_NAME",
		"RARITY_NODE_IP",
		"RARITY_NODE_1_IP",
		"RARITY_NODE_2_IP",
		"RARITY_NODE_3_IP",
		"RARITY_CLUSTER_CIDR",
		"RARITY_CLUSTER_NAME",
		"ETCD_INITIAL_CLUSTER_STATE",
		"ETCD_INITIAL_CLUSTER_TOKEN",
		"PGBACKREST_STANZA",
		"POSTGRES_ADMIN_USER",
		"POSTGRES_ADMIN_PASSWORD",
		"POSTGRES_REPLICATION_USER",
		"POSTGRES_REPLICATION_PASSWORD",
		"POSTGRES_REWIND_USER",
		"POSTGRES_REWIND_PASSWORD",
		"POSTGRES_SUPERUSER",
		"POSTGRES_SUPERUSER_PASSWORD",
	} {
		values[key] = os.Getenv(key)
	}

	if err := haconfig.Render(os.Args[1], os.Args[2], values); err != nil {
		fmt.Fprintf(os.Stderr, "render HA configuration: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "Rendered owner-only HA configuration in %s\n", os.Args[2])
}

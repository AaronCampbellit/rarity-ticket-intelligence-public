# Infrastructure

Rarity deployment, high-availability, backup, observability, and operations assets.

The Compose directory contains the local/demo base, an operator-configured single-node pilot override, and a release override that replaces local API/frontend builds with verified GHCR digest references. The pilot is explicitly non-HA and requires HTTPS ingress plus TLS certificates for PostgreSQL, Valkey, and MinIO. Production HA and backup topology assets are maintained separately so the pilot cannot be mistaken for the three-node production profile.

First-run deployments must set `RARITY_PUBLIC_URL` to the address reachable
from the administrator's browser. Local generation uses
`http://127.0.0.1:18080`; demo deployments require an explicit value; and the
pilot derives `https://<RARITY_PUBLIC_HOST>`. Before installation setup is
complete, API startup output contains a single-use setup URL for 15 minutes.
Treat that output as secret-bearing until the URL is consumed, rotated, or
expired.

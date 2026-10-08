# Third-party notices — Rarity Ticket Intelligence

Original Aaron Campbell material retains the terms in [LICENSE](LICENSE). Third-party components retain their own licenses and copyright notices. Those terms, including permission to use or modify covered components, are not replaced by the original-material restrictions. Rarity names and branding remain subject to [BRANDING.md](BRANDING.md).

This collection records the dependencies and available license texts inspected on **2026-10-08**, against dependency baseline `01a7064bcb3c2e11d7a3c2c70fe49ec6015264a6`. It distinguishes source references from software included in a distributed bundle or binary. The [machine-readable inventory](third-party-licenses/manifest.json) records exact versions, upstream sources and hashes of verbatim copied texts. It is a bounded component inventory, not full release or container license clearance.

## Frontend runtime components

The following locked and installed runtime packages are conservatively included for frontend distribution. Preserve the applicable full texts with delivered bundles; short generated headers do not replace the license grants. Lucide includes ISC material and Feather-derived MIT icon portions.

| Component | Version | Terms | Copied texts | Source |
| --- | --- | --- | --- | --- |
| `lucide-react` | `1.28.0` | ISC; MIT for Feather-derived portions | [LICENSE](third-party-licenses/npm/lucide-react-1.28.0/LICENSE) | [source](https://github.com/lucide-icons/lucide) |
| `react` | `19.2.8` | MIT | [LICENSE](third-party-licenses/npm/react-19.2.8/LICENSE) | [source](https://github.com/react/react) |
| `react-dom` | `19.2.8` | MIT | [LICENSE](third-party-licenses/npm/react-dom-19.2.8/LICENSE) | [source](https://github.com/react/react) |
| `scheduler` | `0.27.0` | MIT | [LICENSE](third-party-licenses/npm/scheduler-0.27.0/LICENSE) | [source](https://github.com/facebook/react) |

## Go executable dependencies

These modules were observed by `go list -deps` for the production executable entry points. Full files are retained, including applicable NOTICE files and nested component licenses. A module containing several licenses is listed with those texts rather than reduced to one grant.

| Component | Version | Terms | Copied texts | Source |
| --- | --- | --- | --- | --- |
| `github.com/dustin/go-humanize` | `v1.0.1` | MIT | [LICENSE](third-party-licenses/go/github.com/dustin/go-humanize/v1.0.1/LICENSE) | [source](https://proxy.golang.org/github.com/dustin/go-humanize/@v/v1.0.1.zip) |
| `github.com/go-ini/ini` | `v1.67.0` | Apache-2.0 | [LICENSE](third-party-licenses/go/github.com/go-ini/ini/v1.67.0/LICENSE) | [source](https://proxy.golang.org/github.com/go-ini/ini/@v/v1.67.0.zip) |
| `github.com/goccy/go-json` | `v0.10.5` | MIT | [LICENSE](third-party-licenses/go/github.com/goccy/go-json/v0.10.5/LICENSE) | [source](https://proxy.golang.org/github.com/goccy/go-json/@v/v0.10.5.zip) |
| `github.com/google/uuid` | `v1.6.0` | BSD-family (preserve full text) | [LICENSE](third-party-licenses/go/github.com/google/uuid/v1.6.0/LICENSE) | [source](https://proxy.golang.org/github.com/google/uuid/@v/v1.6.0.zip) |
| `github.com/jackc/pgpassfile` | `v1.0.0` | MIT | [LICENSE](third-party-licenses/go/github.com/jackc/pgpassfile/v1.0.0/LICENSE) | [source](https://proxy.golang.org/github.com/jackc/pgpassfile/@v/v1.0.0.zip) |
| `github.com/jackc/pgservicefile` | `v0.0.0-20240606120523-5a60cdf6a761` | MIT | [LICENSE](third-party-licenses/go/github.com/jackc/pgservicefile/v0.0.0-20240606120523-5a60cdf6a761/LICENSE) | [source](https://proxy.golang.org/github.com/jackc/pgservicefile/@v/v0.0.0-20240606120523-5a60cdf6a761.zip) |
| `github.com/jackc/pgx/v5` | `v5.9.2` | MIT | [LICENSE](third-party-licenses/go/github.com/jackc/pgx/v5/v5.9.2/LICENSE) | [source](https://proxy.golang.org/github.com/jackc/pgx/v5/@v/v5.9.2.zip) |
| `github.com/jackc/puddle/v2` | `v2.2.2` | MIT | [LICENSE](third-party-licenses/go/github.com/jackc/puddle/v2/v2.2.2/LICENSE) | [source](https://proxy.golang.org/github.com/jackc/puddle/v2/@v/v2.2.2.zip) |
| `github.com/klauspost/compress` | `v1.18.7` | Apache-2.0; BSD-family (preserve full text); MIT | [LICENSE](third-party-licenses/go/github.com/klauspost/compress/v1.18.7/LICENSE)<br>[LICENSE](third-party-licenses/go/github.com/klauspost/compress/v1.18.7/gzhttp/LICENSE)<br>[LICENSE](third-party-licenses/go/github.com/klauspost/compress/v1.18.7/internal/lz4ref/LICENSE)<br>[LICENSE](third-party-licenses/go/github.com/klauspost/compress/v1.18.7/internal/snapref/LICENSE)<br>[LICENSE](third-party-licenses/go/github.com/klauspost/compress/v1.18.7/s2/LICENSE)<br>[LICENSE](third-party-licenses/go/github.com/klauspost/compress/v1.18.7/s2/cmd/internal/filepathx/LICENSE)<br>[LICENSE](third-party-licenses/go/github.com/klauspost/compress/v1.18.7/s2/cmd/internal/readahead/LICENSE)<br>[LICENSE](third-party-licenses/go/github.com/klauspost/compress/v1.18.7/snappy/LICENSE)<br>[LICENSE](third-party-licenses/go/github.com/klauspost/compress/v1.18.7/snappy/xerial/LICENSE)<br>[LICENSE.txt](third-party-licenses/go/github.com/klauspost/compress/v1.18.7/zstd/internal/xxhash/LICENSE.txt) | [source](https://proxy.golang.org/github.com/klauspost/compress/@v/v1.18.7.zip) |
| `github.com/klauspost/cpuid/v2` | `v2.2.11` | MIT | [LICENSE](third-party-licenses/go/github.com/klauspost/cpuid/v2/v2.2.11/LICENSE) | [source](https://proxy.golang.org/github.com/klauspost/cpuid/v2/@v/v2.2.11.zip) |
| `github.com/mfridman/interpolate` | `v0.0.2` | MIT | [LICENSE.txt](third-party-licenses/go/github.com/mfridman/interpolate/v0.0.2/LICENSE.txt) | [source](https://proxy.golang.org/github.com/mfridman/interpolate/@v/v0.0.2.zip) |
| `github.com/minio/crc64nvme` | `v1.0.2` | Apache-2.0 | [LICENSE](third-party-licenses/go/github.com/minio/crc64nvme/v1.0.2/LICENSE) | [source](https://proxy.golang.org/github.com/minio/crc64nvme/@v/v1.0.2.zip) |
| `github.com/minio/md5-simd` | `v1.1.2` | Apache-2.0; BSD-family (preserve full text) | [LICENSE](third-party-licenses/go/github.com/minio/md5-simd/v1.1.2/LICENSE)<br>[LICENSE.Golang](third-party-licenses/go/github.com/minio/md5-simd/v1.1.2/LICENSE.Golang) | [source](https://proxy.golang.org/github.com/minio/md5-simd/@v/v1.1.2.zip) |
| `github.com/minio/minio-go/v7` | `v7.0.95` | Apache-2.0 | [LICENSE](third-party-licenses/go/github.com/minio/minio-go/v7/v7.0.95/LICENSE)<br>[NOTICE](third-party-licenses/go/github.com/minio/minio-go/v7/v7.0.95/NOTICE) | [source](https://proxy.golang.org/github.com/minio/minio-go/v7/@v/v7.0.95.zip) |
| `github.com/philhofer/fwd` | `v1.2.0` | MIT | [LICENSE.md](third-party-licenses/go/github.com/philhofer/fwd/v1.2.0/LICENSE.md) | [source](https://proxy.golang.org/github.com/philhofer/fwd/@v/v1.2.0.zip) |
| `github.com/pressly/goose/v3` | `v3.26.0` | MIT | [LICENSE](third-party-licenses/go/github.com/pressly/goose/v3/v3.26.0/LICENSE) | [source](https://proxy.golang.org/github.com/pressly/goose/v3/@v/v3.26.0.zip) |
| `github.com/rs/xid` | `v1.6.0` | MIT | [LICENSE](third-party-licenses/go/github.com/rs/xid/v1.6.0/LICENSE) | [source](https://proxy.golang.org/github.com/rs/xid/@v/v1.6.0.zip) |
| `github.com/sethvargo/go-retry` | `v0.3.0` | Apache-2.0 | [LICENSE](third-party-licenses/go/github.com/sethvargo/go-retry/v0.3.0/LICENSE) | [source](https://proxy.golang.org/github.com/sethvargo/go-retry/@v/v0.3.0.zip) |
| `github.com/tinylib/msgp` | `v1.3.0` | MIT | [LICENSE](third-party-licenses/go/github.com/tinylib/msgp/v1.3.0/LICENSE) | [source](https://proxy.golang.org/github.com/tinylib/msgp/@v/v1.3.0.zip) |
| `go.uber.org/multierr` | `v1.11.0` | MIT | [LICENSE.txt](third-party-licenses/go/go.uber.org/multierr/v1.11.0/LICENSE.txt) | [source](https://proxy.golang.org/go.uber.org/multierr/@v/v1.11.0.zip) |
| `golang.org/x/crypto` | `v0.56.0` | BSD-family (preserve full text) | [LICENSE](third-party-licenses/go/golang.org/x/crypto/v0.56.0/LICENSE) | [source](https://proxy.golang.org/golang.org/x/crypto/@v/v0.56.0.zip) |
| `golang.org/x/net` | `v0.57.0` | BSD-family (preserve full text) | [LICENSE](third-party-licenses/go/golang.org/x/net/v0.57.0/LICENSE) | [source](https://proxy.golang.org/golang.org/x/net/@v/v0.57.0.zip) |
| `golang.org/x/sync` | `v0.22.0` | BSD-family (preserve full text) | [LICENSE](third-party-licenses/go/golang.org/x/sync/v0.22.0/LICENSE) | [source](https://proxy.golang.org/golang.org/x/sync/@v/v0.22.0.zip) |
| `golang.org/x/sys` | `v0.47.0` | BSD-family (preserve full text) | [LICENSE](third-party-licenses/go/golang.org/x/sys/v0.47.0/LICENSE) | [source](https://proxy.golang.org/golang.org/x/sys/@v/v0.47.0.zip) |
| `golang.org/x/term` | `v0.45.0` | BSD-family (preserve full text) | [LICENSE](third-party-licenses/go/golang.org/x/term/v0.45.0/LICENSE) | [source](https://proxy.golang.org/golang.org/x/term/@v/v0.45.0.zip) |
| `golang.org/x/text` | `v0.41.0` | BSD-family (preserve full text) | [LICENSE](third-party-licenses/go/golang.org/x/text/v0.41.0/LICENSE) | [source](https://proxy.golang.org/golang.org/x/text/@v/v0.41.0.zip) |

| Component | Version | Terms | Copied texts | Source |
| --- | --- | --- | --- | --- |
| `Go runtime and standard library` | `go1.26.8` | BSD-3-Clause | [LICENSE](third-party-licenses/go-runtime/go1.26.8/LICENSE) | [source](https://go.googlesource.com/go/+/refs/tags/go1.26.8/LICENSE) |

## Development and build components

These are selected build/test tools and data with MPL or attribution terms, plus Tailwind generated-CSS terms where applicable. They are not all shipped application libraries. This table is not a complete license pack for redistributing an entire development environment. MPL tool code retains its covered-source requirements if the tool itself or modified covered files are distributed; merely using the tool does not apply MPL to the original application output.

| Component | Version | Terms | Copied texts | Source |
| --- | --- | --- | --- | --- |
| `axe-core` | `4.12.1` | MPL-2.0 | [LICENSE](third-party-licenses/npm/axe-core-4.12.1/LICENSE)<br>[LICENSE-3RD-PARTY.txt](third-party-licenses/npm/axe-core-4.12.1/LICENSE-3RD-PARTY.txt) | [source](https://github.com/dequelabs/axe-core) |
| `lightningcss` | `1.33.0` | MPL-2.0 | [LICENSE](third-party-licenses/npm/lightningcss-1.33.0/LICENSE) | [source](https://github.com/parcel-bundler/lightningcss) |
| `lightningcss-linux-x64-gnu` | `1.33.0` | MPL-2.0 | [LICENSE](third-party-licenses/npm/lightningcss-linux-x64-gnu-1.33.0/LICENSE) | [source](https://github.com/parcel-bundler/lightningcss) |

## Separate infrastructure images

Compose references external images for PostgreSQL, Valkey, MinIO and Caddy. References and downloads are distinct from checking their server source into this repository or conveying the images to someone else. The Apache-2.0 **MinIO Go SDK** linked in the application above is separate from the AGPL-3.0 **MinIO server** image below.

| Service reference | Main-project license and primary text |
| --- | --- |
| `postgres:16.11-alpine3.22` | [PostgreSQL license](https://raw.githubusercontent.com/postgres/postgres/REL_16_11/COPYRIGHT) |
| `valkey/valkey:8.1.3-alpine` | [BSD-3-Clause](https://raw.githubusercontent.com/valkey-io/valkey/8.1.3/COPYING) |
| `minio/minio:RELEASE.2025-04-22T22-12-26Z` | [AGPL-3.0](https://raw.githubusercontent.com/minio/minio/RELEASE.2025-04-22T22-12-26Z/LICENSE) |
| `caddy:2.10.2-alpine` | [Apache-2.0](https://raw.githubusercontent.com/caddyserver/caddy/v2.10.2/LICENSE) |

Image digests are recorded in `infrastructure/compose/compose.yaml`. The table identifies main-project terms only; Alpine and other contained components need an image-specific inventory when images are conveyed. Before conveying MinIO binaries/images, verify the license/notice and Corresponding Source duties for the actual artifact. Modified AGPL network-facing software has additional covered-source access duties to remote users. Separate service use does not by itself establish AGPL coverage for the original RTI source.

Operator-selected etcd, Patroni and HAProxy images in the HA configuration have no fixed versions here. Their image-specific terms are not verified. The scratch API image also copies CA certificates and time-zone data; those copied files need their own release inventory.

## Automatic notice packaging

`npm run build` includes this notice, the original-material terms and the complete collected license texts at `frontend/dist/legal/`. Runtime Docker stages retain the same collection.

## Checks for each release

- Standalone binaries must be accompanied by the equivalent legal directory.
- Verify that this inventory matches the exact release dependency versions and copied/generated components; update it when dependencies change.
- Complete image-specific license/SBOM and source-delivery checks for the actual images being conveyed. This collection is not a complete container license pack.
- Preserve existing source copyright comments, applicable component notices and brand/asset rights. Older assets without explicit capture provenance are not independently cleared by this dependency inventory.

The copied texts control their respective components. Security scanner results, license-count filters, and this notice do not establish complete license compliance or ownership.

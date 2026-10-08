# Release Readiness and Evidence Gate

Publication note: historical commits, CI runs, pull requests, signing identities,
and image digests in this document belong to the original private development
repository. They are retained as provenance for their recorded revisions; this
public source snapshot begins with fresh history and does not publish those
old artifacts or attest a new release.

**Status:** Source and exact published-artifact evidence verified; deployed pilot acceptance pending

Rarity does not promote a revision because source checks pass or a container starts. A release candidate is eligible only when one immutable revision has passed every required gate in the named acceptance environment and each result points to a retained artifact.

## Revision boundary

The August 18 artifact evidence below belongs only to its recorded revision. It
does not certify current source changes or the pending calendar workspace. Use
the [current execution backlog](../09-roadmap/current-execution.md) to finish source
work before selecting and qualifying a new immutable release candidate.

## Required evidence

The release evidence manifest requires passed, revision-bound artifacts for:

- repository security and dependency scans;
- cross-client isolation;
- automated and manual accessibility acceptance;
- backup execution and clean-room restore/DR;
- PostgreSQL HA failover;
- upgrade and rollback rehearsal;
- the approved capacity workload;
- metrics, logs, traces, alerts, and named runbook routing;
- forward and rollback migration verification;
- signed/pinned artifacts, SBOM, and provenance; and
- operator runbook review.

Copy `infrastructure/release/release-evidence.example.json` into the release’s external evidence directory. Keep artifacts outside the repository when they may contain infrastructure identifiers or operational detail. `scripts/verify-release-artifacts.sh` writes the retained exact-digest Cosign verification, Buildx-extracted SPDX SBOM and SLSA provenance, and revision/digest-bound `artifact-provenance.json` files into that directory. The release gate resolves every evidence path relative to the release manifest, requires a regular retained file, validates the extracted SPDX document and BuildKit provenance shape, requires provenance to identify the release revision, and requires the generated artifact manifest to exactly match the release revision and both declared component artifacts. The Cosign verification binds those OCI-index attestations to the declared immutable digest. Set every gate to `passed` only after its artifact exists and records the same full revision.

## Published artifact acceptance

On August 18 2026, exact revision
`098e2a66a29424225911018ef5db0762f136af9a` passed
GitHub Actions run `32164556586` (private development history)
and the chained
GitHub Actions run `32165526069` (private development history).
The publication reran only the failed frontend job after its first signing
attempt encountered an external Sigstore TUF CDN HTTP 403; attempt 2 retained
the same source revision and completed both image jobs without a repository or
workflow change.

The promoted immutable artifacts are:

- API: `ghcr.io/aaroncampbellit/rarity-ticket-intelligence-api@sha256:00d91a7c7fe098201c702850c93bb506aa0b757da3b407a8bfbfae557b5dd6d5`;
- frontend: `ghcr.io/aaroncampbellit/rarity-ticket-intelligence-frontend@sha256:52020cd1eac5e040f9dfe14c29303c0070d1dfafe4f1e72b0d9d1081d7ea8513`.

The repository-native verifier used canonical signing identity
`https://github.com/AaronCampbellit/rarity-ticket-intelligence/.github/workflows/publish.yml@refs/heads/main`.
It verified both exact-digest signatures and retained exactly seven JSON files:
the two signature results, two SPDX SBOMs, two BuildKit provenance documents,
and `artifact-provenance.json`. Both provenance records matched the revision at
the config-source SHA, build argument, OCI revision label, and workflow SHA.
The production `rarity-release-gate` consumer accepted the two real artifact
records inside a complete contract fixture. That consumer exercise proves the
artifact boundary and does not claim execution of the other thirteen pilot
operations; those remain pending until their actual environment evidence is
retained.

Run the fail-closed check:

```bash
go run ./backend/cmd/rarity-release-gate \
  -evidence /absolute/path/to/release-evidence.json \
  -revision "$(git rev-parse HEAD)"
```

Missing, pending, failed, duplicated, stale-revision, or untraceable evidence rejects promotion. The command validates evidence; it does not deploy, sign, migrate, fail over, restore, or infer success.

## Promotion and rollback

The release owner records version, compatibility, migrations, security changes, operational impact, known issues, upgrade procedure, rollback trigger, rollback limits, and documentation revision. Promotion uses only the digest-pinned artifacts represented by the provenance evidence.

Rollback is authorized when a declared health, data-integrity, security, or latency trigger is crossed. The operator follows the rehearsed rollback artifact and does not run a destructive database reversal unless its compatibility window and data effects were explicitly verified. Recovery from an incompatible data transition follows the clean-room restore and DR runbook.

The release decision and approver are recorded alongside the sealed evidence manifest. Source readiness remains distinct from deployed pilot acceptance.

For GitHub-produced images, artifact-provenance evidence records both component digests, the successful trusted-branch CI run, Cosign verification output, BuildKit SBOM/provenance statements, and the vulnerability/secret/configuration/license scan result for each exact digest.

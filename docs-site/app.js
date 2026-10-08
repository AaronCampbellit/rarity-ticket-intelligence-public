const documentPaths = `
docs/README.md
docs/screenshots/README.md
docs/09-roadmap/current-execution.md
docs/06-development/local-development.md
docs/superpowers/README.md
docs/00-foundation/01-project-vision.md
docs/00-foundation/02-architecture-manifesto.md
docs/00-foundation/03-engineering-principles.md
docs/00-foundation/04-guiding-philosophy.md
docs/00-foundation/05-things-we-will-never-do.md
docs/00-foundation/06-glossary.md
docs/00-foundation/golden-document-review.md
docs/00-foundation/milestone-0-approval.md
docs/01-architecture/01-platform-overview.md
docs/01-architecture/02-core-domain-model.md
docs/01-architecture/03-object-model.md
docs/01-architecture/04-event-driven-architecture.md
docs/01-architecture/05-dependency-graph.md
docs/01-architecture/06-workflow-engine.md
docs/01-architecture/07-automation-engine.md
docs/01-architecture/08-ai-platform.md
docs/01-architecture/09-search-architecture.md
docs/01-architecture/10-inbound-intake-pipeline.md
docs/01-architecture/11-entity-contracts.md
docs/01-architecture/12-relationship-registry.md
docs/01-architecture/13-event-catalog.md
docs/01-architecture/v1-service-desk-operating-model.md
docs/02-platform/unified-calendar.md
docs/02-platform/assets.md
docs/02-platform/clients.md
docs/02-platform/contracts.md
docs/02-platform/departments.md
docs/02-platform/identity-and-access.md
docs/02-platform/integrations.md
docs/02-platform/knowledge-base.md
docs/02-platform/notifications.md
docs/02-platform/opportunities.md
docs/02-platform/organizations.md
docs/02-platform/proposals.md
docs/02-platform/projects.md
docs/02-platform/queues.md
docs/02-platform/services.md
docs/02-platform/tasks.md
docs/02-platform/teams.md
docs/02-platform/technicians.md
docs/02-platform/tickets.md
docs/03-security/api-security.md
docs/03-security/audit-logging.md
docs/03-security/authentication.md
docs/03-security/authorization-rbac.md
docs/03-security/backup-security.md
docs/03-security/browser-security.md
docs/03-security/client-isolation-test-model.md
docs/03-security/encryption.md
docs/03-security/entra-sso.md
docs/03-security/incident-response.md
docs/03-security/permission-matrix.md
docs/03-security/secret-management.md
docs/03-security/threat-model.md
docs/03-security/v1-identity-and-ai-boundaries.md
docs/03-security/security-overview.md
docs/04-api/api-standards.md
docs/04-api/authentication.md
docs/04-api/canonical-contract-examples.md
docs/04-api/errors.md
docs/04-api/filtering.md
docs/04-api/integration-automation-ai-contracts.md
docs/04-api/pagination.md
docs/04-api/psa-opportunities-projects.md
docs/04-api/rest-api.md
docs/04-api/sdk-guidelines.md
docs/04-api/sorting.md
docs/04-api/versioning.md
docs/04-api/webhooks.md
docs/04-api/v1-intake-and-integration-contract.md
docs/05-infrastructure/backups.md
docs/05-infrastructure/database.md
docs/05-infrastructure/data-lifecycle-and-retention.md
docs/05-infrastructure/deployment.md
docs/05-infrastructure/disaster-recovery.md
docs/05-infrastructure/docker.md
docs/05-infrastructure/high-availability.md
docs/05-infrastructure/kubernetes.md
docs/05-infrastructure/local-administrator-recovery.md
docs/05-infrastructure/logging.md
docs/05-infrastructure/monitoring.md
docs/05-infrastructure/observability.md
docs/05-infrastructure/postgres-cluster.md
docs/05-infrastructure/redis.md
docs/05-infrastructure/storage.md
docs/05-infrastructure/production-operating-model.md
docs/05-infrastructure/pilot-acceptance-runbook.md
docs/06-development/branching-strategy.md
docs/06-development/ci-cd.md
docs/06-development/coding-standards.md
docs/06-development/commit-guidelines.md
docs/06-development/integration-automation-ai-acceptance.md
docs/06-development/dependency-management.md
docs/06-development/delivery-baseline.md
docs/06-development/release-process.md
docs/06-development/testing.md
docs/06-development/acceptance-environment.md
docs/06-development/psa-acceptance.md
docs/06-development/internal-mentions-acceptance.md
docs/06-development/release-readiness.md
docs/07-ui-ux/accessibility.md
docs/07-ui-ux/dark-mode.md
docs/07-ui-ux/dashboard.md
docs/07-ui-ux/design-system.md
docs/07-ui-ux/design-system-acceptance.md
docs/07-ui-ux/forms.md
docs/07-ui-ux/first-run-and-help.md
docs/07-ui-ux/information-architecture.md
docs/07-ui-ux/navigation.md
docs/07-ui-ux/tables.md
docs/07-ui-ux/technician-workspace-wireframes.md
docs/08-modules/ticket-intelligence.md
docs/09-roadmap/future.md
docs/09-roadmap/build-backlog.md
docs/09-roadmap/build-readiness-decision-register.md
docs/09-roadmap/implementation-plan.md
docs/09-roadmap/phase-0-baseline-backlog.md
docs/09-roadmap/milestone-0.md
docs/09-roadmap/v1.md
docs/09-roadmap/v2.md
docs/09-roadmap/v1-decision-register.md
docs/superpowers/specs/2026-07-28-native-psa-opportunities-projects-design.md
docs/superpowers/plans/2026-07-28-native-psa-opportunities-projects.md
docs/superpowers/specs/2026-07-29-provider-agnostic-ai-runtime-design.md
docs/superpowers/plans/2026-07-29-provider-agnostic-ai-runtime.md
docs/superpowers/plans/2026-08-01-rarity-product-design-system.md
docs/superpowers/specs/2026-08-02-optional-entra-local-platform-admin-design.md
docs/superpowers/plans/2026-08-02-optional-entra-local-platform-admin.md
docs/superpowers/specs/2026-08-02-build-roadmap-status-design.md
docs/superpowers/plans/2026-08-02-build-roadmap-status.md
docs/superpowers/specs/2026-08-03-admin-navigation-and-shell-layout-design.md
docs/superpowers/plans/2026-08-03-admin-navigation-and-shell-layout.md
docs/superpowers/specs/2026-08-03-automatic-remote-bootstrap-url-design.md
docs/superpowers/plans/2026-08-03-automatic-remote-bootstrap-url.md
docs/superpowers/specs/2026-08-03-authenticated-application-boundary-design.md
docs/superpowers/plans/2026-08-03-authenticated-application-boundary.md
docs/superpowers/specs/2026-08-04-global-ai-workspace-design.md
docs/superpowers/specs/2026-08-04-msp-wide-ai-client-actions-design.md
docs/superpowers/plans/2026-08-04-global-ai-workspace.md
docs/superpowers/plans/2026-08-04-msp-wide-ai-client-actions.md
docs/superpowers/plans/2026-08-04-ai-workspace-action-orchestration.md
docs/superpowers/plans/2026-08-04-ai-workspace-task-actions.md
docs/superpowers/specs/2026-08-04-guided-setup-center-design.md
docs/superpowers/plans/2026-08-04-guided-setup-center.md
docs/superpowers/specs/2026-08-04-workforce-time-review-design.md
docs/superpowers/plans/2026-08-04-workforce-time-review.md
docs/superpowers/specs/2026-08-05-ai-object-client-business-data-catalog-design.md
docs/superpowers/plans/2026-08-05-ai-object-client-business-data-catalog.md
docs/superpowers/specs/2026-08-05-distinct-product-directions-design.md
docs/superpowers/plans/2026-08-05-distinct-product-directions.md
docs/superpowers/specs/2026-08-05-governed-tagging-classification-design.md
docs/superpowers/plans/2026-08-05-governed-tagging-classification.md
docs/superpowers/specs/2026-08-05-product-experience-overhaul-design.md
docs/superpowers/plans/2026-08-05-product-experience-overhaul.md
docs/superpowers/specs/2026-08-05-signal-console-consolidation-design.md
docs/superpowers/plans/2026-08-05-signal-console-consolidation.md
docs/superpowers/specs/2026-08-06-internal-mentions-collaboration-design.md
docs/superpowers/plans/2026-08-06-internal-mentions-collaboration.md
docs/superpowers/specs/2026-08-07-unified-calendar-design.md
docs/superpowers/plans/2026-08-07-unified-calendar.md
docs/superpowers/plans/2026-08-14-unified-calendar-task10-persistence.md
docs/superpowers/plans/2026-08-14-unified-calendar-task10-ai-runtime.md
docs/superpowers/specs/2026-08-15-calendar-notification-routing-design.md
docs/superpowers/plans/2026-08-15-calendar-notification-routing.md
docs/superpowers/specs/2026-08-16-frontend-notification-center-design.md
docs/superpowers/plans/2026-08-16-frontend-notification-center.md
docs/superpowers/specs/2026-08-17-ci-release-pipeline-recovery-design.md
docs/superpowers/plans/2026-08-17-ci-release-pipeline-recovery.md
docs/superpowers/specs/2026-08-06-ai-second-operational-wave-design.md
docs/superpowers/plans/2026-08-06-ai-second-operational-wave.md
docs/OPEN-QUESTIONS.md
docs/decisions/ADR-0001-one-msp-per-installation.md
docs/decisions/ADR-0002-api-first.md
docs/decisions/ADR-0003-event-driven.md
docs/decisions/ADR-0004-first-class-objects.md
docs/decisions/ADR-0005-independent-routing-dimensions.md
docs/decisions/ADR-0006-primary-owner-and-collaborators.md
docs/decisions/ADR-0007-shared-work-record.md
docs/decisions/ADR-0008-dependency-graph.md
docs/decisions/ADR-0009-rule-driven-workflows.md
docs/decisions/ADR-0010-v1-automation-boundary.md
docs/decisions/ADR-0011-technician-first-v1.md
docs/decisions/ADR-0012-docker-first.md
docs/decisions/ADR-0013-enterprise-foundation.md
docs/decisions/ADR-0014-ai-human-authority.md
docs/decisions/ADR-0015-production-operating-model.md
docs/decisions/ADR-0016-v1-service-desk-scope.md
docs/decisions/ADR-0017-v1-intake-identity-and-ai.md
docs/decisions/ADR-0018-milestone-0-baseline-approved.md
docs/decisions/ADR-0019-v1-worksheet-defaults.md
docs/decisions/ADR-0020-no-technician-pats-in-v1.md
docs/decisions/ADR-0021-minio-first-self-hosted-object-storage.md
docs/decisions/ADR-0022-go-react-sql-first-stack.md
docs/decisions/ADR-0023-patroni-etcd-haproxy-production-ha.md
docs/decisions/ADR-0024-valkey-temporary-acceleration.md
docs/decisions/ADR-0025-pgbackrest-continuous-pitr.md
docs/decisions/ADR-0026-local-git-first-delivery-baseline.md
docs/decisions/ADR-0027-graph-webhook-and-delta-email-intake.md
docs/decisions/ADR-0028-datto-read-ingest-reconciliation.md
docs/decisions/ADR-0029-teams-webhook-notification-delivery.md
docs/decisions/ADR-0030-v1-capacity-and-performance-contract.md
docs/decisions/ADR-0031-ubuntu-24-04-compose-profile.md
docs/decisions/ADR-0032-rarity-is-native-psa.md
docs/decisions/ADR-0033-temporary-shared-demo-host-exception.md
docs/decisions/ADR-0034-github-reviewed-artifact-delivery.md
docs/decisions/README.md
`.trim().split("\n");

const categoryNames = {
  "00-foundation": "Foundation",
  "01-architecture": "Architecture",
  "02-platform": "Platform",
  "03-security": "Security",
  "04-api": "API",
  "05-infrastructure": "Infrastructure",
  "06-development": "Development",
  "07-ui-ux": "UI / UX",
  "08-modules": "Modules",
  "09-roadmap": "Roadmap",
  "decisions": "Decisions",
  "docs": "Project"
};

const titleCase = value => value
  .replace(/\.md$/i, "")
  .replace(/^ADR-(\d+)-/i, "ADR-$1 · ")
  .replace(/^\d+-/, "")
  .replaceAll("-", " ")
  .replace(/\b\w/g, character => character.toUpperCase())
  .replace(/\bApi\b/g, "API")
  .replace(/\bAi\b/g, "AI")
  .replace(/\bRbac\b/g, "RBAC")
  .replace(/\bSso\b/g, "SSO")
  .replace(/\bCi Cd\b/g, "CI/CD");

const docs = documentPaths.map(path => {
  const parts = path.split("/");
  const folder = parts.length > 2 ? parts[1] : "docs";
  const filename = parts.at(-1);
  let status = "reference";
  if (path.includes("/superpowers/") || path.includes("/decisions/")) status = "historical";
  if (["kubernetes.md", "future.md", "v2.md"].includes(filename)) status = "directional";
  if (["current-execution.md", "local-development.md", "release-readiness.md", "pilot-acceptance-runbook.md"].includes(filename) || path === "docs/superpowers/plans/2026-08-07-unified-calendar.md") status = "current";
  return {
    path,
    href: `../${path}`,
    title: filename === "README.md" ? ({ "docs/README.md": "Documentation Guide", "docs/screenshots/README.md": "Screenshot Preview", "docs/superpowers/README.md": "Plans and Specifications" }[path] || "Architecture Decision Index") : titleCase(filename),
    category: categoryNames[folder] || "Project",
    status
  };
});

const statusLabels = { reference: "Contract / reference", historical: "Decision / history", current: "Current work / guide", directional: "Future direction" };
const searchInput = document.querySelector("#search");
const categoryFilter = document.querySelector("#category-filter");
const statusFilter = document.querySelector("#status-filter");
const grid = document.querySelector("#document-grid");
const resultCount = document.querySelector("#result-count");
const emptyState = document.querySelector("#empty-state");

[...new Set(docs.map(doc => doc.category))].sort().forEach(category => {
  const option = document.createElement("option");
  option.value = category;
  option.textContent = category;
  categoryFilter.append(option);
});

function renderDocuments() {
  const query = searchInput.value.trim().toLowerCase();
  const category = categoryFilter.value;
  const status = statusFilter.value;
  const filtered = docs.filter(doc =>
    (category === "all" || doc.category === category) &&
    (status === "all" || doc.status === status) &&
    (!query || `${doc.title} ${doc.path} ${doc.category}`.toLowerCase().includes(query))
  );

  grid.replaceChildren(...filtered.map(doc => {
    const link = document.createElement("a");
    link.className = "doc-card";
    link.href = doc.href;
    link.innerHTML = `
      <div class="doc-meta"><span>${doc.category}</span><span class="status ${doc.status}">${statusLabels[doc.status]}</span></div>
      <h3>${doc.title}</h3>
      <span class="doc-path">${doc.path}</span>
    `;
    return link;
  }));
  resultCount.textContent = `${filtered.length} of ${docs.length}`;
  emptyState.hidden = filtered.length !== 0;
}

const decisions = [
  ["0001", "One MSP per installation", "ADR-0001-one-msp-per-installation.md"],
  ["0002", "API-first platform", "ADR-0002-api-first.md"],
  ["0003", "Event-driven architecture", "ADR-0003-event-driven.md"],
  ["0004", "First-class object model", "ADR-0004-first-class-objects.md"],
  ["0005", "Independent routing dimensions", "ADR-0005-independent-routing-dimensions.md"],
  ["0006", "Primary owner plus collaborators", "ADR-0006-primary-owner-and-collaborators.md"],
  ["0007", "Shared work-record foundation", "ADR-0007-shared-work-record.md"],
  ["0008", "Core dependency graph", "ADR-0008-dependency-graph.md"],
  ["0009", "Rule-driven workflow selection", "ADR-0009-rule-driven-workflows.md"],
  ["0010", "V1 automation trust boundary", "ADR-0010-v1-automation-boundary.md"],
  ["0011", "Technician-first V1", "ADR-0011-technician-first-v1.md"],
  ["0012", "Docker Compose before Kubernetes", "ADR-0012-docker-first.md"],
  ["0013", "Enterprise foundation", "ADR-0013-enterprise-foundation.md"],
  ["0014", "AI assists; humans retain authority", "ADR-0014-ai-human-authority.md"]
  ,["0015", "Docker-first production operating model", "ADR-0015-production-operating-model.md"]
  ,["0016", "V1 service desk scope", "ADR-0016-v1-service-desk-scope.md"]
  ,["0017", "V1 intake, identity, and AI boundaries", "ADR-0017-v1-intake-identity-and-ai.md"]
  ,["0018", "Milestone 0 baseline approved", "ADR-0018-milestone-0-baseline-approved.md"]
  ,["0019", "V1 decision worksheet defaults", "ADR-0019-v1-worksheet-defaults.md"]
  ,["0020", "No technician personal access tokens in V1", "ADR-0020-no-technician-pats-in-v1.md"]
  ,["0021", "MinIO is the first self-hosted object-storage option", "ADR-0021-minio-first-self-hosted-object-storage.md"]
  ,["0022", "Go, React, and SQL-first application stack", "ADR-0022-go-react-sql-first-stack.md"]
  ,["0023", "Patroni, etcd, and HAProxy production HA", "ADR-0023-patroni-etcd-haproxy-production-ha.md"]
  ,["0024", "Valkey for temporary acceleration", "ADR-0024-valkey-temporary-acceleration.md"]
  ,["0025", "pgBackRest continuous point-in-time recovery", "ADR-0025-pgbackrest-continuous-pitr.md"]
  ,["0026", "Local Git-first delivery baseline", "ADR-0026-local-git-first-delivery-baseline.md"]
  ,["0027", "Microsoft Graph webhook and delta email intake", "ADR-0027-graph-webhook-and-delta-email-intake.md"]
  ,["0028", "Datto read/ingest reconciliation and alert deduplication", "ADR-0028-datto-read-ingest-reconciliation.md"]
  ,["0029", "Teams webhook notification delivery", "ADR-0029-teams-webhook-notification-delivery.md"]
  ,["0030", "V1 capacity and performance contract", "ADR-0030-v1-capacity-and-performance-contract.md"]
  ,["0031", "Ubuntu 24.04 Docker Compose deployment profile", "ADR-0031-ubuntu-24-04-compose-profile.md"]
  ,["0032", "Rarity is the native PSA system of record", "ADR-0032-rarity-is-native-psa.md"]
  ,["0033", "Temporary shared demo-host exception", "ADR-0033-temporary-shared-demo-host-exception.md"]
  ,["0034", "GitHub-reviewed artifact delivery", "ADR-0034-github-reviewed-artifact-delivery.md"]
];

document.querySelector("#decision-list").replaceChildren(...decisions.map(([number, title, file]) => {
  const link = document.createElement("a");
  link.className = "decision";
  link.href = `../docs/decisions/${file}`;
  link.innerHTML = `<code>${number}</code><strong>${title}</strong><em>Accepted</em>`;
  return link;
}));

[searchInput, categoryFilter, statusFilter].forEach(control => control.addEventListener("input", renderDocuments));
document.querySelector("#doc-count").textContent = docs.length;
renderDocuments();

import { readFile, readdir } from "node:fs/promises";
import { join, relative, resolve, sep } from "node:path";

import { releaseAcceptanceDocumentationErrors } from "./validate-release-acceptance-docs.mjs";

const repositoryRoot = resolve(import.meta.dirname, "..");
const contractPath = resolve(
  repositoryRoot,
  "docs/04-api/psa-opportunities-projects.md",
);
const trackerPath = resolve(repositoryRoot, "docs-site/app.js");
const siteIndexPath = resolve(repositoryRoot, "docs-site/index.html");
const restAPIPath = resolve(repositoryRoot, "docs/04-api/rest-api.md");
const taggingRoutesPath = resolve(
  repositoryRoot,
  "backend/internal/httpapi/tagging_routes.go",
);
const taggingReportingPath = resolve(
  repositoryRoot,
  "backend/internal/tagging/reporting.go",
);
const mentionRoutesPath = resolve(
  repositoryRoot,
  "backend/internal/httpapi/mention_routes.go",
);
const collaborationRoutesPath = resolve(
  repositoryRoot,
  "backend/internal/httpapi/collaboration_routes.go",
);
const organizationRoutesPath = resolve(
  repositoryRoot,
  "backend/internal/httpapi/organization_routes.go",
);
const notificationRoutesPath = resolve(
  repositoryRoot,
  "backend/internal/httpapi/notification_routes.go",
);
const mentionServicePath = resolve(
  repositoryRoot,
  "backend/internal/mentions/service.go",
);
const collaborationRepositoryPath = resolve(
  repositoryRoot,
  "backend/internal/store/psa/collaboration_repository.go",
);
const mentionRepositoryPath = resolve(
  repositoryRoot,
  "backend/internal/store/psa/mention_repository.go",
);
const mentionMigrationPath = resolve(
  repositoryRoot,
  "backend/migrations/000089_internal_mentions.sql",
);
const eventCatalogPath = resolve(
  repositoryRoot,
  "docs/01-architecture/13-event-catalog.md",
);
const mentionModelPath = resolve(
  repositoryRoot,
  "backend/internal/mentions/model.go",
);
const mentionAcceptancePath = resolve(
  repositoryRoot,
  "docs/06-development/internal-mentions-acceptance.md",
);
const classificationDocumentPaths = [
  "docs/01-architecture/02-core-domain-model.md",
  "docs/01-architecture/06-workflow-engine.md",
  "docs/01-architecture/07-automation-engine.md",
  "docs/01-architecture/08-ai-platform.md",
  "docs/04-api/rest-api.md",
  "docs/08-modules/ticket-intelligence.md",
];

const requiredPSARoutes = [
  "/api/v1/prospects",
  "/api/v1/pipelines",
  "/api/v1/opportunities/{id}",
  "/api/v1/proposals/{id}/versions",
  "/api/v1/proposal-versions/{id}/accept",
  "/api/v1/opportunities/{id}/conversion-preview",
  "/api/v1/opportunities/{id}/convert",
  "/api/v1/projects",
  "/api/v1/change-orders/{id}/versions",
  "/api/v1/change-order-versions/{id}/approve",
  "/api/v1/change-order-versions/{id}/override-approval",
  "/api/v1/change-order-versions/{id}/apply",
];

const requiredEventFamilies = [
  "prospect.created",
  "pipeline.created",
  "opportunity.stage.changed",
  "proposal_version.issued",
  "proposal.accepted",
  "opportunity.converted",
  "project.created",
  "phase.updated",
  "resource_plan.created",
  "change_order.version.issued",
  "change_order.approval.decided",
  "change_order.applied",
];

const trackedDocuments = [
  "docs/04-api/psa-opportunities-projects.md",
  "docs/06-development/psa-acceptance.md",
];

const requiredRoadmapPhases = Array.from(
  { length: 9 },
  (_, phase) => `data-phase="${phase}"`,
);
const requiredRoadmapStates = [
  'data-state="complete"',
  'data-state="active-acceptance"',
  'data-gate-state="needs-human"',
  'data-gate-state="blocked-environment"',
];
const forbiddenRoadmapCopy = [
  "100% complete",
  "Technician platform",
  "Client + intelligence",
  "MSP operating system",
];
const requiredClassificationContracts = [
  "Type, Status, and Tags",
  "classification.apply",
  "classification.manage",
  "classification.report",
  "classification.ai.manage",
  "work_record",
  "knowledge_article",
  "time_entry",
  "Unclassified",
  "tag.added",
  "tag.removed",
  "projection_as_of",
  "forecast_category",
  "content_classification",
  "AI context classification",
  "billable/billing",
  "classification-preflight",
  "verified_no_op",
];

const requiredMentionContractDocuments = [
  "docs/03-security/permission-matrix.md",
  "docs/03-security/client-isolation-test-model.md",
  "docs/04-api/rest-api.md",
  "docs/01-architecture/13-event-catalog.md",
  "docs/01-architecture/08-ai-platform.md",
  "docs/08-modules/ticket-intelligence.md",
  "docs/07-ui-ux/information-architecture.md",
  "docs/06-development/internal-mentions-acceptance.md",
];

async function markdownPaths(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const paths = await Promise.all(
    entries.map(async (entry) => {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) {
        return markdownPaths(path);
      }
      return entry.isFile() && entry.name.endsWith(".md") ? [path] : [];
    }),
  );
  return paths.flat();
}

async function readable(path) {
  try {
    return await readFile(path, "utf8");
  } catch {
    throw new Error(`missing documentation file: ${relative(repositoryRoot, path)}`);
  }
}

const [contract, tracker, siteIndex, markdownFiles, classificationDocuments, restAPI, taggingRoutes, taggingReporting, mentionRoutes, collaborationRoutes, organizationRoutes, notificationRoutes, mentionModel, mentionService, collaborationRepository, mentionRepository, mentionMigration, eventCatalog, mentionAcceptance, mentionContractDocuments] = await Promise.all([
  readable(contractPath),
  readable(trackerPath),
  readable(siteIndexPath),
  markdownPaths(resolve(repositoryRoot, "docs")),
  Promise.all(
    classificationDocumentPaths.map((path) => readable(resolve(repositoryRoot, path))),
  ),
  readable(restAPIPath),
  readable(taggingRoutesPath),
  readable(taggingReportingPath),
  readable(mentionRoutesPath),
  readable(collaborationRoutesPath),
  readable(organizationRoutesPath),
  readable(notificationRoutesPath),
  readable(mentionModelPath),
  readable(mentionServicePath),
  readable(collaborationRepositoryPath),
  readable(mentionRepositoryPath),
  readable(mentionMigrationPath),
  readable(eventCatalogPath),
  readable(mentionAcceptancePath),
  Promise.all(requiredMentionContractDocuments.map((path) => readable(resolve(repositoryRoot, path)))),
]);

const policyRoute = taggingRoutes.match(
  /HandleFunc\("([A-Z]+ \/api\/v1\/classification\/ai-policy)",\s*r\.updateClassificationAIPolicy\)/,
)?.[1];
if (!policyRoute) {
  throw new Error("classification AI policy update route could not be derived");
}
const reportStruct = taggingReporting.match(/type Report struct \{([\s\S]*?)\n\}/)?.[1];
if (!reportStruct) {
  throw new Error("classification Report response schema could not be derived");
}
const reportResponseFields = [...reportStruct.matchAll(/`json:"([^",]+)/g)].map(
  (match) => match[1],
);
const projectionFreshnessFields = reportResponseFields.filter((field) =>
  /projection|stale|pending/.test(field),
);
const expectedFreshnessStatement = `The report response exposes only \`${projectionFreshnessFields.join(
  "`, `",
)}\` for projection freshness.`;
const normalizedRestAPI = restAPI.replace(/\s+/g, " ");
const classificationDocumentationErrors = [
  !restAPI.includes(`- \`${policyRoute}\``)
    ? `AI policy documentation does not match runtime route ${policyRoute}`
    : "",
  restAPI.includes("`PUT /api/v1/classification/ai-policy`")
    ? "obsolete PUT classification AI policy route is still documented"
    : "",
  !normalizedRestAPI.includes("An active Client is required for every report request.")
    ? "required active-Client report scope is not documented"
    : "",
  !normalizedRestAPI.includes(expectedFreshnessStatement)
    ? `report freshness schema is not documented as: ${expectedFreshnessStatement}`
    : "",
  !normalizedRestAPI.includes("The CLI detects persisted generic Category database columns; it does not scan application source.")
    ? "classification preflight CLI database-only Category scope is not documented"
    : "",
  !normalizedRestAPI.includes("The mandatory separate source gate `node scripts/validate-classification-cutover.mjs` structurally scans production Go, TypeScript, and SQL contracts.")
    ? "separate structural source-validator requirement is not documented"
    : "",
].filter(Boolean);

const runtimeMentionRoutes = [...mentionRoutes.matchAll(/HandleFunc\("([A-Z]+ \/api\/v1\/mentions\/[^" ]+(?:\/[^" ]+)*)"/g)]
  .map((match) => match[1])
  .sort();
const documentedMentionRoutes = runtimeMentionRoutes.filter((route) =>
  restAPI.includes(`\`${route}\``),
);
const parentTypes = [...mentionModel.matchAll(/Parent\w+\s+ParentType\s*=\s*"([^"]+)"/g)]
  .map((match) => match[1])
  .sort();
const sourceKinds = [...mentionModel.matchAll(/Source\w+\s+SourceKind\s*=\s*"([^"]+)"/g)]
  .map((match) => match[1])
  .sort();
const targetTypes = [...mentionModel.matchAll(/Target\w+\s+TargetType\s*=\s*"([^"]+)"/g)]
  .map((match) => match[1])
  .sort();
const mentionContract = mentionContractDocuments.join("\n").replace(/\s+/g, " ");

function fixedRoutes(source) {
  return [...source.matchAll(/HandleFunc\("([A-Z]+ \/api\/v1\/[^" ]+(?:\/[^" ]+)*)"/g)]
    .map((match) => match[1]);
}

function jsonFields(source, structName) {
  const body = source.match(new RegExp(`type ${structName} struct \\{([\\s\\S]*?)\\n\\}`))?.[1];
  if (!body) {
    throw new Error(`runtime DTO ${structName} could not be derived`);
  }
  return [...body.matchAll(/`json:"([^",]+)/g)].map((match) => match[1]);
}

const collaborationResources = collaborationRoutes.match(/\[\]string\{([^}]+)\}/)?.[1]
  ?.match(/"[^"]+"/g)
  ?.map((value) => value.slice(1, -1)) ?? [];
const collaborationTemplates = [...collaborationRoutes.matchAll(/HandleFunc\("([A-Z]+) \/api\/v1\/"\+resource\+"([^"]+)"/g)]
  .map((match) => ({ method: match[1], suffix: match[2] }));
const runtimeCollaborationRoutes = collaborationResources.flatMap((resource) =>
  collaborationTemplates.map(({ method, suffix }) => `${method} /api/v1/${resource}${suffix}`),
).concat(fixedRoutes(collaborationRoutes));
const runtimeTeamRoutes = fixedRoutes(organizationRoutes)
  .filter((route) => route.includes("/admin/teams/{id}/members"));
const runtimePreferenceRoutes = fixedRoutes(notificationRoutes)
  .filter((route) => route.includes("/notification-preferences/mentions"));
const runtimeInternalMentionRoutes = [
  ...runtimeCollaborationRoutes,
  ...runtimeMentionRoutes,
  ...runtimeTeamRoutes,
  ...runtimePreferenceRoutes,
].sort();
const undocumentedInternalMentionRoutes = runtimeInternalMentionRoutes.filter(
  (route) => !restAPI.includes(`\`${route}\``),
);
const requiredMentionDTOs = [
  [collaborationRoutes, "mentionTokenDTO"],
  [collaborationRoutes, "teamConfirmation"],
  [collaborationRoutes, "internalContentRequest"],
  [collaborationRoutes, "internalContentEditRequest"],
  [collaborationRoutes, "internalContentRedactRequest"],
  [collaborationRoutes, "internalContentResponse"],
  [mentionRoutes, "mentionItemStateRequest"],
  [mentionRoutes, "mentionResolveRequest"],
  [organizationRoutes, "replaceTeamMembersRequest"],
  [notificationRoutes, "updateMentionPreferenceRequest"],
];
const missingMentionDTOFields = requiredMentionDTOs.flatMap(([source, name]) =>
  jsonFields(source, name)
    .filter((field) => !restAPI.includes(`\`${field}\``) && !restAPI.includes(`"${field}"`))
    .map((field) => `${name}.${field}`),
);
const eventDataBody = mentionService.match(/Data: map\[string\]any\{([\s\S]*?)\n\t\t\},\n\t\}/)?.[1];
if (!eventDataBody) {
  throw new Error("mention.occurred runtime event data could not be derived");
}
const runtimeMentionEventFields = new Set([
  ...eventDataBody.matchAll(/"([a-z_]+)"\s*:/g),
].map((match) => match[1]));
for (const match of collaborationRepository.matchAll(/event\.Data\["([a-z_]+)"\]/g)) {
  runtimeMentionEventFields.add(match[1]);
}
const mentionEventSection = eventCatalog.match(/## `mention\.occurred` version 1([\s\S]*?)## Data policy/)?.[1] ?? "";
const undocumentedMentionEventFields = [...runtimeMentionEventFields]
  .filter((field) => !mentionEventSection.includes(`"${field}"`))
  .sort();
const occurrenceTable = mentionMigration.match(
  /CREATE TABLE mention_occurrences \(([\s\S]*?)\n\);/,
)?.[1] ?? "";
const occurrenceSchemaErrors = [
  !occurrenceTable.includes("token_id uuid NOT NULL")
    ? "mention occurrences do not retain the opaque token_id"
    : "",
  /\btoken\s+jsonb\b/.test(occurrenceTable)
    ? "mention occurrences retain forbidden token JSON"
    : "",
  /\btarget_label\b/.test(occurrenceTable)
    ? "mention occurrences retain a forbidden rendered target label"
    : "",
  !mentionRepository.includes("token->>'id'=occurrence.token_id::text")
    ? "mention reads do not resolve token_id against the current source"
    : "",
  !mentionMigration.includes("snapshot_occurrence_id uuid NOT NULL") ||
  !mentionMigration.includes("access_loss_confirmed boolean NOT NULL DEFAULT false") ||
  !mentionMigration.includes("authorization_revision bigint NOT NULL") ||
  !mentionMigration.includes("CREATE TABLE mention_access_revisions") ||
  !mentionMigration.includes("CREATE TABLE mention_access_loss_markers") ||
  !mentionMigration.includes("CREATE FUNCTION capture_mention_access_loss_marker") ||
  !mentionMigration.includes("CREATE FUNCTION mention_has_effective_access_at_revision") ||
  !mentionMigration.includes("CREATE INDEX mention_access_invalidations_confirmed_item_idx")
    ? "durable occurrence-scoped access-loss decision schema is incomplete"
    : "",
].filter(Boolean);
const mentionDocumentationErrors = [
  ...occurrenceSchemaErrors,
  runtimeMentionRoutes.length !== 4
    ? `expected 4 runtime mention routes, found ${runtimeMentionRoutes.length}`
    : "",
  documentedMentionRoutes.length !== runtimeMentionRoutes.length
    ? `undocumented runtime mention routes: ${runtimeMentionRoutes.filter((route) => !documentedMentionRoutes.includes(route)).join(", ")}`
    : "",
	undocumentedInternalMentionRoutes.length
	  ? `undocumented internal collaboration/team/preference routes: ${undocumentedInternalMentionRoutes.join(", ")}`
	  : "",
	missingMentionDTOFields.length
	  ? `undocumented internal mention DTO fields: ${missingMentionDTOFields.join(", ")}`
	  : "",
	undocumentedMentionEventFields.length
	  ? `mention.occurred schema omits runtime fields: ${undocumentedMentionEventFields.join(", ")}`
	  : "",
  !collaborationRoutes.includes('[]string{"work-records", "tasks", "projects"}')
    ? "internal collaboration parent route families changed"
    : "",
  parentTypes.join(",") !== "project,task,work_record"
    ? `mention parent types changed: ${parentTypes.join(",")}`
    : "",
  sourceKinds.join(",") !== "comment,details,note"
    ? `mention source kinds changed: ${sourceKinds.join(",")}`
    : "",
  targetTypes.join(",") !== "staff,team"
    ? `mention target types changed: ${targetTypes.join(",")}`
    : "",
  !parentTypes.every((parent) => mentionAcceptance.includes(`\`${parent}\``)) ||
  !sourceKinds.every((kind) => mentionAcceptance.toLowerCase().includes(kind))
    ? "acceptance matrix does not cover every runtime parent/source kind"
    : "",
  !mentionContract.includes("Dashboard widget is the only in-app mention delivery")
    ? "widget-only in-app delivery is not documented"
    : "",
  !mentionContract.includes("AI targets") || !mentionContract.includes("mention intents")
    ? "AI targets and mention intents are not explicitly excluded"
    : "",
  !mentionContract.includes("public tokens")
    ? "public mention tokens are not explicitly excluded"
    : "",
  !mentionContract.includes("exact parent/source/token hash link")
    ? "exact-source deep-link contract is not documented"
    : "",
  !mentionContract.includes("Occurrence rows store only opaque identifiers")
    ? "minimal occurrence token storage is not documented"
    : "",
  !mentionContract.includes("no source body, preview") ||
  !mentionContract.includes("token-adjacent text")
    ? "content-free event/log/metric/delivery contract is incomplete"
    : "",
].filter(Boolean);

const missingContracts = [...requiredPSARoutes, ...requiredEventFamilies].filter(
  (value) => !contract.includes(value),
);
const untrackedDocuments = trackedDocuments.filter(
  (value) => !tracker.includes(value),
);
const manifestMatch = tracker.match(
  /const documentPaths = `\n([\s\S]*?)\n`\.trim\(\)\.split\("\\n"\);/,
);
if (!manifestMatch) {
  throw new Error("docs-site document manifest could not be parsed");
}
const manifestDocuments = new Set(
  manifestMatch[1].split("\n").map((path) => path.trim()).filter(Boolean),
);
const repositoryDocuments = new Set(
  markdownFiles.map((path) => relative(repositoryRoot, path).split(sep).join("/")),
);
const missingFromManifest = [...repositoryDocuments]
  .filter((path) => !manifestDocuments.has(path))
  .sort();
const staleManifestEntries = [...manifestDocuments]
  .filter((path) => path.startsWith("docs/") && !repositoryDocuments.has(path))
  .sort();
const missingRoadmapMarkers = [
  ...requiredRoadmapPhases,
  ...requiredRoadmapStates,
].filter((value) => !siteIndex.includes(value));
const presentForbiddenRoadmapCopy = forbiddenRoadmapCopy.filter((value) =>
  siteIndex.includes(value),
);
const releaseAcceptanceErrors = releaseAcceptanceDocumentationErrors(siteIndex);
const classificationContract = classificationDocuments.join("\n");
const missingClassificationContracts = requiredClassificationContracts.filter(
  (value) => !classificationContract.includes(value),
);

if (
  missingContracts.length ||
  untrackedDocuments.length ||
  missingFromManifest.length ||
  staleManifestEntries.length ||
  missingRoadmapMarkers.length ||
  presentForbiddenRoadmapCopy.length ||
  releaseAcceptanceErrors.length ||
  missingClassificationContracts.length ||
  classificationDocumentationErrors.length ||
  mentionDocumentationErrors.length
) {
  if (missingContracts.length) {
    console.error(`missing PSA contracts:\n${missingContracts.join("\n")}`);
  }
  if (untrackedDocuments.length) {
    console.error(`untracked PSA documents:\n${untrackedDocuments.join("\n")}`);
  }
  if (missingFromManifest.length) {
    console.error(
      `documentation missing from docs-site manifest:\n${missingFromManifest.join("\n")}`,
    );
  }
  if (staleManifestEntries.length) {
    console.error(
      `stale docs-site manifest entries:\n${staleManifestEntries.join("\n")}`,
    );
  }
  if (missingRoadmapMarkers.length || presentForbiddenRoadmapCopy.length) {
    console.error(
      [
        "roadmap status board contract invalid:",
        ...missingRoadmapMarkers.map((value) => `missing ${value}`),
        ...presentForbiddenRoadmapCopy.map((value) => `forbidden ${value}`),
      ].join("\n"),
    );
  }
  if (releaseAcceptanceErrors.length) {
    console.error(
      `release acceptance documentation invalid:\n${releaseAcceptanceErrors.join("\n")}`,
    );
  }
  if (missingClassificationContracts.length) {
    console.error(
      `missing governed classification documentation:\n${missingClassificationContracts.join("\n")}`,
    );
  }
  if (classificationDocumentationErrors.length) {
    console.error(
      `classification route/schema documentation invalid:\n${classificationDocumentationErrors.join("\n")}`,
    );
  }
  if (mentionDocumentationErrors.length) {
    console.error(
      `internal mentions documentation invalid:\n${mentionDocumentationErrors.join("\n")}`,
    );
  }
  process.exitCode = 1;
} else {
  console.log(
    `Documentation contract valid: ${requiredPSARoutes.length} PSA routes, ${requiredEventFamilies.length} PSA events, ${requiredClassificationContracts.length} classification contracts, ${runtimeMentionRoutes.length} mention routes, ${repositoryDocuments.size} site documents.`,
  );
}

// Keep the calendar browser, source, and authorization contracts discoverable.
const calendarContract = await readFile(resolve(repositoryRoot, "docs/02-platform/unified-calendar.md"), "utf8");
const calendarSources = await Promise.all(["calendar_routes.go", "commitment_routes.go", "workforce_schedule_routes.go"].map(file => readFile(resolve(repositoryRoot, "backend/internal/httpapi", file), "utf8")));
const calendarRoutePaths = [...new Set(calendarSources.flatMap(source => [...source.matchAll(/HandleFunc\("(?:GET|POST|PUT|PATCH|DELETE) \/api\/v1([^" ]+)/g)].map(match => match[1])))];
const missingCalendarContracts = [...calendarRoutePaths, "calendar.read", "calendar.schedule", "calendar.commitment.manage", "calendar.policy.manage", "calendar.workforce.manage", "calendar-reconcile", "calendar-demo-seed"].filter(value => !calendarContract.includes(value));
if (missingCalendarContracts.length) {
 console.error(`Missing calendar contracts: ${missingCalendarContracts.join(", ")}`);
 process.exitCode = 1;
}

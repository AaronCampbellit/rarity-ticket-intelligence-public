import { readFile } from "node:fs/promises";
import { resolve } from "node:path";

const root = resolve(import.meta.dirname, "..");
const contractPath = resolve(
  root,
  "docs/04-api/integration-automation-ai-contracts.md",
);
const trackerPath = resolve(root, "docs-site/app.js");
const contract = await readFile(contractPath, "utf8");
const tracker = await readFile(trackerPath, "utf8");

const requiredContracts = [
  "rsk_",
  "HMAC-SHA-256",
  "five minutes",
  "15 minutes",
  "24 hours",
  "read/ingest-only",
  "idempotency key",
  "Depth is capped at eight",
  "pending_human",
  "applied: false",
  "sent: false",
  "ollama",
  "openai_compatible",
  "5 MiB",
  "60 minutes",
  "POST /api/v1/ai/providers",
  "POST /api/v1/work-records/{id}/ai/jobs",
];

const trackedDocuments = [
  "docs/04-api/integration-automation-ai-contracts.md",
  "docs/06-development/integration-automation-ai-acceptance.md",
];

const missing = requiredContracts.filter((value) => !contract.includes(value));
const untracked = trackedDocuments.filter((value) => !tracker.includes(value));
if (missing.length || untracked.length) {
  if (missing.length) console.error(`missing contracts:\n${missing.join("\n")}`);
  if (untracked.length) console.error(`untracked documents:\n${untracked.join("\n")}`);
  process.exitCode = 1;
} else {
  console.log(
    `Integration/automation documentation valid: ${requiredContracts.length} contracts, ${trackedDocuments.length} tracked documents.`,
  );
}

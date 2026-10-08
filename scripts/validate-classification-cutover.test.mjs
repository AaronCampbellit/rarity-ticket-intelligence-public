import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { validateClassificationCutover } from "./validate-classification-cutover.mjs";

const preservedContracts = {
  "backend/internal/sales/model.go": `package sales
type ForecastCategory string
type PipelineStage struct { Category ForecastCategory \`json:"forecast_category"\` }
`,
  "backend/internal/notifications/management.go": `package notifications
type Destination struct { ContentClassification string \`json:"content_classification"\` }
`,
  "backend/internal/aiassist/service.go": `package aiassist
type ContextClassification string
type ContextField struct { Classification ContextClassification }
`,
  "backend/internal/billingexport/export.go": `package billingexport
type Entry struct { Billable bool \`json:"billable"\` }
`,
  "backend/migrations/000010_psa_sales.sql": `CREATE TABLE pipeline_stages (id uuid, forecast_category text NOT NULL);`,
  "backend/migrations/000005_workflow_experience.sql": `CREATE TABLE notification_deliveries (id uuid, content_classification text NOT NULL);`,
  "backend/migrations/000004_work_management.sql": `CREATE TABLE time_entries (id uuid, billable boolean NOT NULL);`,
};

async function fixture(files) {
  const root = await mkdtemp(join(tmpdir(), "classification-cutover-"));
  for (const [path, contents] of Object.entries(files)) {
    const target = join(root, path);
    await mkdir(join(target, ".."), { recursive: true });
    await writeFile(target, contents);
  }
  return root;
}

test("rejects generic Category contracts on supported objects", async (t) => {
  const root = await fixture({
    ...preservedContracts,
    "backend/work.go": `package work; type WorkRecord struct { Category string \`json:"category"\` }`,
    "frontend/task.ts": `export type Task = { category?: string }`,
    "backend/migrations/999999_bad.sql": `ALTER TABLE assets ADD COLUMN category text;`,
  });
  t.after(() => rm(root, { recursive: true, force: true }));

  const result = await validateClassificationCutover(root);

  assert.equal(result.ok, false);
  assert.equal(result.violations.length, 3);
  assert.deepEqual(
    result.violations.map(({ path }) => path).sort(),
    ["backend/migrations/999999_bad.sql", "backend/work.go", "frontend/task.ts"],
  );
});

test("rejects generic Category in composed DTOs, requests, schemas, anonymous responses, and API paths", async (t) => {
  const root = await fixture({
    ...preservedContracts,
    "backend/contracts.go": `
package contracts
type WorkRecordDTO struct { Category string }
type Nested struct { Value struct { Category string \`json:"category"\` } }
type Safe struct { Nested struct { Value string } }
`,
    "frontend/contracts.ts": `
interface SharedClassification { category?: string }
export type CreateTaskRequest = SharedClassification & { title: string };
const schema = z.object({ category: z.string() });
export const response = () => ({ category: "legacy", id: "one" });
export const route = "/api/v1/work-records/one/category";
`,
  });
  t.after(() => rm(root, { recursive: true, force: true }));

  const result = await validateClassificationCutover(root);

  assert.equal(result.ok, false);
  assert.deepEqual(
    result.violations.map(({ path }) => path).sort(),
    ["backend/contracts.go", "backend/contracts.go", "frontend/contracts.ts", "frontend/contracts.ts", "frontend/contracts.ts", "frontend/contracts.ts"],
  );
});

test("permits forecast, content, AI-context, and billing classification contracts", async (t) => {
  const root = await fixture({
    ...preservedContracts,
  });
  t.after(() => rm(root, { recursive: true, force: true }));

  const result = await validateClassificationCutover(root);

  assert.deepEqual(result.missingPreservations, []);
  assert.deepEqual(result.violations, []);
  assert.equal(result.ok, true);
});

for (const [name, pattern] of [
  ["forecast classification", /forecast_category/g],
  ["content classification", /content_classification/g],
  ["AI context classification", /ContextClassification/g],
  ["billing classification", /Billable|billable/g],
]) {
  test(`fails when ${name} is no longer preserved`, async (t) => {
    const files = Object.fromEntries(
      Object.entries(preservedContracts).map(([path, source]) => [path, source.replace(pattern, "removed")]),
    );
    const root = await fixture(files);
    t.after(() => rm(root, { recursive: true, force: true }));

    const result = await validateClassificationCutover(root);

    assert.equal(result.ok, false);
    assert.equal(result.missingPreservations.length, 1);
  });
}

for (const [name, pattern, testOnlySource] of [
  ["forecast classification", /forecast_category/g, `package preserved; type PipelineStageTestFixture struct { Category string \`json:"forecast_category"\` }`],
  ["content classification", /content_classification/g, `package preserved; type DestinationTestFixture struct { Value string \`json:"content_classification"\` }`],
  ["AI context classification", /ContextClassification/g, `package preserved; type ContextClassification string`],
  ["billing classification", /Billable|billable/g, `package preserved; type EntryTestFixture struct { Billable bool \`json:"billable"\` }`],
]) {
  test(`test-only ${name} token cannot satisfy the production preservation contract`, async (t) => {
    const files = Object.fromEntries(
      Object.entries(preservedContracts).map(([path, source]) => [path, source.replace(pattern, "removed")]),
    );
    files["backend/preserved_test.go"] = testOnlySource;
    const root = await fixture(files);
    t.after(() => rm(root, { recursive: true, force: true }));

    const result = await validateClassificationCutover(root);

    assert.equal(result.ok, false);
    assert.deepEqual(result.missingPreservations, [
      name === "forecast classification" ? "forecast_category" :
        name === "content classification" ? "content_classification" :
          name === "AI context classification" ? "ai_context_classification" :
            "billing_classification",
    ]);
  });
}

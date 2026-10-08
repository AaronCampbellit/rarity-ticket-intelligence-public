import assert from "node:assert/strict";
import test from "node:test";

import { releaseAcceptanceDocumentationErrors } from "./validate-release-acceptance-docs.mjs";

const expectedCardinalityError =
  "expected exactly one signed release artifact gate, found 0";
const expectedStateError =
  "signed release artifact gate is not recorded as complete";

test("accepts one complete signed release artifact gate", () => {
  const errors = releaseAcceptanceDocumentationErrors(`
    <li class="gate" data-gate-state="complete" data-gate-id="signed-release-artifacts">
      Signed release artifacts
    </li>
  `);

  assert.deepEqual(errors, []);
});

test("rejects a pending signed release artifact gate", () => {
  const errors = releaseAcceptanceDocumentationErrors(`
    <li data-gate-id="signed-release-artifacts" data-gate-state="blocked-environment">
      Signed release artifacts
    </li>
  `);

  assert.deepEqual(errors, [expectedStateError]);
});

test("ignores a complete marker inside an HTML comment", () => {
  const errors = releaseAcceptanceDocumentationErrors(`
    <!-- <li data-gate-id="signed-release-artifacts" data-gate-state="complete"></li> -->
    <li data-gate-id="signed-release-artifacts" data-gate-state="blocked-environment"></li>
  `);

  assert.deepEqual(errors, [expectedStateError]);
});

test("does not treat prefixed attributes as release gate attributes", () => {
  const errors = releaseAcceptanceDocumentationErrors(`
    <li not-data-gate-id="signed-release-artifacts" not-data-gate-state="complete"></li>
  `);

  assert.deepEqual(errors, [expectedCardinalityError]);
});

test("rejects duplicate signed release artifact gates", () => {
  const errors = releaseAcceptanceDocumentationErrors(`
    <li data-gate-id="signed-release-artifacts" data-gate-state="complete"></li>
    <li data-gate-id="signed-release-artifacts" data-gate-state="complete"></li>
  `);

  assert.deepEqual(errors, [
    "expected exactly one signed release artifact gate, found 2",
  ]);
});

test("rejects a missing signed release artifact gate", () => {
  assert.deepEqual(releaseAcceptanceDocumentationErrors("<ol></ol>"), [
    expectedCardinalityError,
  ]);
});

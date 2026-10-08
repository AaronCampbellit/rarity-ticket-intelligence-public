import { createHash } from "node:crypto";

import type { Page, Route } from "@playwright/test";

const CLIENT_ID = "client-id";
const OPPORTUNITY_ID = "opportunity-1";
const PROPOSAL_ID = "proposal-1";
const PROPOSAL_VERSION_ID = "proposal-version-1";
const PROJECT_ID = "project-1";
const CHANGE_ORDER_VERSION_ID = "change-order-version-1";

// Property order, omitted empty Client fields, and zero times mirror Go's
// json.Marshal(projects.ConversionPreview) input before Hash is populated.
const conversionPreviewSnapshot = {
  opportunity_id: OPPORTUNITY_ID,
  proposal_version_id: PROPOSAL_VERSION_ID,
  client: {
    action: "match",
    client_id: CLIENT_ID,
  },
  project_display_id: "PRJ-PROP-001",
  project_name: "Security modernization",
  planned_start: "0001-01-01T00:00:00Z",
  planned_end: "0001-01-01T00:00:00Z",
  phases: [
    {
      position: 1,
      name: "Delivery",
      proposal_line_ids: ["proposal-line-1"],
      planned_minutes: 2400,
      budget: { minor: 10000000, currency: "USD" },
    },
  ],
  tasks: [{ id: "kickoff", version: 2 }],
  original_baseline: {
    currency: "USD",
    revenue_minor: 10000000,
    cost_minor: 6000000,
    planned_minutes: 2400,
  },
  original_budget: { minor: 10000000, currency: "USD" },
  planned_minutes: 2400,
};
const CONVERSION_PREVIEW_HASH = createHash("sha256")
  .update(JSON.stringify(conversionPreviewSnapshot))
  .digest("hex");

type JSONRecord = Record<string, unknown>;

export type OperationalFixtureEvidence = {
  scopedRequests: string[];
  failures: string[];
  projectDetailRequests: number;
  overrideBodies: JSONRecord[];
  applyBodies: JSONRecord[];
  previewBodies: JSONRecord[];
  conversionBodies: JSONRecord[];
};

function evidence(): OperationalFixtureEvidence {
  return {
    scopedRequests: [],
    failures: [],
    projectDetailRequests: 0,
    overrideBodies: [],
    applyBodies: [],
    previewBodies: [],
    conversionBodies: [],
  };
}

function requestTarget(route: Route): string {
  const url = new URL(route.request().url());
  return `${url.pathname}${url.search}`;
}

async function reject(
  route: Route,
  fixture: OperationalFixtureEvidence,
  message: string,
) {
  fixture.failures.push(message);
  await route.abort("failed");
}

function normalizedJSON(value: unknown): string {
  if (Array.isArray(value)) {
    return `[${value.map(normalizedJSON).join(",")}]`;
  }
  if (value && typeof value === "object") {
    const record = value as Record<string, unknown>;
    return `{${Object.keys(record)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${normalizedJSON(record[key])}`)
      .join(",")}}`;
  }
  return JSON.stringify(value);
}

function sameJSON(actual: unknown, expected: unknown): boolean {
  return normalizedJSON(actual) === normalizedJSON(expected);
}

async function validateRequest(
  route: Route,
  fixture: OperationalFixtureEvidence,
  expectedMethod: "GET" | "POST",
  options: { scoped?: boolean; body?: JSONRecord } = {},
): Promise<JSONRecord | undefined> {
  const request = route.request();
  const target = requestTarget(route);
  if (request.method() !== expectedMethod) {
    await reject(
      route,
      fixture,
      `${target}: expected ${expectedMethod}, received ${request.method()}`,
    );
    return undefined;
  }
  if (options.scoped) {
    if (request.headers()["x-rarity-client-id"] !== CLIENT_ID) {
      await reject(route, fixture, `${target}: invalid client scope`);
      return undefined;
    }
    fixture.scopedRequests.push(`${expectedMethod} ${target}`);
  }
  if (!options.body) return {};
  let body: JSONRecord;
  try {
    body = request.postDataJSON() as JSONRecord;
  } catch {
    await reject(route, fixture, `${target}: malformed JSON body`);
    return undefined;
  }
  if (!sameJSON(body, options.body)) {
    await reject(route, fixture, `${target}: unexpected JSON body`);
    return undefined;
  }
  return body;
}

async function fulfillJSON(
  route: Route,
  body: unknown,
  status = 200,
  headers?: Record<string, string>,
) {
  await route.fulfill({
    status,
    contentType: "application/json",
    headers,
    body: JSON.stringify(body),
  });
}

const opportunity = {
  id: OPPORTUNITY_ID,
  client_id: CLIENT_ID,
  pipeline_id: "pipeline-1",
  stage_id: "stage-accepted",
  display_id: "OPP-001",
  name: "Northwind security modernization",
  amount: { minor: 10000000, currency: "USD" },
  fields: { expected_close_on: "2026-09-30" },
  custom_fields: { engagement: "security modernization" },
  team_id: "security-team",
  contact_ids: ["jordan-lee"],
  proposal_issued: true,
  approval_granted: true,
  version: 4,
  updated_at: "2026-08-18T00:00:00Z",
};

const proposal = {
  id: PROPOSAL_ID,
  client_id: CLIENT_ID,
  opportunity_id: OPPORTUNITY_ID,
  display_id: "PROP-001",
  current_version: 1,
  current_version_id: PROPOSAL_VERSION_ID,
  state: "accepted",
  version: 2,
  updated_at: "2026-08-18T00:00:00Z",
};

const proposalVersion = {
  id: PROPOSAL_VERSION_ID,
  proposal_id: PROPOSAL_ID,
  version: 1,
  state: "accepted",
  currency: "USD",
  lines: [
    {
      id: "proposal-line-1",
      type: "fixed_fee",
      description: "Security modernization delivery",
      quantity: 1,
      unit_price: { minor: 10000000, currency: "USD" },
      unit_cost: { minor: 6000000, currency: "USD" },
      discount: { minor: 0, currency: "USD" },
      tax_treatment: "tax_exempt",
      tax: { minor: 0, currency: "USD" },
      planned_minutes: 2400,
    },
  ],
  subtotal: { minor: 10000000, currency: "USD" },
  tax_total: { minor: 0, currency: "USD" },
  total: { minor: 10000000, currency: "USD" },
  cost: { minor: 6000000, currency: "USD" },
  margin: { minor: 4000000, currency: "USD" },
  requires_internal_approval: false,
};

function financials(currentBudgetMinor: number) {
  return {
    original_budget: { minor: 10000000, currency: "USD" },
    current_budget: { minor: currentBudgetMinor, currency: "USD" },
    planned_labor: { minor: 6000000, currency: "USD" },
    actual_labor: { minor: 1000000, currency: "USD" },
    cost_actuals: { minor: 500000, currency: "USD" },
    committed_cost: { minor: 250000, currency: "USD" },
    billable_work: { minor: 3000000, currency: "USD" },
    profit: { minor: 1500000, currency: "USD" },
    projected_profit: { minor: currentBudgetMinor - 6500000, currency: "USD" },
    margin_basis_points: 3500,
    actual_labor_complete: true,
    profit_available: true,
  };
}

function projectSummary(scheduled: boolean) {
  return {
    id: PROJECT_ID,
    display_id: "PRJ-PROP-001",
    name: "Security modernization",
    lifecycle_state: "active",
    ...(scheduled
      ? { planned_start: "2026-09-01", planned_end: "2026-09-30" }
      : {}),
    version: 1,
  };
}

function projectWorkspace(
  changeOrderState?: "issued" | "approved" | "applied",
  scheduled = true,
) {
  const applied = changeOrderState === "applied";
  const approved = changeOrderState === "approved" || applied;
  const orderVersion = changeOrderState === "issued" ? 5 : applied ? 7 : 6;
  const currentBudgetMinor = applied ? 11500000 : 10000000;
  return {
    id: PROJECT_ID,
    display_id: "PRJ-PROP-001",
    name: "Security modernization",
    client_name: "Northwind Legal",
    lifecycle_state: "active",
    ...(scheduled
      ? { planned_start: "2026-09-01", planned_end: "2026-09-30" }
      : {}),
    original_proposal_version: 1,
    original_baseline: {
      currency: "USD",
      revenue_minor: 10000000,
      cost_minor: 6000000,
      planned_minutes: 2400,
    },
    current_baseline: {
      currency: "USD",
      revenue_minor: currentBudgetMinor,
      cost_minor: applied ? 6750000 : 6000000,
      planned_minutes: applied ? 2700 : 2400,
    },
    phases: [
      {
        id: "phase-1",
        position: 1,
        name: "Delivery",
        state: "planned",
        owner_id: "marcus-reed",
        participating_teams: ["Security engineering"],
        ...(scheduled
          ? { planned_start: "2026-09-01", planned_end: "2026-09-30" }
          : {}),
        planned_minutes: 2400,
        actual_minutes: 600,
        budget: { minor: 10000000, currency: "USD" },
        deliverables: ["Modernized security controls"],
        completion_criteria: ["Customer acceptance recorded"],
        tasks: [],
        version: 1,
      },
    ],
    project_tasks: [
      {
        id: "kickoff",
        title: "Schedule kickoff",
        status: "open",
        owner_id: "marcus-reed",
        owner_name: "Marcus Reed",
        subtasks: 0,
        estimate_minutes: 120,
        actual_minutes: 0,
        version: 2,
      },
    ],
    resource_plans: [],
    cost_actuals: [],
    financials: financials(currentBudgetMinor),
    capacity: scheduled
      ? [
          {
            id: "marcus-reed",
            name: "Marcus Reed",
            available_minutes: 2400,
            scheduled_minutes: 3000,
            actual_minutes: 600,
            remaining_minutes: 0,
            overbooked_minutes: 1200,
          },
          {
            id: "avery-chen",
            name: "Avery Chen",
            available_minutes: 4800,
            scheduled_minutes: 2400,
            actual_minutes: 0,
            remaining_minutes: 2400,
            overbooked_minutes: 0,
          },
        ]
      : [],
    change_orders: changeOrderState
      ? [
          {
            order: {
              id: "change-order-1",
              display_id: "CO-001",
              state: changeOrderState,
              version: orderVersion,
            },
            current_version: {
              id: CHANGE_ORDER_VERSION_ID,
              version: 1,
              description: "Expand the security modernization scope",
              currency: "USD",
              revenue_delta_minor: 1500000,
              cost_delta_minor: 750000,
              labor_delta_minutes: 300,
            },
            ...(approved
              ? {
                  decision: {
                    id: "change-order-decision-1",
                    decision: "approved",
                    override: true,
                    reason: "Customer approval recorded in steering call",
                    decided_by: "e2e-platform-administrator",
                    decided_at: "2026-08-18T12:00:00Z",
                  },
                }
              : {}),
          },
        ]
      : [],
    financials_visible: true,
    version: applied ? 2 : 1,
  };
}

const issuedChangeOrderVersion = {
  id: CHANGE_ORDER_VERSION_ID,
  change_order_id: "change-order-1",
  msp_id: "msp-1",
  client_id: CLIENT_ID,
  version: 1,
  description: "Expand the security modernization scope",
  currency: "USD",
  revenue_delta_minor: 1500000,
  cost_delta_minor: 750000,
  labor_delta_minutes: 300,
  issued_at: "2026-08-18T11:00:00Z",
  issued_by: "e2e-platform-administrator",
};

function appliedProject() {
  return {
    ID: PROJECT_ID,
    MSPID: "msp-1",
    ClientID: CLIENT_ID,
    DisplayID: "PRJ-PROP-001",
    Name: "Security modernization",
    OriginalProposalVersionID: PROPOSAL_VERSION_ID,
    LifecycleState: "active",
    PlannedStart: "2026-09-01T00:00:00Z",
    PlannedEnd: "2026-09-30T00:00:00Z",
    OriginalBaseline: {
      currency: "USD",
      revenue_minor: 10000000,
      cost_minor: 6000000,
      planned_minutes: 2400,
    },
    CurrentBaseline: {
      currency: "USD",
      revenue_minor: 11500000,
      cost_minor: 6750000,
      planned_minutes: 2700,
    },
    Version: 2,
    Phases: [],
    SupportsMilestones: false,
    SupportsTaskDependencies: false,
    CreatedAt: "2026-08-18T00:00:00Z",
    CreatedBy: "e2e-platform-administrator",
  };
}

async function registerProjectRoutes(
  page: Page,
  fixture: OperationalFixtureEvidence,
  options: {
    changeOrder?: boolean;
    requireConversion?: () => boolean;
    scheduled?: boolean;
  } = {},
) {
  const scheduled = options.scheduled ?? true;
  let changeOrderState: "issued" | "approved" | "applied" | undefined =
    options.changeOrder ? "issued" : undefined;

  await page.route(/\/api\/v1\/projects\?limit=100$/, async (route) => {
    if (!(await validateRequest(route, fixture, "GET", { scoped: true })))
      return;
    await fulfillJSON(
      route,
      options.requireConversion && !options.requireConversion()
        ? []
        : [projectSummary(scheduled)],
    );
  });
  await page.route(
    new RegExp(`/api/v1/projects/${PROJECT_ID}$`),
    async (route) => {
      if (!(await validateRequest(route, fixture, "GET", { scoped: true })))
        return;
      if (options.requireConversion && !options.requireConversion()) {
        await reject(
          route,
          fixture,
          "project detail requested before conversion",
        );
        return;
      }
      fixture.projectDetailRequests += 1;
      await fulfillJSON(route, projectWorkspace(changeOrderState, scheduled));
    },
  );
  if (!options.changeOrder) return;
  await page.route(
    new RegExp(
      `/api/v1/change-order-versions/${CHANGE_ORDER_VERSION_ID}/override-approval$`,
    ),
    async (route) => {
      const expected = {
        expected_version: 5,
        reason: "Customer approval recorded in steering call",
      };
      const body = await validateRequest(route, fixture, "POST", {
        scoped: true,
        body: expected,
      });
      if (!body) return;
      fixture.overrideBodies.push(body);
      if (changeOrderState !== "issued") {
        await reject(route, fixture, "change-order override repeated");
        return;
      }
      changeOrderState = "approved";
      await fulfillJSON(route, issuedChangeOrderVersion, 200, {
        ETag: '"1"',
      });
    },
  );
  await page.route(
    new RegExp(
      `/api/v1/change-order-versions/${CHANGE_ORDER_VERSION_ID}/apply$`,
    ),
    async (route) => {
      const body = await validateRequest(route, fixture, "POST", {
        scoped: true,
        body: { expected_version: 6 },
      });
      if (!body) return;
      fixture.applyBodies.push(body);
      if (changeOrderState !== "approved") {
        await reject(route, fixture, "change order applied before approval");
        return;
      }
      changeOrderState = "applied";
      await fulfillJSON(route, appliedProject(), 200, { ETag: '"2"' });
    },
  );
}

export async function installProjectCapacityFixtures(
  page: Page,
): Promise<OperationalFixtureEvidence> {
  const fixture = evidence();
  await registerProjectRoutes(page, fixture);
  return fixture;
}

export async function installChangeOrderFixtures(
  page: Page,
): Promise<OperationalFixtureEvidence> {
  const fixture = evidence();
  await registerProjectRoutes(page, fixture, { changeOrder: true });
  return fixture;
}

export async function installOpportunityToProjectFixtures(
  page: Page,
): Promise<OperationalFixtureEvidence> {
  const fixture = evidence();
  let converted = false;

  await page.route(/\/api\/v1\/opportunities\?limit=100$/, async (route) => {
    if (!(await validateRequest(route, fixture, "GET", { scoped: true })))
      return;
    await fulfillJSON(route, [opportunity]);
  });
  await page.route(/\/api\/v1\/pipelines$/, async (route) => {
    if (!(await validateRequest(route, fixture, "GET"))) return;
    await fulfillJSON(route, [
      {
        id: "pipeline-1",
        key: "managed-services",
        name: "Managed services",
        stages: [
          {
            id: "stage-accepted",
            pipeline_id: "pipeline-1",
            key: "accepted",
            name: "Accepted",
            position: 4,
            probability: 100,
            forecast_category: "closed_won",
            required_fields: [],
            allowed_next_stage_ids: [],
            requires_proposal: true,
            requires_approval: true,
          },
        ],
      },
    ]);
  });
  await page.route(/\/api\/v1\/opportunity-forecast$/, async (route) => {
    if (!(await validateRequest(route, fixture, "GET", { scoped: true })))
      return;
    await fulfillJSON(route, [
      {
        pipeline_id: "pipeline-1",
        stage_id: "stage-accepted",
        stage_name: "Accepted",
        forecast_category: "closed_won",
        probability: 100,
        opportunity_count: 1,
        amount: { minor: 10000000, currency: "USD" },
        weighted_amount: { minor: 10000000, currency: "USD" },
      },
    ]);
  });
  await page.route(
    new RegExp(
      `/api/v1/opportunities/${OPPORTUNITY_ID}/activities\\?limit=100$`,
    ),
    async (route) => {
      if (!(await validateRequest(route, fixture, "GET", { scoped: true })))
        return;
      await fulfillJSON(route, [
        {
          id: "activity-1",
          opportunity_id: OPPORTUNITY_ID,
          kind: "meeting",
          summary: "Customer accepted the commercial baseline",
          details: "Acceptance recorded by Jordan Lee.",
          occurred_at: "2026-08-18T00:00:00Z",
        },
      ]);
    },
  );
  await page.route(
    new RegExp(`/api/v1/opportunities/${OPPORTUNITY_ID}/tasks$`),
    async (route) => {
      if (!(await validateRequest(route, fixture, "GET", { scoped: true })))
        return;
      await fulfillJSON(route, [
        {
          id: "kickoff",
          title: "Schedule kickoff",
          status: "open",
          position: 1,
          version: 2,
        },
        {
          id: "qualified",
          title: "Qualify prospect",
          status: "completed",
          position: 2,
          version: 3,
        },
      ]);
    },
  );
  await page.route(
    new RegExp(`/api/v1/opportunities/${OPPORTUNITY_ID}/attachments$`),
    async (route) => {
      if (!(await validateRequest(route, fixture, "GET", { scoped: true })))
        return;
      await fulfillJSON(route, [
        {
          id: "attachment-1",
          opportunity_id: OPPORTUNITY_ID,
          filename: "accepted-scope.pdf",
          content_type: "application/pdf",
          size_bytes: 4096,
          sha256: "fixture-sha256",
          version: 1,
          created_at: "2026-08-18T00:00:00Z",
        },
      ]);
    },
  );
  await page.route(/\/api\/v1\/proposals\?limit=100$/, async (route) => {
    if (!(await validateRequest(route, fixture, "GET", { scoped: true })))
      return;
    await fulfillJSON(route, [proposal]);
  });
  await page.route(
    new RegExp(`/api/v1/proposal-versions/${PROPOSAL_VERSION_ID}$`),
    async (route) => {
      if (!(await validateRequest(route, fixture, "GET", { scoped: true })))
        return;
      await fulfillJSON(route, proposalVersion);
    },
  );

  const previewRequest = {
    expected_version: 4,
    accepted_proposal_version_id: PROPOSAL_VERSION_ID,
    existing_client_id: CLIENT_ID,
    project_display_id: "PRJ-PROP-001",
    project_name: "Security modernization",
    phases: [
      {
        name: "Delivery",
        proposal_line_ids: ["proposal-line-1"],
      },
    ],
    selected_task_ids: ["kickoff"],
    task_versions: { kickoff: 2 },
  };
  await page.route(
    new RegExp(`/api/v1/opportunities/${OPPORTUNITY_ID}/conversion-preview$`),
    async (route) => {
      const body = await validateRequest(route, fixture, "POST", {
        scoped: true,
        body: previewRequest,
      });
      if (!body) return;
      fixture.previewBodies.push(body);
      await fulfillJSON(route, {
        ...conversionPreviewSnapshot,
        hash: CONVERSION_PREVIEW_HASH,
      });
    },
  );
  await page.route(
    new RegExp(`/api/v1/opportunities/${OPPORTUNITY_ID}/convert$`),
    async (route) => {
      const request = route.request();
      if (request.method() !== "POST") {
        await reject(route, fixture, "conversion: expected POST");
        return;
      }
      if (request.headers()["x-rarity-client-id"] !== CLIENT_ID) {
        await reject(route, fixture, "conversion: invalid client scope");
        return;
      }
      fixture.scopedRequests.push(`POST ${requestTarget(route)}`);
      let body: JSONRecord;
      try {
        body = request.postDataJSON() as JSONRecord;
      } catch {
        await reject(route, fixture, "conversion: malformed JSON body");
        return;
      }
      const { idempotency_key: idempotencyKey, ...stableBody } = body;
      if (
        typeof idempotencyKey !== "string" ||
        !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(
          idempotencyKey,
        ) ||
        !sameJSON(stableBody, {
          ...previewRequest,
          preview_hash: CONVERSION_PREVIEW_HASH,
        })
      ) {
        await reject(route, fixture, "conversion: unexpected JSON body");
        return;
      }
      fixture.conversionBodies.push(body);
      converted = true;
      await fulfillJSON(
        route,
        {
          project_id: PROJECT_ID,
          client_id: CLIENT_ID,
          conversion_id: "conversion-1",
          already_exists: false,
        },
        201,
      );
    },
  );

  await registerProjectRoutes(page, fixture, {
    requireConversion: () => converted,
    scheduled: false,
  });
  return fixture;
}

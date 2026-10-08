import { describe, expect, it } from "vitest";

import {
  availableWorkspaceCatalog,
  catalogGroups,
  resourceKindsForAction,
} from "./workspaceCatalog";

describe("workspace catalog", () => {
  it("filters actions by the principal capabilities without requiring an existing Client", () => {
    const actions = availableWorkspaceCatalog(
      new Set(["client.create", "project.read"]),
    );

    expect(actions.map((action) => action.value)).toEqual([
      "project_search",
      "project_get",
      "client",
    ]);
    expect(catalogGroups).toEqual([
      "Read",
      "Work",
      "Client resources",
      "Knowledge",
      "Sales",
    ]);
  });

  it("uses resource-kind capabilities for create, update, and lifecycle actions", () => {
    const capabilities = new Set([
      "contact.create",
      "asset.update",
      "service.lifecycle",
    ]);

    expect(
      resourceKindsForAction("client_resource_add", capabilities).map(
        ({ kind }) => kind,
      ),
    ).toEqual(["contact"]);
    expect(
      resourceKindsForAction("client_resource_update", capabilities).map(
        ({ kind }) => kind,
      ),
    ).toEqual(["asset"]);
    expect(
      resourceKindsForAction("client_resource_deactivate", capabilities).map(
        ({ kind }) => kind,
      ),
    ).toEqual(["service"]);
  });

  it("exposes only the approved second-wave actions supported by the principal", () => {
    const actions = availableWorkspaceCatalog(
      new Set([
        "work_record.create",
        "work_record.assign",
        "opportunity.read",
        "opportunity.transition",
        "opportunity.activity.create",
        "proposal.read",
        "proposal.create",
        "knowledge.publish",
      ]),
    );

    const approvedSecondWave = [
      "ticket_create",
      "ticket_assign",
      "opportunity_list",
      "opportunity_get",
      "proposal_list",
      "proposal_get",
      "opportunity_transition",
      "opportunity_activity_create",
      "proposal_create",
      "knowledge_publish",
    ] as const;
    const exposedSecondWave = actions
      .map((action) => action.value)
      .filter((value) => approvedSecondWave.includes(value as never));
    expect(exposedSecondWave).toHaveLength(approvedSecondWave.length);
    expect(new Set(exposedSecondWave)).toEqual(new Set(approvedSecondWave));
    expect(
      actions
        .filter((action) => approvedSecondWave.includes(action.value as never))
        .every((action) => action.clientBound),
    ).toBe(true);
    for (const prohibited of [
      "opportunity_convert",
      "proposal_issue",
      "proposal_accept",
    ]) {
      expect(actions.map((action) => action.value)).not.toContain(prohibited);
    }
  });
});

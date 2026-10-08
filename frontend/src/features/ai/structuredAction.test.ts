import { describe, expect, it } from "vitest";

import { serializeStructuredAction } from "./structuredAction";

describe("structured action serialization", () => {
  it("serializes MSP-global Client creation only from supplied business fields", () => {
    expect(
      serializeStructuredAction("client", {
        structuredDisplayID: " FIRST-100 ",
        structuredName: " First Client ",
      }),
    ).toEqual({
      mode: "write",
      tool: "client.create",
      input: { display_id: "FIRST-100", name: "First Client" },
    });
  });

  it("serializes exact Client-bound reads with the public Client reference", () => {
    expect(
      serializeStructuredAction("project_get", {
        clientDisplayID: "CLIENT-001",
        reference: " PRJ-100 ",
      }),
    ).toEqual({
      mode: "read",
      tool: "project.get",
      input: { client: "CLIENT-001", project: "PRJ-100" },
    });
  });

  it("serializes existing Ticket writes without internal metadata", () => {
    expect(
      serializeStructuredAction("transition", {
        targetClientID: "client-1",
        ticketID: " INC-100 ",
        version: "4",
        value: " resolved ",
        reason: " fixed ",
      }),
    ).toEqual({
      mode: "write",
      tool: "ticket.transition",
      input: {
        client_id: "client-1",
        id: "INC-100",
        expected_version: 4,
        status: "resolved",
        reason: "fixed",
      },
    });
  });

  it("serializes Ticket creation with exact public preparation fields", () => {
    expect(
      serializeStructuredAction("ticket_create", {
        clientName: " Northwind Legal ",
        structuredDisplayID: " INC-2042 ",
        ticketType: "incident",
        structuredName: " VPN outage ",
        structuredBody: " Users cannot connect. ",
        ticketStatus: " new ",
        ticketPriority: "high",
        serviceReference: " Managed IT ",
        contractReference: "",
      }),
    ).toEqual({
      mode: "write",
      tool: "ticket.create",
      input: {
        client: "Northwind Legal",
        display_id: "INC-2042",
        type: "incident",
        title: "VPN outage",
        description: "Users cannot connect.",
        status: "new",
        priority: "high",
        service: "Managed IT",
      },
    });
  });

  it("serializes Ticket assignment with an exact technician reference", () => {
    expect(
      serializeStructuredAction("ticket_assign", {
        clientName: "Northwind Legal",
        ticketID: " INC-2042 ",
        technicianReference: " avery@example.com ",
        version: "7",
        reason: " Primary on-call ",
      }),
    ).toEqual({
      mode: "write",
      tool: "ticket.assign",
      input: {
        client: "Northwind Legal",
        ticket: "INC-2042",
        technician: "avery@example.com",
        expected_version: 7,
        reason: "Primary on-call",
      },
    });
  });

  it("serializes explicit-Client Opportunity and Proposal reads", () => {
    expect(
      serializeStructuredAction("opportunity_list", {
        clientName: "Northwind Legal",
        pipelineID: " pipeline-1 ",
        stageID: " stage-2 ",
        limit: "15",
      }),
    ).toEqual({
      mode: "read",
      tool: "opportunity.list",
      input: {
        client: "Northwind Legal",
        pipeline_id: "pipeline-1",
        stage_id: "stage-2",
        limit: 15,
      },
    });
    expect(
      serializeStructuredAction("opportunity_get", {
        clientName: "Northwind Legal",
        reference: " OPP-2042 ",
      }),
    ).toEqual({
      mode: "read",
      tool: "opportunity.get",
      input: { client: "Northwind Legal", opportunity: "OPP-2042" },
    });
    expect(
      serializeStructuredAction("proposal_list", {
        clientName: "Northwind Legal",
        proposalState: "draft",
        proposalOpportunityID: "opportunity-id",
        limit: "10",
      }),
    ).toEqual({
      mode: "read",
      tool: "proposal.list",
      input: {
        client: "Northwind Legal",
        state: "draft",
        opportunity_id: "opportunity-id",
        limit: 10,
      },
    });
    expect(
      serializeStructuredAction("proposal_get", {
        clientName: "Northwind Legal",
        reference: " PROP-2042 ",
      }),
    ).toEqual({
      mode: "read",
      tool: "proposal.get",
      input: { client: "Northwind Legal", proposal: "PROP-2042" },
    });
  });

  it("serializes Opportunity writes without invented activity time", () => {
    expect(
      serializeStructuredAction("opportunity_transition", {
        clientName: "Northwind Legal",
        reference: "OPP-2042",
        opportunityStage: "Qualified",
        version: "4",
        reason: "Discovery completed",
      }),
    ).toEqual({
      mode: "write",
      tool: "opportunity.transition",
      input: {
        client: "Northwind Legal",
        opportunity: "OPP-2042",
        stage: "Qualified",
        expected_version: 4,
        reason: "Discovery completed",
      },
    });
    const activityValues = {
      clientName: "Northwind Legal",
      reference: "OPP-2042",
      activityKind: "meeting" as const,
      activitySummary: "Discovery call",
      activityDetails: "Reviewed scope and timeline.",
      activityTime: "2026-08-07T15:00",
    };
    expect(
      serializeStructuredAction("opportunity_activity_create", activityValues),
    ).toEqual({
      mode: "write",
      tool: "opportunity.activity.create",
      input: {
        client: "Northwind Legal",
        opportunity: "OPP-2042",
        kind: "meeting",
        summary: "Discovery call",
        details: "Reviewed scope and timeline.",
      },
    });
  });

  it("serializes Proposal draft creation and internal Knowledge publication", () => {
    expect(
      serializeStructuredAction("proposal_create", {
        clientName: " Northwind Legal ",
        reference: " OPP-2042 ",
        structuredDisplayID: " PROP-2042 ",
      }),
    ).toEqual({
      mode: "write",
      tool: "proposal.create",
      input: {
        client: "Northwind Legal",
        opportunity: "OPP-2042",
        display_id: "PROP-2042",
      },
    });
    expect(
      serializeStructuredAction("knowledge_publish", {
        clientName: "Northwind Legal",
        reference: " KB-2042 ",
        version: "3",
        reason: " Approved internal runbook ",
      }),
    ).toEqual({
      mode: "write",
      tool: "knowledge.publish",
      input: {
        client: "Northwind Legal",
        article: "KB-2042",
        expected_version: 3,
        reason: "Approved internal runbook",
      },
    });
  });

  it.each([
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
  ] as const)(
    "refuses %s without an explicit canonical Client selection",
    (action) => {
      expect(serializeStructuredAction(action, {})).toBeUndefined();
    },
  );
});

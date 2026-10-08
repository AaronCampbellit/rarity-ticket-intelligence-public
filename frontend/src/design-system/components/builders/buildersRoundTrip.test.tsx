import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it } from "vitest";

import {
  AutomationStepBuilder,
  automationStepsFromForm,
} from "./AutomationStepBuilder";
import { CalendarBuilder, calendarFromForm } from "./CalendarBuilder";
import { ConditionBuilder, conditionsFromForm } from "./ConditionBuilder";

afterEach(cleanup);

function renderedForm(node: ReactNode) {
  const view = render(<form data-testid="form">{node}</form>);
  return new FormData(view.container.querySelector("form")!);
}

describe("guided builder round trips", () => {
  it("preserves advanced condition groups without exposing raw configuration", () => {
    const conditions = {
      client: { tier: "managed" },
      any: [{ priority: "critical" }, { source: { channel: "monitoring" } }],
    };

    const form = renderedForm(<ConditionBuilder value={conditions} />);
    expect(conditionsFromForm(form)).toEqual(conditions);
  });

  it("preserves nested and multi-parameter automation steps exactly", () => {
    const steps = [
      {
        id: "comment",
        kind: "action",
        action: {
          kind: "add_comment",
          parameters: { body: "Acknowledge" },
        },
      },
      {
        id: "branch",
        kind: "branch",
        condition: { field: "priority", operator: "equals", value: "critical" },
        children: [
          {
            id: "assign",
            kind: "action",
            action: {
              kind: "assign",
              parameters: { team_id: "team-1", reason: "Escalation" },
            },
          },
        ],
      },
      {
        id: "priority",
        kind: "action",
        action: {
          kind: "update_field",
          parameters: {
            field: "priority",
            value: "critical",
            reason: "Monitoring escalation",
          },
        },
      },
    ];

    const form = renderedForm(<AutomationStepBuilder steps={steps} />);
    expect(automationStepsFromForm(form)).toEqual(steps);
  });

  it("round trips tag actions and conditions through catalog-backed controls", () => {
    const tagID = "11111111-1111-4111-8111-111111111111";
    const groupID = "22222222-2222-4222-8222-222222222222";
    const steps = [
      {
        id: "add-tag",
        kind: "action",
        action: {
          kind: "add_tags",
          parameters: { tag_ids: JSON.stringify([tagID]) },
        },
      },
      {
        id: "in-group",
        kind: "condition",
        condition: {
          field: "group_ids",
          operator: "has_tag_in_group",
          value: groupID,
        },
        children: [
          {
            id: "comment",
            kind: "action",
            action: { kind: "add_comment", parameters: { body: "matched" } },
          },
        ],
      },
    ];
    const form = renderedForm(
      <AutomationStepBuilder
        steps={steps}
        groups={[
          { id: groupID, label: "Service", description: "", state: "active" },
        ]}
        tags={[
          {
            id: tagID,
            label: "Network",
            groupId: groupID,
            state: "active",
            synonyms: [],
            version: 1,
          },
        ]}
      />,
    );

    expect(automationStepsFromForm(form)).toEqual(steps);
  });

  it("authors, edits, and removes every tag condition with a nested action", async () => {
    const user = userEvent.setup();
    const tagID = "11111111-1111-4111-8111-111111111111";
    const groupID = "22222222-2222-4222-8222-222222222222";
    const archivedGroupID = "33333333-3333-4333-8333-333333333333";
    const view = render(
      <form data-testid="form">
        <AutomationStepBuilder
          steps={[]}
          groups={[
            { id: groupID, label: "Service", description: "", state: "active" },
            {
              id: archivedGroupID,
              label: "Archived",
              description: "",
              state: "archived",
            },
          ]}
          tags={[
            {
              id: tagID,
              label: "Network",
              groupId: groupID,
              state: "active",
              synonyms: [],
              version: 1,
            },
          ]}
        />
      </form>,
    );

    await user.tab();
    await user.tab();
    await user.keyboard("{Enter}");

    const form = () => new FormData(view.container.querySelector("form")!);
    const operator = screen.getByLabelText("Tag condition");
    const selectTag = async () => {
      const picker = screen.getByRole("combobox", { name: /Condition tags/ });
      fireEvent.focus(picker);
      await user.click(screen.getByRole("option", { name: "Network" }));
    };

    expect(() => automationStepsFromForm(form())).toThrow(
      "automation_condition_value_required",
    );
    await selectTag();
    expect(automationStepsFromForm(form())[0]).toMatchObject({
      kind: "condition",
      condition: {
        field: "effective_tag_ids",
        operator: "has_any_tag",
        value: JSON.stringify([tagID]),
      },
      children: [{ kind: "action", action: { kind: "add_comment" } }],
    });

    for (const nextOperator of ["has_all_tags", "has_no_tags"]) {
      await user.selectOptions(operator, nextOperator);
      await selectTag();
      expect(automationStepsFromForm(form())[0]).toMatchObject({
        condition: {
          field: "effective_tag_ids",
          operator: nextOperator,
          value: JSON.stringify([tagID]),
        },
      });
    }

    await user.selectOptions(operator, "has_tag_in_group");
    const group = screen.getByLabelText("Tag group");
    expect(
      screen.queryByRole("option", { name: "Archived" }),
    ).not.toBeInTheDocument();
    expect(() => automationStepsFromForm(form())).toThrow(
      "automation_condition_value_required",
    );
    await user.selectOptions(group, groupID);
    expect(automationStepsFromForm(form())[0]).toMatchObject({
      condition: {
        field: "group_ids",
        operator: "has_tag_in_group",
        value: groupID,
      },
    });

    await user.click(
      screen.getByRole("button", { name: "Remove automation step 1" }),
    );
    expect(automationStepsFromForm(form())).toEqual([]);
  });

  it("preserves every coverage window in a business calendar", () => {
    const definition = {
      timezone: "America/Chicago",
      weekly: {
        monday: [
          { start_minute: 480, end_minute: 720 },
          { start_minute: 780, end_minute: 1020 },
        ],
      },
      holidays: ["2026-12-25"],
    };

    const form = renderedForm(<CalendarBuilder definition={definition} />);
    expect(calendarFromForm(form)).toEqual(definition);
  });
});

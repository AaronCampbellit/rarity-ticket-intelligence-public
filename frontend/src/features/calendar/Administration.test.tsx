import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { CommitmentForm, scaledDecimal } from "./CommitmentForms";
import { PTOForm, validateWeeklyWindows } from "./WorkforceForms";
import { CalendarNotificationPreferences } from "./CalendarNotificationPreferences";
import { CustomDateEditor } from "./CustomDateEditor";
import { CalendarAdministration } from "./CalendarAdministration";
import {
  MutationForm,
  RecurrenceEditor,
  recurrencePayload,
} from "./formSupport";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
function mockRequests(
  respond: (path: string, init?: RequestInit) => unknown = () => [],
) {
  const fetcher = vi.fn(async (path: RequestInfo | URL, init?: RequestInit) =>
    Response.json(respond(String(path), init)),
  );
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
function fill(label: string, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}
it("creates a milestone with date-only bounds and no invented timestamp", async () => {
  const fetcher = mockRequests((path) =>
      path.endsWith("/projects/project") ? { phases: [] } : {},
    ),
    saved = vi.fn();
  render(
    <CommitmentForm
      kind="milestone"
      clientID="client"
      projectID="project"
      technicians={[]}
      clients={[]}
      onSaved={saved}
    />,
  );
  fill("Name", "Cutover");
  fill("Due date", "2026-09-15");
  fill("First day", "2026-09-15");
  fill("Last day (included)", "2026-09-15");
  fireEvent.click(screen.getByRole("button", { name: "Create milestone" }));
  await waitFor(() => expect(saved).toHaveBeenCalledOnce());
  const call = fetcher.mock.calls.find(([, init]) => init?.method === "POST")!;
  const body = JSON.parse(String(call[1]!.body));
  expect(body).toMatchObject({
    name: "Cutover",
    due_on: "2026-09-15",
    starts_on: "2026-09-15",
    ends_on: "2026-09-16",
    all_day: true,
  });
  expect(body.starts_at).toBeUndefined();
  expect(call[1]!.headers).toMatchObject({ "X-Rarity-Client-ID": "client" });
});
it("requests PTO for the signed-in technician and explains tentative capacity", async () => {
  const fetcher = mockRequests(() => ({ id: "pto", version: 1 })),
    saved = vi.fn();
  render(<PTOForm principalID="self" onSaved={saved} />);
  expect(
    screen.getByText(/does not reduce committed capacity/),
  ).toBeInTheDocument();
  fill("PTO type", "vacation");
  fill("First day", "2026-09-15");
  fill("Last day (included)", "2026-09-16");
  fireEvent.click(screen.getByRole("button", { name: "Request PTO" }));
  await waitFor(() => expect(saved).toHaveBeenCalledOnce());
  expect(JSON.parse(String(fetcher.mock.calls[0][1]?.body))).toMatchObject({
    technician_id: "self",
    pto_type: "vacation",
    starts_on: "2026-09-15",
    ends_on: "2026-09-17",
  });
});
it("uses exact decimal units and rejects overlapping working windows", () => {
  expect(scaledDecimal("1.2500", 10000)).toBe(12500);
  expect(scaledDecimal("19.99", 100)).toBe(1999);
  expect(() => scaledDecimal("1.001", 100)).toThrow(/decimal places/);
  expect(() =>
    validateWeeklyWindows([
      {
        weekday: 1,
        starts_minute: 540,
        ends_minute: 600,
        capacity_percent: 100,
      },
      {
        weekday: 1,
        starts_minute: 590,
        ends_minute: 650,
        capacity_percent: 100,
      },
    ]),
  ).toThrow(/overlap/);
});
it("submits bounded selected-weekday recurrence", async () => {
  const submit = vi.fn().mockResolvedValue({});
  render(
    <MutationForm
      label="Save repeat"
      onSaved={() => {}}
      onSubmit={(data) => submit(recurrencePayload(data))}
    >
      <RecurrenceEditor />
    </MutationForm>,
  );
  fill("Frequency", "weekly");
  fireEvent.click(screen.getByLabelText("Monday"));
  fireEvent.click(screen.getByLabelText("Wednesday"));
  fill("Occurrence count", "12");
  fireEvent.click(screen.getByRole("button", { name: "Save repeat" }));
  await waitFor(() =>
    expect(submit).toHaveBeenCalledWith({
      frequency: "weekly",
      interval: 1,
      weekdays: [1, 3],
      count: 12,
    }),
  );
});
it("preserves notification rules and saves against the loaded version", async () => {
  const fetcher = mockRequests(() => ({
    version: 7,
    rules: [
      {
        event_class: "calendar.schedule_changed",
        change_class: "schedule",
        urgency: "routine",
        channel: "email",
        enabled: true,
      },
    ],
  }));
  render(<CalendarNotificationPreferences />);
  fireEvent.click(
    await screen.findByRole("checkbox", { name: "schedule · routine · email" }),
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Save notification preferences" }),
  );
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(
      true,
    ),
  );
  const call = fetcher.mock.calls.find(([, init]) => init?.method === "PUT")!;
  expect(JSON.parse(String(call[1]?.body))).toMatchObject({
    expected_version: 7,
    rules: [{ change_class: "schedule", enabled: false }],
  });
});
it("loads source-authorized date definitions and updates using the value version", async () => {
  const fetcher = mockRequests((path) =>
    path.endsWith("custom-date-fields")
      ? [
          {
            id: "field",
            version: 2,
            label: "Follow-up date",
            field_type: "date",
          },
        ]
      : [
          {
            id: "value",
            version: 4,
            field_id: "field",
            date_value: "2026-09-01",
          },
        ],
  );
  render(
    <CustomDateEditor
      objectType="work_record"
      objectID="record"
      clientID="client"
    />,
  );
  const details = screen.getByText("Custom calendar dates").closest("details")!;
  details.open = true;
  fireEvent(details, new Event("toggle"));
  await screen.findByLabelText("Custom date field");
  fill("Custom date field", "field");
  fill("Follow-up date", "2026-09-03");
  fireEvent.click(screen.getByRole("button", { name: "Save custom date" }));
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(
      true,
    ),
  );
  const call = fetcher.mock.calls.find(([, init]) => init?.method === "PUT")!;
  expect(JSON.parse(String(call[1]?.body))).toMatchObject({
    field_id: "field",
    expected_version: 4,
    date_value: "2026-09-03",
  });
  expect(JSON.parse(String(call[1]?.body)).timestamp_value).toBeUndefined();
});
it("limits creation and settings choices to ordinary capabilities", async () => {
  mockRequests(() => ({
    technicians: [],
    teams: [],
    departments: [],
    queues: [],
    clients: [],
  }));
  render(
    <CalendarAdministration
      principalID="self"
      capabilities={new Set(["calendar.read"])}
      clients={[]}
      onChanged={() => {}}
    />,
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Create and manage calendar records" }),
  );
  expect(
    await screen.findByRole("option", { name: "PTO request" }),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("option", { name: "Maintenance window" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("option", { name: "Conflict policy" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("option", { name: /generic event/i }),
  ).not.toBeInTheDocument();
});
it("keeps an idempotency key on ambiguous failure and requires a new key for changed input", async () => {
  const keys: string[] = [],
    submit = vi.fn(
      async (data: FormData, key: (payload: unknown) => string) => {
        keys.push(key({ title: data.get("title") }));
        throw new Error("Connection lost");
      },
    );
  render(
    <MutationForm label="Submit" onSaved={() => {}} onSubmit={submit}>
      <input aria-label="Title" name="title" defaultValue="first" />
    </MutationForm>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Submit" }));
  await screen.findByText("Connection lost");
  fireEvent.click(screen.getByRole("button", { name: "Submit" }));
  await waitFor(() => expect(submit).toHaveBeenCalledTimes(2));
  expect(keys[0]).toBe(keys[1]);
  fill("Title", "second");
  fireEvent.click(screen.getByRole("button", { name: "Submit" }));
  await waitFor(() => expect(submit).toHaveBeenCalledTimes(3));
  expect(keys[2]).not.toBe(keys[0]);
});

import { useEffect, useMemo, useRef, useState } from "react";

import {
  Button,
  Notice,
  Page,
  StatePanel,
  TagPicker,
  TrendChart,
} from "../../design-system";
import { classificationAdminAPI } from "./api";
import type {
  ClassificationAdminAPI,
  ClassificationReport,
  ClassificationReportFilter,
  ClassificationReportKind,
  ClassificationCatalog,
} from "./types";
import "./classification.css";

type InsightsAPI = {
  report: NonNullable<ClassificationAdminAPI["report"]>;
  reportEvidence?: NonNullable<ClassificationAdminAPI["reportEvidence"]>;
  catalog?: ClassificationAdminAPI["catalog"];
  reportTechnicians?: NonNullable<ClassificationAdminAPI["reportTechnicians"]>;
};
const kinds: ClassificationReportKind[] = [
  "usage",
  "combinations",
  "trends",
  "classification-health",
  "recurring-issues",
];
const emptyOptions: Array<{ id: string; label: string }> = [];

const today = () => new Date().toISOString().slice(0, 10);
const monthAgo = () => {
  const value = new Date();
  value.setUTCDate(value.getUTCDate() - 30);
  return value.toISOString().slice(0, 10);
};

function initialFilters() {
  const query = window.location.hash.split("?", 2)[1] ?? "";
  const parameters = new URLSearchParams(query);
  const value = (name: string, fallback = "") =>
    parameters.get(name)?.slice(0, 256) ?? fallback;
  return {
    objectType: value("object_type"),
    groupID: value("group_id"),
    tagIDs: value("tag_ids"),
    match: value("match", "any"),
    source: value("source"),
    inheritance: value("inheritance", "all"),
    technicianID: value("technician_id"),
    teamID: value("team_id"),
    priority: value("priority"),
    status: value("status"),
    from: value("from", monthAgo()),
    to: value("to", today()),
  };
}

export function ClassificationInsightsPage({
  clientID = "",
  api = classificationAdminAPI as InsightsAPI,
  technicianOptions = emptyOptions,
  teamOptions = emptyOptions,
}: {
  clientID?: string;
  api?: InsightsAPI;
  technicianOptions?: Array<{ id: string; label: string }>;
  teamOptions?: Array<{ id: string; label: string }>;
}) {
  const [draft, setDraft] = useState(initialFilters);
  const [reports, setReports] = useState<ClassificationReport[]>([]);
  const [executedFilter, setExecutedFilter] =
    useState<ClassificationReportFilter>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [catalog, setCatalog] = useState<ClassificationCatalog>({
    groups: [],
    tags: [],
  });
  const [directoryTechnicians, setDirectoryTechnicians] =
    useState(technicianOptions);
  const generation = useRef(0);
  const controller = useRef<AbortController | undefined>(undefined);

  useEffect(() => {
    generation.current += 1;
    controller.current?.abort();
    setReports([]);
    setExecutedFilter(undefined);
    setError("");
    setBusy(false);
    return () => controller.current?.abort();
  }, [clientID]);

  useEffect(() => {
    const requestController = new AbortController();
    void api
      .catalog?.(requestController.signal)
      .then(setCatalog)
      .catch(() => undefined);
    return () => requestController.abort();
  }, [api]);

  useEffect(() => {
    const requestController = new AbortController();
    if (api.reportTechnicians) {
      setDirectoryTechnicians([]);
      void api
        .reportTechnicians(clientID, requestController.signal)
        .then((options) => {
          if (!requestController.signal.aborted)
            setDirectoryTechnicians(options);
        })
        .catch(() => undefined);
    } else setDirectoryTechnicians(technicianOptions);
    return () => requestController.abort();
  }, [api, clientID, technicianOptions]);

  const filter = useMemo<ClassificationReportFilter>(
    () => ({
      clientID,
      objectType: draft.objectType
        ? (draft.objectType as ClassificationReportFilter["objectType"])
        : undefined,
      groupID: draft.groupID || undefined,
      tagIDs: draft.tagIDs
        .split(",")
        .map((value) => value.trim())
        .filter(Boolean),
      match: draft.match as ClassificationReportFilter["match"],
      source: draft.source
        ? (draft.source as ClassificationReportFilter["source"])
        : undefined,
      inheritance:
        draft.inheritance as ClassificationReportFilter["inheritance"],
      technicianID: draft.technicianID || undefined,
      teamID: draft.teamID || undefined,
      priority: draft.priority || undefined,
      status: draft.status || undefined,
      from: draft.from,
      to: draft.to,
      limit: 50,
    }),
    [clientID, draft],
  );

  const run = async () => {
    if (!clientID) return;
    controller.current?.abort();
    const requestController = new AbortController();
    controller.current = requestController;
    const requestGeneration = ++generation.current;
    setBusy(true);
    setError("");
    try {
      const next = await Promise.all(
        kinds.map((kind) => api.report(kind, filter, requestController.signal)),
      );
      if (
        requestGeneration === generation.current &&
        !requestController.signal.aborted
      ) {
        setReports(next);
        setExecutedFilter({ ...filter, tagIDs: [...(filter.tagIDs ?? [])] });
      }
    } catch {
      if (
        requestGeneration === generation.current &&
        !requestController.signal.aborted
      )
        setError(
          "Classification insights could not be loaded for these filters.",
        );
    } finally {
      if (requestGeneration === generation.current) setBusy(false);
    }
  };

  const loadMore = async (report: ClassificationReport) => {
    if (!report.nextCursor || !executedFilter || busy) return;
    const requestGeneration = generation.current;
    setBusy(true);
    try {
      const next = await api.report(report.kind, {
        ...executedFilter,
        cursor: report.nextCursor,
      });
      if (requestGeneration === generation.current)
        setReports((current) =>
          current.map((value) =>
            value.kind === report.kind
              ? { ...next, rows: [...value.rows, ...next.rows] }
              : value,
          ),
        );
    } catch {
      if (requestGeneration === generation.current)
        setError("More classification results could not be loaded.");
    } finally {
      if (requestGeneration === generation.current) setBusy(false);
    }
  };

  const loadEvidence = async (
    report: ClassificationReport,
    rowIndex: number,
  ) => {
    const row = report.rows[rowIndex];
    if (
      !api.reportEvidence ||
      !executedFilter ||
      !row.tagId ||
      !row.objectType ||
      !row.evidenceCursor ||
      busy
    )
      return;
    const requestGeneration = generation.current;
    setBusy(true);
    try {
      const page = await api.reportEvidence(row.tagId, {
        ...executedFilter,
        objectType: row.objectType,
        cursor: row.evidenceCursor,
      });
      if (requestGeneration === generation.current)
        setReports((current) =>
          current.map((value) =>
            value.kind !== report.kind
              ? value
              : {
                  ...value,
                  rows: value.rows.map((candidate, index) =>
                    index !== rowIndex
                      ? candidate
                      : {
                          ...candidate,
                          objectRefs: [
                            ...(candidate.objectRefs ?? []),
                            ...page.items,
                          ],
                          evidenceCursor: page.nextCursor,
                        },
                  ),
                },
          ),
        );
    } catch {
      if (requestGeneration === generation.current)
        setError("More recurring-issue evidence could not be loaded.");
    } finally {
      if (requestGeneration === generation.current) setBusy(false);
    }
  };

  const preserve = new URLSearchParams();
  const preservedFilter = executedFilter ?? filter;
  Object.entries({
    object_type: preservedFilter.objectType,
    group_id: preservedFilter.groupID,
    tag_ids: preservedFilter.tagIDs?.join(","),
    match: preservedFilter.match,
    source: preservedFilter.source,
    inheritance: preservedFilter.inheritance,
    technician_id: preservedFilter.technicianID,
    team_id: preservedFilter.teamID,
    priority: preservedFilter.priority,
    status: preservedFilter.status,
    from: preservedFilter.from,
    to: preservedFilter.to,
  }).forEach(([name, value]) => {
    if (value) preserve.set(name, value);
  });
  const returnTo = `#/classification-insights?${preserve.toString()}`;
  const watermark = reports
    .map((report) => report.projectionAsOf)
    .sort()
    .at(0);

  const update = (name: keyof typeof draft, value: string) =>
    setDraft((current) => ({ ...current, [name]: value }));

  return (
    <Page
      className="classification-insights-page"
      eyebrow="Classification"
      title="Classification insights"
      description="Analyze governed tag usage, combinations, trends, classification health, and recurring issues for the selected client."
    >
      {error ? (
        <Notice title="Insights unavailable" tone="danger">
          {error}
        </Notice>
      ) : null}
      <form
        className="classification-insights-filters"
        onSubmit={(event) => {
          event.preventDefault();
          void run();
        }}
      >
        <label>
          Client
          <input value={clientID} readOnly />
        </label>
        <label>
          Object type
          <select
            value={draft.objectType}
            onChange={(event) => update("objectType", event.target.value)}
          >
            <option value="">All</option>
            {[
              "work_record",
              "task",
              "project",
              "asset",
              "knowledge_article",
              "time_entry",
            ].map((value) => (
              <option key={value}>{value}</option>
            ))}
          </select>
        </label>
        <label>
          Group
          <select
            value={draft.groupID}
            onChange={(event) => update("groupID", event.target.value)}
          >
            <option value="">All groups</option>
            {catalog.groups
              .filter((group) => group.state !== "archived")
              .map((group) => (
                <option key={group.id} value={group.id}>
                  {group.label}
                </option>
              ))}
          </select>
        </label>
        <TagPicker
          label="Tags"
          groups={catalog.groups}
          tags={catalog.tags}
          selectedIds={draft.tagIDs.split(",").filter(Boolean)}
          onChange={(ids) => update("tagIDs", ids.join(","))}
        />
        <label>
          Tag match
          <select
            value={draft.match}
            onChange={(event) => update("match", event.target.value)}
          >
            {["any", "all", "none"].map((value) => (
              <option key={value}>{value}</option>
            ))}
          </select>
        </label>
        <label>
          Source
          <select
            value={draft.source}
            onChange={(event) => update("source", event.target.value)}
          >
            <option value="">All</option>
            {[
              "human",
              "ai_confirmed",
              "ai_automatic",
              "automation",
              "integration",
              "migration",
              "system_fallback",
            ].map((value) => (
              <option key={value}>{value}</option>
            ))}
          </select>
        </label>
        <label>
          Inheritance
          <select
            value={draft.inheritance}
            onChange={(event) => update("inheritance", event.target.value)}
          >
            {["all", "direct", "inherited"].map((value) => (
              <option key={value}>{value}</option>
            ))}
          </select>
        </label>
        <label>
          Technician
          <select
            value={draft.technicianID}
            onChange={(event) => update("technicianID", event.target.value)}
          >
            <option value="">All technicians</option>
            {directoryTechnicians.map((option) => (
              <option key={option.id} value={option.id}>
                {option.label}
              </option>
            ))}
          </select>
        </label>
        <label>
          Team
          <select
            value={draft.teamID}
            onChange={(event) => update("teamID", event.target.value)}
          >
            <option value="">All teams</option>
            {teamOptions.map((option) => (
              <option key={option.id} value={option.id}>
                {option.label}
              </option>
            ))}
          </select>
        </label>
        <label>
          Priority
          <input
            value={draft.priority}
            onChange={(event) => update("priority", event.target.value)}
          />
        </label>
        <label>
          Status
          <input
            value={draft.status}
            onChange={(event) => update("status", event.target.value)}
          />
        </label>
        <label>
          From
          <input
            type="date"
            value={draft.from}
            onChange={(event) => update("from", event.target.value)}
          />
        </label>
        <label>
          To
          <input
            type="date"
            value={draft.to}
            onChange={(event) => update("to", event.target.value)}
          />
        </label>
        <Button type="submit" disabled={!clientID || busy}>
          {busy ? "Running…" : "Run insights"}
        </Button>
      </form>
      {!clientID ? (
        <StatePanel
          state="empty"
          title="Choose a client"
          description="Select an authorized client to run classification insights."
        />
      ) : null}
      {watermark ? (
        <p className="classification-projection-watermark">
          Projected through {new Date(watermark).toLocaleString()}.
        </p>
      ) : null}
      <section
        className="classification-insights-grid"
        aria-label="Classification report lenses"
      >
        {reports.map((report) => (
          <article key={report.kind} className="classification-health-card">
            <h2>{report.kind.replaceAll("-", " ")}</h2>
            {report.kind === "trends" && report.rows.length ? (
              <figure>
                <TrendChart
                  title="Classification trend over the selected date range"
                  values={report.rows.map(
                    (row) => row.activeCount ?? row.count,
                  )}
                />
                <figcaption>
                  Active classifications by report date; exact values follow in
                  the table.
                </figcaption>
              </figure>
            ) : null}
            {report.kind === "trends" && report.rows.length ? (
              <div
                className="table-scroll"
                role="region"
                aria-label="Classification trend values"
                tabIndex={0}
              >
                <table>
                  <caption>Classification trend values</caption>
                  <thead>
                    <tr>
                      <th scope="col">Date</th>
                      <th scope="col">Tag</th>
                      <th scope="col">Added</th>
                      <th scope="col">Removed</th>
                      <th scope="col">Net active</th>
                    </tr>
                  </thead>
                  <tbody>
                    {report.rows.map((row, index) => (
                      <tr key={`trend-${row.tagId}-${row.date}-${index}`}>
                        <td>{row.date}</td>
                        <td>
                          {catalog.tags.find((tag) => tag.id === row.tagId)
                            ?.label ?? "Unknown tag"}
                        </td>
                        <td>{row.addedCount ?? 0}</td>
                        <td>{row.removedCount ?? 0}</td>
                        <td>{row.activeCount ?? row.count}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}
            {report.rows.length === 0 ? (
              <p>No matching results.</p>
            ) : (
              <ul>
                {report.rows.map((row, index) => (
                  <li
                    key={`${row.tagId ?? row.leftTagId ?? row.source}-${index}`}
                  >
                    <span>
                      {row.date ? `${row.date} · ` : ""}
                      {row.tagId
                        ? (catalog.tags.find((tag) => tag.id === row.tagId)
                            ?.label ?? "Unknown tag")
                        : [row.leftTagId, row.rightTagId]
                            .filter(Boolean)
                            .map(
                              (id) =>
                                catalog.tags.find((tag) => tag.id === id)
                                  ?.label ?? "Unknown tag",
                            )
                            .join(" + ") ||
                          row.source ||
                          row.objectType}
                      : {row.count}
                      {row.ageSeconds
                        ? ` · oldest unclassified ${Math.floor(row.ageSeconds / 86400)} days`
                        : ""}
                    </span>
                    {[
                      ...(row.objectRefs ?? []),
                      ...(row.objectIds ?? []).map((objectId) => ({
                        objectId,
                        objectType: row.objectType!,
                        label: objectId,
                        clientId: clientID,
                        parentObjectId: undefined,
                      })),
                    ].map((reference) => {
                      const objectID = reference.objectId;
                      const paths: Partial<Record<string, string>> = {
                        work_record: `#/work?workRecordID=${encodeURIComponent(objectID)}`,
                        task: `#/project?recordType=task&recordID=${encodeURIComponent(objectID)}${reference.parentObjectId ? `&parentRecordID=${encodeURIComponent(reference.parentObjectId)}` : ""}&clientID=${encodeURIComponent(reference.clientId || clientID)}&label=${encodeURIComponent(reference.label)}`,
                        project: `#/project?recordType=project&recordID=${encodeURIComponent(objectID)}&clientID=${encodeURIComponent(reference.clientId || clientID)}&label=${encodeURIComponent(reference.label)}`,
                        asset: `#/client-resources?assetID=${encodeURIComponent(objectID)}`,
                        knowledge_article: `#/knowledge?articleID=${encodeURIComponent(objectID)}`,
                        time_entry: `#/timesheets?timeEntryID=${encodeURIComponent(objectID)}`,
                      };
                      const path =
                        paths[reference.objectType ?? ""] ??
                        `#/classification-insights?objectID=${encodeURIComponent(objectID)}`;
                      return (
                        <a
                          key={objectID}
                          href={`${path}&returnTo=${encodeURIComponent(returnTo)}`}
                          aria-label={`Open ${reference.objectType?.replace("_", " ")} ${reference.label}`}
                        >
                          {" "}
                          Open {reference.label}
                        </a>
                      );
                    })}
                    {row.evidenceCursor && api.reportEvidence ? (
                      <Button
                        type="button"
                        disabled={busy}
                        onClick={() => {
                          void loadEvidence(report, index);
                        }}
                      >
                        Load more evidence
                      </Button>
                    ) : null}
                  </li>
                ))}
              </ul>
            )}
            {report.nextCursor ? (
              <Button
                type="button"
                disabled={busy}
                onClick={() => {
                  void loadMore(report);
                }}
              >
                Load more
              </Button>
            ) : null}
          </article>
        ))}
      </section>
    </Page>
  );
}

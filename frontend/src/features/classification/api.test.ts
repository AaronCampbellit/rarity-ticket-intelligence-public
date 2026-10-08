import { describe, expect, it, vi } from "vitest";

import {
  ClassificationAPIError,
  createClassificationAdminAPI,
  createClassificationAPI,
} from "./api";

const tag = {
  id: "tag-vpn",
  label: "VPN",
  group_id: "group-technology",
  state: "active",
  synonyms: ["remote access"],
  version: 2,
};

const taggedObject = {
  target: { object_type: "work_record", object_id: "work-1" },
  object_version: 4,
  direct: [
    {
      id: "assignment-1",
      tag,
      source: "human",
      assigned_at: "2026-08-05T12:00:00Z",
      assigned_by: "tech-1",
      inherited: false,
    },
  ],
  inherited: [],
  effective: [],
  classification_state: "classified",
};

describe("classification API", () => {
  it("maps snake_case responses and sends a scoped ETag replacement", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify([
            {
              id: "group-technology",
              label: "Technology",
              description: "",
              position: 1,
              state: "active",
              system_managed: false,
              version: 1,
            },
          ]),
          { status: 200 },
        ),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify([tag]), { status: 200 }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify(taggedObject), {
          status: 200,
          headers: { ETag: '"4"' },
        }),
      )
      .mockResolvedValueOnce(new Response(JSON.stringify([]), { status: 200 }))
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ ...taggedObject, object_version: 5 }), {
          status: 200,
          headers: { ETag: '"5"' },
        }),
      );
    const signal = new AbortController().signal;
    const api = createClassificationAPI(fetcher, "client-1");

    await expect(api.catalog(signal)).resolves.toMatchObject({
      groups: [{ id: "group-technology", systemManaged: false }],
      tags: [{ groupId: "group-technology", synonyms: ["remote access"] }],
    });
    await expect(
      api.object({ objectType: "work_record", objectId: "work 1" }, signal),
    ).resolves.toMatchObject({
      objectVersion: 4,
      direct: [{ assignedAt: "2026-08-05T12:00:00Z" }],
    });
    await api.history(
      { objectType: "work_record", objectId: "work 1" },
      signal,
    );
    await api.replaceDirect(
      { objectType: "work_record", objectId: "work 1" },
      {
        tagIDs: ["tag-vpn"],
        expectedVersion: 4,
        reason: "Correct classification",
        idempotencyKey: "change-1",
      },
      signal,
    );

    expect(fetcher.mock.calls.map(([path]) => path)).toEqual([
      "/api/v1/tag-groups",
      "/api/v1/tags",
      "/api/v1/objects/work_record/work%201/tags",
      "/api/v1/objects/work_record/work%201/tag-history",
      "/api/v1/objects/work_record/work%201/tags",
    ]);
    for (const [, init] of fetcher.mock.calls) {
      expect(new Headers(init.headers).get("X-Rarity-Client-ID")).toBe(
        "client-1",
      );
    }
    const [, objectInit] = fetcher.mock.calls[2];
    expect(objectInit.signal).toBe(signal);
    const [, historyInit] = fetcher.mock.calls[3];
    expect(historyInit.signal).toBe(signal);
    const [, mutationInit] = fetcher.mock.calls[4];
    expect(new Headers(mutationInit.headers).get("If-Match")).toBe('"4"');
    expect(mutationInit.signal).toBe(signal);
    expect(JSON.parse(String(mutationInit.body))).toEqual({
      tag_ids: ["tag-vpn"],
      reason: "Correct classification",
      idempotency_key: "change-1",
    });
    expect(String(mutationInit.body)).not.toContain("client");
    expect(String(mutationInit.body)).not.toContain("actor");
    expect(String(mutationInit.body)).not.toContain("source");
  });

  it("parses stable error codes from failed requests", async () => {
    const api = createClassificationAPI(
      vi
        .fn()
        .mockResolvedValueOnce(
          new Response(
            JSON.stringify({
              error: {
                code: "classification_required",
                message: "Select a tag",
              },
            }),
            { status: 422 },
          ),
        )
        .mockResolvedValueOnce(
          new Response(
            JSON.stringify({
              error: {
                code: "classification_required",
                message: "Select a tag",
              },
            }),
            { status: 422 },
          ),
        )
        .mockResolvedValueOnce(
          new Response(
            JSON.stringify({
              error: {
                code: "classification_required",
                message: "Select a tag",
              },
            }),
            { status: 422 },
          ),
        )
        .mockResolvedValueOnce(
          new Response(
            JSON.stringify({
              error: {
                code: "classification_required",
                message: "Select a tag",
              },
            }),
            { status: 422 },
          ),
        ),
    );

    await expect(api.catalog()).rejects.toEqual(
      expect.objectContaining({
        code: "classification_required",
        status: 422,
        message: "Select a tag",
      }),
    );
    await api.catalog().catch((error: unknown) => {
      expect(error).toBeInstanceOf(ClassificationAPIError);
    });
  });

  it("uses exact catalog administration contracts and decodes lifecycle reporting", async () => {
    document.cookie = "rarity_csrf=admin-csrf";
    const group = {
      id: "group-technology",
      label: "Technology",
      description: "",
      position: 2,
      state: "active",
      version: 3,
    };
    const impact = {
      operation: "archive",
      tag_id: "tag-vpn",
      replacement_tag_id: "tag-m365",
      affected_objects: 4,
      affected_saved_views: 2,
      affected_reports: 1,
      affected_automations: 3,
      fallback_by_object_type: { task: 2 },
    };
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(Response.json(group))
      .mockResolvedValueOnce(Response.json(group))
      .mockResolvedValueOnce(Response.json(tag))
      .mockResolvedValueOnce(Response.json(tag))
      .mockResolvedValueOnce(Response.json(impact))
      .mockResolvedValueOnce(Response.json(tag))
      .mockResolvedValueOnce(Response.json(tag))
      .mockResolvedValueOnce(
        Response.json({
          by_object_type: {
            task: { meaningful: 8, unclassified: 1, archive_fallback: 2 },
          },
        }),
      )
      .mockResolvedValueOnce(
        Response.json([
          {
            id: "run-1",
            status: "completed",
            started_at: "2026-08-05T00:00:00Z",
            rows_discovered: 9,
            rows_migrated: 8,
            fallback_assignments: 1,
            category_source_present: true,
          },
        ]),
      );
    const api = createClassificationAdminAPI(fetcher);

    await api.createGroup({
      label: "Technology",
      description: "",
      position: 2,
    });
    await api.updateGroup("group id", {
      label: "Technology",
      description: "Updated",
      position: 3,
      state: "active",
      expectedVersion: 3,
    });
    await api.createTag({
      groupId: "group-technology",
      label: "VPN",
      description: "",
      color: "",
      synonyms: ["remote"],
    });
    await api.updateTag("tag id", {
      groupId: "group-technology",
      label: "VPN",
      description: "",
      color: "",
      synonyms: [],
      expectedVersion: 2,
    });
    await expect(
      api.impact("tag id", "archive", "tag-m365"),
    ).resolves.toMatchObject({
      tagId: "tag-vpn",
      fallbackByObjectType: { task: 2 },
    });
    await api.merge("tag id", {
      survivorTagId: "tag-m365",
      reason: "duplicate",
      expectedVersion: 2,
    });
    await api.archive("tag id", {
      replacementTagId: "",
      reason: "retired",
      expectedVersion: 2,
    });
    await expect(api.health("client-123")).resolves.toMatchObject({
      byObjectType: { task: { archiveFallback: 2 } },
    });
    await expect(api.migrationHistory()).resolves.toMatchObject([
      { startedAt: "2026-08-05T00:00:00Z", rowsMigrated: 8 },
    ]);

    expect(fetcher.mock.calls.map(([path]) => path)).toEqual([
      "/api/v1/tag-groups",
      "/api/v1/tag-groups/group%20id",
      "/api/v1/tags",
      "/api/v1/tags/tag%20id",
      "/api/v1/tags/tag%20id/impact?operation=archive&replacement_tag_id=tag-m365",
      "/api/v1/tags/tag%20id/merge",
      "/api/v1/tags/tag%20id/archive",
      "/api/v1/classification/health",
      "/api/v1/classification/migration-runs",
    ]);
    const mutations = [
      [0, "POST"],
      [1, "PATCH"],
      [2, "POST"],
      [3, "PATCH"],
      [5, "POST"],
      [6, "POST"],
    ] as const;
    for (const [index, method] of mutations) {
      const [, init] = fetcher.mock.calls[index];
      expect(init).toMatchObject({ method, credentials: "same-origin" });
      expect(new Headers(init.headers).get("Content-Type")).toBe(
        "application/json",
      );
      expect(new Headers(init.headers).get("X-Rarity-CSRF")).toBe("admin-csrf");
    }
    for (const [index, version] of [
      [1, '"3"'],
      [3, '"2"'],
      [5, '"2"'],
      [6, '"2"'],
    ] as const) {
      const [, init] = fetcher.mock.calls[index];
      expect(new Headers(init.headers).get("If-Match")).toBe(version);
    }
    const [, groupUpdate] = fetcher.mock.calls[1];
    const [groupCreatePath, groupCreate] = fetcher.mock.calls[0];
    expect(groupCreatePath).toBe("/api/v1/tag-groups");
    expect(groupCreate).toMatchObject({
      method: "POST",
      credentials: "same-origin",
    });
    expect(new Headers(groupCreate.headers).get("Content-Type")).toBe(
      "application/json",
    );
    expect(new Headers(groupCreate.headers).get("X-Rarity-CSRF")).toBe(
      "admin-csrf",
    );
    expect(JSON.parse(groupCreate.body)).toEqual({
      label: "Technology",
      description: "",
      position: 2,
    });
    expect(groupUpdate).toMatchObject({
      method: "PATCH",
      credentials: "same-origin",
    });
    expect(new Headers(groupUpdate.headers).get("If-Match")).toBe('"3"');
    expect(JSON.parse(groupUpdate.body)).toEqual({
      label: "Technology",
      description: "Updated",
      position: 3,
      state: "active",
      expected_version: 3,
    });
    const [, tagCreate] = fetcher.mock.calls[2];
    expect(tagCreate).toMatchObject({
      method: "POST",
      credentials: "same-origin",
    });
    expect(JSON.parse(tagCreate.body)).toEqual({
      group_id: "group-technology",
      label: "VPN",
      description: "",
      color: "",
      synonyms: ["remote"],
    });
    const [, tagUpdate] = fetcher.mock.calls[3];
    expect(tagUpdate).toMatchObject({
      method: "PATCH",
      credentials: "same-origin",
    });
    expect(new Headers(tagUpdate.headers).get("If-Match")).toBe('"2"');
    expect(JSON.parse(tagUpdate.body)).toEqual({
      group_id: "group-technology",
      label: "VPN",
      description: "",
      color: "",
      synonyms: [],
      expected_version: 2,
    });
    const [, impactRequest] = fetcher.mock.calls[4];
    expect(impactRequest).toMatchObject({ credentials: "same-origin" });
    const [, merge] = fetcher.mock.calls[5];
    expect(merge).toMatchObject({ method: "POST", credentials: "same-origin" });
    expect(JSON.parse(merge.body)).toEqual({
      survivor_tag_id: "tag-m365",
      reason: "duplicate",
      expected_version: 2,
    });
    const [, archive] = fetcher.mock.calls[6];
    expect(archive).toMatchObject({
      method: "POST",
      credentials: "same-origin",
    });
    expect(JSON.parse(archive.body)).toEqual({
      replacement_tag_id: "",
      reason: "retired",
      expected_version: 2,
    });
    expect(fetcher.mock.calls[7][1]).toMatchObject({
      credentials: "same-origin",
    });
    expect(
      new Headers(fetcher.mock.calls[7][1].headers).get("X-Rarity-Client-ID"),
    ).toBe("client-123");
    expect(fetcher.mock.calls[8][1]).toMatchObject({
      credentials: "same-origin",
    });
  });

  it("maps an administration mutation failure to its stable server error", async () => {
    const api = createClassificationAdminAPI(
      vi
        .fn()
        .mockResolvedValue(
          Response.json(
            { error: { code: "version_conflict", message: "Refresh first" } },
            { status: 409 },
          ),
        ),
    );

    await expect(
      api.archive("tag-vpn", {
        replacementTagId: "",
        reason: "Retired",
        expectedVersion: 2,
      }),
    ).rejects.toMatchObject({
      code: "version_conflict",
      status: 409,
      message: "Refresh first",
    });
  });

  it("rejects malformed administration payloads with the stable client error", async () => {
    const api = createClassificationAdminAPI(
      vi.fn().mockResolvedValue(
        Response.json([
          {
            id: "run-1",
            status: "completed",
            started_at: "2026-08-05T00:00:00Z",
            rows_discovered: 1,
            rows_migrated: 1,
            fallback_assignments: 0,
            category_source_present: "false",
          },
        ]),
      ),
    );
    await expect(api.migrationHistory()).rejects.toMatchObject({
      code: "invalid_response",
      status: 502,
    });
  });
});

import { describe, expect, it, vi } from "vitest";

import { MentionAPIError, createMentionAPI } from "./api";
import type { MentionContext, MentionDocument } from "./types";

const context: MentionContext = {
  clientId: "client-1",
  parentType: "work_record",
  parentId: "work 1",
  sourceKind: "comment",
};

describe("mention API", () => {
  it("maps the bounded widget contract and omits unavailable previews", async () => {
    const fetcher = vi.fn().mockResolvedValue(
      Response.json({
        counts: { unread: 1, read: 2, archived: 3 },
        items: [
          {
            id: "item-1",
            parent_type: "work_record",
            parent_id: "work-1",
            parent_display_id: "RTY-1042",
            parent_subject: "VPN unavailable",
            latest_occurrence_id: "occurrence-1",
            author_label: "Mira Patel",
            origin: "direct",
            state: "unread",
            last_mentioned_at: "2026-08-08T13:00:00Z",
            version: 3,
          },
        ],
        next_cursor: "signed",
      }),
    );
    const api = createMentionAPI(fetcher);

    await expect(api.listWidget("unread", undefined, 20)).resolves.toEqual({
      counts: { unread: 1, read: 2, archived: 3 },
      items: [
        {
          id: "item-1",
          parentType: "work_record",
          parentId: "work-1",
          parentDisplayId: "RTY-1042",
          parentSubject: "VPN unavailable",
          latestOccurrenceId: "occurrence-1",
          authorLabel: "Mira Patel",
          origin: "direct",
          state: "unread",
          lastMentionedAt: "2026-08-08T13:00:00Z",
          version: 3,
        },
      ],
      nextCursor: "signed",
    });
    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/mentions/widget?state=unread&limit=20",
      expect.objectContaining({ credentials: "same-origin" }),
    );
  });

  it("sends versioned state and resolve commands and requires authenticated client metadata", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(
        Response.json({ id: "item-1", state: "archived", version: 4 }),
      )
      .mockResolvedValueOnce(
        Response.json({
          href: "#/project?parentID=project-1&mentionOccurrenceID=occurrence-1&sourceID=source-1",
          client_id: "client-2",
          parent_type: "project",
          parent_id: "project-1",
          source_id: "source-1",
          token_id: "token-1",
          source_available: true,
          item_version: 5,
        }),
      )
      .mockResolvedValueOnce(
        Response.json({
          href: "#/work?parentID=work-1&mentionOccurrenceID=occurrence-2",
          parent_type: "work_record",
          parent_id: "work-1",
          source_available: false,
          item_version: 2,
        }),
      );
    const api = createMentionAPI(fetcher);

    await expect(api.changeItemState("item-1", "archived", 3)).resolves.toEqual(
      {
        id: "item-1",
        state: "archived",
        version: 4,
      },
    );
    await expect(
      api.resolveOccurrence("occurrence-1", "item-1", 4),
    ).resolves.toMatchObject({
      clientId: "client-2",
      parentType: "project",
      sourceId: "source-1",
      tokenId: "token-1",
      itemVersion: 5,
    });
    await expect(
      api.resolveOccurrence("occurrence-2", "item-2", 1),
    ).rejects.toMatchObject({
      code: "invalid_response",
    });
    expect(fetcher).toHaveBeenNthCalledWith(
      2,
      "/api/v1/mentions/occurrences/occurrence-1/resolve",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ item_id: "item-1", expected_version: 4 }),
      }),
    );
  });

  it("rejects available resolver links without authenticated token identity", async () => {
    const fetcher = vi.fn().mockResolvedValue(
      Response.json({
        href: "#/work?parentID=work-1&mentionOccurrenceID=occurrence-1&sourceID=source-1",
        client_id: "client-1",
        parent_type: "work_record",
        parent_id: "work-1",
        source_id: "source-1",
        source_available: true,
        item_version: 2,
      }),
    );

    await expect(
      createMentionAPI(fetcher).resolveOccurrence("occurrence-1", "item-1", 1),
    ).rejects.toMatchObject({ code: "invalid_response" });
  });

  it("loads non-null structured and legacy internal content for the exact parent", async () => {
    const fetcher = vi.fn().mockResolvedValue(
      Response.json([
        {
          id: "details-1",
          parent_type: "project",
          parent_id: "project 1",
          source_kind: "details",
          body: "Internal rollout context",
          tokens: [],
          author_id: "staff-1",
          lifecycle_state: "active",
          version: 2,
          created_at: "2026-08-08T12:00:00Z",
          updated_at: "2026-08-08T13:00:00Z",
          read_only: false,
          legacy: false,
        },
        {
          id: "legacy-1",
          parent_type: "project",
          parent_id: "project 1",
          source_kind: "comment",
          body: "Plain historical @text",
          tokens: [],
          author_id: "staff-2",
          lifecycle_state: "active",
          version: 1,
          created_at: "2026-08-07T12:00:00Z",
          updated_at: "2026-08-07T12:00:00Z",
          read_only: true,
          legacy: true,
        },
      ]),
    );
    const signal = new AbortController().signal;

    await expect(
      createMentionAPI(fetcher).list(
        { clientId: "client-1", parentType: "project", parentId: "project 1" },
        signal,
      ),
    ).resolves.toEqual([
      expect.objectContaining({
        id: "details-1",
        parentType: "project",
        sourceKind: "details",
        document: {
          body: "Internal rollout context",
          tokens: [],
          confirmedTeamSnapshots: {},
        },
        readOnly: false,
        legacy: false,
      }),
      expect.objectContaining({
        id: "legacy-1",
        document: {
          body: "Plain historical @text",
          tokens: [],
          confirmedTeamSnapshots: {},
        },
        readOnly: true,
        legacy: true,
      }),
    ]);
    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/projects/project%201/internal-content",
      expect.objectContaining({ credentials: "same-origin", signal }),
    );
    expect(
      new Headers(fetcher.mock.calls[0][1].headers).get("X-Rarity-Client-ID"),
    ).toBe("client-1");
  });

  it("maps redacted content with an empty body and rejects nullable list regions", async () => {
    const redacted = {
      id: "comment-1",
      parent_type: "task",
      parent_id: "task-1",
      source_kind: "comment",
      body: "",
      tokens: [],
      author_id: "staff-1",
      lifecycle_state: "redacted",
      version: 3,
      created_at: "2026-08-08T12:00:00Z",
      updated_at: "2026-08-08T14:00:00Z",
      redacted_at: "2026-08-08T14:00:00Z",
      read_only: false,
      legacy: false,
    };
    await expect(
      createMentionAPI(
        vi.fn().mockResolvedValue(Response.json([redacted])),
      ).list({
        clientId: "client-1",
        parentType: "task",
        parentId: "task-1",
      }),
    ).resolves.toEqual([
      expect.objectContaining({
        lifecycleState: "redacted",
        document: { body: "", tokens: [], confirmedTeamSnapshots: {} },
        redactedAt: "2026-08-08T14:00:00Z",
      }),
    ]);
    await expect(
      createMentionAPI(vi.fn().mockResolvedValue(Response.json(null))).list({
        clientId: "client-1",
        parentType: "task",
        parentId: "task-1",
      }),
    ).rejects.toMatchObject({ code: "invalid_response", status: 502 });
    await expect(
      createMentionAPI(
        vi.fn().mockResolvedValue(Response.json([redacted])),
      ).list({
        clientId: "client-1",
        parentType: "task",
        parentId: "different-task",
      }),
    ).rejects.toMatchObject({ code: "invalid_response", status: 502 });
  });

  it("redacts an exact source with its version fence and maps the terminal state", async () => {
    document.cookie = "rarity_csrf=mention-csrf";
    const fetcher = vi.fn().mockResolvedValue(
      Response.json({
        id: "note-1",
        parent_type: "work_record",
        parent_id: "work-1",
        source_kind: "note",
        body: "",
        tokens: [],
        author_id: "staff-1",
        lifecycle_state: "redacted",
        version: 4,
        created_at: "2026-08-08T12:00:00Z",
        updated_at: "2026-08-08T14:00:00Z",
        redacted_at: "2026-08-08T14:00:00Z",
        read_only: false,
        legacy: false,
      }),
    );

    await expect(
      createMentionAPI(fetcher).redact(
        {
          clientId: "client-1",
          parentType: "work_record",
          parentId: "work-1",
          sourceKind: "note",
        },
        "note-1",
        3,
        "redact-1",
      ),
    ).resolves.toMatchObject({
      id: "note-1",
      lifecycleState: "redacted",
      version: 4,
    });
    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/internal-content/note-1/redact",
      expect.objectContaining({ method: "POST", credentials: "same-origin" }),
    );
    expect(JSON.parse(String(fetcher.mock.calls[0][1].body))).toEqual({
      parent_type: "work_record",
      parent_id: "work-1",
      source_kind: "note",
      expected_version: 3,
      idempotency_key: "redact-1",
    });
  });

  it("strictly maps snake_case people and team candidates and preserves the abort signal", async () => {
    const fetcher = vi.fn().mockResolvedValue(
      Response.json([
        {
          target_type: "staff",
          id: "staff-2",
          label: "Mira",
          version: 3,
        },
        {
          target_type: "team",
          id: "team-1",
          label: "NOC",
          eligible_count: 2,
          excluded_count: 1,
          eligible_member_ids: ["staff-2", "staff-3"],
          version: 4,
        },
      ]),
    );
    const signal = new AbortController().signal;

    await expect(
      createMentionAPI(fetcher).candidates(context, "mi ra", signal),
    ).resolves.toEqual([
      {
        targetType: "staff",
        id: "staff-2",
        label: "Mira",
        version: 3,
      },
      {
        targetType: "team",
        id: "team-1",
        label: "NOC",
        eligibleCount: 2,
        excludedCount: 1,
        eligibleMemberIds: ["staff-2", "staff-3"],
        version: 4,
      },
    ]);
    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/mentions/candidates?parent_type=work_record&parent_id=work+1&source_kind=comment&q=mi+ra",
      expect.objectContaining({
        credentials: "same-origin",
        signal,
      }),
    );
    expect(
      new Headers(fetcher.mock.calls[0][1].headers).get("X-Rarity-Client-ID"),
    ).toBe("client-1");
  });

  it("refreshes one authorized Team by stable identity without a mutable label query", async () => {
    const fetcher = vi.fn().mockResolvedValue(
      Response.json([
        {
          target_type: "team",
          id: "team-1",
          label: "Renamed NOC",
          eligible_count: 1,
          excluded_count: 2,
          eligible_member_ids: ["staff-2"],
          version: 5,
        },
      ]),
    );
    const signal = new AbortController().signal;
    const found = await createMentionAPI(fetcher).candidates(
      context,
      "",
      signal,
      { targetType: "team", targetId: "team-1" },
    );
    expect(found[0]).toMatchObject({
      id: "team-1",
      label: "Renamed NOC",
      version: 5,
    });
    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/mentions/candidates?parent_type=work_record&parent_id=work+1&source_kind=comment&q=&target_type=team&target_id=team-1",
      expect.objectContaining({ signal }),
    );
  });

  it("rejects malformed or unsafe candidate payloads", async () => {
    const malformed = [
      [{ targetType: "staff", id: "staff-2", label: "Mira", version: 1 }],
      [
        {
          target_type: "staff",
          id: "staff-2",
          label: "Mira",
          version: 1,
          eligible_member_ids: ["staff-3"],
        },
      ],
      [
        {
          target_type: "team",
          id: "team-1",
          label: "NOC",
          eligible_count: 2,
          excluded_count: 1,
          eligible_member_ids: ["staff-2"],
          version: 1,
        },
      ],
      [
        {
          target_type: "team",
          id: "team-1",
          label: "NOC",
          eligible_count: 2,
          excluded_count: 1,
          eligible_member_ids: ["staff-3", "staff-2"],
          version: 1,
        },
      ],
    ];
    for (const payload of malformed) {
      const api = createMentionAPI(
        vi.fn().mockResolvedValue(Response.json(payload)),
      );
      await expect(api.candidates(context, "")).rejects.toMatchObject({
        code: "invalid_response",
        status: 502,
      });
    }
  });

  it("preserves structured server errors and native AbortError identity", async () => {
    const failed = createMentionAPI(
      vi
        .fn()
        .mockResolvedValue(
          Response.json(
            { error: { code: "not_found", message: "Resource not found" } },
            { status: 404 },
          ),
        ),
    );
    await expect(failed.candidates(context, "mira")).rejects.toEqual(
      expect.objectContaining({
        code: "not_found",
        status: 404,
        message: "Resource not found",
      }),
    );
    await failed.candidates(context, "mira").catch((error: unknown) => {
      expect(error).toBeInstanceOf(MentionAPIError);
    });

    const aborted = new DOMException("superseded", "AbortError");
    const aborting = createMentionAPI(vi.fn().mockRejectedValue(aborted));
    await expect(aborting.candidates(context, "mira")).rejects.toBe(aborted);
  });

  it("serializes the exact structured document for native internal content", async () => {
    document.cookie = "rarity_csrf=mention-csrf";
    const fetcher = vi.fn().mockResolvedValue(
      Response.json(
        {
          id: "source-1",
          parent_type: "work_record",
          parent_id: "work 1",
          source_kind: "comment",
          body: "Ask @Mira",
          tokens: [
            {
              id: "token-1",
              target_type: "staff",
              target_id: "staff-2",
              label: "@Mira",
              start: 4,
              end: 9,
            },
          ],
          author_id: "staff-1",
          lifecycle_state: "active",
          version: 1,
          created_at: "2026-08-08T12:00:00Z",
          updated_at: "2026-08-08T12:00:00Z",
          read_only: false,
          legacy: false,
        },
        { status: 201 },
      ),
    );
    const value: MentionDocument = {
      body: "Ask @Mira",
      tokens: [
        {
          id: "token-1",
          targetType: "staff",
          targetId: "staff-2",
          label: "@Mira",
          start: 4,
          end: 9,
        },
      ],
      confirmedTeamSnapshots: {},
    };

    const saved = await createMentionAPI(fetcher).save(context, value, {
      expectedVersion: 0,
      idempotencyKey: "request-1",
    });
    expect(saved).toMatchObject({
      id: "source-1",
      sourceKind: "comment",
      document: value,
    });
    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/work-records/work%201/internal-comments",
      expect.objectContaining({ method: "POST", credentials: "same-origin" }),
    );
    const init = fetcher.mock.calls[0][1];
    expect(JSON.parse(String(init.body))).toEqual({
      body: "Ask @Mira",
      tokens: [
        {
          id: "token-1",
          target_type: "staff",
          target_id: "staff-2",
          label: "@Mira",
          start: 4,
          end: 9,
        },
      ],
      confirmed_team_snapshots: {},
      expected_version: 0,
      idempotency_key: "request-1",
    });
    expect(new Headers(init.headers).get("X-Rarity-CSRF")).toBe("mention-csrf");
  });

  it("uses PUT for details creation and PATCH only for an existing source", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        Response.json(
          { error: { code: "not_found", message: "Resource not found" } },
          { status: 404 },
        ),
      );
    const api = createMentionAPI(fetcher);
    const document: MentionDocument = {
      body: "Plain internal details",
      tokens: [],
      confirmedTeamSnapshots: {},
    };
    await api
      .save({ ...context, sourceKind: "details" }, document, {
        expectedVersion: 2,
        idempotencyKey: "details-1",
      })
      .catch(() => undefined);
    await api
      .save(context, document, {
        sourceId: "source-1",
        expectedVersion: 2,
        idempotencyKey: "edit-1",
      })
      .catch(() => undefined);

    expect(fetcher.mock.calls.map(([, init]) => init.method)).toEqual([
      "PUT",
      "PATCH",
    ]);
  });
});

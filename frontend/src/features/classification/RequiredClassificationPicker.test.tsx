import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { RequiredClassificationPicker } from "./RequiredClassificationPicker";

const useCatalog = vi.hoisted(() => vi.fn());

vi.mock("./useClientCatalog", () => ({
  useClientClassificationCatalog: useCatalog,
}));

afterEach(cleanup);

describe("RequiredClassificationPicker", () => {
  it("never presents or preserves the immutable Unclassified fallback as a valid choice", async () => {
    const onChange = vi.fn();
    useCatalog.mockReturnValue({
      state: "ready",
      catalog: {
        groups: [{ id: "system", label: "System", description: "" }],
        tags: [
          {
            id: "fallback",
            label: "Unclassified",
            groupId: "system",
            state: "active",
            synonyms: [],
            version: 1,
            systemManaged: true,
            systemFallback: true,
          },
          {
            id: "vpn",
            label: "VPN",
            groupId: "system",
            state: "active",
            synonyms: [],
            version: 1,
          },
        ],
      },
    });

    render(
      <RequiredClassificationPicker
        clientID="client-a"
        selectedIDs={["fallback", "vpn"]}
        onChange={onChange}
      />,
    );

    await waitFor(() => expect(onChange).toHaveBeenCalledWith(["vpn"]));
    expect(screen.queryByText("Unclassified")).not.toBeInTheDocument();
  });
});

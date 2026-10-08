import { describe, expect, it } from "vitest";
import { CLIENT_CONTEXT_HEADER, clientContextHeaders } from "./clientContext";

describe("clientContextHeaders", () => {
  it("requires and normalizes the explicitly selected Client", () => {
    expect(clientContextHeaders(" client-id ")).toEqual({
      [CLIENT_CONTEXT_HEADER]: "client-id",
    });
    expect(() => clientContextHeaders(" ")).toThrow("client_context_required");
  });
});

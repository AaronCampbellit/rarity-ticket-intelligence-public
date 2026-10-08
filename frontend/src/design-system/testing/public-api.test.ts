import { describe, expect, it } from "vitest";

import * as designSystem from "../index";

describe("design-system public API", () => {
  it("exposes a stable package identity through one entry point", () => {
    expect(designSystem.DESIGN_SYSTEM_NAME).toBe("Rarity");
  });
});

import { describe, it, expect } from "vitest";
import { add } from "./math.js";

describe("add", () => {
  it("adds positive numbers", () => {
    expect(add(1, 2)).toBe(3);
  });

  it("fails on purpose", () => {
    expect(add(1, 1)).toBe(3);
  });

  it.skip("is not ready yet", () => {
    expect(true).toBe(false);
  });

  describe("when nested", () => {
    it("still adds", () => {
      expect(add(2, 2)).toBe(4);
    });
  });
});

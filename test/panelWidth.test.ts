import { describe, expect, it } from "vitest";
import { clampListWidth, LIST_MIN, PANE_MIN } from "../src/lib/prefs/panelWidth";

describe("clampListWidth", () => {
  it("keeps a width that fits", () => {
    expect(clampListWidth(400, 1200)).toBe(400);
  });

  it("raises a narrow list to the minimum", () => {
    expect(clampListWidth(100, 1200)).toBe(LIST_MIN);
  });

  it("leaves the reading pane its minimum", () => {
    expect(clampListWidth(1100, 1200)).toBe(1200 - PANE_MIN);
  });

  it("prefers the list minimum when the container fits neither", () => {
    expect(clampListWidth(500, 300)).toBe(LIST_MIN);
  });

  it("rounds to a whole pixel", () => {
    expect(clampListWidth(300.6, 1200)).toBe(301);
  });
});

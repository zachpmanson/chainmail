import { describe, expect, it } from "vitest";
import { toggleRow, toggleShown, type RowSelection } from "../src/lib/inbox/rangeSelection";

const shown = ["A", "B", "C", "D", "E"];
const members = (selection: RowSelection) => [...selection.chosen].sort();

describe("shown-order range selection", () => {
  it.each([
    ["A", "D"],
    ["D", "A"],
  ])("selects an inclusive %s → %s range", (anchor, end) => {
    const first = toggleRow({ chosen: ["independent"] }, shown, anchor);
    const result = toggleRow(first, shown, end, true);
    expect(members(result)).toEqual(["A", "B", "C", "D", "independent"]);
    expect(result.anchor).toBe(anchor);
    expect(first.chosen).toEqual(["independent", anchor]);
  });
  it("deselects the range when the endpoint is being unchecked", () => {
    const start = { chosen: ["A", "B", "C", "D", "independent"], anchor: "A" };
    expect(toggleRow(start, shown, "C", true)).toEqual({
      chosen: ["D", "independent"],
      anchor: "A",
    });
    expect(start.chosen).toEqual(["A", "B", "C", "D", "independent"]);
  });
  it("retains the last ordinary anchor through repeated range clicks", () => {
    const first = toggleRow({ chosen: [] }, shown, "B");
    const second = toggleRow(first, shown, "D", true);
    const third = toggleRow(second, shown, "E", true);
    expect(members(third)).toEqual(["B", "C", "D", "E"]);
    expect(third.anchor).toBe("B");
  });
  it("an ordinary deselection sets the next anchor too", () => {
    const first = toggleRow({ chosen: ["C"] }, shown, "C");
    expect(first).toEqual({ chosen: [], anchor: "C" });
    expect(members(toggleRow(first, shown, "E", true))).toEqual(["C", "D", "E"]);
  });
  it("uses current order, including newly loaded rows", () => {
    const first = toggleRow({ chosen: [] }, shown, "B");
    const reordered = ["E", "B", "new", "A", "C", "D"];
    expect(members(toggleRow(first, reordered, "A", true))).toEqual(["A", "B", "new"]);
  });
  it.each([undefined, "missing"])("falls back to an ordinary toggle with anchor %s", (anchor) => {
    expect(toggleRow({ chosen: ["independent"], anchor }, shown, "C", true)).toEqual({
      chosen: ["independent", "C"],
      anchor: "C",
    });
  });
  it("ignores a missing endpoint without changing its anchor", () => {
    const first = { chosen: ["A"], anchor: "A" };
    expect(toggleRow(first, shown, "missing", true)).toBe(first);
  });
  it("uses a captured native checked value instead of re-toggling it", () => {
    expect(members(toggleRow({ chosen: ["A", "C"], anchor: "A" }, shown, "C", true, true))).toEqual(
      ["A", "B", "C"],
    );
    expect(members(toggleRow({ chosen: ["A"], anchor: "A" }, shown, "C", true, false))).toEqual([]);
  });
});

describe("select all shown only", () => {
  it("fills a mixed selection and clears the anchor without selecting an unloaded ID", () => {
    const next = toggleShown({ chosen: ["B", "independent"], anchor: "B" }, shown);
    expect(members(next)).toEqual(["A", "B", "C", "D", "E", "independent"]);
    expect(next.anchor).toBeUndefined();
    expect(next.chosen).not.toContain("unloaded");
  });
  it("removes only shown IDs from an all-checked list", () => {
    expect(toggleShown({ chosen: ["independent", ...shown], anchor: "A" }, shown)).toEqual({
      chosen: ["independent"],
    });
  });
  it("does nothing to empty lists", () => {
    const first = { chosen: ["independent"], anchor: "A" };
    expect(toggleShown(first, [])).toBe(first);
  });
  it("supports captured native header intent and deduplicates rows", () => {
    expect(toggleShown({ chosen: ["A"] }, ["A", "A", "B"], true).chosen).toEqual(["A", "B"]);
    expect(toggleShown({ chosen: ["A"] }, ["A", "B"], false).chosen).toEqual([]);
  });
});

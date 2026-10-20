import { describe, expect, it } from "vitest";
import { nextCursor, uniqueChains } from "../src/lib/inbox/threadPages";

const chain = (rootExtId: string, last: string) => ({ rootExtId, last });

describe("nextCursor", () => {
  it("ends only when the server supplies no continuation", () => {
    expect(nextCursor({})).toBeUndefined();
    expect(nextCursor({ nextCursor: "" })).toBeUndefined();
  });

  it("passes through opaque cursors even when consecutive pages share timestamps", () => {
    expect(nextCursor({ nextCursor: "tied-second-root-50" })).toBe("tied-second-root-50");
    expect(nextCursor({ nextCursor: "tied-second-root-100" })).toBe("tied-second-root-100");
  });
});

describe("uniqueChains", () => {
  it("keeps the first copy if a changing corpus repeats a chain across pages", () => {
    const rows = uniqueChains([
      { chains: [chain("a", "3"), chain("b", "2")] },
      { chains: [chain("b", "1"), chain("c", "0")] },
      {},
    ]);
    expect(rows.map((r) => `${r.rootExtId}${r.last}`)).toEqual(["a3", "b2", "c0"]);
  });
});

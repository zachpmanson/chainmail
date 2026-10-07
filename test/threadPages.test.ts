import { describe, expect, it } from "vitest";
import { nextCursor, PAGE, uniqueChains } from "../src/lib/inbox/threadPages";

const chain = (rootExtId: string, last: string) => ({ rootExtId, last });

/** A full page of threads `prefix0…`, stamped from `from` down; fixed width so they sort as text. */
const page = (prefix: string, from: number) => ({
  chains: Array.from({ length: PAGE }, (_, i) =>
    chain(`${prefix}${i}`, String(from - i).padStart(4, "0")),
  ),
});

describe("nextCursor", () => {
  it("ends on a short page", () => {
    const short = { chains: [chain("a", "0001")] };
    expect(nextCursor(short, [short], "")).toBeUndefined();
  });

  it("pages from the oldest thread on a full first page", () => {
    const first = page("a", 199);
    expect(nextCursor(first, [first], "")).toBe("0150");
  });

  it("goes on when a page moves the cursor and adds threads", () => {
    const next = page("b", 149);
    expect(nextCursor(next, [page("a", 199), next], "0150")).toBe("0100");
  });

  it("stops when the cursor doesn't move", () => {
    const stuck = page("b", 199);
    expect(nextCursor(stuck, [page("a", 199), stuck], "0150")).toBeUndefined();
  });

  it("stops when a page adds no new threads", () => {
    const again = page("a", 149);
    expect(nextCursor(again, [page("a", 199), again], "0150")).toBeUndefined();
  });
});

describe("uniqueChains", () => {
  it("keeps the first copy of a thread that straddles pages", () => {
    const rows = uniqueChains([
      { chains: [chain("a", "3"), chain("b", "2")] },
      { chains: [chain("b", "1"), chain("c", "0")] },
      {},
    ]);
    expect(rows.map((r) => `${r.rootExtId}${r.last}`)).toEqual(["a3", "b2", "c0"]);
  });
});

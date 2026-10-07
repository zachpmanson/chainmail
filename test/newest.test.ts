import { describe, expect, it } from "vitest";
import { newest } from "../src/lib/inbox/newest";

describe("newest", () => {
  it("picks the latest timestamp regardless of position", () => {
    const es = [
      { id: "b", ts: "2026-03-02T09:00:00Z" },
      { id: "c", ts: "2026-03-04T09:00:00Z" },
      { id: "a", ts: "2026-03-01T09:00:00Z" },
    ];
    expect(newest(es)?.id).toBe("c");
  });

  it("keeps the first of a tie, and is undefined for nothing", () => {
    const ts = "2026-03-02T09:00:00Z";
    expect(
      newest([
        { id: "x", ts },
        { id: "y", ts },
      ])?.id,
    ).toBe("x");
    expect(newest([])).toBeUndefined();
  });
});

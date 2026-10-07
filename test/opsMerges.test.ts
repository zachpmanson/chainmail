import { describe, expect, it } from "vitest";
import { applyMergeBatch, ruleLabel, type MergeTarget } from "../src/lib/ops/opsMerges";

describe("ruleLabel", () => {
  it("labels known dedupe rules and passes others through", () => {
    expect(ruleLabel("dedupe:first-name-and-org")).toBe("first name at one organisation");
    expect(ruleLabel("dedupe:new-rule")).toBe("dedupe:new-rule");
  });
});

describe("applyMergeBatch", () => {
  const pairs: MergeTarget[] = [
    { keepId: 10, dropId: 20 },
    { keepId: 10, dropId: 30 },
    { keepId: 10, dropId: 40 },
  ];

  const run = async (refuse?: number) => {
    const sent: number[] = [];
    const progress: string[] = [];
    const merged: number[] = [];
    const result = await applyMergeBatch(
      pairs,
      async (t) => {
        sent.push(t.dropId);
        if (t.dropId === refuse) throw new Error("plan changed");
      },
      (p) => progress.push(p),
      (id) => merged.push(id),
    );
    return { result, sent, progress, merged };
  };

  it("applies every pair in order", async () => {
    expect(await run()).toEqual({
      result: { merged: 3, total: 3 },
      sent: [20, 30, 40],
      progress: ["1 of 3", "2 of 3", "3 of 3"],
      merged: [20, 30, 40],
    });
  });

  it("stops at the first refusal and reports it", async () => {
    expect(await run(30)).toEqual({
      result: {
        merged: 1,
        total: 3,
        refusal: { target: pairs[1], error: new Error("plan changed") },
      },
      sent: [20, 30],
      progress: ["1 of 3", "2 of 3"],
      merged: [20],
    });
  });

  it("does nothing for an empty batch", async () => {
    const calls: string[] = [];
    const result = await applyMergeBatch(
      [],
      async () => calls.push("apply"),
      () => calls.push("progress"),
      () => calls.push("merged"),
    );
    expect(result).toEqual({ merged: 0, total: 0 });
    expect(calls).toEqual([]);
  });
});

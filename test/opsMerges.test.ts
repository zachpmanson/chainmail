import { describe, expect, it, vi } from "vitest";
import { applyMergeBatch } from "../src/lib/opsMerges";

describe("applying an ops merge batch", () => {
  it("applies pairs in order and stops at the first refusal", async () => {
    const first = { keepId: 10, dropId: 20 };
    const second = { keepId: 10, dropId: 30 };
    const third = { keepId: 10, dropId: 40 };
    const apply = vi.fn(async (target: typeof first) => {
      if (target.dropId === 30) throw new Error("plan changed");
    });
    const progress: string[] = [];
    const merged: number[] = [];

    const result = await applyMergeBatch(
      [first, second, third],
      apply,
      (value) => progress.push(value),
      (dropId) => merged.push(dropId),
    );

    expect(apply.mock.calls.map(([target]) => target.dropId)).toEqual([20, 30]);
    expect(progress).toEqual(["1 of 3", "2 of 3"]);
    expect(merged).toEqual([20]);
    expect(result).toEqual({
      merged: 1,
      total: 3,
      refusal: { target: second, error: new Error("plan changed") },
    });
  });

  it("reports an empty batch without attempting a write", async () => {
    const apply = vi.fn();
    const progress = vi.fn();
    const merged = vi.fn();

    await expect(applyMergeBatch([], apply, progress, merged)).resolves.toEqual({
      merged: 0,
      total: 0,
    });
    expect(apply).not.toHaveBeenCalled();
    expect(progress).not.toHaveBeenCalled();
    expect(merged).not.toHaveBeenCalled();
  });
});

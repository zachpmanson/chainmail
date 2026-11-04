import { createElement as h } from "react";
import { act, create } from "react-test-renderer";
import { expect, it } from "vitest";
import useScopedSelection from "../src/lib/inbox/useScopedSelection";

function fixture() {
  let current: ReturnType<typeof useScopedSelection>;
  function Probe({ scope, rows }: { scope: string; rows: string[] }) {
    current = useScopedSelection(scope, rows);
    return null;
  }
  let rendered: ReturnType<typeof create>;
  const show = (scope: string, rows: string[]) =>
    act(() => {
      const element = h(Probe, { scope, rows });
      if (rendered) rendered.update(element);
      else rendered = create(element);
    });
  return { show, get: () => current!, stop: () => act(() => rendered!.unmount()) };
}

it("uses current displayed order even for a retained callback in the same scope", () => {
  const f = fixture();
  try {
    f.show("work", ["A", "B", "C", "D"]);
    act(() => f.get().toggle("A"));
    const prior = f.get();
    f.show("work", ["D", "A", "new", "B", "C"]);
    act(() => prior.toggle("B", true));
    expect(f.get().chosen).toEqual(["A", "new", "B"]);
    expect(f.get().mixed).toBe(true);
    expect(f.get().allSelected).toBe(false);
  } finally {
    f.stop();
  }
});

it("does not select appended rows automatically and resets the anchor on header operations", () => {
  const f = fixture();
  try {
    f.show("work", ["A", "B"]);
    act(() => f.get().toggle("A"));
    act(() => f.get().toggleAll(true));
    expect(f.get().allSelected).toBe(true);
    f.show("work", ["A", "B", "C"]);
    expect(f.get().chosen).toEqual(["A", "B"]);
    expect(f.get().mixed).toBe(true);
    act(() => f.get().toggle("B", true));
    expect(f.get().chosen).toEqual(["A"]);
    act(() => f.get().clearChosen());
    act(() => f.get().toggle("C", true));
    expect(f.get().chosen).toEqual(["C"]);
    act(() => f.get().toggleAll(false));
    expect(f.get().canClear).toBe(false);
  } finally {
    f.stop();
  }
});

it("header state counts only shown IDs and leaves hidden independent selections alone", () => {
  const f = fixture();
  try {
    f.show("work", ["hidden", "A", "B"]);
    act(() => f.get().toggle("hidden"));
    f.show("work", ["A", "B"]);
    expect(f.get().mixed).toBe(false);
    expect(f.get().allSelected).toBe(false);
    act(() => f.get().toggleAll());
    expect(f.get().chosen).toEqual(["hidden", "A", "B"]);
    expect(f.get().allSelected).toBe(true);
    act(() => f.get().toggleAll());
    expect(f.get().chosen).toEqual(["hidden"]);
    expect(f.get().mixed).toBe(false);
    f.show("work", []);
    expect(f.get().allSelected).toBe(false);
    expect(f.get().mixed).toBe(false);
  } finally {
    f.stop();
  }
});

it("clears anchor and rejects stale callbacks even after returning to a previous scope", () => {
  const f = fixture();
  try {
    f.show("work", ["A", "B", "C"]);
    act(() => f.get().toggle("A"));
    const old = f.get();
    f.show("personal", ["A", "B", "C"]);
    f.show("work", ["A", "B", "C"]);
    act(() => {
      old.toggle("B", true);
      old.toggleAll();
      old.clearChosen();
    });
    expect(f.get().chosen).toEqual([]);
    expect(f.get().canClear).toBe(false);
    act(() => f.get().toggle("C", true));
    expect(f.get().chosen).toEqual(["C"]);
  } finally {
    f.stop();
  }
});

it("keeps clear enabled for a deselected anchor until clear resets it", () => {
  const f = fixture();
  try {
    f.show("work", ["A", "B", "C"]);
    act(() => f.get().toggle("A"));
    act(() => f.get().toggle("A"));
    expect(f.get().chosen).toEqual([]);
    expect(f.get().canClear).toBe(true);
    act(() => f.get().clearChosen());
    expect(f.get().canClear).toBe(false);
    act(() => f.get().toggle("C", true));
    expect(f.get().chosen).toEqual(["C"]);
  } finally {
    f.stop();
  }
});

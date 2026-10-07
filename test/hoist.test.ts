import { describe, expect, it } from "vitest";
import { hoistEdits } from "../src/lib/thread/hoist";
import { corpus } from "./fixtures";

const ids = (es: { extId: string }[]) => es.map((e) => e.extId);

describe("hoistEdits", () => {
  it("leaves a thread with no edits as it is", () => {
    const { shown, parentOf } = hoistEdits([corpus("a"), corpus("b", { parent: "a" })]);
    expect(ids(shown)).toEqual(["a", "b"]);
    expect([...parentOf]).toEqual([
      ["a", undefined],
      ["b", "a"],
    ]);
  });

  it("hides an edited copy and re-points its replies at the copy's parent", () => {
    const { shown, parentOf } = hoistEdits([
      corpus("a"),
      corpus("copy", { parent: "a" }),
      corpus("quoter", { parent: "a", edits: [{ id: "copy", base: "a" }] }),
      corpus("reply", { parent: "copy" }),
    ]);
    expect(ids(shown)).toEqual(["a", "quoter", "reply"]);
    expect(parentOf.get("reply")).toBe("a");
  });

  it("walks past a chain of hoisted copies", () => {
    const { parentOf } = hoistEdits([
      corpus("a"),
      corpus("c1", { parent: "a" }),
      corpus("c2", { parent: "c1" }),
      corpus("q", { edits: [{ id: "c1" }, { id: "c2" }] }),
      corpus("reply", { parent: "c2" }),
    ]);
    expect(parentOf.get("reply")).toBe("a");
  });

  it("drops a parent the corpus does not hold", () => {
    const { parentOf } = hoistEdits([corpus("b", { parent: "gone" })]);
    expect(parentOf.get("b")).toBeUndefined();
  });

  it("ignores an edit naming an id the thread does not have", () => {
    const { shown } = hoistEdits([corpus("a"), corpus("q", { edits: [{ id: "elsewhere" }] })]);
    expect(ids(shown)).toEqual(["a", "q"]);
  });

  it("stops on a cycle of hoisted copies", () => {
    const { shown, parentOf } = hoistEdits([
      corpus("x", { parent: "y" }),
      corpus("y", { parent: "x" }),
      corpus("q", { edits: [{ id: "x" }, { id: "y" }] }),
      corpus("reply", { parent: "x" }),
    ]);
    expect(ids(shown)).toEqual(["q", "reply"]);
    // The walk halts at the first repeat, which is itself a hoisted id.
    expect(parentOf.get("reply")).toBe("x");
  });
});

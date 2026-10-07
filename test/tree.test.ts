import { describe, expect, it } from "vitest";
import { tree, type Knot } from "../src/lib/thread/tree";

/** "a(b(d),c)": a message, then its replies in brackets. */
const shape = (forest: Knot<string>[]): string =>
  forest.map((k) => (k.replies.length ? `${k.entry}(${shape(k.replies)})` : k.entry)).join(",");

const drawn = (parents: Record<string, string | undefined>) =>
  shape(
    tree(
      Object.keys(parents),
      (k) => k,
      (k) => parents[k],
    ),
  );

describe("tree", () => {
  it("nests replies under their parent, siblings in input order", () => {
    expect(drawn({ a: undefined, b: "a", c: "a", d: "b" })).toBe("a(b(d),c)");
  });

  it("leaves unrelated entries as a flat list of roots", () => {
    expect(drawn({ a: undefined, b: undefined, c: undefined })).toBe("a,b,c");
  });

  it("roots an entry whose parent is absent, empty or itself", () => {
    expect(drawn({ a: "gone", b: "", c: "c", d: "a" })).toBe("a(d),b,c");
  });

  it("goes as deep as the chain does", () => {
    expect(drawn({ a: undefined, b: "a", c: "b", d: "c" })).toBe("a(b(c(d)))");
  });

  it("appends entries caught in a cycle as roots at the end", () => {
    expect(drawn({ r: undefined, x: "y", y: "x" })).toBe("r,x(y)");
  });

  it("draws a repeated key once", () => {
    const forest = tree(
      ["a", "a", "b"],
      (k) => k,
      (k) => (k === "b" ? "a" : undefined),
    );
    expect(shape(forest)).toBe("a(b)");
  });
});

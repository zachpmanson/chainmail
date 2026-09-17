import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { nest } from "../src/lib/threading";

/**
 * Reddit-style nesting, as a graph problem — no DOM, no corpus, no thread: the
 * function knows an id and the id it answers, and everything here is built out of
 * those two.
 *
 * The invented thread is the shape the switch exists for: Ada opens, Bo and Cy
 * both answer Ada, and Ada answers Bo. Flat, the transcript reads Ada, Bo, Cy,
 * Ada; the answer to Bo is the last bubble on the page, under a message it has
 * nothing to do with.
 */
const THREAD: Record<string, string | undefined> = {
  a: undefined,
  b: "a",
  c: "a",
  d: "b",
};

const keys = ["a", "b", "c", "d"];

/** The order and the indent, which is the whole of what nesting produces. */
const drawn = (order: string[], parents: Record<string, string | undefined> = THREAD) =>
  nest(order, (k) => k, (k) => parents[k]).map((n) => `${n.entry}:${n.depth}`);

describe("a thread drawn as a tree", () => {
  it("puts every reply under the message it answers, and indents it", () => {
    // Ada opens, Bo answers her, Ada answers Bo, and Cy — who answered the
    // opener too — is back at Bo's level rather than after Ada's answer to him.
    // The clock is not violated by the move: Cy's message is still drawn after
    // Ada's opener, which is the only ordering the flat view ever promised.
    expect(drawn(keys)).toEqual(["a:0", "b:1", "d:2", "c:1"]);
  });

  it("leaves a thread with no replies exactly as the transcript had it", () => {
    // The switch has to be safe on the mail it does nothing for: notification and
    // automated threads are most of a mailbox and most of them have no graph at
    // all, and a switch that reordered those would be a switch nobody leaves on.
    expect(drawn(keys, { a: undefined, b: undefined, c: undefined, d: undefined })).toEqual([
      "a:0",
      "b:0",
      "c:0",
      "d:0",
    ]);
  });

  it("keeps a reply's own replies in the transcript's order", () => {
    // Two answers to the same message are siblings, and their order is the one
    // thing the flat view was already right about.
    expect(drawn(keys, { a: undefined, b: "a", c: "a", d: "a" })).toEqual([
      "a:0",
      "b:1",
      "c:1",
      "d:1",
    ]);
  });

  it("counts depth along the chain, not by how many messages precede it", () => {
    const deep = { a: undefined, b: "a", c: "b", d: "c" };
    expect(drawn(keys, deep)).toEqual(["a:0", "b:1", "c:2", "d:3"]);
  });

  it("counts as deep as a thread goes, with nothing rounded off", () => {
    // No limit on the tree: a chain of thirty answers is thirty levels, and the
    // depth a bubble is handed is exactly that. There is no cap to test against in
    // the walk — the stylesheet is where one could be introduced, and is where the
    // guard for it lives (see below).
    const parents: Record<string, string | undefined> = { m0: undefined };
    const chain = ["m0"];
    for (let i = 1; i < 30; i++) {
      chain.push(`m${i}`);
      parents[`m${i}`] = `m${i - 1}`;
    }
    const out = nest(chain, (k) => k, (k) => parents[k]);
    expect(out.map((n) => n.depth)).toEqual(chain.map((_, i) => i));
  });

  it("is indented by the stylesheet all the way in, with no depth capped", () => {
    // The rule the reader actually sees, read as text: an indent is one step per
    // level and *every* level, which is a fact about the stylesheet rather than
    // about the walk above. A `min()` creeping back in here would leave a reply
    // twenty deep sharing a column with one six deep — a rendering fault, not a
    // limit — and no DOM test can see it, because jsdom computes no cascade.
    const css = readFileSync("src/select.css", "utf8");
    expect(css).toContain("margin-left:calc(var(--nest,0) * var(--step))");
    expect(css).toContain("background-size:calc(var(--nest,0) * var(--step)) 100%");
    expect(css).not.toMatch(/min\(var\(--nest/);
  });

  it("opens a tree at a reply whose parent the corpus does not hold", () => {
    // `/v1/chains` answers a thread whole, so this is a message the corpus is
    // missing rather than one it has not sent yet — and the bubble the reader is
    // looking at is still mail, at the top level, with its own replies under it.
    // Neither of the two cases the flat view draws no arrow for may go missing.
    expect(drawn(keys, { a: undefined, b: "gone", c: "b", d: "a" })).toEqual([
      "a:0",
      "d:1",
      "b:0",
      "c:1",
    ]);
  });

  it("draws a cycle flat rather than dropping mail", () => {
    // Not possible in a mailbox and not impossible in a corpus that reconstructs
    // one: a and b answer each other, so neither is reachable from an opener.
    // Both are still drawn, in the transcript's order, and c — which hangs off
    // one of them — comes with them rather than being cut off by the cycle.
    expect(drawn(keys, { a: "b", b: "a", c: "a", d: undefined })).toEqual([
      "d:0",
      "a:0",
      "b:1",
      "c:1",
    ]);
  });

  it("draws a message that answers itself as an opener", () => {
    expect(drawn(keys, { a: undefined, b: "b", c: "a", d: undefined })).toEqual([
      "a:0",
      "c:1",
      "b:0",
      "d:0",
    ]);
  });

  it("draws every message exactly once, whatever the graph says", () => {
    // The one promise the switch cannot break, asserted against the case most
    // likely to break it: a graph with a cycle, a self-reply, a missing parent
    // and a duplicate handle in it.
    const parents: Record<string, string | undefined> = {
      a: "c",
      b: "a",
      c: "a",
      d: "gone",
    };
    // The walk starts at openers, so the one message that can be placed — the
    // reply to a parent no one has — is drawn first and the cycle follows it,
    // flat. What matters is not where they land but that all four are here.
    const out = nest(["a", "b", "c", "d", "a"], (k) => k, (k) => parents[k]);
    expect(out.map((n) => n.entry)).toEqual(["d", "a", "b", "c"]);
    expect(new Set(out.map((n) => n.entry)).size).toBe(out.length);
  });
});

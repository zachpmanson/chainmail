import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { tree, type Knot } from "../src/lib/tree";

/**
 * The reply tree, as a graph problem — no DOM, no corpus, no thread: the
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

/** The drawn order and the indent, read back off the forest: the pane draws a
 *  message's replies inside a container of their own, so how deep a message is
 *  is how many containers it sits in — walked here the way the DOM nests them. */
const flat = (forest: Knot<string>[], depth = 0): string[] =>
  forest.flatMap((k) => [`${k.entry}:${depth}`, ...flat(k.replies, depth + 1)]);

/** The drawn order and the indent, which is the whole of what the tree produces. */
const drawn = (order: string[], parents: Record<string, string | undefined> = THREAD) =>
  flat(tree(order, (k) => k, (k) => parents[k]));

/** The forest as one string, in the shape the pane draws it: a message, then its
 *  replies in brackets and in the transcript's own order. The recursion is the
 *  point — this is the structure the containers come from, and a cycle that was
 *  not cut would not survive being written down. */
const shape = (forest: Knot<string>[]): string =>
  forest.map((k) => (k.replies.length ? `${k.entry}(${shape(k.replies)})` : k.entry)).join(",");

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
    const out = flat(tree(chain, (k) => k, (k) => parents[k]));
    expect(out.map((n) => Number(n.split(":")[1]))).toEqual(chain.map((_, i) => i));
  });

  it("hands each message its replies, in the containers the pane draws", () => {
    // The one thing beyond the order and the depth: which replies belong to
    // which message. A reply is in its parent's own list rather than merely
    // after it in a walk, because a container per message's replies is what the
    // pane renders and the line beside a level is that container's border (see
    // .ibread .stream .replies). Flat, one container per message with nothing in
    // it — which draws nothing at all.
    expect(shape(tree(keys, (k) => k, (k) => THREAD[k]))).toBe("a(b(d),c)");
  });

  it("opens a tree per message the corpus cannot place", () => {
    expect(shape(tree(keys, (k) => k, (k) => ({ a: undefined, b: "gone", c: "b", d: "a" })[k]))).toBe(
      "a(d),b(c)",
    );
  });

  it("cuts a cycle rather than building a tree with no bottom", () => {
    // The pane recurses over this structure, so a cycle is not a rendering
    // nicety: an a→b→a forest would draw until the pane gave up, and the walk is
    // the only thing standing between a malformed corpus and that.
    expect(shape(tree(keys, (k) => k, (k) => ({ a: "b", b: "a", c: "a", d: undefined })[k]))).toBe(
      "d,a(b,c)",
    );
  });

  it("is indented by the stylesheet all the way in, with no depth capped", () => {
    // The rule the reader actually sees, read as text. An indent is one step per
    // level and *every* level, which is a fact about the stylesheet rather than
    // about the walk above: a `min()` creeping back in here would leave a reply
    // twenty deep sharing a column with one six deep — a rendering fault, not a
    // limit — and no DOM test can see it, because jsdom computes no cascade.
    const css = readFileSync("src/select.css", "utf8");
    const replies = css.slice(css.indexOf(".ibread .stream .replies {"));
    const rule = replies.slice(0, replies.indexOf("}"));
    // The line is the container's own border, which is what makes it one rule
    // per subtree rather than a hairline per bubble in the margin: a border only
    // exists where a container does, and a container only exists where a message
    // has replies, so nothing has to count levels and nothing can dangle.
    expect(rule).toContain("border-left:2px solid var(--line)");
    // And the step is that box's own margin and padding, one place, no cap: the
    // fourth value of each shorthand, since the third is the gap the line is drawn
    // through and the first is for the box above the card it hangs off. How far in
    // the step goes is taste rather than a fact about the tree, so the numbers are
    // not asserted here — what is asserted is that they are one plain measure each
    // and that nothing derives a step from a depth.
    expect(rule).toMatch(/margin:-.5rem 0 0 [\d.]+rem/);
    expect(rule).toMatch(/padding:.5rem 0 0 [\d.]+rem/);
    expect(rule).not.toMatch(/min\(/);
    // The per-bubble indent and its gradient hairlines are gone: a bubble is
    // never handed a depth, and the stylesheet has no rule that would read one.
    expect(css).not.toContain("--nest");
    expect(css).not.toContain("--step");
  });

  it("hangs the line off the message it belongs to, with no gap under the card", () => {
    // The line is the container's border, so where the container's box starts is
    // where the line starts. A card's own bottom margin would put that start half a
    // rem below the card — a gap in the line, and (since the mark follows the line)
    // a strip the pointer crosses with nothing marked, once per level, which is what
    // a reader sees as flicker. The margin is taken back and spent inside the box.
    const css = readFileSync("src/select.css", "utf8");
    const replies = css.slice(css.indexOf(".ibread .stream .replies {"));
    const rule = replies.slice(0, replies.indexOf("}"));
    expect(rule).toMatch(/margin:-.5rem 0 0 [\d.]+rem/);
    expect(rule).toMatch(/padding:.5rem 0 0 [\d.]+rem/);
  });

  it("marks the path to the message pointed at, on the lines, and fades it in", () => {
    // Pointing at a message lights every line from it up to the root, and marks the
    // bubbles not at all: a path drawn on six cards is six rings to read, and the
    // reader already knows which message they are pointing at. What they cannot see
    // is the way down to it, which is what the lines are for.
    //
    // The class is put there by client/behaviour rather than by a `:hover` rule, and
    // the assertion that no `:has()` is doing it is the point of the test rather than
    // a detail of the CSS: lines nest, so a hovered line hovers every container it is
    // nested in, and a `:has()` rule answering the pointer marks a whole ancestry of
    // messages from one point.
    const css = readFileSync("src/styles.css", "utf8");
    const marked = css.slice(css.indexOf(".ibread .stream .replies.rhov {"));
    const paint = marked.slice(0, marked.indexOf("}"));
    // The mark is a shade of the line it is drawn on — the line's own colour mixed
    // towards the muted ink — and not the accent. The accent is the other mark's
    // voice: it says "this one" about a bubble the reader set the pointer down on,
    // and a pointer crossing a thread on its way somewhere else has not asked a
    // question that deserves an answer in that volume. How far the mix goes is taste
    // and is not asserted here; which two colours it is made of is the decision.
    expect(paint).toContain("color-mix(in srgb, var(--line)");
    expect(paint).toContain("var(--muted)");
    expect(paint).not.toContain("var(--accent)");
    expect(css).not.toContain(".msg.rhov");
    expect(css).not.toContain(":has(+ .replies");
    // The fade is on the resting rule, since one declared by the arriving rule would
    // fade in and then vanish, and it is guarded like the rest of the sheet's motion.
    const motion = css.slice(css.indexOf("@media (prefers-reduced-motion: no-preference)", css.indexOf(".mhov")));
    const block = motion.slice(0, motion.indexOf("} }"));
    expect(block).toContain("transition:border-left-color");
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
    // flat. What matters is not where they land but that all four are here, once
    // each, however many times the graph names them.
    const forest = tree(["a", "b", "c", "d", "a"], (k) => k, (k) => parents[k]);
    const out = flat(forest).map((n) => n.split(":")[0]!);
    expect(out).toEqual(["d", "a", "b", "c"]);
    expect(new Set(out).size).toBe(out.length);
  });
});

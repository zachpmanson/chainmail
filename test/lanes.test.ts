import { describe, expect, it } from "vitest";
import { graphLanes, isMeta, layout } from "../src/lib/timeline/lanes";
import { order } from "../src/lib/timeline/chronological";
import type { Entry } from "../src/lib/timeline/spec";
import { idsFor, load, msg } from "./fixtures";

const byId = (e: Entry) => e.id!;

describe("isMeta", () => {
  it("reads scheduling noise by its leading token only", () => {
    expect(isMeta(msg("a", { body: "<p>Invitation: standup</p>" }))).toBe(true);
    expect(isMeta(msg("b", { subject: "Accepted: review" }))).toBe(true);
    expect(isMeta(msg("c", { body: "<p>see the invitation I sent</p>" }))).toBe(false);
  });

  it("treats a note as meta, and an explicit flag wins", () => {
    expect(isMeta(msg("n", { kind: "note" }))).toBe(true);
    expect(isMeta(msg("d", { body: "<p>Invitation: x</p>", meta: false }))).toBe(false);
  });
});

describe("layout", () => {
  it("groups threads and reuses a lane once its thread has ended", () => {
    const es = [
      msg("a"),
      msg("a2", { parent: "a" }),
      msg("b"),
      msg("b2", { parent: "b" }),
      msg("a3", { parent: "a2" }),
      msg("c"),
    ];
    const l = layout(es, byId);
    expect(l.chains.map((c) => [c.root, c.entries, c.lane])).toEqual([
      ["a", ["a", "a2", "a3"], 0],
      ["b", ["b", "b2"], 1],
      ["c", ["c"], 0],
    ]);
    expect(l.laneCount).toBe(2);
    expect(l.row.get("a")).toBe(2);
  });

  it("marks a thread meta only when every entry is", () => {
    const es = [
      msg("a", { subject: "Invitation: sync" }),
      msg("a2", { parent: "a", subject: "Accepted: sync" }),
      msg("b", { subject: "Invitation: sync" }),
      msg("b2", { parent: "b", body: "<p>Can we move it?</p>" }),
    ];
    expect(layout(es, byId).chains.map((c) => c.meta)).toEqual([true, false]);
  });

  it("lays the synthetic spec out as 7 threads in 4 lanes", () => {
    const t = load("synthetic");
    const idOf = idsFor(t.messages);
    const l = layout(order(t.messages, idOf), idOf);
    expect(l.chains).toHaveLength(7);
    expect(l.laneCount).toBe(4);
  });
});

describe("graphLanes", () => {
  it("keeps a first reply in its parent's lane and forks later ones", () => {
    const g = graphLanes(
      [msg("a"), msg("b", { parent: "a" }), msg("c", { parent: "a" }), msg("d", { parent: "b" })],
      byId,
    );
    expect(g.nodes.map((n) => [n.id, n.lane, n.isFork, n.isRoot, n.isLeaf])).toEqual([
      ["a", 0, false, true, false],
      ["b", 0, true, false, false],
      ["c", 1, true, false, true],
      ["d", 0, false, false, true],
    ]);
    expect([g.laneCount, g.forks, g.roots, g.leaves]).toEqual([2, 1, 1, 2]);
  });

  it("frees a lane once its holder has no replies to come", () => {
    const g = graphLanes([msg("a"), msg("b")], byId);
    expect(g.nodes.map((n) => n.lane)).toEqual([0, 0]);
    expect(g.laneCount).toBe(1);
  });

  it("treats a parent outside the list as a root", () => {
    const g = graphLanes([msg("a", { parent: "gone" })], byId);
    expect(g.nodes[0]).toMatchObject({ parent: undefined, isRoot: true });
  });
});

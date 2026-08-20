import { describe, expect, it } from "vitest";
import { existsSync, readFileSync } from "node:fs";
import { normalise } from "../src/lib/normalise";
import { entryId } from "../src/lib/anchors";
import { order, zones, tzMinutes } from "../src/lib/chronological";
import { layout, isMeta } from "../src/lib/lanes";
import type { Entry } from "../src/lib/spec";

const load = (f: string) => normalise(JSON.parse(readFileSync(`fixtures/${f}.json`, "utf8")));
const withIds = (es: Entry[]) => {
  const used = new Set<string>();
  const m = new Map(es.map((e) => [e, entryId(e, used)]));
  return (e: Entry) => m.get(e)!;
};

describe("timezones", () => {
  it("parses labels and numeric offsets", () => {
    expect(tzMinutes("NZST")).toBe(720);
    expect(tzMinutes("AEST")).toBe(600);
    expect(tzMinutes("+0530")).toBe(330);
    expect(tzMinutes("nonsense")).toBeNull();
  });

  it("orders by absolute time, not the displayed clock", () => {
    // 09:51 NZST is 21:51 UTC; the 09:20 AEST reply is 23:20 UTC, so it is later
    const t = load("synthetic");
    const z = zones(t.messages);
    const b = t.messages.find((e) => e.id === "b")!;
    const c = t.messages.find((e) => e.id === "c")!;
    expect(b.time! > c.time!).toBe(true); // the clock disagrees...
    expect(z.absolute(b)).toBeLessThan(z.absolute(c)); // ...absolute time does not
    const seq = order(t.messages, withIds(t.messages)).map((e) => e.id);
    expect(seq.indexOf("b")).toBeLessThan(seq.indexOf("c"));
  });

  it("infers a zone where none was stated, and says so", () => {
    const t = load("minimal");
    const z = zones(t.messages);
    const e = t.messages[0]!;
    expect(e.tz).toBeUndefined();
    expect(z.label(e).inferred).toBe(true);
  });
});

describe("ordering", () => {
  it("never places a reply above its parent", () => {
    const t = load("synthetic");
    const idOf = withIds(t.messages);
    const seq = order(t.messages, idOf);
    const pos = new Map(seq.map((e, i) => [idOf(e), i]));
    expect(seq.filter((e) => e.parent && pos.get(e.parent)! >= pos.get(idOf(e))!)).toEqual([]);
  });

  it("keeps every entry", () => {
    const t = load("synthetic");
    expect(order(t.messages, withIds(t.messages))).toHaveLength(t.messages.length);
  });

  it("does not drop entries when a parent cycle exists", () => {
    const cyclic: Entry[] = [
      { kind: "message", id: "x", date: "Tue 1 Jul 2025", body: "", parent: "y" },
      { kind: "message", id: "y", date: "Tue 1 Jul 2025", body: "", parent: "x" },
    ];
    expect(order(cyclic, withIds(cyclic))).toHaveLength(2);
  });
});

describe("lanes", () => {
  it("puts a single chain in one lane", () => {
    const t = load("synthetic");
    const idOf = withIds(t.messages);
    const l = layout(order(t.messages, idOf), idOf);
    expect(l.chains).toHaveLength(2); // the mail chain, plus the standalone note
    expect(l.laneCount).toBeLessThanOrEqual(2);
  });

  it("classifies a lone meeting note as a meta chain", () => {
    const t = load("synthetic");
    const idOf = withIds(t.messages);
    const l = layout(order(t.messages, idOf), idOf);
    const note = l.chains.find((c) => c.entries.length === 1)!;
    expect(note.meta).toBe(true);
    expect(l.chains.find((c) => c.entries.length === 3)!.meta).toBe(false);
  });

  it("treats an invitation body as meta by its leading token only", () => {
    const mk = (body: string) => ({ kind: "message", date: "x", body }) as Entry;
    expect(isMeta(mk("<p>Invitation: standup</p>"))).toBe(true);
    expect(isMeta(mk("<p>Notes: what we agreed</p>"))).toBe(true);
    expect(isMeta(mk("<p>see the invitation I sent</p>"))).toBe(false);
    expect(isMeta({ ...mk("<p>Invitation: x</p>"), meta: false })).toBe(false);
  });
});

describe("anchors", () => {
  it("derives ids from content and de-duplicates", () => {
    const used = new Set<string>();
    const e = { kind: "message", date: "Thu 16 Jul 2026", time: "11:35", sender: "Ada Kessler", body: "" } as Entry;
    expect(entryId(e, used)).toBe("m-20260716-1135-ak");
    expect(entryId(e, used)).toBe("m-20260716-1135-ak-2");
  });

  it("names a note by its date", () => {
    const used = new Set<string>();
    expect(entryId({ kind: "note", date: "Mon 17 Aug 2026", body: "" } as Entry, used)).toBe("m-20260817-note");
  });
});

describe("legacy specs", () => {
  it("accepts snake_case keys and kind:sys", () => {
    const t = normalise({
      title: "x",
      open_items: ["a"],
      messages: [{ kind: "sys", date: "Mon 17 Aug 2026", body: "", label: "call" },
                 { date: "Tue 18 Aug 2026", body: "", sender: "A B", from_email: "a@b.c", gmail_id: "g1" }],
    });
    expect(t.openItems).toEqual(["a"]);
    expect(t.messages[0]!.kind).toBe("note");
    expect(t.messages[1]!.fromEmail).toBe("a@b.c");
    expect(t.messages[1]!.gmailId).toBe("g1");
  });

  it("drops internal render state that older specs persisted", () => {
    const t = normalise({ title: "x", messages: [{ date: "d", body: "", _new: true, _id: "z" }] });
    expect(Object.keys(t.messages[0]!)).not.toContain("_new");
  });
});

/**
 * Scale invariants, asserted against a real trail kept out of the repo (real mail
 * is sensitive). Drop one at fixtures/local.json to run these; the block skips
 * itself when absent, so the suite passes on a fresh clone.
 */
const LOCAL = "fixtures/local.json";
describe.skipIf(!existsSync(LOCAL))("a real trail (local only)", () => {
  const t = normalise(JSON.parse(existsSync(LOCAL) ? readFileSync(LOCAL, "utf8") : "{}"));
  it("orders 58 entries with no reply inversions", () => {
    const idOf = withIds(t.messages);
    const seq = order(t.messages, idOf);
    const pos = new Map(seq.map((e, i) => [idOf(e), i]));
    expect(seq).toHaveLength(58);
    expect(seq.filter((e) => e.parent && pos.get(e.parent)! >= pos.get(idOf(e))!)).toEqual([]);
  });
  it("recycles lanes: 7 chains share 4", () => {
    const idOf = withIds(t.messages);
    const l = layout(order(t.messages, idOf), idOf);
    expect(l.chains).toHaveLength(7);
    expect(l.laneCount).toBe(4);
    for (const a of l.chains) for (const b of l.chains) {
      if (a === b || a.lane !== b.lane) continue;
      expect(a.lastRow < b.firstRow || b.lastRow < a.firstRow).toBe(true);
    }
  });
});

describe("spec url resolution", () => {
  it("tries the served path before the /@fs route", async () => {
    const { candidates } = await import("../src/lib/loadSpec");
    expect(candidates("/synthetic.json")).toEqual(["/synthetic.json", "/@fs/synthetic.json"]);
    expect(candidates("synthetic.json")).toEqual(["/synthetic.json", "/@fs/synthetic.json"]);
  });

  it("passes through absolute urls and /@fs paths untouched", async () => {
    const { candidates } = await import("../src/lib/loadSpec");
    expect(candidates("https://x.test/s.json")).toEqual(["https://x.test/s.json"]);
    expect(candidates("/@fs/tmp/s.json")).toEqual(["/@fs/tmp/s.json"]);
  });

  it("expands a leading ~", async () => {
    const { candidates } = await import("../src/lib/loadSpec");
    const got = candidates("~/Downloads/s.json");
    expect(got.some((c) => c.endsWith("/Downloads/s.json"))).toBe(true);
    expect(got.every((c) => !c.includes("~"))).toBe(true);
  });
});

describe("diff", () => {
  it("marks entries absent from the previous pass as new", async () => {
    const { diff } = await import("../src/lib/diff");
    const prev = normalise({ title: "t", messages: [{ id: "a", date: "Tue 1 Jul 2025", body: "<p>one</p>" }] });
    const next = normalise({
      title: "t",
      messages: [
        { id: "a", date: "Tue 1 Jul 2025", body: "<p>one</p>" },
        { id: "b", date: "Wed 2 Jul 2025", body: "<p>two</p>" },
      ],
    });
    const m = diff(prev, next);
    expect(m.get("b")).toBe("new");
    expect(m.has("a")).toBe(false);
  });

  it("marks an edited body as revised, not new", async () => {
    const { diff } = await import("../src/lib/diff");
    const prev = normalise({ title: "t", messages: [{ id: "a", date: "d", body: "<p>one</p>" }] });
    const next = normalise({ title: "t", messages: [{ id: "a", date: "d", body: "<p>one, edited</p>" }] });
    expect(diff(prev, next).get("a")).toBe("revised");
  });

  it("treats the same words at a new timestamp as revised, not deleted+new", async () => {
    const { diff } = await import("../src/lib/diff");
    const body = "<p>same words</p>";
    const prev = normalise({ title: "t", messages: [{ date: "Tue 1 Jul 2025", time: "09:00", sender: "A B", body }] });
    const next = normalise({ title: "t", messages: [{ date: "Tue 1 Jul 2025", time: "10:00", sender: "A B", body }] });
    expect([...diff(prev, next).values()]).toEqual(["revised"]);
  });

  it("is idempotent against itself", async () => {
    const { diff } = await import("../src/lib/diff");
    const t = load("synthetic");
    expect(diff(t, t).size).toBe(0);
  });

  it("refuses a page with no embedded spec", async () => {
    const { extractSpec } = await import("../src/lib/diff");
    expect(() => extractSpec("<html><body>nope</body></html>")).toThrow(/no embedded spec/);
  });

  it("round-trips a rendered page's spec", async () => {
    const { extractSpec } = await import("../src/lib/diff");
    const t = load("synthetic");
    const page = `<script type="application/json" id="chainmail-spec">${JSON.stringify(t).replace(/<\//g, "<\\/")}</script>`;
    expect(extractSpec(page).messages).toHaveLength(t.messages.length);
  });
});

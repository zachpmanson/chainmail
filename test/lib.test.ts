import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
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
    // 09:51 NZST is 21:51 UTC; the 09:20 AEST reply the same morning is 23:20 UTC
    const t = load("synthetic");
    const z = zones(t.messages);
    const nz = t.messages.find((e) => e.time === "09:51" && e.tz === "NZST")!;
    const au = t.messages.find((e) => e.time === "09:20" && e.tz === "AEST")!;
    expect(nz.time! > au.time!).toBe(true); // the clock disagrees...
    expect(z.absolute(nz)).toBeLessThan(z.absolute(au)); // ...absolute time does not
    const idOf = withIds(t.messages);
    const seq = order(t.messages, idOf).map((e) => idOf(e));
    expect(seq.indexOf(idOf(nz))).toBeLessThan(seq.indexOf(idOf(au)));
  });

  it("infers a zone where none was stated, and says so", () => {
    const t = load("minimal");
    const z = zones(t.messages);
    const e = t.messages[0]!;
    expect(e.tz).toBeUndefined();
    expect(z.label(e).inferred).toBe(true);
  });

  it("marks every unstated zone as inferred, notes included", () => {
    const t = load("synthetic");
    const z = zones(t.messages);
    const msgs = t.messages.filter((e) => e.kind !== "note");
    expect(msgs.filter((e) => e.tz)).toHaveLength(37);
    expect(msgs.filter((e) => !e.tz)).toHaveLength(18);
    expect(t.messages.filter((e) => !e.tz)).toHaveLength(21);
    expect(t.messages.filter((e) => !e.tz).every((e) => z.label(e).inferred)).toBe(true);
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
    expect(order(t.messages, withIds(t.messages))).toHaveLength(58);
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
  it("recycles a lane once a chain has ended: 7 chains share 4", () => {
    const t = load("synthetic");
    const idOf = withIds(t.messages);
    const l = layout(order(t.messages, idOf), idOf);
    expect(l.chains).toHaveLength(7);
    expect(l.laneCount).toBe(4);
    const perLane = new Map<number, number>();
    for (const c of l.chains) perLane.set(c.lane, (perLane.get(c.lane) ?? 0) + 1);
    expect([...perLane.values()].some((n) => n > 1)).toBe(true);
  });

  it("never overlaps two chains in one lane", () => {
    const t = load("synthetic");
    const idOf = withIds(t.messages);
    const l = layout(order(t.messages, idOf), idOf);
    for (const a of l.chains) {
      for (const b of l.chains) {
        if (a === b || a.lane !== b.lane) continue;
        expect(a.lastRow < b.firstRow || b.lastRow < a.firstRow).toBe(true);
      }
    }
  });

  it("classifies meeting-only chains as meta and leaves mixed chains alone", () => {
    const t = load("synthetic");
    const idOf = withIds(t.messages);
    const l = layout(order(t.messages, idOf), idOf);
    expect(l.chains.filter((c) => c.meta).map((c) => c.entries.length).sort()).toEqual([1, 2]);
    // the longest chain contains a meeting notice but also real correspondence
    expect(l.chains.find((c) => c.entries.length === 27)!.meta).toBe(false);
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
    const e = { kind: "message", date: "Thu 16 Jul 2026", time: "11:35", sender: "Jean-Luc Picard", body: "" } as Entry;
    expect(entryId(e, used)).toBe("m-20260716-1135-jlp");
    expect(entryId(e, used)).toBe("m-20260716-1135-jlp-2");
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

describe("interlinking", () => {
  it("every xref in a body points at an entry that exists", () => {
    const t = load("synthetic");
    const used = new Set<string>();
    const ids = new Set(t.messages.map((e) => entryId(e, used)));
    const hrefs = [...JSON.stringify(t).matchAll(/href=\\?['"]#(m-[^'"\\]+)/g)].map((m) => m[1]!);
    expect(hrefs).toHaveLength(37); // editorial cross-links plus open-item links
    expect(hrefs.filter((h) => !ids.has(h))).toEqual([]);
  });

  it("every parent points at an entry that exists", () => {
    const t = load("synthetic");
    const used = new Set<string>();
    const ids = new Set(t.messages.map((e) => entryId(e, used)));
    const parents = t.messages.map((e) => e.parent).filter(Boolean) as string[];
    expect(parents).toHaveLength(51);
    expect(parents.filter((p) => !ids.has(p))).toEqual([]);
  });
});

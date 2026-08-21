import type { Entry, Timeline } from "./spec";
import { parseDate } from "./anchors";

export const TZ_OFFSETS: Record<string, number> = {
  AEST: 600, AEDT: 660, NZST: 720, NZDT: 780, AWST: 480, ACST: 570, ACDT: 630,
  GMT: 0, UTC: 0, BST: 60, CET: 60, CEST: 120, IST: 330,
  PST: -480, PDT: -420, EST: -300, EDT: -240,
};

/** Minutes east of UTC for a zone label, or null if unrecognised. */
export function tzMinutes(tz: string | undefined): number | null {
  if (!tz) return null;
  const t = tz.trim();
  const m = /^([+-])(\d{2}):?(\d{2})$/.exec(t);
  if (m) {
    const v = Number(m[2]) * 60 + Number(m[3]);
    return m[1] === "+" ? v : -v;
  }
  return TZ_OFFSETS[t.toUpperCase()] ?? null;
}

const mode = (xs: number[]): number | null => {
  if (xs.length === 0) return null;
  const c = new Map<number, number>();
  for (const x of xs) c.set(x, (c.get(x) ?? 0) + 1);
  return [...c.entries()].sort((a, b) => b[1] - a[1])[0]![0];
};

/** How much the page is entitled to claim about one entry's zone. */
export type ZoneState = "stated" | "inferred" | "unknown";

export interface Zones {
  /** The zone to show for an entry, and how much it is worth. */
  label: (e: Entry) => { tz: string | undefined; state: ZoneState };
  absolute: (e: Entry) => number;
}

/**
 * The zone on an entry is the spec's to state, not the renderer's to invent.
 *
 * This used to fill a missing `tz` from the mode of what the sender, then their
 * org, then the whole trail had stated. On a 57-entry page where 41 entries
 * carried no zone that produced a label for every one of them, and the labels
 * were wrong in the way a mode is always wrong: the busiest sender in the trail
 * has stated both +0530 and +1000 over the years, and the mode handed his
 * November message the offset from the wrong continent. It was marked inferred,
 * so it was honest, and it was still eight hours and a hemisphere out.
 *
 * Absent `tz` therefore now means unknown and renders as unknown. The inference
 * belongs upstream (internal/tzinfer), where the evidence is the whole corpus
 * rather than one page, where a candidate can be tested against the order the
 * quotes establish, and where the reasoning can be published beside the claim.
 */
export function zones(entries: Entry[]): Zones {
  const label = (e: Entry): { tz: string | undefined; state: ZoneState } => {
    if (!e.tz) return { tz: undefined, state: "unknown" };
    return { tz: e.tz, state: e.tzSource === "inferred" ? "inferred" : "stated" };
  };

  // Ordering still needs a number for every entry, and an unplaceable clock is
  // read at the trail's prevailing offset for that purpose alone. It cannot
  // mislead the way a displayed label can: ordering is topological, so a
  // misplaced clock reorders same-day siblings and never inverts a reply chain
  // (see order()), and nothing about this number reaches the page.
  const stated = entries.map((e) => tzMinutes(e.tz)).filter((o): o is number => o !== null);
  const prevailing = mode(stated) ?? 0;

  const absolute = (e: Entry): number => {
    const d = parseDate(e.date);
    if (!d) return 0;
    const days = d.y * 372 + d.m * 31 + d.d;
    let hhmm = e.time ?? "";
    let tz = e.tz;
    if (!hhmm && e.kind === "note") {
      const t = /(\d{1,2}):(\d{2})\s*([A-Z]{3,4})?/.exec(e.label ?? "");
      if (t) { hhmm = `${t[1]}:${t[2]}`; tz = tz ?? t[3]; }
    }
    const hm = hhmm.replace(/\D/g, "").slice(0, 4);
    const mins = hm.length === 4 ? Number(hm.slice(0, 2)) * 60 + Number(hm.slice(2)) : 12 * 60;
    const off = tzMinutes(tz) ?? prevailing;
    return days * 1440 + mins - off;
  };

  return { label, absolute };
}

/**
 * Order strictly by absolute time, subject to causality: a reply is never placed
 * above the message it replies to. Time is the sort key; the reply graph breaks
 * ties and overrides the clock wherever a stated zone is missing or ambiguous.
 */
export function order(entries: Entry[], idOf: (e: Entry) => string): Entry[] {
  const z = zones(entries);
  const key = new Map(entries.map((e) => [idOf(e), z.absolute(e)]));
  const byId = new Map(entries.map((e) => [idOf(e), e]));
  const children = new Map<string | null, string[]>();
  for (const e of entries) {
    const p = e.parent && byId.has(e.parent) ? e.parent : null;
    children.set(p, [...(children.get(p) ?? []), idOf(e)]);
  }

  const cmp = (a: string, b: string) => (key.get(a)! - key.get(b)!) || a.localeCompare(b);
  const ready = [...(children.get(null) ?? [])].sort(cmp);
  const seen = new Set<string>();
  const out: Entry[] = [];
  while (ready.length > 0) {
    const cur = ready.shift()!;
    if (seen.has(cur)) continue;
    seen.add(cur);
    out.push(byId.get(cur)!);
    for (const c of children.get(cur) ?? []) if (!seen.has(c)) ready.push(c);
    ready.sort(cmp);
  }
  // a parent cycle would strand entries; keep them rather than dropping silently
  for (const e of entries) if (!seen.has(idOf(e))) out.push(e);
  return out;
}

export function orderTimeline(t: Timeline, idOf: (e: Entry) => string): Timeline {
  // schema declares messages non-empty (minItems 1), which order() preserves
  return { ...t, messages: order(t.messages, idOf) as Timeline["messages"] };
}

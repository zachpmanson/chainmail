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

export interface Zones {
  /** The zone used for an entry, and whether it was stated or inferred. */
  label: (e: Entry) => { tz: string | undefined; inferred: boolean };
  absolute: (e: Entry) => number;
}

/**
 * Zone evidence, most specific first: what this sender stated elsewhere, then
 * their org, then the trail. Per-org alone is too coarse — it read an AU company
 * as NZ because most of its traffic quoted NZ send times.
 */
export function zones(entries: Entry[]): Zones {
  const bySender = new Map<string, number[]>();
  const byOrg = new Map<string, number[]>();
  const all: number[] = [];
  const nameFor = new Map<number, string[]>();

  for (const e of entries) {
    const off = tzMinutes(e.tz);
    if (off === null) continue;
    const push = (m: Map<string, number[]>, k: string) => m.set(k, [...(m.get(k) ?? []), off]);
    push(bySender, e.sender ?? "");
    push(byOrg, e.org ?? "");
    all.push(off);
    nameFor.set(off, [...(nameFor.get(off) ?? []), e.tz!.trim()]);
  }

  const senderTz = new Map([...bySender].map(([k, v]) => [k, mode(v)!]));
  const orgTz = new Map([...byOrg].map(([k, v]) => [k, mode(v)!]));
  const trailTz = mode(all);
  const offsetName = new Map(
    [...nameFor].map(([off, names]) => {
      const c = new Map<string, number>();
      for (const n of names) c.set(n, (c.get(n) ?? 0) + 1);
      return [off, [...c.entries()].sort((a, b) => b[1] - a[1])[0]![0]];
    }),
  );

  const fallback = (e: Entry): number =>
    senderTz.get(e.sender ?? "") ?? orgTz.get(e.org ?? "") ?? trailTz ?? 0;

  const label = (e: Entry) =>
    e.tz ? { tz: e.tz, inferred: false } : { tz: offsetName.get(fallback(e)), inferred: true };

  /**
   * Absolute time in minutes. A guessed zone can reorder same-day siblings but
   * never inverts a reply chain, because ordering is topological (see order()).
   */
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
    const off = tzMinutes(tz) ?? fallback(e);
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

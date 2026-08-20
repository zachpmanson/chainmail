import type { Entry } from "./spec";

export interface Chain {
  root: string;
  entries: string[];
  firstRow: number;
  lastRow: number;
  lane: number;
  subject?: string;
  opener: string;
  date: string;
  meta: boolean;
}

export interface Layout {
  /** row index per entry id, 1-based with room for a header row */
  row: Map<string, number>;
  /** chain root per entry id */
  chainOf: Map<string, string>;
  chains: Chain[];
  laneCount: number;
}

/**
 * Meta-meeting entries: the scheduling exhaust around a meeting rather than
 * correspondence. Matched on the LEADING token, because these words appear
 * mid-body in plenty of real emails. `meta` in the spec forces the call.
 */
const META_LEAD =
  /^\s*(invitation|invite|updated invitation|canceled event|cancelled event|accepted|declined|tentative|notes|recap|note taker)\b[:\s]|^\s*\S.*\bshared (?:a|the) recap for\b/i;

export function isMeta(e: Entry): boolean {
  if (e.meta !== undefined) return e.meta;
  if (e.kind === "note") return true;
  const text = (e.body ?? "").replace(/<[^>]+>/g, " ").replace(/&[a-z]+;/gi, " ").trim();
  return META_LEAD.test(e.subject ?? "") || META_LEAD.test(text);
}

/**
 * Lay entries out as lanes. Vertical position is chronological (one entry per
 * row); the horizontal axis carries no meaning beyond keeping concurrently-live
 * chains apart. A chain takes the lowest lane whose previous occupant finished
 * before it started, so a dead chain's lane is reused rather than standing empty.
 */
export function layout(entries: Entry[], idOf: (e: Entry) => string): Layout {
  const row = new Map<string, number>();
  entries.forEach((e, i) => row.set(idOf(e), i + 2));

  const byId = new Map(entries.map((e) => [idOf(e), e]));
  const chainOf = new Map<string, string>();
  for (const e of entries) {
    let cur = e;
    const seen = new Set<string>();
    while (cur.parent && byId.has(cur.parent) && !seen.has(idOf(cur))) {
      seen.add(idOf(cur));
      cur = byId.get(cur.parent)!;
    }
    chainOf.set(idOf(e), idOf(cur));
  }

  const grouped = new Map<string, string[]>();
  for (const e of entries) {
    const r = chainOf.get(idOf(e))!;
    grouped.set(r, [...(grouped.get(r) ?? []), idOf(e)]);
  }

  const chains: Chain[] = [...grouped].map(([root, ids]) => {
    const head = byId.get(root)!;
    const rows = ids.map((i) => row.get(i)!);
    return {
      root,
      entries: ids,
      firstRow: Math.min(...rows),
      lastRow: Math.max(...rows),
      lane: -1,
      subject: head.subject,
      opener: head.kind === "note" ? (head.label ?? "") : (head.sender ?? ""),
      date: head.date,
      // a chain carries no correspondence only if every entry in it is meta
      meta: ids.every((i) => isMeta(byId.get(i)!)),
    };
  });

  chains.sort((a, b) => a.firstRow - b.firstRow);
  const laneEnd: number[] = [];
  for (const c of chains) {
    let put = laneEnd.findIndex((end) => end < c.firstRow);
    if (put === -1) { laneEnd.push(c.lastRow); put = laneEnd.length - 1; }
    else laneEnd[put] = c.lastRow;
    c.lane = put;
  }

  return { row, chainOf, chains, laneCount: laneEnd.length };
}

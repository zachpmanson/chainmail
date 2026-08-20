import type { Entry } from "./spec";

const MONTHS = ["jan","feb","mar","apr","may","jun","jul","aug","sep","oct","nov","dec"];

export function parseDate(date: string | undefined): { y: number; m: number; d: number } | null {
  const m = /(\d{1,2})\s+([A-Za-z]{3})[a-z]*\s+(\d{4})/.exec(date ?? "");
  if (!m) return null;
  const mon = MONTHS.indexOf(m[2]!.toLowerCase());
  if (mon < 0) return null;
  return { y: Number(m[3]), m: mon + 1, d: Number(m[1]) };
}

/**
 * Stable, content-derived anchor. Ids come from date+time+sender rather than
 * position, because unspooling routinely inserts entries into the middle of a
 * finished trail and positional ids would silently repoint every existing link.
 * An explicit `id` always wins.
 */
export function entryId(e: Entry, used: Set<string>): string {
  let base: string;
  if (e.id) {
    base = e.id;
  } else {
    const d = parseDate(e.date);
    const day = d ? `${d.y}${String(d.m).padStart(2, "0")}${String(d.d).padStart(2, "0")}` : "undated";
    if (e.kind === "note") {
      base = `m-${day}-note`;
    } else {
      const t = (e.time ?? "").replace(/\D/g, "").slice(0, 4) || "0000";
      const who = (e.sender ?? "").match(/[A-Za-z]+/g)?.map((w) => w[0]).join("").slice(0, 3).toLowerCase() || "x";
      base = `m-${day}-${t}-${who}`;
    }
  }
  let id = base;
  for (let n = 2; used.has(id); n++) id = `${base}-${n}`;
  used.add(id);
  return id;
}

export function initials(name: string): string {
  const parts = name.replace(/\(/g, " ").split(/\s+/).filter((p) => /^[A-Za-z]/.test(p));
  if (parts.length === 0) return "?";
  if (parts.length === 1) return parts[0]!.slice(0, 2).toUpperCase();
  return (parts[0]![0]! + parts[parts.length - 1]![0]!).toUpperCase();
}

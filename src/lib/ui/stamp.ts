import type { CorpusEntry } from "../api/api";
import type { ZoneState } from "../timeline/chronological";

// en-GB + hour12:false forces 24-hour clocks to match built pages and the CLI,
// whatever the browser locale.
const CLOCK = new Intl.DateTimeFormat("en-GB", {
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
});

const ISO_DAY = new Intl.DateTimeFormat("en-CA", {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});

const DAY = new Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short" });
const DAY_YEAR = new Intl.DateTimeFormat("en-GB", {
  day: "numeric",
  month: "short",
  year: "numeric",
});

/** "20:51" — a clock, 24-hour, wherever the reader is. */
export function clock(at: Date): string {
  return CLOCK.format(at);
}

/** "2026-09-16" in the local zone; en-CA is the locale that prints ISO order. */
export function isoDay(at: Date): string {
  return ISO_DAY.format(at);
}

/** "16 Sep", or "16 Sep 2025" when it is not this year. */
export function day(at: Date, now = new Date()): string {
  return at.getFullYear() === now.getFullYear() ? DAY.format(at) : DAY_YEAR.format(at);
}

export function when(stamp: string, now = new Date()): string {
  const at = new Date(stamp);
  // Show an unparseable stamp as-is rather than "Invalid Date".
  if (Number.isNaN(at.getTime())) return stamp;
  return `${day(at, now)}, ${clock(at)}`;
}

export function whenShort(stamp?: string, now = new Date()): string {
  if (!stamp) return "";
  const at = new Date(stamp);
  if (Number.isNaN(at.getTime())) return stamp;
  const same = (a: Date, b: Date) =>
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate();
  if (same(at, now)) return clock(at);
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (same(at, yesterday)) return "Yesterday";
  return day(at, now);
}

/** A message's clock as its bubble states it. */
export interface StampData {
  /** as displayed, e.g. "Thu 16 Jul 2026" */
  date: string;
  /** as displayed, e.g. "11:35"; absent for an entry with no clock */
  time?: string;
  /** the zone label, e.g. "AEDT"; absent when nothing placed it */
  tz?: string;
  /** how much the page may claim about `tz` */
  zone: ZoneState;
}

type Sent = Pick<CorpusEntry, "ts" | "tz" | "tzOffsetMinutes">;

/** The instant is already shifted into the sender's clock, so format it as UTC. */
const SENT_DATE = new Intl.DateTimeFormat("en-GB", {
  weekday: "short",
  day: "numeric",
  month: "short",
  year: "numeric",
  timeZone: "UTC",
});
const SENT_TIME = new Intl.DateTimeFormat("en-GB", {
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
  timeZone: "UTC",
});

/** With no offset, reads UTC under the stated label, as internal/spec/zones.go does. */
export function stampOf(e: Sent): StampData {
  const at = new Date(e.ts);
  if (Number.isNaN(at.getTime())) return { date: e.ts, zone: "unknown" };
  const label = (e.tz ?? "").trim();
  const offset = e.tzOffsetMinutes;
  const wall = new Date(at.getTime() + (offset ?? 0) * 60_000);
  const stated = label !== "" || offset !== undefined;
  return {
    date: SENT_DATE.format(wall),
    time: SENT_TIME.format(wall),
    tz: label !== "" ? label : offset !== undefined ? formatOffset(offset) : "",
    zone: stated ? "stated" : "unknown",
  };
}

/** The stamp's date and clock on one line, e.g. "Mon 2 Mar 2026 09:15". */
export function whenOf(e: Sent): string {
  const at = stampOf(e);
  return [at.date, at.time].filter(Boolean).join(" ");
}

/** Minutes east of UTC as a Date-header zone, e.g. "+0545". */
function formatOffset(mins: number): string {
  const sign = mins < 0 ? "-" : "+";
  const abs = Math.abs(mins);
  return `${sign}${String(Math.floor(abs / 60)).padStart(2, "0")}${String(abs % 60).padStart(2, "0")}`;
}

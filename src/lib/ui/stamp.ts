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

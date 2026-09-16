/**
 * The app's clocks and dates, in one place and one shape.
 *
 * Every stamp the app prints is a moment the reader is looking at on their own
 * machine, so it is local time. The *format*, though, is not the machine's to
 * choose: `en-GB` with `hour12: false` is 24-hour, and a browser set to en-US
 * would otherwise print "8:51 PM" for a time that every other surface — a built
 * page, a message stamp, the CLI — prints as "20:51". One reader with two
 * browsers should not have two ideas of when a message arrived.
 *
 * The date form is the same rule: day and short month, and the year only when it
 * is not this one, so a row from March does not read as this March.
 */
const CLOCK = new Intl.DateTimeFormat("en-GB", {
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
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

/** "16 Sep", or "16 Sep 2025" when it is not this year. */
export function day(at: Date, now = new Date()): string {
  return at.getFullYear() === now.getFullYear() ? DAY.format(at) : DAY_YEAR.format(at);
}

/**
 * A stamp with the date in it: "16 Sep 2026, 20:51". Used where the moment is
 * about *when* something happened rather than when in the day — a saved page, a
 * service's last check — so the date is always named, and the year is dropped
 * when it is the current one so a recent check reads fresh.
 */
export function when(stamp: string, now = new Date()): string {
  const at = new Date(stamp);
  // A stamp that will not parse is shown as it arrived: the reader can tell that
  // it is wrong, which "Invalid Date" would hide.
  if (Number.isNaN(at.getTime())) return stamp;
  return `${day(at, now)}, ${clock(at)}`;
}

/**
 * A row's date, written the way a mail client writes one: the clock for today,
 * "Yesterday", then the day and month — the year only when it is not this one.
 * A list is scanned for the shape of a day, so today's rows are told apart by
 * the minute and older ones by their date.
 */
export function whenShort(stamp?: string, now = new Date()): string {
  if (!stamp) return "";
  const at = new Date(stamp);
  if (Number.isNaN(at.getTime())) return stamp;
  const same = (a: Date, b: Date) =>
    a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
  if (same(at, now)) return clock(at);
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (same(at, yesterday)) return "Yesterday";
  return day(at, now);
}

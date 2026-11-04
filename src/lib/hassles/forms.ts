import type { components } from "../api/api.d";

export type HassleLocation = components["schemas"]["HassleLocation"];
export type HassleSchedule = components["schemas"]["HassleSchedule"];
export type HassleTask = components["schemas"]["HassleTask"];
export const timeZone = "Australia/Melbourne";

export function intervalUnit(cadence: string, interval: number): string {
  const units: Record<string, string> = {
    daily: "day",
    weekly: "week",
    monthly: "month",
    yearly: "year",
  };
  const unit = units[cadence];
  return unit ? `${unit}${interval === 1 ? "" : "s"}` : "";
}

export function locationKey(location: HassleLocation): string {
  return JSON.stringify([location.accountId, location.calendarId]);
}

function wall(date: Date): string {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  }).formatToParts(date);
  const p = Object.fromEntries(parts.map(({ type, value }) => [type, value]));
  return `${p.year}-${p.month}-${p.day}T${p.hour}:${p.minute}`;
}

export type ReminderStartPreset = "hour" | "afternoon" | "evening" | "tomorrow";

export function reminderStart(preset: ReminderStartPreset, now = new Date()): string {
  if (preset === "hour") return wall(new Date(now.getTime() + 3600000));
  const local = wall(now);
  const time = { afternoon: "13:00", evening: "18:00", tomorrow: "08:30" }[preset];
  const date = new Date(`${local.slice(0, 10)}T12:00:00Z`);
  if (preset === "tomorrow" || local.slice(11) >= time) date.setUTCDate(date.getUTCDate() + 1);
  return `${date.toISOString().slice(0, 10)}T${time}`;
}

export function defaultSchedule(now = new Date()): HassleSchedule {
  return {
    title: "",
    start: reminderStart("hour", now),
    timeZone,
    cadence: "daily",
    interval: 1,
    until: "",
  };
}

/** Explicit Melbourne conversion: neither transport nor validation uses browser timezone. */
export function wallInstant(value: string): number | null {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(value)) return null;
  const utc = Date.parse(`${value}:00Z`);
  if (!Number.isFinite(utc)) return null;
  const matches: number[] = [];
  // Melbourne is always +10 or +11 in our supported 1970–2100 range.
  for (const hours of [10, 11]) {
    const candidate = utc - hours * 3600000;
    if (wall(new Date(candidate)) === value) matches.push(candidate);
  }
  return matches.length === 1 ? (matches[0] ?? null) : null;
}

export function validateSchedule(s: HassleSchedule): string | null {
  if (!s.title.trim() || new TextEncoder().encode(s.title).length > 500 || /[\r\n]/.test(s.title))
    return "Enter a one-line title (maximum 500 bytes).";
  const start = wallInstant(s.start);
  if (start === null || s.start.slice(0, 4) < "1970" || s.start.slice(0, 4) > "2100")
    return "Choose a valid, unambiguous Melbourne time (1970–2100); daylight-saving gaps and overlaps are unavailable.";
  if (s.timeZone !== timeZone) return "Use Australia/Melbourne time.";
  if (!["once", "daily", "weekdays", "weekly", "monthly", "yearly"].includes(s.cadence))
    return "This cadence is unavailable. Hourly needs separate Google compatibility testing.";
  if (!Number.isInteger(s.interval) || s.interval < 1 || s.interval > 999)
    return "Repeat interval must be a whole number from 1 to 999.";
  if ((s.cadence === "once" || s.cadence === "weekdays") && s.interval !== 1)
    return "This cadence requires interval 1.";
  if (
    s.cadence === "weekdays" &&
    ["Sat", "Sun"].includes(
      new Intl.DateTimeFormat("en", { timeZone, weekday: "short" }).format(new Date(start)),
    )
  )
    return "Choose a weekday for the first weekdays reminder.";
  if (s.cadence === "once" && s.until) return "One-off reminders cannot have a repeat end.";
  if (s.until) {
    const end = wallInstant(s.until);
    if (end === null || end <= start || s.until.slice(0, 4) > "2100")
      return "Repeat end must be a valid Melbourne time after the first reminder.";
  }
  return null;
}

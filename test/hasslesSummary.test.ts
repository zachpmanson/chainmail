import { expect, it } from "vitest";
import { defaultSchedule, type HassleTask } from "../src/lib/hassles/forms";
import { hassleSummary } from "../src/lib/hassles/summary";

const now = Date.parse("2026-11-04T01:15:45Z");
function task(): HassleTask {
  return {
    id: "a",
    title: "Bill",
    resources: 1,
    revision: "1",
    writable: true,
    restriction: "",
    reminder: "Reminder date unavailable · view in calendar",
    schedule: defaultSchedule(new Date(now)),
    timing: { state: "upcoming", next: "2026-11-04T21:30:00Z", last: null, note: "" },
  };
}

it("summarises next occurrence and cadence in Melbourne, independent of browser timezone", () => {
  const t = task();
  expect(hassleSummary(t, now)).toBe("Tomorrow, 8:30 am · Every day");
  t.schedule!.cadence = "weekdays";
  expect(hassleSummary(t, now)).toBe("Tomorrow, 8:30 am · Every weekday");
  t.schedule!.cadence = "weekly";
  t.schedule!.interval = 2;
  expect(hassleSummary(t, now)).toBe("Tomorrow, 8:30 am · Every 2 weeks");
  t.schedule!.cadence = "once";
  expect(hassleSummary(t, now)).toBe("Tomorrow, 8:30 am");
});

it("keeps the repeat end and distinguishes years for distant reminders", () => {
  const t = task();
  t.timing.next = "2027-02-03T22:45:00Z";
  t.schedule!.until = "2027-03-04T09:45";
  expect(hassleSummary(t, now)).toBe("4 Feb 2027, 9:45 am · Every day · Until 4 Mar 2027, 9:45 am");
});

it("uses the last occurrence for ended series and flags incomplete timing", () => {
  const t = task();
  t.timing = { state: "past", next: null, last: "2026-11-04T00:00:00Z", note: "" };
  expect(hassleSummary(t, now)).toBe("Last: Today, 11:00 am · Every day");
  t.timing = { state: "upcoming", next: "2026-11-04T21:30:00Z", last: null, note: "Partial" };
  t.schedule = null;
  expect(hassleSummary(t, now)).toBe("Known next: Tomorrow, 8:30 am");
});

it("keeps the server fallback for unknown or invalid dates instead of inventing a schedule", () => {
  const t = task();
  t.timing = { state: "unknown", next: null, last: null, note: "Unsupported" };
  expect(hassleSummary(t, now)).toBe(t.reminder);
  t.timing.next = "invalid";
  expect(hassleSummary(t, now)).toBe(t.reminder);
});

it("uses calendar days across DST and Melbourne midnight", () => {
  const t = task();
  t.timing.next = "2026-10-03T21:30:00Z";
  expect(hassleSummary(t, Date.parse("2026-10-03T13:45:00Z"))).toBe(
    "Tomorrow, 8:30 am · Every day",
  );
  expect(hassleSummary(t, Date.parse("2026-10-03T14:15:00Z"))).toBe("Today, 8:30 am · Every day");
});

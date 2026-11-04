import { describe, expect, it } from "vitest";
import {
  defaultSchedule,
  locationKey,
  reminderStart,
  validateSchedule,
  wallInstant,
} from "../src/lib/hassles/forms";

describe("Hassle form identity and Melbourne times", () => {
  it("defaults to an hour from now in Melbourne, recurring daily until Done", () => {
    expect(defaultSchedule().cadence).toBe("daily");
    expect(defaultSchedule().until).toBe("");
    expect(defaultSchedule(new Date("2026-11-03T20:00:00Z")).start).toBe("2026-11-04T08:00");
    expect(defaultSchedule(new Date("2026-11-03T22:00:00Z")).start).toBe("2026-11-04T10:00");
    expect(defaultSchedule(new Date("2026-12-31T23:00:00Z")).start).toBe("2027-01-01T11:00");
    const spring = new Date("2026-10-03T15:30:00Z");
    expect(defaultSchedule(spring).start).toBe("2026-10-04T03:30");
    expect(wallInstant(defaultSchedule(spring).start)).toBe(spring.getTime() + 3600000);
  });
  it("resolves presets in Melbourne, rolling passed afternoon/evening to tomorrow", () => {
    const now = new Date("2026-11-04T01:15:45Z"); // 12:15 Melbourne
    expect(reminderStart("hour", now)).toBe("2026-11-04T13:15");
    expect(reminderStart("afternoon", now)).toBe("2026-11-04T13:00");
    expect(reminderStart("evening", now)).toBe("2026-11-04T18:00");
    expect(reminderStart("tomorrow", now)).toBe("2026-11-05T08:30");
    expect(reminderStart("afternoon", new Date("2026-11-04T02:00:00Z"))).toBe("2026-11-05T13:00");
    expect(reminderStart("evening", new Date("2026-11-04T07:00:00Z"))).toBe("2026-11-05T18:00");
    expect(reminderStart("hour", new Date("2026-12-31T12:40:00Z"))).toBe("2027-01-01T00:40");
    expect(reminderStart("tomorrow", new Date("2026-12-31T00:00:00Z"))).toBe("2027-01-01T08:30");
    expect(reminderStart("tomorrow", new Date("2026-11-03T20:00:00Z"))).toBe("2026-11-05T08:30");
  });
  it("uses elapsed minutes across DST and retains ambiguous-time refusal", () => {
    const spring = new Date("2026-10-03T15:30:00Z"); // 01:30 AEST
    expect(reminderStart("hour", spring)).toBe("2026-10-04T03:30");
    expect(wallInstant(reminderStart("hour", spring))).toBe(spring.getTime() + 3600000);
    expect(reminderStart("tomorrow", new Date("2026-10-03T02:00:00Z"))).toBe("2026-10-04T08:30");
    const autumn = new Date("2026-04-04T14:30:00Z"); // 01:30 AEDT
    const ambiguous = reminderStart("hour", autumn);
    expect(ambiguous).toBe("2026-04-05T02:30");
    expect(
      validateSchedule({ ...defaultSchedule(autumn), title: "DST", start: ambiguous }),
    ).toContain("unambiguous");
    const afterOverlap = new Date("2026-04-04T16:30:00Z");
    expect(wallInstant(reminderStart("hour", afterOverlap))).toBe(afterOverlap.getTime() + 3600000);
  });
  it("rejects impossible and ambiguous DST times rather than browser conversion", () => {
    expect(wallInstant("2026-10-04T02:30")).toBeNull();
    expect(wallInstant("2026-04-05T02:30")).toBeNull();
    expect(wallInstant("2026-02-30T08:30")).toBeNull();
    expect(wallInstant("2026-11-04T08:30")).toBe(Date.parse("2026-11-03T21:30:00Z"));
  });
  it("validates cadence, interval and inclusive recurrence end ordering", () => {
    const schedule = { ...defaultSchedule(), title: "Bill", cadence: "daily" as const };
    expect(validateSchedule(schedule)).toBeNull();
    expect(validateSchedule({ ...schedule, until: schedule.start })).toContain("after");
    expect(validateSchedule({ ...schedule, interval: 0 })).toContain("interval");
    expect(validateSchedule({ ...schedule, cadence: "hourly" as "daily" })).toContain(
      "unavailable",
    );
    expect(validateSchedule({ ...schedule, title: "\n" })).toContain("title");
    expect(validateSchedule({ ...schedule, cadence: "once", until: schedule.start })).toContain(
      "One-off",
    );
  });
  it("keeps both calendar and account in remount/query identities", () => {
    expect(locationKey({ accountId: "one", calendarId: "a" })).not.toBe(
      locationKey({ accountId: "two", calendarId: "a" }),
    );
    expect(locationKey({ accountId: "one", calendarId: "a" })).not.toBe(
      locationKey({ accountId: "one", calendarId: "b" }),
    );
  });
});

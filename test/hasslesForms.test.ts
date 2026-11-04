import { describe, expect, it } from "vitest";
import {
  defaultSchedule,
  locationKey,
  validateSchedule,
  wallInstant,
} from "../src/lib/hassles/forms";

describe("Hassle form identity and Melbourne times", () => {
  it("defaults to the next Melbourne 08:30 independent of browser timezone", () => {
    expect(defaultSchedule().cadence).toBe("daily");
    expect(defaultSchedule().until).toBe("");
    expect(defaultSchedule(new Date("2026-11-03T20:00:00Z")).start).toBe("2026-11-04T08:30");
    expect(defaultSchedule(new Date("2026-11-03T22:00:00Z")).start).toBe("2026-11-05T08:30");
    expect(defaultSchedule(new Date("2026-12-31T23:00:00Z")).start).toBe("2027-01-02T08:30");
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

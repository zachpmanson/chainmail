import { describe, expect, it } from "vitest";
import {
  defaultSchedule,
  intervalUnit,
  hourlyIntervals,
  validateSchedule,
  type HassleSchedule,
} from "../src/lib/hassles/forms";

describe("Hassle work-hour controls", () => {
  it("defaults unchecked and exposes singular/plural hourly intervals", () => {
    expect(defaultSchedule().workHours).toBe(false);
    expect(intervalUnit("hourly", 1)).toBe("hour");
    expect(intervalUnit("hourly", 0.5)).toBe("hours");
    expect(hourlyIntervals).toEqual([0.5, 1, 2, 3, 4, 6, 8, 12, 24]);
  });
  it("validates all cadence/checkbox combinations and interval extremes", () => {
    const cadences: HassleSchedule["cadence"][] = [
      "once",
      "hourly",
      "daily",
      "weekdays",
      "weekly",
      "monthly",
      "yearly",
    ];
    for (const cadence of cadences)
      for (const workHours of [false, true]) {
        const s = {
          ...defaultSchedule(),
          title: "Office",
          start: "2026-11-06T09:00",
          cadence,
          workHours,
        };
        expect(validateSchedule(s)).toBeNull();
        if (cadence === "once" || cadence === "weekdays") continue;
        for (const interval of cadence === "hourly" ? hourlyIntervals : [1, 2, 7, 24, 999])
          expect(validateSchedule({ ...s, interval })).toBeNull();
        for (const interval of cadence === "hourly"
          ? [0, 0.6, 1.5, 5, 7, 999, NaN, Infinity]
          : [0, 0.5, 1.5, 1000, NaN, Infinity])
          expect(validateSchedule({ ...s, interval })).toContain("interval");
      }
  });
  it("constrains the notification instant, not event end, and does not move invalid starts", () => {
    for (const [start, valid] of [
      ["2026-11-06T08:59", false],
      ["2026-11-06T09:00", true],
      ["2026-11-06T16:59", true],
      ["2026-11-06T17:00", false],
      ["2026-11-07T09:00", false],
      ["2026-11-08T16:00", false],
    ] as const) {
      const s = { ...defaultSchedule(), title: "Office", start, workHours: true };
      expect(validateSchedule(s) === null).toBe(valid);
      expect(s.start).toBe(start);
      expect(validateSchedule({ ...s, workHours: false })).toBeNull();
    }
  });
});

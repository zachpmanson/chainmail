import { describe, expect, it } from "vitest";
import { clock, day, isoDay, stampOf, when, whenOf, whenShort } from "../src/lib/ui/stamp";
import { MON_2_MAR } from "./fixtures";

describe("stampOf", () => {
  it("shifts the instant into the sender's offset and keeps their label", () => {
    expect(stampOf({ ts: "2026-03-01T22:15:00Z", tz: "AEDT", tzOffsetMinutes: 660 })).toEqual({
      date: MON_2_MAR,
      time: "09:15",
      tz: "AEDT",
      zone: "stated",
    });
  });

  it("names an unlabelled offset as a Date-header zone", () => {
    expect(stampOf({ ts: "2026-03-02T03:30:00Z", tzOffsetMinutes: 345 }).tz).toBe("+0545");
    expect(stampOf({ ts: "2026-03-02T15:00:00Z", tzOffsetMinutes: -270 })).toEqual({
      date: MON_2_MAR,
      time: "10:30",
      tz: "-0430",
      zone: "stated",
    });
  });

  it("reads a label with no offset as UTC under that label", () => {
    expect(stampOf({ ts: "2026-03-02T09:15:00Z", tz: "NZST" })).toEqual({
      date: MON_2_MAR,
      time: "09:15",
      tz: "NZST",
      zone: "stated",
    });
  });

  it("calls a stamp with neither label nor offset unknown, shown in UTC", () => {
    expect(stampOf({ ts: "2026-03-02T09:15:00Z", tz: "  " })).toEqual({
      date: MON_2_MAR,
      time: "09:15",
      tz: "",
      zone: "unknown",
    });
  });

  it("shows an unparseable stamp as it arrived", () => {
    expect(stampOf({ ts: "last Tuesday" })).toEqual({ date: "last Tuesday", zone: "unknown" });
  });
});

describe("whenOf", () => {
  // Suspected bug: the doc comments and built pages say "Mon 2 Mar 2026 09:15"; en-GB adds a comma.
  it.fails("matches the documented, comma-free date", () => {
    expect(whenOf({ ts: "2026-03-02T09:15:00Z" })).toBe("Mon 2 Mar 2026 09:15");
  });

  it("joins the date and clock", () => {
    expect(whenOf({ ts: "2026-03-01T22:15:00Z", tzOffsetMinutes: 660 })).toBe(`${MON_2_MAR} 09:15`);
  });

  it("is the raw stamp when it cannot be read", () => {
    expect(whenOf({ ts: "soon" })).toBe("soon");
  });
});

// These read the reader's own zone, so inputs are built as local times.
const at = (y: number, m: number, d: number, h: number, min: number) => new Date(y, m, d, h, min);
const now = at(2026, 2, 16, 15, 0);

describe("local clocks", () => {
  it("is 24-hour", () => {
    expect(clock(at(2026, 2, 16, 0, 5))).toBe("00:05");
    expect(clock(at(2026, 2, 16, 23, 5))).toBe("23:05");
  });

  it("names the year only when it is not this one", () => {
    expect(day(at(2026, 2, 11, 9, 0), now)).toBe("11 Mar");
    expect(day(at(2025, 2, 11, 9, 0), now)).toBe("11 Mar 2025");
  });

  it("prints an ISO day, padded", () => {
    expect(isoDay(at(2026, 0, 5, 0, 0))).toBe("2026-01-05");
  });

  it("formats a full stamp, or returns one it cannot read", () => {
    expect(when(at(2026, 2, 11, 9, 0).toISOString(), now)).toBe("11 Mar, 09:00");
    expect(when("last Tuesday", now)).toBe("last Tuesday");
  });

  it("is a clock today, a word yesterday, a date before that", () => {
    expect(whenShort(at(2026, 2, 16, 14, 30).toISOString(), now)).toBe("14:30");
    expect(whenShort(at(2026, 2, 15, 23, 50).toISOString(), now)).toBe("Yesterday");
    expect(whenShort(at(2026, 2, 11, 9, 0).toISOString(), now)).toBe("11 Mar");
    expect(whenShort(undefined, now)).toBe("");
    expect(whenShort("garbled", now)).toBe("garbled");
  });
});

import { describe, expect, it } from "vitest";
import { clock, day, when, whenShort } from "../src/lib/stamp";

/**
 * The app's clocks, which are one shape on every surface.
 *
 * Every case is built as a *local* time and read back as one, so the assertions
 * are about the format and not about the machine the suite happens to run on —
 * the same trap the row's own date test avoids.
 */
const at = (y: number, m: number, d: number, h: number, min: number) =>
  new Date(y, m, d, h, min);
const now = at(2026, 8, 16, 15, 0);

describe("a clock", () => {
  it("is 24-hour, at midnight and in the evening", () => {
    expect(clock(at(2026, 8, 16, 0, 5))).toBe("00:05");
    expect(clock(at(2026, 8, 16, 23, 5))).toBe("23:05");
    // Not "12:05 AM", not "11:05 PM": a reader who has never used a 12-hour
    // clock should not have to work out which side of noon a row is on.
    expect(clock(at(2026, 8, 16, 12, 0))).toBe("12:00");
  });
});

describe("a day", () => {
  it("names the year only when it is not this one", () => {
    expect(day(at(2026, 2, 11, 9, 0), now)).toBe("11 Mar");
    expect(day(at(2025, 2, 11, 9, 0), now)).toBe("11 Mar 2025");
  });
});

describe("a stamp", () => {
  it("is the date and a 24-hour clock", () => {
    expect(when("2026-09-16T23:05:00Z")).toBe(
      `${day(new Date("2026-09-16T23:05:00Z"))}, ${clock(new Date("2026-09-16T23:05:00Z"))}`,
    );
    // "17 Sept, 09:05" in the reader's own clock ("16 Sept" is UTC — the stamp is an
    // instant, and it is read where the reader is). en-GB says "Sept", which is
    // the point of naming a locale: the reader gets the same string whatever
    // their machine is set to.
    expect(when("2026-09-16T23:05:00Z", now)).toMatch(/^\d{1,2} \w+, \d{2}:\d{2}$/);
    expect(when("2026-09-16T23:05:00Z", now)).not.toMatch(/[AaPp]\.?[Mm]/);
    // A stamp from another year names it, because "17 Sept" alone would read as
    // this September.
    expect(when("2025-09-16T23:05:00Z", now)).toMatch(/^\d{1,2} \w+ 2025, \d{2}:\d{2}$/);
  });

  it("shows a stamp it cannot read as it arrived", () => {
    // Better a visibly wrong stamp than "Invalid Date": the reader can see that
    // something is wrong with the source rather than with their browser.
    expect(when("last Tuesday")).toBe("last Tuesday");
  });
});

describe("a row's date", () => {
  it("is a clock today, a word yesterday, and a date after that", () => {
    expect(whenShort(at(2026, 8, 16, 14, 30).toISOString(), now)).toBe("14:30");
    expect(whenShort(at(2026, 8, 15, 14, 30).toISOString(), now)).toBe("Yesterday");
    expect(whenShort(at(2026, 2, 11, 9, 0).toISOString(), now)).toBe("11 Mar");
    expect(whenShort(at(2025, 2, 11, 9, 0).toISOString(), now)).toBe("11 Mar 2025");
  });

  it("crosses midnight in the reader's own clock", () => {
    // A row written at 00:30 and read at 00:10 the next day is yesterday, not a
    // clock that reads later than the row above it.
    const justAfterMidnight = at(2026, 8, 17, 0, 10);
    expect(whenShort(at(2026, 8, 16, 23, 50).toISOString(), justAfterMidnight)).toBe("Yesterday");
    expect(whenShort(at(2026, 8, 17, 0, 5).toISOString(), justAfterMidnight)).toBe("00:05");
  });
});

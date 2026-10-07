import { describe, expect, it } from "vitest";
import { entryId, initials, parseDate } from "../src/lib/timeline/anchors";
import { msg } from "./fixtures";

describe("parseDate", () => {
  it("reads a day, month name and year", () => {
    expect(parseDate("Thu 16 Jul 2026")).toEqual({ y: 2026, m: 7, d: 16 });
    expect(parseDate("3 September 2025")).toEqual({ y: 2025, m: 9, d: 3 });
  });

  it("is null for anything else", () => {
    expect(parseDate("2026-07-16")).toBeNull();
    expect(parseDate("16 Foo 2026")).toBeNull();
    expect(parseDate(undefined)).toBeNull();
  });
});

describe("entryId", () => {
  it("derives an id from date, time and sender, de-duplicating", () => {
    const used = new Set<string>();
    const e = msg("", {
      id: undefined,
      date: "Thu 16 Jul 2026",
      time: "11:35",
      sender: "Jean-Luc Picard",
    });
    expect(entryId(e, used)).toBe("m-20260716-1135-jlp");
    expect(entryId(e, used)).toBe("m-20260716-1135-jlp-2");
  });

  it("names notes by date and falls back for missing parts", () => {
    const used = new Set<string>();
    expect(entryId(msg("", { id: undefined, kind: "note", date: "Mon 17 Aug 2026" }), used)).toBe(
      "m-20260817-note",
    );
    expect(entryId(msg("", { id: undefined, date: "whenever" }), used)).toBe("m-undated-0000-x");
  });

  it("prefers an explicit id", () => {
    expect(entryId(msg("given"), new Set())).toBe("given");
  });
});

describe("initials", () => {
  it("takes first and last initials, or two letters of one name", () => {
    expect(initials("Jean-Luc Picard")).toBe("JP");
    expect(initials("Data")).toBe("DA");
    expect(initials("Ada (Loomworks) Okoye")).toBe("AO");
    expect(initials("42")).toBe("?");
  });
});

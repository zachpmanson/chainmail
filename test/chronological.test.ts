import { describe, expect, it } from "vitest";
import { order, tzMinutes, zones } from "../src/lib/timeline/chronological";
import { idsFor, load, msg } from "./fixtures";

describe("tzMinutes", () => {
  it("reads labels and numeric offsets", () => {
    expect(tzMinutes("NZST")).toBe(720);
    expect(tzMinutes(" aedt ")).toBe(660);
    expect(tzMinutes("+0530")).toBe(330);
    expect(tzMinutes("-04:00")).toBe(-240);
  });

  it("is null for an unknown or absent zone", () => {
    expect(tzMinutes("Mars/Olympus")).toBeNull();
    expect(tzMinutes("")).toBeNull();
    expect(tzMinutes(undefined)).toBeNull();
  });
});

describe("zones", () => {
  it("labels a zone stated, inferred or unknown", () => {
    const z = zones([]);
    expect(z.label(msg("a", { tz: "AEST" }))).toEqual({ tz: "AEST", state: "stated" });
    expect(z.label(msg("b", { tz: "+1000", tzSource: "inferred" }))).toEqual({
      tz: "+1000",
      state: "inferred",
    });
    expect(z.label(msg("c"))).toEqual({ tz: undefined, state: "unknown" });
  });

  it("reads an unknown zone at the prevailing offset", () => {
    const stated = msg("a", { time: "09:00", tz: "AEST" });
    const unstated = msg("b", { time: "09:00" });
    const z = zones([stated, msg("x", { tz: "AEST" }), unstated]);
    expect(z.absolute(unstated)).toBe(z.absolute(stated));
  });

  it("takes a note's clock and zone from its label", () => {
    const z = zones([]);
    const note = msg("n", { kind: "note", label: "Call at 14:30 AEST" });
    expect(z.absolute(note)).toBe(z.absolute(msg("m", { time: "14:30", tz: "AEST" })));
  });

  it("puts an unparseable date at zero", () => {
    expect(zones([]).absolute(msg("a", { date: "someday" }))).toBe(0);
  });
});

describe("order", () => {
  it("sorts by absolute time, not the displayed clock", () => {
    const nz = msg("nz", { time: "09:51", tz: "NZST" });
    const au = msg("au", { time: "09:20", tz: "AEST" });
    const es = [au, nz];
    expect(order(es, idsFor(es)).map((e) => e.id)).toEqual(["nz", "au"]);
  });

  it("never puts a reply above its parent", () => {
    const parent = msg("p", { time: "10:00", tz: "UTC" });
    const reply = msg("r", { time: "09:00", tz: "UTC", parent: "p" });
    const es = [reply, parent];
    expect(order(es, idsFor(es)).map((e) => e.id)).toEqual(["p", "r"]);
  });

  it("keeps entries stranded by a parent cycle", () => {
    const es = [msg("x", { parent: "y" }), msg("y", { parent: "x" }), msg("z")];
    expect(order(es, idsFor(es)).map((e) => e.id)).toEqual(["z", "x", "y"]);
  });

  it("keeps all 58 synthetic entries, every reply after its parent", () => {
    const t = load("synthetic");
    const idOf = idsFor(t.messages);
    const seq = order(t.messages, idOf);
    const pos = new Map(seq.map((e, i) => [idOf(e), i]));
    expect(seq).toHaveLength(58);
    expect(seq.filter((e) => e.parent && pos.get(e.parent)! >= pos.get(idOf(e))!)).toEqual([]);
  });
});

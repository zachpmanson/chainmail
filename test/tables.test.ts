import { describe, expect, it } from "vitest";
import {
  chooseDelimiter,
  couldBeTable,
  MAX_COLS,
  MAX_ROWS,
  parseDelimited,
  readTable,
} from "../src/lib/message/tables";

describe("parseDelimited", () => {
  it("honours quoted delimiters, doubled quotes and newlines in fields", () => {
    expect(parseDelimited('name,amount\n"Okoye, Ada","1,204"\n"O""Brien","a\nb"\n', ",")).toEqual([
      ["name", "amount"],
      ["Okoye, Ada", "1,204"],
      ['O"Brien', "a\nb"],
    ]);
  });

  it("reads CRLF, a trailing delimiter and no final newline", () => {
    expect(parseDelimited("a,b\r\n1,2\r\n", ",")).toEqual([
      ["a", "b"],
      ["1", "2"],
    ]);
    expect(parseDelimited("a,b,\n", ",")).toEqual([["a", "b", ""]]);
    expect(parseDelimited("a,b", ",")).toEqual([["a", "b"]]);
  });

  it("leaves a mid-field quote literal", () => {
    expect(parseDelimited('6" pipe,O"Brien\n', ",")).toEqual([['6" pipe', 'O"Brien']]);
  });
});

describe("couldBeTable and chooseDelimiter", () => {
  it("asks the name and the type", () => {
    expect(couldBeTable("readings.CSV", "")).toBe(true);
    expect(couldBeTable("data", "text/tab-separated-values; charset=utf-8")).toBe(true);
    expect(couldBeTable("notes.txt", "text/plain")).toBe(false);
  });

  it("sniffs the delimiter a .csv actually uses", () => {
    expect(chooseDelimiter("a;b\n1;2\n", "x.csv", "")).toBe(";");
    expect(chooseDelimiter("a|b\n1|2\n", "x.csv", "")).toBe("|");
    expect(chooseDelimiter("a,b\n", "x.tsv", "")).toBe("\t");
    expect(chooseDelimiter("one\ntwo\n", "x.csv", "")).toBeNull();
  });
});

describe("readTable", () => {
  it("reads a header, rows and numeric columns", () => {
    expect(readTable("shed,reading\nNova,41.2\nOrion,38.9\n", "r.csv", "")).toEqual({
      header: ["shed", "reading"],
      rows: [
        ["Nova", "41.2"],
        ["Orion", "38.9"],
      ],
      numeric: [false, true],
      rowCount: 2,
      colCount: 2,
    });
  });

  it("pads a ragged row", () => {
    expect(readTable("a,b,c\n1,2\n", "r.csv", "")!.rows).toEqual([["1", "2", ""]]);
  });

  it("refuses prose, a lone header and an unannounced file", () => {
    expect(readTable("Dear Ada,\n\nThanks, and regards,\nBen\n", "l.csv", "")).toBeNull();
    expect(readTable("a,b", "r.csv", "")).toBeNull();
    expect(readTable("a,b\n1,2\n", "slurp.log", "")).toBeNull();
  });

  it("caps what it shows and counts what it saw", () => {
    const line = Array.from({ length: 50 }, (_, i) => `c${i}`).join(",");
    const t = readTable(Array(701).fill(line).join("\n"), "wide.csv", "")!;
    expect([t.header.length, t.rows.length, t.rowCount, t.colCount]).toEqual([
      MAX_COLS,
      MAX_ROWS,
      700,
      50,
    ]);
  });
});

import { describe, expect, it } from "vitest";
import { diffBaseToEdit, toText, editHtml, type Span } from "../src/lib/editDiff";

/** join spans the way the renderer does: single space between runs */
const plain = (spans: Span[]) => spans.map((s) => s.text).join(" ").trim();

describe("diffBaseToEdit", () => {
  it("marks a single-word substitution as strike + insert", () => {
    const spans = diffBaseToEdit("E: Amount Due", "E: Invoice Amount");
    expect(spans.filter((s) => s.kind === "del").map((s) => s.text)).toContain("Due");
    expect(spans.filter((s) => s.kind === "ins").map((s) => s.text)).toContain("Invoice");
    // struck removal + inserted replacement read in the quoter's line
    expect(plain(spans)).toMatch(/Invoice/);
    expect(plain(spans)).toMatch(/Due/);
    expect(plain(spans)).toMatch(/Amount/);
  });

  it("keeps identical text entirely as same", () => {
    const spans = diffBaseToEdit("the levy question", "the levy question");
    expect(spans.every((s) => s.kind === "same")).toBe(true);
    expect(plain(spans)).toBe("the levy question");
  });

  it("treats a pure insertion as an insert run", () => {
    const spans = diffBaseToEdit("on the cover sheet", "on the cover sheet and I");
    expect(plain(spans)).toBe("on the cover sheet and I");
    expect(spans.some((s) => s.kind === "ins")).toBe(true);
  });

  it("returns the edit verbatim when either side is empty", () => {
    expect(plain(diffBaseToEdit("", "we track Invoice Amount"))).toBe("we track Invoice Amount");
    expect(plain(diffBaseToEdit("a body", ""))).toBe("");
  });
});

describe("toText", () => {
  it("strips tags and unescapes entities", () => {
    expect(toText("<p>CSV &amp; layout</p>")).toBe("CSV & layout");
    expect(toText("<b>A:</b> Member").replace(/\s+/g, " ")).toBe("A: Member");
  });
});

describe("editHtml", () => {
  it("wraps the change in strike/insert and escapes the rest", () => {
    const h = editHtml("<p>E: Amount Due</p>", "E: <Invoice Amount");
    expect(h).toContain("<s class=\"edel\"");
    expect(h).toContain("<b class=\"eins\"");
    expect(h).toContain("&lt;"); // the leading < of <Invoice is escaped, not markup
    expect(h).not.toContain("<Invoice"); // no live tag from the quoter's text
  });

  it("is stable and lossless for the CSV example", () => {
    const base =
      "<p>CSV layout: A: Member Number &middot; B: ATS Number &middot; " +
      "C: Property Name &middot; D: Statement Date &middot; E: Amount Due</p>";
    const edit = "CSV layout: A: Member Number \u00b7 B: ATS Number \u00b7 " +
      "C: Property Name \u00b7 D: Statement Date \u00b7 E: Invoice Amount";
    const h = editHtml(base, edit);
    // the unchanged field list survives, and the E: change is marked
    expect(h).toContain("Member Number");
    expect(h).toContain("Invoice");
    expect(h).toContain("<b class=\"eins\"");
    expect(h.replace(/<[^>]+>/g, "").replace(/\s+/g, " ")).toContain("E: Invoice Amount");
  });
});
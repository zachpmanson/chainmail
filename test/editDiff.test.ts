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
  // In production the copy is the pasted message (which carries formatting) and
  // the original is the pre-edit base; the quoted body is the quoter's version.
  it("highlights the quoter's added words, keeping the copy's formatting", () => {
    const copy = "<p>CSV layout: E: <b>Invoice</b> Amount</p>";
    const original = "<p>CSV layout: E: Amount Due</p>";
    const h = editHtml(copy, original, "CSV layout: E: Invoice Amount");
    // the added word is a live-inserted highlight (escaped source-safe)
    expect(h).toContain("<b class=\"eins\">Invoice</b>");
    // an original-only word is elided, not struck: the copy simply does not have it
    expect(h).not.toContain("Due");
  });

  it("keeps the rest of the CSV list and marks only the quoter's added answer", () => {
    const copy =
      "<p>CSV layout: A: Member Number &middot; B: ATS Number " +
      "&middot; E: Invoice Amount</p>";
    const original =
      "<p>CSV layout: A: Member Number &middot; B: ATS Number &middot; E: Amount Due</p>";
    const body = "CSV layout: A: Member Number \u00b7 B: ATS Number \u00b7 E: Invoice Amount";
    const h = editHtml(copy, original, body);
    // the list survives, and only Invoice is highlighted as new to the original
    expect(h).toContain("Member Number");
    expect(h).toContain("<b class=\"eins\">Invoice</b>");
    expect(h).not.toContain("Due");
  });

  it("keeps the copy's tags, including a coloured run (the #52 repro)", () => {
    const copy =
      "<p>Once we use the CSV, will it be ours? - " +
      "<span style=\"color:red\">Yes, PDFs go to members</span>.</p>";
    const original = "<p>Once we use the CSV, will it be ours?</p>";
    const body = "Once we use the CSV, will it be ours? - Yes, PDFs go to members.";
    const h = editHtml(copy, original, body);
    // the red span survives and the added answer is highlighted inside it
    expect(h).toContain("<span style=\"color:red\">");
    expect(h).toContain("Yes, PDFs go to members");
    expect(h).toContain("<b class=\"eins\">");
  });

  it("renders a copy with no added words verbatim (formatting only)", () => {
    const copy = "<p>The <b>critical</b> figure stood at &middot; 1,240.</p>";
    const h = editHtml(copy, copy, "The critical figure stood at · 1,240.");
    expect(h).toContain("<p>");
    expect(h).toContain("<b>");
    expect(h).toContain("&middot;");
    expect(h).not.toContain("eins");
  });

  it("falls back to a plain diff when there is no derived copy", () => {
    // (no copy body) → diff the original against the quoter's text, still marked
    const h = editHtml("", "<p>E: Amount Due</p>", "E: Invoice Amount");
    expect(h).toContain("Invoice");
    expect(h).toContain("<del class=\"edel\"");
    expect(h).toContain("<b class=\"eins\"");
  });
});
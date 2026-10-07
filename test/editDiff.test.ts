import { describe, expect, it } from "vitest";
import { diffBaseToEdit, editHtml, toText } from "../src/lib/timeline/editDiff";

// The diff markup's classes are styling; the tags are what the diff decides.
const bare = (html: string) => html.replace(/ class="[^"]*"/g, "");

describe("diffBaseToEdit", () => {
  it("marks a substitution as an insert and a strike", () => {
    expect(diffBaseToEdit("E: Amount Due", "E: Invoice Amount")).toEqual([
      { kind: "same", text: "E:" },
      { kind: "ins", text: "Invoice" },
      { kind: "same", text: "Amount" },
      { kind: "del", text: "Due" },
    ]);
  });

  it("joins a run of inserted words into one span", () => {
    expect(diffBaseToEdit("on the sheet", "on the sheet and I").at(-1)).toEqual({
      kind: "ins",
      text: "and I",
    });
  });

  it("returns the edit verbatim when either side is empty", () => {
    expect(diffBaseToEdit("", "new words")).toEqual([{ kind: "same", text: "new words" }]);
    expect(diffBaseToEdit("a body", "")).toEqual([{ kind: "same", text: "" }]);
    expect(diffBaseToEdit("   ", "x")).toEqual([{ kind: "same", text: "x" }]);
  });
});

describe("toText", () => {
  it("strips tags and decodes known entities, leaving unknown ones", () => {
    expect(toText("<p>CSV &amp; layout &middot; &bogus;</p>")).toBe("CSV & layout · &bogus;");
  });
});

describe("editHtml", () => {
  it("highlights the copy's new words inside its own formatting", () => {
    expect(
      bare(
        editHtml(
          "<p>CSV layout: E: <b>Invoice</b> Amount</p>",
          "<p>CSV layout: E: Amount Due</p>",
          "CSV layout: E: Invoice Amount",
        ),
      ),
    ).toBe("<p>CSV layout: E: <b><b>Invoice</b></b> Amount</p>");
  });

  it("renders a copy identical to its original verbatim", () => {
    const copy = "<p>The <b>critical</b> figure &middot; 1,240.</p>";
    expect(editHtml(copy, copy, "ignored")).toBe(copy);
  });

  it("falls back to a flat, escaped diff with no derived copy", () => {
    expect(bare(editHtml("", "<p>E: Amount Due</p>", "E: Invoice <Amount>"))).toBe(
      "E: <del>Amount Due</del> <b>Invoice &lt;Amount&gt;</b>",
    );
  });

  it("keeps the quoter's line breaks when there is nothing to diff against", () => {
    expect(editHtml("", "", "line one\nline two")).toBe("line one<br>line two");
  });
});

// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import {
  analyseOriginal,
  defaultsToOriginal,
  originalHtmlScore,
} from "../src/lib/message/originalDefault";

const nested = "<table><tr><td>A<table><tr><td>B</td></tr></table></td></tr></table>";
const styled = '<div style="color:red"><p class="a">News</p><p class="b">More</p></div>';
const linkedImage = '<img src="logo.png"><a href="a">A</a><a href="b">B</a>';
const designed = nested + styled + linkedImage;
const signatureWrappers = [
  [
    "Gmail",
    (html: string) => `<div class="gmail_signature" data-smartmail="gmail_signature">${html}</div>`,
  ],
  [
    "collector fold",
    (html: string) =>
      `<details class="sig"><summary>signature</summary><div class="sigbd">${html}</div></details>`,
  ],
] as const;

describe("original HTML analysis", () => {
  it("does not select absent or ordinary HTML", () => {
    expect(analyseOriginal("  ")).toEqual({ score: 0, matches: [], defaultsToOriginal: false });
    expect(defaultsToOriginal("<p>Hi <a href='/'>there</a></p>")).toBe(false);
  });

  it("selects nested styled bodies at the unchanged threshold", () => {
    expect(analyseOriginal(nested + styled)).toEqual({
      score: 3,
      matches: [
        { rule: "nested tables", points: 2 },
        { rule: "3+ styled elements", points: 1 },
      ],
      defaultsToOriginal: true,
    });
  });

  it("does not count separate sibling tables as nested", () => {
    const siblings = "<table><tr><td>A</td></tr></table><table><tr><td>B</td></tr></table>";
    expect(originalHtmlScore(siblings)).toBe(0);
    expect(originalHtmlScore(siblings + styled + linkedImage)).toBe(2);
    expect(defaultsToOriginal(siblings + styled + linkedImage)).toBe(false);
  });

  it("counts an element with both class and style only once", () => {
    expect(originalHtmlScore('<p class="a" style="color:red">A</p><p class="b">B</p>')).toBe(0);
  });

  it("combines all body signals and explains exactly their total", () => {
    const analysis = analyseOriginal(designed);
    expect(analysis.score).toBe(4);
    expect(analysis.matches).toEqual([
      { rule: "nested tables", points: 2 },
      { rule: "3+ styled elements", points: 1 },
      { rule: "image + 2+ links", points: 1 },
    ]);
    expect(analysis.score).toBe(analysis.matches.reduce((sum, m) => sum + m.points, 0));
    expect(analysis.defaultsToOriginal).toBe(true);
  });

  for (const [name, wrap] of signatureWrappers) {
    it(`excludes every signal supplied by a ${name} signature, even with no body`, () => {
      expect(originalHtmlScore(wrap(designed))).toBe(0);
      expect(originalHtmlScore("<p>Hello</p>" + wrap(designed))).toBe(0);
    });

    it(`keeps body signals without letting a ${name} signature complete them`, () => {
      expect(originalHtmlScore(nested + wrap(styled + linkedImage))).toBe(2);
      expect(originalHtmlScore(styled + wrap(nested + linkedImage))).toBe(1);
      expect(originalHtmlScore(linkedImage + wrap(nested + styled))).toBe(1);
      expect(originalHtmlScore(designed + wrap(designed))).toBe(4);
      expect(
        originalHtmlScore(
          '<p class="a">A</p><p style="color:red">B</p>' + wrap('<p class="c">C</p>'),
        ),
      ).toBe(0);
      expect(originalHtmlScore('<img src="x"><a href="a">A</a>' + wrap('<a href="b">B</a>'))).toBe(
        0,
      );
      expect(originalHtmlScore("<table><tr><td>Body" + wrap(nested) + "</td></tr></table>")).toBe(
        0,
      );
    });
  }

  it("recognises marker class tokens, including nested wrappers, not substring lookalikes", () => {
    expect(
      originalHtmlScore(`<div><div class="other gmail_signature extra">${designed}</div></div>`),
    ).toBe(0);
    expect(originalHtmlScore(`<div class="not_gmail_signature">${designed}</div>`)).toBe(4);
    expect(originalHtmlScore(`<div class="sig">${designed}</div>`)).toBe(4);
  });

  it("conservatively retains unmarked content instead of guessing from a sign-off or footer", () => {
    expect(originalHtmlScore(`<p>Regards, Ada</p><div id="footer">${designed}</div>`)).toBe(4);
  });

  it("does not read fake tags in attributes, comments, styles or escaped text as elements", () => {
    expect(
      originalHtmlScore(
        `<!-- ${designed} --><style>/* ${designed} */</style><p title='${designed}'>text</p>&lt;table&gt;`,
      ),
    ).toBe(0);
  });

  it("does not treat size, one table, or an image as enough", () => {
    expect(defaultsToOriginal(`<p>${"word ".repeat(5000)}</p>`)).toBe(false);
    expect(defaultsToOriginal(nested)).toBe(false);
    expect(defaultsToOriginal('<img src="x">')).toBe(false);
  });
});

import { describe, expect, it } from "vitest";
import { defaultsToOriginal, originalHtmlScore } from "../src/lib/message/originalDefault";

describe("original HTML default", () => {
  it("does not select absent or ordinary HTML", () => {
    expect(defaultsToOriginal("  ")).toBe(false);
    expect(defaultsToOriginal("<p>Hi <a href='/'>there</a></p>")).toBe(false);
  });

  it("selects multi-table styled bodies at the threshold", () => {
    const html =
      '<div class="mail"><table style="x"><tr><td>A</td></tr></table><table class="x"><tr><td>B</td></tr></table><p class="x">C</p></div>';
    expect(originalHtmlScore(html)).toBe(3);
    expect(defaultsToOriginal(html)).toBe(true);
  });

  it("combines image-and-links with styling", () => {
    const html =
      '<div style="x"><p class="x">News</p><table><tr><td>A</td></tr></table><table><tr><td>B</td></tr></table><img src="x"><a href="a">A</a><a href="b">B</a></div>';
    expect(originalHtmlScore(html)).toBe(4);
    expect(defaultsToOriginal(html)).toBe(true);
  });

  it("does not treat size, one table, or image presence as enough", () => {
    expect(defaultsToOriginal(`<p>${"word ".repeat(5000)}</p>`)).toBe(false);
    expect(defaultsToOriginal("<table><tr><td>large</td></tr></table>".repeat(1))).toBe(false);
    expect(defaultsToOriginal('<img src="x">')).toBe(false);
  });
});

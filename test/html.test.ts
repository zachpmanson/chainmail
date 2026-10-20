import { describe, expect, it } from "vitest";
import { escapeHtmlText } from "../src/lib/ui/html";

describe("HTML template text", () => {
  it("keeps a title from creating markup", () => {
    expect(escapeHtmlText('</title><p data-extra="yes">title</p>')).toBe(
      "&lt;/title&gt;&lt;p data-extra=&quot;yes&quot;&gt;title&lt;/p&gt;",
    );
  });

  it("preserves literal entity-looking text and ordinary Unicode", () => {
    expect(escapeHtmlText("R&D &lt; José's report")).toBe("R&amp;D &amp;lt; José&#39;s report");
  });
});

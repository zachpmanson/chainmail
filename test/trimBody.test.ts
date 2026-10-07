import { describe, expect, it } from "vitest";
import { hasBody, trimBody } from "../src/lib/message/trimBody";

const SIG = '<details class="sig"><summary>signature</summary><div><p>Regards</p></div></details>';

describe("trimBody", () => {
  it("drops blank paragraphs at either edge", () => {
    expect(trimBody("<p> </p><p>Good morning.</p><p><br></p><p>&nbsp;</p>")).toBe(
      "<p>Good morning.</p>",
    );
  });

  it("keeps an author's mid-message blank line", () => {
    const body = "<p>Hello Tom,</p><p> </p><p>Please see below.</p>";
    expect(trimBody(body)).toBe(body);
  });

  it("trims up to a signature fold and keeps the fold", () => {
    expect(trimBody(`<p>Please see below.</p><p> </p><p> </p>${SIG}`)).toBe(
      `<p>Please see below.</p>${SIG}`,
    );
  });

  it("recurses into a single wrapping div", () => {
    expect(trimBody(`<div dir="ltr"><div>Hi Jason,</div><div><br/></div>${SIG}</div>`)).toBe(
      `<div dir="ltr"><div>Hi Jason,</div>${SIG}</div>`,
    );
  });

  it("peels a deep blank run at the very end", () => {
    expect(
      trimBody(
        '<div dir="ltr"><div>Thanks,</div><div><br/></div>' +
          '<div><span>Bye.</span><br/><span><br/></span></div><br clear="all"/></div>',
      ),
    ).toBe('<div dir="ltr"><div>Thanks,</div><div><br/></div><div><span>Bye.</span></div></div>');
  });

  it("keeps a <pre>, an image, and an all-blank body empties", () => {
    expect(trimBody("<p>Code:</p><pre>  indented  </pre>")).toBe(
      "<p>Code:</p><pre>  indented  </pre>",
    );
    expect(trimBody('<div><img src="p.png"></div><p> </p>')).toBe('<div><img src="p.png"></div>');
    expect(trimBody("<p> </p><p><br></p>")).toBe("");
  });

  it("is idempotent", () => {
    const once = trimBody(`<p> </p><p>Done.</p><p> </p>${SIG}`);
    expect(trimBody(once)).toBe(once);
  });
});

describe("hasBody", () => {
  it("sees text, an image or a fold as content", () => {
    expect(hasBody("<p>hi</p>")).toBe(true);
    expect(hasBody('<img src="x">')).toBe(true);
    expect(hasBody(SIG)).toBe(true);
  });

  it("ignores blanks, comments, scripts and styles", () => {
    expect(hasBody("<p>&nbsp;</p><br>")).toBe(false);
    expect(hasBody("<!--[if mso]><table><tr><td>Hi</td></tr></table><![endif]-->")).toBe(false);
    expect(hasBody("<style>p{}</style><script>x()</script>")).toBe(false);
  });
});

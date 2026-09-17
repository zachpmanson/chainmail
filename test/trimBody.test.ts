import { describe, expect, it } from "vitest";
import { hasBody, trimBody } from "../src/lib/trimBody";

/**
 * trimBody strips whitespace-only nodes from the exposed edges of a body,
 * leaving the author's content and any signature fold intact. The shapes here
 * are the ones real mail clients emit: a leading blank paragraph, a trailing
 * run of spacer paragraphs, Gmail <u></u> filler, and the folded signature the
 * backend produces before the frontend sees the body.
 */
describe("trimBody", () => {
  it("drops a leading blank paragraph", () => {
    expect(trimBody("<p> </p><p>Good morning.</p>")).toBe("<p>Good morning.</p>");
  });

  it("drops trailing spacer paragraphs", () => {
    expect(trimBody("<p>Done.</p><p><br></p><p>&nbsp;</p>")).toBe("<p>Done.</p>");
  });

  it("trims directly up to a folded signature, keeping the fold", () => {
    const body =
      "<p>Please see below.</p><p> </p><p> </p>" +
      '<details class="sig"><summary>signature</summary><div><p>Regards</p></div></details>';
    expect(trimBody(body)).toBe(
      "<p>Please see below.</p>" +
      '<details class="sig"><summary>signature</summary><div><p>Regards</p></div></details>',
    );
  });

  it("keeps an author's mid-message blank line", () => {
    expect(trimBody("<p>Hello Tom,</p><p> </p><p>Please see below.</p>")).toBe(
      "<p>Hello Tom,</p><p> </p><p>Please see below.</p>",
    );
  });

  it("keeps edge whitespace inside a <pre>", () => {
    expect(trimBody("<p>Code:</p><pre>  indented  </pre>")).toBe("<p>Code:</p><pre>  indented  </pre>");
  });

  it("keeps an image as content", () => {
    expect(trimBody('<div><img src="p.png"></div><p>See above.</p>')).toBe(
      '<div><img src="p.png"></div><p>See above.</p>',
    );
  });

  it("is idempotent on an already-trimmed body", () => {
    const body = "<p>First.</p><p>Second.</p>";
    expect(trimBody(body)).toBe(body);
  });

  it("handles Gmail-style u-filler paragraphs", () => {
    const body =
      "<p>Hello Tom,<u></u><u></u></p>" +
      "<p> <u></u><u></u></p>" +
      "<p>Please see below.<u></u><u></u></p>" +
      '<details class="sig"><summary>signature</summary><div><p>Regards</p></div></details>';
    expect(trimBody(body)).toBe(
      "<p>Hello Tom,<u></u><u></u></p>" +
      "<p> <u></u><u></u></p>" +
      "<p>Please see below.<u></u><u></u></p>" +
      '<details class="sig"><summary>signature</summary><div><p>Regards</p></div></details>',
    );
  });

  it("trims a blank nested before the fold inside a single wrapper", () => {
    // Mail clients commonly wrap the whole message in one <div>; the blank
    // spacer sits inside it, just above the fold. Top-level trimming would
    // miss it, so the wrapper's interior is trimmed recursively.
    const body =
      '<div dir="ltr"><div>Hi Jason,</div><div>Yes we also have</div>' +
      '<div><br/></div>' +
      '<details class="sig"><summary>signature</summary><div>Regards</div></details></div>';
    expect(trimBody(body)).toBe(
      '<div dir="ltr"><div>Hi Jason,</div><div>Yes we also have</div>' +
      '<details class="sig"><summary>signature</summary><div>Regards</div></details></div>',
    );
  });
});
  it("trims a deep <br clear=\"all\"/> run before a wrapped fold", () => {
    // Gmail nests the message and its signature as adjacent sibling blocks,
    // and the <br clear="all"/> lives two levels down, at the tail of the
    // content block. The trim must descend into that block so the body ends
    // edge-on to the disclosure.
    const body =
      '<div dir="ltr">' +
      '<div><span>Done.</span><br/><span><br/></span></div>' +
      '<br clear="all"/>' +
      '</div>' +
      '<div><details class="sig"><summary>signature</summary><div>Ada</div></details></div>';
    expect(trimBody(body)).toBe(
      '<div dir="ltr"><div><span>Done.</span></div></div>' +
      '<div><details class="sig"><summary>signature</summary><div>Ada</div></details></div>',
    );
  });

  it("trims a deep blank run at the very end of a body", () => {
    // No fold: the trailing <br>/blank run sits nested inside the last content
    // element. The final kept span's own tail is trimmed too.
    const body =
      '<div dir="ltr"><div>Thanks,</div><div><br/></div>' +
      '<div><span>Bye.</span><br/><span style="text-align:center"><br/></span></div>' +
      '<br clear="all"/></div>';
    expect(trimBody(body)).toBe(
      '<div dir="ltr"><div>Thanks,</div><div><br/></div><div><span>Bye.</span></div></div>',
    );
  });

  it("trims leading blanks inside a wrap that hides a fold", () => {
    // A Gmail signature block is an HTML table that opens with one or two
    // <br clear="all"/> before the table, so the separator sits as leading
    // children *inside* the wrapper that also holds the fold. The body must
    // still lift edge-on to the disclosure — those interior leading blanks
    // are dropped, the fold and the wrapper's open/close tags stay.
    const body =
      '<div dir="auto">Hi mate,</div>' +
      '<div dir="auto"><br/></div>' +
      '<div dir="auto">Reach Jason&nbsp;</div>' +
      '<div><br clear="all"/><br clear="all"/>' +
      '<div><details class="sig"><summary>signature</summary><div>Lane</div></details></div></div>';
    expect(trimBody(body)).toBe(
      '<div dir="auto">Hi mate,</div>' +
      '<div dir="auto"><br/></div>' +
      '<div dir="auto">Reach Jason&nbsp;</div>' +
      '<div><div><details class="sig"><summary>signature</summary><div>Lane</div></details></div></div>',
    );
  });

/**
 * hasBody answers the one question a bubble asks before drawing a body: is there
 * anything here at all? It is the trim's own judgement of content (visible text,
 * an image, a preformatted block, a folded signature) asked of the whole body,
 * so a body the trim would find nothing to keep in is exactly a body that says
 * "No body" instead of drawing empty.
 */
describe("hasBody", () => {
  it("says nothing of a body with no text in it", () => {
    for (const body of [
      "",
      "   ",
      "\n\t",
      "<p></p>",
      "<p> </p>",
      "<br>",
      "<div>&nbsp;</div>",
      "<div>&#160;</div>",
      "<p><span> </span></p>",
      "<!-- a comment is not a sentence -->",
      "<p><!--[if mso]><table><tr><td>hidden from every browser</td></tr></table><![endif]--></p>",
      "<style>p{color:red}</style>",
    ]) {
      expect(hasBody(body), JSON.stringify(body)).toBe(false);
    }
  });

  it("finds a body in anything that says something", () => {
    for (const body of [
      "hi",
      "<p>Roof access is fine.</p>",
      "<p>&nbsp;ok&nbsp;</p>",
      '<p><img src="/v1/attachments/abc" alt=""></p>',
      "<pre>  </pre>",
      "<pre>code</pre>",
      '<details class="sig"><summary>signature</summary><div><p>Regards</p></div></details>',
      // A fold the client wrapped in its own div is still a fold: the trim keeps
      // it, so this cannot call the body empty and draw the notice over it.
      '<div><div><details class="sig"><summary>signature</summary><div>Lane</div></details></div></div>',
    ]) {
      expect(hasBody(body), JSON.stringify(body)).toBe(true);
    }
  });

  it("still finds a body when a comment stands beside one", () => {
    // The stripping above is what a browser draws nothing for, not a licence to
    // stop reading: a message with a conditional comment above its words is a
    // message with words, and the notice must not be drawn over them.
    expect(hasBody("<p><!--[if mso]><td>x</td><![endif]-->Hi</p>")).toBe(true);
  });
});

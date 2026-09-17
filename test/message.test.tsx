// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Message, type MessageProps } from "../src/components/Message";

afterEach(cleanup);

/**
 * `Message` is the presentation half of the transcript, so it is tested with no
 * spec anywhere near it: the props below are everything a caller holding a
 * sender, a clock and a body can supply. What only the pipeline can produce —
 * the reply link, the provenance line, an inline edit, the clipboard payload —
 * is furniture the component must draw where it is given and leave out where it
 * is not, and that is what these tests pin.
 */

const bubble = (over: Partial<MessageProps> = {}): MessageProps => ({
  id: "m1",
  body: "<p>invented body</p>",
  sender: "Ada Okoye",
  org: "Loomworks",
  orgSlot: "o2",
  stamp: { date: "Mon 2 Mar 2026", time: "09:15", zone: "unknown" },
  ...over,
});

const draw = (over: Partial<MessageProps> = {}) => render(<Message {...bubble(over)} />);

describe("a bubble drawn from its props alone", () => {
  it("takes the sender, the colour slot and the clock from the caller", () => {
    const { container } = draw();
    const msg = container.querySelector(".msg")!;
    expect(msg.className).toBe("msg o2");
    expect(msg.id).toBe("m1");
    expect(msg.querySelector(".nm")!.textContent).toBe("Ada Okoye");
    expect(msg.querySelector(".org")!.textContent).toBe("Loomworks");
    expect(msg.querySelector(".bd")!.innerHTML).toBe("<p>invented body</p>");
    // the timestamp links to the bubble it is printed on
    expect(msg.querySelector(".tm")!.getAttribute("href")).toBe("#m1");
    expect(msg.querySelector(".to")!.textContent).toBe("to —");
  });

  it("states the message's own subject in the receipt, where it had one", () => {
    // The header line is who and when; a second title there would compete with the
    // thread's own, and a reply that renames a thread would be nowhere on the page.
    // So a message states the subject it carried in the receipt it opens.
    const { container } = draw({ subject: "Solar install quote: dates" });
    const subj = container.querySelector(".hdet .subj")!;
    expect(subj.textContent).toBe("Solar install quote: dates");
    // The whole subject on hover, since the line can be long and the panel ellipses
    // nothing: a title is what a reader checks a truncated one against.
    expect(subj.getAttribute("title")).toBe("Solar install quote: dates");
    // And a message that stated none says nothing: not an empty line, and not a
    // word standing in for the absence.
    expect(draw().container.querySelector(".hdet .subj")).toBeNull();
    expect(draw().container.querySelector(".hdet .hsub")).toBeNull();
  });

  it("puts the ids on the subject's own line, at its right end", () => {
    // The subject is what the message is about and the ids under it are where it
    // is: a reader inspecting a message is usually after one or the other, so they
    // share the receipt's first line instead of the quiet line of ids taking one
    // of its own under the recipient.
    const { container } = draw({
      subject: "Solar install quote: dates",
      source: <span className="src">msg 18bd3f21</span>,
    });
    const line = container.querySelector(".hdet .hsub")!;
    expect(line.querySelector(".subj")!.textContent).toBe("Solar install quote: dates");
    // The subject first, the ids after it: one row, and the ids are its right end.
    expect(line.firstElementChild!.className).toBe("subj");
    expect(line.lastElementChild!.className).toBe("src");
    // A message with no subject still shows where it was found — the row is the
    // subject's line rather than a row that needs one.
    const ids = draw({ source: <span className="src">msg 18bd3f21</span> });
    expect(ids.container.querySelector(".hdet .hsub .src")).not.toBeNull();
    expect(ids.container.querySelector(".hdet .hsub .subj")).toBeNull();
  });

  it("carries the flags it is given as the classes the stylesheet reads", () => {
    const { container } = draw({ me: true, quoted: true, chainStart: true });
    expect(container.querySelector(".msg")!.className).toBe("msg o2 me q chstart");
  });

  it("trims the edges of the body it is handed", () => {
    // A leading blank paragraph is not something the reader should be shown, and
    // the caller should not have to know that: the bubble is where a body's
    // edges become presentable.
    const { container } = draw({ body: "<p> </p><p>Good morning.</p>" });
    expect(container.querySelector(".bd")!.innerHTML).toBe("<p>Good morning.</p>");
  });

  it("stays quiet where the caller has no spec furniture to give it", () => {
    const { container } = draw();
    // nothing only the pipeline can produce — and nothing standing in for it
    expect(container.querySelector(".copyjson")).toBeNull();
    expect(container.querySelector(".newpill, .revpill")).toBeNull();
    expect(container.querySelector(".par, .tstart")).toBeNull();
    expect(container.querySelector(".src")).toBeNull();
    expect(container.querySelector(".edits")).toBeNull();
    expect(container.querySelector(".atts")).toBeNull();
  });

  it("draws the furniture it is given, in the bubble's own places", () => {
    const { container } = draw({
      mark: "new",
      copyJson: { id: "m1" },
      reply: <a className="par">in reply to Bo</a>,
      source: <span className="src">unspooled</span>,
      edits: <div className="edits" />,
    });
    expect(container.querySelector(".msg")!.className).toContain("isnew");
    expect(container.querySelector(".hdr .newpill")!.textContent).toBe("new");
    expect(container.querySelector(".hdr .copyjson")).not.toBeNull();
    expect(container.querySelector(".hdr .htail .par")).not.toBeNull();
    expect(container.querySelector(".hdet .src")).not.toBeNull();
    expect(container.querySelector(".bub .edits")).not.toBeNull();
  });

  it("opens the receipt from the header, with the copy control behind it", () => {
    // The header is the disclosure and the receipt is what it opens: nothing
    // that only the pipeline supplies — the to line, the ids, the clip — is left
    // standing in the summary, where it would be read once and be height on
    // every message thereafter. The reply is the exception, and deliberately so:
    // it is the shape of the conversation, and it sits at the right of the line
    // with the caret, on the part a reader scans.
    const { container } = draw({
      to: "Bo Halvorsen, cc Cy Okafor",
      copyJson: { id: "m1" },
      reply: <a className="par">in reply to Bo</a>,
      source: <span className="src">unspooled</span>,
    });
    const hdr = container.querySelector("details.hdr")!;
    const sum = hdr.querySelector("summary")!;
    expect(sum).not.toBeNull();
    expect(sum.querySelector(".copyjson, .src, .to")).toBeNull();
    const det = hdr.querySelector(".hdet")!;
    expect(det.querySelector(".to")!.textContent).toBe("to Bo Halvorsen, cc Cy Okafor");
    expect(det.querySelector(".par")).toBeNull();
    expect(det.querySelector(".src")).not.toBeNull();
    expect(det.querySelector(".copyjson")).not.toBeNull();
    // the summary is the sender, the clock and the reply, and the tail closes it
    expect(hdr.firstElementChild!.tagName).toBe("SUMMARY");
    expect(sum.querySelector(".nm")!.textContent).toBe("Ada Okoye");
    expect(sum.querySelector(".tm")).not.toBeNull();
    const tail = sum.querySelector(".htail")!;
    expect(tail.querySelector(".par")!.textContent).toBe("in reply to Bo");
    // always drawn, reply or none: the caret lives inside it
    const bare = draw({});
    expect(bare.container.querySelector("details.hdr > summary > .htail")!.textContent).toBe("");
    expect(container.querySelector(".foot")).toBeNull();
  });

  it("copies the payload it was handed, as JSON", () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    draw({ copyJson: { id: "m1", chain: null } });
    fireEvent.click(screen.getByRole("button", { name: /copy this message's json/i }));
    expect(JSON.parse(writeText.mock.calls[0]![0])).toEqual({ id: "m1", chain: null });
  });
});

describe("how a bubble says what it knows about its clock", () => {
  it("states a stated zone plainly", () => {
    const { container } = draw({ stamp: { date: "Mon 2 Mar 2026", time: "09:15", tz: "AEDT", zone: "stated" } });
    const tz = container.querySelector(".tz")!;
    expect(tz.className).toBe("tz");
    expect(tz.textContent).toBe("AEDT");
  });

  it("shows an inferred zone as the claim it is", () => {
    const { container } = draw({ stamp: { date: "Mon 2 Mar 2026", time: "09:15", tz: "AEDT", zone: "inferred" } });
    expect(container.querySelector(".tz")!.className).toBe("tz tzi");
    expect(container.querySelector(".tz")!.textContent).toBe(" AEDT?");
  });

  it("says an unplaced clock is unplaced, rather than leaving it bare", () => {
    const { container } = draw({ stamp: { date: "Mon 2 Mar 2026", zone: "unknown" } });
    expect(container.querySelector(".tz")!.className).toBe("tz tzu");
    // One mark, and a tooltip that says why: an unplaced clock is common enough
    // that a sentence at each one stops being read.
    expect(container.querySelector(".tz")!.textContent).toBe(" ?");
    expect(container.querySelector(".tz")!.getAttribute("title")).toMatch(/Zone unknown/);
  });
});

/**
 * A body with nothing in it. Most mail has words; the ones that do not are the
 * ones worth pinning down, because a bubble drawn without a body is a gap the
 * reader has to interpret — and the answer ("the sender sent nothing") is not the
 * one a gap suggests ("this failed to render").
 */
describe("a message that carried no body", () => {
  it("says so, in the page's own voice", () => {
    const { container } = draw({ body: "" });
    const bd = container.querySelector(".bd")!;
    expect(bd.querySelector(".nobody")!.textContent).toBe("No body");
    // Nothing of the sender's is drawn: the placeholder is the body.
    expect(bd.children).toHaveLength(1);
  });

  it("treats markup that says nothing as nothing", () => {
    // What an empty text part looks like once the ingest has serialized it: an
    // empty paragraph, a line break, a run of spaces. None of them is a sentence.
    for (const body of ["<p></p>", "<p> </p>", "<br>", "<div>&nbsp;</div>", "<p>\n\t</p>", "   ", "<!-- nothing -->"]) {
      const { container } = draw({ body });
      expect(container.querySelector(".nobody"), JSON.stringify(body)).not.toBeNull();
    }
  });

  it("leaves a body alone when it has anything to show", () => {
    // A picture, a preformatted block or a folded signature all say something a
    // sentence does not, and none of them is a hole to fill with our words. A
    // <pre> counts even when its own text is blank, because its whitespace is the
    // author's — the same judgement the trim makes about it.
    const bodies = [
      "<p>Roof access is fine from the 14th.</p>",
      '<p><img src="/v1/attachments/abc" alt=""></p>',
      "<pre>  </pre>",
      '<details class="sig"><summary>…</summary><div class="sigbd"><p>Ada</p></div></details>',
    ];
    for (const body of bodies) {
      const { container } = draw({ body });
      expect(container.querySelector(".nobody"), JSON.stringify(body)).toBeNull();
      // And the sender's own markup is what is drawn, untouched by this.
      expect(container.querySelector(".bd")!.innerHTML, JSON.stringify(body)).not.toBe("");
    }
  });
});

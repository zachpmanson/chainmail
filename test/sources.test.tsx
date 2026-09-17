import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Timeline } from "../src/components/Timeline";
import { normalise } from "../src/lib/normalise";
import { msgCount, provenance } from "../src/lib/sources";
import type { Entry, Timeline as Spec } from "../src/lib/spec";

/** Invented handles, hex-shaped like the real thing. No mailbox here is real. */
const H = [
  "1a2b3c4d5e6f7a8b",
  "2b3c4d5e6f7a8b9c",
  "3c4d5e6f7a8b9cad",
  "4d5e6f7a8b9cadbe",
  "5e6f7a8b9cadbecf",
  "6f7a8b9cadbecfd0",
  "7a8b9cadbecfd0e1",
];

const entry = (over: Partial<Entry>): Entry => ({
  date: "Mon 2 Mar 2026",
  time: "09:15",
  tz: "AEDT",
  tzSource: "stated",
  sender: "Ada Byron",
  org: "Loomworks",
  fromEmail: "ada@loomworks.example",
  to: "Bo Halvorsen",
  body: "<p>invented body</p>",
  ...over,
});

const page = (messages: Entry[]) =>
  renderToStaticMarkup(
    <Timeline spec={normalise({ title: "Loom cutover", messages } as Spec)} />,
  );

/** Just the header's receipt, so a panel above cannot satisfy an assertion by
 *  accident — cut off before the bubble, so a body cannot either. */
const receipt = (messages: Entry[]) => {
  const m = /<div class="hdet">(.*?)<\/div><\/details><div class="bub">/s.exec(page(messages));
  if (!m) throw new Error("no header receipt in the rendered page");
  return m[1]!;
};

/** Every header receipt, so a multi-message page can assert on a specific one. */
const receipts = (messages: Entry[]) =>
  [...page(messages).matchAll(/<div class="hdet">(.*?)<\/div><\/details><div class="bub">/gs)].map((m) => m[1]!);

describe("reading a provenance line", () => {
  it("recognises a list of generated ids", () => {
    const p = provenance(`unspooled from msg ${H[0]}, msg ${H[1]}`);
    expect(p.kind).toBe("ids");
    if (p.kind !== "ids") return;
    expect(p.prefix).toBe("unspooled from ");
    expect(p.ids.map((i) => i.gmailId)).toEqual([H[0], H[1]]);
  });

  it("leaves a hand-written source as the prose it is", () => {
    // fixtures/synthetic.json writes sources this way, and a count of the commas
    // in it would be a claim about the trail that nobody made.
    for (const s of ["unspooled from the 2 Aug email", "unspooled from quoted text"]) {
      expect(provenance(s)).toEqual({ kind: "prose", text: s });
    }
  });

  it("takes a host the mailbox does not hold as an id with nothing to open", () => {
    const p = provenance("unspooled from mail:<a@loomworks>, quote:abc123");
    expect(p.kind).toBe("ids");
    if (p.kind !== "ids") return;
    expect(p.ids.map((i) => i.gmailId)).toEqual([undefined, undefined]);
  });
});

describe("counting messages", () => {
  it("never says '1 msgs'", () => {
    expect(msgCount(1)).toBe("1 msg");
    expect(msgCount(2)).toBe("2 msgs");
    expect(msgCount(7)).toBe("7 msgs");
  });
});

describe("the source line under a bubble", () => {
  it("names every host on one line, since the receipt above it is the disclosure", () => {
    const rec = receipt([
      entry({ quoted: true, source: `unspooled from ${H.map((h) => `msg ${h}`).join(", ")}` }),
    ]);
    // Every id is drawn, in order, on one line. The receipt this sits in is a
    // <details> already, so a second disclosure here asked the reader to open the
    // receipt and then open the line to reach the ids — which are the only reason
    // to open either.
    expect(rec).not.toContain("<details");
    expect(rec).toContain('unspooled from <span class="sid">');
    let at = -1;
    for (const h of H) {
      // named where it is, and in the thread's order: the commas outside .sid are
      // the only break opportunity, so the line cannot reorder itself by wrapping
      expect(rec.indexOf(`msg ${h}`, at + 1)).toBeGreaterThan(at);
      at = rec.indexOf(`msg ${h}`, at + 1);
      // none of these hosts is a row on this page, so no unspooled id is an
      // outbound Gmail link either
      expect(rec).not.toContain(`href="https://mail.google.com/mail/u/0/#all/${h}"`);
    }
    expect(rec).not.toMatch(/\b\d+ msgs?\b/);
  });

  it("shows one host inline, and never says '1 msgs'", () => {
    const rec = receipt([entry({ quoted: true, source: `unspooled from msg ${H[0]}` })]);
    expect(rec).not.toMatch(/\b1 msgs?\b/);
    expect(rec).not.toContain("<details");
    // no on-page message with this gmailId: the id is named, not shipped to Gmail
    expect(rec).toContain(`unspooled from <span class="sid">msg ${H[0]}</span>`);
    expect(rec).not.toContain("mail.google.com");
  });

  it("anchors an unspooled id to the same-page message it came from", () => {
    const real = entry({ sender: "Ada Byron", source: `msg ${H[0]}`, gmailId: H[0] });
    const spool = entry({ sender: "Bo Halvorsen", quoted: true, source: `unspooled from msg ${H[0]}` });
    const all = receipts([real, spool]);
    const spoolFooter = all.find((f) => f.includes("unspooled from"))!;
    // the unspooled id links to the on-page anchor of the message it was lifted
    // out of, not out to Gmail
    expect(spoolFooter).toContain(
      `unspooled from <span class="sid"><a href="#m-20260302-0915-ab" title="The message this was unspooled from, on this page">msg ${H[0]}</a></span>`,
    );
    expect(spoolFooter).not.toContain("mail.google.com");
    // the named message's own receipt still opens its mailbox copy
    const realFooter = all.find((f) => f.includes(`msg ${H[0]}`) && !f.includes("unspooled"))!;
    expect(realFooter).toContain(`href="https://mail.google.com/mail/u/0/#all/${H[0]}"`);
  });

  it("links the mailbox's own id on a direct message", () => {
    const rec = receipt([entry({ source: `msg ${H[0]}`, gmailId: H[0] })]);
    expect(rec).toContain(`href="https://mail.google.com/mail/u/0/#all/${H[0]}"`);
    expect(rec).not.toContain("<details");
  });

  it("leaves an entry with no source alone", () => {
    const rec = receipt([entry({})]);
    expect(rec).not.toContain('class="src');
    expect(rec).not.toContain("unspooled");
  });
});

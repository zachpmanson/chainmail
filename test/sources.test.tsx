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

/** Just the bubble footer, so a panel above cannot satisfy an assertion by accident. */
const footer = (messages: Entry[]) => {
  const m = /<div class="foot">(.*?)<\/div><\/div><\/div><\/div>/s.exec(page(messages));
  if (!m) throw new Error("no bubble footer in the rendered page");
  return m[1]!;
};

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
  it("collapses many hosts to a count and keeps every id in the document", () => {
    const foot = footer([
      entry({ quoted: true, source: `unspooled from ${H.map((h) => `msg ${h}`).join(", ")}` }),
    ]);
    expect(foot).toContain("unspooled from 7 msgs");
    // collapsed, not dropped: every id is in the document, behind a closed
    // <details>. Dropping them would pass a count assertion just as well.
    // a native <details>/<summary>, which is what makes it operable from the
    // keyboard and with scripting off; a div and a click handler would be neither
    expect(foot).toContain('<details class="src srcx"><summary>');
    expect(foot).not.toContain("open=");
    for (const h of H) {
      expect(foot).toContain(`href="https://mail.google.com/mail/u/0/#all/${h}"`);
      expect(foot).toContain(`msg ${h}`);
    }
  });

  it("shows one host inline, and never says '1 msgs'", () => {
    const foot = footer([entry({ quoted: true, source: `unspooled from msg ${H[0]}` })]);
    expect(foot).not.toMatch(/\b1 msgs?\b/);
    expect(foot).not.toContain("<details");
    expect(foot).toContain(
      `unspooled from <span class="sid"><a href="https://mail.google.com/mail/u/0/#all/${H[0]}"`,
    );
  });

  it("collapses from two, since two handles outrun the summary that replaces them", () => {
    const foot = footer([entry({ quoted: true, source: `unspooled from msg ${H[0]}, msg ${H[1]}` })]);
    expect(foot).toContain("unspooled from 2 msgs");
    expect(foot).toContain('<details class="src srcx">');
  });

  it("links the mailbox's own id on a direct message", () => {
    const foot = footer([entry({ source: `msg ${H[0]}`, gmailId: H[0] })]);
    expect(foot).toContain(`href="https://mail.google.com/mail/u/0/#all/${H[0]}"`);
    expect(foot).not.toContain("<details");
  });

  it("leaves an entry with no source alone", () => {
    const foot = footer([entry({})]);
    expect(foot).not.toContain('class="src');
    expect(foot).not.toContain("unspooled");
  });
});

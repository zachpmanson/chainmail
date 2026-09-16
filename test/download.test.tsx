// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Timeline } from "../src/components/Timeline";
import { MEDIA_BASE, attHref, localHref } from "../src/lib/attachments";
import { normalise } from "../src/lib/normalise";
import { attach } from "../src/client/behaviour";
import type { Entry, Timeline as Spec } from "../src/lib/spec";

/**
 * What a click does once the corpus holds the file: the chip stops going to
 * Gmail and starts going to this host, and the popover gains the one route to the
 * original bytes rather than the embedded thumbnail.
 */

const PIXEL =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR4nGMAAQAABQABDQottAAAAABJRU5ErkJggg==";

const SHA = "a".repeat(64);

const entry = (over: Partial<Entry>): Entry => ({
  date: "Mon 2 Mar 2026",
  time: "09:15",
  sender: "Ada Byron",
  org: "Loomworks",
  body: "<p>invented body</p>",
  ...over,
});

const page = (messages: Entry[], mediaBase?: string, onPull?: (extId: string) => void) =>
  renderToStaticMarkup(
    <Timeline
      spec={normalise({ title: "Loom cutover", messages } as Spec)}
      mediaBase={mediaBase}
      onPull={onPull}
    />,
  );

/** Just the attachment strip, so a panel above cannot satisfy an assertion. */
const strip = (html: string) => {
  const m = /<div class="atts">.*?<\/div>(?=<div class="foot")/s.exec(html);
  if (!m) throw new Error("no attachment strip in the rendered page");
  return m[0];
};

const storedShot = {
  name: "loom-throughput.png",
  kind: "image",
  size: "212 KB",
  gmailId: "18f0",
  blobSha: SHA,
  open: "popup" as const,
  preview: PIXEL,
  previewW: 640,
  previewH: 427,
};
const storedPdf = { name: "quote.pdf", kind: "PDF", size: "88 KB", gmailId: "18f0", blobSha: "b".repeat(64), open: "download" as const };
const storedText = { name: "readings.csv", kind: "CSV", size: "18 KB", gmailId: "18f0", blobSha: "c".repeat(64), open: "popup" as const };
/** A thumbnail from the Slack archive: the bytes are on disk but this host does not serve them. */
const archivedShot = { name: "board.png", kind: "image", size: "41 KB", link: "https://chat.example/files/F001/board.png", preview: PIXEL, previewW: 320, previewH: 200 };

describe("where an attachment opens", () => {
  it("prefers the corpus to Gmail once the bytes are here", () => {
    expect(attHref(storedShot, MEDIA_BASE)).toBe(`${MEDIA_BASE}/${SHA}`);
    expect(localHref(storedShot, MEDIA_BASE)).toBe(`${MEDIA_BASE}/${SHA}`);
  });

  it("keeps Gmail while the file is somewhere else", () => {
    // The digest without a serving base is the static export; the base without a
    // digest is a file nobody has pulled. Neither is a local link.
    expect(attHref(storedShot)).toBe("https://mail.google.com/mail/u/0/#all/18f0");
    expect(attHref({ ...storedShot, blobSha: undefined }, MEDIA_BASE)).toBe(
      "https://mail.google.com/mail/u/0/#all/18f0",
    );
    expect(localHref(storedShot, "")).toBeUndefined();
  });

  it("falls back to the source link, and to nothing at all", () => {
    expect(attHref(archivedShot, MEDIA_BASE)).toBe(archivedShot.link);
    expect(attHref({ name: "recovered.pdf" }, MEDIA_BASE)).toBeUndefined();
  });
});

describe("the chip a stored file sits on", () => {
  it("points at this host instead of Gmail", () => {
    const s = strip(page([entry({ attachments: [storedShot] })], MEDIA_BASE));
    expect(s).toContain(`href="${MEDIA_BASE}/${SHA}"`);
    expect(s).not.toContain("mail.google.com");
  });

  it("opens beside the page for a file the reader is looking at", () => {
    const s = strip(page([entry({ attachments: [storedText] })], MEDIA_BASE));
    expect(s).toContain('target="_blank"');
  });

  it("stays on the page for a file the reader is taking", () => {
    // A download must not navigate: the browser saves the file and the transcript
    // is exactly where it was. The header decides that, not this markup.
    const s = strip(page([entry({ attachments: [storedPdf] })], MEDIA_BASE));
    expect(s).toContain(`href="${MEDIA_BASE}/${storedPdf.blobSha}"`);
    expect(s).not.toContain('target="_blank"');
  });

  it("leaves the static export pointing at Gmail", () => {
    const s = strip(page([entry({ attachments: [storedShot] })]));
    expect(s).toContain("mail.google.com");
    expect(s).not.toContain(MEDIA_BASE);
    expect(s).not.toContain("data-get");
  });
});

/**
 * jsdom has no IntersectionObserver, and `attach` builds one for the scroll-spy
 * before it reaches anything here. Stubbed rather than guarded in the source:
 * every browser has had it for years, so a guard would be dead code carried to
 * suit the test environment.
 */
class NoopObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() { return []; }
}
(globalThis as { IntersectionObserver?: unknown }).IntersectionObserver = NoopObserver;

/** Mount a page into jsdom and wire the behaviour module to it. */
const mount = (messages: Entry[]) => {
  document.body.innerHTML = page(messages, MEDIA_BASE);
  const detach = attach(document);
  const chips = [...document.querySelectorAll<HTMLAnchorElement>(".att[data-pop]")];
  return {
    detach,
    chips,
    pop: () => document.querySelector<HTMLElement>(".pop"),
    save: () => document.querySelector<HTMLAnchorElement>(".popget"),
  };
};

describe("getting the original out of the popover", () => {
  it("offers the file itself, not just the thumbnail", () => {
    const m = mount([entry({ attachments: [storedShot] })]);
    m.chips[0]!.click();
    const save = m.save()!;
    expect(save.hidden).toBe(false);
    expect(save.getAttribute("href")).toBe(`${MEDIA_BASE}/${SHA}`);
    // A picture is served inline, so `download` is what makes this a save rather
    // than another view of it. With no value, the name still comes from the
    // Content-Disposition header.
    expect(save.hasAttribute("download")).toBe(true);
    expect(save.textContent).toBe("save");
    m.detach();
  });

  it("hides it when the bytes are not this host's to serve", () => {
    // An archived thumbnail is on disk but lives in the uploads root, not the
    // corpus: its chip links to the archive, and calling that a download would be
    // a lie. Only the corpus has an endpoint here.
    const m = mount([entry({ attachments: [archivedShot] })]);
    m.chips[0]!.click();
    expect(m.save()!.hidden).toBe(true);
    m.detach();
  });

  it("keeps the keyboard trap working with one control and with two", () => {
    const m = mount([entry({ attachments: [storedShot] })]);
    m.chips[0]!.click();
    const pop = m.pop()!;
    const close = pop.querySelector<HTMLElement>(".popx")!;
    const save = m.save()!;
    const tab = (shiftKey = false) => {
      const ev = new KeyboardEvent("keydown", { key: "Tab", shiftKey, bubbles: true, cancelable: true });
      pop.dispatchEvent(ev);
      return ev;
    };
    // Close takes the keyboard on open; save is the last stop before it, so the
    // trap has to wrap rather than stay put now that there are two.
    expect(document.activeElement).toBe(close);
    expect(tab().defaultPrevented).toBe(true);
    expect(document.activeElement).toBe(save);
    // From the middle the browser's own Tab is right: save and Close are adjacent
    // in the popover, and the page behind is inert.
    expect(tab().defaultPrevented).toBe(false);
    expect(tab(true).defaultPrevented).toBe(true);
    expect(document.activeElement).toBe(close);
    m.detach();
  });
});

describe("a file the corpus has refused", () => {
  /** A message whose one file was declined: the reason is recorded, not the answer. */
  const skipped = { name: "walkthrough.mp4", kind: "MP4", size: "48 MB", gmailId: "18f0", skip: "too_large" } as const;
  const extId = "mail:<roof@loomworks.example>";
  const offer = (messages: Entry[]) => page([{ ...messages[0]!, extId }], MEDIA_BASE, () => {});

  it("says why on the chip, where the reader is already looking", () => {
    const s = strip(page([entry({ attachments: [skipped] })], MEDIA_BASE));
    expect(s).toContain('title="not stored: larger than the fetch cap"');
    // Still a link to the source: the file exists, it is just not ours to hold.
    expect(s).toContain("mail.google.com");
  });

  it("stops offering to fetch a message that has nothing left to ask for", () => {
    // The wart this closes: a declined file used to keep the button alive, and
    // pressing it answered "wanted 0" every time.
    const html = offer([entry({ attachments: [skipped] })]);
    expect(html).not.toContain("attget");
  });

  it("still offers while another file on the message is waiting", () => {
    const html = offer([
      entry({ attachments: [skipped, { name: "quote.pdf", kind: "PDF", size: "88 KB", gmailId: "18f0" }] }),
    ]);
    expect(html).toContain("attget");
  });

  it("keeps the offer for a file nobody has decided about", () => {
    // Absent reason, absent bytes: the ordinary "not pulled yet" state, which is
    // exactly what the button is for.
    const html = offer([entry({ attachments: [{ name: "quote.pdf", kind: "PDF", size: "88 KB", gmailId: "18f0" }] })]);
    expect(html).toContain("attget");
  });
});

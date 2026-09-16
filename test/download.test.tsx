// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
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
  const m = /<div class="atts">.*?<\/div>(?=<\/div><\/div><\/div>)/s.exec(html);
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
  view: "image" as const,
  preview: PIXEL,
  previewW: 640,
  previewH: 427,
};
const storedPdf = { name: "quote.pdf", kind: "PDF", size: "88 KB", gmailId: "18f0", blobSha: "b".repeat(64), open: "download" as const, view: "pdf" as const };
const storedText = { name: "readings.csv", kind: "CSV", size: "18 KB", gmailId: "18f0", blobSha: "c".repeat(64), open: "popup" as const, view: "text" as const };
/** A zip: bytes of ours, a download on click, and nothing that can be shown. */
const storedZip = { name: "archive.zip", kind: "ZIP", size: "2.1 MB", gmailId: "18f0", blobSha: "e".repeat(64), open: "download" as const };
/** A small picture: the bytes are here, but the builder embedded no preview. */
const BARE = "d".repeat(64);
const storedSmall = { name: "image001.png", kind: "image", size: "17 KB", gmailId: "18f0", blobSha: BARE, open: "popup" as const, view: "image" as const };
/** A small picture as a page saved before `view` existed would carry it. */
const storedSmallOld = { name: "image003.png", kind: "image", size: "9.4 KB", gmailId: "18f0", blobSha: "f".repeat(64), open: "popup" as const };
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

/** jsdom has no blob URLs either, and a framed document is one. */
const revoked = vi.fn();
Object.assign(URL, { createObjectURL: () => "blob:opened", revokeObjectURL: revoked });

/**
 * A response, without the network. Only the parts the window uses: whether it
 * worked, the text, and the bytes it turns into a blob.
 */
const reply = (body: string, status = 200) => ({
  ok: status < 400,
  status,
  text: async () => body,
  blob: async () => new Blob([body]),
}) as unknown as Response;

const fetched = vi.fn<(url: string) => Promise<Response>>();

/** Mount a page into jsdom and wire the behaviour module to it. */
const mount = (messages: Entry[], fetcher?: (url: string) => Promise<Response>) => {
  fetched.mockReset();
  fetched.mockImplementation(fetcher ?? (() => Promise.reject(new Error("no file here"))));
  vi.stubGlobal("fetch", fetched);
  document.body.innerHTML = page(messages, MEDIA_BASE);
  const detach = attach(document);
  const chips = [...document.querySelectorAll<HTMLAnchorElement>(".att[data-pop]")];
  return {
    detach: () => { detach(); vi.unstubAllGlobals(); },
    chips,
    fetched,
    pop: () => document.querySelector<HTMLElement>(".pop"),
    shot: () => document.querySelector<HTMLImageElement>(".popimg"),
    text: () => document.querySelector<HTMLElement>(".poptext"),
    frame: () => document.querySelector<HTMLIFrameElement>(".popframe"),
    cap: () => document.querySelector<HTMLElement>(".popcap"),
    note: () => document.querySelector<HTMLElement>(".popnote"),
    save: () => document.querySelector<HTMLAnchorElement>(".popget"),
  };
};

describe("opening a file over the page", () => {
  it("pops up a stored picture that has no thumbnail", () => {
    // The case this was built for: a small screenshot is bytes with no preview —
    // the builder embeds one only above a size floor — so the chip had nothing to
    // arm it and the click left the page for a bare image in a tab.
    const s = strip(page([entry({ attachments: [storedSmall] })], MEDIA_BASE));
    expect(s).toContain('class="att haspop"');
    expect(s).not.toContain("athumb");
    expect(s).toContain(`data-pop="${storedSmall.name}"`);
    expect(s).toContain('data-view="image"');

    const m = mount([entry({ attachments: [storedSmall] })]);
    m.chips[0]!.click();
    expect(m.pop()!.hidden).toBe(false);
    expect(m.shot()!.getAttribute("src")).toBe(`${MEDIA_BASE}/${BARE}`);
    // The bytes are this host's, so the window carries the route to the original
    // as well — which for a chip with no thumbnail is the only thing showing it.
    expect(m.save()!.hidden).toBe(false);
    m.detach();
  });

  it("shows the original rather than the embedded preview", () => {
    // The preview is 640 pixels on its long edge: enlarged to fill a screen it is
    // a blur, and the whole reason for pulling the bytes is that the real picture
    // is now here.
    const m = mount([entry({ attachments: [storedShot] })]);
    m.chips[0]!.click();
    expect(m.shot()!.getAttribute("src")).toBe(`${MEDIA_BASE}/${SHA}`);
    expect(m.shot()!.getAttribute("src")).not.toBe(PIXEL);
    m.detach();
  });

  it("falls back to the preview when the bytes are no longer served", () => {
    // A saved page outlives the bytes it points at: the corpus prunes what
    // nothing references, and a 404 must not leave the window empty-handed.
    const m = mount([entry({ attachments: [storedShot] })]);
    m.chips[0]!.click();
    const shot = m.shot()!;
    shot.dispatchEvent(new Event("error"));
    expect(shot.getAttribute("src")).toBe(PIXEL);
    m.detach();
  });

  it("keeps showing the thumbnail for a picture this host does not serve", () => {
    const m = mount([entry({ attachments: [archivedShot] })]);
    m.chips[0]!.click();
    expect(m.shot()!.getAttribute("src")).toBe(PIXEL);
    expect(m.save()!.hidden).toBe(true);
    m.detach();
  });

  it("still enlarges a picture on a page saved before views had names", () => {
    // The saved spec is a derivation, so a page written before `view` existed has
    // no such field — and the bytes it points at are just as much a picture.
    const s = strip(page([entry({ attachments: [storedSmallOld] })], MEDIA_BASE));
    expect(s).toContain('class="att haspop"');
    expect(s).toContain('data-view="image"');
  });

  it("leaves a file with nothing to show alone", () => {
    // A zip can only be taken. The chip keeps the link it always had, and the
    // click is not intercepted.
    const s = strip(page([entry({ attachments: [storedZip] })], MEDIA_BASE));
    expect(s).not.toContain("data-pop");
    expect(s).not.toContain("haspop");
  });

  it("frames a PDF, which downloads on click and still shows", () => {
    // The one file where `open` and `view` deliberately disagree: a click hands
    // the bytes over as a file (`Content-Disposition: attachment`), and the window
    // still frames it in the browser's own viewer.
    const s = strip(page([entry({ attachments: [storedPdf] })], MEDIA_BASE));
    expect(s).toContain('class="att haspop"');
    expect(s).toContain('data-view="pdf"');
    expect(s).not.toContain('target="_blank"');
  });
});

describe("a text file in the window", () => {
  it("reads the bytes in rather than framing them", () => {
    const m = mount([entry({ attachments: [storedText] })], () => Promise.resolve(reply("shed,readings\nNova,41.2\n")));
    m.chips[0]!.click();
    return vi.waitFor(() => {
      expect(m.fetched).toHaveBeenCalledWith(`${MEDIA_BASE}/${storedText.blobSha}`);
      expect(m.text()!.hidden).toBe(false);
      expect(m.text()!.textContent).toBe("shed,readings\nNova,41.2\n");
      // Nothing else is on: one element at a time, and the picture is not it.
      expect(m.shot()!.hidden).toBe(true);
      expect(m.frame()!.hidden).toBe(true);
      expect(m.note()!.textContent).toBe("");
      expect(m.save()!.getAttribute("href")).toBe(`${MEDIA_BASE}/${storedText.blobSha}`);
      m.detach();
    });
  });

  it("puts no markup in the page, whatever the file contains", () => {
    // The bytes are a sender's, and a `<pre>` with textContent cannot become
    // markup. Framing a text file would have been the other way to show it, and
    // this is why it is not done.
    const evil = "<img src=x onerror=alert(1)>\n";
    const m = mount([entry({ attachments: [storedText] })], () => Promise.resolve(reply(evil)));
    m.chips[0]!.click();
    return vi.waitFor(() => {
      expect(m.text()!.textContent).toBe(evil);
      expect(m.text()!.querySelector("img")).toBeNull();
      expect(m.pop()!.querySelectorAll("img").length).toBe(1); // the empty shot only
      m.detach();
    });
  });

  it("stops at the cap and says so, with the file a click away", () => {
    const long = "x".repeat(300 * 1024);
    const m = mount([entry({ attachments: [storedText] })], () => Promise.resolve(reply(long)));
    m.chips[0]!.click();
    return vi.waitFor(() => {
      expect(m.text()!.textContent!.length).toBe(256 * 1024);
      expect(m.note()!.textContent).toBe("truncated — save it for the rest");
      expect(m.save()!.hidden).toBe(false);
      m.detach();
    });
  });

  it("says so when the bytes cannot be read", () => {
    const m = mount([entry({ attachments: [storedText] })], () => Promise.resolve(reply("", 404)));
    m.chips[0]!.click();
    return vi.waitFor(() => {
      expect(m.note()!.textContent).toBe("could not be read here");
      // Still a file, and still this host's: the save control does not depend on
      // the window being able to show it.
      expect(m.save()!.hidden).toBe(false);
      m.detach();
    });
  });
});

describe("a PDF in the window", () => {
  it("frames the bytes as a blob of its own type", () => {
    // A frame handed the served URL renders nothing — the file is served as an
    // attachment — so the page makes its own blob, with a type it chose.
    const m = mount([entry({ attachments: [storedPdf] })], () => Promise.resolve(reply("%PDF-1.7\n")));
    m.chips[0]!.click();
    return vi.waitFor(() => {
      expect(m.fetched).toHaveBeenCalledWith(`${MEDIA_BASE}/${storedPdf.blobSha}`);
      expect(m.frame()!.hidden).toBe(false);
      expect(m.frame()!.getAttribute("src")).toBe("blob:opened");
      expect(m.frame()!.getAttribute("title")).toBe(storedPdf.name);
      expect(m.shot()!.hidden).toBe(true);
      expect(m.text()!.hidden).toBe(true);
      m.detach();
    });
  });

  it("lets the blob go when the window closes", () => {
    const m = mount([entry({ attachments: [storedPdf] })], () => Promise.resolve(reply("%PDF-1.7\n")));
    m.chips[0]!.click();
    return vi.waitFor(() => {
      expect(m.frame()!.hidden).toBe(false);
      revoked.mockClear();
      (m.pop()!.querySelector(".popx") as HTMLElement).click();
      expect(revoked).toHaveBeenCalledWith("blob:opened");
      expect(m.frame()!.hasAttribute("src")).toBe(false);
      m.detach();
    });
  });

  it("drops a fetch that lands after the reader has gone", () => {
    // Bytes arriving late must not paint over a window the reader closed, or the
    // next file they opened.
    let resolve: (r: Response) => void = () => {};
    const pending = new Promise<Response>((r) => { resolve = r; });
    const m = mount([entry({ attachments: [storedPdf] })], () => pending);
    m.chips[0]!.click();
    (m.pop()!.querySelector(".popx") as HTMLElement).click();
    resolve(reply("%PDF-1.7\n"));
    return pending.then(() => new Promise((r) => setTimeout(r, 0))).then(() => {
      expect(m.frame()!.hidden).toBe(true);
      expect(m.frame()!.hasAttribute("src")).toBe(false);
      m.detach();
    });
  });
});

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

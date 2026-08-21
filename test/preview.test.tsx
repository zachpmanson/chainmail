// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Timeline } from "../src/components/Timeline";
import { normalise } from "../src/lib/normalise";
import { attach } from "../src/client/behaviour";
import type { Entry, Timeline as Spec } from "../src/lib/spec";

/**
 * A one-pixel PNG, base64. Stands in for a thumbnail: every assertion here is
 * about what the page does with a preview, never about the picture in it, so
 * the smallest valid image is the honest fixture.
 */
const PIXEL =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR4nGMAAQAABQABDQottAAAAABJRU5ErkJggg==";

const entry = (over: Partial<Entry>): Entry => ({
  date: "Mon 2 Mar 2026",
  time: "09:15",
  sender: "Ada Byron",
  org: "Loomworks",
  body: "<p>invented body</p>",
  ...over,
});

const page = (messages: Entry[]) =>
  renderToStaticMarkup(
    <Timeline spec={normalise({ title: "Loom cutover", messages } as Spec)} />,
  );

const shot = {
  name: "loom-throughput.png",
  kind: "image",
  size: "212 KB",
  link: "https://chat.example/files/F001/loom-throughput.png",
  preview: PIXEL,
  previewW: 640,
  previewH: 427,
};
const logo = { name: "image001.png", kind: "image", size: "4.1 KB", link: "https://chat.example/files/F009/image001.png" };
const sheet = { name: "readings.csv", kind: "CSV", size: "18 KB", link: "https://chat.example/files/F010/readings.csv" };

/** Just the attachment strip, so a panel above cannot satisfy an assertion. */
const strip = (html: string) => {
  const m = /<div class="atts">.*?<\/div>(?=<div class="foot")/s.exec(html);
  if (!m) throw new Error("no attachment strip in the rendered page");
  return m[0];
};

describe("which attachments get a preview", () => {
  it("shows a thumbnail on an image that deserves one", () => {
    const s = strip(page([entry({ attachments: [shot] })]));
    expect(s).toContain('class="att haspop"');
    expect(s).toContain('class="athumb"');
    expect(s).toContain('width="640"');
    expect(s).toContain('height="427"');
    expect(s).toContain(shot.name);
  });

  it("leaves a signature logo as a plain chip", () => {
    // The decision was made where the bytes were, so a filtered attachment
    // simply arrives with no preview field. The page must not invent one.
    const s = strip(page([entry({ attachments: [logo] })]));
    expect(s).not.toContain("athumb");
    expect(s).not.toContain("haspop");
    expect(s).toContain(logo.name);
  });

  it("leaves a non-image attachment exactly as it was", () => {
    const s = strip(page([entry({ attachments: [sheet] })]));
    expect(s).not.toContain("athumb");
    expect(s).not.toContain("haspop");
    expect(s).toContain(sheet.name);
    expect(s).toContain("CSV");
  });

  it("gives a Slack attachment the link the corpus recorded for it", () => {
    // Before there was a link field, anything not from Gmail rendered as an
    // unopenable label even though its permalink was sitting in the corpus.
    const s = strip(page([entry({ attachments: [shot] })]));
    expect(s).toContain(`href="${shot.link}"`);
    expect(s).not.toContain("nolink");
  });

  it("keeps a chip with nowhere to go unopenable rather than offering a dead control", () => {
    const { link, ...noLink } = shot;
    const s = strip(page([entry({ attachments: [noLink] })]));
    expect(s).toContain("athumb"); // the picture is still worth showing
    expect(s).toContain("nolink");
    expect(s).not.toContain("haspop"); // but nothing invites a click
    expect(s).not.toContain("data-pop");
  });
});

describe("the page fetches nothing when it loads", () => {
  it("embeds every thumbnail rather than pointing at one", () => {
    const html = page([entry({ attachments: [shot, logo, sheet] })]);
    const srcs = [...html.matchAll(/<img[^>]*\bsrc="([^"]*)"/g)].map((m) => m[1]!);
    expect(srcs.length).toBeGreaterThan(0);
    for (const src of srcs) expect(src.startsWith("data:")).toBe(true);
  });

  it("does not turn a body the sender wrote into a fetch it did not already make", () => {
    // A remote picture in a body is the sender's markup and renders as it always
    // has; the popover only intercepts the click. This asserts the count is
    // unchanged, not that it is zero — see the note on the render.
    const body = '<p>see below</p><img src="https://cdn.example/chart.png" width="200">';
    const html = page([entry({ body })]);
    const remote = [...html.matchAll(/<img[^>]*\bsrc="(https?:[^"]*)"/g)];
    expect(remote).toHaveLength(1);
    expect(remote[0]![1]).toBe("https://cdn.example/chart.png");
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

/** Mount a rendered page into jsdom and wire the behaviour module to it. */
const mount = (messages: Entry[]) => {
  document.body.innerHTML = page(messages);
  const detach = attach(document);
  const chip = document.querySelector<HTMLAnchorElement>(".att[data-pop]");
  return {
    detach,
    chip,
    pop: () => document.querySelector<HTMLElement>(".pop"),
    open: () => {
      const p = document.querySelector<HTMLElement>(".pop");
      return !!p && !p.hidden;
    },
  };
};

describe("enlarging a preview", () => {
  it("opens on the chip, and shows the picture the chip showed", () => {
    const m = mount([entry({ attachments: [shot] })]);
    expect(m.pop()).toBeNull(); // nothing built until it is wanted
    m.chip!.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    expect(m.open()).toBe(true);
    expect(m.pop()!.querySelector<HTMLImageElement>(".popimg")!.getAttribute("src")).toBe(PIXEL);
    expect(m.pop()!.querySelector(".popcap")!.textContent).toBe(shot.name);
    m.detach();
  });

  it("announces itself as a dialog and takes the keyboard on open", () => {
    const m = mount([entry({ attachments: [shot] })]);
    expect(m.chip!.getAttribute("aria-haspopup")).toBe("dialog");
    m.chip!.click();
    const pop = m.pop()!;
    expect(pop.getAttribute("role")).toBe("dialog");
    expect(pop.getAttribute("aria-modal")).toBe("true");
    expect(document.activeElement).toBe(pop.querySelector(".popx"));
    m.detach();
  });

  it("closes on escape and hands the keyboard back to the chip", () => {
    const m = mount([entry({ attachments: [shot] })]);
    m.chip!.click();
    m.pop()!.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    expect(m.open()).toBe(false);
    // A reader who enlarged a picture half way down a transcript must come back
    // to where they were, not to the top of the document.
    expect(document.activeElement).toBe(m.chip);
    m.detach();
  });

  it("keeps tab inside the popover while it is open", () => {
    const m = mount([entry({ attachments: [shot] })]);
    m.chip!.click();
    const pop = m.pop()!;
    const x = pop.querySelector<HTMLButtonElement>(".popx")!;
    for (const shiftKey of [false, true]) {
      x.focus();
      const ev = new KeyboardEvent("keydown", { key: "Tab", shiftKey, bubbles: true, cancelable: true });
      pop.dispatchEvent(ev);
      expect(ev.defaultPrevented).toBe(true);
      expect(pop.contains(document.activeElement)).toBe(true);
    }
    m.detach();
  });

  it("closes on the backdrop but not on the picture", () => {
    const m = mount([entry({ attachments: [shot] })]);
    m.chip!.click();
    const pop = m.pop()!;
    pop.querySelector<HTMLElement>(".popimg")!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(m.open()).toBe(true);
    pop.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(m.open()).toBe(false);
    m.detach();
  });

  it("leaves a modified click to the link underneath", () => {
    const m = mount([entry({ attachments: [shot] })]);
    const ev = new MouseEvent("click", { bubbles: true, cancelable: true, metaKey: true });
    m.chip!.dispatchEvent(ev);
    // Not intercepted: the reader asked for a new tab, and the chip is a link.
    expect(ev.defaultPrevented).toBe(false);
    expect(m.open()).toBe(false);
    m.detach();
  });

  it("stops the transcript scrolling behind an open popover", () => {
    const m = mount([entry({ attachments: [shot] })]);
    m.chip!.click();
    expect(document.body.classList.contains("popped")).toBe(true);
    m.pop()!.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    expect(document.body.classList.contains("popped")).toBe(false);
    m.detach();
  });

  it("wires nothing on a page with no previews", () => {
    const m = mount([entry({ attachments: [logo, sheet] })]);
    expect(m.chip).toBeNull();
    expect(m.pop()).toBeNull();
    m.detach();
  });
});

describe("enlarging a picture the sender put in the body", () => {
  // On a mail-only page this is the whole feature: mail attachments have no
  // archived bytes, so a body picture is the only thing there is to enlarge.
  const bodyWith = (img: string) => entry({ body: `<p>see below</p>${img}` });
  const wire = (img: string) => {
    document.body.innerHTML = page([bodyWith(img)]);
    const detach = attach(document);
    const el = document.querySelector<HTMLImageElement>(".bd img")!;
    return { detach, el, open: () => { const p = document.querySelector<HTMLElement>(".pop"); return !!p && !p.hidden; } };
  };

  it("enlarges a screenshot on click and on the keyboard", () => {
    const w = wire('<img src="https://cdn.example/chart.png" width="900" height="600">');
    expect(w.el.getAttribute("aria-haspopup")).toBe("dialog");
    expect(w.el.tabIndex).toBe(0); // reachable without a pointer
    w.el.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    expect(w.open()).toBe(true);
    w.detach();
  });

  it.each([
    ["a tracking pixel", 98, 99],
    ["a signature banner", 540, 70],
    ["a wordmark", 120, 22],
  ])("leaves %s alone", (_why, width, height) => {
    // Every one of these shapes was measured on a real mail page. A page that
    // offered to enlarge them would put a popover on eight logos per message.
    const w = wire(`<img src="https://cdn.example/logo.png" width="${width}" height="${height}">`);
    expect(w.el.hasAttribute("aria-haspopup")).toBe(false);
    w.el.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    expect(w.open()).toBe(false);
    w.detach();
  });

  it("puts the tab stop on the link when the picture is inside one", () => {
    // Two tab stops for one picture would mean tabbing through the transcript
    // stopping twice in the same place.
    document.body.innerHTML = page([
      bodyWith('<a href="https://example.test/full"><img src="https://cdn.example/c.png" width="900" height="600"></a>'),
    ]);
    const detach = attach(document);
    const img = document.querySelector<HTMLImageElement>(".bd img")!;
    const link = img.closest("a")!;
    expect(img.hasAttribute("tabindex")).toBe(false);
    expect(link.getAttribute("aria-haspopup")).toBe("dialog");
    link.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    const pop = document.querySelector<HTMLElement>(".pop")!;
    expect(pop.hidden).toBe(false);
    pop.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    expect(document.activeElement).toBe(link);
    detach();
  });
});

describe("a body picture whose size is not knowable yet", () => {
  // 8 of 29 pictures on a real mail page declare no width or height. Arming them
  // on sight would put a tab stop on every one, before anything knows whether
  // they are screenshots or wordmarks.
  const wire = (attrs: string) => {
    document.body.innerHTML = page([entry({ body: `<img src="https://cdn.example/x.png"${attrs}>` })]);
    const detach = attach(document);
    return { detach, el: document.querySelector<HTMLImageElement>(".bd img")! };
  };
  /** jsdom never loads or lays out, so natural size is set by hand, as a load would. */
  const loadsAs = (img: HTMLImageElement, w: number, h: number) => {
    Object.defineProperty(img, "naturalWidth", { value: w, configurable: true });
    Object.defineProperty(img, "naturalHeight", { value: h, configurable: true });
    img.dispatchEvent(new Event("load"));
  };

  it("stays unarmed until it loads", () => {
    const w = wire("");
    expect(w.el.hasAttribute("aria-haspopup")).toBe(false);
    expect(w.el.hasAttribute("tabindex")).toBe(false);
    w.detach();
  });

  it("arms once it turns out to be a screenshot", () => {
    const w = wire("");
    loadsAs(w.el, 1400, 900);
    expect(w.el.getAttribute("aria-haspopup")).toBe("dialog");
    w.el.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    expect(document.querySelector<HTMLElement>(".pop")!.hidden).toBe(false);
    w.detach();
  });

  it("stays unarmed once it turns out to be an icon", () => {
    const w = wire("");
    loadsAs(w.el, 48, 48);
    expect(w.el.hasAttribute("aria-haspopup")).toBe(false);
    w.detach();
  });

  it("never armed a picture with no source at all", () => {
    document.body.innerHTML = page([entry({ body: '<img alt="stripped">' })]);
    const detach = attach(document);
    expect(document.querySelector(".bd img")!.hasAttribute("aria-haspopup")).toBe(false);
    detach();
  });
});

describe("the transcript behind an open popover", () => {
  it("is made inert, and released again on close", () => {
    document.body.innerHTML = page([entry({ attachments: [shot] })]);
    const detach = attach(document);
    const wrap = document.querySelector(".wrap")!;
    const chip = document.querySelector<HTMLAnchorElement>(".att[data-pop]")!;
    chip.click();
    // The overlay stops a pointer; this is what stops a screen reader reading
    // the transcript underneath as though it were still the page.
    expect(wrap.hasAttribute("inert")).toBe(true);
    document.querySelector(".pop")!.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    expect(wrap.hasAttribute("inert")).toBe(false);
    detach();
  });
});

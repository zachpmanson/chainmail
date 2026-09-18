// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Message, type MessageProps } from "../src/components/Message";
import { ApiError } from "../src/lib/api";
import { fetchOriginal, dropSchemeVariants, mountOriginal } from "../src/lib/original";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

/**
 * The reader's second reading of a message: the html its sender wrote, mounted in
 * a shadow root of its own, swapped in for the transcript's stripped rendering of
 * the same body.
 *
 * What is asserted here is the swap and who may ask for it, because both halves
 * have a way of looking finished while being wrong: a control drawn where there is
 * nowhere to fetch from is a button that can only fail, and an original drawn
 * *beside* the rendered body (rather than in place of it) doubles the height of a
 * thread to hold a comparison the reader has already made.
 *
 * Every test names its own ext id, because lib/original holds what it fetched for
 * the session — the same caching a reader gets, and the same reason two tests
 * sharing an id would be observing each other. For the same reason every test that
 * presses the control names its own sender: the switch behind it is per sender and
 * is held for the session (and in localStorage), so two tests sharing an address
 * would be watching one switch between them.
 *
 * Note for whoever writes the next one: the control lives inside the bubble's
 * receipt, pressed or not. jsdom does not hide a closed <details>, so a role query
 * finds it here whether the receipt is open or not — these tests are about the
 * swap, not about it being on screen. The test that does assert where it is drawn
 * is the placement test at the end.
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

/** The sender's own html, as the server would serve it: a stylesheet the
 *  transcript's rendering cannot keep, and the class that addresses it. */
const sent = "<style>:host{background:#eef}</style><p class=\"card\">Booking confirmed</p>";

const draw = (over: Partial<MessageProps> = {}) => render(<Message {...bubble(over)} />);

const control = () =>
  screen.queryByRole("button", { name: /read this sender|back to the page|fetching the sender/i });

describe("a message whose own html the corpus holds", () => {
  it("swaps the rendered body for the sender's, and back", async () => {
    const load = vi.fn(async () => sent);
    const { container } = draw({ original: { extId: "mail:<orig-swap@loomworks.example>", load } });

    // Before asking: the transcript's rendering, and a control that is not pressed.
    expect(container.querySelector(".bd")!.innerHTML).toBe("<p>invented body</p>");
    expect(control()!.getAttribute("aria-pressed")).toBe("false");

    fireEvent.click(control()!);
    // The body is replaced, not added to: one bubble, one body, two renderings.
    await waitFor(() => expect(container.querySelector(".bdo")).not.toBeNull());
    expect(container.querySelectorAll(".bd").length).toBe(1);
    expect(container.querySelector(".bd")!.innerHTML).not.toContain("invented body");
    expect(control()!.getAttribute("aria-pressed")).toBe("true");

    // The mount is a shadow root, which is the whole of what keeps a sender's
    // stylesheet off this page and this page's styles off theirs.
    const host = container.querySelector(".bdo") as HTMLElement;
    expect(host.shadowRoot).not.toBeNull();
    expect(host.shadowRoot!.innerHTML).toBe(sent);

    // And the way back is the same control, pressed. The host itself has to go:
    // a shadow root cannot be detached from the element it was created on, so an
    // element React keeps across the swap keeps *drawing* the sender's html
    // whatever the light DOM says — the class coming off is not enough.
    fireEvent.click(control()!);
    expect(container.querySelector(".bdo")).toBeNull();
    expect(container.querySelector(".bd")!.innerHTML).toBe("<p>invented body</p>");
    expect(host.shadowRoot === null || !host.isConnected).toBe(true);
    expect([...container.querySelectorAll(".bd")].some((el) => el.shadowRoot)).toBe(false);
    expect(control()!.getAttribute("aria-pressed")).toBe("false");
  });

  it("asks for the part once, however many times the reader flips", async () => {
    // Comparing the two renderings is what this control is for, and a round trip
    // per flip would make the comparison the slow part of reading the message.
    const load = vi.fn(async () => sent);
    const { container } = draw({ original: { extId: "mail:<orig-once@loomworks.example>", load } });
    for (const _ of [1, 2, 3]) {
      fireEvent.click(control()!);
      await waitFor(() => expect(container.querySelector(".bd")!.className).toMatch(/bdo|(?!)/));
      fireEvent.click(control()!);
    }
    // The second press of a pair is the way back (no fetch), and the third is the
    // one that could have re-asked. It did not.
    expect(load).toHaveBeenCalledTimes(1);
    expect(load).toHaveBeenCalledWith("mail:<orig-once@loomworks.example>");
  });

  it("keeps the rendered body on screen while the part is on its way", async () => {
    // A blank bubble would be a worse answer than the wrong one the reader is
    // already looking at.
    let arrive: (html: string) => void = () => {};
    const load = () => new Promise<string>((resolve) => (arrive = resolve));
    const { container } = draw({ original: { extId: "mail:<orig-wait@loomworks.example>", load } });
    fireEvent.click(control()!);
    expect(container.querySelector(".bd")!.innerHTML).toBe("<p>invented body</p>");
    expect(control()!.hasAttribute("disabled")).toBe(true);
    arrive(sent);
    await waitFor(() => expect(container.querySelector(".bdo")).not.toBeNull());
  });

  it("says so where the reader asked, when the corpus has nothing to show", async () => {
    // The corpus's own sentence says which nothing it found, so it rides the note's
    // hover rather than being replaced with a word here. The switch stays where the
    // reader put it and stays pressable: it is the sender's switch and not this
    // message's, so a message with no part of its own is still how the reader stops
    // reading the rest of that sender's mail this way — which is the thing a control
    // that vanished here could not do.
    const why = "mail:<orig-none@loomworks.example> carries no text/html part of its own";
    const load = vi.fn(async () => {
      throw new ApiError(404, why);
    });
    const { container } = draw({ original: { extId: "mail:<orig-none@loomworks.example>", load } });
    fireEvent.click(control()!);
    await waitFor(() => expect(container.querySelector(".origwhy")).not.toBeNull());
    expect(container.querySelector(".origwhy")!.getAttribute("title")).toBe(why);
    expect(control()!.getAttribute("aria-pressed")).toBe("true");
    // What was on screen is still on screen: the ask changed nothing but the note.
    expect(container.querySelector(".bd")!.innerHTML).toBe("<p>invented body</p>");
    // And pressing it again is still the way back, whatever this one message
    // could show.
    fireEvent.click(control()!);
    expect(control()!.getAttribute("aria-pressed")).toBe("false");
  });

  it("draws no control where the caller has nowhere to fetch from", () => {
    // A built page, a static export, a message that arrived as plain text: the
    // prop is the flag, so its absence is the answer for all three.
    const { container } = draw();
    expect(container.querySelector(".origbtn")).toBeNull();
    expect(container.querySelector(".hdetend")).toBeNull();
  });

  it("puts the control in the receipt, beside the copy button", () => {
    // The reader who needs this is the one who has noticed the rendering is wrong,
    // which means going to look at the message — and this is where looking at a
    // message lives. So: in the details body rather than the summary (where it
    // would be a control on every bubble in the thread), in the bubble's header
    // rather than its body (where it would sit on top of the content it exists to
    // show), and next to the clip rather than alone at the other end of the line.
    const { container } = draw({
      original: { extId: "mail:<orig-place@loomworks.example>", load: async () => sent },
      copyJson: { id: "m1" },
    });
    const hdr = container.querySelector("details.hdr")!;
    expect(hdr.querySelector("summary .origbtn, summary .copyjson")).toBeNull();
    const det = hdr.querySelector(".hdet")!;
    expect(det.querySelector(".bub .origbtn")).toBeNull();
    const end = det.querySelector(".hdetend")!;
    // The style switch is a glyph, not a word: the sender's own markup, in the same
    // 18px box as the copy control beside it (the shape itself is the stylesheet's
    // test, in message.test.tsx). Its aria-label is where the word lives now.
    const sw = end.querySelector(".origbtn") as HTMLElement;
    expect(sw.textContent).toBe("");
    expect(sw.querySelector("svg")).not.toBeNull();
    expect(sw.getAttribute("aria-label")).toMatch(/^Read this sender's mail/);
    expect(sw.getAttribute("aria-pressed")).toBe("false");
    expect(end.querySelector(".copyjson")).not.toBeNull();
    // The clip is the last thing on the line and the swap sits immediately before
    // it: both in the one group that is pushed to the right edge, so the pair
    // stays together however the receipt's fields above them wrap.
    expect(end.lastElementChild!.className).toBe("copyjson");
  });

  it("turns the same ↻ the rest of the app turns while it is being fetched", async () => {
    // The button states the wait in its glyph rather than in a word: "loading…" was a
    // second width in the one place the reader is watching, and it moved the receipt's
    // line under the hand that had just pressed it. The glyph it turns is the one the
    // nav's refresh and the attachment chips turn (see .navrefresh .spinner).
    let release: (html: string) => void = () => {};
    const load = vi.fn(
      () => new Promise<string>((res) => { release = res; }),
    );
    const { container } = draw({
      original: { extId: "mail:<orig-asking@loomworks.example>", load },
      fromEmail: "asking@loomworks.example",
    });
    fireEvent.click(control()!);
    const busy = await waitFor(() => {
      const b = container.querySelector(".origbtn.busy") as HTMLButtonElement;
      expect(b).toBeTruthy();
      return b;
    });
    expect(busy.disabled).toBe(true);
    expect(busy.querySelector(".spinner")).not.toBeNull();
    expect(busy.querySelector("svg")).toBeNull();
    expect(busy.getAttribute("aria-label")).toMatch(/^Fetching the sender's own/);
    release(sent);
    await waitFor(() => expect(container.querySelector(".bdo")).not.toBeNull());
    const after = container.querySelector(".origbtn") as HTMLElement;
    expect(after.className).toBe("origbtn");
    expect(after.querySelector(".spinner")).toBeNull();
    expect(after.querySelector("svg")).not.toBeNull();
  });

  it("keeps the control in the receipt once the body has been swapped", async () => {
    // The control does not move when it is pressed. The receipt is where a reader
    // inspects a message, the bubble's own line is where they read one, and the
    // pressed state is carried by the colour rather than by a second home — so a
    // reader who has shut the receipt can see from the line that this message is
    // being read the other way, and opens the same receipt to turn it back.
    const load = vi.fn(async () => sent);
    const { container } = draw({
      original: { extId: "mail:<orig-line@loomworks.example>", load },
      fromEmail: "line@loomworks.example",
      copyJson: { id: "m1" },
    });
    fireEvent.click(control()!);
    await waitFor(() => expect(container.querySelector(".bdo")).not.toBeNull());

    const hdr = container.querySelector("details.hdr")!;
    expect(hdr.querySelector("summary .origbtn")).toBeNull();
    const on = hdr.querySelector(".hdetend .origbtn") as HTMLElement;
    expect(on.querySelector("svg")).not.toBeNull();
    expect(on.getAttribute("aria-pressed")).toBe("true");
    // And the same one glyph says the other thing now: what a press would do is the
    // label, because the shape has no room for two words and needs none.
    expect(on.getAttribute("aria-label")).toMatch(/^Back to the page's own rendering/);
    expect(hdr.querySelector(".hdetend .copyjson")).not.toBeNull();

    // And it is still the way back.
    fireEvent.click(on);
    expect(container.querySelector(".bdo")).toBeNull();
    expect(hdr.querySelector(".hdetend .origbtn")!.getAttribute("aria-pressed")).toBe("false");
  });

  it("switches one sender's whole run of mail, not the message pressed", async () => {
    // The mail this is for arrives as a series from one address — GitHub's
    // notifications, a booking system — and the reader who has decided that one of
    // them is illegible has decided it for all of them. Two bubbles from one sender
    // are mounted here, which is what a thread from a notification sender is.
    const load = vi.fn(async (extId: string) => `<p>${extId}</p>`);
    const sender = "notifications@github.example";
    const one: Partial<MessageProps> = {
      original: { extId: "mail:<gh-1@github.example>", load },
      fromEmail: sender,
    };
    const two: Partial<MessageProps> = {
      id: "m2",
      original: { extId: "mail:<gh-2@github.example>", load },
      fromEmail: sender,
    };
    const { container } = render(
      <>
        <Message {...bubble(one)} />
        <Message {...bubble(two)} />
      </>,
    );
    const bubbles = () => [...container.querySelectorAll(".bd")] as HTMLElement[];
    // Two bubbles, two controls: one switch, drawn wherever the reader is looking
    // at the mail it governs.
    const switches = () =>
      [...container.querySelectorAll(".origbtn")] as HTMLElement[];
    expect(bubbles().map((b) => b.className)).toEqual(["bd", "bd"]);

    fireEvent.click(switches()[0]!);
    await waitFor(() => expect(container.querySelectorAll(".bdo").length).toBe(2));
    // Both bubbles swapped, and each asked for its own part rather than sharing one.
    expect(load.mock.calls.map((c) => c[0])).toEqual([
      "mail:<gh-1@github.example>",
      "mail:<gh-2@github.example>",
    ]);

    // Pressing the control on either of them turns the pair back together.
    fireEvent.click(switches()[0]!);
    await waitFor(() => expect(container.querySelectorAll(".bdo").length).toBe(0));
    expect(bubbles().map((b) => b.className)).toEqual(["bd", "bd"]);
  });

  it("answers a sender's mail from the stored switch, without being asked again", async () => {
    // Held in localStorage rather than in the session: the next slurp brings more
    // mail from the same sender, and "how do I read this person" is not a question
    // to re-answer every time the corpus grows.
    const sender = "receipts@loomworks.example";
    const load = vi.fn(async () => sent);
    const first = draw({
      original: { extId: "mail:<stored-1@loomworks.example>", load },
      fromEmail: sender,
    });
    fireEvent.click(control()!);
    await waitFor(() => expect(first.container.querySelector(".bdo")).not.toBeNull());
    first.unmount();

    // A later message from the same address, mounted cold: it opens on the
    // reader's answer rather than on the default.
    const second = draw({
      id: "m2",
      original: { extId: "mail:<stored-2@loomworks.example>", load },
      fromEmail: sender,
    });
    await waitFor(() => expect(second.container.querySelector(".bdo")).not.toBeNull());
    expect(second.container.querySelector(".bd")!.innerHTML).not.toContain("invented body");
    expect(control()!.getAttribute("aria-pressed")).toBe("true");
  });
});

describe("a sender the corpus has an answer about", () => {
  /**
   * Where the answer is stored is what makes it the reader's rather than this
   * browser's: the corpus holds it against the person the sender resolved to, so
   * the reader's phone reads the same mail the same way and a merge of two
   * spellings of that person cannot lose it (see people.prefer_original). What is
   * asserted here is that the bubble is drawn from that answer and hands the press
   * back out rather than writing a second, private copy of it.
   */
  const stored = (preferOriginal: boolean) => ({
    original: { extId: "mail:<corpus-answer@loomworks.example>", load: vi.fn(async () => sent) },
    fromEmail: "corpus-answer@loomworks.example",
    person: { id: 42, preferOriginal },
    onPreferOriginal: vi.fn(),
  });

  it("opens on the corpus's answer, and hands the press back out", async () => {
    const props = stored(true);
    const { container } = draw(props);
    // On, because the corpus says so: nothing was pressed, and the sender's own
    // html is fetched because the switch was already on when the bubble mounted.
    await waitFor(() => expect(container.querySelector(".bdo")).not.toBeNull());
    expect(control()!.getAttribute("aria-pressed")).toBe("true");

    fireEvent.click(control()!);
    // Handed out rather than written here: whether the press lands in the corpus
    // is the caller's business, and the corpus is what will answer this bubble
    // next time it is read.
    expect(props.onPreferOriginal).toHaveBeenCalledWith(false);
    const held = JSON.parse(localStorage.getItem("chainmail:styled-senders") ?? "[]") as string[];
    expect(held).not.toContain("corpus-answer@loomworks.example");
  });

  it("follows the corpus when the entry is read again", async () => {
    // A built page remembers the switch in this browser and nothing else can move
    // it. Where the corpus is behind the bubble, the corpus is what moves it: the
    // write comes back, the thread is read again, and the entry that arrives says
    // the other thing.
    const { container, rerender } = draw(stored(true));
    await waitFor(() => expect(control()!.getAttribute("aria-pressed")).toBe("true"));

    rerender(<Message {...bubble(stored(false))} />);
    await waitFor(() => expect(control()!.getAttribute("aria-pressed")).toBe("false"));
    // And the body goes back to the transcript's, because the switch that put the
    // sender's own rendering there is off.
    expect(container.querySelector(".bdo")).toBeNull();
  });
});

describe("fetching and mounting one message's own html", () => {
  it("asks the entry's own route, percent-encoded, and takes the server's sentence", async () => {
    const seen: string[] = [];
    vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
      seen.push(String(input));
      return new Response(JSON.stringify({ error: "no entry with that id" }), {
        status: 404,
        headers: { "content-type": "application/json" },
      });
    });
    const missing = "mail:<orig-fetch@loomworks.example>";
    await expect(fetchOriginal(missing)).rejects.toThrow("no entry with that id");
    expect(seen[0]).toBe(`/v1/entries/${encodeURIComponent(missing)}/original`);
  });

  it("holds what arrived for the session, and never a failure", async () => {
    // A held answer spares the flip a round trip; a held failure would make one
    // bad second the answer for the rest of the session.
    let calls = 0;
    vi.stubGlobal("fetch", async () => {
      calls++;
      return calls === 1
        ? new Response(JSON.stringify({ error: "the corpus is mid-slurp" }), { status: 503 })
        : new Response(sent, { status: 200, headers: { "content-type": "text/html" } });
    });
    const id = "mail:<orig-retry@loomworks.example>";
    await expect(fetchOriginal(id)).rejects.toThrow("mid-slurp");
    await expect(fetchOriginal(id)).resolves.toBe(sent);
    // And now that it has an answer, it is not asked for again.
    await expect(fetchOriginal(id)).resolves.toBe(sent);
    expect(calls).toBe(2);
  });

  it("fills a host it has already mounted into, rather than throwing", () => {
    // attachShadow throws on a second call, and a reader flipping twice re-enters
    // a root the browser is still holding.
    const host = document.createElement("div");
    mountOriginal(host, "<p>one</p>");
    expect(() => mountOriginal(host, "<p>two</p>")).not.toThrow();
    expect(host.shadowRoot!.innerHTML).toBe("<p>two</p>");
  });
});

describe("the sender's own colour-scheme rules", () => {
  /**
   * The mail's stylesheets have to be real ones for this to mean anything, so they
   * are parsed by the document: jsdom's shadow roots have no `styleSheets` at all
   * (the mount in the last test of the describe above is given one for that
   * reason), and the walk is written to take whatever has them.
   */
  function sheetOf(css: string): CSSStyleSheet {
    const style = document.createElement("style");
    style.textContent = css;
    document.head.appendChild(style);
    return (style as HTMLStyleElement).sheet!;
  }

  it("takes the dark-scheme variant out, and keeps the rest of the mail's rules", () => {
    // The app draws a mail on white paper with black ink — the paper its sender
    // wrote it for (see the canvas in internal/spec/original.go) — while the app's
    // own dark theme is a `prefers-color-scheme` query too, and a query inside a
    // shadow root is answered by the document rather than by the host's
    // `color-scheme`. So a sender's dark variant lands on the white paper: measured
    // on a Google Calendar invitation, `#e8eaed` ink on `#fff`, which is 1.1:1 and
    // a mail nobody can read.
    const sheet = sheetOf(
      `p { margin:0 } @media (prefers-color-scheme: dark) { p { color:#e8eaed } } h1 { font-size:20px }`,
    );
    dropSchemeVariants({ styleSheets: [sheet] });
    expect(sheet.cssRules.length).toBe(2);
    expect([...sheet.cssRules].map((r) => (r as CSSStyleRule).selectorText)).toEqual(["p", "h1"]);
  });

  it("leaves the modes that are about the reader's display alone", () => {
    // A width query, a forced-colours block and a contrast query all mean what
    // they say about the display they are read on; dropping them to fix a colour
    // would be losing an accessibility mode.
    const sheet = sheetOf(
      `@media (max-width:580px) { p { font-size:12px } }
       @media (forced-colors: active) { p { border:1px solid } }
       @media (prefers-contrast: more) { p { font-weight:700 } }`,
    );
    const before = [...sheet.cssRules].map((r) => r.cssText);
    dropSchemeVariants({ styleSheets: [sheet] });
    expect([...sheet.cssRules].map((r) => r.cssText)).toEqual(before);
  });

  it("reaches a colour-scheme block nested inside another query", () => {
    // A scheme block inside a width query is still a scheme block, and it is the
    // shape a mail uses when its dark variant is only for phones.
    const sheet = sheetOf(
      `@media (max-width:580px) { @media (prefers-color-scheme: dark) { p { color:#eee } } p { font-size:12px } }`,
    );
    dropSchemeVariants({ styleSheets: [sheet] });
    const outer = sheet.cssRules[0] as CSSMediaRule;
    expect(outer.conditionText).toBe("(max-width:580px)");
    expect(outer.cssRules.length).toBe(1);
    expect((outer.cssRules[0] as CSSStyleRule).selectorText).toBe("p");
  });

  it("is what a mount does, so no mail is drawn from its dark rules by accident", () => {
    // jsdom's shadow root has no `styleSheets`, so the host is handed a root that
    // does: what is under test is that the walk is on the mount's path at all.
    const sheet = sheetOf(`@media (prefers-color-scheme: dark) { p { color:#e8eaed } } p { margin:0 }`);
    const root = { innerHTML: "", styleSheets: [sheet] };
    const host = document.createElement("div");
    Object.defineProperty(host, "shadowRoot", { value: root });
    mountOriginal(host, `<style>${sheet.ownerNode!.textContent}</style><p>words</p>`);
    expect(root.innerHTML).toContain("<p>words</p>");
    expect(sheet.cssRules.length).toBe(1);
    expect((sheet.cssRules[0] as CSSStyleRule).selectorText).toBe("p");
  });
});
// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Message, type MessageProps } from "../src/components/Message";
import { ApiError } from "../src/lib/api";
import { fetchOriginal, mountOriginal } from "../src/lib/original";

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
 * sharing an id would be observing each other.
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

const control = () => screen.queryByRole("button", { name: /original|loading/ });

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

    // And the way back is the same control, pressed.
    fireEvent.click(control()!);
    expect(container.querySelector(".bdo")).toBeNull();
    expect(container.querySelector(".bd")!.innerHTML).toBe("<p>invented body</p>");
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
    // The one answer this control can get that leaves it with nothing to press.
    // The sentence is the server's own — it says which nothing it found — so it
    // rides the note's hover rather than being replaced with a word here.
    const why = "mail:<orig-none@loomworks.example> carries no text/html part of its own";
    const load = vi.fn(async () => {
      throw new ApiError(404, why);
    });
    const { container } = draw({ original: { extId: "mail:<orig-none@loomworks.example>", load } });
    fireEvent.click(control()!);
    await waitFor(() => expect(container.querySelector(".origwhy")).not.toBeNull());
    expect(control()).toBeNull();
    expect(container.querySelector(".origwhy")!.getAttribute("title")).toBe(why);
    // What was on screen is still on screen: the ask changed nothing but the note.
    expect(container.querySelector(".bd")!.innerHTML).toBe("<p>invented body</p>");
  });

  it("draws no control where the caller has nowhere to fetch from", () => {
    // A built page, a static export, a message that arrived as plain text: the
    // prop is the flag, so its absence is the answer for all three.
    const { container } = draw();
    expect(container.querySelector(".origrow")).toBeNull();
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
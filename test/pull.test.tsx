// @vitest-environment jsdom
import { cleanup, fireEvent, render, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Timeline } from "../src/components/Timeline";
import { MEDIA_BASE } from "../src/lib/attachments";
import { normalise } from "../src/lib/normalise";
import type { Entry, Timeline as Spec } from "../src/lib/spec";

afterEach(cleanup);

/**
 * The download a chip becomes when its bytes are not here yet. What is worth
 * asserting is when that is offered at all — it spends mailbox round trips, so
 * every page that cannot or need not fetch must leave the chip the plain link to
 * the mailbox it has always been.
 */

const entry = (over: Partial<Entry>): Entry => ({
  extId: "mail:<c0ffee-1@loomworks.example>",
  date: "Mon 2 Mar 2026",
  time: "09:15",
  sender: "Ada Okoye",
  body: "<p>invented body</p>",
  ...over,
});

/** A file the mailbox holds and the corpus does not. */
const unFetched = { name: "shed.csv", kind: "CSV", size: "18 KB", gmailId: "19d263bb5a6b00db" };

/** The same file, once its bytes are stored. */
const fetched = { ...unFetched, blobSha: "sha-of-the-bytes", open: "download" as const };

const page = (messages: Entry[], onPull?: (extId: string) => void, pulling?: string | null) =>
  render(
    <Timeline
      spec={normalise({ title: "Loom cutover", messages } as Spec)}
      onPull={onPull}
      pulling={pulling}
      mediaBase={MEDIA_BASE}
    />,
  );

// Scoped to the message's own strip: the sources panel lists these files too, and
// it is not the chip the reader presses.
const chip = () => within(document.querySelector(".atts")!).getByRole("link", { name: /shed\.csv/ });
const press = () => fireEvent.click(chip());

describe("a chip whose file the corpus does not hold", () => {
  it("asks for that message's files when it is pressed", () => {
    const onPull = vi.fn();
    page([entry({ attachments: [unFetched] })], onPull);

    // It is still the same link to where the file is today — the press is
    // intercepted, the href is not replaced — so a modified press, or a reader
    // with no script, still lands in the mailbox rather than nowhere.
    expect(chip().getAttribute("href")).toContain("mail.google.com");

    press();
    expect(onPull).toHaveBeenCalledWith("mail:<c0ffee-1@loomworks.example>");
    expect(onPull).toHaveBeenCalledTimes(1);
    // The press is remembered on the element, because the render that follows the
    // pull is the one that has to replay it (see behaviour.ts).
    expect(chip().hasAttribute("data-download")).toBe(true);
  });

  it("turns the chip's own ↻ and says nothing else, and takes no second press while it does", () => {
    const onPull = vi.fn();
    page(
      [entry({ attachments: [unFetched] })],
      onPull,
      "mail:<c0ffee-1@loomworks.example>",
    );

    // The chip's own words are gone and the file's name is left alone: what used
    // to be a line that grew a word and shrank again is now the turn the nav's
    // refresh wears while it works, so the row of files does not reflow under the
    // pointer mid-download. It is still said in words for a reader who cannot see
    // it turn, because a mark that spins is the whole of the state.
    expect(chip().textContent).not.toContain("downloading");
    const spin = chip().querySelector(".spinner")!;
    expect(spin.getAttribute("aria-label")).toBe("Downloading…");
    expect(spin.getAttribute("aria-hidden")).toBeNull();
    // Named, not a live region: the pane already has one of those for what
    // happens to the thread (see .pullnote), and a mark that appears and goes on
    // every chip is not a second one.
    expect(spin.getAttribute("role")).toBe("img");
    expect(chip().getAttribute("aria-busy")).toBe("true");
    // A press while the files are already coming is absorbed: the endpoint spends
    // a mailbox round trip per call, and the reader's press has been taken.
    press();
    expect(onPull).not.toHaveBeenCalled();
  });

  it("leaves a chip alone once its bytes are here", () => {
    const onPull = vi.fn();
    page([entry({ attachments: [fetched] })], onPull);

    // The corpus's own copy is where the chip points, so the press is the server's
    // to answer (open or download, by its own rule) rather than a fetch.
    expect(chip().getAttribute("href")).toBe(`${MEDIA_BASE}/sha-of-the-bytes`);
    press();
    expect(onPull).not.toHaveBeenCalled();
    expect(chip().hasAttribute("data-download")).toBe(false);
  });

  it("is a plain link on a host that cannot fetch", () => {
    // A page rendered to a file, and any host started without -media: there is
    // nobody to ask, so the chip must not promise anything.
    page([entry({ attachments: [unFetched] })]);
    press();
    expect(chip().hasAttribute("data-download")).toBe(false);
  });

  it("is a plain link on an entry with no corpus id to name", () => {
    // An entry the page invented (a note) has nothing the server could look up.
    page([entry({ extId: undefined, attachments: [unFetched] })], () => {});
    press();
    expect(chip().hasAttribute("data-download")).toBe(false);
  });
});

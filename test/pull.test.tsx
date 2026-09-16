// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Timeline } from "../src/components/Timeline";
import { normalise } from "../src/lib/normalise";
import type { Entry, Timeline as Spec } from "../src/lib/spec";

afterEach(cleanup);

/**
 * The button that fetches one message's files. What is worth asserting is when
 * it exists at all — it is a control that spends mailbox round trips, so every
 * page that cannot or need not fetch must not show one.
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
    />,
  );

const fetchButton = () => screen.queryByRole("button", { name: /fetch/i });

describe("the fetch-files button", () => {
  it("offers to fetch a message's files, and asks for that message", () => {
    const onPull = vi.fn();
    page([entry({ attachments: [unFetched] })], onPull);
    const btn = fetchButton();
    expect(btn).not.toBeNull();
    fireEvent.click(btn!);
    expect(onPull).toHaveBeenCalledWith("mail:<c0ffee-1@loomworks.example>");
    expect(onPull).toHaveBeenCalledTimes(1);
  });

  it("is absent on a page nobody can fetch from", () => {
    // The static export, and any host started without -media: the renderer is
    // handed no way to fetch, so the control would be a promise it cannot keep.
    page([entry({ attachments: [unFetched] })]);
    expect(fetchButton()).toBeNull();
  });

  it("is absent once every file is in the corpus", () => {
    page([entry({ attachments: [fetched] })], () => {});
    expect(fetchButton()).toBeNull();
  });

  it("is offered while any one file is still missing", () => {
    // A message with one stored file and one not: the whole point is the one
    // that is not, so the button belongs here.
    page([entry({ attachments: [fetched, unFetched] })], () => {});
    expect(fetchButton()).not.toBeNull();
  });

  it("says it is fetching, and holds every other message", () => {
    const extId = "mail:<c0ffee-1@loomworks.example>";
    page(
      [
        entry({ attachments: [unFetched] }),
        entry({ extId: "mail:<c0ffee-2@fjordline.example>", attachments: [unFetched] }),
      ],
      () => {},
      extId,
    );
    const btns = screen.getAllByRole("button", { name: /fetch/i });
    expect(btns).toHaveLength(2);
    expect(btns[0]!.textContent).toBe("fetching…");
    expect((btns[0] as HTMLButtonElement).disabled).toBe(true);
    // A second pull against a spec that is already being replaced is not
    // something a reader should be able to start.
    expect((btns[1] as HTMLButtonElement).disabled).toBe(true);
    expect(btns[1]!.textContent).toBe("fetch files");
  });

  it("is absent on an entry with no corpus id to name", () => {
    // An entry the page invented (a note) has nothing the server could look up,
    // so the control has nothing to say.
    page([entry({ extId: undefined, attachments: [unFetched] })], () => {});
    expect(fetchButton()).toBeNull();
  });

  it("is absent on a message with no attachments", () => {
    page([entry({})], () => {});
    expect(fetchButton()).toBeNull();
  });
});

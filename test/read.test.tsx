// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within, act } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { makeQueryClient } from "../src/lib/queryClient";
import { createChainmailRouter } from "../src/router";

/**
 * Unread state: what the list says, and the one control that changes it.
 *
 * The state itself lives in the mailbox — the server writes it there and the
 * reader's phone shows the same thing — so what is asserted here is the client's
 * half: the count on a row, the two-click gesture that asks for the other state,
 * the button in the pane, and what the list shows in the moment before the mailbox
 * has answered — patched with what the reader asked for, and re-read to settle it
 * (see lib/lists). Everything below is invented; this corpus holds real
 * correspondence.
 */

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

interface Call {
  url: string;
  method: string;
  body?: string;
}

let calls: Call[];
type Handler = (call: Call) => Promise<Response> | Response;
let handler: Handler;

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });

const pathOf = (c: Call) => new URL(c.url).pathname;

const ROOT = "mail:<loom-cutover-1@example.fed>";
const OTHER = "mail:<fence-panel-9@example.fed>";

const entry = (extId: string, person: string, snippet: string) => ({
  extId,
  source: "mail",
  ts: "2026-03-11T17:40:00Z",
  personId: 1,
  person,
  snippet,
  score: 0,
  proseRank: 0,
  identRank: 0,
  semRank: 0,
});

/** One thread as /v1/search answers it. `unread` is a property of the thread, so
 *  it is set here rather than derived: the server counts the whole trail, and a
 *  client that re-derived it from the three entries a row carries would be
 *  counting a third of the thread. */
const thread = (over: Record<string, unknown>) => ({
  rootExtId: ROOT,
  subject: "Loom cutover schedule",
  first: "2026-03-02T09:15:00Z",
  last: "2026-03-11T17:40:00Z",
  sources: ["mail"],
  entries: 3,
  matched: 3,
  people: 3,
  attachments: 0,
  score: 0.01,
  unread: 0,
  best: [entry(ROOT, "Bo Halvorsen", "Roof access is fine from the 14th.")],
  ...over,
});

const CHAIN_BODY = {
  rootExtId: ROOT,
  entries: [
    {
      extId: ROOT,
      source: "mail",
      quoted: false,
      ts: "2026-03-11T17:40:00Z",
      author: "Bo Halvorsen",
      subject: "Loom cutover schedule",
      body: "Roof access is fine from the 14th.",
      html: "<p>Roof access is fine from the 14th.</p>",
      tz: "AEST",
      tzOffsetMinutes: 600,
      org: "Loomworks",
      // Who the corpus recorded on the entry: the sender first, then the people it
      // was addressed to — the two of them sent nothing, which is what the pane's
      // own panel has to be able to list.
      participants: [
        { personId: 1, name: "Bo Halvorsen", role: "from" },
        { personId: 2, name: "Ada Byron", role: "to" },
        { personId: 3, name: "Cy Devlin", role: "cc" },
      ],
      // The file this message carried, exactly as the thread read answers it: the
      // chip is drawn from these fields and nothing else, so a pane can only show
      // one where the corpus stated one (see internal/corpus.Show).
      attachments: [
        {
          name: "roof-access.csv",
          kind: "CSV",
          size: "512 B",
          blobSha: "sha-roof",
          open: "popup",
          view: "text",
        },
      ],
    },
  ],
};

/** The whole surface the inbox reads, with /v1/search answerable per test. */
const server = (search: () => Response, read?: Handler, mail?: Handler): Handler => (c) => {
  const p = pathOf(c);
  if (p === "/v1/search") return search();
  if (p === "/v1/read") return read ? read(c) : json(200, { chain: ROOT, unread: false, marked: 3, skipped: 0 });
  if (p === "/v1/mail")
    return mail
      ? mail(c)
      : json(200, { action: "archive", changed: 3, skipped: 0, chains: [] });
  if (p.startsWith("/v1/chains/")) return json(200, CHAIN_BODY);
  // The mailbox's folders, for the pane's move control. The inbox is in the list the
  // service gives, and is what the control has to leave out.
  if (p === "/v1/labels") {
    return json(200, { labels: [{ name: "INBOX", messages: 5 }, { name: "Work", messages: 2 }] });
  }
  if (p === "/v1/settings") return json(200, {});
  if (p === "/auth/status") return json(200, { signed_in: true });
  return json(500, { error: `unexpected call to ${c.method} ${p}` });
};

const reads = () => calls.filter((c) => c.method === "POST" && pathOf(c) === "/v1/read");
const mails = () => calls.filter((c) => c.method === "POST" && pathOf(c) === "/v1/mail");
const searches = () => calls.filter((c) => pathOf(c) === "/v1/search");
const chains = () => calls.filter((c) => pathOf(c).startsWith("/v1/chains/"));

const page = (chains: unknown[]) => () => json(200, { mode: "lexical", chains });

beforeEach(() => {
  calls = [];
  (globalThis as { IntersectionObserver?: unknown }).IntersectionObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
    takeRecords() {
      return [];
    }
  };
  handler = () => json(500, { error: "no handler installed" });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const req =
      input instanceof Request ? input : new Request(new URL(String(input), location.href), init);
    const call: Call = { url: req.url, method: req.method };
    if (req.method !== "GET") call.body = await req.text();
    if (pathOf(call) === "/v1/version") return json(200, {});
    calls.push(call);
    return handler(call);
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  history.replaceState(null, "", "/");
});

async function mountApp(path = "/") {
  const router = createChainmailRouter([path]);
  await router.load();
  render(
    <QueryClientProvider client={makeQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

/** The reading pane, so an assertion about the control cannot be satisfied by a
 *  row that happens to carry the same words. */
const pane = () => document.querySelector(".ibread") as HTMLElement;

/** Open a row, which is how a thread reaches the pane now that the pane starts
 *  empty — the circle only exists once there is a thread to be a state of. The
 *  rows arrive with the list, so it waits for them the way a reader has to. */
const openRow = async (n = 0) => {
  await waitFor(() =>
    expect(document.querySelectorAll(".ibrow button").length).toBeGreaterThan(n),
  );
  fireEvent.click(document.querySelectorAll(".ibrow button")[n] as HTMLElement);
};

describe("what a thread's read state looks like", () => {
  it("marks the chains with unread mail, and dims the ones that are read", async () => {
    handler = server(page([thread({ unread: 2 }), thread({ rootExtId: OTHER, subject: "Fence panels", unread: 0 })]));
    await mountApp();

    const rows = await screen.findAllByRole("checkbox");
    expect(rows).toHaveLength(2);
    // The unread thread is the one the class names, and it is named so that the
    // *other* row can be dimmed: there is no mark on the row with unread mail —
    // it is the row left alone, at full weight on a plain background.
    expect(document.querySelectorAll(".ibrow.unread")).toHaveLength(1);
    const row = document.querySelector(".ibrow.unread") as HTMLElement;
    expect(row.querySelector(".ibwho")?.textContent).toContain("Bo Halvorsen");
    // Nothing is drawn for it: the count it used to carry as a pill, then as a
    // circle, is the row's tooltip in words and no more.
    expect(document.querySelectorAll(".ibunread")).toHaveLength(0);
    expect(row.querySelector(".ibopen")?.getAttribute("title")).toBe(
      "2 unread messages in this thread",
    );
    // The read thread wears no such class — which is the whole marking, and the
    // only part of it a test can see: the weight and the background are CSS.
    const read = document.querySelectorAll(".ibrow:not(.unread)");
    expect(read).toHaveLength(1);
    expect(read[0]!.textContent).toContain("Fence panels");
    expect(read[0]!.querySelector(".ibopen")?.getAttribute("title")).toBeNull();
    // The count and the emphasis are one claim, so the row that has no unread
    // mail is not emphasised either.
    expect(document.querySelectorAll(".ibrow.unread")).toHaveLength(1);
    // The pane's circle is the same claim in the toolbar, and the same kind of
    // button as the two verbs beside it: filled (and pressed) while the thread it
    // is reading is unread, and drawn as a glyph in the button the bin and the
    // trash can are in rather than as a bare circle on the line.
    await openRow();
    await waitFor(() => expect(pane().querySelector(".ibread-read")).toBeTruthy());
    const circle = pane().querySelector(".ibread-read") as HTMLElement;
    expect(circle.className).toContain("unread");
    expect(circle.className).toContain("ibicon");
    expect(circle.querySelector("svg circle")).toBeTruthy();
    expect(circle.getAttribute("aria-pressed")).toBe("true");
  });

  it("counts the files a thread carries, and says nothing when there are none", async () => {
    // The third count, beside the people and the mail: a paperclip and a number.
    // A thread with no files draws no clip in the list — an empty one says nothing
    // on a page where most threads have none — and the pane's head draws the zero,
    // because there it is an answer about the thread the reader has open.
    handler = server(
      page([
        thread({ attachments: 3, people: 3, entries: 3 }),
        thread({ rootExtId: OTHER, subject: "Fence panels", attachments: 0, people: 2, entries: 1 }),
      ]),
    );
    await mountApp();
    const rows = await screen.findAllByRole("checkbox");
    expect(rows).toHaveLength(2);
    const clips = document.querySelectorAll(".ibatt");
    expect(clips).toHaveLength(1);
    expect(clips[0]!.textContent!.trim()).toBe("3");
    expect(clips[0]!.getAttribute("title")).toBe("3 attachments in this thread");
    expect(clips[0]!.querySelector("svg")).toBeTruthy();

    // Two people is a letter and its reply, and one message is the subject's own
    // count: neither mark is drawn on that row.
    const bare = rows[1]!.closest(".ibrow")!;
    expect(bare.querySelector(".ibppl")).toBeNull();
    expect(bare.querySelector(".ibcount")).toBeNull();
    expect(bare.querySelector(".ibatt")).toBeNull();

    // Opened, the head wears all three — including the zero.
    fireEvent.click(rows[0]!.closest(".ibrow")!.querySelector(".ibopen") as HTMLElement);
    await waitFor(() => expect(pane().querySelector(".ibread-counts .ibatt")).toBeTruthy());
    const counts = pane().querySelector(".ibread-counts")!;
    expect(counts.querySelector(".ibatt")!.textContent!.trim()).toBe("3");
    expect(counts.querySelectorAll("svg")).toHaveLength(3);
  });

  it("opens the thread on a click, and marks the row on a double click", async () => {
    // The list is where a reader triages, and two clicks is how a mail client has
    // always let them do it — the same write the pane's own button makes, on a set
    // of one. Nothing is debounced: the click opens at once and says the same thing
    // however many times it is made, so the browser's own double click has nothing
    // to undo and the pane never moves twice (see ThreadRow, and dripfeed-web's
    // list, which is where the shape comes from).
    handler = server(page([thread({ unread: 2 })]));
    await mountApp();
    await screen.findAllByRole("checkbox");
    const row = document.querySelector(".ibopen") as HTMLElement;

    fireEvent.click(row);
    await waitFor(() => expect(chains()).toHaveLength(1));
    expect(decodeURIComponent(chains()[0]!.url)).toContain(ROOT);
    expect(reads()).toHaveLength(0);

    // The pair the browser reports: two clicks, then the dblclick — which is the
    // write, and which leaves the pane on the thread the first click opened.
    fireEvent.click(row);
    fireEvent.doubleClick(row);
    await waitFor(() => expect(reads()).toHaveLength(1));
    expect(JSON.parse(reads()[0]!.body ?? "{}")).toEqual({ chain: ROOT, unread: false });
    expect(chains()).toHaveLength(1);
  });

  it("shows the state the reader asked for before the mailbox has answered", async () => {
    // The write is a round trip through Gmail, and a row that sits unchanged for
    // the length of it reads as a press that missed (see lib/lists): the count
    // changes with the press, and the re-read is what settles it afterwards. The
    // read is held open here so the assertion is about the patch and not about the
    // answer.
    handler = server(page([thread({ unread: 2 })]), () => new Promise<Response>(() => {}));
    await mountApp();
    await screen.findAllByRole("checkbox");
    expect(document.querySelectorAll(".ibrow.unread")).toHaveLength(1);
    const row = document.querySelector(".ibopen") as HTMLElement;

    fireEvent.click(row);
    fireEvent.doubleClick(row);
    await waitFor(() => expect(document.querySelectorAll(".ibrow.unread")).toHaveLength(0));
  });

  it("has no read state to offer for a thread whose count it does not hold", async () => {
    // A thread named only by the address bar: with no state to invert, the gesture
    // has no direction, so nothing is asked (see the pane's own button) — the click
    // still opens it, and the double click does nothing at all.
    handler = server(() =>
      json(200, {
        mode: "lexical",
        chains: [{ rootExtId: ROOT, subject: "Loom cutover", entries: 2 }],
      }),
    );
    await mountApp();
    await screen.findAllByRole("checkbox");

    const row = document.querySelector(".ibopen") as HTMLElement;
    fireEvent.click(row);
    fireEvent.doubleClick(row);
    await waitFor(() => expect(chains()).toHaveLength(1));
    await new Promise((r) => setTimeout(r, 20));
    expect(reads()).toHaveLength(0);
  });

  it("moves the thread from the pane's own dropdown, and keeps the head describing it", async () => {
    // The pane is where a reader finishes with a thread — archive and delete are
    // already here — and moving it out of the folder it was listed in is the third
    // way of saying the same thing, so the control is here too, drawn as the
    // selection bar draws it (see MailVerbs and .ibmove).
    //
    // The move takes the thread out of the view it was listed in (see lib/lists) and
    // the reader is looking at it, so the head has to keep the subject and the counts
    // the row had: a move that turned the thread into "(no subject)" would be the one
    // action that took the mail away from the person reading it.
    handler = server(page([thread({ unread: 0 })]));
    await mountApp();
    await openRow();
    await waitFor(() => expect(pane().querySelector(".ibread-subj")?.textContent).toBe("Loom cutover schedule"));

    const move = (await screen.findByLabelText("Move to a folder")) as HTMLSelectElement;
    // The inbox is not offered: a move whose destination is where it is leaving.
    expect([...move.options].map((o) => o.textContent)).toEqual(["Move…", "Work"]);

    fireEvent.change(move, { target: { value: "Work" } });
    await waitFor(() => expect(mails()).toHaveLength(1));
    expect(JSON.parse(mails()[0]!.body ?? "{}")).toEqual({
      chains: [ROOT],
      action: "move",
      labels: ["Work"],
    });

    // The head still describes the thread, and the control has not moved off its
    // placeholder: a folder named in it is a folder the reader could pick twice.
    await new Promise((res) => setTimeout(res, 20));
    expect(pane().querySelector(".ibread-subj")?.textContent).toBe("Loom cutover schedule");
    expect(pane().textContent).not.toContain("(no subject)");
    expect(move.value).toBe("");
  });

  it("offers the other state, and asks the server for it", async () => {
    handler = server(page([thread({ unread: 2 })]));
    await mountApp();
    await openRow();

    const button = await screen.findByRole("button", { name: "Mark read" });
    fireEvent.click(button);
    await waitFor(() => expect(reads()).toHaveLength(1));
    expect(JSON.parse(reads()[0]!.body ?? "{}")).toEqual({ chain: ROOT, unread: false });
  });

  it("names the state it is in, and the action it would take", async () => {
    // Unread in the pane: the press it offers is "mark read". A read thread
    // offers the reverse — the control is the state, so it never labels itself
    // with the state it already shows.
    let answered = 0;
    handler = server(() => {
      answered += 1;
      return json(200, { mode: "lexical", chains: [thread({ unread: answered > 1 ? 0 : 2 })] });
    });
    await mountApp();
    await openRow();

    const unread = (await screen.findByRole("button", { name: "Mark read" })) as HTMLElement;
    expect(unread.getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(unread);
    await waitFor(() =>
      expect((pane().querySelector(".ibread-read") as HTMLElement).getAttribute("aria-pressed")).toBe(
        "false",
      ),
    );
    const read = pane().querySelector(".ibread-read") as HTMLElement;
    expect(read.getAttribute("aria-label")).toBe("Mark unread");
    expect(read.className).not.toContain("unread");
  });

  it("reads the list again rather than patching the count itself", async () => {
    // The second page of the list is the same thread, now read: what the button
    // may show afterwards is what the server said, not what the client assumed.
    let answered = 0;
    handler = server(() => {
      answered += 1;
      return json(200, { mode: "lexical", chains: [thread({ unread: answered > 1 ? 0 : 2 })] });
    });
    await mountApp();
    await openRow();

    fireEvent.click(await screen.findByRole("button", { name: "Mark read" }));
    // The row stops being the unread one when the mail is read, and the button
    // flips to the state a reader would want next: what the row shows afterwards
    // is the server's answer, not the client's assumption.
    expect(await screen.findByRole("button", { name: "Mark unread" })).toBeTruthy();
    await waitFor(() => expect(document.querySelectorAll(".ibrow.unread")).toHaveLength(0));
    expect(pane().querySelectorAll(".ibread-read.unread")).toHaveLength(0);
  });

  it("says so when the host cannot change the mailbox", async () => {
    handler = server(page([thread({ unread: 1 })]), () =>
      json(403, { error: "marking mail read is disabled: this server was started without -mark-read" }),
    );
    await mountApp();
    await openRow();

    fireEvent.click(await screen.findByRole("button", { name: "Mark read" }));
    expect(await screen.findByText(/without -mark-read/)).toBeTruthy();
    // And nothing is claimed about the state: the row is still the unread one,
    // because the count is still the server's.
    expect(document.querySelectorAll(".ibrow.unread")).toHaveLength(1);
  });

  it("shows no control for a thread the list has not described", async () => {
    // A thread named only by the address bar has no count, and a button that had
    // to guess which way it went would be wrong half the time.
    handler = server(page([thread({ rootExtId: OTHER, subject: "Fence panels", unread: 0 })]));
    await mountApp(`/?open=${encodeURIComponent(ROOT)}`);

    await screen.findByText(/Roof access is fine/);
    const head = pane().querySelector(".ibread-head") as HTMLElement;
    expect(head.querySelector(".ibread-read")).toBeNull();
  });

  it("draws the message's own subject, and the files it carried", async () => {
    // Both come off the thread read and nothing else. The subject is the entry's:
    // the pane's head names the thread's, and a reply that renames a thread is
    // otherwise nowhere on the page. The files are the corpus's rows for that
    // entry — the read that used to answer neither, which is why a file could be
    // visible in a built page and absent here.
    handler = server(page([thread({})]));
    await mountApp();
    await openRow();

    // The snippet is in the list before the pane has drawn anything, so the wait
    // is for the bubble itself.
    const msg = await waitFor(() => {
      const m = pane().querySelector(".msg");
      if (!m) throw new Error("the pane has not drawn the message yet");
      return m as HTMLElement;
    });
    const subj = msg.querySelector(".hdet .subj");
    expect(subj?.textContent).toBe("Loom cutover schedule");
    // And only where the entry had one: a message that stated no subject says
    // nothing rather than leaving an empty line where a title would be.
    expect(subj?.getAttribute("title")).toBe("Loom cutover schedule");

    const chip = msg.querySelector(".att") as HTMLAnchorElement;
    expect(chip.querySelector(".afn")?.textContent).toBe("roof-access.csv");
    expect(chip.querySelector(".ameta")?.textContent).toBe("CSV · 512 B");
    // Bytes the corpus holds are served by this app rather than linked back to
    // the mailbox, and the digest is what asks for them (see localHref).
    expect(chip.getAttribute("href")).toBe("/v1/attachments/sha-roof");
  });
});

describe("what the pane does to the thread it has open", () => {
  /** The pane’s own pair of verbs, waited for: the head only exists once the
   *  thread the list opened has arrived in it, which is a fetch away. */
  const verb = (name: string) => within(pane()).findByRole("button", { name });
  it("archives it, and says what archiving meant", async () => {
    handler = server(page([thread({})]), undefined, () =>
      json(200, { action: "archive", changed: 3, skipped: 0, chains: [] }),
    );
    await mountApp();
    await openRow();

    const asked = searches().length;
    fireEvent.click(await verb("Archive"));
    await waitFor(() => expect(mails()).toHaveLength(1));
    // One thread, named by its root ext id: the same verb the bar sends for a
    // ticked set, and a set of one is a set.
    expect(JSON.parse(mails()[0]!.body ?? "{}")).toEqual({ chains: [ROOT], action: "archive" });
    // The same sentence the bar gives for the same write, in the same corner the
    // shell draws every account in — and not in the pane, where it would take a
    // line from the thread the reader has just filed. The list is asked again
    // rather than a row quietly dropped from under the reader.
    const said = await screen.findByText(/Archived 3 messages/);
    expect(said.closest(".toasts")).not.toBeNull();
    expect(pane().contains(said)).toBe(false);
    expect(said.textContent).toBe("Archived 3 messages — out of the inbox, still in All Mail.");
    await waitFor(() => expect(searches().length).toBeGreaterThan(asked));
  });

  it("deletes through the trash, and says how long that lasts", async () => {
    handler = server(page([thread({})]), undefined, () =>
      json(200, { action: "trash", changed: 3, skipped: 0, chains: [] }),
    );
    await mountApp();
    await openRow();

    fireEvent.click(await verb("Delete"));
    await waitFor(() => expect(mails()).toHaveLength(1));
    expect(JSON.parse(mails()[0]!.body ?? "{}")).toEqual({ chains: [ROOT], action: "trash" });
    // "Deleted" is a claim about the reader's mailbox, so the sentence says where
    // it went and how long it can be undone — the trash is Gmail's, not a folder
    // this page invented.
    expect(await screen.findByText(/recoverable for 30 days/)).toBeTruthy();
  });

  it("drops the account when the reader moves to another thread", async () => {
    // The pane is not remounted between chains — only its contents change — so a
    // sentence about the thread that has just moved would otherwise stand under
    // the head of the next one: a claim about a thread nobody is looking at.
    handler = server(
      page([thread({}), thread({ rootExtId: OTHER, subject: "Fence panels" })]),
      undefined,
      () => json(200, { action: "archive", changed: 3, skipped: 0, chains: [] }),
    );
    await mountApp();
    await openRow(0);
    fireEvent.click(await verb("Archive"));
    await screen.findByText(/Archived 3 messages/);

    await openRow(1);
    await screen.findByText("Fence panels");
    await waitFor(() => expect(screen.queryByText(/Archived 3 messages/)).toBeNull());
  });

  it("takes the account away once it has been read", async () => {
    // A sentence about work that is over, in a pane nobody has left, is the page
    // still reporting a moment that has passed. Watched from before the action, so
    // the timer the note arms is the one this test drives.
    handler = server(page([thread({})]), undefined, () =>
      json(200, { action: "archive", changed: 3, skipped: 0, chains: [] }),
    );
    const armed = vi.spyOn(globalThis, "setTimeout");
    await mountApp();
    await openRow();

    fireEvent.click(await verb("Archive"));
    await screen.findByText(/Archived 3 messages/);

    const armedTimer = armed.mock.calls.find(([, ms]) => ms === 5000);
    armed.mockRestore();
    if (!armedTimer) throw new Error("the note armed no five-second timer");
    act(() => (armedTimer[0] as () => void)());
    expect(screen.queryByText(/Archived 3 messages/)).toBeNull();
  });

  it("names the switch that would allow the write, and keeps saying it", async () => {
    // A refusal is the host's own answer rather than a failure of the write — it
    // was started without the switch that allows this one — so it names the switch
    // and it is not on the note's timer: it is something to act on.
    handler = server(page([thread({})]), undefined, () =>
      json(403, { error: "changing mail is disabled: this server was started without -mail-write" }),
    );
    const armed = vi.spyOn(globalThis, "setTimeout");
    await mountApp();
    await openRow();

    fireEvent.click(await verb("Delete"));
    expect(await screen.findByText(/without -mail-write/)).toBeTruthy();
    const watchers = armed.mock.calls.filter(([, ms]) => ms === 5000);
    armed.mockRestore();
    expect(watchers).toHaveLength(0);
    const refusal = document.querySelector(".toasts .toast.bad");
    expect(refusal?.textContent).toContain("without -mail-write");
  });
});

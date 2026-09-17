// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within, act } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { makeQueryClient } from "../src/lib/queryClient";
import { createChainmailRouter } from "../src/router";
import { whenShort } from "../src/lib/stamp";

/**
 * The inbox: "/" with nothing asked of it. Every name, address and id below is
 * invented — this corpus holds real correspondence, and no fixture here may be
 * one — and every assertion is about a list, so the invented content is only
 * ever read back as text.
 */

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

interface Call {
  url: string;
  method: string;
  body?: string;
}

/** Records what was asked for, so a test asserts the request rather than the mock. */
let calls: Call[];
type Handler = (call: Call) => Promise<Response> | Response;
let handler: Handler;

const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json", ...headers },
  });

const pathOf = (c: Call) => new URL(c.url).pathname;
const paramsOf = (c: Call) => new URL(c.url).searchParams;

/**
 * jsdom has no IntersectionObserver, and the inbox asks for the next page when
 * the end of the list scrolls into view. This stands in for it and can be driven
 * by hand — `scrollToEnd()` is a reader reaching the bottom — so the tests are
 * about the wiring (a marker exists, it is watched, being seen asks for what is
 * older) rather than about a browser's scroll arithmetic.
 */
const watchers = new Set<() => void>();
class ScrollWatcher {
  root = null;
  rootMargin = "";
  thresholds: number[] = [];
  private seen: () => void;
  constructor(cb: IntersectionObserverCallback) {
    this.seen = () =>
      cb([{ isIntersecting: true } as IntersectionObserverEntry], this as unknown as IntersectionObserver);
    watchers.add(this.seen);
  }
  observe() {}
  unobserve() {}
  disconnect() {
    watchers.delete(this.seen);
  }
  takeRecords() {
    return [];
  }
}
/** The marker declared below is not the only one: this fires every live watcher. */
const scrollToEnd = () => {
  for (const see of [...watchers]) see();
};

/** The shape /v1/search answers with, for a thread a row is built from. Loose on
 *  purpose: a fixture only has to survive JSON to the client, and the fields a
 *  test reads back are typed here rather than inferred from each literal. */
interface FixtureChain {
  rootExtId: string;
  subject: string;
  first: string;
  last: string;
  sources: string[];
  entries: number;
  matched: number;
  people: number;
  score: number;
  best: unknown[];
}

function thread(over: Record<string, unknown>): FixtureChain {
  return { sources: ["mail"], entries: 1, matched: 1, people: 2, score: 0.01, ...over } as FixtureChain;
}

/** An entry attached to a thread, as the wire's EntryHit: no ranking found it,
 *  so the snippet is the opening of the message and every rank is 0. */
const entry = (over: Record<string, unknown>) => ({
  source: "mail",
  personId: 1,
  score: 0,
  proseRank: 0,
  identRank: 0,
  semRank: 0,
  ...over,
});

const CHAINS: FixtureChain[] = [
  thread({
    rootExtId: "mail:<fence-panel-9@example.fed>",
    subject: "Fence panels",
    last: "2026-04-01T08:00:00Z",
    first: "2026-04-01T08:00:00Z",
    best: [
      entry({
        extId: "mail:<fence-panel-9@example.fed>",
        ts: "2026-04-01T08:00:00Z",
        person: "Ada Okoye",
        snippet: "Unrelated: the fence panels arrived.",
      }),
    ],
  }),
  thread({
    rootExtId: "mail:<loom-cutover-1@example.fed>",
    subject: "Loom cutover schedule",
    entries: 4,
    matched: 4,
    people: 4,
    first: "2026-03-02T09:15:00Z",
    last: "2026-03-11T17:40:00Z",
    best: [
      // Deliberately older first: a row is a summary of the newest message, and
      // `best` is whatever the service thought was worth attaching.
      entry({
        extId: "mail:<loom-cutover-2@example.fed>",
        ts: "2026-03-02T09:15:00Z",
        person: "Ada Okoye",
        snippet: "Can you quote the solar install for the north shed?",
      }),
      entry({
        extId: "mail:<loom-cutover-4@example.fed>",
        ts: "2026-03-11T17:40:00Z",
        personId: 2,
        person: "Bo Halvorsen",
        snippet: "Roof access is fine from the 14th.",
      }),
    ],
  }),
];

const SPEC = {
  title: "Loom cutover",
  messages: [
    {
      date: "Mon 2 Mar 2026",
      time: "09:15",
      sender: "Ada Byron",
      org: "Loomworks",
      body: "<p>invented body</p>",
    },
  ],
};

/** A page of the list, as the service answers an empty query: chains, newest
 *  message first, each carrying the entries a row reads. */
const pageOf = (chains: unknown[]) => json(200, { mode: "lexical", chains });

/** The reading pane asks /v1/chains/<root> for whichever thread is selected, and
 *  every thread in the fixtures has a body of its own, so "the pane swapped" is a
 *  claim about what is on screen rather than about a call being made.
 *
 *  The body arrives as the rendered `html` a build would put in the bubble (the
 *  service renders it with the same conversion), and the plain text beside it:
 *  the pane draws the html, and a test that asserted against the text alone would
 *  pass just as well if it drew the wrong one. `Loom cutover`'s html carries
 *  markup the text does not, which is how a test can tell them apart. */
const CHAIN_BODIES: Record<
  string,
  {
    author: string;
    subject: string;
    body: string;
    html: string;
    to?: string;
    fromEmail?: string;
    fromQuotedBy?: string;
    tz: string;
    tzOffsetMinutes: number;
  }
> = {
  "mail:<fence-panel-9@example.fed>": {
    author: "Ada Okoye",
    subject: "Fence panels",
    body: "Unrelated: the fence panels arrived, and the gate needs a new hinge.",
    html: "<p>Unrelated: the fence panels arrived, and the gate needs a new hinge. <b>Regards, Ada</b></p>",
    to: "Bo Halvorsen, cc Cy Okafor",
    fromEmail: "ada@okoye.example",
    tz: "AEST",
    tzOffsetMinutes: 600,
  },
  "mail:<loom-cutover-1@example.fed>": {
    author: "Bo Halvorsen",
    subject: "Loom cutover schedule",
    body: "Roof access is fine from the 14th.",
    html: "<p>Roof access is fine from the 14th.</p>",
    to: "Ada Okoye",
    fromEmail: "bo@halvorsen.example",
    tz: "AEST",
    tzOffsetMinutes: 600,
  },
  // A message recovered from inside someone else's quote: it has no headers of
  // its own, so the corpus sends no recipient line and no address, and the pane
  // must say so rather than leave the gap looking like a rendering failure. What
  // it sends instead is the person who quoted it, which is where the entry came
  // from — the one address here that is evidence rather than a guess.
  "quote:9f2c1ab4e77d": {
    author: "Dana Reyes",
    subject: "Fence panels",
    body: "The gate hinge was ordered.",
    html: "<p>The gate hinge was ordered.</p>",
    fromQuotedBy: "Ada Okoye <ada@okoye.example>",
    tz: "AEST",
    tzOffsetMinutes: 600,
  },
};

/** A thread of more than one, oldest first, where the newest entry is not the one
 *  the list row starts from. A row is a summary of the newest message (see
 *  ThreadRow), so a pane that opens the thread at its top is the row's promise
 *  broken — see the landing test below. */
const MULTI_ROOT = "mail:<solar-trail-1@example.fed>";
const MULTI_ENTRIES = [
  {
    extId: "mail:<solar-trail-2@example.fed>",
    source: "mail",
    quoted: false,
    ts: "2026-03-02T09:15:00Z",
    author: "Ada Okoye",
    body: "Can you quote the north shed?",
    html: "<p>Can you quote the north shed?</p>",
    to: "Bo Halvorsen",
    fromEmail: "ada@okoye.example",
    tz: "AEST",
    tzOffsetMinutes: 600,
    // A message the mailbox holds: the receipt names it by the id the mailbox
    // gave it, which is what its permalink opens.
    permalink: "https://mail.google.com/mail/u/0/#all/19fee08b9d28e28b",
    sightings: [{ kind: "direct" }],
  },
  {
    extId: "mail:<solar-trail-4@example.fed>",
    source: "mail",
    quoted: true,
    ts: "2026-03-11T17:40:00Z",
    author: "Bo Halvorsen",
    body: "Roof access is fine from the 14th.",
    html: "<p>Roof access is fine from the 14th.</p>",
    tz: "AEST",
    tzOffsetMinutes: 600,
    // It answers the first message of the thread, which is what the pane's reply
    // link points at — the corpus resolves a parent to the entry it names.
    parent: "mail:<solar-trail-2@example.fed>",
    // Recovered from the first message's quote, so its receipt names the host it
    // was unspooled from — and that host is on this page, so the id links to it
    // rather than out to a mailbox.
    sightings: [{ kind: "quoted", seenIn: "mail:<solar-trail-2@example.fed>" }],
  },
];

const chainHandler: Handler = (c) => {
  const root = decodeURIComponent(pathOf(c).slice("/v1/chains/".length));
  if (root === MULTI_ROOT) return json(200, { rootExtId: root, entries: MULTI_ENTRIES });
  const b = CHAIN_BODIES[root];
  return b
    ? json(200, {
        rootExtId: root,
        entries: [{ extId: root, source: "mail", quoted: false, ts: "2026-04-01T08:00:00Z", ...b }],
      })
    : json(404, { error: `no thread ${root}` });
};

/** A handler that answers the pane for any thread, and lets the test's own
 *  handler see everything else. */
const withChains = (inner: Handler): Handler => (c) =>
  pathOf(c).startsWith("/v1/chains/") ? chainHandler(c) : inner(c);

const buildHandler: Handler = withChains((c) => {
  const p = pathOf(c);
  if (p === "/v1/spec" && c.method === "POST") return json(200, SPEC);
  if (p.startsWith("/v1/specs/")) return json(200, SPEC);
  if (p === "/v1/search") return pageOf(CHAINS);
  // The people the corpus holds, for the nav's Person control: one person with
  // two addresses, because folding a person's aliases into one person is the
  // whole reason that control exists (see the same fixture in client.test.tsx).
  if (p === "/v1/people") {
    return json(200, {
      people: [
        {
          personId: 1,
          displayName: "Ada Byron",
          identities: ["email:ada@okoye.example", "email:ada@work.example"],
          sent: 340,
          received: 121,
        },
      ],
    });
  }
  // No default folder unless a test says otherwise: the inbox opens on the
  // whole corpus, which is what every list test below assumes.
  if (p === "/v1/settings") return json(200, {});
  // The shell's sign-in banner probes auth on every route; answer it signed in
  // so a test exercises the app, not the banner.
  if (p === "/auth/status") return json(200, { signed_in: true });
  return json(500, { error: `unexpected call to ${c.method} ${p}` });
});

beforeEach(() => {
  calls = [];
  watchers.clear();
  (globalThis as { IntersectionObserver?: unknown }).IntersectionObserver = ScrollWatcher;
  handler = () => json(500, { error: "no handler installed" });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const req =
      input instanceof Request ? input : new Request(new URL(String(input), location.href), init);
    const call: Call = { url: req.url, method: req.method };
    if (req.method !== "GET") call.body = await req.text();
    // The nav's deploy stamp asks /v1/version on every route. No test in this
    // file is about it (see deploy.test.tsx), and a stamp rendered into the nav
    // of every other test would answer a question they did not ask — so it is
    // answered here, and not recorded as one of their calls.
    if (new URL(call.url).pathname === "/v1/version") return json(200, {});
    calls.push(call);
    return handler(call);
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  history.replaceState(null, "", "/");
});

/** The app under a fresh router with an in-memory history, mounted at the URL
 *  under test — "/" for the inbox, "/?q=…" for the search page. */
async function mountApp(...initialEntries: string[]) {
  const router = createChainmailRouter(initialEntries);
  await router.load();
  render(
    <QueryClientProvider client={makeQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return router;
}

const click = (el: Element) => fireEvent.click(el);

/** Braid the ticked chains into a page. The bar's button opens the dialog that
 *  holds the title, and the builds below type none: an untitled page takes a
 *  clock name rather than borrowing a subject's slug. */
async function braid() {
  click(screen.getByRole("button", { name: "Braid Threads" }));
  click(await screen.findByRole("button", { name: "Braid" }));
}

/** The reading pane, so an assertion about the thread shown cannot be satisfied
 *  by the same text in a row. */
const pane = () => document.querySelector(".ibread") as HTMLElement;

describe("the home page with no query", () => {
  it("lists every thread, newest message first", async () => {
    handler = buildHandler;
    await mountApp("/");

    // The rows arrive in the order the service answered: a browse is by time,
    // and the client re-orders nothing.
    const boxes = await screen.findAllByRole("checkbox");
    expect(boxes).toHaveLength(2);
    const rowText = document.querySelectorAll(".ibrow");
    expect(rowText[0]!.textContent).toContain("Fence panels");
    expect(rowText[1]!.textContent).toContain("Loom cutover schedule");
  });

  it("reads a row as sender, subject, snippet, date and count — and nothing else", async () => {
    handler = buildHandler;
    await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    const loom = document.querySelectorAll(".ibrow")[1]!.textContent ?? "";
    // Who wrote *last*, not the author of the first entry attached: the row is a
    // summary of the newest message.
    expect(loom).toContain("Bo Halvorsen");
    expect(loom).not.toContain("Ada Okoye");
    expect(loom).toContain("Roof access is fine from the 14th.");
    // The date of the newest entry, not of the first one attached — and written
    // in the reader's own timezone, so it is asserted through the same function
    // the row uses rather than as a fixed string.
    const loomRow = document.querySelectorAll(".ibrow")[1]!;
    expect(loomRow.querySelector(".ibwhen")!.textContent).toBe(whenShort("2026-03-11T17:40:00Z"));
    const fenceRow = document.querySelectorAll(".ibrow")[0]!;
    expect(fenceRow.querySelector(".ibwhen")!.textContent).not.toBe(
      whenShort("2026-03-11T17:40:00Z"),
    );
    // Gmail-minimal: the count appears only when a thread has more than one
    // message, and no participant or relevance metadata does at all.
    expect(document.querySelectorAll(".ibcount")).toHaveLength(1);
    expect(document.querySelectorAll(".ibcount")[0]!.textContent).toBe("4");
    expect(loom).not.toContain("participants");
    expect(loom).not.toContain("matched");
  });

  it("asks for the corpus with no query and no cursor", async () => {
    handler = buildHandler;
    await mountApp("/");
    await waitFor(() => expect(calls.some((c) => pathOf(c) === "/v1/search")).toBe(true));

    const req = paramsOf(calls.find((c) => pathOf(c) === "/v1/search")!);
    expect(req.has("q")).toBe(false);
    expect(req.get("limit")).toBe("50");
    // An empty cursor is the first page: nothing is excluded.
    expect(req.get("before") ?? "").toBe("");
  });

  it("expands the nav's own box into the search, and leaves the list where it is", async () => {
    handler = buildHandler;
    const router = await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    const box = screen.getByRole("textbox", { name: "Search the corpus" });
    fireEvent.focus(box);

    // The options are the nav's now, and only while it is open: the box is the
    // whole of the search shut, and the four things a search is when it is not.
    expect(await screen.findByLabelText("Mode")).toBeTruthy();
    expect(screen.getByLabelText("Person")).toBeTruthy();
    // A date field, because `since` is a date on the wire: the browser's own
    // picker rather than a string the corpus has to interpret.
    const since = screen.getByLabelText("Since") as HTMLInputElement;
    expect(since.type).toBe("date");
    // Opening it asks nothing: no address was written, so the list below is the
    // list that was there — the default view, not a search page with nothing on
    // it. This is what makes an empty box harmless.
    expect(screen.getByText("Loom cutover schedule")).toBeTruthy();
    expect(document.querySelector(".ibwrap")).toBeTruthy();
    expect(router.state.location.searchStr).toBe("");
    expect(screen.queryByText("No thread matched.")).toBeNull();
  });

  it("folds the panel away when the empty box loses the caret", async () => {
    handler = buildHandler;
    await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    const box = screen.getByRole("textbox", { name: "Search the corpus" });
    fireEvent.focus(box);
    await screen.findByLabelText("Mode");

    // Blurred with nothing in it, the panel goes: an empty box is nothing being
    // searched, so what is left is the nav it was covering.
    fireEvent.blur(box);
    await waitFor(() => expect(screen.queryByLabelText("Mode")).toBeNull());
    expect(screen.getByRole("textbox", { name: "Search the corpus" })).toBeTruthy();
  });

  it("keeps the panel open while the caret is inside it, and while it asks something", async () => {
    handler = buildHandler;
    const router = await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    // Reaching for the mode dropdown is not leaving the search, so the panel
    // stays open across the move — the blur that shut it would otherwise take the
    // field away from whoever had just decided how to ask.
    const box = screen.getByRole("textbox", { name: "Search the corpus" });
    fireEvent.focus(box);
    const mode = await screen.findByLabelText("Mode");
    fireEvent.blur(box, { relatedTarget: mode });
    expect(screen.getByLabelText("Mode")).toBeTruthy();

    // And typing a query and clicking away searches: what was typed is committed
    // as the caret leaves, which is the whole gesture — no button to find first.
    fireEvent.change(box, { target: { value: "cutover" } });
    fireEvent.blur(mode);
    await waitFor(() => expect(router.state.location.searchStr).toBe("?q=cutover"));
    // The list below is the answer to what was just typed: the search page, with
    // the candidates the corpus answered with.
    await waitFor(() => expect(document.querySelector(".selwrap")).toBeTruthy());
    await screen.findByText("Fence panels", { selector: ".ibsubj" });
    // The panel stays. A question in force is what its options are for, and one
    // that folded itself away would put them behind another click to narrow the
    // search that is on screen.
    expect(screen.getByLabelText("Mode")).toBeTruthy();
    expect((box as HTMLInputElement).value).toBe("cutover");

    // Emptying it is the way out that is not Escape: with nothing being asked,
    // the panel goes and the nav has its row back.
    fireEvent.change(box, { target: { value: "" } });
    fireEvent.blur(box);
    await waitFor(() => expect(screen.queryByLabelText("Mode")).toBeNull());
  });

  it("clears the box and the question with Escape, and folds the panel away", async () => {
    handler = buildHandler;
    const router = await mountApp("/?q=cutover");
    await screen.findByText("Fence panels", { selector: ".ibsubj" });

    const box = screen.getByRole("textbox", { name: "Search the corpus" });
    fireEvent.focus(box);
    await screen.findByLabelText("Mode");

    // Escape in a field empties it, and this is the field that shows the question
    // in force — so the question goes too, rather than being dropped from the
    // draft and turning up in the box again the moment the panel shuts. Nothing
    // is asked, so what is below is the default view rather than a search page
    // with nothing on it.
    fireEvent.change(box, { target: { value: "cutover plum" } });
    fireEvent.keyDown(box, { key: "Escape" });
    await waitFor(() => expect(screen.queryByLabelText("Mode")).toBeNull());
    expect((box as HTMLInputElement).value).toBe("");
    await waitFor(() => expect(router.state.location.searchStr).toBe(""));
    expect(await screen.findByText("Loom cutover schedule")).toBeTruthy();

    // And the box opens again on a click: Escape shuts the panel without moving
    // the caret, so a box that answered only to focus would be one gesture short.
    fireEvent.click(box);
    await screen.findByLabelText("Mode");

    // Escape in a box that asks nothing is only a way out: the address is left
    // exactly as it was, folder and open thread included.
    fireEvent.keyDown(box, { key: "Escape" });
    await waitFor(() => expect(screen.queryByLabelText("Mode")).toBeNull());
    expect(router.state.location.searchStr).toBe("");
  });

  it("says on the body that this page is a workspace, and takes it off again", async () => {
    handler = buildHandler;
    const router = await mountApp("/");
    await screen.findByText("Loom cutover schedule");
    // The layout itself is a browser's business — jsdom lays nothing out, and the
    // height thread was checked in Chromium — but who claims the class, and that it
    // is given back, is this page's own promise.
    expect(document.body.classList.contains("inbox")).toBe(true);
    await act(async () => {
      router.navigate({ to: "/specs" });
    });
    await waitFor(() => expect(document.body.classList.contains("inbox")).toBe(false));
  });

  it("starts with the pane empty, and fills it from the row that is clicked", async () => {
    handler = buildHandler;
    await mountApp("/");
    await screen.findByRole("button", { name: "Fence panels" });

    // A pane beside the list, not a modal over it: nothing was opened, and the
    // list is still there to be read.
    expect(screen.queryByRole("dialog")).toBeNull();
    // Nothing is open until something is chosen. No row is current, the pane
    // holds no thread, and it says so rather than filling itself in with the top
    // of the list — that would be a thread the reader has to dismiss.
    expect(
      screen.getByRole("button", { name: "Fence panels" }).getAttribute("aria-current"),
    ).toBeNull();
    expect(within(pane()).getByText(/Nothing open/)).toBeTruthy();
    expect(pane().querySelector(".msg")).toBeNull();

    click(screen.getByRole("button", { name: "Fence panels" }));
    await waitFor(() =>
      expect(within(pane()).getByText(/and the gate needs a new hinge/)).toBeTruthy(),
    );
    // The top row is what the pane is showing, and it says so.
    expect(
      screen.getByRole("button", { name: "Fence panels" }).getAttribute("aria-current"),
    ).toBe("true");
  });

  it("keeps the row open when it is clicked again, and closes on the pane's own Back", async () => {
    // A row's click is the opening, and it is the same thing however many times it
    // is made: that is what lets a double click mark the row without the pane
    // appearing and disappearing under it (see ThreadRow). Closing is the pane's
    // own way out — its "← List" — which is a click the reader makes in the pane
    // they want shut.
    handler = buildHandler;
    const router = await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    click(screen.getByRole("button", { name: "Loom cutover schedule" }));
    await waitFor(() =>
      expect(within(pane()).getByText(/Roof access is fine from the 14th/)).toBeTruthy(),
    );

    click(screen.getByRole("button", { name: "Loom cutover schedule" }));
    await new Promise((r) => setTimeout(r, 30));
    expect(pane().querySelector(".msg")).not.toBeNull();
    expect(router.state.location.search.open).toBe("mail:<loom-cutover-1@example.fed>");

    click(within(pane()).getByRole("button", { name: "← List" }));
    await waitFor(() => expect(pane().querySelector(".msg")).toBeNull());
    // The address lets go of it as well: closing is a state a reload lands on, the
    // same way opening is.
    expect(router.state.location.search.open).toBeUndefined();
    expect(
      screen.getByRole("button", { name: "Loom cutover schedule" }).getAttribute("aria-current"),
    ).toBeNull();
    expect(within(pane()).getByText(/Nothing open/)).toBeTruthy();

    // And clicking a different row still opens that one.
    click(screen.getByRole("button", { name: "Fence panels" }));
    await waitFor(() =>
      expect(within(pane()).getByText(/and the gate needs a new hinge/)).toBeTruthy(),
    );
  });

  it("swaps the pane when another row is clicked", async () => {
    handler = buildHandler;
    await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    click(screen.getByRole("button", { name: "Loom cutover schedule" }));
    await waitFor(() =>
      expect(within(pane()).getByText(/Roof access is fine from the 14th/)).toBeTruthy(),
    );
    // The previous thread is gone from the pane, and one row is current at a
    // time — the row is the pane's, and the pane is the row's.
    expect(within(pane()).queryByText(/and the gate needs a new hinge/)).toBeNull();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(
      (await screen.findByRole("button", { name: "Loom cutover schedule" })).getAttribute(
        "aria-current",
      ),
    ).toBe("true");
    expect(screen.getByRole("button", { name: "Fence panels" }).getAttribute("aria-current")).toBeNull();
  });

  it("writes the open thread into the address, so a reload lands on it", async () => {
    handler = buildHandler;
    const router = await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    click(screen.getByRole("button", { name: "Loom cutover schedule" }));
    await waitFor(() =>
      expect(router.state.location.search).toMatchObject({
        open: "mail:<loom-cutover-1@example.fed>",
      }),
    );
  });

  it("opens the thread the address names, without a click", async () => {
    handler = buildHandler;
    await mountApp(`/?open=${encodeURIComponent("mail:<loom-cutover-1@example.fed>")}`);

    // The pane is the address's, not the newest row's: what was open when the
    // page was left is what is open on the way back in.
    await waitFor(() =>
      expect(within(pane()).getByText(/Roof access is fine from the 14th/)).toBeTruthy(),
    );
    expect(within(pane()).queryByText(/and the gate needs a new hinge/)).toBeNull();
    expect(
      (await screen.findByRole("button", { name: "Loom cutover schedule" })).getAttribute(
        "aria-current",
      ),
    ).toBe("true");
  });

  it("scrolls the list to the row the address names, once", async () => {
    handler = buildHandler;
    // jsdom has no scrollIntoView, and the list asks for one either way: which
    // row it asks for is the whole claim. The pane scrolls too (to the message it
    // lands on), so what is collected is the row's own root id.
    const scrolled: string[] = [];
    const orig = Element.prototype.scrollIntoView;
    Element.prototype.scrollIntoView = function () {
      scrolled.push((this as HTMLElement).dataset.root ?? this.id);
    };
    try {
      await mountApp(`/?open=${encodeURIComponent("mail:<loom-cutover-1@example.fed>")}`);
      const row = (await screen.findByRole("button", { name: "Loom cutover schedule" })).closest(
        ".ibrow",
      );
      expect(row?.getAttribute("data-root")).toBe("mail:<loom-cutover-1@example.fed>");
      await waitFor(() => expect(scrolled).toContain("mail:<loom-cutover-1@example.fed>"));
      // Once, and not again as the list re-renders: a reader who scrolls away from
      // the row they opened is not asking to be pulled back to it.
      expect(scrolled.filter((s) => s === "mail:<loom-cutover-1@example.fed>")).toHaveLength(1);
    } finally {
      Element.prototype.scrollIntoView = orig;
    }
  });

  it("lands on the newest message of the thread it opens, and marks it", async () => {
    handler = buildHandler;
    // jsdom has no scrollIntoView, and the pane asks for one either way: which
    // element it asks for is the whole claim, and a landing that cannot scroll
    // is still a landing.
    const scrolled: string[] = [];
    const orig = Element.prototype.scrollIntoView;
    Element.prototype.scrollIntoView = function () {
      scrolled.push(this.id);
    };
    try {
      await mountApp(`/?open=${encodeURIComponent(MULTI_ROOT)}`);
      await waitFor(() => expect(pane().querySelector(".msg.landed")).not.toBeNull());
      // The newest message is the one the row would have summarised, so it is the
      // one marked and the one scrolled to — not the top of the thread.
      expect(pane().querySelector(".msg.landed .nm")?.textContent).toBe("Bo Halvorsen");
      expect(scrolled).toEqual(["entry-1"]);
      // Landing is where the pane opens, not a filter on what it draws: the rest
      // of the thread is still there, above it.
      expect(within(pane()).getByText(/Can you quote the north shed/)).toBeTruthy();
    } finally {
      Element.prototype.scrollIntoView = orig;
    }
  });

  it("opens a thread the loaded page does not hold, and claims nothing about it", async () => {
    // Only the loom thread is in this page of the list, which is the position a
    // reader is in when an old thread is opened, then reloaded: the row is not
    // there to be found. The pane reads the thread from its id — the only thing
    // the address carries — so the head claims no subject or count it cannot
    // know, and what the thread says is read from the thread itself.
    handler = (c) =>
      pathOf(c).startsWith("/v1/chains/")
        ? chainHandler(c)
        : pathOf(c) === "/v1/search"
          ? pageOf([CHAINS[1]!])
          : buildHandler(c);
    await mountApp(`/?open=${encodeURIComponent("mail:<fence-panel-9@example.fed>")}`);

    await waitFor(() =>
      expect(within(pane()).getByText(/and the gate needs a new hinge/)).toBeTruthy(),
    );
    const head = document.querySelector(".ibread-head")!.textContent ?? "";
    expect(head).toContain("(no subject)");
    expect(head).not.toMatch(/entr(y|ies)/);
    // The one row is the loom thread, and it is not the one open.
    expect(
      (await screen.findByRole("button", { name: "Loom cutover schedule" })).getAttribute(
        "aria-current",
      ),
    ).toBeNull();
  });

  it("clears the address when the list is asked for again", async () => {
    handler = buildHandler;
    const router = await mountApp(`/?open=${encodeURIComponent("mail:<loom-cutover-1@example.fed>")}`);
    await screen.findByRole("button", { name: "Loom cutover schedule" });

    click(within(pane()).getByRole("button", { name: /List/ }));
    await waitFor(() => expect(router.state.location.search).not.toMatchObject({ open: expect.anything() }));

    // Back to nothing open: the list is a list, and the pane is not showing a
    // thread nobody chose.
    await waitFor(() => expect(pane().querySelector(".msg")).toBeNull());
    expect(within(pane()).getByText(/Nothing open/)).toBeTruthy();
  });

  it("draws the thread with the transcript's own message component", async () => {
    handler = buildHandler;
    await mountApp("/");
    await screen.findByText("Fence panels");
    click(screen.getByRole("button", { name: "Fence panels" }));

    // The page's classes, not the pane's own: one component draws a message in
    // both places, so the bubble, the header and its receipt are the same boxes.
    await waitFor(() => expect(pane().querySelector(".msg .bub")).not.toBeNull());
    expect(pane().querySelector(".msg .hdr .nm")?.textContent).toBe("Ada Okoye");
    // And the body is the service's rendered html, not the plain text: the
    // fixture's two differ by a <b> that only the html has.
    expect(pane().querySelector(".bd b")?.textContent).toBe("Regards, Ada");
    expect(pane().querySelector(".bd")?.textContent).not.toContain("<b>");
    // The recipient line the message itself stated. It is the same receipt the
    // page prints, filled from the thread read rather than left as "to —".
    expect(pane().querySelector(".msg .hdet .to")?.textContent).toBe("to Bo Halvorsen, cc Cy Okafor");
    // The subject this message carried, in the same receipt: the thread's own is
    // in the head, and the entry's is the only place a renamed thread is stated.
    expect(pane().querySelector(".msg .hdet .subj")?.textContent).toBe("Fence panels");
  });

  it("names the sender's address on hover, and only where the entry has one", async () => {
    handler = buildHandler;
    await mountApp("/?open=mail%3A%3Cfence-panel-9%40example.fed%3E");
    await screen.findByText("Fence panels", { selector: ".ibsubj" });

    // The name and the avatar both say who this is: "Ada Okoye" alone is a name
    // the reader cannot check against anything.
    const named = await waitFor(() => {
      const el = pane().querySelector(".msg .hdr .nm");
      if (el?.getAttribute("title") !== "Ada Okoye <ada@okoye.example>") {
        throw new Error(`the name still reads ${JSON.stringify(el?.getAttribute("title"))}`);
      }
      return el;
    });
    expect(named.parentElement?.querySelector(".av")?.getAttribute("title")).toBe(
      "Ada Okoye <ada@okoye.example>",
    );
  });

  it("names who quoted a recovered message, which has no address of its own", async () => {
    handler = buildHandler;
    await mountApp("/?open=quote%3A9f2c1ab4e77d");

    await waitFor(() => expect(pane().querySelector(".msg .hdr .nm")?.textContent).toBe("Dana Reyes"));
    // The absence, and where the address that is offered came from: the quoter's
    // address is not the sender's, and a hover that showed it bare would
    // attribute it to Dana. The direct bubble is unchanged (the test above),
    // because there the entry does have an address of its own to name.
    const title = "Dana Reyes (address unknown; quoted by Ada Okoye <ada@okoye.example>)";
    expect(pane().querySelector(".msg .hdr .nm")?.getAttribute("title")).toBe(title);
    // The avatar says the same thing: it is one person, and two ways of naming
    // them would be two answers to one question.
    expect(pane().querySelector(".msg .hdr .av")?.getAttribute("title")).toBe(title);
  });

  it("says a recovered entry named no recipients instead of leaving the gap dark", async () => {
    handler = buildHandler;
    await mountApp("/?open=quote%3A9f2c1ab4e77d");

    await waitFor(() => expect(pane().querySelector(".msg .bub")).not.toBeNull());
    expect(pane().querySelector(".msg .hdet .to")?.textContent).toBe("to —");
  });

  it("shows where each message was found, in the receipt a built page prints", async () => {
    handler = buildHandler;
    await mountApp(`/?open=${encodeURIComponent(MULTI_ROOT)}`);
    await waitFor(() => expect(pane().querySelectorAll(".msg").length).toBe(2));

    // The id in the receipt is the one thing a reader holds when they want to
    // find the message somewhere else, and a built page has always printed it —
    // the pane is the same reader looking at the same message.
    const line = pane().querySelectorAll(".msg .hdet .src");
    expect(line[0]?.textContent).toBe("msg 19fee08b9d28e28b");
    // It opens the mailbox copy, which is where the id came from.
    expect(line[0]?.querySelector("a")?.getAttribute("href")).toBe(
      "https://mail.google.com/mail/u/0/#all/19fee08b9d28e28b",
    );
    // A recovered message has no id of its own, so the receipt names the message
    // it was unspooled from — and links to the row for it on this page rather
    // than sending the reader out to the mailbox for something already here.
    expect(line[1]?.textContent?.trim()).toBe("unspooled from msg 19fee08b9d28e28b");
    expect(line[1]?.querySelector("a")?.getAttribute("href")).toBe("#entry-0");
    expect(document.getElementById("entry-0")).not.toBeNull();
  });

  it("links each message to the one it replies to, as a built page does", async () => {
    handler = buildHandler;
    await mountApp(`/?open=${encodeURIComponent(MULTI_ROOT)}`);
    await waitFor(() => expect(pane().querySelectorAll(".msg").length).toBe(2));

    // The opener answers nothing, and the line says so rather than being left
    // out: a head with no reply mark on it reads as a rendering failure.
    const tails = pane().querySelectorAll(".msg .hdr .htail");
    expect(tails[0]?.querySelector(".par")).toBeNull();
    expect(tails[0]?.querySelector(".tstart")?.textContent).toBe("thread start");

    // The reply names what it answers, in the words that message's own bubble
    // wears — its sender and its clock, in the same form the bubble prints it —
    // and points at the row for it here.
    const par = tails[1]?.querySelector(".par");
    expect(par?.getAttribute("href")).toBe("#entry-0");
    expect(par?.querySelector(".parlbl")?.textContent).toBe(
      "in reply to Ada Okoye, Mon, 2 Mar 2026 19:15",
    );
    expect(par?.getAttribute("title")).toBe("In reply to Ada Okoye, Mon, 2 Mar 2026 19:15");
    // The anchor it names is the parent's own bubble, so the link lands on it.
    expect(document.getElementById("entry-0")?.textContent).toContain("Ada Okoye");
  });

  it("drops every tick on Escape, and leaves the box's own Escape to the box", async () => {
    // The ticks are built up by clicking rows, and the bar's Deselect all is the
    // only way out of them otherwise: a control that appears with the selection and
    // has to be found by eye. Escape is what the page's other open things already
    // answer — and the box and the folder menu are skipped, so a reader who had
    // ticked rows and then changed their mind about a search loses the search and
    // not the selection.
    handler = buildHandler;
    await mountApp("/");
    await screen.findByRole("button", { name: "Fence panels" });

    click(screen.getByLabelText("Select Fence panels"));
    click(screen.getByLabelText("Select Loom cutover schedule"));
    await screen.findByRole("button", { name: "Deselect all" });

    fireEvent.keyDown(document.body, { key: "Escape" });
    await waitFor(() => expect(document.querySelector(".ibbuild")).toBeNull());
    expect((screen.getByLabelText("Select Loom cutover schedule") as HTMLInputElement).checked).toBe(false);

    // Ticked again, and the caret put in the search box: its Escape closes the box
    // and the ticks stay where they are.
    click(screen.getByLabelText("Select Loom cutover schedule"));
    await screen.findByRole("button", { name: "Deselect all" });
    const box = screen.getByLabelText("Search the corpus");
    click(box);
    fireEvent.keyDown(box, { key: "Escape" });
    expect((screen.getByLabelText("Select Loom cutover schedule") as HTMLInputElement).checked).toBe(true);
    expect(screen.getByRole("button", { name: "Deselect all" })).toBeTruthy();
  });

  it("builds a page from the ticked chains, recording no query for it", async () => {
    handler = buildHandler;
    const router = await mountApp("/");
    await screen.findByRole("button", { name: "Fence panels" });

    click(screen.getByLabelText("Select Fence panels"));
    await braid();
    await waitFor(() => expect(calls.some((c) => pathOf(c) === "/v1/spec")).toBe(true));

    const post = JSON.parse(calls.find((c) => pathOf(c) === "/v1/spec")!.body!);
    expect(post.chains).toEqual(["mail:<fence-panel-9@example.fed>"]);
    // No title was typed, so the page takes a clock name rather than a subject's
    // slug — two untitled builds must not overwrite each other.
    expect(post.name).toMatch(/^spec-/);
    // Nothing found these chains: recording a query would have refresh proposing
    // threads nobody searched for.
    expect(post.queries).toBeUndefined();
    await waitFor(() => expect(router.state.location.pathname).toBe(`/view/${post.name}`));
  });
});

describe("paging the inbox", () => {
  // Fifty chains fill a page (the client asks for 50), and the second page
  // repeats the boundary thread: the cursor includes its own second, so the
  // alternative is losing a thread that ends beside it.
  const firstPage = Array.from({ length: 50 }, (_, i) =>
    thread({
      rootExtId: `mail:<page-one-${i}@example.fed>`,
      subject: `Thread ${i + 1}`,
      first: `2026-05-${String(50 - i).padStart(2, "0")}T09:00:00Z`,
      last: `2026-05-${String(50 - i).padStart(2, "0")}T09:00:00Z`,
      best: [
        entry({
          extId: `mail:<page-one-${i}@example.fed>`,
          ts: `2026-05-${String(50 - i).padStart(2, "0")}T09:00:00Z`,
          person: "Ada Okoye",
          snippet: `Message ${i + 1}.`,
        }),
      ],
    }),
  );

  const boundary = firstPage[firstPage.length - 1]!;
  const secondPage = [
    boundary,
    thread({
      rootExtId: "mail:<page-two-older@example.fed>",
      subject: "An older thread",
      first: "2026-04-02T09:00:00Z",
      last: "2026-04-02T09:00:00Z",
      best: [
        entry({
          extId: "mail:<page-two-older@example.fed>",
          ts: "2026-04-02T09:00:00Z",
          person: "Bo Halvorsen",
          snippet: "The last thing in the corpus.",
        }),
      ],
    }),
  ];

  it("asks for what is older with the cursor, and shows a repeated thread once", async () => {
    handler = withChains((c) => {
      if (pathOf(c) !== "/v1/search") return json(500, { error: "unexpected" });
      return paramsOf(c).get("before") ? pageOf(secondPage) : pageOf(firstPage);
    });
    await mountApp("/");
    await waitFor(() => expect(screen.getAllByRole("checkbox")).toHaveLength(50));

    await act(async () => scrollToEnd());
    await screen.findByText("An older thread");

    const asked = calls.filter((c) => pathOf(c) === "/v1/search");
    expect(asked).toHaveLength(2);
    expect(paramsOf(asked[1]!).get("before")).toBe(boundary.last);

    // 50 + 1 new, not 52: the repeated thread is one row, keyed on its root.
    expect(screen.getAllByRole("checkbox")).toHaveLength(51);
    expect(screen.getAllByText("Thread 50")).toHaveLength(1);
    // A short page is the end of the corpus, so there is nothing left to watch.
    expect(document.querySelector(".ibend")).toBeNull();
  });

  // A thread whose entries straddle the cursor is returned again on the next page
  // (its `last` is the whole thread's newest message, not the newest inside the
  // window), so a page can arrive having added nothing. Measured against the live
  // corpus: two hundred rows held 194 distinct threads. Asking forever after that
  // is how a "Load older" button becomes a spinner.
  it("stops asking when a page adds nothing the list has not already shown", async () => {
    handler = withChains((c) =>
      pathOf(c) === "/v1/search" ? pageOf(firstPage) : json(500, { error: "unexpected" }),
    );
    await mountApp("/");
    await waitFor(() => expect(screen.getAllByRole("checkbox")).toHaveLength(50));

    await act(async () => scrollToEnd());
    await waitFor(() => expect(document.querySelector(".ibend")).toBeNull());
    // The second page was fetched, and then the asking stopped: the list is the
    // same fifty threads, not a hundred rows of them.
    expect(calls.filter((c) => pathOf(c) === "/v1/search")).toHaveLength(2);
    expect(screen.getAllByRole("checkbox")).toHaveLength(50);
  });

  // A page that failed is the one case automatic paging must not keep firing:
  // the marker stays where it was, so asking again on every render would be a
  // request loop nobody reads the answer to. The reader gets the error, and the
  // button comes back for the one case the automatic path cannot handle.
  it("stops watching the end when a page fails, and offers the retry", async () => {
    let second = 0;
    handler = (c) => {
      // The pane asks for whichever thread is on screen; empty is enough here, and
      // an answer keeps its own failure out of the alert this test is reading.
      if (pathOf(c).startsWith("/v1/chains/")) return json(200, { entries: [] });
      if (pathOf(c) !== "/v1/search") return json(500, { error: "unexpected" });
      if (!paramsOf(c).get("before")) return pageOf(firstPage);
      second += 1;
      return json(503, { error: "the corpus is busy" });
    };
    await mountApp("/");
    await waitFor(() => expect(screen.getAllByRole("checkbox")).toHaveLength(50));

    await act(async () => scrollToEnd());
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toMatch(/the corpus is busy/);
    expect(second).toBe(1);

    // Nothing is watching the end any more, so scrolling it into view again asks
    // nothing, and the retry is the reader's to make.
    await act(async () => scrollToEnd());
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeNull();
    expect(calls.filter((c) => pathOf(c) === "/v1/search")).toHaveLength(2);
  });
});

describe("how a row writes its date", () => {
  // Local times, built as local times: the function answers in the reader's own
  // clock, so a test that pinned UTC strings would be asserting the machine's
  // timezone as much as the row.
  const now = new Date(2026, 8, 16, 15, 0);
  const at = (y: number, m: number, d: number, h: number, min: number) =>
    new Date(y, m, d, h, min).toISOString();

  it("is a clock for today, and 'Yesterday' for yesterday", () => {
    // 24-hour, and not the machine's idea of a clock: an en-US browser printed
    // "2:30 PM" here for a time every other surface prints as 14:30.
    expect(whenShort(at(2026, 8, 16, 14, 30), now)).toBe("14:30");
    // Midnight is 00:05, not "12:05 AM", and not 24:05.
    expect(whenShort(at(2026, 8, 16, 0, 5), now)).toBe("00:05");
    expect(whenShort(at(2026, 8, 15, 14, 30), now)).toBe("Yesterday");
  });

  it("is a day and month inside the year, and names the year outside it", () => {
    expect(whenShort(at(2026, 2, 11, 9, 0), now)).toBe("11 Mar");
    expect(whenShort(at(2025, 2, 11, 9, 0), now)).toBe("11 Mar 2025");
  });

  it("says nothing at all when there is no timestamp", () => {
    expect(whenShort(undefined, now)).toBe("");
  });
});

/**
 * The folder button above the list. The folders are the mailbox's own labels —
 * the list is what Gmail filed the mail under, so the tests answer with labels
 * and counts and assert that those are what is shown; nothing here may invent a
 * folder or a number.
 */
describe("the folder button", () => {
  const FOLDER_PAGE: Handler = withChains((c) => {
    const p = pathOf(c);
    if (p === "/v1/labels") {
      return json(200, {
        labels: [
          { name: "INBOX", messages: 2 },
          { name: "CATEGORY_PROMOTIONS", messages: 1 },
          { name: "SENT", messages: 1 },
        ],
      });
    }
    if (p === "/v1/settings") {
      const body = c.body ? (JSON.parse(c.body) as { defaultFolder?: string }) : {};
      return json(200, body.defaultFolder ? body : {});
    }
    if (p === "/v1/search") {
      switch (paramsOf(c).get("label")) {
        case "INBOX":
          return pageOf([CHAINS[0]]);
        case "SENT":
          return pageOf([CHAINS[1]]);
        case "RECEIPTS":
          return pageOf([]);
        default:
          return pageOf(CHAINS);
      }
    }
    if (p === "/auth/status") return json(200, { signed_in: true });
    return json(500, { error: `unexpected call to ${c.method} ${p}` });
  });

  it("says which folder the list is in, and opens the mailbox's own list", async () => {
    handler = FOLDER_PAGE;
    await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    // Nothing chosen is every folder at once, and it says so rather than naming
    // one the reader never picked.
    const button = screen.getByRole("button", { name: /All mail/ });
    expect(screen.queryByRole("menu")).toBeNull();

    click(button);
    const menu = await screen.findByRole("menu");
    // The mailbox's labels with the mailbox's counts, and the button's own
    // "All mail" above them as the way back out.
    expect(menu.textContent).toContain("INBOX");
    expect(menu.textContent).toContain("CATEGORY_PROMOTIONS");
    const inboxRow = within(menu).getByRole("menuitem", { name: /INBOX/ });
    expect(inboxRow.textContent).toContain("2");
    expect(within(menu).getByRole("menuitem", { name: "All mail" })).not.toBeNull();
  });

  it("filters the list when a folder is picked, and keeps the folder in the address", async () => {
    handler = FOLDER_PAGE;
    const router = await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    click(screen.getByRole("button", { name: /All mail/ }));
    click(await screen.findByRole("menuitem", { name: /SENT/ }));

    // The filter is the address's, so a reload or a sent link comes back to the
    // folder rather than to the whole mailbox.
    await waitFor(() => expect(router.state.location.searchStr).toBe("?label=SENT"));
    const asked = calls.filter((c) => pathOf(c) === "/v1/search");
    expect(paramsOf(asked[asked.length - 1]!).get("label")).toBe("SENT");
    // And the list is the folder's: the outbound thread, not the inbox one.
    await waitFor(() => expect(screen.getAllByRole("checkbox")).toHaveLength(1));
    expect(document.querySelector(".ibrow")!.textContent).toContain("Loom cutover schedule");
  });

  it("opens the folder the address names, without a click", async () => {
    handler = FOLDER_PAGE;
    await mountApp("/?label=INBOX");
    // The row, not the pane's heading: both name the subject, and only one of
    // them is the list.
    await waitFor(() => expect(document.querySelectorAll(".ibrow")).toHaveLength(1));
    expect(screen.getByRole("button", { name: /INBOX/ })).not.toBeNull();
    const asked = calls.filter((c) => pathOf(c) === "/v1/search");
    expect(paramsOf(asked[0]!).get("label")).toBe("INBOX");
  });

  it("says a folder is empty rather than that the corpus is", async () => {
    handler = FOLDER_PAGE;
    await mountApp("/?label=RECEIPTS");
    const note = await screen.findByText(/Nothing in RECEIPTS/);
    expect(note.textContent).not.toContain("slurp");
  });

  it("closes on Escape, so the reader is not stuck behind it", async () => {
    handler = FOLDER_PAGE;
    await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    click(screen.getByRole("button", { name: /All mail/ }));
    await screen.findByRole("menu");
    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
  });
});

/**
 * The default folder: the one chainmail opens in, stored server-side rather than
 * in the browser because the reader has more than one browser. The handler below
 * keeps the setting the way the server does — the write answers with the state,
 * and an empty folder is stored as no default at all.
 */
describe("the default folder", () => {
  const foldersHandler = (initial: string): Handler => {
    let stored = initial;
    return withChains((c) => {
      const p = pathOf(c);
      if (p === "/v1/settings") {
        if (c.method === "POST") {
          stored = (JSON.parse(c.body ?? "{}") as { defaultFolder?: string }).defaultFolder ?? "";
        }
        return json(200, stored ? { defaultFolder: stored } : {});
      }
      if (p === "/v1/labels") return json(200, { labels: [{ name: "INBOX", messages: 1 }] });
      if (p === "/v1/search") {
        return paramsOf(c).get("label") === "INBOX" ? pageOf([CHAINS[0]]) : pageOf(CHAINS);
      }
      if (p === "/auth/status") return json(200, { signed_in: true });
      return json(500, { error: `unexpected call to ${c.method} ${p}` });
    });
  };

  it("opens the list in the reader's own folder", async () => {
    handler = foldersHandler("INBOX");
    await mountApp("/");
    // A row that only INBOX holds, so "the default applied" is a claim about
    // what is on screen rather than about a request being made.
    await waitFor(() => expect(document.querySelectorAll(".ibrow")).toHaveLength(1));

    expect(screen.getByRole("button", { name: /INBOX/ })).not.toBeNull();
    const asked = calls.filter((c) => pathOf(c) === "/v1/search");
    expect(paramsOf(asked[0]!).get("label")).toBe("INBOX");
  });

  it("waits for the setting before asking, rather than asking twice", async () => {
    handler = foldersHandler("INBOX");
    await mountApp("/");
    await waitFor(() => expect(document.querySelectorAll(".ibrow")).toHaveLength(1));

    // The default is read first and the list is asked once, for the folder. A
    // filter that arrives after the first page is a correction to it, and the
    // reader watches the list change under them.
    const asked = calls.filter((c) => pathOf(c) === "/v1/search");
    expect(asked).toHaveLength(1);
  });

  it("shows the folder it is in as the default, and turns it off", async () => {
    handler = foldersHandler("INBOX");
    await mountApp("/");
    await waitFor(() => expect(document.querySelectorAll(".ibrow")).toHaveLength(1));

    click(screen.getByRole("button", { name: /INBOX/ }));
    const on = await screen.findByRole("menuitemcheckbox", { name: "Open INBOX by default" });
    expect(on.getAttribute("aria-checked")).toBe("true");

    click(on);
    // The setting is the effect, not the request: an empty folder means no
    // default, which is All mail — so the list loses its filter as well.
    await waitFor(() => expect(document.querySelectorAll(".ibrow")).toHaveLength(2));
    const posts = calls.filter((c) => c.method === "POST" && pathOf(c) === "/v1/settings");
    expect(JSON.parse(posts[posts.length - 1]!.body!)).toEqual({ defaultFolder: "" });
    expect(screen.getByRole("button", { name: /All mail/ })).not.toBeNull();
  });

  it("makes the folder the reader is in the default", async () => {
    handler = foldersHandler("");
    await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    // Nothing chosen opens in All mail, and the control says so rather than
    // leaving the question unanswered.
    click(screen.getByRole("button", { name: /All mail/ }));
    const all = await screen.findByRole("menuitemcheckbox", { name: "Open All mail by default" });
    expect(all.getAttribute("aria-checked")).toBe("true");

    // Go into a folder, then make it where chainmail opens.
    click(await screen.findByRole("menuitem", { name: /INBOX/ }));
    await waitFor(() => expect(document.querySelectorAll(".ibrow")).toHaveLength(1));
    click(screen.getByRole("button", { name: /INBOX/ }));
    click(await screen.findByRole("menuitemcheckbox", { name: "Open INBOX by default" }));

    await waitFor(() => {
      const posts = calls.filter((c) => c.method === "POST" && pathOf(c) === "/v1/settings");
      expect(posts).toHaveLength(1);
      expect(JSON.parse(posts[0]!.body!)).toEqual({ defaultFolder: "INBOX" });
    });
    // And it is now the folder the page opens in: the control agrees.
    expect(
      (await screen.findByRole("menuitemcheckbox", { name: "Open INBOX by default" }))
        .getAttribute("aria-checked"),
    ).toBe("true");
  });

  it("lets the address override the default, including 'All mail'", async () => {
    handler = foldersHandler("INBOX");
    // An empty label is a choice of its own: every folder, deliberately. It is
    // not the same as saying nothing, which is what the default is for.
    await mountApp("/?label=");
    await waitFor(() => expect(document.querySelectorAll(".ibrow")).toHaveLength(2));

    expect(screen.getByRole("button", { name: /All mail/ })).not.toBeNull();
    const asked = calls.filter((c) => pathOf(c) === "/v1/search");
    expect(paramsOf(asked[0]!).get("label")).toBeNull();
    // The default is still INBOX and the reader has stepped outside it, so the
    // control is unticked: chainmail does not open where they are.
    click(screen.getByRole("button", { name: /All mail/ }));
    const here = await screen.findByRole("menuitemcheckbox", { name: "Open All mail by default" });
    expect(here.getAttribute("aria-checked")).toBe("false");
  });
});

/**
 * Resizing the two panels by dragging the border between them.
 *
 * jsdom lays nothing out — every rect is zero — so the splitter's arithmetic has
 * no numbers to work with unless a test supplies them. The stub answers by
 * class, which is what the splitter itself reads: the split's width is what the
 * pane is left with, and the column's is where the arrow keys start.
 */
// The rects the splitter reads, supplied by hand: jsdom lays nothing out. The
// stub answers by class, which is what the component itself asks — the split's
// width is what the pane is left with, and the column's is where the border is.
const WIDE = 1200;
const MIN = 15 * 16; // the list's floor
const MOST = WIDE - 26 * 16; // and what leaves the pane its own minimum
let restoreRects: (() => void) | null;

const stubLayout = (column = 300) => {
  let list = column;
  const real = Element.prototype.getBoundingClientRect;
  const box = (w: number) =>
    ({
      x: 0,
      y: 0,
      left: 0,
      top: 0,
      right: w,
      bottom: 100,
      width: w,
      height: 100,
      toJSON: () => ({}),
    }) as DOMRect;
  Element.prototype.getBoundingClientRect = function (this: Element) {
    if (this.classList?.contains("ibsplit")) return box(WIDE);
    if (this.classList?.contains("ibcol")) return box(list);
    return real.call(this);
  };
  restoreRects = () => {
    Element.prototype.getBoundingClientRect = real;
    restoreRects = null;
  };
  // The grid can hand the column a different width at any moment — a reset does
  // exactly that — so a test can too.
  return {
    setColumn: (px: number) => {
      list = px;
    },
  };
};

const border = () => screen.getByRole("separator", { name: "Resize the list" });

/** The width the list is being drawn at, or "" when the grid still decides. */
const listw = () =>
  (document.querySelector(".ibsplit") as HTMLElement).style.getPropertyValue("--listw");

describe("resizing the panels", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => restoreRects?.());

  it("moves the border, and the list with it", async () => {
    stubLayout();
    handler = buildHandler;
    await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    fireEvent.pointerDown(border(), { button: 0, clientX: 280 });
    // The border moves by the pointer's travel, not to the pointer: it sits in a
    // column of its own at the pane's edge, so the column's own width (300 in the
    // stub) is where a drag of +240 starts from.
    fireEvent.pointerMove(window, { clientX: 520 });
    expect(listw()).toBe("540px");
    // Still following the pointer: a drag is not a single jump.
    fireEvent.pointerMove(window, { clientX: 610 });
    expect(listw()).toBe("630px");

    // And the width is the reader's, so it outlives the visit.
    fireEvent.pointerUp(window);
    expect(localStorage.getItem("cm-list")).toBe("630");
  });

  it("holds the border where both panels can still be read", async () => {
    stubLayout();
    handler = buildHandler;
    await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    fireEvent.pointerDown(border(), { button: 0, clientX: 300 });
    // Off the left edge of the window entirely.
    fireEvent.pointerMove(window, { clientX: -400 });
    expect(listw()).toBe(`${MIN}px`);
    // And shoved right, where the list would otherwise have the thread's width.
    fireEvent.pointerMove(window, { clientX: 2000 });
    expect(listw()).toBe(`${MOST}px`);
    fireEvent.pointerUp(window);
  });

  it("opens where the reader last left the border", async () => {
    stubLayout();
    localStorage.setItem("cm-list", "480");
    handler = buildHandler;
    await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    expect(listw()).toBe("480px");
  });

  it("gives the layout's own width back when the border is double-clicked", async () => {
    stubLayout();
    localStorage.setItem("cm-list", "480");
    handler = buildHandler;
    await mountApp("/");
    await screen.findByText("Loom cutover schedule");
    expect(listw()).toBe("480px");

    fireEvent.doubleClick(border());
    // No custom property at all: the grid's own minmax() decides again, which is
    // a different thing from a width of zero.
    expect(listw()).toBe("");
    expect(localStorage.getItem("cm-list")).toBeNull();
  });

  it("moves the border for a reader who cannot drag it", async () => {
    stubLayout(300);
    handler = buildHandler;
    await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    // Where the arrow keys start from is where the layout put the column, and the
    // separator says so rather than claiming a width nobody chose.
    await waitFor(() => expect(border().getAttribute("aria-valuenow")).toBe("300"));
    expect(border().getAttribute("aria-valuemin")).toBe(String(MIN));
    expect(border().getAttribute("aria-valuemax")).toBe(String(MOST));

    fireEvent.keyDown(border(), { key: "ArrowRight" });
    expect(listw()).toBe("316px");
    fireEvent.keyDown(border(), { key: "ArrowLeft" });
    fireEvent.keyDown(border(), { key: "ArrowLeft" });
    expect(listw()).toBe("284px");
    fireEvent.keyDown(border(), { key: "End" });
    expect(listw()).toBe(`${MOST}px`);
    fireEvent.keyDown(border(), { key: "Home" });
    expect(listw()).toBe(`${MIN}px`);
    // A key press is a decision of its own, so it is kept.
    expect(localStorage.getItem("cm-list")).toBe(String(MIN));
    // And a key the separator does not take moves nothing.
    fireEvent.keyDown(border(), { key: "PageDown" });
    expect(listw()).toBe(`${MIN}px`);
  });
});

describe("the border's own idea of where it is", () => {
  // A width another test stored would be read as this reader's choice, which is
  // the whole point of storing it.
  beforeEach(() => localStorage.clear());
  afterEach(() => restoreRects?.());

  it("follows the layout when the layout changes", async () => {
    const layout = stubLayout(300);
    handler = buildHandler;
    await mountApp("/");
    await screen.findByText("Loom cutover schedule");
    await waitFor(() => expect(border().getAttribute("aria-valuenow")).toBe("300"));

    layout.setColumn(380);
    fireEvent.keyDown(border(), { key: "ArrowRight" });
    expect(listw()).toBe("396px");
  });
});

/**
 * The reader's own messages in the pane.
 *
 * The mark is the corpus's, not the client's: the thread read carries `mine`,
 * which the service resolved from the addresses the reader stored as a setting,
 * and the pane draws the page's own `.msg.me` — the class the stylesheet already
 * tints — rather than inventing a second way to say "sent by you". The client
 * resolves nothing, which is why the handler below answers `mine` from the
 * setting it holds: that is the whole thread of custody this is testing.
 *
 * A reader who has never named an address sees no marks, and that is a state to
 * pin rather than a gap to fill: the pane has no surface of its own for saying
 * who the reader is, and guessing from the corpus would be inventing a fact
 * about them.
 */
describe("the reader's own messages in the pane", () => {
  const READER = "mail:<fence-panel-9@example.fed>"; // CHAINS[0]; opened by its row
  const READER_ADDR = "ada@okoye.example";
  /** The address that reads the reader's own thread: the pane starts empty now, so
   *  a test about what the pane draws has to say which thread is open. */
  const OPEN_READER = `/?open=${encodeURIComponent(READER)}`;

  /** The settings API as the server behaves: a field the body names is written,
   *  a field it does not name is left as it stands, and the addresses are tidied
   *  into the one comma-separated form the field is typed in. A mock that
   *  cleared everything on every write would let this file pass while the pane
   *  wiped the folder it never mentioned. */
  const mineHandler = (initial: { defaultFolder?: string; me?: string[] }): Handler => {
    let stored = initial;
    return (c) => {
      const p = pathOf(c);
      if (p === "/v1/settings") {
        if (c.method === "POST") {
          const body = JSON.parse(c.body ?? "{}") as { defaultFolder?: string; me?: string[] };
          stored = {
            defaultFolder:
              body.defaultFolder !== undefined ? body.defaultFolder : stored.defaultFolder,
            me:
              body.me !== undefined
                ? [...new Set(body.me.flatMap((v) => v.split(",")).map((v) => v.trim()))].filter(
                    (v) => v !== "",
                  )
                : stored.me,
          };
        }
        return json(200, {
          ...(stored.defaultFolder ? { defaultFolder: stored.defaultFolder } : {}),
          ...(stored.me?.length ? { me: stored.me } : {}),
        });
      }
      if (p.startsWith("/v1/chains/")) {
        const named = stored.me ?? [];
        return json(200, {
          rootExtId: READER,
          entries: [
            {
              extId: "mail:<fence-panel-8@example.fed>",
              source: "mail",
              quoted: false,
              ts: "2026-04-01T07:00:00Z",
              author: "Bo Halvorsen",
              html: "<p>Somebody else's message.</p>",
            },
            {
              extId: READER,
              source: "mail",
              quoted: false,
              ts: "2026-04-01T08:00:00Z",
              author: "Ada Okoye",
              html: "<p>The reader's own message.</p>",
              ...(named.includes(READER_ADDR) ? { mine: true } : {}),
            },
          ],
        });
      }
      if (p === "/v1/search") return pageOf(CHAINS);
      if (p === "/v1/spec" && c.method === "POST") return json(200, SPEC);
      if (p.startsWith("/v1/specs/")) return json(200, SPEC);
      if (p === "/auth/status") return json(200, { signed_in: true });
      return json(500, { error: `unexpected call to ${c.method} ${p}` });
    };
  };

  const settingsWrites = (): Call[] =>
    calls.filter((c) => c.method === "POST" && pathOf(c) === "/v1/settings");
  const marked = () => pane().querySelectorAll(".msg.me");

  it("tints the reader's own bubble, and only that one", async () => {
    handler = mineHandler({ me: [READER_ADDR] });
    await mountApp(OPEN_READER);
    await waitFor(() => expect(pane().querySelectorAll(".msg")).toHaveLength(2));

    // One of the two, and it is the reader's: the mark lands by who wrote the
    // message, not by where it sits in the thread.
    const mine = marked();
    expect(mine).toHaveLength(1);
    expect(mine[0]!.textContent).toContain("The reader's own message.");
    // The page's own mark — `me`, which is what `.msg.me .bub` tints — rather
    // than a pane-only class or border.
    expect(mine[0]!.classList.contains("me")).toBe(true);
    const theirs = pane().querySelectorAll(".msg")[0]!;
    expect(theirs.textContent).toContain("Somebody else's message.");
    expect(theirs.classList.contains("me")).toBe(false);
  });

  it("marks nothing for a reader who has never named an address", async () => {
    handler = mineHandler({});
    await mountApp(OPEN_READER);
    // The pane has drawn the whole thread, so an empty mark is a claim about
    // what is on screen rather than about what was fetched.
    await waitFor(() => expect(pane().querySelectorAll(".msg")).toHaveLength(2));
    expect(marked()).toHaveLength(0);
  });

  it("hands a build the stored addresses, and writes no setting of its own", async () => {
    handler = mineHandler({ me: [READER_ADDR, "bo@halvorsen.example"] });
    await mountApp("/");
    await screen.findByRole("button", { name: "Fence panels" });
    click(screen.getByLabelText("Select Fence panels"));

    // There is no addresses field beside the build button: the addresses are a
    // setting (written on the services page, where the other settings are), and a
    // field here was a second place to say who the reader is — one of which the
    // pane did not read.
    expect(screen.queryByLabelText("Your addresses")).toBeNull();

    await braid();

    // The build marks the page from the setting. Nothing about the reader is
    // written from here: this bar asks for a page, it does not keep preferences.
    await waitFor(() =>
      expect(calls.some((c) => c.method === "POST" && pathOf(c) === "/v1/spec")).toBe(true),
    );
    const build = calls.find((c) => c.method === "POST" && pathOf(c) === "/v1/spec")!;
    expect(JSON.parse(build.body!).me).toEqual([READER_ADDR, "bo@halvorsen.example"]);
    expect(settingsWrites()).toHaveLength(0);
  });
});

describe("downloading a file the pane does not hold yet", () => {
  // The pane draws the corpus, so a file whose bytes are not in it is a chip that
  // has to leave for the mailbox — and, where the thread comes from the corpus
  // rather than from a saved page, pressing that chip is the download: it asks the
  // host for the message's files, and the file opens over the pane once they are
  // here. Every id, name and address in this block is invented.
  const ROOT = "mail:<loom-cutover-1@example.fed>";
  const OPEN_ROOT = `/?open=${encodeURIComponent(ROOT)}`;
  const SHA = "sha-of-the-bytes";
  const SHEET = "shed,readings\nNova,41.2";
  /** The file the mailbox holds and the corpus does not: the corpus knows the
   *  message rather than the file, so the only destination it can name today is
   *  the message's permalink (see toCorpusAttachment). */
  const UNFETCHED = {
    name: "shed.csv",
    kind: "CSV",
    size: "512 B",
    link: "https://mail.google.com/mail/u/0/#all/19d263bb5a6b00db",
  };
  /** The same file once a pull has stored it, with the host's own way to show it. */
  const STORED = { ...UNFETCHED, blobSha: SHA, open: "popup" as const, view: "text" as const };

  const ENTRY = {
    extId: ROOT,
    source: "mail",
    quoted: false,
    ts: "2026-03-02T09:15:00Z",
    author: "Ada Okoye",
    subject: "Loom cutover schedule",
    body: "quote attached",
    html: "<p>quote attached</p>",
    tz: "AEST",
    tzOffsetMinutes: 600,
  };

  /** A pane whose thread carries one file, either before or after a pull, and a
   *  pull endpoint that answers the counts. Everything else is the app's own. */
  const paneHandler = (opts: { stored: () => boolean; pull: () => Response }): Handler => (c) => {
    const p = pathOf(c);
    if (p.startsWith("/v1/chains/")) {
      return json(200, {
        rootExtId: ROOT,
        entries: [{ ...ENTRY, attachments: [opts.stored() ? STORED : UNFETCHED] }],
      });
    }
    if (p === "/v1/media/pull" && c.method === "POST") return opts.pull();
    // The bytes, once they are stored: the window reads them from this host.
    if (p.startsWith("/v1/attachments/"))
      return new Response(SHEET, { status: 200, headers: { "content-type": "text/csv" } });
    return buildHandler(c);
  };

  const pulledAnswer = () =>
    json(200, {
      wanted: 1,
      pulled: 1,
      skipped: 0,
      failed: 0,
      bytes: 512,
      files: [{ name: "shed.csv", source: "mail", sha: SHA, bytes: 512 }],
    });

  const chip = () => within(pane()).getByRole("link", { name: /shed\.csv/ });
  const pulls = () => calls.filter((c) => c.method === "POST" && pathOf(c) === "/v1/media/pull");
  const chainReads = () => calls.filter((c) => pathOf(c).startsWith("/v1/chains/"));
  const windowOver = () => document.querySelector(".pop") as HTMLElement | null;

  it("fetches that message's files, and opens the file in the window", async () => {
    const log = vi.spyOn(console, "log").mockImplementation(() => {});
    let stored = false;
    // The server stores the bytes before it answers — so the flag flips inside the
    // handler, and the thread the pane re-reads is one that has them.
    handler = paneHandler({
      stored: () => stored,
      pull: () => {
        stored = true;
        return pulledAnswer();
      },
    });

    await mountApp(OPEN_ROOT);
    await within(pane()).findByRole("link", { name: /shed\.csv/ });
    // Before the press the chip is a link to the mailbox: the corpus holds no
    // bytes for it, and the click is the reader's own to make.
    expect(chip().getAttribute("href")).toBe(UNFETCHED.link);

    fireEvent.click(chip());

    // One call, and it names the message alone: a pane opened from an address bar
    // has no saved page to be handed back, and asking for one would make a saved
    // page the price of looking at a file (see ThreadMessages).
    await waitFor(() => expect(pulls()).toHaveLength(1));
    expect(JSON.parse(pulls()[0]!.body!)).toEqual({ entry: ROOT });

    // Then it opens, over the pane: the press was replayed once the re-read had
    // put bytes behind the chip (behaviour.ts), and the file's own text is in the
    // window.
    const win = await waitFor(() => {
      const w = windowOver();
      if (!w || w.hidden) throw new Error("no window yet");
      return w;
    });
    expect(win.querySelector(".popcap")!.textContent).toBe("shed.csv");
    await waitFor(() =>
      expect(
        [...win.querySelectorAll(".poptable tbody td")].map((td) => td.textContent),
      ).toEqual(["Nova", "41.2"]),
    );

    // The pane is a picture of the corpus, so the bytes appearing is the thread
    // being read again rather than a chip being patched.
    expect(chainReads().length).toBeGreaterThan(1);
    expect(chip().getAttribute("href")).toBe(`/v1/attachments/${SHA}`);

    // What the pull did is on the console, where the counts and the reasons are:
    // the file that opened says the rest.
    expect(log.mock.calls.map((c) => c[0]).join("\n")).toMatch(/1\/1 files fetched/);
  });

  it("does not make a download of a file the corpus already holds", async () => {
    handler = paneHandler({ stored: () => true, pull: pulledAnswer });
    await mountApp(OPEN_ROOT);

    await waitFor(() =>
      expect(chip().getAttribute("href")).toBe(`/v1/attachments/${SHA}`),
    );
    fireEvent.click(chip());
    expect(pulls()).toHaveLength(0);
  });

  it("says on the pane why a pull failed, rather than looking like nothing happened", async () => {
    // A host started without -media answers 403: a reach, and one that a reader
    // asking a page for bytes has to be told about.
    handler = paneHandler({
      stored: () => false,
      pull: () => json(403, { error: "this host cannot fetch media" }),
    });
    await mountApp(OPEN_ROOT);
    await within(pane()).findByRole("link", { name: /shed\.csv/ });

    fireEvent.click(chip());

    const note = await within(pane()).findByRole("status");
    expect(note.textContent).toMatch(/started without -media/);
    // The file is still missing, so the chip is still the download it was, and
    // pressing it again asks again.
    fireEvent.click(chip());
    await waitFor(() => expect(pulls()).toHaveLength(2));
  });
});

// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within, act } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { makeQueryClient } from "../src/lib/queryClient";
import { createChainmailRouter } from "../src/router";
import { whenShort } from "../src/components/Inbox";

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

/** The shape /v1/search answers with, for a chain a row is built from. Loose on
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

function chain(over: Record<string, unknown>): FixtureChain {
  return { sources: ["mail"], entries: 1, matched: 1, people: 2, score: 0.01, ...over } as FixtureChain;
}

/** An entry attached to a chain, as the wire's EntryHit: no ranking found it,
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
  chain({
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
  chain({
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

/** The reading pane asks /v1/chains/<root> for whichever chain is selected, and
 *  every chain in the fixtures has a body of its own, so "the pane swapped" is a
 *  claim about what is on screen rather than about a call being made. */
const CHAIN_BODIES: Record<string, { author: string; subject: string; body: string }> = {
  "mail:<fence-panel-9@example.fed>": {
    author: "Ada Okoye",
    subject: "Fence panels",
    body: "Unrelated: the fence panels arrived, and the gate needs a new hinge.",
  },
  "mail:<loom-cutover-1@example.fed>": {
    author: "Bo Halvorsen",
    subject: "Loom cutover schedule",
    body: "Roof access is fine from the 14th.",
  },
};

const chainHandler: Handler = (c) => {
  const root = decodeURIComponent(pathOf(c).slice("/v1/chains/".length));
  const b = CHAIN_BODIES[root];
  return b
    ? json(200, {
        rootExtId: root,
        entries: [{ extId: root, source: "mail", quoted: false, ts: "2026-04-01T08:00:00Z", ...b }],
      })
    : json(404, { error: `no chain ${root}` });
};

/** A handler that answers the pane for any chain, and lets the test's own
 *  handler see everything else. */
const withChains = (inner: Handler): Handler => (c) =>
  pathOf(c).startsWith("/v1/chains/") ? chainHandler(c) : inner(c);

const buildHandler: Handler = withChains((c) => {
  const p = pathOf(c);
  if (p === "/v1/spec" && c.method === "POST") return json(200, SPEC);
  if (p.startsWith("/v1/specs/")) return json(200, SPEC);
  if (p === "/v1/search") return pageOf(CHAINS);
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

/** The reading pane, so an assertion about the thread shown cannot be satisfied
 *  by the same text in a row. */
const pane = () => document.querySelector(".ibread") as HTMLElement;

describe("the home page with no query", () => {
  it("lists every chain, newest message first", async () => {
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
    // Gmail-minimal: the count appears only when a chain has more than one
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

  it("leaves for the search page when a query is typed, and says so in the URL", async () => {
    handler = buildHandler;
    const router = await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    const box = screen.getByLabelText("Search the corpus");
    fireEvent.change(box, { target: { value: "cutover" } });
    fireEvent.submit(box.closest("form")!);

    await waitFor(() => expect(router.state.location.searchStr).toBe("?q=cutover"));
    // The search page's own fields, not the list's: the query is what turns one
    // into the other.
    expect(await screen.findByLabelText("Query")).toBeTruthy();
    expect(screen.queryByLabelText("Search the corpus")).toBeNull();
  });

  it("reads the newest chain in the pane before a row is clicked", async () => {
    handler = buildHandler;
    await mountApp("/");
    // The row, not the text: the pane is already showing the same subject, and a
    // duplicate on screen is the point of the layout.
    await screen.findByRole("button", { name: "Fence panels" });

    // A pane beside the list, not a modal over it: nothing was opened, and the
    // list is still there to be read.
    expect(screen.queryByRole("dialog")).toBeNull();
    await waitFor(() =>
      expect(within(pane()).getByText(/and the gate needs a new hinge/)).toBeTruthy(),
    );
    // The top row is what the pane is showing, and it says so.
    expect(
      screen.getByRole("button", { name: "Fence panels" }).getAttribute("aria-current"),
    ).toBe("true");
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
      screen.getByRole("button", { name: "Loom cutover schedule" }).getAttribute("aria-current"),
    ).toBe("true");
    expect(screen.getByRole("button", { name: "Fence panels" }).getAttribute("aria-current")).toBeNull();
  });

  it("builds a page from the ticked chains, recording no query for it", async () => {
    handler = buildHandler;
    const router = await mountApp("/");
    await screen.findByRole("button", { name: "Fence panels" });

    click(screen.getByLabelText("Select Fence panels"));
    click(screen.getByRole("button", { name: /Build page from 1 chain$/ }));
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
  // alternative is losing a chain that ends beside it.
  const firstPage = Array.from({ length: 50 }, (_, i) =>
    chain({
      rootExtId: `mail:<page-one-${i}@example.fed>`,
      subject: `Chain ${i + 1}`,
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
    chain({
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
    expect(screen.getAllByText("Chain 50")).toHaveLength(1);
    // A short page is the end of the corpus, so there is nothing left to watch.
    expect(document.querySelector(".ibend")).toBeNull();
  });

  // A chain whose entries straddle the cursor is returned again on the next page
  // (its `last` is the whole chain's newest message, not the newest inside the
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
      // The pane asks for whichever chain is on screen; empty is enough here, and
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
    const today = whenShort(at(2026, 8, 16, 14, 30), now);
    expect(today).toMatch(/\d/);
    // No month, and no year: today's clock is the whole answer.
    expect(today).not.toContain("Mar");
    expect(today).not.toContain("2026");
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

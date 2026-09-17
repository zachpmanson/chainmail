// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { $api, searchQuery } from "../src/lib/api";
import { makeQueryClient } from "../src/lib/queryClient";
import { createChainmailRouter } from "../src/router";

// React refuses to batch updates outside act() unless told it is under test.
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

/** The renderer's scroll-spy builds one on mount, and jsdom has none. */
class NoopObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}
(globalThis as { IntersectionObserver?: unknown }).IntersectionObserver = NoopObserver;

/**
 * Every name, address and id below is invented. The corpus this client talks to
 * holds personal mail, so no fixture here may be a real one.
 */
const CHAINS = [
  {
    rootExtId: "mail:<loom-cutover-1@example.fed>",
    subject: "Loom cutover schedule",
    sources: ["mail"],
    entries: 4,
    matched: 3,
    people: 4,
    first: "2026-03-02T09:15:00Z",
    last: "2026-03-11T17:40:00Z",
    score: 0.91,
  },
  {
    rootExtId: "mail:<lease-renewal-1@example.fed>",
    subject: "Warehouse lease renewal",
    sources: ["mail", "slack"],
    entries: 180,
    matched: 3,
    people: 12,
    attachments: 7,
    first: "2025-11-04T08:00:00Z",
    last: "2026-02-19T11:02:00Z",
    score: 0.22,
  },
];

const SPEC = {
  title: "Loom cutover",
  messages: [
    {
      date: "Mon 2 Mar 2026",
      time: "09:15",
      sender: "Ada Byron",
      org: "Loomworks",
      fromEmail: "ada@loomworks.example",
      body: "<p>invented body</p>",
    },
  ],
};

interface Call {
  url: string;
  method: string;
  body?: string;
}

/** Records what was asked for, so a test can assert the request, not the mock. */
let calls: Call[];
type Handler = (call: Call) => Promise<Response> | Response;
let handler: Handler;

/**
 * The thread the right-hand pane reads, for any id. A candidate is fetched by id
 * — the same read the inbox's pane makes — and the tests here are about the
 * search and the build rather than about what a thread holds, so every id is
 * answered with one invented entry. A handler that did not answer it would fail
 * the pane into an alert, and a test looking for the page's own failure would
 * find that one instead.
 *
 * `html` as well as `body`, because the pane draws the transcript's bubbles: the
 * corpus renders the sender's markup server-side (internal/spec, the same
 * conversion a build uses) and the pane shows that, so a fixture without it would
 * pass a "the pane has text in it" assertion while rendering nothing.
 */
const CHAIN_ENTRIES = [
  {
    extId: "mail:<loom-cutover-1@example.fed>",
    source: "mail",
    quoted: false,
    ts: "2026-03-02T09:15:00Z",
    author: "Ada Byron",
    subject: "Loom cutover schedule",
    body: "Cutover goes ahead on the 11th.",
    html: "<p>Cutover goes ahead on the 11th.</p>",
    tz: "AEST",
    tzOffsetMinutes: 600,
  },
];

const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json", ...headers },
  });

/** Dispatch on the exact path: "/v1/spec" is a substring of "/v1/specs/<name>". */
const pathOf = (c: Call) => new URL(c.url).pathname;

beforeEach(() => {
  calls = [];
  handler = () => json(500, { error: "no handler installed" });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    // Resolved against the page: a relative path is what the client sends, and
    // Request outside a browser will not parse one.
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
    // The pane's read of one candidate thread: the search page shows its top
    // result, so every test that gets results reads a thread whether it meant to
    // or not.
    if (pathOf(call).startsWith("/v1/chains/")) {
      return json(200, {
        rootExtId: decodeURIComponent(pathOf(call).slice("/v1/chains/".length)),
        entries: CHAIN_ENTRIES,
      });
    }
    return handler(call);
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  history.replaceState(null, "", "/");
});

/**
 * The whole app under a fresh router with an in-memory history: jsdom's real
 * window.history is shared across a file, and a singleton router would keep
 * its first URL forever, so each test starts from its own URL (and its own
 * query client, so caches never leak between tests) and asserts on
 * router.state.location, which is the URL the client actually owns.
 */
async function mountApp(...initialEntries: string[]) {
  const router = createChainmailRouter(initialEntries);
  // The router resolves its initial URL and builds the match tree asynchronously;
  // render only once it has somewhere to go.
  await router.load();
  render(
    <QueryClientProvider client={makeQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return router;
}

/** fireEvent.change, not a direct .value assignment: React tracks the previous
 *  value on the node and ignores a write it did not see. */
const typeInto = (label: string, value: string) =>
  fireEvent.change(screen.getByLabelText(label), { target: { value } });

const click = (el: Element) => fireEvent.click(el);

/** Braid the ticked chains into a page. The bar's button opens a dialog, and the
 *  title is typed in there — a title is a decision about the page being made, so
 *  it lives beside the button that commits to it rather than on the bar's line
 *  with Archive and Delete, which are about the mailbox. */
async function braid(title?: string) {
  click(screen.getByRole("button", { name: "Braid Threads" }));
  await screen.findByLabelText("Page title");
  if (title !== undefined) typeInto("Page title", title);
  click(screen.getByRole("button", { name: "Braid" }));
}

/** jsdom does not submit a form when its submit button is clicked, so the submit
 *  event is dispatched directly. The button itself is still asserted on. */
function submitSearch() {
  const button = screen.getByRole("button", { name: "Search" }) as HTMLButtonElement;
  expect(button.disabled).toBe(false);
  fireEvent.submit(button.closest("form")!);
}

/**
 * The nav's search, opened the way a person opens it: the caret goes in the box.
 * The box is the search on every page — the page below has no form of its own —
 * so opening it is what puts the mode, the person and the date on screen, and it
 * asks nothing by itself: the address is untouched until something is committed.
 */
function openSearch(): HTMLInputElement {
  const box = screen.getByRole("textbox", { name: "Search the corpus" }) as HTMLInputElement;
  fireEvent.focus(box);
  return box;
}

const searchCalls = () => calls.filter((c) => pathOf(c) === "/v1/search");

/**
 * Search from the inbox: the nav's box is the search, so the query is typed where
 * it lives and committed there. That is the journey a person takes, and the one
 * that writes the query into the URL the search page reads.
 */
async function searchFromInbox(text: string) {
  const box = openSearch();
  fireEvent.change(box, { target: { value: text } });
  fireEvent.submit(box.closest("form")!);
  await waitFor(() =>
    expect((openSearch() as HTMLInputElement).value).toBe(text),
  );
}

/** The common building mocks: search answers with the two chains, a build with
 *  SPEC, and the saved page a build lands on with the same SPEC. */
const buildHandler: Handler = (c) => {
  const p = pathOf(c);
  if (p === "/v1/spec" && c.method === "POST") return json(200, SPEC);
  if (p === "/v1/specs/loom-cutover") return json(200, SPEC);
  if (p === "/v1/search") return json(200, { mode: "lexical", chains: CHAINS });
  // The nav's Person control reads the same people /status does, and only while
  // the panel is open — a test that never opens it never asks.
  if (p === "/v1/people") return json(200, PEOPLE);
  // The shell's sign-in banner probes auth on every route; answer it signed in
  // so tests exercise the app, not the banner.
  if (p === "/auth/status") return json(200, { signed_in: true });
  return json(500, { error: `unexpected call to ${c.method} ${p}` });
};

/** The /status view's fixtures: three backends and a small corpus. */
const STATUS = {
  checkedAt: "2026-08-22T15:04:00Z",
  nextSlurpAt: "2026-08-22T16:00:00Z",
  services: [
    { id: "mail", label: "Gmail", status: "ok" },
    { id: "slack", label: "Slack (slackdump)", status: "needs-auth", detail: "run the slackdump import" },
    { id: "embed", label: "Embedding daemon (ollama)", status: "down", detail: "start it with `ollama serve`" },
  ],
};

const STATS = {
  entries: 4281,
  bySource: { mail: 4001, slack: 280 },
  people: 214,
  chainRoots: 1024,
  unresolved: 13,
  embeddings: [{ model: "nomic-embed-text", dim: 768, vectors: 4100, skipped: 120, stale: 0, eligible: 61 }],
};

/**
 * The people the corpus holds, for the controls that pick one: the reader's own on
 * /status, and the Person filter in the nav's search. Ada carries two addresses
 * because the folding of several aliases into one person is the whole reason both
 * of them pick a person, and the third has none at all — a name recovered from
 * somebody else's quote is not a mailbox anything can be marked as coming from.
 */
const PEOPLE = {
  people: [
    {
      personId: 1,
      displayName: "Ada Byron",
      identities: ["email:ada@okoye.example", "email:ada@work.example"],
      sent: 340,
      received: 121,
    },
    {
      personId: 2,
      displayName: "Bo Halvorsen",
      identities: ["email:bo@halvorsen.example"],
      sent: 90,
      received: 30,
    },
    { personId: 3, displayName: "Ben", identities: ["display_name:Ben"], sent: 0, received: 4 },
  ],
};

const statusHandler: Handler = (c) => {
  const p = pathOf(c);
  if (p === "/v1/status") return json(200, STATUS);
  if (p === "/v1/stats") return json(200, STATS);
  if (p === "/v1/settings") return json(200, { slurpEvery: "10m", defaultFolder: "INBOX" });
  // The folder control on this screen reads the same label list the home page's
  // own folder button does.
  if (p === "/v1/labels") {
    return json(200, { labels: [{ name: "INBOX", messages: 210 }, { name: "Work", messages: 41 }] });
  }
  // The people the corpus holds, for the control that says which of them the
  // reader is — and, on any page, for the nav's search (see PEOPLE).
  if (p === "/v1/people") return json(200, PEOPLE);
  return json(500, { error: `unexpected call to ${c.method} ${p}` });
};

describe("searching for chains", () => {
  it("lists each candidate with the matched-of-total ratio, in the inbox's own rows", async () => {
    // One entry attached to the top candidate, so the row has a sender, a clock
    // and a snippet to draw — and a semantic rank, which is what the ranked
    // row's similarity is read from.
    handler = () =>
      json(200, {
        mode: "lexical",
        chains: [
          {
            ...CHAINS[0],
            best: [
              {
                extId: "mail:<loom-cutover-4@example.fed>",
                source: "mail",
                ts: "2026-03-11T17:40:00Z",
                personId: 1,
                person: "Ada Byron",
                snippet: "Roof access is fine from the 14th.",
                score: 0.9,
                proseRank: 0,
                identRank: 0,
                semRank: 1,
                similarity: 0.83,
              },
            ],
          },
          CHAINS[1],
        ],
      });
    await mountApp("/?q=cutover");

    await screen.findByText("Loom cutover schedule", { selector: ".ibsubj" });
    // The row is the inbox's row: who wrote last, when, what the thread is
    // called, what they said, the people in it and how many messages, drawn by
    // the same component with the same classes. What the ranking adds is the
    // meta line, and it is the only thing under the snippet.
    const row = screen
      .getByText("Loom cutover schedule", { selector: ".ibsubj" })
      .closest(".ibrow") as HTMLElement;
    expect(row.querySelector(".ibwho")!.textContent).toBe("Ada Byron");
    expect(row.querySelector(".ibwhen")!.textContent).toBeTruthy();
    expect(row.querySelector(".ibsnippet")!.textContent).toBe("Roof access is fine from the 14th.");
    // Two counts, each in its own glyph: the people in the thread, and how many
    // messages it holds.
    expect(row.querySelector(".ibppl")!.textContent).toContain("4");
    expect(row.querySelector(".ibppl svg")).toBeTruthy();
    expect(row.querySelector(".ibcount")!.textContent).toContain("4");
    expect(row.querySelector(".ibcount svg")).toBeTruthy();
    // What a ranked row adds, and nothing else: the ratio the person is judging
    // the candidate by, and the similarity behind it. The span and the sources
    // are gone — a thread does not grow a date range because it was searched for.
    const meta = row.querySelector(".ibmeta")!;
    expect(meta.textContent).toContain("3 of 4 matched");
    expect(meta.textContent).toContain("sim 0.83");
    // The thread's own span, at the far end of the line and so at the bottom
    // right of the row: the one fact here that is about the conversation rather
    // than about the search that found it.
    const span = meta.querySelector(".ibspan")!;
    expect(span.textContent).toBe("2026-03-02 – 2026-03-11");
    expect(meta.lastElementChild).toBe(span);
    expect(row.querySelector(".selpvbtn")).toBeNull();
    // the same numerator over a different thread size — the ratio is what
    // separates a thread about the query from one that mentioned it
    expect(await screen.findByText("3 of 180 matched")).toBeTruthy();
    expect(document.querySelectorAll(".ibppl").length).toBe(2);
  });

  it("passes the mode and drops the filters left blank", async () => {
    handler = () => json(200, { mode: "lexical", chains: CHAINS });
    // The mode comes from the URL here, as it does for a reload or a Back:
    // the inbox's own box carries only the query.
    await mountApp("/?q=cutover&mode=hybrid");

    const url = new URL(searchCalls()[0]!.url);
    expect(url.searchParams.get("q")).toBe("cutover");
    expect(url.searchParams.get("mode")).toBe("hybrid");
    expect(url.searchParams.has("person")).toBe(false);
    expect(url.searchParams.has("since")).toBe(false);
  });

  it("does not show the previous mode's results while the next mode loads", async () => {
    handler = (c) =>
      json(200, {
        mode: new URL(c.url).searchParams.get("mode") === "semantic" ? "semantic" : "lexical",
        chains: new URL(c.url).searchParams.get("mode") === "semantic" ? [CHAINS[1]] : [CHAINS[0]],
      });
    await mountApp("/?q=cutover");
    await screen.findByText("Loom cutover schedule", { selector: ".ibsubj" });

    // hold the semantic answer open: whatever is on screen mid-flight is the
    // claim the page is making, and it must not be the lexical result set
    let release: (r: Response) => void = () => {};
    handler = () => new Promise<Response>((res) => (release = res));
    openSearch();
    typeInto("Mode", "semantic");

    await waitFor(() => expect(screen.queryByText("Loom cutover schedule")).toBeNull());
    expect(screen.getByText("Searching…")).toBeTruthy();

    await act(async () => {
      release(json(200, { mode: "semantic", chains: [CHAINS[1]] }));
    });
    await screen.findByText("Warehouse lease renewal", { selector: ".ibsubj" });
  });
});

describe("reading a candidate beside the results", () => {
  const pane = () => document.querySelector(".ibread") as HTMLElement;
  const rowOf = (subject: string) =>
    screen.getByText(subject, { selector: ".ibsubj" }).closest(".ibrow") as HTMLElement;

  it("reads the same viewer the inbox reads, in the split rather than over it", async () => {
    handler = () => json(200, { mode: "lexical", chains: CHAINS });
    await mountApp("/?q=cutover");
    await screen.findByText("Loom cutover schedule", { selector: ".ibsubj" });

    // The pane is a column of the split, and it starts empty: nothing is open
    // until a result is clicked, exactly as on the inbox. A pane that filled
    // itself in with the top of the ranking would be a candidate to dismiss.
    expect(document.querySelector(".ibsplit .ibread")).toBeTruthy();
    expect(pane().querySelector(".msg")).toBeNull();
    click(
      within(rowOf("Loom cutover schedule")).getByRole("button", {
        name: "Loom cutover schedule",
      }),
    );
    await waitFor(() =>
      expect(pane().querySelector(".ibread-subj")!.textContent).toBe("Loom cutover schedule"),
    );
    await waitFor(() => expect(pane().textContent).toContain("Cutover goes ahead on the 11th."));
    // The transcripts' own bubbles, not the pane's old plain cards: the same
    // `Message` a built page draws for the same entry, over the corpus's
    // rendered html. A search result and a browsed thread are the same reading.
    expect(pane().querySelector(".stream .msg .bub")).toBeTruthy();
    expect(pane().querySelector(".selen")).toBeNull();
    // No dialog over the list: the whole point is reading one candidate and
    // comparing it with the others.
    expect(document.querySelector(".selpv")).toBeNull();
    // And the row it is reading says so, the way the inbox's open row does.
    expect(rowOf("Loom cutover schedule").classList.contains("sel")).toBe(true);
  });

  it("reads the address's search back into the nav's box, and leaves it alone", async () => {
    handler = () => json(200, { mode: "lexical", chains: CHAINS });
    const router = await mountApp("/?q=cutover&mode=semantic&person=ada");
    await screen.findByText("Loom cutover schedule", { selector: ".ibsubj" });

    // The box carries the search it can see rather than replacing it: a box that
    // cleared the query would throw away the search the person is in the middle
    // of, and one that dropped the filters would answer a different question than
    // the one on screen.
    const box = openSearch();
    expect(box.value).toBe("cutover");
    expect((screen.getByLabelText("Mode") as HTMLSelectElement).value).toBe("semantic");
    expect((screen.getByLabelText("Person") as HTMLInputElement).value).toBe("ada");
    // And opening it asked nothing: the address is still the search it was.
    expect(router.state.location.searchStr).toContain("q=cutover");
    expect(router.state.location.searchStr).toContain("mode=semantic");
    expect(router.state.location.searchStr).toContain("person=ada");
  });

  it("opens the row that was pressed in the pane, and puts it in the URL", async () => {
    handler = () => json(200, { mode: "lexical", chains: CHAINS });
    const router = await mountApp("/?q=cutover");
    const other = CHAINS[1]!.rootExtId;
    await screen.findByText("Warehouse lease renewal", { selector: ".ibsubj" });

    // The row body, which is what opens a thread on the inbox — the ranked list's
    // own "Preview" button was a second way to do the one thing.
    click(
      within(rowOf("Warehouse lease renewal")).getByRole("button", {
        name: "Warehouse lease renewal",
      }),
    );

    await waitFor(() => expect(pane().querySelector(".ibread-subj")!.textContent).toBe("Warehouse lease renewal"));
    await waitFor(() =>
      expect(
        calls.some((c) => decodeURIComponent(pathOf(c)) === `/v1/chains/${other}`),
      ).toBe(true),
    );
    // The address carries it, so a reload or a shared link lands on the same
    // candidate rather than back at the top of the results.
    expect(decodeURIComponent(router.state.location.searchStr)).toContain(`open=${other}`);
    expect(rowOf("Warehouse lease renewal").classList.contains("sel")).toBe(true);
    expect(rowOf("Loom cutover schedule").classList.contains("sel")).toBe(false);
  });

  it("wears the thread's counts in the pane's head, as the row wears them", async () => {
    handler = () => json(200, { mode: "lexical", chains: CHAINS });
    await mountApp("/?q=cutover");
    await screen.findByText("Warehouse lease renewal", { selector: ".ibsubj" });
    click(
      within(rowOf("Warehouse lease renewal")).getByRole("button", {
        name: "Warehouse lease renewal",
      }),
    );
    await waitFor(() =>
      expect(pane().querySelector(".ibread-subj")!.textContent).toBe("Warehouse lease renewal"),
    );

    // The mail count, the participant count and the paperclip, as the glyphs the
    // list uses — not "180 entries" in words. Two vocabularies for one thread is
    // what this replaces; the numbers are asserted on both the head and the row
    // so they are the same numbers.
    const counts = pane().querySelector(".ibread-counts")!;
    expect(counts.querySelector(".ibcount")!.textContent!.trim()).toBe("180");
    expect(counts.querySelector(".ibppl")!.textContent!.trim()).toBe("12");
    expect(counts.querySelector(".ibatt")!.textContent!.trim()).toBe("7");
    expect(counts.querySelector(".ibcount svg")).toBeTruthy();
    expect(counts.querySelector(".ibppl svg")).toBeTruthy();
    expect(counts.querySelector(".ibatt svg")).toBeTruthy();
    expect(counts.textContent).not.toContain("entr");

    const row = rowOf("Warehouse lease renewal");
    expect(row.querySelector(".ibcount")!.textContent!.trim()).toBe("180");
    expect(row.querySelector(".ibppl")!.textContent!.trim()).toBe("12");
    expect(row.querySelector(".ibatt")!.textContent!.trim()).toBe("7");
  });

  it("reads a candidate named on the URL, and hands the list back when it is closed", async () => {
    handler = () => json(200, { mode: "lexical", chains: CHAINS });
    const router = await mountApp(
      `/?q=cutover&open=${encodeURIComponent(CHAINS[1]!.rootExtId)}`,
    );

    await waitFor(() => expect(pane().querySelector(".ibread-subj")!.textContent).toBe("Warehouse lease renewal"));

    // The pane's own way back, which is the whole of the list on a narrow
    // screen: clearing the address is what un-picks it.
    click(screen.getByRole("button", { name: "← Results" }));
    await waitFor(() =>
      expect(decodeURIComponent(router.state.location.searchStr)).not.toContain("open="),
    );
  });
});

describe("the key a search is cached under", () => {
  /**
   * Asserted on the key itself, not through the UI: the pending-state test below
   * proves the consequence, but it would also pass if the two requests differed
   * for some other reason. This is the property everything else rests on — a key
   * that omitted mode would serve the lexical answer to a semantic question and
   * say nothing about having done so.
   */
  const keyFor = (mode: "lexical" | "semantic") =>
    $api.queryOptions("get", "/v1/search", {
      params: { query: searchQuery({ q: "cutover", mode }) },
    }).queryKey;

  it("distinguishes two modes of the same query", () => {
    expect(keyFor("lexical")).not.toEqual(keyFor("semantic"));
    expect(JSON.stringify(keyFor("semantic"))).toContain("semantic");
  });

  it("is the same key for the same search, so a repeat is not a fresh miss", () => {
    expect(keyFor("lexical")).toEqual(keyFor("lexical"));
  });
});

describe("declining", () => {
  it("shows the service's own words when the embedding daemon is down, and asks once", async () => {
    handler = () =>
      json(503, {
        error: 'mode "semantic": no embedding daemon reachable at http://localhost:11434',
      });
    await mountApp("/?q=cutover&mode=semantic");

    await screen.findByText(/no embedding daemon reachable/);
    // a 503 here is a fact about the operator's machine, not a hiccup; retrying
    // it only delays saying so
    await new Promise((r) => setTimeout(r, 120));
    expect(searchCalls()).toHaveLength(1);
  });

  it("tells a rejected query apart from a name that matches nothing", async () => {
    handler = () => json(400, { error: "unbalanced quote in query" });
    await mountApp("/?q=" + encodeURIComponent('"cutover'));
    expect((await screen.findByRole("alert")).textContent).toContain("Rejected (400)");

    cleanup();
    calls = [];
    handler = () => json(404, { error: "no entry carries id mail:<nothing@example.fed>" });
    await mountApp("/?q=" + encodeURIComponent("mail:<nothing@example.fed>"));
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Not found (404)");
    expect(alert.textContent).toContain("no entry carries id mail:<nothing@example.fed>");
  });
});

describe("building a page from the chosen set", () => {
  it("shows the build bar only once something is ticked", async () => {
    handler = buildHandler;
    await mountApp("/?q=cutover");
    await screen.findByText("Loom cutover schedule", { selector: ".ibsubj" });

    // Nothing ticked is nothing to build from, so there is no bar — the same
    // rule as the inbox, where the search page used to show a row of controls
    // that could not do anything yet.
    expect(screen.queryByRole("button", { name: "Braid Threads" })).toBeNull();

    click(screen.getAllByRole("checkbox")[0]!);
    expect(await screen.findByRole("button", { name: "Braid Threads" })).toBeTruthy();
    // The title is not on the bar: it is in the dialog the button opens, so the
    // bar's own line holds only the verbs and the folder those verbs take.
    expect(screen.queryByLabelText("Page title")).toBeNull();
    click(screen.getByRole("button", { name: "Braid Threads" }));
    expect(await screen.findByLabelText("Page title")).toBeTruthy();
    // And it is the one bar, so no field asking who the reader is: the addresses
    // are a setting, written on the services page.
    expect(screen.queryByLabelText("Your addresses")).toBeNull();
  });

  it("posts exactly the ticked chains and lands on the page", async () => {
    handler = buildHandler;
    const router = await mountApp("/?q=cutover");
    await screen.findByText("Loom cutover schedule", { selector: ".ibsubj" });

    const boxes = screen.getAllByRole("checkbox");
    click(boxes[0]!);
    click(boxes[1]!);
    click(boxes[1]!); // unticked again: it must not reach the request
    await braid("Loom cutover");
    await waitFor(() => expect(calls.some((c) => pathOf(c) === "/v1/spec")).toBe(true));

    const post = calls.find((c) => pathOf(c) === "/v1/spec")!;
    expect(post.method).toBe("POST");
    // The title was typed, so the page earns the slug as its name.
    expect(JSON.parse(post.body!)).toEqual({
      chains: ["mail:<loom-cutover-1@example.fed>"],
      name: "loom-cutover", // slug of the page title: the URL it earns
      title: "Loom cutover",
      queries: [{ q: "cutover", note: "corpus search, mode=hybrid" }],
    });
    // The page route owns the URL now, clean of the search that built it.
    await waitFor(() => expect(router.state.location.pathname).toBe("/view/loom-cutover"));
    expect(router.state.location.searchStr).toBe("");
  });

  it("comes back after the delay the service named when two builds are already running", async () => {
    let posts = 0;
    handler = (c) => {
      const p = pathOf(c);
      if (p === "/v1/spec" && c.method === "POST") {
        posts += 1;
        return posts === 1
          ? json(429, { error: "two spec builds already in flight" }, { "retry-after": "0" })
          : json(200, SPEC);
      }
      if (p === "/v1/specs/loom-cutover") return json(200, SPEC);
      if (p === "/v1/search") return json(200, { mode: "lexical", chains: CHAINS });
      return json(500, { error: "unexpected" });
    };
    const router = await mountApp("/?q=cutover");
    await screen.findByText("Loom cutover schedule", { selector: ".ibsubj" });
    click(screen.getAllByRole("checkbox")[0]!);
    await braid("Loom cutover");

    // "not yet", with a time attached, is the one decline worth re-asking
    await waitFor(() => expect(router.state.location.pathname).toBe("/view/loom-cutover"));
    expect(posts).toBe(2);
  });

  it("does not re-ask when the embedding deadline was missed", async () => {
    let posts = 0;
    handler = (c) => {
      if (pathOf(c) === "/v1/spec" && c.method === "POST") {
        posts += 1;
        return json(504, { error: "embedding deadline exceeded" });
      }
      if (pathOf(c) === "/v1/search") return json(200, { mode: "lexical", chains: CHAINS });
      return json(500, { error: "unexpected" });
    };
    await mountApp("/?q=cutover");
    await screen.findByText("Loom cutover schedule", { selector: ".ibsubj" });
    click(screen.getAllByRole("checkbox")[0]!);
    await braid();

    expect((await screen.findByRole("alert")).textContent).toContain("Timed out (504)");
    await new Promise((r) => setTimeout(r, 120));
    expect(posts).toBe(1);
  });

  it("keeps the wait visible while the spec is built", async () => {
    let release: (r: Response) => void = () => {};
    handler = (c) => {
      const p = pathOf(c);
      if (p === "/v1/spec" && c.method === "POST")
        return new Promise<Response>((res) => (release = res));
      if (p === "/v1/specs/loom-cutover") return json(200, SPEC);
      if (p === "/v1/search") return json(200, { mode: "lexical", chains: CHAINS });
      return json(500, { error: "unexpected" });
    };
    await mountApp("/?q=cutover");
    await screen.findByText("Loom cutover schedule", { selector: ".ibsubj" });

    click(screen.getAllByRole("checkbox")[0]!);
    await braid("Loom cutover");

    const status = await screen.findByRole("status");
    expect(status.textContent).toContain("boilerplate");
    expect((await screen.findByRole("button", { name: "Braiding…" })).hasAttribute("disabled")).toBe(true);

    await act(async () => {
      release(json(200, SPEC));
    });
    // The save succeeded and the page route opens.
    await screen.findByText("Loom cutover");
  });
});

describe("a spec named on the URL", () => {
  it("still loads from the file path, without touching the API", async () => {
    handler = (c) =>
      c.url.endsWith("/synthetic.json")
        ? json(200, SPEC)
        : json(404, { error: "not a spec route" });
    await mountApp("/?spec=/synthetic.json");

    await screen.findByText("Loom cutover");
    // The spec file is fetched, and only the spec file: the banner's auth
    // probe is filtered out, since it is a shell concern, not this route's.
    const specCalls = calls.filter((c) => pathOf(c) === "/synthetic.json");
    expect(specCalls.map((c) => new URL(c.url).pathname)).toEqual(["/synthetic.json"]);
  });
});

describe("the status route /status", () => {
  it("shows each service's state and the corpus coverage", async () => {
    handler = statusHandler;
    await mountApp("/status");

    await waitFor(() => expect(calls.some((c) => pathOf(c) === "/v1/status")).toBe(true));
    expect(calls.some((c) => pathOf(c) === "/v1/stats")).toBe(true);

    // Badges: two of the three are not fully in.
    expect(await screen.findByText("logged in")).toBeTruthy();
    expect(await screen.findByText("needs auth")).toBeTruthy();
    expect(await screen.findByText("down")).toBeTruthy();
    expect(await screen.findByText("Gmail")).toBeTruthy();
    // The detail under a not-ok row says what the fix is.
    expect(await screen.findByText("start it with `ollama serve`")).toBeTruthy();

    // The note shows when the state was last measured; the schedule is the
    // sweep section's own line now, since the cadence beside it is what decides
    // it. Exact text varies by the machine's timezone, so assert the sentence
    // holds a clock rather than a fixed string.
    const note = (await screen.findByText(/Last checked/)).textContent ?? "";
    expect(note).toMatch(/\d{1,2}:\d{2}/);

    // Corpus coverage from /v1/stats.
    expect(await screen.findByText("4281")).toBeTruthy();
    expect(await screen.findByText("mail entries")).toBeTruthy();
  });
});

// The sweep cadence is the one control on the status screen, and the only thing
// there that writes: the server owns the schedule (cmd/server/schedule.go), so
// what the page has to prove is that the value in force is the one shown and
// that choosing another stores it.
describe("the sweep cadence on /status", () => {
  it("shows the cadence in force with the next sweep it implies", async () => {
    handler = statusHandler;
    await mountApp("/status");

    const control = (await screen.findByLabelText("How often to sweep the mailbox")) as HTMLSelectElement;
    await waitFor(() => expect(control.value).toBe("10m"));
    // The option label, not the word: the page says "10 minutes" and stores "10m".
    expect(control.selectedOptions[0]!.textContent).toBe("10 minutes");

    const line = control.closest("dd")!.textContent ?? "";
    expect(line).toContain("next");
  });

  it("writes the cadence that was chosen, and takes the new schedule back", async () => {
    let stored = "10m";
    handler = (c) => {
      const p = pathOf(c);
      if (p === "/v1/settings") {
        if (c.method === "POST") stored = (JSON.parse(c.body ?? "{}") as { slurpEvery?: string }).slurpEvery ?? "";
        return json(200, { slurpEvery: stored });
      }
      return statusHandler(c);
    };
    await mountApp("/status");

    const control = (await screen.findByLabelText("How often to sweep the mailbox")) as HTMLSelectElement;
    await waitFor(() => expect(control.value).toBe("10m"));
    fireEvent.change(control, { target: { value: "30m" } });

    // Only the field being changed travels: the addresses and the folder are
    // left as they stand rather than sent again from a screen that never saw
    // them (see setSettings).
    await waitFor(() => {
      const writes = calls.filter((c) => pathOf(c) === "/v1/settings" && c.method === "POST");
      expect(writes.map((c) => JSON.parse(c.body!))).toEqual([{ slurpEvery: "30m" }]);
    });
    await waitFor(() => expect(control.value).toBe("30m"));
  });
});

describe("the default folder on /status", () => {
  it("shows the folder the home page opens in, from the labels the corpus has", async () => {
    handler = statusHandler;
    await mountApp("/status");

    const control = (await screen.findByLabelText(
      "Which folder the home page opens in",
    )) as HTMLSelectElement;
    await waitFor(() => expect(control.value).toBe("INBOX"));
    // The labels the corpus has, and the choice of none at the top — the same
    // words the home page's own folder button uses for it.
    expect([...control.options].map((o) => o.textContent)).toEqual(["All mail", "INBOX", "Work"]);

    // The row names what the control sets, and the control is that setting: the
    // page lists them as name and value rather than stating each one in a
    // sentence, so the label is the name rather than a clause around the field.
    const row = control.closest("dd")!;
    expect(row.previousElementSibling!.textContent).toBe("Home folder");
    expect(row.querySelector(".sttail")).toBeNull();
  });

  it("writes the folder that was chosen, and writes none when All mail is", async () => {
    let stored = "INBOX";
    handler = (c) => {
      const p = pathOf(c);
      if (p === "/v1/settings") {
        if (c.method === "POST") {
          stored = (JSON.parse(c.body ?? "{}") as { defaultFolder?: string }).defaultFolder ?? "";
        }
        return json(200, { slurpEvery: "10m", defaultFolder: stored });
      }
      return statusHandler(c);
    };
    await mountApp("/status");

    const control = (await screen.findByLabelText(
      "Which folder the home page opens in",
    )) as HTMLSelectElement;
    await waitFor(() => expect(control.value).toBe("INBOX"));
    fireEvent.change(control, { target: { value: "Work" } });

    // Only the folder travels: the cadence beside it is left as it stands rather
    // than sent again from this screen (see setSettings).
    await waitFor(() => {
      const writes = calls.filter((c) => pathOf(c) === "/v1/settings" && c.method === "POST");
      expect(writes.map((c) => JSON.parse(c.body!))).toEqual([{ defaultFolder: "Work" }]);
    });
    await waitFor(() => expect(control.value).toBe("Work"));

    // All mail is the empty value, not a folder named All mail: no default is a
    // state of the setting, and the server stores it as one.
    fireEvent.change(control, { target: { value: "" } });
    await waitFor(() => expect(control.value).toBe(""));
    expect(calls.filter((c) => c.method === "POST").at(-1)!.body).toBe('{"defaultFolder":""}');
  });

  it("shows a folder the corpus has no label for rather than snapping to All mail", async () => {
    // The server does not validate the folder against the label list — it may be
    // one the next sweep brings in — so the control has to be able to render the
    // value it was handed, or it would silently show a setting that is not in
    // force and write that back on the next change.
    handler = (c) => {
      const p = pathOf(c);
      if (p === "/v1/settings") return json(200, { slurpEvery: "10m", defaultFolder: "Later" });
      return statusHandler(c);
    };
    await mountApp("/status");

    const control = (await screen.findByLabelText(
      "Which folder the home page opens in",
    )) as HTMLSelectElement;
    await waitFor(() => expect(control.value).toBe("Later"));
    expect([...control.options].map((o) => o.textContent)).toEqual(["All mail", "INBOX", "Work", "Later"]);
  });
});

/**
 * Who the reader is: which mail is theirs. The setting lives here rather than
 * beside the build button on the inbox and the search page, because it is not a
 * fact about a page being built — it decides which messages are marked as the
 * reader's wherever mail is read, including the threads nobody is building a
 * page from. This screen is where the settings are, so it is where this one is
 * set.
 *
 * A person is picked, not a list of addresses: zach@termina.io and
 * zach@threadlet.com.au behind one account are one person in the corpus's
 * identity graph, and retyping that folding by hand is what this control stopped
 * asking for. What is stored is that person's id — not their addresses, which the
 * server resolves from the identity graph on every read and serves beside it —
 * so the tests assert the id travels and the addresses come back.
 */
describe("who you are on /status", () => {
  const settingsWrites = () => calls.filter((c) => pathOf(c) === "/v1/settings" && c.method === "POST");
  // The addresses each person is known by, as the server would resolve them. The
  // client neither sends these nor derives them: it shows what it was served.
  const ADDRESSES: Record<number, string[]> = {
    1: ["ada@okoye.example", "ada@work.example"],
    2: ["bo@halvorsen.example"],
  };
  /**
   * The settings endpoint as the server implements it: a person id is stored, and
   * that person's addresses are served beside it. A write of 0 is nobody, which
   * clears the setting and the addresses with it — a state of its own rather than
   * an address list that happens to be empty.
   */
  const settingsHandler = (person?: number, addresses: string[] = []): Handler => {
    let mePersonId = person;
    let me = addresses;
    return (c) => {
      const p = pathOf(c);
      if (p === "/v1/settings") {
        if (c.method === "POST") {
          const body = JSON.parse(c.body ?? "{}") as { mePersonId?: number };
          mePersonId = body.mePersonId === 0 ? undefined : body.mePersonId;
          me = mePersonId === undefined ? [] : ADDRESSES[mePersonId] ?? [];
        }
        return json(200, {
          slurpEvery: "10m",
          defaultFolder: "INBOX",
          ...(mePersonId === undefined ? {} : { mePersonId }),
          ...(me.length ? { me } : {}),
        });
      }
      return statusHandler(c);
    };
  };
  const options = (control: HTMLSelectElement) => [...control.options].map((o) => o.textContent);
  const stored = (c: { body?: string | null }) => JSON.parse(c.body ?? "{}");

  it("offers the corpus's people, and writes the person that was picked", async () => {
    handler = settingsHandler();
    await mountApp("/status");

    const control = (await screen.findByLabelText("Which person you are")) as HTMLSelectElement;
    // The people the corpus has an address for, most involved first, and the
    // choice of none ahead of them. The person known only by a recovered display
    // name is not offered: no mail came from them, so nothing could be marked.
    await waitFor(() => expect(options(control)).toEqual(["Nobody", "Ada Byron", "Bo Halvorsen"]));
    expect(control.value).toBe("");
    expect(control.closest("dd")!.textContent).toContain("nothing is marked as yours");
    expect(settingsWrites()).toHaveLength(0);

    fireEvent.change(control, { target: { value: "1" } });
    await waitFor(() => expect(settingsWrites()).toHaveLength(1));
    // The person's id travels, and nothing else: which addresses are one human is
    // the corpus's reading, and a page that sent a list would be storing a second
    // one that goes stale the moment an alias is learned. Only `mePersonId` is
    // named in the body, which is what leaves the cadence and the folder as they
    // stand.
    expect(stored(settingsWrites()[0]!)).toEqual({ mePersonId: 1 });
    // And the sentence names both halves of the answer: the person the control
    // shows, and the aliases the corpus resolved them to.
    await waitFor(() => expect(control.selectedOptions[0]!.textContent).toBe("Ada Byron"));
    expect(control.closest("dd")!.textContent).toContain(
      "Ada Byron: ada@okoye.example, ada@work.example",
    );
  });

  it("opens on the person that is stored, and reads it without writing", async () => {
    handler = settingsHandler(1);
    await mountApp("/status");

    const control = (await screen.findByLabelText("Which person you are")) as HTMLSelectElement;
    await waitFor(() => expect(control.selectedOptions[0]!.textContent).toBe("Ada Byron"));
    // Reading the setting is not writing it: a page load must not post back the
    // value it just read, not even to widen it to the person's other aliases.
    expect(settingsWrites()).toHaveLength(0);
  });

  it("opens on the address list a corpus was configured with, and replaces it", async () => {
    // The setting a corpus had before this control named a person: there is no
    // person id to open on, so the addresses are shown as the value in force — the
    // way the folder and cadence controls keep a value they cannot name — rather
    // than as nobody, which would be a setting that is not what is stored.
    handler = settingsHandler(undefined, ["old@elsewhere.example"]);
    await mountApp("/status");

    const control = (await screen.findByLabelText("Which person you are")) as HTMLSelectElement;
    await waitFor(() => expect(control.selectedOptions[0]!.textContent).toBe("old@elsewhere.example"));
    expect(options(control)).toEqual(["Nobody", "Ada Byron", "Bo Halvorsen", "old@elsewhere.example"]);
    expect(control.closest("dd")!.textContent).toContain("old@elsewhere.example");

    // Nobody is a choice, asked for as the zero that is not a person id, and it
    // clears the list rather than leaving it to be read again afterwards.
    fireEvent.change(control, { target: { value: "" } });
    await waitFor(() => expect(settingsWrites()).toHaveLength(1));
    expect(stored(settingsWrites()[0]!)).toEqual({ mePersonId: 0 });
    await waitFor(() => expect(control.value).toBe(""));
    expect(control.closest("dd")!.textContent).toContain("nothing is marked as yours");
  });
});

/** The /specs index: saved pages, newest first, each named by its URL. */
const SPECS = {
  specs: [
    { name: "loom-cutover", title: "Loom cutover", savedAt: "2026-08-22T15:04:00Z" },
    // A second, older page sharing the title proves the row resolves by name +
    // when, not by title alone.
    { name: "cutover-note", title: "Loom cutover", savedAt: "2026-08-01T09:30:00Z" },
  ],
};

const specsHandler: Handler = (c) =>
  pathOf(c) === "/v1/specs"
    ? json(200, SPECS)
    : json(500, { error: `unexpected call to ${c.method} ${pathOf(c)}` });

/**
 * The site nav is a header now, not a footer: one set of cross-links at the top
 * of every route, above the page's own header. The point of the test is the
 * move — the shell cannot quietly grow a footer again, and every link stays
 * reachable without scrolling to the end of a long transcript. The site's name
 * is the first link, and the way home: the pages it heads no longer repeat it in
 * a title block of their own.
 */
describe("the site navigation", () => {
  it("is the header above the page's own, and nothing renders a footer", async () => {
    handler = () => json(200, { signed_in: true });
    await mountApp("/");

    const site = document.querySelector("header.sitehead");
    if (!site) throw new Error("the shell rendered no site header");
    // Document order is the claim: the site nav is the first thing on the page.
    expect(document.querySelectorAll("header")[0]).toBe(site);
    for (const name of ["chainmail", "Braids", "Settings", "Ops"]) {
      expect(within(site as HTMLElement).getByRole("link", { name })).toBeTruthy();
    }
    expect(document.querySelector("footer")).toBeNull();
  });

  it("names the site, and that name is the link home", async () => {
    handler = () => json(200, { signed_in: true });
    // A page away from home, so the brand is not simply where we already are.
    await mountApp("/status");

    const brand = document.querySelector("header.sitehead a.brand") as HTMLAnchorElement | null;
    if (!brand) throw new Error("no site name in the nav");
    expect(brand.textContent).toBe("chainmail");
    expect(new URL(brand.href).pathname).toBe("/");
  });

  it("re-reads the corpus on the refresh button, and says so while it is asking", async () => {
    handler = buildHandler;
    await mountApp("/");
    await screen.findByText("Loom cutover schedule");
    const before = searchCalls().length;

    // Hold the next answer open, so the state a reader sees while the corpus is
    // being re-read is the state this asserts on.
    let release: (r: Response) => void = () => {};
    handler = (c) =>
      pathOf(c) === "/v1/search"
        ? new Promise<Response>((res) => (release = res))
        : buildHandler(c);

    const button = screen.getByRole("button", { name: "Refresh" });
    // The stamp's neighbour, in the nav's own right-hand group — the row where
    // "is my merge live" is written, which is the page whose answer a refresh can
    // change.
    expect(button.closest(".navright")).toBeTruthy();
    expect(button.closest("header.sitehead")).toBeTruthy();
    click(button);

    await waitFor(() => expect(button.getAttribute("aria-busy")).toBe("true"));
    expect(button).toHaveProperty("disabled", true);
    // The indicator is dripfeed-web's spinner: a span whose ::before is the ↻ the
    // stylesheet turns (see .navrefresh .spinner).
    expect(button.querySelector(".spinner")).toBeTruthy();
    expect(searchCalls().length).toBeGreaterThan(before);

    await act(async () => {
      release(json(200, { mode: "lexical", chains: CHAINS }));
    });
    await waitFor(() => expect(button.getAttribute("aria-busy")).toBe("false"));
    expect(button).toHaveProperty("disabled", false);
  });
});

describe("the specs index /specs", () => {
  it("lists every saved page, linked to its view route, ordered by saved-at", async () => {
    handler = specsHandler;
    await mountApp("/specs");

    await waitFor(() => expect(calls.some((c) => pathOf(c) === "/v1/specs" && c.method === "GET")).toBe(true));

    // Each title is shown and rates its own link to /view/<name>.
    expect(await screen.findAllByText("Loom cutover")).toHaveLength(2);
    const links: HTMLAnchorElement[] = screen.getAllByRole("link", { name: "Loom cutover" });
    expect(links.map((l) => l.getAttribute("href"))).toEqual(
      expect.arrayContaining(["/view/loom-cutover", "/view/cutover-note"]),
    );
  });
});

describe("the reply tree's reserved column", () => {
  const viewHandler: Handler = (c) =>
    pathOf(c) === "/v1/specs/loom-cutover"
      ? json(200, SPEC)
      : pathOf(c) === "/v1/search"
        ? json(200, { mode: "lexical", chains: [] })
        : json(500, { error: `unexpected call to ${pathOf(c)}` });

  it("is reserved only while a transcript is on screen", async () => {
    handler = viewHandler;
    const router = await mountApp("/view/loom-cutover");
    await screen.findByText("Loom cutover");
    // The transcript marks the body so the panel's measured width can be fed into
    // --panel; the toolbar and the reserved content column both inset from it.
    expect(document.body.classList.contains("hasmap")).toBe(true);

    // Leave for the inbox the way a reader does, client-side: the shell is one
    // page, so nothing reloads the body class away.
    click(screen.getByRole("link", { name: "chainmail" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));

    // No tree on the inbox, so no column reserved for one. The leftover was the
    // bug: the map's inset outlived the map, and every page visited afterwards
    // had a couple of hundred pixels of nothing down its right edge.
    expect(document.body.classList.contains("hasmap")).toBe(false);
    expect(document.body.style.getPropertyValue("--panel")).toBe("");
  });
});

// The render route /view/<name>.
describe("the render route /view/<name>", () => {
  it("loads the saved page from the API when the URL names one", async () => {
    handler = (c) =>
      pathOf(c) === "/v1/specs/loom-cutover"
        ? json(200, SPEC)
        : json(500, { error: "unexpected call" });
    await mountApp("/view/loom-cutover");

    await waitFor(() => expect(calls.some((c) => pathOf(c) === "/v1/specs/loom-cutover")).toBe(true));
    await screen.findByText("Loom cutover");
    // No back button: a page under /view/<name> just is, it was not the result
    // of a search.
    expect(screen.queryByRole("button", { name: /Back/ })).toBeNull();
  });

  it("names the browser tab after the loaded spec's title", async () => {
    handler = (c) =>
      pathOf(c) === "/v1/specs/loom-cutover"
        ? json(200, SPEC)
        : json(500, { error: "unexpected call" });
    await mountApp("/view/loom-cutover");
    await screen.findByText("Loom cutover");

    // The shell serves one static <title>; the spec replaces it with its own.
    await waitFor(() => expect(document.title).toBe("Loom cutover — Chainmail"));
  });

  it("moves the address bar to /view/<name> when a page is built", async () => {
    handler = buildHandler;
    const router = await mountApp("/?q=cutover");
    await screen.findByText("Loom cutover schedule", { selector: ".ibsubj" });
    // The bar appears with the first tick — nothing ticked is nothing to build
    // from — so the title is typed after it.
    click(screen.getAllByRole("checkbox")[0]!);
    await braid("Loom cutover");

    await waitFor(() => expect(router.state.location.pathname).toBe("/view/loom-cutover"));
    await screen.findByText("Loom cutover");
  });

  it("goes back to the inbox when the page is left", async () => {
    handler = (c) =>
      pathOf(c) === "/v1/specs/loom-cutover"
        ? json(200, SPEC)
        : json(500, { error: "unexpected call" });
    // Two history entries, so Back has somewhere to go.
    const router = await mountApp("/", "/view/loom-cutover");
    await screen.findByText("Loom cutover");

    await act(async () => {
      router.history.back();
    });
    await waitFor(() => expect(screen.queryByText("Loom cutover")).toBeNull());
    expect(router.state.location.pathname).toBe("/");
    expect(screen.getByRole("textbox", { name: "Search the corpus" })).toBeTruthy();
  });

  it("renders the client's own 404 view for a URL that is not a route", async () => {
    handler = () => json(500, { error: "no API call should happen for an unknown page" });
    await mountApp("/viwe/typo");

    expect(await screen.findByText(/No page at/)).toBeTruthy();
    // The 404 route itself must not touch the API; the shell's auth probe is
    // a separate concern and answered signed in by the shared handler.
    const routeCalls = calls.filter((c) => pathOf(c) !== "/auth/status");
    expect(routeCalls.length).toBe(0);
  });
});

describe("pressing refresh on a saved page", () => {
  // One line per phase, in the CLI's own shape: this is a transcript, and the
  // page shows it as one.
  const TRANSCRIPT = "[1/5] mail: created 2, changed 0\n[2/5] twins: no duplicates\n";

  const refreshHandler = (opts: { slurp: () => Response }) =>
    ((c: Call) => {
      const p = pathOf(c);
      if (p === "/v1/specs/loom-cutover") return json(200, SPEC);
      if (p === "/v1/slurp" && c.method === "POST") return opts.slurp();
      if (p === "/v1/refresh" && c.method === "POST")
        return json(200, {
          spec: SPEC,
          report: {
            entriesBefore: 4,
            entriesAfter: 4,
            nothingNew: true,
            chainsAdded: [],
            chainsGrown: [],
            chainsProposed: [],
            unranked: [],
          },
        });
      return json(500, { error: `unexpected call to ${c.method} ${p}` });
    }) as Handler;

  it("fetches from the mailbox before it re-derives, and shows what came back", async () => {
    const log = vi.spyOn(console, "log").mockImplementation(() => {});
    handler = refreshHandler({ slurp: () => json(200, { report: TRANSCRIPT }) });    await mountApp("/view/loom-cutover");
    await screen.findByText("Loom cutover");

    click(screen.getByRole("button", { name: "Re-derive this page from the corpus" }));

    // The order is the contract: re-deriving before the ingest would rebuild the
    // page from the very corpus the fetch was supposed to extend.
    await waitFor(() =>
      expect(calls.filter((c) => c.method === "POST").map(pathOf)).toEqual([
        "/v1/slurp",
        "/v1/refresh",
      ]),
    );
    // The rebuild still reports itself, and the fetch's own transcript survives
    // alongside it — in the console now, not a corner box.
    await waitFor(() =>
      expect(log.mock.calls.map((c) => c[0])).toEqual([
        TRANSCRIPT.trim(),
        "already up to date",
      ]),
    );
  });

  it("re-derives anyway on a host with no mailbox reach, and says so", async () => {
    // The deployed default: no -slurp, so the endpoint refuses. A 403 here is
    // not a failure of the button, it is the read-most fallback.
    const log = vi.spyOn(console, "log").mockImplementation(() => {});
    const err = vi.spyOn(console, "error").mockImplementation(() => {});
    handler = refreshHandler({
      slurp: () =>
        json(403, {
          error: "slurping is disabled: this server was started without -slurp, so it cannot reach the work mailbox.",
        }),
    });
    await mountApp("/view/loom-cutover");
    await screen.findByText("Loom cutover");

    click(screen.getByRole("button", { name: "Re-derive this page from the corpus" }));

    await waitFor(() =>
      expect(err).toHaveBeenCalledWith(
        expect.stringMatching(/no mailbox reach on this host/),
      ),
    );
    await waitFor(() =>
      expect(log.mock.calls.map((c) => c[0])).toEqual(["already up to date"]),
    );
  });

  it("reports a failed ingest without swallowing it", async () => {
    const err = vi.spyOn(console, "error").mockImplementation(() => {});
    handler = refreshHandler({
      slurp: () => json(502, { error: "slurp failed: docket refused: no threading headers" }),
    });
    await mountApp("/view/loom-cutover");
    await screen.findByText("Loom cutover");

    click(screen.getByRole("button", { name: "Re-derive this page from the corpus" }));

    // A failure that is not "disabled" is worth reading, so the server's own
    // words are what the console shows.
    await waitFor(() =>
      expect(err).toHaveBeenCalledWith(
        expect.stringMatching(/docket refused: no threading headers/),
      ),
    );
  });
});

describe("downloading a file the page does not hold yet", () => {
  // A page with one file the corpus does not hold. The chip is the download:
  // pressing it fetches that message's files, and the file opens in the window
  // over the page the moment the bytes are behind it. This is the wiring.
  const PULL_SPEC = {
    title: "Loom cutover",
    messages: [
      {
        date: "Mon 2 Mar 2026",
        time: "09:15",
        sender: "Ada Byron",
        extId: "mail:<loom-cutover-1@example.fed>",
        body: "<p>quote attached</p>",
        attachments: [
          { name: "shed.csv", kind: "CSV", size: "512 B", gmailId: "19d263bb5a6b00db" },
        ],
      },
    ],
  };

  /** What the file turns out to hold, once it is here. */
  const SHEET = "shed,readings\nNova,41.2";

  const report = {
    entriesBefore: 1,
    entriesAfter: 1,
    nothingNew: true,
    chainsAdded: [],
    chainsGrown: [],
    chainsProposed: [],
    unranked: [],
  };

  const handlerWith = (pull: () => Response): Handler => (c) => {
    const p = pathOf(c);
    if (p === "/v1/specs/loom-cutover") return json(200, PULL_SPEC);
    if (p === "/v1/media/pull" && c.method === "POST") return pull();
    if (p === "/v1/refresh" && c.method === "POST") return json(200, { spec: PULL_SPEC, report });
    // The bytes, once they are stored: the window reads them from this host.
    if (p.startsWith("/v1/attachments/"))
      return new Response(SHEET, { status: 200, headers: { "content-type": "text/csv" } });
    return json(500, { error: `unexpected call to ${c.method} ${p}` });
  };

  /** The chip in the bubble's own strip, which is the download. The sources panel
   *  lists the same file by name, and it is not the control being pressed. */
  const chip = () => document.querySelector(".atts a") as HTMLElement;

  it("asks for that message's files, and opens the file when they arrive", async () => {
    const log = vi.spyOn(console, "log").mockImplementation(() => {});
    // The page after the pull: the file has bytes and a way to be shown. This is
    // what the server sends back beside the counts, having re-derived and
    // rewritten the saved page itself.
    const pulled = {
      ...PULL_SPEC,
      messages: [
        {
          ...PULL_SPEC.messages[0],
          attachments: [
            {
              ...PULL_SPEC.messages[0]!.attachments[0],
              blobSha: "sha-of-the-bytes",
              open: "popup",
              view: "text",
            },
          ],
        },
      ],
    };
    handler = handlerWith(() =>
      json(200, {
        wanted: 2,
        pulled: 1,
        skipped: 1,
        failed: 0,
        bytes: 512,
        files: [
          { name: "shed.csv", source: "mail", sha: "sha-of-the-bytes", bytes: 512 },
          { name: "roof.mp4", source: "mail", reason: "too_large" },
        ],
        spec: pulled,
        report,
      }),
    );
    await mountApp("/view/loom-cutover");
    await screen.findByText("Loom cutover");

    // Before the press the chip is what it always was: a link to where the file
    // is today.
    expect(chip().getAttribute("href")).toContain("mail.google.com");
    click(chip());

    // One call, and no second one to be abandoned: the rebuild that makes the
    // bytes visible happens on the server, behind the fetch, so a reader who
    // reloads mid-pull still lands on a page that shows the file. The message
    // and the page both go in the request — the message is the fetch, the name
    // is what the server rewrites.
    await waitFor(() => expect(calls.filter((c) => c.method === "POST").length).toBe(1));
    const pull = calls.find((c) => pathOf(c) === "/v1/media/pull")!;
    expect(JSON.parse(pull.body!)).toEqual({
      entry: "mail:<loom-cutover-1@example.fed>",
      name: "loom-cutover",
    });
    expect(calls.some((c) => pathOf(c) === "/v1/refresh")).toBe(false);

    // Then it opens: the press was replayed once the renderer had bytes behind
    // the chip (behaviour.ts), and the window over the page is showing the file.
    const win = await waitFor(() => {
      const w = document.querySelector(".pop") as HTMLElement | null;
      if (!w || w.hidden) throw new Error("no window yet");
      return w;
    });
    expect(win.querySelector(".popcap")!.textContent).toBe("shed.csv");
    await waitFor(() =>
      expect(
        [...win.querySelectorAll(".poptable tbody td")].map((td) => td.textContent),
      ).toEqual(["Nova", "41.2"]),
    );
    // The chip now points at this host, which is what made the window possible.
    expect(chip().getAttribute("href")).toBe("/v1/attachments/sha-of-the-bytes");

    // A file that did not arrive is the reason the console is worth reading: the
    // page cannot show what is not there, and a count alone would not say why.
    await waitFor(() =>
      expect(log.mock.calls.map((c) => c[0]).join("\n")).toMatch(/roof\.mp4: too_large/),
    );
  });

  it("re-derives the page itself when the server sent none back", async () => {
    // A pull with no page in its answer: a host given no name, or a rebuild that
    // could not run. The bytes are stored either way, so the browser falls back
    // to asking for the page — the behaviour that used to be the only one.
    handler = handlerWith(() =>
      json(200, {
        wanted: 1,
        pulled: 1,
        skipped: 0,
        failed: 0,
        bytes: 512,
        files: [{ name: "shed.csv", source: "mail", sha: "sha-of-the-bytes", bytes: 512 }],
      }),
    );
    await mountApp("/view/loom-cutover");
    await screen.findByText("Loom cutover");

    click(chip());

    await waitFor(() =>
      expect(calls.filter((c) => c.method === "POST").map(pathOf)).toEqual([
        "/v1/media/pull",
        "/v1/refresh",
      ]),
    );
  });

  it("says so in the page on a host that will not fetch, and leaves the page alone", async () => {
    handler = handlerWith(() =>
      json(403, {
        error: "media pulls are disabled: this server was started without -media, so it will not fetch attachment bytes.",
      }),
    );
    await mountApp("/view/loom-cutover");
    await screen.findByText("Loom cutover");

    click(chip());

    // A press that spends mailbox round trips and then fails must not look like
    // nothing happening: console-only was the wrong place for it, so the page
    // says it — and the file is genuinely still missing, so the chip is still the
    // download it was, and pressing it again asks again.
    const note = await waitFor(() => {
      const n = document.querySelector(".pullnote");
      if (!n) throw new Error("no note yet");
      return n;
    });
    expect(note.textContent).toMatch(/cannot fetch files/);
    click(chip());
    await waitFor(() =>
      expect(calls.filter((c) => pathOf(c) === "/v1/media/pull")).toHaveLength(2),
    );
    // Nothing changed in the corpus, so nothing needs redrawing.
    expect(calls.some((c) => pathOf(c) === "/v1/refresh")).toBe(false);
  });
});

describe("adding another email to a page", () => {
  it("searches the corpus from the toolbar and adds the chosen thread by accept", async () => {
    const log = vi.spyOn(console, "log").mockImplementation(() => {});
    handler = (c) => {
      const p = pathOf(c);
      if (p === "/v1/specs/loom-cutover") return json(200, SPEC);
      if (p === "/v1/search")
        return json(200, { mode: "hybrid", chains: [CHAINS[1]] });
      if (p === "/v1/refresh" && c.method === "POST")
        return json(200, {
          spec: SPEC,
          report: {
            entriesBefore: 1,
            entriesAfter: 5,
            nothingNew: false,
            chainsAdded: ["mail:<lease-renewal-1@example.fed>"],
            chainsGrown: [],
            chainsProposed: [],
            unranked: [],
            queriesRecorded: ["lease"],
          },
        });
      if (p === "/auth/status") return json(200, { signed_in: true });
      return json(500, { error: `unexpected call to ${c.method} ${p}` });
    };
    await mountApp("/view/loom-cutover");
    await screen.findByText("Loom cutover");

    // The toolbar button only appears where a refresh can accept the choice.
    click(screen.getByRole("button", { name: "Search the corpus for another email to add to this page" }));
    const dialog = await screen.findByRole("dialog", { name: "Add another email" });

    // A fresh corpus search, scoped to the page, not a build.
    typeInto("Search query", "lease");
    const button = screen.getByRole("button", { name: "Search" }) as HTMLButtonElement;
    expect(button.disabled).toBe(false);
    fireEvent.submit(button.closest("form")!);
    await waitFor(() => expect(searchCalls().length).toBeGreaterThan(0));
    await screen.findByText("Warehouse lease renewal", { selector: ".ibsubj" });

    // One tick, then the add goes back through the same accept path a proposal
    // uses: re-run the refresh with the roots named. The checkbox is scoped to
    // the dialog — the page behind has exclusion checkboxes of its own.
    click(within(dialog).getAllByRole("checkbox")[0]!);
    click(within(dialog).getByRole("button", { name: "add 1 to page" }));
    await waitFor(() =>
      expect(calls.some((c) => pathOf(c) === "/v1/refresh")).toBe(true),
    );
    const body = JSON.parse(
      calls.find((c) => pathOf(c) === "/v1/refresh")!.body!,
    );
    expect(body.name).toBe("loom-cutover");
    expect(body.accept).toEqual(["mail:<lease-renewal-1@example.fed>"]);
    // The search that found the thread goes with it, so the page records where
    // the thread came from rather than gaining an unexplained one.
    expect(body.queries).toEqual([{ q: "lease", note: "add-email search, mode=hybrid" }]);
    // The modal closed once the add was sent.
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add another email" })).toBeNull());

    // The refreshed page reports the growth like any other refresh — console
    // only — and says the search was recorded with it.
    await waitFor(() =>
      expect(log).toHaveBeenCalledWith("refresh: 1 added, 1 search recorded"),
    );
  });
});

describe("the search lives in the URL", () => {
  it("restores the search from the query string on load", async () => {
    handler = (c) =>
      json(200, {
        mode: new URL(c.url).searchParams.get("mode"),
        chains: new URL(c.url).searchParams.get("mode") === "semantic" ? [CHAINS[1]] : [CHAINS[0]],
      });
    await mountApp("/?q=cutover&mode=semantic");

    // Both the box and the mode come back from the address, and the search ran
    // itself: the fields the person set are the ones on screen, and the results
    // are the answer to them. The box is also filled the way a current nav item
    // is — there is a question on the address, and the nav can say so here.
    await waitFor(() =>
      expect((screen.getByRole("textbox", { name: "Search the corpus" }) as HTMLInputElement).value).toBe("cutover"),
    );
    expect(
      screen.getByRole("textbox", { name: "Search the corpus" }).getAttribute("aria-current"),
    ).toBe("page");
    openSearch();
    expect((screen.getByLabelText("Mode") as HTMLSelectElement).value).toBe("semantic");
    await screen.findByText("Warehouse lease renewal", { selector: ".ibsubj" });
  });

  it("writes the search to the URL, and Back from a built page lands on it", async () => {
    handler = buildHandler;
    const router = await mountApp("/");
    await searchFromInbox("cutover");
    // The default mode is omitted from the URL — the canonical home search is
    // plain /.q=cutover, not a URL that spells out the default.
    await waitFor(() => expect(router.state.location.searchStr).toBe("?q=cutover"));

    await screen.findByText("Loom cutover schedule", { selector: ".ibsubj" });
    // The bar appears with the first tick, so the title waits for it.
    click(screen.getAllByRole("checkbox")[0]!);
    await braid("Loom cutover");
    await waitFor(() => expect(router.state.location.pathname).toBe("/view/loom-cutover"));
    await screen.findByText("Loom cutover");

    // Back: the address bar returns to the search that built this page, and
    // the search page comes back with its query and results, not a blank page.
    await act(async () => {
      router.history.back();
    });
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(router.state.location.searchStr).toBe("?q=cutover");
    await waitFor(() =>
      expect((screen.getByRole("textbox", { name: "Search the corpus" }) as HTMLInputElement).value).toBe("cutover"),
    );
    await screen.findByText("Loom cutover schedule", { selector: ".ibsubj" });
  });

  it("answers an empty search with the default view, not an empty search page", async () => {
    handler = buildHandler;
    const router = await mountApp("/?q=");

    // A box somebody emptied: nothing is being asked, so what belongs under it is
    // the inbox — the default view — rather than a search page carrying a
    // question nobody asked. The address is left as it is: the box wrote it, and
    // re-writing it would be a navigation for nothing.
    await waitFor(() => expect(document.querySelector(".ibwrap")).toBeTruthy());
    expect(document.querySelector(".selwrap")).toBeNull();
    expect(router.state.location.searchStr).toBe("?q=");
    // And the box says nothing about where the reader is, there being nothing in
    // it to be asking.
    expect(
      screen.getByRole("textbox", { name: "Search the corpus" }).getAttribute("aria-current"),
    ).toBeNull();
  });

  it("gives the search up when the box is emptied, and the inbox is what is left", async () => {
    handler = buildHandler;
    const router = await mountApp("/?q=cutover");
    await screen.findByText("Loom cutover schedule", { selector: ".ibsubj" });
    // Asking a new question does not clear the answer to the old one on the way:
    // the arm the reader is on keeps its question until they commit another.
    expect(document.querySelector(".selwrap")).toBeTruthy();

    const box = openSearch();
    fireEvent.change(box, { target: { value: "" } });
    // Submitted directly because the button is disabled with nothing to ask — the
    // same reason submitSearch() asserts on the button before submitting: an
    // empty box asks nothing, so committing it is a giving-up rather than a
    // search.
    fireEvent.submit(box.closest("form")!);

    await waitFor(() => expect(router.state.location.searchStr).toBe(""));
    await waitFor(() => expect(document.querySelector(".ibwrap")).toBeTruthy());
    expect(document.querySelector(".selwrap")).toBeNull();
    expect((box as HTMLInputElement).value).toBe("");
  });

  it("searches from the panel's button, not only from its Enter key", async () => {
    handler = buildHandler;
    const router = await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    const box = openSearch();
    fireEvent.change(box, { target: { value: "cutover" } });
    // The panel is a form, so its button and its Enter key are one code path; the
    // button is what makes Enter work at all, and it is the way in for anyone who
    // does not know the field submits.
    submitSearch();

    await waitFor(() => expect(router.state.location.searchStr).toBe("?q=cutover"));
    await screen.findByText("Loom cutover schedule", { selector: ".ibsubj" });
    expect(document.querySelector(".ibwrap")).toBeNull();
  });

  it("narrows the search to a person rather than to a string", async () => {
    handler = buildHandler;
    const router = await mountApp("/");
    await screen.findByText("Loom cutover schedule");

    const box = openSearch();
    fireEvent.change(box, { target: { value: "cutover" } });
    const person = (await screen.findByLabelText("Person")) as HTMLInputElement;

    // The corpus's people are under the field as it is typed in, looked up by
    // name or by address: the addresses are the corpus's to fold, so the reader
    // finds Ada by the half of her name they remember rather than having to
    // recall which of her two addresses to type.
    fireEvent.focus(person);
    fireEvent.change(person, { target: { value: "byron" } });
    const ada = await screen.findByRole("option", { name: /Ada Byron/ });
    expect(screen.queryByRole("option", { name: /Bo Halvorsen/ })).toBeNull();
    // The alias the pick will write is on the row itself, so which of her two
    // addresses this search is narrowed to is read before it is asked.
    expect(ada.textContent).toContain("ada@okoye.example");

    // Picking one commits at once, with the query that was typed beside it: who is
    // being asked about is a decision about a question already being asked.
    fireEvent.click(ada);
    await waitFor(() =>
      expect(router.state.location.searchStr).toContain("person=ada%40okoye.example"),
    );
    await waitFor(() => {
      const asked = new URL(searchCalls().at(-1)!.url).searchParams;
      expect(asked.get("q")).toBe("cutover");
      expect(asked.get("person")).toBe("ada@okoye.example");
    });
  });

  it("keeps a person the people list cannot name, rather than snapping it away", async () => {
    handler = buildHandler;
    // An address typed into the address bar, or a search settled before the people
    // answered: the field shows it as the value it is, and the suggestions under
    // it are a suggestion rather than a correction. A field that quietly reset it
    // would be answering a different question than the one on screen.
    const router = await mountApp("/?q=cutover&person=someoneelse%40example.fed");
    await screen.findByText("Loom cutover schedule", { selector: ".ibsubj" });

    openSearch();
    const person = (await screen.findByLabelText("Person")) as HTMLInputElement;
    await waitFor(() => expect(person.value).toBe("someoneelse@example.fed"));
    // And opening the panel asked nothing: the field is showing the question in
    // force rather than one it has decided to replace.
    expect(router.state.location.searchStr).toContain("person=someoneelse%40example.fed");
  });
});
// A page carrying a quoter's edit (#42): the host message repeats a chunk the
// quoter changed, with an `edits` record naming the original and the change.
const EDIT_SPEC = {
  title: "CSV layout",
  messages: [
    {
      id: "c-orig", date: "Fri 21 Aug 2026", time: "09:00", tz: "+1000",
      sender: "Charles XPTO", org: "ruralco",
      body: "<p>CSV layout: A: Member Number &middot; E: Amount Due</p>",
    },
    {
      id: "j-host", date: "Fri 21 Aug 2026", time: "14:00", tz: "+1000",
      sender: "Jason Yago", org: "termina", parent: "c-orig",
      body: "<p>Actually one change — we track Invoice Amount.</p>",
      edits: [{
        id: "c-edit", base: "c-orig", who: "Jason Yago", time: "14:00",
        body: "CSV layout: A: Member Number \u00b7 E: Invoice Amount",
      }],
    },
  ],
};


describe("in-page anchor links", () => {
  it("are plain #-fragment links that no JS intercepts", async () => {
    handler = (c) =>
      pathOf(c) === "/v1/specs/loom-cutover"
        ? json(200, EDIT_SPEC)
        : json(500, { error: "unexpected call" });
    // With scroll-behavior:smooth in CSS, a plain href="#id" link smooth-scrolls
    // natively — the browser does the rest. jsdom cannot run that native
    // navigation (and the router intercepts clicks), so the contract to pin is
    // that the link is a same-page fragment AND our code no longer hijacks the
    // click with a scrollIntoView (the earlier JS handler is gone).
    const scrolled: Array<[Element, ScrollIntoViewOptions | undefined]> = [];
    const orig = Element.prototype.scrollIntoView;
    Element.prototype.scrollIntoView = function (arg?: ScrollIntoViewOptions) {
      scrolled.push([this, arg]);
    };
    try {
      await mountApp("/view/loom-cutover");
      const anchor = (await screen.findByText("original")).closest("a")!;
      // A bare # fragment into the page's own message — left to the browser.
      expect(anchor.getAttribute("href")).toBe("#c-orig");
      expect(document.getElementById("c-orig")).not.toBeNull();
      click(anchor);
      // Our JS asked for no scroll; whether the browser scrolls is up to it.
      expect(scrolled).toHaveLength(0);
    } finally {
      Element.prototype.scrollIntoView = orig;
    }
  });
});

describe("the reply link on a built page", () => {
  it("names the message each one answers, and links to its row here", async () => {
    handler = (c) =>
      pathOf(c) === "/v1/specs/loom-cutover"
        ? json(200, EDIT_SPEC)
        : json(500, { error: "unexpected call" });
    await mountApp("/view/loom-cutover");
    await screen.findByText("CSV layout");

    // The reply: the parent's own name and clock, in the words the parent's
    // bubble prints them in, pointing at the parent's row on this page.
    const par = document.querySelector(".msg .hdr .par");
    expect(par?.getAttribute("href")).toBe("#c-orig");
    expect(par?.querySelector(".parlbl")?.textContent).toBe(
      "in reply to Charles XPTO, Fri 21 Aug 2026 09:00",
    );
    expect(par?.getAttribute("title")).toBe("In reply to Charles XPTO, Fri 21 Aug 2026 09:00");
    expect(document.getElementById("c-orig")?.textContent).toContain("Charles XPTO");

    // The opener answers nothing and says so, rather than leaving the line off.
    const first = document.querySelector(".msg .hdr .tstart");
    expect(first?.textContent).toBe("thread start");
  });
});

describe("a quoter's edit in the transcript", () => {
  it("renders the edited quote and an attributed 'original from … at …' header", async () => {
    handler = (c) =>
      pathOf(c) === "/v1/specs/loom-cutover"
        ? json(200, EDIT_SPEC)
        : json(500, { error: "unexpected call" });
    await mountApp("/view/loom-cutover");
    const original = await screen.findByText("original");
    const anchor = original.closest("a");
    expect(anchor?.getAttribute("href")).toBe("#c-orig");
    const headerText = anchor?.parentElement?.textContent ?? "";
    expect(headerText).toContain("edited by Jason Yago");
    expect(headerText).toContain("original from Charles XPTO");
    expect(headerText).toContain("at Fri 21 Aug 2026 09:00");
    expect(screen.getByText("Invoice")).toBeTruthy();
  });
});

/**
 * The /ops review surface. Every name, address and id is invented, with the
 * same .example domains the backend fixtures use — the ops screen shows people
 * data, so nothing here may be a real person's.
 */
const OPS_PLAN_BEFORE = {
  people: 5,
  merges: [
    {
      rule: "dedupe:same-display-name",
      keepId: 7,
      keepName: "Ada Okoye",
      keepIdentities: ["email:ada@loomworks.example"],
      dropId: 8,
      dropName: "Ada Okoye",
      dropIdentities: ["display_name:ada okoye"],
      evidence: "name-only person, and the kept person is on every entry they are",
      applicable: true,
    },
    {
      rule: "dedupe:same-display-name-in-thread",
      keepId: 21,
      keepName: "Bo Halvorsen",
      keepIdentities: ["email:bo@fjordline.example"],
      dropId: 22,
      dropName: "Bo Halvorsen",
      dropIdentities: ["display_name:bo halvorsen"],
      evidence: "name-only person, and the kept person is in every thread they appear in",
      applicable: true,
    },
    {
      rule: "dedupe:first-name-and-org",
      keepId: 9,
      keepName: "Camille Vaughn",
      keepIdentities: ["email:camille.vaughn@millrace.example"],
      dropId: 10,
      dropName: "Camille Vaughn",
      dropIdentities: ["email:camille@quarry.example"],
      evidence: "first name camille at millrace.example",
      applicable: false,
    },
  ],
  refusals: [
    {
      rule: "dedupe:same-display-name",
      subject: "dai rhys",
      reason: "two people of that name fit the evidence equally",
      people: [11, 12, 13],
    },
  ],
  candidates: [
    {
      aId: 14,
      aName: "Bryn Lowther",
      aAddresses: ["bryn@quarry.example"],
      bId: 15,
      bName: "Bryn Lowther",
      bAddresses: ["bryn.lowther@millrace.example"],
      reason: "same local part, different domain",
      suggest: "corpus alias -from quarry.example -to millrace.example",
    },
  ],
  twinsDeclined: [{ reason: "no other copy within a plausible offset of its stated clock", count: 612 }],
  trail: [],
};

const OPS_RECORD = {
  keepId: 7,
  keepName: "Ada Okoye",
  dropId: 8,
  dropName: "Ada Okoye",
  reason: "dedupe:same-display-name (name-only person, and the kept person is on every entry they are)",
  mergedAt: "2026-08-22T15:04:00Z",
};

const OPS_RECORD_2 = {
  keepId: 21,
  keepName: "Bo Halvorsen",
  dropId: 22,
  dropName: "Bo Halvorsen",
  reason:
    "dedupe:same-display-name-in-thread (name-only person, and the kept person is in every thread they appear in)",
  mergedAt: "2026-08-22T15:05:00Z",
};

/** The plan as the server re-derives it once the named people are folded in. */
const planAfter = (drops: number[], trail: unknown[] = []) => ({
  ...OPS_PLAN_BEFORE,
  people: OPS_PLAN_BEFORE.people - drops.length,
  merges: OPS_PLAN_BEFORE.merges.filter((m) => !drops.includes(m.dropId)),
  trail,
});

const OPS_PLAN_AFTER = planAfter([8], [OPS_RECORD]);

describe("the ops route /ops", () => {
  it("shows the plan with the evidence, and folds one ticked pair behind a confirm", async () => {
    let applied = false;
    handler = (c) => {
      const p = pathOf(c);
      if (p === "/v1/ops/plan") return json(200, applied ? OPS_PLAN_AFTER : OPS_PLAN_BEFORE);
      if (p === "/v1/ops/merge" && c.method === "POST") {
        applied = true;
        return json(200, { merge: OPS_RECORD });
      }
      return json(500, { error: `unexpected call to ${c.method} ${p}` });
    };
    await mountApp("/ops?tab=merges");

    // The applicable pairs: the evidence is on screen, and so is a checkbox.
    expect(
      await screen.findByText(/name-only person, and the kept person is on every entry they are/),
    ).toBeTruthy();
    expect(screen.getAllByText("apply")).toHaveLength(2);
    expect(screen.getByText("read-only")).toBeTruthy();
    // Two applicable pairs and the select-all; the read-only tier gets none,
    // because the server refuses it whatever this screen renders.
    expect(screen.getAllByRole("checkbox")).toHaveLength(3);
    expect(screen.queryByLabelText(/Select folding #10/)).toBeNull();
    expect(
      (screen.getByRole("button", { name: "merge 0 selected" }) as HTMLButtonElement).disabled,
    ).toBe(true);

    // The reads: the refusal, the twins aggregate, the empty trail.
    expect(await screen.findByText(/two people of that name fit the evidence equally/)).toBeTruthy();
    expect(screen.getByText(/twins pass declined 612 entries/)).toBeTruthy();
    expect(screen.getByText("0 merges recorded — the person_merges trail")).toBeTruthy();

    // Ticking asks for a confirm; nothing has left the browser yet.
    click(screen.getByLabelText("Select folding #8 Ada Okoye into #7 Ada Okoye"));
    click(screen.getByRole("button", { name: "merge 1 selected" }));
    expect(await screen.findByText(/This cannot be undone/)).toBeTruthy();
    expect(calls.some((c) => pathOf(c) === "/v1/ops/merge")).toBe(false);

    // The confirming click names the pair and sends it.
    click(screen.getByRole("button", { name: "merge this pair" }));
    await waitFor(() =>
      expect(calls.some((c) => pathOf(c) === "/v1/ops/merge" && c.method === "POST")).toBe(true),
    );
    const post = calls.find((c) => pathOf(c) === "/v1/ops/merge");
    expect(JSON.parse(post!.body ?? "{}")).toEqual({ keepId: 7, dropId: 8 });

    // Success: the note counts the batch, the plan was refetched, and the folded
    // pair is gone from the list — the other applicable pair is still offered.
    expect(await screen.findByText("merged 1 pair — the plan below is the current one.")).toBeTruthy();
    await waitFor(() => expect(screen.queryByLabelText(/Select folding #8/)).toBeNull());
    expect(screen.getByLabelText(/Select folding #22/)).toBeTruthy();
    expect(screen.getByText("1 merge recorded — the person_merges trail")).toBeTruthy();
  });

  it("folds several ticked pairs behind one confirm, dropping each as it applies", async () => {
    const drops: number[] = [];
    handler = (c) => {
      const p = pathOf(c);
      if (p === "/v1/ops/plan")
        return json(
          200,
          drops.length
            ? planAfter(drops, [OPS_RECORD, OPS_RECORD_2].slice(0, drops.length))
            : OPS_PLAN_BEFORE,
        );
      if (p === "/v1/ops/merge" && c.method === "POST") {
        const body = JSON.parse(c.body ?? "{}") as { dropId: number };
        drops.push(body.dropId);
        return json(200, { merge: body.dropId === 8 ? OPS_RECORD : OPS_RECORD_2 });
      }
      return json(500, { error: `unexpected call to ${c.method} ${p}` });
    };
    await mountApp("/ops?tab=merges");
    await screen.findByLabelText(/Select folding #8/);

    // Select-all ticks every applicable pair, and only those.
    click(screen.getByLabelText("select all 2 applicable"));
    click(screen.getByRole("button", { name: "merge 2 selected" }));
    expect(await screen.findByText(/These 2 pairs will be folded/)).toBeTruthy();
    // The confirm names them, so the irreversible batch is readable first.
    expect(screen.getByText("#8 Ada Okoye")).toBeTruthy();
    expect(screen.getByText("#22 Bo Halvorsen")).toBeTruthy();

    click(screen.getByRole("button", { name: "merge these 2 pairs" }));
    // One POST per pair, in the order the plan lists them: the endpoint's
    // contract is a single pair, and the server re-derives the plan for each.
    await waitFor(() =>
      expect(calls.filter((c) => pathOf(c) === "/v1/ops/merge").length).toBe(2),
    );
    expect(
      calls.filter((c) => pathOf(c) === "/v1/ops/merge").map((c) => JSON.parse(c.body ?? "{}")),
    ).toEqual([
      { keepId: 7, dropId: 8 },
      { keepId: 21, dropId: 22 },
    ]);

    // Both are gone: no checkbox left for either, no action bar (nothing
    // applicable remains), and the read-only tier is still listed.
    expect(await screen.findByText("merged 2 pairs — the plan below is the current one.")).toBeTruthy();
    await waitFor(() => expect(screen.queryAllByRole("checkbox")).toHaveLength(0));
    expect(screen.getByText("read-only")).toBeTruthy();
    expect(screen.getByText("2 merges recorded — the person_merges trail")).toBeTruthy();
  });

  it("stops the batch at a refusal and says how far it got", async () => {
    const drops: number[] = [];
    handler = (c) => {
      const p = pathOf(c);
      if (p === "/v1/ops/plan")
        return json(200, drops.length ? planAfter(drops, [OPS_RECORD]) : OPS_PLAN_BEFORE);
      if (p === "/v1/ops/merge" && c.method === "POST") {
        const body = JSON.parse(c.body ?? "{}") as { dropId: number };
        if (body.dropId === 22)
          return json(409, {
            error:
              "21 <- 22 is not in the current dedupe plan — already merged, or the corpus changed since this screen loaded",
          });
        drops.push(body.dropId);
        return json(200, { merge: OPS_RECORD });
      }
      return json(500, { error: `unexpected call to ${c.method} ${p}` });
    };
    await mountApp("/ops?tab=merges");
    await screen.findByLabelText(/Select folding #8/);
    click(screen.getByLabelText("select all 2 applicable"));
    click(screen.getByRole("button", { name: "merge 2 selected" }));
    click(await screen.findByRole("button", { name: "merge these 2 pairs" }));

    // The first pair applied, the second was refused, and the message says which
    // — a batch reporting only "failed" would hide that one merge had landed.
    expect(
      await screen.findByText(/1 of 2 merged, then folding #22 into #21 was refused/),
    ).toBeTruthy();
    expect(screen.getByText(/not in the current dedupe plan/)).toBeTruthy();
    expect(calls.filter((c) => pathOf(c) === "/v1/ops/merge").length).toBe(2);
    // The pair that did apply is gone from the list all the same.
    await waitFor(() => expect(screen.queryByLabelText(/Select folding #8/)).toBeNull());
    expect(screen.getByLabelText(/Select folding #22/)).toBeTruthy();
  });
});

describe("what the bar does to the mail", () => {
  /** The chains this suite ticks are the search page's rows: they are the ones
   *  reachable without a second fixture, and the bar is the same bar on both
   *  pages. */
  const mailHandler = (answer: Response): Handler => (c) => {
    const p = pathOf(c);
    if (p === "/v1/mail") return answer;
    if (p === "/v1/labels") {
      return json(200, {
        labels: [
          { name: "INBOX", messages: 210 },
          { name: "Work", messages: 41 },
          { name: "Archive", messages: 5 },
        ],
      });
    }
    return buildHandler(c);
  };

  const ticksTwo = async () => {
    await screen.findByText("Loom cutover schedule", { selector: ".ibsubj" });
    const boxes = screen.getAllByRole("checkbox");
    click(boxes[0]!);
    click(boxes[1]!);
  };

  it("archives every ticked thread in one call, and says what the mailbox answered", async () => {
    handler = mailHandler(
      json(200, {
        action: "archive",
        changed: 12,
        skipped: 1,
        chains: [
          { rootExtId: "mail:<loom-cutover-1@example.fed>", changed: 4, skipped: 1 },
          { rootExtId: "mail:<lease-renewal-1@example.fed>", changed: 8, skipped: 0 },
        ],
      }),
    );
    await mountApp("/?q=cutover");
    await ticksTwo();

    // The two mailbox verbs are icons: the word is on the button as its name and
    // its tooltip, and the row draws a glyph. The bar also holds the braid, the
    // folder dropdown, the count and the way out, and spelling Archive and Delete
    // along that row is what wrapped it.
    const archive = screen.getByRole("button", { name: "Archive" });
    expect(archive.getAttribute("title")).toBe("Archive");
    expect(archive.textContent).toBe("");
    expect(screen.getByRole("button", { name: "Delete" }).textContent).toBe("");

    click(archive);
    await waitFor(() => expect(calls.some((c) => pathOf(c) === "/v1/mail")).toBe(true));

    const post = calls.find((c) => pathOf(c) === "/v1/mail")!;
    expect(post.method).toBe("POST");
    // One call for the set, in the order it was ticked: four requests would be
    // four chances for the mailbox to be left half changed.
    expect(JSON.parse(post.body!)).toEqual({
      chains: ["mail:<loom-cutover-1@example.fed>", "mail:<lease-renewal-1@example.fed>"],
      action: "archive",
    });
    // The sentence is the mailbox's numbers, not the caller's — and the skipped
    // entry is named rather than swallowed: the reader can see four messages and
    // be told about three.
    expect((await screen.findByRole("status")).textContent).toContain(
      "Archived 12 messages — out of the inbox, still in All Mail. 1 entry has no mailbox copy and was left alone.",
    );
    // The ticks are cleared, and the bar with them: the chains are not in this
    // list any more.
    expect(screen.queryByRole("button", { name: "Archive" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Braid Threads" })).toBeNull();
  });

  it("deletes to the trash, and says how long it can be got back", async () => {
    handler = mailHandler(json(200, { action: "trash", changed: 3, skipped: 0, chains: [] }));
    await mountApp("/?q=cutover");
    await ticksTwo();

    click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(calls.some((c) => pathOf(c) === "/v1/mail")).toBe(true));

    expect(JSON.parse(calls.find((c) => pathOf(c) === "/v1/mail")!.body!)).toEqual({
      chains: ["mail:<loom-cutover-1@example.fed>", "mail:<lease-renewal-1@example.fed>"],
      action: "trash",
    });
    // "deleted" is a claim about the reader's mailbox, so the sentence says where
    // it went and how long it can be undone — the trash is Gmail's, not a folder
    // this page invented.
    // Said in the shell's corner rather than in the header's row (see Toasts):
    // the bar has to be free to leave with the ticks it was drawn for.
    const said = await screen.findByText(/Deleted 3 messages/);
    expect(said.closest(".toasts")).not.toBeNull();
    expect(said.textContent).toBe("Deleted 3 messages — in the trash, recoverable for 30 days.");
  });

  it("takes the account of what happened away once it has been read", async () => {
    // A sentence about work that is over cannot stand in the corner for the rest
    // of the session: it says what one action did, which is over the moment it
    // has been read.
    handler = mailHandler(json(200, { action: "trash", changed: 3, skipped: 0, chains: [] }));
    // Watched from before the action, so the timer the note arms is the one this
    // test drives. Five seconds of a reader's life in one call, on the callback
    // the component itself handed the timer.
    const armed = vi.spyOn(globalThis, "setTimeout");
    await mountApp("/?q=cutover");
    await ticksTwo();

    click(screen.getByRole("button", { name: "Delete" }));
    const note = await screen.findByText(/Deleted 3 messages/);
    expect(note.className).toBe("toasttext");

    const armedTimer = armed.mock.calls.find(([, ms]) => ms === 5000);
    armed.mockRestore();
    if (!armedTimer) throw new Error("the note armed no five-second timer");
    act(() => (armedTimer[0] as () => void)());
    // Gone, and the bar with it: a note about mail that has already moved is not
    // a reason to keep the header's row, and it is not a reason to keep the note
    // either. Both leave, because neither is what the page is for.
    expect(screen.queryByText(/Deleted 3 messages/)).toBeNull();
    expect(screen.queryByRole("button", { name: "Archive" })).toBeNull();
  });

  it("takes its own account away without touching a refusal", async () => {
    // A refusal is something to act on rather than a moment that has passed, so
    // the note's timer is not what ends it: nothing here is on a clock, and the
    // reader still has the reason in front of them after the wait.
    handler = mailHandler(json(502, { error: "gmail: rate limited, try again in a minute" }));
    const armed = vi.spyOn(globalThis, "setTimeout");
    await mountApp("/?q=cutover");
    await ticksTwo();

    click(screen.getByRole("button", { name: "Delete" }));
    expect(await screen.findByText(/rate limited/)).toBeTruthy();
    const watchers = armed.mock.calls.filter(([, ms]) => ms === 5000);
    armed.mockRestore();
    expect(watchers).toHaveLength(0);
    // And it is still there: the failure is not on the note's timer and not on
    // one of its own either.
    expect(screen.getByText(/rate limited/)).toBeTruthy();
  });

  it("moves to the folder the dropdown names, in the one action the choice is", async () => {
    handler = mailHandler(
      json(200, { action: "move", labels: ["Work"], changed: 3, skipped: 0, chains: [] }),
    );
    await mountApp("/?q=cutover");
    await ticksTwo();

    const folders = screen.getByLabelText("Move to a folder") as HTMLSelectElement;
    // The inbox is not offered: a move to the place the mail is leaving is not a
    // move. Everything else the mailbox has is, including folders this page has
    // never seen mail in.
    expect([...folders.options].map((o) => o.textContent)).toEqual(["Move…", "Archive", "Work"]);
    // Choosing the folder IS the move: no button to press afterwards, and no
    // title above the control saying which verb it is — the placeholder does.
    expect(screen.queryByRole("button", { name: "Move" })).toBeNull();
    expect(folders.value).toBe("");

    fireEvent.change(folders, { target: { value: "Work" } });
    await waitFor(() => expect(calls.some((c) => pathOf(c) === "/v1/mail")).toBe(true));

    expect(JSON.parse(calls.find((c) => pathOf(c) === "/v1/mail")!.body!)).toEqual({
      chains: ["mail:<loom-cutover-1@example.fed>", "mail:<lease-renewal-1@example.fed>"],
      action: "move",
      labels: ["Work"],
    });
    expect((await screen.findByText("Moved 3 messages to Work.")).closest(".toasts")).not.toBeNull();
  });

  it("stands in the nav's row while a selection does, and deselects every tick from there", async () => {
    handler = mailHandler(json(200, { action: "archive", changed: 0, skipped: 0, chains: [] }));
    await mountApp("/?q=cutover");
    await ticksTwo();

    // The bar is the header's now, in the row the nav is on — not a card at the
    // foot of the list it acts on. Hiding the nav is the stylesheet's `:has`
    // rule, which jsdom does not apply; what the client owns, and what this
    // asserts, is where the bar is drawn.
    const site = document.querySelector("header.sitehead");
    const bar = site?.querySelector(".ibbuild");
    if (!bar) throw new Error("the bar is not in the site header");
    expect(document.querySelector(".wrap .ibbuild")).toBeNull();

    // How much is ticked, and the way out of it, at the bar's own end.
    expect(within(bar as HTMLElement).getByText("2 selected")).toBeTruthy();
    expect((screen.getAllByRole("checkbox")[0] as HTMLInputElement).checked).toBe(true);

    click(within(bar as HTMLElement).getByRole("button", { name: "Deselect all" }));
    await waitFor(() => expect(document.querySelector(".ibbuild")).toBeNull());
    expect((screen.getAllByRole("checkbox")[0] as HTMLInputElement).checked).toBe(false);
    // Nothing was done to the mail, so there is nothing to report: deselecting
    // dismisses the bar rather than leaving a sentence about an action that was
    // never taken.
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("leaves the ticks alone when the mailbox refuses", async () => {
    handler = mailHandler(
      json(403, {
        error:
          "changing mail is disabled: this server was started without -mail-write, so it will not archive, trash or move anything.",
      }),
    );
    await mountApp("/?q=cutover");
    await ticksTwo();

    click(screen.getByRole("button", { name: "Archive" }));
    // The refusal names the switch, because the person reading it is the one who
    // can restart the service with it. It is drawn where every other account is,
    // in the shell's corner, and it is the marked kind: a refusal is something to
    // act on rather than a moment that has passed.
    const refused = await screen.findByText(/-mail-write/);
    expect(refused.closest(".toast.bad")).not.toBeNull();
    // Nothing moved, so nothing is cleared: the ticks are still the reader's
    // selection and the buttons still stand.
    expect((screen.getByRole("button", { name: "Archive" }) as HTMLButtonElement).disabled).toBe(
      false,
    );
    expect((screen.getAllByRole("checkbox")[0] as HTMLInputElement).checked).toBe(true);
  });
});

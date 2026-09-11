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
    calls.push(call);
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

/** jsdom does not submit a form when its submit button is clicked, so the submit
 *  event is dispatched directly. The button itself is still asserted on. */
function submitSearch() {
  const button = screen.getByRole("button", { name: "Search" }) as HTMLButtonElement;
  expect(button.disabled).toBe(false);
  fireEvent.submit(button.closest("form")!);
}

const searchCalls = () => calls.filter((c) => pathOf(c) === "/v1/search");

async function searchFor(text: string) {
  typeInto("Query", text);
  submitSearch();
  await waitFor(() => expect(searchCalls().length).toBeGreaterThan(0));
}

/** The common building mocks: search answers with the two chains, a build with
 *  SPEC, and the saved page a build lands on with the same SPEC. */
const buildHandler: Handler = (c) => {
  const p = pathOf(c);
  if (p === "/v1/spec" && c.method === "POST") return json(200, SPEC);
  if (p === "/v1/specs/loom-cutover") return json(200, SPEC);
  if (p === "/v1/search") return json(200, { mode: "lexical", chains: CHAINS });
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
    { id: "mail", label: "Gmail (docket)", status: "ok" },
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

const statusHandler: Handler = (c) => {
  const p = pathOf(c);
  if (p === "/v1/status") return json(200, STATUS);
  if (p === "/v1/stats") return json(200, STATS);
  return json(500, { error: `unexpected call to ${c.method} ${p}` });
};

describe("searching for chains", () => {
  it("lists each candidate with the matched-of-total ratio", async () => {
    handler = () => json(200, { mode: "lexical", chains: CHAINS });
    await mountApp("/");
    await searchFor("cutover");

    await screen.findByText("Loom cutover schedule");
    expect(await screen.findByText("3 of 4 matched")).toBeTruthy();
    // The cast of the whole chain, not just the authors of the hits.
    expect(await screen.findByText("4 participants")).toBeTruthy();
    // the same numerator over a different chain size — the ratio is what
    // separates a thread about the query from one that mentioned it
    expect(await screen.findByText("3 of 180 matched")).toBeTruthy();
    expect(await screen.findByText("12 participants")).toBeTruthy();
    expect(screen.getByText("2026-03-02 – 2026-03-11")).toBeTruthy();
  });

  it("passes the mode and drops the filters left blank", async () => {
    handler = () => json(200, { mode: "lexical", chains: CHAINS });
    await mountApp("/");
    typeInto("Mode", "hybrid");
    await searchFor("cutover");

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
    await mountApp("/");
    await searchFor("cutover");
    await screen.findByText("Loom cutover schedule");

    // hold the semantic answer open: whatever is on screen mid-flight is the
    // claim the page is making, and it must not be the lexical result set
    let release: (r: Response) => void = () => {};
    handler = () => new Promise<Response>((res) => (release = res));
    typeInto("Mode", "semantic");
    submitSearch();

    await waitFor(() => expect(screen.queryByText("Loom cutover schedule")).toBeNull());
    expect(screen.getByText("Searching…")).toBeTruthy();

    await act(async () => {
      release(json(200, { mode: "semantic", chains: [CHAINS[1]] }));
    });
    await screen.findByText("Warehouse lease renewal");
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
    await mountApp("/");
    typeInto("Mode", "semantic");
    await searchFor("cutover");

    await screen.findByText(/no embedding daemon reachable/);
    // a 503 here is a fact about the operator's machine, not a hiccup; retrying
    // it only delays saying so
    await new Promise((r) => setTimeout(r, 120));
    expect(searchCalls()).toHaveLength(1);
  });

  it("tells a rejected query apart from a name that matches nothing", async () => {
    handler = () => json(400, { error: "unbalanced quote in query" });
    await mountApp("/");
    await searchFor('"cutover');
    expect((await screen.findByRole("alert")).textContent).toContain("Rejected (400)");

    cleanup();
    calls = [];
    handler = () => json(404, { error: "no entry carries id mail:<nothing@example.fed>" });
    await mountApp("/");
    await searchFor("mail:<nothing@example.fed>");
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Not found (404)");
    expect(alert.textContent).toContain("no entry carries id mail:<nothing@example.fed>");
  });
});

describe("building a page from the chosen set", () => {
  it("posts exactly the ticked chains and lands on the page", async () => {
    handler = buildHandler;
    const router = await mountApp("/");
    await searchFor("cutover");
    await screen.findByText("Loom cutover schedule");

    const boxes = screen.getAllByRole("checkbox");
    click(boxes[0]!);
    click(boxes[1]!);
    click(boxes[1]!); // unticked again: it must not reach the request
    typeInto("Page title", "Loom cutover");

    click(screen.getByRole("button", { name: /Build page from 1 chain$/ }));
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
    const router = await mountApp("/");
    await searchFor("cutover");
    await screen.findByText("Loom cutover schedule");
    click(screen.getAllByRole("checkbox")[0]!);
    typeInto("Page title", "Loom cutover");
    click(screen.getByRole("button", { name: /Build page/ }));

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
    await mountApp("/");
    await searchFor("cutover");
    await screen.findByText("Loom cutover schedule");
    click(screen.getAllByRole("checkbox")[0]!);
    click(screen.getByRole("button", { name: /Build page/ }));

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
    await mountApp("/");
    await searchFor("cutover");
    await screen.findByText("Loom cutover schedule");

    click(screen.getAllByRole("checkbox")[0]!);
    typeInto("Page title", "Loom cutover");
    const button = screen.getByRole("button", { name: /Build page/ });
    click(button);

    const status = await screen.findByRole("status");
    expect(status.textContent).toContain("boilerplate");
    expect((await screen.findByRole("button", { name: "Building…" })).hasAttribute("disabled")).toBe(true);

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
    expect(await screen.findByText("Gmail (docket)")).toBeTruthy();
    // The detail under a not-ok row says what the fix is.
    expect(await screen.findByText("start it with `ollama serve`")).toBeTruthy();

    // The note shows when the state was last measured AND when the next scheduled
    // slurp fires; both carry a clock now, not just a day. Exact text varies by
    // the machine's timezone, so assert the sentence holds both timestamps.
    const note = (await screen.findByText(/Next slurp/)).textContent ?? "";
    expect(note).toContain("Last checked");
    expect(note).toMatch(/\d{1,2}:\d{2}/);

    // Corpus coverage from /v1/stats.
    expect(await screen.findByText("4281")).toBeTruthy();
    expect(await screen.findByText("mail entries")).toBeTruthy();
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
 * reachable without scrolling to the end of a long transcript.
 */
describe("the site navigation", () => {
  it("is the header above the page's own, and nothing renders a footer", async () => {
    handler = () => json(200, { signed_in: true });
    await mountApp("/");

    const site = document.querySelector("header.sitehead");
    if (!site) throw new Error("the shell rendered no site header");
    // Document order is the claim: the site nav is the first thing on the page.
    expect(document.querySelectorAll("header")[0]).toBe(site);
    for (const name of ["Home", "Browse saved specs", "Services", "Ops"]) {
      expect(within(site as HTMLElement).getByRole("link", { name })).toBeTruthy();
    }
    expect(document.querySelector("footer")).toBeNull();
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
    const router = await mountApp("/");

    await searchFor("cutover");
    await screen.findByText("Loom cutover schedule");
    typeInto("Page title", "Loom cutover");
    click(screen.getAllByRole("checkbox")[0]!);
    click(screen.getByRole("button", { name: /Build page from 1 chain$/ }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/view/loom-cutover"));
    await screen.findByText("Loom cutover");
  });

  it("goes back to the search when the page is left", async () => {
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
    expect(screen.getByRole("button", { name: "Search" })).toBeTruthy();
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

describe("adding another email to a page", () => {
  it("searches the corpus from the toolbar and adds the chosen chain by accept", async () => {
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
    await screen.findByText("Warehouse lease renewal");

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
    // The search that found the chain goes with it, so the page records where
    // the chain came from rather than gaining an unexplained one.
    expect(body.queries).toEqual([{ q: "lease", note: "add-email search, mode=hybrid" }]);
    // The modal closed once the add was sent.
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add another email" })).toBeNull());

    // The refreshed page reports the growth like any other refresh, and says
    // the search was recorded with it.
    await screen.findByText(/1 added/);
    await screen.findByText(/1 search recorded/);
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

    // Both inputs are back, and the search ran itself.
    await waitFor(() => expect((screen.getByLabelText("Query") as HTMLInputElement).value).toBe("cutover"));
    expect((screen.getByLabelText("Mode") as HTMLSelectElement).value).toBe("semantic");
    await screen.findByText("Warehouse lease renewal");
  });

  it("writes the search to the URL, and Back from a built page lands on it", async () => {
    handler = buildHandler;
    const router = await mountApp("/");

    await searchFor("cutover");
    // The default mode is omitted from the URL — the canonical home search is
    // plain /.q=cutover, not a URL that spells out the default.
    expect(router.state.location.searchStr).toBe("?q=cutover");

    await screen.findByText("Loom cutover schedule");
    typeInto("Page title", "Loom cutover");
    click(screen.getAllByRole("checkbox")[0]!);
    click(screen.getByRole("button", { name: /Build page from 1 chain$/ }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/view/loom-cutover"));
    await screen.findByText("Loom cutover");

    // Back: the address bar returns to the search that built this page, and
    // the search page comes back with its query and results, not a blank form.
    await act(async () => {
      router.history.back();
    });
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(router.state.location.searchStr).toBe("?q=cutover");
    await waitFor(() => expect((screen.getByLabelText("Query") as HTMLInputElement).value).toBe("cutover"));
    await screen.findByText("Loom cutover schedule");
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

const OPS_PLAN_AFTER = {
  ...OPS_PLAN_BEFORE,
  people: 4,
  merges: OPS_PLAN_BEFORE.merges.filter((m) => m.dropId !== 8),
  trail: [OPS_RECORD],
};

describe("the ops route /ops", () => {
  it("shows the plan with the evidence, and applies one pair behind a confirm", async () => {
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
    await mountApp("/ops");

    // The applicable pair: the evidence is on screen, and so is its button.
    expect(
      await screen.findByText(/name-only person, and the kept person is on every entry they are/),
    ).toBeTruthy();
    expect(screen.getByText("apply")).toBeTruthy();
    expect(screen.getByText("read-only")).toBeTruthy();
    // The first-name-and-org tier is listed but has no apply button: exactly one
    // Merge exists, for the same-name pair.
    expect(screen.getAllByRole("button", { name: "Merge" })).toHaveLength(1);

    // The reads: the refusal, the twins aggregate, the empty trail.
    expect(await screen.findByText(/two people of that name fit the evidence equally/)).toBeTruthy();
    expect(screen.getByText(/twins pass declined 612 entries/)).toBeTruthy();
    expect(screen.getByText("0 merges recorded — the person_merges trail")).toBeTruthy();

    // First click asks for confirmation; nothing has left the browser yet.
    click(screen.getByRole("button", { name: "Merge" }));
    expect(await screen.findByText(/This cannot be undone/)).toBeTruthy();
    expect(calls.some((c) => pathOf(c) === "/v1/ops/merge")).toBe(false);

    // The confirming click names the pair and sends it.
    click(screen.getByRole("button", { name: "Merge #8 into #7" }));
    await waitFor(() =>
      expect(calls.some((c) => pathOf(c) === "/v1/ops/merge" && c.method === "POST")).toBe(true),
    );
    const post = calls.find((c) => pathOf(c) === "/v1/ops/merge");
    expect(JSON.parse(post!.body ?? "{}")).toEqual({ keepId: 7, dropId: 8 });

    // Success: the note names the merged pair, the plan was refetched, and the
    // only merge left on screen is the read-only tier, without a button.
    expect(await screen.findByText("merged #8 into #7 — the plan below is the current one.")).toBeTruthy();
    await waitFor(() => expect(screen.queryAllByRole("button", { name: "Merge" })).toHaveLength(0));
    expect(screen.getByText("1 merge recorded — the person_merges trail")).toBeTruthy();
  });
});

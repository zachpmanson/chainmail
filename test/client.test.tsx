// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { $api, searchQuery } from "../src/lib/api";
import { App } from "../src/main";
import { SelectView } from "../src/components/Select";
import { makeQueryClient } from "../src/lib/queryClient";

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
  // A search writes its parameters into the URL (that is the feature), and
  // jsdom shares one location across the whole file — so each test starts
  // from the bare home page, not from some earlier test's query string.
  history.replaceState(null, "", "/");
});

const mount = (ui: React.ReactElement) =>
  render(<QueryClientProvider client={makeQueryClient()}>{ui}</QueryClientProvider>);

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

const searchCalls = () => calls.filter((c) => c.url.includes("/v1/search"));

async function searchFor(text: string) {
  typeInto("Query", text);
  submitSearch();
  await waitFor(() => expect(searchCalls().length).toBeGreaterThan(0));
}

describe("searching for chains", () => {
  it("lists each candidate with the matched-of-total ratio", async () => {
    handler = () => json(200, { mode: "lexical", chains: CHAINS });
    mount(<SelectView onBuilt={() => {}} />);
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
    mount(<SelectView onBuilt={() => {}} />);
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
    mount(<SelectView onBuilt={() => {}} />);
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
    mount(<SelectView onBuilt={() => {}} />);
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
    mount(<SelectView onBuilt={() => {}} />);
    await searchFor('"cutover');
    expect((await screen.findByRole("alert")).textContent).toContain("Rejected (400)");

    cleanup();
    calls = [];
    handler = () => json(404, { error: "no entry carries id mail:<nothing@example.fed>" });
    mount(<SelectView onBuilt={() => {}} />);
    await searchFor("mail:<nothing@example.fed>");
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Not found (404)");
    expect(alert.textContent).toContain("no entry carries id mail:<nothing@example.fed>");
  });
});

describe("building a page from the chosen set", () => {
  it("posts exactly the ticked chains", async () => {
    handler = (c) => (c.url.includes("/v1/spec") ? json(200, SPEC) : json(200, { mode: "lexical", chains: CHAINS }));
    const built = vi.fn();
    mount(<SelectView onBuilt={built} />);
    await searchFor("cutover");
    await screen.findByText("Loom cutover schedule");

    const boxes = screen.getAllByRole("checkbox");
    click(boxes[0]!);
    click(boxes[1]!);
    click(boxes[1]!); // unticked again: it must not reach the request
    typeInto("Page title", "Loom cutover");

    click(screen.getByRole("button", { name: /Build page from 1 chain$/ }));
    await waitFor(() => expect(calls.some((c) => c.url.includes("/v1/spec"))).toBe(true));

    const post = calls.find((c) => c.url.includes("/v1/spec"))!;
    expect(post.method).toBe("POST");
    // The title was typed, so the page earns the slug as its name.
    expect(JSON.parse(post.body!)).toEqual({
      chains: ["mail:<loom-cutover-1@example.fed>"],
      name: "loom-cutover", // slug of the page title: the URL it earns
      title: "Loom cutover",
      queries: [{ q: "cutover", note: "corpus search, mode=hybrid" }],
    });
    await waitFor(() => expect(built).toHaveBeenCalledTimes(1));
  });

  it("comes back after the delay the service named when two builds are already running", async () => {
    let posts = 0;
    handler = (c) => {
      if (!c.url.includes("/v1/spec")) return json(200, { mode: "lexical", chains: CHAINS });
      posts += 1;
      return posts === 1
        ? json(429, { error: "two spec builds already in flight" }, { "retry-after": "0" })
        : json(200, SPEC);
    };
    const built = vi.fn();
    mount(<SelectView onBuilt={built} />);
    await searchFor("cutover");
    await screen.findByText("Loom cutover schedule");
    click(screen.getAllByRole("checkbox")[0]!);
    click(screen.getByRole("button", { name: /Build page/ }));

    // "not yet", with a time attached, is the one decline worth re-asking
    await waitFor(() => expect(built).toHaveBeenCalledTimes(1));
    expect(posts).toBe(2);
  });

  it("does not re-ask when the embedding deadline was missed", async () => {
    let posts = 0;
    handler = (c) => {
      if (!c.url.includes("/v1/spec")) return json(200, { mode: "lexical", chains: CHAINS });
      posts += 1;
      return json(504, { error: "embedding deadline exceeded" });
    };
    mount(<SelectView onBuilt={() => {}} />);
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
    handler = (c) =>
      c.url.includes("/v1/spec")
        ? new Promise<Response>((res) => (release = res))
        : json(200, { mode: "lexical", chains: CHAINS });
    mount(<SelectView onBuilt={() => {}} />);
    await searchFor("cutover");
    await screen.findByText("Loom cutover schedule");

    click(screen.getAllByRole("checkbox")[0]!);
    const button = screen.getByRole("button", { name: /Build page/ });
    click(button);

    const status = await screen.findByRole("status");
    expect(status.textContent).toContain("boilerplate");
    expect((await screen.findByRole("button", { name: "Building…" })).hasAttribute("disabled")).toBe(true);

    await act(async () => {
      release(json(200, SPEC));
    });
  });
});

describe("a spec named on the URL", () => {
  it("still loads from the file path, without touching the API", async () => {
    history.replaceState(null, "", "/?spec=/synthetic.json");
    handler = (c) =>
      c.url.endsWith("/synthetic.json")
        ? json(200, SPEC)
        : json(404, { error: "not a spec route" });
    mount(<App />);

    await screen.findByText("Loom cutover");
    expect(calls.map((c) => new URL(c.url).pathname)).toEqual(["/synthetic.json"]);
    history.replaceState(null, "", "/");
  });
});

describe("the render route /view/<name>", () => {
  it("loads the saved page from the API when the URL names one", async () => {
    history.replaceState(null, "", "/view/loom-cutover");
    handler = (c) =>
      c.url.includes("/v1/specs/loom-cutover")
        ? json(200, SPEC)
        : json(500, { error: "unexpected call" });
    mount(<App />);

    await waitFor(() => expect(calls.some((c) => c.url.includes("/v1/specs/loom-cutover"))).toBe(true));
    await screen.findByText("Loom cutover");
    // No back button: a page under /view/<name> just is, it was not the result
    // of a search.
    expect(screen.queryByRole("button", { name: /Back/ })).toBeNull();
    history.replaceState(null, "", "/");
  });

  it("moves the address bar to /view/<name> when a page is built", async () => {
    history.replaceState(null, "", "/");
    handler = (c) =>
      c.url.includes("/v1/spec") ? json(200, SPEC) : json(200, { mode: "lexical", chains: CHAINS });
    mount(<App />);

    await searchFor("cutover");
    await screen.findByText("Loom cutover schedule");
    typeInto("Page title", "Loom cutover");
    click(screen.getAllByRole("checkbox")[0]!);
    click(screen.getByRole("button", { name: /Build page from 1 chain$/ }));

    await waitFor(() => expect(location.pathname).toBe("/view/loom-cutover"));
    await screen.findByText("Loom cutover");
  });

  it("goes back to the search when the page is left", async () => {
    history.replaceState(null, "", "/view/loom-cutover");
    handler = (c) =>
      c.url.includes("/v1/specs/loom-cutover")
        ? json(200, SPEC)
        : json(500, { error: "unexpected call" });
    mount(<App />);
    await screen.findByText("Loom cutover");

    history.pushState(null, "", "/");
    window.dispatchEvent(new PopStateEvent("popstate"));
    await waitFor(() => expect(screen.queryByText("Loom cutover")).toBeNull());
    expect(screen.getByRole("button", { name: "Search" })).toBeTruthy();
    history.replaceState(null, "", "/");
  });
});

describe("the search lives in the URL", () => {
  it("restores the search from the query string on load", async () => {
    history.replaceState(null, "", "/?q=cutover&mode=semantic");
    // semantic answer for a semantic request, so the restore is not the mock
    // cheerfully answering anything: the mode really flowed through.
    handler = (c) =>
      json(200, {
        mode: new URL(c.url).searchParams.get("mode"),
        chains: new URL(c.url).searchParams.get("mode") === "semantic" ? [CHAINS[1]] : [CHAINS[0]],
      });
    mount(<App />);

    // Both inputs are back, and the search ran itself.
    await waitFor(() => expect((screen.getByLabelText("Query") as HTMLInputElement).value).toBe("cutover"));
    expect((screen.getByLabelText("Mode") as HTMLSelectElement).value).toBe("semantic");
    await screen.findByText("Warehouse lease renewal");
  });

  it("writes the search to the URL, and Back from a built page lands on it", async () => {
    history.replaceState(null, "", "/");
    handler = (c) =>
      c.url.includes("/v1/spec") ? json(200, SPEC) : json(200, { mode: "lexical", chains: CHAINS });
    mount(<App />);

    await searchFor("cutover");
    // The default mode is omitted from the URL — the canonical home search is
    // plain /.q=cutover, not a URL that spells out the default.
    expect(location.search).toBe("?q=cutover");

    await screen.findByText("Loom cutover schedule");
    typeInto("Page title", "Loom cutover");
    click(screen.getAllByRole("checkbox")[0]!);
    click(screen.getByRole("button", { name: /Build page from 1 chain$/ }));
    await waitFor(() => expect(location.pathname).toBe("/view/loom-cutover"));
    await screen.findByText("Loom cutover");

    // Back: the address bar returns to the search that built this page, and
    // the search page comes back with its query and results, not a blank form.
    history.back();
    await waitFor(() => expect(location.pathname).toBe("/"));
    await waitFor(() => expect((screen.getByLabelText("Query") as HTMLInputElement).value).toBe("cutover"));
    await screen.findByText("Loom cutover schedule");
  });
});

// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { App } from "../src/main";
import { SelectView } from "../src/components/Select";
import { makeQueryClient } from "../src/lib/queries";

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

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
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
    // the same numerator over a different chain size — the ratio is what
    // separates a thread about the query from one that mentioned it
    expect(await screen.findByText("3 of 180 matched")).toBeTruthy();
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

    click(screen.getByRole("button", { name: /Build page from 1 chain$/ }));
    await waitFor(() => expect(calls.some((c) => c.url.includes("/v1/spec"))).toBe(true));

    const post = calls.find((c) => c.url.includes("/v1/spec"))!;
    expect(post.method).toBe("POST");
    expect(JSON.parse(post.body!)).toEqual({
      chains: ["mail:<loom-cutover-1@example.fed>"],
      queries: [{ q: "cutover", note: "corpus search, mode=lexical" }],
    });
    await waitFor(() => expect(built).toHaveBeenCalledTimes(1));
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

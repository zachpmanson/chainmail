// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { makeQueryClient } from "../src/lib/queryClient";
import { createChainmailRouter } from "../src/router";

/**
 * Unread state: what the list says, and the one control that changes it.
 *
 * The state itself lives in the mailbox — the server writes it there and the
 * reader's phone shows the same thing — so what is asserted here is the client's
 * half: the count on a row, the button that asks for the other state, and the
 * list being re-read afterwards rather than patched with a number the client
 * invented. Everything below is invented; this corpus holds real correspondence.
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
const reads = () => calls.filter((c) => c.method === "POST" && pathOf(c) === "/v1/read");

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

/** One chain as /v1/search answers it. `unread` is a property of the chain, so
 *  it is set here rather than derived: the server counts the whole trail, and a
 *  client that re-derived it from the three entries a row carries would be
 *  counting a third of the thread. */
const chain = (over: Record<string, unknown>) => ({
  rootExtId: ROOT,
  subject: "Loom cutover schedule",
  first: "2026-03-02T09:15:00Z",
  last: "2026-03-11T17:40:00Z",
  sources: ["mail"],
  entries: 3,
  matched: 3,
  people: 3,
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
    },
  ],
};

/** The whole surface the inbox reads, with /v1/search answerable per test. */
const server = (search: () => Response, read?: Handler): Handler => (c) => {
  const p = pathOf(c);
  if (p === "/v1/search") return search();
  if (p === "/v1/read") return read ? read(c) : json(200, { chain: ROOT, unread: false, marked: 3, skipped: 0 });
  if (p.startsWith("/v1/chains/")) return json(200, CHAIN_BODY);
  if (p === "/v1/settings") return json(200, {});
  if (p === "/auth/status") return json(200, { signed_in: true });
  return json(500, { error: `unexpected call to ${c.method} ${p}` });
};

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

describe("what a chain's read state looks like", () => {
  it("marks a chain with unread mail, and counts it", async () => {
    handler = server(page([chain({ unread: 2 }), chain({ rootExtId: OTHER, subject: "Fence panels", unread: 0 })]));
    await mountApp();

    const rows = await screen.findAllByRole("checkbox");
    expect(rows).toHaveLength(2);
    const badges = document.querySelectorAll(".ibrow.unread .ibunread");
    expect(badges).toHaveLength(1);
    expect(badges[0]!.textContent).toBe("2");
    // The count and the emphasis are one claim, so the row that has no unread
    // mail is not emphasised either.
    expect(document.querySelectorAll(".ibrow.unread")).toHaveLength(1);
    // The pane's circle is the same claim in the toolbar: filled (and pressed)
    // while the newest chain — the one the pane opens by itself — is unread.
    const circle = pane().querySelector(".ibread-read") as HTMLElement;
    expect(circle.className).toContain("unread");
    expect(circle.getAttribute("aria-pressed")).toBe("true");
  });

  it("offers the other state, and asks the server for it", async () => {
    handler = server(page([chain({ unread: 2 })]));
    await mountApp();

    const button = await screen.findByRole("button", { name: "Mark read" });
    fireEvent.click(button);
    await waitFor(() => expect(reads()).toHaveLength(1));
    expect(JSON.parse(reads()[0]!.body ?? "{}")).toEqual({ chain: ROOT, unread: false });
  });

  it("names the state it is in, and the action it would take", async () => {
    // Unread in the pane: the press it offers is "mark read". A read chain
    // offers the reverse — the control is the state, so it never labels itself
    // with the state it already shows.
    let answered = 0;
    handler = server(() => {
      answered += 1;
      return json(200, { mode: "lexical", chains: [chain({ unread: answered > 1 ? 0 : 2 })] });
    });
    await mountApp();

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
    // The second page of the list is the same chain, now read: what the button
    // may show afterwards is what the server said, not what the client assumed.
    let answered = 0;
    handler = server(() => {
      answered += 1;
      return json(200, { mode: "lexical", chains: [chain({ unread: answered > 1 ? 0 : 2 })] });
    });
    await mountApp();

    fireEvent.click(await screen.findByRole("button", { name: "Mark read" }));
    // The badge goes when the mail is read, and the button flips to the state a
    // reader would want next.
    expect(await screen.findByRole("button", { name: "Mark unread" })).toBeTruthy();
    await waitFor(() => expect(document.querySelectorAll(".ibunread")).toHaveLength(0));
    expect(pane().querySelectorAll(".ibread-read.unread")).toHaveLength(0);
  });

  it("says so when the host cannot change the mailbox", async () => {
    handler = server(page([chain({ unread: 1 })]), () =>
      json(403, { error: "marking mail read is disabled: this server was started without -mark-read" }),
    );
    await mountApp();

    fireEvent.click(await screen.findByRole("button", { name: "Mark read" }));
    expect(await screen.findByText(/without -mark-read/)).toBeTruthy();
    // And nothing is claimed about the state: the count is still the server's.
    expect(document.querySelectorAll(".ibunread")).toHaveLength(1);
  });

  it("shows no control for a chain the list has not described", async () => {
    // A chain named only by the address bar has no count, and a button that had
    // to guess which way it went would be wrong half the time.
    handler = server(page([chain({ rootExtId: OTHER, subject: "Fence panels", unread: 0 })]));
    await mountApp(`/?open=${encodeURIComponent(ROOT)}`);

    await screen.findByText(/Roof access is fine/);
    const head = pane().querySelector(".ibread-head") as HTMLElement;
    expect(head.querySelector(".ibread-read")).toBeNull();
  });
});

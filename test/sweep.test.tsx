// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { makeQueryClient } from "../src/lib/queryClient";
import { createChainmailRouter } from "../src/router";

/**
 * The nav's indicator for an ingest nobody pressed. What it has to get right is
 * the two states the page is otherwise silent about: a walk in flight (a corpus
 * mid-rebuild reads as a threading bug — mailbox parents are linked per batch),
 * and a walk that ended short of its work, which is a corpus missing mail with
 * nothing on the page to say so. And that a clean corpus says nothing at all.
 */

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });

type Sweep = {
  running: boolean;
  startedAt?: string;
  finishedAt?: string;
  outcome?: "complete" | "incomplete" | "failed";
};

/** The app on the home page, with the sweep's own answer under test and every
 *  other call answered minimally so the nav can render. */
async function mountApp(sweep: Sweep) {
  vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
    const url = new URL(input instanceof Request ? input.url : String(input), location.href);
    switch (url.pathname) {
      case "/v1/status":
        return json(200, { sweep, services: [] });
      case "/v1/version":
        return json(200, { rev: "abcdef1234567890abcdef1234567890abcdef12" });
      case "/auth/status":
        return json(200, { signed_in: true });
      case "/v1/settings":
        return json(200, {});
      case "/v1/search":
        return json(200, { mode: "lexical", chains: [] });
      default:
        return json(500, { error: `unexpected call to ${url.pathname}` });
    }
  });
  const router = createChainmailRouter(["/"]);
  await router.load();
  render(
    <QueryClientProvider client={makeQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  history.replaceState(null, "", "/");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("the sweep indicator", () => {
  it("says an ingest is running, with when it started", async () => {
    await mountApp({ running: true, startedAt: "2026-09-20T10:02:00Z" });
    const pill = await screen.findByText("ingesting");
    // The turn is the whole of what says the work is happening, so the label
    // carries it for anyone reading the nav without seeing the glyph.
    expect(pill.getAttribute("aria-label")).toContain("Ingesting mail, started");
  });

  it("holds the word when the last ingest stopped short of its work", async () => {
    await mountApp({
      running: false,
      finishedAt: "2026-09-20T10:18:00Z",
      outcome: "incomplete",
    });
    const pill = await screen.findByText("ingest incomplete");
    expect(pill.getAttribute("title")).toContain("stopped early");
  });

  it("says a broken ingest failed rather than that it is unfinished", async () => {
    await mountApp({ running: false, outcome: "failed" });
    expect(await screen.findByText("ingest failed")).toBeTruthy();
    expect(screen.queryByText("ingest incomplete")).toBeNull();
  });

  it("says nothing about an ingest that finished its work", async () => {
    await mountApp({ running: false, outcome: "complete" });
    // The nav has rendered — the search box is in it — and the indicator is not.
    expect(await screen.findByLabelText("Refresh")).toBeTruthy();
    expect(screen.queryByText("ingesting")).toBeNull();
    expect(screen.queryByText("ingest incomplete")).toBeNull();
  });

  it("says nothing when the route cannot be read", async () => {
    vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
      const url = new URL(input instanceof Request ? input.url : String(input), location.href);
      if (url.pathname === "/v1/status") return json(500, { error: "no status" });
      return json(200, {});
    });
    const router = createChainmailRouter(["/"]);
    await router.load();
    render(
      <QueryClientProvider client={makeQueryClient()}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );
    expect(await screen.findByLabelText("Refresh")).toBeTruthy();
    expect(screen.queryByText("ingesting")).toBeNull();
  });
});
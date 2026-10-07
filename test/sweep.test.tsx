// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { makeQueryClient } from "../src/lib/queryClient";
import { createChainmailRouter } from "../src/router";

/** The refresh button also reports scheduled ingest state. */

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

/** The app on the home page, with ingest status under test. */
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

describe("refresh button ingest status", () => {
  it("reports a running ingest and when it started", async () => {
    await mountApp({ running: true, startedAt: "2026-09-20T10:02:00Z" });
    const button = await screen.findByRole("button", { name: /refresh; ingesting mail, started/i });
    expect(button.getAttribute("aria-busy")).toBe("true");
    expect(button.getAttribute("title")).toContain("Threads fill in as the walk runs");
    expect(button.textContent).toContain("ingesting");
  });

  it("warns when the last ingest stopped short of its work", async () => {
    await mountApp({
      running: false,
      finishedAt: "2026-09-20T10:18:00Z",
      outcome: "incomplete",
    });
    const button = await screen.findByRole("button", {
      name: "Refresh; last ingest stopped early",
    });
    expect(button.getAttribute("class")).toContain("warn");
    expect(button.getAttribute("title")).toContain("stopped early");
    expect(button.textContent).toContain("ingest incomplete");
  });

  it("warns when an ingest failed", async () => {
    await mountApp({ running: false, outcome: "failed" });
    const button = await screen.findByRole("button", { name: "Refresh; last ingest failed" });
    expect(button.getAttribute("class")).toContain("warn");
    expect(button.textContent).toContain("ingest failed");
  });

  it("does not warn after an ingest finished its work", async () => {
    await mountApp({ running: false, outcome: "complete" });
    expect(await screen.findByRole("button", { name: "Refresh" })).toBeTruthy();
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

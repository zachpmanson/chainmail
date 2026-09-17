// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { makeQueryClient } from "../src/lib/queryClient";
import { createChainmailRouter } from "../src/router";

/**
 * The deploy stamp in the nav: the revision this build is running and the day it
 * started. The point of it is answering "is the fix I merged live", so the tests
 * are about the two facts and the link, and about the case where the server
 * cannot name a revision at all.
 */

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const REV = "abcdef1234567890abcdef1234567890abcdef12";

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });

type Handler = (path: string) => Response;
let handler: Handler;

beforeEach(() => {
  handler = () => json(500, { error: "no handler installed" });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
    const url = new URL(input instanceof Request ? input.url : String(input), location.href);
    return handler(url.pathname);
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  history.replaceState(null, "", "/");
});

/** The app on the home page, with everything but the stamp's own call answered
 *  minimally so the nav can render. */
async function mountApp() {
  handler = (path) => {
    if (path === "/v1/version") return json(200, { rev: REV, startedAt: "2026-09-16T13:21:23Z" });
    if (path === "/auth/status") return json(200, { signed_in: true });
    if (path === "/v1/settings") return json(200, {});
    if (path === "/v1/search") return json(200, { mode: "lexical", chains: [] });
    return json(500, { error: `unexpected call to ${path}` });
  };
  const router = createChainmailRouter(["/"]);
  await router.load();
  render(
    <QueryClientProvider client={makeQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

describe("the deploy stamp", () => {
  it("names the revision and the day it started, and links to the commit", async () => {
    await mountApp();
    const stamp = await screen.findByRole("link", { name: /abcdef1/ });
    // The day comes first and the hash after an `@`, as in penultimate-guitar's
    // footer: the date is the half a reader compares against "today", and the
    // hash is the half they go and look up. The date is loose — the fixture's
    // instant is UTC and the reader's own zone decides which day that falls on,
    // which is the point of formatting it here rather than in Go.
    expect(stamp.textContent).toMatch(/2026-09-1[56] @ abcdef1/);
    expect(stamp.getAttribute("href")).toBe(
      `https://github.com/zachpmanson/chainmail/commit/${REV}`,
    );
    // The tooltip is where the clock lives: the day is on screen, the minute is
    // there when someone is checking whether a restart happened.
    expect(stamp.getAttribute("title")).toMatch(/^deployed .*\d{2}:\d{2}$/);
  });

  it("shows nothing when the server cannot name a revision", async () => {
    handler = (path) => {
      if (path === "/v1/version") return json(200, { startedAt: "2026-09-16T13:21:23Z" });
      if (path === "/auth/status") return json(200, { signed_in: true });
      if (path === "/v1/settings") return json(200, {});
      if (path === "/v1/search") return json(200, { mode: "lexical", chains: [] });
      return json(500, { error: `unexpected call to ${path}` });
    };
    const router = createChainmailRouter(["/"]);
    await router.load();
    render(
      <QueryClientProvider client={makeQueryClient()}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );
    // The nav's own links are there; nothing claims to be a revision.
    await screen.findByRole("link", { name: "Braids" });
    expect(document.querySelector(".deploy")).toBeNull();
  });
});

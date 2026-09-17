// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { makeQueryClient } from "../src/lib/queryClient";
import { createChainmailRouter } from "../src/router";

/**
 * The ops page's tabs: one subject each, because the page manages three
 * different kinds of row and they are not read together.
 *
 * Two claims. The first is that a tab is a place — the address names it, so a
 * reload or a pasted link lands on the same one, and a value this build does not
 * know opens the page rather than failing. The second is what the tab buys: the
 * merge plan is re-derived by walking the whole corpus, so it is asked for when
 * the tab that shows it is open, not on every visit to the page.
 */

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

interface Call {
  url: string;
  method: string;
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

const handler_: Handler = (c) => {
  const p = pathOf(c);
  if (p === "/v1/people") {
    return json(200, {
      people: [
        { personId: 1, displayName: "Ada Okoye", identities: ["email:ada@loomworks.example"], sent: 2, received: 1 },
      ],
    });
  }
  if (p === "/v1/ops/orgs") return json(200, { domains: [{ domain: "loomworks.example", messages: 2, people: 1, org: "Loomworks", stored: false, guess: "Loomworks" }] });
  if (p === "/v1/ops/plan") {
    return json(200, {
      people: 1,
      merges: [
        {
          keepId: 1,
          keepName: "Ada Okoye",
          dropId: 2,
          dropName: "Ada",
          rule: "dedupe:same-display-name",
          applicable: true,
        },
      ],
      refusals: [],
      candidates: [],
      twinsDeclined: [],
      trail: [],
    });
  }
  if (p === "/v1/settings") return json(200, {});
  if (p === "/auth/status") return json(200, { signed_in: true });
  return json(500, { error: `unexpected call to ${c.method} ${p}` });
};

beforeEach(() => {
  calls = [];
  handler = handler_;
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const req =
      input instanceof Request ? input : new Request(new URL(String(input), location.href), init);
    const call: Call = { url: req.url, method: req.method };
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
const asked = (path: string) => calls.some((c) => pathOf(c) === path);

describe("the ops page's tabs", () => {
  it("opens on people and switches to the other two", async () => {
    await mountApp("/ops");

    const tabs = screen.getAllByRole("tab");
    expect(tabs.map((t) => t.textContent)).toEqual(["People", "Organisations", "Merges"]);
    expect(screen.getByRole("tab", { name: "People" }).getAttribute("aria-selected")).toBe("true");
    expect(await screen.findByText("Ada Okoye")).toBeTruthy();

    click(screen.getByRole("tab", { name: "Organisations" }));
    // One tab at a time: the people list is unmounted once the other tab is
    // drawn, so nothing can be asserted about it before that render lands.
    await waitFor(() => expect(screen.queryByText("Ada Okoye")).toBeNull());
    expect(await screen.findByText("loomworks.example")).toBeTruthy();

    click(screen.getByRole("tab", { name: "Merges" }));
    expect(await screen.findByText("Merge plan")).toBeTruthy();
    expect(screen.queryByText("loomworks.example")).toBeNull();
    expect(screen.getByRole("tab", { name: "Merges" }).getAttribute("aria-selected")).toBe("true");
  });

  it("puts the tab in the address, and reads it back", async () => {
    // A link that names a tab opens on it: this is what makes the page
    // shareable, and what a reload has to land on.
    const router = await mountApp("/ops?tab=merges");
    expect(screen.getByRole("tab", { name: "Merges" }).getAttribute("aria-selected")).toBe("true");
    expect(await screen.findByText("Merge plan")).toBeTruthy();

    click(screen.getByRole("tab", { name: "Organisations" }));
    // The address is what a reload reads, so the tab has to reach the router's
    // location and not only the component that drew it.
    await waitFor(() => expect(router.state.location.search.tab).toBe("orgs"));
    expect(await screen.findByText("loomworks.example")).toBeTruthy();
  });

  it("opens the first tab for a value it does not know", async () => {
    // A stale or hand-typed link shows the page rather than failing: a tab name
    // this build has never heard of is not a broken request.
    await mountApp("/ops?tab=archive");
    expect(screen.getByRole("tab", { name: "People" }).getAttribute("aria-selected")).toBe("true");
    expect(await screen.findByText("Ada Okoye")).toBeTruthy();
  });

  it("asks for the merge plan only on the tab that shows it", async () => {
    await mountApp("/ops");
    await screen.findByText("Ada Okoye");
    expect(asked("/v1/people")).toBe(true);
    // The plan walks the corpus to re-derive every proposed merge, so it is not
    // fetched by a visit to a tab that does not draw it.
    expect(asked("/v1/ops/plan")).toBe(false);

    click(screen.getByRole("tab", { name: "Merges" }));
    await waitFor(() => expect(asked("/v1/ops/plan")).toBe(true));
    // And it is asked for once: switching tabs is not a reason to re-derive it.
    expect(calls.filter((c) => pathOf(c) === "/v1/ops/plan")).toHaveLength(1);
  });
});

// @vitest-environment jsdom
import { createElement as h } from "react";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { describe, expect, it, vi } from "vitest";
import { MailboxScope } from "../src/lib/inbox/useAccountId";
import { useBuildPage } from "../src/lib/inbox/build";
import { domHarness, pick } from "./domHarness";

const { mutate } = vi.hoisted(() => ({ mutate: vi.fn() }));
vi.mock("../src/lib/api/api", () => ({ $api: { useMutation: () => ({ mutate }) } }));
const render = domHarness();

function BuildProbe() {
  const { start } = useBuildPage();
  return h(
    "button",
    { onClick: () => start({ chains: ["mail:<synthetic-target@example.test>"] }) },
    "Build",
  );
}

describe("received-reaction page build account boundary", () => {
  it("passes effective selected-account scope to saved spec builds", async () => {
    vi.stubGlobal("scrollTo", vi.fn());
    const root = createRootRoute();
    const route = createRoute({
      getParentRoute: () => root,
      path: "/",
      component: () =>
        h(MailboxScope.Provider, { value: { accountId: "selected-account" } }, h(BuildProbe)),
    });
    const router = createRouter({
      routeTree: root.addChildren([route]),
      history: createMemoryHistory({ initialEntries: ["/"] }),
    });
    await router.load();
    const host = await render(h(RouterProvider, { router }));
    await pick(host.querySelector("button")!);
    expect(mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          accountId: "selected-account",
          chains: ["mail:<synthetic-target@example.test>"],
        }),
      }),
    );
  });
});

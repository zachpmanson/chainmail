import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { describe, expect, it } from "vitest";
import { MailboxScope, useAccountId } from "../src/lib/inbox/useAccountId";

function AccountProbe() {
  return createElement("span", { "data-account": useAccountId() ?? "all" });
}

describe("effective account boundary", () => {
  it.each([
    { scope: null, expected: "personal" },
    { scope: { accountId: "work" }, expected: "work" },
    { scope: { accountId: undefined }, expected: "all" },
  ])("uses $expected for an off-home row/preview", async ({ scope, expected }) => {
    const root = createRootRoute({
      component: () =>
        scope === null
          ? createElement(Outlet)
          : createElement(MailboxScope.Provider, { value: scope }, createElement(Outlet)),
    });
    const settings = createRoute({
      getParentRoute: () => root,
      path: "/settings",
      validateSearch: () => ({ accountId: "personal" }),
      component: AccountProbe,
    });
    const router = createRouter({
      routeTree: root.addChildren([settings]),
      history: createMemoryHistory({ initialEntries: ["/settings?accountId=personal"] }),
    });
    await router.load();
    expect(renderToStaticMarkup(createElement(RouterProvider, { router }))).toContain(
      `data-account="${expected}"`,
    );
  });
});

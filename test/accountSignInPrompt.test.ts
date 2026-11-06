// @vitest-environment jsdom
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import AccountSignInPrompt from "../src/components/navigation/AccountSignInPrompt";
import { $api } from "../src/lib/api/api";
import type { components } from "../src/lib/api/api.d";

type AuthStatus = components["schemas"]["AuthStatusResponse"];
const disconnected = {
  id: "offline",
  displayName: "Disconnected account",
  signedIn: false,
};
const connected = { ...disconnected, id: "connected", signedIn: true };
const options = $api.queryOptions("get", "/auth/status", {});
let root: Root;
let host: HTMLDivElement;
let client: QueryClient;

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal(
    "fetch",
    vi.fn(() => new Promise<Response>(() => {})),
  );
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  client.clear();
  vi.unstubAllGlobals();
});
async function render(status?: AuthStatus) {
  if (status) client.setQueryData(options.queryKey, status);
  await act(async () =>
    root.render(createElement(QueryClientProvider, { client }, createElement(AccountSignInPrompt))),
  );
}
function signIn() {
  return host.querySelector<HTMLAnchorElement>('a[href="/auth/login"]');
}

it.each([{ accounts: [] }, { accounts: [disconnected] }])(
  "offers the existing Google login flow with no signed-in accounts: %j",
  async ({ accounts }) => {
    await render({ signed_in: false, accounts });
    expect(signIn()?.textContent).toBe("Sign in with Google");
    expect(signIn()?.classList.contains("bg-accent")).toBe(true);
    expect(host.querySelector('section[aria-label="Connect a Gmail account"]')).not.toBeNull();
    expect(host.textContent).toContain("start syncing your mail");
    expect(fetch).not.toHaveBeenCalled();
  },
);

it.each([{ accounts: [connected] }, { accounts: [disconnected, connected] }])(
  "hides the prompt when any account is signed in: %j",
  async ({ accounts }) => {
    await render({ signed_in: true, accounts });
    expect(host.innerHTML).toBe("");
  },
);

it("hides while status is loading rather than assuming no accounts", async () => {
  await render();
  expect(host.innerHTML).toBe("");
});

it("hides an account-status error even with cached disconnected data", async () => {
  await render({ signed_in: false, accounts: [] });
  expect(signIn()).not.toBeNull();
  await act(async () => {
    client
      .getQueryCache()
      .find(options)!
      .setState({ status: "error", error: new Error("Offline") });
    await new Promise((resolve) => setTimeout(resolve, 10));
  });
  expect(host.innerHTML).toBe("");
});

it("reacts to account connection and disconnection without a page reload", async () => {
  await render({ signed_in: false, accounts: [] });
  expect(signIn()).not.toBeNull();
  await act(async () => {
    client.setQueryData(options.queryKey, { signed_in: true, accounts: [connected] });
    await new Promise((resolve) => setTimeout(resolve, 10));
  });
  expect(signIn()).toBeNull();
  await act(async () => {
    client.setQueryData(options.queryKey, { signed_in: false, accounts: [disconnected] });
    await new Promise((resolve) => setTimeout(resolve, 10));
  });
  expect(signIn()).not.toBeNull();
});

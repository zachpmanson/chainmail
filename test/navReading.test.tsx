// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { NavReading } from "../src/components/NavReading";
import { makeQueryClient } from "../src/lib/queryClient";
import { createChainmailRouter } from "../src/router";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

/** The nav's status with a client of its own: this component's whole input is
 *  what the query client is doing, so the test drives that directly. */
function mount() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queryClient}>
      <NavReading />
    </QueryClientProvider>,
  );
  return queryClient;
}

it("says the corpus is being read while a question is out, and stops when it lands", async () => {
  const queryClient = mount();
  expect(screen.queryByRole("status")).toBeNull();

  let land: (answer: string) => void = () => {};
  let asked: Promise<string> | undefined;
  await act(async () => {
    asked = queryClient.fetchQuery({
      queryKey: ["corpus"],
      queryFn: () => new Promise<string>((resolve) => (land = resolve)),
    });
  });
  await waitFor(() =>
    expect(screen.getByRole("status").textContent).toBe("Reading the corpus…"),
  );

  await act(async () => {
    land("answered");
    await asked;
  });
  await waitFor(() => expect(screen.queryByRole("status")).toBeNull());
});

it("is the shell's, not a page's: the read is announced in the nav, and nowhere else", async () => {
  const json = (body: unknown) =>
    Promise.resolve(
      new Response(JSON.stringify(body), {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    );
  vi.stubGlobal("fetch", (input: RequestInfo | URL) => {
    const url = input instanceof Request ? input.url : String(input);
    const path = new URL(url, "http://localhost/").pathname;
    // Everything the shell asks for while the inbox loads, except the corpus
    // itself: /v1/search never answers, so the read stays in flight.
    if (path === "/v1/version") return json({});
    if (path === "/auth/status") return json({ signed_in: true });
    if (path === "/v1/settings" || path === "/v1/labels") return json({});
    return new Promise<Response>(() => {});
  });

  history.replaceState(null, "", "/");
  const router = createChainmailRouter(["/"]);
  await router.load();
  render(
    <QueryClientProvider client={makeQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );

  const told = await screen.findByRole("status");
  expect(told.textContent).toBe("Reading the corpus…");
  // Above the page, in the header — the page body no longer says it, which is
  // the move: the reading is why the list is missing, not part of it.
  expect(told.closest("header.sitehead")).toBeTruthy();
  expect(told.closest(".ibwrap")).toBeNull();
  expect(document.querySelector(".ibwrap")!.textContent).not.toContain("Reading the corpus");
});

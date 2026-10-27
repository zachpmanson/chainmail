// @vitest-environment jsdom
import { act, createElement, useCallback, useRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import useLoadMore from "../src/lib/ui/useLoadMore";
import useThreadList from "../src/lib/inbox/useThreadList";

type Observation = {
  callback: IntersectionObserverCallback;
  options: IntersectionObserverInit;
  observe: ReturnType<typeof vi.fn>;
  disconnect: ReturnType<typeof vi.fn>;
};
let observations: Observation[];
let container: HTMLDivElement;
let root: Root;
let client: QueryClient;

function intersect(observation = observations.at(-1)!, visible = true) {
  act(() => {
    observation.callback(
      [{ isIntersecting: visible } as IntersectionObserverEntry],
      {} as IntersectionObserver,
    );
  });
}

function Marker({
  enabled,
  more,
  pages,
  updatedAt = 0,
}: {
  enabled: boolean;
  more: () => unknown;
  pages?: unknown;
  updatedAt?: number;
}) {
  const end = useRef<HTMLDivElement>(null);
  useLoadMore(end, enabled, more, pages, updatedAt);
  return createElement("div", { ref: end });
}

beforeEach(() => {
  observations = [];
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(callback: IntersectionObserverCallback, options: IntersectionObserverInit) {
        observations.push({
          callback,
          options,
          observe: this.observe,
          disconnect: this.disconnect,
        });
      }
      observe = vi.fn();
      disconnect = vi.fn();
    },
  );
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
});
afterEach(() => {
  act(() => root.unmount());
  client.clear();
  container.remove();
  vi.unstubAllGlobals();
});

describe("prefetch observation", () => {
  it("buffers the desktop scroll panel, not its clipping viewport", () => {
    container.style.overflowY = "scroll";
    const more = vi.fn();
    act(() => root.render(createElement(Marker, { enabled: true, more })));
    expect(observations[0]!.options).toEqual({
      root: container,
      rootMargin: "0px 0px 600px 0px",
    });
    intersect(undefined, false);
    expect(more).not.toHaveBeenCalled();
    intersect();
    intersect();
    expect(more).toHaveBeenCalledTimes(1);
    expect(observations[0]!.disconnect).toHaveBeenCalled();
  });

  it("uses the mobile viewport and changes roots across responsive resize", () => {
    const more = vi.fn();
    act(() => root.render(createElement(Marker, { enabled: true, more })));
    const mobile = observations[0]!;
    expect(mobile.options.root).toBeNull();
    container.style.overflowY = "auto";
    act(() => window.dispatchEvent(new Event("resize")));
    expect(mobile.disconnect).toHaveBeenCalled();
    expect(observations.at(-1)!.options.root).toBe(container);
    intersect(mobile); // Ignore deliveries queued before disconnect.
    expect(more).not.toHaveBeenCalled();
    intersect();
    expect(more).toHaveBeenCalledTimes(1);
    act(() => window.dispatchEvent(new Event("resize")));
    intersect();
    expect(more).toHaveBeenCalledTimes(1);
  });

  it("disables loading/error/end states and ignores deliveries after cleanup", () => {
    const more = vi.fn();
    const render = (enabled: boolean) =>
      act(() => root.render(createElement(Marker, { enabled, more })));
    render(false);
    expect(observations).toHaveLength(0);
    render(true);
    const old = observations[0]!;
    render(false);
    intersect(old);
    expect(more).not.toHaveBeenCalled();
    render(true);
    intersect();
    expect(more).toHaveBeenCalledTimes(1);
  });
});

function PagedList() {
  const { list, rows } = useThreadList("INBOX", "personal", true);
  const { fetchNextPage } = list;
  const more = useCallback(() => fetchNextPage({ cancelRefetch: false }), [fetchNextPage]);
  return createElement(
    "section",
    null,
    createElement("output", null, rows.map((r) => r.rootExtId).join(",")),
    createElement(Marker, {
      enabled: list.hasNextPage === true && !list.isFetchNextPageError && !list.isFetching,
      more,
      pages: list.data?.pages,
      updatedAt: list.dataUpdatedAt,
    }),
    list.isFetchNextPageError
      ? createElement("button", { onClick: more, disabled: list.isFetching }, "Try again")
      : null,
  );
}

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 10));
  });
}

it("rearms after an unchanged background refetch consumes the intersection", async () => {
  const urls: URL[] = [];
  vi.stubGlobal("fetch", async (request: Request) => {
    const url = new URL(request.url);
    urls.push(url);
    return Response.json(
      url.searchParams.get("cursor")
        ? { chains: [{ rootExtId: "b" }] }
        : { chains: [{ rootExtId: "a" }], nextCursor: "opaque-1" },
    );
  });
  act(() => {
    root.render(createElement(QueryClientProvider, { client }, createElement(PagedList)));
  });
  await settle();
  const first = observations.at(-1)!;
  expect(first).toBeDefined();
  const data = client.getQueryCache().getAll()[0]!.state.data;
  await act(async () => {
    const refetch = client.refetchQueries();
    intersect(first); // Joins the background refetch instead of appending a page.
    await refetch;
    await new Promise((resolve) => setTimeout(resolve, 10));
  });
  expect(urls).toHaveLength(2);
  expect(client.getQueryCache().getAll()[0]!.state.data).toBe(data);
  expect(observations.at(-1)).not.toBe(first);
  intersect();
  await settle();
  expect(urls).toHaveLength(3);
  expect(urls[2]!.searchParams.get("cursor")).toBe("opaque-1");
  expect(container.querySelector("output")!.textContent).toBe("a,b");
});

it("fills a short list serially, preserves opaque cursors, pauses on error and ends", async () => {
  const urls: URL[] = [];
  let finish: ((response: Response) => void) | undefined;
  const page = (ids: string[], nextCursor?: string) =>
    Response.json({ chains: ids.map((rootExtId) => ({ rootExtId })), nextCursor });
  vi.stubGlobal("fetch", (request: Request) => {
    urls.push(new URL(request.url));
    return new Promise<Response>((resolve) => {
      finish = resolve;
    });
  });
  act(() => {
    root.render(createElement(QueryClientProvider, { client }, createElement(PagedList)));
  });
  expect(observations).toHaveLength(0); // Initial request is still pending.
  finish!(page(["a"], "opaque-1"));
  await settle();
  intersect(); // Visible sentinel in a short list needs no scroll event.
  intersect();
  expect(urls).toHaveLength(2);
  expect(urls[1]!.searchParams.get("cursor")).toBe("opaque-1");
  finish!(page(["a", "b"], "opaque-2"));
  await settle();
  expect(container.querySelector("output")!.textContent).toBe("a,b");
  intersect();
  expect(urls).toHaveLength(3);
  expect(urls[2]!.searchParams.get("cursor")).toBe("opaque-2");
  finish!(Response.json({ error: "unavailable" }, { status: 503 }));
  await settle();
  intersect(); // A queued delivery must not spin on a failed page.
  expect(urls).toHaveLength(3);
  expect(container.querySelector("button")!.textContent).toBe("Try again");
  act(() => container.querySelector("button")!.click());
  expect(urls).toHaveLength(4);
  expect(urls[3]!.searchParams.get("cursor")).toBe("opaque-2");
  finish!(page(["c"]));
  await settle();
  expect(container.querySelector("output")!.textContent).toBe("a,b,c");
  expect(container.querySelector("button")).toBeNull();
  intersect();
  expect(urls).toHaveLength(4);
  expect(urls.every((url) => url.searchParams.get("accountId") === "personal")).toBe(true);
  expect(urls.every((url) => url.searchParams.get("label") === "INBOX")).toBe(true);
});

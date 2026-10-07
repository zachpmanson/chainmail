// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, cleanup } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useMailAction, useReadAction } from "../src/lib/mailActions";
import type { ChainHit } from "../src/lib/api";

const ROOT = "mail:<action-lifecycle@example.test>";
const LIST_KEY = ["get", "/v1/search", { params: { query: { label: "INBOX" } } }] as const;
const LABELS_KEY = ["get", "/v1/labels"] as const;
const STATS_KEY = ["get", "/v1/stats"] as const;
const CHAIN_KEY = ["get", "/v1/chains/{rootExtId}"] as const;

function Harness() {
  const mail = useMailAction();
  const read = useReadAction({ invalidateChain: true });
  return (
    <>
      <button onClick={() => mail.mutate({ body: { chains: [ROOT], action: "archive" } })}>
        Archive
      </button>
      <button onClick={() => read.mutate({ body: { chain: ROOT, unread: true } })}>
        Mark unread
      </button>
    </>
  );
}

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(LIST_KEY, {
    chains: [{ rootExtId: ROOT, entries: 3, unread: 0 } as ChainHit],
  });
  client.setQueryData(LABELS_KEY, {});
  client.setQueryData(STATS_KEY, {});
  client.setQueryData(CHAIN_KEY, {});
  render(
    <QueryClientProvider client={client}>
      <Harness />
    </QueryClientProvider>,
  );
  return client;
}

function response(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

function stubResponse(path: string, result: Response) {
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const request =
      input instanceof Request ? input : new Request(new URL(String(input), location.href), init);
    if (new URL(request.url).pathname === path) return result;
    return response(200, {});
  });
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("shared mail action lifecycle", () => {
  it("restores every optimistic list change when a mail write is refused", async () => {
    stubResponse("/v1/mail", response(403, { error: "not allowed" }));
    const client = setup();

    fireEvent.click(screen.getByRole("button", { name: "Archive" }));

    await waitFor(() => {
      expect(client.getQueryData<{ chains: ChainHit[] }>(LIST_KEY)?.chains).toHaveLength(1);
    });
    expect(client.getQueryState(LIST_KEY)?.isInvalidated).toBe(false);
  });

  it("invalidates list, label, stats, and thread queries after a mail write", async () => {
    stubResponse(
      "/v1/mail",
      response(200, { action: "archive", changed: 1, skipped: 0, chains: [] }),
    );
    const client = setup();

    fireEvent.click(screen.getByRole("button", { name: "Archive" }));

    await waitFor(() => expect(client.getQueryState(LABELS_KEY)?.isInvalidated).toBe(true));
    expect(client.getQueryState(LIST_KEY)?.isInvalidated).toBe(true);
    expect(client.getQueryState(STATS_KEY)?.isInvalidated).toBe(true);
    expect(client.getQueryState(CHAIN_KEY)?.isInvalidated).toBe(true);
  });

  it("optimistically marks unread, then invalidates the list and open thread", async () => {
    stubResponse("/v1/read", response(200, { chain: ROOT, unread: true, marked: 3, skipped: 0 }));
    const client = setup();

    fireEvent.click(screen.getByRole("button", { name: "Mark unread" }));

    await waitFor(() => {
      expect(client.getQueryData<{ chains: ChainHit[] }>(LIST_KEY)?.chains[0]?.unread).toBe(3);
      expect(client.getQueryState(CHAIN_KEY)?.isInvalidated).toBe(true);
    });
    expect(client.getQueryState(LIST_KEY)?.isInvalidated).toBe(true);
  });

  it("rolls back the unread count when the read write is refused", async () => {
    stubResponse("/v1/read", response(403, { error: "not allowed" }));
    const client = setup();

    fireEvent.click(screen.getByRole("button", { name: "Mark unread" }));

    await waitFor(() => {
      expect(client.getQueryData<{ chains: ChainHit[] }>(LIST_KEY)?.chains[0]?.unread).toBe(0);
    });
    expect(client.getQueryState(LIST_KEY)?.isInvalidated).toBe(false);
  });
});

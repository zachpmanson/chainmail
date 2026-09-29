// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import { QueryClientProvider, useQuery } from "@tanstack/react-query";
import { makeQueryClient } from "../src/lib/queryClient";
import { AUTO_REFRESH_MS, AutoRefresh } from "../src/components/AutoRefresh";

// React refuses to batch updates outside act() unless told it is under test.
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

/**
 * The corpus read, as the cadence makes it happen. `reads` is the observable —
 * one call to the query function is one read of the corpus — and the `holding`
 * switch keeps one in the air so a tick, or a focus, can be made to land on top
 * of it.
 */
let reads = 0;
let holding = false;
let release: (() => void) | null = null;

/** One query, kept stale-able by the same client the app builds. */
function Reader() {
  useQuery({
    queryKey: ["corpus"],
    queryFn: () => {
      reads += 1;
      if (!holding) return Promise.resolve({ ok: true });
      return new Promise<{ ok: boolean }>((resolve) => {
        release = () => resolve({ ok: true });
      });
    },
    staleTime: 5 * 60 * 1000,
  });
  return <AutoRefresh />;
}

/** `document.visibilityState` is a getter on the prototype, so a test sets its
 *  own over it and deletes that again in afterEach. */
function setVisibility(state: "visible" | "hidden") {
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    get: () => state,
  });
}

/** Mount the shell's cadence over one reader, and let the first read land. */
async function mount() {
  render(
    <QueryClientProvider client={makeQueryClient()}>
      <Reader />
    </QueryClientProvider>,
  );
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
}

/** Let fake timers run, and let anything they resolve settle. */
const tick = (ms: number) =>
  act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });

beforeEach(() => {
  vi.useFakeTimers();
  reads = 0;
  holding = false;
  release = null;
  setVisibility("visible");
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  Reflect.deleteProperty(document, "visibilityState");
});

describe("the corpus re-reads itself", () => {
  it("reads once per interval, and not before the interval is up", async () => {
    await mount();
    expect(reads).toBe(1);

    await tick(AUTO_REFRESH_MS - 1);
    expect(reads).toBe(1);

    await tick(1);
    expect(reads).toBe(2);

    await tick(AUTO_REFRESH_MS);
    expect(reads).toBe(3);
  });

  it("a tick that lands on a read in flight does not start a second", async () => {
    await mount();
    holding = true;

    await tick(AUTO_REFRESH_MS);
    expect(reads).toBe(2); // the tick's read, held open

    // Two more intervals pass with the first read still unanswered: a fetch that
    // is slow, or a server that is not answering, must not pile up behind it.
    await tick(AUTO_REFRESH_MS * 2);
    expect(reads).toBe(2);

    holding = false;
    await act(async () => {
      release?.();
    });
    await tick(AUTO_REFRESH_MS);
    expect(reads).toBe(3);
  });

  it("a focus mid-read does not start a second", async () => {
    await mount();
    holding = true;
    await tick(AUTO_REFRESH_MS);
    expect(reads).toBe(2);

    await act(async () => {
      window.dispatchEvent(new Event("focus"));
    });
    expect(reads).toBe(2);

    holding = false;
    await act(async () => {
      release?.();
    });
  });

  it("reads at once on focus, and a focus beside a visibility change is one read", async () => {
    await mount();
    expect(reads).toBe(1);

    // A window coming back reports both signals, in whichever order. They are one
    // event, so they are one read.
    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"));
      window.dispatchEvent(new Event("focus"));
    });
    expect(reads).toBe(2);

    // A focus on its own is the window-unfocused case, and is owed a read too.
    await tick(2000);
    await act(async () => {
      window.dispatchEvent(new Event("focus"));
    });
    expect(reads).toBe(3);
  });

  it("does not read while hidden, and reads once when the reader comes back", async () => {
    await mount();
    expect(reads).toBe(1);

    setVisibility("hidden");
    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"));
    });

    await tick(AUTO_REFRESH_MS * 3);
    expect(reads).toBe(1);

    setVisibility("visible");
    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(reads).toBe(2);

    await tick(AUTO_REFRESH_MS);
    expect(reads).toBe(3);
  });
});

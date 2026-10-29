// @vitest-environment jsdom
import { act, createElement, useLayoutEffect } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Body from "../src/components/thread/Body";
import Message, { type MessageReading } from "../src/components/thread/Message";
import type { OriginalPayload } from "../src/lib/message/original";
import type { Original } from "../src/lib/message/useOriginal";
import { usePrefs } from "../src/lib/prefs/usePrefs";

const original = (
  defaultsToOriginal: boolean,
  html = "<p>Original message</p>",
): OriginalPayload => ({
  html,
  analysis: { score: 0, matches: [], defaultsToOriginal, uncertain: false },
});
const save = vi.fn(async () => {});
const reading = (
  load: MessageReading["original"]["load"],
  preferOriginal?: boolean,
  extId = "body-test",
): MessageReading => ({
  original: { extId, load },
  person: { id: 1, preferOriginal },
  onPreferOriginal: save,
});
function deferred() {
  let resolve!: (payload: OriginalPayload) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<OriginalPayload>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
let root: Root;
let container: HTMLDivElement;
const body = () => container.querySelector<HTMLElement>(".bd")!;
const select = () => container.querySelector<HTMLSelectElement>("select")!;
const snapshot = () => ({
  text: body().textContent,
  shadow: body().shadowRoot?.textContent ?? null,
  status: body().getAttribute("role"),
});
type Snapshot = ReturnType<typeof snapshot>;
let beforePaint: Snapshot[];

// A parent's layout effect observes the real body before passive fetching effects run.
function Observe({ source, text }: { source?: MessageReading; text: string }) {
  useLayoutEffect(() => {
    beforePaint.push(snapshot());
  });
  return createElement(Message, {
    email: {
      sender: "Ada",
      fromEmail: "body@example.test",
      body: text,
      stamp: { date: "21 Oct 2026", zone: "unknown" },
    },
    place: { id: "body" },
    look: { orgSlot: "" },
    reading: source,
  });
}
function render(source?: MessageReading, text = "<p>Simplified message</p>") {
  act(() => root.render(createElement(Observe, { source, text })));
  return beforePaint.at(-1)!;
}
const loading: Snapshot = { text: "Loading message…", shadow: null, status: "status" };
const simplified: Snapshot = { text: "Simplified message", shadow: null, status: null };
function expectOriginal(text = "Original message") {
  expect(snapshot()).toEqual({ text: "", shadow: text, status: null });
}

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  usePrefs.setState({ styledSenders: [], plainSenders: [] });
  save.mockClear();
  beforePaint = [];
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  usePrefs.setState({ styledSenders: [], plainSenders: [] });
  vi.unstubAllGlobals();
});

describe("message body rendering decision", () => {
  it.each([true, false])("waits from the first paint for Default to resolve to %s", async (on) => {
    const pending = deferred();
    render(reading(() => pending.promise));
    expect(beforePaint).toEqual([loading]);
    expect(snapshot()).toEqual(loading);
    expect(select().value).toBe("default");
    expect(select().getAttribute("aria-busy")).toBe("true");
    await act(async () => pending.resolve(original(on)));
    if (on) expectOriginal();
    else expect(snapshot()).toEqual(simplified);
    expect(select().value).toBe("default");
    expect(select().getAttribute("aria-busy")).toBe("false");
    expect(save).not.toHaveBeenCalled();
  });

  it("waits for explicit Original even when analysis selects Simplified", async () => {
    const pending = deferred();
    expect(render(reading(() => pending.promise, true))).toEqual(loading);
    expect(select().value).toBe("original");
    await act(async () => pending.resolve(original(false)));
    expectOriginal();
    expect(select().value).toBe("original");
  });

  it("keeps explicit Simplified immediate even while header analysis loads", async () => {
    const pending = deferred();
    const load = vi.fn(() => pending.promise);
    expect(render(reading(load, false))).toEqual(simplified);
    expect(load).not.toHaveBeenCalled();
    await act(async () => {
      const details = container.querySelector("details")!;
      details.open = true;
      details.dispatchEvent(new Event("toggle"));
    });
    expect(load).toHaveBeenCalledOnce();
    expect(snapshot()).toEqual(simplified);
    expect(container.querySelector("[role=status]")).toBeNull();
    await act(async () => pending.resolve(original(true)));
    expect(snapshot()).toEqual(simplified);
    expect(select().value).toBe("simplified");
  });

  it("renders immediately without an original source, including empty bodies", () => {
    expect(render()).toEqual(simplified);
    expect(render(undefined, "").text).toBe("No body");
    expect(container.querySelector("[role=status]")).toBeNull();
  });

  it.each([
    { mode: undefined, automatic: true },
    { mode: undefined, automatic: false },
    { mode: true, automatic: false },
  ])("does not flash Simplified for a fulfilled cache promise: %j", async ({ mode, automatic }) => {
    const cached = Promise.resolve(original(automatic));
    await cached;
    expect(render(reading(() => cached, mode))).toEqual(loading);
    await act(async () => {});
    if (mode || automatic) expectOriginal();
    else expect(snapshot()).toEqual(simplified);
  });

  it.each([
    { change: "message", previousOriginal: true },
    { change: "message", previousOriginal: false },
    { change: "loader", previousOriginal: true },
    { change: "loader", previousOriginal: false },
  ])(
    "discards old body before paint on $change change (original=$previousOriginal)",
    async ({ change, previousOriginal }) => {
      const old = deferred();
      const next = deferred();
      const load = vi.fn((id: string) => (id === "new" ? next.promise : old.promise));
      render(reading(load));
      await act(async () => old.resolve(original(previousOriginal, "<p>Old original</p>")));
      if (previousOriginal) expectOriginal("Old original");
      else expect(snapshot()).toEqual(simplified);
      const source = reading(
        change === "loader" ? () => next.promise : load,
        undefined,
        change === "message" ? "new" : "body-test",
      );
      expect(render(source, "<p>New simplified</p>")).toEqual(loading);
      expect(snapshot()).toEqual(loading);
      await act(async () => next.resolve(original(true, "<p>New original</p>")));
      expectOriginal("New original");
    },
  );

  it("ignores a late response after switching sources", async () => {
    const old = deferred();
    const next = deferred();
    const load = (id: string) => (id === "new" ? next.promise : old.promise);
    render(reading(load));
    expect(render(reading(load, undefined, "new"))).toEqual(loading);
    await act(async () => old.resolve(original(true, "<p>Stale original</p>")));
    expect(snapshot()).toEqual(loading);
    await act(async () => next.resolve(original(false)));
    expect(snapshot()).toEqual(simplified);
  });

  it("does not add a loading frame when switching modes with resolved content", async () => {
    const load = vi.fn(async () => original(true));
    const source = reading(load);
    render(source);
    await act(async () => {});
    const host = body();
    expectOriginal();
    expect(render(reading(load, true)).shadow).toBe("Original message");
    expect(body()).toBe(host);
    expect(render(reading(load, false))).toEqual(simplified);
    expect(render(source).shadow).toBe("Original message");
    expect(load).toHaveBeenCalledOnce();
  });

  it.each([undefined, true])("settles rejected/unavailable originals in mode %s", async (mode) => {
    const pending = deferred();
    expect(render(reading(() => pending.promise, mode))).toEqual(loading);
    await act(async () => pending.reject(new Error("original unavailable")));
    expect(snapshot()).toEqual(simplified);
    expect(select().value).toBe(mode ? "original" : "default");
    expect(container.querySelector("[role=status]")).toBeNull();
  });

  it.each([undefined, true])("settles missing legacy analysis in mode %s", async (mode) => {
    const pending = deferred();
    expect(render(reading(() => pending.promise, mode))).toEqual(loading);
    await act(async () => pending.resolve({ html: "<p>Legacy original</p>" }));
    if (mode) expectOriginal("Legacy original");
    else expect(snapshot()).toEqual(simplified);
    expect(container.querySelector("[role=status]")).toBeNull();
  });

  it("immediately shows Simplified when chosen during a pending Default decision", async () => {
    const pending = deferred();
    const load = () => pending.promise;
    render(reading(load));
    expect(render(reading(load, false))).toEqual(simplified);
    await act(async () => pending.resolve(original(true)));
    expect(snapshot()).toEqual(simplified);
  });

  it("keeps standalone browser preferences and source removal immediate", async () => {
    usePrefs.getState().setStyled("body@example.test", false);
    const pending = deferred();
    const source = { original: { extId: "local", load: () => pending.promise } };
    expect(render(source)).toEqual(simplified);
    act(() => usePrefs.getState().setStyled("body@example.test", true));
    expect(snapshot()).toEqual(loading);
    await act(async () => pending.resolve(original(false)));
    expectOriginal();
    expect(render()).toEqual(simplified);
  });

  it("mounts resolved Original HTML before the next paint, not in a passive effect", () => {
    function ObserveBody({ state }: { state: Original }) {
      useLayoutEffect(() => {
        beforePaint.push(snapshot());
      });
      return createElement(Body, { body: "<p>Simplified message</p>", state });
    }
    act(() => root.render(createElement(ObserveBody, { state: { at: "asking" } })));
    expect(snapshot()).toEqual(loading);
    act(() =>
      root.render(
        createElement(ObserveBody, { state: { at: "sent", html: "<p>Mounted original</p>" } }),
      ),
    );
    expect(beforePaint).toEqual([loading, { text: "", shadow: "Mounted original", status: null }]);
  });
});

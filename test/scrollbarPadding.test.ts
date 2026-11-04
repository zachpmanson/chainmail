// @vitest-environment jsdom
import { act, createElement as h } from "react";
import { beforeEach, expect, it, vi } from "vitest";
import ThreadListScroll from "../src/components/inbox/ThreadListScroll";
import { domHarness } from "./domHarness";

const render = domHarness();
let outer: number;
let inner: number;
let left: string;
let right: string;
let observers: Observer[];
class Observer {
  observe = vi.fn();
  disconnect = vi.fn();
  constructor(readonly callback: ResizeObserverCallback) {
    observers.push(this);
  }
}
beforeEach(() => {
  outer = 400;
  inner = 398;
  left = right = "1px";
  observers = [];
  vi.stubGlobal("ResizeObserver", Observer);
  vi.stubGlobal("getComputedStyle", () => ({ borderLeftWidth: left, borderRightWidth: right }));
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockImplementation(() => outer);
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(() => inner);
});
const view = () => render(h(ThreadListScroll, null, h("p", null, "Unchanged children")));
const padded = (host: HTMLElement) =>
  host.firstElementChild!.classList.contains("min-[60rem]:pe-2.5");
async function notify() {
  await act(async () =>
    observers.at(-1)!.callback([], observers.at(-1)! as unknown as ResizeObserver),
  );
}

it("adds desktop-only padding when borders account for the entire width difference", async () => {
  const host = await view();
  expect(padded(host)).toBe(true);
  expect(host.firstElementChild!.classList.contains("pe-2.5")).toBe(false);
  expect(host.textContent).toBe("Unchanged children");
  expect(observers[0]!.observe).toHaveBeenCalledWith(host.firstElementChild);
});

it("does not double-pad a native scrollbar gutter", async () => {
  inner = 388;
  expect(padded(await view())).toBe(false);
});

it.each([
  ["0px", "0px", 400],
  ["1.5px", "1.5px", 397],
  ["1px", "3px", 396],
])(
  "subtracts border widths %s/%s rather than misidentifying them as a gutter",
  async (a, b, client) => {
    left = a;
    right = b;
    inner = client;
    expect(padded(await view())).toBe(true);
  },
);

it("tracks overlay/classic mode changes and settles after its padding update", async () => {
  const host = await view();
  expect(padded(host)).toBe(true);
  inner = 388;
  await notify();
  expect(padded(host)).toBe(false);
  inner = 398;
  await notify();
  expect(padded(host)).toBe(true);
  await notify();
  await notify();
  expect(padded(host)).toBe(true);
  expect(observers).toHaveLength(1);
});

it("waits for a hidden element to acquire a real width", async () => {
  outer = inner = 0;
  const host = await view();
  expect(padded(host)).toBe(false);
  outer = 400;
  inner = 398;
  await notify();
  expect(padded(host)).toBe(true);
});

it("disconnects on unmount", async () => {
  await view();
  await render(h("p", null, "Gone"));
  expect(observers[0]!.disconnect).toHaveBeenCalledOnce();
});

it("falls back to resize measurements and removes that listener when no observer exists", async () => {
  vi.stubGlobal("ResizeObserver", undefined);
  const host = await view();
  expect(padded(host)).toBe(true);
  inner = 388;
  await act(async () => {
    window.dispatchEvent(new Event("resize"));
  });
  expect(padded(host)).toBe(false);
  const remove = vi.spyOn(window, "removeEventListener");
  await render(h("p", null, "Gone"));
  expect(remove).toHaveBeenCalledWith("resize", expect.any(Function));
});

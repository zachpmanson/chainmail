// @vitest-environment jsdom
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { act, createElement, StrictMode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import HasslesView from "../src/components/hassles/HasslesView";
import { hasslesFixture } from "./hasslesFixture";

const index = readFileSync("index.html", "utf8");
let root: Root;
let host: HTMLDivElement;
let client: QueryClient;
let previousHead: string;

beforeEach(() => {
  previousHead = document.head.innerHTML;
  document.head.innerHTML = new DOMParser().parseFromString(index, "text/html").head.innerHTML;
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const fixture = hasslesFixture();
  vi.stubGlobal("fetch", async (request: Request) => {
    const result = fixture.handle(request.url, request.method);
    return new Response(JSON.stringify(result.data), {
      status: result.status,
      headers: { "Content-Type": "application/json" },
    });
  });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  client.clear();
  document.head.innerHTML = previousHead;
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function icon() {
  return document.querySelector<HTMLLinkElement>('link[rel="icon"]');
}
async function mount(strict = false) {
  const view = createElement(QueryClientProvider, { client }, createElement(HasslesView));
  await act(async () => root.render(strict ? createElement(StrictMode, null, view) : view));
}
function expectHasslesIcon() {
  expect(document.title).toBe("Hassles");
  expect(document.querySelectorAll('link[rel="icon"]')).toHaveLength(1);
  expect(icon()!.getAttribute("href")).toBe("/hassles-favicon.svg");
  expect(icon()!.getAttribute("type")).toBe("image/svg+xml");
}

it("applies only while Hassles is mounted and restores the Chainmail PNG on navigation away", async () => {
  const original = icon()!;
  const attributes = original.outerHTML;
  expect(original.getAttribute("href")).toBe("/favicon.png");
  expect(original.getAttribute("type")).toBe("image/png");
  await mount();
  expectHasslesIcon();
  expect(icon()).toBe(original);
  await act(async () => root.render(createElement("p", null, "Another page")));
  expect(document.title).toBe("Chainmail");
  expect(icon()!.outerHTML).toBe(attributes);
  await mount();
  expectHasslesIcon();
  await act(async () => root.render(null));
  expect(icon()!.outerHTML).toBe(attributes);
  expect(document.title).toBe("Chainmail");
});

it("restores favicon and title on root unmount", async () => {
  const original = icon()!.outerHTML;
  await mount();
  expectHasslesIcon();
  await act(async () => root.unmount());
  expect(icon()!.outerHTML).toBe(original);
  expect(document.title).toBe("Chainmail");
  root = createRoot(host);
});

it.each([false, true])(
  "creates and removes a temporary icon when absent (StrictMode=%s)",
  async (strict) => {
    icon()!.remove();
    const touch = document.createElement("link");
    touch.rel = "apple-touch-icon";
    touch.href = "/touch.png";
    document.head.append(touch);
    const original = document.head.innerHTML;
    await mount(strict);
    expectHasslesIcon();
    expect(touch.getAttribute("href")).toBe("/touch.png");
    await act(async () => root.render(null));
    expect(icon()).toBeNull();
    expect(document.head.innerHTML).toBe(original);
  },
);

it("survives StrictMode effect replay and repeated entry without retaining the Hassles icon", async () => {
  const original = icon()!;
  const attributes = original.outerHTML;
  const setAttribute = vi.spyOn(original, "setAttribute");
  await mount(true);
  expectHasslesIcon();
  // StrictMode replays cleanup between the two effect setups.
  expect(setAttribute.mock.calls).toContainEqual(["href", "/favicon.png"]);
  expect(setAttribute.mock.calls).toContainEqual(["type", "image/png"]);
  for (let i = 0; i < 3; i++) {
    await act(async () => root.render(null));
    expect(icon()).toBe(original);
    expect(original.outerHTML).toBe(attributes);
    expect(document.title).toBe("Chainmail");
    await mount(true);
    expectHasslesIcon();
  }
  await act(async () => root.render(null));
  expect(original.outerHTML).toBe(attributes);
});

it.each(["href", "type", "both"])(
  "restores absent %s attributes rather than inventing defaults",
  async (absent) => {
    if (absent !== "type") icon()!.removeAttribute("href");
    if (absent !== "href") icon()!.removeAttribute("type");
    const original = icon()!.outerHTML;
    await mount();
    expectHasslesIcon();
    await act(async () => root.render(null));
    expect(icon()!.outerHTML).toBe(original);
  },
);

it("preserves the owner's source byte-for-byte and changes only the favicon canvas", () => {
  const source = readFileSync("docs/assets/hassles/dotgrid-26W12-897056.svg");
  expect(createHash("sha256").update(source).digest("hex")).toBe(
    "62359e73eaaa3be5937e1acce6259161f77f47b69c5a8ba6bb54b34f54eea842",
  );
  const favicon = readFileSync("fixtures/hassles-favicon.svg", "utf8");
  expect(favicon).toBe(
    source
      .toString("utf8")
      .replace(
        'width="675px" height="345px"',
        'width="320px" height="320px" viewBox="-40 5 320 320"',
      ),
  );
  const svg = new DOMParser().parseFromString(favicon, "image/svg+xml").documentElement;
  expect(svg.localName).toBe("svg");
  expect(svg.querySelector("parsererror")).toBeNull();
  const [x, y, width, height] = svg.getAttribute("viewBox")!.split(" ").map(Number) as [
    number,
    number,
    number,
    number,
  ];
  expect(width).toBe(height);
  expect(svg.getAttribute("width")).toBe(`${width}px`);
  expect(svg.getAttribute("height")).toBe(`${height}px`);
  // Owner-supplied visible bounds, including the 15px round stroke.
  const bounds = { left: 67.5, right: 172.5, top: 37.5, bottom: 292.5 };
  expect(bounds.left - x).toBe(x + width - bounds.right);
  expect(bounds.top - y).toBe(y + height - bounds.bottom);
  expect(bounds.top - y).toBe(32.5);
  expect((bounds.bottom - bounds.top) / height).toBeCloseTo(0.8, 2);
});

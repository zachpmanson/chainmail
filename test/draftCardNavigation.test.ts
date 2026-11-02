import ThreadMessages from "../src/components/thread/ThreadMessages";
// @vitest-environment jsdom
import { act, createElement as h } from "react";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
  useNavigate,
  useSearch,
} from "@tanstack/react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ComposeProvider from "../src/components/compose/ComposeProvider";
import ComposeBox from "../src/components/compose/ComposeBox";
import { makeQueryClient } from "../src/lib/api/queryClient";
import { domHarness, pick, settle } from "./domHarness";
import { parseTextSearch } from "../src/lib/ui/searchParams";
import type { SavedDraft } from "../src/lib/ui/draftSelection";

vi.mock("../src/lib/message/who", () => {
  const people = new Map();
  return {
    usePersonAddresses: () => people,
    addressesOf: () => [],
    wordsOf: () => ({ who: "Synthetic sender", when: "today" }),
  };
});
vi.mock("../src/components/thread/ThreadEntry", () => ({ default: () => null }));
vi.mock("../src/components/thread/ParticipantsPanel", () => ({ default: () => null }));
const saved: SavedDraft = {
  accountId: "A",
  id: "00012345678901234567890",
  messageId: "revision-1",
  kind: "compose",
  resolution: "compose",
  to: "Áda <ada@example.test>",
  cc: "",
  subject: "Saved subject",
  body: "Saved words",
  threadId: "not-a-corpus-root",
};
const render = domHarness();
let reads: string[];
let writes: { method: string; url: string; body: Record<string, unknown> }[];
let detail: () => Promise<Response>;
const json = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { "Content-Type": "application/json" } });
function Root() {
  const navigate = useNavigate();
  return h(ComposeProvider, {
    composing: true,
    closeCompose: () => void navigate({ to: "/", search: {} }),
    children: h(Outlet),
  });
}
function Editor() {
  const search = useSearch({ strict: false });
  if (search.open === "root") return h(ThreadMessages, { thread: { rootExtId: "root" } });
  return search.open === "compose"
    ? h(ComposeBox, { onClose: () => {} })
    : h("p", null, "Other view");
}
async function mount(url: string) {
  const root = createRootRoute({ component: Root });
  const index = createRoute({
    getParentRoute: () => root,
    path: "/",
    validateSearch: (search: Record<string, unknown>) => search,
    component: Editor,
  });
  const history = createMemoryHistory({ initialEntries: [url] });
  const router = createRouter({
    routeTree: root.addChildren([index]),
    history,
    parseSearch: parseTextSearch,
  });
  const client = makeQueryClient();
  await router.load();
  const host = await render(h(QueryClientProvider, { client }, h(RouterProvider, { router })));
  await settle();
  return { host, router, history };
}
async function words(host: HTMLElement, text: string) {
  const area = host.querySelector("textarea")!;
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(area, text);
    area.dispatchEvent(new InputEvent("input", { bubbles: true }));
  });
  await settle();
}
beforeEach(() => {
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
  reads = [];
  writes = [];
  detail = async () => json(saved);
  vi.spyOn(window, "confirm").mockReturnValue(true);
  vi.stubGlobal("fetch", async (request: Request) => {
    const url = new URL(request.url);
    if (request.method !== "GET") {
      const body = await request.clone().json();
      writes.push({ method: request.method, url: url.pathname, body });
      if (request.method === "PUT") return json({ ...saved, messageId: "revision-2" });
      if (request.method === "POST" && url.pathname === "/v1/drafts") return json(saved);
      throw new Error(`Unexpected mutation: ${request.method} ${url}`);
    }
    reads.push(url.pathname + url.search);
    if (url.pathname === "/auth/status")
      return json({ accounts: [{ id: "A", signedIn: true, displayName: "Synthetic" }] });
    if (url.pathname === "/v1/drafts")
      return json([
        saved,
        { ...saved, id: "unresolved", kind: "unresolved", resolution: "lookup-unavailable" },
      ]);
    if (url.pathname === `/v1/drafts/${saved.id}`) return detail();
    if (url.pathname === "/v1/chains/root")
      return json({
        entries: [
          {
            extId: "older",
            source: "mail",
            quoted: false,
            ts: "2026-01-01T00:00:00Z",
            subject: "Older",
            fromEmail: "sender@example.test",
            permalink: "https://mail.invalid/#all/old",
          },
          {
            extId: "newer",
            source: "mail",
            quoted: false,
            ts: "2026-01-02T00:00:00Z",
            subject: "Newer",
            fromEmail: "sender@example.test",
            permalink: "https://mail.invalid/#all/new",
          },
        ],
      });
    if (url.pathname === "/v1/settings") return json({});
    if (url.pathname === "/v1/people") return json({ people: [] });
    throw new Error(`Unexpected read: ${url}`);
  });
});
describe("real editor and provider with memory history", () => {
  it("lists cards below controls without prefilling; one click PUSHes account/draft/destination and opens without writes", async () => {
    const { host, router, history } = await mount("/?accountId=A&open=compose");
    expect(host.querySelector("textarea")!.value).toBe("");
    const cards = host.querySelector('[aria-label="Saved Gmail drafts"]')!;
    expect(
      host.querySelector("textarea")!.compareDocumentPosition(cards) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(cards.textContent).toContain("lookup-unavailable");
    const index = history.location.state.__TSR_index;
    await pick(cards.querySelector("button")!);
    await settle();
    expect(router.state.location.search).toMatchObject({
      accountId: "A",
      draft: saved.id,
      open: "compose",
    });
    expect(history.location.state.__TSR_index).toBe(index + 1);
    expect(host.querySelector("textarea")!.value).toBe("Saved words");
    await settle(750);
    expect(writes).toEqual([]);
    await words(host, "Newer words");
    await settle(750);
    expect(writes).toHaveLength(1);
    expect(writes[0]).toMatchObject({
      method: "PUT",
      body: { body: "Newer words", expectedMessageId: "revision-1" },
    });
    await pick(cards.querySelector("button")!);
    expect(history.location.state.__TSR_index).toBe(index + 1);
    expect(host.querySelector("textarea")!.value).toBe("Newer words");
  });
  it("requires an explicit owner and refuses malformed/wrong-owner details without writing", async () => {
    const { host } = await mount(`/?draft=${saved.id}&open=compose`);
    expect(host.textContent).toContain("explicit account ID");
    expect(reads.some((url) => url.startsWith(`/v1/drafts/${saved.id}`))).toBe(false);
    await words(host, "Input while invalid");
    await settle(750);
    expect(writes).toEqual([]);
  });
  it("protects input typed before detail resolution and requires deliberate replacement", async () => {
    let resolve!: (value: Response) => void;
    detail = () =>
      new Promise((done) => {
        resolve = done;
      });
    const { host } = await mount(`/?accountId=A&draft=${saved.id}&open=compose`);
    await words(host, "Typed during GET");
    await act(async () => resolve(json(saved)));
    await settle();
    expect(host.querySelector("textarea")!.value).toBe("Typed during GET");
    await settle(750);
    expect(writes).toEqual([]);
    const replace = [...host.querySelectorAll("button")].find(
      (b) => b.textContent === "Replace with selected draft",
    )!;
    vi.mocked(window.confirm).mockReturnValueOnce(false);
    await pick(replace);
    expect(host.querySelector("textarea")!.value).toBe("Typed during GET");
    await pick(replace);
    expect(host.querySelector("textarea")!.value).toBe("Saved words");
    await settle(750);
    expect(writes).toEqual([]);
  });
  it("canonicalizes an incompatible destination by REPLACE, not a PUSH or guessed list handle", async () => {
    const { router, history } = await mount(`/?accountId=A&draft=${saved.id}&open=stale-root`);
    await settle();
    expect(router.state.location.search.open).toBe("compose");
    expect(history.location.state.__TSR_index).toBe(0);
    expect(writes).toEqual([]);
  });
  it("cancels and approves actual navigation; browser unload is protected", async () => {
    const { host, router } = await mount("/?accountId=A&open=compose");
    await words(host, "Local input");
    const unload = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(unload);
    expect(unload.defaultPrevented).toBe(true);
    vi.mocked(window.confirm).mockReturnValueOnce(false);
    await act(async () => {
      void router.navigate({ to: "/", search: { accountId: "A", open: "elsewhere" } });
    });
    await settle();
    expect(router.state.location.search.open).toBe("compose");
    expect(host.querySelector("textarea")!.value).toBe("Local input");
    await act(async () => {
      void router.navigate({ to: "/", search: { accountId: "A", open: "elsewhere" } });
    });
    await settle();
    expect(host.querySelector("textarea")).toBeNull();
  });
});

it("a real selected reply targets the older raw parent, opens with no PUT, and shows all account cards below the editor", async () => {
  detail = async () =>
    json({
      ...saved,
      kind: "reply",
      resolution: "resolved",
      rootId: "root",
      replyToEntry: "older",
      to: "",
      cc: "",
    });
  const { host } = await mount("/?accountId=A&draft=" + saved.id + "&open=root");
  expect(host.querySelector("textarea")!.value).toBe("Saved words");
  await settle(750);
  expect(host.querySelectorAll("[data-draft]")).toHaveLength(2);
  expect(writes).toEqual([]);
  await words(host, "A real reply edit");
  await settle(750);
  expect(writes).toHaveLength(1);
  expect(writes[0]!.body).toMatchObject({
    body: "A real reply edit",
    replyToEntry: "older",
    expectedMessageId: "revision-1",
    to: [],
    cc: [],
  });
});

it("refuses a switch while a noncancelable save is outstanding", async () => {
  let finish!: (response: Response) => void;
  const normal = globalThis.fetch;
  vi.stubGlobal("fetch", (request: Request) =>
    request.method === "PUT"
      ? new Promise<Response>((resolve) => {
          finish = resolve;
        })
      : normal(request),
  );
  const { host, router } = await mount("/?accountId=A&draft=" + saved.id + "&open=compose");
  await words(host, "Pending write");
  await settle(750);
  await act(async () => {
    void router.navigate({ to: "/", search: { accountId: "A", open: "elsewhere" } });
  });
  await settle();
  expect(router.state.location.search.open).toBe("compose");
  expect(host.textContent).toContain("Wait for the current draft operation");
  expect(window.confirm).not.toHaveBeenCalled();
  await act(async () => finish(json({ ...saved, messageId: "revision-2" })));
  await settle();
});

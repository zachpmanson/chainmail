// @vitest-environment jsdom
import { act, createElement as h, useEffect } from "react";
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
import Inbox from "../src/components/inbox/Inbox";
import SelectView from "../src/components/inbox/SelectView";
import ThreadPopup from "../src/components/thread/ThreadPopup";
import ThreadPane from "../src/components/thread/ThreadPane";
import { makeQueryClient } from "../src/lib/api/queryClient";
import { domHarness, key, pick, settle, type } from "./domHarness";
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

vi.mock("../src/components/inbox/SplitPane", () => ({
  default: ({ pane }: { pane: import("react").ReactNode }) => pane,
}));
vi.mock("../src/components/inbox/ActionBar", () => ({ default: () => null }));
vi.mock("../src/components/inbox/FolderPicker", () => ({ default: () => null }));
const saved: SavedDraft = {
  accountId: "A",
  id: "opaque-00012345678901234567890",
  messageId: "m1",
  kind: "compose",
  resolution: "compose",
  to: "Ada <ada@example.test>",
  cc: "",
  subject: "Synthetic subject",
  body: "A / m1",
};
const render = domHarness();
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
const owner = (account: string, id: string) => JSON.stringify([account, id]);
let store: Map<string, SavedDraft>;
let reads: string[];
let writes: { method: string; path: string; body: Record<string, unknown> }[];
let detail: (account: string, id: string) => Promise<Response>;
let connected: string[];
let entries: Record<string, unknown>[];
let realHosts: boolean;
let mutation: (
  method: string,
  path: string,
  body: Record<string, unknown>,
) => Promise<Response> | undefined;
let mounted: string[];
let unmounted: string[];
function Root() {
  const navigate = useNavigate();
  const search = useSearch({ strict: false });
  return h(ComposeProvider, {
    composing: search.open === "compose",
    closeCompose: () => void navigate({ to: "/", search: {} }),
    children: h(Outlet),
  });
}
function Editor() {
  const search = useSearch({ strict: false });
  if (search.open === "compose") return h(ComposeBox, { onClose: () => {} });
  if (typeof search.open === "string")
    return h(ThreadPane, {
      thread: { rootExtId: search.open },
      label: "Synthetic thread",
      backLabel: "Close",
      empty: "No thread",
      onClose: () => {},
    });
  return h("p", null, "Other view");
}
// Different component TYPES reproduce Inbox → SelectView's actual editor unmount,
// not just a rerender with changed search props beneath the same editor instance.
function InboxLike() {
  useEffect(() => {
    mounted.push("inbox");
    return () => {
      unmounted.push("inbox");
    };
  }, []);
  return h(Editor);
}
function SelectLike() {
  useEffect(() => {
    mounted.push("select");
    return () => {
      unmounted.push("select");
    };
  }, []);
  return h(Editor);
}
function View() {
  const search = useSearch({ strict: false });
  if (realHosts) {
    if (search.popup === "1" && typeof search.open === "string" && search.open !== "compose")
      return h(ThreadPopup, { rootExtId: search.open });
    return search.q?.trim() || search.person?.trim() || search.since?.trim()
      ? h(SelectView)
      : h(Inbox);
  }
  return search.q ? h(SelectLike) : h(InboxLike);
}
async function mount(url = `/?accountId=A&draft=${saved.id}&open=compose`) {
  const root = createRootRoute({ component: Root });
  const index = createRoute({
    getParentRoute: () => root,
    path: "/",
    validateSearch: (search: Record<string, unknown>) => search,
    component: View,
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
  const navigate = async (search: Record<string, unknown>, replace = false) => {
    await act(async () => {
      void router.navigate({ to: "/", search, replace });
    });
    await settle();
  };
  return { host, router, client, history, navigate };
}
async function words(host: HTMLElement, text: string) {
  const area = host.querySelector("textarea")!;
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(area, text);
    area.dispatchEvent(new InputEvent("input", { bubbles: true }));
  });
  await settle();
}
function button(host: HTMLElement, text: string) {
  const found = [...host.querySelectorAll("button")].find((item) => item.textContent === text);
  expect(found, `button ${text}`).toBeTruthy();
  return found!;
}
const detailReads = () =>
  reads.filter((url) =>
    new URL(url, "https://synthetic.invalid").pathname.startsWith("/v1/drafts/"),
  );
const selected = (accountId = "A", draft = saved.id) => ({ accountId, draft, open: "compose" });
beforeEach(() => {
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
  vi.spyOn(window, "confirm").mockReturnValue(true);
  store = new Map([[owner("A", saved.id), { ...saved }]]);
  reads = [];
  writes = [];
  mounted = [];
  unmounted = [];
  connected = ["A", "B"];
  realHosts = false;
  mutation = () => undefined;
  entries = [
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
  ];
  detail = async (account, id) =>
    store.has(owner(account, id))
      ? json(store.get(owner(account, id)))
      : json({ error: "Deleted" }, 404);
  vi.stubGlobal("fetch", async (request: Request) => {
    const url = new URL(request.url);
    const account = url.searchParams.get("accountId") ?? "";
    if (request.method !== "GET") {
      const body = request.method === "DELETE" ? {} : await request.clone().json();
      writes.push({ method: request.method, path: url.pathname, body });
      const response = mutation(request.method, url.pathname, body);
      if (response) return response;
      const id = decodeURIComponent(url.pathname.split("/")[3] ?? "");
      if (request.method === "POST" && url.pathname === "/v1/drafts") {
        const created: SavedDraft = {
          ...saved,
          accountId: body.accountId as string,
          body: body.body as string,
          subject: body.subject as string,
          ...(body.replyToEntry
            ? {
                kind: "reply",
                resolution: "resolved",
                rootId: "root",
                replyToEntry: body.replyToEntry as string,
              }
            : {}),
        };
        store.set(owner(created.accountId, created.id), created);
        return json(created);
      }
      if (request.method === "PUT") {
        const key = owner(body.accountId as string, id);
        const prior = store.get(key)!;
        if (body.expectedMessageId !== prior.messageId)
          return json({ error: "Synthetic revision conflict" }, 409);
        const next: SavedDraft = {
          ...prior,
          body: body.body as string,
          subject: body.subject as string,
          to: body.to ? (body.to as string[]).join(", ") : prior.to,
          cc: body.cc ? (body.cc as string[]).join(", ") : prior.cc,
          messageId: "m2",
        };
        store.set(key, next);
        return json(next);
      }
      // Keep a stale list/detail response available after DELETE to exercise retirement.
      if (request.method === "DELETE") return json({ deleted: true });
      throw new Error(`Unexpected mutation: ${request.method} ${url}`);
    }
    reads.push(url.pathname + url.search);
    if (url.pathname === "/auth/status")
      return json({
        accounts: connected.map((id) => ({ id, signedIn: true, displayName: `Synthetic ${id}` })),
      });
    if (url.pathname === "/v1/drafts")
      return json([...store.values()].filter((item) => item.accountId === account));
    if (url.pathname.startsWith("/v1/drafts/"))
      return detail(account, decodeURIComponent(url.pathname.split("/")[3]!));
    if (url.pathname.startsWith("/v1/chains/")) return json({ entries });
    if (url.pathname === "/v1/search") return json({ chains: [] });
    if (url.pathname === "/v1/settings") return json({});
    if (url.pathname === "/v1/people") return json({ people: [] });
    throw new Error(`Unexpected read: ${url}`);
  });
});

describe("selected detail epoch and owner safety", () => {
  it("ignores an out-of-order B response after a fresh A → B → A selection", async () => {
    const a1 = deferred<Response>();
    const a2 = deferred<Response>();
    const b = deferred<Response>();
    let aReads = 0;
    detail = (account, id) =>
      id === "draft-B" ? b.promise : ++aReads === 1 ? a1.promise : a2.promise;
    const { host, navigate, router } = await mount();
    await navigate(selected("A", "draft-B"));
    await act(async () => a1.resolve(json(saved)));
    await settle();
    expect(host.querySelector("textarea")!.value).toBe("");
    await navigate(selected());
    await act(async () => b.resolve(json({ ...saved, id: "draft-B", body: "LATE B" })));
    await settle();
    expect(host.querySelector("textarea")!.value).not.toBe("LATE B");
    await act(async () => a2.resolve(json({ ...saved, body: "Fresh A", messageId: "m3" })));
    await settle();
    expect(aReads).toBe(2);
    expect(router.state.location.search.draft).toBe(saved.id);
    expect(host.querySelector("textarea")!.value).toBe("Fresh A");
    await settle(750);
    expect(writes).toEqual([]);
  });

  it("scopes the same opaque ID to its account, including delayed old-owner detail", async () => {
    const a = deferred<Response>();
    detail = async (account) =>
      account === "A"
        ? a.promise
        : json({ ...saved, accountId: "B", body: "Account B", messageId: "B-m1" });
    const { host, navigate } = await mount();
    await navigate(selected("B"));
    await settle();
    expect(host.querySelector("textarea")!.value).toBe("Account B");
    await act(async () => a.resolve(json(saved)));
    await settle();
    expect(host.querySelector("textarea")!.value).toBe("Account B");
    expect(detailReads().some((url) => url.includes("accountId=A"))).toBe(true);
    expect(detailReads().some((url) => url.includes("accountId=B"))).toBe(true);
    await words(host, "B edit");
    store.set(owner("B", saved.id), { ...saved, accountId: "B", messageId: "B-m1" });
    await settle(750);
    expect(writes).toHaveLength(1);
    expect(writes[0]!.body).toMatchObject({
      accountId: "B",
      expectedMessageId: "B-m1",
      body: "B edit",
    });
  });

  it.each(["wrong account", "wrong ID", "deleted", "unresolved", "disconnected"])(
    "fails closed for %s without retries, extra detail GETs or writes",
    async (scenario) => {
      if (scenario === "disconnected") connected = ["B"];
      detail = async () =>
        scenario === "deleted"
          ? json({ error: "Deleted" }, 404)
          : json({
              ...saved,
              ...(scenario === "wrong account" ? { accountId: "B" } : {}),
              ...(scenario === "wrong ID" ? { id: "other" } : {}),
              ...(scenario === "unresolved"
                ? { kind: "unresolved", resolution: "lookup-unavailable" }
                : {}),
            });
      const { host } = await mount();
      expect(host.textContent).toMatch(
        scenario === "disconnected"
          ? /disconnected or missing/
          : scenario === "deleted"
            ? /could not be read/
            : /no valid destination/,
      );
      expect(host.querySelector("textarea")!.value).toBe("");
      await words(host, "Must remain local");
      await settle(750);
      expect(detailReads()).toHaveLength(scenario === "disconnected" ? 0 : 1);
      expect(writes).toEqual([]);
    },
  );

  it("does not overwrite recipient-only input entered before GET resolves", async () => {
    const pending = deferred<Response>();
    detail = () => pending.promise;
    const { host } = await mount();
    const input = host.querySelector<HTMLInputElement>('input[aria-label="to addresses"]')!;
    expect(input).toBeTruthy();
    await type(input, "local@example.test");
    await key(input, "Enter");
    expect(host.querySelector('[aria-label="remove local@example.test"]')).not.toBeNull();
    await act(async () => pending.resolve(json(saved)));
    await settle();
    expect(host.querySelector('[aria-label="remove local@example.test"]')).not.toBeNull();
    expect(host.querySelector('[aria-label="remove Ada <ada@example.test>"]')).toBeNull();
    expect(host.querySelector("textarea")!.value).toBe("");
    expect(host.textContent).toContain("Replace with selected draft");
    await settle(750);
    expect(writes).toEqual([]);
  });
});

describe("provider lifetime across different editor component mounts", () => {
  it.each(["compose", "reply", "popup"])(
    "adopts %s's own autosaved card without losing newer live input during its deferred GET",
    async (kind) => {
      realHosts = true;
      store.clear();
      const pending = deferred<Response>();
      detail = () => pending.promise;
      const { host, router } = await mount(
        `/?accountId=A&open=${kind === "compose" ? "compose" : "root"}${kind === "popup" ? "&popup=1" : ""}`,
      );
      await words(host, "Locally autosaved m1");
      await settle(750);
      expect(writes.map((item) => item.method)).toEqual(["POST"]);
      const created = { ...store.get(owner("A", saved.id))! };
      const area = host.querySelector("textarea");
      await words(host, "Newer not-yet-saved input");
      await pick(button(host, "Open draft"));
      expect(router.state.location.search.draft).toBe(saved.id);
      expect(router.state.location.search.popup).toBe(kind === "popup" ? "1" : undefined);
      expect(window.confirm).not.toHaveBeenCalled();
      expect
        .soft(
          host.querySelector("textarea"),
          "own-card adoption must not unmount the live editor while detail is pending",
        )
        .toBe(area);
      expect.soft(host.querySelector("textarea")?.value).toBe("Newer not-yet-saved input");
      expect(writes).toHaveLength(1);
      await act(async () => pending.resolve(json(created)));
      await settle();
      expect
        .soft(
          host.querySelector("textarea"),
          "authoritative GET must not replace the same owner's editor",
        )
        .toBe(area);
      expect.soft(host.querySelector("textarea")?.value).toBe("Newer not-yet-saved input");
      await settle(750);
      expect(writes).toHaveLength(2);
      expect(writes[1]!.body).toMatchObject({
        expectedMessageId: "m1",
        body: "Newer not-yet-saved input",
      });
    },
  );
  it("reconstructs acknowledged B/m2, never frozen A/m1, after a q-driven editor remount", async () => {
    const { host, navigate } = await mount();
    const oldArea = host.querySelector("textarea")!;
    expect(oldArea.value).toBe("A / m1");
    await words(host, "B / m2");
    await settle(750);
    expect(writes).toHaveLength(1);
    expect(store.get(owner("A", saved.id))).toMatchObject({ body: "B / m2", messageId: "m2" });
    await navigate({ ...selected(), q: "synthetic" });
    const newArea = host.querySelector("textarea")!;
    expect(newArea).not.toBe(oldArea);
    expect(mounted).toEqual(["inbox", "select"]);
    expect(unmounted).toEqual(["inbox"]);
    expect
      .soft(
        newArea.value,
        `acknowledged server B/m2 must reconstruct; detail GET count: ${detailReads().length}`,
      )
      .toBe("B / m2");
    await settle(750);
    expect(writes).toHaveLength(1);
    await words(host, "C after remount");
    await settle(750);
    expect(writes).toHaveLength(2);
    expect(writes[1]!.body).toMatchObject({ expectedMessageId: "m2", body: "C after remount" });
  });

  it("keeps retirement through a different component mount and Back, despite stale server cards", async () => {
    const { host, navigate, history, router } = await mount();
    await navigate({ ...selected(), q: "synthetic" });
    expect(mounted).toEqual(["inbox", "select"]);
    const count = detailReads().length;
    await pick(button(host, "Discard draft"));
    await settle();
    expect(writes.map((item) => item.method)).toEqual(["DELETE"]);
    expect(router.state.location.search.draft).toBeUndefined();
    expect(host.querySelectorAll("[data-draft]")).toHaveLength(0);
    await act(async () => {
      history.back();
    });
    await settle();
    expect(router.state.location.search.draft).toBe(saved.id);
    expect(mounted).toEqual(["inbox", "select", "inbox"]);
    expect(host.textContent).toContain("sent or discarded in this session");
    expect(host.querySelector("textarea")!.value).toBe("");
    expect(host.querySelectorAll("[data-draft]")).toHaveLength(0);
    await settle(750);
    expect(detailReads()).toHaveLength(count);
    expect(writes.map((item) => item.method)).toEqual(["DELETE"]);
  });

  it("does not overwrite live same-owner input on authoritative detail refetch", async () => {
    const { host, client } = await mount();
    await words(host, "New live words");
    store.set(owner("A", saved.id), {
      ...saved,
      body: "Server refetch must not hydrate",
      messageId: "external-m9",
    });
    await act(async () => {
      await client.invalidateQueries({ queryKey: ["get", "/v1/drafts/{id}"] });
    });
    await settle();
    expect(detailReads()).toHaveLength(2);
    expect(host.querySelector("textarea")!.value).toBe("New live words");
  });
});

describe("authoritative destination and exact parent", () => {
  it("guards canonical REPLACE: cancel preserves input/history, explicit proceed replaces once", async () => {
    const pending = deferred<Response>();
    detail = () => pending.promise;
    const { host, history, router } = await mount();
    const area = host.querySelector("textarea");
    await words(host, "Dirty before canonical destination");
    vi.mocked(window.confirm).mockReturnValueOnce(false);
    await act(async () =>
      pending.resolve(
        json({
          ...saved,
          kind: "reply",
          resolution: "resolved",
          rootId: "root",
          replyToEntry: "older",
        }),
      ),
    );
    await settle();
    expect(router.state.location.search.open).toBe("compose");
    expect(host.querySelector("textarea")).toBe(area);
    expect(host.querySelector("textarea")!.value).toBe("Dirty before canonical destination");
    expect(history.location.state.__TSR_index).toBe(0);
    expect(window.confirm).toHaveBeenCalledTimes(1);
    expect(writes).toEqual([]);
    await pick(button(host, "Open destination"));
    await settle();
    expect(router.state.location.search.open).toBe("root");
    expect(history.location.state.__TSR_index).toBe(0);
    expect(window.confirm).toHaveBeenCalledTimes(2);
    expect(host.querySelector("textarea")!.value).toBe("A / m1");
    await settle(750);
    expect(writes).toEqual([]);
  });

  it("uses a fresh canonical root after reselection, but never falls back if its exact parent is absent", async () => {
    store.set(owner("A", saved.id), {
      ...saved,
      kind: "reply",
      resolution: "resolved",
      rootId: "root",
      replyToEntry: "older",
    });
    const { host, navigate, router, history } = await mount(
      `/?accountId=A&draft=${saved.id}&open=root`,
    );
    expect(host.querySelector("textarea")!.value).toBe("A / m1");
    await navigate({ accountId: "A" });
    store.set(owner("A", saved.id), {
      ...saved,
      kind: "reply",
      resolution: "resolved",
      rootId: "moved-root",
      replyToEntry: "missing-exact-parent",
    });
    await navigate({ accountId: "A", draft: saved.id, open: "root" });
    await settle();
    expect(detailReads()).toHaveLength(2);
    expect(router.state.location.search.open).toBe("moved-root");
    expect(history.location.state.__TSR_index).toBe(2);
    expect(reads.some((url) => url.startsWith("/v1/chains/moved-root"))).toBe(true);
    expect(host.textContent).toContain("No fallback message will be used");
    expect(host.querySelector("textarea")).toBeNull();
    await settle(750);
    expect(writes).toEqual([]);
  });

  it("does not reset the same selected reply target on detail and raw-chain DTO refetch", async () => {
    store.set(owner("A", saved.id), {
      ...saved,
      kind: "reply",
      resolution: "resolved",
      rootId: "root",
      replyToEntry: "older",
    });
    const { host, client, router } = await mount(`/?accountId=A&draft=${saved.id}&open=root`);
    const area = host.querySelector("textarea");
    await words(host, "Live reply words");
    store.set(owner("A", saved.id), {
      ...saved,
      kind: "reply",
      resolution: "resolved",
      rootId: "moved-root",
      replyToEntry: "older",
      body: "Stale reread",
    });
    entries = entries.map((entry) => ({ ...entry, subject: `${entry.subject} reread` }));
    await act(async () => {
      await client.invalidateQueries({ queryKey: ["get", "/v1/drafts/{id}"] });
      await client.invalidateQueries({ queryKey: ["get", "/v1/chains/{rootExtId}"] });
    });
    await settle();
    expect(detailReads()).toHaveLength(2);
    expect(router.state.location.search.open).toBe("root");
    expect(host.querySelector("textarea")).toBe(area);
    expect(host.querySelector("textarea")!.value).toBe("Live reply words");
  });

  it("does not fall back to newer mail when the exact selected parent is unanswerable", async () => {
    entries = entries.map((entry) =>
      entry.extId === "older" ? { ...entry, fromEmail: "", permalink: "" } : entry,
    );
    store.set(owner("A", saved.id), {
      ...saved,
      kind: "reply",
      resolution: "resolved",
      rootId: "root",
      replyToEntry: "older",
    });
    const { host } = await mount(`/?accountId=A&draft=${saved.id}&open=root`);
    expect(host.textContent).toContain("No fallback message will be used");
    expect(host.querySelector("textarea")).toBeNull();
    await settle(750);
    expect(writes).toEqual([]);
  });

  it("a real ThreadPane popup action preserves the explicit selected reply pair", async () => {
    store.set(owner("A", saved.id), {
      ...saved,
      kind: "reply",
      resolution: "resolved",
      rootId: "root",
      replyToEntry: "older",
    });
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    const { host, router } = await mount(`/?accountId=A&draft=${saved.id}&open=root`);
    const link = host.querySelector<HTMLAnchorElement>('[aria-label="Open in new window"]')!;
    const target = new URL(link.href);
    expect(Object.fromEntries(target.searchParams)).toEqual({
      open: "root",
      popup: "1",
      draft: saved.id,
      accountId: "A",
    });
    await pick(link);
    expect(open).toHaveBeenCalledWith(link.href, "_blank", expect.stringContaining("popup=yes"));
    expect(router.state.location.search.open).toBe("root");
    await settle(750);
    expect(writes).toEqual([]);
  });
});

describe("reviewed editor preservation regressions", () => {
  it.each([
    [true, false],
    [false, false],
    [true, true],
    [false, true],
  ] as const)(
    "keeps a busy owned reply mounted during adoption (explicit account: %s, popup: %s)",
    async (explicit, popup) => {
      realHosts = true;
      connected = ["A"];
      store.clear();
      const get = deferred<Response>();
      const put = deferred<Response>();
      detail = () => get.promise;
      mutation = (method) => (method === "PUT" ? put.promise : undefined);
      const { host, router } = await mount(
        `/?${explicit ? "accountId=A&" : ""}open=root${popup ? "&popup=1" : ""}`,
      );
      await words(host, "First saved");
      await settle(750);
      const created = { ...store.get(owner("A", saved.id))! };
      const area = host.querySelector("textarea")!;
      const checks = [...host.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')];
      await pick(checks[0]!);
      await pick(checks[1]!);
      await words(host, "PUT in progress");
      await settle(750);
      expect(writes.map((w) => w.method)).toEqual(["POST", "PUT"]);
      await words(host, "NEW words during PUT");
      area.setSelectionRange(2, 5);
      await pick(button(host, "Open draft"));
      expect(router.state.location.search).toMatchObject({
        accountId: "A",
        draft: saved.id,
        open: "root",
      });
      expect(router.state.location.search.popup).toBe(popup ? "1" : undefined);
      expect(host.querySelector("textarea")).toBe(area);
      expect(area.value).toBe("NEW words during PUT");
      expect(area.selectionStart).toBe(2);
      expect(area.selectionEnd).toBe(5);
      expect(window.confirm).not.toHaveBeenCalled();
      // Any transition that could replace the editor still refuses the outstanding PUT.
      await act(async () => {
        void router.navigate({ to: "/", search: { accountId: "A", open: "compose" } });
      });
      await settle();
      expect(router.state.location.search.open).toBe("root");
      expect(host.textContent).toContain("Wait for the current draft operation");
      await act(async () => get.resolve(json(created)));
      await settle();
      expect(host.querySelector("textarea")).toBe(area);
      expect(area.value).toBe("NEW words during PUT");
      expect(checks.map((c) => c.checked)).toEqual([false, false]);
      const updated = { ...created, messageId: "m2", body: "PUT in progress" };
      store.set(owner("A", saved.id), updated);
      await act(async () => put.resolve(json(updated)));
      await settle();
      mutation = () => undefined;
      await words(host, "Next edit after PUT");
      await settle(750);
      expect(writes).toHaveLength(3);
      expect(writes[2]!.body).toMatchObject({
        accountId: "A",
        replyToEntry: "newer",
        replyAll: false,
        expectedMessageId: "m2",
        body: "Next edit after PUT",
      });
    },
  );

  it.each([false, true])(
    "preserves an older admitted parent through sender detach, next autosave and preview (hoisted: %s)",
    async (hoisted) => {
      if (hoisted) entries[1] = { ...entries[1], edits: [{ id: "older" }] };
      store.set(owner("A", saved.id), {
        ...saved,
        kind: "reply",
        resolution: "resolved",
        rootId: "root",
        replyToEntry: "older",
      });
      mutation = (method, path, body) =>
        path === "/v1/send"
          ? Promise.resolve(
              json({
                accountId: body.accountId,
                to: "sender@example.test",
                subject: "Re: Older",
                body: body.body,
              }),
            )
          : undefined;
      const { host, router } = await mount(`/?accountId=A&draft=${saved.id}&open=root`);
      const area = host.querySelector("textarea");
      const checks = [...host.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')];
      await pick(checks[0]!);
      await pick(checks[1]!);
      await pick(host.querySelector<HTMLButtonElement>('button[title="Edit recipients"]')!);
      const from = host.querySelector<HTMLSelectElement>('select[aria-label="From"]')!;
      await act(async () => {
        from.value = "B";
        from.dispatchEvent(new Event("change", { bubbles: true }));
      });
      await settle();
      expect(router.state.location.search).toMatchObject({ accountId: "B", open: "root" });
      expect(router.state.location.search.draft).toBeUndefined();
      expect(host.querySelector("textarea")).toBe(area);
      expect(host.querySelector("textarea")!.value).toBe("A / m1");
      expect(
        [...host.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')].map(
          (c) => c.checked,
        ),
      ).toEqual([false, false]);
      await settle(750);
      expect(writes).toHaveLength(1);
      expect(writes[0]).toMatchObject({
        method: "POST",
        body: { accountId: "B", replyToEntry: "older", replyAll: false, body: "A / m1" },
      });
      await pick(button(host, "preview"));
      expect(writes).toHaveLength(2);
      expect(writes[1]).toMatchObject({
        path: "/v1/send",
        body: {
          accountId: "B",
          entry: "older",
          all: false,
          html: false,
          confirm: false,
          body: "A / m1",
        },
      });
    },
  );

  it.each(["unchanged", "body", "recipient"])(
    "reuses only unchanged departure approval for canonical REPLACE (%s)",
    async (change) => {
      const get = deferred<Response>();
      detail = () => get.promise;
      const { host, navigate, history, router } = await mount("/?accountId=A&open=compose");
      await words(host, "Approved local input");
      await navigate(selected());
      expect(window.confirm).toHaveBeenCalledTimes(1);
      if (change === "body") await words(host, "New input while detail pending");
      if (change === "recipient") {
        const to = host.querySelector<HTMLInputElement>('input[aria-label="to addresses"]')!;
        await type(to, "new@example.test");
        await key(to, "Enter");
      }
      if (change !== "unchanged") vi.mocked(window.confirm).mockReturnValueOnce(false);
      await act(async () =>
        get.resolve(
          json({
            ...saved,
            kind: "reply",
            resolution: "resolved",
            rootId: "root",
            replyToEntry: "older",
          }),
        ),
      );
      await settle();
      await settle();
      expect(window.confirm).toHaveBeenCalledTimes(change === "unchanged" ? 1 : 2);
      expect(history.location.state.__TSR_index).toBe(1);
      if (change === "unchanged") {
        expect(router.state.location.search.open).toBe("root");
        expect(host.querySelector("textarea")!.value).toBe("A / m1");
      } else {
        expect(router.state.location.search.open).toBe("compose");
        expect(host.querySelector("textarea")!.value).toBe(
          change === "body" ? "New input while detail pending" : "Approved local input",
        );
        if (change === "recipient")
          expect(host.querySelector('[aria-label="remove new@example.test"]')).not.toBeNull();
      }
      await settle(750);
      expect(writes).toEqual([]);
    },
  );

  it("refuses canonical replacement if a new noncancelable operation started after approval", async () => {
    store.clear();
    const get = deferred<Response>();
    const del = deferred<Response>();
    detail = () => get.promise;
    mutation = (method) => (method === "DELETE" ? del.promise : undefined);
    const { host, navigate, router } = await mount("/?accountId=A&open=compose");
    await words(host, "Owned local input");
    await settle(750);
    await navigate({ accountId: "A", draft: "other-selected", open: "compose" });
    expect(window.confirm).toHaveBeenCalledTimes(1);
    await pick(button(host, "Discard draft"));
    expect(writes.map((w) => w.method)).toEqual(["POST", "DELETE"]);
    await act(async () =>
      get.resolve(
        json({
          ...saved,
          id: "other-selected",
          kind: "reply",
          resolution: "resolved",
          rootId: "root",
          replyToEntry: "older",
        }),
      ),
    );
    await settle();
    expect(router.state.location.search.open).toBe("compose");
    expect(host.textContent).toContain("Wait for the current draft operation");
    await act(async () => del.resolve(json({ deleted: true })));
    await settle();
  });

  it.each(["compose", "reply"])(
    "freshly admits saved %s body/revision through actual Inbox → SelectView → Back hosts",
    async (kind) => {
      realHosts = true;
      const open = kind === "reply" ? "root" : "compose";
      if (kind === "reply")
        store.set(owner("A", saved.id), {
          ...saved,
          kind: "reply",
          resolution: "resolved",
          rootId: "root",
          replyToEntry: "older",
        });
      const { host, navigate, history } = await mount(
        `/?accountId=A&draft=${saved.id}&open=${open}`,
      );
      const first = host.querySelector("textarea");
      await words(host, "B / m2");
      await settle(750);
      await navigate({ accountId: "A", draft: saved.id, open, q: "synthetic" });
      expect(host.querySelector("textarea")).not.toBe(first);
      expect(host.querySelector("textarea")!.value).toBe("B / m2");
      expect(detailReads()).toHaveLength(2);
      const second = host.querySelector("textarea");
      await act(async () => {
        history.back();
      });
      await settle();
      expect(host.querySelector("textarea")).not.toBe(second);
      expect(host.querySelector("textarea")!.value).toBe("B / m2");
      expect(detailReads()).toHaveLength(3);
      expect(writes).toHaveLength(1);
      await words(host, "C after Back");
      await settle(750);
      expect(writes).toHaveLength(2);
      expect(writes[1]!.body).toMatchObject({
        expectedMessageId: "m2",
        body: "C after Back",
        ...(kind === "reply" ? { replyToEntry: "older" } : {}),
      });
    },
  );

  it("freshly admits saved reply across actual popup host reconstruction and Back", async () => {
    realHosts = true;
    store.set(owner("A", saved.id), {
      ...saved,
      kind: "reply",
      resolution: "resolved",
      rootId: "root",
      replyToEntry: "older",
    });
    const { host, navigate, history } = await mount(`/?accountId=A&draft=${saved.id}&open=root`);
    const first = host.querySelector("textarea");
    await words(host, "Saved before popup");
    await settle(750);
    await navigate({ accountId: "A", draft: saved.id, open: "root", popup: "1" });
    expect(host.querySelector("textarea")).not.toBe(first);
    expect(host.querySelector("textarea")!.value).toBe("Saved before popup");
    expect(detailReads()).toHaveLength(2);
    await act(async () => {
      history.back();
    });
    await settle();
    expect(host.querySelector("textarea")!.value).toBe("Saved before popup");
    expect(detailReads()).toHaveLength(3);
    await words(host, "After popup Back");
    await settle(750);
    expect(writes[1]!.body).toMatchObject({ replyToEntry: "older", expectedMessageId: "m2" });
  });
});

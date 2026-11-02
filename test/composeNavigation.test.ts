import { QueryClientProvider } from "@tanstack/react-query";
import { makeQueryClient } from "../src/lib/api/queryClient";
// @vitest-environment jsdom
import { act, createElement as h, type ReactNode } from "react";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { router } from "../src/router";
import { domHarness, focus, pick, settle, type } from "./domHarness";
import { safeComposeOrigin } from "../src/lib/ui/openSelection";

const observed = vi.hoisted(() => ({
  descriptions: [] as (string | null | undefined)[],
  panes: [] as (string | undefined)[],
  queries: [] as string[],
  empty: false,
  sentinelRow: false,
}));
vi.mock("../src/lib/api/api", async (original) => ({
  ...(await original<typeof import("../src/lib/api/api")>()),
  $api: {
    useQuery: (_method: string, path: string) => {
      observed.queries.push(path);
      if (path === "/v1/settings") return { data: { defaultFolder: "Inbox" } };
      if (path === "/v1/search")
        return {
          data: { chains: observed.empty ? [] : [{ rootExtId: "123", subject: "Thread" }] },
        };
      if (path === "/v1/people") return { data: { people: [] } };
      if (path === "/auth/status") return { data: { accounts: [] } };
      throw new Error(`Unexpected API query: ${path}`);
    },
  },
}));
vi.mock("../src/lib/inbox/useThreadList", () => ({
  default: () => ({
    list: { data: { pages: [] }, fetchNextPage: vi.fn() },
    rows: observed.empty
      ? []
      : [{ rootExtId: observed.sentinelRow ? "compose" : "123", subject: "Thread" }],
  }),
}));
vi.mock("../src/lib/inbox/lists", async (original) => {
  const actual = await original<typeof import("../src/lib/inbox/lists")>();
  return {
    ...actual,
    useLastDescription: (root: string | null, found: { rootExtId: string } | null) => {
      observed.descriptions.push(root);
      expect(found?.rootExtId).not.toBe("compose");
      return actual.useLastDescription(root, found);
    },
  };
});
vi.mock("../src/components/compose/ComposeBox", () => ({
  default: ({ onClose }: { onClose: () => void }) =>
    h("button", { "data-compose": true, onClick: onClose }, "Close compose"),
}));
vi.mock("../src/components/thread/ThreadPane", () => ({
  default: ({ thread, onClose }: { thread: { rootExtId: string } | null; onClose: () => void }) => {
    observed.panes.push(thread?.rootExtId);
    expect(thread?.rootExtId).not.toBe("compose");
    return h("button", { "data-thread": thread?.rootExtId, onClick: onClose }, "Close thread");
  },
}));
vi.mock("../src/components/thread/ThreadPopup", () => ({
  default: ({ rootExtId }: { rootExtId: string }) => {
    expect(rootExtId).not.toBe("compose");
    return h("div", { "data-popup": rootExtId });
  },
}));
vi.mock("../src/components/inbox/CompactThreadRow", () => ({
  default: ({
    thread,
    current,
    onOpen,
  }: {
    thread: { rootExtId: string };
    current: boolean;
    onOpen: () => void;
  }) =>
    h(
      "li",
      { "data-root": thread.rootExtId },
      h(
        "button",
        { "data-row": thread.rootExtId, "aria-current": current, onClick: onOpen },
        "Open thread",
      ),
    ),
}));
vi.mock("../src/components/inbox/FullThreadRow", () => ({
  default: ({
    thread,
    current,
    onOpen,
  }: {
    thread: { rootExtId: string };
    current: boolean;
    onOpen: () => void;
  }) =>
    h(
      "li",
      { "data-root": thread.rootExtId },
      h(
        "button",
        { "data-row": thread.rootExtId, "aria-current": current, onClick: onOpen },
        "Open thread",
      ),
    ),
}));
vi.mock("../src/components/inbox/SplitPane", () => ({
  default: ({ list, pane }: { list: ReactNode; pane: ReactNode }) => h("main", null, list, pane),
}));
vi.mock("../src/components/inbox/ActionBar", () => ({ default: () => null }));
vi.mock("../src/components/inbox/FolderPicker", () => ({
  default: ({ onPick }: { onPick: (label: string, accountId: string) => void }) =>
    h("button", { "data-folder": true, onClick: () => onPick("Archive", "other") }, "Pick folder"),
}));
vi.mock("../src/components/inbox/RankMeta", () => ({ default: () => null }));
vi.mock("../src/components/settings/SettingsView", () => ({
  default: () => h("p", null, "Synthetic settings"),
}));
vi.mock("../src/components/navigation/DeployStamp", () => ({ default: () => null }));
vi.mock("../src/components/navigation/NavReading", () => ({ default: () => null }));
vi.mock("../src/components/navigation/NavRefresh", () => ({ default: () => null }));
vi.mock("../src/components/navigation/AutoRefresh", () => ({ default: () => null }));
vi.mock("../src/components/navigation/NavUndo", () => ({ default: () => null }));
vi.mock("../src/components/ui/ToastHost", () => ({ default: () => null }));

const render = domHarness();
beforeEach(() => {
  observed.descriptions = [];
  observed.panes = [];
  observed.queries = [];
  observed.empty = false;
  observed.sentinelRow = false;
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
  vi.stubGlobal(
    "fetch",
    vi.fn(() => {
      throw new Error("No live requests allowed");
    }),
  );
});
async function mount(url: string, state?: { composeOrigin: string }) {
  const history = createMemoryHistory({ initialEntries: [url] });
  if (state) history.replace(url, state);
  const subject = createRouter({ ...router.options, history });
  await subject.load();
  const host = await render(
    h(QueryClientProvider, { client: makeQueryClient() }, h(RouterProvider, { router: subject })),
  );
  return { subject, history, host };
}
function control(host: HTMLElement, selector: string) {
  const found = host.querySelector<HTMLElement>(selector);
  expect(found, selector).not.toBeNull();
  return found!;
}
const compose = (host: HTMLElement) => pick(control(host, '[aria-label="Compose"]'));
const close = (host: HTMLElement) => pick(control(host, "[data-compose]"));
async function travel(
  history: ReturnType<typeof createMemoryHistory>,
  direction: "back" | "forward",
) {
  await act(async () => history[direction]());
  await settle();
}

describe("URL-owned Compose with real router history", () => {
  it("pushes Compose once, preserves home scope, and supports Back/Forward and Close origin restoration", async () => {
    const { subject, history, host } = await mount(
      "/?q=hello&mode=lexical&person=Pat&since=2026-01-01&accountId=acct&label=Inbox&open=123",
    );
    const origin = subject.state.location.href;
    const index = history.location.state.__TSR_index;
    await compose(host);
    expect(subject.state.location.search).toMatchObject({
      open: "compose",
      q: "hello",
      mode: "lexical",
      person: "Pat",
      since: "2026-01-01",
      accountId: "acct",
      label: "Inbox",
    });
    expect(subject.state.location.state.composeOrigin).toBe(origin);
    expect(history.location.state.__TSR_index).toBe(index + 1);
    const key = history.location.state.__TSR_key;
    await compose(host);
    expect(history.location.state.__TSR_key).toBe(key);
    expect(history.location.state.__TSR_index).toBe(index + 1);
    await travel(history, "back");
    expect(subject.state.location.href).toBe(origin);
    expect(host.querySelector("[data-compose]")).toBeNull();
    await travel(history, "forward");
    expect(host.querySelector("[data-compose]")).not.toBeNull();
    await close(host);
    expect(subject.state.location.href).toBe(origin);
    expect(history.location.state.__TSR_index).toBe(index + 2);
  });

  it.each(["", "q=old&"])(
    "uses the blur-committed question for Compose scope and Close origin (%s)",
    async (prefix) => {
      const { subject, history, host } = await mount(
        `/?${prefix}open=123&label=Inbox&accountId=acct`,
      );
      const index = history.location.state.__TSR_index;
      const input = control(host, '[aria-label="Search the corpus"]') as HTMLInputElement;
      await focus(input);
      await type(input, "pending question");
      expect(subject.state.location.search.q).toBe(prefix ? "old" : undefined);
      const changes: { href: string; type: string }[] = [];
      const unsubscribe = history.subscribe(({ location, action }) =>
        changes.push({ href: location.href, type: action.type }),
      );
      // pick focuses the button (committing blur), then clicks before React rerenders.
      await compose(host);
      expect(changes.map((change) => change.type)).toEqual(["REPLACE", "PUSH"]);
      const origin = changes[0]!.href;
      expect(subject.state.location.search).toMatchObject({
        q: "pending question",
        accountId: "acct",
        open: "compose",
      });
      expect(subject.state.location.state.composeOrigin).toBe(origin);
      expect(history.location.state.__TSR_index).toBe(index + 1);
      expect(host.querySelector("[data-compose]")).not.toBeNull();
      await compose(host);
      expect(changes.map((change) => change.type)).toEqual(["REPLACE", "PUSH"]);
      await close(host);
      unsubscribe();
      expect(subject.state.location.href).toBe(origin);
      expect(subject.state.location.search).toMatchObject({
        q: "pending question",
        accountId: "acct",
      });
      expect(subject.state.location.search.open).toBeUndefined();
      expect(history.location.state.__TSR_index).toBe(index + 2);
      await travel(history, "back");
      expect(subject.state.location.search.open).toBe("compose");
      await travel(history, "forward");
      expect(subject.state.location.href).toBe(origin);
    },
  );

  it.each(["", "q=hello&", "q=hello&popup=1&", "popup=1&"])(
    "deep links %sopen=compose without popup, thread metadata or thread API calls",
    async (prefix) => {
      observed.empty = true;
      const { subject, history, host } = await mount(
        `/?${prefix}accountId=acct&label=Inbox&open=compose`,
      );
      const origin = subject.state.location.href;
      const index = history.location.state.__TSR_index;
      expect(host.querySelector("[data-compose]")).not.toBeNull();
      expect(host.querySelector("[data-popup]")).toBeNull();
      expect(host.querySelector('[aria-label="Compose"]')).not.toBeNull();
      expect(observed.descriptions.length).toBeGreaterThan(0);
      expect(observed.descriptions.every((root) => root === null)).toBe(true);
      expect(observed.panes).toEqual([]);
      expect(observed.queries.every((path) => !path.includes("thread"))).toBe(true);
      await close(host);
      expect(subject.state.location.pathname).toBe("/");
      expect(subject.state.location.search).toMatchObject({
        accountId: "acct",
        label: "Inbox",
      });
      expect(subject.state.location.search.open).toBeUndefined();
      expect(subject.state.location.search.popup).toBeUndefined();
      expect(history.location.state.__TSR_index).toBe(index + 1);
      const fallback = subject.state.location.href;
      await travel(history, "back");
      expect(subject.state.location.href).toBe(origin);
      expect(host.querySelector("[data-compose]")).not.toBeNull();
      await travel(history, "forward");
      expect(subject.state.location.href).toBe(fallback);
      expect(host.querySelector("[data-compose]")).toBeNull();
    },
  );

  it.each(["", "q=hello&"])(
    "opens an inbox/search row atomically from %scompose",
    async (prefix) => {
      const { subject, history, host } = await mount(`/?${prefix}open=compose`);
      const changes: string[] = [];
      const unsubscribe = history.subscribe(({ location }) => changes.push(location.href));
      await pick(control(host, '[data-row="123"]'));
      unsubscribe();
      expect(changes).toHaveLength(1);
      expect(subject.state.location.search.open).toBe("123");
      expect(host.querySelector("[data-compose]")).toBeNull();
      await travel(history, "back");
      expect(subject.state.location.search.open).toBe("compose");
      expect(host.querySelector("[data-compose]")).not.toBeNull();
    },
  );

  it("never scrolls to or marks a corpus row matching the reserved sentinel", async () => {
    observed.sentinelRow = true;
    const query = vi.spyOn(document, "querySelectorAll");
    const { host } = await mount("/?open=compose");
    expect(control(host, '[data-row="compose"]').getAttribute("aria-current")).toBe("false");
    expect(query).not.toHaveBeenCalledWith("li[data-root]");
    expect(observed.descriptions.every((root) => root === null)).toBe(true);
  });

  it("keeps the original close destination across folder/account changes while composing", async () => {
    const { subject, host } = await mount("/?open=123&label=Inbox&accountId=acct");
    const origin = subject.state.location.href;
    await compose(host);
    await pick(control(host, "[data-folder]"));
    expect(subject.state.location.search).toMatchObject({
      open: "compose",
      label: "Archive",
      accountId: "other",
    });
    expect(subject.state.location.state.composeOrigin).toBe(origin);
    await close(host);
    expect(subject.state.location.href).toBe(origin);
  });

  it.each(["", "q=hello&"])(
    "does not select or describe the underlying row while composing (%s)",
    async (prefix) => {
      const { host } = await mount(`/?${prefix}open=compose`);
      expect(control(host, "[data-row]").getAttribute("aria-current")).toBe("false");
      expect(observed.descriptions.every((root) => root === null)).toBe(true);
      expect(observed.panes).toEqual([]);
    },
  );

  it("navigates settings → home Compose → settings on Close, including Back/Forward", async () => {
    const { subject, history, host } = await mount("/settings?q=ignored&accountId=ignored");
    await compose(host);
    expect(subject.state.location.pathname).toBe("/");
    expect(subject.state.location.search.open).toBe("compose");
    expect(subject.state.location.search.q).toBeUndefined();
    expect(subject.state.location.search.accountId).toBeUndefined();
    await travel(history, "back");
    expect(subject.state.location.pathname).toBe("/settings");
    await travel(history, "forward");
    await close(host);
    expect(subject.state.location.pathname).toBe("/settings");
  });

  it.each([
    "https://evil.test/",
    "//evil.test/",
    "/?open=compose",
    "/?open=123&open=compose",
    "/v1/threads",
  ])("ignores unsafe origin %s", async (origin) => {
    const { subject, history, host } = await mount("/?q=hello&open=compose&popup=1", {
      composeOrigin: origin,
    });
    const initial = subject.state.location.href;
    const index = history.location.state.__TSR_index;
    await close(host);
    expect(subject.state.location.pathname).toBe("/");
    expect(subject.state.location.search).toMatchObject({
      q: "hello",
    });
    expect(subject.state.location.search.open).toBeUndefined();
    expect(subject.state.location.search.popup).toBeUndefined();
    expect(history.location.state.__TSR_index).toBe(index + 1);
    const fallback = subject.state.location.href;
    await travel(history, "back");
    expect(subject.state.location.href).toBe(initial);
    expect(host.querySelector("[data-compose]")).not.toBeNull();
    await travel(history, "forward");
    expect(subject.state.location.href).toBe(fallback);
    expect(host.querySelector("[data-compose]")).toBeNull();
  });

  it("preserves Compose and its original origin through new-question and clear-search replacements", async () => {
    const { subject, history, host } = await mount("/?open=123&accountId=acct&label=Inbox");
    const origin = subject.state.location.href;
    await compose(host);
    const index = history.location.state.__TSR_index;
    const input = control(host, '[aria-label="Search the corpus"]') as HTMLInputElement;
    await focus(input);
    await type(input, "hello");
    await act(async () =>
      input.form!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })),
    );
    await settle();
    expect(subject.state.location.search).toMatchObject({
      q: "hello",
      open: "compose",
      accountId: "acct",
    });
    expect(subject.state.location.state.composeOrigin).toBe(origin);
    expect(history.location.state.__TSR_index).toBe(index);
    await pick(control(host, '[aria-label="Clear the search"]'));
    expect(subject.state.location.search.open).toBe("compose");
    expect(subject.state.location.search.q).toBeUndefined();
    expect(subject.state.location.state.composeOrigin).toBe(origin);
    await close(host);
    expect(subject.state.location.href).toBe(origin);
  });

  it.each(["123", "90071992547409931234567890", "true", "text-thread"])(
    "keeps real-thread %s selection and ordinary thread Close/search behavior",
    async (open) => {
      const { subject, host } = await mount(`/?q=hello&open=${open}`);
      expect(host.querySelector(`[data-thread="${open}"]`)).not.toBeNull();
      await focus(control(host, '[aria-label="Search the corpus"]'));
      await pick(control(host, '[aria-label="Clear the search"]'));
      expect(subject.state.location.search.open).toBe(open);
      const input = control(host, '[aria-label="Search the corpus"]') as HTMLInputElement;
      await type(input, "next");
      await act(async () =>
        input.form!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })),
      );
      await settle();
      expect(subject.state.location.search.open).toBeUndefined();
      await act(async () => {
        void subject.navigate({ to: "/", search: { open } });
      });
      await settle();
      await pick(control(host, "[data-thread]"));
      expect(subject.state.location.search.open).toBeUndefined();
    },
  );

  it("still dispatches a real popup and Compose action clears the popup flag", async () => {
    const { subject, host } = await mount("/?open=123&popup=1");
    expect(host.querySelector('[data-popup="123"]')).not.toBeNull();
    await act(async () => {
      void subject.navigate({ to: "/", search: { open: "123", popup: "0" } });
    });
    await settle();
    await compose(host);
    expect(subject.state.location.search.popup).toBeUndefined();
  });
});

describe("safe internal Compose origins", () => {
  it.each([
    "/",
    "/?open=123&q=hello",
    "/settings",
    "/ops?tab=merges",
    "/specs",
    "/view/example#part",
  ])("accepts %s", (href) => expect(safeComposeOrigin(href)).toBe(href));
  it.each([
    undefined,
    123,
    "javascript:alert(1)",
    "//[",
    "/\\evil.test",
    "/\n/evil.test",
    "/unknown",
    "/?open=%22compose%22",
  ])("rejects %s", (href) => expect(safeComposeOrigin(href)).toBeUndefined());
});

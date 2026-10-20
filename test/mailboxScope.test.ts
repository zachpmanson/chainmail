import { createElement } from "react";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ChainHit } from "../src/lib/api/api";
import type { SearchParams } from "../src/router";
import Inbox from "../src/components/inbox/Inbox";
import SelectView from "../src/components/inbox/SelectView";
import ThreadPopup from "../src/components/thread/ThreadPopup";
import ActionBar from "../src/components/inbox/ActionBar";
import ThreadPane from "../src/components/thread/ThreadPane";

const state = vi.hoisted(() => ({
  search: {} as SearchParams,
  rows: [] as ChainHit[],
  defaults: { defaultFolder: "INBOX", defaultFolderAccountId: "work" },
  settingsPending: false,
  compact: false,
  mail: vi.fn(),
  read: vi.fn(),
  labels: vi.fn(),
  list: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
  useSearch: () => state.search,
  useNavigate: () => state.navigate,
}));
vi.mock("../src/lib/api/api", () => ({
  searchQuery: (query: unknown) => query,
  $api: {
    useQuery: (_method: string, path: string, init: unknown) => {
      if (path === "/v1/settings")
        return { data: state.defaults, isPending: state.settingsPending };
      if (path === "/auth/status")
        return { data: { accounts: ["work", "personal"].map((id) => ({ id, signedIn: true })) } };
      if (path === "/v1/labels") {
        state.labels(init);
        return { data: { labels: [{ name: "Saved" }] } };
      }
      if (path === "/v1/search") return { data: { chains: state.rows } };
      return { data: { summary: { unread: 1 } } };
    },
    useMutation: () => ({ mutate: vi.fn() }),
  },
}));
vi.mock("../src/lib/inbox/useThreadList", () => ({
  default: (label: string, accountId: string | undefined, enabled: boolean) => {
    state.list(label, accountId, enabled);
    return { rows: state.rows, list: {} };
  },
}));
vi.mock("../src/lib/inbox/mailActions", () => ({
  useReadAction: () => ({ mutate: state.read }),
  useMailAction: () => ({ mutate: state.mail }),
}));
vi.mock("../src/lib/prefs/usePrefs", () => ({
  usePrefs: (select: (prefs: object) => unknown) =>
    select({ compact: state.compact, tree: false, listWidth: null }),
}));
vi.mock("../src/lib/ui/useOwnToast", () => ({ default: () => vi.fn() }));
vi.mock("../src/lib/ui/useLoadMore", () => ({ default: () => {} }));
vi.mock("../src/components/compose/ComposeContext", () => ({
  useCompose: () => ({ composing: false, closeCompose: vi.fn() }),
}));
vi.mock("../src/components/compose/ComposeBox", () => ({ default: () => null }));
vi.mock("../src/components/thread/ThreadMessages", () => ({ default: () => null }));

const shared = { rootExtId: "S", subject: "Shared", unread: 1, entries: 1 } as ChainHit;
const visible = { rootExtId: "T", subject: "Visible", unread: 1, entries: 1 } as ChainHit;
let rendered: ReactTestRenderer | undefined;
function render(component = Inbox) {
  act(() => {
    rendered = create(createElement(component));
  });
}
function refresh(component = Inbox) {
  act(() => {
    rendered!.update(createElement(component));
  });
}
function click(label: string) {
  act(() => {
    rendered!.root
      .findByType(ActionBar)
      .findAllByType("button")
      .find((node) => node.props["aria-label"] === label)!
      .props.onClick();
  });
}
function tick(subject: string) {
  act(() => {
    rendered!.root.findByProps({ "aria-label": `Select ${subject}` }).props.onChange();
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  state.search = {};
  state.rows = [shared];
  state.defaults = { defaultFolder: "INBOX", defaultFolderAccountId: "work" };
  state.settingsPending = false;
  state.compact = false;
  vi.stubGlobal("document", {
    body: { classList: { add: vi.fn(), remove: vi.fn() } },
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    querySelector: () => null,
    querySelectorAll: () => [],
  });
});
afterEach(() => {
  act(() => {
    rendered?.unmount();
  });
  rendered = undefined;
  vi.unstubAllGlobals();
});

describe("selection belongs to the effective mailbox location", () => {
  it("does not trash a hidden shared copy after switching accounts", () => {
    state.search = { accountId: "work", label: "INBOX" };
    render();
    tick("Shared");
    expect(rendered!.root.findByType(ActionBar).props.chosen).toEqual(["S"]);
    state.search = { accountId: "personal", label: "INBOX" };
    state.rows = [visible]; // S exists in Personal/Saved, not this folder.
    refresh();
    expect(rendered!.root.findByType(ActionBar).props.chosen).toEqual([]);
    tick("Visible");
    click("Delete");
    expect(state.mail).toHaveBeenCalledExactlyOnceWith({
      body: { chains: ["T"], action: "trash", accountId: "personal" },
    });
    state.search = { accountId: "work", label: "INBOX" };
    state.rows = [shared];
    refresh();
    expect(rendered!.root.findByType(ActionBar).props.chosen).toEqual([]);
  });

  it("clears ticks when only the label changes", () => {
    state.search = { accountId: "work", label: "INBOX" };
    render();
    tick("Shared");
    state.search = { accountId: "work", label: "Saved" };
    refresh();
    expect(rendered!.root.findByType(ActionBar).props.chosen).toEqual([]);
  });

  it("clears ticks when the settings-resolved default changes", () => {
    render();
    tick("Shared");
    state.defaults = { defaultFolder: "INBOX", defaultFolderAccountId: "personal" };
    refresh();
    expect(rendered!.root.findByType(ActionBar).props.chosen).toEqual([]);
  });

  it("clears search selection in the same render as a new account/question", () => {
    state.search = { q: "shared", accountId: "work" };
    render(SelectView);
    tick("Shared");
    state.search = { q: "shared", accountId: "personal" };
    refresh(SelectView);
    expect(rendered!.root.findByType(ActionBar).props.chosen).toEqual([]);
  });
});

describe("all write surfaces use the displayed account", () => {
  it.each(["default", "explicit", "search"])(
    "scopes row, bulk, pane and move actions from %s",
    (kind) => {
      state.compact = true;
      state.search =
        kind === "default"
          ? { open: "S" }
          : {
              accountId: "work",
              label: "INBOX",
              open: "S",
              ...(kind === "search" ? { q: "shared" } : {}),
            };
      render(kind === "search" ? SelectView : Inbox);
      if (kind !== "search") expect(state.list).toHaveBeenLastCalledWith("INBOX", "work", true);
      act(() => {
        rendered!.root
          .findByType("li")
          .findByProps({ "aria-label": "Shared" })
          .props.onDoubleClick();
      });
      expect(state.read).toHaveBeenLastCalledWith({
        body: { chain: "S", unread: false, accountId: "work" },
      });
      act(() => {
        rendered!.root
          .findAllByType("button")
          .find((node) => node.props["aria-label"] === "Archive Shared")!
          .props.onClick();
      });
      expect(state.mail).toHaveBeenLastCalledWith({
        body: { chains: ["S"], action: "archive", accountId: "work" },
      });
      tick("Shared");
      for (const [label, action] of [
        ["Archive", "archive"],
        ["Delete", "trash"],
      ]) {
        click(label!);
        expect(state.mail).toHaveBeenLastCalledWith({
          body: { chains: ["S"], action, accountId: "work" },
        });
      }
      const pane = rendered!.root.findByType(ThreadPane);
      for (const [label, action] of [
        ["Archive", "archive"],
        ["Delete", "trash"],
      ]) {
        act(() => {
          pane
            .findAllByType("button")
            .find((node) => node.props["aria-label"] === label)!
            .props.onClick();
        });
        expect(state.mail).toHaveBeenLastCalledWith({
          body: { chains: ["S"], action, accountId: "work" },
        });
      }
      act(() => {
        pane
          .findAllByType("button")
          .find((node) => node.props["aria-label"] === "Mark read")!
          .props.onClick();
      });
      expect(state.read).toHaveBeenLastCalledWith({
        body: { chain: "S", unread: false, accountId: "work" },
      });
      // Both move selectors must offer only the effective account, not other mailboxes.
      for (const select of rendered!.root.findAllByType("select")) {
        act(() => {
          select.props.onChange({ target: { value: "Saved" } });
        });
        expect(state.mail).toHaveBeenLastCalledWith({
          body: { chains: ["S"], action: "move", labels: ["Saved"], accountId: "work" },
        });
      }
      expect(
        state.labels.mock.calls.every(([init]) => init.params.query.accountId === "work"),
      ).toBe(true);
      const popup = pane.findByProps({ "aria-label": "Open in new window" });
      const url = new URL(popup.props.href, "http://localhost");
      expect(url.searchParams.get("accountId")).toBe("work");
      expect(url.searchParams.get("popup")).toBe("1");
    },
  );

  it("scopes the full-size row's read toggle too", () => {
    render();
    act(() => {
      rendered!.root.findByType("li").findByProps({ "aria-label": "Shared" }).props.onDoubleClick();
    });
    expect(state.read).toHaveBeenLastCalledWith({
      body: { chain: "S", unread: false, accountId: "work" },
    });
  });

  it("does not apply the saved home default to an unscoped search", () => {
    state.search = { q: "shared", open: "S" };
    render(SelectView);
    tick("Shared");
    click("Delete");
    expect(state.mail).toHaveBeenLastCalledWith({ body: { chains: ["S"], action: "trash" } });
  });

  it("keeps explicit popup scope instead of applying the home default", () => {
    state.search = { accountId: "personal", popup: "1", open: "S" };
    act(() => {
      rendered = create(createElement(ThreadPopup, { rootExtId: "S" }));
    });
    act(() => {
      rendered!.root
        .findAllByType("button")
        .find((node) => node.props["aria-label"] === "Delete")!
        .props.onClick();
    });
    expect(state.mail).toHaveBeenLastCalledWith({
      body: { chains: ["S"], action: "trash", accountId: "personal" },
    });
  });
});

describe("empty search results keep the explicitly opened reader", () => {
  it("renders an empty-result deep link and its back control", () => {
    state.search = { q: "absent", open: "S" };
    state.rows = [];
    render(SelectView);
    expect(rendered!.root.findByType(ThreadPane).props.thread).toEqual({ rootExtId: "S" });
    const back = rendered!.root
      .findByType(ThreadPane)
      .findAllByType("button")
      .find((node) => node.children.includes("← Results"))!;
    act(() => {
      back.props.onClick();
    });
    const update = state.navigate.mock.lastCall![0].search;
    expect(update(state.search).open).toBeUndefined();
  });

  it("retains the reader instance after the final matching result disappears", () => {
    state.search = { q: "shared", open: "S" };
    render(SelectView);
    const pane = rendered!.root.findByType(ThreadPane);
    state.rows = [];
    refresh(SelectView);
    expect(rendered!.root.findByType(ThreadPane)).toBe(pane);
    expect(pane.props.thread.rootExtId).toBe("S");
    expect(pane.props.thread.subject).toBe("Shared");
  });
});

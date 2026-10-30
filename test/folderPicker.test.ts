// @vitest-environment jsdom
import { act, createElement as h, useState, type ComponentProps } from "react";
import { beforeEach, expect, it, vi } from "vitest";
import FolderPicker from "../src/components/inbox/FolderPicker";
import { domHarness, focus, key, pick, settle } from "./domHarness";
import { useEscapeToClear } from "../src/lib/inbox/selection";

type Query = { data?: object; isPending?: boolean; isError?: boolean };
const state = vi.hoisted(() => ({
  auth: {} as Query,
  folders: {} as Record<string, Query>,
  queries: [] as string[],
}));
vi.mock("../src/lib/api/api", () => ({
  $api: {
    useQuery: (
      _method: string,
      path: string,
      options: { params?: { query: { accountId?: string } } },
    ) => {
      if (path === "/auth/status") return state.auth;
      const accountId = options.params?.query.accountId ?? "";
      state.queries.push(accountId);
      return state.folders[accountId] ?? { data: { labels: [] } };
    },
  },
}));

const render = domHarness();
beforeEach(() => {
  // jsdom lacks PointerEvent; Base UI synthesizes keyboard clicks with this constructor.
  vi.stubGlobal("PointerEvent", MouseEvent);
  state.auth = {
    data: {
      accounts: [
        { id: "personal", email: "reader@example.test", displayName: "Reader", signedIn: true },
        { id: "work", email: "", displayName: "Work", signedIn: true },
        { id: "offline", email: "offline@example.test", signedIn: false },
      ],
    },
  };
  state.folders = {
    personal: {
      data: {
        labels: [
          { name: "Archive", messages: 12 },
          { name: "Zoo", messages: 2 },
        ],
      },
    },
    work: { data: { labels: [{ name: "Archive", messages: 42 }] } },
  };
  state.queries = [];
});

const menu = () => document.querySelector<HTMLElement>('[role="menu"]');
const items = (within: ParentNode = document) => [
  ...within.querySelectorAll<HTMLElement>('[role="menuitem"]'),
];
function group(name: string) {
  const found = [...document.querySelectorAll<HTMLElement>('[role="group"]')].find((node) =>
    node.textContent?.startsWith(name),
  );
  if (!found) throw new Error(`Missing account group: ${name}`);
  expect(document.getElementById(found.getAttribute("aria-labelledby")!)?.textContent).toBe(name);
  return found;
}
async function mount(props: Partial<ComponentProps<typeof FolderPicker>> = {}) {
  const onPick = vi.fn();
  const host = await render(h(FolderPicker, { current: "", onPick, ...props }));
  const trigger = host.querySelector<HTMLButtonElement>("button")!;
  return { trigger, onPick, host };
}
async function pressKey(element: HTMLElement, value: string) {
  await key(element, value);
  await act(async () =>
    element.dispatchEvent(
      new KeyboardEvent("keyup", { key: value, bubbles: true, cancelable: true }),
    ),
  );
  await settle();
}

it("groups only signed-in accounts, retains account-specific current markers and counts, and picks by account ID", async () => {
  const { trigger, onPick, host } = await mount({ current: "Archive", currentAccountId: "work" });
  expect(trigger.textContent).toContain("Archive (Work)");
  expect(trigger.getAttribute("aria-haspopup")).toBe("menu");
  await pick(trigger);
  expect(trigger.getAttribute("aria-expanded")).toBe("true");
  expect(host.contains(menu())).toBe(false);
  expect(document.querySelectorAll('[role="group"]')).toHaveLength(2);
  expect(document.body.textContent).not.toContain("offline@example.test");
  expect(state.queries).not.toContain("offline");
  const personal = items(group("reader@example.test"));
  const work = items(group("Work"));
  expect(personal[1]!.textContent).toBe("Archive12");
  expect(work[1]!.textContent).toBe("Archive42");
  expect(personal[1]!.hasAttribute("aria-current")).toBe(false);
  expect(work[1]!.getAttribute("aria-current")).toBe("true");
  await pick(work[1]!);
  expect(onPick).toHaveBeenCalledExactlyOnceWith("Archive", "work");
  expect(menu()).toBeNull();
  expect(document.activeElement).toBe(trigger);
});

it.each(["all", "personal", "work"])(
  "distinguishes %s All mail from the other accounts",
  async (account) => {
    const currentAccountId = account === "all" ? undefined : account;
    const { trigger, onPick } = await mount({ currentAccountId });
    await pick(trigger);
    const target =
      account === "all"
        ? items()[0]!
        : items(group(account === "work" ? "Work" : "reader@example.test"))[0]!;
    expect(target.getAttribute("aria-current")).toBe("true");
    expect(document.querySelectorAll('[aria-current="true"]')).toHaveLength(1);
    await pick(target);
    expect(onPick.mock.calls).toEqual(account === "all" ? [[""]] : [["", account]]);
    expect(menu()).toBeNull();
  },
);

it("toggles the inbox default checkbox with pointer and Space without selecting or closing", async () => {
  const onDefault = vi.fn();
  const onPick = vi.fn();
  function Harness() {
    const [isDefault, setDefault] = useState(false);
    return h(FolderPicker, {
      current: "Archive",
      currentAccountId: "work",
      isDefault,
      onPick,
      onDefault: (checked) => {
        onDefault(checked);
        setDefault(checked);
      },
    });
  }
  const host = await render(h(Harness));
  await pick(host.querySelector("button")!);
  const checkbox = document.querySelector<HTMLElement>('[role="menuitemcheckbox"]')!;
  expect(checkbox.textContent).toContain("Open Archive (Work) by default");
  expect(checkbox.getAttribute("aria-checked")).toBe("false");
  await pick(checkbox);
  expect(checkbox.getAttribute("aria-checked")).toBe("true");
  expect(checkbox.querySelector("svg")).not.toBeNull();
  expect(menu()).not.toBeNull();
  await pressKey(checkbox, " ");
  expect(checkbox.getAttribute("aria-checked")).toBe("false");
  expect(onDefault.mock.calls).toEqual([[true], [false]]);
  expect(onPick).not.toHaveBeenCalled();
  expect(menu()).not.toBeNull();
});

it("hides the checkbox in default mode and preserves its accessible settings label", async () => {
  const { trigger, onPick } = await mount({
    mode: "default",
    ariaLabel: "Which folder the home page opens in",
  });
  expect(trigger.getAttribute("aria-label")).toBe("Which folder the home page opens in");
  await pick(trigger);
  expect(document.querySelector('[role="menuitemcheckbox"]')).toBeNull();
  await pick(items(group("reader@example.test"))[1]!);
  expect(onPick).toHaveBeenCalledExactlyOnceWith("Archive", "personal");
  expect(menu()).toBeNull();
});

it("supports arrow opening/navigation, Home, End, typeahead and Enter selection with focus restoration", async () => {
  const { trigger, onPick } = await mount({ mode: "default" });
  await focus(trigger);
  await key(trigger, "ArrowDown");
  expect(document.activeElement).toBe(items()[0]);
  await key(document.activeElement as HTMLElement, "ArrowDown");
  expect(document.activeElement).toBe(items()[1]);
  await key(document.activeElement as HTMLElement, "End");
  expect(document.activeElement).toBe(items().at(-1));
  await key(document.activeElement as HTMLElement, "Home");
  expect(document.activeElement).toBe(items()[0]);
  await key(document.activeElement as HTMLElement, "z");
  expect(document.activeElement?.textContent).toBe("Zoo2");
  await pressKey(document.activeElement as HTMLElement, "Enter");
  expect(onPick).toHaveBeenCalledExactlyOnceWith("Zoo", "personal");
  expect(menu()).toBeNull();
  expect(document.activeElement).toBe(trigger);
});

it("opens from ArrowUp at the last item and supports Space selection", async () => {
  const { trigger, onPick } = await mount({ mode: "default" });
  await focus(trigger);
  await key(trigger, "ArrowUp");
  expect(document.activeElement).toBe(items().at(-1));
  await pressKey(document.activeElement as HTMLElement, " ");
  expect(onPick).toHaveBeenCalledExactlyOnceWith("Archive", "work");
  expect(menu()).toBeNull();
});

it("dismisses with Escape and restores focus without selecting", async () => {
  const { trigger, onPick } = await mount();
  await pick(trigger);
  await key(menu()!, "Escape");
  expect(menu()).toBeNull();
  expect(trigger.getAttribute("aria-expanded")).toBe("false");
  expect(document.activeElement).toBe(trigger);
  expect(onPick).not.toHaveBeenCalled();
});

it("consumes menu Escape without clearing the inbox selection, then clears outside the picker", async () => {
  const clear = vi.fn();
  function Harness() {
    useEscapeToClear(true, clear);
    return h(
      "div",
      null,
      h(FolderPicker, { current: "", onPick: vi.fn() }),
      h("button", null, "Outside"),
    );
  }
  const host = await render(h(Harness));
  const trigger = host.querySelectorAll("button")[0]!;
  const outside = host.querySelectorAll("button")[1]!;
  await pick(trigger);
  await key(menu()!, "ArrowDown");
  await key(document.activeElement as HTMLElement, "Escape");
  expect(menu()).toBeNull();
  expect(clear).not.toHaveBeenCalled();
  expect(document.activeElement).toBe(trigger);
  await focus(outside);
  await key(outside, "Escape");
  expect(clear).toHaveBeenCalledOnce();
});

it("allows outside presses and focus to leave without scroll locking or stealing focus", async () => {
  const host = await render(
    h("div", null, h(FolderPicker, { current: "", onPick: vi.fn() }), h("button", null, "Outside")),
  );
  const trigger = host.querySelectorAll("button")[0]!;
  const outside = host.querySelectorAll("button")[1]!;
  const overflow = document.body.style.overflow;
  await pick(trigger);
  expect(document.body.style.overflow).toBe(overflow);
  expect(outside.closest('[aria-hidden="true"]')).toBeNull();
  await pick(outside);
  expect(menu()).toBeNull();
  expect(document.activeElement).toBe(outside);
});

it.each([false, true])(
  "closes on %s-shift Tab focus departure instead of trapping focus",
  async (backward) => {
    const host = await render(
      h(
        "div",
        null,
        h("button", null, "Before"),
        h(FolderPicker, { current: "", onPick: vi.fn() }),
        h("button", null, "After"),
      ),
    );
    const before = host.querySelectorAll("button")[0]!;
    const trigger = host.querySelectorAll("button")[1]!;
    const after = host.querySelectorAll("button")[2]!;
    await pick(trigger);
    const popup = menu()!;
    await act(async () =>
      popup.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "Tab",
          shiftKey: backward,
          bubbles: true,
          cancelable: true,
        }),
      ),
    );
    if (!backward) {
      // jsdom has no native Tab default action: focus the guard that browser Tab reaches.
      await focus(popup.nextElementSibling as HTMLElement);
    }
    await settle();
    expect(menu()).toBeNull();
    expect(document.activeElement).toBe(backward ? trigger : after);
    expect(document.activeElement).not.toBe(before);
  },
);

it("keeps a disabled settings trigger native, unfocusable and closed", async () => {
  const { trigger, onPick } = await mount({ disabled: true, mode: "default" });
  expect(trigger.disabled).toBe(true);
  await act(async () => trigger.click());
  await key(trigger, "ArrowDown");
  expect(menu()).toBeNull();
  expect(onPick).not.toHaveBeenCalled();
});

it("uses unscoped folders when no account is signed in and keeps the fallback current marker", async () => {
  state.auth = { data: { accounts: [{ id: "offline", signedIn: false }] } };
  state.folders[""] = { data: { labels: [{ name: "Legacy", messages: 7 }] } };
  const { trigger, onPick } = await mount({ current: "Legacy", currentAccountId: "missing" });
  expect(trigger.textContent).toContain("Legacy (missing)");
  await pick(trigger);
  expect(document.querySelector('[role="group"]')).toBeNull();
  expect(state.queries).toContain("");
  const legacy = items().at(-1)!;
  expect(legacy.getAttribute("aria-current")).toBe("true");
  await pick(legacy);
  expect(onPick).toHaveBeenCalledExactlyOnceWith("Legacy", undefined);
});

it.each(["pending", "error"])(
  "preserves auth %s feedback without querying fallback labels",
  async (status) => {
    state.auth = status === "pending" ? { isPending: true } : { isError: true };
    const { trigger } = await mount();
    await pick(trigger);
    expect(menu()?.textContent).toContain(
      status === "pending"
        ? "Reading connected accounts…"
        : "Connected accounts could not be read.",
    );
    if (status === "error")
      expect(document.querySelector('[role="alert"]')?.textContent).toContain("Connected accounts");
    expect(state.queries).toEqual([]);
    expect(items()).toHaveLength(1);
  },
);

it.each(["pending", "error", "empty"])(
  "preserves folder %s feedback and per-account All mail",
  async (status) => {
    state.folders.personal =
      status === "pending"
        ? { isPending: true }
        : status === "error"
          ? { isError: true }
          : { data: { labels: [] } };
    const { trigger } = await mount();
    await pick(trigger);
    const personal = group("reader@example.test");
    expect(personal.textContent).toContain(
      status === "pending"
        ? "Reading folders…"
        : status === "error"
          ? "The folder list could not be read."
          : "No message carries a label yet",
    );
    expect(items(personal).map((item) => item.textContent)).toEqual(["All mail"]);
    if (status === "error") expect(personal.querySelector('[role="alert"]')).not.toBeNull();
  },
);

it.each([2, 80])(
  "exposes all %i folders, with content sizing and a rem-based viewport-only cap",
  async (count) => {
    state.folders.personal = {
      data: {
        labels: Array.from({ length: count }, (_, index) => ({
          name: `Folder ${index + 1}`,
          messages: index,
        })),
      },
    };
    const { trigger } = await mount({ mode: "default" });
    await pick(trigger);
    const popup = menu()!;
    expect(items(group("reader@example.test"))).toHaveLength(count + 1);
    expect(popup.style.maxHeight).toBe("max(0px, calc(var(--available-height) - 2rem))");
    expect(popup.classList.contains("box-border")).toBe(true);
    expect(popup.classList.contains("overflow-y-auto")).toBe(true);
    expect(popup.classList.contains("w-(--anchor-width)")).toBe(true);
    expect([...popup.classList].some((name) => /^(?:max-h-|h-)/.test(name))).toBe(false);
    expect(popup.getAttribute("data-side")).toBe("bottom");
    await focus(popup);
    await key(popup, "End");
    await key(document.activeElement as HTMLElement, "ArrowUp");
    await key(document.activeElement as HTMLElement, "ArrowUp");
    expect(document.activeElement?.textContent).toBe(`Folder ${count}${count - 1}`);
  },
);

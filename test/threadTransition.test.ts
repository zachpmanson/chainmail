// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import ThreadPane from "../src/components/thread/ThreadPane";
import { usePrefs } from "../src/lib/prefs/usePrefs";
import { appUndo } from "../src/lib/ui/undo";
import { corpus } from "./fixtures";

const entries = [
  corpus("root", { author: "Alice", html: "<p>Root message</p>" }),
  corpus("other", { author: "Bob", html: "<p>Other message</p>" }),
  corpus("reply", { author: "Carol", parent: "root", html: "<p>Reply message</p>" }),
];
vi.mock("../src/lib/api/api", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  $api: {
    useQuery: (_method: string, path: string) => ({
      data: path === "/v1/chains/{rootExtId}" ? { entries } : { people: [] },
      isPending: false,
      isError: false,
    }),
    useMutation: () => ({ mutate: vi.fn() }),
  },
}));
vi.mock("../src/lib/inbox/useAccountId", () => ({ useAccountId: () => "default" }));
vi.mock("../src/lib/inbox/mailActions", () => ({
  useMailAction: () => ({ isPending: false, mutate: vi.fn() }),
  useReadAction: () => ({ isPending: false, mutate: vi.fn() }),
}));
vi.mock("../src/lib/ui/useOwnToast", () => ({ default: () => vi.fn() }));
vi.mock("../src/components/inbox/MoveFolder", () => ({ default: () => null }));

let root: Root;
let container: HTMLDivElement;
let client: QueryClient;
const scrollIntoView = vi.fn();
const button = () => container.querySelector<HTMLButtonElement>("button[aria-pressed]")!;
const messages = () => [...container.querySelectorAll<HTMLElement>(".msg[id]")];
const snapshot = () => ({
  tree: button().getAttribute("aria-pressed"),
  messages: messages().map((el) => ({
    id: el.id,
    name: el.style.viewTransitionName,
    nested: el.parentElement!.classList.contains("replies"),
  })),
});
type Snapshot = ReturnType<typeof snapshot>;
let snapshots: { before: Snapshot; after: Snapshot }[];
let transitions: Promise<void>[];
let start: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal(
    "matchMedia",
    vi.fn(() => ({ matches: false })),
  );
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
    configurable: true,
    value: scrollIntoView,
  });
  scrollIntoView.mockClear();
  usePrefs.setState({ tree: false });
  history.replaceState(null, "", "/?open=root&accountId=default");
  snapshots = [];
  transitions = [];
  start = vi.fn((update: () => void | Promise<void>) => {
    const before = snapshot();
    const finished = Promise.resolve()
      .then(() => {
        expect(update()).toBeUndefined();
      })
      .then(() => {
        snapshots.push({ before, after: snapshot() });
      });
    transitions.push(finished);
    return { finished };
  });
  Object.defineProperty(document, "startViewTransition", { configurable: true, value: start });
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  client = new QueryClient();
  act(() => {
    root.render(
      createElement(
        QueryClientProvider,
        { client },
        createElement(ThreadPane, {
          thread: { rootExtId: "root", subject: "Hello" },
          label: "Email",
          backLabel: "Back",
          empty: "Choose a thread",
          onClose: vi.fn(),
        }),
      ),
    );
  });
});
afterEach(async () => {
  await act(async () => {
    while (appUndo.getSnapshot().label) await appUndo.undo();
    root.unmount();
  });
  client.clear();
  container.remove();
  Reflect.deleteProperty(document, "startViewTransition");
  Reflect.deleteProperty(HTMLElement.prototype, "scrollIntoView");
  vi.unstubAllGlobals();
});

it("captures both layouts without waiting for or changing mailbox undo", async () => {
  const url = location.href;
  const scrolls = scrollIntoView.mock.calls.length;
  const undoArchive = vi.fn();
  await appUndo.perform(() => appUndo.remember({ label: "archive", undo: undoArchive }));
  let release!: () => void;
  const held = appUndo.perform(() => new Promise<void>((resolve) => (release = resolve)));
  try {
    await act(async () => {
      await Promise.resolve();
      button().click();
      await transitions[0];
    });
    expect(button().getAttribute("aria-pressed")).toBe("true");
    await act(async () => {
      button().click();
      await transitions[1];
    });

    const flat = [0, 1, 2].map((i) => ({ id: `entry-${i}`, name: `entry-${i}`, nested: false }));
    const tree = [flat[0], { id: "entry-2", name: "entry-2", nested: true }, flat[1]];
    expect(snapshots).toEqual([
      { before: { tree: "false", messages: flat }, after: { tree: "true", messages: tree } },
      { before: { tree: "true", messages: tree }, after: { tree: "false", messages: flat } },
    ]);
    expect(appUndo.getSnapshot()).toEqual({ label: "archive", pending: true });
    expect(messages().every((el) => el.style.viewTransitionName === "")).toBe(true);
    expect(localStorage.getItem("cm-treeview")).toBe("0");
    expect(location.href).toBe(url);
    expect(scrollIntoView).toHaveBeenCalledTimes(scrolls);
  } finally {
    release();
    await held;
  }
  await appUndo.undo();
  expect(undoArchive).toHaveBeenCalledOnce();
  expect(usePrefs.getState().tree).toBe(false);
  expect(appUndo.getSnapshot().label).toBe("");
});

it.each(["unsupported", "reduced motion"])("still switches both ways with %s", async (mode) => {
  if (mode === "unsupported") Reflect.deleteProperty(document, "startViewTransition");
  else
    vi.stubGlobal(
      "matchMedia",
      vi.fn(() => ({ matches: true })),
    );
  for (const next of [true, false]) {
    await act(async () => {
      button().click();
    });
    expect(button().getAttribute("aria-pressed")).toBe(String(next));
    expect(container.querySelectorAll(".replies")).toHaveLength(next ? 1 : 0);
    expect(localStorage.getItem("cm-treeview")).toBe(next ? "1" : "0");
  }
  expect(start).not.toHaveBeenCalled();
});

// @vitest-environment jsdom
import { act, createElement, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import ActionBar from "../src/components/inbox/ActionBar";
import SpecView from "../src/components/specs/SpecView";
import { msg, timeline } from "./fixtures";

const build = vi.hoisted(() => ({
  isPending: false,
  isError: false,
  error: null as unknown,
  mutate: vi.fn(),
}));
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn() }));
vi.mock("../src/lib/inbox/useAccountId", () => ({ useAccountId: () => "personal" }));
vi.mock("../src/lib/inbox/mailActions", () => ({
  useMailAction: () => ({ isPending: false, mutate: vi.fn() }),
}));
vi.mock("../src/lib/api/api", async (original) => ({
  ...(await original<typeof import("../src/lib/api/api")>()),
  $api: {
    useMutation: () => build,
    useQuery: (_method: string, path: string) => ({
      data:
        path === "/v1/settings" ? { me: ["reader@example.test"] } : { accounts: [], labels: [] },
    }),
  },
}));

let root: Root;
let host: HTMLDivElement;
beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  build.isPending = false;
  build.isError = false;
  build.error = null;
  build.mutate.mockReset();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
}
async function click(element: HTMLElement) {
  await act(async () => {
    element.focus();
    element.dispatchEvent(new MouseEvent("pointerdown", { bubbles: true, button: 0 }));
    element.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, button: 0 }));
    element.dispatchEvent(new MouseEvent("pointerup", { bubbles: true, button: 0 }));
    element.dispatchEvent(new MouseEvent("mouseup", { bubbles: true, button: 0 }));
    element.click();
  });
  await settle();
}
async function escape() {
  await act(async () => {
    document.activeElement!.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }),
    );
  });
  await settle();
}
async function type(input: HTMLInputElement, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}
function button(text: string) {
  const found = [...document.querySelectorAll("button")].find(
    (button) => button.textContent === text,
  );
  if (!found) throw new Error(`Missing button ${text}`);
  return found;
}
function dialog() {
  return document.querySelector<HTMLElement>("[role=dialog]");
}
function backdrop() {
  return [...document.querySelectorAll<HTMLElement>("[data-base-ui-portal] > [role=presentation]")]
    .filter((node) => !node.querySelector("[role=dialog]"))
    .at(-1)!;
}
const queries = [{ q: "recorded search", note: "why these threads" }];
const braid = () =>
  createElement(ActionBar, { chosen: ["mail:a", "mail:b"], queries, onDone: vi.fn() });

it("Braid focuses the title, keeps build/settings/query semantics, and retains title and error/pending flow", async () => {
  await act(async () => root.render(braid()));
  await click(button("Braid Threads"));
  const title = dialog()!.querySelector("input")!;
  expect(document.activeElement).toBe(title);
  expect(title.closest("label")!.textContent).toContain("Page title");
  expect(dialog()!.textContent).toContain("2 threads ticked");
  await click(button("Braid"));
  expect(build.mutate).toHaveBeenLastCalledWith({
    body: {
      chains: ["mail:a", "mail:b"],
      name: expect.any(String),
      me: ["reader@example.test"],
      queries,
    },
  });
  await type(title, "  My braid  ");
  await click(button("Braid"));
  expect(build.mutate).toHaveBeenLastCalledWith({
    body: {
      chains: ["mail:a", "mail:b"],
      name: "my-braid",
      title: "My braid",
      me: ["reader@example.test"],
      queries,
    },
  });
  build.isPending = true;
  await act(async () => root.render(braid()));
  expect(button("Braiding…").disabled).toBe(true);
  expect(dialog()!.querySelector("[role=status]")!.textContent).toContain("across 2 threads");
  // Builds were always dismissible while pending; this isn't an abort of the request.
  await escape();
  expect(dialog()).toBeNull();
  expect(document.activeElement).toBe(button("Braid Threads"));
  build.isPending = false;
  build.isError = true;
  build.error = new Error("Synthetic build failure");
  await click(button("Braid Threads"));
  expect(dialog()!.querySelector("input")!.value).toBe("  My braid  ");
  expect(dialog()!.textContent).toContain("Synthetic build failure");
  expect(button("Braid").disabled).toBe(false);
});

it.each(["escape", "backdrop", "close"])(
  "Braid dismisses by %s, not an inside press, and restores focus",
  async (method) => {
    await act(async () => root.render(braid()));
    await click(button("Braid Threads"));
    await click(dialog()!);
    expect(dialog()).not.toBeNull();
    if (method === "escape") await escape();
    else if (method === "backdrop") await click(backdrop());
    else await click(button("Close"));
    expect(dialog()).toBeNull();
    expect(document.activeElement).toBe(button("Braid Threads"));
  },
);

it.each(["escape", "close"])(
  "Spec JSON is fullscreen, traps focus, copies full data and closes by %s",
  async (method) => {
    const spec = {
      ...timeline(msg("one"), msg("notice", { kind: "note" })),
      avatars: { Alice: `data:image/png;base64,${"a".repeat(200)}` },
    };
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    function Harness() {
      const [open, setOpen] = useState(false);
      return createElement(
        "div",
        null,
        createElement("button", { onClick: () => setOpen(true) }, "JSON"),
        open ? createElement(SpecView, { spec, onClose: () => setOpen(false) }) : null,
      );
    }
    await act(async () => root.render(createElement(Harness)));
    await click(button("JSON"));
    const popup = dialog()!;
    const pre = popup.querySelector("pre")!;
    expect(popup.getAttribute("aria-modal")).toBe("true");
    expect(popup.className).toContain("size-full");
    expect(popup.className).not.toContain("max-h-");
    expect(document.activeElement).toBe(pre);
    expect(pre.textContent).toContain("KB omitted");
    expect(pre.textContent).not.toContain(spec.avatars.Alice);
    expect(popup.textContent).toContain("1 messages · 1 notices");
    await act(async () => (popup.nextElementSibling as HTMLElement).focus());
    await settle();
    expect(document.activeElement).toBe(button("copy"));
    await click(button("copy"));
    expect(writeText).toHaveBeenCalledExactlyOnceWith(JSON.stringify(spec, null, 2));
    expect(button("copied")).toBeTruthy();
    await click(backdrop());
    expect(dialog()).toBe(popup);
    if (method === "escape") await escape();
    else await click(button("close"));
    expect(dialog()).toBeNull();
    expect(document.activeElement).toBe(button("JSON"));
  },
);

it("Spec JSON handles unavailable clipboard without claiming a successful copy", async () => {
  vi.stubGlobal("navigator", {
    clipboard: { writeText: vi.fn().mockRejectedValue(new Error("denied")) },
  });
  await act(async () =>
    root.render(createElement(SpecView, { spec: timeline(msg("one")), onClose: vi.fn() })),
  );
  await settle();
  await click(button("copy"));
  expect(button("copy")).toBeTruthy();
});

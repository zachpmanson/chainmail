// @vitest-environment jsdom
import { act, createElement as h, StrictMode, useEffect, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import ToastHost from "../src/components/ui/ToastHost";
import {
  clearToasts,
  dismissToast,
  pushToast,
  SAID_MS,
  useToasts,
  type Toast,
} from "../src/lib/ui/toasts";
import useOwnToast from "../src/lib/ui/useOwnToast";
import { useEscapeToClear } from "../src/lib/inbox/selection";
import DialogShell from "../src/components/ui/DialogShell";

let root: Root;
let host: HTMLDivElement;
beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.useFakeTimers();
  clearToasts();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(async () => {
  await act(async () => root.unmount());
  clearToasts();
  host.remove();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function Snapshot() {
  const toasts = useToasts();
  return h("output", { "data-toast-model": true }, JSON.stringify(toasts));
}
function snapshot(): Toast[] {
  return JSON.parse(host.querySelector("output")!.textContent!);
}
async function advance(ms: number) {
  await act(async () => vi.advanceTimersByTimeAsync(ms));
}
async function render(children: ReactNode = h(ToastHost), strict = false) {
  const content = h("div", null, h(Snapshot), children);
  await act(async () => root.render(strict ? h(StrictMode, null, content) : content));
  await advance(0);
}
async function click(element: HTMLElement) {
  await act(async () => element.click());
  await advance(0);
}
async function key(element: HTMLElement, key: string, shiftKey = false) {
  await act(async () =>
    element.dispatchEvent(
      new KeyboardEvent("keydown", { key, shiftKey, bubbles: true, cancelable: true }),
    ),
  );
  await advance(0);
}
function notifications() {
  return [...document.querySelectorAll<HTMLElement>('[role="dialog"]')];
}
function closeButton(text: string) {
  return document.querySelector<HTMLButtonElement>(`[aria-label="Dismiss: ${text}"]`)!;
}
function NamedCaller({ name, resetKey }: { name: string; resetKey?: unknown }) {
  const say = useOwnToast(resetKey);
  return h(
    "div",
    null,
    h("button", { onClick: () => say(`${name} done`) }, `${name} note`),
    h("button", { onClick: () => say(`${name} failed`, "fail") }, `${name} fail`),
  );
}
function callerButton(text: string) {
  return [...host.querySelectorAll("button")].find((button) => button.textContent === text)!;
}

it("retains numeric monotonic IDs and oldest-first module snapshots without a Host", async () => {
  await render(null);
  let first = 0;
  let second = 0;
  await act(async () => {
    first = pushToast("Default note");
    second = pushToast("Persistent failure", "fail", null);
  });
  expect(Number.isInteger(first)).toBe(true);
  expect(second).toBeGreaterThan(first);
  expect(snapshot()).toEqual([
    { id: first, text: "Default note", kind: "note" },
    { id: second, text: "Persistent failure", kind: "fail" },
  ]);
  await advance(SAID_MS * 10);
  expect(snapshot()).toHaveLength(2);
  await act(async () => {
    dismissToast(first);
    dismissToast(first);
    dismissToast(-1);
  });
  expect(snapshot().map((toast) => toast.id)).toEqual([second]);
  await act(async () => {
    clearToasts();
    clearToasts();
  });
  expect(snapshot()).toEqual([]);
});

it("expires explicit five-second notes without a mounted Host and cancels timers on close/clear", async () => {
  await render(null);
  await act(async () => pushToast("Timed", "note", SAID_MS));
  expect(vi.getTimerCount()).toBe(1);
  await advance(SAID_MS - 1);
  expect(snapshot().map((toast) => toast.text)).toEqual(["Timed"]);
  await advance(1);
  expect(snapshot()).toEqual([]);
  let id = 0;
  await act(async () => {
    id = pushToast("Dismiss me", "note", SAID_MS);
  });
  await act(async () => dismissToast(id));
  expect(vi.getTimerCount()).toBe(0);
  await act(async () => {
    pushToast("Clear one", "note", SAID_MS);
    pushToast("Clear two", "note", SAID_MS);
  });
  await act(async () => clearToasts());
  expect(vi.getTimerCount()).toBe(0);
  await advance(SAID_MS);
  expect(snapshot()).toEqual([]);
});

it("bridges existing notifications on mount, preserving oldest-first styling and an unlimited persistent stack", async () => {
  const ids = [
    pushToast("First"),
    pushToast("Second", "fail"),
    pushToast("Third"),
    pushToast("Fourth", "fail"),
    pushToast("Fifth"),
  ];
  await render();
  expect(notifications()).toHaveLength(5);
  expect(notifications().map((toast) => toast.querySelector("span")?.textContent)).toEqual([
    "First",
    "Second",
    "Third",
    "Fourth",
    "Fifth",
  ]);
  expect(snapshot().map((toast) => toast.id)).toEqual(ids);
  expect(document.querySelector("[data-limited], [inert]")).toBeNull();
  expect(notifications()[1]!.classList.contains("border-red-700")).toBe(true);
  expect(notifications()[0]!.classList.contains("border-red-700")).toBe(false);
  await advance(SAID_MS * 2);
  expect(notifications()).toHaveLength(5);
});

it.each([false, true])(
  "does not lose a caller-effect notification when Host appears first=%s",
  async (hostFirst) => {
    function Startup() {
      useEffect(() => {
        pushToast("Raised during child effect", "fail");
      }, []);
      return null;
    }
    await render(
      h("div", null, hostFirst ? h(ToastHost) : h(Startup), hostFirst ? h(Startup) : h(ToastHost)),
    );
    expect(snapshot()).toHaveLength(1);
    expect(notifications()).toHaveLength(1);
    expect(closeButton("Raised during child effect")).not.toBeNull();
  },
);

it("uses only a polite Base viewport announcement, with named nonmodal toast dialogs and native close controls", async () => {
  await render();
  await act(async () => {
    pushToast("Saved");
    pushToast("Could not save", "fail");
  });
  await advance(0);
  const viewport = document.querySelector<HTMLElement>(
    '[role="region"][aria-label="Notifications"]',
  )!;
  expect(viewport.getAttribute("aria-live")).toBe("polite");
  expect(document.querySelectorAll('[aria-live="polite"]')).toHaveLength(1);
  expect(
    document.querySelector('[role="status"], [role="alert"], [role="alertdialog"]'),
  ).toBeNull();
  for (const toast of notifications()) {
    expect(toast.getAttribute("aria-modal")).toBe("false");
    const title = document.getElementById(toast.getAttribute("aria-labelledby")!);
    expect(title?.tagName).toBe("SPAN");
    expect(title?.textContent).toBe(snapshot()[notifications().indexOf(toast)]!.text);
    expect(toast.hasAttribute("aria-live")).toBe(false);
    const button = toast.querySelector("button")!;
    expect(button.type).toBe("button");
    expect(button.getAttribute("aria-hidden")).toBe("false");
    expect(button.getAttribute("aria-label")).toBe(`Dismiss: ${title?.textContent}`);
  }
});

it("syncs UI close to the module model and cancels its timer without affecting other toasts", async () => {
  await render();
  await act(async () => {
    pushToast("Timed", "note", SAID_MS);
    pushToast("Keep", "fail");
  });
  await advance(0);
  await click(closeButton("Timed"));
  expect(snapshot().map((toast) => toast.text)).toEqual(["Keep"]);
  await advance(100);
  expect(closeButton("Timed")).toBeNull();
  expect(notifications()).toHaveLength(1);
  await advance(SAID_MS * 2);
  expect(snapshot().map((toast) => toast.text)).toEqual(["Keep"]);
  await act(async () => {
    dismissToast(snapshot()[0]!.id);
    dismissToast(-1);
  });
  await advance(100);
  expect(notifications()).toHaveLength(0);
  await act(async () => {
    clearToasts();
    clearToasts();
  });
  await advance(100);
  expect(notifications()).toHaveLength(0);
});

it("keeps five-second note timing unchanged while hovered, keyboard-focused or the window is blurred", async () => {
  await render(h("div", null, h(NamedCaller, { name: "Action" }), h(ToastHost)));
  await click(callerButton("Action note"));
  const viewport = document.querySelector<HTMLElement>('[role="region"]')!;
  await act(async () => {
    viewport.dispatchEvent(new MouseEvent("mouseover", { bubbles: true }));
    closeButton("Action done").focus();
    window.dispatchEvent(new FocusEvent("blur"));
  });
  await advance(SAID_MS - 1);
  expect(snapshot().map((toast) => toast.text)).toEqual(["Action done"]);
  await advance(1);
  expect(snapshot()).toEqual([]);
  await advance(100);
  expect(notifications()).toHaveLength(0);
});

it("replaces only the caller's previous toast and resetKey clears it without disturbing another caller", async () => {
  const content = (resetKey: number) =>
    h(
      "div",
      null,
      h(NamedCaller, { name: "A", resetKey }),
      h(NamedCaller, { name: "B" }),
      h(ToastHost),
    );
  await render(content(1));
  await click(callerButton("A note"));
  await click(callerButton("B fail"));
  const firstId = snapshot()[0]!.id;
  await click(callerButton("A fail"));
  expect(snapshot().map((toast) => toast.text)).toEqual(["B failed", "A failed"]);
  expect(snapshot()[1]!.id).toBeGreaterThan(firstId);
  await advance(100);
  expect(closeButton("A done")).toBeNull();
  expect(notifications()).toHaveLength(2);
  await render(content(2));
  await advance(100);
  expect(snapshot().map((toast) => toast.text)).toEqual(["B failed"]);
  expect(notifications()).toHaveLength(1);
  await advance(SAID_MS * 2);
  expect(snapshot().map((toast) => toast.text)).toEqual(["B failed"]);
});

it("preserves remaining lifetimes across Host removal and StrictMode remount without duplicates or resurrection", async () => {
  await render(h(ToastHost), true);
  await act(async () => {
    pushToast("Timed", "note", SAID_MS);
    pushToast("Persistent", "fail");
  });
  await advance(2000);
  await render(null, true);
  expect(notifications()).toHaveLength(0);
  await advance(2000);
  await render(h(ToastHost), true);
  expect(notifications()).toHaveLength(2);
  await advance(999);
  expect(snapshot()).toHaveLength(2);
  await advance(1);
  expect(snapshot().map((toast) => toast.text)).toEqual(["Persistent"]);
  await advance(100);
  expect(notifications()).toHaveLength(1);
  await render(null, true);
  await act(async () => clearToasts());
  await render(h(ToastHost), true);
  expect(notifications()).toHaveLength(0);
});

it("keeps a caller's note when its page unmounts and still expires it at five seconds", async () => {
  await render(h("div", null, h(NamedCaller, { name: "Page" }), h(ToastHost)));
  await click(callerButton("Page note"));
  await advance(2000);
  await render(h("div", null, null, h(ToastHost)));
  expect(snapshot()).toHaveLength(1);
  expect(notifications()).toHaveLength(1);
  await advance(3000);
  expect(snapshot()).toHaveLength(0);
  await advance(100);
  expect(notifications()).toHaveLength(0);
});

it("reconciles a StrictMode startup caller replacement without duplicating visible notifications", async () => {
  function Startup() {
    const say = useOwnToast();
    useEffect(() => {
      say("Startup failed", "fail");
    }, [say]);
    return null;
  }
  await render(h("div", null, h(Startup), h(ToastHost)), true);
  await advance(100);
  expect(snapshot().map((toast) => toast.text)).toEqual(["Startup failed"]);
  expect(notifications()).toHaveLength(1);
});

it.each([false, true])(
  "focused toast Escape preserves inbox selection and a containing dialog (dialog=%s)",
  async (modal) => {
    const clearSelection = vi.fn();
    const closeDialog = vi.fn();
    function Selection() {
      useEscapeToClear(true, clearSelection);
      return null;
    }
    await render(
      h(
        "div",
        null,
        h(Selection),
        h(ToastHost),
        modal
          ? h(DialogShell, {
              label: "Toast integration",
              onClose: closeDialog,
              children: h("button", null, "Modal action"),
            })
          : null,
      ),
    );
    await act(async () => pushToast("Focused failure", "fail"));
    await advance(0);
    const close = closeButton("Focused failure");
    await act(async () => close.focus());
    expect(close.closest('[aria-hidden="true"], [inert]')).toBeNull();
    await key(close, "Escape");
    expect(snapshot()).toEqual([]);
    expect(clearSelection).not.toHaveBeenCalled();
    expect(closeDialog).not.toHaveBeenCalled();
    if (modal) expect(document.querySelector('[aria-label="Toast integration"]')).not.toBeNull();
  },
);

it("supports F6 entry, Shift+Tab focus return and Escape close through Base UI", async () => {
  await render(h("div", null, h("button", null, "Launcher"), h(ToastHost)));
  const launcher = callerButton("Launcher");
  await act(async () => launcher.focus());
  await act(async () => pushToast("Persistent", "fail"));
  await advance(0);
  await key(launcher, "F6");
  const viewport = document.querySelector<HTMLElement>('[role="region"]')!;
  expect(document.activeElement).toBe(viewport);
  await key(viewport, "Tab", true);
  expect(document.activeElement).toBe(launcher);
  await key(launcher, "F6");
  // jsdom has no native Tab default action: exercise the viewport's forward focus guard.
  await act(async () => (viewport.firstElementChild as HTMLElement).focus());
  await advance(0);
  expect(document.activeElement).toBe(notifications()[0]);
  await act(async () => closeButton("Persistent").focus());
  await key(closeButton("Persistent"), "Escape");
  expect(snapshot()).toEqual([]);
  await advance(100);
  expect(notifications()).toHaveLength(0);
});

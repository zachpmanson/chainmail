// @vitest-environment jsdom
import { act, createElement, useEffect, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import DialogShell from "../src/components/ui/DialogShell";
import AddEmailsModal from "../src/components/specs/AddEmailsModal";
import ProposalsModal from "../src/components/specs/ProposalsModal";
import ThreadPreview from "../src/components/thread/ThreadPreview";
import type { ChainHit, RefreshCandidate } from "../src/lib/api/api";
import { attach } from "../src/client/behaviour";

// Keep the real dialog, search form, result rows and proposal controls; don't load a mailbox.
vi.mock("../src/components/thread/ThreadPane", () => ({
  default: ({ onClose }: { onClose: () => void }) =>
    createElement("button", { onClick: onClose }, "Close preview"),
}));
vi.mock("../src/lib/inbox/mailActions", () => ({ useReadAction: () => ({}) }));
vi.mock("../src/lib/inbox/useAccountId", () => ({ useAccountId: () => undefined }));
const search = vi.hoisted(() => ({ data: {}, isFetching: false, isError: false, error: null }));
vi.mock("../src/lib/api/api", () => ({
  $api: { useQuery: () => search },
  searchQuery: (query: unknown) => query,
}));

const proposal: RefreshCandidate = {
  rootExtId: "mail:test",
  subject: "A thread",
  entries: 2,
  matched: 1,
  query: "recorded query",
};
const hit: ChainHit = {
  ...proposal,
  people: 1,
  attachments: 0,
  unread: 0,
  first: "2026-01-01T12:00:00Z",
  last: "2026-01-02T12:00:00Z",
  score: 1,
};
let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  search.data = { chains: [hit] };
  search.isFetching = false;
  search.isError = false;
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
function button(text: string) {
  const match = [...document.querySelectorAll("button")].find((b) => b.textContent === text);
  if (!match) throw new Error(`Missing button: ${text}`);
  return match;
}
function dialog(label: string) {
  return document.querySelector<HTMLElement>(`[role="dialog"][aria-label="${label}"]`);
}
function backdrop() {
  return [...document.querySelectorAll<HTMLElement>("[data-base-ui-portal] > [role=presentation]")]
    .filter((node) => !node.querySelector("[role=dialog]"))
    .at(-1)!;
}
async function launch(content: (close: () => void) => ReturnType<typeof createElement>) {
  function Harness() {
    const [open, setOpen] = useState(false);
    return createElement(
      "div",
      null,
      createElement("button", { onClick: () => setOpen(true) }, "Launch"),
      open ? content(() => setOpen(false)) : null,
    );
  }
  await act(async () => root.render(createElement(Harness)));
  const launcher = button("Launch");
  await click(launcher);
  return launcher;
}

it("traps focus in the shell, wraps both tab boundaries, and restores the original launcher", async () => {
  const launcher = await launch((onClose) =>
    createElement(
      DialogShell,
      { label: "Example", onClose, children: null },
      createElement("button", null, "First"),
      createElement("button", { onClick: onClose }, "Last"),
    ),
  );
  const popup = dialog("Example")!;
  expect(popup.getAttribute("aria-modal")).toBe("true");
  expect(document.activeElement).toBe(button("First"));
  expect(host.getAttribute("aria-hidden")).toBe("true");
  // jsdom has no native Tab navigation: focus the guards that browser Tab reaches.
  await act(async () => (popup.previousElementSibling as HTMLElement).focus());
  await settle();
  expect(document.activeElement).toBe(button("Last"));
  await act(async () => (popup.nextElementSibling as HTMLElement).focus());
  await settle();
  expect(document.activeElement).toBe(button("First"));
  await escape();
  expect(dialog("Example")).toBeNull();
  expect(document.activeElement).toBe(launcher);
  expect(host.hasAttribute("aria-hidden")).toBe(false);
});

it("dismisses standalone previews by backdrop, not by a press inside, and restores focus", async () => {
  const launcher = await launch((onClose) =>
    createElement(ThreadPreview, { thread: hit, onClose }),
  );
  await click(dialog("Thread preview")!);
  expect(dialog("Thread preview")).not.toBeNull();
  await click(backdrop());
  expect(dialog("Thread preview")).toBeNull();
  expect(document.activeElement).toBe(launcher);
});

it.each(["escape", "backdrop", "button"])(
  "keeps proposals usable after a nested preview closes by %s, restoring focus in order",
  async (method) => {
    const onAccept = vi.fn();
    const launcher = await launch((onClose) =>
      createElement(ProposalsModal, { proposals: [proposal], open: true, onClose, onAccept }),
    );
    const parent = dialog("Proposed chains")!;
    const previewLauncher = button("Preview");
    await click(previewLauncher);
    expect(dialog("Thread preview")?.hasAttribute("data-nested")).toBe(true);
    expect(parent.hasAttribute("data-nested-dialog-open")).toBe(true);
    expect(document.activeElement).toBe(button("Close preview"));
    if (method === "escape") await escape();
    else if (method === "backdrop") await click(backdrop());
    else await click(button("Close preview"));
    expect(dialog("Thread preview")).toBeNull();
    expect(dialog("Proposed chains")).toBe(parent);
    expect(document.activeElement).toBe(previewLauncher);
    await click(document.querySelector<HTMLInputElement>("input[type=checkbox]")!);
    await click(button("accept 1"));
    expect(onAccept).toHaveBeenCalledExactlyOnceWith([proposal.rootExtId]);
    await escape();
    expect(dialog("Proposed chains")).toBeNull();
    expect(document.activeElement).toBe(launcher);
  },
);

it("preserves proposal backdrop policy and blocks Escape, close and acceptance while refreshing", async () => {
  const onClose = vi.fn();
  const onAccept = vi.fn();
  const render = async (refreshing: boolean) => {
    await act(async () =>
      root.render(
        createElement(ProposalsModal, {
          proposals: [proposal],
          open: true,
          refreshing,
          onClose,
          onAccept,
        }),
      ),
    );
    await settle();
  };
  await render(true);
  await click(backdrop());
  await escape();
  await click(button("close"));
  await click(button("accept all 1"));
  expect(onClose).not.toHaveBeenCalled();
  expect(onAccept).not.toHaveBeenCalled();
  expect(button("accepting…").disabled).toBe(true);
  await click(button("Preview"));
  await escape();
  expect(dialog("Thread preview")).toBeNull();
  expect(dialog("Proposed chains")).not.toBeNull();
  expect(onClose).not.toHaveBeenCalled();
  await render(false);
  await click(backdrop());
  expect(onClose).not.toHaveBeenCalled();
  await click(button("accept all 1"));
  expect(onAccept).toHaveBeenCalledExactlyOnceWith([proposal.rootExtId]);
  await escape();
  expect(onClose).toHaveBeenCalledOnce();
});

function LightboxBody() {
  useEffect(() => attach(document), []);
  return createElement(
    "div",
    { className: "bd" },
    createElement("img", {
      src: "synthetic.png",
      alt: "Synthetic picture",
      width: 200,
      height: 200,
    }),
  );
}

it.each(["escape", "button", "backdrop"])(
  "keeps a dialog open when its real image lightbox closes by %s",
  async (method) => {
    const launcher = await launch((onClose) =>
      createElement(
        DialogShell,
        { label: "Reader", onClose, children: null },
        createElement(LightboxBody),
      ),
    );
    const reader = dialog("Reader")!;
    const image = reader.querySelector("img")!;
    // Reopening uses the same lightbox host; both opens must remain within the modal.
    for (let i = 0; i < 2; i++) {
      await click(image);
      const lightbox = document.querySelector<HTMLElement>(".pop:not([hidden])")!;
      expect(lightbox).not.toBeNull();
      expect(lightbox.contains(document.activeElement)).toBe(true);
      expect(lightbox.closest('[aria-hidden="true"], [inert]')).toBeNull();
      if (method === "escape") await escape();
      else if (method === "button") await click(lightbox.querySelector("button")!);
      else await click(lightbox);
      expect(lightbox.hidden).toBe(true);
      expect(dialog("Reader")).toBe(reader);
      expect(document.activeElement).toBe(image);
    }
    await escape();
    expect(dialog("Reader")).toBeNull();
    expect(document.activeElement).toBe(launcher);
  },
);

it("keeps Add email search, selection and recorded-query semantics with a nested preview", async () => {
  const onAdd = vi.fn();
  const launcher = await launch((onClose) => createElement(AddEmailsModal, { onClose, onAdd }));
  const input = document.querySelector<HTMLInputElement>("input[aria-label='Search query']")!;
  expect(document.activeElement).toBe(input);
  expect(button("Search").disabled).toBe(true);
  const setValue = async (value: string) => {
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input, value);
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
  };
  await setValue("  actual query  ");
  await click(button("Search"));
  await setValue("unsent query");
  await click(document.querySelector<HTMLInputElement>("input[type=checkbox]")!);
  await click(document.querySelector<HTMLElement>("button[aria-label='A thread']")!);
  await escape();
  expect(dialog("Thread preview")).toBeNull();
  expect(dialog("Add another email")).not.toBeNull();
  expect(document.activeElement?.getAttribute("aria-label")).toBe("A thread");
  await click(button("add 1 to page"));
  expect(onAdd).toHaveBeenCalledExactlyOnceWith([hit.rootExtId], "actual query");
  await click(backdrop());
  expect(dialog("Add another email")).toBeNull();
  expect(document.activeElement).toBe(launcher);
});

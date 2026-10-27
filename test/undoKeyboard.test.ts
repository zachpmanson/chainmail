import { createElement } from "react";
import { act, create } from "react-test-renderer";
import { afterEach, expect, it, vi } from "vitest";
import NavUndo from "../src/components/navigation/NavUndo";
import { appUndo } from "../src/lib/ui/undo";
import { clearToasts } from "../src/lib/ui/toasts";

afterEach(() => {
  vi.unstubAllGlobals();
  clearToasts();
});

it("only prevents a matching application undo, preserves editing, and removes its listener", async () => {
  let handler: ((event: KeyboardEvent) => void) | undefined;
  const remove = vi.fn();
  vi.stubGlobal("document", {
    addEventListener: (_: string, fn: (event: KeyboardEvent) => void) => {
      handler = fn;
    },
    removeEventListener: remove,
  });
  const reversed = vi.fn();
  await appUndo.perform(() => appUndo.remember({ label: "archive", undo: reversed }));
  let tree!: ReturnType<typeof create>;
  await act(async () => {
    tree = create(createElement(NavUndo));
  });
  const event = (path: EventTarget[] = []) => ({
    key: "z",
    ctrlKey: true,
    metaKey: false,
    altKey: false,
    shiftKey: false,
    repeat: false,
    isComposing: false,
    defaultPrevented: false,
    composedPath: () => path,
    preventDefault: vi.fn(),
  });
  const editing = event([{ matches: () => true } as unknown as HTMLElement]);
  await act(async () => {
    handler!(editing as unknown as KeyboardEvent);
  });
  expect(editing.preventDefault).not.toHaveBeenCalled();
  expect(reversed).not.toHaveBeenCalled();
  const application = event();
  await act(async () => {
    handler!(application as unknown as KeyboardEvent);
    await appUndo.perform(() => undefined);
  });
  expect(application.preventDefault).toHaveBeenCalledOnce();
  expect(reversed).toHaveBeenCalledOnce();
  const empty = event();
  await act(async () => {
    handler!(empty as unknown as KeyboardEvent);
  });
  expect(empty.preventDefault).not.toHaveBeenCalled();
  act(() => tree.unmount());
  expect(remove).toHaveBeenCalledWith("keydown", handler);
});

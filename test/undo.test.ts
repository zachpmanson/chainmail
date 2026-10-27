import { describe, expect, it } from "vitest";
import { UndoStack, isUndoKey } from "../src/lib/ui/undo";

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((yes) => {
    resolve = yes;
  });
  return { promise, resolve };
}

describe("undo history", () => {
  it("is LIFO, bounded, and empty-safe", async () => {
    const stack = new UndoStack(),
      seen: number[] = [];
    expect(await stack.undo()).toBeUndefined();
    for (let n = 0; n < 105; n++)
      await stack.perform(() => {
        stack.remember({
          label: String(n),
          undo: () => {
            seen.push(n);
          },
        });
      });
    for (let n = 0; n < 101; n++) await stack.undo();
    expect(seen).toEqual(Array.from({ length: 100 }, (_, i) => 104 - i));
  });
  it("orders pending forward, undo, and later forward across async work", async () => {
    const stack = new UndoStack(),
      gate = deferred(),
      seen: string[] = [];
    const first = stack.perform(async () => {
      seen.push("start A");
      await gate.promise;
      seen.push("finish A");
      stack.remember({
        label: "A",
        undo: () => {
          seen.push("undo A");
        },
      });
    });
    const undo = stack.undo();
    const later = stack.perform(() => {
      seen.push("B");
      stack.remember({
        label: "B",
        undo: () => {
          seen.push("undo B");
        },
      });
    });
    await Promise.resolve();
    expect(seen).toEqual(["start A"]);
    gate.resolve();
    await Promise.all([first, undo, later]);
    expect(seen).toEqual(["start A", "finish A", "undo A", "B"]);
    expect(stack.getSnapshot()).toEqual({ label: "B", pending: false });
  });
  it("retains failed undo for retry and does not poison the queue", async () => {
    const stack = new UndoStack();
    let fail = true;
    await stack.perform(() =>
      stack.remember({
        label: "A",
        undo: () => {
          if (fail) throw new Error("offline");
        },
      }),
    );
    await expect(stack.undo()).rejects.toThrow("offline");
    expect(stack.getSnapshot().label).toBe("A");
    fail = false;
    expect(await stack.undo()).toBe("A");
    await expect(
      stack.perform(() => {
        throw new Error("forward failed");
      }),
    ).rejects.toThrow();
    expect(await stack.undo()).toBeUndefined();
  });
});

const key = {
  key: "z",
  ctrlKey: true,
  metaKey: false,
  altKey: false,
  shiftKey: false,
  repeat: false,
  isComposing: false,
  defaultPrevented: false,
};
const element = (tag: string, attrs: Record<string, string> = {}, editable = false) =>
  ({
    matches: (selector: string) => selector.split(", ").includes(tag),
    hasAttribute: (name: string) => name in attrs,
    getAttribute: (name: string) => attrs[name] ?? null,
    isContentEditable: editable,
  }) as unknown as HTMLElement;

describe("undo shortcut", () => {
  it("supports Ctrl+Z and Command+Z, not redo/Alt/held/composing/handled events", () => {
    expect(isUndoKey(key, [])).toBe(true);
    expect(isUndoKey({ ...key, key: "Z", ctrlKey: false, metaKey: true }, [])).toBe(true);
    for (const patch of [
      { shiftKey: true },
      { altKey: true },
      { repeat: true },
      { isComposing: true },
      { defaultPrevented: true },
      { ctrlKey: false },
      { metaKey: true },
      { key: "y" },
    ])
      expect(isUndoKey({ ...key, ...patch }, [])).toBe(false);
  });
  it("leaves native editing undo alone, including editable ancestors and shadow paths", () => {
    for (const editable of [
      element("input"),
      element("textarea"),
      element("select"),
      element("div", {}, true),
      element("div", { contenteditable: "" }),
      element("div", { contenteditable: "plaintext-only" }),
      element("span", { role: "textbox" }),
    ])
      expect(isUndoKey(key, [element("span"), editable])).toBe(false);
    expect(
      isUndoKey(key, [
        element("button"),
        element("div", { contenteditable: "false" }),
        {} as Document,
      ]),
    ).toBe(true);
  });
});

// @vitest-environment jsdom
import { act, createElement as h, useState } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import TextArea from "../src/components/ui/TextArea";
import ComposerFields from "../src/components/compose/ComposerFields";
import { emptyDraft } from "../src/components/compose/Draft";
import { domHarness } from "./domHarness";

const render = domHarness();
let css: HTMLStyleElement;
let width: number;
let reads: number;
let frame: FrameRequestCallback | undefined;
const flushFrame = () => {
  const callback = frame;
  frame = undefined;
  callback?.(0);
};
let observers: { notify: () => void; disconnect: ReturnType<typeof vi.fn> }[];
beforeEach(() => {
  css = document.createElement("style");
  css.textContent = "textarea { box-sizing: border-box; padding: 8px; border: 1px solid; }";
  document.head.append(css);
  width = 200;
  reads = 0;
  observers = [];
  frame = undefined;
  vi.stubGlobal(
    "requestAnimationFrame",
    vi.fn((callback: FrameRequestCallback) => {
      frame = callback;
      return 1;
    }),
  );
  vi.stubGlobal(
    "cancelAnimationFrame",
    vi.fn(() => {
      frame = undefined;
    }),
  );
  vi.stubGlobal(
    "ResizeObserver",
    class {
      disconnect = vi.fn();
      constructor(notify: () => void) {
        observers.push({ notify, disconnect: this.disconnect });
      }
      observe() {}
    },
  );
  vi.spyOn(HTMLTextAreaElement.prototype, "clientWidth", "get").mockImplementation(() => width);
  // Native rows remain the floor with height:auto; scrollHeight includes padding, not borders.
  vi.spyOn(HTMLTextAreaElement.prototype, "scrollHeight", "get").mockImplementation(function (
    this: HTMLTextAreaElement,
  ) {
    reads++;
    const lines = this.value
      .split("\n")
      .reduce((n, line) => n + Math.max(1, Math.ceil(line.length / (width / 10))), 0);
    const content = Math.max(this.rows, lines) * 20 + 16;
    const assigned = parseFloat(this.style.height) || 0;
    return Math.max(content, assigned - (this.style.boxSizing === "border-box" ? 2 : -16));
  });
});
afterEach(() => css.remove());
const textarea = () => document.querySelector("textarea")!;
const style = { padding: "8px", border: "1px solid", boxSizing: "border-box" as const };
async function input(value: string, inputType = "insertText") {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(
      textarea(),
      value,
    );
    textarea().dispatchEvent(new InputEvent("input", { bubbles: true, inputType }));
  });
}

it.each(["border-box", "content-box"] as const)(
  "grows and shrinks to four rows without padding accumulation (%s)",
  async (boxSizing) => {
    const props = { autoGrow: true, rows: 4, style: { ...style, boxSizing }, onChange: vi.fn() };
    const minimum = boxSizing === "border-box" ? 98 : 80;
    await render(h(TextArea, { ...props, value: "" }));
    expect(textarea().style.height).toBe(`${minimum}px`);
    await render(h(TextArea, { ...props, value: "a\n".repeat(9) }));
    expect(textarea().style.height).toBe(`${minimum + 120}px`);
    await render(h(TextArea, { ...props, value: "short" }));
    expect(textarea().style.height).toBe(`${minimum}px`);
    expect(textarea().className).toContain("overflow-y-hidden");
    expect(textarea().className).toContain("resize-none");
  },
);

it("refits wrapped hydrated content on width changes, ignores height-only notifications, and cleans up", async () => {
  await render(
    h(TextArea, { autoGrow: true, rows: 4, style, value: "x".repeat(120), onChange: vi.fn() }),
  );
  expect(textarea().style.height).toBe("138px");
  const observer = observers[0]!;
  const before = reads;
  await act(async () => observer.notify());
  expect(reads).toBe(before);
  width = 100;
  await act(async () => observer.notify());
  expect(reads).toBe(before);
  await act(async () => flushFrame());
  expect(textarea().style.height).toBe("258px");
  const after = reads;
  await act(async () => observer.notify());
  expect(reads).toBe(after);
  width = 400;
  await act(async () => observer.notify());
  await act(async () => flushFrame());
  expect(textarea().style.height).toBe("98px");
  width = 100;
  await act(async () => observer.notify());
  expect(frame).toBeDefined();
  await render(h("div"));
  expect(observer.disconnect).toHaveBeenCalledOnce();
  expect(frame).toBeUndefined();
});

it("leaves ordinary textareas and their manual sizing untouched", async () => {
  await render(
    h(TextArea, { rows: 4, style: { height: "150px" }, defaultValue: "long\n".repeat(30) }),
  );
  expect(textarea().style.height).toBe("150px");
  expect(textarea().className).toContain("resize-y");
  expect(textarea().className).not.toContain("overflow-y-hidden");
  expect(observers).toHaveLength(0);
  expect(reads).toBe(0);
});

it.each(["compose", "reply"] as const)(
  "opts in %s bodies and preserves draft patches, caret, hydration, paste, and disabled state",
  async (kind) => {
    const onDraft = vi.fn();
    function Harness({ body = "", busy = false }: { body?: string; busy?: boolean }) {
      const [draft, setDraft] = useState({ ...emptyDraft, body });
      return h(ComposerFields, {
        draft,
        busy,
        from: "me@example.test",
        suggestions: [],
        mode:
          kind === "compose"
            ? { kind }
            : { kind, editingRecipients: false, onEditRecipients: vi.fn(), target: null },
        onDraft: (patch) => {
          onDraft(patch);
          setDraft((old) => ({ ...old, ...patch }));
        },
      });
    }
    await render(h(Harness));
    expect(textarea().rows).toBe(4);
    expect(textarea().style.height).toBe("98px");
    expect(textarea().getAttribute("aria-label")).toBe(
      kind === "compose" ? "Message" : "Your reply",
    );
    textarea().focus();
    await input("hello");
    expect(onDraft).toHaveBeenLastCalledWith({ body: "hello" });
    const pasted = "pasted\n".repeat(12);
    await input(pasted, "insertFromPaste");
    expect(onDraft).toHaveBeenLastCalledWith({ body: pasted });
    expect(textarea().style.height).toBe("278px");
    expect(document.activeElement).toBe(textarea());
    expect(textarea().selectionStart).toBe(pasted.length);
    await input("");
    expect(textarea().style.height).toBe("98px");
    await render(h(Harness, { key: "saved", body: pasted, busy: true }));
    expect(textarea().style.height).toBe("278px");
    expect(textarea().disabled).toBe(true);
    expect(textarea().value).toBe(pasted);
  },
);

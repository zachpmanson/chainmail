import { act, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, vi } from "vitest";

export function domHarness() {
  let root: Root;
  let host: HTMLDivElement;
  beforeEach(() => {
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
  });
  afterEach(async () => {
    await act(async () => root.unmount());
    host.remove();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });
  return async (element: ReactNode) => {
    await act(async () => root.render(element));
    await settle();
    return host;
  };
}

export async function settle(ms = 30) {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, ms));
  });
}
export async function focus(element: HTMLElement) {
  await act(async () => element.focus());
  await settle();
}
export async function type(input: HTMLInputElement, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input, value);
    input.dispatchEvent(new InputEvent("input", { bubbles: true, inputType: "insertText" }));
  });
  await settle();
}
export async function key(element: HTMLElement, key: string) {
  const event = new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true });
  await act(async () => element.dispatchEvent(event));
  await settle();
  return event;
}
export async function pick(element: HTMLElement) {
  await act(async () => {
    const pointerDown = new MouseEvent("pointerdown", {
      bubbles: true,
      cancelable: true,
      button: 0,
    });
    Object.defineProperties(pointerDown, {
      isPrimary: { value: true },
      pointerType: { value: "mouse" },
    });
    element.dispatchEvent(pointerDown);
    const down = new MouseEvent("mousedown", { bubbles: true, cancelable: true, button: 0 });
    if (element.dispatchEvent(down)) element.focus();
    element.dispatchEvent(new MouseEvent("pointerup", { bubbles: true, button: 0 }));
    element.dispatchEvent(new MouseEvent("mouseup", { bubbles: true, button: 0 }));
    element.click();
  });
  await settle();
}

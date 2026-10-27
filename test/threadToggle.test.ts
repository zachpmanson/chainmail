import { Bars3BottomRightIcon, Bars3Icon } from "@heroicons/react/24/outline";
import { createElement } from "react";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import ThreadMessages from "../src/components/thread/ThreadMessages";
import ThreadPane from "../src/components/thread/ThreadPane";
import { usePrefs } from "../src/lib/prefs/usePrefs";
import { appUndo } from "../src/lib/ui/undo";

vi.mock("../src/lib/inbox/useAccountId", () => ({ useAccountId: () => undefined }));
vi.mock("../src/lib/inbox/mailActions", () => ({
  useMailAction: () => ({ isPending: false, mutate: vi.fn() }),
  useReadAction: () => ({ isPending: false, mutate: vi.fn() }),
}));
vi.mock("../src/lib/ui/useOwnToast", () => ({ default: () => vi.fn() }));
vi.mock("../src/components/inbox/MoveFolder", () => ({ default: () => null }));
vi.mock("../src/components/thread/ThreadMessages", () => ({ default: () => null }));

let rendered: ReactTestRenderer | undefined;
beforeEach(() => {
  vi.stubGlobal("document", {});
  vi.stubGlobal("window", {});
});
afterEach(() => {
  act(() => rendered?.unmount());
  rendered = undefined;
  usePrefs.setState({ tree: false });
  vi.unstubAllGlobals();
});

it.each([false, true])("offers the destination layout icon when tree is %s", async (tree) => {
  usePrefs.setState({ tree });
  act(() => {
    rendered = create(
      createElement(ThreadPane, {
        thread: { rootExtId: "root", subject: "Hello" },
        label: "Email",
        backLabel: "Back",
        empty: "Choose a thread",
        onClose: vi.fn(),
      }),
    );
  });
  const label = (on: boolean) =>
    on
      ? "Reply tree: each answer under the message it answers — click for the order they were sent"
      : "Reply tree: the order they were sent — click to draw each answer under the message it answers";
  const check = (on: boolean) => {
    const button = rendered!.root
      .findAllByType("button")
      .find((node) => node.props["aria-label"]?.startsWith("Reply tree:"))!;
    expect(button.props["aria-pressed"]).toBe(on);
    expect(button.props["aria-label"]).toBe(label(on));
    expect(button.props.title).toBe(label(on));
    const icon = button.findByType(on ? Bars3Icon : Bars3BottomRightIcon);
    expect(icon.props["aria-hidden"]).toBe("true");
    expect(button.findAllByType(on ? Bars3BottomRightIcon : Bars3Icon)).toHaveLength(0);
    expect(rendered!.root.findByType(ThreadMessages).props.tree).toBe(on);
    expect(usePrefs.getState().tree).toBe(on);
    return button;
  };
  const button = check(tree);
  await act(async () => {
    button.props.onClick();
    await appUndo.perform(() => undefined);
  });
  const toggled = check(!tree);
  await act(async () => {
    toggled.props.onClick();
    await appUndo.perform(() => undefined);
  });
  check(tree);
});

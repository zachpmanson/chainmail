import { createElement } from "react";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import ActionBar from "../src/components/inbox/ActionBar";
import ThreadPane from "../src/components/thread/ThreadPane";

const mail = vi.hoisted(() => ({ isPending: false, mutate: vi.fn() }));
vi.mock("../src/lib/inbox/mailActions", () => ({
  useMailAction: () => mail,
  useReadAction: () => ({ isPending: false, mutate: vi.fn() }),
}));
vi.mock("../src/lib/inbox/useAccountId", () => ({ useAccountId: () => "acct" }));
vi.mock("../src/lib/inbox/build", () => ({
  useBuildPage: () => ({ build: { isPending: false }, start: vi.fn() }),
}));
vi.mock("../src/lib/api/api", () => ({
  $api: {
    useQuery: (_method: string, path: string) => ({
      data:
        path === "/auth/status"
          ? { accounts: [{ id: "acct", signedIn: true }] }
          : path === "/v1/labels"
            ? { labels: [{ name: "INBOX" }, { name: "Saved" }] }
            : { me: [] },
      isPending: false,
      isError: false,
    }),
  },
}));
vi.mock("../src/components/thread/ThreadMessages", () => ({ default: () => null }));

let root: ReactTestRenderer;
const order = ["Archive", "Move to a folder", "Delete"];
beforeEach(() => {
  mail.isPending = false;
  mail.mutate.mockClear();
});
afterEach(() => act(() => root?.unmount()));

function render(pane: boolean, populated = true) {
  act(() => {
    root = create(
      pane
        ? createElement(ThreadPane, {
            thread: populated ? { rootExtId: "root", subject: "Hello", unread: 1 } : null,
            label: "Email",
            backLabel: "Back",
            empty: "Choose a thread",
            moveDefault: "INBOX",
            onClose: vi.fn(),
          })
        : createElement(ActionBar, {
            chosen: populated ? ["root", "second"] : [],
            moveDefault: "INBOX",
            onDone: vi.fn(),
          }),
    );
  });
  return root.root.findAll(
    (node) =>
      (node.type === "button" || node.type === "select") &&
      order.includes(node.props["aria-label"]),
  );
}

it.each([false, true])("orders shared actions consistently (pane: %s)", (pane) => {
  const controls = render(pane);
  expect(controls.map((node) => node.props["aria-label"])).toEqual(order);
  expect(
    controls.map((node) => (node.type === "select" ? node.parent?.props.title : node.props.title)),
  ).toEqual(order);
  expect(controls.map((node) => node.type)).toEqual(["button", "select", "button"]);
  expect(controls[1]!.props.value).toBe("INBOX");
  const chains = pane ? ["root"] : ["root", "second"];
  act(() => controls[0]!.props.onClick());
  act(() => controls[1]!.props.onChange({ target: { value: "Saved" } }));
  act(() => controls[2]!.props.onClick());
  expect(mail.mutate.mock.calls).toEqual([
    [{ body: { chains, action: "archive", accountId: "acct" } }],
    [{ body: { chains, action: "move", labels: ["Saved"], accountId: "acct" } }],
    [{ body: { chains, action: "trash", accountId: "acct" } }],
  ]);
});

it.each([false, true])("keeps all shared actions disabled while busy (pane: %s)", (pane) => {
  mail.isPending = true;
  expect(render(pane).map((node) => node.props.disabled)).toEqual([true, true, true]);
});

it.each([false, true])("omits actions without a selection or thread (pane: %s)", (pane) => {
  expect(render(pane, false)).toEqual([]);
});

import { createElement } from "react";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import FolderPicker from "../src/components/inbox/FolderPicker";

const state = vi.hoisted(() => ({ count: 2 }));
vi.mock("../src/lib/api/api", () => ({
  $api: {
    useQuery: (_method: string, path: string) => ({
      data:
        path === "/auth/status"
          ? { accounts: [{ id: "personal", email: "reader@example.test", signedIn: true }] }
          : {
              labels: Array.from({ length: state.count }, (_, i) => ({
                name: `Folder ${i + 1}`,
                messages: i + 1,
              })),
            },
    }),
  },
}));

let rendered: ReactTestRenderer;
let top: number;
let listeners: Map<string, () => void>;
const remove = vi.fn();

beforeEach(() => {
  top = 120;
  listeners = new Map();
  remove.mockReset();
  vi.stubGlobal("window", {
    addEventListener: (event: string, listener: () => void) => listeners.set(event, listener),
    removeEventListener: remove,
  });
  vi.stubGlobal("document", { addEventListener: vi.fn(), removeEventListener: vi.fn() });
});
afterEach(() => {
  act(() => rendered?.unmount());
  vi.unstubAllGlobals();
});

function open(mode: "inbox" | "default" = "inbox") {
  act(() => {
    rendered = create(createElement(FolderPicker, { current: "", onPick: vi.fn(), mode }), {
      createNodeMock: () => ({ getBoundingClientRect: () => ({ top }) }),
    });
  });
  act(() => rendered.root.findByProps({ "aria-haspopup": "menu" }).props.onClick());
}

it.each([2, 80])("lets all %i folders size naturally within the viewport cap", (count) => {
  state.count = count;
  open();
  const menu = rendered.root.findByProps({ role: "menu" });
  expect(
    menu.findAll((node) => node.type === "button" && node.props.role === "menuitem"),
  ).toHaveLength(count + 2);
  expect(menu.props.style).toEqual({ maxHeight: "max(0px, calc(100dvh - 120px - 2rem))" });
  const classes = menu.props.className.split(/\s+/);
  expect(classes).toContain("overflow-y-auto");
  expect(classes).toContain("box-border"); // Padding and border must fit inside the cap too.
  expect(classes.some((c: string) => /^(?:max-h-|h-)/.test(c))).toBe(false);
});

it.each(["inbox", "default"] as const)("remeasures the %s picker on resize and scroll", (mode) => {
  open(mode);
  for (const event of ["resize", "scroll"]) {
    top += 40;
    act(() => listeners.get(event)!());
    expect(rendered.root.findByProps({ role: "menu" }).props.style.maxHeight).toBe(
      `max(0px, calc(100dvh - ${top}px - 2rem))`,
    );
  }
  act(() => rendered.root.findByProps({ "aria-haspopup": "menu" }).props.onClick());
  expect(rendered.root.findAllByProps({ role: "menu" })).toHaveLength(0);
  expect(remove).toHaveBeenCalledWith("resize", listeners.get("resize"));
  expect(remove).toHaveBeenCalledWith("scroll", listeners.get("scroll"), true);
});

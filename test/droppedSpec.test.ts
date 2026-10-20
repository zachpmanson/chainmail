import { createElement, type ComponentType, type ReactNode } from "react";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { router } from "../src/router";

const state = vi.hoisted(() => ({ pathname: "/settings" }));
vi.mock("@tanstack/react-router", async (original) => ({
  ...(await original<typeof import("@tanstack/react-router")>()),
  useRouterState: ({ select }: { select: (value: unknown) => unknown }) =>
    select({ location: { pathname: state.pathname, search: {} } }),
  Link: () => null,
  Outlet: () => createElement("main", { "data-page": state.pathname }),
}));
vi.mock("../src/components/compose/ComposeProvider", () => ({
  default: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("../src/components/specs/Rendered", () => ({
  default: ({ onBack }: { onBack?: () => void }) =>
    createElement("button", { onClick: onBack, "data-dropped": true }, "Return"),
}));
vi.mock("../src/components/navigation/DeployStamp", () => ({ default: () => null }));
vi.mock("../src/components/navigation/NavReading", () => ({ default: () => null }));
vi.mock("../src/components/navigation/NavRefresh", () => ({ default: () => null }));
vi.mock("../src/components/navigation/AutoRefresh", () => ({ default: () => null }));
vi.mock("../src/components/navigation/NavSearch", () => ({ default: () => null }));
vi.mock("../src/components/ui/ToastHost", () => ({ default: () => null }));

let rendered: ReactTestRenderer | undefined;
const events = new Map<string, (event: unknown) => Promise<void>>();
const Root = router.options.routeTree!.options.component as ComponentType;
beforeEach(() => {
  state.pathname = "/settings";
  events.clear();
  vi.stubGlobal("addEventListener", (name: string, handler: (event: unknown) => Promise<void>) =>
    events.set(name, handler),
  );
  vi.stubGlobal("removeEventListener", (name: string) => events.delete(name));
});
afterEach(() => {
  act(() => {
    rendered?.unmount();
  });
  rendered = undefined;
  vi.unstubAllGlobals();
});

it.each(["/settings", "/ops", "/"])(
  "always returns from a dropped spec to %s",
  async (pathname) => {
    state.pathname = pathname;
    await act(async () => {
      rendered = create(createElement(Root));
    });
    await act(async () => {
      await events.get("drop")!({
        preventDefault() {},
        dataTransfer: {
          files: [
            {
              text: async () =>
                JSON.stringify({
                  title: "Synthetic",
                  messages: [{ date: "2026-01-01", body: "Synthetic" }],
                }),
            },
          ],
        },
      });
    });
    const back = rendered!.root.findByProps({ "data-dropped": true });
    expect(back.props.onClick).toBeTypeOf("function");
    act(() => {
      back.props.onClick();
    });
    expect(rendered!.root.findByProps({ "data-page": pathname })).toBeDefined();
    expect(rendered!.root.findAllByProps({ "data-dropped": true })).toHaveLength(0);
  },
);

it("keeps an exit when the underlying route changes during the dropped view", async () => {
  await act(async () => {
    rendered = create(createElement(Root));
  });
  await act(async () => {
    await events.get("drop")!({
      preventDefault() {},
      dataTransfer: { files: [{ text: async () => '{"title":"Synthetic","messages":[]}' }] },
    });
  });
  state.pathname = "/ops";
  act(() => {
    rendered!.update(createElement(Root));
  });
  act(() => {
    rendered!.root.findByProps({ "data-dropped": true }).props.onClick();
  });
  expect(rendered!.root.findByProps({ "data-page": "/ops" })).toBeDefined();
});

// @vitest-environment jsdom
import { act, createElement, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import NavPerson from "../src/components/navigation/NavPerson";
import NavSearch from "../src/components/navigation/NavSearch";
import type { PersonSummary } from "../src/lib/api/api";

const people: PersonSummary[] = [
  { displayName: "Alice", identities: ["slack_uid:U_ALICE", "email:alice@example.test"] },
  { displayName: "Bob", identities: ["slack_uid:U_BOB"] },
  { displayName: "Carol", identities: [] },
  ...Array.from({ length: 9 }, (_, i) => ({
    displayName: `Extra ${i}`,
    identities: [`email:extra${i}@example.test`],
  })),
].map((person, personId) => ({ ...person, personId, sent: 1, received: 1 }));
vi.mock("../src/lib/api/api", () => ({
  $api: {
    useQuery: (_method: string, path: string) => ({
      data: path === "/v1/people" ? { people } : { accounts: [] },
    }),
  },
}));

let root: Root;
let host: HTMLDivElement;
beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
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
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
}
async function focus(element: HTMLElement) {
  await act(async () => element.focus());
  await settle();
}
async function type(input: HTMLInputElement, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input, value);
    input.dispatchEvent(new InputEvent("input", { bubbles: true, inputType: "insertText" }));
  });
  await settle();
}
async function key(element: HTMLElement, key: string) {
  const event = new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true });
  await act(async () => element.dispatchEvent(event));
  await settle();
  return event;
}
async function pick(element: HTMLElement) {
  // Like a browser, only move focus if the down event permits it. Options must retain input focus.
  await act(async () => {
    // jsdom lacks PointerEvent; include the fields Base UI uses to distinguish a click from drag selection.
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
function input() {
  return document.querySelector<HTMLInputElement>("[role=combobox]")!;
}
function options() {
  return [...document.querySelectorAll<HTMLElement>("[role=option]")];
}
function option(name: string) {
  const row = options().find((row) => row.textContent?.startsWith(name));
  if (!row) throw new Error(`Missing option ${name}`);
  return row;
}
async function standalone() {
  const onEdit = vi.fn();
  const onCommit = vi.fn();
  const onSubmit = vi.fn();
  function Harness() {
    const [value, setValue] = useState("");
    return createElement(
      "form",
      {
        onSubmit: (event) => {
          event.preventDefault();
          onSubmit();
        },
      },
      createElement(NavPerson, {
        people,
        value,
        onEdit: (text) => {
          setValue(text);
          onEdit(text);
        },
        onCommit: (text) => {
          setValue(text);
          onCommit(text);
        },
      }),
      createElement("button", { type: "submit" }, "Submit"),
    );
  }
  await act(async () => root.render(createElement(Harness)));
  await focus(input());
  return { onEdit, onCommit, onSubmit };
}

it("portals capped suggestions, filters aliases without identity prefixes and preserves free text", async () => {
  const { onEdit, onCommit, onSubmit } = await standalone();
  expect(options()).toHaveLength(9); // Anyone plus eight people.
  expect(option("Anyone")).toBeDefined();
  expect(host.contains(document.querySelector("[role=listbox]"))).toBe(false);
  expect(document.getElementById(input().getAttribute("aria-controls")!)).toBeTruthy();
  await type(input(), "mai");
  expect(options()).toHaveLength(0);
  await type(input(), "u_alice");
  expect(options()).toHaveLength(1);
  expect(option("Alice").textContent).toContain("alice@example.test");
  await type(input(), "  unmatched free text  ");
  expect(input().value).toBe("  unmatched free text  ");
  expect(onEdit).toHaveBeenLastCalledWith("  unmatched free text  ");
  expect(onCommit).not.toHaveBeenCalled();
  expect((await key(input(), "Enter")).defaultPrevented).toBe(false);
  expect(onSubmit).not.toHaveBeenCalled();
});

it("accepts first and arrow-highlighted suggestions with Enter, using email/slack/name query values", async () => {
  const { onCommit, onSubmit } = await standalone();
  for (const [name, value] of [
    ["Alice", "alice@example.test"],
    ["Bob", "U_BOB"],
    ["Carol", "Carol"],
  ]) {
    await type(input(), name!);
    expect(
      document.getElementById(input().getAttribute("aria-activedescendant")!)?.textContent,
    ).toContain(name);
    expect((await key(input(), "Enter")).defaultPrevented).toBe(true);
    expect(onCommit).toHaveBeenLastCalledWith(value);
    expect(input().value).toBe(value);
    expect(input().getAttribute("aria-expanded")).toBe("false");
  }
  await type(input(), "");
  await key(input(), "ArrowDown");
  await key(input(), "ArrowDown");
  await key(input(), "ArrowUp");
  await key(input(), "Enter");
  expect(onCommit).toHaveBeenLastCalledWith("alice@example.test");
  expect(onSubmit).not.toHaveBeenCalled();
});

it("accepts pointer choices and Anyone without blurring the input or submitting its parent form", async () => {
  const { onCommit, onSubmit } = await standalone();
  await pick(option("Bob"));
  expect(onCommit).toHaveBeenCalledExactlyOnceWith("U_BOB");
  expect(document.activeElement).toBe(input());
  await type(input(), "");
  await pick(option("Anyone"));
  expect(onCommit).toHaveBeenLastCalledWith("");
  expect(input().value).toBe("");
  expect(document.activeElement).toBe(input());
  expect(onSubmit).not.toHaveBeenCalled();
});

async function search(url: string) {
  const rootRoute = createRootRoute({ component: NavSearch });
  const home = createRoute({ getParentRoute: () => rootRoute, path: "/" });
  const router = createRouter({
    routeTree: rootRoute.addChildren([home]),
    history: createMemoryHistory({ initialEntries: [url] }),
  });
  await act(async () => {
    await router.load();
    root.render(createElement(RouterProvider, { router }));
  });
  await settle();
  await focus(document.querySelector<HTMLInputElement>("[aria-label='Search the corpus']")!);
  await focus(input());
  return router;
}

it("NavSearch commits a portalled pointer selection once, with the selected person rather than stale blur text", async () => {
  const router = await search("/?q=original&open=thread&folder=INBOX");
  await type(input(), "ali");
  expect(router.state.location.search).not.toHaveProperty("person");
  const navigate = vi.spyOn(router, "navigate");
  await pick(option("Alice"));
  expect(router.state.location.search).toEqual({ q: "original", person: "alice@example.test" });
  expect(navigate).toHaveBeenCalledTimes(1);
  expect(document.activeElement).toBe(input());
  const outside = document.createElement("button");
  document.body.append(outside);
  await focus(outside);
  expect(navigate).toHaveBeenCalledTimes(1);
  expect(input().value).toBe("alice@example.test");
  outside.remove();
});

it("NavSearch keeps intra-form edits uncommitted, commits trimmed free text on outside blur and preserves unchanged URL scope", async () => {
  const router = await search("/?q=original&open=thread&folder=INBOX");
  await focus(document.querySelector<HTMLInputElement>("[aria-label='Search the corpus']")!);
  expect(router.state.location.search).toEqual({ q: "original", open: "thread", folder: "INBOX" });
  await focus(input());
  await type(input(), "  unlisted person  ");
  await focus(document.querySelector<HTMLSelectElement>("[aria-label='Gmail account']")!);
  expect(router.state.location.search).not.toHaveProperty("person");
  const outside = document.createElement("button");
  document.body.append(outside);
  await focus(outside);
  expect(router.state.location.search).toEqual({ q: "original", person: "unlisted person" });
  outside.remove();
});

it.each(["ali", "no matches"])(
  "NavSearch Escape first closes suggestions without editing %s, then clears the whole search",
  async (typed) => {
    const router = await search("/?q=original&person=Alice&open=thread&folder=INBOX");
    await type(input(), typed);
    await key(input(), "Escape");
    expect(input().getAttribute("aria-expanded")).toBe("false");
    expect(input().value).toBe(typed);
    expect(router.state.location.search).toEqual({
      q: "original",
      person: "Alice",
      open: "thread",
      folder: "INBOX",
    });
    await key(input(), "Escape");
    expect(router.state.location.search).toEqual({ open: "thread" });
    expect(document.querySelector("[role=combobox]")).toBeNull();
    expect(
      document.querySelector<HTMLInputElement>("[aria-label='Search the corpus']")!.value,
    ).toBe("");
  },
);

it("NavSearch Enter selection commits once without submitting again, and Anyone removes just the person filter", async () => {
  const router = await search("/?q=original&person=Alice");
  const navigate = vi.spyOn(router, "navigate");
  await type(input(), "Bob");
  await key(input(), "Enter");
  expect(router.state.location.search).toEqual({ q: "original", person: "U_BOB" });
  expect(navigate).toHaveBeenCalledTimes(1);
  await type(input(), "");
  await pick(option("Anyone"));
  expect(router.state.location.search).toEqual({ q: "original" });
  expect(navigate).toHaveBeenCalledTimes(2);
  // A closed autocomplete still remembers its accepted item internally.
  await key(input(), "Escape");
  expect(router.state.location.search).toEqual({});
});

// @vitest-environment jsdom
import { act, createElement, Fragment } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import HasslesView from "../src/components/hassles/HasslesView";
import NavHasslesRefresh from "../src/components/navigation/NavHasslesRefresh";
import { defaultSchedule } from "../src/lib/hassles/forms";
import { hassleOperations } from "../src/lib/hassles/operations";
import { canWrite, nativeOperations, ready, type GroupRequest } from "../src/lib/hassles/native";
import { hasslesNativeFixture } from "./hasslesNativeFixture";
import { fixtureLocation } from "./hasslesFixture";
import { $api } from "../src/lib/api/api";
import SettingsAccounts from "../src/components/settings/SettingsAccounts";
import SettingsHassles from "../src/components/settings/SettingsHassles";
import { clearToasts } from "../src/lib/ui/toasts";

let root: Root;
let host: HTMLDivElement;
let client: QueryClient;
let fixture: ReturnType<typeof hasslesNativeFixture>;
let hold: Promise<void> | undefined;
let lose = false;
const settle = () =>
  act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 25));
  });
const screen = () =>
  createElement(
    QueryClientProvider,
    { client },
    createElement(Fragment, null, createElement(NavHasslesRefresh), createElement(HasslesView)),
  );
async function remount() {
  await act(async () => root.render(createElement("p", null, "Away")));
  await act(async () => root.render(screen()));
  await settle();
}
const button = (text: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find(
    (b) => b.textContent === text || b.getAttribute("aria-label") === text,
  )!;
async function click(text: string) {
  await act(async () => button(text).click());
  await settle();
}
const writes = () => fixture.requests.filter((r) => r.method === "POST");
const schedule = {
  ...defaultSchedule(),
  title: "Office cycle",
  cadence: "hourly" as const,
  interval: 0.5,
  start: "2027-01-01T09:00",
  until: "2027-02-01T09:00",
  workHours: true,
};
async function field(element: Element, value: string) {
  await act(async () => {
    const prototype =
      element.tagName === "SELECT"
        ? HTMLSelectElement.prototype
        : element.tagName === "TEXTAREA"
          ? HTMLTextAreaElement.prototype
          : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(prototype, "value")!.set!.call(element, value);
    element.dispatchEvent(
      new Event(element.tagName === "SELECT" ? "change" : "input", { bubbles: true }),
    );
  });
}
async function create() {
  await act(async () => {
    expect(nativeOperations(client).create(fixtureLocation, schedule)).toBe(true);
  });
  await settle();
}
beforeEach(async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  fixture = hasslesNativeFixture();
  hold = undefined;
  lose = false;
  vi.stubGlobal("fetch", async (request: Request) => {
    const body = request.method === "POST" ? await request.json() : undefined;
    const result = fixture.handle(request.url, request.method, body);
    if (request.method === "POST" && new URL(request.url).pathname === "/v1/hassles/groups") {
      if (hold) await hold;
      if (lose) throw Error("Response lost");
    }
    return new Response(JSON.stringify(result.data), {
      status: result.status,
      headers: { "Content-Type": "application/json" },
    });
  });
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  await act(async () => root.render(screen()));
  await settle();
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  client.clear();
  clearToasts();
  vi.unstubAllGlobals();
});

it("renders one native card with work/cadence/until and only Delete, including singleton", async () => {
  await create();
  expect(host.querySelectorAll("li")).toHaveLength(1);
  expect(host.textContent).toContain("Every 0.5 hours");
  expect(host.textContent).toContain("Until");
  expect(host.textContent).toContain("Timing unavailable");
  expect(host.querySelector("[data-hassle-work]")?.textContent).toBe("work");
  expect(host.querySelectorAll("[data-hassle-actions] button")).toHaveLength(1);
  const task = client.getQueryData<{ tasks: componentsTask[] }>([
    "get",
    "/v1/hassles",
    { params: { query: fixtureLocation } },
  ])!.tasks[0]!;
  fixture.setTasks([{ ...task, resources: 1 }]);
  await click("Refresh hassles");
  expect(button("Delete Office cycle").disabled).toBe(false);
});
type componentsTask = import("../src/lib/hassles/forms").HassleTask;
it("captures exact count/title/location/revision once, retains card while delete response is held", async () => {
  await create();
  await click("Delete Office cycle");
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain("24 components");
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
    fixtureLocation.calendarId,
  );
  let release!: () => void;
  hold = new Promise((resolve) => {
    release = resolve;
  });
  await click("Remove all reminders");
  expect(host.querySelectorAll("li")).toHaveLength(1);
  expect(button("Delete Office cycle").disabled).toBe(true);
  await remount();
  expect(host.querySelectorAll("li")).toHaveLength(1);
  const request = writes().at(-1)!.body as GroupRequest;
  expect(request.group).toMatchObject({
    action: "delete",
    revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    fence: "11111111-1111-4111-8111-111111111111:2",
  });
  expect(nativeOperations(client).submit(request)).toBe(false);
  release();
  await settle();
  await settle();
  expect(host.querySelectorAll("li")).toHaveLength(0);
  expect(writes().map((r) => r.path)).toEqual(["/v1/hassles/groups", "/v1/hassles/groups"]);
});
it("blocks partial 409 across reload and failed inventory; only expected-ID recovery is posted", async () => {
  await create();
  fixture.partial();
  await click("Delete Office cycle");
  await click("Remove all reminders");
  const original = writes().at(-1)!.body as GroupRequest;
  expect(host.textContent).toContain("Pending delete: Office cycle · 24 components");
  expect(host.querySelectorAll("li")).toHaveLength(1);
  expect(button("Delete Office cycle").disabled).toBe(true);
  fixture.inventoryError();
  client.clear();
  await remount();
  expect(host.textContent).toContain("Pending delete: Office cycle · 24 components");
  expect(host.querySelector("textarea")?.disabled).toBe(true);
  await click("Refresh operation");
  expect(writes().filter((r) => r.path === "/v1/hassles/groups")).toHaveLength(2);
  expect(writes().at(-1)).toMatchObject({
    path: "/v1/hassles/recovery",
    body: { expectedOperationId: original.group.operationId },
  });
});
it("keeps original immutable IDs/fence after lost response and remount, never silently resends on ready", async () => {
  lose = true;
  await create();
  const original = structuredClone(writes()[0]!.body);
  const store = nativeOperations(client);
  expect(store.getSnapshot().request).toEqual(original);
  await remount();
  expect(store.getSnapshot().request).toEqual(original);
  expect(store.create(fixtureLocation, { ...schedule, title: "Changed" })).toBe(false);
  await click("Refresh hassles");
  expect(writes()).toHaveLength(1);
  expect(host.textContent).toContain("may have applied");
  lose = false;
  await click("Start a separate new hassle");
  await create();
  const next = writes().at(-1)!.body as GroupRequest;
  expect(next.group.operationId).not.toBe((original as GroupRequest).group.operationId);
  expect(next.group.fence).toBe("11111111-1111-4111-8111-111111111111:2");
});
it.each([
  undefined,
  { status: "unavailable" },
  { status: "busy" },
  { status: "ready" },
  { status: "ready", fence: "", pending: undefined },
  { status: "corrupt" },
])("fails closed for storage state %j", async (recovery) => {
  fixture.setRecovery(recovery as never);
  await click("Refresh hassles");
  expect(ready(recovery as never)).toBe(false);
  expect(nativeOperations(client).create(fixtureLocation, schedule)).toBe(false);
  expect(host.querySelector("textarea")?.disabled).toBe(true);
  expect(writes()).toHaveLength(0);
});

it.each(
  [0.5, 1, 2, 3, 4, 6, 8, 12, 24].flatMap((interval) =>
    [false, true].map((workHours) => ({ interval, workHours })),
  ),
)(
  "creates hourly $interval / workHours $workHours only through groups",
  async ({ interval, workHours }) => {
    const form = host.querySelector('section[aria-label="Add hassle"]')!;
    await field(form.querySelector("textarea")!, schedule.title);
    await field(form.querySelector("select")!, "custom");
    await field(form.querySelector('input[type="datetime-local"]')!, schedule.start);
    await field(
      [...form.querySelectorAll("select")].find((s) =>
        [...s.options].some((o) => o.value === "hourly"),
      )!,
      "hourly",
    );
    await field(
      [...form.querySelectorAll("select")].find((s) =>
        [...s.options].some((o) => o.value === "0.5"),
      )!,
      String(interval),
    );
    await field(
      [...form.querySelectorAll("select")].find((s) =>
        [...s.options].some((o) => o.value === "done"),
      )!,
      "date",
    );
    await field([...form.querySelectorAll('input[type="datetime-local"]')][1]!, schedule.until);
    if (workHours)
      await act(async () =>
        form.querySelector<HTMLInputElement>('input[type="checkbox"]')!.click(),
      );
    await click("Add hassle");
    const request = writes()[0]!.body as GroupRequest;
    expect(request.group).toMatchObject({
      action: "create",
      fence: "11111111-1111-4111-8111-111111111111:1",
      schedule: { ...schedule, interval, workHours },
    });
    expect(writes().map((r) => r.path)).toEqual(["/v1/hassles/groups"]);
  },
);
it("never upgrades a stale confirmation to the newer inventory revision or count", async () => {
  await create();
  await click("Delete Office cycle");
  const tasks = (fixture.handle("/v1/hassles", "GET").data as { tasks: componentsTask[] }).tasks;
  fixture.setTasks([{ ...tasks[0]!, revision: "newer", resources: 1, title: "Changed elsewhere" }]);
  await act(async () => {
    await nativeOperations(client).inventory(fixtureLocation);
  });
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain("24 components");
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain("Office cycle");
  await click("Remove all reminders");
  expect((writes().at(-1)!.body as GroupRequest).group).toMatchObject({
    revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  });
  expect(host.textContent).toContain("may have applied");
  expect(host.querySelectorAll("li")).toHaveLength(1);
});
it.each(["v2:resource:abcdef", "v2:not-a-uuid", "v2:00000000-0000-0000-0000-000000000000"])(
  "unknown/native fallback %s never receives legacy actions",
  async (id) => {
    await create();
    const tasks = (fixture.handle("/v1/hassles", "GET").data as { tasks: componentsTask[] }).tasks;
    fixture.setTasks([
      { ...tasks[0]!, id, format: "future-format" as never, writable: true, schedule },
    ]);
    await click("Refresh hassles");
    expect(host.querySelectorAll("[data-hassle-actions] button")).toHaveLength(0);
  },
);
it("global nav refresh reconciles only original pending ID, never mutator POSTs or mail", async () => {
  fixture.setRecovery({
    status: "pending",
    fence: "11111111-1111-4111-8111-111111111111:1",
    pending: {
      operationId: "22222222-2222-4222-8222-222222222222",
      action: "delete",
      title: "Absent group",
      components: 24,
      location: fixtureLocation,
    },
  });
  await click("Refresh hassles");
  expect(writes()).toEqual([
    {
      path: "/v1/hassles/recovery",
      method: "POST",
      body: { expectedOperationId: "22222222-2222-4222-8222-222222222222" },
    },
  ]);
  expect(fixture.requests.every((r) => r.path.startsWith("/v1/hassles"))).toBe(true);
});
it.each([
  {
    settled: false,
    recovery: { status: "ready", fence: "11111111-1111-4111-8111-111111111111:2" },
  },
  { settled: true, recovery: { status: "ready", fence: "11111111-1111-4111-8111-111111111111:1" } },
  { settled: true, recovery: { status: "ready" } },
  {
    settled: "true",
    recovery: { status: "ready", fence: "11111111-1111-4111-8111-111111111111:2" },
  },
])("does not accept an invalid 200 settlement %j", async (result) => {
  const previous = globalThis.fetch;
  vi.stubGlobal("fetch", async (request: Request) => {
    if (request.method === "POST" && new URL(request.url).pathname === "/v1/hassles/groups")
      return new Response(JSON.stringify(result), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    return previous(request);
  });
  await create();
  expect(nativeOperations(client).getSnapshot().request).toBeDefined();
  expect(host.textContent).toContain("not verified settled");
});

it("naturally remains unsettled then retires only after matching explicit verified reconciliation", async () => {
  await create();
  fixture.partial();
  await click("Delete Office cycle");
  await click("Remove all reminders");
  const store = nativeOperations(client);
  const original = structuredClone(store.getSnapshot().request);
  fixture.setTasks([]);
  await act(async () => store.inventory(fixtureLocation));
  expect(host.querySelectorAll("li")).toHaveLength(1);
  await click("Refresh operation");
  expect(store.getSnapshot().request).toEqual(original);
  expect(host.textContent).toContain("Pending delete");
  expect(host.querySelectorAll("li")).toHaveLength(1);
  fixture.verify();
  await click("Refresh operation");
  expect(store.getSnapshot()).toMatchObject({
    error: "",
    request: undefined,
    original: undefined,
    uncertain: undefined,
  });
  expect(host.querySelectorAll("li")).toHaveLength(0);
  expect(host.querySelector("textarea")?.disabled).toBe(false);
  expect(writes().filter((r) => r.path === "/v1/hassles/groups")).toHaveLength(2);
});

it.each(["/v1/hassles/location", "/v1/hassles/recovery"] as const)(
  "ignores held signal-ignoring stale %s success after pending publication",
  async (path) => {
    await create();
    const previous = globalThis.fetch;
    let release!: () => void;
    let captured = false;
    const gate = new Promise<void>((r) => {
      release = r;
    });
    vi.stubGlobal("fetch", async (request: Request) => {
      if (!captured && request.method === "GET" && new URL(request.url).pathname === path) {
        captured = true;
        const response = await previous(request);
        await gate; // Deliberately ignores Request.signal.
        return response;
      }
      return previous(request);
    });
    const oldRead = client
      .fetchQuery($api.queryOptions("get", path, {}, { staleTime: 0, retry: false }))
      .catch(() => {});
    await settle();
    fixture.partial();
    await click("Delete Office cycle");
    await click("Remove all reminders");
    expect(nativeOperations(client).getSnapshot().recovery?.status).toBe("pending");
    release();
    await oldRead;
    await settle();
    expect(nativeOperations(client).getSnapshot().recovery?.status).toBe("pending");
    expect(canWrite(client)).toBe(false);
    expect(host.textContent).not.toContain("Start a separate new hassle");
  },
);

it.each([false, true])(
  "invalidates reads issued during a held mutator, including late errors: %s",
  async (failure) => {
    await create();
    let releaseWrite!: () => void;
    hold = new Promise<void>((r) => {
      releaseWrite = r;
    });
    fixture.partial();
    await click("Delete Office cycle");
    await click("Remove all reminders");
    const previous = globalThis.fetch;
    let releaseRead!: () => void;
    let captured = false;
    const gate = new Promise<void>((r) => {
      releaseRead = r;
    });
    vi.stubGlobal("fetch", async (request: Request) => {
      if (
        !captured &&
        request.method === "GET" &&
        new URL(request.url).pathname === "/v1/hassles/recovery"
      ) {
        captured = true;
        await gate;
        if (failure) throw Error("Old status failed");
        return new Response(
          JSON.stringify({ status: "ready", fence: "11111111-1111-4111-8111-111111111111:2" }),
          { headers: { "Content-Type": "application/json" } },
        );
      }
      return previous(request);
    });
    const oldRead = nativeOperations(client).status();
    await settle();
    releaseWrite();
    await settle();
    await settle();
    releaseRead();
    await oldRead;
    await settle();
    expect(nativeOperations(client).getSnapshot().recovery?.status).toBe("pending");
    expect(canWrite(client)).toBe(false);
  },
);

it.each([
  { status: 200, text: "<html>Proxy failure</html>" },
  { status: 200, text: "" },
  { status: 502, text: "<html>Bad gateway</html>" },
  { status: 502, text: "not JSON" },
  {
    status: 409,
    text: JSON.stringify({
      error: "Corrupt status",
      recovery: {
        status: "ready",
        fence: "11111111-1111-4111-8111-111111111111:2",
        pending: false,
      },
    }),
  },
  { status: 0, text: "network" },
  ...[false, 0, "", null, []].map((pending) => ({
    status: 200,
    text: JSON.stringify({
      settled: true,
      recovery: { status: "ready", fence: "11111111-1111-4111-8111-111111111111:2", pending },
    }),
  })),
])(
  "retains original and refusal through actual fetch/proxy failure $status $text",
  async ({ status, text }) => {
    const previous = globalThis.fetch;
    vi.stubGlobal("fetch", async (request: Request) => {
      if (request.method === "POST" && new URL(request.url).pathname === "/v1/hassles/groups") {
        if (!status) throw TypeError("Network lost");
        return new Response(text, { status, headers: { "Content-Type": "application/json" } });
      }
      return previous(request);
    });
    await create();
    const store = nativeOperations(client);
    expect(store.getSnapshot().request?.group.action).toBe("create");
    expect(store.getSnapshot().error).toContain("may have applied");
    expect(canWrite(client)).toBe(false);
    expect(store.create(fixtureLocation, schedule)).toBe(false);
  },
);

it("clones caller schedule/location and captured delete before asynchronous delivery", async () => {
  let release!: () => void;
  hold = new Promise<void>((r) => {
    release = r;
  });
  const input = structuredClone(schedule);
  const location = structuredClone(fixtureLocation);
  await act(async () => {
    expect(nativeOperations(client).create(location, input)).toBe(true);
  });
  input.title = "Changed input";
  location.accountId = "wrong";
  await settle();
  expect((writes()[0]!.body as GroupRequest).location).toEqual(fixtureLocation);
  expect((writes()[0]!.body as GroupRequest).group).toMatchObject({
    schedule: { title: "Office cycle" },
  });
  release();
  await settle();
  await settle();
  hold = undefined;
  const task = (fixture.handle("/v1/hassles", "GET").data as { tasks: componentsTask[] }).tasks[0]!;
  const request = nativeOperations(client).captureDelete(task, fixtureLocation)!;
  await act(async () => {
    expect(nativeOperations(client).submit(request, task)).toBe(true);
  });
  request.location.accountId = "wrong";
  request.group.fence = "wrong";
  task.title = "Changed task";
  await settle();
  expect((writes().at(-1)!.body as GroupRequest).location).toEqual(fixtureLocation);
  expect((writes().at(-1)!.body as GroupRequest).group.fence).toBe(
    "11111111-1111-4111-8111-111111111111:2",
  );
});

it("blocks delete capture/button after ambiguous ready and scopes retained A card away from B", async () => {
  await create();
  lose = true;
  await click("Delete Office cycle");
  await click("Remove all reminders");
  const store = nativeOperations(client);
  const original = structuredClone(store.getSnapshot().request);
  const task = store.getSnapshot().original!;
  expect(store.captureDelete(task, fixtureLocation)).toBeUndefined();
  expect(button("Delete Office cycle").disabled).toBe(true);
  expect(host.textContent).toContain("New reminder operations are blocked");
  const other = { ...fixtureLocation, accountId: "other" };
  fixture.handle("/v1/hassles/location", "POST", { location: other });
  await click("Refresh hassles");
  expect(host.querySelectorAll("li")).toHaveLength(0);
  expect(store.getSnapshot().request).toEqual(original);
  expect(store.captureDelete(task, other)).toBeUndefined();
});

it("reciprocally blocks bound-account disconnect and settings selection while hassles or disconnect is active", async () => {
  await act(async () =>
    root.render(
      createElement(
        QueryClientProvider,
        { client },
        createElement(
          Fragment,
          null,
          createElement(SettingsAccounts),
          createElement(SettingsHassles),
          createElement(HasslesView),
        ),
      ),
    ),
  );
  await settle();
  let release!: () => void;
  hold = new Promise<void>((r) => {
    release = r;
  });
  await create();
  expect(button("Disconnect offline@example.test").disabled).toBe(true);
  expect(button("Disconnect other@example.test").disabled).toBe(false);
  expect(
    host.querySelector<HTMLSelectElement>('section[aria-label="Reminder location"] select')!
      .disabled,
  ).toBe(true);
  release();
  await settle();
  await settle();
  const previous = globalThis.fetch;
  let releaseDisconnect!: () => void;
  const gate = new Promise<void>((r) => {
    releaseDisconnect = r;
  });
  vi.stubGlobal("confirm", () => true);
  vi.spyOn(window, "confirm").mockReturnValue(true);
  vi.stubGlobal("fetch", async (request: Request) => {
    if (new URL(request.url).pathname.endsWith("/disconnect")) await gate;
    return previous(request);
  });
  await click("Disconnect offline@example.test");
  expect(canWrite(client)).toBe(false);
  expect(host.querySelector("textarea")?.disabled).toBe(true);
  expect(
    host.querySelector<HTMLSelectElement>('section[aria-label="Reminder location"] select')!
      .disabled,
  ).toBe(true);
  expect(host.querySelector('a[href="/auth/login"]')).not.toBeNull();
  releaseDisconnect();
  await settle();
});

it.each([
  "11111111-1111-4111-8111-111111111111:2",
  "11111111-1111-4111-8111-111111111111:4",
  "22222222-2222-4222-8222-222222222222:3",
])("does not retire original after explicit recovery with non-exact fence %s", async (fence) => {
  await create();
  fixture.partial();
  await click("Delete Office cycle");
  await click("Remove all reminders");
  const store = nativeOperations(client);
  const original = structuredClone(store.getSnapshot().request);
  const previous = globalThis.fetch;
  vi.stubGlobal("fetch", async (request: Request) => {
    if (request.method === "POST" && new URL(request.url).pathname === "/v1/hassles/recovery")
      return new Response(JSON.stringify({ settled: true, recovery: { status: "ready", fence } }), {
        headers: { "Content-Type": "application/json" },
      });
    return previous(request);
  });
  await click("Refresh operation");
  expect(store.getSnapshot().request).toEqual(original);
  expect(store.getSnapshot().error).toContain("not verified settled");
  expect(canWrite(client)).toBe(false);
  expect(writes().filter((r) => r.path === "/v1/hassles/groups")).toHaveLength(2);
});

it("never retires original A when explicit recovery verifies another operation", async () => {
  await create();
  fixture.partial();
  await click("Delete Office cycle");
  await click("Remove all reminders");
  const store = nativeOperations(client);
  const original = structuredClone(store.getSnapshot().request);
  const recovery = structuredClone(store.getSnapshot().recovery!);
  // A different operation can only be admitted after the original fence retires.
  recovery.fence = "11111111-1111-4111-8111-111111111111:3";
  recovery.pending!.operationId = "22222222-2222-4222-8222-222222222222";
  fixture.setRecovery(recovery);
  await act(async () => store.status());
  fixture.verify();
  await click("Refresh operation");
  expect(store.getSnapshot().request).toEqual(original);
  expect(store.getSnapshot().error).toContain("not verified settled");
  expect(canWrite(client)).toBe(false);
});

it("does not downgrade pending to same-fence ready even on a later status read", async () => {
  await create();
  fixture.partial();
  await click("Delete Office cycle");
  await click("Remove all reminders");
  const store = nativeOperations(client);
  const pending = structuredClone(store.getSnapshot().recovery!);
  fixture.setRecovery({ status: "ready", fence: pending.fence });
  await act(async () => store.status());
  expect(store.getSnapshot().recovery).toEqual(pending);
  expect(button("Refresh operation")).toBeDefined();
  expect(host.textContent).not.toContain("Start a separate new hassle");
});

it("refuses a confirmation after a fence change with visible feedback instead of silently rebasing", async () => {
  await create();
  await click("Delete Office cycle");
  fixture.setRecovery({ status: "ready", fence: "11111111-1111-4111-8111-111111111111:3" });
  await act(async () => nativeOperations(client).status());
  expect(button("Remove all reminders").disabled).toBe(true);
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
    "This confirmation is stale",
  );
  expect(writes()).toHaveLength(1);
});

it("blocks an independently published pending operation in Settings, including bound disconnect and selection", async () => {
  const pending = {
    status: "pending" as const,
    fence: "11111111-1111-4111-8111-111111111111:1",
    pending: {
      operationId: "22222222-2222-4222-8222-222222222222",
      action: "delete" as const,
      title: "Other browser original",
      components: 24,
      location: fixtureLocation,
    },
  };
  fixture.setRecovery(pending);
  await act(async () =>
    root.render(
      createElement(
        QueryClientProvider,
        { client },
        createElement(
          Fragment,
          null,
          createElement(SettingsAccounts),
          createElement(SettingsHassles),
        ),
      ),
    ),
  );
  await act(async () => nativeOperations(client).status());
  await settle();
  expect(host.textContent).toContain("Pending delete: Other browser original");
  expect(button("Disconnect offline@example.test").disabled).toBe(true);
  expect(button("Disconnect other@example.test").disabled).toBe(false);
  expect(
    host.querySelector<HTMLSelectElement>('section[aria-label="Reminder location"] select')!
      .disabled,
  ).toBe(true);
  expect(button("Use calendar").disabled).toBe(true);
  expect(host.querySelector('a[href="/auth/login"]')).not.toBeNull();
  await click("Refresh operation");
  expect(nativeOperations(client).getSnapshot().error).not.toBe("");
  fixture.verify();
  await click("Refresh operation");
  expect(nativeOperations(client).getSnapshot().error).toBe("");
  expect(button("Disconnect offline@example.test").disabled).toBe(false);
  expect(
    host.querySelector<HTMLSelectElement>('section[aria-label="Reminder location"] select')!
      .disabled,
  ).toBe(false);
  expect(writes().every((r) => r.path === "/v1/hassles/recovery")).toBe(true);
});

it("refuses a captured A confirmation after selection B without retargeting or posting it", async () => {
  await create();
  const store = nativeOperations(client);
  const task = (fixture.handle("/v1/hassles", "GET").data as { tasks: componentsTask[] }).tasks[0]!;
  const request = store.captureDelete(task, fixtureLocation)!;
  const original = structuredClone(request);
  const other = { ...fixtureLocation, accountId: "other" };
  fixture.handle("/v1/hassles/location", "POST", { location: other });
  await click("Refresh hassles");
  await act(async () => {
    expect(store.submit(request, task)).toBe(false);
  });
  expect(request).toEqual(original);
  expect(writes().filter((r) => r.path === "/v1/hassles/groups")).toHaveLength(1);
  expect(store.getSnapshot().error).toContain("no longer eligible");
});

it("refreshes authoritative dedicated recovery on a later configuration/remount instead of trusting its status", async () => {
  await create();
  fixture.setRecovery({
    status: "pending",
    fence: "11111111-1111-4111-8111-111111111111:2",
    pending: {
      operationId: "22222222-2222-4222-8222-222222222222",
      action: "delete",
      title: "Other browser",
      components: 24,
      location: fixtureLocation,
    },
  });
  await remount();
  expect(nativeOperations(client).getSnapshot().recovery?.status).toBe("pending");
  expect(host.querySelector("textarea")?.disabled).toBe(true);
  expect(canWrite(client)).toBe(false);
  expect(writes()).toHaveLength(1);
});

it("polls busy local status to fresh readiness without a mutator or Calendar reconciliation", async () => {
  fixture.setRecovery({ status: "busy" });
  await act(async () => nativeOperations(client).status());
  expect(host.querySelector("textarea")?.disabled).toBe(true);
  fixture.setRecovery({ status: "ready", fence: "11111111-1111-4111-8111-111111111111:1" });
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 1100));
  });
  expect(ready(nativeOperations(client).getSnapshot().recovery)).toBe(true);
  expect(host.querySelector("textarea")?.disabled).toBe(false);
  expect(writes()).toHaveLength(0);
});

it.each(
  ["network", "malformed", "unavailable"].flatMap((failure) =>
    [
      "11111111-1111-4111-8111-111111111111:1",
      "11111111-1111-4111-8111-111111111111:0",
      "22222222-2222-4222-8222-222222222222:9",
    ].map((staleFence) => ({ failure, staleFence })),
  ),
)(
  "preserves pending witness through $failure before stale ready $staleFence",
  async ({ failure, staleFence }) => {
    const pending = {
      status: "pending" as const,
      fence: "11111111-1111-4111-8111-111111111111:1",
      pending: {
        operationId: "22222222-2222-4222-8222-222222222222",
        action: "create" as const,
        title: "Original pending",
        components: 24,
        location: fixtureLocation,
      },
    };
    fixture.setRecovery(pending);
    await act(async () => nativeOperations(client).status());
    const previous = globalThis.fetch;
    vi.stubGlobal("fetch", async (request: Request) => {
      if (new URL(request.url).pathname === "/v1/hassles/recovery") {
        if (failure === "network") throw TypeError("Status connection lost");
        return new Response(
          JSON.stringify(failure === "malformed" ? [] : { status: "unavailable" }),
          {
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      return previous(request);
    });
    await act(async () => nativeOperations(client).status());
    expect(canWrite(client)).toBe(false);
    vi.stubGlobal("fetch", previous);
    fixture.setRecovery({ status: "ready", fence: staleFence });
    await act(async () => nativeOperations(client).status());
    expect(nativeOperations(client).getSnapshot().recovery).toEqual(pending);
    expect(canWrite(client)).toBe(false);
    expect(host.textContent).toContain("Original pending");
    expect(host.textContent).not.toContain("Start a separate new hassle");
    fixture.setRecovery({ status: "ready", fence: "11111111-1111-4111-8111-111111111111:2" });
    await act(async () => nativeOperations(client).status());
    expect(canWrite(client)).toBe(true);
    expect(writes()).toHaveLength(0);
  },
);

it.each(
  [200, 409].flatMap((status) =>
    (["pending", "unavailable"] as const).map((recoveryStatus) => ({ status, recoveryStatus })),
  ),
)(
  "refreshes dedicated recovery after legacy $status before reopening $recoveryStatus",
  async ({ status, recoveryStatus }) => {
    const previous = globalThis.fetch;
    vi.stubGlobal("fetch", async (request: Request) => {
      if (request.method === "POST" && new URL(request.url).pathname === "/v1/hassles") {
        fixture.handle(request.url, request.method, await request.json());
        fixture.setRecovery(
          recoveryStatus === "unavailable"
            ? { status: "unavailable" }
            : {
                status: "pending",
                fence: "11111111-1111-4111-8111-111111111111:1",
                pending: {
                  operationId: "22222222-2222-4222-8222-222222222222",
                  action: "create",
                  title: "Other browser pending",
                  components: 24,
                  location: fixtureLocation,
                },
              },
        );
        return new Response(
          JSON.stringify(
            status === 200
              ? { message: "Saved" }
              : { error: "operation store is pending or unavailable" },
          ),
          { status, headers: { "Content-Type": "application/json" } },
        );
      }
      return previous(request);
    });
    await act(async () => {
      expect(
        hassleOperations(client, fixtureLocation).create({
          ...schedule,
          cadence: "daily",
          interval: 1,
          workHours: false,
        }),
      ).toBe(true);
    });
    await settle();
    expect(nativeOperations(client).getSnapshot().recovery?.status).toBe(recoveryStatus);
    expect(canWrite(client)).toBe(false);
    const posted = fixture.requests.findIndex(
      (r) => r.path === "/v1/hassles" && r.method === "POST",
    );
    expect(posted).toBeGreaterThanOrEqual(0);
    expect(
      fixture.requests
        .slice(posted + 1)
        .some((r) => r.path === "/v1/hassles/recovery" && r.method === "GET"),
    ).toBe(true);
    expect(writes().map((r) => r.path)).toEqual(["/v1/hassles"]);
  },
);

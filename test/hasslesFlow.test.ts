// @vitest-environment jsdom
import { act, createElement, Fragment } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider, QueryObserver } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import SettingsHassles from "../src/components/settings/SettingsHassles";
import HasslesView from "../src/components/hassles/HasslesView";
import NavHasslesRefresh from "../src/components/navigation/NavHasslesRefresh";
import ToastHost from "../src/components/ui/ToastHost";
import { clearToasts } from "../src/lib/ui/toasts";
import { hasslesFixture, fixtureLocation } from "./hasslesFixture";

function hassleScreen() {
  return createElement(
    Fragment,
    null,
    createElement(NavHasslesRefresh),
    createElement(HasslesView),
    createElement(ToastHost),
  );
}

let root: Root;
let host: HTMLDivElement;
let client: QueryClient;
let fixture: ReturnType<typeof hasslesFixture>;
let inventoryGate: Promise<void> | null = null;
beforeEach(async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-04T01:15:45Z"));
  fixture = hasslesFixture();
  inventoryGate = null;
  vi.stubGlobal("fetch", async (input: Request) => {
    const text = await input.text();
    const result = fixture.handle(input.url, input.method, text ? JSON.parse(text) : undefined);
    const payload = JSON.stringify(result.data);
    if (input.method === "GET" && new URL(input.url).pathname === "/v1/hassles" && inventoryGate)
      await inventoryGate;
    return new Response(payload, {
      status: result.status,
      headers: { "Content-Type": "application/json" },
    });
  });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  await act(async () =>
    root.render(createElement(QueryClientProvider, { client }, hassleScreen())),
  );
  await settle();
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  client.clear();
  clearToasts();
  vi.restoreAllMocks();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 40));
  });
}
function button(text: string) {
  const el = [...document.querySelectorAll("button")].find((el) => el.textContent === text);
  if (!el) throw new Error(`Missing button ${text}: ${document.body.textContent}`);
  return el;
}
async function click(el: HTMLElement) {
  await act(async () => {
    el.focus();
    el.click();
  });
  await settle();
}
async function select(el: HTMLSelectElement, value: string) {
  await act(async () => {
    el.value = value;
    el.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await settle();
}
async function type(el: HTMLInputElement, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(el, value);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  });
}
async function waitReady(text: string) {
  for (let i = 0; i < 50; i++) {
    const el = [...document.querySelectorAll("button")].find((el) => el.textContent === text);
    if (el && !el.disabled) return;
    await settle();
  }
  throw new Error(`Button not ready: ${text}`);
}
async function choose() {
  await act(async () =>
    root.render(createElement(QueryClientProvider, { client }, createElement(SettingsHassles))),
  );
  await settle();
  await settle();
  await select(field("Connected Google account").querySelector("select")!, "offline");
  await select(
    field("Dedicated reminder calendar").querySelector("select")!,
    fixtureLocation.calendarId,
  );
  await click(document.querySelector<HTMLInputElement>('input[type="checkbox"]')!);
  await click(button("Use calendar"));
  await act(async () =>
    root.render(createElement(QueryClientProvider, { client }, hassleScreen())),
  );
  await waitReady("Add hassle");
}
function addForm() {
  return document.querySelector('section[aria-label="Add hassle"]')!;
}
function field(label: string, scope: ParentNode = document) {
  const wrapper = [...scope.querySelectorAll("label")].find(
    (el) => el.querySelector("span")?.textContent === label,
  );
  if (!wrapper) throw new Error(`Missing field ${label}`);
  return wrapper;
}
function repeat(scope: ParentNode = addForm()) {
  return field("Repeat", scope).querySelector("select")!;
}
function repeatUntil(scope: ParentNode = addForm()) {
  return field("Repeat until", scope).querySelector("select")!;
}
function firstReminder(scope: ParentNode = addForm()) {
  return field("First reminder · Australia/Melbourne", scope).querySelector("select")!;
}
function customStart(scope: ParentNode = addForm()) {
  return field("First reminder date and time · Australia/Melbourne", scope).querySelector("input")!;
}
function titleInput(scope: ParentNode = addForm()) {
  return scope.querySelector<HTMLInputElement>('input[aria-label="Hassle"]')!;
}
function refresh() {
  return document.querySelector<HTMLButtonElement>('[aria-label="Refresh hassles"]')!;
}
async function add(title: string) {
  await waitReady("Add hassle");
  await type(titleInput(), title);
  await select(firstReminder(), "custom");
  await type(customStart(), "2027-01-01T08:30");
  await click(button("Add hassle"));
}
it("keeps Add hassle inline above the list through create, edit, Done and Delete", async () => {
  expect(document.body.textContent).toContain("dedicated reminder calendar");
  await choose();
  expect(document.body.textContent).toContain("No hassles");
  expect(addForm()).not.toBeNull();
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(
    addForm().compareDocumentPosition(document.querySelector('[role="tablist"]')!) &
      Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  await add("Offline bill");
  expect(document.body.textContent).toContain("Offline bill");
  expect(titleInput().value).toBe("");
  await type(titleInput(), "Unsaved next hassle");
  await click(button("Edit"));
  expect(document.activeElement).toBe(document.querySelector('[role="dialog"] input'));
  await act(async () =>
    document.activeElement!.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }),
    ),
  );
  await settle();
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(titleInput().value).toBe("Unsaved next hassle");
  await click(button("Edit"));
  await type(document.querySelector('[role="dialog"] input')!, "Edited bill");
  await click(button("Save reminder"));
  expect(document.body.textContent).toContain("Edited bill");
  expect(titleInput().value).toBe("Unsaved next hassle");
  await click(button("Done"));
  await click(button("Remove all reminders"));
  expect(document.body.textContent).toContain("No hassles");
  await add("Delete me");
  await click(button("Delete"));
  await click(button("Remove all reminders"));
  expect(document.body.textContent).toContain("No hassles");
  const writes = fixture.requests.filter(
    (r) => r.path.endsWith("/v1/hassles") && r.method === "POST",
  );
  expect(writes.map((r) => (r.body as { action: string }).action)).toEqual([
    "create",
    "edit",
    "done",
    "create",
    "delete",
  ]);
  expect(
    writes.every(
      (r) =>
        JSON.stringify((r.body as { location: unknown }).location) ===
        JSON.stringify(fixtureLocation),
    ),
  ).toBe(true);
});
it("keeps an uncertain create UUID for explicit retry and displays permission errors", async () => {
  await choose();
  fixture.handle("/__hassles_fixture", "POST", { mode: "read-only" });
  await add("Permission test");
  expect(document.body.textContent).toContain("HTTP 403");
  const first = fixture.requests
    .filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"))
    .at(-1)!.body as { id: string };
  fixture.handle("/__hassles_fixture", "POST", { mode: "normal" });
  expect(titleInput().value).toBe("Permission test");
  await click(button("Add hassle"));
  const last = fixture.requests
    .filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"))
    .at(-1)!.body as { id: string };
  expect(last.id).toBe(first.id);
  expect(document.body.textContent).toContain("Permission test");
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(titleInput().value).toBe("");
  await add("Another task");
  const next = fixture.requests
    .filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"))
    .at(-1)!.body as { id: string };
  expect(next.id).not.toBe(first.id);
});
it("does not expose Calendar access while the feature grant is disabled", async () => {
  fixture.handle("/__hassles_fixture", "POST", { mode: "disabled" });
  await act(async () => {
    await client.invalidateQueries();
  });
  await settle();
  expect(document.body.textContent).toContain("Hassles is disabled");
  expect(document.querySelector("select")).toBeNull();
});
it("discards an open editor and late old-location inventory after a selection switch", async () => {
  await choose();
  await add("Old location task");
  await click(button("Edit"));
  let release!: () => void;
  inventoryGate = new Promise<void>((resolve) => {
    release = resolve;
  });
  // Keep an old inventory response outstanding while another tab changes selection.
  await act(async () => {
    void client.refetchQueries({ queryKey: ["get", "/v1/hassles"] });
  });
  await settle();
  const other = { ...fixtureLocation, calendarId: "/caldav/v2/second@example.test/events/" };
  fixture.handle("/v1/hassles/location", "POST", {
    location: other,
    previous: fixtureLocation,
    dedicated: true,
  });
  inventoryGate = null;
  await act(async () => {
    await client.invalidateQueries({ queryKey: ["get", "/v1/hassles/location"] });
  });
  await settle();
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(document.body.textContent).not.toContain("Old location task");
  await act(async () => {
    release();
  });
  await settle();
  expect(document.body.textContent).not.toContain("Old location task");
  expect(document.body.textContent).toContain("No hassles");
});
it("offers Repeat until Done / Date and time and clears the timestamp when Done is chosen", async () => {
  await choose();
  let dialog = addForm();
  await type(titleInput(dialog), "Repeat end test");
  await select(firstReminder(dialog), "custom");
  await type(customStart(dialog), "2027-01-01T08:30");
  await select(repeat(dialog), "daily");
  expect(dialog.querySelectorAll('input[type="datetime-local"]')).toHaveLength(1);
  await select(repeatUntil(dialog), "date");
  await type(
    field("Repeat end · Australia/Melbourne", dialog).querySelector("input")!,
    "2027-02-01T08:30",
  );
  await click(button("Add hassle"));
  await click(button("Edit"));
  dialog = document.querySelector('[role="dialog"]')!;
  expect(repeatUntil(dialog).value).toBe("date");
  expect(field("Repeat end · Australia/Melbourne", dialog).querySelector("input")!.value).toBe(
    "2027-02-01T08:30",
  );
  await select(repeatUntil(dialog), "done");
  expect(dialog.querySelectorAll('input[type="datetime-local"]')).toHaveLength(1);
  await click(button("Save reminder"));
  const edit = fixture.requests
    .filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"))
    .at(-1)!.body as { schedule: { until: string } };
  expect(edit.schedule.until).toBe("");
});
it("defaults to daily until Done and keeps the create draft/UUID through accessible tabs", async () => {
  await choose();
  expect(repeat().value).toBe("daily");
  expect(repeatUntil().value).toBe("done");
  fixture.handle("/__hassles_fixture", "POST", { mode: "read-only" });
  await add("Keep across tabs");
  const first = fixture.requests
    .filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"))
    .at(-1)!.body as { id: string };
  await click(button("Past"));
  expect(button("Past").getAttribute("aria-selected")).toBe("true");
  expect(document.querySelector('[role="tabpanel"]')!.getAttribute("aria-labelledby")).toBe(
    button("Past").id,
  );
  expect(document.body.textContent).toContain("Last 30 days");
  expect(titleInput().value).toBe("Keep across tabs");
  await click(button("Upcoming"));
  fixture.handle("/__hassles_fixture", "POST", { mode: "normal" });
  await click(button("Add hassle"));
  const retry = fixture.requests
    .filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"))
    .at(-1)!.body as { id: string; schedule: { cadence: string; until: string } };
  expect(retry.id).toBe(first.id);
  expect(retry.schedule.cadence).toBe("daily");
  expect(retry.schedule.until).toBe("");
  expect(document.body.textContent).toContain("Keep across tabs");
});
it("separates ended reminders from ongoing/unknown schedules and shares Melbourne day headers", async () => {
  await choose();
  const recent = new Date(Date.now() - 3600000).toISOString();
  const sample = {
    revision: "1",
    resources: 1,
    writable: false,
    schedule: null,
    restriction: "Imported timezone definition: schedule editing unavailable",
    reminder: "Display only",
  };
  fixture.handle("/__hassles_fixture", "POST", {
    tasks: [
      {
        ...sample,
        id: "a",
        title: "Later daily",
        timing: { state: "upcoming", next: "2027-01-01T02:00:00Z", last: recent, note: "" },
      },
      {
        ...sample,
        id: "b",
        title: "Earlier daily",
        timing: { state: "upcoming", next: "2027-01-01T01:00:00Z", last: recent, note: "" },
      },
      {
        ...sample,
        id: "c",
        title: "Ended once",
        timing: { state: "past", next: null, last: recent, note: "" },
      },
      {
        ...sample,
        id: "d",
        title: "Unknown imported",
        timing: { state: "unknown", next: null, last: null, note: "Unsupported schedule" },
      },
    ],
  });
  await click(refresh());
  const panel = document.querySelector('[role="tabpanel"]')!;
  expect([...panel.querySelectorAll("h3")].map((el) => el.textContent)).toEqual([
    "Earlier daily",
    "Later daily",
    "Unknown imported",
  ]);
  expect([...panel.querySelectorAll("h2")].map((el) => el.textContent)).toEqual([
    "Fri, 1 Jan 2027",
    "Date unavailable · view in calendar",
  ]);
  expect(panel.textContent).not.toContain("Ended once");
  await click(button("Past"));
  expect(
    [...document.querySelectorAll('[role="tabpanel"] h3')].map((el) => el.textContent),
  ).toEqual(["Ended once"]);
  expect(document.body.textContent).not.toContain("Overdue");
});
it("keeps recurrence controls in one row and names the actual interval unit", async () => {
  await choose();
  const row = addForm().querySelector<HTMLElement>("[data-hassle-repeat]")!;
  expect(row.classList.contains("flex")).toBe(true);
  expect(row.classList.contains("flex-wrap")).toBe(false);
  expect([...row.children].map((field) => field.querySelector("span")?.textContent)).toEqual([
    "Repeat",
    "Every",
    "Repeat until",
  ]);
  for (const [cadence, singular, plural] of [
    ["daily", "day", "days"],
    ["weekly", "week", "weeks"],
    ["monthly", "month", "months"],
    ["yearly", "year", "years"],
  ]) {
    await select(repeat(), cadence!);
    const every = row.children[1]!;
    const unit = () => every.querySelector("div > span")!.textContent;
    expect(unit()).toBe(singular);
    await type(field("Every", addForm()).querySelector("input")!, "2");
    expect(unit()).toBe(plural);
  }
  expect(row.textContent).not.toContain("units");
  await select(repeat(), "weekdays");
  expect(row.querySelector('input[type="number"]')).toBeNull();
  expect(row.querySelectorAll("select")).toHaveLength(2);
  await select(repeat(), "once");
  expect(row.children).toHaveLength(1);
});
it("shows configured calendar details only in Settings, not the Hassles header", async () => {
  await choose();
  expect(document.body.textContent).not.toContain(fixtureLocation.calendarId);
  expect(document.body.textContent).not.toContain("Reminder calendar settings");
  expect(document.querySelector('a[href="/settings#hassles-heading"]')).toBeNull();
  await act(async () =>
    root.render(createElement(QueryClientProvider, { client }, createElement(SettingsHassles))),
  );
  await settle();
  expect(document.body.textContent).toContain(fixtureLocation.calendarId);
});
it("uses placeholder-only title and presets, revealing the picker only for Custom", async () => {
  await choose();
  expect(addForm().querySelector("h2")).toBeNull();
  expect([...addForm().querySelectorAll("label")].map((el) => el.textContent)).not.toContain(
    "Title",
  );
  expect(titleInput().getAttribute("aria-label")).toBe("Hassle");
  expect(titleInput().placeholder).toBe("Hassle me about…");
  expect([...firstReminder().options].map((option) => option.textContent)).toEqual([
    "In an hour",
    "This afternoon",
    "This evening",
    "Tomorrow morning",
    "Custom",
  ]);
  expect(firstReminder().value).toBe("tomorrow");
  expect(addForm().querySelector('input[type="datetime-local"]')).toBeNull();
  await type(titleInput(), "Custom time");
  await select(firstReminder(), "custom");
  expect(customStart().value).toBe("2026-11-05T08:30");
  await type(customStart(), "2027-02-03T09:45");
  await click(button("Add hassle"));
  const create = fixture.requests
    .filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"))
    .at(-1)!.body as { schedule: { start: string } };
  expect(create.schedule.start).toBe("2027-02-03T09:45");
  await click(button("Edit"));
  const dialog = document.querySelector('[role="dialog"]')!;
  expect(firstReminder(dialog).value).toBe("custom");
  expect(customStart(dialog).value).toBe(create.schedule.start);
});
it("keeps today's next 08:30 as truthful Custom before Melbourne morning", async () => {
  vi.setSystemTime(new Date("2026-11-03T20:00:00Z"));
  await choose();
  expect(firstReminder().value).toBe("custom");
  expect(customStart().value).toBe("2026-11-04T08:30");
});
it("resolves presets at selection and keeps that payload and UUID on uncertain retry", async () => {
  await choose();
  await type(titleInput(), "One hour");
  fixture.handle("/__hassles_fixture", "POST", { mode: "read-only" });
  vi.setSystemTime(new Date("2026-11-04T03:45:10Z"));
  await select(firstReminder(), "afternoon");
  await select(firstReminder(), "custom");
  expect(customStart().value).toBe("2026-11-05T13:00");
  await select(firstReminder(), "evening");
  await select(firstReminder(), "custom");
  expect(customStart().value).toBe("2026-11-04T18:00");
  await select(firstReminder(), "hour");
  expect(addForm().querySelector('input[type="datetime-local"]')).toBeNull();
  await click(button("Add hassle"));
  const writes = () =>
    fixture.requests.filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"));
  const first = writes().at(-1)!.body as { schedule: { start: string } };
  expect(first.schedule.start).toBe("2026-11-04T15:45");
  vi.setSystemTime(new Date("2026-11-04T05:15:00Z"));
  await click(refresh());
  fixture.handle("/__hassles_fixture", "POST", { mode: "normal" });
  await click(button("Add hassle"));
  expect(writes().at(-1)!.body).toEqual(first);
});
it("global refresh reads config then only the exact selected inventory, preserving draft and retry UUID", async () => {
  await choose();
  fixture.handle("/__hassles_fixture", "POST", { mode: "read-only" });
  await add("Keep through refresh");
  const writes = () =>
    fixture.requests.filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"));
  const first = writes().at(-1)!.body;
  const mailRead = vi.fn(async () => ({ messages: [] }));
  const otherRead = vi.fn(async () => ({ tasks: [] }));
  const mail = new QueryObserver(client, {
    queryKey: ["get", "/v1/mail"],
    queryFn: mailRead,
    staleTime: Infinity,
  });
  const otherLocation = { accountId: "another-account", calendarId: "another-calendar" };
  const other = new QueryObserver(client, {
    queryKey: ["get", "/v1/hassles", { params: { query: otherLocation } }],
    queryFn: otherRead,
    staleTime: Infinity,
  });
  const stopMail = mail.subscribe(() => {});
  const stopOther = other.subscribe(() => {});
  await settle();
  const beforeMail = mailRead.mock.calls.length;
  const beforeOther = otherRead.mock.calls.length;
  const start = fixture.requests.length;
  await click(refresh());
  const requests = fixture.requests.slice(start);
  expect(requests.map((r) => [r.method, new URL(r.path, "http://localhost").pathname])).toEqual([
    ["GET", "/v1/hassles/location"],
    ["GET", "/v1/hassles"],
  ]);
  const query = new URL(requests[1]!.path, "http://localhost").searchParams;
  expect(Object.fromEntries(query)).toEqual(fixtureLocation);
  expect(mailRead).toHaveBeenCalledTimes(beforeMail);
  expect(otherRead).toHaveBeenCalledTimes(beforeOther);
  expect(titleInput().value).toBe("Keep through refresh");
  fixture.handle("/__hassles_fixture", "POST", { mode: "normal" });
  await click(button("Add hassle"));
  expect(writes().at(-1)!.body).toEqual(first);
  stopMail();
  stopOther();
});
it("global refresh follows a newly selected account/calendar instead of refetching the old one", async () => {
  await choose();
  await add("Old calendar");
  await type(titleInput(), "Old draft");
  const other = { accountId: "other-account", calendarId: "other-calendar" };
  fixture.handle("/v1/hassles/location", "POST", {
    location: other,
    previous: fixtureLocation,
    dedicated: true,
  });
  const start = fixture.requests.length;
  await click(refresh());
  await settle();
  const requests = fixture.requests.slice(start);
  expect(requests[0]!.method).toBe("GET");
  expect(new URL(requests[0]!.path, "http://localhost").pathname).toBe("/v1/hassles/location");
  const inventories = requests.filter(
    (r) => new URL(r.path, "http://localhost").pathname === "/v1/hassles",
  );
  expect(inventories.length).toBeGreaterThan(0);
  expect(
    inventories.every(
      (r) =>
        r.method === "GET" &&
        JSON.stringify(Object.fromEntries(new URL(r.path, "http://localhost").searchParams)) ===
          JSON.stringify(other),
    ),
  ).toBe(true);
  expect(document.body.textContent).not.toContain("Old calendar");
  expect(titleInput().value).toBe("");
  await add("New calendar");
  const last = fixture.requests
    .filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"))
    .at(-1)!.body as { location: unknown };
  expect(last.location).toEqual(other);
});
it("exposes global refresh busy state and accessible inventory failures", async () => {
  await choose();
  let release!: () => void;
  inventoryGate = new Promise<void>((resolve) => {
    release = resolve;
  });
  fixture.handle("/__hassles_fixture", "POST", { mode: "disconnected" });
  await click(refresh());
  expect(refresh().disabled).toBe(true);
  expect(refresh().getAttribute("aria-busy")).toBe("true");
  expect(refresh().querySelector("svg")!.classList.contains("motion-safe:animate-spin")).toBe(true);
  await act(async () => {
    release();
  });
  await settle();
  expect(refresh().disabled).toBe(false);
  expect(document.querySelector('[role="alert"]')!.textContent).toContain("disconnected");
  expect(document.body.textContent).toContain("Refreshing hassles failed");
});

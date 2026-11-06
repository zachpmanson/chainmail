// @vitest-environment jsdom
import { act, createElement, Fragment } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider, QueryObserver } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import SettingsHassles from "../src/components/settings/SettingsHassles";
import HasslesView from "../src/components/hassles/HasslesView";
import NavHasslesRefresh from "../src/components/navigation/NavHasslesRefresh";
import DeployStamp from "../src/components/navigation/DeployStamp";
import ToastHost from "../src/components/ui/ToastHost";
import { clearToasts } from "../src/lib/ui/toasts";
import { hasslesFixture, fixtureLocation } from "./hasslesFixture";

function hassleScreen() {
  return createElement(
    Fragment,
    null,
    createElement("header", null, createElement(DeployStamp), createElement(NavHasslesRefresh)),
    createElement(HasslesView),
    createElement(ToastHost),
  );
}

let root: Root;
let previousTitle: string;
let host: HTMLDivElement;
let client: QueryClient;
let fixture: ReturnType<typeof hasslesFixture>;
let inventoryGate: Promise<void> | null = null;
let configGate: Promise<void> | null = null;
let writeGate: Promise<void> | null = null;
let processing: ((request: Request, body: Record<string, unknown>) => Promise<void>) | null = null;
let delivery: ((request: Request, body: Record<string, unknown>) => Promise<void>) | null = null;
beforeEach(async () => {
  previousTitle = document.title;
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-04T01:15:45Z"));
  fixture = hasslesFixture();
  inventoryGate = null;
  configGate = null;
  writeGate = null;
  processing = null;
  delivery = null;
  vi.stubGlobal("fetch", async (input: Request) => {
    const text = await input.text();
    const body = text ? JSON.parse(text) : {};
    if (processing) await processing(input, body);
    const result = fixture.handle(input.url, input.method, text ? body : undefined);
    const payload = JSON.stringify(result.data);
    const path = new URL(input.url).pathname;
    if (input.method === "GET" && path === "/v1/hassles" && inventoryGate) await inventoryGate;
    if (input.method === "GET" && path === "/v1/hassles/location" && configGate) await configGate;
    if (input.method === "POST" && path === "/v1/hassles" && writeGate) await writeGate;
    if (delivery) await delivery(input, body);
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
it("submits hourly work-hours schedules, preserves a failed-create draft and edits the checkbox", async () => {
  await choose();
  const checkbox = (scope: ParentNode = addForm()) =>
    scope.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
  expect(checkbox().checked).toBe(false);
  const hourly = [...repeat().options].find((o) => o.value === "hourly")!;
  expect(hourly.disabled).toBe(false);
  expect([...repeat().options].map((option) => option.value)).toEqual([
    "once",
    "hourly",
    "daily",
    "weekdays",
    "weekly",
    "monthly",
    "yearly",
  ]);
  await type(titleInput(), "Office cycle");
  await select(firstReminder(), "custom");
  await type(customStart(), "2027-01-01T09:00");
  await select(repeat(), "hourly");
  const every = field("Every", addForm()).querySelector("select")!;
  expect([...every.options].map((option) => Number(option.value))).toEqual([
    0.5, 1, 2, 3, 4, 6, 8, 12, 24,
  ]);
  await select(every, "0.5");
  expect(field("Every", addForm()).textContent).toContain("hours");
  expect(every.classList.contains("min-w-14")).toBe(true);
  expect(every.parentElement!.classList.contains("flex-wrap")).toBe(true);
  await click(checkbox());
  expect(addForm().textContent).not.toContain("Reminders outside");
  fixture.handle("/__hassles_fixture", "POST", { mode: "read-only" });
  await click(button("Add hassle"));
  const writes = () =>
    fixture.requests.filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"));
  const first = writes().at(-1)!.body as {
    id: string;
    schedule: { cadence: string; interval: number; workHours: boolean };
  };
  expect(first.schedule).toMatchObject({ cadence: "hourly", interval: 0.5, workHours: true });
  expect(checkbox().checked).toBe(true);
  await click(button("Ended"));
  await click(button("Upcoming"));
  fixture.handle("/__hassles_fixture", "POST", { mode: "normal" });
  await click(button("Add hassle"));
  expect(writes().at(-1)!.body).toEqual(first);
  expect(checkbox().checked).toBe(false);
  expect(document.querySelector('section[aria-label="Reminders"] p')?.textContent).toContain(
    "work",
  );
  await click(button("Edit"));
  const dialog = document.querySelector('[role="dialog"][aria-modal="true"]')!;
  expect(document.querySelectorAll('[role="dialog"][aria-modal="true"]')).toHaveLength(1);
  expect(dialog.querySelector('[role="dialog"]')).toBeNull();
  const editForm = dialog.querySelector('section[aria-label="Edit hassle"]')!;
  expect(editForm.classList.contains("border")).toBe(false);
  expect(editForm.classList.contains("p-4")).toBe(false);
  expect(dialog.classList.contains("box-border")).toBe(true);
  expect(addForm().classList.contains("border")).toBe(true);
  expect(addForm().classList.contains("p-4")).toBe(true);
  expect(checkbox(dialog).checked).toBe(true);
  expect(dialog.textContent).not.toContain("Reminders outside");
  expect(repeat(dialog).value).toBe("hourly");
  expect(field("Every", dialog).querySelector("select")!.value).toBe("0.5");
  await click(checkbox(dialog));
  await click(button("Save reminder"));
  expect((writes().at(-1)!.body as { schedule: { workHours: boolean } }).schedule.workHours).toBe(
    false,
  );
  expect(document.querySelector('section[aria-label="Reminders"] p')?.textContent).not.toContain(
    "work",
  );
});
it.each(
  [0.5, 1, 2, 3, 4, 6, 8, 12, 24].flatMap((interval) =>
    [false, true].map((workHours) => ({ interval, workHours })),
  ),
)(
  "roundtrips hourly $interval / work hours $workHours through create retry and edit",
  async ({ interval, workHours }) => {
    await choose();
    await type(titleInput(), "Supported slots");
    await select(firstReminder(), "custom");
    await type(customStart(), "2027-01-01T16:45");
    await select(repeat(), "hourly");
    await select(field("Every", addForm()).querySelector("select")!, String(interval));
    if (workHours)
      await click(addForm().querySelector<HTMLInputElement>('input[type="checkbox"]')!);
    fixture.handle("/__hassles_fixture", "POST", { mode: "read-only" });
    await click(button("Add hassle"));
    const writes = () =>
      fixture.requests.filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"));
    const first = writes().at(-1)!.body;
    fixture.handle("/__hassles_fixture", "POST", { mode: "normal" });
    await click(button("Add hassle"));
    expect(writes().at(-1)!.body).toEqual(first);
    await click(button("Edit"));
    const dialog = document.querySelector('[role="dialog"][aria-modal="true"]')!;
    expect(field("Every", dialog).querySelector("select")!.value).toBe(String(interval));
    const checkbox = dialog.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
    expect(checkbox.checked).toBe(workHours);
    await click(checkbox);
    await click(button("Save reminder"));
    expect((writes().at(-1)!.body as { schedule: unknown }).schedule).toMatchObject({
      cadence: "hourly",
      interval,
      workHours: !workHours,
    });
  },
);
it("validates work-hours first reminders without moving them or sending a write", async () => {
  await choose();
  await type(titleInput(), "Invalid office time");
  await select(firstReminder(), "custom");
  await type(customStart(), "2027-01-01T18:00");
  await click(addForm().querySelector<HTMLInputElement>('input[type="checkbox"]')!);
  await click(button("Add hassle"));
  expect(addForm().textContent).toContain("Choose a first reminder on a weekday");
  expect(customStart().value).toBe("2027-01-01T18:00");
  expect(
    fixture.requests.filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles")),
  ).toHaveLength(0);
});
it("sets the document title to Hassles and restores it when leaving the page", async () => {
  expect(document.title).toBe("Hassles");
  expect(document.head.querySelector("title")!.textContent).toBe("Hassles");
  await act(async () => root.render(createElement("p", null, "Another page")));
  expect(document.title).toBe(previousTitle);
});
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 40));
  });
}
function button(text: string) {
  const el = [...document.querySelectorAll("button")].find(
    (el) => el.textContent === text || el.getAttribute("aria-label")?.startsWith(`${text} `),
  );
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
async function type(el: HTMLInputElement | HTMLTextAreaElement, value: string) {
  await act(async () => {
    const prototype =
      el instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(prototype, "value")!.set!.call(el, value);
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
  return field("Until", scope).querySelector("select")!;
}
function firstReminder(scope: ParentNode = addForm()) {
  return field("When · Melbourne", scope).querySelector("select")!;
}
function customStart(scope: ParentNode = addForm()) {
  return field("Date and time", scope).querySelector("input")!;
}
function titleInput(scope: ParentNode = addForm()) {
  return scope.querySelector<HTMLTextAreaElement>('textarea[aria-label="Hassle"]')!;
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
it("keeps Add hassle inline through create, pencil-icon editing and tick-icon Done without Delete", async () => {
  expect(document.body.textContent).toContain("dedicated reminder calendar");
  await choose();
  expect(document.body.textContent).toContain("No hassles");
  expect(addForm()).not.toBeNull();
  expect(document.querySelector('[role="dialog"][aria-modal="true"]')).toBeNull();
  expect(
    addForm().compareDocumentPosition(document.querySelector('[role="tablist"]')!) &
      Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  await add("Offline bill");
  expect(document.body.textContent).toContain("Offline bill");
  expect(document.querySelector('[aria-label="Dismiss: Created"]')).not.toBeNull();
  expect(document.querySelector('[aria-label="Hassle inventory"]')!.textContent).not.toContain(
    "Created",
  );
  expect(titleInput().value).toBe("");
  const edit = button("Edit");
  const card = edit.closest("li")!;
  const heading = card.querySelector("h3")!;
  expect(heading.textContent).toBe("Offline bill");
  expect(heading.children).toHaveLength(0);
  expect(heading.hasAttribute("tabindex")).toBe(false);
  await click(heading);
  expect(document.querySelector('[role="dialog"][aria-modal="true"]')).toBeNull();
  expect(edit.getAttribute("aria-label")).toBe("Edit Offline bill");
  expect(edit.title).toBe("Edit");
  expect(edit.querySelector('svg[aria-hidden="true"]')).not.toBeNull();
  expect(edit.textContent).toBe("");
  expect(edit.nextElementSibling).toBe(button("This instance only:"));
  expect(button("This instance only:").nextElementSibling).toBe(button("All instances:"));
  expect(card.classList.contains("bg-card")).toBe(true);
  expect(card.classList.contains("border-line")).toBe(true);
  expect(card.contains(button("All instances:"))).toBe(true);
  expect(button("All instances:").getAttribute("aria-label")).toBe("All instances: Offline bill");
  expect(button("All instances:").title).toBe("All instances: Offline bill");
  expect(button("All instances:").querySelector('svg[aria-hidden="true"]')).not.toBeNull();
  expect(button("All instances:").textContent).toBe("");
  expect([...document.querySelectorAll("button")].some((el) => el.textContent === "Delete")).toBe(
    false,
  );
  expect([...document.querySelectorAll("button")].some((el) => el.textContent === "Edit")).toBe(
    false,
  );
  await type(titleInput(), "Unsaved next hassle");
  await click(button("Edit"));
  expect(document.activeElement).toBe(
    document.querySelector('[role="dialog"][aria-modal="true"] textarea'),
  );
  await act(async () =>
    document.activeElement!.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }),
    ),
  );
  await settle();
  expect(document.querySelector('[role="dialog"][aria-modal="true"]')).toBeNull();
  expect(titleInput().value).toBe("Unsaved next hassle");
  await click(button("Edit"));
  await type(document.querySelector('[role="dialog"][aria-modal="true"] textarea')!, "Edited bill");
  await click(button("Save reminder"));
  expect(document.body.textContent).toContain("Edited bill");
  expect(document.querySelector('[aria-label="Dismiss: Updated"]')).not.toBeNull();
  expect(document.querySelector('[aria-label="Hassle inventory"]')!.textContent).not.toContain(
    "Updated",
  );
  expect(titleInput().value).toBe("Unsaved next hassle");
  await click(button("All instances:"));
  expect(document.querySelector('[role="dialog"][aria-modal="true"]')!.textContent).toContain(
    "Permanently remove all reminders for this hassle from Calendar. This cannot be undone.",
  );
  await click(button("Remove all reminders"));
  expect(document.body.textContent).toContain("No hassles");
  expect(
    document.querySelector('[aria-label="Dismiss: Removed all reminder resources"]'),
  ).not.toBeNull();
  expect(document.querySelector('[aria-label="Hassle inventory"]')!.textContent).not.toContain(
    "Removed all reminder resources",
  );
  const writes = fixture.requests.filter(
    (r) => r.path.endsWith("/v1/hassles") && r.method === "POST",
  );
  expect(writes.map((r) => (r.body as { action: string }).action)).toEqual([
    "create",
    "edit",
    "done",
  ]);
  expect(
    writes.every(
      (r) =>
        JSON.stringify((r.body as { location: unknown }).location) ===
        JSON.stringify(fixtureLocation),
    ),
  ).toBe(true);
});
it("uses four-row title fields and saves line breaks as spaces in the one-line Calendar title", async () => {
  await choose();
  expect(titleInput().rows).toBe(4);
  await type(titleInput(), "Pay bill\nCheck receipt");
  await click(button("Add hassle"));
  const create = fixture.requests
    .filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"))
    .at(-1)!.body as { schedule: { title: string } };
  expect(create.schedule.title).toBe("Pay bill Check receipt");
  await click(button("Edit"));
  const dialog = document.querySelector('[role="dialog"][aria-modal="true"]')!;
  expect(titleInput(dialog).rows).toBe(4);
  expect(titleInput(dialog).value).toBe(create.schedule.title);
});
it("centres the reminders column with the create form in a balanced left sidebar", async () => {
  await choose();
  const layout = document.querySelector('section[aria-label="Hassle inventory"]')!;
  const reminders = layout.querySelector('section[aria-label="Reminders"]')!;
  expect(layout.classList.contains("grid")).toBe(true);
  expect(layout.classList.contains("grid-cols-1")).toBe(true);
  expect(layout.className).toContain(
    "@min-[calc(900px+43rem)]:grid-cols-[450px_minmax(0,40rem)_450px]",
  );
  expect(layout.className).toContain("max-w-[40rem]");
  expect(layout.className).toContain("@min-[calc(900px+43rem)]:max-w-none");
  const main = document.querySelector("main")!;
  expect(main.classList.contains("max-w-[calc(900px+45rem)]")).toBe(true);
  expect(main.classList.contains("@container")).toBe(true);
  expect(main.classList.contains("box-border")).toBe(true);
  expect(layout.className).not.toContain("min-[60rem]");
  expect(layout.children).toHaveLength(2);
  const createPanel = layout.children[0]!;
  expect(createPanel.contains(addForm())).toBe(true);
  expect(createPanel.classList.contains("w-full")).toBe(true);
  expect(createPanel.className).not.toContain("max-w-");
  expect(layout.children[1]).toBe(reminders);
  expect(reminders.contains(document.querySelector('[role="tablist"]'))).toBe(true);
  expect(reminders.contains(addForm())).toBe(false);
  await type(titleInput(), "Draft across columns");
  await click(button("Ended"));
  expect(titleInput().value).toBe("Draft across columns");
});
it("constrains a long unbroken plain title to the row width and edits via its pencil icon", async () => {
  await choose();
  const title = "A".repeat(100);
  await add(title);
  const heading = document.querySelector("h3")!;
  expect(heading.textContent).toBe(title);
  expect(heading.children).toHaveLength(0);
  expect(heading.parentElement!.classList.contains("min-w-0")).toBe(true);
  expect(heading.parentElement!.classList.contains("wrap-break-word")).toBe(true);
  await click(button("Edit"));
  expect(titleInput(document.querySelector('[role="dialog"][aria-modal="true"]')!).value).toBe(
    title,
  );
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
  expect(document.querySelector('[role="dialog"][aria-modal="true"]')).toBeNull();
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
  expect(document.querySelector('[role="dialog"][aria-modal="true"]')).toBeNull();
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
  await type(field("End date and time", dialog).querySelector("input")!, "2027-02-01T08:30");
  await click(button("Add hassle"));
  await click(button("Edit"));
  dialog = document.querySelector('[role="dialog"][aria-modal="true"]')!;
  expect(repeatUntil(dialog).value).toBe("date");
  expect(field("End date and time", dialog).querySelector("input")!.value).toBe("2027-02-01T08:30");
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
  await click(button("Ended"));
  expect(button("Ended").getAttribute("aria-selected")).toBe("true");
  expect(document.querySelector('[role="tabpanel"]')!.getAttribute("aria-labelledby")).toBe(
    button("Ended").id,
  );
  expect(document.body.textContent).toContain("No ended reminders in the last 30 days.");
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
  expect(panel.textContent).toContain("Unsupported schedule");
  expect(panel.textContent).toContain("Imported timezone definition: schedule editing unavailable");
  expect(button("Edit").disabled).toBe(true);
  expect(button("All instances:").disabled).toBe(true);
  await click(button("Ended"));
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
    "Until",
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
it("omits the visible page heading and keeps configured calendar details only in Settings", async () => {
  await choose();
  expect(document.querySelector("main")?.getAttribute("aria-label")).toBe("Hassles");
  expect(document.querySelector("main h1")).toBeNull();
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
  expect(document.body.textContent).not.toContain("Five-minute, free events");
  expect(document.body.textContent).not.toContain("Phone delivery is not live-verified");
  expect(document.body.textContent).not.toContain("Monthly/yearly repeats skip");
  expect(document.body.textContent).not.toContain("Done and Delete both");
  expect(document.body.textContent).not.toContain("Notifications and recurrence belong");
  expect(document.body.textContent).not.toContain("Next scheduled reminders");
  expect(addForm().textContent?.match(/Melbourne/g)).toHaveLength(1);
  expect(addForm().querySelector('input[type="checkbox"]')!.closest("label")!.textContent).toBe(
    "Work hours only",
  );
  expect([...firstReminder().options].map((option) => option.textContent)).toEqual([
    "In an hour (13:15)",
    "This afternoon (13:00)",
    "This evening (18:00)",
    "Tomorrow morning (08:30)",
    "Custom",
  ]);
  expect(firstReminder().value).toBe("hour");
  expect(addForm().querySelector('input[type="datetime-local"]')).toBeNull();
  await type(titleInput(), "Custom time");
  await select(firstReminder(), "custom");
  expect(customStart().value).toBe("2026-11-04T13:15");
  await type(customStart(), "2027-02-03T09:45");
  await click(button("Add hassle"));
  const create = fixture.requests
    .filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"))
    .at(-1)!.body as { schedule: { start: string } };
  expect(create.schedule.start).toBe("2027-02-03T09:45");
  await click(button("Edit"));
  const dialog = document.querySelector('[role="dialog"][aria-modal="true"]')!;
  expect(firstReminder(dialog).value).toBe("custom");
  expect(customStart(dialog).value).toBe(create.schedule.start);
});
it("defaults to In an hour before morning too, without showing a custom picker", async () => {
  vi.setSystemTime(new Date("2026-11-03T20:00:00Z"));
  await choose();
  expect(firstReminder().value).toBe("hour");
  expect(addForm().querySelector('input[type="datetime-local"]')).toBeNull();
  await type(titleInput(), "Default hour");
  await click(button("Add hassle"));
  const create = fixture.requests
    .filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"))
    .at(-1)!.body as { schedule: { start: string } };
  expect(create.schedule.start).toBe("2026-11-04T08:00");
  expect(firstReminder().value).toBe("hour");
  await click(button("Edit"));
  const dialog = document.querySelector('[role="dialog"][aria-modal="true"]')!;
  expect(firstReminder(dialog).value).toBe("custom");
  expect(customStart(dialog).value).toBe(create.schedule.start);
});
it("refreshes unselected hour previews without changing a saved reminder time", async () => {
  await choose();
  await select(firstReminder(), "custom");
  vi.setSystemTime(new Date("2026-11-04T03:45:00Z"));
  await act(async () => firstReminder().focus());
  expect(firstReminder().options[0]!.textContent).toBe("In an hour (15:45)");
  expect(customStart().value).toBe("2026-11-04T13:15");
  await select(firstReminder(), "hour");
  vi.setSystemTime(new Date("2026-11-04T05:15:00Z"));
  await act(async () => {
    firstReminder().blur();
    firstReminder().focus();
  });
  expect(firstReminder().options[0]!.textContent).toBe("In an hour (15:45)");
  await select(firstReminder(), "custom");
  expect(customStart().value).toBe("2026-11-04T15:45");
  expect(firstReminder().options[0]!.textContent).toBe("In an hour (17:15)");
  vi.setSystemTime(new Date("2026-11-04T06:25:00Z"));
  await act(async () => {
    firstReminder().dispatchEvent(new Event("pointerdown", { bubbles: true }));
  });
  expect(firstReminder().options[0]!.textContent).toBe("In an hour (18:25)");
  expect(customStart().value).toBe("2026-11-04T15:45");
  vi.setSystemTime(new Date("2026-11-04T07:35:00Z"));
  await act(async () => {
    firstReminder().dispatchEvent(
      new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }),
    );
  });
  expect(firstReminder().options[0]!.textContent).toBe("In an hour (19:35)");
  expect(customStart().value).toBe("2026-11-04T15:45");
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
it("keeps Add usable on cached page entry and cancels a stale initial read after creating", async () => {
  await choose();
  await act(async () =>
    root.render(createElement(QueryClientProvider, { client }, createElement("p", null, "Away"))),
  );
  client.removeQueries({ queryKey: ["get", "/v1/hassles"] });
  let releaseConfig!: () => void;
  let releaseInventory!: () => void;
  configGate = new Promise<void>((resolve) => {
    releaseConfig = resolve;
  });
  inventoryGate = new Promise<void>((resolve) => {
    releaseInventory = resolve;
  });
  await act(async () =>
    root.render(createElement(QueryClientProvider, { client }, hassleScreen())),
  );
  await settle();
  expect(refresh().disabled).toBe(true);
  expect(titleInput().disabled).toBe(false);
  expect(firstReminder().disabled).toBe(false);
  await type(titleInput(), "Created during first inventory read");
  await select(firstReminder(), "custom");
  await type(customStart(), "2027-01-01T08:30");
  expect(titleInput().value).toBe("Created during first inventory read");
  expect(button("Add hassle").disabled).toBe(false);
  const inventoryReads = () =>
    fixture.requests.filter(
      (r) => r.method === "GET" && new URL(r.path, "http://localhost").pathname === "/v1/hassles",
    ).length;
  const readsBeforeCreate = inventoryReads();
  expect(readsBeforeCreate).toBeGreaterThan(0);
  await click(button("Add hassle"));
  expect(titleInput().disabled).toBe(false);
  expect(button("Add hassle").disabled).toBe(false);
  expect(titleInput().value).toBe("");
  expect(inventoryReads()).toBeGreaterThan(readsBeforeCreate);
  await act(async () => {
    releaseConfig();
    releaseInventory();
  });
  await settle();
  expect(document.querySelector("h3")!.textContent).toBe("Created during first inventory read");
});
it("keeps cached rows and Add usable through unrelated reads and writes", async () => {
  await choose();
  await add("Existing reminder");
  let releaseConfig!: () => void;
  let releaseInventory!: () => void;
  configGate = new Promise<void>((resolve) => {
    releaseConfig = resolve;
  });
  inventoryGate = new Promise<void>((resolve) => {
    releaseInventory = resolve;
  });
  expect(document.querySelector('header a[href*="/commit/"]')).not.toBeNull();
  await click(refresh());
  expect(document.querySelector('header [role="status"]')!.textContent).toBe("Reading reminders…");
  expect(document.querySelector('header a[href*="/commit/"]')).toBeNull();
  expect(document.querySelector('[aria-label="Hassle inventory"]')!.textContent).not.toContain(
    "Reading reminders…",
  );
  expect(button("Edit").disabled).toBe(false);
  expect(titleInput().disabled).toBe(false);
  await type(titleInput(), "Draft while settings refresh");
  await act(async () => {
    releaseConfig();
  });
  await settle();
  expect(refresh().disabled).toBe(true);
  expect(button("Edit").disabled).toBe(false);
  expect(titleInput().disabled).toBe(false);
  expect(button("Add hassle").disabled).toBe(false);
  await type(titleInput(), "Draft while reminders refresh");
  await act(async () => {
    releaseInventory();
  });
  await settle();
  expect(titleInput().value).toBe("Draft while reminders refresh");
  expect(button("Edit").disabled).toBe(false);
  expect(document.querySelector('header [role="status"]')).toBeNull();
  expect(document.querySelector('header a[href*="/commit/"]')).not.toBeNull();
  let releaseWrite!: () => void;
  writeGate = new Promise<void>((resolve) => {
    releaseWrite = resolve;
  });
  await click(button("Add hassle"));
  expect(rowAction("Edit Existing reminder").disabled).toBe(false);
  expect(titleInput().disabled).toBe(true);
  expect(button("Saving…").disabled).toBe(true);
  await act(async () => {
    releaseWrite();
  });
  await settle();
  expect(titleInput().disabled).toBe(false);
  expect(titleInput().value).toBe("");
});
it("replaces the deploy stamp for other reads without remounting its version query", async () => {
  await choose();
  const versionReads = () =>
    fixture.requests.filter((r) => new URL(r.path, "http://localhost").pathname === "/v1/version")
      .length;
  const before = versionReads();
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  const other = new QueryObserver(client, {
    queryKey: ["get", "/v1/mail"],
    queryFn: async () => {
      await gate;
      return { messages: [] };
    },
    staleTime: Infinity,
  });
  const stop = other.subscribe(() => {});
  await settle();
  expect(document.querySelector('header [role="status"]')!.textContent).toBe("Reading corpus…");
  expect(document.querySelector('header a[href*="/commit/"]')).toBeNull();
  await act(async () => {
    release();
  });
  await settle();
  expect(document.querySelector('header [role="status"]')).toBeNull();
  expect(document.querySelector('header a[href*="/commit/"]')).not.toBeNull();
  expect(versionReads()).toBe(before);
  stop();
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

it("completes only the displayed instance, refreshes rather than removes the card, and keeps pencil editing", async () => {
  await choose();
  await type(titleInput(), "Instance bill");
  await click(button("Add hassle"));
  const card = button("Edit").closest("li")!;
  const before = card.textContent;
  const single = button("This instance only:");
  expect(single.getAttribute("aria-label")).toBe("This instance only: Instance bill");
  expect(single.title).toBe("This instance only: Instance bill");
  const singleIcon = single.querySelector("svg")!;
  const allIcon = button("All instances:").querySelector("svg")!;
  expect(singleIcon.classList.contains("tabler-icon-check")).toBe(true);
  expect(singleIcon.classList.contains("tabler-icon-checks")).toBe(false);
  expect(allIcon.classList.contains("tabler-icon-checks")).toBe(true);
  for (const icon of [singleIcon, allIcon]) {
    expect(icon.getAttribute("stroke-width")).toBe("2");
    expect(icon.getAttribute("aria-hidden")).toBe("true");
    expect(icon.getAttribute("stroke-linecap")).toBe("round");
    expect(icon.getAttribute("stroke-linejoin")).toBe("round");
  }
  let release!: () => void;
  writeGate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await click(single);
  expect(button("This instance only:").disabled).toBe(true);
  expect(button("All instances:").disabled).toBe(true);
  expect(button("Edit").disabled).toBe(true);
  release();
  writeGate = null;
  await settle();
  await settle();
  const request = fixture.requests
    .filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"))
    .at(-1)!.body as Record<string, unknown>;
  expect(request.action).toBe("done-instance");
  expect(request.dryRun).toBe(false);
  expect(request.instanceAt).toBeTruthy();
  expect(request.instanceToken).toBeTruthy();
  expect(button("Edit").closest("li")!.textContent).not.toBe(before);
  expect(
    document.querySelector('[aria-label="Dismiss: Completed this instance only"]'),
  ).not.toBeNull();
  await click(button("Edit"));
  expect(document.querySelector('[role="dialog"] textarea')).not.toBeNull();
});

it("disables only instance completion for an owned ambiguous schedule and explains why", async () => {
  await choose();
  fixture.handle("/__hassles_fixture", "POST", {
    tasks: [
      {
        id: "task:ambiguous",
        title: "Ambiguous bill",
        revision: "1",
        resources: 2,
        writable: true,
        restriction: "Multiple resources",
        schedule: null,
        instance: null,
        instanceRestriction: "This instance only unavailable: ambiguous occurrence",
        reminder: "Unknown",
        timing: { state: "unknown", next: null, last: null, note: "Unsupported" },
      },
    ],
  });
  await click(refresh());
  expect(button("This instance only:").disabled).toBe(true);
  expect(button("This instance only:").title).toContain("ambiguous occurrence");
  expect(button("All instances:").disabled).toBe(false);
  expect(button("Edit").disabled).toBe(true);
  await click(button("All instances:"));
  await click(button("Remove all reminders"));
  expect(document.body.textContent).not.toContain("Ambiguous bill");
});

it("does not retry a failed instance completion and refreshes its inventory", async () => {
  await choose();
  await type(titleInput(), "Stale instance");
  await click(button("Add hassle"));
  fixture.handle("/__hassles_fixture", "POST", { mode: "conflict" });
  const reads = fixture.requests.filter(
    (r) => r.method === "GET" && r.path.includes("/v1/hassles?"),
  ).length;
  await click(button("This instance only:"));
  await settle();
  const writes = fixture.requests.filter(
    (r) => (r.body as { action?: string } | undefined)?.action === "done-instance",
  );
  expect(writes).toHaveLength(1);
  expect(document.body.textContent).toContain("Calendar changed");
  expect(document.body.textContent).toContain("Stale instance");
  expect(
    fixture.requests.filter((r) => r.method === "GET" && r.path.includes("/v1/hassles?")).length,
  ).toBeGreaterThan(reads);
});

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
function rowAction(label: string) {
  return document.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`)!;
}
function writes(action?: string) {
  return fixture.requests.filter(
    (r) =>
      r.method === "POST" &&
      new URL(r.path, "http://localhost").pathname === "/v1/hassles" &&
      (!action || (r.body as { action: string }).action === action),
  );
}
async function navigateAwayAndBack() {
  await act(async () =>
    root.render(createElement(QueryClientProvider, { client }, createElement("p", null, "Away"))),
  );
  await act(async () =>
    root.render(createElement(QueryClientProvider, { client }, hassleScreen())),
  );
  await settle();
}

it("projects edit before processing and independently creates and completes another row", async () => {
  await choose();
  await add("Alpha");
  await add("Beta");
  const edit = deferred();
  const complete = deferred();
  processing = async (_request, body) => {
    if (body.action === "edit") await edit.promise;
    if (body.action === "done-instance") await complete.promise;
  };
  await click(rowAction("Edit Alpha"));
  await type(
    document.querySelector<HTMLTextAreaElement>('[role="dialog"] textarea')!,
    "Alpha edited",
  );
  await click(button("Save reminder"));
  expect(document.querySelector('[role="dialog"][aria-modal="true"]')).toBeNull();
  expect(rowAction("Edit Alpha edited").disabled).toBe(true);
  expect(rowAction("Edit Beta").disabled).toBe(false);
  expect(titleInput().disabled).toBe(false);
  expect(writes("edit")).toHaveLength(0); // processing, not just response delivery, is gated
  await click(rowAction("This instance only: Beta"));
  expect(rowAction("Edit Beta").closest("li")!.textContent).toContain("Updating next reminder…");
  expect(rowAction("Edit Beta").closest("li")!.textContent).not.toContain("Tomorrow");
  await add("Gamma");
  expect(rowAction("Edit Gamma").disabled).toBe(false);
  expect(rowAction("Edit Alpha edited").disabled).toBe(true);
  complete.resolve();
  await settle();
  expect(rowAction("Edit Beta").disabled).toBe(false);
  expect(rowAction("Edit Alpha edited").disabled).toBe(true);
  edit.resolve();
  await settle();
  expect(rowAction("Edit Alpha edited").disabled).toBe(false);
  expect(writes("edit")).toHaveLength(1);
  expect(writes("done-instance")).toHaveLength(1);
});

it("retains acknowledged removal after failed GET; retry refresh never resends POST", async () => {
  await choose();
  await add("Remove me");
  const response = deferred();
  delivery = async (_request, body) => {
    if (body.action === "done") await response.promise;
  };
  await click(rowAction("All instances: Remove me"));
  await click(button("Cancel"));
  expect(writes("done")).toHaveLength(0);
  await click(rowAction("All instances: Remove me"));
  await click(button("Remove all reminders"));
  expect(rowAction("Edit Remove me")).toBeNull();
  expect(titleInput().disabled).toBe(false);
  fixture.handle("/__hassles_fixture", "POST", { mode: "inventory-error" });
  response.resolve();
  await settle();
  expect(document.body.textContent).toContain("Saved; awaiting Calendar refresh");
  expect(rowAction("Edit Remove me")).toBeNull();
  expect(writes("done")).toHaveLength(1);
  fixture.handle("/__hassles_fixture", "POST", { mode: "normal" });
  await click(button("Retry refresh"));
  expect(writes("done")).toHaveLength(1);
  expect(document.body.textContent).not.toContain("Saved; awaiting Calendar refresh");
  expect(rowAction("Edit Remove me")).toBeNull();
});

it("persists a lost-response create's exact UUID/payload as Add across remount", async () => {
  await choose();
  const response = deferred();
  delivery = async (_request, body) => {
    if (body.action === "create") {
      await response.promise;
      throw new TypeError("Lost response");
    }
  };
  await type(titleInput(), "Uncertain create");
  await click(button("Add hassle"));
  const original = structuredClone(writes("create")[0]!.body);
  expect(rowAction("Edit Uncertain create").disabled).toBe(true);
  await navigateAwayAndBack();
  expect(button("Saving…").disabled).toBe(true);
  expect(titleInput().value).toBe("Uncertain create");
  response.resolve();
  await settle();
  delivery = null;
  await navigateAwayAndBack();
  expect(addForm().textContent).not.toContain("Edit hassle");
  expect(titleInput().value).toBe("Uncertain create");
  expect(titleInput().disabled).toBe(true); // attempted payload is immutable
  expect(button("Add hassle").disabled).toBe(false);
  await click(button("Add hassle"));
  expect(writes("create")).toHaveLength(2);
  expect(writes("create")[1]!.body).toEqual(original);
  expect([...document.querySelectorAll("h3")].map((el) => el.textContent)).toEqual([
    "Uncertain create",
  ]);
  expect(titleInput().value).toBe("");
});

it("keeps an intermediate read from retiring a later settle or stranding earlier success", async () => {
  await choose();
  await add("First");
  await add("Second");
  const firstResponse = deferred();
  const secondResponse = deferred();
  const firstRead = deferred();
  const secondRead = deferred();
  let reads = 0;
  delivery = async (request, body) => {
    if (body.action === "done-instance") {
      const task = body.id as string;
      const firstId = (writes("create")[0]!.body as { id: string }).id;
      await (task === `task:${firstId}` ? firstResponse.promise : secondResponse.promise);
    }
    if (request.method === "GET" && new URL(request.url).pathname === "/v1/hassles") {
      reads++;
      await (reads === 1 ? firstRead.promise : secondRead.promise);
    }
  };
  await click(rowAction("This instance only: First"));
  await click(rowAction("This instance only: Second"));
  firstResponse.resolve();
  await settle();
  expect(reads).toBe(1);
  secondResponse.resolve();
  await settle();
  expect(rowAction("Edit First").disabled).toBe(true);
  expect(rowAction("Edit Second").disabled).toBe(true);
  firstRead.resolve();
  await settle();
  expect(reads).toBe(2);
  expect(rowAction("Edit First").disabled).toBe(false);
  expect(rowAction("Edit Second").disabled).toBe(true);
  secondRead.resolve();
  await settle();
  expect(rowAction("Edit Second").disabled).toBe(false);
  const before = writes("done-instance").length;
  await click(rowAction("This instance only: Second"));
  expect(writes("done-instance")).toHaveLength(before + 1); // authoritative replacement witness
});

it("restores only failed edit draft without stealing an unrelated open editor", async () => {
  await choose();
  await add("Failed edit");
  await add("Other edit");
  const response = deferred();
  fixture.handle("/__hassles_fixture", "POST", { mode: "conflict" });
  delivery = async (_request, body) => {
    if (body.action === "edit") await response.promise;
  };
  await click(rowAction("Edit Failed edit"));
  await type(
    document.querySelector<HTMLTextAreaElement>('[role="dialog"] textarea')!,
    "Captured draft",
  );
  await click(button("Save reminder"));
  await click(rowAction("Edit Other edit"));
  response.resolve();
  await settle();
  expect(document.querySelector<HTMLTextAreaElement>('[role="dialog"] textarea')!.value).toBe(
    "Other edit",
  );
  expect(rowAction("Edit Failed edit").disabled).toBe(false);
  await click(button("Cancel"));
  await click(button("Restore submitted edit"));
  expect(document.querySelector<HTMLTextAreaElement>('[role="dialog"] textarea')!.value).toBe(
    "Captured draft",
  );
  const original = writes("edit")[0]!.body as { revision: string };
  fixture.handle("/__hassles_fixture", "POST", { mode: "normal" });
  await click(button("Save reminder"));
  expect((writes("edit")[1]!.body as { revision: string }).revision).toBe(original.revision);
});

it("blocks uncertain partial-delete witnesses until refresh exposes the actual remainder", async () => {
  await choose();
  await add("Partial delete");
  const inventory = fixture.handle("/v1/hassles", "GET").data as {
    tasks: { resources: number; revision: string }[];
  };
  inventory.tasks[0]!.resources = 2;
  await click(refresh());
  const oldRevision = inventory.tasks[0]!.revision;
  const response = deferred();
  delivery = async (_request, body) => {
    if (body.action === "done") await response.promise;
  };
  fixture.handle("/__hassles_fixture", "POST", { mode: "partial-delete" });
  await click(rowAction("All instances: Partial delete"));
  await click(button("Remove all reminders"));
  fixture.handle("/__hassles_fixture", "POST", { mode: "inventory-error" });
  response.resolve();
  await settle();
  expect(document.body.textContent).toContain("Removed 1 resource");
  expect(rowAction("All instances: Partial delete").disabled).toBe(true);
  expect(titleInput().disabled).toBe(false);
  fixture.handle("/__hassles_fixture", "POST", { mode: "normal" });
  await click(button("Retry refresh"));
  expect(rowAction("All instances: Partial delete").disabled).toBe(false);
  const remaining = fixture.handle("/v1/hassles", "GET").data as {
    tasks: { resources: number; revision: string }[];
  };
  expect(remaining.tasks[0]!.resources).toBe(1);
  expect(remaining.tasks[0]!.revision).not.toBe(oldRevision);
  await click(rowAction("All instances: Partial delete"));
  await click(button("Remove all reminders"));
  expect((writes("done")[1]!.body as { revision: string }).revision).toBe(
    remaining.tasks[0]!.revision,
  );
});

it("reserves a row synchronously against same-tick instance double clicks", async () => {
  await choose();
  await add("Rapid click");
  const response = deferred();
  delivery = async (_request, body) => {
    if (body.action === "done-instance") await response.promise;
  };
  const action = rowAction("This instance only: Rapid click");
  await act(async () => {
    action.click();
    action.click();
  });
  await settle();
  expect(writes("done-instance")).toHaveLength(1);
  expect(titleInput().disabled).toBe(false);
  response.resolve();
  await settle();
});

it("keeps selection protected after navigation and isolates late old-location callbacks", async () => {
  await choose();
  const response = deferred();
  delivery = async (_request, body) => {
    if (body.action === "create") await response.promise;
  };
  await type(titleInput(), "Old location pending");
  await click(button("Add hassle"));
  await act(async () =>
    root.render(createElement(QueryClientProvider, { client }, createElement(SettingsHassles))),
  );
  await settle();
  expect(
    document.querySelector<HTMLSelectElement>('section[aria-label="Reminder location"] select')!
      .disabled,
  ).toBe(true);
  expect(button("Use calendar").disabled).toBe(true);
  const other = { ...fixtureLocation, calendarId: "/other/" };
  fixture.handle("/v1/hassles/location", "POST", {
    location: other,
    previous: fixtureLocation,
    dedicated: true,
  });
  await client.invalidateQueries({ queryKey: ["get", "/v1/hassles/location"] });
  await act(async () =>
    root.render(createElement(QueryClientProvider, { client }, hassleScreen())),
  );
  await settle();
  await type(titleInput(), "New location draft");
  response.resolve();
  await settle();
  expect(titleInput().value).toBe("New location draft");
  expect(rowAction("Edit Old location pending")).toBeNull();
  expect(document.body.textContent).not.toContain("Old location pending:");
  delivery = null;
  await click(button("Add hassle"));
  expect((writes("create").at(-1)!.body as { location: unknown }).location).toEqual(other);
});

it("offers an explicitly separate new create rather than changing an uncertain request", async () => {
  await choose();
  fixture.handle("/__hassles_fixture", "POST", { mode: "read-only" });
  await add("Original failed draft");
  const original = writes("create")[0]!.body as { id: string; schedule: unknown };
  expect(titleInput().disabled).toBe(true);
  await click(button("Start a separate new hassle"));
  expect(titleInput().value).toBe("");
  expect(titleInput().disabled).toBe(false);
  fixture.handle("/__hassles_fixture", "POST", { mode: "normal" });
  await add("Separate create");
  expect((writes("create")[1]!.body as { id: string }).id).not.toBe(original.id);
  const replay = fixture.handle("/v1/hassles", "POST", {
    ...(writes("create")[1]!.body as object),
    schedule: original.schedule,
  });
  expect(replay.status).toBe(409);
});

it("blocks cached-calendar writes through pending selection and publishes only confirmed config before delayed readback", async () => {
  await choose();
  await add("Old selected task");
  const other = { ...fixtureLocation, calendarId: "/new-confirmed-calendar/" };
  fixture.handle("/__hassles_fixture", "POST", {
    calendars: [
      { id: fixtureLocation.calendarId, name: "Original calendar" },
      { id: other.calendarId, name: "New calendar" },
    ],
  });
  await act(async () =>
    root.render(createElement(QueryClientProvider, { client }, createElement(SettingsHassles))),
  );
  await settle();
  await select(
    field("Dedicated reminder calendar", document).querySelector("select")!,
    other.calendarId,
  );
  await click(document.querySelector<HTMLInputElement>('input[type="checkbox"]')!);
  const selectProcessing = deferred();
  const staleConfig = deferred();
  const newConfig = deferred();
  let reads = 0;
  processing = async (request) => {
    if (request.method === "POST" && new URL(request.url).pathname === "/v1/hassles/location")
      await selectProcessing.promise;
  };
  delivery = async (request) => {
    if (request.method === "GET" && new URL(request.url).pathname === "/v1/hassles/location") {
      reads++;
      await (reads === 1 ? staleConfig.promise : newConfig.promise);
    }
  };
  await click(button("Use calendar"));
  await act(async () =>
    root.render(createElement(QueryClientProvider, { client }, hassleScreen())),
  );
  await settle();
  expect(reads).toBe(1);
  expect(titleInput().disabled).toBe(true);
  expect(button("Saving…").disabled).toBe(true);
  expect(rowAction("Edit Old selected task").disabled).toBe(true);
  expect(rowAction("This instance only: Old selected task").disabled).toBe(true);
  expect(rowAction("All instances: Old selected task").disabled).toBe(true);
  expect(refresh().disabled).toBe(true);
  const before = writes().length;
  await type(titleInput(), "Blocked cached create");
  await act(async () => {
    rowAction("This instance only: Old selected task").click();
    addForm()
      .querySelector("form")!
      .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  });
  expect(writes()).toHaveLength(before);
  selectProcessing.resolve();
  await settle();
  expect(reads).toBeGreaterThanOrEqual(2); // onSaved readback remains gated
  expect(titleInput().disabled).toBe(false); // POST response itself confirms new config
  expect(rowAction("Edit Old selected task")).toBeNull();
  await add("Confirmed calendar create");
  expect((writes("create").at(-1)!.body as { location: unknown }).location).toEqual(other);
  staleConfig.resolve();
  await settle();
  expect(rowAction("Edit Old selected task")).toBeNull();
  newConfig.resolve();
  await settle();
  expect(rowAction("Edit Confirmed calendar create").disabled).toBe(false);
});

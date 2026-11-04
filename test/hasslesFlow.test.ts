// @vitest-environment jsdom
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import SettingsHassles from "../src/components/settings/SettingsHassles";
import HasslesView from "../src/components/hassles/HasslesView";
import { hasslesFixture, fixtureLocation } from "./hasslesFixture";

let root: Root;
let host: HTMLDivElement;
let client: QueryClient;
let fixture: ReturnType<typeof hasslesFixture>;
let inventoryGate: Promise<void> | null = null;
beforeEach(async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
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
    root.render(createElement(QueryClientProvider, { client }, createElement(HasslesView))),
  );
  await settle();
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  client.clear();
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
  await select(document.querySelectorAll("select")[0]!, "offline");
  await select(document.querySelectorAll("select")[1]!, fixtureLocation.calendarId);
  await click(document.querySelector<HTMLInputElement>('input[type="checkbox"]')!);
  await click(button("Use calendar"));
  await act(async () =>
    root.render(createElement(QueryClientProvider, { client }, createElement(HasslesView))),
  );
  await waitReady("Add hassle");
}
function addForm() {
  return document.querySelector('section[aria-label="Add hassle"]')!;
}
async function add(title: string) {
  await waitReady("Add hassle");
  const inputs = addForm().querySelectorAll("input");
  await type(inputs[0]!, title);
  await type(inputs[1]!, "2027-01-01T08:30");
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
  expect(addForm().querySelector("input")!.value).toBe("");
  await type(addForm().querySelector("input")!, "Unsaved next hassle");
  await click(button("Edit"));
  expect(document.activeElement).toBe(document.querySelector('[role="dialog"] input'));
  await act(async () =>
    document.activeElement!.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }),
    ),
  );
  await settle();
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(addForm().querySelector("input")!.value).toBe("Unsaved next hassle");
  await click(button("Edit"));
  await type(document.querySelector('[role="dialog"] input')!, "Edited bill");
  await click(button("Save reminder"));
  expect(document.body.textContent).toContain("Edited bill");
  expect(addForm().querySelector("input")!.value).toBe("Unsaved next hassle");
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
  expect(addForm().querySelector("input")!.value).toBe("Permission test");
  await click(button("Add hassle"));
  const last = fixture.requests
    .filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"))
    .at(-1)!.body as { id: string };
  expect(last.id).toBe(first.id);
  expect(document.body.textContent).toContain("Permission test");
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(addForm().querySelector("input")!.value).toBe("");
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
  await type(dialog.querySelectorAll("input")[0]!, "Repeat end test");
  await type(dialog.querySelectorAll("input")[1]!, "2027-01-01T08:30");
  await select(dialog.querySelectorAll("select")[0]!, "daily");
  expect(dialog.querySelectorAll('input[type="datetime-local"]')).toHaveLength(1);
  await select(dialog.querySelectorAll("select")[1]!, "date");
  await type(
    dialog.querySelectorAll<HTMLInputElement>('input[type="datetime-local"]')[1]!,
    "2027-02-01T08:30",
  );
  await click(button("Add hassle"));
  await click(button("Edit"));
  dialog = document.querySelector('[role="dialog"]')!;
  expect(dialog.querySelectorAll("select")[1]!.value).toBe("date");
  expect(dialog.querySelectorAll<HTMLInputElement>('input[type="datetime-local"]')[1]!.value).toBe(
    "2027-02-01T08:30",
  );
  await select(dialog.querySelectorAll("select")[1]!, "done");
  expect(dialog.querySelectorAll('input[type="datetime-local"]')).toHaveLength(1);
  await click(button("Save reminder"));
  const edit = fixture.requests
    .filter((r) => r.method === "POST" && r.path.endsWith("/v1/hassles"))
    .at(-1)!.body as { schedule: { until: string } };
  expect(edit.schedule.until).toBe("");
});
it("defaults to daily until Done and keeps the create draft/UUID through accessible tabs", async () => {
  await choose();
  expect(addForm().querySelectorAll("select")[0]!.value).toBe("daily");
  expect(addForm().querySelectorAll("select")[1]!.value).toBe("done");
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
  expect(addForm().querySelector("input")!.value).toBe("Keep across tabs");
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
  await click(button("Refresh reminders"));
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

// @vitest-environment jsdom
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import HassleList from "../src/components/hassles/HassleList";
import type { HassleTask } from "../src/lib/hassles/forms";

const task: HassleTask = {
  id: "task:card-layout",
  revision: "original-revision",
  title: "A long reminder title that needs the whole card rather than sharing its row with actions",
  reminder: "Timing unavailable",
  resources: 1,
  writable: true,
  restriction: "Original edit restriction",
  instanceRestriction: "",
  instance: { at: "2099-11-06T02:00:00Z", token: "original-instance-witness" },
  schedule: {
    title: "Original title",
    start: "2099-11-06T13:00",
    timeZone: "Australia/Melbourne",
    cadence: "daily",
    interval: 1,
    until: "",
    workHours: true,
  },
  timing: {
    state: "upcoming",
    next: "2099-11-06T02:00:00Z",
    last: null,
    note: "Original timing note",
  },
};

function card(value: HassleTask = task, busy = false, blocked = false) {
  const root = document.createElement("div");
  root.innerHTML = renderToStaticMarkup(
    createElement(HassleList, {
      tasks: [value],
      busy,
      blocked: new Set(blocked ? [value.id] : []),
      onEdit: () => {},
      onDone: () => {},
      onDoneInstance: () => {},
    }),
  );
  return root.querySelector("li")!;
}

it("gives the title a full-width row and puts all three controls beside the grey summary", () => {
  const row = card();
  const title = row.querySelector("[data-hassle-title]")!;
  const details = row.querySelector("[data-hassle-details]")!;
  const summary = row.querySelector("[data-hassle-summary]")!;
  const actions = row.querySelector("[data-hassle-actions]")!;
  expect(title.parentElement).toBe(row);
  expect(title.classList.contains("w-full")).toBe(true);
  expect(title.textContent?.trim()).toBe(task.title);
  expect(title.nextElementSibling).toBe(details);
  expect(summary.parentElement).toBe(details);
  expect(actions.parentElement).toBe(details);
  expect(
    [...actions.querySelectorAll("button")].map((button) => button.getAttribute("aria-label")),
  ).toEqual([
    `Edit ${task.title}`,
    `This instance only: ${task.title}`,
    `All instances: ${task.title}`,
  ]);
  expect(title.querySelector("button")).toBeNull();
  expect(summary.textContent).toContain("work");
  expect([...row.children].slice(2).map((node) => node.textContent)).toEqual([
    task.timing.note,
    task.restriction,
  ]);
});

it.each([
  {
    busy: true,
    blocked: false,
    writable: true,
    schedule: true,
    instance: true,
    disabled: [true, true, true],
  },
  {
    busy: false,
    blocked: true,
    writable: true,
    schedule: true,
    instance: true,
    disabled: [true, true, true],
  },
  {
    busy: false,
    blocked: false,
    writable: false,
    schedule: true,
    instance: true,
    disabled: [true, true, true],
  },
  {
    busy: false,
    blocked: false,
    writable: true,
    schedule: false,
    instance: true,
    disabled: [true, false, false],
  },
  {
    busy: false,
    blocked: false,
    writable: true,
    schedule: true,
    instance: false,
    disabled: [false, true, false],
  },
  {
    busy: false,
    blocked: false,
    writable: true,
    schedule: true,
    instance: true,
    disabled: [false, false, false],
  },
])("retains independent action restrictions %j", (value) => {
  const row = card(
    {
      ...task,
      writable: value.writable,
      schedule: value.schedule ? task.schedule : null,
      instance: value.instance ? task.instance : null,
    },
    value.busy,
    value.blocked,
  );
  expect([...row.querySelectorAll("button")].map((button) => button.disabled)).toEqual(
    value.disabled,
  );
});

// @vitest-environment jsdom
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, expect, it, vi } from "vitest";
import HassleList from "../src/components/hassles/HassleList";
import { defaultSchedule, type HassleTask } from "../src/lib/hassles/forms";

afterEach(() => vi.useRealTimers());

it.each([true, false, undefined, null])(
  "shows a work badge only for a known work-hours schedule (%s)",
  (workHours) => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-11-05T00:00:00Z"));
    const task: HassleTask = {
      id: "task:badge",
      title: "Pay invoice",
      revision: "1",
      resources: 1,
      writable: false,
      restriction: "",
      instance: null,
      instanceRestriction: "",
      reminder: "Next reminder",
      schedule: workHours === null ? null : { ...defaultSchedule(), workHours },
      timing: {
        state: "upcoming",
        next: "2026-11-06T04:00:00Z",
        last: null,
        note: "",
      },
    };
    const root = document.createElement("div");
    root.innerHTML = renderToStaticMarkup(
      createElement(HassleList, {
        tasks: [task],
        busy: false,
        onEdit: () => {},
        onDone: () => {},
        onDoneInstance: () => {},
      }),
    );
    const badges = [...root.querySelectorAll("[data-hassle-work] > span")];
    expect(badges).toHaveLength(workHours === true ? 1 : 0);
    expect(root.querySelector("h3")?.textContent).toBe("Pay invoice");
    expect(root.textContent).not.toContain("Mon–Fri 9–5");
    if (badges[0]) {
      expect(badges[0].className).toContain("rounded-full");
      expect(badges[0].closest("[data-hassle-heading]")).not.toBeNull();
      expect(root.querySelector("[data-hassle-summary]")?.textContent).not.toContain("work");
    }
  },
);

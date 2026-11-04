import { expect, it } from "vitest";
import type { HassleTask } from "../src/lib/hassles/forms";
import { hassleGroups } from "../src/lib/hassles/groups";

const now = Date.parse("2026-11-04T00:00:00Z");
function task(id: string, state: HassleTask["timing"]["state"], date: string | null): HassleTask {
  return {
    id,
    title: id,
    revision: "1",
    resources: 1,
    writable: false,
    restriction: "Imported timezone definition: schedule editing unavailable",
    schedule: null,
    reminder: "A display string is not schedule state",
    timing: {
      state,
      next: state === "upcoming" ? date : null,
      last: state === "past" ? date : null,
      note: state === "unknown" ? "Unavailable" : "",
    },
  };
}
it("groups chronological next occurrences by Melbourne day, with undated schedules last", () => {
  const groups = hassleGroups(
    [
      task("undated", "unknown", null),
      task("later", "upcoming", "2026-11-04T23:00:00Z"),
      task("first", "upcoming", "2026-11-04T01:00:00Z"),
      task("second", "upcoming", "2026-11-04T12:59:00Z"),
      task("next-day", "upcoming", "2026-11-04T13:00:00Z"),
      task("ended", "past", "2026-11-03T01:00:00Z"),
    ],
    "upcoming",
    now,
  );
  expect(groups.map((g) => g.key)).toEqual(["2026-11-04", "2026-11-05", "undated"]);
  expect(groups.map((g) => g.tasks.map((t) => t.id))).toEqual([
    ["first", "second"],
    ["next-day", "later"],
    ["undated"],
  ]);
  expect(groups[0]!.label).toContain("4 Nov 2026");
});
it("shows only ended reminders from the last 30 days, most recent first", () => {
  const groups = hassleGroups(
    [
      task("old", "past", "2026-10-04T23:59:59Z"),
      task("cutoff", "past", "2026-10-05T00:00:00Z"),
      task("recent", "past", "2026-11-03T23:00:00Z"),
      task("earlier", "past", "2026-11-03T22:00:00Z"),
      task("ongoing", "upcoming", "2026-11-05T01:00:00Z"),
      task("unknown", "unknown", null),
    ],
    "past",
    now,
  );
  expect(groups.map((g) => g.tasks.map((t) => t.id))).toEqual([["recent", "earlier"], ["cutoff"]]);
});
it("uses Melbourne day boundaries on either side of the daylight-saving transition", () => {
  const groups = hassleGroups(
    [
      task("before", "upcoming", "2026-10-03T13:59:00Z"),
      task("midnight", "upcoming", "2026-10-03T14:00:00Z"),
      task("after", "upcoming", "2026-10-04T13:00:00Z"),
    ],
    "upcoming",
    now,
  );
  expect(groups.map((g) => g.key)).toEqual(["2026-10-03", "2026-10-04", "2026-10-05"]);
});

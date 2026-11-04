import { timeZone, type HassleTask } from "./forms";

export type HassleTab = "upcoming" | "past";
const dateKey = new Intl.DateTimeFormat("en-CA", {
  timeZone,
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});
const dayLabel = new Intl.DateTimeFormat("en-AU", {
  timeZone,
  weekday: "short",
  day: "numeric",
  month: "short",
  year: "numeric",
});

export function hassleGroups(tasks: HassleTask[], tab: HassleTab, now = Date.now()) {
  const entries = tasks
    .filter((task) => {
      if (tab === "upcoming") return task.timing.state !== "past";
      const last = task.timing.last ? Date.parse(task.timing.last) : NaN;
      return task.timing.state === "past" && last >= now - 30 * 86400000 && last < now;
    })
    .map((task) => ({
      task,
      instant: Date.parse((tab === "past" ? task.timing.last : task.timing.next) ?? ""),
    }))
    .sort((a, b) => {
      const aDated = Number.isFinite(a.instant);
      const bDated = Number.isFinite(b.instant);
      if (aDated !== bDated) return aDated ? -1 : 1;
      if (aDated && a.instant !== b.instant)
        return tab === "past" ? b.instant - a.instant : a.instant - b.instant;
      return a.task.id.localeCompare(b.task.id);
    });
  const groups: { key: string; label: string; tasks: HassleTask[] }[] = [];
  for (const { task, instant } of entries) {
    const dated = Number.isFinite(instant);
    const key = dated ? dateKey.format(instant) : "undated";
    let group = groups.at(-1);
    if (group?.key !== key) {
      group = {
        key,
        label: dated ? dayLabel.format(instant) : "Date unavailable · view in calendar",
        tasks: [],
      };
      groups.push(group);
    }
    group!.tasks.push(task);
  }
  return groups;
}

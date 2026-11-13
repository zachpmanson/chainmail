import { intervalUnit, timeZone, wallInstant, type HassleTask } from "./forms";

const dayKey = new Intl.DateTimeFormat("en-CA", {
  timeZone,
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});
const clock = new Intl.DateTimeFormat("en-AU", {
  timeZone,
  hour: "numeric",
  minute: "2-digit",
  hour12: true,
});

function dateTime(instant: number, now: number, allDay = false): string {
  const day = dayKey.format(instant);
  const today = dayKey.format(now);
  const tomorrow = new Date(`${today}T12:00:00Z`);
  tomorrow.setUTCDate(tomorrow.getUTCDate() + 1);
  const label =
    day === today
      ? "Today"
      : day === tomorrow.toISOString().slice(0, 10)
        ? "Tomorrow"
        : new Intl.DateTimeFormat("en-AU", {
            timeZone,
            day: "numeric",
            month: "short",
            ...(day.slice(0, 4) !== today.slice(0, 4) ? { year: "numeric" } : {}),
          }).format(instant);
  return `${label}, ${allDay ? "All day" : clock.format(instant)}`;
}

export function hassleSummary(task: HassleTask, now = Date.now()): string {
  const instant = Date.parse(
    (task.timing.state === "past" ? task.timing.last : task.timing.next) ?? "",
  );

  if (!Number.isFinite(instant) && !task.displaySchedule) return task.reminder;
  const prefix = task.timing.state === "past" ? "Last: " : task.timing.note ? "Known next: " : "";
  const allDay = task.timing.state === "past" ? task.timing.lastAllDay : task.timing.nextAllDay;
  const parts = [
    Number.isFinite(instant) ? prefix + dateTime(instant, now, allDay) : task.reminder,
  ];
  const schedule = task.displaySchedule ?? task.schedule;
  if (schedule && schedule.cadence !== "once") {
    parts.push(
      schedule.cadence === "weekdays"
        ? "Every weekday"
        : `Every ${schedule.interval === 1 ? "" : `${schedule.interval} `}${intervalUnit(schedule.cadence, schedule.interval)}`,
    );
    const until = wallInstant(schedule.until);
    if (until !== null) parts.push(`Until ${dateTime(until, now)}`);
  }
  return parts.join(" · ");
}

import { Tabs } from "@base-ui/react/tabs";
import { IconCheck, IconChecks, IconPencil, IconTrash } from "@tabler/icons-react";
import { isNative, isLegacy } from "../../lib/hassles/native";
import type { HassleTask } from "../../lib/hassles/forms";
import { hassleGroups, type HassleTab } from "../../lib/hassles/groups";
import { hassleSummary } from "../../lib/hassles/summary";
import IconButton from "../ui/IconButton";
import StatusBadge from "../ui/StatusBadge";
import { buttonClasses } from "../ui/styles";

export default function HassleList({
  tasks,
  busy,
  blocked = new Set<string>(),
  onEdit,
  onDone,
  onDoneInstance,
}: {
  tasks: HassleTask[];
  busy: boolean;
  blocked?: Set<string>;
  onEdit: (task: HassleTask) => void;
  onDone: (task: HassleTask) => void;
  onDoneInstance: (task: HassleTask) => void;
}) {
  return (
    <Tabs.Root defaultValue="upcoming">
      <Tabs.List aria-label="Reminder schedule" className="flex gap-2">
        {(["upcoming", "past"] as const).map((tab) => (
          <Tabs.Tab
            key={tab}
            value={tab}
            className={buttonClasses(
              "secondary",
              "normal",
              "aria-selected:border-accent aria-selected:text-accent",
            )}
          >
            {tab === "upcoming" ? "Upcoming" : "Ended"}
          </Tabs.Tab>
        ))}
      </Tabs.List>
      {(["upcoming", "past"] as HassleTab[]).map((tab) => {
        const groups = hassleGroups(tasks, tab);
        return (
          <Tabs.Panel key={tab} value={tab}>
            {groups.length === 0 ? (
              <p>
                {tab === "past"
                  ? "No ended reminders in the last 30 days."
                  : "No hassles upcoming."}
              </p>
            ) : null}
            {groups.map((group) => (
              <section
                key={group.key}
                aria-label={group.label}
                className="mb-3 w-full max-w-[40rem] min-w-0"
              >
                <h2 className="text-xs font-medium text-muted">{group.label}</h2>
                <ul className="m-0 flex list-none flex-col gap-3 p-0">
                  {group.tasks.map((task) => (
                    <li key={task.id} className="min-w-0 rounded-md border border-line bg-card p-4">
                      <div data-hassle-heading className="flex min-w-0 items-start gap-2">
                        <h3
                          data-hassle-title
                          className="m-0 min-w-0 flex-1 text-base wrap-break-word"
                        >
                          {task.title}
                        </h3>
                        {(task.displaySchedule ?? task.schedule)?.workHours ? (
                          <span data-hassle-work className="shrink-0">
                            <StatusBadge tone="neutral">work</StatusBadge>
                          </span>
                        ) : null}
                      </div>
                      <div data-hassle-details className="mt-1 flex min-w-0 items-center gap-3">
                        <p
                          data-hassle-summary
                          className="m-0 flex min-w-0 flex-1 flex-wrap items-center gap-2 text-sm text-muted"
                        >
                          <span className="min-w-0 wrap-break-word">{hassleSummary(task)}</span>
                        </p>
                        <div data-hassle-actions className="flex shrink-0 items-center gap-3">
                          {isLegacy(task) ? (
                            <>
                              <IconButton
                                aria-label={`Edit ${task.title}`}
                                title="Edit"
                                disabled={
                                  busy || blocked.has(task.id) || !task.writable || !task.schedule
                                }
                                onClick={() => onEdit(task)}
                              >
                                <IconPencil aria-hidden="true" />
                              </IconButton>
                              <IconButton
                                aria-label={`This instance only: ${task.title}`}
                                title={
                                  task.instance
                                    ? `This instance only: ${task.title}`
                                    : `${task.title}: ${task.instanceRestriction || "This instance only unavailable"}`
                                }
                                disabled={
                                  busy || blocked.has(task.id) || !task.writable || !task.instance
                                }
                                onClick={() => onDoneInstance(task)}
                              >
                                <IconCheck aria-hidden="true" />
                              </IconButton>
                              <IconButton
                                aria-label={`All instances: ${task.title}`}
                                title={`All instances: ${task.title}`}
                                disabled={busy || blocked.has(task.id) || !task.writable}
                                onClick={() => onDone(task)}
                              >
                                <IconChecks aria-hidden="true" />
                              </IconButton>
                            </>
                          ) : isNative(task) ? (
                            <IconButton
                              aria-label={`Delete ${task.title}`}
                              title={`Delete ${task.title}`}
                              disabled={
                                busy ||
                                blocked.has(task.id) ||
                                task.groupStatus !== "ready" ||
                                !task.capabilities?.delete
                              }
                              onClick={() => onDone(task)}
                            >
                              <IconTrash aria-hidden="true" />
                            </IconButton>
                          ) : null}
                        </div>
                      </div>
                      {task.timing.note ? (
                        <p className="my-1 text-xs wrap-break-word text-muted">
                          {task.timing.note}
                        </p>
                      ) : null}
                      {task.restriction ? (
                        <p className="my-1 text-xs wrap-break-word text-muted">
                          {task.restriction}
                        </p>
                      ) : null}
                    </li>
                  ))}
                </ul>
              </section>
            ))}
          </Tabs.Panel>
        );
      })}
    </Tabs.Root>
  );
}

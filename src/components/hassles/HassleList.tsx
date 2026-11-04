import { Tabs } from "@base-ui/react/tabs";
import { CheckIcon } from "@heroicons/react/24/outline";
import type { HassleTask } from "../../lib/hassles/forms";
import { hassleGroups, type HassleTab } from "../../lib/hassles/groups";
import { hassleSummary } from "../../lib/hassles/summary";
import Button from "../ui/Button";
import IconButton from "../ui/IconButton";
import { buttonClasses } from "../ui/styles";

export default function HassleList({
  tasks,
  busy,
  onEdit,
  onDone,
}: {
  tasks: HassleTask[];
  busy: boolean;
  onEdit: (task: HassleTask) => void;
  onDone: (task: HassleTask) => void;
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
              <section key={group.key} aria-label={group.label} className="mb-3">
                <h2 className="text-xs font-medium text-muted">{group.label}</h2>
                <ul className="m-0 flex list-none flex-col gap-3 p-0">
                  {group.tasks.map((task) => (
                    <li
                      key={task.id}
                      className="flex flex-wrap items-center gap-3 rounded-md border border-line p-4"
                    >
                      <div className="min-w-0 flex-1 wrap-break-word">
                        <h3 className="m-0 text-base">
                          <Button
                            variant="bare"
                            className="max-w-full text-left wrap-break-word enabled:hover:underline"
                            aria-label={`Edit ${task.title}`}
                            disabled={busy || !task.writable || !task.schedule}
                            onClick={() => onEdit(task)}
                          >
                            {task.title}
                          </Button>
                        </h3>
                        <p className="my-1 text-sm text-muted">{hassleSummary(task)}</p>
                        {task.timing.note ? (
                          <p className="my-1 text-xs text-muted">{task.timing.note}</p>
                        ) : null}
                        {task.restriction ? (
                          <p className="my-1 text-xs text-muted">{task.restriction}</p>
                        ) : null}
                      </div>
                      <IconButton
                        aria-label={`Done with ${task.title}`}
                        title="Done"
                        disabled={busy || !task.writable}
                        onClick={() => onDone(task)}
                      >
                        <CheckIcon aria-hidden="true" />
                      </IconButton>
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

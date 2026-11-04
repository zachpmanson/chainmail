import { Tabs } from "@base-ui/react/tabs";
import type { HassleTask } from "../../lib/hassles/forms";
import { hassleGroups, type HassleTab } from "../../lib/hassles/groups";
import Button from "../ui/Button";
import { buttonClasses } from "../ui/styles";

export default function HassleList({
  tasks,
  busy,
  onEdit,
  onRemove,
}: {
  tasks: HassleTask[];
  busy: boolean;
  onEdit: (task: HassleTask) => void;
  onRemove: (task: HassleTask, action: "done" | "delete") => void;
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
            {tab === "upcoming" ? "Upcoming" : "Past"}
          </Tabs.Tab>
        ))}
      </Tabs.List>
      {(["upcoming", "past"] as HassleTab[]).map((tab) => {
        const groups = hassleGroups(tasks, tab);
        return (
          <Tabs.Panel key={tab} value={tab}>
            <p className="text-xs text-muted">
              {tab === "past"
                ? "Last 30 days · ended reminders only · Australia/Melbourne dates"
                : "Next scheduled reminders · Australia/Melbourne dates. Unknown schedules stay here until verified in Calendar."}
            </p>
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
                        <h3 className="m-0 text-base">{task.title}</h3>
                        <p className="my-1 text-sm text-muted">{task.reminder}</p>
                        {task.timing.note ? (
                          <p className="my-1 text-xs text-muted">{task.timing.note}</p>
                        ) : null}
                        {task.restriction ? (
                          <p className="my-1 text-xs text-muted">{task.restriction}</p>
                        ) : null}
                      </div>
                      <div className="flex flex-wrap gap-2">
                        <Button
                          disabled={busy || !task.writable || !task.schedule}
                          onClick={() => onEdit(task)}
                        >
                          Edit
                        </Button>
                        <Button
                          disabled={busy || !task.writable}
                          onClick={() => onRemove(task, "done")}
                        >
                          Done
                        </Button>
                        <Button
                          variant="danger"
                          disabled={busy || !task.writable}
                          onClick={() => onRemove(task, "delete")}
                        >
                          Delete
                        </Button>
                      </div>
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

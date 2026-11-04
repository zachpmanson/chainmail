import { useState } from "react";
import { $api } from "../../lib/api/api";
import { type HassleLocation, type HassleTask } from "../../lib/hassles/forms";
import { errText } from "../../lib/ui/errText";
import Button from "../ui/Button";
import DialogShell from "../ui/DialogShell";
import InlineAlert from "../ui/InlineAlert";
import HassleForm from "./HassleForm";

type Editor = { id: string; task?: HassleTask };
export default function HassleInventory({
  location,
  disabled,
}: {
  location: HassleLocation;
  disabled: boolean;
}) {
  const inventory = $api.useQuery(
    "get",
    "/v1/hassles",
    { params: { query: location } },
    { retry: false, staleTime: 0 },
  );
  const [editor, setEditor] = useState<Editor | null>(null);
  const [removing, setRemoving] = useState<{ task: HassleTask; action: "done" | "delete" } | null>(
    null,
  );
  const [notice, setNotice] = useState("");
  const write = $api.useMutation("post", "/v1/hassles", {
    retry: false,
    onSuccess: (result) => {
      setEditor(null);
      setRemoving(null);
      setNotice(result.message);
    },
    onSettled: () => {
      void inventory.refetch();
    },
  });
  const busy = disabled || write.isPending || inventory.isFetching || inventory.isError;
  return (
    <section aria-label="Hassle inventory" className="flex flex-col gap-3">
      <div className="flex flex-wrap gap-2">
        <Button
          disabled={busy}
          onClick={() => {
            write.reset();
            setEditor({ id: crypto.randomUUID() });
          }}
        >
          Add hassle
        </Button>
        <Button
          disabled={disabled || write.isPending || inventory.isFetching}
          onClick={() => {
            write.reset();
            void inventory.refetch();
          }}
        >
          Refresh reminders
        </Button>
      </div>
      <p className="m-0 text-xs text-muted">
        Done and Delete both remove all reminder resources permanently, with no completed history.
        Notifications and recurrence belong to your calendar, not an app scheduler.
      </p>
      {inventory.isError ? <InlineAlert>{errText(inventory.error)}</InlineAlert> : null}
      {write.isError && !editor ? <InlineAlert>{errText(write.error)}</InlineAlert> : null}
      <p role="status" className="m-0 text-sm">
        {inventory.isFetching ? "Reading reminders…" : notice}
      </p>
      {inventory.data?.tasks.length === 0 ? <p>No hassles in this calendar.</p> : null}
      <ul className="m-0 flex list-none flex-col gap-3 p-0">
        {inventory.data?.tasks.map((task) => (
          <li
            key={task.id}
            className="flex flex-wrap items-center gap-3 rounded-md border border-line p-4"
          >
            <div className="min-w-0 flex-1 wrap-break-word">
              <h2 className="m-0 text-base">{task.title}</h2>
              <p className="my-1 text-sm text-muted">{task.reminder}</p>
              {task.restriction ? (
                <p className="my-1 text-xs text-muted">{task.restriction}</p>
              ) : null}
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                disabled={busy || !task.writable || !task.schedule}
                onClick={() => {
                  write.reset();
                  setEditor({ id: task.id, task });
                }}
              >
                Edit
              </Button>
              <Button
                disabled={busy || !task.writable}
                onClick={() => {
                  write.reset();
                  setRemoving({ task, action: "done" });
                }}
              >
                Done
              </Button>
              <Button
                variant="danger"
                disabled={busy || !task.writable}
                onClick={() => {
                  write.reset();
                  setRemoving({ task, action: "delete" });
                }}
              >
                Delete
              </Button>
            </div>
          </li>
        ))}
      </ul>
      {editor ? (
        <HassleForm
          initial={editor.task?.schedule ?? undefined}
          busy={disabled || write.isPending || inventory.isFetching}
          error={write.isError ? errText(write.error) : ""}
          onClose={() => setEditor(null)}
          onSave={(schedule) =>
            write.mutate({
              body: {
                location,
                action: editor.task ? "edit" : "create",
                id: editor.id,
                revision: editor.task?.revision,
                schedule,
              },
            })
          }
        />
      ) : null}
      {removing ? (
        <DialogShell
          label="Remove hassle"
          onClose={() => setRemoving(null)}
          closeDisabled={write.isPending}
          panelClassName="max-w-full p-5"
        >
          <h2 className="mt-0 text-lg">
            {removing.action === "done" ? "Done with" : "Delete"} {removing.task.title}?
          </h2>
          <p>
            Permanently remove all {removing.task.resources} calendar resources. This cannot be
            undone.
          </p>
          <div className="flex justify-end gap-2">
            <Button disabled={write.isPending} onClick={() => setRemoving(null)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              disabled={busy}
              onClick={() => {
                const { task, action } = removing;
                setRemoving(null);
                write.mutate({ body: { location, action, id: task.id, revision: task.revision } });
              }}
            >
              Remove all reminders
            </Button>
          </div>
        </DialogShell>
      ) : null}
    </section>
  );
}

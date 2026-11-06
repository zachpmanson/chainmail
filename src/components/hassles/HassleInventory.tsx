import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, useSyncExternalStore } from "react";
import { type HassleLocation, type HassleTask } from "../../lib/hassles/forms";
import { hassleOperations, projectTasks } from "../../lib/hassles/operations";
import { errText } from "../../lib/ui/errText";
import Button from "../ui/Button";
import DialogShell from "../ui/DialogShell";
import InlineAlert from "../ui/InlineAlert";
import HassleForm from "./HassleForm";
import HassleList from "./HassleList";

type Editor = { task: HassleTask };
export default function HassleInventory({
  location,
  disabled,
}: {
  location: HassleLocation;
  disabled: boolean;
}) {
  const store = hassleOperations(useQueryClient(), location);
  const state = useSyncExternalStore(store.subscribe, store.getSnapshot, store.getSnapshot);
  const inventory = useQuery(store.options);
  const [editor, setEditor] = useState<Editor | null>(null);
  const [removing, setRemoving] = useState<HassleTask | null>(null);
  const createBusy =
    disabled ||
    state.create.writing ||
    (inventory.isError && !inventory.data && !state.create.request);
  const busy = disabled || (inventory.isError && !inventory.data);
  const blocked = new Set(state.operations.filter((op) => op.blocked).map((op) => op.rowId));
  return (
    <section
      aria-label="Hassle inventory"
      className="grid grid-cols-[minmax(0,40rem)] items-start justify-center gap-6 @min-[83rem]:grid-cols-[40rem_40rem_minmax(0,40rem)]"
    >
      {/* Side by side needs two 40rem tracks and one gap: 83rem of the parent container. */}
      <div className="min-w-0">
        <HassleForm
          key={state.create.id}
          initial={state.create.request?.schedule}
          editing={false}
          locked={!!state.create.request}
          busy={createBusy}
          error={state.create.error}
          onSave={store.create}
        />
        {state.create.request && !state.create.writing ? (
          <>
            <p className="text-sm text-muted">
              Retry sends the exact original reminder. It may already be saved.
            </p>
            <Button onClick={store.newCreate}>Start a separate new hassle</Button>
          </>
        ) : null}
      </div>
      <section aria-label="Reminders" className="flex min-w-0 flex-col gap-3">
        {inventory.isError ? <InlineAlert>{errText(inventory.error)}</InlineAlert> : null}
        {state.operations.map((op) => (
          <div key={op.token} role="status" className="wrap-break-word">
            {op.status === "failed" ? (
              <>
                <InlineAlert>
                  {op.original?.title ?? op.request.schedule?.title}: {op.error}
                  {op.blocked
                    ? " Refresh required before using this reminder again."
                    : " Calendar refreshed; the write may have partially applied."}
                </InlineAlert>
                {op.request.action === "edit" && op.original && op.request.schedule ? (
                  <Button
                    onClick={() =>
                      setEditor({ task: { ...op.original!, schedule: op.request.schedule! } })
                    }
                  >
                    Restore submitted edit
                  </Button>
                ) : null}
              </>
            ) : (
              <span className="text-sm text-muted">
                {op.request.schedule?.title ?? op.original?.title}:{" "}
                {op.status === "writing" ? "Saving…" : "Saved; awaiting Calendar refresh…"}
              </span>
            )}
          </div>
        ))}
        {state.refreshError ? (
          <InlineAlert>
            Calendar refresh failed: {state.refreshError}. Saved changes will not be sent again.
          </InlineAlert>
        ) : null}
        {state.refreshError ? (
          <Button disabled={state.refreshing} onClick={store.refresh}>
            Retry refresh
          </Button>
        ) : null}
        <HassleList
          tasks={projectTasks(inventory.data?.tasks ?? [], state.operations)}
          busy={busy}
          blocked={blocked}
          onEdit={(task) => setEditor({ task })}
          onDoneInstance={(task) => {
            if (busy || blocked.has(task.id) || !task.writable || !task.instance) return;
            store.submit(
              {
                location,
                action: "done-instance",
                id: task.id,
                revision: task.revision,
                instanceAt: task.instance.at,
                instanceToken: task.instance.token,
                dryRun: false,
              },
              task,
            );
          }}
          onDone={setRemoving}
        />
      </section>
      {/* The empty right track centres the list. It shrinks to 0 before the layout stacks. */}
      <div className="hidden @min-[83rem]:block" />
      {editor ? (
        <DialogShell
          label="Edit hassle"
          onClose={() => setEditor(null)}
          panelClassName="box-border w-[min(34rem,95vw)] overflow-y-auto p-5"
        >
          <HassleForm
            initial={editor.task.schedule ?? undefined}
            busy={busy || blocked.has(editor.task.id)}
            error=""
            onClose={() => setEditor(null)}
            onSave={(schedule) => {
              const task = editor.task;
              if (
                store.submit(
                  { location, action: "edit", id: task.id, revision: task.revision, schedule },
                  task,
                )
              )
                setEditor(null);
            }}
          />
        </DialogShell>
      ) : null}
      {removing ? (
        <DialogShell
          label="Finish hassle"
          onClose={() => setRemoving(null)}
          panelClassName="max-w-full p-5"
        >
          <h2 className="mt-0 text-lg">Done with {removing.title}?</h2>
          <p>
            Permanently remove all reminders for this hassle from Calendar. This cannot be undone.
          </p>
          <div className="flex justify-end gap-2">
            <Button onClick={() => setRemoving(null)}>Cancel</Button>
            <Button
              variant="danger"
              disabled={busy || blocked.has(removing.id)}
              onClick={() => {
                const task = removing;
                if (
                  store.submit(
                    { location, action: "done", id: task.id, revision: task.revision },
                    task,
                  )
                )
                  setRemoving(null);
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

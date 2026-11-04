import { useState } from "react";
import { $api } from "../../lib/api/api";
import { type HassleLocation, type HassleTask } from "../../lib/hassles/forms";
import { errText } from "../../lib/ui/errText";
import Button from "../ui/Button";
import DialogShell from "../ui/DialogShell";
import InlineAlert from "../ui/InlineAlert";
import HassleForm from "./HassleForm";
import HassleList from "./HassleList";

type Editor = { id: string; task: HassleTask };
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
  const [createId, setCreateId] = useState(() => crypto.randomUUID());
  const [editor, setEditor] = useState<Editor | null>(null);
  const [removing, setRemoving] = useState<{ task: HassleTask; action: "done" | "delete" } | null>(
    null,
  );
  const [notice, setNotice] = useState("");
  const write = $api.useMutation("post", "/v1/hassles", {
    retry: false,
    onSuccess: (result, variables) => {
      if (variables.body.action === "create") setCreateId(crypto.randomUUID());
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
      <HassleForm
        key={createId}
        busy={busy}
        error={
          write.isError && write.variables?.body.action === "create" ? errText(write.error) : ""
        }
        onSave={(schedule) =>
          write.mutate({ body: { location, action: "create", id: createId, schedule } })
        }
      />
      <p className="m-0 text-xs text-muted">
        Done and Delete both remove all reminder resources permanently, with no completed history.
        Notifications and recurrence belong to your calendar, not an app scheduler.
      </p>
      {inventory.isError ? <InlineAlert>{errText(inventory.error)}</InlineAlert> : null}
      {write.isError && !editor && write.variables?.body.action !== "create" ? (
        <InlineAlert>{errText(write.error)}</InlineAlert>
      ) : null}
      <p role="status" className="m-0 text-sm">
        {inventory.isFetching ? "Reading reminders…" : notice}
      </p>
      <HassleList
        tasks={inventory.data?.tasks ?? []}
        busy={busy}
        onEdit={(task) => {
          write.reset();
          setEditor({ id: task.id, task });
        }}
        onRemove={(task, action) => {
          write.reset();
          setRemoving({ task, action });
        }}
      />
      {editor ? (
        <DialogShell
          label="Edit hassle"
          onClose={() => setEditor(null)}
          closeDisabled={write.isPending}
          panelClassName="w-[min(34rem,95vw)] overflow-y-auto p-5"
        >
          <HassleForm
            initial={editor.task.schedule ?? undefined}
            busy={disabled || write.isPending || inventory.isFetching}
            error={write.isError ? errText(write.error) : ""}
            onClose={() => setEditor(null)}
            onSave={(schedule) =>
              write.mutate({
                body: {
                  location,
                  action: "edit",
                  id: editor.id,
                  revision: editor.task.revision,
                  schedule,
                },
              })
            }
          />
        </DialogShell>
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

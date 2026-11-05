import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { $api } from "../../lib/api/api";
import { type HassleLocation, type HassleTask } from "../../lib/hassles/forms";
import { errText } from "../../lib/ui/errText";
import { pushToast, SAID_MS } from "../../lib/ui/toasts";
import Button from "../ui/Button";
import DialogShell from "../ui/DialogShell";
import InlineAlert from "../ui/InlineAlert";
import HassleForm from "./HassleForm";
import HassleList from "./HassleList";

type Editor = { id: string; task: HassleTask };
export default function HassleInventory({
  location,
  disabled,
  refreshing,
}: {
  location: HassleLocation;
  disabled: boolean;
  refreshing: boolean;
}) {
  const qc = useQueryClient();
  const inventoryOptions = $api.queryOptions(
    "get",
    "/v1/hassles",
    { params: { query: location } },
    { retry: false, staleTime: 0 },
  );
  const inventory = useQuery(inventoryOptions);
  const [createId, setCreateId] = useState(() => crypto.randomUUID());
  const [editor, setEditor] = useState<Editor | null>(null);
  const [removing, setRemoving] = useState<HassleTask | null>(null);
  const write = $api.useMutation("post", "/v1/hassles", {
    retry: false,
    onSuccess: (result, variables) => {
      if (variables.body.action === "create") setCreateId(crypto.randomUUID());
      setEditor(null);
      setRemoving(null);
      pushToast(result.message, "note", SAID_MS);
    },
    onSettled: () => {
      // An initial read can predate a create; cancel it before requesting the post-write list.
      void qc
        .cancelQueries({ queryKey: inventoryOptions.queryKey, exact: true })
        .then(() => inventory.refetch());
    },
  });
  const createBusy = disabled || write.isPending || inventory.isError;
  const busy = createBusy || refreshing || inventory.isFetching;
  return (
    <section
      aria-label="Hassle inventory"
      className="mx-auto grid w-full max-w-[40rem] items-start justify-center gap-6 min-[60rem]:max-w-none min-[60rem]:grid-cols-[minmax(0,450px)_minmax(0,40rem)_minmax(0,450px)]"
    >
      {/* The empty right track balances the form, centring the list itself. */}
      <div className="w-full max-w-[450px] min-w-0">
        <HassleForm
          key={createId}
          busy={createBusy}
          error={
            write.isError && write.variables?.body.action === "create" ? errText(write.error) : ""
          }
          onSave={(schedule) =>
            write.mutate({ body: { location, action: "create", id: createId, schedule } })
          }
        />
      </div>
      <section aria-label="Reminders" className="flex min-w-0 flex-col gap-3">
        {inventory.isError ? <InlineAlert>{errText(inventory.error)}</InlineAlert> : null}
        {write.isError && !editor && write.variables?.body.action !== "create" ? (
          <InlineAlert>{errText(write.error)}</InlineAlert>
        ) : null}
        <HassleList
          tasks={inventory.data?.tasks ?? []}
          busy={busy}
          onEdit={(task) => {
            write.reset();
            setEditor({ id: task.id, task });
          }}
          onDoneInstance={(task) => {
            if (busy || !task.writable || !task.instance) return;
            write.reset();
            write.mutate({
              body: {
                location,
                action: "done-instance",
                id: task.id,
                revision: task.revision,
                instanceAt: task.instance.at,
                instanceToken: task.instance.token,
                dryRun: false,
              },
            });
          }}
          onDone={(task) => {
            write.reset();
            setRemoving(task);
          }}
        />
      </section>
      {editor ? (
        <DialogShell
          label="Edit hassle"
          onClose={() => setEditor(null)}
          closeDisabled={write.isPending}
          panelClassName="w-[min(34rem,95vw)] overflow-y-auto p-5"
        >
          <HassleForm
            initial={editor.task.schedule ?? undefined}
            busy={disabled || refreshing || write.isPending || inventory.isFetching}
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
          label="Finish hassle"
          onClose={() => setRemoving(null)}
          closeDisabled={write.isPending}
          panelClassName="max-w-full p-5"
        >
          <h2 className="mt-0 text-lg">Done with {removing.title}?</h2>
          <p>
            Permanently remove all reminders for this hassle from Calendar. This cannot be undone.
          </p>
          <div className="flex justify-end gap-2">
            <Button disabled={write.isPending} onClick={() => setRemoving(null)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              disabled={busy}
              onClick={() => {
                const task = removing;
                setRemoving(null);
                write.mutate({
                  body: { location, action: "done", id: task.id, revision: task.revision },
                });
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

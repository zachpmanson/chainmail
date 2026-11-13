import { hashKey, QueryClient } from "@tanstack/react-query";
import { $api, fetchClient } from "../api/api";
import type { components } from "../api/api.d";
import { locationKey, type HassleLocation, type HassleTask } from "./forms";
import { errText } from "../ui/errText";
import { pushToast, SAID_MS } from "../ui/toasts";

export type HassleRequest = components["schemas"]["HassleMutation"];
export type Operation = {
  token: number;
  rowId: string;
  request: HassleRequest;
  original?: HassleTask;
  status: "writing" | "acknowledged" | "failed";
  blocked: boolean;
  error: string;
};
type CreateSlot = { id: string; request?: HassleRequest; writing: boolean; error: string };
type Snapshot = {
  operations: Operation[];
  create: CreateSlot;
  refreshError: string;
  refreshing: boolean;
};

export function projectTasks(base: HassleTask[], operations: Operation[]): HassleTask[] {
  const tasks = new Map(base.map((task) => [task.id, task]));
  for (const op of operations) {
    if (op.status === "failed") continue;
    const { request } = op;
    if (request.action === "done" || request.action === "delete") {
      tasks.delete(op.rowId);
      continue;
    }
    const task = tasks.get(op.rowId) ?? op.original;
    const schedule = request.schedule ?? task?.schedule ?? null;
    tasks.set(op.rowId, {
      id: op.rowId,
      title: request.title ?? schedule?.title ?? task?.title ?? "Reminder",
      schedule,
      revision: "",
      resources: task?.resources ?? 0,
      writable: false,
      restriction: "",
      instance: null,
      instanceRestriction: "Awaiting Calendar refresh",
      reminder:
        request.action === "done-instance"
          ? "Updating next reminder…"
          : "Timing awaiting Calendar…",
      timing: { state: "unknown", next: null, last: null, note: "" },
    });
  }
  return [...tasks.values()];
}

const stores = new WeakMap<QueryClient, Map<string, HassleOperations>>();
export function hassleOperations(client: QueryClient, location: HassleLocation): HassleOperations {
  let locations = stores.get(client);
  if (!locations) stores.set(client, (locations = new Map()));
  const key = locationKey(location);
  let store = locations.get(key);
  if (!store) locations.set(key, (store = new HassleOperations(client, location)));
  return store;
}

export class HassleOperations {
  private listeners = new Set<() => void>();
  private sequence = 0;
  private refreshRequested = 0;
  private refreshRunning = false;
  private snapshot: Snapshot = {
    operations: [],
    create: { id: crypto.randomUUID(), writing: false, error: "" },
    refreshError: "",
    refreshing: false,
  };
  readonly options;
  constructor(
    private client: QueryClient,
    private location: HassleLocation,
  ) {
    this.options = $api.queryOptions(
      "get",
      "/v1/hassles",
      { params: { query: location } },
      { retry: false, staleTime: 0 },
    );
    let cutoff = new Set<number>();
    // Query's retryer ignores cancelled responses. Capture eligibility at request start,
    // and retire overlays only after that query has committed its authoritative data.
    client.getQueryCache().subscribe((event) => {
      if (event.type !== "updated" || event.query.queryHash !== hashKey(this.options.queryKey))
        return;
      if (event.action.type === "fetch") {
        cutoff = new Set(
          this.snapshot.operations.filter((op) => op.status !== "writing").map((op) => op.token),
        );
      } else if (event.action.type === "success" && !event.action.manual) {
        this.update({
          operations: this.snapshot.operations.flatMap((op) =>
            !cutoff.has(op.token)
              ? [op]
              : op.status === "failed"
                ? [{ ...op, blocked: false }]
                : [],
          ),
          refreshError: "",
        });
      }
    });
  }
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  getSnapshot = () => this.snapshot;
  private update(patch: Partial<Snapshot>) {
    this.snapshot = { ...this.snapshot, ...patch };
    this.listeners.forEach((listener) => listener());
  }
  newCreate = () => {
    if (this.snapshot.create.writing) return;
    this.update({ create: { id: crypto.randomUUID(), writing: false, error: "" } });
  };
  create = (schedule: components["schemas"]["HassleSchedule"]) => {
    const slot = this.snapshot.create;
    if (slot.writing) return false;
    return this.submit(
      slot.request ?? { location: this.location, action: "create", id: slot.id, schedule },
    );
  };
  submit(request: HassleRequest, original?: HassleTask): boolean {
    if (
      this.client.isMutating({
        predicate: (mutation) =>
          mutation.options.mutationKey?.includes("/v1/hassles/location") ?? false,
      })
    )
      return false;
    const rowId =
      request.action === "create"
        ? `${request.schedule?.cadence === "hourly" ? "v2" : "task"}:${request.id.toLowerCase()}`
        : request.id;
    if (
      this.snapshot.operations.some(
        (op) =>
          op.rowId === rowId &&
          op.blocked &&
          !(op.status === "failed" && request.action === "create"),
      )
    )
      return false;
    if (request.action === "create" && this.snapshot.create.writing) return false;
    // Requests and witnesses are captured once; retries never acquire a newer revision.
    const captured = structuredClone(request);
    const operation: Operation = {
      token: ++this.sequence,
      rowId,
      request: captured,
      original: original ? structuredClone(original) : undefined,
      status: "writing",
      blocked: true,
      error: "",
    };
    this.update({
      operations: [...this.snapshot.operations.filter((op) => op.rowId !== rowId), operation],
      ...(request.action === "create"
        ? { create: { id: request.id, request: captured, writing: true, error: "" } }
        : {}),
    });
    const mutation = this.client.getMutationCache().build(this.client, {
      mutationKey: ["post", "/v1/hassles"],
      retry: false,
      mutationFn: async (body: HassleRequest) => {
        const result = await fetchClient.POST("/v1/hassles", { body });
        if (!result.data) throw new Error("Missing Calendar write acknowledgement");
        return result.data;
      },
      onSuccess: (result) => {
        this.settle(operation, "acknowledged");
        pushToast(result.message, "note", SAID_MS);
      },
      onError: (error) => this.settle(operation, "failed", errText(error)),
    });
    void mutation.execute(captured).catch(() => {});
    return true;
  }
  private settle(operation: Operation, status: "acknowledged" | "failed", error = "") {
    if (!this.snapshot.operations.some((op) => op.token === operation.token)) return;
    const slot = this.snapshot.create;
    this.update({
      operations: this.snapshot.operations.map((op) =>
        op.token === operation.token ? { ...op, status, error } : op,
      ),
      ...(operation.request.action === "create" && slot.id === operation.request.id
        ? {
            create:
              status === "acknowledged"
                ? { id: crypto.randomUUID(), writing: false, error: "" }
                : { ...slot, writing: false, error },
          }
        : {}),
    });
    this.refresh();
  }
  refresh = () => {
    this.refreshRequested++;
    if (!this.refreshRunning) void this.reconcile();
  };
  private async reconcile() {
    this.refreshRunning = true;
    this.update({ refreshing: true, refreshError: "" });
    while (true) {
      const requested = this.refreshRequested;
      await this.client.cancelQueries({ queryKey: this.options.queryKey, exact: true });
      try {
        await this.client.fetchQuery(this.options);
      } catch (error) {
        this.update({ refreshError: errText(error) });
      }
      if (requested === this.refreshRequested) break;
    }
    this.refreshRunning = false;
    this.update({ refreshing: false });
  }
}

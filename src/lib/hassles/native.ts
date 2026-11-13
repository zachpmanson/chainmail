import { QueryClient } from "@tanstack/react-query";
import { $api, ApiError, fetchClient } from "../api/api";
import type { components } from "../api/api.d";
import type { HassleLocation, HassleSchedule, HassleTask } from "./forms";
import { errText } from "../ui/errText";
import { pushToast } from "../ui/toasts";
import { fenceAdvances, parseRecovery, ready, settled } from "./recovery";
export { ready } from "./recovery";

type Recovery = components["schemas"]["HassleRecovery"];
export type GroupRequest = components["schemas"]["HassleLocatedGroupRequest"];
export function isNative(task: HassleTask): boolean {
  return (
    task.format === "daily-parts-v2" &&
    /^v2:[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(task.id)
  );
}
export function isLegacy(task: HassleTask): boolean {
  return !task.format && !task.id.startsWith("v2:");
}
export function hassleMutation(mutation: {
  options: { mutationKey?: readonly unknown[] };
}): boolean {
  return (
    mutation.options.mutationKey?.some(
      (key) =>
        typeof key === "string" &&
        (key.startsWith("/v1/hassles") || key === "/auth/accounts/{accountId}/disconnect"),
    ) ?? false
  );
}
export function canWrite(client: QueryClient): boolean {
  return canSelect(client) && !nativeOperations(client).getSnapshot().request;
}
export function canSelect(client: QueryClient): boolean {
  const config = client.getQueryData<components["schemas"]["HassleConfig"]>(
    $api.queryOptions("get", "/v1/hassles/location", {}).queryKey,
  );
  return (
    !!config?.enabled &&
    ready(nativeOperations(client).getSnapshot().recovery) &&
    !client.isMutating({ predicate: hassleMutation })
  );
}
type Snapshot = {
  recovery?: Recovery;
  request?: GroupRequest;
  original?: HassleTask;
  uncertain?: GroupRequest;
  error: string;
  writing: boolean;
  createKey: string;
};
const stores = new WeakMap<QueryClient, NativeOperations>();
export function nativeOperations(client: QueryClient): NativeOperations {
  let store = stores.get(client);
  if (!store) stores.set(client, (store = new NativeOperations(client)));
  return store;
}
class NativeOperations {
  private listeners = new Set<() => void>();
  private snapshot: Snapshot = { error: "", writing: false, createKey: crypto.randomUUID() };
  private generation = 0;
  private authoritative = false;
  private pendingWitness?: Recovery;
  constructor(private client: QueryClient) {
    const key = $api.queryOptions("get", "/v1/hassles/location", {}).queryKey;
    const cached = client.getQueryData<components["schemas"]["HassleConfig"]>(key);
    this.update({ recovery: cached?.recovery });
    const reads = new WeakMap<object, number>();
    client.getQueryCache().subscribe((event) => {
      if (event.type !== "updated") return;
      const path = event.query.queryKey[1];
      if (path !== "/v1/hassles/location" && path !== "/v1/hassles/recovery") return;
      if (event.action.type === "fetch") {
        reads.set(event.query, this.generation);
        if (path === "/v1/hassles/recovery") this.authoritative = true;
      }
      if (reads.get(event.query) !== this.generation) return;
      // Configuration bootstraps status only. Dedicated recovery owns it thereafter.
      if (path === "/v1/hassles/location" && this.authoritative) {
        if (event.action.type === "success" && !event.action.manual) void this.status();
        return;
      }
      if (event.action.type === "success" && !event.action.manual)
        this.update({
          recovery:
            path === "/v1/hassles/recovery"
              ? (event.query.state.data as Recovery)
              : (event.query.state.data as components["schemas"]["HassleConfig"])?.recovery,
        });
      if (event.action.type === "error") this.update({ recovery: undefined });
    });
    client.getMutationCache().subscribe((event) => {
      if (
        event.type === "updated" &&
        hassleMutation(event.mutation) &&
        ["pending", "success", "error"].includes(event.action.type)
      ) {
        this.invalidateReads();
        if (
          event.action.type !== "pending" &&
          event.mutation.options.mutationKey?.some(
            (key) =>
              key === "/v1/hassles" ||
              key === "/v1/hassles/location" ||
              key === "/auth/accounts/{accountId}/disconnect",
          )
        ) {
          // These responses do not carry operation status. Do not reopen admission
          // from the old ready cache while the authoritative refresh is pending.
          this.update({ recovery: { status: "busy" } });
          void this.status();
        }
      }
    });
  }
  private invalidateReads() {
    this.generation++;
    for (const path of ["/v1/hassles/location", "/v1/hassles/recovery"] as const)
      void this.client.cancelQueries({
        exact: true,
        queryKey: $api.queryOptions("get", path, {}).queryKey,
      });
  }
  private publishResponse(recovery: unknown) {
    this.invalidateReads();
    this.authoritative = true;
    this.update({ recovery: parseRecovery(recovery) });
  }
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  getSnapshot = () => this.snapshot;
  private update(patch: Partial<Snapshot>) {
    if ("recovery" in patch) {
      patch.recovery = parseRecovery(patch.recovery);
      const previous = this.pendingWitness;
      if (
        previous &&
        (patch.recovery?.status === "pending" || patch.recovery?.status === "ready") &&
        !fenceAdvances(patch.recovery.fence, previous.fence)
      ) {
        // A failed/malformed read may hide current status, never erase the
        // last validated pending witness or authorize a same/older fence.
        patch.recovery = previous;
      }
      if (patch.recovery?.status === "pending") this.pendingWitness = patch.recovery;
    }
    this.snapshot = { ...this.snapshot, ...patch };
    this.listeners.forEach((listener) => listener());
  }
  private selected(location: HassleLocation): boolean {
    const config = this.client.getQueryData<components["schemas"]["HassleConfig"]>(
      $api.queryOptions("get", "/v1/hassles/location", {}).queryKey,
    );
    return (
      !!config?.location &&
      config.location.accountId === location.accountId &&
      config.location.calendarId === location.calendarId
    );
  }
  captureDelete(task: HassleTask, location: HassleLocation): GroupRequest | undefined {
    if (
      !canWrite(this.client) ||
      !!this.snapshot.request ||
      !this.selected(location) ||
      !isNative(task) ||
      task.groupStatus !== "ready" ||
      !task.capabilities?.delete ||
      !task.incarnation ||
      !task.revision
    )
      return;
    return structuredClone({
      location,
      group: {
        action: "delete",
        operationId: crypto.randomUUID(),
        task: task.id.slice(3),
        incarnation: task.incarnation,
        revision: task.revision,
        fence: this.snapshot.recovery!.fence!,
      },
    });
  }
  create = (location: HassleLocation, schedule: HassleSchedule): boolean => {
    if (
      !canWrite(this.client) ||
      this.snapshot.request ||
      !this.selected(location) ||
      schedule.cadence !== "hourly"
    )
      return false;
    return this.submit(
      structuredClone({
        location,
        group: {
          action: "create",
          operationId: crypto.randomUUID(),
          task: crypto.randomUUID(),
          incarnation: crypto.randomUUID(),
          fence: this.snapshot.recovery!.fence!,
          schedule: {
            ...schedule,
            cadence: "hourly",
            until: schedule.until,
            workHours: schedule.workHours ?? false,
          },
        },
      }),
    );
  };
  newCreate = () => {
    if (!canSelect(this.client) || this.snapshot.writing) return;
    this.update({
      request: undefined,
      original: undefined,
      error: "",
      createKey: crypto.randomUUID(),
    });
  };
  submit = (request: GroupRequest, original?: HassleTask): boolean => {
    if (
      !canWrite(this.client) ||
      this.snapshot.writing ||
      this.snapshot.request ||
      !this.selected(request.location) ||
      request.group.fence !== this.snapshot.recovery?.fence
    ) {
      this.update({
        error:
          "This confirmation is no longer eligible. Review the retained original operation or refresh and confirm again; it will not be rebased or resent.",
      });
      return false;
    }
    const captured = structuredClone(request);
    this.update({
      request: captured,
      original: original ? structuredClone(original) : undefined,
      writing: true,
      error: "",
    });
    const mutation = this.client.getMutationCache().build(this.client, {
      mutationKey: ["post", "/v1/hassles/groups"],
      retry: false,
      mutationFn: async () => {
        const result = await fetchClient.POST("/v1/hassles/groups", { body: captured });
        this.publishResponse(result.data?.recovery);
        if (result.response.status !== 200 || !settled(result.data, captured.group.fence))
          throw new Error(
            result.data?.error ||
              "Operation is not verified settled. Refresh operation status; do not resend.",
          );
      },
      onSuccess: () => {
        this.update({
          request: undefined,
          original: undefined,
          uncertain:
            this.snapshot.uncertain?.group.operationId === captured.group.operationId
              ? undefined
              : this.snapshot.uncertain,
          writing: false,
          error: "",
          createKey: crypto.randomUUID(),
        });
        pushToast(captured.group.action === "create" ? "Created" : "Deleted", "note");
      },
      onError: (error) => {
        this.publishResponse(error instanceof ApiError ? error.recovery : this.snapshot.recovery);
        this.update({
          writing: false,
          uncertain: captured,
          error: `${errText(error)} The operation may have applied. It will not be sent again.`,
        });
      },
      onSettled: async () => {
        await this.status();
        await this.inventory(captured.location);
      },
    });
    void mutation.execute(undefined).catch(() => {});
    return true;
  };
  status = async () => {
    this.authoritative = true;
    const key = $api.queryOptions("get", "/v1/hassles/recovery", {}).queryKey;
    await this.client.cancelQueries({ exact: true, queryKey: key });
    try {
      await this.client.fetchQuery(
        $api.queryOptions("get", "/v1/hassles/recovery", {}, { retry: false, staleTime: 0 }),
      );
    } catch {
      // Only the generation-guarded cache subscription may publish read failures.
    }
  };
  inventory = async (location: HassleLocation, throwOnError = false) => {
    if (!location.accountId || !location.calendarId) return;
    await this.client.invalidateQueries(
      {
        exact: true,
        queryKey: $api.queryOptions("get", "/v1/hassles", { params: { query: location } }).queryKey,
      },
      { cancelRefetch: false, throwOnError },
    );
  };
  refreshOperation = async () => {
    const pending = this.snapshot.recovery?.pending;
    if (
      this.snapshot.recovery?.status !== "pending" ||
      !pending?.operationId ||
      this.client.isMutating({ predicate: hassleMutation })
    )
      return;
    const captured = structuredClone(pending);
    const originalFence = this.snapshot.recovery!.fence!;
    const mutation = this.client.getMutationCache().build(this.client, {
      mutationKey: ["post", "/v1/hassles/recovery"],
      retry: false,
      mutationFn: async () => {
        const result = await fetchClient.POST("/v1/hassles/recovery", {
          body: { expectedOperationId: captured.operationId },
        });
        this.publishResponse(result.data?.recovery);
        const request = this.snapshot.request;
        if (
          result.response.status !== 200 ||
          !settled(result.data, originalFence) ||
          (request &&
            (request.group.operationId !== captured.operationId ||
              request.group.fence !== originalFence ||
              request.group.action !== captured.action ||
              request.location.accountId !== captured.location.accountId ||
              request.location.calendarId !== captured.location.calendarId))
        )
          throw new Error(
            "Operation is not verified settled. The original operation remains read-only; do not resend.",
          );
        this.update({
          request: undefined,
          original: undefined,
          uncertain:
            this.snapshot.uncertain?.group.operationId === captured.operationId &&
            this.snapshot.uncertain.group.fence === originalFence
              ? undefined
              : this.snapshot.uncertain,
          error: "",
          createKey: crypto.randomUUID(),
        });
        pushToast("Operation verified complete", "note");
      },
      onError: (error) => {
        this.publishResponse(error instanceof ApiError ? error.recovery : this.snapshot.recovery);
        this.update({ error: errText(error) });
      },
      onSettled: async () => {
        await this.status();
        await this.inventory(captured.location);
      },
    });
    await mutation.execute(undefined).catch(() => {});
  };
}

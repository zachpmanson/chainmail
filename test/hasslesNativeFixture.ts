import type { components } from "../src/lib/api/api.d";
import { fixtureLocation } from "./hasslesFixture";
type Request = components["schemas"]["HassleLocatedGroupRequest"];
type Recovery = components["schemas"]["HassleRecovery"];
type Task = components["schemas"]["HassleTask"];

/** Synthetic native provider only: never connects to Calendar or enables production storage. */
export function hasslesNativeFixture(initialLocation = fixtureLocation) {
  let recovery: Recovery = { status: "ready", fence: "11111111-1111-4111-8111-111111111111:1" };
  let tasks: Task[] = [];
  let location = structuredClone(initialLocation);
  const buckets = new Map<string, Task[]>();
  let verified = false;
  let partial = false;
  let inventoryError = false;
  const requests: { path: string; method: string; body: unknown }[] = [];
  const selectedLocation = () => location;
  const response = (data: unknown, status = 200) => ({ data, status });
  const advanceRecovery = (): Recovery => {
    const [epoch, generation] = recovery.fence!.split(":");
    return { status: "ready", fence: `${epoch}:${BigInt(generation!) + 1n}` };
  };
  const handle = (url: string, method = "GET", body?: unknown) => {
    const path = new URL(url, "http://127.0.0.1").pathname;
    requests.push({ path, method, body: structuredClone(body) });
    if (path === "/auth/status")
      return response({
        signedIn: true,
        accounts: ["offline", "other"].map((id) => ({
          id,
          email: `${id}@example.test`,
          displayName: id,
          signedIn: true,
        })),
      });
    if (path === "/v1/hassles/calendars")
      return response({
        calendars: [{ id: initialLocation.calendarId, name: "Offline reminders" }],
      });
    if (path.startsWith("/auth/accounts/") && path.endsWith("/disconnect")) return response({});
    if (path === "/v1/hassles/location") {
      if (method === "POST") {
        buckets.set(JSON.stringify(location), tasks);
        location = structuredClone((body as { location: typeof location }).location);
        tasks = buckets.get(JSON.stringify(location)) ?? [];
      }
      return response({ enabled: true, location, recovery });
    }
    if (path === "/v1/hassles/recovery") {
      if (
        method === "POST" &&
        (body as { expectedOperationId: string }).expectedOperationId !==
          recovery.pending?.operationId
      )
        return response({ error: "Wrong operation" }, 409);
      if (method === "POST") {
        if (!verified)
          return response(
            {
              settled: false,
              recovery,
              error: "Original operation is still incomplete; no reminders were resent.",
            },
            409,
          );
        recovery = advanceRecovery();
        tasks = [];
        return response({ settled: true, recovery });
      }
      return response(recovery);
    }
    if (path === "/v1/hassles")
      return inventoryError
        ? response({ error: "Inventory unavailable" }, 502)
        : response({ tasks });
    if (path === "/v1/hassles/groups" && method === "POST") {
      const { group, location } = body as Request;
      if (
        recovery.status !== "ready" ||
        group.fence !== recovery.fence ||
        JSON.stringify(location) !== JSON.stringify(selectedLocation())
      )
        return response({ error: "Conflict", recovery, settled: false }, 409);
      if (group.action === "create") {
        tasks = [
          {
            id: `v2:${group.task}`,
            revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            title: group.schedule.title,
            reminder: "Timing unavailable",
            resources: 24,
            writable: false,
            restriction: "",
            schedule: null,
            displaySchedule: group.schedule,
            incarnation: group.incarnation,
            format: "daily-parts-v2",
            groupStatus: "ready",
            capabilities: { delete: true, edit: false, instance: false, complete: false },
            instance: null,
            instanceRestriction: "",
            timing: { state: "unknown", next: null, last: null, note: "" },
          },
        ];
      } else {
        if (group.revision !== tasks[0]?.revision)
          return response({ error: "Stale revision", recovery, settled: false }, 409);
        tasks = partial
          ? tasks.map((task) => ({
              ...task,
              resources: 12,
              groupStatus: "read-only",
              capabilities: { delete: false, edit: false, instance: false, complete: false },
            }))
          : [];
      }
      if (partial) {
        recovery = {
          status: "pending",
          fence: group.fence,
          pending: {
            action: group.action,
            title: tasks[0]?.title ?? "Office cycle",
            components: 24,
            operationId: group.operationId,
            location,
          },
        };
        return response({ recovery, settled: false, error: "Partial operation" }, 409);
      }
      recovery = advanceRecovery();
      return response({ recovery, settled: true });
    }
    return response({ error: `Unexpected offline request ${method} ${path}` }, 400);
  };
  return {
    handle,
    requests,
    setRecovery: (value: Recovery) => {
      recovery = value;
    },
    setTasks: (value: Task[]) => {
      tasks = value;
    },
    verify: (value = true) => {
      verified = value;
    },
    partial: (value = true) => {
      partial = value;
    },
    inventoryError: (value = true) => {
      inventoryError = value;
    },
  };
}

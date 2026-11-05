import { wallInstant } from "../src/lib/hassles/forms";
import type { components } from "../src/lib/api/api.d";

type Location = components["schemas"]["HassleLocation"];
type Task = components["schemas"]["HassleTask"];
type Mutation = components["schemas"]["HassleMutation"];
export const fixtureLocation: Location = {
  accountId: "offline",
  calendarId: "/caldav/v2/offline@example.test/events/",
};

/** Offline UI fixture only. No Calendar transport, credentials or disk persistence. */
export function hasslesFixture() {
  let location: Location = { accountId: "", calendarId: "" };
  let tasks: Task[] = [];
  const buckets = new Map<string, Task[]>();
  let calendars = [{ id: fixtureLocation.calendarId, name: "Offline reminders" }];
  let revision = 0;
  let mode = "normal";
  const requests: { path: string; method: string; body: unknown }[] = [];
  function handle(path: string, method = "GET", body?: unknown): { status: number; data: unknown } {
    requests.push({ path, method, body });
    const url = new URL(path, "http://localhost");
    const input = body as Record<string, unknown> | undefined;
    const ok = (data: unknown) => ({ status: 200, data });
    const fail = (status: number, error: string) => ({ status, data: { error } });
    if (url.pathname === "/__hassles_fixture") {
      mode = String(input?.mode ?? "normal");
      if (mode === "reset") {
        location = { accountId: "", calendarId: "" };
        tasks = [];
        mode = "normal";
      }
      if (Array.isArray(input?.tasks)) tasks = input.tasks as Task[];
      if (Array.isArray(input?.calendars)) calendars = input.calendars as typeof calendars;
      return ok({ mode });
    }
    if (url.pathname === "/auth/status")
      return ok({
        signedIn: mode !== "disconnected",
        accounts: [
          {
            id: "offline",
            email: "offline@example.test",
            displayName: "Offline fixture",
            signedIn: mode !== "disconnected",
          },
        ],
      });
    if (url.pathname === "/v1/hassles/location") {
      if (method === "POST") {
        if (!input?.dedicated || JSON.stringify(input?.previous) !== JSON.stringify(location))
          return fail(409, "Selection changed; refresh");
        buckets.set(JSON.stringify(location), tasks);
        location = input.location as Location;
        tasks = buckets.get(JSON.stringify(location)) ?? [];
      }
      return ok({ enabled: mode !== "disabled", location });
    }
    if (url.pathname.startsWith("/v1/hassles") && mode === "disconnected")
      return fail(409, "Selected account is disconnected; reconnect it");
    if (url.pathname === "/v1/hassles/calendars") return ok({ calendars });
    if (url.pathname === "/v1/hassles") {
      if (method === "GET")
        return mode === "inventory-error" ? fail(502, "Inventory unavailable") : ok({ tasks });
      const mutation = body as Mutation;
      if (!["create", "edit", "done", "delete", "done-instance"].includes(mutation.action))
        return fail(400, "Offline fixture does not support this task action");
      if (JSON.stringify(mutation.location) !== JSON.stringify(location))
        return fail(409, "Location changed; refresh");
      if (mode === "read-only")
        return fail(502, "Calendar write denied (HTTP 403); check write permission");
      if (mode === "conflict") return fail(409, "Calendar changed; refresh before trying again");
      if (mutation.action === "create") {
        if (!mutation.schedule) return fail(400, "Schedule required");
        const id = `task:${mutation.id}`;
        const existing = tasks.find((t) => t.id === id);
        if (existing && JSON.stringify(existing.schedule) !== JSON.stringify(mutation.schedule))
          return fail(409, "Create ID already used with a different schedule");
        if (!existing)
          tasks.push({
            id,
            revision: String(++revision),
            title: mutation.schedule.title,
            reminder: `${mutation.schedule.start} Australia/Melbourne · ${mutation.schedule.cadence}`,
            resources: 1,
            writable: true,
            restriction: "",
            instance: {
              at: new Date(wallInstant(mutation.schedule.start)!)
                .toISOString()
                .replace(".000Z", "Z"),
              token: "fixture-token",
            },
            instanceRestriction: "",
            schedule: mutation.schedule,
            timing: {
              state: "upcoming",
              next: new Date(wallInstant(mutation.schedule.start)!).toISOString(),
              last: null,
              note: "",
            },
          });
      } else {
        const task = tasks.find((t) => t.id === mutation.id);
        if (!task || task.revision !== mutation.revision) return fail(409, "Stale task revision");
        if (mutation.action === "done-instance") {
          if (
            !task.writable ||
            !task.instance ||
            mutation.instanceAt !== task.instance.at ||
            mutation.instanceToken !== task.instance.token ||
            mutation.dryRun !== false
          )
            return fail(409, "Wrong instance witness");
          if (task.schedule?.cadence === "once") tasks = tasks.filter((t) => t.id !== mutation.id);
          else {
            task.timing.next = new Date(Date.parse(task.instance.at) + 86400000).toISOString();
            task.instance.at = task.timing.next.replace(".000Z", "Z");
            task.revision = String(++revision);
          }
        } else if (mutation.action === "edit") {
          if (!mutation.schedule) return fail(400, "Schedule required");
          task.title = mutation.schedule.title;
          task.schedule = mutation.schedule;
          task.timing.next = new Date(wallInstant(mutation.schedule.start)!).toISOString();
          task.reminder = `${mutation.schedule.start} Australia/Melbourne · ${mutation.schedule.cadence}`;
          task.revision = String(++revision);
        } else if (mode === "partial-delete" && task.resources > 1) {
          task.resources--;
          task.revision = String(++revision);
          return fail(502, "Removed 1 resource; remaining delete failed");
        } else tasks = tasks.filter((t) => t.id !== mutation.id);
      }
      return ok({
        removed: mutation.action === "done" || mutation.action === "delete" ? 1 : 0,
        message:
          mutation.action === "create"
            ? "Created"
            : mutation.action === "done-instance"
              ? "Completed this instance only"
              : mutation.action === "edit"
                ? "Updated"
                : "Removed all reminder resources",
      });
    }
    if (url.pathname === "/v1/stats")
      return ok({
        entries: 0,
        people: 0,
        chainRoots: 0,
        unresolved: 0,
        bySource: {},
        embeddings: [],
      });
    if (url.pathname === "/v1/people") return ok({ people: [] });
    if (url.pathname === "/v1/labels") return ok({ labels: [], accounts: [] });
    if (url.pathname === "/v1/status") return ok({ services: [], sweeping: false });
    if (url.pathname === "/v1/settings") return ok({ me: [], slurpEvery: "off" });
    if (url.pathname === "/v1/version")
      return ok({ rev: "offline-fixture", startedAt: "2026-11-04T00:00:00Z" });
    return fail(404, "Offline fixture has no upstream fallback");
  }
  return { handle, requests };
}

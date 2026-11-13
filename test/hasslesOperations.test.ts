import { QueryClient } from "@tanstack/react-query";
import { $api } from "../src/lib/api/api";
import { afterEach, expect, it, vi } from "vitest";
import { defaultSchedule, type HassleTask } from "../src/lib/hassles/forms";
import {
  hassleOperations,
  projectTasks,
  type Operation,
  type HassleRequest,
} from "../src/lib/hassles/operations";
import { clearToasts } from "../src/lib/ui/toasts";

const location = { accountId: "test", calendarId: "calendar" };
function task(id: string): HassleTask {
  return {
    id,
    revision: "original",
    title: id,
    schedule: { ...defaultSchedule(), title: id },
    resources: 1,
    writable: true,
    restriction: "",
    instance: { at: "2027-01-01T00:00:00Z", token: "original-witness" },
    instanceRestriction: "",
    reminder: "original timing",
    timing: { state: "upcoming", next: "2027-01-01T00:00:00Z", last: null, note: "" },
  };
}
function op(
  id: string,
  action: HassleRequest["action"],
  status: Operation["status"] = "writing",
): Operation {
  const original = task(id);
  return {
    token: 1,
    rowId: action === "create" ? `task:${id}` : id,
    original,
    request: {
      location,
      action,
      id,
      revision: original.revision,
      schedule:
        action === "edit" || action === "create"
          ? { ...original.schedule!, title: `${id} edited` }
          : undefined,
    },
    status,
    blocked: true,
    error: "",
  };
}
afterEach(() => {
  vi.unstubAllGlobals();
  clearToasts();
});
it("projects independent rows without mutating authoritative data, and rolls back only the failed operation", () => {
  const base = [task("A"), task("B"), task("C")];
  const copy = structuredClone(base);
  const a = op("A", "edit");
  const b = op("B", "done", "acknowledged");
  expect(projectTasks(base, [a, b]).map((t) => t.title)).toEqual(["A edited", "C"]);
  expect(projectTasks(base, [{ ...a, status: "failed" }, b]).map((t) => t.title)).toEqual([
    "A",
    "C",
  ]);
  expect(base).toEqual(copy);
});
it("deduplicates create UUIDs and never predicts timing, revision, or a completion witness", () => {
  const create = op("uuid", "create");
  const result = projectTasks([task("task:uuid")], [create]);
  expect(result).toHaveLength(1);
  for (const operation of [create, op("A", "edit"), op("A", "done-instance")]) {
    const pending = projectTasks([], [operation])[0]!;
    expect(pending.timing).toEqual({ state: "unknown", next: null, last: null, note: "" });
    expect(pending.instance).toBeNull();
    expect(pending.revision).toBe("");
    expect(pending.writable).toBe(false);
  }
});
it("restores a failed delete against the freshest base, not the captured snapshot", () => {
  const deletion = op("A", "done", "failed");
  const fresh = { ...task("A"), revision: "remainder", resources: 1, title: "Changed elsewhere" };
  expect(projectTasks([fresh, task("B")], [deletion])[0]!).toEqual(fresh);
  expect(projectTasks([task("B")], [deletion]).map((t) => t.id)).toEqual(["B"]);
});
it("isolates stores by canonical location and QueryClient", () => {
  const client = new QueryClient();
  client.setQueryData($api.queryOptions("get", "/v1/hassles/location", {}).queryKey, {
    enabled: true,
    recovery: { status: "ready", fence: "11111111-1111-4111-8111-111111111111:1" },
  });
  const first = hassleOperations(client, location);
  expect(
    hassleOperations(client, { calendarId: location.calendarId, accountId: location.accountId }),
  ).toBe(first);
  expect(hassleOperations(client, { ...location, calendarId: "other" })).not.toBe(first);
  expect(hassleOperations(new QueryClient(), location)).not.toBe(first);
  client.clear();
});

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
function json(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
function recoveryResponse(request: Request) {
  if (new URL(request.url).pathname === "/v1/hassles/recovery")
    return json({ status: "ready", fence: "11111111-1111-4111-8111-111111111111:1" });
}
async function tick() {
  await new Promise((resolve) => setTimeout(resolve, 10));
}

it("serializes writes across legacy rows", async () => {
  const client = new QueryClient();
  client.setQueryData($api.queryOptions("get", "/v1/hassles/location", {}).queryKey, {
    enabled: true,
    recovery: { status: "ready", fence: "11111111-1111-4111-8111-111111111111:1" },
  });
  const store = hassleOperations(client, location);
  const gate = deferred();
  vi.stubGlobal("fetch", async (request: Request) => {
    const recovery = recoveryResponse(request);
    if (recovery) return recovery;
    if (request.method === "POST") await gate.promise;
    return json(request.method === "GET" ? { tasks: [] } : { message: "Updated", removed: 0 });
  });
  expect(store.submit(op("A", "edit").request, task("A"))).toBe(true);
  expect(store.submit(op("B", "done").request, task("B"))).toBe(false);
  gate.resolve();
  await tick();
  await tick();
  client.clear();
});

it("ignores a cancelled stale read delivered after a newer read, without resurrecting a create", async () => {
  const client = new QueryClient();
  client.setQueryData($api.queryOptions("get", "/v1/hassles/location", {}).queryKey, {
    enabled: true,
    recovery: { status: "ready", fence: "11111111-1111-4111-8111-111111111111:1" },
  });
  const store = hassleOperations(client, location);
  const stale = deferred();
  let reads = 0;
  let saved: HassleTask | undefined;
  vi.stubGlobal("fetch", async (request: Request) => {
    const recovery = recoveryResponse(request);
    if (recovery) return recovery;
    if (request.method === "GET") {
      if (++reads === 1) {
        await stale.promise;
        return json({ tasks: [] });
      }
      return json({ tasks: saved ? [saved] : [] });
    }
    const body = (await request.json()) as HassleRequest;
    saved = { ...task(`task:${body.id}`), title: body.schedule!.title, schedule: body.schedule! };
    return json({ message: "Created", removed: 0 });
  });
  const initial = client.fetchQuery(store.options).catch(() => {});
  await tick();
  store.create({ ...defaultSchedule(), title: "Created" });
  await tick();
  await tick();
  expect(reads).toBe(2);
  expect(store.getSnapshot().operations).toEqual([]);
  stale.resolve();
  await initial;
  await tick();
  expect(
    client.getQueryData<{ tasks: HassleTask[] }>(store.options.queryKey)!.tasks[0]!.title,
  ).toBe("Created");
  client.clear();
});

it("does not retire a write from an older read or manual cache replacement; newer retry tokens survive", async () => {
  const client = new QueryClient();
  client.setQueryData($api.queryOptions("get", "/v1/hassles/location", {}).queryKey, {
    enabled: true,
    recovery: { status: "ready", fence: "11111111-1111-4111-8111-111111111111:1" },
  });
  const store = hassleOperations(client, location);
  const read = deferred(),
    write = deferred();
  let reads = 0;
  vi.stubGlobal("fetch", async (request: Request) => {
    const recovery = recoveryResponse(request);
    if (recovery) return recovery;
    if (request.method === "GET") {
      if (++reads === 1) await read.promise;
      return json({ tasks: [task("A")] });
    }
    await write.promise;
    return json({ error: "Uncertain" }, 502);
  });
  const initial = client.fetchQuery(store.options);
  await tick();
  const edit = op("A", "edit");
  store.submit(edit.request, edit.original);
  await tick();
  read.resolve();
  await initial;
  expect(store.getSnapshot().operations[0]!.status).toBe("writing");
  client.setQueryData(store.options.queryKey, { tasks: [task("A")] });
  expect(store.getSnapshot().operations).toHaveLength(1);
  write.resolve();
  await tick();
  await tick();
  const oldToken = store.getSnapshot().operations[0]!.token;
  const retry = deferred();
  vi.stubGlobal("fetch", async (request: Request) => {
    const recovery = recoveryResponse(request);
    if (recovery) return recovery;
    if (request.method === "GET") return json({ tasks: [task("A")] });
    await retry.promise;
    return json({ message: "Updated", removed: 0 });
  });
  expect(store.submit(edit.request, edit.original)).toBe(true);
  expect(store.getSnapshot().operations[0]!.token).not.toBe(oldToken);
  store.refresh();
  await tick();
  expect(store.getSnapshot().operations[0]!.status).toBe("writing");
  retry.resolve();
  await tick();
  await tick();
  expect(store.getSnapshot().operations).toHaveLength(0);
  client.clear();
});

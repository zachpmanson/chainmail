import type { components } from "../api/api.d";

type Recovery = components["schemas"]["HassleRecovery"];
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const maxGeneration = 18446744073709551615n;
function record(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}
function fence(value: unknown): { epoch: string; generation: bigint } | undefined {
  if (typeof value !== "string") return;
  const parts = value.split(":");
  if (parts.length !== 2 || !uuid.test(parts[0]!) || !/^(0|[1-9][0-9]{0,19})$/.test(parts[1]!))
    return;
  const generation = BigInt(parts[1]!);
  if (generation > maxGeneration) return;
  return { epoch: parts[0]!, generation };
}

/** Public status is untrusted, including on successful HTTP responses. */
export function parseRecovery(value: unknown): Recovery | undefined {
  if (
    !record(value) ||
    typeof value.status !== "string" ||
    !["ready", "busy", "pending", "unavailable"].includes(value.status)
  )
    return;
  if ("fence" in value && !fence(value.fence)) return;
  if (value.status === "pending") {
    const p = value.pending;
    if (
      !fence(value.fence) ||
      !record(p) ||
      typeof p.operationId !== "string" ||
      !uuid.test(p.operationId) ||
      typeof p.action !== "string" ||
      !["create", "delete"].includes(p.action) ||
      typeof p.title !== "string" ||
      !Number.isInteger(p.components) ||
      (p.components as number) < 1 ||
      (p.components as number) > 24 ||
      !record(p.location) ||
      typeof p.location.accountId !== "string" ||
      !p.location.accountId ||
      typeof p.location.calendarId !== "string" ||
      !p.location.calendarId
    )
      return;
  } else if ("pending" in value || (value.status === "ready" && !fence(value.fence))) return;
  return structuredClone(value) as Recovery;
}
export function ready(value: unknown): boolean {
  return parseRecovery(value)?.status === "ready";
}
/** A known pending witness survives failures and cannot be superseded by an older realm/counter. */
export function fenceAdvances(value: unknown, previous: unknown): boolean {
  const before = fence(previous);
  const after = fence(value);
  return (
    !!before && !!after && before.epoch === after.epoch && after.generation > before.generation
  );
}
export function settled(value: unknown, originalFence: string): boolean {
  if (!record(value) || value.settled !== true || !ready(value.recovery)) return false;
  const before = fence(originalFence);
  const after = fence((value.recovery as Recovery).fence);
  return (
    !!before &&
    !!after &&
    before.generation < maxGeneration &&
    before.epoch === after.epoch &&
    before.generation + 1n === after.generation
  );
}

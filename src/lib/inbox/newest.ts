/** By timestamp, not `best[0]`: a search's "best" isn't recency. */
export function newest<T extends { ts: string }>(entries: readonly T[]): T | undefined {
  let out: T | undefined;
  for (const e of entries) if (!out || e.ts > out.ts) out = e;
  return out;
}

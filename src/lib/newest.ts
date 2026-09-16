/**
 * The newest of a run of entries, by the timestamp the corpus stored.
 *
 * This is the message a list row is a summary of — who wrote last, when, and
 * what they said (see ChainRow) — and it is also where the reading pane lands
 * when a chain opens, so both ends of a click read the same rule rather than
 * each keeping their own idea of "the message this row is about".
 *
 * It is read off the timestamps deliberately, not taken as `best[0]`: the
 * entries attached to a chain are its top-scoring ones, and a search's idea of
 * "best" is not recency. On a browse the two coincide, and a caller that
 * silently relied on that would show the wrong message the moment it stopped
 * being true.
 */
export function newest<T extends { ts: string }>(entries: readonly T[]): T | undefined {
  let out: T | undefined;
  for (const e of entries) if (!out || e.ts > out.ts) out = e;
  return out;
}

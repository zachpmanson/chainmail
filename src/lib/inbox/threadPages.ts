/** Threads per page of the inbox list. */
export const PAGE = 50;

type Chain = { rootExtId: string; last: string };

/**
 * The `before` cursor for the page after `last`, or undefined at the end. A thread's `last`
 * is its newest message, so a long thread can straddle the cursor; stop unless the page
 * moves the cursor and adds threads, or paging loops.
 */
export function nextCursor<C extends Chain>(
  last: { chains?: C[] },
  pages: { chains?: C[] }[],
  cursor: unknown,
): string | undefined {
  const chains = last.chains ?? [];
  // A short page is the end; asking again would return the same page forever.
  if (chains.length < PAGE) return undefined;
  const oldest = chains[chains.length - 1];
  if (!oldest) return undefined;
  if (typeof cursor === "string" && cursor && oldest.last >= cursor) return undefined;
  const shown = new Set(
    pages.slice(0, -1).flatMap((p) => (p.chains ?? []).map((c) => c.rootExtId)),
  );
  return chains.some((c) => !shown.has(c.rootExtId)) ? oldest.last : undefined;
}

/** Straddling threads come back on both pages; rows key on root ext id, so keep the first. */
export function uniqueChains<C extends Chain>(pages: { chains?: C[] }[]): C[] {
  const seen = new Set<string>();
  const rows: C[] = [];
  for (const page of pages) {
    for (const c of page.chains ?? []) {
      if (seen.has(c.rootExtId)) continue;
      seen.add(c.rootExtId);
      rows.push(c);
    }
  }
  return rows;
}

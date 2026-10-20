/** Threads per page of the inbox list. */
export const PAGE = 50;

type Chain = { rootExtId: string; last: string };

/** The server owns the compound chain ordering and its continuation cursor. */
export function nextCursor(last: { nextCursor?: string }): string | undefined {
  return last.nextCursor || undefined;
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

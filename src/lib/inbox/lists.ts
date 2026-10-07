import { useRef } from "react";
import type { QueryClient } from "@tanstack/react-query";
import type { ChainHit } from "../api/api";
import { searchKey } from "../api/queryKeys";

// Optimistic list updates for writes. Every list is a /v1/search query, cached either as
// a page (`{chains}`) or as infinite-query pages (`{pages: [{chains}, …], pageParams}`).
const LISTS = { queryKey: searchKey };

/** What the caches held before a write touched them: enough to put every one back. */
export type Was = [readonly unknown[], unknown][];

/** An unreadable key yields no folder, which is the safe answer for dropFromLists. */
function folderOf(key: readonly unknown[]): string {
  const init = key[2] as { params?: { query?: { label?: unknown } } } | undefined;
  const label = init?.params?.query?.label;
  return typeof label === "string" ? label : "";
}

/** Marking unread guesses the thread length until the server answers; zero would redraw it as read. */
function countAfter(hit: ChainHit, unread: boolean): number {
  return unread ? Math.max(hit.entries ?? 1, hit.unread ?? 0) : 0;
}

/** Rebuilt only when the thread is found, so other lists keep their identity. */
function remark(data: unknown, root: string, unread: boolean): unknown {
  if (typeof data !== "object" || data === null) return data;
  const holder = data as { chains?: ChainHit[]; pages?: unknown[] };
  if (Array.isArray(holder.pages)) {
    let changed = false;
    const pages = holder.pages.map((p) => {
      const next = remark(p, root, unread);
      if (next !== p) changed = true;
      return next;
    });
    return changed ? { ...holder, pages } : data;
  }
  if (!Array.isArray(holder.chains)) return data;
  let changed = false;
  const chains = holder.chains.map((c) => {
    if (c?.rootExtId !== root) return c;
    changed = true;
    return { ...c, unread: countAfter(c, unread) };
  });
  return changed ? { ...holder, chains } : data;
}

/** One page, with some chains taken out of it. */
function without(data: unknown, roots: Set<string>): unknown {
  if (typeof data !== "object" || data === null) return data;
  const holder = data as { chains?: ChainHit[]; pages?: unknown[] };
  if (Array.isArray(holder.pages)) {
    let changed = false;
    const pages = holder.pages.map((p) => {
      const next = without(p, roots);
      if (next !== p) changed = true;
      return next;
    });
    return changed ? { ...holder, pages } : data;
  }
  if (!Array.isArray(holder.chains)) return data;
  const kept = holder.chains.filter((c) => !roots.has(c?.rootExtId));
  return kept.length === holder.chains.length ? data : { ...holder, chains: kept };
}

/** Every list the app holds, and what each of them held, before a patch. */
function each(queryClient: QueryClient, patch: (data: unknown, folder: string) => unknown): Was {
  // Otherwise an in-flight re-read can land afterwards and restore the old rows.
  void queryClient.cancelQueries(LISTS);
  const was: Was = [];
  for (const query of queryClient.getQueryCache().findAll(LISTS)) {
    was.push([query.queryKey, query.state.data]);
    const next = patch(query.state.data, folderOf(query.queryKey));
    if (next !== query.state.data) queryClient.setQueryData(query.queryKey, next);
  }
  return was;
}

/** Mark one thread read or unread in every list that holds it. */
export function markInLists(queryClient: QueryClient, root: string, unread: boolean): Was {
  return each(queryClient, (data) => remark(data, root, unread));
}

/** Only folder views lose the rows; the re-read fixes any mistakes. */
export function dropFromLists(queryClient: QueryClient, roots: string[]): Was {
  const gone = new Set(roots);
  return each(queryClient, (data, folder) => (folder === "" ? data : without(data, gone)));
}

/** Put every list back as it was: what a refused write leaves behind. */
export function putBackLists(queryClient: QueryClient, was: Was): void {
  for (const [key, data] of was) {
    if (data !== undefined) queryClient.setQueryData(key, data);
  }
}

/**
 * Keeps the pane's head after a write drops the thread from its list. Written during
 * render so the head never flashes "(no subject)"; the write is idempotent.
 */
export function useLastDescription<T extends { rootExtId: string }>(
  root: string | null | undefined,
  found: T | null,
): T | null {
  const kept = useRef<{ root: string; chain: T } | null>(null);
  if (found && kept.current?.chain !== found) {
    kept.current = { root: found.rootExtId, chain: found };
  }
  if (found || !root) return found;
  return kept.current?.root === root ? kept.current.chain : null;
}

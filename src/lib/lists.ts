import { useRef } from "react";
import type { QueryClient } from "@tanstack/react-query";
import type { ChainHit } from "./api";

/**
 * What a write does to the lists the app already holds.
 *
 * A read-state change and a mailbox verb both land on a thread the reader is
 * looking at, and both used to be answered by re-reading the list. That is the
 * honest answer and it is the slow one: the round trip to Gmail and back is
 * hundreds of milliseconds, and in that time the row the reader just pressed sits
 * there unchanged — a control that appears not to have worked, pressed again by
 * anyone who is not sure. So the list is changed first, from what the reader
 * asked for, and then re-read: the re-read is what settles anything this got
 * wrong, and a refused write is put back exactly as it was.
 *
 * Every list a thread hit appears in is the same request — the inbox's pages and
 * the search page's single page — so one key covers them all, and each cache entry
 * is one of two shapes: a page (`{chains}`), or an infinite query's pages
 * (`{pages: [{chains}, …], pageParams}`).
 */
const LISTS = { queryKey: ["get", "/v1/search"] } as const;

/** What the caches held before a write touched them: enough to put every one back. */
export type Was = [readonly unknown[], unknown][];

/** A list query's initialisation, out of the key openapi-react-query builds
 *  (`["get", "/v1/search", {params: {query: {label}}}]`). Read defensively: a key
 *  this cannot follow has no folder, which is the safe answer — see dropFromLists. */
function folderOf(key: readonly unknown[]): string {
  const init = key[2] as { params?: { query?: { label?: unknown } } } | undefined;
  const label = init?.params?.query?.label;
  return typeof label === "string" ? label : "";
}

/** The unread count a thread is given the moment the reader asks for it.
 *
 * Marking read is exact — every mailbox message in the thread loses the label, so
 * the count is nothing. Marking unread is not exact: the write marks what it can,
 * and what the count then is belongs to the server, which knows which entries have
 * a mailbox copy at all. Until it answers, the mark is the thing that matters, so
 * the count is the thread's length — an upper bound that is usually the truth,
 * rather than a zero, which would draw the row as read a moment after the reader
 * said it was not. */
function countAfter(hit: ChainHit, unread: boolean): number {
  return unread ? Math.max(hit.entries ?? 1, hit.unread ?? 0) : 0;
}

/** One page, with one thread's count changed. The object is rebuilt only when a
 *  thread was actually found, so a list that does not hold it keeps its identity and
 *  nothing re-renders for it. */
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
  // A list being re-read while the reader presses would otherwise land afterwards
  // and put the old rows back — the one race that makes an optimistic update look
  // like a bug.
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

/**
 * Take some chains out of the lists they were filed away from.
 *
 * Only out of a folder view. A thread archived out of the inbox is not in the inbox
 * any more — that is the whole of what archiving is to a reader looking at it —
 * while a thread in the corpus is still in All mail, and a list that asked for no
 * folder keeps its rows. The re-read settles anything this got wrong, which is why
 * the rule can be this blunt: a row that should have stayed comes back a moment
 * later, and one that should have gone is dealt with somewhere the reader is not
 * looking.
 */
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
 * The list's last description of the thread the pane has open, for as long as the
 * pane is on it.
 *
 * A write takes the thread out of the view it was listed in (see dropFromLists), and
 * the reader is still reading it: the pane's head describes the mail in front of
 * them, so it cannot be allowed to lose the subject and the counts the row had a
 * moment ago and fall back to "(no subject)" (and to no counts at all) because the
 * list it came from no longer lists it. The description a list gave is kept for the
 * root it named, and the asker falls back to it; the bare id is only what stands in
 * before any list has described this chain.
 *
 * It is written during the render that first has it, deliberately: the value has to
 * be decided in the render the list drops the thread in, or the head would draw one
 * frame of the wrong thing — a flicker on the very action the reader is watching.
 * The write is idempotent, so the double render in development makes no difference.
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

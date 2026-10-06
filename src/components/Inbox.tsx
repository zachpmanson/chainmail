import { Button } from "./controls";
import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { $api, type ChainHit } from "../lib/api";
import { useCompactMode } from "../lib/compactMode";
import { useEscapeToClear } from "../lib/selection";
import { ActionBar } from "./ActionBar";
import { useLastDescription } from "../lib/lists";
import { ThreadPane } from "./ThreadPane";
import { ThreadRow } from "./ThreadRow";
import { Failure, type PreviewableThread } from "./ThreadShared";
import { SplitPane } from "./SplitPane";
import { CompactListHeader } from "./CompactListControls";
import { ComposeBox } from "./ComposeBox";
import { useCompose } from "./ComposeContext";
import { FolderPicker } from "./FolderPicker";

/**
 * The home page with nothing asked of it: the corpus in the order it arrived,
 * newest first. A list rather than a ranking — the service answers an empty
 * query by each thread's last message (corpus.Query.ranked), and every row here
 * is one conversation, which is the unit a mail client shows and the unit a page
 * is built from.
 *
 * Typing a query does not filter this list: it leaves for the search route,
 * where chains are ranked and the ones that belong are ticked. The two are one
 * URL apart, which is why the search box here only navigates.
 *
 * Two panels, as a mail client reads: the list on the left, the chosen thread on
 * the right. The reading pane is the same reading the search page shows in a
 * modal — here there is room to put it beside the list, and putting it beside
 * the list is the point of an inbox.
 */

/** Rows per page. Wide enough that the first screen is a real list, narrow
 * enough that a page is read rather than scrolled past. */
const PAGE = 50;

export function Inbox() {
  const navigate = useNavigate();
  const { composing, closeCompose } = useCompose();
  const [compact] = useCompactMode();
  const [chosen, setChosen] = useState<string[]>([]);
  // Dropping the ticks is one action, wherever it is asked for from: the bar's
  // Deselect all, the write that was just made, and Escape (see the hook).
  const clearChosen = useCallback(() => setChosen([]), []);
  useEscapeToClear(chosen.length > 0, clearChosen);

  // This page is a workspace: one window, with the list and the thread scrolling
  // inside it rather than the page scrolling under them. That is a fact about the
  // page, so it is said on the body — the way a transcript says `chains` — and
  // taken off when the reader leaves, so the next page scrolls like a document
  // again. A class a page adds is that page's to remove.
  useEffect(() => {
    document.body.classList.add("inbox");
    return () => document.body.classList.remove("inbox");
  }, []);

  // Which thread the reading pane is showing. It is the URL's, not this
  // component's: a reader who reloads, or sends themselves the address, means to
  // land on the thread they were reading rather than at the top of the list —
  // and the browser's own Back then steps from a thread to the list it was
  // opened from, which no amount of local state can offer.
  //
  // Absent means nothing was picked, and then nothing is open: the pane waits to
  // be given a thread rather than filling itself in with the top of the list. What
  // the address says still matters most on a narrow screen, where being picked is
  // what switches panels.
  const opened = useSearch({ from: "/" }).open;
  const urlLabel = useSearch({ from: "/" }).label;
  const urlAccountId = useSearch({ from: "/" }).accountId;
  const settings = $api.useQuery("get", "/v1/settings", {});
  const save = $api.useMutation("post", "/v1/settings", {
    onSuccess: () => {
      void settings.refetch();
    },
  });

  // Where the list opens, and the one place the reader's own default is read.
  //
  // The address wins when it says anything, including when it says "", which is
  // All mail chosen on purpose. Only when it says nothing does the default
  // apply — and a default that is still being read is not a decision, so the
  // list waits for it (`enabled` below) rather than showing the whole corpus
  // and then swapping it out from under the reader.
  const home = urlLabel === undefined && urlAccountId === undefined;
  const label = urlLabel !== undefined ? urlLabel : (settings.data?.defaultFolder ?? "");
  const accountId = urlAccountId !== undefined
    ? urlAccountId
    : home ? settings.data?.defaultFolderAccountId : undefined;
  // The home choice is a mailbox location, not a label name: the same folder
  // name can be present in several connected accounts.
  const isDefault = (settings.data?.defaultFolder ?? "") === label
    && (settings.data?.defaultFolderAccountId ?? "") === (accountId ?? "");
  const pickFolder = (name: string, pickedAccountId?: string) =>
    navigate({ to: "/", search: (prev) => ({
      ...prev,
      label: name,
      accountId: pickedAccountId,
    }) });
  const makeDefault = (on: boolean) => save.mutate({ body: {
    defaultFolder: on ? label : "",
    defaultFolderAccountId: on ? accountId ?? "" : "",
  } });
  // Picking a mail takes the pane, and the pane holds one thing: the compose
  // panel is dismissed by the act of choosing a thread to read. Without this a
  // reader who had the compose panel open and clicked a row saw the panel stay
  // where it was — the thread they picked had opened underneath it, and they
  // were left writing to nobody (ComposeBox's own Close is the other direction,
  // for the reader who wants to go back to the mail they had open).
  const openChain = (root: string) => {
    closeCompose();
    // Already the thread the address names: the compose panel was the only thing
    // in the way, and there is no new address to push onto the reader's history.
    if (root === opened) return;
    // Merged onto whatever else the address carries, so opening a thread cannot
    // silently drop a parameter someone put there.
    navigate({ to: "/", search: (prev) => ({ ...prev, open: root }) });
  };
  const closeChain = () => navigate({ to: "/", search: (prev) => ({ ...prev, open: undefined }) });

  // Pages accumulate in one cache entry, keyed on the request: the cursor is
  // injected by pageParamName as `before`, so each page asks for what is older
  // than the last row the previous one returned.
  const inbox = $api.useInfiniteQuery(
    "get",
    "/v1/search",
    { params: { query: { limit: PAGE, ...(label ? { label } : {}), ...(accountId ? { accountId } : {}) } } },
    {
      // Held until the settings have settled, so a default folder arrives as the
      // first page rather than as a correction to it. Settled, not successful: a
      // settings read that failed still means "no default known", which is All mail
      // — waiting for a success that is not coming would be waiting forever.
      enabled: !settings.isPending,
      pageParamName: "before",
      initialPageParam: "",
      getNextPageParam: (last, pages, cursor) => {
        const chains = last.chains ?? [];
        // A short page is the end of the corpus, not a thing to ask past: the
        // server returns what it has, and asking again with the last row's stamp
        // would be answered with the same short list forever.
        if (chains.length < PAGE) return undefined;
        const oldest = chains[chains.length - 1];
        if (!oldest) return undefined;
        // A thread whose entries straddle the cursor comes back on the next page:
        // its `last` is the whole thread's newest message, not the newest inside
        // the window, so a long thread can sit above a cursor its own older
        // entries fall below. The cursor must therefore be seen to move — a page
        // ending no earlier than the one it was asked for, or one adding no
        // thread the list has not already shown, is the same page again, and
        // asking for that forever is how "Load older" becomes a spinner.
        if (cursor && oldest.last >= cursor) return undefined;
        const shown = new Set(
          pages.slice(0, -1).flatMap((p) => (p.chains ?? []).map((c) => c.rootExtId)),
        );
        return chains.some((c) => !shown.has(c.rootExtId)) ? oldest.last : undefined;
      },
    },
  );

  // A thread that straddles a page boundary is returned on both pages (the
  // cursor includes its own second, so the alternative is losing it), and a row
  // shown twice is worse than a row fetched twice: the list keys on root ext id.
  const seen = new Set<string>();
  const rows: ChainHit[] = [];
  for (const page of inbox.data?.pages ?? []) {
    for (const c of page.chains ?? []) {
      if (seen.has(c.rootExtId)) continue;
      seen.add(c.rootExtId);
      rows.push(c);
    }
  }

  const toggle = (root: string) =>
    setChosen((prev) => (prev.includes(root) ? prev.filter((r) => r !== root) : [...prev, root]));

  // Who the reader is, for the pane's own marks, is not this page's business:
  // the addresses are a setting (see the services page) and the pane is drawn
  // from the thread the thread read returns.

  // The address may name a thread this page of the list does not hold — an old
  // thread opened, then reloaded, comes back before the list has been paged that
  // far. The pane reads it from the id either way (it fetches the thread by id),
  // so the head says what is known rather than inventing a subject or a count.
  //
  // With the address naming nothing, **nothing is open**: the pane starts empty
  // and stays empty until a row is clicked. The top of the list is not a choice
  // anyone made, and a pane that filled itself in would be a thread the reader
  // has to dismiss — the same reason nothing is marked read by being looked at.
  const onlyID: PreviewableThread = { rootExtId: opened ?? "" };
  // Described by the list when the list has it, and by the last list that did when
  // a write has just taken it out of this view (see useLastDescription) — moving a
  // thread out of the inbox must not take its subject off the pane's head.
  const described = useLastDescription(opened ?? null, rows.find((c) => c.rootExtId === opened) ?? null);
  const selected: PreviewableThread | null = opened ? described ?? onlyID : null;

  // A deep link is answered twice: the pane reads the thread, and the list scrolls
  // to the row it is reading. The address is what one reader hands another —
  // "look at this thread" — and the row is the half of that the pane cannot say.
  //
  // Once per thread, not once per render: pages arrive under this effect, and a
  // reader who has scrolled away from the row is not asking to be pulled back to
  // it. Reopening the same thread later is a new deep link, which is why the note
  // of what was scrolled to is cleared when nothing is open.
  const scrolledTo = useRef<string | null>(null);
  const hasOpenRow = rows.some((c) => c.rootExtId === opened);
  useEffect(() => {
    if (!opened) {
      scrolledTo.current = null;
      return;
    }
    // The row is not on this page of the list, which an old thread opened and
    // reloaded often is not: nothing to scroll to, and the pane reads the thread
    // from the address regardless (see above).
    if (scrolledTo.current === opened || !hasOpenRow) return;
    const row = [...document.querySelectorAll<HTMLElement>(".iblist .ibrow")].find(
      (r) => r.dataset.root === opened,
    );
    if (!row) return;
    scrolledTo.current = opened;
    // Nearest rather than centred: the list is scrolled to a row, not rebuilt
    // around one, and a row already on screen is left exactly where it is.
    row.scrollIntoView?.({ block: "nearest" });
  }, [opened, hasOpenRow]);

  // Reading to the end of the list is the request for more of it: a reader who
  // keeps scrolling keeps getting rows, where a button made them say so after
  // they had already decided. The end of the list is a marker the observer
  // watches; it is not rendered once a page has failed, because a marker that
  // stays in view would fire again on every render and the corpus would be asked
  // the same question forever. A failure shows the error and a button instead.
  const end = useRef<HTMLDivElement | null>(null);
  const paging = inbox.hasNextPage === true && inbox.isFetchNextPageError !== true;
  useEffect(() => {
    const marker = end.current;
    if (!marker || !paging) return;
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) void inbox.fetchNextPage();
      },
      // Ahead of the fold: the next page lands while the reader is still going
      // through rows, rather than after they stop at the end and wait.
      { rootMargin: "300px" },
    );
    io.observe(marker);
    return () => io.disconnect();
  }, [paging, inbox.fetchNextPage]);

  return (
    <div className="wrap ibwrap mx-0 w-full max-w-none pt-0 pb-0 min-[60rem]:flex min-[60rem]:flex-1 min-[60rem]:flex-col min-[60rem]:min-h-0">
      {/* A failure with nothing to show is the whole page's; one with rows already
          on screen belongs at the end of the list, where the reader is. */}
      {inbox.isError && !inbox.data ? <Failure error={inbox.error} /> : null}
      {/* No "reading the corpus" line here: the read is the site's, not this
          page's, and it is said in the nav. */}
      {!inbox.isPending && !inbox.isError && rows.length === 0 ? (
        <p className="selnote">
          {label ? (
            <>Nothing in {label}.</>
          ) : (
            <>
              Nothing in the corpus yet — <code>corpus slurp</code> ingests the mailbox.
            </>
          )}
        </p>
      ) : null}

      <SplitPane
        hasChoice={composing || Boolean(opened)}
        list={
          <>
            <FolderPicker
              current={label}
              currentAccountId={accountId}
              isDefault={isDefault}
              onPick={pickFolder}
              onDefault={makeDefault}
            />
            <div className="iblistwrap min-w-0 min-[60rem]:min-h-0 min-[60rem]:flex-1 min-[60rem]:overflow-y-auto min-[60rem]:rounded-lg min-[60rem]:border min-[60rem]:border-line min-[60rem]:bg-card min-[60rem]:[scrollbar-gutter:stable]">
              {compact && rows.length > 0 ? <CompactListHeader /> : null}
              {rows.length > 0 ? (
                <ul className={`iblist m-0 list-none divide-y divide-line overflow-hidden rounded-lg border border-line bg-card p-0 min-[60rem]:border-0 min-[60rem]:rounded-none min-[60rem]:bg-transparent${compact ? " compact" : ""}`}>
                  {rows.map((c) => (
                    <ThreadRow
                      key={c.rootExtId}
                      thread={c}
                      checked={chosen.includes(c.rootExtId)}
                      // The pane can only be reading one thing, and while the
                      // compose panel is open it is not reading this row: a row
                      // marked current under a compose panel would be the list
                      // claiming a thread is on screen that is not. It is also
                      // what lets the row's own click take the pane back (see
                      // ThreadRow's press, which does nothing to the row it is
                      // already reading).
                      current={!composing && selected?.rootExtId === c.rootExtId}
                      compact={compact}
                      onToggle={() => toggle(c.rootExtId)}
                      onOpen={() => openChain(c.rootExtId)}
                    />
                  ))}
                </ul>
              ) : null}

              {paging ? (
                <div className="ibend py-[.6rem] text-center" ref={end}>
                  {inbox.isFetchingNextPage ? (
                    <p className="selnote m-0" role="status">
                      Reading further back…
                    </p>
                  ) : null}
                </div>
              ) : null}
              {inbox.isFetchNextPageError ? (
                <>
                  <Failure error={inbox.error} />
                  <Button type="button" className="mx-auto mt-[.7rem] block rounded-full px-[.9rem] py-[.35rem] text-[.78rem] text-[var(--muted)] hover:border-[var(--accent)] hover:text-[var(--accent)]" onClick={() => inbox.fetchNextPage()}>
                    Try again
                  </Button>
                </>
              ) : null}
            </div>
          </>
        }
        pane={composing ? <ComposeBox onClose={closeCompose} /> : (
          <ThreadPane
            thread={selected}
            label="The selected thread"
            backLabel="← List"
            empty="Nothing open — pick a thread from the list."
            moveDefault={label}
            onClose={closeChain}
          />
        )}
      />

      {/* The bar of things to do with the ticked chains, which appears once
          something is ticked: until then there is nothing to braid the page from
          and nothing to archive, and the form would be an instruction with no
          object. The same bar the search page shows, because what it acts on is
          the chains that were ticked rather than anything about the list they
          were ticked in. */}
      <ActionBar
        chosen={chosen}
        moveDefault={label}
        onDone={clearChosen}
      />
    </div>
  );
}

import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { $api, type ChainHit } from "../lib/api";
import { useEscapeToClear } from "../lib/selection";
import { ActionBar } from "./ActionBar";
import { useLastDescription } from "../lib/lists";
import { ThreadPane } from "./ThreadPane";
import { ThreadRow } from "./ThreadRow";
import { Failure, type PreviewableThread } from "./ThreadPreview";
import { SplitPane } from "./SplitPane";

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

/**
 * The folder button, and the popup it opens: the mailbox's own labels, which are
 * what a reader means by a folder. Nothing here decides what a folder should be —
 * the list is what Gmail filed the mail under, with the counts it carries, and
 * the button says which one the list below is showing.
 *
 * A popup rather than a second sidebar column: the list is already the narrow
 * column, and a folder list that is always open would cost the rows width to say
 * something that is read once and then acted on.
 */
function Folders({
  current,
  isDefault,
  onPick,
  onDefault,
}: {
  // The folder the list is showing, or "" for every folder at once. Never
  // undefined: by the time this is drawn the reader's default has been resolved.
  current: string;
  isDefault: boolean;
  onPick: (name: string) => void;
  onDefault: (on: boolean) => void;
}) {
  const folders = $api.useQuery("get", "/v1/labels", {});
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLDivElement | null>(null);

  // Closing on a click elsewhere and on Escape: a popup that closed only when
  // its own button was found again is one a reader gets stuck behind.
  useEffect(() => {
    if (!open) return;
    const away = (ev: MouseEvent) => {
      if (box.current && !box.current.contains(ev.target as Node)) setOpen(false);
    };
    const esc = (ev: KeyboardEvent) => {
      if (ev.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", away);
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("mousedown", away);
      document.removeEventListener("keydown", esc);
    };
  }, [open]);

  const labels = folders.data?.labels ?? [];
  const pick = (name: string) => {
    setOpen(false);
    onPick(name);
  };
  // Named for what it does rather than for what it is: the setting is "where
  // does chainmail open", and the folder it would open in is the one on screen.
  const here = current || "All mail";

  return (
    <div className="ibfolders" ref={box}>
      <button
        type="button"
        className="ibfbtn"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="ibfname">{here}</span>
        <span className="ibfcaret" aria-hidden="true">
          ▾
        </span>
      </button>

      {open ? (
        <div className="ibpop" role="menu" aria-label="Folders">
          {/* At the top, and stuck there, because it is not one of the folders: it
              says what happens next time chainmail is opened, and a mailbox with
              thirty labels would leave a control at the bottom below the fold of
              a list it is not part of. */}
          <button
            type="button"
            role="menuitemcheckbox"
            aria-checked={isDefault}
            className="ibfrow ibfdefault"
            onClick={() => onDefault(!isDefault)}
          >
            <span className="ibfname">Open {here} by default</span>
            <span className="ibfmark" aria-hidden="true">
              {isDefault ? "✓" : ""}
            </span>
          </button>
          <button
            type="button"
            role="menuitem"
            className={`ibfrow${current ? "" : " sel"}`}
            onClick={() => pick("")}
          >
            All mail
          </button>
          {folders.isPending ? <p className="ibfnote">Reading the mailbox…</p> : null}
          {folders.isError ? <p className="ibfnote">The folder list could not be read.</p> : null}
          {labels.map((l) => (
            <button
              key={l.name}
              type="button"
              role="menuitem"
              aria-current={l.name === current ? "true" : undefined}
              className={`ibfrow${l.name === current ? " sel" : ""}`}
              onClick={() => pick(l.name)}
            >
              <span className="ibfname">{l.name}</span>
              <span className="ibfcount">{l.messages}</span>
            </button>
          ))}
          {!folders.isPending && !folders.isError && labels.length === 0 ? (
            <p className="ibfnote">
              No message carries a label yet — the mailbox's own labels are what this list is,
              so it is empty rather than invented.
            </p>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

export function Inbox() {
  const navigate = useNavigate();
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
  const label = urlLabel !== undefined ? urlLabel : (settings.data?.defaultFolder ?? "");
  // Which folder chainmail opens in, as a state of the folder on screen: no
  // default at all opens in All mail, so All mail is what is showing when there
  // is nothing to tick.
  const isDefault = (settings.data?.defaultFolder ?? "") === label;
  const pickFolder = (name: string) =>
    navigate({ to: "/", search: (prev) => ({ ...prev, label: name }) });
  const makeDefault = (on: boolean) => save.mutate({ body: { defaultFolder: on ? label : "" } });
  const openChain = (root: string) =>
    // Merged onto whatever else the address carries, so opening a thread cannot
    // silently drop a parameter someone put there.
    navigate({ to: "/", search: (prev) => ({ ...prev, open: root }) });
  const closeChain = () => navigate({ to: "/", search: (prev) => ({ ...prev, open: undefined }) });

  // Pages accumulate in one cache entry, keyed on the request: the cursor is
  // injected by pageParamName as `before`, so each page asks for what is older
  // than the last row the previous one returned.
  const inbox = $api.useInfiniteQuery(
    "get",
    "/v1/search",
    { params: { query: { limit: PAGE, ...(label ? { label } : {}) } } },
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
    <div className="wrap ibwrap">
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
        hasChoice={Boolean(opened)}
        list={
          <>
            <Folders
              current={label}
              isDefault={isDefault}
              onPick={pickFolder}
              onDefault={makeDefault}
            />
            <div className="iblistwrap">
              {rows.length > 0 ? (
                <ul className="iblist">
                  {rows.map((c) => (
                    <ThreadRow
                      key={c.rootExtId}
                      thread={c}
                      checked={chosen.includes(c.rootExtId)}
                      current={selected?.rootExtId === c.rootExtId}
                      onToggle={() => toggle(c.rootExtId)}
                      onOpen={() => openChain(c.rootExtId)}
                    />
                  ))}
                </ul>
              ) : null}

              {paging ? (
                <div className="ibend" ref={end}>
                  {inbox.isFetchingNextPage ? (
                    <p className="selnote" role="status">
                      Reading further back…
                    </p>
                  ) : null}
                </div>
              ) : null}
              {inbox.isFetchNextPageError ? (
                <>
                  <Failure error={inbox.error} />
                  <button type="button" className="ibmore" onClick={() => inbox.fetchNextPage()}>
                    Try again
                  </button>
                </>
              ) : null}
            </div>
          </>
        }
        pane={
          <ThreadPane
            thread={selected}
            label="The selected thread"
            backLabel="← List"
            empty="Nothing open — pick a thread from the list."
            onClose={closeChain}
          />
        }
      />

      {/* The bar of things to do with the ticked chains, which appears once
          something is ticked: until then there is nothing to braid the page from
          and nothing to archive, and the form would be an instruction with no
          object. The same bar the search page shows, because what it acts on is
          the chains that were ticked rather than anything about the list they
          were ticked in. */}
      <ActionBar chosen={chosen} onDone={clearChosen} />
    </div>
  );
}

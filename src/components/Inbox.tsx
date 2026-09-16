import { useEffect, useRef, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { $api, type ChainHit, type EntryHit } from "../lib/api";
import { useBuildPage } from "../lib/build";
import {
  LIST_MIN,
  LIST_STEP,
  PANE_MIN,
  clampListWidth,
  readListWidth,
  rememberListWidth,
} from "../lib/panelWidth";
import { ChainMessages } from "./ChainMessages";
import { Failure, type PreviewableChain } from "./ChainPreview";

/**
 * The home page with nothing asked of it: the corpus in the order it arrived,
 * newest first. A list rather than a ranking — the service answers an empty
 * query by each chain's last message (corpus.Query.ranked), and every row here
 * is one conversation, which is the unit a mail client shows and the unit a page
 * is built from.
 *
 * Typing a query does not filter this list: it leaves for the search route,
 * where chains are ranked and the ones that belong are ticked. The two are one
 * URL apart, which is why the search box here only navigates.
 *
 * Two panels, as a mail client reads: the list on the left, the chosen chain on
 * the right. The reading pane is the same reading the search page shows in a
 * modal — here there is room to put it beside the list, and putting it beside
 * the list is the point of an inbox.
 */

/** Rows per page. Wide enough that the first screen is a real list, narrow
 * enough that a page is read rather than scrolled past. */
const PAGE = 50;

/**
 * A row's date, written the way a mail client writes one: the clock for today,
 * "Yesterday", then the day and month — the year only when it is not this one, so
 * a row from March does not read as this March. Local time throughout: the
 * stamps on the wire are UTC, and a person reads their own clock.
 */
export function whenShort(stamp?: string, now = new Date()): string {
  if (!stamp) return "";
  const d = new Date(stamp);
  if (Number.isNaN(d.getTime())) return stamp;
  const clock = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  const same = (a: Date, b: Date) =>
    a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
  if (same(d, now)) return clock;
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (same(d, yesterday)) return "Yesterday";
  return d.toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
    ...(d.getFullYear() === now.getFullYear() ? {} : { year: "numeric" }),
  });
}

/**
 * The newest entry of a chain, which is what a row is a summary of: who wrote
 * last, and what they said. Read off the timestamps rather than taken as
 * `best[0]` — the entries attached to a chain are its top-scoring ones, and a
 * search's idea of "best" is not recency. Here they coincide, and a row that
 * silently relied on that would show the wrong message the moment it stopped
 * being true.
 */
function newest(chain: ChainHit): EntryHit | undefined {
  let out: EntryHit | undefined;
  for (const e of chain.best ?? []) {
    if (!out || e.ts > out.ts) out = e;
  }
  return out;
}

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

/** One conversation, Gmail-minimal: who, when, what it is called, what it says,
 * and how many messages are in it when there is more than one. */
function InboxRow({
  chain,
  checked,
  current,
  onToggle,
  onOpen,
}: {
  chain: ChainHit;
  checked: boolean;
  current: boolean;
  onToggle: () => void;
  onOpen: () => void;
}) {
  const last = newest(chain);
  const subject = chain.subject || "(no subject)";
  return (
    <li className={`ibrow${current ? " sel" : ""}`}>
      {/* The tick is the selection a page is built from, and the row body is the
          thread itself, so one hit area cannot mean both. */}
      <label className="ibchk" title="include this chain in a page">
        <input
          type="checkbox"
          checked={checked}
          onChange={onToggle}
          aria-label={`Select ${subject}`}
        />
      </label>
      {/* aria-current, not a second class: the row the pane is showing is the
          current row, and a screen reader should hear it as one. */}
      <button
        type="button"
        className="ibopen"
        onClick={onOpen}
        aria-label={subject}
        aria-current={current ? "true" : undefined}
      >
        <span className="ibwho">{last?.person || "unknown sender"}</span>
        <span className="ibwhen">{whenShort(last?.ts ?? chain.last)}</span>
        <span className="ibsubj">{subject}</span>
        <span className="ibsnippet">{last?.snippet ?? ""}</span>
        {chain.entries > 1 ? (
          <span className="ibcount" title={`${chain.entries} messages in this chain`}>
            {chain.entries}
          </span>
        ) : null}
      </button>
    </li>
  );
}

export function Inbox() {
  const navigate = useNavigate();
  const [q, setQ] = useState("");
  const [chosen, setChosen] = useState<string[]>([]);
  const [title, setTitle] = useState("");
  const [me, setMe] = useState("");
  const { build, start } = useBuildPage();

  // Which chain the reading pane is showing. It is the URL's, not this
  // component's: a reader who reloads, or sends themselves the address, means to
  // land on the thread they were reading rather than at the top of the list —
  // and the browser's own Back then steps from a thread to the list it was
  // opened from, which no amount of local state can offer.
  //
  // Absent means nothing was picked, and then the pane shows the newest: the top
  // of a list is what a reader is looking at anyway, and an empty pane would be a
  // state nothing can be read from. The distinction still matters on a narrow
  // screen, where being picked is what switches panels.
  const opened = useSearch({ from: "/" }).open;
  const urlLabel = useSearch({ from: "/" }).label;
  const settings = $api.useQuery("get", "/v1/settings", {});
  const save = $api.useMutation("post", "/v1/settings", {
    onSuccess: () => void settings.refetch(),
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

  // How wide the list is, and the border the reader drags to say so.
  //
  // `listw` is what the reader chose, and null means they never did — which is
  // the layout's own width for the screen, a different thing from a width anyone
  // picked, and the reason the splitter can be reset rather than only moved.
  //
  // What the layout is *now* is read where it is needed rather than kept: a
  // measurement taken at mount is wrong the moment a reset hands the width back
  // to the grid, and the arrow keys then start from a width the list no longer
  // has. jsdom cannot see that — it lays nothing out — and a real browser caught
  // it, one key press away: ArrowRight from the default moved the border to 256
  // instead of 400. `remeasure` is only what makes React ask again when the
  // window changes size.
  const split = useRef<HTMLDivElement>(null);
  const column = useRef<HTMLDivElement>(null);
  const [listw, setListw] = useState<number | null>(readListWidth);
  // The same value as state, for the drag's end to read: the listener that ends a
  // drag was subscribed when the drag started and closes over the width from that
  // render, which is the one thing it cannot be trusted with.
  const latest = useRef<number | null>(listw);
  const [dragging, setDragging] = useState(false);
  const [, remeasure] = useState(0);
  useEffect(() => {
    const again = () => remeasure((n) => n + 1);
    window.addEventListener("resize", again);
    return () => window.removeEventListener("resize", again);
  }, []);
  const room = () => split.current?.getBoundingClientRect().width ?? 0;
  /** The list column as it is drawn: the width the reader chose, or the grid's. */
  const drawn = () => column.current?.getBoundingClientRect().width ?? 0;
  // A width the reader chose is held to what this window can afford: it was
  // dragged on some other screen, and the one they are in now may be narrower.
  const width = listw === null ? null : clampListWidth(listw, room());
  const bounds = () => ({ lo: LIST_MIN, hi: Math.max(LIST_MIN, room() - PANE_MIN) });
  /** Move the border, and put the width where the reader can watch it move. */
  const setWidth = (px: number) => {
    const { lo, hi } = bounds();
    const held = Math.round(Math.min(Math.max(px, lo), hi));
    latest.current = held;
    setListw(held);
  };
  /** Move it from a single event — a key press — and keep it for the next visit. */
  const apply = (px: number) => {
    setWidth(px);
    rememberListWidth(latest.current);
  };
  const reset = () => {
    latest.current = null;
    setListw(null);
    rememberListWidth(null);
  };
  // Dragging listens on the window rather than on the handle: the pointer leaves
  // an eight-pixel border immediately, and a drag that stopped tracking there
  // would be a drag the reader has to aim at.
  useEffect(() => {
    if (!dragging) return;
    const move = (ev: PointerEvent) => {
      const box = split.current?.getBoundingClientRect();
      if (box) setWidth(ev.clientX - box.left);
    };
    const up = () => {
      setDragging(false);
      // Once, at the end: a drag is a hundred moves, and the reader's browser has
      // no use for a hundred writes to say one width.
      rememberListWidth(latest.current);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    window.addEventListener("pointercancel", up);
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      window.removeEventListener("pointercancel", up);
    };
    // setWidth and rememberListWidth read the DOM, so they are not dependencies:
    // this effect re-subscribes when a drag starts and stops, which is all it is
    // for.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dragging]);

  const onKeyDown = (ev: React.KeyboardEvent) => {
    // From where the border is, not from where it was when the page loaded.
    const here = listw ?? drawn();
    const { lo, hi } = bounds();
    if (ev.key === "ArrowLeft") apply(Math.max(lo, here - LIST_STEP));
    else if (ev.key === "ArrowRight") apply(Math.min(hi, here + LIST_STEP));
    else if (ev.key === "Home") apply(lo);
    else if (ev.key === "End") apply(hi);
    else return;
    ev.preventDefault();
  };

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
        // A chain whose entries straddle the cursor comes back on the next page:
        // its `last` is the whole chain's newest message, not the newest inside
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

  const addresses = me
    .split(",")
    .map((a) => a.trim())
    .filter((a) => a !== "");

  const submit = (ev: React.FormEvent) => {
    ev.preventDefault();
    if (!q.trim()) return;
    // Only the query is carried over: mode, person and since have no defaults
    // worth imposing from here, and the search route applies its own.
    navigate({ to: "/", search: { q: q.trim() } });
  };

  // The address may name a chain this page of the list does not hold — an old
  // thread opened, then reloaded, comes back before the list has been paged that
  // far. The pane reads it from the id either way (it fetches the chain by id),
  // so the head says what is known rather than inventing a subject or a count.
  const onlyID: PreviewableChain = { rootExtId: opened ?? "" };
  const selected: PreviewableChain | null =
    rows.find((c) => c.rootExtId === opened) ?? (opened ? onlyID : null) ?? rows[0] ?? null;

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
      <form className="ibsearch" onSubmit={submit}>
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search the corpus"
          aria-label="Search the corpus"
        />
        <button type="submit" disabled={!q.trim()}>
          Search
        </button>
      </form>

      {/* A failure with nothing to show is the whole page's; one with rows already
          on screen belongs at the end of the list, where the reader is. */}
      {inbox.isError && !inbox.data ? <Failure error={inbox.error} /> : null}
      {inbox.isPending ? <p className="selnote">Reading the corpus…</p> : null}
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

      <div
        className={`ibsplit${opened ? " has-choice" : ""}${dragging ? " dragging" : ""}`}
        ref={split}
        // The list's width where the reader has said, and the grid's own
        // minmax() where they have not. Setting it as a custom property rather
        // than a style on the column keeps the CSS the one place that decides
        // what the two columns are.
        style={width === null ? undefined : ({ "--listw": `${width}px` } as React.CSSProperties)}
      >
        <div className="ibcol" ref={column}>
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
                  <InboxRow
                    key={c.rootExtId}
                    chain={c}
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
        </div>

        {/* The border between the panels is the control. A separator rather
            than a slider: what it changes is the border itself, and a reader who
            cannot drag it can still move it with the arrow keys. Double-click
            puts it back to the width the layout chose. */}
        <div
          className="ibdrag"
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize the list"
          aria-valuenow={Math.round(width ?? drawn())}
          aria-valuemin={LIST_MIN}
          aria-valuemax={Math.round(bounds().hi)}
          title="Drag to resize the list — double-click to reset"
          tabIndex={0}
          onPointerDown={(ev) => {
            // Left button only: the right button opens a menu, and the middle
            // one is a scroll.
            if (ev.button !== 0) return;
            ev.preventDefault();
            setDragging(true);
          }}
          onDoubleClick={reset}
          onKeyDown={onKeyDown}
        />

        {/* The pane heads itself so a narrow screen can get back to the list: the
            back button is CSS-hidden where both panels fit side by side. */}
        <aside className="ibread" aria-label="The selected chain">
          {selected ? (
            <>
              <div className="ibread-head">
                <button
                  type="button"
                  className="ibback"
                  onClick={closeChain}
                >
                  ← List
                </button>
                <span className="ibread-subj">{selected.subject || "(no subject)"}</span>
                <span className="note">
                  {selected.entries
                    ? `${selected.entries} entr${selected.entries === 1 ? "y" : "ies"}`
                    : ""}
                </span>
              </div>
              {/* The same component the page built from this thread uses, over a
                  corpus read that carries each body already rendered. */}
              <ChainMessages chain={selected} />
            </>
          ) : (
            <p className="selnote">Nothing in the corpus to read yet.</p>
          )}
        </aside>
      </div>

      {/* The build bar appears only once something is ticked: until then a page
          has nothing to be built from, and the form would be an instruction with
          no object. */}
      {chosen.length > 0 ? (
        <div className="selbuild ibbuild">
          <label className="self">
            <span>Page title</span>
            <input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="optional" />
          </label>
          <label className="self">
            <span>Your addresses</span>
            <input value={me} onChange={(e) => setMe(e.target.value)} placeholder="comma separated" />
          </label>
          <button
            type="button"
            disabled={build.isPending}
            onClick={() => start({ chains: chosen, title, me: addresses })}
          >
            {build.isPending
              ? "Building…"
              : `Build page from ${chosen.length} chain${chosen.length === 1 ? "" : "s"}`}
          </button>
          {build.isPending ? (
            <p className="selnote" role="status">
              Recovering HTML and detecting boilerplate across {chosen.length} chain
              {chosen.length === 1 ? "" : "s"}. This takes a few seconds.
            </p>
          ) : null}
          {build.isError ? <Failure error={build.error} /> : null}
        </div>
      ) : null}
    </div>
  );
}

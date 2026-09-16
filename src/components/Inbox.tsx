import { useEffect, useRef, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { $api, type ChainHit, type EntryHit } from "../lib/api";
import { useBuildPage } from "../lib/build";
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
    { params: { query: { limit: PAGE } } },
    {
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
          Nothing in the corpus yet — <code>corpus slurp</code> ingests the mailbox.
        </p>
      ) : null}

      <div className={`ibsplit${opened ? " has-choice" : ""}`}>
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

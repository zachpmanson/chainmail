import { Checkbox } from "../ui/Checkbox";
import { Button } from "../ui/controls";
import { useRef, type ReactNode } from "react";
import { type ChainHit } from "../../lib/api";
import { useMailAction, useReadAction } from "../../lib/mailActions";
import { newest } from "../../lib/newest";
import { whenShort } from "../../lib/stamp";
import { UserGroupIcon, EnvelopeIcon, PaperClipIcon } from "@heroicons/react/24/outline";
import { ArchiveGlyph, refusal, sentence, VERBS, SAID_MS } from "./MailVerbs";
import { dismissToast, pushToast } from "../../lib/toasts";

/**
 * One conversation in a list, drawn the way a mail client draws one: who wrote
 * last, when, what the thread is called, what they said, how many messages are in
 * it, and whether any of it is unread.
 *
 * It is shared by both lists — the inbox, which browses the corpus, the search
 * page, which ranks it, and the dialog that adds a found thread to a page —
 * because they are the same list of the same unit and were not: the search page
 * drew its own rows (a subject, a line of ratios, a Preview button), so finding a
 * thread and browsing to one gave two different answers to "what does a thread look
 * like". **What a ranked row adds is what the ranking knows**: the similarity and
 * the match, in the `meta` slot. Everything else about a thread is the same fact
 * on both pages, including how many people are in it.
 *
 * The tick is the selection a page is built from and the row body is the thread,
 * so they are two hit areas: the tick is a label pinned to the top-right corner
 * (see .ibchk), and the body is one button.
 */

/** The search page's relevance floor, for the highlight. A thread whose best
 *  cosine clears it is marked as a strong semantic match — the same number
 *  refresh holds semantic-only proposals to (see internal/refresh). */
export const HIGHLIGHT_FLOOR = 0.8;

/** The thread's best cosine similarity to the query, from its best entry hits. */
export function threadSimilarity(thread: ChainHit): number {
  let best = 0;
  for (const e of thread.best ?? []) {
    if (e.semRank > 0 && e.similarity !== undefined && e.similarity > best) best = e.similarity;
  }
  return best;
}

/** The span between the ends of a thread — the first message and the last — or
 *  the one date when it never got a reply. Days rather than timestamps: a thread
 *  is judged over weeks, and the clock is on the newest message in the row's own
 *  head. Undated is said rather than left blank, because an empty range reads as
 *  a range that failed to load. */
export function spanOf(thread: ChainHit): string {
  const day = (t?: string) => (t ? t.slice(0, 10) : "");
  const a = day(thread.first);
  const b = day(thread.last);
  if (!a && !b) return "undated";
  if (!b || a === b) return a || b;
  return `${a} – ${b}`;
}

/**
 * What a ranked row says that a browsed one cannot: how much of the thread the
 * query found, how close it scored, and how long the thread ran — the last at the
 * far end of the line, under everything else, because it is the one fact here
 * that is about the thread rather than about the search.
 */
export function RankMeta({ thread }: { thread: ChainHit }) {
  const sim = threadSimilarity(thread);
  return (
    <>
      <span className="ibrank font-semibold text-fg" title="matching entries of the whole thread">
        {thread.matched} of {thread.entries} matched
      </span>
      {sim > 0 ? (
        <span
          className="ibsim font-semibold text-strong"
          title="best cosine similarity of the thread"
        >
          sim {sim.toFixed(2)}
        </span>
      ) : null}
      <span className="ibspan ml-auto tabular-nums" title="the thread's first and last message">
        {spanOf(thread)}
      </span>
    </>
  );
}

/** The thread's own name, or the fact that it has none — never an empty subject
 *  line, which reads as a rendering failure rather than as a message that was
 *  sent without one. */
export const subjectOf = (thread: { subject?: string }) => thread.subject || "(no subject)";

/**
 * The two counts a thread wears, as glyphs: how many people are in it, and how
 * many messages. Shared by the list row and the head of the pane reading the same
 * thread, so one fact is one mark wherever it is read — a reader scanning the list
 * and then the pane beside it is comparing one thread with itself.
 *
 * Zero is printed rather than hidden: a thread of recovered quotes has no address
 * to count, and that is an answer.
 *
 * **Who draws it is the caller's business, and the two callers differ.** The
 * row draws the people count only from three, because two people is what a mail
 * thread is — a letter and its reply — and a mark on every row is a mark that
 * stops being read; the pane's head draws it whenever the thread read carries it,
 * because there it is a fact about the thread the reader has open rather than a
 * flourish on a line of forty of them. The mail count has the same split.
 */
export function PeopleCount({ people }: { people: number }) {
  return (
    <span
      className="ibppl inline-flex items-center gap-[.2rem] text-[.68rem] tabular-nums text-muted"
      title={`${people} people in this thread — senders and recipients`}
    >
      <UserGroupIcon width={11} height={11} aria-hidden="true" />
      {people}
    </span>
  );
}

/** The messages in the thread. The row prints it only when there is more than one
 *  — a list row's subject is its own count — but the pane's head always does,
 *  because there it is the head's own answer rather than a row's flourish. */
export function MailCount({ entries }: { entries: number }) {
  return (
    <span
      className="ibcount inline-flex items-center gap-[.2rem] text-[.68rem] tabular-nums text-muted"
      title={`${entries} messages in this thread`}
    >
      <EnvelopeIcon width={11} height={11} aria-hidden="true" />
      {entries}
    </span>
  );
}

/** The files the thread carries, as a paperclip and a count — the third of the
 *  counts, and the same mark in the pane's head (see its callers).
 *
 *  A count of files rather than of messages carrying them: the paperclip is stuck
 *  to the document, and a message with three of them is three things to open. */
export function AttachmentCount({ attachments }: { attachments: number }) {
  return (
    <span
      className="ibatt inline-flex items-center gap-[.2rem] text-[.68rem] tabular-nums text-muted"
      title={`${attachments} attachment${attachments === 1 ? "" : "s"} in this thread`}
    >
      <PaperClipIcon width={11} height={11} aria-hidden="true" />
      {attachments}
    </span>
  );
}

/** The counts shown at the end of a thread row, with the row's minimum-noise thresholds. */
export function ThreadCounts({
  people,
  entries,
  attachments,
  className = "",
}: {
  people: number;
  entries: number;
  attachments: number;
  className?: string;
}) {
  return (
    <span className={`ibtail ml-auto flex shrink-0 items-center gap-2 ${className}`.trim()}>
      {people > 2 ? <PeopleCount people={people} /> : null}
      {entries > 1 ? <MailCount entries={entries} /> : null}
      {attachments > 0 ? <AttachmentCount attachments={attachments} /> : null}
    </span>
  );
}

export function ThreadRow({
  thread,
  checked,
  current,
  meta,
  compact = false,
  onToggle,
  onOpen,
}: {
  thread: ChainHit;
  checked: boolean;
  /** Whether this is the thread the pane is reading. */
  current: boolean;
  /** What the page knows about the row that the row itself does not say — the
   *  ranking's similarity and match, on a ranked list. Absent on the inbox, which
   *  has no ranking to explain. */
  meta?: ReactNode;
  compact?: boolean;
  onToggle: () => void;
  onOpen: () => void;
}) {
  const last = newest(thread.best ?? []);
  const subject = subjectOf(thread);
  // Two clicks mark the row the other way, without opening it: the list is where a
  // reader triages, and reaching the pane's own button means walking through the
  // thread first. It is the same write as that button (`POST /v1/read`, a set of
  // one), and it is answered the same way: the row's count changes with the press
  // (see lib/lists), and the list is read back afterwards so the row ends up
  // showing what the server said rather than what the press assumed.
  //
  // Nothing is asked for a thread whose count the caller does not hold — a thread
  // named only by an address bar has no state to invert, and the gesture would
  // have no direction (see the button in ThreadPane, which is absent for the same
  // reason). And nothing is said when the write is refused: a host without
  // `-mark-read` refuses it, and the pane is where that is explained in words; the
  // list has no line to say it on — but the row goes back to the state it held,
  // because a refusal that left the optimistic answer on screen would be a mark the
  // mailbox does not agree with.
  const toggle = useReadAction();
  const flip = thread.unread === undefined ? null : thread.unread === 0;
  const archiveToast = useRef<number | null>(null);
  const sayArchive = (text: string, kind: "note" | "fail") => {
    if (archiveToast.current !== null) dismissToast(archiveToast.current);
    archiveToast.current = pushToast(text, kind, kind === "note" ? SAID_MS : null);
  };
  const archive = useMailAction({
    onError: (error, request) => {
      sayArchive(refusal(error, "-mail-write", VERBS[request.action] ?? "That change"), "fail");
    },
    onSuccess: (res) => {
      sayArchive(sentence(res.action, res.labels, res.changed, res.skipped), "note");
    },
  });
  // One click opens the thread, two clicks mark it the other way, and neither waits
  // for the other: the click says what the row is for and says it the same thing
  // however many times it is made, so the second click of a double click has
  // nothing to undo. That is the whole of how the two gestures share a row — it is
  // also how dripfeed-web's list does it (a click selects, a double click tops the
  // read flag), and its selection is idempotent for the same reason. The cost is
  // that clicking the open row again does not shut the pane: the click is the
  // opening, and closing is the pane's own way out.
  //
  // Nothing is waited for and nothing is debounced, so the browser's own double
  // click (the platform's threshold, 500ms in Chrome and Firefox) is what decides a
  // pair — the same thing it decides in dripfeed-web.
  const press = () => {
    // Already the thread the pane is reading, so the click has nothing to open: the
    // guard is here rather than at the call sites so that both lists behave the
    // same, and so a second click cannot push a second copy of the address onto the
    // reader's history.
    if (!current) onOpen();
  };
  return (
    // One class for the state, and it names the row with something unread in it:
    // the marking is a dimming of the read ones (see .ibrow in select.css), since
    // a mailbox is mostly read and the rows that still want something should be
    // the ordinary-looking ones. Before this, the unread row wore a mark of its
    // own — a count on the subject line, then a circle in the gutter — which was
    // the same claim drawn the other way round, and it left the reader hunting
    // for glyphs in a column where nearly every row carried one.
    <li
      className={[
        "ibrow group relative [--tick:1.5rem] hover:bg-[color-mix(in_srgb,var(--muted)_22%,var(--card))]",
        (thread.unread === undefined || thread.unread <= 0) &&
          "bg-[color-mix(in_srgb,var(--muted)_12%,var(--card))]",
        current && "[box-shadow:inset_2px_0_0_var(--accent)]",
        compact && "compact",
        current && "sel",
        thread.unread > 0 && "unread",
      ]
        .filter(Boolean)
        .join(" ")}
      // Which thread this row is, as an attribute rather than a ref map: a deep
      // link (`?open=`) is answered by scrolling to the row the pane is reading,
      // and that means finding a row from outside the component that drew it.
      data-root={thread.rootExtId}
    >
      {/* aria-current, not a second class: the row the pane is showing is the
          current row, and a screen reader should hear it as one. */}
      <button
        type="button"
        className={[
          "ibopen grid w-full grid-cols-[minmax(0,1fr)_auto] cursor-pointer appearance-none gap-x-3 gap-y-[.12rem] border-0 bg-transparent p-[.55rem_.35rem_.6rem_.8rem] text-left font-[inherit] text-inherit shadow-none",
          compact &&
            "grid-cols-[minmax(7rem,1fr)_minmax(0,2fr)_auto] items-center gap-y-0 py-[.42rem] pl-[.8rem] pr-[calc(var(--tick)+.7rem)]",
        ]
          .filter(Boolean)
          .join(" ")}
        onClick={press}
        // Two clicks on the row mark it the other way: the list is where a reader
        // triages, and reaching the pane's own button means walking through the
        // thread first. Left to the browser rather than counted here, because the
        // browser already knows what a double click is — and nothing pending is
        // disturbed by the two clicks arriving first.
        onDoubleClick={
          flip === null || toggle.isPending
            ? undefined
            : () => toggle.mutate({ body: { chain: thread.rootExtId, unread: flip } })
        }
        aria-label={subject}
        aria-current={current ? "true" : undefined}
        // How much of the thread is unread, in words, on the rows that have any.
        // The list says *whether* by weight, and the count is what a reader wants
        // next when they want it — four of twelve is a different row from one of
        // twelve — so it is on hover rather than drawn, where it would be the
        // number to read on a row nobody has started reading yet.
        //
        // On the row rather than on the sender: it is a fact about the thread, and
        // the sender's line is the one the eye is already on when it arrives.
        title={
          thread.unread > 0
            ? `${thread.unread} unread message${thread.unread === 1 ? "" : "s"} in this thread`
            : undefined
        }
      >
        {compact ? (
          <>
            <span
              className={`ibwho [grid-area:1/1] min-w-0 wrap-anywhere whitespace-nowrap overflow-hidden text-ellipsis text-[.88rem] ${thread.unread > 0 ? "[font-weight:750]" : "font-normal"}`}
              title={last?.person || "unknown sender"}
            >
              {last?.person || "unknown sender"}
            </span>
            <span className="ibsubrow [grid-area:1/2] mt-0 min-w-0 gap-[.45rem]">
              <span
                className={`ibsubj min-w-0 flex-1 whitespace-nowrap overflow-hidden text-ellipsis text-[.84rem] ${thread.unread > 0 ? "font-semibold" : "font-normal"}`}
                title={`${subject}${last?.snippet ? ` — ${last.snippet}` : ""}`}
              >
                {subject}
              </span>
              <ThreadCounts
                people={thread.people}
                entries={thread.entries}
                attachments={thread.attachments}
                className="group-hover:invisible"
              />
            </span>
            {meta ? (
              <span className="ibmeta [grid-area:2/2] flex min-w-0 flex-wrap gap-[.6rem] text-[.7rem] text-muted [&_.ibspan]:ml-auto [&_.ibspan]:tabular-nums [&_.ibrank]:font-semibold [&_.ibrank]:text-fg [&_.ibsim]:font-semibold [&_.ibsim]:text-strong">
                {meta}
              </span>
            ) : null}
            <span className="ibwhen [grid-area:1/3] mr-0 justify-self-end whitespace-nowrap text-[.72rem] tabular-nums text-muted group-hover:invisible">
              {whenShort(last?.ts ?? thread.last)}
            </span>
          </>
        ) : (
          <>
            <span
              className={`ibwho [grid-area:1/1] min-w-0 wrap-anywhere text-[.88rem] ${thread.unread > 0 ? "[font-weight:750]" : "font-normal"}`}
            >
              {last?.person || "unknown sender"}
            </span>
            <span className="ibwhen [grid-area:1/2] mr-[var(--tick)] justify-self-end whitespace-nowrap text-[.72rem] tabular-nums text-muted">
              {whenShort(last?.ts ?? thread.last)}
            </span>
            <span className="ibsubrow [grid-area:2/1/2/3] mt-[.2rem] gap-[.6rem]">
              {/* The subject is one line in the list, elided rather than wrapped
              (see .ibsubj), so the whole of it is on hover: a truncated subject
              is one the reader can otherwise only guess at. */}
              <span
                className={`ibsubj min-w-0 flex-1 whitespace-nowrap overflow-hidden text-ellipsis text-[.84rem] ${thread.unread > 0 ? "font-semibold" : "font-normal"}`}
                title={subject}
              >
                {subject}
              </span>
              {/* The tail of the line: how many people are in the thread, and how many
              messages (see PeopleCount and MailCount — the pane's head wears the
              same two marks for the thread it is reading). Both are counts a
              reader picks by, so both are numbers rather than words: the person
              is what the glyph says, and the mail is the glyph a mail client
              already uses for a message.

              The people count is drawn only from three (see .ibppl): two people
              is a letter and its reply, which is what a mail thread is, so the
              mark said nothing on most rows and cost the line its room for the
              ones where it did. Three is the first count that tells a reader
              this thread is a room rather than a conversation. The mail count is
              only drawn above one, because on a row it is the subject that says
              the thread is a single message. And the paperclip is drawn only when
              the thread carries something: an empty clip says nothing on a page of
              mail where most threads have no files. */}
              <ThreadCounts
                people={thread.people}
                entries={thread.entries}
                attachments={thread.attachments}
              />
            </span>
            <span className="ibsnippet [grid-area:3/1/3/3] min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-[.78rem] font-light text-muted">
              {last?.snippet ?? ""}
            </span>
            {meta ? (
              <span className="ibmeta [grid-area:4/1/4/3] flex flex-wrap gap-[.6rem] text-[.7rem] text-muted [&_.ibspan]:ml-auto [&_.ibspan]:tabular-nums [&_.ibrank]:font-semibold [&_.ibrank]:text-fg [&_.ibsim]:font-semibold [&_.ibsim]:text-strong">
                {meta}
              </span>
            ) : null}
          </>
        )}
      </button>
      {compact ? (
        <Button
          type="button"
          density="compact"
          className="ibrow-archive absolute right-[calc(var(--tick)+.4rem)] top-1/2 z-[1] hidden h-7 w-[1.875rem] -translate-y-1/2 items-center justify-center rounded-md border border-transparent bg-card p-1 text-muted group-hover:inline-flex hover:border-line hover:text-accent"
          aria-label={`Archive ${subject}`}
          title="Archive"
          disabled={archive.isPending}
          onClick={() =>
            archive.mutate({ body: { chains: [thread.rootExtId], action: "archive" } })
          }
        >
          <ArchiveGlyph />
        </Button>
      ) : null}
      {/* The tick sits on the first line, beside the time it belongs with,
          because that is the line a reader scans down when they are picking
          threads out of a list. It follows the row body rather than leading it,
          so the thread and the selection never share one hit area. */}
      <label
        className="ibchk absolute top-0 right-0 flex w-[var(--tick)] cursor-pointer justify-end pt-[.6rem] pr-[.35rem] pb-[.35rem]"
        title="include this thread in a page"
      >
        <Checkbox
          checked={checked}
          onChange={onToggle}
          aria-label={`Select ${subject}`}
          className="m-0 cursor-pointer"
        />
      </label>
    </li>
  );
}

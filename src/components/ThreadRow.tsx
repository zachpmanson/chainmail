import type { ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { $api, type ChainHit } from "../lib/api";
import { markInLists, putBackLists } from "../lib/lists";
import { newest } from "../lib/newest";
import { whenShort } from "../lib/stamp";

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
      <span className="ibrank" title="matching entries of the whole thread">
        {thread.matched} of {thread.entries} matched
      </span>
      {sim > 0 ? (
        <span className="ibsim" title="best cosine similarity of the thread">
          sim {sim.toFixed(2)}
        </span>
      ) : null}
      <span className="ibspan" title="the thread's first and last message">
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
    <span className="ibppl" title={`${people} people in this thread — senders and recipients`}>
      <svg
        viewBox="0 0 16 16"
        width="11"
        height="11"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        aria-hidden="true"
      >
        <circle cx="8" cy="4.8" r="2.7" />
        <path d="M3.2 13.6c0-2.7 2.1-4.4 4.8-4.4s4.8 1.7 4.8 4.4" />
      </svg>
      {people}
    </span>
  );
}

/** The messages in the thread. The row prints it only when there is more than one
 *  — a list row's subject is its own count — but the pane's head always does,
 *  because there it is the head's own answer rather than a row's flourish. */
export function MailCount({ entries }: { entries: number }) {
  return (
    <span className="ibcount" title={`${entries} messages in this thread`}>
      <svg
        viewBox="0 0 16 16"
        width="11"
        height="11"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <rect x="1.8" y="3.6" width="12.4" height="8.8" rx="1.2" />
        <path d="M2.2 4.8 8 9.2l5.8-4.4" />
      </svg>
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
      className="ibatt"
      title={`${attachments} attachment${attachments === 1 ? "" : "s"} in this thread`}
    >
      {/* A 24-box rather than the 16 the other two glyphs use: a paperclip is a
          spiral, and drawn from memory at 16 it comes out looking like a hook. The
          shape is the one every icon set draws, and it scales down to the same
          11px the person and the envelope are drawn at. */}
      <svg
        viewBox="0 0 24 24"
        width="11"
        height="11"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48" />
      </svg>
      {attachments}
    </span>
  );
}

export function ThreadRow({
  thread,
  checked,
  current,
  meta,
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
  onToggle: () => void;
  onOpen: () => void;
}) {
  const last = newest(thread.best ?? []);
  const subject = subjectOf(thread);
  const queryClient = useQueryClient();
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
  const toggle = $api.useMutation("post", "/v1/read", {
    onMutate: (v) => ({ was: markInLists(queryClient, v.body.chain, v.body.unread) }),
    onError: (_e, _v, ctx) => {
      if (ctx) putBackLists(queryClient, ctx.was);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["get", "/v1/search"] });
    },
  });
  const flip = thread.unread === undefined ? null : thread.unread === 0;
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
      className={`ibrow${current ? " sel" : ""}${thread.unread > 0 ? " unread" : ""}`}
      // Which thread this row is, as an attribute rather than a ref map: a deep
      // link (`?open=`) is answered by scrolling to the row the pane is reading,
      // and that means finding a row from outside the component that drew it.
      data-root={thread.rootExtId}
    >
      {/* aria-current, not a second class: the row the pane is showing is the
          current row, and a screen reader should hear it as one. */}
      <button
        type="button"
        className="ibopen"
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
        <span className="ibwho">{last?.person || "unknown sender"}</span>
        <span className="ibwhen">{whenShort(last?.ts ?? thread.last)}</span>
        <span className="ibsubrow">
          {/* The subject is one line in the list, elided rather than wrapped
              (see .ibsubj), so the whole of it is on hover: a truncated subject
              is one the reader can otherwise only guess at. */}
          <span className="ibsubj" title={subject}>
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
          <span className="ibtail">
            {thread.people > 2 ? <PeopleCount people={thread.people} /> : null}
            {thread.entries > 1 ? <MailCount entries={thread.entries} /> : null}
            {thread.attachments > 0 ? <AttachmentCount attachments={thread.attachments} /> : null}
          </span>
        </span>
        <span className="ibsnippet">{last?.snippet ?? ""}</span>
        {meta ? <span className="ibmeta">{meta}</span> : null}
      </button>
      {/* The tick sits on the first line, beside the time it belongs with,
          because that is the line a reader scans down when they are picking
          threads out of a list. It follows the row body rather than leading it,
          so the thread and the selection never share one hit area. */}
      <label className="ibchk" title="include this thread in a page">
        <input
          type="checkbox"
          checked={checked}
          onChange={onToggle}
          aria-label={`Select ${subject}`}
        />
      </label>
    </li>
  );
}
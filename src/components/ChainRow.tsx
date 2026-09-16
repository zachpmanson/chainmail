import type { ReactNode } from "react";
import type { ChainHit } from "../lib/api";
import { newest } from "../lib/newest";
import { whenShort } from "../lib/stamp";

/**
 * One conversation in a list, drawn the way a mail client draws one: who wrote
 * last, when, what the thread is called, what they said, how many messages are in
 * it, and whether any of it is unread.
 *
 * It is shared by both lists — the inbox, which browses the corpus, the search
 * page, which ranks it, and the dialog that adds a found chain to a page —
 * because they are the same list of the same unit and were not: the search page
 * drew its own rows (a subject, a line of ratios, a Preview button), so finding a
 * chain and browsing to one gave two different answers to "what does a chain look
 * like". **What a ranked row adds is what the ranking knows**: the similarity and
 * the match, in the `meta` slot. Everything else about a chain is the same fact
 * on both pages, including how many people are in it.
 *
 * The tick is the selection a page is built from and the row body is the thread,
 * so they are two hit areas: the tick is a label pinned to the top-right corner
 * (see .ibchk), and the body is one button.
 */

/** The search page's relevance floor, for the highlight. A chain whose best
 *  cosine clears it is marked as a strong semantic match — the same number
 *  refresh holds semantic-only proposals to (see internal/refresh). */
export const HIGHLIGHT_FLOOR = 0.8;

/** The chain's best cosine similarity to the query, from its best entry hits. */
export function chainSimilarity(chain: ChainHit): number {
  let best = 0;
  for (const e of chain.best ?? []) {
    if (e.semRank > 0 && e.similarity !== undefined && e.similarity > best) best = e.similarity;
  }
  return best;
}

/** The span between the ends of a chain — the first message and the last — or
 *  the one date when it never got a reply. Days rather than timestamps: a chain
 *  is judged over weeks, and the clock is on the newest message in the row's own
 *  head. Undated is said rather than left blank, because an empty range reads as
 *  a range that failed to load. */
export function spanOf(chain: ChainHit): string {
  const day = (t?: string) => (t ? t.slice(0, 10) : "");
  const a = day(chain.first);
  const b = day(chain.last);
  if (!a && !b) return "undated";
  if (!b || a === b) return a || b;
  return `${a} – ${b}`;
}

/**
 * What a ranked row says that a browsed one cannot: how much of the chain the
 * query found, how close it scored, and how long the thread ran — the last at the
 * far end of the line, under everything else, because it is the one fact here
 * that is about the thread rather than about the search.
 */
export function RankMeta({ chain }: { chain: ChainHit }) {
  const sim = chainSimilarity(chain);
  return (
    <>
      <span className="ibrank" title="matching entries of the whole chain">
        {chain.matched} of {chain.entries} matched
      </span>
      {sim > 0 ? (
        <span className="ibsim" title="best cosine similarity of the chain">
          sim {sim.toFixed(2)}
        </span>
      ) : null}
      <span className="ibspan" title="the thread's first and last message">
        {spanOf(chain)}
      </span>
    </>
  );
}

/** The chain's own name, or the fact that it has none — never an empty subject
 *  line, which reads as a rendering failure rather than as a message that was
 *  sent without one. */
export const subjectOf = (chain: { subject?: string }) => chain.subject || "(no subject)";

/**
 * The two counts a chain wears, as glyphs: how many people are in it, and how
 * many messages. Shared by the list row and the head of the pane reading the same
 * chain, so one fact is one mark wherever it is read — a reader scanning the list
 * and then the pane beside it is comparing one chain with itself.
 *
 * Zero is printed rather than hidden: a chain of recovered quotes has no address
 * to count, and that is an answer.
 */
export function PeopleCount({ people }: { people: number }) {
  return (
    <span className="ibppl" title={`${people} people in this chain — senders and recipients`}>
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

/** The messages in the chain. The row prints it only when there is more than one
 *  — a list row's subject is its own count — but the pane's head always does,
 *  because there it is the head's own answer rather than a row's flourish. */
export function MailCount({ entries }: { entries: number }) {
  return (
    <span className="ibcount" title={`${entries} messages in this chain`}>
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

export function ChainRow({
  chain,
  checked,
  current,
  meta,
  onToggle,
  onOpen,
}: {
  chain: ChainHit;
  checked: boolean;
  /** Whether this is the chain the pane is reading. */
  current: boolean;
  /** What the page knows about the row that the row itself does not say — the
   *  ranking's similarity and match, on a ranked list. Absent on the inbox, which
   *  has no ranking to explain. */
  meta?: ReactNode;
  onToggle: () => void;
  onOpen: () => void;
}) {
  const last = newest(chain.best ?? []);
  const subject = subjectOf(chain);
  return (
    <li
      className={`ibrow${current ? " sel" : ""}${chain.unread > 0 ? " unread" : ""}`}
    >
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
        <span className="ibsubrow">
          {/* The count of unread messages, before the subject the way a mail
              client puts its dot: it is the first thing scanned for when
              picking through a list, and the number rather than a dot because a
              twelve-message trail with four unread is not the same row as one
              with twelve. The row's subject is emboldened by CSS off the same
              state, so the count and the weight never disagree. */}
          {chain.unread > 0 ? (
            <span
              className="ibunread"
              title={`${chain.unread} unread message${chain.unread === 1 ? "" : "s"} in this chain`}
            >
              {chain.unread}
            </span>
          ) : null}
          <span className="ibsubj">{subject}</span>
          {/* The tail of the line: how many people are in the chain, and how many
              messages (see PeopleCount and MailCount — the pane's head wears the
              same two marks for the chain it is reading). Both are counts a
              reader picks by, so both are numbers rather than words: the person
              is what the glyph says, and the mail is the glyph a mail client
              already uses for a message.

              The people count is on every row on every page: it is a fact about
              the chain, not about the search that found it, and a chain holding
              five people reads differently from one holding one whoever asked.
              The mail count is only drawn above one, because on a row it is the
              subject that says the thread is a single message. */}
          <span className="ibtail">
            <PeopleCount people={chain.people} />
            {chain.entries > 1 ? <MailCount entries={chain.entries} /> : null}
          </span>
        </span>
        <span className="ibsnippet">{last?.snippet ?? ""}</span>
        {meta ? <span className="ibmeta">{meta}</span> : null}
      </button>
      {/* The tick sits on the first line, beside the time it belongs with,
          because that is the line a reader scans down when they are picking
          threads out of a list. It follows the row body rather than leading it,
          so the thread and the selection never share one hit area. */}
      <label className="ibchk" title="include this chain in a page">
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
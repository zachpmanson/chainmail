/**
 * The line under a bubble's head that says what the message answers: an arrow,
 * "in reply to <who>, <when>", and the link that takes the reader to it.
 *
 * One component because two renderers draw the same line from two different
 * holdings. A built page resolves a parent through the spec's rows; the reading
 * pane resolves it through the corpus entries it was handed. The two questions
 * the line answers — who wrote the message being answered, and what clock their
 * own bubble states — are the same in both, and the mark is the same mark, so
 * what a parent *is* is the caller's business and this takes the resolved target.
 *
 * A parent that is not there is said in words rather than left blank: a head
 * with no reply line on it reads as a rendering failure, and "nothing answers
 * this" is an answer — the message opens the thread. Both renderers reach that
 * state the same way, for a chain's opener and for a parent that names a message
 * the corpus does not hold (the spec carries no parent for either): the line
 * says the thread starts here, which is the page's long-standing reading of a
 * hole as well as of a root.
 */
export interface ReplyTarget {
  /** The anchor of the message being answered, as the caller's own bubbles carry
   *  it: a spec row id on a page, `entry-N` in the pane. */
  anchor: string;
  /** Who wrote it, in the words their own bubble wears — a name, or a note's
   *  label, which is what a page draws for a system note. */
  who?: string;
  /** What hovering the name says, when the caller can do better than the name
   *  alone: the same "Name <address>" a bubble wears (see lib/who). Absent for a
   *  caller that holds no address, and then the label is the whole of the title. */
  whoTitle?: string;
  /** Their clock as the parent bubble states it, e.g. "Mon 2 Mar 2026 09:15". */
  when?: string;
}

export function ReplyLink({ parent }: { parent: ReplyTarget | null }) {
  if (!parent) return <span className="tstart">thread start</span>;
  return (
    <a
      className="par"
      href={`#${parent.anchor}`}
      /* The label is what the link says; the title repeats it in full for the
         hover, and is the whole of it in column mode, where CSS collapses the
         label to the arrow alone (see .parlbl in styles.css). The address goes
         inside the title with the name rather than replacing the sentence: this
         is the only place a reader in column mode finds out which message the
         arrow answers, and it is also the only place the name's address is said. */
      title={`In reply to ${parent.whoTitle ?? parent.who}, ${parent.when}`}
    >
      <span className="arw">&#8617;</span>
      <span className="parlbl">
        in reply to <b>{parent.who}</b>, {parent.when}
      </span>
    </a>
  );
}

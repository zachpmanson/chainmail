/**
 * edits — turn a quoter's edited copy of a quoted message into what a bubble
 * draws (issue #42).
 *
 * The corpus decides at ingest that a re-quoted message with words inserted
 * inside it is a MODIFIED COPY of the message it quotes, and names the base it
 * modified. What a renderer does with that is the same in both places that draw
 * a message: the change is drawn inline inside the message that made it, diffed
 * against the original, attributed to the quoting message's own sender and clock
 * — never left floating as its own node.
 *
 * That rule is here, once, because the two callers hold different things. A page
 * build resolves ids against the spec's own rows; the reading pane resolves them
 * against the corpus entries the thread handed it. Which entries an edit names,
 * what the diff is made of, and what the attribution falls back to are the same
 * questions in both, so only the naming is the caller's: `find` says how to read
 * a person and a clock in its own vocabulary.
 */
import { editHtml } from "./editDiff";

/** A quoter's edit resolved for the bubble: diff markup plus attribution. */
export interface RowEdit {
  /** the id of the original message the change was made to (the anchor target),
   *  in whatever id space the caller's own bubbles carry */
  base: string;
  who: string;
  time: string;
  /** the quoter's modified text as diff-marked HTML (`.edel` strike / `.eins` insert) */
  html: string;
  /** the original message's sender, for "original from <y>" (may be empty) */
  origWho: string;
  /** the original message's date + time, for "at <timestamp>" (may be empty) */
  origStamp: string;
}

/** One edit as it arrives: the derived copy's id, the base it modified, and —
 *  where the writer stated them — who made the change, when, and the modified
 *  text. A page build fills all three in; a trail read leaves who and when to
 *  the bubble the edit is drawn inside. */
export interface EditRef {
  id?: string;
  base?: string;
  who?: string;
  time?: string;
  body?: string;
}

/** What a renderer must know about the entries an edit names, in its own
 *  vocabulary: the entry's rendered markup (a pasted quote's formatting lives in
 *  the copy, not in the bare original), and how this caller writes the entry's
 *  author and clock. */
export interface EditEntry {
  html: string;
  who: string;
  stamp: string;
}

/**
 * Resolve the edits attached to one message into what its bubble draws.
 *
 * `host` is the message the edit was made in — its own sender and clock are the
 * attribution, because the person who edited the quote is the person who sent
 * the message quoting it, whatever the recovered copy still says its sender was.
 * The diff is the quoter's stored text against the original's rendered body: one
 * plain text, one markup, which is what editHtml reads.
 */
export function resolveEdits(
  edits: EditRef[] | undefined,
  find: (id: string | undefined) => EditEntry | undefined,
  host: { who: string; time: string },
): RowEdit[] | undefined {
  if (!edits?.length) return undefined;
  return edits.map((ed) => {
    const copy = find(ed.id);
    const base = find(ed.base);
    return {
      base: ed.base ?? "",
      who: ed.who || host.who,
      time: ed.time || host.time,
      // The copy's markup is the surface: it is where the quoter's own formatting
      // survived (an answer written in red inside a forwarded thread), and the
      // words it does not hold are the ones the diff marks.
      html: editHtml(copy?.html ?? "", base?.html ?? "", ed.body ?? ""),
      origWho: base?.who ?? "",
      origStamp: base?.stamp ?? "",
    };
  });
}

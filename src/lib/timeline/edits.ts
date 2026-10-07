/** Resolves a quoter's edited copy of a quoted message into what a bubble draws (issue #42). */
import { editHtml } from "./editDiff";

/** A quoter's edit resolved for the bubble: diff markup plus attribution. */
export interface RowEdit {
  /** the original message's id, in the caller's own id space */
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

export interface EditRef {
  id?: string;
  base?: string;
  who?: string;
  time?: string;
  body?: string;
}

export interface EditEntry {
  html: string;
  who: string;
  stamp: string;
}

/** Attributed to `host`'s sender and clock: whoever sent the quoting message made the edit. */
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
      // The copy's markup, not the base's, holds the quoter's own formatting.
      html: editHtml(copy?.html ?? "", base?.html ?? "", ed.body ?? ""),
      origWho: base?.who ?? "",
      origStamp: base?.stamp ?? "",
    };
  });
}

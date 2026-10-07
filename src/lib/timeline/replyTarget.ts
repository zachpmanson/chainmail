import type { Row, View } from "./derive";

export interface ReplyTarget {
  /** a spec row id on a page, `entry-N` in the pane */
  anchor: string;
  /** a name, or a system note's label */
  who?: string;
  /** "Name <address>" (see lib/message/who); absent falls back to `who` */
  whoTitle?: string;
  /** Their clock as the parent bubble states it, e.g. "Mon 2 Mar 2026 09:15". */
  when?: string;
}

export function replyTarget(row: Row, v: View): ReplyTarget | null {
  const parent = row.entry.parent ? v.rows.find((r) => r.id === row.entry.parent) : undefined;
  if (!parent) return null;
  const who = parent.entry.kind === "note" ? parent.entry.label : parent.entry.sender;
  const when = [parent.entry.date, parent.entry.time].filter(Boolean).join(" ");
  return {
    anchor: parent.id,
    who,
    whoTitle: parent.entry.kind === "note" ? undefined : v.whoTitle(who ?? ""),
    when,
  };
}

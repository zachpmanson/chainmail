import type { ReplyTarget } from "../../components/thread/ReplyLink";
import type { Row, View } from "./derive";

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

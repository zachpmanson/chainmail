import { type ReactNode } from "react";
import { type ChainHit } from "../../lib/api/api";
import { newest } from "../../lib/inbox/newest";
import { whenShort } from "../../lib/ui/stamp";
import ThreadCounts from "./ThreadCounts";
import ThreadRow, { subjectOf } from "./ThreadRow";

export default function FullThreadRow({
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
  meta?: ReactNode;
  onToggle: () => void;
  onOpen: () => void;
}) {
  const last = newest(thread.best ?? []);
  const subject = subjectOf(thread);
  return (
    <ThreadRow
      thread={thread}
      checked={checked}
      current={current}
      onToggle={onToggle}
      onOpen={onOpen}
    >
      <span
        className={`[grid-area:1/1] min-w-0 wrap-anywhere text-[.88rem] ${thread.unread > 0 ? "[font-weight:750]" : "font-normal"}`}
      >
        {last?.person || "unknown sender"}
      </span>
      <span className="[grid-area:1/2] mr-6 justify-self-end whitespace-nowrap text-[.72rem] tabular-nums text-muted">
        {whenShort(last?.ts ?? thread.last)}
      </span>
      <span className="[grid-area:2/1/2/3] mt-1 gap-2">
        <span
          className={`min-w-0 flex-1 whitespace-nowrap overflow-hidden text-ellipsis text-[.84rem] ${thread.unread > 0 ? "font-semibold" : "font-normal"}`}
          title={subject}
        >
          {subject}
        </span>
        <ThreadCounts
          people={thread.people}
          entries={thread.entries}
          attachments={thread.attachments}
        />
      </span>
      <span className="[grid-area:3/1/3/3] min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-[.78rem] font-light text-muted">
        {last?.snippet ?? ""}
      </span>
      {meta ? (
        <span className="[grid-area:4/1/4/3] flex flex-wrap gap-2 text-[.7rem] text-muted">
          {meta}
        </span>
      ) : null}
    </ThreadRow>
  );
}

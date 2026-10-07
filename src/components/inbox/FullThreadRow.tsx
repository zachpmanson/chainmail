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
      openClassName="flex-col gap-0.5"
      onToggle={onToggle}
      onOpen={onOpen}
    >
      <span className="flex items-baseline gap-3">
        <span
          className={`min-w-0 flex-1 text-sm wrap-anywhere ${thread.unread > 0 ? "font-[750]" : "font-normal"}`}
        >
          {last?.person || "unknown sender"}
        </span>
        <span className="mr-6 shrink-0 text-xs whitespace-nowrap text-muted tabular-nums">
          {whenShort(last?.ts ?? thread.last)}
        </span>
      </span>
      <span className="mt-1 flex items-center gap-2">
        <span
          className={`min-w-0 flex-1 truncate text-sm ${thread.unread > 0 ? "font-semibold" : "font-normal"}`}
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
      <span className="truncate text-xs font-light text-muted">{last?.snippet ?? ""}</span>
      {meta ? <span className="flex flex-wrap gap-2 text-xs text-muted">{meta}</span> : null}
    </ThreadRow>
  );
}

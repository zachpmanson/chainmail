import { useRef, type ReactNode } from "react";
import { type ChainHit } from "../../lib/api/api";
import { newest } from "../../lib/inbox/newest";
import { whenShort } from "../../lib/ui/stamp";
import { Button } from "../ui/controls";
import { useMailAction } from "../../lib/inbox/mailActions";
import ArchiveGlyph from "./ArchiveGlyph";
import { refusal, sentence, VERBS, SAID_MS } from "./MailVerbs";
import { dismissToast, pushToast } from "../../lib/ui/toasts";
import ThreadCounts from "./ThreadCounts";
import ThreadRow, { subjectOf } from "./ThreadRow";

export default function CompactThreadRow({
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
  return (
    <ThreadRow
      thread={thread}
      checked={checked}
      current={current}
      openClassName="grid-cols-[minmax(7rem,1fr)_minmax(0,2fr)_auto] items-center gap-y-0 py-2 pl-3 pr-9"
      aside={
        <Button
          type="button"
          density="compact"
          className="absolute right-8 top-1/2 z-[1] hidden h-7 w-[1.875rem] -translate-y-1/2 items-center justify-center rounded-md border border-transparent bg-card p-1 text-muted group-hover:inline-flex hover:border-line hover:text-accent [&_svg]:size-5"
          aria-label={`Archive ${subject}`}
          title="Archive"
          disabled={archive.isPending}
          onClick={() =>
            archive.mutate({ body: { chains: [thread.rootExtId], action: "archive" } })
          }
        >
          <ArchiveGlyph />
        </Button>
      }
      onToggle={onToggle}
      onOpen={onOpen}
    >
      <span
        className={`[grid-area:1/1] min-w-0 wrap-anywhere whitespace-nowrap overflow-hidden text-ellipsis text-[.88rem] ${thread.unread > 0 ? "[font-weight:750]" : "font-normal"}`}
        title={last?.person || "unknown sender"}
      >
        {last?.person || "unknown sender"}
      </span>
      <span className="[grid-area:1/2] mt-0 min-w-0 gap-2">
        <span
          className={`min-w-0 flex-1 whitespace-nowrap overflow-hidden text-ellipsis text-[.84rem] ${thread.unread > 0 ? "font-semibold" : "font-normal"}`}
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
        <span className="[grid-area:2/2] flex min-w-0 flex-wrap gap-2 text-[.7rem] text-muted">
          {meta}
        </span>
      ) : null}
      <span className="[grid-area:1/3] mr-0 justify-self-end whitespace-nowrap text-[.72rem] tabular-nums text-muted group-hover:invisible">
        {whenShort(last?.ts ?? thread.last)}
      </span>
    </ThreadRow>
  );
}

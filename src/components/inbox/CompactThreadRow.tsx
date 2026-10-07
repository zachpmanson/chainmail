import { useRef, type ReactNode } from "react";
import { type ChainHit } from "../../lib/api/api";
import { newest } from "../../lib/inbox/newest";
import { whenShort } from "../../lib/ui/stamp";
import IconButton from "../ui/IconButton";
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
      openClassName="items-baseline gap-3 pr-9"
      aside={
        <IconButton
          // opacity, not `invisible`, so it stays reachable by keyboard.
          className="absolute top-1/2 right-8 z-[1] -translate-y-1/2 pointer-events-none opacity-0 group-hover:pointer-events-auto group-hover:opacity-100 focus-visible:opacity-100"
          aria-label={`Archive ${subject}`}
          title="Archive"
          disabled={archive.isPending}
          onClick={() =>
            archive.mutate({ body: { chains: [thread.rootExtId], action: "archive" } })
          }
        >
          <ArchiveGlyph />
        </IconButton>
      }
      onToggle={onToggle}
      onOpen={onOpen}
    >
      <span
        className={`min-w-28 flex-1 truncate text-sm ${thread.unread > 0 ? "font-[750]" : "font-normal"}`}
        title={last?.person || "unknown sender"}
      >
        {last?.person || "unknown sender"}
      </span>
      <span className="flex min-w-0 flex-2 flex-col">
        <span className="flex items-center gap-2">
          <span
            className={`min-w-0 flex-1 truncate text-sm ${thread.unread > 0 ? "font-semibold" : "font-normal"}`}
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
          <span className="flex min-w-0 flex-wrap gap-2 text-xs text-muted">{meta}</span>
        ) : null}
      </span>
      <span className="shrink-0 whitespace-nowrap text-xs tabular-nums text-muted group-hover:invisible">
        {whenShort(last?.ts ?? thread.last)}
      </span>
    </ThreadRow>
  );
}

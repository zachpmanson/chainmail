import { ArrowTopRightOnSquareIcon, QueueListIcon } from "@heroicons/react/24/outline";
import { useEffect, useRef } from "react";
import { flushSync } from "react-dom";
import { useMailAction, useReadAction } from "../../lib/inbox/mailActions";
import { useAccountId } from "../../lib/inbox/useAccountId";
import { usePrefs } from "../../lib/prefs/usePrefs";
import { withTransition } from "../../lib/thread/viewTransition";
import { dismissToast, pushToast, SAID_MS } from "../../lib/ui/toasts";
import ArchiveGlyph from "../inbox/ArchiveGlyph";
import AttachmentCount from "../inbox/AttachmentCount";
import MailCount from "../inbox/MailCount";
import { VERBS, refusal, sentence } from "../inbox/MailVerbs";
import MoveFolder from "../inbox/MoveFolder";
import PeopleCount from "../inbox/PeopleCount";
import TrashGlyph from "../inbox/TrashGlyph";
import IconButton from "../ui/IconButton";
import Button from "../ui/Button";
import ThreadMessages from "./ThreadMessages";
import type { PreviewableThread } from "./ThreadShared";

export default function ThreadPane({
  thread,
  label,
  backLabel,
  empty,
  moveDefault,
  onClose,
  openInWindow = true,
  className,
}: {
  /** null when the page has nothing to show yet; a bare root ext id is enough to fetch by */
  thread: PreviewableThread | null;
  /** The pane's accessible name, which is the page's word for what is in it. */
  label: string;
  backLabel: string;
  empty: string;
  /** Current folder when the pane was opened from a folder-scoped list. */
  moveDefault?: string;
  onClose: () => void;
  /** Whether to offer a second standalone window from this reader. */
  openInWindow?: boolean;
  className?: string;
}) {
  const accountId = useAccountId();

  const tree = usePrefs((s) => s.tree);
  const setTree = usePrefs((s) => s.setTree);

  // The pane outlives the thread, so its toast is taken down by hand when the thread changes.
  const said = useRef<number | null>(null);
  const say = (text: string, kind: "note" | "fail") => {
    if (said.current !== null) dismissToast(said.current);
    said.current = pushToast(text, kind, kind === "note" ? SAID_MS : null);
  };

  useEffect(() => {
    if (said.current !== null) dismissToast(said.current);
    said.current = null;
  }, [thread?.rootExtId]);

  // Optimistic (see lib/inbox/lists), then re-read: the server reconciles every message in the
  // thread, and a refusal puts the count back.
  const read = useReadAction({
    invalidateChain: true,
    onError: (error) => {
      // A host started without -mark-read refuses every press, so say so in words.
      say(refusal(error, "-mark-read", "Marking"), "fail");
    },
  });

  // The row leaves its folder view on press (see lib/inbox/lists); a refusal puts it back.
  const act = useMailAction({
    onSuccess: (res) => {
      say(sentence(res.action, res.labels, res.changed, res.skipped), "note");
    },
    onError: (error, request) => {
      say(refusal(error, "-mail-write", VERBS[request.action] ?? "That change"), "fail");
    },
  });

  return (
    <aside
      className={`ibread flex min-w-0 flex-col min-[60rem]:h-full min-[60rem]:min-h-0 ${className ?? ""}`}
      aria-label={label}
    >
      {thread ? (
        <>
          <div className="ibread-head flex flex-none items-center gap-2 border-b border-line bg-card px-3 py-2 min-[60rem]:px-6">
            <Button
              type="button"
              density="compact"
              className={`min-h-0 px-2 py-1 text-xs ${backLabel === "Close" ? "" : "min-[60rem]:hidden"}`}
              onClick={onClose}
            >
              {backLabel}
            </Button>
            <span className="min-w-0 wrap-break-word text-sm font-semibold">
              {thread.subject || "(no subject)"}
            </span>
            <span className="ml-auto flex items-center gap-2">
              {thread.people !== undefined ? <PeopleCount people={thread.people} /> : null}
              {thread.entries !== undefined ? <MailCount entries={thread.entries} /> : null}
              {thread.attachments !== undefined ? (
                <AttachmentCount attachments={thread.attachments} />
              ) : null}
            </span>
            {openInWindow ? (
              <IconButton
                aria-label="Open in new window"
                title="Open in new window"
                href={`/?open=${encodeURIComponent(thread.rootExtId)}&popup=1`}
                target="_blank"
                rel="noopener"
                onClick={(event) => {
                  // Deliberately a popup: if the browser blocks it, don't fall back to a tab or navigation.
                  event.preventDefault();
                  window.open(
                    event.currentTarget.href,
                    "_blank",
                    "popup=yes,width=900,height=800,resizable=yes,scrollbars=yes,noopener",
                  );
                }}
              >
                <ArrowTopRightOnSquareIcon aria-hidden="true" />
              </IconButton>
            ) : null}
            <IconButton
              aria-pressed={tree}
              aria-label={treeLabel(tree)}
              title={treeLabel(tree)}
              onClick={() => {
                // flushSync puts the state in the DOM before the transition's second snapshot (see lib/thread/viewTransition).
                withTransition(document, () => flushSync(() => setTree(!tree)));
              }}
            >
              <QueueListIcon aria-hidden="true" />
            </IconButton>
            <MoveFolder
              defaultFolder={moveDefault}
              busy={act.isPending}
              onMove={(to, targetAccountId) =>
                act.mutate({
                  body: {
                    chains: [thread.rootExtId],
                    action: "move",
                    labels: [to],
                    ...(targetAccountId ? { accountId: targetAccountId } : {}),
                  },
                })
              }
            />
            <IconButton
              aria-label="Archive"
              title="Archive"
              disabled={act.isPending}
              onClick={() =>
                act.mutate({
                  body: {
                    chains: [thread.rootExtId],
                    action: "archive",
                    ...(accountId ? { accountId } : {}),
                  },
                })
              }
            >
              <ArchiveGlyph />
            </IconButton>
            <IconButton
              aria-label="Delete"
              title="Delete"
              disabled={act.isPending}
              onClick={() =>
                act.mutate({
                  body: {
                    chains: [thread.rootExtId],
                    action: "trash",
                    ...(accountId ? { accountId } : {}),
                  },
                })
              }
            >
              <TrashGlyph />
            </IconButton>
            {/* The only read control: the pane opens the top thread by itself, so marking on open would
               clear mail nobody read. Absent when only an id is held and the state is unknown. */}
            {thread.unread !== undefined ? (
              <IconButton
                disabled={read.isPending}
                aria-label={thread.unread > 0 ? "Mark read" : "Mark unread"}
                aria-pressed={thread.unread > 0}
                title={
                  thread.unread > 0
                    ? "Unread in the mailbox — mark this thread read"
                    : "Read in the mailbox — mark this thread unread"
                }
                onClick={() =>
                  read.mutate({
                    body: {
                      chain: thread.rootExtId,
                      unread: thread.unread === 0,
                      ...(accountId ? { accountId } : {}),
                    },
                  })
                }
              >
                <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false">
                  <circle
                    cx="8"
                    cy="8"
                    r="6.4"
                    className={
                      thread.unread > 0 ? "fill-current" : "fill-none stroke-current stroke-[1.6]"
                    }
                  />
                </svg>
              </IconButton>
            ) : null}
          </div>
          <div className="min-h-0 min-w-0 flex-1 overflow-auto [&_.stream]:p-4 [&_.stream]:pb-40 [&_.stream_.pan.people]:mt-0 [&_.stream_.pan.people]:mb-4">
            <ThreadMessages thread={thread} tree={tree} />
          </div>
        </>
      ) : (
        <p className="mx-3 mt-3 flex-[1_1_100%] text-xs text-muted">{empty}</p>
      )}
    </aside>
  );
}

/** Same shape as behaviour.ts's treeLabel: the state, then the press. */
function treeLabel(on: boolean): string {
  return on
    ? "Reply tree: each answer under the message it answers — click for the order they were sent"
    : "Reply tree: the order they were sent — click to draw each answer under the message it answers";
}

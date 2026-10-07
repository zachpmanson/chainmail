import FormField from "../ui/FormField";
import IconButton from "../ui/IconButton";
import Button from "../ui/Button";
import TextInput from "../ui/TextInput";
import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { $api } from "../../lib/api/api";
import { useAccountId } from "../../lib/inbox/useAccountId";
import { useMailAction } from "../../lib/inbox/mailActions";
import { useBuildPage } from "../../lib/inbox/build";
import useOwnToast from "../../lib/ui/useOwnToast";
import Failure from "../thread/Failure";
import ArchiveGlyph from "./ArchiveGlyph";
import MoveFolder from "./MoveFolder";
import TrashGlyph from "./TrashGlyph";
import { VERBS, refusal, sentence } from "./MailVerbs";

/**
 * Read per render, not memoised: the header commits with the page's first render,
 * so an early read would stay null. Null on static pages, which have no header.
 */
function buildBarSlot(): HTMLElement | null {
  return typeof document === "undefined" ? null : document.querySelector(".buildslot");
}

export default function ActionBar({
  chosen,
  queries,
  moveDefault,
  onDone,
}: {
  /** Root ext ids of the chains to act on, in the order they were ticked. */
  chosen: string[];
  /** Current folder when the selected rows are known to share one. */
  moveDefault?: string;
  /** Recorded on the page so a later refresh can rerun the query. */
  queries?: { q: string; note?: string }[];
  onDone: () => void;
}) {
  const [title, setTitle] = useState("");
  const [braiding, setBraiding] = useState(false);
  // A later action replaces its toast rather than stacking.
  const say = useOwnToast();
  const { build, start } = useBuildPage();
  // The corpus doesn't record whose mailbox it came from, so the braid needs the reader's addresses from settings.
  const settings = $api.useQuery("get", "/v1/settings", {});
  const accountId = useAccountId();
  // The header's slot, or null on a page that has no header.
  const slot = buildBarSlot();

  // Filed out of the list view optimistically (see lib/inbox/lists); a refusal puts them back.
  const act = useMailAction({
    onSuccess: (res) => {
      say(sentence(res.action, res.labels, res.changed, res.skipped), "note");
      onDone();
    },
    onError: (error, request) => {
      say(refusal(error, "-mail-write", VERBS[request.action] ?? "That change"), "fail");
    },
  });

  if (chosen.length === 0) return null;

  const busy = act.isPending;
  const body = (
    <>
      {chosen.length > 0 ? (
        <div className="ibbuild flex h-9 min-w-0 items-center gap-2 overflow-x-auto overflow-y-hidden">
          <Button
            type="button"
            density="compact"
            className="shrink-0 px-3 py-1 text-xs text-muted hover:border-accent hover:text-accent"
            onClick={onDone}
          >
            Deselect all
          </Button>
          <Button
            type="button"
            variant="subtle"
            density="compact"
            className="px-3 py-1 text-xs"
            onClick={() => setBraiding(true)}
          >
            Braid Threads
          </Button>
          <IconButton
            aria-label="Archive"
            title="Archive"
            disabled={busy}
            onClick={() =>
              act.mutate({
                body: { chains: chosen, action: "archive", ...(accountId ? { accountId } : {}) },
              })
            }
          >
            <ArchiveGlyph />
          </IconButton>
          <IconButton
            aria-label="Delete"
            title="Delete"
            disabled={busy}
            onClick={() =>
              act.mutate({
                body: { chains: chosen, action: "trash", ...(accountId ? { accountId } : {}) },
              })
            }
          >
            <TrashGlyph />
          </IconButton>
          <MoveFolder
            defaultFolder={moveDefault}
            busy={busy}
            onMove={(to, targetAccountId) =>
              act.mutate({
                body: {
                  chains: chosen,
                  action: "move",
                  labels: [to],
                  ...(targetAccountId ? { accountId: targetAccountId } : {}),
                },
              })
            }
          />
          <span className="ml-auto flex shrink-0 items-center">
            <span className="text-xs text-muted tabular-nums">{chosen.length} selected</span>
          </span>
        </div>
      ) : null}

      {braiding ? (
        <BraidDialog
          count={chosen.length}
          title={title}
          onTitle={setTitle}
          busy={build.isPending}
          error={build.isError ? build.error : null}
          onClose={() => setBraiding(false)}
          onBraid={() => start({ chains: chosen, title, me: settings.data?.me ?? [], queries })}
        />
      ) : null}
    </>
  );

  return slot ? createPortal(body, slot) : body;
}

function BraidDialog({
  count,
  title,
  onTitle,
  busy,
  error,
  onBraid,
  onClose,
}: {
  count: number;
  title: string;
  onTitle: (t: string) => void;
  busy: boolean;
  error: unknown;
  onBraid: () => void;
  onClose: () => void;
}) {
  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-55 flex items-center justify-center bg-black/45"
      role="dialog"
      aria-modal="true"
      aria-label="Braid threads"
      onClick={onClose}
    >
      <div
        className="flex max-h-[82vh] max-w-[min(46rem,94vw)] flex-col rounded-lg border border-line bg-card shadow-[0_8px_40px_rgba(0,0,0,.35)]"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 border-b border-line px-3 py-2">
          <b className="text-xs font-bold tracking-[.09em] text-muted uppercase">braid threads</b>
          <span className="ml-auto min-w-0 truncate text-xs text-muted">
            {count} thread{count === 1 ? "" : "s"} ticked
          </span>
          <Button
            type="button"
            density="compact"
            className="bg-bg px-2 py-1 text-xs text-muted hover:border-accent hover:text-accent"
            onClick={onClose}
          >
            Close
          </Button>
        </div>
        <div className="m-3 mb-1 flex flex-wrap items-end gap-2">
          <FormField
            className="flex flex-[1_1_18rem] flex-col gap-1"
            label="Page title"
            labelClassName="text-2xs font-bold uppercase tracking-[.09em] text-muted"
          >
            <TextInput
              className="w-full px-2 py-1 text-sm"
              autoFocus
              value={title}
              onChange={(e) => onTitle(e.target.value)}
              placeholder="optional"
            />
          </FormField>
          <Button
            type="button"
            variant="subtle"
            density="compact"
            className="px-3 py-1 text-xs"
            disabled={busy}
            onClick={onBraid}
          >
            {busy ? "Braiding…" : "Braid"}
          </Button>
        </div>
        <p className="mx-3 my-2 flex-[1_1_100%] text-xs text-muted">
          Left empty, the page is titled with the earliest thread's subject.
        </p>
        {busy ? (
          <p className="mx-3 my-2 flex-[1_1_100%] text-xs text-muted" role="status">
            Recovering HTML and detecting boilerplate across {count} thread
            {count === 1 ? "" : "s"}. This takes a few seconds.
          </p>
        ) : null}
        {error ? <Failure error={error} className="mx-3 mt-2 mb-3" /> : null}
      </div>
    </div>
  );
}
